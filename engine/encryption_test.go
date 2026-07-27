//nolint:errcheck
package engine

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/srjn45/scriva/crypto"
	"github.com/srjn45/scriva/query"
)

func mustKey(t *testing.T) []byte {
	t.Helper()
	k, err := crypto.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	return k
}

// encCfg returns a collection config with encryption enabled under a fresh
// keyring built from key. Reuse the same key across reopens.
func encCfg(t *testing.T, key []byte, policy *EncryptionPolicy) CollectionConfig {
	t.Helper()
	kr, err := crypto.NewKeyring("k1", key)
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	cfg := testCfg()
	cfg.Encryption = policy
	cfg.KeyProvider = kr
	return cfg
}

func segBytes(t *testing.T, dir, collection string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, collection, "seg_000001.ndjson"))
	if err != nil {
		t.Fatalf("read segment: %v", err)
	}
	return b
}

func fieldsPolicy(fields ...string) *EncryptionPolicy {
	return &EncryptionPolicy{Mode: EncryptModeFields, Fields: fields}
}

func recordPolicy(indexFields ...string) *EncryptionPolicy {
	return &EncryptionPolicy{Mode: EncryptModeRecord, IndexFields: indexFields}
}

func TestFieldEncryptionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	col, err := OpenCollection("users", dir, encCfg(t, mustKey(t), fieldsPolicy("password", "ssn")))
	if err != nil {
		t.Fatal(err)
	}
	defer col.Close()

	id, _, err := col.Insert(map[string]any{
		"email":    "a@b.com",
		"password": "hunter2",
		"ssn":      "123-45-6789",
		"age":      float64(30),
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, _, err := col.FindByID(id)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	for k, want := range map[string]any{"email": "a@b.com", "password": "hunter2", "ssn": "123-45-6789", "age": float64(30)} {
		if got[k] != want {
			t.Errorf("field %q = %v, want %v", k, got[k], want)
		}
	}
}

func TestFieldEncryptionOnDiskOpaque(t *testing.T) {
	dir := t.TempDir()
	col, err := OpenCollection("users", dir, encCfg(t, mustKey(t), fieldsPolicy("password")))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := col.Insert(map[string]any{"email": "a@b.com", "password": "hunter2"}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	col.Close()

	raw := segBytes(t, dir, "users")
	if bytes.Contains(raw, []byte("hunter2")) {
		t.Fatal("plaintext secret 'hunter2' found on disk")
	}
	if !bytes.Contains(raw, []byte("a@b.com")) {
		t.Fatal("non-encrypted field 'email' should be plaintext on disk")
	}
}

func TestRecordEncryptionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	col, err := OpenCollection("audit", dir, encCfg(t, mustKey(t), recordPolicy("tenant")))
	if err != nil {
		t.Fatal(err)
	}
	defer col.Close()

	id, _, err := col.Insert(map[string]any{
		"tenant": "acme",
		"email":  "a@b.com",
		"note":   "sensitive",
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, _, err := col.FindByID(id)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	for k, want := range map[string]any{"tenant": "acme", "email": "a@b.com", "note": "sensitive"} {
		if got[k] != want {
			t.Errorf("field %q = %v, want %v", k, got[k], want)
		}
	}
	if _, leaked := got[secureDataField]; leaked {
		t.Fatal("secure_data blob leaked into the materialised record")
	}
}

func TestRecordEncryptionOnDiskOpaque(t *testing.T) {
	dir := t.TempDir()
	col, err := OpenCollection("audit", dir, encCfg(t, mustKey(t), recordPolicy("tenant")))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := col.Insert(map[string]any{"tenant": "acme", "email": "a@b.com", "note": "sensitive"}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	col.Close()

	raw := segBytes(t, dir, "audit")
	if bytes.Contains(raw, []byte("a@b.com")) || bytes.Contains(raw, []byte("sensitive")) {
		t.Fatal("record-level: non-index fields must not appear in plaintext on disk")
	}
	if !bytes.Contains(raw, []byte("acme")) {
		t.Fatal("record-level: index field 'tenant' should be plaintext on disk")
	}
	if !bytes.Contains(raw, []byte(secureDataField)) {
		t.Fatal("record-level: secure_data blob should be present on disk")
	}
}

func TestWrongKeyOnReopen(t *testing.T) {
	dir := t.TempDir()
	col, err := OpenCollection("users", dir, encCfg(t, mustKey(t), fieldsPolicy("password")))
	if err != nil {
		t.Fatal(err)
	}
	col.Insert(map[string]any{"password": "hunter2"})
	col.Close()

	// Reopen with a different key — the persisted key-check must fail fast.
	_, err = OpenCollection("users", dir, encCfg(t, mustKey(t), fieldsPolicy("password")))
	if !errors.Is(err, crypto.ErrWrongEncryptionKey) {
		t.Fatalf("reopen with wrong key: got %v, want ErrWrongEncryptionKey", err)
	}
}

func TestReopenSameKeyReadsData(t *testing.T) {
	dir := t.TempDir()
	key := mustKey(t)
	col, err := OpenCollection("users", dir, encCfg(t, key, fieldsPolicy("password")))
	if err != nil {
		t.Fatal(err)
	}
	id, _, _ := col.Insert(map[string]any{"email": "a@b.com", "password": "hunter2"})
	col.Close()

	// Reopen with the same key but no policy in config — the policy is loaded from
	// meta.json and the data must decrypt.
	kr, _ := crypto.NewKeyring("k1", key)
	cfg := testCfg()
	cfg.KeyProvider = kr
	col2, err := OpenCollection("users", dir, cfg)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer col2.Close()

	got, _, err := col2.FindByID(id)
	if err != nil {
		t.Fatalf("FindByID after reopen: %v", err)
	}
	if got["password"] != "hunter2" {
		t.Fatalf("password after reopen = %v, want hunter2", got["password"])
	}

	// A new write after reopen must still be encrypted (policy came from meta).
	id2, _, _ := col2.Insert(map[string]any{"password": "s3cret"})
	col2.Close()
	raw := segBytes(t, dir, "users")
	if bytes.Contains(raw, []byte("s3cret")) {
		t.Fatal("policy not applied to writes after reopen: secret found in plaintext")
	}
	_ = id2
}

func TestOpenEncryptedWithoutKeyFails(t *testing.T) {
	dir := t.TempDir()
	col, err := OpenCollection("users", dir, encCfg(t, mustKey(t), fieldsPolicy("password")))
	if err != nil {
		t.Fatal(err)
	}
	col.Insert(map[string]any{"password": "hunter2"})
	col.Close()

	// Reopen with no key at all — an encrypted collection cannot be opened.
	_, err = OpenCollection("users", dir, testCfg())
	if !errors.Is(err, crypto.ErrKeyUnavailable) {
		t.Fatalf("reopen without key: got %v, want ErrKeyUnavailable", err)
	}
}

func TestRejectIndexOnEncryptedField(t *testing.T) {
	dir := t.TempDir()
	col, err := OpenCollection("users", dir, encCfg(t, mustKey(t), fieldsPolicy("password")))
	if err != nil {
		t.Fatal(err)
	}
	defer col.Close()

	if err := col.EnsureIndex("password"); !errors.Is(err, crypto.ErrFieldEncrypted) {
		t.Fatalf("EnsureIndex on encrypted field: got %v, want ErrFieldEncrypted", err)
	}
	if err := col.EnsureUniqueIndex("password"); !errors.Is(err, crypto.ErrFieldEncrypted) {
		t.Fatalf("EnsureUniqueIndex on encrypted field: got %v, want ErrFieldEncrypted", err)
	}
	// A non-encrypted field indexes fine.
	if err := col.EnsureIndex("email"); err != nil {
		t.Fatalf("EnsureIndex on plaintext field: %v", err)
	}
}

func TestRejectFilterOnEncryptedField(t *testing.T) {
	dir := t.TempDir()
	col, err := OpenCollection("users", dir, encCfg(t, mustKey(t), fieldsPolicy("password")))
	if err != nil {
		t.Fatal(err)
	}
	defer col.Close()
	col.Insert(map[string]any{"email": "a@b.com", "password": "hunter2"})

	_, err = col.Scan(&query.FieldFilter{Field: "password", Op: query.OpEq, Value: `"hunter2"`})
	if !errors.Is(err, crypto.ErrFieldEncrypted) {
		t.Fatalf("filter on encrypted field: got %v, want ErrFieldEncrypted", err)
	}

	// A nested filter still catches the encrypted field.
	nested := &query.AndFilter{Filters: []query.Filter{
		&query.FieldFilter{Field: "email", Op: query.OpEq, Value: `"a@b.com"`},
		&query.FieldFilter{Field: "password", Op: query.OpEq, Value: `"hunter2"`},
	}}
	if _, err := col.Scan(nested); !errors.Is(err, crypto.ErrFieldEncrypted) {
		t.Fatalf("nested filter on encrypted field: got %v, want ErrFieldEncrypted", err)
	}

	// A filter on a plaintext field works and returns decrypted records.
	res, err := col.Scan(&query.FieldFilter{Field: "email", Op: query.OpEq, Value: `"a@b.com"`})
	if err != nil {
		t.Fatalf("filter on plaintext field: %v", err)
	}
	if len(res) != 1 || res[0].Data["password"] != "hunter2" {
		t.Fatalf("plaintext filter result = %+v", res)
	}
}

func TestRejectSortOnEncryptedField(t *testing.T) {
	dir := t.TempDir()
	col, err := OpenCollection("users", dir, encCfg(t, mustKey(t), fieldsPolicy("password")))
	if err != nil {
		t.Fatal(err)
	}
	defer col.Close()
	col.Insert(map[string]any{"password": "hunter2"})

	_, err = col.ScanStream(context.Background(), ScanOptions{Sort: []SortField{{Field: "password"}}}, func(ScanResult) error { return nil })
	if !errors.Is(err, crypto.ErrFieldEncrypted) {
		t.Fatalf("sort on encrypted field: got %v, want ErrFieldEncrypted", err)
	}
	_, err = col.ScanStream(context.Background(), ScanOptions{OrderBy: "password"}, func(ScanResult) error { return nil })
	if !errors.Is(err, crypto.ErrFieldEncrypted) {
		t.Fatalf("order-by on encrypted field: got %v, want ErrFieldEncrypted", err)
	}
}

func TestRejectAggregateOnEncryptedField(t *testing.T) {
	dir := t.TempDir()
	col, err := OpenCollection("users", dir, encCfg(t, mustKey(t), fieldsPolicy("salary")))
	if err != nil {
		t.Fatal(err)
	}
	defer col.Close()
	col.Insert(map[string]any{"dept": "eng", "salary": float64(100)})

	err = col.Aggregate(context.Background(), AggregateSpec{GroupBy: "salary"}, func(GroupResult) error { return nil })
	if !errors.Is(err, crypto.ErrFieldEncrypted) {
		t.Fatalf("group-by encrypted field: got %v, want ErrFieldEncrypted", err)
	}
	err = col.Aggregate(context.Background(), AggregateSpec{GroupBy: "dept", Field: "salary"}, func(GroupResult) error { return nil })
	if !errors.Is(err, crypto.ErrFieldEncrypted) {
		t.Fatalf("aggregate encrypted field: got %v, want ErrFieldEncrypted", err)
	}
}

func TestReservedPrefixRejected(t *testing.T) {
	dir := t.TempDir()
	key := mustKey(t)
	col, err := OpenCollection("users", dir, encCfg(t, key, fieldsPolicy("password")))
	if err != nil {
		t.Fatal(err)
	}
	defer col.Close()

	// A value that begins with the reserved marker (here, a real envelope) must be
	// rejected on write, even in a non-encrypted field, so the marker stays an
	// infallible read discriminator.
	env, err := crypto.Encrypt(key, "k1", []byte("x"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := col.Insert(map[string]any{"email": env}); !errors.Is(err, crypto.ErrReservedPrefix) {
		t.Fatalf("insert reserved-prefix value: got %v, want ErrReservedPrefix", err)
	}
}

func TestRecordModeFilterRules(t *testing.T) {
	dir := t.TempDir()
	col, err := OpenCollection("audit", dir, encCfg(t, mustKey(t), recordPolicy("tenant")))
	if err != nil {
		t.Fatal(err)
	}
	defer col.Close()
	col.Insert(map[string]any{"tenant": "acme", "email": "a@b.com"})
	col.Insert(map[string]any{"tenant": "globex", "email": "c@d.com"})

	// Filter on the plaintext index field works.
	res, err := col.Scan(&query.FieldFilter{Field: "tenant", Op: query.OpEq, Value: `"acme"`})
	if err != nil {
		t.Fatalf("filter on index field: %v", err)
	}
	if len(res) != 1 || res[0].Data["email"] != "a@b.com" {
		t.Fatalf("index-field filter result = %+v", res)
	}
	// Filter on a non-index (encrypted) field is rejected.
	if _, err := col.Scan(&query.FieldFilter{Field: "email", Op: query.OpEq, Value: `"a@b.com"`}); !errors.Is(err, crypto.ErrFieldEncrypted) {
		t.Fatalf("filter on non-index field: got %v, want ErrFieldEncrypted", err)
	}
}

func TestFieldProjectionLazyDecrypt(t *testing.T) {
	dir := t.TempDir()
	col, err := OpenCollection("users", dir, encCfg(t, mustKey(t), fieldsPolicy("password")))
	if err != nil {
		t.Fatal(err)
	}
	defer col.Close()
	col.Insert(map[string]any{"email": "a@b.com", "password": "hunter2"})

	// Project only the non-encrypted field: password must not appear.
	var got []ScanResult
	_, err = col.ScanStream(context.Background(), ScanOptions{Fields: []string{"email"}}, func(r ScanResult) error {
		got = append(got, r)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Data["email"] != "a@b.com" {
		t.Fatalf("projection result = %+v", got)
	}
	if _, present := got[0].Data["password"]; present {
		t.Fatal("password should not be present when projecting only email")
	}

	// Project the encrypted field: it must be decrypted.
	got = nil
	_, err = col.ScanStream(context.Background(), ScanOptions{Fields: []string{"password"}}, func(r ScanResult) error {
		got = append(got, r)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Data["password"] != "hunter2" {
		t.Fatalf("projected encrypted field = %+v", got)
	}
}

func TestRecordProjection(t *testing.T) {
	dir := t.TempDir()
	col, err := OpenCollection("audit", dir, encCfg(t, mustKey(t), recordPolicy("tenant")))
	if err != nil {
		t.Fatal(err)
	}
	defer col.Close()
	col.Insert(map[string]any{"tenant": "acme", "email": "a@b.com", "note": "x"})

	// Project a blob field: it must be decrypted and returned.
	var got []ScanResult
	_, err = col.ScanStream(context.Background(), ScanOptions{Fields: []string{"email"}}, func(r ScanResult) error {
		got = append(got, r)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Data["email"] != "a@b.com" {
		t.Fatalf("record projection of blob field = %+v", got)
	}
	if _, present := got[0].Data["note"]; present {
		t.Fatal("note should not be present when projecting only email")
	}
}

func TestKeyedWriteWithEncryption(t *testing.T) {
	dir := t.TempDir()
	col, err := OpenCollection("users", dir, encCfg(t, mustKey(t), fieldsPolicy("password")))
	if err != nil {
		t.Fatal(err)
	}
	defer col.Close()

	if _, _, err := col.InsertWithKey("u-1", map[string]any{"password": "hunter2"}); err != nil {
		t.Fatalf("InsertWithKey: %v", err)
	}
	rec, err := col.GetByKey("u-1")
	if err != nil {
		t.Fatalf("GetByKey: %v", err)
	}
	if rec.Key != "u-1" || rec.Data["password"] != "hunter2" {
		t.Fatalf("keyed record = %+v", rec)
	}
	if rec.Data[KeyField] != "u-1" {
		t.Fatalf("_key should stay plaintext, got %v", rec.Data[KeyField])
	}
}

func TestUpdateReEncrypts(t *testing.T) {
	dir := t.TempDir()
	col, err := OpenCollection("users", dir, encCfg(t, mustKey(t), fieldsPolicy("password")))
	if err != nil {
		t.Fatal(err)
	}
	id, _, _ := col.Insert(map[string]any{"password": "old"})
	if _, err := col.Update(id, map[string]any{"password": "new"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _, _ := col.FindByID(id)
	if got["password"] != "new" {
		t.Fatalf("after update password = %v, want new", got["password"])
	}
	col.Close()
	raw := segBytes(t, dir, "users")
	if bytes.Contains(raw, []byte(`"old"`)) || bytes.Contains(raw, []byte(`"new"`)) {
		t.Fatal("updated password value leaked in plaintext on disk")
	}
}

func TestEnableEncryptionOnLegacyData(t *testing.T) {
	dir := t.TempDir()
	// Start plaintext.
	col, err := OpenCollection("users", dir, testCfg())
	if err != nil {
		t.Fatal(err)
	}
	legacyID, _, _ := col.Insert(map[string]any{"email": "a@b.com", "password": "legacy"})
	col.Close()

	// Reopen with a policy — encryption is enabled going forward.
	col2, err := OpenCollection("users", dir, encCfg(t, mustKey(t), fieldsPolicy("password")))
	if err != nil {
		t.Fatalf("reopen with policy: %v", err)
	}
	defer col2.Close()

	// The legacy record has no marker, so it reads through as plaintext.
	got, _, err := col2.FindByID(legacyID)
	if err != nil {
		t.Fatalf("read legacy record: %v", err)
	}
	if got["password"] != "legacy" {
		t.Fatalf("legacy password = %v, want legacy", got["password"])
	}

	// A new write is encrypted.
	newID, _, _ := col2.Insert(map[string]any{"password": "fresh"})
	got2, _, _ := col2.FindByID(newID)
	if got2["password"] != "fresh" {
		t.Fatalf("new password = %v, want fresh", got2["password"])
	}
}

func TestSecureDataFieldReserved(t *testing.T) {
	dir := t.TempDir()
	col, err := OpenCollection("audit", dir, encCfg(t, mustKey(t), recordPolicy("tenant")))
	if err != nil {
		t.Fatal(err)
	}
	defer col.Close()

	if _, _, err := col.Insert(map[string]any{"tenant": "acme", secureDataField: "x"}); !errors.Is(err, ErrReservedField) {
		t.Fatalf("insert with secure_data field: got %v, want ErrReservedField", err)
	}
}

// materialize must surface ErrKeyUnavailable (not garbage or ErrDecryptFailed)
// when the key that sealed a blob is no longer in the keyring — the keyless /
// pre-rotation-retire path.
func TestMaterializeKeyUnavailable(t *testing.T) {
	policy := EncryptionPolicy{Mode: EncryptModeFields, Fields: []string{"pw"}}
	krA, _ := crypto.NewKeyring("k1", mustKey(t))
	encA := newEncryptor("users", policy.Mode, policy, krA, 1)
	stored, err := encA.encrypt(map[string]any{"pw": "secret"})
	if err != nil {
		t.Fatal(err)
	}

	// A keyring that does not contain k1.
	krB, _ := crypto.NewKeyring("k2", mustKey(t))
	encB := newEncryptor("users", policy.Mode, policy, krB, 1)
	if _, err := encB.materialize(context.Background(), stored, nil); !errors.Is(err, crypto.ErrKeyUnavailable) {
		t.Fatalf("materialize with missing key: got %v, want ErrKeyUnavailable", err)
	}
}

// A tampered ciphertext blob must fail closed with ErrDecryptFailed.
func TestMaterializeTamperFailsClosed(t *testing.T) {
	policy := EncryptionPolicy{Mode: EncryptModeFields, Fields: []string{"pw"}}
	kr, _ := crypto.NewKeyring("k1", mustKey(t))
	enc := newEncryptor("users", policy.Mode, policy, kr, 1)
	stored, err := enc.encrypt(map[string]any{"pw": "secret"})
	if err != nil {
		t.Fatal(err)
	}
	// Corrupt the envelope's payload.
	env := stored["pw"].(string)
	stored["pw"] = env[:len(env)-2] + "AA"
	if _, err := enc.materialize(context.Background(), stored, nil); !errors.Is(err, crypto.ErrDecryptFailed) {
		t.Fatalf("materialize tampered blob: got %v, want ErrDecryptFailed", err)
	}
}

// Compaction must stay key-oblivious: it copies the stored ciphertext verbatim,
// so encrypted data survives a pass unchanged and still decrypts, and the
// encryption block survives in meta.json.
func TestCompactionPreservesEncryption(t *testing.T) {
	dir := t.TempDir()
	key := mustKey(t)
	col, err := OpenCollection("users", dir, encCfg(t, key, fieldsPolicy("password")))
	if err != nil {
		t.Fatal(err)
	}
	id, _, _ := col.Insert(map[string]any{"email": "a@b.com", "password": "hunter2"})
	// Overwrite so the original version becomes stale (reclaimable by compaction).
	col.Update(id, map[string]any{"email": "a@b.com", "password": "hunter3"})
	if err := col.CompactNow(); err != nil {
		t.Fatalf("CompactNow: %v", err)
	}

	got, _, err := col.FindByID(id)
	if err != nil {
		t.Fatalf("FindByID after compaction: %v", err)
	}
	if got["password"] != "hunter3" {
		t.Fatalf("password after compaction = %v, want hunter3", got["password"])
	}
	col.Close()

	// Reopen with the same key — the encryption block survived compaction+close.
	kr, _ := crypto.NewKeyring("k1", key)
	cfg := testCfg()
	cfg.KeyProvider = kr
	col2, err := OpenCollection("users", dir, cfg)
	if err != nil {
		t.Fatalf("reopen after compaction: %v", err)
	}
	defer col2.Close()
	got2, _, err := col2.FindByID(id)
	if err != nil {
		t.Fatalf("FindByID after reopen: %v", err)
	}
	if got2["password"] != "hunter3" {
		t.Fatalf("password after reopen = %v, want hunter3", got2["password"])
	}
}

func TestInvalidPolicyRejected(t *testing.T) {
	dir := t.TempDir()
	// _key cannot be encrypted.
	if _, err := OpenCollection("c1", dir, encCfg(t, mustKey(t), fieldsPolicy(KeyField))); err == nil {
		t.Fatal("policy encrypting _key should be rejected")
	}
	// field-level mode needs at least one field.
	if _, err := OpenCollection("c2", dir, encCfg(t, mustKey(t), &EncryptionPolicy{Mode: EncryptModeFields})); err == nil {
		t.Fatal("empty field-level policy should be rejected")
	}
	// unknown mode.
	if _, err := OpenCollection("c3", dir, encCfg(t, mustKey(t), &EncryptionPolicy{Mode: "bogus"})); err == nil {
		t.Fatal("unknown mode should be rejected")
	}
}
