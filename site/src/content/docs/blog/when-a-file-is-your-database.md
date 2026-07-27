---
title: "When a file is your database"
description: Not every app needs Postgres. ScrivaDB is a lightweight, append-only, file-based document database — human-readable NDJSON on disk, a gRPC + REST API from one binary, and an embeddable Go engine. Here's the problem it solves, where it fits, and where it doesn't.
date: 2026-07-27
authors: srjn45
excerpt: Half the time you reach for a database, what you actually needed was a file you could trust. ScrivaDB is the space between `json.Marshal` to disk and standing up Postgres — durable, queryable, and still just text you can `cat`.
tags:
  - scrivadb
  - storage
  - local-first
  - go
---

Most projects start with the same small lie: *"I'll just write it to a JSON
file for now."*

It works until it doesn't. The file grows. Two goroutines write at once and you
get half a record. A crash mid-write truncates the whole thing. You add a
mutex, then a temp-file-and-rename, then a backup copy, then an index because a
linear scan got slow — and somewhere along the way you've hand-rolled a bad
database. So you give up and stand up PostgreSQL: a daemon, a schema, a
connection pool, a migration tool, and a container in every environment — to
store a few thousand records that fit in RAM.

**ScrivaDB is the thing that belongs in the gap between those two.** Durable and
queryable like a database, but on disk it's still just text you can read.

## The problem it solves

The gap is real and most stacks ignore it. On one side, a flat file is honest
and inspectable but has no concurrency safety, no crash durability, no query
engine, and no integrity checks. On the other, a full RDBMS gives you all of
that — and an operational tax you pay forever: a separate process to run and
monitor, a wire protocol, credentials, backups, version upgrades.

ScrivaDB's bet is that a large class of software — CLI tools, local services,
IoT daemons, desktop apps, small web backends, embedded state stores — needs the
*guarantees* of a database without the *operational footprint* of one. So it
keeps the guarantees and throws the footprint away:

- **Append-only writes.** Inserts, updates, and deletes are always new lines
  appended to a segment file. Nothing is ever modified in place, so a crash
  can't corrupt an existing record — the worst case is a torn final line, which
  is detected and skipped on load. A background compactor later merges and
  deduplicates sealed segments.
- **End-to-end integrity.** Every entry carries a CRC32C checksum. Silent
  bit-rot on disk is caught on *read* and surfaced as an error, instead of
  quietly handing you wrong data.
- **A real query engine.** Secondary indexes give O(1) equality and range
  lookups; `Find` streams results with keyset pagination; aggregations run
  server-side. It's not "load everything and filter in a loop."
- **More than key-value.** Caller-supplied keys, per-record revisions with
  compare-and-swap, upsert, TTL, optimistic transactions, and live `Watch`
  subscriptions.

## How it simplifies local storage

The part that changes how it *feels* to operate is the on-disk format. A
collection is a set of **NDJSON segment files** — one JSON object per line,
appended in order. There is no binary blob, no page format, no WAL you can't
read. Your data is text:

```bash
$ scriva-cli insert users '{"name":"alice","age":30}' --api-key dev-key
  ✓ id=01J8...  key=01J8...  rev=1

$ tail -1 data/users/000001.ndjson      # your data is just text
  {"id":"01J8...","key":"01J8...","rev":1,"data":{"name":"alice","age":30},"crc":"a1b2"}
```

That one property collapses a surprising number of operational chores into
tools you already know:

- **Inspect** with `cat`, `grep`, `jq`, `tail -f`. No client, no REPL, no
  `SELECT` required to see what's in there.
- **Back up** by streaming the directory through `gzip`. **Restore** is
  `tar xzf`. Your backup is a file, not a `pg_dump` ritual.
- **Debug** by reading. When something looks wrong, the source of truth is a
  text file you can open — not an opaque store you have to interrogate through
  the very layer you suspect.
- **Migrate and diff** with line-oriented Unix tools and version control
  intuition, because every write is a line.

And it's **one small Go binary**. No JVM, no Python runtime, no daemon zoo, no
config file required to get started. `scriva serve` gives you gRPC on `:5433`
and REST on `:8080` from a single process. Or skip the server entirely:
`go get` the engine and run the whole database **in-process**, with no network
and no separate daemon — keyed CRUD, CAS, secondary indexes, and in-process
`Watch`, embedded directly in your Go program.

## Where to use it

ScrivaDB is the right tool when:

- **You need persistence without operating a database.** A CLI, a desktop app,
  an IoT or edge daemon, a small internal service — anything where "install and
  run Postgres" is heavier than the problem deserves.
- **Your working set fits on one machine.** Single-node data that you're happy
  to keep on local disk.
- **You value being able to read your own data.** Auditability, debuggability,
  and "just look at the file" are features you'll actually use.
- **You want one API from any language.** REST and gRPC come from the same
  binary, and there are client SDKs across ten languages.
- **You're writing Go and want it embedded.** In-process storage with real
  indexes and `Watch`, and zero gRPC/Prometheus/OTel dependencies pulled into
  your binary.

warden — a fleet supervisor for AI coding agents — embeds ScrivaDB as exactly
this: the local store for sessions, pipelines, and agent state, with no external
database to run alongside it.

## Where *not* to use it

Being honest about the edges is the whole point of choosing a small tool
deliberately. Reach for something else when:

- **Your dataset is too large to compact on a single machine.** ScrivaDB is
  single-node by design. If you need horizontal sharding, it's the wrong shape.
- **You need multi-node consensus and automatic failover.** Replication is
  leader→follower with **manual** failover — deliberately simple. There's no
  Raft, no automatic leader election. That's a feature for small deployments and
  a dealbreaker for high-availability ones.
- **You need complex relational queries.** There are secondary indexes,
  ranges, and aggregations — but no joins and no SQL planner. If your access
  pattern is a five-table join, use a relational database.
- **You have very high-cardinality, write-heavy churn** where compaction can't
  keep up, or you need the maturity and ecosystem of a decades-old engine for a
  system of record at scale.

Picking a database is mostly picking your constraints. ScrivaDB's constraints
are "one machine, readable on disk, no daemon to babysit" — and in exchange you
get durability, integrity, and a real query engine without the operational
weight. If that trade matches your problem, it's a very comfortable place to
live.

## Try it

- [What is ScrivaDB?](/scriva/start/what-is-scriva/) — the two-minute tour.
- [Install](/scriva/start/install/) the server and CLI.
- [Quickstart](/scriva/start/quickstart/) — from empty directory to first query.
- [Embedding guide](/scriva/guides/embedding/) — run it in-process from Go.
