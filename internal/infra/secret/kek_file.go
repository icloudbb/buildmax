package secret

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
)

// kekFileProvider is the default KEK backend: a set of KEKs loaded from a file
// mounted read-only into the Server, with a pointer to the one new writes use.
// It holds a set rather than a single key so key_id versioning is real and a
// KEK rotation can rewrap existing rows without touching ciphertext. See
// docs/design/space-secrets.md §9.1. The KEK is never taken from an environment
// variable.
type kekFileProvider struct {
	keys    map[string][]byte
	current string
}

// kekFile is the on-disk shape. `current` names the KEK new writes use; `keys`
// maps a key_id to base64-encoded 32-byte key material. During a rotation the
// file holds both the old and new keys so every row stays decryptable.
type kekFile struct {
	Current string            `json:"current"`
	Keys    map[string]string `json:"keys"`
}

// LoadKEKFile reads and validates a KEK file. It fails rather than invent a
// key: a missing or malformed file when encrypted data exists means the values
// are unreadable, and pretending otherwise would silently lose them.
func LoadKEKFile(path string) (KEKProvider, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("secret: read KEK file: %w", err)
	}
	var f kekFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("secret: parse KEK file: %w", err)
	}
	if f.Current == "" {
		return nil, fmt.Errorf("secret: KEK file has no current key")
	}
	if len(f.Keys) == 0 {
		return nil, fmt.Errorf("secret: KEK file has no keys")
	}
	keys := make(map[string][]byte, len(f.Keys))
	for id, b64 := range f.Keys {
		key, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf("secret: KEK %q is not valid base64: %w", id, err)
		}
		if len(key) != dekSize {
			return nil, fmt.Errorf("secret: KEK %q is %d bytes, want %d", id, len(key), dekSize)
		}
		keys[id] = key
	}
	if _, ok := keys[f.Current]; !ok {
		return nil, fmt.Errorf("secret: current KEK %q is not among the file's keys", f.Current)
	}
	return &kekFileProvider{keys: keys, current: f.Current}, nil
}

// NewKEKFileProviderFromKeys builds a provider directly, for tests and for
// callers that generate keys in memory.
func NewKEKFileProviderFromKeys(keys map[string][]byte, current string) (KEKProvider, error) {
	if _, ok := keys[current]; !ok {
		return nil, fmt.Errorf("secret: current KEK %q is not among the keys", current)
	}
	dup := make(map[string][]byte, len(keys))
	for id, k := range keys {
		if len(k) != dekSize {
			return nil, fmt.Errorf("secret: KEK %q is %d bytes, want %d", id, len(k), dekSize)
		}
		dup[id] = k
	}
	return &kekFileProvider{keys: dup, current: current}, nil
}

// Wrap seals dek under the current KEK. The wrapped form is nonce||ciphertext
// so unwrap needs only the key material named by the returned keyID.
func (p *kekFileProvider) Wrap(dek []byte) ([]byte, string, error) {
	gcm, err := newGCM(p.keys[p.current])
	if err != nil {
		return nil, "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, "", fmt.Errorf("secret: generate wrap nonce: %w", err)
	}
	wrapped := gcm.Seal(nonce, nonce, dek, nil)
	return wrapped, p.current, nil
}

// Unwrap opens a wrapped DEK under the KEK named by keyID. A keyID the file no
// longer holds is refused rather than falling back, so a removed KEK surfaces
// as a clear error instead of a wrong key.
func (p *kekFileProvider) Unwrap(wrapped []byte, keyID string) ([]byte, error) {
	key, ok := p.keys[keyID]
	if !ok {
		return nil, fmt.Errorf("secret: KEK %q is not loaded", keyID)
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(wrapped) < ns {
		return nil, fmt.Errorf("secret: wrapped DEK is too short")
	}
	nonce, ciphertext := wrapped[:ns], wrapped[ns:]
	dek, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("secret: unwrap under KEK %q: %w", keyID, err)
	}
	return dek, nil
}

// CurrentKeyID names the KEK new wraps use.
func (p *kekFileProvider) CurrentKeyID() string { return p.current }

// KeyIDs lists the file's keys, sorted so a report is stable.
func (p *kekFileProvider) KeyIDs() []string {
	ids := make([]string, 0, len(p.keys))
	for id := range p.keys {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
