package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
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

// TestPortalSessionWithoutCookieIsUnauthorized: nothing to exchange is a 401, not
// a server error.
func TestPortalSessionWithoutCookieIsUnauthorized(t *testing.T) {
	mux, _ := newAuthTestMux(t, Config{})
	rec := postJSON(t, mux, "/api/auth/portal/session", ``, portalHeaders())
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no-cookie session got %d, want 401: %s", rec.Code, rec.Body.String())
	}
}
