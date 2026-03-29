# FileDB v2 — Architecture

## Overview

FileDB v2 is a lightweight, append-only, file-based document database written in Go. It exposes a gRPC API (with a REST gateway) and stores data as human-readable NDJSON files on disk.

---

## Storage Model

### One directory per collection

```
data/
└── users/
    ├── seg_000001.ndjson   # sealed (immutable)
    ├── seg_000002.ndjson   # sealed
    ├── seg_000003.ndjson   # active (append target)
    ├── index.json          # persisted id → {segment, offset} map
    └── meta.json           # id counter, created_at
```

### Segment files (NDJSON)

Each line is one operation entry:

```json
{"id":1,"op":"insert","ts":"2026-03-29T10:00:00Z","data":{"userName":"admin"}}
{"id":1,"op":"update","ts":"2026-03-29T11:00:00Z","data":{"userName":"admin2"}}
{"id":2,"op":"delete","ts":"2026-03-29T12:00:00Z"}
```

- `op` is one of `insert`, `update`, `delete`
- For `delete`, `data` is omitted (tombstone entry)
- The **latest entry for each id wins**

A segment is **sealed** (made immutable) when its file size exceeds `SegmentMaxSize` (default 4 MiB). After sealing a new active segment is created.

---

## Write Path

```
client request
    │
    ▼
Collection.Insert / Update / Delete
    │
    ├── acquire write lock (sync.RWMutex)
    ├── append NDJSON entry to active segment (sequential write)
    ├── update in-memory index
    ├── release write lock
    │
    ├── if segment size ≥ limit → rotate (seal + new active)
    └── emit WatchEvent to subscribers
```

Writes are always sequential appends — the fastest possible disk operation.

---

## Read Path

```
FindById:
    acquire read lock → index lookup → seek to offset → read one line → decode

Find (scan):
    acquire read lock → iterate all segments → apply filter → stream results
```

The in-memory index makes `FindById` an O(1) index lookup + one disk seek.

---

## In-Memory Index

```
map[uint64]IndexEntry{
    SegmentPath string
    Offset      int64
}
```

- Updated on every write (same write lock scope)
- Persisted to `index.json` with a SHA-256 checksum on every close
- Loaded on startup; rebuilt from segment scans if checksum fails
- Rebuild is also triggered after compaction (offsets change)

---

## Concurrency Model

**Pessimistic locking per collection using `sync.RWMutex`:**

| Operation | Lock |
|---|---|
| Insert / Update / Delete | Write lock |
| FindById / Scan | Read lock |
| Compaction (rebuild phase) | Write lock (brief) |

Multiple concurrent reads proceed without blocking each other. The write lock is held only for the duration of the file append + in-memory index update, which is typically microseconds.

The compaction goroutine acquires the write lock only during the final atomic segment swap — reads and writes are unblocked for the entire resolve + rewrite phase.

---

## Background Compactor

Runs as a goroutine per collection. Two trigger conditions (whichever fires first):

1. **Dirty ratio**: >30% of entries in sealed segments are stale (overwritten or deleted)
2. **Time interval**: every 5 minutes (configurable)

### Compaction algorithm

```
1. Snapshot sealed segment list (read lock, release)
2. Check dirty ratio — skip if below threshold
3. Scan all sealed segments, keep latest entry per id, drop deletes
4. Write resolved entries to new temp segments (no lock held)
5. Acquire write lock
6. Atomic rename: temp → final segment files
7. Delete old dirty segments
8. Rebuild in-memory index
9. Release write lock
10. Persist updated index to disk
```

### Rebalancer

After compaction, adjacent segments smaller than 10% of `SegmentMaxSize` are merged to prevent segment count bloat from many small leftover files.

---

## Crash Safety

- **Partial write recovery**: on segment open, the last line is validated. Any partial line (from a crash mid-write) is detected and truncated before the segment is used.
- **Index recovery**: on startup, the index checksum is verified. A mismatch triggers a full rebuild by replaying all segment entries.
- **Atomic segment swap**: compaction uses `os.Rename` which is atomic on POSIX filesystems. The old segments are only deleted after the new ones are in place.

---

## Network Layer

```
┌──────────────────────────────────────┐
│  filedb binary                       │
│                                      │
│  ┌────────────┐   ┌────────────────┐ │
│  │ gRPC server│   │ REST gateway   │ │
│  │ :5433      │   │ :8080          │ │
│  │ (TCP)      │   │ (grpc-gateway) │ │
│  └─────┬──────┘   └───────┬────────┘ │
│        │                  │          │
│  ┌─────▼──────────────────▼────────┐ │
│  │ Unix socket /tmp/filedb.sock    │ │
│  │ (local connections)             │ │
│  └────────────────┬────────────────┘ │
│                   │                  │
│  ┌────────────────▼────────────────┐ │
│  │ engine.DB                       │ │
│  └─────────────────────────────────┘ │
└──────────────────────────────────────┘
```

The CLI client auto-detects the Unix socket when available (lower latency for local use), falling back to TCP.
