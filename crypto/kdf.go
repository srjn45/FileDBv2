package crypto

import (
	"crypto/rand"
	"fmt"

	"golang.org/x/crypto/argon2"
)

// KDFParams are the Argon2id parameters used to derive a key from a passphrase.
// They are persisted (non-secret) in meta.json so the same key can be re-derived
// on reopen. The JSON tags match the meta.json "kdf.params" block.
type KDFParams struct {
	Memory      uint32 `json:"memory"`      // memory cost in KiB
	Iterations  uint32 `json:"iterations"`  // time cost (passes)
	Parallelism uint8  `json:"parallelism"` // number of lanes
	KeyLen      uint32 `json:"key_len"`     // derived key length in bytes
	SaltLen     int    `json:"salt_len"`    // random salt length in bytes
}

// DefaultKDFParams returns the baseline Argon2id parameters (OWASP-style):
// 64 MiB memory, 3 iterations, 1 lane, a 32-byte key, and a 16-byte salt. They
// are tunable for constrained or large hardware via an advanced option.
func DefaultKDFParams() KDFParams {
	return KDFParams{
		Memory:      64 * 1024, // 64 MiB, expressed in KiB
		Iterations:  3,
		Parallelism: 1,
		KeyLen:      KeySize,
		SaltLen:     16,
	}
}

// DeriveKey derives a key from passphrase and salt under the given Argon2id
// parameters. The same (passphrase, salt, params) always yields the same key, so
// callers persist salt and params (never the passphrase or key) in meta.json.
func DeriveKey(passphrase string, salt []byte, p KDFParams) []byte {
	return argon2.IDKey([]byte(passphrase), salt, p.Iterations, p.Memory, p.Parallelism, p.KeyLen)
}

// NewSalt returns n cryptographically random bytes for use as a KDF salt.
func NewSalt(n int) ([]byte, error) {
	if n <= 0 {
		return nil, fmt.Errorf("crypto: salt length must be positive, got %d", n)
	}
	salt := make([]byte, n)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	return salt, nil
}
