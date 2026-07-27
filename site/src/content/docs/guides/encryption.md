---
title: Encryption at rest
description: Transparent field- and record-level encryption for the embedded engine — passphrase or raw-key, wrong-key detection, key rotation, and lazy or bulk migration.
---

ScrivaDB can encrypt sensitive data **at rest** while keeping everything that
makes it pleasant — human-readable segments, CRC integrity, cheap backups — for
the fields you *don't* encrypt. Encryption happens at a single boundary (the
collection API), so segments, compaction, indexes, replication, and backups all
stay **key-oblivious**: they only ever see ciphertext.

It is a feature of the **embedded engine** (root package `scriva`), configured
when you `Open` the database in-process. The standalone server does not expose
it.

:::note[Threat model]
At-rest encryption protects a **stolen disk, a leaked backup, or another OS user
reading your `data/` directory** — not a compromised running process. While the
DB is open, the key and decrypted plaintext live in memory. Back up the key
**separately**: a backup without it is unrecoverable (that's the point).
:::

## Quick start

Pick one key source and declare a policy per collection:

```go
import (
	"os"
	"github.com/srjn45/scriva"
)

db, err := scriva.Open("./data",
	// Key source — one of WithPassphrase / WithEncryptionKey / WithKeyProvider.
	scriva.WithPassphrase(os.Getenv("SCRIVA_PASSPHRASE")),

	// Field-level: name the fields to seal; everything else stays queryable.
	scriva.WithCollectionEncryption("users", scriva.EncryptFields("password", "ssn")),

	// Record-level: seal the whole record, keeping only the named index fields
	// (and the reserved _key) plaintext and queryable.
	scriva.WithCollectionEncryption("audit", scriva.EncryptRecord("id", "tenant")),
)
if err != nil {
	// A wrong passphrase fails fast here with crypto.ErrWrongEncryptionKey.
	log.Fatal(err)
}

users := db.MustCollection("users")
id, _, _ := users.Insert(map[string]any{"email": "a@b.com", "password": "hunter2"})

rec, _ := users.Get(id)         // rec.Data["password"] == "hunter2"
// on disk, the password field is opaque ciphertext; email is plaintext
```

Nothing else in your code changes: `Insert`/`Update`/`Get`/`Scan` behave
exactly as before, returning plaintext.

## Key sources

| Option | Use it for |
|---|---|
| `WithPassphrase(pw)` | A human-supplied secret. Derived via **Argon2id** (64 MiB / 3 / 1); a random salt is minted once and stored (non-secret) in `meta.json`, then re-derived on every reopen. |
| `WithEncryptionKey(key)` | A raw 32-byte key you already manage (e.g. fetched from a KMS). A wrong length fails at `Open`. |
| `WithKeyProvider(p)` | A custom `crypto.KeyProvider` — an OS keychain, Vault, a KMS, or a `crypto.Keyring` you rotate yourself. |

One key protects the whole database. Reopening with the same passphrase (or key)
just works; a wrong one is caught by a **key-check** in `meta.json` and fails
`Open` immediately with `crypto.ErrWrongEncryptionKey`, rather than returning
garbage on the first read.

## Two modes

**Field-level** (`EncryptFields`) seals a deny-list of top-level fields in place,
leaving every other field plaintext and queryable:

```go
scriva.WithCollectionEncryption("users", scriva.EncryptFields("password", "ssn"))
```

**Record-level** (`EncryptRecord`) seals the whole record into a single opaque
blob, keeping only an allow-list of index fields (and the reserved `_key`)
plaintext:

```go
scriva.WithCollectionEncryption("audit", scriva.EncryptRecord("tenant", "action_time"))
// tenant + action_time stay filterable/sortable; everything else is sealed
```

The reconstruction mode is fixed when encryption is first enabled and never
changes for the life of the collection, so older records always decode. Switch
between field and record mode by disabling, running a compaction pass, then
re-enabling under the new mode.

## The one tradeoff

An encrypted field is opaque on disk, so the engine **cannot index, filter, sort,
or aggregate on it** — and it enforces that rather than letting you discover it
as a silent bug. Trying to index or filter an encrypted field returns a typed
`crypto.ErrFieldEncrypted`, never a `sidx_*.json` file leaking the plaintext
back onto disk. For the fields you'd actually encrypt — passwords, tokens, SSNs
— this costs nothing, because you look them up by *some other* key and read them
back.

## Runtime administration

The handle returned by `db.Collection(...)` is an `*engine.Collection`, which
exposes the admin API for changing policy on a live collection:

```go
users, _ := db.Collection("users")
ctx := context.Background()

// Enable, adjust the field list, or disable (nil) at runtime.
policy := scriva.EncryptFields("password", "ssn", "phone").Policy()
_ = users.SetEncryptionPolicy(ctx, &policy)

// Rotate to a new current key (add + promote it on your keyring first).
_ = users.RotateKey(ctx)

// Bulk-migrate existing data to the current policy/key (security completion).
_ = users.MigrateNow(ctx)

// Inspect migration progress — cheap, from the in-memory index.
st := users.EncryptionStatus()
// st.Enabled, st.CurrentKeyID, st.Epoch, st.LiveRecords, st.LiveAtEpoch,
// st.FunctionalComplete
```

### How migration works

Every policy change (enable/disable, field add/remove, key rotation) bumps a
policy **epoch**. Existing records migrate two ways:

- **Lazily** — an ordinary `Update` rewrites a record under the current policy.
- **In bulk** — a **re-encrypting compaction pass** rewrites surviving records.
  `MigrateNow` seals the active segment and forces that pass; it is **fail-closed**
  (a retired key that can't decrypt aborts the pass before anything is mutated).

Reads are **policy-independent** the entire time: each value self-describes (via
a reserved marker and the id of the key that sealed it), so a half-migrated
collection — legacy plaintext, new ciphertext, and everything in between — reads
correctly with zero coordination.

## Key rotation

Because every blob names its own key, rotation is incremental and non-blocking:

```go
kr, _ := crypto.NewKeyring("k1", k1)          // at Open: scriva.WithKeyProvider(kr)
// ... later ...
_ = kr.Add("k2", k2)                          // register the new key
_ = kr.SetCurrent("k2")                        // new writes seal under k2
_ = users.RotateKey(ctx)                       // advance the collection
_ = users.MigrateNow(ctx)                      // re-encrypt old blobs under k2
// now k1 can be retired
```

Old segments keep decrypting under `k1` until the compaction pass re-encrypts
them, so reads never break during a rotation.

## Backups are encrypted for free

Because segment files already hold ciphertext, `scriva-cli backup` (or a plain
`tar` of the data directory) produces an **already-encrypted** archive — a
leaked backup is useless without the key. `meta.json` travels with it (policy,
KDF salt, key-check — all non-secret), so a restored collection knows how to
derive and verify the key. **Store the key separately from the backup.** See
[Durability & backup](/scriva/guides/durability-and-backup/).

## Next

- [Embedding (Go library)](/scriva/guides/embedding/) — the full in-process API.
- Read the design essay:
  [Encrypting a database you can `cat`](/scriva/blog/encrypting-a-database-you-can-cat/).
- The complete design doc lives in the repo at
  [`docs/encryption-at-rest.md`](https://github.com/srjn45/scriva/blob/main/docs/encryption-at-rest.md).
