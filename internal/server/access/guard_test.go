package access

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/testsupport"
)

const guardSecret = "guard-test-secret"

// The guard is the single funnel every authenticated route reaches, so its
// refusals — revoked session, expired session, a token whose sid names another
// account, a disabled account — are what make "log out" and "disable" immediate.
// These pin each branch directly rather than through a handler.
func TestActiveUser(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)
	revoked := now.Add(-time.Minute)

	newGuard := func(users *mock.MockUserStore, sessions *mock.MockAuthSessionStore) *Guard {
		return &Guard{JWTSecret: guardSecret, Users: users, Sessions: sessions, Now: func() time.Time { return now }}
	}
	// sessionStore builds a store holding one session for sid->userID with the
	// given expiry and revocation.
	sessionStore := func(sid, userID string, exp time.Time, rev *time.Time) *mock.MockAuthSessionStore {
		return &mock.MockAuthSessionStore{Sessions: map[string]*mock.MockAuthSession{
			sid: {SID: sid, UserID: userID, AbsoluteExpiresAt: exp, RevokedAt: rev},
		}}
	}
	activeUsers := func() *mock.MockUserStore {
		return &mock.MockUserStore{ByID: map[string]*coreidentity.User{"u1": {ID: "u1"}}}
	}

	req := func(token string) (*httptest.ResponseRecorder, *http.Request) {
		r := httptest.NewRequest(http.MethodGet, "/api/anything", nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		return httptest.NewRecorder(), r
	}

	t.Run("a valid token with an active session and account is allowed", func(t *testing.T) {
		g := newGuard(activeUsers(), sessionStore("s1", "u1", future, nil))
		rec, r := req(testsupport.SignJWTWithSID("u1", "s1", guardSecret))
		id, ok := g.ActiveUser(rec, r)
		if !ok || id != "u1" {
			t.Fatalf("ActiveUser = (%q, %v), want (u1, true); body=%s", id, ok, rec.Body.String())
		}
	})

	t.Run("no or malformed token is 401", func(t *testing.T) {
		g := newGuard(activeUsers(), sessionStore("s1", "u1", future, nil))
		for _, tok := range []string{"", "not-a-jwt", testsupport.SignJWTWithSID("u1", "s1", "wrong-secret")} {
			rec, r := req(tok)
			if _, ok := g.ActiveUser(rec, r); ok || rec.Code != http.StatusUnauthorized {
				t.Errorf("token %q: ok=%v code=%d, want refused 401", tok, ok, rec.Code)
			}
		}
	})

	t.Run("a revoked session is 401", func(t *testing.T) {
		g := newGuard(activeUsers(), sessionStore("s1", "u1", future, &revoked))
		rec, r := req(testsupport.SignJWTWithSID("u1", "s1", guardSecret))
		if _, ok := g.ActiveUser(rec, r); ok || rec.Code != http.StatusUnauthorized {
			t.Errorf("ok=%v code=%d, want refused 401 for a revoked session", ok, rec.Code)
		}
	})

	t.Run("an expired session is 401", func(t *testing.T) {
		g := newGuard(activeUsers(), sessionStore("s1", "u1", past, nil))
		rec, r := req(testsupport.SignJWTWithSID("u1", "s1", guardSecret))
		if _, ok := g.ActiveUser(rec, r); ok || rec.Code != http.StatusUnauthorized {
			t.Errorf("ok=%v code=%d, want refused 401 for an expired session", ok, rec.Code)
		}
	})

	t.Run("a token whose sid names another account's session is 401", func(t *testing.T) {
		// The session s1 belongs to u2, but the token claims subject u1.
		g := newGuard(&mock.MockUserStore{ByID: map[string]*coreidentity.User{"u1": {ID: "u1"}, "u2": {ID: "u2"}}},
			sessionStore("s1", "u2", future, nil))
		rec, r := req(testsupport.SignJWTWithSID("u1", "s1", guardSecret))
		if _, ok := g.ActiveUser(rec, r); ok || rec.Code != http.StatusUnauthorized {
			t.Errorf("ok=%v code=%d, want refused 401 when sid's owner != subject", ok, rec.Code)
		}
	})

	t.Run("a disabled account is 403 with the disabled message", func(t *testing.T) {
		disabledAt := past
		users := &mock.MockUserStore{ByID: map[string]*coreidentity.User{"u1": {ID: "u1", DisabledAt: &disabledAt}}}
		g := newGuard(users, sessionStore("s1", "u1", future, nil))
		rec, r := req(testsupport.SignJWTWithSID("u1", "s1", guardSecret))
		if _, ok := g.ActiveUser(rec, r); ok || rec.Code != http.StatusForbidden {
			t.Fatalf("ok=%v code=%d, want refused 403 for a disabled account", ok, rec.Code)
		}
	})

	t.Run("no session store skips the session check", func(t *testing.T) {
		g := &Guard{JWTSecret: guardSecret, Users: activeUsers(), Now: func() time.Time { return now }}
		// A token with no sid at all is admitted when no session store is wired.
		rec, r := req(testsupport.SignJWT("u1", guardSecret))
		if _, ok := g.ActiveUser(rec, r); !ok {
			t.Errorf("ok=%v code=%d, want allowed when no session store is wired", ok, rec.Code)
		}
	})
}
