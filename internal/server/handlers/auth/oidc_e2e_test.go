package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	infraoidc "github.com/icloudbb/buildmax/internal/infra/oidc"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/audit"
	"github.com/icloudbb/buildmax/internal/testsupport/mockoidc"
)

// The tests above stand a fake in for the provider. This one runs the real
// relying party — discovery, PKCE, client_secret_basic, ID Token verification —
// against a real OIDC provider over TLS, through the registered routes and a
// browser-like cookie jar, on a deployment that also keeps native login. It is
// the in-process half of the claim that SSO and password/login-code sign-in
// coexist; the kind smoke and Portal browser suite are the deployed half.

const (
	e2eClientID     = "buildmax-e2e"
	e2eClientSecret = "e2e-client-secret"
	e2eDomain       = "example.com"
)

type ssoDeployment struct {
	t      *testing.T
	server *httptest.Server
	client *http.Client
	users  *mock.MockUserStore
}

func newSSODeployment(t *testing.T, idpCfg mockoidc.Config) *ssoDeployment {
	t.Helper()
	// The IdP's URL is only known once it listens, and the provider needs it as
	// its issuer, so the handler is attached after the listener starts.
	var idpHandler http.Handler
	idp := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { idpHandler.ServeHTTP(w, r) }))
	t.Cleanup(idp.Close)
	idpCfg.Issuer, idpCfg.ClientID, idpCfg.ClientSecret = idp.URL, e2eClientID, e2eClientSecret
	provider, err := mockoidc.New(idpCfg)
	if err != nil {
		t.Fatal(err)
	}
	idpHandler = provider.Handler()

	alice := &coreidentity.User{ID: "u-alice", Email: "alice@" + e2eDomain, Name: "Alice"}
	bob := &coreidentity.User{ID: "u-bob", Email: "bob@" + e2eDomain, Name: "Bob"}
	users := &mock.MockUserStore{
		ByEmail: map[string]*coreidentity.User{alice.Email: alice, bob.Email: bob},
		ByID:    map[string]*coreidentity.User{alice.ID: alice, bob.ID: bob},
	}
	refresh := &mock.MockRefreshTokenStore{}
	mux := http.NewServeMux()
	bmx := httptest.NewServer(mux)
	t.Cleanup(bmx.Close)
	New(Config{
		JWTSecret:       "oidc-e2e-secret",
		LocalLogin:      "all",
		OIDCEnabled:     true,
		OIDCDisplayName: "Mock IdP",
		OIDC: infraoidc.New(infraoidc.Config{
			Issuer: idp.URL, ClientID: e2eClientID, ClientSecret: e2eClientSecret, HTTPClient: idp.Client(),
		}),
		PublicBaseURL:       bmx.URL,
		Provisioning:        identityProvisioningJIT,
		AllowedEmailDomains: []string{e2eDomain},
		Users:               users,
		ExternalIdentities:  &mock.MockExternalIdentityStore{Users: users},
		Passwords:           &mock.MockPasswordStore{Hashes: map[string]string{alice.ID: hashFor(t, testPassword)}},
		LoginCodes: &mock.MockLoginCodeStore{Codes: map[string]*mock.MockLoginCode{
			"bob-code": {UserID: bob.ID, ExpiresAt: time.Now().Add(time.Hour)},
		}},
		Sessions:      &mock.MockAuthSessionStore{Refresh: refresh},
		RefreshTokens: refresh,
		Audit:         audit.NewRecorder(&mock.MockAuditStore{}),
	}).Register(mux)

	return &ssoDeployment{t: t, server: bmx, users: users, client: newBrowser(t, idp)}
}

// newBrowser is a client that keeps cookies per origin and trusts the IdP's
// certificate, and stops at every redirect so each hop can be checked.
func newBrowser(t *testing.T, idp *httptest.Server) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{
		Jar:           jar,
		Transport:     idp.Client().Transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (d *ssoDeployment) get(rawURL string) *http.Response {
	d.t.Helper()
	resp, err := d.client.Get(rawURL)
	if err != nil {
		d.t.Fatalf("GET %s: %v", rawURL, err)
	}
	return resp
}

// signInWithSSO walks the browser flow: start, the IdP's sign-in form, the
// callback. It returns where the callback sent the browser.
func (d *ssoDeployment) signInWithSSO(email string) *url.URL {
	d.t.Helper()
	start := d.get(d.server.URL + "/api/auth/oidc/start")
	drain(start)
	if start.StatusCode != http.StatusFound {
		d.t.Fatalf("start status = %d, want 302", start.StatusCode)
	}
	authorize, err := url.Parse(start.Header.Get("Location"))
	if err != nil {
		d.t.Fatal(err)
	}
	form := d.get(authorize.String())
	drain(form)
	if form.StatusCode != http.StatusOK {
		d.t.Fatalf("IdP sign-in form status = %d", form.StatusCode)
	}

	// Submit what the form carries: the authorize parameters, plus who the
	// person says they are.
	values := authorize.Query()
	values.Set("email", email)
	values.Set("email_verified", "true")
	values.Set("decision", "allow")
	submit, err := d.client.PostForm(authorize.Scheme+"://"+authorize.Host+authorize.Path, values)
	if err != nil {
		d.t.Fatal(err)
	}
	drain(submit)
	if submit.StatusCode != http.StatusFound {
		d.t.Fatalf("IdP submit status = %d, want 302", submit.StatusCode)
	}

	callback := d.get(submit.Header.Get("Location"))
	drain(callback)
	if callback.StatusCode != http.StatusFound {
		d.t.Fatalf("callback status = %d, want 302", callback.StatusCode)
	}
	landed, err := url.Parse(callback.Header.Get("Location"))
	if err != nil {
		d.t.Fatal(err)
	}
	return landed
}

// portalSession exchanges the Portal refresh cookie the way the Portal does on
// load, and returns whose session it is.
func (d *ssoDeployment) portalSession() string {
	d.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, d.server.URL+"/api/auth/portal/session", nil)
	req.Header.Set("Origin", d.server.URL)
	resp, err := d.client.Do(req)
	if err != nil {
		d.t.Fatal(err)
	}
	return d.subjectOf(resp)
}

