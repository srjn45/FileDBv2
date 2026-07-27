package crypto

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestKeyringCurrentAndByID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	k1 := testKey(t)
	kr, err := NewKeyring("k1", k1)
	if err != nil {
		t.Fatal(err)
	}
	id, key, err := kr.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if id != "k1" {
		t.Fatalf("Current id = %q, want k1", id)
	}
	if !bytes.Equal(key, k1) {
		t.Fatal("Current key mismatch")
	}
	got, err := kr.ByID(ctx, "k1")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, k1) {
		t.Fatal("ByID key mismatch")
	}
}

func TestKeyringByIDUnknown(t *testing.T) {
	t.Parallel()
	kr, err := NewKeyring("k1", testKey(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kr.ByID(context.Background(), "nope"); !errors.Is(err, ErrKeyUnavailable) {
		t.Fatalf("ByID unknown: got %v, want ErrKeyUnavailable", err)
	}
}

func TestKeyringRotation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	k1 := testKey(t)
	k2 := testKey(t)
	kr, err := NewKeyring("k1", k1)
	if err != nil {
		t.Fatal(err)
	}
	if err := kr.Add("k2", k2); err != nil {
		t.Fatal(err)
	}
	// Adding a key does not change current.
	if id, _, _ := kr.Current(ctx); id != "k1" {
		t.Fatalf("current after Add = %q, want k1", id)
	}
	if err := kr.SetCurrent("k2"); err != nil {
		t.Fatal(err)
	}
	id, key, err := kr.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if id != "k2" || !bytes.Equal(key, k2) {
		t.Fatalf("current after SetCurrent = %q, want k2", id)
	}
	// Old key still resolvable for decrypting old blobs.
	if _, err := kr.ByID(ctx, "k1"); err != nil {
		t.Fatalf("old key k1 no longer resolvable: %v", err)
	}
}

func TestKeyringSetCurrentUnknown(t *testing.T) {
	t.Parallel()
	kr, err := NewKeyring("k1", testKey(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := kr.SetCurrent("k2"); !errors.Is(err, ErrKeyUnavailable) {
		t.Fatalf("SetCurrent unknown: got %v, want ErrKeyUnavailable", err)
	}
}

func TestKeyringRejectsBadInput(t *testing.T) {
	t.Parallel()
	if _, err := NewKeyring("k1", make([]byte, 16)); err == nil {
		t.Fatal("NewKeyring with 16-byte key: want error")
	}
	if _, err := NewKeyring("bad:id", testKey(t)); err == nil {
		t.Fatal("NewKeyring with colon in id: want error")
	}
}

func TestKeyringDefensiveCopy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	orig := testKey(t)
	kr, err := NewKeyring("k1", orig)
	if err != nil {
		t.Fatal(err)
	}
	// Mutating the caller's slice must not affect the stored key.
	for i := range orig {
		orig[i] = 0
	}
	_, stored, err := kr.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(stored, orig) {
		t.Fatal("keyring did not defensively copy the key on Add")
	}
	// Mutating the returned slice must not affect the stored key either.
	for i := range stored {
		stored[i] = 0xff
	}
	_, stored2, _ := kr.Current(ctx)
	if bytes.Equal(stored2, stored) {
		t.Fatal("keyring did not defensively copy the key on Current")
	}
}

// Keyring must satisfy KeyProvider.
var _ KeyProvider = (*Keyring)(nil)
