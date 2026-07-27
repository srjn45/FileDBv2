//nolint:errcheck
package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/srjn45/scriva/crypto"
	"github.com/srjn45/scriva/query"
	"github.com/srjn45/scriva/store"
)

// segPathN returns the path of a collection's n-th segment file (1-based).
func segPathN(dir, collection string, n int) string {
	return filepath.Join(dir, collection, fmt.Sprintf("seg_%06d.ndjson", n))
}

// readIfExists reads path, returning (nil, nil) when it does not exist.
func readIfExists(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

// eqStr builds an equality filter on a string field (Value is a JSON literal).
func eqStr(field, val string) query.Filter {
	return &query.FieldFilter{Field: field, Op: query.OpEq, Value: fmt.Sprintf("%q", val)}
}

// encCfgKR builds a collection config wired to a caller-owned keyring, so a test
// can rotate the key (Add / SetCurrent) after the collection is open. policy may be
// nil to open a plaintext collection that can be encrypted later at runtime.
func encCfgKR(kr crypto.KeyProvider, policy *EncryptionPolicy) CollectionConfig {
	cfg := testCfg()
	cfg.Encryption = policy
	cfg.KeyProvider = kr
	return cfg
}

// allSegEntries decodes every entry across all of a collection's segment files, in
// segment then line order, so a test can assert on the physical on-disk form (which
// epoch each stored entry carries, whether its data is opaque, etc.).
func allSegEntries(t *testing.T, dir, collection string) []store.Entry {
	t.Helper()
	var out []store.Entry
	for i := 1; ; i++ {
		path := segPathN(dir, collection, i)
		b, err := readIfExists(path)
		if err != nil {
			t.Fatalf("read segment %s: %v", path, err)
		}
		if b == nil {
			break
		}
		for _, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
			if line == "" {
				continue
			}
			e, err := store.Decode([]byte(line))
			if err != nil {
				t.Fatalf("decode %s: %v", path, err)
			}
			out = append(out, e)
		}
	}
	return out
}

// liveEpochsOnDisk returns the epoch stamped on every live (insert/update) entry
// across the collection's segments — a witness of physical migration progress.
func liveEpochsOnDisk(t *testing.T, dir, collection string) []uint64 {
	t.Helper()
	var epochs []uint64
	for _, e := range allSegEntries(t, dir, collection) {
		if e.Op == store.OpDelete {
			continue
		}
		epochs = append(epochs, e.Epoch)
	}
	return epochs
}

