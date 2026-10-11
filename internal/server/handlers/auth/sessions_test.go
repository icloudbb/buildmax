package auth

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	"github.com/icloudbb/buildmax/internal/mock"
)

// addBob gives the native test server a second account that signs in with the
// same password, so a test can act across accounts.
func (s *nativeServer) addBob() {
	s.t.Helper()
	bob := &coreidentity.User{ID: "u-bob", Email: "bob@example.com", Name: "Bob", Kind: coreidentity.KindHuman}
	s.users.ByEmail[bob.Email] = bob
	s.users.ByID[bob.ID] = bob
	s.cfg.Passwords.(*mock.MockPasswordStore).Hashes[bob.ID] = hashFor(s.t, testPassword)
}

func (s *nativeServer) jsonLogin(mux http.Handler, email string) (access, refresh string) {
	s.t.Helper()
	rec := call(mux, http.MethodPost, "/api/auth/login",
		`{"email":"`+email+`","password":"`+testPassword+`","platform":"desktop"}`, "", nil)
	if rec.Code != http.StatusOK {
		s.t.Fatalf("login %s = %d %s", email, rec.Code, rec.Body.String())
	}
	body := decodeJSON(s.t, rec)
	return body["access_token"].(string), body["refresh_token"].(string)
}

// nativeSession signs a CLI in through Portal confirmation and returns its
// token pair.
func (s *nativeServer) nativeSession(mux http.Handler, portal, label string) (access, refresh string) {
	s.t.Helper()
	started := s.start(mux, `{"platform":"cli","client_label":"`+label+`"}`)
	if rec := call(mux, http.MethodPost, "/api/auth/native-sign-ins/confirm", `{"code":"`+started.UserCode+`"}`, portal, nil); rec.Code != http.StatusNoContent {
		s.t.Fatalf("confirm = %d %s", rec.Code, rec.Body.String())
	}
	s.clock.advance(10 * time.Second)
	rec := call(mux, http.MethodPost, "/api/auth/native-sign-ins/poll", pollBody(started.PollSecret), "", nil)
	if rec.Code != http.StatusOK {
		s.t.Fatalf("poll = %d %s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(s.t, rec)
	return body["access_token"].(string), body["refresh_token"].(string)
}

func listSessions(t *testing.T, mux http.Handler, bearer string) []Session {
	t.Helper()
	rec := call(mux, http.MethodGet, "/api/auth/sessions", "", bearer, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d %s", rec.Code, rec.Body.String())
	}
	var out SessionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Sessions
}

func (s *nativeServer) revokedAudits() []coreaudit.Event {
	var out []coreaudit.Event
	for _, e := range s.audit.Events {
		if e.Action == coreaudit.SessionRevoked {
			out = append(out, e)
		}
	}
	return out
}

// TestSelfServiceRevokeNativeSession is the task's acceptance path: a CLI session
// from native sign-in is listed to its owner with platform cli and its label,
// and revoking it stops both its access and its refresh token at once.
func TestSelfServiceRevokeNativeSession(t *testing.T) {
	s := newNativeServer(t)
	mux := s.mux(nil)
	portal := s.portalToken(mux)
	cliAccess, cliRefresh := s.nativeSession(mux, portal, "build-box · Linux")

	rec := call(mux, http.MethodGet, "/api/auth/sessions", "", portal, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d %s", rec.Code, rec.Body.String())
	}
	if raw := rec.Body.String(); strings.Contains(raw, cliAccess) || strings.Contains(raw, cliRefresh) ||
		strings.Contains(raw, "token") || strings.Contains(raw, "hash") {
		t.Fatalf("the list carries token material: %s", raw)
	}
	sessions := listSessions(t, mux, portal)
	if len(sessions) != 2 {
		t.Fatalf("sessions = %+v, want the Portal and the CLI session", sessions)
	}
	var cli, current *Session
	for i := range sessions {
		if sessions[i].Platform == "cli" {
			cli = &sessions[i]
		}
		if sessions[i].Current {
			if current != nil {
				t.Fatal("two sessions are marked current")
			}
			current = &sessions[i]
		}
	}
	if cli == nil || cli.ClientLabel != "build-box · Linux" || cli.AuthMethod != "password" ||
		cli.Current || cli.CreatedAt.IsZero() || cli.ExpiresAt.IsZero() {
		t.Fatalf("CLI session = %+v", cli)
	}
	if current == nil || current.Platform != "portal" {
		t.Fatalf("current = %+v, want the Portal session", current)
	}
	// The CLI sees the same list with itself marked.
	for _, sess := range listSessions(t, mux, cliAccess) {
		if sess.Current != (sess.SessionID == cli.SessionID) {
			t.Errorf("from the CLI, %s current = %v", sess.SessionID, sess.Current)
		}
	}

	if rec := call(mux, http.MethodDelete, "/api/auth/sessions/"+cli.SessionID, "", portal, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke = %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(mux, http.MethodGet, "/api/auth/sessions", "", cliAccess, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("revoked CLI access token = %d, want 401", rec.Code)
	}
	if rec := call(mux, http.MethodPost, "/api/auth/token/refresh", `{"refresh_token":"`+cliRefresh+`"}`, "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("revoked CLI refresh token = %d, want 401", rec.Code)
	}
	// The Portal session is untouched and no longer lists the CLI.
	if left := listSessions(t, mux, portal); len(left) != 1 || !left[0].Current {
		t.Errorf("after revoke = %+v", left)
	}
	events := s.revokedAudits()
	if len(events) != 1 || events[0].ActorType != coreaudit.ActorUser || events[0].ActorID != "u-alice" ||
		events[0].TargetID != "u-alice" || events[0].Detail != cli.SessionID {
		t.Errorf("audit = %+v", events)
	}
	// Revoking it again is a missing session.
	if rec := call(mux, http.MethodDelete, "/api/auth/sessions/"+cli.SessionID, "", portal, nil); rec.Code != http.StatusNotFound {
		t.Errorf("second revoke = %d, want 404", rec.Code)
	}
}

// TestSelfServiceSessionsAreOwnAccountOnly: a person sees only their own
// sessions, and another account's session id is indistinguishable from one
// that does not exist.
func TestSelfServiceSessionsAreOwnAccountOnly(t *testing.T) {
	s := newNativeServer(t)
	s.addBob()
	mux := s.mux(nil)
	aliceAccess, _ := s.jsonLogin(mux, "alice@example.com")
	bobAccess, _ := s.jsonLogin(mux, "bob@example.com")

	aliceSessions := listSessions(t, mux, aliceAccess)
	if len(aliceSessions) != 1 {
		t.Fatalf("alice sessions = %+v", aliceSessions)
	}
	aliceSID := aliceSessions[0].SessionID
	for _, sess := range listSessions(t, mux, bobAccess) {
		if sess.SessionID == aliceSID {
			t.Fatal("bob's list includes alice's session")
		}
	}

	cross := call(mux, http.MethodDelete, "/api/auth/sessions/"+aliceSID, "", bobAccess, nil)
	missing := call(mux, http.MethodDelete, "/api/auth/sessions/no-such-session", "", bobAccess, nil)
	if cross.Code != http.StatusNotFound || missing.Code != http.StatusNotFound || cross.Body.String() != missing.Body.String() {
		t.Errorf("cross-account revoke = %d %s; missing = %d %s; want identical 404s",
			cross.Code, cross.Body.String(), missing.Code, missing.Body.String())
	}
	if rec := call(mux, http.MethodGet, "/api/auth/sessions", "", aliceAccess, nil); rec.Code != http.StatusOK {
		t.Errorf("alice after bob's attempt = %d, want her session still live", rec.Code)
	}
	if got := s.revokedAudits(); len(got) != 0 {
		t.Errorf("a refused revoke was audited: %+v", got)
	}

	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/auth/sessions"},
		{http.MethodDelete, "/api/auth/sessions/" + aliceSID},
	} {
		if rec := call(mux, c.method, c.path, "", "", nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("anonymous %s %s = %d, want 401", c.method, c.path, rec.Code)
		}
	}
}

// TestSelfServiceRevokeCurrentSession: revoking the session that makes the
// request signs that caller out.
func TestSelfServiceRevokeCurrentSession(t *testing.T) {
	s := newNativeServer(t)
	mux := s.mux(nil)
	access, _ := s.jsonLogin(mux, "alice@example.com")
	sid := listSessions(t, mux, access)[0].SessionID
	if rec := call(mux, http.MethodDelete, "/api/auth/sessions/"+sid, "", access, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke current = %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(mux, http.MethodGet, "/api/auth/sessions", "", access, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("after revoking itself = %d, want 401", rec.Code)
	}
}

func TestSelfServiceSessionsNotConfigured(t *testing.T) {
	s := newNativeServer(t)
	// Without a session store the guard cannot check sessions either, so the
	// token is accepted and the route reports the missing store.
	mux := s.mux(func(c *Config) { c.Sessions = nil; c.NativeSignIns = nil })
	access, _ := s.jsonLogin(mux, "alice@example.com")
	if rec := call(mux, http.MethodGet, "/api/auth/sessions", "", access, nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("list without a store = %d %s, want 503", rec.Code, rec.Body.String())
	}
}
