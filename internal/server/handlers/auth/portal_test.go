package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	"github.com/icloudbb/buildmax/internal/mock"
)

// The httptest requests below arrive on Host "example.com" over plain HTTP, so a
// same-origin Origin is this.
const portalOrigin = "http://example.com"

func portalHeaders() map[string]string { return map[string]string{"Origin": portalOrigin} }

func portalCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == portalRefreshCookie {
			return c
		}
	}
	return nil
}

// TestPortalLoginSetsCookieAndWithholdsRefreshToken is the property that moves
// the renewable credential out of script's reach: the body carries an access
// token but never a refresh token, which rides in an HttpOnly cookie instead.
func TestPortalLoginSetsCookieAndWithholdsRefreshToken(t *testing.T) {
	mux, _ := newAuthTestMux(t, Config{})

	rec := postJSON(t, mux, "/api/auth/portal/login", `{"email":"a@b.c","otp":"code-1"}`, portalHeaders())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	if body["access_token"] == "" || body["access_token"] == nil {
		t.Error("no access token in body")
	}
	if _, ok := body["refresh_token"]; ok {
		t.Error("the Portal login body carried a refresh token; it must ride in the cookie")
	}
	c := portalCookie(rec)
	if c == nil {
		t.Fatal("no refresh cookie set")
	}
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != portalRefreshCookiePath {
		t.Errorf("cookie attrs = %+v; want HttpOnly, SameSite=Strict, Path=%s", c, portalRefreshCookiePath)
	}
	if c.Value == "" {
		t.Error("refresh cookie has no value")
	}
}

// TestPortalSessionRotatesTheCookie: exchanging the cookie returns a fresh access
// token and replaces the cookie, the same rotation the JSON refresh does.
func TestPortalSessionRotatesTheCookie(t *testing.T) {
	mux, _ := newAuthTestMux(t, Config{})
	login := postJSON(t, mux, "/api/auth/portal/login", `{"email":"a@b.c","otp":"code-1"}`, portalHeaders())
	first := portalCookie(login)
	if first == nil {
		t.Fatal("login set no cookie")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/portal/session", nil)
	req.Header.Set("Origin", portalOrigin)
	req.AddCookie(first)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("session status = %d, body %s", rec.Code, rec.Body.String())
	}
	next := portalCookie(rec)
	if next == nil || next.Value == "" {
		t.Fatal("session did not rotate the cookie")
	}
	if next.Value == first.Value {
		t.Error("the cookie value did not change on exchange")
	}
	if body := decodeJSON(t, rec); body["access_token"] == "" {
		t.Error("session returned no access token")
	}
}

// TestPortalLogoutClearsCookieAndRevokesSession: after logout the cookie is
// cleared and the session it named no longer exchanges.
func TestPortalLogoutClearsCookieAndRevokesSession(t *testing.T) {
	mux, _ := newAuthTestMux(t, Config{})
	login := postJSON(t, mux, "/api/auth/portal/login", `{"email":"a@b.c","otp":"code-1"}`, portalHeaders())
	cookie := portalCookie(login)
	if cookie == nil {
		t.Fatal("login set no cookie")
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/api/auth/portal/logout", nil)
	logoutReq.Header.Set("Origin", portalOrigin)
	logoutReq.AddCookie(cookie)
	logoutRec := httptest.NewRecorder()
	mux.ServeHTTP(logoutRec, logoutReq)
	if logoutRec.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, body %s", logoutRec.Code, logoutRec.Body.String())
	}
	if cleared := portalCookie(logoutRec); cleared == nil || cleared.MaxAge >= 0 {
		t.Errorf("logout did not clear the cookie: %+v", cleared)
	}

	// The revoked session's cookie no longer exchanges.
	sessReq := httptest.NewRequest(http.MethodPost, "/api/auth/portal/session", nil)
	sessReq.Header.Set("Origin", portalOrigin)
	sessReq.AddCookie(cookie)
	sessRec := httptest.NewRecorder()
	mux.ServeHTTP(sessRec, sessReq)
	if sessRec.Code == http.StatusOK {
		t.Errorf("a logged-out session still exchanged: %s", sessRec.Body.String())
	}
}

