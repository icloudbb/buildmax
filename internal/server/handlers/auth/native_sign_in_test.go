package auth

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/audit"
	"github.com/icloudbb/buildmax/internal/testsupport/mockoidc"
)

// testClock starts in the past so a test can step it forward through poll
// intervals and still mint access tokens whose real-time expiry has not
// passed.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock {
	return &testClock{now: time.Now().UTC().Add(-5 * time.Minute)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type nativeServer struct {
	t        *testing.T
	clock    *testClock
	users    *mock.MockUserStore
	sessions *mock.MockAuthSessionStore
	signIns  *mock.MockNativeSignInStore
	audit    *mock.MockAuditStore
	cfg      Config
}

// newNativeServer wires the auth routes over shared in-memory stores, where
// alice signs in with testPassword. Each mux it returns is one replica, or one
// restart under a changed configuration, over those same stores.
func newNativeServer(t *testing.T) *nativeServer {
	t.Helper()
	alice := &coreidentity.User{ID: "u-alice", Email: "alice@example.com", Name: "Alice", Kind: coreidentity.KindHuman}
	users := &mock.MockUserStore{
		ByEmail: map[string]*coreidentity.User{alice.Email: alice},
		ByID:    map[string]*coreidentity.User{alice.ID: alice},
	}
	refresh := &mock.MockRefreshTokenStore{}
	sessions := &mock.MockAuthSessionStore{Refresh: refresh}
	s := &nativeServer{
		t:        t,
		clock:    newTestClock(),
		users:    users,
		sessions: sessions,
		signIns:  &mock.MockNativeSignInStore{Users: users, Sessions: sessions, Refresh: refresh},
		audit:    &mock.MockAuditStore{},
	}
	s.cfg = Config{
		JWTSecret:     refreshTestSecret,
		LocalLogin:    "all",
		Users:         users,
		Passwords:     &mock.MockPasswordStore{Hashes: map[string]string{alice.ID: hashFor(t, testPassword)}},
		RefreshTokens: refresh,
		Sessions:      sessions,
		NativeSignIns: s.signIns,
		Grants:        &mock.MockSystemGrantStore{},
		Audit:         audit.NewRecorder(s.audit),
		Now:           s.clock.Now,
	}
	return s
}

func (s *nativeServer) mux(change func(*Config)) *http.ServeMux {
	cfg := s.cfg
	if change != nil {
		change(&cfg)
	}
	mux := http.NewServeMux()
	New(cfg).Register(mux)
	return mux
}

func call(mux http.Handler, method, path, body, bearer string, headers map[string]string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body %q: %v", rec.Body.String(), err)
	}
	return body.Error
}

func (s *nativeServer) portalToken(mux http.Handler) string {
	s.t.Helper()
	rec := call(mux, http.MethodPost, "/api/auth/portal/login",
		`{"email":"alice@example.com","password":"`+testPassword+`"}`, "", portalHeaders())
	if rec.Code != http.StatusOK {
		s.t.Fatalf("portal login = %d %s", rec.Code, rec.Body.String())
	}
	return decodeJSON(s.t, rec)["access_token"].(string)
}

func (s *nativeServer) start(mux http.Handler, body string) NativeSignInStartResponse {
	s.t.Helper()
	rec := call(mux, http.MethodPost, "/api/auth/native-sign-ins", body, "", nil)
	if rec.Code != http.StatusOK {
		s.t.Fatalf("start = %d %s", rec.Code, rec.Body.String())
	}
	var out NativeSignInStartResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		s.t.Fatal(err)
	}
	return out
}

func pollBody(secret string) string { return `{"poll_secret":"` + secret + `"}` }

