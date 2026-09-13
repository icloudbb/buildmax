package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/server/access"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	"github.com/icloudbb/buildmax/internal/mock"
)

const refreshTestSecret = "test-jwt-secret"

func refreshTestUser() *coreidentity.User {
	return &coreidentity.User{ID: "u1", Email: "a@b.c", Name: "Alice"}
}

// newAuthTestMux wires a handler that can log in, refresh, and log out, and
// returns the refresh token store so a test can inspect what it did.
func newAuthTestMux(t *testing.T, cfg Config) (*http.ServeMux, *mock.MockRefreshTokenStore) {
	t.Helper()
	user := refreshTestUser()
	store := &mock.MockRefreshTokenStore{}
	if cfg.Users == nil {
		cfg.Users = &mock.MockUserStore{
			ByEmail: map[string]*coreidentity.User{"a@b.c": user},
			ByID:    map[string]*coreidentity.User{"u1": user},
		}
	}
	if cfg.LoginCodes == nil {
		cfg.LoginCodes = &mock.MockLoginCodeStore{Codes: map[string]*mock.MockLoginCode{
			"code-1": {UserID: "u1", ExpiresAt: time.Now().Add(time.Hour).UTC()},
		}}
	}
	if cfg.RefreshTokens == nil {
		cfg.RefreshTokens = store
	} else if s, ok := cfg.RefreshTokens.(*mock.MockRefreshTokenStore); ok {
		store = s
	}
	if cfg.Sessions == nil {
		cfg.Sessions = &mock.MockAuthSessionStore{Refresh: store}
	}
	if cfg.JWTSecret == "" {
		cfg.JWTSecret = refreshTestSecret
	}
	mux := http.NewServeMux()
	New(cfg).Register(mux)
	return mux, store
}

func postJSON(t *testing.T, mux *http.ServeMux, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return m
}

