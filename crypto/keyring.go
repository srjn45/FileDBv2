package crypto

import (
	"context"
	"fmt"
	"sync"
)

// KeyProvider supplies keys to the engine: the current key for new writes, and
// any (possibly retired) key by id for decrypting old blobs. It is the single
// seam through which power users wire in an OS keychain, Vault, or KMS. The
// built-in WithEncryptionKey / WithPassphrase paths are thin providers over a
// static Keyring.
type KeyProvider interface {
	// Current returns the key to encrypt new writes with, and its id.
	Current(ctx context.Context) (id string, key []byte, err error)
	// ByID returns a (possibly retired) key for decrypting old blobs. It returns
	// ErrKeyUnavailable when the id is unknown.
	ByID(ctx context.Context, id string) (key []byte, err error)
}

// Keyring is the built-in static KeyProvider: an in-memory set of id→key entries
// with one marked current. It supports rotation — Add a new key, SetCurrent to
// it — while retaining old keys so blobs written under them still decrypt until a
// re-encrypting compaction pass retires them.
type Keyring struct {
	mu        sync.RWMutex
	keys      map[string][]byte
	currentID string
}

// NewKeyring returns a Keyring holding a single key marked current. The key must
// be exactly KeySize bytes and keyID must be a valid envelope key-id.
func NewKeyring(keyID string, key []byte) (*Keyring, error) {
	k := &Keyring{keys: make(map[string][]byte)}
	if err := k.Add(keyID, key); err != nil {
		return nil, err
	}
	k.currentID = keyID
	return k, nil
}

// Add registers a key under keyID without changing which key is current. It is
// how a retired or incoming rotation key is made resolvable for reads.
func (k *Keyring) Add(keyID string, key []byte) error {
	if err := validateKeyID(keyID); err != nil {
		return err
	}
	if len(key) != KeySize {
		return fmt.Errorf("crypto: key for id %q must be %d bytes, got %d", keyID, KeySize, len(key))
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	// Defensive copy so a caller mutating its slice cannot corrupt the keyring.
	cp := make([]byte, len(key))
	copy(cp, key)
	k.keys[keyID] = cp
	return nil
}

// SetCurrent marks an already-registered key as the one for new writes. It
// returns ErrKeyUnavailable if keyID is unknown.
func (k *Keyring) SetCurrent(keyID string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, ok := k.keys[keyID]; !ok {
		return fmt.Errorf("%w: key-id %q not in keyring", ErrKeyUnavailable, keyID)
	}
	k.currentID = keyID
	return nil
}

// Current returns the current key and its id.
func (k *Keyring) Current(ctx context.Context) (string, []byte, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	key, ok := k.keys[k.currentID]
	if !ok {
		return "", nil, fmt.Errorf("%w: no current key", ErrKeyUnavailable)
	}
	return k.currentID, clone(key), nil
}

// ByID returns the key registered under id, or ErrKeyUnavailable if none is.
func (k *Keyring) ByID(ctx context.Context, id string) ([]byte, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	key, ok := k.keys[id]
	if !ok {
		return nil, fmt.Errorf("%w: key-id %q not in keyring", ErrKeyUnavailable, id)
	}
	return clone(key), nil
}

func clone(b []byte) []byte {
	cp := make([]byte, len(b))
	copy(cp, b)
	return cp
}