func (d *ssoDeployment) portalLogin(body string) string {
	d.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, d.server.URL+"/api/auth/portal/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", d.server.URL)
	resp, err := d.client.Do(req)
	if err != nil {
		d.t.Fatal(err)
	}
	return d.subjectOf(resp)
}

// subjectOf reads the user an access-token response names. The token is the
// server's own; the test only needs its sub claim, not to re-verify it.
func (d *ssoDeployment) subjectOf(resp *http.Response) string {
	d.t.Helper()
	defer drain(resp)
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		d.t.Fatalf("status = %d, body %s", resp.StatusCode, raw)
	}
	var body struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || body.AccessToken == "" {
		d.t.Fatalf("no access token in %s", raw)
	}
	parts := strings.Split(body.AccessToken, ".")
	if len(parts) != 3 {
		d.t.Fatalf("access token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		d.t.Fatal(err)
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		d.t.Fatal(err)
	}
	return claims.Sub
}

func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func TestSSOAndNativeLoginCoexistAgainstARealProvider(t *testing.T) {
	d := newSSODeployment(t, mockoidc.Config{})

	methods := d.get(d.server.URL + "/api/auth/methods")
	var advertised MethodsResponse
	if err := json.NewDecoder(methods.Body).Decode(&advertised); err != nil {
		t.Fatal(err)
	}
	drain(methods)
	if advertised.LocalLogin != "all" || !advertised.OIDC.Enabled || advertised.OIDC.DisplayName != "Mock IdP" {
		t.Fatalf("methods = %+v; want native login and SSO both offered", advertised)
	}

	// Native login is untouched by SSO being on.
	if sub := d.portalLogin(`{"email":"alice@example.com","password":"` + testPassword + `"}`); sub != "u-alice" {
		t.Errorf("password sign-in opened a session for %q, want u-alice", sub)
	}
	if sub := d.portalLogin(`{"email":"bob@example.com","otp":"bob-code"}`); sub != "u-bob" {
		t.Errorf("login-code sign-in opened a session for %q, want u-bob", sub)
	}

	// An operator-created account signs in through the IdP and is the same
	// account, not a second one.
	if landed := d.signInWithSSO("alice@example.com"); landed.String() != d.server.URL+"/" {
		t.Fatalf("SSO landed on %s, want the Portal root", landed)
	}
	if sub := d.portalSession(); sub != "u-alice" {
		t.Errorf("SSO as alice opened a session for %q, want the existing u-alice", sub)
	}
	// And still has her password afterwards.
	if sub := d.portalLogin(`{"email":"alice@example.com","password":"` + testPassword + `"}`); sub != "u-alice" {
		t.Errorf("password sign-in after SSO opened a session for %q, want u-alice", sub)
	}

	// Someone with no account yet, from an allowed domain, is provisioned.
	d.signInWithSSO("carol@example.com")
	carol, _ := d.users.UserByEmail(t.Context(), "carol@example.com")
	if carol == nil {
		t.Fatal("first SSO sign-in from an allowed domain did not provision an account")
	}
	if sub := d.portalSession(); sub != carol.ID {
		t.Errorf("SSO as carol opened a session for %q, want %q", sub, carol.ID)
	}

	// A domain outside the allow list is refused with the coarse class only.
	landed := d.signInWithSSO("mallory@elsewhere.test")
	if landed.Path != "/login" || landed.Query().Get("sso_error") != ssoErrNotAuthorized {
		t.Errorf("disallowed domain landed on %s, want /login?sso_error=%s", landed, ssoErrNotAuthorized)
	}
	if u, _ := d.users.UserByEmail(t.Context(), "mallory@elsewhere.test"); u != nil {
		t.Error("a disallowed domain was provisioned")
	}
}

// Okta's org authorization server puts email in the ID Token but answers
// email_verified only at UserInfo. Reading the absent claim as false refused
// every first Okta sign-in, verified or not.
func TestSSOReadsEmailVerifiedFromUserInfoWhenTheIDTokenOmitsIt(t *testing.T) {
	d := newSSODeployment(t, mockoidc.Config{EmailVerifiedOnlyAtUserInfo: true})

	if landed := d.signInWithSSO("alice@example.com"); landed.String() != d.server.URL+"/" {
		t.Fatalf("SSO landed on %s, want the Portal root", landed)
	}
	if sub := d.portalSession(); sub != "u-alice" {
		t.Errorf("SSO as alice opened a session for %q, want the existing u-alice", sub)
	}
}
