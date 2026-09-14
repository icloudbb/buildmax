package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	infraoidc "github.com/icloudbb/buildmax/internal/infra/oidc"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/audit"
)

// fakeFlow stands in for the OIDC provider so these tests drive the whole
// callback without a network or a real IdP. It records what Complete was given,
// which is how the state/PKCE binding is checked from the handler's side.
type fakeFlow struct {
	authURL     string
	txn         infraoidc.Transaction
	beginErr    error
	claims      *infraoidc.Claims
	completeErr error
	gotCode     string
	gotTxn      infraoidc.Transaction
}

func (f *fakeFlow) BeginAuth(_ context.Context, _ string, _ []string) (string, infraoidc.Transaction, error) {
	if f.beginErr != nil {
		return "", infraoidc.Transaction{}, f.beginErr
	}
	return f.authURL, f.txn, nil
}

func (f *fakeFlow) Complete(_ context.Context, code string, txn infraoidc.Transaction, _ string, _ []string) (*infraoidc.Claims, error) {
	f.gotCode, f.gotTxn = code, txn
	if f.completeErr != nil {
		return nil, f.completeErr
	}
	return f.claims, nil
}

const oidcTestBase = "https://buildmax.example.com"

type oidcFixture struct {
	h     *Handler
	flow  *fakeFlow
	users *mock.MockUserStore
	eids  *mock.MockExternalIdentityStore
}

func newOIDCFixture(t *testing.T, flow *fakeFlow, domains []string) *oidcFixture {
	t.Helper()
	users := &mock.MockUserStore{}
	eids := &mock.MockExternalIdentityStore{Users: users}
	h := New(Config{
		JWTSecret:           "oidc-test-secret",
		OIDCEnabled:         true,
		OIDC:                flow,
		PublicBaseURL:       oidcTestBase,
		Provisioning:        identityProvisioningJIT,
		AllowedEmailDomains: domains,
		Users:               users,
		ExternalIdentities:  eids,
		Sessions:            &mock.MockAuthSessionStore{},
		RefreshTokens:       &mock.MockRefreshTokenStore{},
		Audit:               audit.NewRecorder(&mock.MockAuditStore{}),
	})
	return &oidcFixture{h: h, flow: flow, users: users, eids: eids}
}

// identityProvisioningJIT mirrors the service constant without importing it into
// the fixture; the value is what the config passes through.
const identityProvisioningJIT = "jit"

func (f *oidcFixture) start(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	f.h.oidcStartHandler(rec, httptest.NewRequest(http.MethodGet, oidcTestBase+"/api/auth/oidc/start", nil))
	return rec
}

// txnCookieFrom extracts the transaction cookie a start response set, so a
// callback can present it exactly as the browser would.
func txnCookieFrom(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == oidcTxnCookie {
			return c
		}
	}
	t.Fatal("start set no transaction cookie")
	return nil
}

func (f *oidcFixture) callback(t *testing.T, query string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, oidcTestBase+"/api/auth/oidc/callback?"+query, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	f.h.oidcCallbackHandler(rec, req)
	return rec
}

func TestOIDCStartRedirectsAndSetsTxnCookie(t *testing.T) {
	flow := &fakeFlow{authURL: "https://idp.example.com/authorize?x=1", txn: infraoidc.Transaction{State: "st", Nonce: "no", Verifier: "vf"}}
	f := newOIDCFixture(t, flow, []string{"example.com"})
	rec := f.start(t)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != flow.authURL {
		t.Errorf("redirected to %q, want the IdP authorize URL", loc)
	}
	cookie := txnCookieFrom(t, rec)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("txn cookie must be HttpOnly and SameSite=Lax: %+v", cookie)
	}
}

func TestOIDCStartIs404WhenDisabled(t *testing.T) {
	f := newOIDCFixture(t, &fakeFlow{}, nil)
	f.h.cfg.OIDCEnabled = false
	if got := f.start(t).Code; got != http.StatusNotFound {
		t.Errorf("status = %d, want 404 when SSO is off", got)
	}
}

// TestOIDCCallbackProvisionsAndSetsSession is the happy path: a verified first
// sign-in creates the account, opens a session, and hands back the Portal
// refresh cookie — with nothing sensitive in the redirect.
func TestOIDCCallbackProvisionsAndSetsSession(t *testing.T) {
	flow := &fakeFlow{
		authURL: "https://idp/authorize",
		txn:     infraoidc.Transaction{State: "state-1", Nonce: "nonce-1", Verifier: "verifier-1"},
		claims:  &infraoidc.Claims{Issuer: "https://idp", Subject: "okta|1", Email: "new@example.com", EmailVerified: true, Name: "New"},
	}
	f := newOIDCFixture(t, flow, []string{"example.com"})
	cookie := txnCookieFrom(t, f.start(t))

	rec := f.callback(t, "state=state-1&code=abc123", cookie)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if loc != oidcTestBase+"/" {
		t.Errorf("redirected to %q, want the Portal root", loc)
	}
	if strings.Contains(loc, "token") || strings.Contains(loc, "@") || strings.Contains(loc, "code") {
		t.Errorf("redirect URL leaks something sensitive: %q", loc)
	}
	// The code and transaction reached the provider intact.
	if f.flow.gotCode != "abc123" || f.flow.gotTxn.Verifier != "verifier-1" {
		t.Errorf("Complete got code=%q verifier=%q", f.flow.gotCode, f.flow.gotTxn.Verifier)
	}
	// A session was opened: the Portal refresh cookie is set, and the txn cookie
	// is cleared.
	var refreshSet, txnCleared bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == portalRefreshCookie && c.Value != "" {
			refreshSet = true
		}
		if c.Name == oidcTxnCookie && c.MaxAge < 0 {
			txnCleared = true
		}
	}
	if !refreshSet {
		t.Error("no portal refresh cookie was set")
	}
	if !txnCleared {
		t.Error("the one-time transaction cookie was not cleared")
	}
	// The account really exists now.
	if u, _ := f.users.UserByEmail(context.Background(), "new@example.com"); u == nil {
		t.Error("JIT did not create the account")
	}
}

