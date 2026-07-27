package scriva

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/srjn45/scriva/crypto"
	"github.com/srjn45/scriva/engine"
)

// This file adds the encryption-at-rest surface to the embedded façade: DB-wide
// key options (WithEncryptionKey / WithPassphrase / WithKeyProvider) and the
// per-collection policy option (WithCollectionEncryption + EncryptFields /
// EncryptRecord). Encryption is transparent — values are sealed on write and
// reconstructed on read at the collection boundary — and every option here is
// additive to the frozen v1.0.0 façade API.
//
// Runtime administration (rotating keys, migrating existing data in bulk,
// inspecting migration progress) is not re-wrapped here: DB.Collection returns an
// *engine.Collection, whose RotateKey, MigrateNow, CompactNow, SetEncryptionPolicy,
// and EncryptionStatus methods are the runtime admin surface. EncryptSpec.Policy
// lets the same EncryptFields / EncryptRecord builders drive SetEncryptionPolicy.

// builtinKeyID is the key id the built-in WithEncryptionKey and WithPassphrase
// providers seal new writes under. Rotating to a second key (k2, …) is done by
// supplying a WithKeyProvider you control — a *crypto.Keyring you Add/SetCurrent
// on — and then calling Collection.RotateKey.
const builtinKeyID = "k1"

// WithEncryptionKey configures a raw 32-byte encryption key for every collection
// that enables encryption. It is the entry point for apps that already manage key
// material (for example fetched from a KMS). The key must be exactly
// crypto.KeySize bytes; a wrong length fails at Open. For a human-supplied secret
// use WithPassphrase instead, and for an OS keychain / Vault / KMS integration use
// WithKeyProvider.
func WithEncryptionKey(key []byte) Option {
	return func(c *engine.CollectionConfig) {
		kr, err := crypto.NewKeyring(builtinKeyID, key)
		if err != nil {
			c.KeyProvider = errProvider{fmt.Errorf("scriva: WithEncryptionKey: %w", err)}
			return
		}
		c.KeyProvider = kr
	}
}

// WithPassphrase derives the encryption key from a human-supplied passphrase via
// Argon2id. A random salt is minted the first time a passphrase-encrypted
// collection is created and persisted (non-secret) in that collection's meta.json;
// every subsequent Open re-derives the same key from the persisted salt, so the
// passphrase alone is enough to reopen. The passphrase itself is never written to
// disk. A wrong passphrase fails fast on Open with crypto.ErrWrongEncryptionKey.
func WithPassphrase(passphrase string) Option {
	return func(c *engine.CollectionConfig) {
		c.KeyProvider = &deferredPassphrase{passphrase: passphrase, params: crypto.DefaultKDFParams()}
	}
}

// WithKeyProvider wires in a custom crypto.KeyProvider — an OS keychain, Vault, a
// KMS, or a *crypto.Keyring you rotate yourself. It is the extension point behind
// which key rotation lives: Add a new key and SetCurrent to it on a keyring you
// own, then call Collection.RotateKey to seal new writes under it while old blobs
// stay readable by id.
func WithKeyProvider(p crypto.KeyProvider) Option {
	return func(c *engine.CollectionConfig) { c.KeyProvider = p }
}

// EncryptSpec describes a collection's encryption policy. Build it with
// EncryptFields (field-level) or EncryptRecord (record-level) and pass it to
// WithCollectionEncryption.
type EncryptSpec struct {
	policy engine.EncryptionPolicy
}

// EncryptFields seals a deny-list of named top-level fields, leaving every other
// field plaintext and queryable (field-level mode). At least one field is
// required; the reserved _key and secure_data fields cannot be named. Encrypted
// fields cannot be indexed, filtered, sorted, or aggregated on — they are opaque
// on disk.
func EncryptFields(fields ...string) EncryptSpec {
	return EncryptSpec{policy: engine.EncryptionPolicy{
		Mode:   engine.EncryptModeFields,
		Fields: fields,
	}}
}

// EncryptRecord seals the whole record into a single opaque blob, keeping only
// the named index fields (and the reserved _key) plaintext and queryable
// (record-level mode). Name the fields you still need to filter or sort on;
// everything else moves into the sealed blob.
func EncryptRecord(indexFields ...string) EncryptSpec {
	return EncryptSpec{policy: engine.EncryptionPolicy{
		Mode:        engine.EncryptModeRecord,
		IndexFields: indexFields,
	}}
}

// Policy returns the engine.EncryptionPolicy this spec describes. It lets the
// EncryptFields / EncryptRecord builders also drive a runtime policy change via
// Collection.SetEncryptionPolicy on a handle returned by DB.Collection, not just
// the open-time WithCollectionEncryption path.
func (s EncryptSpec) Policy() engine.EncryptionPolicy { return s.policy }

