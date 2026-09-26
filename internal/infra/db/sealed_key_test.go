package db

import (
	"crypto/rand"
	"fmt"
	"strings"
	"testing"

	coresecret "github.com/icloudbb/buildmax/internal/core/secret"
	infrasecret "github.com/icloudbb/buildmax/internal/infra/secret"
)

// kekRotation is the key sets one KEK rotation passes through, under key ids
// unique to the test so reference counts are its own rows only.
type kekRotation struct {
	oldID, newID   string
	before, during *infrasecret.Cipher
	after          infrasecret.KEKProvider
}

func newKEKRotation(t *testing.T) kekRotation {
	t.Helper()
	name := strings.ToLower(testPublicID(t))
	r := kekRotation{oldID: "file:" + name + ":1", newID: "file:" + name + ":2"}
	oldKey, newKey := make([]byte, 32), make([]byte, 32)
	_, _ = rand.Read(oldKey)
	_, _ = rand.Read(newKey)
	provider := func(keys map[string][]byte, current string) infrasecret.KEKProvider {
		p, err := infrasecret.NewKEKFileProviderFromKeys(keys, current)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	r.before = infrasecret.NewCipher(provider(map[string][]byte{r.oldID: oldKey}, r.oldID))
	r.during = infrasecret.NewCipher(provider(map[string][]byte{r.oldID: oldKey, r.newID: newKey}, r.newID))
	r.after = provider(map[string][]byte{r.newID: newKey}, r.newID)
	return r
}

// TestRewrapSealedKeys_MovesEveryRowToTheCurrentKey is the KEK rotation
// contract end to end on MySQL: startup refuses a key file missing the key rows
// name; rewrap moves every Space Secret and model credential, across more than
// one batch, to the current key; each stays readable with only that key under
// its own AAD; and a second run finds nothing to do.
func TestRewrapSealedKeys_MovesEveryRowToTheCurrentKey(t *testing.T) {
	s, ctx := newTestStore(t)
	userID, spaceID := secretTestSpace(t, s, "kek-rewrap@example.com")
	rot := newKEKRotation(t)
	s.SetCredentialCipher(rot.before)
	aad := coresecret.AAD(spaceID)

	const secrets = rewrapBatch + 3
	ids := make([]string, 0, secrets)
	for i := range secrets {
		sealed, err := rot.before.Seal(coresecret.Items{"TOKEN": fmt.Sprintf("v%d", i)}, aad)
		if err != nil {
			t.Fatal(err)
		}
		created, err := s.CreateSecret(ctx, coresecret.CreateInput{
			SpaceID: spaceID, Name: fmt.Sprintf("s%03d", i), CreatedBy: userID,
			ItemNames: []string{"TOKEN"}, Sealed: sealed,
		})
		if err != nil {
			t.Fatalf("CreateSecret: %v", err)
		}
		ids = append(ids, created.ID)
	}
	// A destroyed Secret holds no sealed material and depends on no key.
	if _, err := s.SetState(ctx, ids[0], coresecret.StateDestroyed); err != nil {
		t.Fatal(err)
	}
	model, err := s.CreateLLMModel(ctx, sampleModelInput("Rewrap "+testPublicID(t)))
	if err != nil {
		t.Fatalf("CreateLLMModel: %v", err)
	}
	t.Cleanup(func() {
		_ = s.db.Delete(&llmModelRow{}, "public_id = ?", canonicalPublicID(model.ID)).Error
	})
	// The live Secrets, less the destroyed one, plus the credential.
	const sealedRows = secrets - 1 + 1
	stored, err := s.GetLLMModel(ctx, model.ID)
	if err != nil || stored == nil {
		t.Fatalf("GetLLMModel: %v", err)
	}

	refs, err := s.SealedKeyReferences(ctx, infrasecret.SealedValueKeyID)
	if err != nil {
		t.Fatalf("SealedKeyReferences: %v", err)
	}
	if refs[rot.oldID] != sealedRows {
		t.Fatalf("rows under the old key = %d, want %d", refs[rot.oldID], sealedRows)
	}
	// Startup with the old key already removed is refused, naming it.
	err = infrasecret.RequireLoadedKeys(rot.after, refs)
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%q (%d rows)", rot.oldID, sealedRows)) {
		t.Fatalf("startup check with the old key removed: err = %v", err)
	}

	res, err := s.RewrapSealedKeys(ctx, rot.during)
	if err != nil {
		t.Fatalf("RewrapSealedKeys: %v", err)
	}
	if res.Rewrapped[rot.oldID] != sealedRows || res.Skipped != 0 {
		t.Fatalf("rewrap result = %+v, want %d rows moved off %s", res, sealedRows, rot.oldID)
	}

	refs, err = s.SealedKeyReferences(ctx, infrasecret.SealedValueKeyID)
	if err != nil {
		t.Fatal(err)
	}
	if refs[rot.oldID] != 0 || refs[rot.newID] != sealedRows {
		t.Fatalf("after rewrap: old key %d rows, new key %d rows; want 0 and %d",
			refs[rot.oldID], refs[rot.newID], sealedRows)
	}
	if err := infrasecret.RequireLoadedKeys(rot.after, refs); err != nil {
		t.Fatalf("the old key is unreferenced after rewrap, yet removing it is refused: %v", err)
	}

	// Every row opens with the new key alone, under its original AAD.
	onlyNew := infrasecret.NewCipher(rot.after)
	for i, id := range ids[1:] {
		_, sealed, err := s.GetSealed(ctx, id)
		if err != nil || sealed == nil {
			t.Fatalf("GetSealed %s: %v", id, err)
		}
		items, err := onlyNew.Open(*sealed, aad)
		if err != nil {
			t.Fatalf("secret %s does not open with only the new key: %v", id, err)
		}
		if want := fmt.Sprintf("v%d", i+1); items["TOKEN"] != want {
			t.Fatalf("secret %s TOKEN = %q, want %q", id, items["TOKEN"], want)
		}
	}
	s.SetCredentialCipher(onlyNew)
	key, err := s.LLMModelCredential(ctx, model.ID)
	if err != nil || key != catalogSecret {
		t.Fatalf("credential after rewrap = %q, %v", key, err)
	}
	// A rewrap changes no value, so it must not look like a key change to the
	// gateway, which rebuilds a cached client when updated_at moves.
	if got, _ := s.GetLLMModel(ctx, model.ID); got == nil || !got.UpdatedAt.Equal(stored.UpdatedAt) {
		t.Fatalf("rewrap moved the model's updated_at: %v -> %+v", stored.UpdatedAt, got)
	}

	res, err = s.RewrapSealedKeys(ctx, rot.during)
	if err != nil || len(res.Rewrapped) != 0 || res.Skipped != 0 {
		t.Fatalf("second rewrap = %+v, %v; want nothing to do", res, err)
	}
}

