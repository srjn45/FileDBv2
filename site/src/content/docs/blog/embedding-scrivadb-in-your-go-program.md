---
title: "Embedding ScrivaDB in your Go program"
description: You don't need the server. ScrivaDB's engine is a plain Go library — go get it and run the whole database in-process, with keyed CRUD, compare-and-swap, secondary indexes, and in-process Watch, and without dragging gRPC or a metrics stack into your binary.
date: 2026-07-27
authors: srjn45
excerpt: Most people meet ScrivaDB as a server on :5433. But if your program is written in Go and is the only writer, you can skip the server entirely — `go get` the engine, call Open, and get a real database in-process. No daemon, no network, no gRPC in your binary.
tags:
  - scrivadb
  - go
  - embedded
  - tutorial
---

Most databases make you run a server. You install a daemon, open a port, manage
credentials, pool connections — and then talk to it over a socket, even when
"it" is running on the same laptop as your program.

If your program is written in Go and is the only thing writing the data, that's
a lot of ceremony for no benefit. ScrivaDB's storage engine is a plain Go
library, so you can skip all of it and run the database **in-process** — no
gRPC, no network, no separate daemon. Same durability, same query model, living
directly inside your binary.

## Two lines to a database

```bash
go get github.com/srjn45/scriva
```

```go
import "github.com/srjn45/scriva"

db, _ := scriva.Open("./data")   // embedded durability defaults (fsync ~1s)
defer db.Close()
```

`Open` points at a directory. That directory *is* the database — the NDJSON
segment files, the indexes, the metadata. There's nothing else to stand up.

## Keyed CRUD with your own keys

You bring the keys. ScrivaDB stores records under caller-supplied string keys, so
you're not forced to round-trip a generated id back into your own model:

```go
sessions := db.MustCollection("sessions")

id, _, _ := sessions.InsertWithKey("sess-1", map[string]any{"status": "open"})
rec, _   := sessions.GetByKey("sess-1")
```

Collections are created on demand — `MustCollection` hands you one, materializing
it if it's new. No migration step, no `CREATE TABLE`.

## Compare-and-swap, not locks

Concurrency is handled optimistically. Every record carries a revision, and you
can make an update conditional on the revision you last read — a compare-and-swap.
If someone else wrote in between, your update is rejected and you retry, instead
of two writers silently clobbering each other:

```go
rec, _ := sessions.GetByKey("sess-1")

// only succeeds if sess-1 is still at rec.Rev
_, err := sessions.UpdateIfRev("sess-1", rec.Rev, map[string]any{"status": "closed"})
if err != nil {
    // someone updated it first — re-read and decide
}
```

This is the right primitive for the embedded case: no lock to hold across a
network, no deadlock to untangle — just "write only if the world hasn't moved
under me."

## The rest of the surface

Embedding isn't a stripped-down mode. You get the real engine:

- **Secondary indexes** for fast equality and range queries — the same query
  engine the server exposes.
- **Upsert**, **count**, and **exists** for the common one-liners.
- In-process **`Watch`** subscriptions, so your program can react to changes
  without polling (with a documented overflow contract so a slow consumer can't
  wedge a writer).
- A **`LoadJSONL`** bulk-import path for seeding.

## The part that matters for your binary

Here's the detail that makes embedding actually pleasant: the `engine` package
pulls in **no gRPC, no protobuf, no Prometheus, no cobra, no OpenTelemetry**.
A CI gate enforces it on every commit. Embedding ScrivaDB won't quietly drag a
server framework and a metrics stack into your dependency graph — you get a
storage engine, and that's it.

That's the difference between "a database that also has a library mode" and a
library that happens to be a database. The dependency discipline is a feature,
and it's tested like one.

## When embedded is the right call

Reach for the embedded engine when:

- Your program is written in Go and is the **sole writer** of the data.
- You want durability and a real query model without operating a separate
  process.
- You're building a CLI, a desktop app, an edge/IoT daemon, or a service that
  wants local state without a database container beside it.

And when you *don't* fit that shape — multiple processes need to write, or you
want to talk to the data from another language — the very same repo builds a
[standalone server](/scriva/start/install/) with gRPC + REST and
[client SDKs in ten languages](/scriva/guides/clients/). The storage engine is
identical; only the front door changes.

warden — a supervisor for fleets of AI coding agents — embeds ScrivaDB exactly
this way: its sessions, pipelines, and agent state live in an in-process store,
with no database to run alongside the daemon. That's the embedded case in one
sentence.

---

- [Embedding guide](/scriva/guides/embedding/) — the full API, durability modes, and the Watch overflow contract.
- [Data model](/scriva/guides/data-model/) — keys, revisions, and CAS in depth.
