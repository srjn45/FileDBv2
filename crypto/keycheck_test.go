package crypto

import (
	"errors"
	"testing"
)

func TestKeyCheckRoundTrip(t *testing.T) {
	t.Parallel()
	key := testKey(t)
	kc, err := MakeKeyCheck(key, "k1")
	if err != nil {
		t.Fatalf("MakeKeyCheck: %v", err)
	}
	if !IsEncrypted(kc) {
		t.Fatal("key-check is not a valid envelope")
	}
	if err := VerifyKeyCheck(key, kc); err != nil {
		t.Fatalf("VerifyKeyCheck with correct key: %v", err)
	}
}

func TestKeyCheckWrongKey(t *testing.T) {
	t.Parallel()
	key := testKey(t)
	wrong := testKey(t)
	kc, err := MakeKeyCheck(key, "k1")
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyKeyCheck(wrong, kc); !errors.Is(err, ErrWrongEncryptionKey) {
		t.Fatalf("VerifyKeyCheck with wrong key: got %v, want ErrWrongEncryptionKey", err)
	}
}

func TestKeyCheckMalformed(t *testing.T) {
	t.Parallel()
	key := testKey(t)
	if err := VerifyKeyCheck(key, "not an envelope"); !errors.Is(err, ErrWrongEncryptionKey) {
		t.Fatalf("VerifyKeyCheck on garbage: got %v, want ErrWrongEncryptionKey", err)
	}
}

// A derived (passphrase) key must produce a key-check that verifies only when
// the same passphrase re-derives the key — the wrong-passphrase-on-open guard.
func TestKeyCheckWithDerivedKey(t *testing.T) {
	t.Parallel()
	p := KDFParams{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, KeyLen: KeySize, SaltLen: 16}
	salt, _ := NewSalt(p.SaltLen)
	kc, err := MakeKeyCheck(DeriveKey("right", salt, p), "k1")
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyKeyCheck(DeriveKey("right", salt, p), kc); err != nil {
		t.Fatalf("correct passphrase: %v", err)
	}
	if err := VerifyKeyCheck(DeriveKey("wrong", salt, p), kc); !errors.Is(err, ErrWrongEncryptionKey) {
		t.Fatalf("wrong passphrase: got %v, want ErrWrongEncryptionKey", err)
	}
}
