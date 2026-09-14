package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/audit"
	"github.com/icloudbb/buildmax/internal/testsupport"
)

type identityFixture struct {
	mux      *http.ServeMux
	users    *mock.MockUserStore
	sessions *mock.MockAuthSessionStore
	eids     *mock.MockExternalIdentityStore
	target   *coreidentity.User
	actorSID string
}

// newIdentityFixture builds an admin handler with an external-identity store, or
// with none when withSSO is false, so both the wired and the 503 paths are
// reachable.
func newIdentityFixture(t *testing.T, withSSO bool) *identityFixture {
	t.Helper()
	users := &mock.MockUserStore{}
	grants := &mock.MockSystemGrantStore{}
	grants.GrantForTest(adminUser, coreidentity.SystemRoleAdmin)
	seedUser(t, users, adminUser, "admin@example.com")
	target := seedUser(t, users, "u_target", "target@example.com")

	f := &identityFixture{
		users:    users,
		sessions: &mock.MockAuthSessionStore{},
	}
	cfg := Config{
		JWTSecret: testSecret,
		Grants:    grants,
		Users:     users,
		Spaces:    &mock.MockSpaceStore{},
		Sessions:  f.sessions,
		Audit:     audit.NewRecorder(&mock.MockAuditStore{}),
	}
	if withSSO {
		f.eids = &mock.MockExternalIdentityStore{Users: users}
		cfg.ExternalIdentities = f.eids
	}
	f.target = target
	h := New(cfg)
	f.mux = http.NewServeMux()
	h.Register(f.mux)
	// A live session for the acting admin, so the guard's session check passes.
	sid, err := f.sessions.CreateSession(t.Context(), coreidentity.NewAuthSession{
		UserID: adminUser, Platform: "portal", AuthMethod: "login_code",
		AbsoluteExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	f.actorSID = sid
	return f
}

func (f *identityFixture) do(t *testing.T, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+testsupport.SignJWTWithSID(adminUser, f.actorSID, testSecret))
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	return rec
}

func TestAdminListsIdentities(t *testing.T) {
	f := newIdentityFixture(t, true)
	if _, err := f.eids.LinkExisting(context.Background(), coreidentity.LinkIdentity{
		UserID: f.target.ID, Issuer: "https://example.okta.com", Subject: "okta|1",
		Seen: coreidentity.SeenClaims{Email: "target@example.com", Name: "Target"},
	}); err != nil {
		t.Fatalf("seed link: %v", err)
	}
	rec := f.do(t, http.MethodGet, "/api/admin/users/u_target/identities")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var resp AdminExternalIdentitiesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Identities) != 1 || resp.Identities[0].Issuer != "https://example.okta.com" || resp.Identities[0].Subject != "okta|1" {
		t.Errorf("identities = %+v", resp.Identities)
	}
}

func TestAdminIdentitiesReports503WithoutSSO(t *testing.T) {
	f := newIdentityFixture(t, false)
	if got := f.do(t, http.MethodGet, "/api/admin/users/u_target/identities").Code; got != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 when SSO is not configured", got)
	}
}

func TestAdminUnlinkRequiresDisabled(t *testing.T) {
	f := newIdentityFixture(t, true)
	link, err := f.eids.LinkExisting(context.Background(), coreidentity.LinkIdentity{
		UserID: f.target.ID, Issuer: "https://example.okta.com", Subject: "okta|1",
	})
	if err != nil {
		t.Fatalf("seed link: %v", err)
	}

	// Enabled: refused with 409.
	if got := f.do(t, http.MethodDelete, "/api/admin/users/u_target/identities/"+link.ID).Code; got != http.StatusConflict {
		t.Fatalf("status = %d, want 409 while the account is enabled", got)
	}

	// Disabled: allowed, 204, and the link is gone.
	f.users.DisableForTest(f.target.ID, time.Now())
	if got := f.do(t, http.MethodDelete, "/api/admin/users/u_target/identities/"+link.ID).Code; got != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 once disabled", got)
	}
	remaining, _ := f.eids.ListUserIdentities(context.Background(), f.target.ID)
	if len(remaining) != 0 {
		t.Errorf("link survived the unlink: %+v", remaining)
	}
}

func TestAdminUnlinkUnknownIsNotFound(t *testing.T) {
	f := newIdentityFixture(t, true)
	f.users.DisableForTest(f.target.ID, time.Now())
	if got := f.do(t, http.MethodDelete, "/api/admin/users/u_target/identities/eid_nope").Code; got != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for an unknown identity", got)
	}
}
