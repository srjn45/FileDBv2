package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/srjn45/scriva/crypto"
	"github.com/srjn45/scriva/query"
	"github.com/srjn45/scriva/store"
)

// EncryptionMode selects the granularity of transparent at-rest encryption for a
// collection.
type EncryptionMode string

const (
	// EncryptModeFields encrypts a deny-list of named top-level fields, leaving
	// every other field plaintext and queryable.
	EncryptModeFields EncryptionMode = "fields"
	// EncryptModeRecord encrypts the whole record into a single blob, keeping only
	// an allow-list of index fields (and the reserved _key) plaintext.
	EncryptModeRecord EncryptionMode = "record"
)

// secureDataField is the reserved top-level field that holds the sealed blob in
// record-level mode. It is reserved on write when record-level encryption is
// active, so it can never collide with a caller field.
const secureDataField = "secure_data"

// EncryptionPolicy configures transparent field- or record-level encryption for a
// single collection. The zero value (empty Mode) means no encryption. It is
// persisted in the collection's meta.json so it survives restart and is applied
// consistently by every writer; reads are policy-independent (driven by the
// per-value marker), so a mixed/half-migrated collection still reads correctly.
type EncryptionPolicy struct {
	Mode EncryptionMode `json:"mode"`
	// Fields is the deny-list of top-level field names to encrypt (mode "fields").
	Fields []string `json:"fields,omitempty"`
	// IndexFields is the allow-list of top-level field names left plaintext and
	// queryable (mode "record"); every other field is sealed into secure_data.
	IndexFields []string `json:"index_fields,omitempty"`
}

// validate rejects a policy that cannot be applied safely: an unknown mode, an
// empty field-level deny-list, or a reserved field (_key, secure_data) named as
// encryptable. The reserved _key must stay plaintext because keyed CRUD depends
// on it (see keys.go).
func (p EncryptionPolicy) validate() error {
	switch p.Mode {
	case EncryptModeFields:
		if len(p.Fields) == 0 {
			return fmt.Errorf("engine: encryption policy: field-level mode needs at least one field")
		}
		for _, f := range p.Fields {
			if f == KeyField {
				return fmt.Errorf("engine: encryption policy: reserved field %q cannot be encrypted", KeyField)
			}
			if f == secureDataField {
				return fmt.Errorf("engine: encryption policy: reserved field %q cannot be encrypted", secureDataField)
			}
		}
		return nil
	case EncryptModeRecord:
		for _, f := range p.IndexFields {
			if f == secureDataField {
				return fmt.Errorf("engine: encryption policy: reserved field %q cannot be a plaintext index field", secureDataField)
			}
		}
		return nil
	default:
		return fmt.Errorf("engine: encryption policy: unknown mode %q", p.Mode)
	}
}

// encryptor applies a collection's EncryptionPolicy at the read/write boundary.
// It is the only engine component that touches key material; everything
// downstream (segments, compaction, index, replication) stays key-oblivious
// because the stored data map holds ciphertext.
type encryptor struct {
	collection string
	policy     EncryptionPolicy
	keys       crypto.KeyProvider
	fieldSet   map[string]struct{} // fields mode: fields to encrypt
	indexSet   map[string]struct{} // record mode: plaintext allow-list
}

func newEncryptor(collection string, p EncryptionPolicy, keys crypto.KeyProvider) *encryptor {
	e := &encryptor{collection: collection, policy: p, keys: keys}
	switch p.Mode {
	case EncryptModeFields:
		e.fieldSet = toSet(p.Fields)
	case EncryptModeRecord:
		e.indexSet = toSet(p.IndexFields)
	}
	return e
}

func toSet(fields []string) map[string]struct{} {
	s := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		s[f] = struct{}{}
	}
	return s
}

// aad returns the AEAD associated data binding a blob to its context — the
// collection name and the field it lives under — so a ciphertext cannot be
// relocated to another field or collection and still decrypt.
func (e *encryptor) aad(field string) []byte {
	// The unit separator cannot appear in either component in practice and keeps
	// the two parts unambiguous.
	return []byte(e.collection + "\x1f" + field)
}