// TestNativeSignInEndToEnd drives the server half of §14 through the routes: a
// CLI starts a request, polls through slow_down and authorization_pending, a
// password Portal session looks it up and confirms it on another replica, the
// next poll redeems it, and the token pair it returns works.
func TestNativeSignInEndToEnd(t *testing.T) {
	s := newNativeServer(t)
	logs := captureSlog(t)
	replicaA := s.mux(func(c *Config) { c.PublicBaseURL = "https://buildmax.example.com/" })
	replicaB := s.mux(func(c *Config) { c.PublicBaseURL = "https://buildmax.example.com/" })

	rec := call(replicaA, http.MethodPost, "/api/auth/native-sign-ins", `{"platform":"cli","client_label":"alice-mbp · macOS"}`, "", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("start = %d, Cache-Control %q, %s", rec.Code, rec.Header().Get("Cache-Control"), rec.Body.String())
	}
	var started NativeSignInStartResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	if started.ExpiresIn != 600 || started.Interval != 5 || len(started.UserCode) != 9 ||
		!strings.HasPrefix(started.PollSecret, "bmxsignin_") ||
		started.VerificationURI != "https://buildmax.example.com/#/sign-in" ||
		started.VerificationURIComplete != "https://buildmax.example.com/#/sign-in/"+started.UserCode {
		t.Fatalf("started = %+v", started)
	}

	rec = call(replicaB, http.MethodPost, "/api/auth/native-sign-ins/poll", pollBody(started.PollSecret), "", nil)
	if rec.Code != http.StatusBadRequest || errorCode(t, rec) != "slow_down" {
		t.Fatalf("immediate poll = %d %s, want 400 slow_down", rec.Code, rec.Body.String())
	}
	s.clock.advance(10 * time.Second)
	rec = call(replicaA, http.MethodPost, "/api/auth/native-sign-ins/poll", pollBody(started.PollSecret), "", nil)
	if rec.Code != http.StatusBadRequest || errorCode(t, rec) != "authorization_pending" {
		t.Fatalf("poll before a decision = %d %s, want 400 authorization_pending", rec.Code, rec.Body.String())
	}

	portal := s.portalToken(replicaB)
	rec = call(replicaB, http.MethodGet, "/api/auth/native-sign-ins?code="+strings.ToLower(started.UserCode), "", portal, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("look-up = %d %s", rec.Code, rec.Body.String())
	}
	var view NativeSignInView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Platform != "cli" || view.ClientLabel != "alice-mbp · macOS" || view.SessionExpiresAt.IsZero() ||
		!view.ExpiresAt.Equal(view.CreatedAt.Add(10*time.Minute)) {
		t.Errorf("view = %+v", view)
	}

	rec = call(replicaB, http.MethodPost, "/api/auth/native-sign-ins/confirm", `{"code":"`+started.UserCode+`"}`, portal, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("confirm = %d %s", rec.Code, rec.Body.String())
	}
	s.clock.advance(10 * time.Second)
	rec = call(replicaA, http.MethodPost, "/api/auth/native-sign-ins/poll", pollBody(started.PollSecret), "", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("poll after confirmation = %d %s", rec.Code, rec.Body.String())
	}
	redeemed := decodeJSON(t, rec)
	// The body is POST /api/auth/login's, key for key.
	login := call(replicaA, http.MethodPost, "/api/auth/login", `{"email":"alice@example.com","password":"`+testPassword+`","platform":"cli"}`, "", nil)
	loginBody := decodeJSON(t, login)
	if !slices.Equal(sortedKeys(redeemed), sortedKeys(loginBody)) {
		t.Errorf("redeemed keys %v, login keys %v", sortedKeys(redeemed), sortedKeys(loginBody))
	}
	if user, _ := redeemed["user"].(map[string]any); user["id"] != "u-alice" {
		t.Errorf("redeemed user = %v", redeemed["user"])
	}
	access := redeemed["access_token"].(string)

	// The token works on an authenticated route...
	rec = call(replicaB, http.MethodPost, "/api/auth/password",
		`{"current_password":"`+testPassword+`","new_password":"another long passphrase"}`, access, nil)
	if rec.Code != http.StatusNoContent {
		t.Errorf("password change with the native token = %d %s", rec.Code, rec.Body.String())
	}
	// ...as a CLI session, which cannot confirm another sign-in...
	rec = call(replicaB, http.MethodGet, "/api/auth/native-sign-ins?code=ABCD-EFGH", "", access, nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("look-up with a CLI token = %d, want 403", rec.Code)
	}
	// ...and its refresh token rotates like any login's.
	rec = call(replicaA, http.MethodPost, "/api/auth/token/refresh", `{"refresh_token":"`+redeemed["refresh_token"].(string)+`"}`, "", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("refresh = %d %s", rec.Code, rec.Body.String())
	}

	// Redeemed once.
	s.clock.advance(30 * time.Second)
	rec = call(replicaB, http.MethodPost, "/api/auth/native-sign-ins/poll", pollBody(started.PollSecret), "", nil)
	if rec.Code != http.StatusBadRequest || errorCode(t, rec) != "expired_token" {
		t.Errorf("poll after redemption = %d %s, want expired_token", rec.Code, rec.Body.String())
	}

	if out := logs.String(); strings.Contains(out, started.UserCode) || strings.Contains(out, coreidentity.NormalizeUserCode(started.UserCode)) ||
		strings.Contains(out, started.PollSecret) || strings.Contains(out, access) {
		t.Errorf("a code, secret, or token reached the log:\n%s", out)
	}
}