// WithCollectionEncryption enables encryption for the named collection under spec.
// It is DB-wide: pass one per encrypted collection to Open. The policy takes effect
// when the collection is opened and is persisted in its meta.json, so it applies
// consistently across restarts. A key option (WithEncryptionKey / WithPassphrase /
// WithKeyProvider) must also be supplied, or opening the collection fails.
//
//	db, _ := scriva.Open("./data",
//	    scriva.WithPassphrase(os.Getenv("SCRIVA_PASSPHRASE")),
//	    scriva.WithCollectionEncryption("users", scriva.EncryptFields("password", "ssn")),
//	    scriva.WithCollectionEncryption("audit", scriva.EncryptRecord("id", "tenant")),
//	)
func WithCollectionEncryption(name string, spec EncryptSpec) Option {
	return func(c *engine.CollectionConfig) {
		if c.EncryptionByCollection == nil {
			c.EncryptionByCollection = make(map[string]*engine.EncryptionPolicy)
		}
		p := spec.policy
		c.EncryptionByCollection[name] = &p
	}
}

// errProvider is a sentinel KeyProvider that carries an option-construction error
// (for example a wrong-length raw key) so it can be surfaced eagerly at Open
// rather than on the first encrypted write. resolveKeyOptions detects it.
type errProvider struct{ err error }

func (e errProvider) Current(context.Context) (string, []byte, error) { return "", nil, e.err }
func (e errProvider) ByID(context.Context, string) ([]byte, error)    { return nil, e.err }

// errPassphraseUnresolved guards against a *deferredPassphrase leaking past Open
// unresolved; resolveKeyOptions always replaces it with a real keyring, so this
// error is an internal invariant check, never expected in practice.
var errPassphraseUnresolved = errors.New("scriva: passphrase provider used before resolution")

// deferredPassphrase is the placeholder KeyProvider WithPassphrase installs. It
// cannot derive a key on its own because the salt lives under the data directory,
// which is only known inside Open — resolveKeyOptions swaps it for a real keyring
// once the directory (and any persisted salt) is available.
type deferredPassphrase struct {
	passphrase string
	params     crypto.KDFParams
}

func (d *deferredPassphrase) Current(context.Context) (string, []byte, error) {
	return "", nil, errPassphraseUnresolved
}
func (d *deferredPassphrase) ByID(context.Context, string) ([]byte, error) {
	return nil, errPassphraseUnresolved
}

// resolveKeyOptions finalizes the DB-wide key provider once the data directory is
// known: it surfaces a deferred option error eagerly, and resolves a passphrase
// provider against the persisted salt (or a freshly minted one). It runs inside
// Open after all Options have been applied to base.
func resolveKeyOptions(dir string, base *engine.CollectionConfig) error {
	switch p := base.KeyProvider.(type) {
	case errProvider:
		return p.err
	case *deferredPassphrase:
		kr, kdf, err := resolvePassphrase(dir, p)
		if err != nil {
			return err
		}
		base.KeyProvider = kr
		base.EncryptionKDF = kdf
	}
	return nil
}

// resolvePassphrase turns a passphrase into a static keyring plus the KDF block to
// persist. It reuses the salt an existing passphrase-encrypted collection already
// recorded (so the same passphrase re-derives the same key on reopen), minting a
// fresh random salt only for a database with no encrypted collection yet.
func resolvePassphrase(dir string, d *deferredPassphrase) (crypto.KeyProvider, *engine.EncryptionKDF, error) {
	existing, err := engine.FindPersistedKDF(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("scriva: WithPassphrase: %w", err)
	}

	var (
		salt   []byte
		params crypto.KDFParams
		kdf    *engine.EncryptionKDF
	)
	if existing != nil {
		salt, err = base64.StdEncoding.DecodeString(existing.Salt)
		if err != nil {
			return nil, nil, fmt.Errorf("scriva: WithPassphrase: decode persisted salt: %w", err)
		}
		params = existing.Params
		kdf = existing
	} else {
		params = d.params
		salt, err = crypto.NewSalt(params.SaltLen)
		if err != nil {
			return nil, nil, fmt.Errorf("scriva: WithPassphrase: %w", err)
		}
		kdf = &engine.EncryptionKDF{
			Algo:   "argon2id",
			Salt:   base64.StdEncoding.EncodeToString(salt),
			Params: params,
		}
	}

	key := crypto.DeriveKey(d.passphrase, salt, params)
	kr, err := crypto.NewKeyring(builtinKeyID, key)
	if err != nil {
		return nil, nil, fmt.Errorf("scriva: WithPassphrase: %w", err)
	}
	return kr, kdf, nil
}
