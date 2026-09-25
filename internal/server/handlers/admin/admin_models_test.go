package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/audit"
)

const modelSecret = "sk-this-must-never-be-served"

func adminModelsMux(t *testing.T) (*http.ServeMux, *mock.MockLLMModelStore, *mock.MockAuditStore) {
	t.Helper()
	users := &mock.MockUserStore{}
	seedUser(t, users, adminUser, "admin@example.com")
	grants := &mock.MockSystemGrantStore{}
	grants.GrantForTest(adminUser, coreidentity.SystemRoleAdmin)

	models := &mock.MockLLMModelStore{}
	fast, err := models.CreateLLMModel(t.Context(), coregw.CreateModelInput{
		Name: "Fast", ProviderType: "openai_compatible", APIURL: "https://example.test/v1",
		APIKey: modelSecret, Model: "provider/fast",
	})
	if err != nil {
		t.Fatalf("CreateLLMModel: %v", err)
	}
	if _, err := models.CreateLLMModel(t.Context(), coregw.CreateModelInput{
		Name: "Deep", ProviderType: "openai_compatible", APIURL: "https://example.test/v1",
		APIKey: modelSecret, Model: "provider/deep",
	}); err != nil {
		t.Fatalf("CreateLLMModel: %v", err)
	}

	audits := &mock.MockAuditStore{}
	h := New(Config{
		JWTSecret:  testSecret,
		Grants:     grants,
		Users:      users,
		Spaces:     &mock.MockSpaceStore{},
		Models:     models,
		Audits:     audits,
		Audit:      audit.NewRecorder(audits),
		Deployment: DeploymentInfo{DefaultModel: fast.Name},
	})
	mux := http.NewServeMux()
	h.Register(mux)
	return mux, models, audits
}

// TestAdminModelsNeverCarryACredential is the assertion this route exists
// under. The catalog is the one table in the system holding provider keys.
func TestAdminModelsNeverCarryACredential(t *testing.T) {
	mux, _, _ := adminModelsMux(t)
	rec := adminRequestAs(t, mux, adminCase{"GET", "/api/admin/llm/models"}, adminUser)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, modelSecret) || strings.Contains(strings.ToLower(body), "api_key") {
		t.Errorf("the catalog response carried a credential: %s", body)
	}
}

// TestAdminModelsReportWhichAreReachable: a model no alias points at cannot be
// called by any space however enabled it is, and that is the most common reason
// an operator's model "does not work".
func TestAdminModelsReportWhichAreReachable(t *testing.T) {
	mux, _, _ := adminModelsMux(t)
	rec := adminRequestAs(t, mux, adminCase{"GET", "/api/admin/llm/models"}, adminUser)
	var out AdminModelsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.DefaultModel != "Fast" {
		t.Errorf("default_model = %q, want %q", out.DefaultModel, "Fast")
	}
	if len(out.Models) != 2 {
		t.Fatalf("models = %d, want 2", len(out.Models))
	}
}

