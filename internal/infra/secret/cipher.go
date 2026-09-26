// Package secret implements the deployment's envelope encryption: a fresh
// per-write data key under AES-256-GCM, wrapped by a key-encryption key. It is
// the deployment's one key-management boundary, shared by two callers — Space
// Secrets (an item map, via Seal/Open) and managed-model provider credentials
// (a single value, via SealValue/OpenValue) — so a deployment configures and
// rotates one KEK, not two. It is the only place that touches plaintext bytes
// and key material. See docs/design/space-secrets.md §9.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"

	coresecret "github.com/icloudbb/buildmax/internal/core/secret"
)

// dekSize is 32 bytes: AES-256. A fresh DEK is generated per write, so a
// single key never encrypts two different item maps -- the property GCM
// requires and the reason the group is one atomic ciphertext.
const dekSize = 32

// KEKProvider wraps and unwraps a data-encryption key. The DEK never persists
// in the clear; only its wrapped form and the id of the KEK that wrapped it
// are stored. Implementations: a mounted key file (kekFileProvider), and later
// a cloud KMS or Vault transit key. See docs/design/space-secrets.md §9.1.
type KEKProvider interface {
	// Wrap seals dek and returns the wrapped bytes plus the id of the KEK
	// used, which unwrap needs to select the right key after a rotation.
	Wrap(dek []byte) (wrapped []byte, keyID string, err error)
	// Unwrap opens a DEK sealed under the KEK named by keyID.
	Unwrap(wrapped []byte, keyID string) (dek []byte, err error)
	// CurrentKeyID names the KEK Wrap uses: the target of a rewrap.
	CurrentKeyID() string
	// KeyIDs lists every KEK Unwrap can open, so startup can refuse a key
	// file that no longer holds a key a stored row names.
	KeyIDs() []string
}

// Cipher seals and opens a Secret's item map with envelope encryption: a fresh
// random DEK per write under AES-256-GCM, and the DEK wrapped by the KEK.
type Cipher struct {
	kek KEKProvider
}

// NewCipher returns a Cipher over the given KEK provider.
func NewCipher(kek KEKProvider) *Cipher { return &Cipher{kek: kek} }

// Cipher is the Sealer the secret service depends on.
var _ coresecret.Sealer = (*Cipher)(nil)

// Seal encrypts items into a Sealed blob. aad is bound into the ciphertext, so
// a blob authenticated for one deployment/space/secret fails to open under
// another -- the caller passes the associated data that names those.
func (c *Cipher) Seal(items coresecret.Items, aad []byte) (coresecret.Sealed, error) {
	plaintext, err := json.Marshal(map[string]string(items))
	if err != nil {
		return coresecret.Sealed{}, fmt.Errorf("secret: marshal items: %w", err)
	}
	ciphertext, nonce, wrapped, keyID, err := c.sealBytes(plaintext, aad)
	if err != nil {
		return coresecret.Sealed{}, err
	}
	return coresecret.Sealed{
		Ciphertext: ciphertext,
		Nonce:      nonce,
		WrappedDEK: wrapped,
		KeyID:      keyID,
	}, nil
}

// Open decrypts a Sealed blob back into items. aad must equal what Seal was
// given, or authentication fails. A failure here means tampering, a wrong KEK,
// or a mismatched associated data -- never a partial result.
func (c *Cipher) Open(s coresecret.Sealed, aad []byte) (coresecret.Items, error) {
	plaintext, err := c.openBytes(s.Ciphertext, s.Nonce, s.WrappedDEK, s.KeyID, aad)
	if err != nil {
		return nil, err
	}
	var items map[string]string
	if err := json.Unmarshal(plaintext, &items); err != nil {
		return nil, fmt.Errorf("secret: unmarshal items: %w", err)
	}
	return items, nil
}

// sealedValue is the framed on-disk form of a single value SealValue produced:
// the envelope fields serialized together so a caller stores one column instead
// of four. It is private -- callers treat the blob as opaque.
type sealedValue struct {
	Ciphertext []byte `json:"c"`
	Nonce      []byte `json:"n"`
	WrappedDEK []byte `json:"w"`
	KeyID      string `json:"k"`
}

// SealValue encrypts a single secret string into one self-describing blob,
// using the same envelope as Seal. aad domain-separates it: a model-credential
// blob (see the caller's tag) cannot be opened as a Space Secret blob or the
// reverse, even under the one shared KEK.
func (c *Cipher) SealValue(plaintext string, aad []byte) ([]byte, error) {
	ct, nonce, wrapped, keyID, err := c.sealBytes([]byte(plaintext), aad)
	if err != nil {
		return nil, err
	}
	blob, err := json.Marshal(sealedValue{Ciphertext: ct, Nonce: nonce, WrappedDEK: wrapped, KeyID: keyID})
	if err != nil {
		return nil, fmt.Errorf("secret: marshal sealed value: %w", err)
	}
	return blob, nil
}

// OpenValue reverses SealValue. A failure means tampering, a wrong KEK, or a
// mismatched aad -- never a partial result.
func (c *Cipher) OpenValue(blob []byte, aad []byte) (string, error) {
	var v sealedValue
	if err := json.Unmarshal(blob, &v); err != nil {
		return "", fmt.Errorf("secret: unmarshal sealed value: %w", err)
	}
	plaintext, err := c.openBytes(v.Ciphertext, v.Nonce, v.WrappedDEK, v.KeyID, aad)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// sealBytes envelope-encrypts plaintext: a fresh random DEK under AES-256-GCM,
// the DEK wrapped by the KEK. aad is bound into the ciphertext.
func (c *Cipher) sealBytes(plaintext, aad []byte) (ciphertext, nonce, wrapped []byte, keyID string, err error) {
	dek := make([]byte, dekSize)
	if _, err = io.ReadFull(rand.Reader, dek); err != nil {
		return nil, nil, nil, "", fmt.Errorf("secret: generate dek: %w", err)
	}
	gcm, err := newGCM(dek)
	if err != nil {
		return nil, nil, nil, "", err
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, nil, "", fmt.Errorf("secret: generate nonce: %w", err)
	}
	ciphertext = gcm.Seal(nil, nonce, plaintext, aad)
	wrapped, keyID, err = c.kek.Wrap(dek)
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("secret: wrap dek: %w", err)
	}
	return ciphertext, nonce, wrapped, keyID, nil
}

// openBytes reverses sealBytes.
func (c *Cipher) openBytes(ciphertext, nonce, wrapped []byte, keyID string, aad []byte) ([]byte, error) {
	dek, err := c.kek.Unwrap(wrapped, keyID)
	if err != nil {
		return nil, fmt.Errorf("secret: unwrap dek: %w", err)
	}
	gcm, err := newGCM(dek)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("secret: nonce is %d bytes, want %d", len(nonce), gcm.NonceSize())
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("secret: open ciphertext: %w", err)
	}
	return plaintext, nil
}

// newGCM builds an AES-256-GCM AEAD from a 32-byte key.
func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != dekSize {
		return nil, fmt.Errorf("secret: key is %d bytes, want %d", len(key), dekSize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secret: aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secret: gcm: %w", err)
	}
	return gcm, nil
}