// encrypt transforms a plaintext record into its stored (ciphertext) form under
// the policy. It first rejects any caller value that collides with the reserved
// marker so the marker stays an infallible read discriminator, and reserves the
// secure_data field in record mode. The input map is never mutated.
func (e *encryptor) encrypt(data map[string]any) (map[string]any, error) {
	for _, v := range data {
		if s, ok := v.(string); ok && crypto.HasReservedPrefix(s) {
			return nil, fmt.Errorf("%w: a value begins with the reserved encryption marker", crypto.ErrReservedPrefix)
		}
	}

	keyID, key, err := e.keys.Current(context.Background())
	if err != nil {
		return nil, fmt.Errorf("engine: encrypt %q: %w", e.collection, err)
	}

	switch e.policy.Mode {
	case EncryptModeFields:
		out := make(map[string]any, len(data))
		for f, v := range data {
			out[f] = v
		}
		for f := range e.fieldSet {
			v, ok := data[f]
			if !ok {
				continue
			}
			env, err := e.seal(key, keyID, f, v)
			if err != nil {
				return nil, err
			}
			out[f] = env
		}
		return out, nil

	case EncryptModeRecord:
		if _, ok := data[secureDataField]; ok {
			return nil, fmt.Errorf("%w: %q is reserved for record-level encryption", ErrReservedField, secureDataField)
		}
		out := make(map[string]any, len(e.indexSet)+2)
		rest := make(map[string]any, len(data))
		for f, v := range data {
			if f == KeyField {
				out[f] = v
				continue
			}
			if _, ok := e.indexSet[f]; ok {
				out[f] = v
				continue
			}
			rest[f] = v
		}
		env, err := e.seal(key, keyID, secureDataField, rest)
		if err != nil {
			return nil, err
		}
		out[secureDataField] = env
		return out, nil

	default:
		return data, nil
	}
}

// seal JSON-encodes v and returns its envelope under the given key and field aad.
func (e *encryptor) seal(key []byte, keyID, field string, v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("engine: encrypt %s.%s: %w", e.collection, field, err)
	}
	env, err := crypto.Encrypt(key, keyID, b, e.aad(field))
	if err != nil {
		return "", fmt.Errorf("engine: encrypt %s.%s: %w", e.collection, field, err)
	}
	return env, nil
}

// materialize returns the plaintext record limited to fields (empty = the full
// record), decrypting only the blobs required to produce those fields. It is
// fail-closed: a decrypt error (crypto.ErrKeyUnavailable / crypto.ErrDecryptFailed)
// is propagated with collection/field context, never swallowed or partially
// returned. The stored map is never mutated.
func (e *encryptor) materialize(ctx context.Context, stored map[string]any, fields []string) (map[string]any, error) {
	if e.policy.Mode == EncryptModeRecord {
		return e.materializeRecord(ctx, stored, fields)
	}
	return e.materializeFields(ctx, stored, fields)
}

func (e *encryptor) materializeFields(ctx context.Context, stored map[string]any, fields []string) (map[string]any, error) {
	// Full record: decrypt every marker-bearing top-level value.
	if len(fields) == 0 {
		out := make(map[string]any, len(stored))
		for f, v := range stored {
			dv, err := e.maybeDecryptValue(ctx, f, v)
			if err != nil {
				return nil, err
			}
			out[f] = dv
		}
		return out, nil
	}
	// Projection: only the selected fields are materialised, so a projection that
	// excludes every encrypted field needs no key and cannot fail (lazy decrypt).
	// The reserved _key is always retained, mirroring ProjectData.
	out := make(map[string]any, len(fields)+1)
	for _, f := range fields {
		v, ok := stored[f]
		if !ok {
			continue
		}
		dv, err := e.maybeDecryptValue(ctx, f, v)
		if err != nil {
			return nil, err
		}
		out[f] = dv
	}
	if v, ok := stored[KeyField]; ok {
		out[KeyField] = v // _key is never encrypted
	}
	return out, nil
}