// login runs a real login and returns the token pair, so the tests below
// exercise the same credentials a client would hold.
func login(t *testing.T, mux *http.ServeMux) (accessToken, refreshToken string) {
	t.Helper()
	rec := postJSON(t, mux, "/api/auth/login", `{"email":"a@b.c","otp":"code-1","platform":"cli"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, body %s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	access, _ := body["access_token"].(string)
	refresh, _ := body["refresh_token"].(string)
	if access == "" || refresh == "" {
		t.Fatalf("login returned access=%q refresh=%q", access, refresh)
	}
	return access, refresh
}

func TestLoginIssuesBothCredentials(t *testing.T) {
	mux, _ := newAuthTestMux(t, Config{})
	rec := postJSON(t, mux, "/api/auth/login", `{"email":"a@b.c","otp":"code-1","platform":"cli"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)

	// token is the pre-split name for the access token. A client written
	// against the old response must keep working.
	if body["token"] != body["access_token"] {
		t.Errorf("token = %v, want the same value as access_token %v", body["token"], body["access_token"])
	}
	if body["expires_in"] != float64(coreidentity.AccessTokenTTLDefault.Seconds()) {
		t.Errorf("expires_in = %v, want %v", body["expires_in"], coreidentity.AccessTokenTTLDefault.Seconds())
	}
	if _, ok := body["refresh_token"].(string); !ok {
		t.Error("response carries no refresh token")
	}
}

// A deployment with no refresh token store still logs people in — it just
// hands out the one credential, as BuildMax did before the split.
func TestLoginWithoutRefreshStoreStillIssuesAnAccessToken(t *testing.T) {
	user := refreshTestUser()
	mux := http.NewServeMux()
	New(Config{
		Users: &mock.MockUserStore{
			ByEmail: map[string]*coreidentity.User{"a@b.c": user},
			ByID:    map[string]*coreidentity.User{"u1": user},
		},
		LoginCodes: &mock.MockLoginCodeStore{Codes: map[string]*mock.MockLoginCode{
			"code-1": {UserID: "u1", ExpiresAt: time.Now().Add(time.Hour).UTC()},
		}},
		JWTSecret: refreshTestSecret,
	}).Register(mux)

	rec := postJSON(t, mux, "/api/auth/login", `{"email":"a@b.c","otp":"code-1"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	if body["access_token"] == "" {
		t.Error("no access token issued")
	}
	if _, ok := body["refresh_token"]; ok {
		t.Error("a deployment with no store returned a refresh token")
	}
}

func TestRefreshRotatesAndTheOldTokenStopsWorking(t *testing.T) {
	mux, _ := newAuthTestMux(t, Config{})
	_, refreshToken := login(t, mux)

	rec := postJSON(t, mux, "/api/auth/token/refresh", `{"refresh_token":"`+refreshToken+`"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, body %s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	next, _ := body["refresh_token"].(string)
	if next == "" || next == refreshToken {
		t.Fatalf("refresh returned %q, want a token different from the one presented", next)
	}
	if access, _ := body["access_token"].(string); access == "" {
		t.Error("refresh returned no access token")
	}

	// The replacement works.
	if rec := postJSON(t, mux, "/api/auth/token/refresh", `{"refresh_token":"`+next+`"}`, nil); rec.Code != http.StatusOK {
		t.Errorf("rotated token status = %d, want 200", rec.Code)
	}
}

// The access token minted by a refresh must authenticate real requests. A
// refresh that returned a token the middleware rejects would look like a
// success and log everyone out one TTL later.
func TestRefreshedAccessTokenAuthenticates(t *testing.T) {
	mux, _ := newAuthTestMux(t, Config{})
	_, refreshToken := login(t, mux)

	rec := postJSON(t, mux, "/api/auth/token/refresh", `{"refresh_token":"`+refreshToken+`"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d", rec.Code)
	}
	token, _ := decodeJSON(t, rec)["access_token"].(string)

	claims, ok := access.Verify(token, refreshTestSecret)
	if !ok {
		t.Fatal("the refreshed access token does not verify")
	}
	if claims.Sub != "u1" {
		t.Errorf("sub = %q, want u1", claims.Sub)
	}
	if claims.Sid == "" {
		t.Error("the refreshed token carries no session id")
	}
	if claims.Typ != access.TypeAccess {
		t.Errorf("typ = %q, want %q", claims.Typ, access.TypeAccess)
	}
}

// Rotation keeps the session, so an access token minted by a refresh can still
// be logged out by the session it descends from.
func TestRefreshKeepsTheSession(t *testing.T) {
	mux, _ := newAuthTestMux(t, Config{})
	firstAccess, refreshToken := login(t, mux)
	firstClaims, ok := access.Verify(firstAccess, refreshTestSecret)
	if !ok {
		t.Fatal("login token does not verify")
	}

	rec := postJSON(t, mux, "/api/auth/token/refresh", `{"refresh_token":"`+refreshToken+`"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d", rec.Code)
	}
	token, _ := decodeJSON(t, rec)["access_token"].(string)
	claims, ok := access.Verify(token, refreshTestSecret)
	if !ok {
		t.Fatal("refreshed token does not verify")
	}
	if claims.Sid != firstClaims.Sid {
		t.Errorf("session changed across a refresh: %q then %q", firstClaims.Sid, claims.Sid)
	}
}

// Each login is its own session, so signing in on a second machine cannot
// revoke the first.
func TestEachLoginOpensItsOwnSession(t *testing.T) {
	mux, _ := newAuthTestMux(t, Config{
		LoginCodes: &mock.MockLoginCodeStore{Codes: map[string]*mock.MockLoginCode{
			"code-1": {UserID: "u1", ExpiresAt: time.Now().Add(time.Hour).UTC()},
			"code-2": {UserID: "u1", ExpiresAt: time.Now().Add(time.Hour).UTC()},
		}},
	})
	firstAccess, firstRefresh := login(t, mux)

	rec := postJSON(t, mux, "/api/auth/login", `{"email":"a@b.c","otp":"code-2","platform":"portal"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("second login status = %d, body %s", rec.Code, rec.Body.String())
	}
	secondAccess, _ := decodeJSON(t, rec)["access_token"].(string)

	first, _ := access.Verify(firstAccess, refreshTestSecret)
	second, _ := access.Verify(secondAccess, refreshTestSecret)
	if first.Sid == second.Sid {
		t.Fatal("two logins share one session; revoking either would sign out both")
	}

	// Logging the second one out leaves the first able to refresh.
	if rec := postJSON(t, mux, "/api/auth/logout", "", map[string]string{
		"Authorization": "Bearer " + secondAccess,
	}); rec.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d", rec.Code)
	}
	if rec := postJSON(t, mux, "/api/auth/token/refresh", `{"refresh_token":"`+firstRefresh+`"}`, nil); rec.Code != http.StatusOK {
		t.Errorf("logging out one session broke another: refresh status = %d", rec.Code)
	}
}

// The reason rotation is worth its complexity: presenting a spent token means
// two holders, and neither keeps the session.
func TestReusedRefreshTokenRevokesTheWholeSession(t *testing.T) {
	mux, store := newAuthTestMux(t, Config{RefreshRotationGrace: time.Minute})
	_, stolen := login(t, mux)

	rec := postJSON(t, mux, "/api/auth/token/refresh", `{"refresh_token":"`+stolen+`"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("first refresh status = %d", rec.Code)
	}
	legitimate, _ := decodeJSON(t, rec)["refresh_token"].(string)

	// Push the exchange out of the grace window rather than waiting for it.
	// Grace is measured in whole seconds, so a test that relied on real time
	// would either sleep or race.
	if !store.Backdate(stolen, time.Hour) {
		t.Fatal("the presented token was not marked spent by the first exchange")
	}

	// The copy is presented after the grace window.
	rec = postJSON(t, mux, "/api/auth/token/refresh", `{"refresh_token":"`+stolen+`"}`, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("replayed token status = %d, want 401", rec.Code)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("reuse")) {
		t.Error("the response tells the caller reuse was detected")
	}

	// The holder that did nothing wrong is signed out too. That is the point:
	// with two copies in circulation there is no way to tell which is which.
	rec = postJSON(t, mux, "/api/auth/token/refresh", `{"refresh_token":"`+legitimate+`"}`, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("the session survived a reuse report: status = %d", rec.Code)
	}

	for plaintext, row := range store.Tokens {
		if row.RevokedAt == nil {
			t.Errorf("token %q is still live after reuse", plaintext)
		}
	}
}

// Two processes sharing one credentials file refresh at the same moment. Both
// must come away usable; treating the second as a replay would sign the user
// out for running two commands at once.
func TestConcurrentRefreshWithinGraceBothSucceed(t *testing.T) {
	mux, _ := newAuthTestMux(t, Config{RefreshRotationGrace: time.Hour})
	_, refreshToken := login(t, mux)

	first := postJSON(t, mux, "/api/auth/token/refresh", `{"refresh_token":"`+refreshToken+`"}`, nil)
	second := postJSON(t, mux, "/api/auth/token/refresh", `{"refresh_token":"`+refreshToken+`"}`, nil)
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("statuses = %d and %d, want both 200", first.Code, second.Code)
	}
	firstNext, _ := decodeJSON(t, first)["refresh_token"].(string)
	secondNext, _ := decodeJSON(t, second)["refresh_token"].(string)
	if firstNext == secondNext {
		t.Fatal("both exchanges returned the same token")
	}
	// Both replacements are live.
	for _, tok := range []string{firstNext, secondNext} {
		if rec := postJSON(t, mux, "/api/auth/token/refresh", `{"refresh_token":"`+tok+`"}`, nil); rec.Code != http.StatusOK {
			t.Errorf("token from a concurrent refresh is not usable: status = %d", rec.Code)
		}
	}
}

func TestRefreshRejectsUnknownAndMissingTokens(t *testing.T) {
	mux, _ := newAuthTestMux(t, Config{})
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{"unknown token", `{"refresh_token":"mock-refresh-nobody-9"}`, http.StatusUnauthorized},
		{"empty token", `{"refresh_token":""}`, http.StatusBadRequest},
		{"malformed body", `not json`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := postJSON(t, mux, "/api/auth/token/refresh", tt.body, nil)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

// A refresh token outlives many access tokens, so the account behind it is
// re-checked on every exchange rather than trusted from the login.
func TestRefreshRejectsADeletedUser(t *testing.T) {
	mux, store := newAuthTestMux(t, Config{})
	_, refreshToken := login(t, mux)

	// Rebuild the handler with the user gone but the same token store behind
	// it. That is the shape of an account removed between two refreshes.
	deleted, _ := newAuthTestMux(t, Config{
		Users:         &mock.MockUserStore{ByID: map[string]*coreidentity.User{}},
		RefreshTokens: store,
	})
	rec := postJSON(t, deleted, "/api/auth/token/refresh", `{"refresh_token":"`+refreshToken+`"}`, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for a token whose account is gone", rec.Code)
	}

	// The session is retired rather than left for the next attempt.
	if rec := postJSON(t, mux, "/api/auth/token/refresh", `{"refresh_token":"`+refreshToken+`"}`, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("the session survived a deleted account: status = %d", rec.Code)
	}
}

func TestLogoutByRefreshTokenEndsTheSession(t *testing.T) {
	mux, _ := newAuthTestMux(t, Config{})
	_, refreshToken := login(t, mux)

	if rec := postJSON(t, mux, "/api/auth/logout", `{"refresh_token":"`+refreshToken+`"}`, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, body %s", rec.Code, rec.Body.String())
	}
	if rec := postJSON(t, mux, "/api/auth/token/refresh", `{"refresh_token":"`+refreshToken+`"}`, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("a logged-out token still refreshes: status = %d", rec.Code)
	}
}

// Logging out with an expired access token and no refresh token is not an
// error the client can act on, but it must not leave the session live either.
func TestLogoutWithoutAnyCredentialIsUnauthorized(t *testing.T) {
	mux, _ := newAuthTestMux(t, Config{})
	if rec := postJSON(t, mux, "/api/auth/logout", "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// Logging out something the server has already forgotten is a success: the
// client's goal — that token no longer works — is already true.
func TestLogoutWithAnUnknownTokenSucceeds(t *testing.T) {
	mux, _ := newAuthTestMux(t, Config{})
	if rec := postJSON(t, mux, "/api/auth/logout", `{"refresh_token":"mock-refresh-nobody-9"}`, nil); rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
}
