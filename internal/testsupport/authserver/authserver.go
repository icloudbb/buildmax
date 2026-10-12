// Package authserver serves the real sign-in routes over in-memory stores, for
// the CLI and Desktop suites that sign in end to end.
//
// Everything under the HTTP boundary is the production auth handler and
// identity service; only the stores are the in-memory mocks. A test plays the
// person in the Portal through Confirm and Deny, from a Portal session it
// opens directly, so no browser is involved.
package authserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	"github.com/icloudbb/buildmax/internal/infra/llmwire"
	infraoidc "github.com/icloudbb/buildmax/internal/infra/oidc"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/server/access"
	authroutes "github.com/icloudbb/buildmax/internal/server/handlers/auth"
	"github.com/icloudbb/buildmax/internal/server/httputil"
	"github.com/icloudbb/buildmax/internal/service/audit"
	"github.com/icloudbb/buildmax/internal/testsupport"
)

const (
	// Email and Password sign the one account in.
	Email    = "alice@example.com"
	Password = "correct horse battery staple"
	// UserID is that account's id.
	UserID = "u-alice"

	secret = "authserver-test-secret"
)

// Options shape the deployment.
type Options struct {
	// LocalLogin is "all" (the default), "system_admins", or "off".
	LocalLogin string
	// OIDC advertises SSO and admits a Portal SSO session's confirmation.
	OIDC bool
	// AccessTokenTTL is how long an access token lasts; zero is the server's
	// default. A short one makes the client refresh.
	AccessTokenTTL time.Duration
}

// Server is a running deployment.
type Server struct {
	t        testing.TB
	URL      string
	sessions *mock.MockAuthSessionStore

	mu     sync.Mutex
	offset time.Duration
}

// Start serves the auth routes, and a one-model GET /api/llm/models so a
// signed-in client can prove its login, until the test ends.
func Start(t testing.TB, opts Options) *Server {
	t.Helper()
	hash, err := coreidentity.HashPassword(Password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	alice := &coreidentity.User{ID: UserID, Email: Email, Name: "Alice", Kind: coreidentity.KindHuman}
	users := &mock.MockUserStore{
		ByEmail: map[string]*coreidentity.User{alice.Email: alice},
		ByID:    map[string]*coreidentity.User{alice.ID: alice},
	}
	refresh := &mock.MockRefreshTokenStore{}
	sessions := &mock.MockAuthSessionStore{Refresh: refresh}
	s := &Server{t: t, sessions: sessions}
	local := opts.LocalLogin
	if local == "" {
		local = "all"
	}
	cfg := authroutes.Config{
		JWTSecret:      secret,
		LocalLogin:     local,
		OIDCEnabled:    opts.OIDC,
		AccessTokenTTL: opts.AccessTokenTTL,
		Users:          users,
		Passwords:      &mock.MockPasswordStore{Hashes: map[string]string{alice.ID: hash}},
		RefreshTokens:  refresh,
		Sessions:       sessions,
		NativeSignIns:  &mock.MockNativeSignInStore{Users: users, Sessions: sessions, Refresh: refresh},
		Grants:         &mock.MockSystemGrantStore{},
		Audit:          audit.NewRecorder(&mock.MockAuditStore{}),
		Now:            s.now,
	}
	if opts.OIDC {
		cfg.OIDCDisplayName = "Mock IdP"
		cfg.OIDC = unusedSSO{}
	}
	mux := http.NewServeMux()
	authroutes.New(cfg).Register(mux)
	guard := &access.Guard{JWTSecret: secret, Users: users, Sessions: sessions, Now: s.now}
	mux.HandleFunc("GET "+llmwire.ModelsPath, func(w http.ResponseWriter, r *http.Request) {
		if _, ok := guard.ActiveUser(w, r); !ok {
			return
		}
		httputil.WriteJSON(w, http.StatusOK, llmwire.ModelsResponse{Models: []llmwire.Model{
			{Name: "Fast", ContextWindow: 128000, Default: true, Capabilities: []string{}},
		}})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	s.URL = server.URL
	return s
}

func (s *Server) now() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Now().Add(s.offset)
}

// Advance moves the server's clock forward, as a request's lifetime passing.
func (s *Server) Advance(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.offset += d
}

// portalSession opens a Portal session for the account, signed in by method,
// as the Portal's sign-in would, and returns its access token.
func (s *Server) portalSession(method string) string {
	s.t.Helper()
	sid, err := s.sessions.CreateSession(context.Background(), coreidentity.NewAuthSession{
		UserID: UserID, Platform: coreidentity.PlatformPortal, AuthMethod: method,
		AbsoluteExpiresAt: s.now().Add(12 * time.Hour),
	})
	if err != nil {
		s.t.Fatalf("open a Portal session: %v", err)
	}
	return testsupport.SignJWTWithSID(UserID, sid, secret)
}

// Confirm confirms the request with this code from a Portal session signed in
// by method ("password" or "oidc"), as the person clicking Confirm sign-in.
func (s *Server) Confirm(code, method string) {
	s.t.Helper()
	s.decide("confirm", code, method)
}

// Deny denies it, as the person clicking Deny.
func (s *Server) Deny(code string) {
	s.t.Helper()
	s.decide("deny", code, "password")
}

func (s *Server) decide(decision, code, method string) {
	body, _ := json.Marshal(map[string]string{"code": code})
	req, err := http.NewRequest(http.MethodPost, s.URL+"/api/auth/native-sign-ins/"+decision, bytes.NewReader(body))
	if err != nil {
		s.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.portalSession(method))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatalf("%s %s: %v", decision, code, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		var refused map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&refused)
		s.t.Fatalf("%s %s = %d %v", decision, code, resp.StatusCode, refused)
	}
}

// ActiveNativeSessions lists the CLI and Desktop sessions the server still
// honours; a logged-out or revoked one is gone from it.
func (s *Server) ActiveNativeSessions() []coreidentity.AuthSession {
	s.t.Helper()
	all, err := s.sessions.ListUserSessions(context.Background(), UserID, s.now())
	if err != nil {
		s.t.Fatalf("list sessions: %v", err)
	}
	var out []coreidentity.AuthSession
	for _, sess := range all {
		if sess.Platform != coreidentity.PlatformPortal {
			out = append(out, sess)
		}
	}
	return out
}

// unusedSSO stands for a configured provider, which is what makes SSO
// enabled. Nothing signs in through it: the Portal SSO session a confirmation
// needs is opened directly (see Confirm).
type unusedSSO struct{}

func (unusedSSO) BeginAuth(context.Context, string, []string) (string, infraoidc.Transaction, error) {
	return "", infraoidc.Transaction{}, errors.New("this test deployment signs in through no IdP")
}

func (unusedSSO) Complete(context.Context, string, infraoidc.Transaction, string, []string) (*infraoidc.Claims, error) {
	return nil, errors.New("this test deployment signs in through no IdP")
}

// String names the server in a failure message.
func (s *Server) String() string { return fmt.Sprintf("auth server at %s", s.URL) }
