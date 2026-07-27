package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/srjn45/scriva/crypto"
)

const metaFilename = "meta.json"

// metaPath returns the meta.json path for a collection directory.
func metaPath(dir string) string { return filepath.Join(dir, metaFilename) }

// collectionMeta holds the small amount of state that is expensive to
// reconstruct by scanning segments on every startup.
type collectionMeta struct {
	IDCounter uint64    `json:"id_counter"`
	CreatedAt time.Time `json:"created_at"`
	// DefaultTTLSeconds, when > 0, is a per-collection default record TTL set
	// explicitly at CreateCollection time. It is omitted (0) for collections
	// that simply inherit the server-wide default, so changing the global
	// default still applies to them.
	DefaultTTLSeconds int64 `json:"default_ttl_seconds,omitempty"`
	// Encryption, when non-nil, records that this collection encrypts data at
	// rest: the policy applied to writes, the key-derivation parameters (for the
	// passphrase path), a key-check for fast wrong-key detection on open, and the
	// id of the key new writes use. It carries no secret material. It is omitted
	// for collections that are not encrypted.
	Encryption *encryptionMeta `json:"encryption,omitempty"`
}

// encryptionMeta is the persisted "encryption" block of a collection's meta.json.
// Everything in it is non-secret and safe to store in cleartext — it reveals
// nothing without the key.
type encryptionMeta struct {
	Policy       EncryptionPolicy `json:"policy"`
	KDF          *EncryptionKDF   `json:"kdf,omitempty"`
	KeyCheck     string           `json:"key_check"`
	CurrentKeyID string           `json:"current_key_id"`
}

// EncryptionKDF records the key-derivation parameters for a passphrase-derived
// key so the same key can be re-derived on reopen. The salt and parameters are
// non-secret. It is nil when the key is supplied directly (raw-key provider).
type EncryptionKDF struct {
	Algo   string           `json:"algo"`   // e.g. "argon2id"
	Salt   string           `json:"salt"`   // base64-encoded
	Params crypto.KDFParams `json:"params"` // cost parameters
}

// loadMeta reads and parses the meta.json file at path.
// Returns os.ErrNotExist if the file does not exist.
func loadMeta(path string) (collectionMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return collectionMeta{}, err
	}
	var m collectionMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return collectionMeta{}, err
	}
	return m, nil
}

// persistMeta writes m to path as JSON, atomically and durably (temp file →
// fsync → rename → fsync dir). A corrupt or partial write still degrades
// gracefully: the next startup falls back to the full segment scan and rewrites
// a fresh meta.json.
func persistMeta(path string, m collectionMeta) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o644)
}
