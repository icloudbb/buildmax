package space

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coresecret "github.com/icloudbb/buildmax/internal/core/secret"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	infrasecret "github.com/icloudbb/buildmax/internal/infra/secret"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/audit"
	secretsvc "github.com/icloudbb/buildmax/internal/service/secret"
	"github.com/icloudbb/buildmax/internal/testsupport"
)

const secretTestSecret = "secret-test-secret"

// memSecretStore is a minimal in-memory coresecret.Store for handler tests.
type memSecretStore struct {
	rows map[string]*coresecret.Secret
	seal map[string]coresecret.Sealed
	n    int
}

func newMemSecretStore() *memSecretStore {
	return &memSecretStore{rows: map[string]*coresecret.Secret{}, seal: map[string]coresecret.Sealed{}}
}

func (m *memSecretStore) CreateSecret(_ context.Context, in coresecret.CreateInput) (*coresecret.Secret, error) {
	m.n++
	id := "sec_" + string(rune('a'+m.n))
	s := &coresecret.Secret{ID: id, SpaceID: in.SpaceID, Name: in.Name, Description: in.Description,
		Provider: in.Provider, State: coresecret.StateActive, ItemNames: in.ItemNames, CreatedBy: in.CreatedBy}
	m.rows[id] = s
	m.seal[id] = in.Sealed
	cp := *s
	return &cp, nil
}
func (m *memSecretStore) GetSecret(_ context.Context, id string) (*coresecret.Secret, error) {
	s, ok := m.rows[id]
	if !ok {
		return nil, nil
	}
	cp := *s
	return &cp, nil
}
func (m *memSecretStore) ListSecretsBySpace(_ context.Context, spaceID string) ([]coresecret.Secret, error) {
	var out []coresecret.Secret
	for _, s := range m.rows {
		if s.SpaceID == spaceID {
			out = append(out, *s)
		}
	}
	return out, nil
}
func (m *memSecretStore) GetSealed(_ context.Context, id string) (*coresecret.Secret, *coresecret.Sealed, error) {
	s, ok := m.rows[id]
	if !ok {
		return nil, nil, nil
	}
	if s.State == coresecret.StateDestroyed {
		return nil, nil, apierrNotFound()
	}
	cp := *s
	sl := m.seal[id]
	return &cp, &sl, nil
}
func (m *memSecretStore) UpdateItems(_ context.Context, in coresecret.UpdateItemsInput) (*coresecret.Secret, error) {
	s, ok := m.rows[in.ID]
	if !ok {
		return nil, apierrNotFound()
	}
	s.ItemNames = in.ItemNames
	m.seal[in.ID] = in.Sealed
	cp := *s
	return &cp, nil
}
func (m *memSecretStore) SetState(_ context.Context, id string, state coresecret.State) (*coresecret.Secret, error) {
	s, ok := m.rows[id]
	if !ok {
		return nil, apierrNotFound()
	}
	s.State = state
	cp := *s
	return &cp, nil
}

func apierrNotFound() error { return apierr.ErrNotFound }

func newSecretHandler(t *testing.T, role string) (*Handler, string) {
	t.Helper()
	spaceID := "tm_1"
	spaceStore := &mock.MockSpaceStore{
		Spaces:  []corespace.Space{{ID: spaceID, Name: "Space", CreatedBy: "u1"}},
		Members: []corespace.Member{{SpaceID: spaceID, UserID: "u1", Role: role}},
	}
	key := make([]byte, 32)
	_, _ = io.ReadFull(rand.Reader, key)
	kek, err := infrasecret.NewKEKFileProviderFromKeys(map[string][]byte{"file:root:1": key}, "file:root:1")
	if err != nil {
		t.Fatal(err)
	}
	svc := &secretsvc.Service{Store: newMemSecretStore(), Sealer: infrasecret.NewCipher(kek)}
	h := New(Config{JWTSecret: secretTestSecret, Spaces: spaceStore, SecretService: svc})
	return h, spaceID
}

