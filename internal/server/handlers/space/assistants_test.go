package space

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/mock"
	assistantsvc "github.com/icloudbb/buildmax/internal/service/assistant"
	"github.com/icloudbb/buildmax/internal/service/audit"
	spacesvc "github.com/icloudbb/buildmax/internal/service/space"
	"github.com/icloudbb/buildmax/internal/testsupport"
)

// TestPublishingAnAssistantOverHTTP: define, be refused publishing with the
// statement to confirm, publish with its digest, and see each step audited.
func TestPublishingAnAssistantOverHTTP(t *testing.T) {
	users := &mock.MockUserStore{ByID: map[string]*coreidentity.User{
		matrixOwner: {ID: matrixOwner, Email: "owner@example.com", Kind: coreidentity.KindHuman},
	}}
	spaces := &mock.MockSpaceStore{
		Spaces:  []corespace.Space{{ID: matrixSpace, Name: "HR", CreatedBy: matrixOwner}},
		Members: []corespace.Member{{SpaceID: matrixSpace, UserID: matrixOwner, Role: corespace.RoleOwner}},
	}
	auditStore := &mock.MockAuditStore{}
	recorder := audit.NewRecorder(auditStore)
	h := New(Config{
		JWTSecret: matrixSecret, Spaces: spaces, Users: users, Audits: auditStore, Audit: recorder,
		Assistants: &assistantsvc.Service{
			Store: &mock.MockAssistantStore{}, Spaces: spaces, Users: users, Audit: recorder,
			ServiceAccounts: &spacesvc.Service{Spaces: spaces, Users: users, ServiceAccounts: &mock.MockServiceAccountStore{Users: users, Spaces: spaces}},
		},
	})
	mux := http.NewServeMux()
	h.Register(mux)
	owner := "Bearer " + testsupport.SignJWT(matrixOwner, matrixSecret)
	base := "/api/spaces/" + matrixSpace + "/assistants"

	rec := doJSON(t, mux, http.MethodPost, base, owner, `{"name":"HR Assistant","audience":"all_users"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", rec.Code, rec.Body.String())
	}
	var created assistantResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.State != "paused" || created.ServiceAccountID == "" || created.Statement.Digest == "" {
		t.Fatalf("created = %+v", created)
	}

	rec = doJSON(t, mux, http.MethodPut, base+"/"+created.ID+"/state", owner, `{"state":"active"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("publish without confirming = %d: %s", rec.Code, rec.Body.String())
	}
	var refused statementRequiredResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &refused)
	if refused.Statement.Digest != created.Statement.Digest || !strings.Contains(refused.Statement.Text, "Any active user") {
		t.Fatalf("409 body = %s", rec.Body.String())
	}

	rec = doJSON(t, mux, http.MethodPut, base+"/"+created.ID+"/state", owner, `{"state":"active","confirm_statement":"`+refused.Statement.Digest+`"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"availability":"available"`) {
		t.Fatalf("publish confirmed = %d: %s", rec.Code, rec.Body.String())
	}

	var actions []string
	for _, e := range auditStore.Events {
		actions = append(actions, e.Action)
	}
	for _, want := range []string{coreaudit.ServiceAccountCreated, coreaudit.AssistantCreated, coreaudit.AssistantActivated} {
		if !strings.Contains(strings.Join(actions, ","), want) {
			t.Errorf("audit lacks %s: %v", want, actions)
		}
	}
}