// TestNativeSignInConfirmedBySSOSession confirms from a Portal session the real
// OIDC flow opened against a real provider, and pins that the native session
// inherits that session's oidc method and exact absolute expiry.
func TestNativeSignInConfirmedBySSOSession(t *testing.T) {
	clock := newTestClock()
	var cfg Config
	d := newSSODeployment(t, mockoidc.Config{}, func(c *Config) {
		c.Now = clock.Now
		c.LocalLogin = "off"
		c.OIDCSessionMaxAge = 12 * time.Hour
		c.NativeSignIns = &mock.MockNativeSignInStore{
			Users: c.Users.(*mock.MockUserStore), Sessions: c.Sessions.(*mock.MockAuthSessionStore),
			Refresh: c.RefreshTokens.(*mock.MockRefreshTokenStore),
		}
		cfg = *c
	})
	sessions := cfg.Sessions.(*mock.MockAuthSessionStore)

	started := d.post("/api/auth/native-sign-ins", `{"platform":"desktop","client_label":"alice-mbp · macOS"}`, "", http.StatusOK)
	var start NativeSignInStartResponse
	if err := json.Unmarshal(started, &start); err != nil {
		t.Fatal(err)
	}
	// No public_base_url here, so the page is on the origin the client called.
	if start.VerificationURIComplete != d.server.URL+"/#/sign-in/"+start.UserCode {
		t.Errorf("verification_uri_complete = %q", start.VerificationURIComplete)
	}
	clock.advance(5 * time.Second)
	if body := d.post("/api/auth/native-sign-ins/poll", pollBody(start.PollSecret), "", http.StatusBadRequest); !strings.Contains(string(body), "authorization_pending") {
		t.Fatalf("poll = %s", body)
	}

	d.signInWithSSO("alice@example.com")
	portal := d.portalAccessToken()
	var portalSession *mock.MockAuthSession
	for _, s := range sessions.Sessions {
		if s.UserID == "u-alice" && s.Platform == "portal" && s.AuthMethod == "oidc" {
			portalSession = s
		}
	}
	if portalSession == nil {
		t.Fatal("SSO opened no oidc Portal session")
	}
	d.post("/api/auth/native-sign-ins/confirm", `{"code":"`+start.UserCode+`"}`, portal, http.StatusNoContent)

	clock.advance(5 * time.Second)
	body := d.post("/api/auth/native-sign-ins/poll", pollBody(start.PollSecret), "", http.StatusOK)
	var redeemed LoginResponse
	if err := json.Unmarshal(body, &redeemed); err != nil {
		t.Fatal(err)
	}
	var native *mock.MockAuthSession
	for _, s := range sessions.Sessions {
		if s.Platform == "desktop" {
			native = s
		}
	}
	if native == nil || native.UserID != "u-alice" || native.AuthMethod != "oidc" || native.ClientLabel != "alice-mbp · macOS" ||
		!native.AbsoluteExpiresAt.Equal(portalSession.AbsoluteExpiresAt) {
		t.Fatalf("native session = %+v; want desktop, oidc, the label, and expiry %v", native, portalSession.AbsoluteExpiresAt)
	}
	// local_login is off, so this token is the only way a native client could
	// have signed in; it works on an authenticated route.
	d.post("/api/auth/password", `{"current_password":"`+testPassword+`","new_password":"another long passphrase"}`, redeemed.AccessToken, http.StatusNoContent)
}

func (d *ssoDeployment) post(path, body, bearer string, want int) []byte {
	d.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, d.server.URL+path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		d.t.Fatal(err)
	}
	defer drain(resp)
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		d.t.Fatalf("POST %s = %d %s, want %d", path, resp.StatusCode, raw, want)
	}
	return raw
}

func (d *ssoDeployment) portalAccessToken() string {
	d.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, d.server.URL+"/api/auth/portal/session", nil)
	req.Header.Set("Origin", d.server.URL)
	resp, err := d.client.Do(req)
	if err != nil {
		d.t.Fatal(err)
	}
	defer drain(resp)
	var body PortalSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.AccessToken == "" {
		d.t.Fatalf("portal session: %v", err)
	}
	return body.AccessToken
}