func doJSON(t *testing.T, mux *http.ServeMux, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Authorization", token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestSecretHandlers_OwnerLifecycle(t *testing.T) {
	h, spaceID := newSecretHandler(t, corespace.RoleOwner)
	mux := http.NewServeMux()
	h.Register(mux)
	token := "Bearer " + testsupport.SignJWT("u1", secretTestSecret)
	base := "/api/spaces/" + spaceID + "/secrets"

	// Create.
	rec := doJSON(t, mux, http.MethodPost, base, token,
		`{"name":"aws","items":{"access_key_id":"AKIA","secret_access_key":"wJa"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", rec.Code, rec.Body.String())
	}
	var created secretResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if len(created.ItemNames) != 2 || created.State != string(coresecret.StateActive) {
		t.Fatalf("created = %+v", created)
	}
	// The response must never carry a value.
	if strings.Contains(rec.Body.String(), "AKIA") || strings.Contains(rec.Body.String(), "wJa") {
		t.Fatal("create response leaked an item value")
	}

	// List.
	rec = doJSON(t, mux, http.MethodGet, base, token, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), created.ID) {
		t.Fatalf("list status = %d: %s", rec.Code, rec.Body.String())
	}

	// Patch: add region, remove secret_access_key.
	rec = doJSON(t, mux, http.MethodPatch, base+"/"+created.ID, token,
		`{"set":{"region":"us-east-1"},"remove":["secret_access_key"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d: %s", rec.Code, rec.Body.String())
	}
	var patched secretResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &patched)
	if len(patched.ItemNames) != 2 || patched.ItemNames[0] != "access_key_id" || patched.ItemNames[1] != "region" {
		t.Fatalf("patched item names = %v", patched.ItemNames)
	}

	// Sending both replace and patch is a bad request.
	rec = doJSON(t, mux, http.MethodPatch, base+"/"+created.ID, token,
		`{"items":{"a":"b"},"set":{"c":"d"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("mixed edit status = %d, want 400", rec.Code)
	}

	// Disable via state.
	rec = doJSON(t, mux, http.MethodPut, base+"/"+created.ID+"/state", token, `{"state":"disabled"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable status = %d: %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, mux, http.MethodGet, base+"/"+created.ID, token, "")
	var got secretResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.State != string(coresecret.StateDisabled) {
		t.Fatalf("state after disable = %q", got.State)
	}
}

func TestSecretHandlers_MemberForbidden(t *testing.T) {
	h, spaceID := newSecretHandler(t, corespace.RoleMember)
	mux := http.NewServeMux()
	h.Register(mux)
	token := "Bearer " + testsupport.SignJWT("u1", secretTestSecret)
	rec := doJSON(t, mux, http.MethodGet, "/api/spaces/"+spaceID+"/secrets", token, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member list status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
}

func TestSecretHandlers_FeatureOff(t *testing.T) {
	spaceID := "tm_1"
	spaceStore := &mock.MockSpaceStore{
		Spaces:  []corespace.Space{{ID: spaceID, Name: "Space", CreatedBy: "u1"}},
		Members: []corespace.Member{{SpaceID: spaceID, UserID: "u1", Role: corespace.RoleOwner}},
	}
	h := New(Config{JWTSecret: secretTestSecret, Spaces: spaceStore}) // no SecretService
	mux := http.NewServeMux()
	h.Register(mux)
	token := "Bearer " + testsupport.SignJWT("u1", secretTestSecret)
	rec := doJSON(t, mux, http.MethodGet, "/api/spaces/"+spaceID+"/secrets", token, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("feature-off status = %d, want 503", rec.Code)
	}
}

func (m *memSecretStore) RecordEnvGrant(_ context.Context, _ coresecret.GrantRecord) error {
	return nil
}

// A Secret is a credential an agent granted it can read, so its create/disable/
// destroy lifecycle belongs in the space audit trail — with the secret id as the
// target and no item name, value, or ciphertext anywhere in the event.
func TestSecretLifecycleIsAudited(t *testing.T) {
	spaceID := "tm_1"
	spaceStore := &mock.MockSpaceStore{
		Spaces:  []corespace.Space{{ID: spaceID, Name: "Space", CreatedBy: "u1"}},
		Members: []corespace.Member{{SpaceID: spaceID, UserID: "u1", Role: corespace.RoleOwner}},
	}
	key := make([]byte, 32)
	_, _ = io.ReadFull(rand.Reader, key)
	kek, err := infrasecret.NewKEKFileProviderFromKeys(map[string][]byte{"file:root:1": key}, "file:root:1")
	if err != nil {
		t.Fatal(err)
	}
	svc := &secretsvc.Service{Store: newMemSecretStore(), Sealer: infrasecret.NewCipher(kek)}
	store := &mock.MockAuditStore{}
	h := New(Config{
		JWTSecret:     secretTestSecret,
		Spaces:        spaceStore,
		SecretService: svc,
		Audits:        store,
		Audit:         audit.NewRecorder(store),
	})
	mux := http.NewServeMux()
	h.Register(mux)
	token := "Bearer " + testsupport.SignJWT("u1", secretTestSecret)
	base := "/api/spaces/" + spaceID + "/secrets"

	rec := doJSON(t, mux, http.MethodPost, base, token,
		`{"name":"aws","items":{"access_key_id":"AKIA","secret_access_key":"wJa"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", rec.Code, rec.Body.String())
	}
	var created secretResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	ev := firstEvent(t, store, coreaudit.SecretCreated)
	if ev.ActorID != "u1" || ev.SpaceID != spaceID || ev.TargetType != "secret" || ev.TargetID != created.ID {
		t.Errorf("secret.created event = %+v", ev)
	}
	if ev.Detail != "" {
		t.Errorf("secret.created detail = %q, want empty", ev.Detail)
	}

	rec = doJSON(t, mux, http.MethodPut, base+"/"+created.ID+"/state", token, `{"state":"disabled"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable status = %d: %s", rec.Code, rec.Body.String())
	}
	if ev := firstEvent(t, store, coreaudit.SecretDisabled); ev.TargetID != created.ID {
		t.Errorf("secret.disabled event = %+v", ev)
	}

	rec = doJSON(t, mux, http.MethodPut, base+"/"+created.ID+"/state", token, `{"state":"destroyed"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("destroy status = %d: %s", rec.Code, rec.Body.String())
	}
	if ev := firstEvent(t, store, coreaudit.SecretDestroyed); ev.TargetID != created.ID {
		t.Errorf("secret.destroyed event = %+v", ev)
	}

	// No item name, value, or ciphertext may appear in any recorded event.
	for _, e := range store.Events {
		for _, leak := range []string{"access_key_id", "secret_access_key", "AKIA", "wJa"} {
			if strings.Contains(e.Detail, leak) || strings.Contains(e.TargetID, leak) {
				t.Errorf("event %s leaked %q: %+v", e.Action, leak, e)
			}
		}
	}
}