func TestAdminModelEnableDisable(t *testing.T) {
	mux, models, audits := adminModelsMux(t)
	id := models.Models[0].ID

	rec := adminRequestJSON(t, mux, "PUT", "/api/admin/llm/models/"+id+"/state", adminUser, `{"enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable got %d: %s", rec.Code, rec.Body.String())
	}
	if models.Models[0].Enabled {
		t.Error("the model is still enabled")
	}
	if strings.Contains(rec.Body.String(), modelSecret) {
		t.Errorf("the toggle response carried a credential: %s", rec.Body.String())
	}

	if got := adminRequestJSON(t, mux, "PUT", "/api/admin/llm/models/"+id+"/state", adminUser, `{"enabled":true}`).Code; got != http.StatusOK {
		t.Fatalf("enable got %d", got)
	}
	if !models.Models[0].Enabled {
		t.Error("the model was not re-enabled")
	}

	// The same actions the operator command writes: the trail does not
	// distinguish a catalog change by where it was made, only by who made it.
	want := []string{coreaudit.ModelDisabled, coreaudit.ModelEnabled}
	if len(audits.Events) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(audits.Events), audits.Events)
	}
	for i, action := range want {
		e := audits.Events[i]
		if e.Action != action || e.ActorType != coreaudit.ActorUser || e.ActorID != adminUser {
			t.Errorf("event %d = %+v, want %s by the administrator", i, e, action)
		}
		if e.Detail != "Fast" {
			t.Errorf("event %d should name the model: %+v", i, e)
		}
	}
}

// TestAdminModelCreate adds a model over HTTP and checks the whole contract:
// created, audited, and the credential in neither the response nor the audit.
func TestAdminModelCreate(t *testing.T) {
	mux, models, audits := adminModelsMux(t)

	const newSecret = "sk-CREATE-must-never-be-served"
	body := `{
		"name": "Created",
		"provider_type": "openai_compatible",
		"api_url": "https://example.test/v1",
		"api_key": "` + newSecret + `",
		"model": "provider/created",
		"context_window": 128000
	}`
	rec := adminPostJSON(t, mux, "/api/admin/llm/models", adminUser, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}

	// The response is the whole surface a browser sees; the key must not be on it.
	out := rec.Body.String()
	if strings.Contains(out, newSecret) || strings.Contains(strings.ToLower(out), "api_key") {
		t.Errorf("the create response carried a credential: %s", out)
	}
	var created AdminModel
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Name != "Created" {
		t.Errorf("created.Name = %q", created.Name)
	}

	// It is really in the catalog now.
	if _, err := models.GetLLMModel(t.Context(), created.ID); err != nil {
		t.Errorf("the created model is not in the catalog: %v", err)
	}

	// Audited as a creation by the administrator, and the record names the model,
	// not the key.
	if len(audits.Events) != 1 {
		t.Fatalf("got %d audit events, want 1: %+v", len(audits.Events), audits.Events)
	}
	e := audits.Events[0]
	if e.Action != coreaudit.ModelCreated || e.ActorType != coreaudit.ActorUser || e.ActorID != adminUser {
		t.Errorf("event = %+v, want %s by the administrator", e, coreaudit.ModelCreated)
	}
	if strings.Contains(e.Detail, newSecret) {
		t.Errorf("the audit detail carried the credential: %+v", e)
	}
}

func TestAdminModelCreateRejectsInvalidAndDuplicate(t *testing.T) {
	mux, _, _ := adminModelsMux(t)

	// Missing api_url: the catalog refuses it, and the edge answers 400.
	missing := `{"name":"Bad","provider_type":"openai_compatible","api_key":"sk-x","model":"m"}`
	if got := adminPostJSON(t, mux, "/api/admin/llm/models", adminUser, missing).Code; got != http.StatusBadRequest {
		t.Errorf("missing api_url got %d, want 400", got)
	}

	// A name the seed already holds: 409, not a second row.
	dup := `{"name":"Fast","provider_type":"openai_compatible","api_url":"https://example.test/v1","api_key":"sk-x","model":"m"}`
	if got := adminPostJSON(t, mux, "/api/admin/llm/models", adminUser, dup).Code; got != http.StatusConflict {
		t.Errorf("duplicate name got %d, want 409", got)
	}
}

func TestAdminModelToggleOnAnUnknownModel(t *testing.T) {
	mux, _, _ := adminModelsMux(t)
	if got := adminRequestAs(t, mux, adminCase{"PUT", "/api/admin/llm/models/lm_nobody/state"}, adminUser).Code; got != http.StatusNotFound {
		t.Errorf("got %d, want 404", got)
	}
}

// TestAdminModelsWithoutACatalogIs503 rather than an empty list, which would
// read as "this deployment has no models" on a deployment with no database.
func TestAdminModelsWithoutACatalogIs503(t *testing.T) {
	users := &mock.MockUserStore{}
	seedUser(t, users, adminUser, "admin@example.com")
	grants := &mock.MockSystemGrantStore{}
	grants.GrantForTest(adminUser, coreidentity.SystemRoleAdmin)
	audits := &mock.MockAuditStore{}
	h := New(Config{
		JWTSecret: testSecret,
		Grants:    grants,
		Users:     users,
		Spaces:    &mock.MockSpaceStore{},
		Audits:    audits,
		Audit:     audit.NewRecorder(audits),
	})
	mux := http.NewServeMux()
	h.Register(mux)

	if got := adminRequestAs(t, mux, adminCase{"GET", "/api/admin/llm/models"}, adminUser).Code; got != http.StatusServiceUnavailable {
		t.Errorf("got %d, want 503", got)
	}
}

// A leaked or expired key is rotated in place: same model, same name, a new
// key, audited without the key, and never echoed back.
func TestAdminModelCredentialReplace(t *testing.T) {
	mux, models, audits := adminModelsMux(t)
	id := models.Models[0].ID
	const rotated = "sk-rotated-and-still-secret"

	rec := adminRequestJSON(t, mux, "PUT", "/api/admin/llm/models/"+id+"/credential", adminUser, `{"api_key":"`+rotated+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("replace got %d: %s", rec.Code, rec.Body.String())
	}
	if models.Credentials[id] != rotated {
		t.Errorf("stored credential = %q, want the rotated key", models.Credentials[id])
	}
	if models.Models[0].Name != "Fast" {
		t.Errorf("the model was renamed to %q", models.Models[0].Name)
	}
	if strings.Contains(rec.Body.String(), rotated) {
		t.Errorf("the response carried the new key: %s", rec.Body.String())
	}
	if len(audits.Events) != 1 || audits.Events[0].Action != coreaudit.ModelCredentialReplaced ||
		audits.Events[0].Detail != "Fast" || strings.Contains(audits.Events[0].Detail, rotated) {
		t.Errorf("audit = %+v, want one credential_replaced event naming the model", audits.Events)
	}

	if got := adminRequestJSON(t, mux, "PUT", "/api/admin/llm/models/"+id+"/credential", adminUser, `{"api_key":"  "}`).Code; got != http.StatusBadRequest {
		t.Errorf("an empty key for a provider that needs one got %d, want 400", got)
	}
	if got := adminRequestJSON(t, mux, "PUT", "/api/admin/llm/models/nosuchmodel/credential", adminUser, `{"api_key":"k"}`).Code; got != http.StatusNotFound {
		t.Errorf("an unknown model got %d, want 404", got)
	}
}