func (e *encryptor) materializeRecord(ctx context.Context, stored map[string]any, fields []string) (map[string]any, error) {
	// Full record: copy the plaintext index fields, then decrypt and merge the blob.
	if len(fields) == 0 {
		out := make(map[string]any, len(stored))
		for f, v := range stored {
			if f == secureDataField {
				continue
			}
			out[f] = v
		}
		merged, err := e.decryptBlob(ctx, stored[secureDataField])
		if err != nil {
			return nil, err
		}
		for k, v := range merged {
			out[k] = v
		}
		return out, nil
	}
	// Projection: take index fields (and _key) from the stored form directly; only
	// touch the blob when a requested field lives inside it, so a projection over
	// index columns alone needs no key.
	out := make(map[string]any, len(fields)+1)
	needBlob := false
	for _, f := range fields {
		if f == KeyField {
			continue // retained below
		}
		if _, ok := e.indexSet[f]; ok {
			if v, ok := stored[f]; ok {
				out[f] = v
			}
			continue
		}
		needBlob = true
	}
	if v, ok := stored[KeyField]; ok {
		out[KeyField] = v
	}
	if needBlob {
		merged, err := e.decryptBlob(ctx, stored[secureDataField])
		if err != nil {
			return nil, err
		}
		for _, f := range fields {
			if f == KeyField {
				continue
			}
			if _, isIndex := e.indexSet[f]; isIndex {
				continue
			}
			if v, ok := merged[f]; ok {
				out[f] = v
			}
		}
	}
	return out, nil
}

// maybeDecryptValue decrypts a single field value when it is a marker-bearing
// string, otherwise returns it unchanged (the policy-independent read invariant:
// a value is decrypted iff it carries the marker).
func (e *encryptor) maybeDecryptValue(ctx context.Context, field string, v any) (any, error) {
	s, ok := v.(string)
	if !ok || !crypto.IsEncrypted(s) {
		return v, nil
	}
	pt, err := crypto.DecryptWith(ctx, e.keys, s, e.aad(field))
	if err != nil {
		return nil, e.wrap(field, err)
	}
	var val any
	if err := json.Unmarshal(pt, &val); err != nil {
		return nil, e.wrap(field, fmt.Errorf("%w: %w", crypto.ErrDecryptFailed, err))
	}
	return val, nil
}

// decryptBlob decrypts the record-level secure_data blob into its field map. A
// blob that is absent or not a marker string (a legacy plaintext record that
// predates record mode) yields an empty map, so such records read through
// unchanged.
func (e *encryptor) decryptBlob(ctx context.Context, blob any) (map[string]any, error) {
	s, ok := blob.(string)
	if !ok || !crypto.IsEncrypted(s) {
		return nil, nil
	}
	pt, err := crypto.DecryptWith(ctx, e.keys, s, e.aad(secureDataField))
	if err != nil {
		return nil, e.wrap(secureDataField, err)
	}
	var m map[string]any
	if err := json.Unmarshal(pt, &m); err != nil {
		return nil, e.wrap(secureDataField, fmt.Errorf("%w: %w", crypto.ErrDecryptFailed, err))
	}
	return m, nil
}

func (e *encryptor) wrap(field string, err error) error {
	return fmt.Errorf("engine: decrypt %s.%s: %w", e.collection, field, err)
}

// isEncryptedField reports whether field is opaque on disk under the policy, and
// therefore cannot be indexed, filtered, sorted, or aggregated. In field mode
// that is exactly the deny-list; in record mode it is every field except the
// plaintext index allow-list and the reserved _key.
func (e *encryptor) isEncryptedField(field string) bool {
	switch e.policy.Mode {
	case EncryptModeFields:
		_, ok := e.fieldSet[field]
		return ok
	case EncryptModeRecord:
		if field == KeyField {
			return false
		}
		_, ok := e.indexSet[field]
		return !ok
	default:
		return false
	}
}

// checkField rejects a sort/group/aggregate reference to an encrypted field. An
// empty field name (no such clause) is always allowed.
func (e *encryptor) checkField(field string) error {
	if field != "" && e.isEncryptedField(field) {
		return fmt.Errorf("%w: cannot query on encrypted field %q", crypto.ErrFieldEncrypted, field)
	}
	return nil
}

// checkFilterFields walks a filter tree and rejects any predicate on an encrypted
// field, so an opaque field is a hard error at scan planning rather than a filter
// that silently never matches.
func (e *encryptor) checkFilterFields(f query.Filter) error {
	if f == nil {
		return nil
	}
	if bad, ok := e.encryptedFilterField(f); ok {
		return fmt.Errorf("%w: cannot filter on encrypted field %q", crypto.ErrFieldEncrypted, bad)
	}
	return nil
}