// TestNativeSignInConfirmRefusals: only a Portal session confirms, and only
// while this deployment still admits the way it signed in.
func TestNativeSignInConfirmRefusals(t *testing.T) {
	s := newNativeServer(t)
	open := s.mux(nil)
	started := s.start(open, `{"platform":"cli"}`)
	code := `{"code":"` + started.UserCode + `"}`

	login := call(open, http.MethodPost, "/api/auth/login", `{"email":"alice@example.com","password":"`+testPassword+`","platform":"desktop"}`, "", nil)
	native := decodeJSON(t, login)["access_token"].(string)
	rec := call(open, http.MethodPost, "/api/auth/native-sign-ins/confirm", code, native, nil)
	if rec.Code != http.StatusForbidden || errorCode(t, rec) != "native sign-in is confirmed from a Portal session" {
		t.Errorf("confirm from a Desktop session = %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(open, http.MethodPost, "/api/auth/native-sign-ins/confirm", code, "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous confirm = %d, want 401", rec.Code)
	}
	// A JSON login cannot label itself a Portal session to get around that.
	forged := call(open, http.MethodPost, "/api/auth/login", `{"email":"alice@example.com","password":"`+testPassword+`","platform":"portal"}`, "", nil)
	if forged.Code != http.StatusBadRequest {
		t.Errorf("JSON login as platform portal = %d %s, want 400", forged.Code, forged.Body.String())
	}

	// A password Portal session from before the operator restricted local login
	// to System Administrators no longer confirms.
	portal := s.portalToken(open)
	tightened := s.mux(func(c *Config) { c.LocalLogin = "system_admins" })
	rec = call(tightened, http.MethodPost, "/api/auth/native-sign-ins/confirm", code, portal, nil)
	if rec.Code != http.StatusForbidden || !strings.Contains(errorCode(t, rec), "sign in to the Portal again") {
		t.Errorf("confirm after local_login tightened = %d %s", rec.Code, rec.Body.String())
	}

	// Under the original configuration it confirms, once.
	if rec := call(open, http.MethodPost, "/api/auth/native-sign-ins/confirm", code, portal, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("confirm = %d %s", rec.Code, rec.Body.String())
	}
	for _, path := range []string{"/api/auth/native-sign-ins/confirm", "/api/auth/native-sign-ins/deny"} {
		rec := call(open, http.MethodPost, path, code, portal, nil)
		if rec.Code != http.StatusNotFound || errorCode(t, rec) != "sign-in code is not valid" {
			t.Errorf("second decision at %s = %d %s", path, rec.Code, rec.Body.String())
		}
	}
	if got := len(s.audit.Events); got == 0 {
		t.Error("nothing audited")
	}
	var confirmed int
	for _, e := range s.audit.Events {
		if e.Action == coreaudit.NativeSignInConfirmed {
			confirmed++
		}
	}
	if confirmed != 1 {
		t.Errorf("%d confirmations audited, want 1", confirmed)
	}
}

// TestNativeSignInRedemptionRefusedAfterDisable: an account disabled between
// confirmation and redemption gets access_denied and no session.
func TestNativeSignInRedemptionRefusedAfterDisable(t *testing.T) {
	s := newNativeServer(t)
	mux := s.mux(nil)
	started := s.start(mux, `{"platform":"cli"}`)
	portal := s.portalToken(mux)
	if rec := call(mux, http.MethodPost, "/api/auth/native-sign-ins/confirm", `{"code":"`+started.UserCode+`"}`, portal, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("confirm = %d %s", rec.Code, rec.Body.String())
	}
	before := len(s.sessions.Sessions)
	at := s.clock.Now()
	s.users.ByID["u-alice"].DisabledAt = &at
	s.clock.advance(30 * time.Second)
	rec := call(mux, http.MethodPost, "/api/auth/native-sign-ins/poll", pollBody(started.PollSecret), "", nil)
	if rec.Code != http.StatusBadRequest || errorCode(t, rec) != "access_denied" {
		t.Errorf("poll = %d %s, want access_denied", rec.Code, rec.Body.String())
	}
	if len(s.sessions.Sessions) != before {
		t.Error("a refused redemption created a session")
	}
}

// TestNativeSignInStartCap: the 501st live pending request is refused with 503
// and Retry-After, and nothing else is affected.
func TestNativeSignInStartCap(t *testing.T) {
	s := newNativeServer(t)
	mux := s.mux(nil)
	for range coreidentity.NativeSignInPendingCap {
		s.start(mux, `{"platform":"desktop"}`)
	}
	rec := call(mux, http.MethodPost, "/api/auth/native-sign-ins", `{"platform":"desktop"}`, "", nil)
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "60" ||
		errorCode(t, rec) != "too many pending sign-in requests" {
		t.Fatalf("501st start = %d, Retry-After %q, %s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
	// Portal sign-in is unaffected.
	s.portalToken(mux)
}

func TestNativeSignInStartRefusesBadInput(t *testing.T) {
	s := newNativeServer(t)
	mux := s.mux(nil)
	for _, body := range []string{
		`{"platform":"portal"}`,
		`{"platform":"cli","client_label":"` + strings.Repeat("x", 65) + `"}`,
		`{"platform":"cli","client_label":"a\u0007b"}`,
		`{"platform":"cli","extra":true}`,
	} {
		if rec := call(mux, http.MethodPost, "/api/auth/native-sign-ins", body, "", nil); rec.Code != http.StatusBadRequest {
			t.Errorf("start %s = %d, want 400", body, rec.Code)
		}
	}
	if rec := call(mux, http.MethodPost, "/api/auth/native-sign-ins/poll", `{}`, "", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("poll without a secret = %d", rec.Code)
	}
	if rec := call(mux, http.MethodPost, "/api/auth/native-sign-ins/poll", pollBody("bmxsignin_nope"), "", nil); errorCode(t, rec) != "expired_token" {
		t.Errorf("unknown secret = %s, want expired_token", rec.Body.String())
	}
	unconfigured := s.mux(func(c *Config) { c.NativeSignIns = nil })
	if rec := call(unconfigured, http.MethodPost, "/api/auth/native-sign-ins", `{"platform":"cli"}`, "", nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("start without a store = %d, want 503", rec.Code)
	}
}

// TestNativeSignInCodeThrottle: ten unknown codes per account per ten minutes,
// then 429 even for a good code, until the window passes.
func TestNativeSignInCodeThrottle(t *testing.T) {
	s := newNativeServer(t)
	mux := s.mux(nil)
	started := s.start(mux, `{"platform":"cli"}`)
	portal := s.portalToken(mux)
	for i := range codeAttemptLimit {
		path := "/api/auth/native-sign-ins?code=ZZZZ-ZZZ" + string(rune('A'+i))
		if i%2 == 1 {
			rec := call(mux, http.MethodPost, "/api/auth/native-sign-ins/deny", `{"code":"ZZZZ-ZZZZ"}`, portal, nil)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("attempt %d = %d", i, rec.Code)
			}
			continue
		}
		if rec := call(mux, http.MethodGet, path, "", portal, nil); rec.Code != http.StatusNotFound {
			t.Fatalf("attempt %d = %d", i, rec.Code)
		}
	}
	rec := call(mux, http.MethodGet, "/api/auth/native-sign-ins?code="+started.UserCode, "", portal, nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("after %d unknown codes = %d, want 429", codeAttemptLimit, rec.Code)
	}
	// The window and a request's life are both ten minutes, so the first
	// request has expired with the failures; a fresh one looks up again.
	s.clock.advance(codeAttemptWindow)
	fresh := s.start(mux, `{"platform":"cli"}`)
	if rec := call(mux, http.MethodGet, "/api/auth/native-sign-ins?code="+fresh.UserCode, "", portal, nil); rec.Code != http.StatusOK {
		t.Errorf("after the window = %d %s", rec.Code, rec.Body.String())
	}
}

func TestLoginAcceptsClientLabel(t *testing.T) {
	s := newNativeServer(t)
	mux := s.mux(nil)
	rec := call(mux, http.MethodPost, "/api/auth/login",
		`{"email":"alice@example.com","password":"`+testPassword+`","platform":"cli","client_label":"build-box · Linux"}`, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d %s", rec.Code, rec.Body.String())
	}
	found := false
	for _, sess := range s.sessions.Sessions {
		found = found || (sess.Platform == "cli" && sess.ClientLabel == "build-box · Linux")
	}
	if !found {
		t.Error("the login's session does not carry its label")
	}
	rec = call(mux, http.MethodPost, "/api/auth/login",
		`{"email":"alice@example.com","password":"`+testPassword+`","client_label":"`+strings.Repeat("x", 65)+`"}`, "", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("login with a long label = %d, want 400", rec.Code)
	}
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// captureSlog collects every record logged while the test runs.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}
