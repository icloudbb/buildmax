package secret

import (
	"bytes"
	"strings"
	"testing"

	coresecret "github.com/icloudbb/buildmax/internal/core/secret"
)

// rotated returns ciphers over the key sets a rotation passes through: the old
// key alone, both keys with the new one current, and the new key alone.
func rotated(t *testing.T) (before, during, after *Cipher) {
	t.Helper()
	oldKey, newKey := randKey(t), randKey(t)
	mk := func(keys map[string][]byte, current string) *Cipher {
		kek, err := NewKEKFileProviderFromKeys(keys, current)
		if err != nil {
			t.Fatal(err)
		}
		return NewCipher(kek)
	}
	before = mk(map[string][]byte{"file:root:1": oldKey}, "file:root:1")
	during = mk(map[string][]byte{"file:root:1": oldKey, "file:root:2": newKey}, "file:root:2")
	after = mk(map[string][]byte{"file:root:2": newKey}, "file:root:2")
	return before, during, after
}

// TestRewrapDEK_MovesItemsToTheCurrentKey is the rotation contract for a Space
// Secret: after a rewrap the row opens without the old key, under the same AAD,
// from the same ciphertext.
func TestRewrapDEK_MovesItemsToTheCurrentKey(t *testing.T) {
	before, during, after := rotated(t)
	aad := coresecret.AAD("sp_1")
	sealed, err := before.Seal(coresecret.Items{"TOKEN": "v1"}, aad)
	if err != nil {
		t.Fatal(err)
	}

	wrapped, keyID, err := during.RewrapDEK(sealed.WrappedDEK, sealed.KeyID)
	if err != nil {
		t.Fatal(err)
	}
	if keyID != "file:root:2" {
		t.Fatalf("keyID = %q, want the current key", keyID)
	}
	moved := sealed
	moved.WrappedDEK, moved.KeyID = wrapped, keyID
	items, err := after.Open(moved, aad)
	if err != nil {
		t.Fatalf("a rewrapped row must open with only the new key: %v", err)
	}
	if items["TOKEN"] != "v1" {
		t.Fatalf("items = %v", items)
	}
	if _, err := after.Open(moved, coresecret.AAD("sp_2")); err == nil {
		t.Fatal("a rewrap must keep the ciphertext bound to its Space")
	}

	// Already current: returned untouched, so a re-run writes nothing new.
	same, id, err := during.RewrapDEK(wrapped, keyID)
	if err != nil || id != keyID || !bytes.Equal(same, wrapped) {
		t.Fatalf("rewrap of a current DEK = %x, %q, %v; want it unchanged", same, id, err)
	}
}

func TestRewrapValue_MovesACredentialToTheCurrentKey(t *testing.T) {
	before, during, after := rotated(t)
	aad := []byte("bmax-llm-credential\x00")
	blob, err := before.SealValue("sk-live", aad)
	if err != nil {
		t.Fatal(err)
	}
	if id, err := SealedValueKeyID(blob); err != nil || id != "file:root:1" {
		t.Fatalf("SealedValueKeyID = %q, %v", id, err)
	}

	moved, from, err := during.RewrapValue(blob)
	if err != nil {
		t.Fatal(err)
	}
	if from != "file:root:1" || moved == nil {
		t.Fatalf("from = %q, moved = %v", from, moved != nil)
	}
	if id, _ := SealedValueKeyID(moved); id != "file:root:2" {
		t.Fatalf("rewrapped blob names %q, want the current key", id)
	}
	got, err := after.OpenValue(moved, aad)
	if err != nil || got != "sk-live" {
		t.Fatalf("OpenValue after rewrap = %q, %v", got, err)
	}
	if _, err := after.OpenValue(moved, []byte("bmax-secret\x00")); err == nil {
		t.Fatal("a rewrap must keep the credential's domain separation")
	}

	again, from, err := during.RewrapValue(moved)
	if err != nil || again != nil || from != "file:root:2" {
		t.Fatalf("rewrap of a current blob = %v, %q, %v; want nil", again != nil, from, err)
	}
}

func TestRewrap_RefusesAnUnloadedKey(t *testing.T) {
	before, _, after := rotated(t)
	sealed, err := before.Seal(coresecret.Items{"K": "v"}, coresecret.AAD("sp_1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := after.RewrapDEK(sealed.WrappedDEK, sealed.KeyID); err == nil ||
		!strings.Contains(err.Error(), "file:root:1") {
		t.Fatalf("rewrap without the old key: err = %v, want it named", err)
	}
}

func TestRequireLoadedKeys(t *testing.T) {
	_, during, after := rotated(t)
	refs := map[string]int64{"file:root:1": 3, "file:root:2": 1}

	if err := RequireLoadedKeys(during.kek, refs); err != nil {
		t.Fatalf("both keys loaded: %v", err)
	}
	err := RequireLoadedKeys(after.kek, refs)
	if err == nil || !strings.Contains(err.Error(), `"file:root:1" (3 rows)`) ||
		!strings.Contains(err.Error(), "does not hold it") {
		t.Fatalf("removed key still referenced: err = %v, want the key and its count", err)
	}
	err = RequireLoadedKeys(nil, map[string]int64{"file:root:1": 1})
	if err == nil || !strings.Contains(err.Error(), `"file:root:1" (1 row)`) ||
		!strings.Contains(err.Error(), "secret.kek_file is not configured") {
		t.Fatalf("no key file with sealed rows: err = %v", err)
	}
	if err := RequireLoadedKeys(nil, nil); err != nil {
		t.Fatalf("nothing sealed and no key file: %v", err)
	}
	if err := RequireLoadedKeys(after.kek, map[string]int64{"file:root:1": 0}); err != nil {
		t.Fatalf("a key no row names may be absent: %v", err)
	}
}
