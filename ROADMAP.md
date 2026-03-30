# FileDBv2 — Project Roadmap & Status

## What This Is

A ground-up rebuild of [FileDB PHP](https://github.com/srjn45/FileDB-php) (a college-era flat-file JSON database) into a production-quality Go service. The goal is a **lightweight, embeddable, local-first database** that:

- Ships as a single binary with zero runtime dependencies
- Stores data as human-readable NDJSON files on disk
- Exposes a gRPC API (with REST gateway) over TCP and Unix socket
- Has a full CLI client with interactive REPL, one-shot commands, and batch scripting
- Auto-generates language-specific client SDKs from a `.proto` file

---

## Design Decisions (Why We Made These Choices)

| Decision | Choice | Reason |
|---|---|---|
| Language | Go | Single static binary, goroutines for concurrency, fast compile, easy cross-compile |
| Storage format | Append-only NDJSON | Human readable, crash-safe (no in-place writes), sequential append = fastest disk op |
| Segments | Multiple files per collection, capped by size (default 4MB) | Bounds RAM usage, enables background compaction without blocking writes |
| Concurrency model | Pessimistic locking (`sync.RWMutex` per collection) | Write lock held for microseconds (just append + index update), so reader starvation never happens in practice |
| API protocol | gRPC primary + REST via grpc-gateway | gRPC gives persistent dual-channel connections (HTTP/2 multiplexed), bidirectional streaming, auto-generated SDKs; REST for curl/browser |
| Local transport | Unix domain socket | Bypasses TCP stack entirely for CLI on same machine |
| Compaction trigger | Dirty ratio (>30%) OR time interval (5min) | Dirty ratio prevents wasted space; timer catches slow-write collections |
| Auth | API key via gRPC metadata (`x-api-key`) | Simple, stateless, good enough for local/trusted-network use |

---

## Architecture in One Page

```
data/
└── users/                         ← one dir per collection
    ├── seg_000001.ndjson           ← sealed (immutable, old entries)
    ├── seg_000002.ndjson           ← sealed
    ├── seg_000003.ndjson           ← active (current append target)
    ├── index.json                  ← id → {segment, byte_offset} + SHA-256 checksum
    └── meta.json                   ← id counter, created_at

Each line in a segment:
{"id":1,"op":"insert","ts":"2026-03-29T10:00:00Z","data":{"name":"alice"}}
{"id":1,"op":"update","ts":"2026-03-29T11:00:00Z","data":{"name":"alice2"}}
{"id":2,"op":"delete","ts":"2026-03-29T12:00:00Z"}

Write path:  append line → update in-memory index → (rotate if size ≥ limit)
Read path:   index lookup → seek to offset → read one line
Compaction:  resolve latest per id → write clean segments → atomic swap → rebuild index
```

---

## What Is Done ✅

### Phase 1 — Project Scaffold
- [x] Directory structure: `internal/`, `server/`, `cmd/`, `clients/`, `docs/`, `.github/`
- [x] Go module: `github.com/srjn45/filedbv2` (Go 1.22+)
- [x] `Makefile` with targets: `build`, `test`, `proto`, `lint`, `run`, `cli`, `release`, `clean`
- [x] `buf.yaml` + `buf.gen.yaml` for proto code generation via [buf](https://buf.build)

### Phase 2 — Proto API Contract
- [x] `proto/filedb.proto` — defines all 15+ RPCs
- [x] Generated: `internal/pb/proto/filedb.pb.go`, `filedb_grpc.pb.go`, `filedb.pb.gw.go`
- [x] Full REST annotations via `google/api/annotations.proto`

**RPCs implemented:**
```
CreateCollection  DropCollection  ListCollections
Insert  InsertMany  FindById  Find (streaming)  Update  Delete
Watch (server-streaming change feed)
CollectionStats
EnsureIndex  DropIndex  ListIndexes
BeginTx  CommitTx  RollbackTx
```

### Phase 3 — Storage Engine
- [x] `internal/store/ndjson.go` — Entry struct, Encode/Decode, NewInsert/NewUpdate/NewDelete
- [x] `internal/engine/segment.go` — Append, ReadAt, ScanAll, Seal, crash recovery (partial line truncation)
- [x] `internal/engine/index.go` — In-memory `map[uint64]IndexEntry`, SHA-256 checksum persist/load, Rebuild from segments
- [x] `internal/engine/collection.go` — RWMutex, Insert/Update/Delete/FindByID/Scan, segment rotation, Watch subscribers
- [x] `internal/engine/secondary_index.go` — Field-value → ID set inverted index, EnsureIndex/DropIndex/ListIndexes/IndexLookup, persist/load/rebuild
- [x] `internal/engine/compactor.go` — Background goroutine, dirty-ratio trigger, timer trigger, rebalancer (merge small segments)
- [x] `internal/engine/db.go` — Collection registry, Open/CreateCollection/DropCollection/ListCollections/Close
- [x] `internal/query/filter.go` — FieldFilter, AndFilter, OrFilter, ops: eq/neq/gt/gte/lt/lte/contains/regex

### Phase 4 — Server
- [x] `internal/auth/apikey.go` — gRPC unary + stream interceptors, `crypto/subtle.ConstantTimeCompare`
- [x] `server/config.go` — Config struct with defaults, `EngineConfig()` converter
- [x] `server/grpc.go` — Full `FileDBServer` implementation, proto↔engine mapping, filter conversion
- [x] `server/rest.go` — grpc-gateway bridge (TCP + Unix socket variants)
- [x] `cmd/filedb/main.go` — `cobra` CLI, `serve` subcommand, TCP + Unix socket + REST listeners, graceful shutdown

### Phase 5 — CLI Client
- [x] `cmd/filedb-cli/main.go` — Connection management (Unix socket auto-detect → TCP fallback), auth context
- [x] `cmd/filedb-cli/commands.go` — All commands: collections, create-collection, drop-collection, insert, find, get, update, delete, stats, export, import
- [x] `cmd/filedb-cli/repl.go` — Interactive REPL with readline history, tab-completion scaffold, `use <collection>` context
- [x] `cmd/filedb-cli/batch.go` — `.fql` script runner + stdin pipe support

### Phase 6 — Build Pipeline
- [x] `.github/workflows/ci.yml` — Lint + race tests + build on every push/PR
- [x] `.github/workflows/release.yml` — GoReleaser on `v*` tag push, publishes to GitHub Releases + GHCR
- [x] `.goreleaser.yml` — Cross-compile: linux/darwin/windows × amd64/arm64, Docker image to `ghcr.io/srjn45/filedbv2`
- [x] `Dockerfile` — Multi-stage, Alpine, non-root user

### Phase 7 — Documentation
- [x] `README.md` — Quick start, positioning, links
- [x] `docs/getting-started.md` — Install, server setup, CLI usage, REST examples, filter syntax
- [x] `docs/architecture.md` — Storage model, write/read paths, compaction, crash safety, network layer

### Tests
- [x] `internal/store/ndjson_test.go` — encode/decode parity, delete entry
- [x] `internal/engine/segment_test.go` — append + readAt, scanAll, crash recovery, seal
- [x] `internal/engine/collection_test.go` — insert/findById, update, delete, scan, persist across reopen, concurrent writes (race detector), watcher
- [x] `internal/engine/index_test.go` — Set/Get/Delete, Len, Persist+Load, checksum mismatch, Rebuild from segments
- [x] `internal/engine/compactor_test.go` — isDirty threshold, compact reduces segments, records readable after compact, rebalancer merges tiny segments
- [x] `internal/engine/secondary_index_test.go` — EnsureIndex/DropIndex/ListIndexes, insert/update/delete maintenance, Scan uses index, Scan falls back, Persist+Load, rebuild from existing data, survives compaction
- [x] `internal/query/filter_test.go` — all 8 ops, And/Or/nested, MatchAll, missing field, invalid regex
- [x] `server/grpc_integration_test.go` — in-process gRPC server, CRUD, Find with filter/order/limit, transactions, error paths

**All 50+ tests pass with `go test ./... -race`**

---

## What Is NOT Done ❌

### High Priority

#### 1. Language clients
The proto file is ready. All three clients just need `protoc`/`buf` generation + thin wrappers.

| Client | Package manager | Status |
|---|---|---|
| `clients/python/` | PyPI: `pip install filedbv2` | Not started |
| `clients/php/` | Packagist: `composer require srjn45/filedbv2` | Not started |
| `clients/js/` | npm: `npm install filedbv2` | Not started |

Each client needs:
1. Run `buf generate` with the target language plugin
2. Write a `FileDB` class wrapper with ergonomic method names
3. Handle connection setup (host, API key, Unix socket for Python/Node local use)
4. Write a README + publish to the package registry

### Medium Priority

#### 2. `golangci-lint` — stricter rules
`.golangci.yml` exists with `bodyclose`, `errorlint`, `copyloopvar`. Consider adding `staticcheck`, `govet`, `unused` once pb-generated code exclusions are tuned.

### Low Priority / Future

#### ~~3. Secondary indexes~~ ✅ Done
`internal/engine/secondary_index.go` — in-memory inverted index (field-value → ID set).
- `EnsureIndex(field)` / `DropIndex(field)` / `ListIndexes()` on `Collection`
- `Scan` uses the index for single eq-filters (O(1)), falls back to full scan otherwise
- Index maintained on Insert/Update/Delete and rebuilt after compaction
- Persisted to `sidx_<field>.json` with SHA-256 checksum, reloaded on startup
- gRPC: `EnsureIndex` / `DropIndex` / `ListIndexes` RPCs + REST via grpc-gateway
- CLI: `ensure-index`, `drop-index`, `indexes` commands

#### 4. TLS support
Currently gRPC uses `insecure.NewCredentials()`. Add optional TLS via:
- `--tls-cert` and `--tls-key` flags on the server
- `--tls-ca` flag on the CLI for client verification

#### 5. Config file (`filedb.yaml`)
Currently all config comes from CLI flags or env vars. Add YAML config file support via `gopkg.in/yaml.v3` (already in `go.mod`).

#### 6. Metrics / observability
Add Prometheus metrics endpoint (`/metrics`) for:
- Records per collection
- Segment count
- Compaction frequency and duration
- Request latency histograms

---

## Key Files Reference

| File | Purpose |
|---|---|
| [proto/filedb.proto](proto/filedb.proto) | Single source of truth for all APIs — edit here first |
| [internal/engine/collection.go](internal/engine/collection.go) | Core read/write logic, RWMutex, Watch |
| [internal/engine/compactor.go](internal/engine/compactor.go) | Background compaction goroutine |
| [internal/engine/index.go](internal/engine/index.go) | In-memory index, checksum, rebuild |
| [internal/engine/segment.go](internal/engine/segment.go) | NDJSON file I/O, crash recovery |
| [server/grpc.go](server/grpc.go) | gRPC handlers — proto → engine mapping |
| [cmd/filedb/main.go](cmd/filedb/main.go) | Server binary, startup, graceful shutdown |
| [cmd/filedb-cli/repl.go](cmd/filedb-cli/repl.go) | Interactive REPL |
| [cmd/filedb-cli/commands.go](cmd/filedb-cli/commands.go) | All CLI subcommands |
| [Makefile](Makefile) | All dev tasks |

---

## How to Pick This Up

```bash
cd FileDBv2

# Build
make build

# Run tests
make test

# Start server
make run          # serves on :5433 (gRPC), :8080 (REST), /tmp/filedb.sock

# Use CLI
make cli          # connects to local socket automatically
```

Next logical steps in order:
1. Language clients — unlocks the "use from any language" goal (Python, PHP, JS)
2. Secondary indexes — makes filtered queries O(log n) instead of O(n)
3. TLS support — hardens network transport
4. Metrics endpoint — adds Prometheus observability