func (e *encryptor) encryptedFilterField(f query.Filter) (string, bool) {
	switch ff := f.(type) {
	case *query.FieldFilter:
		if e.isEncryptedField(ff.Field) {
			return ff.Field, true
		}
	case *query.AndFilter:
		for _, sub := range ff.Filters {
			if bad, ok := e.encryptedFilterField(sub); ok {
				return bad, true
			}
		}
	case *query.OrFilter:
		for _, sub := range ff.Filters {
			if bad, ok := e.encryptedFilterField(sub); ok {
				return bad, true
			}
		}
	}
	return "", false
}

// ---- Collection integration ------------------------------------------------

// initEncryption wires up the collection's encryptor after load(). For an
// already-encrypted collection it verifies the supplied key against the
// persisted key-check (fail-fast wrong-key detection) and loads the policy from
// meta.json. For a collection that config asks to encrypt for the first time, it
// validates and persists the policy plus a fresh key-check. A collection that is
// neither encrypted on disk nor asked to be stays plaintext.
func (c *Collection) initEncryption(cfg CollectionConfig) error {
	m, err := loadMeta(metaPath(c.dir))
	if err != nil {
		// A missing/corrupt meta.json means nothing is persisted yet; treat it as
		// no encryption block. load() has already reconstructed the rest.
		m = collectionMeta{}
	}

	switch {
	case m.Encryption != nil:
		// The collection holds encrypted data on disk — a key is mandatory.
		if cfg.KeyProvider == nil {
			return fmt.Errorf("engine: collection %q is encrypted and requires a key to open: %w", c.name, crypto.ErrKeyUnavailable)
		}
		_, key, err := cfg.KeyProvider.Current(context.Background())
		if err != nil {
			return fmt.Errorf("engine: open encrypted collection %q: %w", c.name, err)
		}
		if err := crypto.VerifyKeyCheck(key, m.Encryption.KeyCheck); err != nil {
			return fmt.Errorf("engine: open collection %q: %w", c.name, err)
		}
		c.encMeta = m.Encryption
		c.enc = newEncryptor(c.name, m.Encryption.Policy, cfg.KeyProvider)
		return nil

	case cfg.Encryption != nil:
		// Enabling encryption on a new (or legacy-plaintext) collection.
		if err := cfg.Encryption.validate(); err != nil {
			return err
		}
		if cfg.KeyProvider == nil {
			return fmt.Errorf("engine: collection %q: encryption policy set without a key provider", c.name)
		}
		keyID, key, err := cfg.KeyProvider.Current(context.Background())
		if err != nil {
			return fmt.Errorf("engine: enable encryption on %q: %w", c.name, err)
		}
		kc, err := crypto.MakeKeyCheck(key, keyID)
		if err != nil {
			return fmt.Errorf("engine: enable encryption on %q: %w", c.name, err)
		}
		c.encMeta = &encryptionMeta{
			Policy:       *cfg.Encryption,
			KDF:          cfg.EncryptionKDF,
			KeyCheck:     kc,
			CurrentKeyID: keyID,
		}
		c.enc = newEncryptor(c.name, *cfg.Encryption, cfg.KeyProvider)
		// Persist the encryption block immediately so a crash right after enabling
		// still records the policy and key-check.
		if err := persistMeta(metaPath(c.dir), c.metaSnapshot()); err != nil {
			return fmt.Errorf("engine: persist encryption meta for %q: %w", c.name, err)
		}
		return nil

	default:
		return nil // no encryption
	}
}

// sealEntryData replaces e.Data with its stored (ciphertext) form under the
// collection's policy, validating that no caller value collides with the
// reserved marker. It is a no-op when encryption is disabled. Callers invoke it
// before appending so nothing is written if sealing fails.
func (c *Collection) sealEntryData(e *store.Entry) error {
	if c.enc == nil {
		return nil
	}
	stored, err := c.enc.encrypt(e.Data)
	if err != nil {
		return err
	}
	e.Data = stored
	return nil
}
