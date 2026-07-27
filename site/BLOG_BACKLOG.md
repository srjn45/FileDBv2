# ScrivaDB blog backlog

Planned blog posts, not yet written. Each publishes to `site/src/content/docs/blog/`
using the `starlight-blog` plugin (frontmatter: `title`, `description`, `date`,
`authors: srjn45`, `excerpt`, `tags`). Adding a post automatically updates the
RSS feed at `/scriva/blog/rss.xml`, which the `srjn45.github.io` master site
aggregates into its `/blog` stream.

Keep every technical claim grounded in the repo — link the source column before
writing. Order below is a rough priority.

## Published so far

- ✅ **When a file is your database** — positioning / intro (problem, fit, non-fit).
- ✅ **Encrypting a database you can `cat`** — encryption-at-rest design tension.
- ✅ **How append-only survives a crash** — crash safety deep-dive.
- ✅ **Embedding ScrivaDB in your Go program** — in-process `go get` walkthrough.

## Backlog

### Engineering deep-dives

| Working title | Angle / hook | Grounding source |
|---|---|---|
| **The compactor that never blocks a write** | How a background goroutine merges and deduplicates sealed segments — dirty-ratio + timer triggers, small-segment rebalancing — without stopping writers. Pairs with the crash-safety post (where superseded records come from). | `engine/compactor.go`; ROADMAP "Phase 3"; `docs/architecture.md` (compaction) |
| **Secondary indexes over a text file** | O(1) equality and range lookups, keyset pagination, and server-side aggregations on top of plain NDJSON — an inverted `field-value → id set` index that's checksummed and rebuildable. | `engine/secondary_index.go`, `engine/index.go`, `query/filter.go`; guide `guides/queries.md` |
| **Optimistic concurrency without locks** | Per-record revisions, compare-and-swap (`UpdateIfRev`), upsert, and optimistic transactions — why CAS beats locks for the embedded/single-writer case. | `engine/collection.go`; guide `guides/data-model.md`; scriva.go façade |

### Use-case / how-to

| Working title | Angle / hook | Grounding source |
|---|---|---|
| **Backup is a `gzip` stream, restore is `tar xzf`** | Durability policy (`--sync none\|always\|interval`), `DB.SnapshotTo`, and point-in-time recovery with tools you already have. Note: with encryption on, the tar is already ciphertext. | guide `guides/durability-and-backup.md`; ROADMAP "Durability" |
| **Reacting to change with in-process Watch** | Building reactive local apps on `Watch` subscriptions; the overflow contract that stops a slow consumer wedging a writer. | `engine/collection.go` (Watch); guide `guides/embedding.md` |
| **TTL for ephemeral data** | Per-record TTL for caches, sessions, expiring tokens — how expiry rides the same append-only + compaction machinery. | `engine/ttl.go`; ROADMAP (per-record TTL over the wire) |

### Positioning / comparisons

| Working title | Angle / hook | Grounding source |
|---|---|---|
| **ScrivaDB vs SQLite** | Two embedded stores, different trade-offs: readable-on-disk NDJSON + document model vs a single opaque binary file. When each wins. | README positioning; `guides/data-model.md` |
| **ScrivaDB vs BoltDB / Badger** | Document model + human-readable segments vs opaque B-tree/LSM KV pages; the debuggability argument. | README; `docs/architecture.md` |
| **"warden runs on ScrivaDB"** | Dogfooding case study — why a fleet supervisor embeds ScrivaDB instead of running Postgres beside its daemon. Cross-link warden's blog. | warden repo (`../warden`), its blog post `scrivadb-rotation-zero-records.md` |

### Feature / forward-looking

| Working title | Angle / hook | Grounding source |
|---|---|---|
| **Replication & why manual failover is a feature** | Leader→follower replication, ciphertext-on-the-wire, keyless cold-standby followers, and the deliberate choice of manual (not automatic) failover for small deployments. | `engine/replication.go`; guide `guides/replication.md`; encryption doc §13 #6 |
| **Rotating an encryption key with zero downtime** | Follow-up to the encryption post: every blob names its key, so rotation is an incremental re-encrypting compaction pass; functional vs security completion. | `docs/encryption-at-rest.md` §7, §12; `crypto/keyring.go` |

## Notes for whoever picks this up

- Add posts on the `feat/site-blog` branch (PR #102) if still open, else a fresh
  branch off `main`. Do git work in an **isolated worktree** — the main repo path
  is shared with the encryption agents.
- Validate with `npm run build` in `site/` (symlink the existing `node_modules`
  if the worktree lacks deps) — it fails on bad frontmatter and confirms
  `dist/blog/rss.xml` regenerates.
- Commit email must be `29410402+srjn45@users.noreply.github.com` or the push is
  rejected.
