package crypto

import "crypto/subtle"

// keyCheckConstant is the fixed plaintext sealed into meta.json's key-check
// value. On Open it is decrypted and compared; a mismatch means the wrong key.
const keyCheckConstant = "scriva encryption key-check v1"

// keyCheckAAD binds the key-check envelope to its purpose so it cannot be
// confused with a data blob.
var keyCheckAAD = []byte("scriva:key-check")

// MakeKeyCheck seals the known key-check constant under key, producing the
// envelope stored in meta.json's "key_check" field. It is non-secret: it reveals
// nothing without the key, and lets a later Open verify the key fast.
func MakeKeyCheck(key []byte, keyID string) (string, error) {
	return Encrypt(key, keyID, []byte(keyCheckConstant), keyCheckAAD)
}

// VerifyKeyCheck confirms that key opens the stored key-check envelope. Any
// failure — unresolvable envelope, AEAD mismatch, or a decrypted value that does
// not equal the expected constant — returns ErrWrongEncryptionKey, so Open fails
// fast with a clear signal instead of producing garbage decrypts later.
func VerifyKeyCheck(key []byte, envelope string) error {
	plaintext, err := Decrypt(key, envelope, keyCheckAAD)
	if err != nil {
		return ErrWrongEncryptionKey
	}
	if subtle.ConstantTimeCompare(plaintext, []byte(keyCheckConstant)) != 1 {
		return ErrWrongEncryptionKey
	}
	return nil
}
