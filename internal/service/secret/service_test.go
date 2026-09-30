package secret

import (
	"context"
	"crypto/rand"
	"io"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coresecret "github.com/icloudbb/buildmax/internal/core/secret"
	infrasecret "github.com/icloudbb/buildmax/internal/infra/secret"
)

// memStore is an in-memory coresecret.Store keyed by a generated id.
type memStore struct {
	byID map[string]*storedSecret
	seq  int
}

type storedSecret struct {
	meta   coresecret.Secret
	sealed coresecret.Sealed
}

func newMemStore() *memStore { return &memStore{byID: map[string]*storedSecret{}} }

func (m *memStore) CreateSecret(_ context.Context, in coresecret.CreateInput) (*coresecret.Secret, error) {
	for _, s := range m.byID {
		if s.meta.SpaceID == in.SpaceID && s.meta.Name == in.Name {
			return nil, coresecret.ErrNameTaken
		}
	}
	m.seq++
	id := "sec_" + string(rune('a'+m.seq))
	meta := coresecret.Secret{
		ID: id, SpaceID: in.SpaceID, Name: in.Name, Description: in.Description,
		Provider: in.Provider, State: coresecret.StateActive, ItemNames: in.ItemNames, CreatedBy: in.CreatedBy,
	}
	m.byID[id] = &storedSecret{meta: meta, sealed: in.Sealed}
	cp := meta
	return &cp, nil
}

func (m *memStore) GetSecret(_ context.Context, id string) (*coresecret.Secret, error) {
	s, ok := m.byID[id]
	if !ok {
		return nil, nil
	}
	cp := s.meta
	return &cp, nil
}

func (m *memStore) ListSecretsBySpace(_ context.Context, spaceID string) ([]coresecret.Secret, error) {
	var out []coresecret.Secret
	for _, s := range m.byID {
		if s.meta.SpaceID == spaceID {
			out = append(out, s.meta)
		}
	}
	return out, nil
}

func (m *memStore) GetSealed(_ context.Context, id string) (*coresecret.Secret, *coresecret.Sealed, error) {
	s, ok := m.byID[id]
	if !ok {
		return nil, nil, nil
	}
	if s.meta.State == coresecret.StateDestroyed {
		return nil, nil, coresecretNotFound()
	}
	meta, sealed := s.meta, s.sealed
	return &meta, &sealed, nil
}

func (m *memStore) UpdateItems(_ context.Context, in coresecret.UpdateItemsInput) (*coresecret.Secret, error) {
	s, ok := m.byID[in.ID]
	if !ok {
		return nil, coresecretNotFound()
	}
	s.meta.ItemNames = in.ItemNames
	s.sealed = in.Sealed
	cp := s.meta
	return &cp, nil
}

func (m *memStore) SetState(_ context.Context, id string, state coresecret.State) (*coresecret.Secret, error) {
	s, ok := m.byID[id]
	if !ok {
		return nil, coresecretNotFound()
	}
	s.meta.State = state
	if state == coresecret.StateDestroyed {
		s.sealed = coresecret.Sealed{}
	}
	cp := s.meta
	return &cp, nil
}

func coresecretNotFound() error { return apierr.ErrNotFound }

func testService(t *testing.T) *Service {
	t.Helper()
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		t.Fatal(err)
	}
	kek, err := infrasecret.NewKEKFileProviderFromKeys(map[string][]byte{"file:root:1": key}, "file:root:1")
	if err != nil {
		t.Fatal(err)
	}
	return &Service{Store: newMemStore(), Sealer: infrasecret.NewCipher(kek)}
}

