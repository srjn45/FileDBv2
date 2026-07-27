package engine

import (
	"errors"
	"os"
	"path/filepath"
)

// FindPersistedKDF scans the collections already on disk under dataDir and
// returns the first passphrase key-derivation block it finds, or nil if no
// collection records one.
//
// The embedded façade uses it to re-derive a passphrase key with the collection's
// original salt on reopen: the salt lives (non-secret) in each encrypted
// collection's meta.json, so a fresh random salt is minted only the first time a
// passphrase-encrypted collection is created, and every subsequent open re-derives
// the same key. One DB-wide key protects every collection, so the first block
// found is authoritative. A dataDir that does not exist yet is not an error
// (returns nil, nil) — it is simply a database that has never been opened.
func FindPersistedKDF(dataDir string) (*EncryptionKDF, error) {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m, err := loadMeta(metaPath(filepath.Join(dataDir, e.Name())))
		if err != nil {
			continue // missing/corrupt meta for this collection — skip, not fatal
		}
		if m.Encryption != nil && m.Encryption.KDF != nil {
			return m.Encryption.KDF, nil
		}
	}
	return nil, nil
}
