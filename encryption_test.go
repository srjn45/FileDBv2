package scriva_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/srjn45/scriva"
	"github.com/srjn45/scriva/crypto"
	"github.com/srjn45/scriva/query"
)

// diskContains reports whether the raw on-disk bytes of any file under dir
// contain needle. Encryption tests use it to prove a secret never lands in
// cleartext in a segment file.
func diskContains(t *testing.T, dir, needle string) bool {
	t.Helper()
	found := false
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(b, []byte(needle)) {
			found = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %q: %v", dir, err)
	}
	return found
}

// TestFacadeFieldEncryptionRoundTrip exercises the headline §10 flow: a
// passphrase-derived key plus a field-level policy declared at Open. The secret
// field is opaque on disk, reads return plaintext, and a plaintext field stays
// queryable.
func TestFacadeFieldEncryptionRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	db, err := scriva.Open(dir,
		scriva.WithPassphrase("correct horse battery staple"),
		scriva.WithCollectionEncryption("users", scriva.EncryptFields("password", "ssn")),
	)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	users := db.MustCollection("users")

	id, _, err := users.Insert(map[string]any{
		"email":    "a@b.com",
		"password": "hunter2",
		"ssn":      "123-45-6789",
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	rec, err := users.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if rec.Data["password"] != "hunter2" || rec.Data["ssn"] != "123-45-6789" {
		t.Fatalf("decrypted record = %+v", rec.Data)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Secrets must not appear in cleartext on disk; the plaintext field may.
	if diskContains(t, dir, "hunter2") {
		t.Fatal("password present in cleartext on disk")
	}
	if diskContains(t, dir, "123-45-6789") {
		t.Fatal("ssn present in cleartext on disk")
	}
	if !diskContains(t, dir, "a@b.com") {
		t.Fatal("plaintext email should be readable on disk")
	}

	// Reopen with the same passphrase re-derives the key from the persisted salt.
	db2, err := scriva.Open(dir,
		scriva.WithPassphrase("correct horse battery staple"),
		scriva.WithCollectionEncryption("users", scriva.EncryptFields("password", "ssn")),
	)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()
	rec2, err := db2.MustCollection("users").Get(id)
	if err != nil {
		t.Fatalf("get after reopen: %v", err)
	}
	if rec2.Data["password"] != "hunter2" {
		t.Fatalf("reopened record = %+v", rec2.Data)
	}
}

// TestFacadeWrongPassphraseFailsFast confirms the key-check trips on Open with a
// wrong passphrase rather than yielding garbage on first read.
func TestFacadeWrongPassphraseFailsFast(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	db, err := scriva.Open(dir,
		scriva.WithPassphrase("right-passphrase"),
		scriva.WithCollectionEncryption("secrets", scriva.EncryptFields("v")),
	)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, _, err := db.MustCollection("secrets").Insert(map[string]any{"v": "top"}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	_, err = scriva.Open(dir,
		scriva.WithPassphrase("WRONG-passphrase"),
		scriva.WithCollectionEncryption("secrets", scriva.EncryptFields("v")),
	)
	if !errors.Is(err, crypto.ErrWrongEncryptionKey) {
		t.Fatalf("reopen with wrong passphrase: got %v, want ErrWrongEncryptionKey", err)
	}
}

// TestFacadeRawKeyEncryption covers WithEncryptionKey: a valid 32-byte key
// round-trips, and a wrong-length key fails eagerly at Open.
func TestFacadeRawKeyEncryption(t *testing.T) {
	t.Parallel()

	key, err := crypto.NewKey()
	if err != nil {
		t.Fatalf("new key: %v", err)
	}

	dir := t.TempDir()
	db, err := scriva.Open(dir,
		scriva.WithEncryptionKey(key),
		scriva.WithCollectionEncryption("vault", scriva.EncryptFields("secret")),
	)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	id, _, err := db.MustCollection("vault").Insert(map[string]any{"secret": "s3kr3t"})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	rec, err := db.MustCollection("vault").Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if rec.Data["secret"] != "s3kr3t" {
		t.Fatalf("record = %+v", rec.Data)
	}
	db.Close()

	// A wrong-length key is rejected at Open, before any collection is touched.
	_, err = scriva.Open(t.TempDir(),
		scriva.WithEncryptionKey([]byte("too-short")),
		scriva.WithCollectionEncryption("vault", scriva.EncryptFields("secret")),
	)
	if err == nil {
		t.Fatal("expected Open to reject a wrong-length key")
	}
}

// TestFacadeRecordEncryption covers record-level mode: the named index field
// stays plaintext and queryable, while every other field is sealed into the
// opaque blob.
func TestFacadeRecordEncryption(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	key, _ := crypto.NewKey()
	db, err := scriva.Open(dir,
		scriva.WithEncryptionKey(key),
		scriva.WithCollectionEncryption("audit", scriva.EncryptRecord("tenant")),
	)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	audit := db.MustCollection("audit")
	if _, _, err := audit.Insert(map[string]any{"tenant": "acme", "action": "delete-user", "actor": "root"}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// The plaintext index field is queryable.
	res, err := audit.Scan(&query.FieldFilter{Field: "tenant", Op: query.OpEq, Value: `"acme"`})
	if err != nil {
		t.Fatalf("scan by index field: %v", err)
	}
	if len(res) != 1 || res[0].Data["action"] != "delete-user" || res[0].Data["actor"] != "root" {
		t.Fatalf("record-mode scan result = %+v", res)
	}

	db.Close()
	// The sealed fields are opaque on disk; the index field is not.
	if diskContains(t, dir, "delete-user") || diskContains(t, dir, "root") {
		t.Fatal("record-mode sealed fields present in cleartext on disk")
	}
	if !diskContains(t, dir, "acme") {
		t.Fatal("record-mode index field should be plaintext on disk")
	}
}

// TestFacadeKeyProviderRotation drives the WithKeyProvider extension point and
// the runtime rotation surface on the returned engine.Collection: rotate to a
// second key, migrate existing data, and confirm reads keep working.
func TestFacadeKeyProviderRotation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	k1, _ := crypto.NewKey()
	kr, err := crypto.NewKeyring("k1", k1)
	if err != nil {
		t.Fatalf("new keyring: %v", err)
	}

	db, err := scriva.Open(dir,
		scriva.WithKeyProvider(kr),
		scriva.WithCollectionEncryption("users", scriva.EncryptFields("password")),
	)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	users := db.MustCollection("users")
	id, _, err := users.Insert(map[string]any{"email": "a@b.com", "password": "hunter2"})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Rotate the shared keyring to k2, then advance the collection to seal under it.
	k2, _ := crypto.NewKey()
	if err := kr.Add("k2", k2); err != nil {
		t.Fatalf("add k2: %v", err)
	}
	if err := kr.SetCurrent("k2"); err != nil {
		t.Fatalf("set current k2: %v", err)
	}
	ctx := context.Background()
	if err := users.RotateKey(ctx); err != nil {
		t.Fatalf("rotate key: %v", err)
	}

	// Bulk re-encrypt existing data under k2 (security completion).
	if err := users.MigrateNow(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := users.EncryptionStatus()
	if !st.FunctionalComplete || st.CurrentKeyID != "k2" {
		t.Fatalf("status after migrate = %+v", st)
	}

	rec, err := users.Get(id)
	if err != nil {
		t.Fatalf("get after rotation: %v", err)
	}
	if rec.Data["password"] != "hunter2" {
		t.Fatalf("record after rotation = %+v", rec.Data)
	}
	db.Close()
}

// TestFacadeEncryptSpecPolicy confirms the EncryptFields / EncryptRecord builders
// also drive a runtime enable via SetEncryptionPolicy, and that a plaintext
// collection can be encrypted going forward.
func TestFacadeEncryptSpecPolicy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	key, _ := crypto.NewKey()
	db, err := scriva.Open(dir, scriva.WithEncryptionKey(key))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Opened without a policy, so plaintext for now.
	notes := db.MustCollection("notes")
	if _, _, err := notes.Insert(map[string]any{"body": "before"}); err != nil {
		t.Fatalf("insert plaintext: %v", err)
	}

	// Enable field-level encryption at runtime using the façade builder.
	policy := scriva.EncryptFields("body").Policy()
	if err := notes.SetEncryptionPolicy(context.Background(), &policy); err != nil {
		t.Fatalf("set policy: %v", err)
	}
	id, _, err := notes.Insert(map[string]any{"body": "after"})
	if err != nil {
		t.Fatalf("insert after enable: %v", err)
	}
	rec, err := notes.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if rec.Data["body"] != "after" {
		t.Fatalf("record = %+v", rec.Data)
	}
	db.Close()

	if diskContains(t, dir, "after") {
		t.Fatal("newly encrypted field present in cleartext on disk")
	}
}
