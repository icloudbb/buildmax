package secret

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// KEK rotation re-wraps each stored DEK under the current KEK and leaves the
// ciphertext alone: the DEK did not change, so neither did anything it
// encrypted. The DEK wrap binds no associated data, which is why a rewrap needs
// no knowledge of the Space or credential AAD its payload was sealed with, and
// cannot disturb it. See docs/design/space-secrets.md §9.1.

// CurrentKeyID names the KEK new seals and rewraps use.
func (c *Cipher) CurrentKeyID() string { return c.kek.CurrentKeyID() }

// RewrapDEK moves a DEK wrapped under keyID to the current KEK and returns the
// new wrapped form with the current key id. A DEK already under the current
// KEK is returned as it is.
func (c *Cipher) RewrapDEK(wrapped []byte, keyID string) ([]byte, string, error) {
	current := c.kek.CurrentKeyID()
	if keyID == current {
		return wrapped, keyID, nil
	}
	dek, err := c.kek.Unwrap(wrapped, keyID)
	if err != nil {
		return nil, "", err
	}
	defer clear(dek)
	rewrapped, newID, err := c.kek.Wrap(dek)
	if err != nil {
		return nil, "", fmt.Errorf("secret: rewrap dek: %w", err)
	}
	return rewrapped, newID, nil
}

// RewrapValue moves a SealValue blob's DEK to the current KEK and returns the
// rewritten blob and the key id it was under. rewrapped is nil when the blob is
// already under the current KEK, so a caller can skip the write.
func (c *Cipher) RewrapValue(blob []byte) (rewrapped []byte, fromKeyID string, err error) {
	var v sealedValue
	if err := json.Unmarshal(blob, &v); err != nil {
		return nil, "", fmt.Errorf("secret: unmarshal sealed value: %w", err)
	}
	if v.KeyID == c.kek.CurrentKeyID() {
		return nil, v.KeyID, nil
	}
	from := v.KeyID
	v.WrappedDEK, v.KeyID, err = c.RewrapDEK(v.WrappedDEK, v.KeyID)
	if err != nil {
		return nil, from, err
	}
	out, err := json.Marshal(v)
	if err != nil {
		return nil, from, fmt.Errorf("secret: marshal sealed value: %w", err)
	}
	return out, from, nil
}

// SealedValueKeyID returns the key id a SealValue blob names. It needs no KEK,
// so startup can find which keys stored credentials depend on even when no key
// file is configured.
func SealedValueKeyID(blob []byte) (string, error) {
	var v sealedValue
	if err := json.Unmarshal(blob, &v); err != nil {
		return "", fmt.Errorf("secret: unmarshal sealed value: %w", err)
	}
	return v.KeyID, nil
}

// RequireLoadedKeys refuses a KEK set that cannot open every stored row. refs
// counts sealed rows by the key id that wrapped them; kek is nil when the
// deployment configures no key file.
//
// This is the one gate on retiring a KEK. The key file is operator-owned and
// read only at startup, so removing a key a row still names is refused when the
// file is next loaded, before anything is served: generating a replacement or
// treating the rows as empty would lose them silently.
func RequireLoadedKeys(kek KEKProvider, refs map[string]int64) error {
	loaded := map[string]bool{}
	if kek != nil {
		for _, id := range kek.KeyIDs() {
			loaded[id] = true
		}
	}
	var missing []string
	for id, n := range refs {
		if n > 0 && !loaded[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	slices.Sort(missing)
	named := make([]string, 0, len(missing))
	for _, id := range missing {
		named = append(named, fmt.Sprintf("%q (%s)", id, rowCount(refs[id])))
	}
	where, remedy := "the KEK file does not hold it", "restore the key to the file"
	if len(missing) > 1 {
		where, remedy = "the KEK file does not hold them", "restore the keys to the file"
	}
	if kek == nil {
		where, remedy = "secret.kek_file is not configured", "configure secret.kek_file with a file that holds it"
	}
	return fmt.Errorf("secret: stored data is sealed under KEK %s, but %s; %s, "+
		"since a row whose KEK is gone can never be decrypted, "+
		"and retire a key only after `buildmax-server secret rewrap` reports no row under it",
		strings.Join(named, ", "), where, remedy)
}

func rowCount(n int64) string {
	if n == 1 {
		return "1 row"
	}
	return fmt.Sprintf("%d rows", n)
}
