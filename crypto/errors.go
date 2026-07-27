package crypto

import "errors"

// The typed error vocabulary shared by the crypto primitive and the engine
// boundary that calls it. Defining them here keeps error identity stable across
// layers: the engine wraps these with record context (collection, id, field,
// segment/offset, key-id) but callers still match them with errors.Is.
var (
	// ErrReservedPrefix indicates a user-supplied value begins with the reserved
	// encryption marker. Such writes are rejected so the marker remains an
	// infallible discriminator for encrypted values on read.
	ErrReservedPrefix = errors.New("crypto: value uses the reserved encryption marker")

	// ErrKeyUnavailable indicates the key that a blob was encrypted under cannot
	// be resolved (its key-id is not in the keyring). This is an operational
	// condition — retry after providing the key — and is the expected outcome on
	// a keyless follower asked to decrypt an encrypted field.
	ErrKeyUnavailable = errors.New("crypto: encryption key unavailable")

	// ErrDecryptFailed indicates the envelope is malformed or the AEAD tag did
	// not verify. It is an integrity/tamper signal, mirroring
	// store.ErrCorruptEntry — never the result of a merely missing key
	// (that is ErrKeyUnavailable).
	ErrDecryptFailed = errors.New("crypto: decryption failed")

	// ErrWrongEncryptionKey indicates the key-check value in meta.json did not
	// verify under the supplied key or passphrase, so Open fails fast rather than
	// producing garbage decrypts on the first read.
	ErrWrongEncryptionKey = errors.New("crypto: wrong encryption key or passphrase")

	// ErrFieldEncrypted indicates an operation was attempted that an encrypted
	// field cannot support — indexing, filtering, sorting, or aggregating on it —
	// because the value is opaque on disk. Enforced at the engine boundary.
	ErrFieldEncrypted = errors.New("crypto: field is encrypted and cannot be indexed, filtered, or aggregated")
)
