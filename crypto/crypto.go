// Package crypto is the self-contained cryptographic primitive behind ScrivaDB's
// transparent encryption-at-rest. It knows nothing about the storage engine,
// collections, or segments: it turns a key plus a plaintext value into a
// self-describing envelope string (and back), derives keys from passphrases, and
// manages a keyring for rotation.
//
// The envelope is a single string suitable for storing directly in a
// store.Entry data map:
//
//	<marker>:v1:<key-id>:<base64url( nonce || AEAD-ciphertext-with-tag )>
//
// The marker is a rare reserved sentinel (see IsEncrypted / HasReservedPrefix):
// values beginning with it are recognised as encrypted on read, and the engine
// rejects user writes that begin with it (ErrReservedPrefix) so the marker is an
// infallible "is this encrypted?" discriminator.
//
// The cipher is XChaCha20-Poly1305: an AEAD providing confidentiality and
// tamper-detection in one primitive, with a 192-bit random nonce that needs no
// counter bookkeeping. The AEAD associated data ("aad") is supplied by the
// caller — the engine binds each blob to its context (collection ‖ field) so a
// ciphertext cannot be relocated to another field or collection and still
// decrypt.
package crypto

import (
	"crypto/rand"

	"golang.org/x/crypto/chacha20poly1305"
)

// KeySize is the required length, in bytes, of an encryption key. It matches the
// XChaCha20-Poly1305 key size (256 bits).
const KeySize = chacha20poly1305.KeySize

// NonceSize is the length, in bytes, of the per-write random nonce prepended to
// each ciphertext. It matches the XChaCha20-Poly1305 extended-nonce size
// (192 bits), large enough that a fresh random nonce per write is safe without
// counter bookkeeping.
const NonceSize = chacha20poly1305.NonceSizeX

// NewKey returns a fresh, cryptographically random KeySize-byte key, suitable
// for use with WithEncryptionKey or as a keyring entry.
func NewKey() ([]byte, error) {
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}
