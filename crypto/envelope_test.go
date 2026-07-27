package crypto

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	key, err := NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	return key
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	t.Parallel()
	key := testKey(t)
	aad := []byte("users:password")
	plaintext := []byte("hunter2")

	env, err := Encrypt(key, "k1", plaintext, aad)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if !IsEncrypted(env) {
		t.Fatalf("IsEncrypted(%q) = false, want true", env)
	}
	if !strings.HasPrefix(env, marker+":"+version+":k1:") {
		t.Fatalf("envelope has unexpected shape: %q", env)
	}

	got, err := Decrypt(key, env, aad)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("round-trip = %q, want %q", got, plaintext)
	}
}

func TestEncryptFreshNoncePerWrite(t *testing.T) {
	t.Parallel()
	key := testKey(t)
	aad := []byte("c:f")
	a, err := Encrypt(key, "k1", []byte("same"), aad)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encrypt(key, "k1", []byte("same"), aad)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two encryptions of the same plaintext produced identical envelopes; nonce not fresh")
	}
}

func TestEmptyPlaintextRoundTrips(t *testing.T) {
	t.Parallel()
	key := testKey(t)
	env, err := Encrypt(key, "k1", []byte{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decrypt(key, env, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("empty plaintext round-trip = %q, want empty", got)
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	t.Parallel()
	key := testKey(t)
	other := testKey(t)
	env, err := Encrypt(key, "k1", []byte("secret"), []byte("aad"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Decrypt(other, env, []byte("aad"))
	if !errors.Is(err, ErrDecryptFailed) {
		t.Fatalf("Decrypt with wrong key: got %v, want ErrDecryptFailed", err)
	}
}

func TestDecryptWrongAADFails(t *testing.T) {
	t.Parallel()
	key := testKey(t)
	env, err := Encrypt(key, "k1", []byte("secret"), []byte("users:password"))
	if err != nil {
		t.Fatal(err)
	}
	// Same key, different associated data — must not decrypt, proving the aad
	// binds the blob to its context (a blob can't be relocated to another field).
	_, err = Decrypt(key, env, []byte("users:ssn"))
	if !errors.Is(err, ErrDecryptFailed) {
		t.Fatalf("Decrypt with wrong aad: got %v, want ErrDecryptFailed", err)
	}
}

func TestDecryptTamperedCiphertextFails(t *testing.T) {
	t.Parallel()
	key := testKey(t)
	env, err := Encrypt(key, "k1", []byte("secret"), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Flip a byte in the base64 payload.
	b := []byte(env)
	b[len(b)-1] ^= 0x01
	_, err = Decrypt(key, string(b), nil)
	if !errors.Is(err, ErrDecryptFailed) {
		t.Fatalf("Decrypt of tampered envelope: got %v, want ErrDecryptFailed", err)
	}
}

func TestDecryptMalformedEnvelopes(t *testing.T) {
	t.Parallel()
	key := testKey(t)
	cases := map[string]string{
		"plaintext":         "just a plain value",
		"marker only":       marker,
		"no payload":        marker + ":" + version + ":k1",
		"bad version":       marker + ":v2:k1:AAAA",
		"empty key-id":      marker + ":" + version + "::AAAA",
		"bad base64":        marker + ":" + version + ":k1:!!!!",
		"payload too short": marker + ":" + version + ":k1:AAAA",
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := Decrypt(key, env, nil); !errors.Is(err, ErrDecryptFailed) {
				t.Fatalf("Decrypt(%q): got %v, want ErrDecryptFailed", env, err)
			}
		})
	}
}

func TestIsEncryptedAndReservedPrefix(t *testing.T) {
	t.Parallel()
	if IsEncrypted("hello") {
		t.Fatal("plaintext reported as encrypted")
	}
	if HasReservedPrefix("hello") {
		t.Fatal("plaintext reported as reserved-prefixed")
	}
	// A value carrying the marker but not the well-formed "marker:" is not a valid
	// envelope, yet must still be caught as reserved so the write path rejects it.
	forged := marker + "garbage"
	if IsEncrypted(forged) {
		t.Fatal("forged marker misreported as valid envelope")
	}
	if !HasReservedPrefix(forged) {
		t.Fatal("forged marker not caught by HasReservedPrefix")
	}
}

func TestKeyID(t *testing.T) {
	t.Parallel()
	key := testKey(t)
	env, err := Encrypt(key, "rotated-key-7", []byte("x"), nil)
	if err != nil {
		t.Fatal(err)
	}
	id, err := KeyID(env)
	if err != nil {
		t.Fatal(err)
	}
	if id != "rotated-key-7" {
		t.Fatalf("KeyID = %q, want rotated-key-7", id)
	}
	if _, err := KeyID("not an envelope"); !errors.Is(err, ErrDecryptFailed) {
		t.Fatalf("KeyID on plaintext: got %v, want ErrDecryptFailed", err)
	}
}

func TestEncryptRejectsBadKeyID(t *testing.T) {
	t.Parallel()
	key := testKey(t)
	if _, err := Encrypt(key, "", []byte("x"), nil); err == nil {
		t.Fatal("Encrypt with empty key-id: want error")
	}
	if _, err := Encrypt(key, "has:colon", []byte("x"), nil); err == nil {
		t.Fatal("Encrypt with colon in key-id: want error")
	}
}

func TestEncryptRejectsWrongKeySize(t *testing.T) {
	t.Parallel()
	if _, err := Encrypt(make([]byte, 16), "k1", []byte("x"), nil); err == nil {
		t.Fatal("Encrypt with 16-byte key: want error")
	}
}

func TestDecryptWith(t *testing.T) {
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

	// A blob under k2 decrypts through the keyring even though k1 is current.
	env, err := Encrypt(k2, "k2", []byte("payload"), []byte("aad"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecryptWith(ctx, kr, env, []byte("aad"))
	if err != nil {
		t.Fatalf("DecryptWith: %v", err)
	}
	if string(got) != "payload" {
		t.Fatalf("DecryptWith = %q, want payload", got)
	}

	// A blob under an unknown key-id is ErrKeyUnavailable, not ErrDecryptFailed.
	envUnknown, err := Encrypt(testKey(t), "k9", []byte("x"), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = DecryptWith(ctx, kr, envUnknown, nil)
	if !errors.Is(err, ErrKeyUnavailable) {
		t.Fatalf("DecryptWith unknown key-id: got %v, want ErrKeyUnavailable", err)
	}
}