// editingRewrapper edits the row it is asked to rewrap before answering, which
// is a Portal edit landing between the rewrap's read and its write.
type editingRewrapper struct {
	*infrasecret.Cipher
	edit func()
}

func (e editingRewrapper) RewrapDEK(wrapped []byte, keyID string) ([]byte, string, error) {
	if e.edit != nil {
		e.edit()
	}
	return e.Cipher.RewrapDEK(wrapped, keyID)
}

// TestRewrapSealedKeys_NeverOverwritesAConcurrentEdit proves the optimistic
// check: an edit between read and write wins, and the rewrap counts the row as
// skipped rather than restoring the old items under a new key.
func TestRewrapSealedKeys_NeverOverwritesAConcurrentEdit(t *testing.T) {
	s, ctx := newTestStore(t)
	userID, spaceID := secretTestSpace(t, s, "kek-rewrap-race@example.com")
	rot := newKEKRotation(t)
	aad := coresecret.AAD(spaceID)

	sealed, err := rot.before.Seal(coresecret.Items{"TOKEN": "old"}, aad)
	if err != nil {
		t.Fatal(err)
	}
	created, err := s.CreateSecret(ctx, coresecret.CreateInput{
		SpaceID: spaceID, Name: "raced", CreatedBy: userID, ItemNames: []string{"TOKEN"}, Sealed: sealed,
	})
	if err != nil {
		t.Fatal(err)
	}

	r := editingRewrapper{Cipher: rot.during, edit: func() {
		edited, err := rot.during.Seal(coresecret.Items{"TOKEN": "edited"}, aad)
		if err != nil {
			t.Error(err)
			return
		}
		if _, err := s.UpdateItems(ctx, coresecret.UpdateItemsInput{
			ID: created.ID, ItemNames: []string{"TOKEN"}, Sealed: edited,
		}); err != nil {
			t.Error(err)
		}
	}}
	res, err := s.RewrapSealedKeys(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 1 || res.Rewrapped[rot.oldID] != 0 {
		t.Fatalf("rewrap result = %+v, want the raced row skipped", res)
	}
	_, got, err := s.GetSealed(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	items, err := infrasecret.NewCipher(rot.after).Open(*got, aad)
	if err != nil || items["TOKEN"] != "edited" {
		t.Fatalf("after a raced rewrap: items = %v, err = %v; want the edit kept", items, err)
	}
}
