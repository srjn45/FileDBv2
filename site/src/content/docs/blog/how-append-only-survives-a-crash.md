---
title: "How append-only survives a crash"
description: A database's real test is the moment the power dies mid-write. ScrivaDB's answer is deliberately boring — never modify a byte in place, recover by truncating the torn tail, and checksum every record so bit-rot is caught on read instead of returned as data.
date: 2026-07-27
authors: srjn45
excerpt: The interesting moment in any storage engine is `kill -9` in the middle of a write. ScrivaDB's crash story is three unglamorous rules — append-only, truncate the torn tail on open, and CRC32C every record — that together mean a crash can cost you the last write but never a record that was already there.
tags:
  - scrivadb
  - storage
  - durability
  - internals
---

Every storage engine looks correct until the power goes out mid-write. That's
the moment that separates "a file I write JSON to" from "a database." So it's
worth asking of ScrivaDB directly: **what actually happens if the process is
killed halfway through a write?**

The answer is deliberately unglamorous. There's no journal replay, no
multi-phase recovery, no fsck that walks the whole store. There are three simple
rules that compose into a strong guarantee.

## Rule 1: never modify a byte in place

ScrivaDB is append-only. An insert, an update, and a delete are all the *same*
physical operation — a new line appended to the end of the current segment file.
An update doesn't seek back and overwrite the old record; it writes a new one
with a higher revision. A delete writes a tombstone line.

This is the load-bearing decision for crash safety. If you never overwrite
existing bytes, a crash **cannot corrupt a record that was already on disk** —
there is no in-place mutation for it to interrupt. The worst a crash can do is
damage the write that was in flight, and that write is always at the *tail* of
the file, never in the middle.

```
seg_000001.ndjson
  {"id":"01J8...","op":"put","rev":1,"data":{...},"crc":"a1b2"}   ← safe, sealed by later writes
  {"id":"01J8...","op":"put","rev":2,"data":{...},"crc":"c3d4"}   ← safe
  {"id":"01J8...","op":"put","rev":3,"data":{...},"cr             ← crash hit here
```

Everything above that torn last line is untouched and intact. The blast radius
of a crash is exactly one record: the one you hadn't finished.

## Rule 2: on open, truncate the torn tail

So when ScrivaDB opens a segment, it doesn't trust that the file ends cleanly.
It seeks backward from the end to find the last complete line — the last one
terminated by a newline — and **truncates anything after it**. That half-written
final record from the crash simply disappears, and the segment is left at a known-good
boundary, ready to append to again.

This is why recovery is O(seek), not O(scan): the only place damage can live is
the very end, so that's the only place recovery has to look. A clean shutdown and
a `kill -9` converge to the same valid state on the next open — the crash just
costs you the one unacknowledged write, which the caller never got a success for
anyway.

## Rule 3: checksum every record, verify on read

Truncation handles the write that was *interrupted*. But there's a subtler
failure: a record that was written fine and then **rotted** — a flipped bit from
a bad disk, a dodgy cable, a cosmic ray. The bytes are all there and the line is
newline-terminated, so truncation won't catch it. Left alone, the engine would
hand you silently-wrong data, which is worse than an error.

So every entry carries a **CRC32C (Castagnoli)** checksum, computed over its
identifying fields and its data and stored on the line itself. On read, the
checksum is recomputed and compared. A mismatch is surfaced as a corruption
error — the read *fails loudly* instead of returning data that looks valid but
isn't.

CRC32C specifically because it's cheap (hardware-accelerated on modern CPUs, so
it's nearly free on the read path) and it's the right tool for the job it's
doing: catching accidental bit-rot, not defending against a malicious tamperer.
(For that threat you'd reach for [the AEAD tag that comes with
encryption-at-rest](/scriva/blog/encrypting-a-database-you-can-cat/) — a
different primitive for a different problem.)

## Why the three compose

Put together, the guarantee is easy to state:

- **A crash costs you at most the last in-flight write** — and nothing you
  received a success for. (Append-only + tail truncation.)
- **Silent corruption of a record that was already durable is caught, not
  returned.** (Per-record CRC32C.)

None of these three rules is clever on its own. Truncating a partial line is
obvious. CRC32C is decades old. Append-only is the oldest trick in the log-structured
book. The engineering is in choosing all three *together* and letting a
background compactor reclaim the space from superseded records later — so you get
durable, crash-safe writes without a write-ahead log, and a store whose recovery
path is short enough to reason about in a single sitting.

Boring is the goal. When the power comes back, you want your database to come up
in a known state and get on with it — and to do that by truncating one line, not
by replaying a journal you have to trust.

---

- [What is ScrivaDB?](/scriva/start/what-is-scriva/) — the append-only model end to end.
- [Durability & backup](/scriva/guides/durability-and-backup/) — fsync policies and point-in-time recovery.
