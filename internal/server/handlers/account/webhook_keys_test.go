package account

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

const (
	acctSecret = "account-secret"
	acctMember = "u_member"
)

func webhookFixture(t *testing.T) (*http.ServeMux, *mock.MockAuditStore) {
	t.Helper()
	store := &mock.MockAuditStore{}
	h := New(Config{
		JWTSecret:   acctSecret,
		Users:       &mock.MockUserStore{},
		WebhookKeys: &mock.MockUserWebhookKeyStore{},
		Audit:       audit.NewRecorder(store),
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

// A webhook key admits work under its owner's identity. Its life is worth the
// trail; the key material is not, and it is account-scoped so it carries no
// space.
func TestWebhookKeyLifecycleIsAudited(t *testing.T) {
	mux, store := webhookFixture(t)
	auth := "Bearer " + testsupport.SignJWT(acctMember, acctSecret)

	req := httptest.NewRequest(http.MethodPost, "/api/webhook-keys", strings.NewReader(`{"name":"ci"}`))
	req.Header.Set("Authorization", auth)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var made struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &made); err != nil {
		t.Fatalf("decode: %v", err)
	}
	ev := firstEvent(t, store, coreaudit.WebhookKeyCreated)
	if ev.ActorID != acctMember || ev.SpaceID != "" || ev.TargetID != made.ID {
		t.Errorf("webhook_key.created event = %+v", ev)
	}

	del := httptest.NewRequest(http.MethodDelete, "/api/webhook-keys/"+made.ID, nil)
	del.Header.Set("Authorization", auth)
	delRec := httptest.NewRecorder()
	mux.ServeHTTP(delRec, del)
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204: %s", delRec.Code, delRec.Body.String())
	}
	rev := firstEvent(t, store, coreaudit.WebhookKeyRevoked)
	if rev.ActorID != acctMember || rev.SpaceID != "" || rev.TargetID != made.ID {
		t.Errorf("webhook_key.revoked event = %+v", rev)
	}
}
