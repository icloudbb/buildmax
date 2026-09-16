package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/accountlifecycle"
	"github.com/icloudbb/buildmax/internal/service/audit"
	"github.com/icloudbb/buildmax/internal/testsupport"
)

func (f *disableFixture) do(t *testing.T, method, path, userID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if userID != "" {
		// The guard checks the session store this fixture wires, so the actor's
		// token must name a live session, exactly as a real login's would.
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWTWithSID(userID, f.actorSession(t, userID), testSecret))
	}
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	return rec
}

// actorSession returns a live session id for the actor, creating one on first
// use, so a forged token authenticates the way a logged-in caller's would.
func (f *disableFixture) actorSession(t *testing.T, userID string) string {
	t.Helper()
	if f.actorSIDs == nil {
		f.actorSIDs = map[string]string{}
	}
	if sid, ok := f.actorSIDs[userID]; ok {
		return sid
	}
	sid, err := f.sessions.CreateSession(t.Context(), coreidentity.NewAuthSession{
		UserID: userID, Platform: "portal", AuthMethod: "login_code",
		AbsoluteExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	f.actorSIDs[userID] = sid
	return sid
}

func (f *disableFixture) actions() []string {
	out := make([]string, 0, len(f.audits.Events))
	for _, e := range f.audits.Events {
		out = append(out, e.Action)
	}
	return out
}

// TestDisableStopsTheAccessTokenOnTheNextRequest is the claim that makes
// disablement mean anything.
//
// The access token is a signed JWT the server never stores, so it cannot be
// retired — waiting it out would make "disable" mean "in about a week". The
// check lives where the identity is resolved, and this test is what says the
// token stops working now rather than at expiry.
func TestDisableRevokesSessionsAndRefusesRefresh(t *testing.T) {
	f := newDisableFixture(t)
	sid := f.seedSession(t, "portal")
	plaintext, _, err := f.refresh.CreateRefreshToken(t.Context(), coreidentity.NewRefreshToken{
		UserID: f.target.ID, SessionID: sid, Platform: "portal", TTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("CreateRefreshToken: %v", err)
	}

	rec := f.do(t, "PUT", "/api/admin/users/"+f.target.ID+"/state", adminUser, `{"disabled":true}`)
	var body struct {
		SessionsRevoked int64 `json:"sessions_revoked"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	if body.SessionsRevoked != 1 {
		t.Errorf("sessions_revoked = %d, want 1", body.SessionsRevoked)
	}

	refresh := f.do(t, "POST", "/api/auth/token/refresh", "", `{"refresh_token":"`+plaintext+`"}`)
	if refresh.Code == http.StatusOK {
		t.Errorf("a disabled account refreshed into a new access token: %s", refresh.Body.String())
	}
}

// TestDeactivationImpactReportsCountsOnly: an operator can preview what a
// disable would stop, and the projection carries counts and ids, never content.
func TestDeactivationImpactReportsCountsOnly(t *testing.T) {
	f := newDisableFixture(t)
	f.seedSession(t, "portal")
	if _, _, err := f.keys.CreateKey(t.Context(), f.target.ID, "ci"); err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	rec := f.do(t, "GET", "/api/admin/users/"+f.target.ID+"/deactivation-impact", adminUser, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var impact struct {
		LiveSessions      int    `json:"live_sessions"`
		WebhookKeys       int    `json:"webhook_keys"`
		CancellationBound string `json:"cancellation_bound"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &impact); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if impact.LiveSessions != 1 {
		t.Errorf("live_sessions = %d, want 1", impact.LiveSessions)
	}
	if impact.WebhookKeys != 1 {
		t.Errorf("webhook_keys = %d, want 1", impact.WebhookKeys)
	}
	if impact.CancellationBound == "" {
		t.Error("cancellation_bound not reported")
	}
}

// TestAdminCannotDisableThemselves: the mistake is easy to make, impossible to
// undo through the API, and pointless to allow.
func TestAdminCannotDisableThemselves(t *testing.T) {
	f := newDisableFixture(t)
	rec := f.do(t, "PUT", "/api/admin/users/"+adminUser+"/state", adminUser, `{"disabled":true}`)
	if rec.Code != http.StatusConflict {
		t.Errorf("self-disable got %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if f.users.ByID[adminUser].Disabled() {
		t.Error("the administrator disabled themselves anyway")
	}
}

// TestLoginCodeForADisabledAccountIsRefused: issuing a way in that opens
// nothing would read to the operator as "they can sign in now".
func TestLoginCodeForADisabledAccountIsRefused(t *testing.T) {
	f := newDisableFixture(t)
	f.users.DisableForTest(f.target.ID, time.Unix(1, 0).UTC())

	rec := f.do(t, "POST", "/api/admin/users/"+f.target.ID+"/login-code", adminUser, "")
	if rec.Code != http.StatusConflict {
		t.Errorf("got %d, want 409: %s", rec.Code, rec.Body.String())
	}
}

// TestDisablingTheLastAdministratorIsRefused: the store decides the last-holder
// rule atomically against the grant table; this proves the handler turns that
// refusal into a 409 an operator can read, not a 500.
func TestDisablingTheLastAdministratorIsRefused(t *testing.T) {
	f := newDisableFixture(t)
	f.users.DisableErr = coreidentity.ErrSystemGrantLastHolder

	rec := f.do(t, "PUT", "/api/admin/users/"+f.target.ID+"/state", adminUser, `{"disabled":true}`)
	if rec.Code != http.StatusConflict {
		t.Errorf("got %d, want 409: %s", rec.Code, rec.Body.String())
	}
}

// TestAdminSessionsListAndSingleRevoke is the "revoke one device" journey: an
// operator lists an account's sessions, retires one, and the other stays live.
func TestAdminSessionsListAndSingleRevoke(t *testing.T) {
	f := newDisableFixture(t)
	laptop := f.seedSession(t, "portal")
	phone := f.seedSession(t, "cli")

	rec := f.do(t, "GET", "/api/admin/users/"+f.target.ID+"/sessions", adminUser, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list got %d: %s", rec.Code, rec.Body.String())
	}
	// The list carries metadata to recognise a device, never the credential.
	body := strings.ToLower(rec.Body.String())
	for _, forbidden := range []string{"token", "hash", "bmxrefresh", "mock-refresh"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the session list leaked %q: %s", forbidden, rec.Body.String())
		}
	}
	var list AdminSessionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(list.Sessions))
	}

	if got := f.do(t, "DELETE", "/api/admin/users/"+f.target.ID+"/sessions/"+laptop, adminUser, "").Code; got != http.StatusOK {
		t.Fatalf("revoke one got %d", got)
	}

	rec = f.do(t, "GET", "/api/admin/users/"+f.target.ID+"/sessions", adminUser, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Sessions) != 1 || list.Sessions[0].SessionID != phone {
		t.Fatalf("after revoke sessions = %+v, want only %q", list.Sessions, phone)
	}

	// Revoking one that is not this account's live session is a 404, not a
	// success recorded against the wrong person.
	if got := f.do(t, "DELETE", "/api/admin/users/"+f.target.ID+"/sessions/as_bogus", adminUser, "").Code; got != http.StatusNotFound {
		t.Errorf("revoke unknown session got %d, want 404", got)
	}

	if !slices.Contains(f.actions(), coreaudit.SessionRevoked) {
		t.Errorf("actions = %v, want a %s event", f.actions(), coreaudit.SessionRevoked)
	}
}

// TestAdminUserListFilters: the account list narrows by state, so an operator
// working a specific set does not page through everyone.
func TestAdminUserListFilters(t *testing.T) {
	f := newDisableFixture(t)
	f.users.DisableForTest(f.target.ID, time.Unix(1, 0).UTC())
	f.users.ByID[adminUser].HasPassword = true

	decode := func(path string) AdminUsersResponse {
		rec := f.do(t, "GET", path, adminUser, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s got %d: %s", path, rec.Code, rec.Body.String())
		}
		var resp AdminUsersResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		return resp
	}

	disabled := decode("/api/admin/users?status=disabled")
	if len(disabled.Users) != 1 || disabled.Users[0].ID != f.target.ID {
		t.Errorf("status=disabled returned %+v, want only the disabled account", disabled.Users)
	}
	for _, u := range decode("/api/admin/users?status=enabled").Users {
		if u.ID == f.target.ID {
			t.Errorf("status=enabled leaked the disabled account")
		}
	}
	for _, u := range decode("/api/admin/users?has_password=false").Users {
		if u.ID == adminUser {
			t.Errorf("has_password=false leaked an account that has a password")
		}
	}
}

// A malformed last-login bound is answered with 400, not silently dropped:
// dropping it would return a wider result than the operator's filter asked for.
func TestAdminUserListRejectsMalformedLastLogin(t *testing.T) {
	f := newDisableFixture(t)
	for _, param := range []string{"last_login_after", "last_login_before"} {
		rec := f.do(t, "GET", "/api/admin/users?"+param+"=yesterday", adminUser, "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s=yesterday got %d, want 400: %s", param, rec.Code, rec.Body.String())
		}
	}
}