func TestService_CreateAndEdit(t *testing.T) {
	ctx := context.Background()
	svc := testService(t)

	created, err := svc.Create(ctx, CreateCmd{
		SpaceID: "tm_1", CreatedBy: "u_1", Name: "aws",
		Items: map[string]string{"access_key_id": "AKIA", "secret_access_key": "wJa"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(created.ItemNames) != 2 {
		t.Fatalf("item names = %v", created.ItemNames)
	}

	// Patch: add region, remove secret_access_key. The value round-trips
	// through decrypt/re-seal.
	patched, err := svc.PatchItems(ctx, "tm_1", created.ID,
		map[string]string{"region": "us-east-1"}, []string{"secret_access_key"})
	if err != nil {
		t.Fatalf("PatchItems: %v", err)
	}
	if got := patched.ItemNames; len(got) != 2 || got[0] != "access_key_id" || got[1] != "region" {
		t.Fatalf("after patch, item names = %v", got)
	}

	// Confirm the merged plaintext by reading the sealed bytes back.
	_, sealed, _ := svc.Store.GetSealed(ctx, created.ID)
	items, err := svc.Sealer.Open(*sealed, coresecret.AAD("tm_1"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if items["region"] != "us-east-1" || items["access_key_id"] != "AKIA" {
		t.Fatalf("merged items = %v", items)
	}
	if _, ok := items["secret_access_key"]; ok {
		t.Fatal("removed item is still present")
	}
}

func TestService_Validation(t *testing.T) {
	ctx := context.Background()
	svc := testService(t)

	if _, err := svc.Create(ctx, CreateCmd{SpaceID: "t", CreatedBy: "u", Name: "", Items: map[string]string{"k": "v"}}); err != ErrNameRequired {
		t.Fatalf("empty name err = %v", err)
	}
	if _, err := svc.Create(ctx, CreateCmd{SpaceID: "t", CreatedBy: "u", Name: "n", Items: nil}); err != ErrNoItems {
		t.Fatalf("no items err = %v", err)
	}
	if _, err := svc.Create(ctx, CreateCmd{SpaceID: "t", CreatedBy: "u", Name: "n", Items: map[string]string{"bad-name": "v"}}); err != ErrInvalidItem {
		t.Fatalf("invalid item err = %v", err)
	}
}

// A name a Space already uses is the caller's conflict to resolve, not a server
// fault: the store's unique index used to surface as a 500.
func TestService_CreateDuplicateNameIsConflict(t *testing.T) {
	ctx := context.Background()
	svc := testService(t)
	cmd := CreateCmd{SpaceID: "t", CreatedBy: "u", Name: "dup", Items: map[string]string{"k": "v"}}

	if _, err := svc.Create(ctx, cmd); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := svc.Create(ctx, cmd); err != ErrNameTaken {
		t.Fatalf("duplicate name err = %v, want ErrNameTaken", err)
	}
	if kind, _ := apierr.KindOf(ErrNameTaken); kind != apierr.KindConflict {
		t.Fatalf("ErrNameTaken kind = %v, want conflict", kind)
	}
	other := cmd
	other.SpaceID = "t2"
	if _, err := svc.Create(ctx, other); err != nil {
		t.Fatalf("same name in another space: %v", err)
	}
}

func TestService_SpaceScopeAndState(t *testing.T) {
	ctx := context.Background()
	svc := testService(t)
	created, err := svc.Create(ctx, CreateCmd{SpaceID: "tm_1", CreatedBy: "u", Name: "n", Items: map[string]string{"k": "v"}})
	if err != nil {
		t.Fatal(err)
	}

	// Another space cannot see it.
	if _, err := svc.Get(ctx, "tm_2", created.ID); err != ErrNotFound {
		t.Fatalf("cross-space get err = %v", err)
	}

	// Destroy, then edits are refused and the material is gone.
	if _, err := svc.SetState(ctx, "tm_1", created.ID, coresecret.StateDestroyed); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if _, err := svc.ReplaceItems(ctx, "tm_1", created.ID, map[string]string{"k": "v2"}); err == nil {
		t.Fatal("editing a destroyed secret should fail")
	}
}

// Destruction is terminal: a destroyed Secret has no sealed material, so
// reviving it to active/disabled must be refused rather than leave one that
// reads usable but can never materialize.
func TestService_DestroyedSecretCannotBeReactivated(t *testing.T) {
	ctx := context.Background()
	svc := testService(t)
	created, err := svc.Create(ctx, CreateCmd{SpaceID: "tm_1", CreatedBy: "u", Name: "term", Items: map[string]string{"k": "v"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetState(ctx, "tm_1", created.ID, coresecret.StateDestroyed); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	for _, to := range []coresecret.State{coresecret.StateActive, coresecret.StateDisabled} {
		if _, err := svc.SetState(ctx, "tm_1", created.ID, to); err != ErrStateTransition {
			t.Errorf("reactivate destroyed -> %s: err = %v, want ErrStateTransition", to, err)
		}
	}
	// It is still readable as destroyed, and re-destroying is a harmless no-op.
	got, err := svc.Get(ctx, "tm_1", created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.State != coresecret.StateDestroyed {
		t.Errorf("state = %q after refused reactivations, want destroyed", got.State)
	}
	if _, err := svc.SetState(ctx, "tm_1", created.ID, coresecret.StateDestroyed); err != nil {
		t.Errorf("re-destroy (no-op) err = %v, want nil", err)
	}
}

// active and disabled toggle freely; both may still be destroyed.
func TestService_ActiveDisabledToggleAndDestroy(t *testing.T) {
	ctx := context.Background()
	svc := testService(t)
	created, err := svc.Create(ctx, CreateCmd{SpaceID: "tm_1", CreatedBy: "u", Name: "toggle", Items: map[string]string{"k": "v"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, to := range []coresecret.State{coresecret.StateDisabled, coresecret.StateActive, coresecret.StateDisabled, coresecret.StateDestroyed} {
		if _, err := svc.SetState(ctx, "tm_1", created.ID, to); err != nil {
			t.Fatalf("-> %s: %v", to, err)
		}
	}
}

// Name and description are bounded to their columns, and whitespace-only names
// and over-long fields are a clean 400, not a write error.
func TestService_CreateFieldBounds(t *testing.T) {
	ctx := context.Background()
	svc := testService(t)
	cases := []struct {
		name string
		cmd  CreateCmd
		want error
	}{
		{"whitespace name", CreateCmd{SpaceID: "tm_1", CreatedBy: "u", Name: "   ", Items: map[string]string{"k": "v"}}, ErrNameRequired},
		{"name too long", CreateCmd{SpaceID: "tm_1", CreatedBy: "u", Name: strings.Repeat("x", maxSecretNameRunes+1), Items: map[string]string{"k": "v"}}, ErrNameTooLong},
		{"description too long", CreateCmd{SpaceID: "tm_1", CreatedBy: "u", Name: "ok", Description: strings.Repeat("d", maxSecretDescriptionRunes+1), Items: map[string]string{"k": "v"}}, ErrDescriptionTooLong},
	}
	for _, tc := range cases {
		if _, err := svc.Create(ctx, tc.cmd); err != tc.want {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	// A name at exactly the cap is accepted.
	if _, err := svc.Create(ctx, CreateCmd{SpaceID: "tm_1", CreatedBy: "u", Name: strings.Repeat("y", maxSecretNameRunes), Items: map[string]string{"k": "v"}}); err != nil {
		t.Errorf("name at the cap should be accepted: %v", err)
	}
}

func TestService_PatchRemoveMissing(t *testing.T) {
	ctx := context.Background()
	svc := testService(t)
	created, _ := svc.Create(ctx, CreateCmd{SpaceID: "t", CreatedBy: "u", Name: "n", Items: map[string]string{"k": "v"}})
	if _, err := svc.PatchItems(ctx, "t", created.ID, nil, []string{"absent"}); err != ErrItemNotFound {
		t.Fatalf("remove-missing err = %v", err)
	}
	// Removing the only item leaves it empty, which is refused.
	if _, err := svc.PatchItems(ctx, "t", created.ID, nil, []string{"k"}); err != ErrNoItems {
		t.Fatalf("remove-all err = %v", err)
	}
}

func (m *memStore) RecordEnvGrant(_ context.Context, _ coresecret.GrantRecord) error { return nil }