func TestOIDCCallbackRefusesStateMismatch(t *testing.T) {
	flow := &fakeFlow{txn: infraoidc.Transaction{State: "state-1", Nonce: "n", Verifier: "v"}}
	f := newOIDCFixture(t, flow, []string{"example.com"})
	cookie := txnCookieFrom(t, f.start(t))

	rec := f.callback(t, "state=WRONG&code=abc", cookie)
	assertSSOError(t, rec, ssoErrExpired)
	if f.flow.gotCode != "" {
		t.Error("Complete was called despite a state mismatch")
	}
}

func TestOIDCCallbackRefusesMissingCookie(t *testing.T) {
	flow := &fakeFlow{txn: infraoidc.Transaction{State: "state-1"}}
	f := newOIDCFixture(t, flow, []string{"example.com"})
	rec := f.callback(t, "state=state-1&code=abc", nil)
	assertSSOError(t, rec, ssoErrExpired)
}

func TestOIDCCallbackRefusesTamperedCookie(t *testing.T) {
	flow := &fakeFlow{txn: infraoidc.Transaction{State: "state-1", Nonce: "n", Verifier: "v"}}
	f := newOIDCFixture(t, flow, []string{"example.com"})
	cookie := txnCookieFrom(t, f.start(t))
	cookie.Value = cookie.Value + "x" // break the MAC

	rec := f.callback(t, "state=state-1&code=abc", cookie)
	assertSSOError(t, rec, ssoErrExpired)
}

func TestOIDCCallbackIdPErrorIsNotAuthorized(t *testing.T) {
	flow := &fakeFlow{txn: infraoidc.Transaction{State: "state-1"}}
	f := newOIDCFixture(t, flow, []string{"example.com"})
	cookie := txnCookieFrom(t, f.start(t))
	rec := f.callback(t, "error=access_denied&state=state-1", cookie)
	assertSSOError(t, rec, ssoErrNotAuthorized)
}

func TestOIDCCallbackExchangeFailureIsUnavailable(t *testing.T) {
	flow := &fakeFlow{
		txn:         infraoidc.Transaction{State: "state-1", Nonce: "n", Verifier: "v"},
		completeErr: context.DeadlineExceeded,
	}
	f := newOIDCFixture(t, flow, []string{"example.com"})
	cookie := txnCookieFrom(t, f.start(t))
	rec := f.callback(t, "state=state-1&code=abc", cookie)
	assertSSOError(t, rec, ssoErrUnavailable)
}

func TestOIDCCallbackDisallowedDomainIsNotAuthorized(t *testing.T) {
	flow := &fakeFlow{
		txn:    infraoidc.Transaction{State: "state-1", Nonce: "n", Verifier: "v"},
		claims: &infraoidc.Claims{Issuer: "https://idp", Subject: "okta|9", Email: "x@evil.com", EmailVerified: true},
	}
	f := newOIDCFixture(t, flow, []string{"example.com"})
	cookie := txnCookieFrom(t, f.start(t))
	rec := f.callback(t, "state=state-1&code=abc", cookie)
	assertSSOError(t, rec, ssoErrNotAuthorized)
}

func TestOIDCCallbackDisabledAccountIsDisabled(t *testing.T) {
	flow := &fakeFlow{
		txn:    infraoidc.Transaction{State: "state-1", Nonce: "n", Verifier: "v"},
		claims: &infraoidc.Claims{Issuer: "https://idp", Subject: "okta|d", Email: "off@example.com", EmailVerified: true},
	}
	f := newOIDCFixture(t, flow, []string{"example.com"})
	// Seed a disabled account already linked to this subject, so association
	// resolves it by rule 1 and refuses it by rule 2.
	u := &coreidentity.User{ID: "u_off", Email: "off@example.com"}
	f.users.ByID = map[string]*coreidentity.User{u.ID: u}
	f.users.ByEmail = map[string]*coreidentity.User{u.Email: u}
	if _, err := f.eids.LinkExisting(context.Background(), coreidentity.LinkIdentity{
		UserID: "u_off", Issuer: "https://idp", Subject: "okta|d",
	}); err != nil {
		t.Fatalf("seed link: %v", err)
	}
	f.users.DisableForTest("u_off", time.Now().UTC())

	cookie := txnCookieFrom(t, f.start(t))
	rec := f.callback(t, "state=state-1&code=abc", cookie)
	assertSSOError(t, rec, ssoErrDisabled)
}

func assertSSOError(t *testing.T, rec *httptest.ResponseRecorder, class string) {
	t.Helper()
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body %s", rec.Code, rec.Body.String())
	}
	want := oidcTestBase + "/login?sso_error=" + class
	if loc := rec.Header().Get("Location"); loc != want {
		t.Errorf("redirected to %q, want %q", loc, want)
	}
}