// TestAdminAccountActionsAreRecorded: every privileged action names the person
// who took it, not the binary — the caller proved who they are.
func TestAdminAccountActionsAreRecorded(t *testing.T) {
	f := newDisableFixture(t)

	f.do(t, "POST", "/api/admin/users", adminUser, `{"email":"new@example.com"}`)
	f.do(t, "POST", "/api/admin/users/"+f.target.ID+"/login-code", adminUser, "")
	f.do(t, "PUT", "/api/admin/users/"+f.target.ID+"/state", adminUser, `{"disabled":true}`)
	f.do(t, "PUT", "/api/admin/users/"+f.target.ID+"/state", adminUser, `{"disabled":false}`)
	f.do(t, "DELETE", "/api/admin/users/"+f.target.ID+"/sessions", adminUser, "")

	want := []string{
		coreaudit.UserCreated,
		coreaudit.LoginCodeIssued,
		coreaudit.UserDisabled,
		coreaudit.UserEnabled,
		coreaudit.SessionsRevoked,
	}
	got := f.actions()
	if len(got) != len(want) {
		t.Fatalf("actions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("action %d = %q, want %q", i, got[i], want[i])
		}
	}
	for _, e := range f.audits.Events {
		if e.ActorType != coreaudit.ActorUser || e.ActorID != adminUser {
			t.Errorf("event should name the administrator: %+v", e)
		}
		if e.SpaceID != "" {
			t.Errorf("an account action is not space-scoped: %+v", e)
		}
	}
}