// TestEpochStampedOnWrite verifies that enabling encryption starts at epoch 1 and
// that writes stamp the current epoch onto both the segment entry and the index.
func TestEpochStampedOnWrite(t *testing.T) {
	dir := t.TempDir()
	col, err := OpenCollection("users", dir, encCfg(t, mustKey(t), fieldsPolicy("pw")))
	if err != nil {
		t.Fatal(err)
	}
	defer col.Close()

	if _, _, err := col.Insert(map[string]any{"email": "a@b.com", "pw": "x"}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	st := col.EncryptionStatus()
	if !st.Configured || !st.Enabled || st.Mode != EncryptModeFields {
		t.Fatalf("status = %+v, want configured+enabled fields mode", st)
	}
	if st.Epoch != 1 || st.LiveRecords != 1 || st.LiveAtEpoch != 1 || !st.FunctionalComplete {
		t.Fatalf("status = %+v, want epoch 1, 1/1 at epoch, functional complete", st)
	}
	for _, ep := range liveEpochsOnDisk(t, dir, "users") {
		if ep != 1 {
			t.Fatalf("on-disk epoch = %d, want 1", ep)
		}
	}
}

// TestEnableEncryptionAtRuntime opens a plaintext collection that carries a key
// provider, writes plaintext, then enables encryption. Old rows read through as
// plaintext; new rows are encrypted; a MigrateNow brings the old rows up to date.
func TestEnableEncryptionAtRuntime(t *testing.T) {
	dir := t.TempDir()
	kr, _ := crypto.NewKeyring("k1", mustKey(t))
	col, err := OpenCollection("users", dir, encCfgKR(kr, nil))
	if err != nil {
		t.Fatal(err)
	}
	defer col.Close()

	legacyID, _, err := col.Insert(map[string]any{"email": "old@b.com", "pw": "plain"})
	if err != nil {
		t.Fatalf("Insert legacy: %v", err)
	}

	if err := col.SetEncryptionPolicy(context.Background(), fieldsPolicy("pw")); err != nil {
		t.Fatalf("SetEncryptionPolicy: %v", err)
	}

	newID, _, err := col.Insert(map[string]any{"email": "new@b.com", "pw": "secret"})
	if err != nil {
		t.Fatalf("Insert new: %v", err)
	}

	// Both records read correctly: the legacy row is plaintext, the new row decrypts.
	for id, want := range map[uint64]string{legacyID: "plain", newID: "secret"} {
		got, _, err := col.FindByID(id)
		if err != nil {
			t.Fatalf("FindByID %d: %v", id, err)
		}
		if got["pw"] != want {
			t.Errorf("id %d pw = %v, want %v", id, got["pw"], want)
		}
	}

	// The new row is opaque on disk; the legacy row's pw is still plaintext.
	st := col.EncryptionStatus()
	if st.FunctionalComplete {
		t.Fatalf("expected migration incomplete while legacy row is at epoch 0: %+v", st)
	}

	if err := col.MigrateNow(context.Background()); err != nil {
		t.Fatalf("MigrateNow: %v", err)
	}
	st = col.EncryptionStatus()
	if !st.FunctionalComplete || st.LiveAtEpoch != st.LiveRecords {
		t.Fatalf("after MigrateNow status = %+v, want functional complete", st)
	}
	for _, e := range allSegEntries(t, dir, "users") {
		if e.Op == store.OpDelete {
			continue
		}
		if s, ok := e.Data["pw"].(string); ok && !crypto.IsEncrypted(s) {
			t.Fatalf("id %d pw still plaintext on disk after migration: %q", e.ID, s)
		}
	}
}

// TestKeyRotationReEncrypts rotates to a new key, migrates, and proves the old key
// is no longer needed: a reopen with a keyring holding only the new key reads every
// record — which is only possible once the data has been re-encrypted under it.
func TestKeyRotationReEncrypts(t *testing.T) {
	dir := t.TempDir()
	k1, k2 := mustKey(t), mustKey(t)
	kr, _ := crypto.NewKeyring("k1", k1)
	col, err := OpenCollection("users", dir, encCfgKR(kr, fieldsPolicy("pw")))
	if err != nil {
		t.Fatal(err)
	}

	id, _, err := col.Insert(map[string]any{"email": "a@b.com", "pw": "secret"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// Promote a new current key on the shared keyring, then rotate.
	if err := kr.Add("k2", k2); err != nil {
		t.Fatalf("Add k2: %v", err)
	}
	if err := kr.SetCurrent("k2"); err != nil {
		t.Fatalf("SetCurrent k2: %v", err)
	}
	if err := col.RotateKey(context.Background()); err != nil {
		t.Fatalf("RotateKey: %v", err)
	}
	if st := col.EncryptionStatus(); st.Epoch != 2 || st.CurrentKeyID != "k2" || st.FunctionalComplete {
		t.Fatalf("after rotate status = %+v, want epoch 2, key k2, not yet complete", st)
	}

	if err := col.MigrateNow(context.Background()); err != nil {
		t.Fatalf("MigrateNow: %v", err)
	}
	col.Close()

	// Reopen with ONLY k2: the key-check (now for k2) passes, and every record must
	// decrypt — proof the old-key blob was re-encrypted under k2.
	kr2, _ := crypto.NewKeyring("k2", k2)
	col2, err := OpenCollection("users", dir, encCfgKR(kr2, nil))
	if err != nil {
		t.Fatalf("reopen with k2-only: %v", err)
	}
	defer col2.Close()
	got, _, err := col2.FindByID(id)
	if err != nil {
		t.Fatalf("FindByID after rotate+migrate with k2-only: %v", err)
	}
	if got["pw"] != "secret" {
		t.Fatalf("pw = %v, want secret", got["pw"])
	}
}

// TestKeyRotationOldKeyStillNeededBeforeMigration is the negative of the above:
// before the migration pass, reopening without the old key must fail to read the
// old-key blob (fail-closed), confirming rotation alone does not re-key data.
func TestKeyRotationOldKeyStillNeededBeforeMigration(t *testing.T) {
	dir := t.TempDir()
	k1, k2 := mustKey(t), mustKey(t)
	kr, _ := crypto.NewKeyring("k1", k1)
	col, err := OpenCollection("users", dir, encCfgKR(kr, fieldsPolicy("pw")))
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := col.Insert(map[string]any{"pw": "secret"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	kr.Add("k2", k2)
	kr.SetCurrent("k2")
	if err := col.RotateKey(context.Background()); err != nil {
		t.Fatalf("RotateKey: %v", err)
	}
	col.Close() // no migration

	kr2, _ := crypto.NewKeyring("k2", k2)
	col2, err := OpenCollection("users", dir, encCfgKR(kr2, nil))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer col2.Close()
	if _, _, err := col2.FindByID(id); !errors.Is(err, crypto.ErrKeyUnavailable) {
		t.Fatalf("read old-key row without k1: got %v, want ErrKeyUnavailable", err)
	}
}

// TestDisableEncryptionMigratesToPlaintext disables encryption and confirms new
// writes are plaintext, old rows still read, and a migration rewrites the old
// ciphertext back to plaintext on disk.
func TestDisableEncryptionMigratesToPlaintext(t *testing.T) {
	dir := t.TempDir()
	key := mustKey(t)
	kr, _ := crypto.NewKeyring("k1", key)
	col, err := OpenCollection("audit", dir, encCfgKR(kr, fieldsPolicy("note")))
	if err != nil {
		t.Fatal(err)
	}

	encID, _, err := col.Insert(map[string]any{"note": "sensitive"})
	if err != nil {
		t.Fatalf("Insert encrypted: %v", err)
	}

	if err := col.SetEncryptionPolicy(context.Background(), nil); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if st := col.EncryptionStatus(); st.Enabled {
		t.Fatalf("expected disabled after nil policy: %+v", st)
	}

	// New write is plaintext on disk.
	plainID, _, err := col.Insert(map[string]any{"note": "public"})
	if err != nil {
		t.Fatalf("Insert plain: %v", err)
	}

	if err := col.MigrateNow(context.Background()); err != nil {
		t.Fatalf("MigrateNow: %v", err)
	}
	// Nothing on disk is encrypted any more.
	for _, e := range allSegEntries(t, dir, "audit") {
		if e.Op == store.OpDelete {
			continue
		}
		if s, ok := e.Data["note"].(string); ok && crypto.IsEncrypted(s) {
			t.Fatalf("id %d note still encrypted after disable+migrate: %q", e.ID, s)
		}
	}
	// Both records still read with the right values.
	for id, want := range map[uint64]string{encID: "sensitive", plainID: "public"} {
		got, _, err := col.FindByID(id)
		if err != nil {
			t.Fatalf("FindByID %d: %v", id, err)
		}
		if got["note"] != want {
			t.Errorf("id %d note = %v, want %v", id, got["note"], want)
		}
	}
	col.Close()
}

// TestAddFieldMigration adds a field to the encrypted deny-list: the new field is
// opaque going forward (queries on it are rejected), old rows keep it plaintext
// until a migration seals it.
func TestAddFieldMigration(t *testing.T) {
	dir := t.TempDir()
	key := mustKey(t)
	kr, _ := crypto.NewKeyring("k1", key)
	col, err := OpenCollection("users", dir, encCfgKR(kr, fieldsPolicy("pw")))
	if err != nil {
		t.Fatal(err)
	}
	defer col.Close()

	oldID, _, err := col.Insert(map[string]any{"pw": "p1", "ssn": "111"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// ssn is queryable while it is still plaintext.
	if _, err := col.Count(eqStr("ssn", "111")); err != nil {
		t.Fatalf("Count on ssn before add: %v", err)
	}

	if err := col.SetEncryptionPolicy(context.Background(), fieldsPolicy("pw", "ssn")); err != nil {
		t.Fatalf("add ssn: %v", err)
	}

	// Now ssn is opaque: a filter on it is rejected.
	if _, err := col.Count(eqStr("ssn", "111")); !errors.Is(err, crypto.ErrFieldEncrypted) {
		t.Fatalf("Count on ssn after add: got %v, want ErrFieldEncrypted", err)
	}

	newID, _, err := col.Insert(map[string]any{"pw": "p2", "ssn": "222"})
	if err != nil {
		t.Fatalf("Insert new: %v", err)
	}

	// The old row still has ssn plaintext on disk; migrate, then all ssn opaque.
	if err := col.MigrateNow(context.Background()); err != nil {
		t.Fatalf("MigrateNow: %v", err)
	}
	for _, e := range allSegEntries(t, dir, "users") {
		if e.Op == store.OpDelete {
			continue
		}
		if s, ok := e.Data["ssn"].(string); ok && !crypto.IsEncrypted(s) {
			t.Fatalf("id %d ssn still plaintext after migrate: %q", e.ID, s)
		}
	}
	// Values still round-trip.
	for id, want := range map[uint64]string{oldID: "111", newID: "222"} {
		got, _, err := col.FindByID(id)
		if err != nil {
			t.Fatalf("FindByID %d: %v", id, err)
		}
		if got["ssn"] != want {
			t.Errorf("id %d ssn = %v, want %v", id, got["ssn"], want)
		}
	}
}

// TestLazyMigrationOnUpdate confirms a record migrates itself to the current epoch
// when it is rewritten, without any compaction.
func TestLazyMigrationOnUpdate(t *testing.T) {
	dir := t.TempDir()
	key := mustKey(t)
	kr, _ := crypto.NewKeyring("k1", key)
	col, err := OpenCollection("users", dir, encCfgKR(kr, fieldsPolicy("pw")))
	if err != nil {
		t.Fatal(err)
	}
	defer col.Close()

	idA, _, err := col.Insert(map[string]any{"pw": "a"})
	if err != nil {
		t.Fatalf("Insert A: %v", err)
	}
	if _, _, err := col.Insert(map[string]any{"pw": "b"}); err != nil {
		t.Fatalf("Insert B: %v", err)
	}

	// Bump the epoch with a no-op-ish policy change (rotate), then rewrite only A.
	kr.Add("k2", mustKey(t))
	kr.SetCurrent("k2")
	if err := col.RotateKey(context.Background()); err != nil {
		t.Fatalf("RotateKey: %v", err)
	}
	if _, err := col.Update(idA, map[string]any{"pw": "a2"}); err != nil {
		t.Fatalf("Update A: %v", err)
	}

	st := col.EncryptionStatus()
	if st.Epoch != 2 || st.LiveRecords != 2 || st.LiveAtEpoch != 1 || st.FunctionalComplete {
		t.Fatalf("status = %+v, want epoch 2 with 1/2 migrated (only A rewritten)", st)
	}
}

// TestModeSwitchRejected verifies the reconstruction mode is fixed: you cannot flip
// a field-mode collection to record-mode (or vice versa) while data exists.
func TestModeSwitchRejected(t *testing.T) {
	dir := t.TempDir()
	kr, _ := crypto.NewKeyring("k1", mustKey(t))
	col, err := OpenCollection("users", dir, encCfgKR(kr, fieldsPolicy("pw")))
	if err != nil {
		t.Fatal(err)
	}
	defer col.Close()

	if err := col.SetEncryptionPolicy(context.Background(), recordPolicy("id")); err == nil {
		t.Fatal("expected mode-switch fields→record to be rejected")
	}
}

// TestMigrateNowReachesSecurityCompletion asserts that after a forced migration no
// stale below-epoch bytes remain anywhere in the segments (security completion),
// not merely that the live index is at the current epoch (functional completion).
func TestMigrateNowReachesSecurityCompletion(t *testing.T) {
	dir := t.TempDir()
	key := mustKey(t)
	kr, _ := crypto.NewKeyring("k1", key)
	col, err := OpenCollection("users", dir, encCfgKR(kr, fieldsPolicy("pw")))
	if err != nil {
		t.Fatal(err)
	}
	defer col.Close()

	id, _, err := col.Insert(map[string]any{"pw": "v1"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	// Supersede it a few times, leaving stale epoch-1 versions physically behind.
	for _, v := range []string{"v2", "v3"} {
		if _, err := col.Update(id, map[string]any{"pw": v}); err != nil {
			t.Fatalf("Update %s: %v", v, err)
		}
	}
	kr.Add("k2", mustKey(t))
	kr.SetCurrent("k2")
	if err := col.RotateKey(context.Background()); err != nil {
		t.Fatalf("RotateKey: %v", err)
	}

	if err := col.MigrateNow(context.Background()); err != nil {
		t.Fatalf("MigrateNow: %v", err)
	}
	// After a forced migration pass, every entry left on disk is at the current epoch.
	for _, ep := range liveEpochsOnDisk(t, dir, "users") {
		if ep != 2 {
			t.Fatalf("stale on-disk epoch %d remains after MigrateNow (want all 2)", ep)
		}
	}
}

// mutableProvider is a test KeyProvider whose keys can be retired at runtime, so
// tests can force a decrypt failure that a plain Keyring (no removal) cannot.
type mutableProvider struct {
	mu      sync.Mutex
	keys    map[string][]byte
	current string
}

func newMutableProvider(id string, key []byte) *mutableProvider {
	return &mutableProvider{keys: map[string][]byte{id: key}, current: id}
}

func (p *mutableProvider) add(id string, key []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.keys[id] = key
	p.current = id
}

func (p *mutableProvider) retire(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.keys, id)
}

func (p *mutableProvider) Current(context.Context) (string, []byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	k, ok := p.keys[p.current]
	if !ok {
		return "", nil, crypto.ErrKeyUnavailable
	}
	return p.current, append([]byte(nil), k...), nil
}

func (p *mutableProvider) ByID(_ context.Context, id string) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	k, ok := p.keys[id]
	if !ok {
		return nil, crypto.ErrKeyUnavailable
	}
	return append([]byte(nil), k...), nil
}

// TestMigrateFailsClosedOnRetiredKey confirms a migration pass aborts (and mutates
// nothing) when a key that still protects on-disk blobs has been retired too early.
func TestMigrateFailsClosedOnRetiredKey(t *testing.T) {
	dir := t.TempDir()
	prov := newMutableProvider("k1", mustKey(t))
	col, err := OpenCollection("users", dir, encCfgKR(prov, fieldsPolicy("pw")))
	if err != nil {
		t.Fatal(err)
	}
	defer col.Close()

	if _, _, err := col.Insert(map[string]any{"pw": "old"}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	// Rotate to k2, then retire k1 before migrating — the old blob can no longer be
	// decrypted, so the pass must fail closed.
	prov.add("k2", mustKey(t))
	if err := col.RotateKey(context.Background()); err != nil {
		t.Fatalf("RotateKey: %v", err)
	}
	newID, _, err := col.Insert(map[string]any{"pw": "new"})
	if err != nil {
		t.Fatalf("Insert new: %v", err)
	}
	prov.retire("k1")

	if err := col.MigrateNow(context.Background()); !errors.Is(err, crypto.ErrKeyUnavailable) {
		t.Fatalf("MigrateNow with retired key: got %v, want ErrKeyUnavailable", err)
	}
	// The pass aborted without losing data: the k2 row still reads.
	got, _, err := col.FindByID(newID)
	if err != nil {
		t.Fatalf("FindByID after aborted migrate: %v", err)
	}
	if got["pw"] != "new" {
		t.Fatalf("pw = %v, want new", got["pw"])
	}
}
