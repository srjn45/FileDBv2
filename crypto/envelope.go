package crypto

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
)

// marker is the reserved sentinel that prefixes every encryption envelope. The
// leading NUL makes an accidental collision with a legitimate plaintext value
// essentially impossible, while the readable tail keeps the on-disk form
// self-identifying. It is deliberately not the human-readable "enc:v1:" used in
// the design doc's examples.
const marker = "\x00scrivaenc"

// version is the envelope format version, carried after the marker so the layout
// can evolve without ambiguity.
const version = "v1"

// envelopeCodec base64url-encodes the nonce‖ciphertext payload without padding,
// so the payload token never contains ':' and the envelope splits cleanly.
var envelopeCodec = base64.RawURLEncoding

// IsEncrypted reports whether s is a well-formed encryption envelope — i.e. it
// carries the reserved marker followed by the version delimiter. This is the
// read-side discriminator: a value for which IsEncrypted is false is plaintext
// and passed through unchanged.
func IsEncrypted(s string) bool {
	return strings.HasPrefix(s, marker+":")
}

// HasReservedPrefix reports whether s begins with the reserved marker at all,
// well-formed or not. The write path rejects any such user value
// (ErrReservedPrefix) so the marker can never be forged into a plaintext value
// and misread as ciphertext.
func HasReservedPrefix(s string) bool {
	return strings.HasPrefix(s, marker)
}

// Encrypt seals plaintext under key and returns the envelope string. aad is the
// AEAD associated data binding the blob to its context (e.g. collection ‖ field);
// the same aad must be supplied to Decrypt. keyID identifies the key so the value
// can be decrypted after rotation; it must be non-empty and contain no ':'.
func Encrypt(key []byte, keyID string, plaintext, aad []byte) (string, error) {
	if err := validateKeyID(keyID); err != nil {
		return "", err
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return "", fmt.Errorf("crypto: new cipher: %w", err)
	}
	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("crypto: read nonce: %w", err)
	}
	// Seal appends the ciphertext+tag to nonce, so the returned slice is the full
	// nonce‖ct payload with no extra copy.
	payload := aead.Seal(nonce, nonce, plaintext, aad)
	return marker + ":" + version + ":" + keyID + ":" + envelopeCodec.EncodeToString(payload), nil
}

// Decrypt opens the envelope under key, verifying aad, and returns the plaintext.
// A malformed envelope or a failed AEAD tag returns ErrDecryptFailed; it never
// returns ErrKeyUnavailable (key resolution is the caller's concern — see
// DecryptWith). Decrypt does not check that the envelope's key-id matches key, so
// callers that support rotation must resolve the key by KeyID first.
func Decrypt(key []byte, envelope string, aad []byte) ([]byte, error) {
	_, payload, err := parse(envelope)
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("%w: new cipher: %w", ErrDecryptFailed, err)
	}
	if len(payload) < NonceSize {
		return nil, fmt.Errorf("%w: payload shorter than nonce", ErrDecryptFailed)
	}
	nonce, ct := payload[:NonceSize], payload[NonceSize:]
	plaintext, err := aead.Open(nil, nonce, ct, aad)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDecryptFailed, err)
	}
	return plaintext, nil
}

// DecryptWith resolves the envelope's key through the provider and opens it. A
// key-id the provider cannot resolve yields ErrKeyUnavailable; a malformed
// envelope or failed AEAD tag yields ErrDecryptFailed. This is the convenience
// entry point for the engine read path, which supports rotation via the keyring.
func DecryptWith(ctx context.Context, p KeyProvider, envelope string, aad []byte) ([]byte, error) {
	id, err := KeyID(envelope)
	if err != nil {
		return nil, err
	}
	key, err := p.ByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("%w: key-id %q: %w", ErrKeyUnavailable, id, err)
	}
	return Decrypt(key, envelope, aad)
}

// KeyID returns the key-id an envelope was encrypted under, so a caller can
// resolve the right key before decrypting. It returns ErrDecryptFailed for a
// malformed envelope.
func KeyID(envelope string) (string, error) {
	id, _, err := parse(envelope)
	return id, err
}

// parse validates the marker and version and splits the envelope into its key-id
// and decoded nonce‖ciphertext payload. Every malformed-input path returns
// ErrDecryptFailed.
func parse(envelope string) (keyID string, payload []byte, err error) {
	if !IsEncrypted(envelope) {
		return "", nil, fmt.Errorf("%w: missing encryption marker", ErrDecryptFailed)
	}
	// Strip "<marker>:" then split the remaining "v1:<key-id>:<payload>" into its
	// three parts. SplitN with 3 keeps the payload intact even though it cannot
	// contain ':'.
	rest := envelope[len(marker)+1:]
	parts := strings.SplitN(rest, ":", 3)
	if len(parts) != 3 {
		return "", nil, fmt.Errorf("%w: malformed envelope", ErrDecryptFailed)
	}
	if parts[0] != version {
		return "", nil, fmt.Errorf("%w: unsupported envelope version %q", ErrDecryptFailed, parts[0])
	}
	if parts[1] == "" {
		return "", nil, fmt.Errorf("%w: empty key-id", ErrDecryptFailed)
	}
	payload, derr := envelopeCodec.DecodeString(parts[2])
	if derr != nil {
		return "", nil, fmt.Errorf("%w: base64: %w", ErrDecryptFailed, derr)
	}
	return parts[1], payload, nil
}

// validateKeyID rejects key-ids that would break envelope parsing.
func validateKeyID(keyID string) error {
	if keyID == "" {
		return fmt.Errorf("crypto: key-id must not be empty")
	}
	if strings.ContainsRune(keyID, ':') {
		return fmt.Errorf("crypto: key-id %q must not contain ':'", keyID)
	}
	return nil
}