// TestAdminResponsesCarryNoSecrets is item 5 of the design's authorization
// matrix. It is crude on purpose: it catches the realistic failure, which is
// someone returning a row struct instead of a response struct.
func TestAdminResponsesCarryNoSecrets(t *testing.T) {
	f := newDisableFixture(t)
	f.users.ByID[f.target.ID].HasPassword = true

	for _, path := range []string{
		"/api/admin/me",
		"/api/admin/users",
		"/api/admin/users/" + f.target.ID,
		"/api/admin/grants",
	} {
		rec := f.do(t, "GET", path, adminUser, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s got %d: %s", path, rec.Code, rec.Body.String())
		}
		body := strings.ToLower(rec.Body.String())
		for _, forbidden := range []string{"password_hash", "api_key", "token_hash", "secret", "code_hash"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("%s leaked %q: %s", path, forbidden, rec.Body.String())
			}
		}
	}
}

// TestAdminUserDetailShowsSpacesWithoutContents: an administrator learns that an
// account can reach a space, never what is in it.
func TestAdminUserDetailShowsSpacesWithoutContents(t *testing.T) {
	f := newDisableFixture(t)
	rec := f.do(t, "GET", "/api/admin/users/"+f.target.ID, adminUser, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	var detail AdminUserDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if detail.ID != f.target.ID {
		t.Errorf("user_id = %q", detail.ID)
	}
	if detail.Spaces == nil || detail.SystemRoles == nil {
		t.Errorf("empty collections should serialize as [], not null: %+v", detail)
	}
}

// TestAdminUserRoutesOnAnUnknownAccount: 404, not a 500 and not a silent
// success on a user id nobody has.
func TestAdminUserRoutesOnAnUnknownAccount(t *testing.T) {
	f := newDisableFixture(t)
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/admin/users/u_nobody"},
		{"PUT", "/api/admin/users/u_nobody/state"},
		{"POST", "/api/admin/users/u_nobody/login-code"},
		{"DELETE", "/api/admin/users/u_nobody/sessions"},
	} {
		if got := f.do(t, tc.method, tc.path, adminUser, "").Code; got != http.StatusNotFound {
			t.Errorf("%s %s got %d, want 404", tc.method, tc.path, got)
		}
	}
}

