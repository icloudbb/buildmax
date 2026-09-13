package space

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/audit"
	"github.com/icloudbb/buildmax/internal/testsupport"
)

// governanceFixture wires the stores the second-slice governance actions touch:
// space creation, plus the trail it writes into.
func governanceFixture(t *testing.T) (*http.ServeMux, *mock.MockAuditStore) {
	t.Helper()
	store := &mock.MockAuditStore{}
	h := New(Config{
		JWTSecret:        matrixSecret,
		DefaultQuotaTier: "team",
		Spaces:           &mock.MockSpaceStore{},
		Users:            &mock.MockUserStore{},
		Agents:           &mock.MockAgentStore{},
		Audits:           store,
		Audit:            audit.NewRecorder(store),
	})
	mux := http.NewServeMux()
	h.Register(mux)
	return mux, store
}

func firstEvent(t *testing.T, store *mock.MockAuditStore, action string) coreaudit.Event {
	t.Helper()
	for _, e := range store.Events {
		if e.Action == action {
			return e
		}
	}
	t.Fatalf("no %s recorded; got %+v", action, store.Events)
	return coreaudit.Event{}
}

// A space is an authorization boundary, and the tier it runs under is decided at
// creation and nowhere else, so the creation is recorded with that tier.
func TestSpaceCreationIsAudited(t *testing.T) {
	mux, store := governanceFixture(t)

	req := httptest.NewRequest(http.MethodPost, "/api/spaces", strings.NewReader(`{"name":"Payments"}`))
	req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT(matrixOwner, matrixSecret))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	ev := firstEvent(t, store, coreaudit.SpaceCreated)
	if ev.ActorID != matrixOwner || ev.SpaceID != created.ID || ev.TargetID != created.ID {
		t.Errorf("space.created event = %+v", ev)
	}
	if ev.Detail != "team" {
		t.Errorf("space.created detail = %q, want the quota tier", ev.Detail)
	}
}
