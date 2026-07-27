package crypto

import (
	"bytes"
	"testing"
)

func TestDefaultKDFParams(t *testing.T) {
	t.Parallel()
	p := DefaultKDFParams()
	if p.Memory != 64*1024 {
		t.Errorf("Memory = %d KiB, want 65536 (64 MiB)", p.Memory)
	}
	if p.Iterations != 3 {
		t.Errorf("Iterations = %d, want 3", p.Iterations)
	}
	if p.Parallelism != 1 {
		t.Errorf("Parallelism = %d, want 1", p.Parallelism)
	}
	if p.KeyLen != KeySize {
		t.Errorf("KeyLen = %d, want %d", p.KeyLen, KeySize)
	}
	if p.SaltLen != 16 {
		t.Errorf("SaltLen = %d, want 16", p.SaltLen)
	}
}

func TestDeriveKeyDeterministic(t *testing.T) {
	t.Parallel()
	// Use cheap params so the test stays fast; determinism is what matters here.
	p := KDFParams{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, KeyLen: KeySize, SaltLen: 16}
	salt, err := NewSalt(p.SaltLen)
	if err != nil {
		t.Fatal(err)
	}
	a := DeriveKey("correct horse battery staple", salt, p)
	b := DeriveKey("correct horse battery staple", salt, p)
	if !bytes.Equal(a, b) {
		t.Fatal("DeriveKey not deterministic for same passphrase+salt+params")
	}
	if len(a) != int(p.KeyLen) {
		t.Fatalf("derived key length = %d, want %d", len(a), p.KeyLen)
	}
}

func TestDeriveKeyVariesBySaltAndPassphrase(t *testing.T) {
	t.Parallel()
	p := KDFParams{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, KeyLen: KeySize, SaltLen: 16}
	salt1, _ := NewSalt(p.SaltLen)
	salt2, _ := NewSalt(p.SaltLen)
	base := DeriveKey("pw", salt1, p)
	if bytes.Equal(base, DeriveKey("pw", salt2, p)) {
		t.Fatal("different salt produced same key")
	}
	if bytes.Equal(base, DeriveKey("PW", salt1, p)) {
		t.Fatal("different passphrase produced same key")
	}
}

func TestDerivedKeyEncrypts(t *testing.T) {
	t.Parallel()
	p := KDFParams{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, KeyLen: KeySize, SaltLen: 16}
	salt, _ := NewSalt(p.SaltLen)
	key := DeriveKey("passphrase", salt, p)
	env, err := Encrypt(key, "k1", []byte("secret"), nil)
	if err != nil {
		t.Fatalf("Encrypt with derived key: %v", err)
	}
	// Re-derive and decrypt, as a reopen would.
	got, err := Decrypt(DeriveKey("passphrase", salt, p), env, nil)
	if err != nil {
		t.Fatalf("Decrypt with re-derived key: %v", err)
	}
	if string(got) != "secret" {
		t.Fatalf("round-trip = %q, want secret", got)
	}
}

func TestNewSaltRejectsNonPositive(t *testing.T) {
	t.Parallel()
	if _, err := NewSalt(0); err == nil {
		t.Fatal("NewSalt(0): want error")
	}
}

func TestNewSaltUnique(t *testing.T) {
	t.Parallel()
	a, _ := NewSalt(16)
	b, _ := NewSalt(16)
	if bytes.Equal(a, b) {
		t.Fatal("two salts collided")
	}
}