func newDisableFixture(t *testing.T) *disableFixture {
	t.Helper()
	users := &mock.MockUserStore{}
	grants := &mock.MockSystemGrantStore{}
	grants.GrantForTest(adminUser, coreidentity.SystemRoleAdmin)
	// Two, so revoking one is not the last-grant case.
	grants.GrantForTest("u_second_admin", coreidentity.SystemRoleAdmin)

	refresh := &mock.MockRefreshTokenStore{}
	f := &disableFixture{
		users:    users,
		sessions: &mock.MockAuthSessionStore{Refresh: refresh},
		refresh:  refresh,
		codes:    &mock.MockLoginCodeStore{},
		keys:     &mock.MockUserWebhookKeyStore{},
		audits:   &mock.MockAuditStore{},
	}
	f.admin = seedUser(t, users, adminUser, "admin@example.com")
	f.target = seedUser(t, users, "u_target", "target@example.com")

	h := New(Config{
		JWTSecret:     testSecret,
		Grants:        grants,
		Users:         users,
		Spaces:        &mock.MockSpaceStore{},
		LoginCodes:    f.codes,
		RefreshTokens: f.refresh,
		Sessions:      f.sessions,
		Lifecycle: &accountlifecycle.Service{
			Users:    users,
			Sessions: f.sessions,
			Webhooks: f.keys,
			Spaces:   &mock.MockSpaceStore{},
		},
		// Present so the webhook route reaches its credential check rather
		// than answering "not configured" first.
		Audits: f.audits,
		Audit:  audit.NewRecorder(f.audits),
	})
	f.mux = http.NewServeMux()
	h.Register(f.mux)
	return f
}

// seedSession opens a session for the target account and returns its id.
func (f *disableFixture) seedSession(t *testing.T, platform string) string {
	t.Helper()
	sid, err := f.sessions.CreateSession(t.Context(), coreidentity.NewAuthSession{
		UserID: f.target.ID, Platform: platform, AuthMethod: "login_code",
		AbsoluteExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return sid
}

// disableFixture is a deployment with one administrator and one ordinary
// account, wired with the stores disablement actually touches.
type disableFixture struct {
	mux       *http.ServeMux
	users     *mock.MockUserStore
	sessions  *mock.MockAuthSessionStore
	refresh   *mock.MockRefreshTokenStore
	codes     *mock.MockLoginCodeStore
	keys      *mock.MockUserWebhookKeyStore
	audits    *mock.MockAuditStore
	admin     *coreidentity.User
	target    *coreidentity.User
	actorSIDs map[string]string
}
