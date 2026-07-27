---
title: "Encrypting a database you can cat"
description: ScrivaDB's whole pitch is that your data is human-readable text on disk. So how do you add encryption at rest without throwing that away? The answer is to encrypt at exactly one boundary and let every other subsystem stay key-oblivious.
date: 2026-07-27
authors: srjn45
excerpt: The selling point is that you can `cat` your database. Encryption at rest sounds like the opposite of that. Here's how ScrivaDB reconciles the two — by encrypting values at a single boundary and keeping compaction, indexing, checksums, and backups completely key-oblivious.
tags:
  - scrivadb
  - encryption
  - security
  - design
---

ScrivaDB's whole personality is that your data is **just text**. One JSON object
per line, in a file you can `cat`, `grep`, and `tar`. That transparency is the
feature.

Now add a requirement: *sensitive fields must be unreadable on disk without a
key.* At first glance it reads like a contradiction — encryption is, definitionally,
making bytes unreadable. So the design question isn't "which cipher?" It's:
**how do you encrypt the sensitive parts without breaking the transparency,
durability, and query model that make the thing worth using?**

The answer turned out to be less about cryptography and more about *where* you
put it.

## The threat you're actually defending against

Be precise about the threat model, because it decides the whole design.
ScrivaDB is an embedded, local-storage engine — the app calls `Open(...)` and
reads and writes in the same process. There's no separate untrusted caller to
bypass. So the thing you're defending is **data at rest**:

- A stolen laptop or disk.
- A leaked backup tarball or filesystem snapshot.
- Another OS user `cat`-ing your `data/` directory.

What's explicitly *out* of scope: a compromised running process. While the DB
is open, the key and the decrypted plaintext are in memory — at-rest encryption
can't help you there, and pretending otherwise just produces false confidence.
Naming that boundary out loud is what keeps the design honest.

## One boundary, and everything else stays dumb

Here's the core move. Encrypt and decrypt happen at **exactly one place**: the
collection API boundary — `Insert`, `Update`, `Upsert`, `Get`, and the scan
path. The map of data that gets serialized to a segment file always holds
**ciphertext** for encrypted fields. The plaintext exists only above that line,
in your process.

The payoff is everything *below* that line never learns a key exists:

| Subsystem | What it sees |
|---|---|
| **Compaction** | Copies opaque ciphertext blobs when it rewrites entries. Never needs the key. |
| **Index rebuild** | Reads ciphertext from segments — safe, because encrypted fields are never indexed. |
| **CRC32C checksum** | Computed over the stored (ciphertext) form. Still catches bit-rot. |
| **Backup / snapshot** | A tar of the segments is *already ciphertext* — encrypted for free. |
| **Replication** | Ships the stored ciphertext; a follower needs no key to replicate. |

That table is the whole design. By choosing *one* choke point, encryption
becomes a local concern instead of a cross-cutting one — no subsystem-by-subsystem
retrofit, no key threaded through the compactor.

## What a value looks like on disk

Each encrypted value is a self-describing string: a reserved marker, an envelope
version, the id of the key that sealed it, and the payload.

```
<marker>:v1:<key-id>:<base64url( nonce || ciphertext+tag )>
```

The marker isn't the human-readable `enc:v1:` you might guess — it's a
distinctive, rare magic string, and **any user write that begins with it is
rejected**. That single rule turns "is this value encrypted?" into an infallible
check on read, with no side-table and nothing to drift out of sync.

The cipher is **XChaCha20-Poly1305**. Two properties earn its place:

- It's an **AEAD** — confidentiality *and* tamper-detection in one primitive. A
  modified ciphertext is *rejected*, never quietly decrypted to garbage. That
  composes perfectly with ScrivaDB's existing "detect corruption on read" stance.
- Its **192-bit random nonce** needs no counter bookkeeping. Append-only writes
  mean every insert or update is a fresh write with a fresh nonce, so reuse
  simply can't happen.

Passphrase-derived keys go through **Argon2id** (64 MiB / 3 iterations), with a
per-DB random salt stored — safely — in cleartext in `meta.json`. The salt
reveals nothing without the passphrase.

## The one real tradeoff

An encrypted field is opaque, so the engine can't filter, range, sort, or index
on it — and the design *enforces* that rather than letting you discover it as a
silent bug. Try to build an index on an encrypted field and you get a typed
error, not a `sidx_*.json` file quietly leaking the plaintext back onto disk. A
filter that references an encrypted field is rejected at query-planning time, not
left to never-match.

For the fields you'd actually encrypt — passwords, tokens, SSNs, secrets — this
costs exactly nothing, because you never query *by* them. You look them up by
some *other* key and read them back. Naming that as the one tradeoff, instead of
hand-waving it, is what makes the feature trustworthy.

## Reads don't care about your policy

The detail I find most satisfying: **reads are driven by the per-value marker,
not by the current configuration.**

> A value that begins with the marker is decrypted (the key chosen by the id it
> carries). Anything else is passed through as plaintext.

Because every value self-describes, a collection in *any* mixed state — legacy
plaintext from before you turned encryption on, new ciphertext, a half-migrated
tail — reads correctly with zero coordination. The policy governs only what new
writes encrypt; it never governs reads. That's what makes migration a non-event:
flip the policy, new writes conform immediately, and a background re-encrypting
compaction pass backfills the old records on its own schedule. Reads are correct
the entire time.

Key rotation falls out of the same mechanism for free — every blob names its own
key, so old segments keep decrypting under the old key while new writes use the
new one, and a compaction pass retires the old key when you're ready.

## The lesson

The interesting part of adding encryption to ScrivaDB wasn't the cipher — modern
AEAD is close to a solved problem. It was **placing the boundary** so that a
human-readable, append-only, self-compacting store could gain confidential
fields without any other part of it having to change. Encrypt at one line; keep
everything below it key-oblivious; let each value say what it is. You end up with
a database you can still `cat` — you just can't read the parts that matter
without the key.

---

- [What is ScrivaDB?](/scriva/start/what-is-scriva/) — the transparency-first design.
- [Durability & backup](/scriva/guides/durability-and-backup/) — why an encrypted tar is a free encrypted backup.