// TestPortalRoutesRefuseCrossOrigin: SameSite already withholds the cookie
// cross-site; the Origin check is the belt to that suspenders.
func TestPortalRoutesRefuseCrossOrigin(t *testing.T) {
	mux, _ := newAuthTestMux(t, Config{})
	for _, path := range []string{"/api/auth/portal/login", "/api/auth/portal/session", "/api/auth/portal/logout"} {
		rec := postJSON(t, mux, path, `{"email":"a@b.c","otp":"code-1"}`, map[string]string{"Origin": "http://evil.example"})
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s cross-origin got %d, want 403", path, rec.Code)
		}
	}
}

// TestPortalSessionWithoutCookieIsNoContent: a signed-out browser asking whether
// it has a session gets "no" as an answer, not a 401 logged on every
// signed-out page load, and nothing is set.
func TestPortalSessionWithoutCookieIsNoContent(t *testing.T) {
	mux, _ := newAuthTestMux(t, Config{})
	rec := postJSON(t, mux, "/api/auth/portal/session", ``, portalHeaders())
	if rec.Code != http.StatusNoContent {
		t.Errorf("no-cookie session got %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("no-cookie session carried a body: %s", rec.Body.String())
	}
	if c := portalCookie(rec); c != nil {
		t.Errorf("no-cookie session set a cookie: %+v", c)
	}
}

// TestPortalSessionWithDeadCookieIsUnauthorized: a cookie that names no live
// session is still refused, and cleared so the refusal is not repeated.
func TestPortalSessionWithDeadCookieIsUnauthorized(t *testing.T) {
	mux, _ := newAuthTestMux(t, Config{})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/portal/session", nil)
	req.Header.Set("Origin", portalOrigin)
	req.AddCookie(&http.Cookie{Name: portalRefreshCookie, Value: "not-a-session"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("dead-cookie session got %d, want 401: %s", rec.Code, rec.Body.String())
	}
	if c := portalCookie(rec); c == nil || c.MaxAge >= 0 {
		t.Errorf("dead-cookie session did not clear the cookie: %+v", c)
	}
}

// TestPortalSessionCarriesSystemRoles: the Portal learns the account's
// deployment authority from the session it already exchanges, at login and at
// every exchange, rather than from an admin route that refuses everyone else.
func TestPortalSessionCarriesSystemRoles(t *testing.T) {
	roles := func(t *testing.T, rec *httptest.ResponseRecorder) []any {
		t.Helper()
		got, ok := decodeJSON(t, rec)["system_roles"].([]any)
		if !ok {
			t.Fatalf("system_roles missing or not a list: %s", rec.Body.String())
		}
		return got
	}

	t.Run("an administrator's roles, at login and at exchange", func(t *testing.T) {
		grants := &mock.MockSystemGrantStore{}
		grants.GrantForTest("u1", coreidentity.SystemRoleAdmin)
		mux, _ := newAuthTestMux(t, Config{Grants: grants})
		login := postJSON(t, mux, "/api/auth/portal/login", `{"email":"a@b.c","otp":"code-1"}`, portalHeaders())
		if got := roles(t, login); len(got) != 1 || got[0] != coreidentity.SystemRoleAdmin {
			t.Errorf("login system_roles = %v, want [%s]", got, coreidentity.SystemRoleAdmin)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/auth/portal/session", nil)
		req.Header.Set("Origin", portalOrigin)
		req.AddCookie(portalCookie(login))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if got := roles(t, rec); len(got) != 1 || got[0] != coreidentity.SystemRoleAdmin {
			t.Errorf("session system_roles = %v, want [%s]", got, coreidentity.SystemRoleAdmin)
		}
	})

	t.Run("an empty list, never null, for everyone else", func(t *testing.T) {
		for name, grants := range map[string]coreidentity.SystemGrantStore{
			"no grant":           &mock.MockSystemGrantStore{},
			"no grant store":     nil,
			"grants unreachable": &mock.MockSystemGrantStore{Err: errors.New("database down")},
		} {
			t.Run(name, func(t *testing.T) {
				mux, _ := newAuthTestMux(t, Config{Grants: grants})
				rec := postJSON(t, mux, "/api/auth/portal/login", `{"email":"a@b.c","otp":"code-1"}`, portalHeaders())
				if rec.Code != http.StatusOK {
					t.Fatalf("login status = %d, body %s", rec.Code, rec.Body.String())
				}
				if got := roles(t, rec); len(got) != 0 {
					t.Errorf("system_roles = %v, want []", got)
				}
			})
		}
	})
}
