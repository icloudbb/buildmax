package desktop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/interface/auth"
	"github.com/icloudbb/buildmax/internal/interface/client"
	"github.com/icloudbb/buildmax/internal/testsupport/authserver"
)

// window stands in for the two window operations a browser sign-in uses. No
// test may open a real browser or touch the real clipboard.
type window struct {
	mu     sync.Mutex
	opened []string
	copied []string
}

func (w *window) open(_ context.Context, url string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.opened = append(w.opened, url)
}

func (w *window) copy(_ context.Context, text string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.copied = append(w.copied, text)
	return nil
}

func (w *window) openedURLs() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.opened...)
}

// signInApp is an app in a fresh home whose events and window are recorded.
func signInApp(t *testing.T) (*App, *uiEvents, *window) {
	t.Helper()
	t.Setenv(config.EnvKeyBuildmaxHome, t.TempDir())
	t.Setenv(config.EnvKeyBuildmaxCredentialStore, "file")
	events := &uiEvents{}
	win := &window{}
	app := NewApp()
	app.emit = events.emit
	app.signIns.open = win.open
	app.signIns.copy = win.copy
	t.Cleanup(app.CancelBrowserSignIn)
	return app, events, win
}

func outcome(t *testing.T, data any) BrowserSignInResultPayload {
	t.Helper()
	result, ok := data.(BrowserSignInResultPayload)
	if !ok {
		t.Fatalf("event data = %T %+v", data, data)
	}
	return result
}

// The bridge end to end against the real sign-in routes, on a deployment where
// SSO is the only way in: the page reads the methods, starts a request, the
// browser opens at the confirmation page, a Portal SSO session confirms it, and
// Desktop is signed in with a login that signs out like any other.
func TestBridgeSignsInThroughTheBrowserWhereOnlySSOIsAllowed(t *testing.T) {
	server := authserver.Start(t, authserver.Options{LocalLogin: "off", OIDC: true})
	app, events, win := signInApp(t)

	methods, err := app.GetAuthMethods(server.URL + "/")
	if err != nil {
		t.Fatalf("GetAuthMethods: %v", err)
	}
	if methods.LocalLogin != "off" || !methods.OIDC.Enabled || methods.OIDC.DisplayName != "Mock IdP" {
		t.Fatalf("methods = %+v", methods)
	}
	if _, err := app.DoLoginWithPassword(server.URL, authserver.Email, authserver.Password); err == nil {
		t.Fatal("a password sign-in was accepted under local_login: off")
	}

	started, err := app.StartBrowserSignIn(" " + server.URL + " ")
	if err != nil {
		t.Fatalf("StartBrowserSignIn: %v", err)
	}
	if want := server.URL + "/#/sign-in/" + started.UserCode; started.VerificationURIComplete != want {
		t.Errorf("complete URI = %q, want %q", started.VerificationURIComplete, want)
	}
	if opened := win.openedURLs(); len(opened) != 1 || opened[0] != started.VerificationURIComplete {
		t.Errorf("opened %v, want the complete URI once", opened)
	}
	if events.seen(eventBrowserSignIn) {
		t.Fatal("an outcome arrived before anyone decided")
	}

	server.Confirm(started.UserCode, "oidc")
	result := outcome(t, events.waitFor(t, eventBrowserSignIn))
	if result.Outcome != browserSignInSignedIn || result.Status == nil || result.Status.Email != authserver.Email {
		t.Fatalf("outcome = %+v", result)
	}
	creds, err := auth.StoredLogin()
	if err != nil || creds == nil || creds.Email != authserver.Email || creds.ServerURL != server.URL {
		t.Fatalf("stored login = %+v, %v", creds, err)
	}
	sessions := server.ActiveNativeSessions()
	if len(sessions) != 1 || sessions[0].Platform != "desktop" || sessions[0].AuthMethod != "oidc" ||
		sessions[0].ClientLabel != client.ClientLabel() {
		t.Fatalf("native sessions = %+v; want one desktop oidc session with this machine's label", sessions)
	}
	if err := app.OpenBrowserSignInAgain(); err == nil {
		t.Error("reopening succeeded after the sign-in ended")
	}

	if err := app.Logout(); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if left := server.ActiveNativeSessions(); len(left) != 0 {
		t.Errorf("sessions after signing out = %+v, want the session revoked", left)
	}
}

// scriptedSignIns answers start with a one-second interval and each poll with
// the next answer, repeating the last.
type scriptedSignIns struct {
	mu      sync.Mutex
	answers []string
	polls   int
	labels  []string
}

func (s *scriptedSignIns) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		defer s.mu.Unlock()
		switch r.URL.Path {
		case "/api/auth/native-sign-ins":
			s.labels = append(s.labels, body["platform"]+"|"+body["client_label"])
			_, _ = w.Write([]byte(`{"user_code":"WDJB-MJHT","poll_secret":"bmxsignin_x","expires_in":600,"interval":1,` +
				`"verification_uri":"http://portal.test/#/sign-in","verification_uri_complete":"http://portal.test/#/sign-in/WDJB-MJHT"}`))
		case "/api/auth/native-sign-ins/poll":
			answer := s.answers[min(s.polls, len(s.answers)-1)]
			s.polls++
			if answer == "ok" {
				_, _ = w.Write([]byte(`{"access_token":"at","refresh_token":"rt","expires_in":900,"user":{"id":"u_1","email":"alice@example.com"}}`))
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"` + answer + `"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (s *scriptedSignIns) pollCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.polls
}

// A denial and an expiry each end the wait with their own outcome, and leave
// the app signed out.
func TestBrowserSignInReportsDenialAndExpiry(t *testing.T) {
	for answer, want := range map[string]string{
		"access_denied": browserSignInDenied,
		"expired_token": browserSignInExpired,
	} {
		t.Run(answer, func(t *testing.T) {
			script := &scriptedSignIns{answers: []string{"authorization_pending", answer}}
			srv := script.serve(t)
			app, events, _ := signInApp(t)

			if _, err := app.StartBrowserSignIn(srv.URL); err != nil {
				t.Fatalf("StartBrowserSignIn: %v", err)
			}
			result := outcome(t, events.waitFor(t, eventBrowserSignIn))
			if result.Outcome != want || result.Status != nil {
				t.Errorf("outcome = %+v, want %s", result, want)
			}
			if creds, _ := auth.StoredLogin(); creds != nil {
				t.Errorf("a login was saved: %+v", creds)
			}
			if script.labels[0] != "desktop|"+client.ClientLabel() {
				t.Errorf("started as %q", script.labels[0])
			}
		})
	}
}

// Cancel stops polling at once and reports nothing; a confirmation arriving
// afterwards finds no one to redeem it, so no session is created.
func TestCancellingABrowserSignInStopsPolling(t *testing.T) {
	script := &scriptedSignIns{answers: []string{"authorization_pending", "ok"}}
	srv := script.serve(t)
	app, events, win := signInApp(t)

	if _, err := app.StartBrowserSignIn(srv.URL); err != nil {
		t.Fatalf("StartBrowserSignIn: %v", err)
	}
	if err := app.OpenBrowserSignInAgain(); err != nil {
		t.Fatalf("OpenBrowserSignInAgain: %v", err)
	}
	if err := app.CopyBrowserSignInLink(); err != nil {
		t.Fatalf("CopyBrowserSignInLink: %v", err)
	}
	const link = "http://portal.test/#/sign-in/WDJB-MJHT"
	if opened := win.openedURLs(); len(opened) != 2 || opened[1] != link {
		t.Errorf("opened %v, want the link twice", opened)
	}
	if len(win.copied) != 1 || win.copied[0] != link {
		t.Errorf("copied %v", win.copied)
	}

	app.CancelBrowserSignIn()
	polled := script.pollCount()
	time.Sleep(2500 * time.Millisecond)
	if after := script.pollCount(); after != polled {
		t.Errorf("polled %d more times after Cancel", after-polled)
	}
	if events.seen(eventBrowserSignIn) {
		t.Errorf("a cancelled sign-in reported an outcome:\n%s", events.summary())
	}
	if creds, _ := auth.StoredLogin(); creds != nil {
		t.Errorf("a login was saved: %+v", creds)
	}
	if err := app.CopyBrowserSignInLink(); err == nil || !strings.Contains(err.Error(), "no browser sign-in is waiting") {
		t.Errorf("copy after Cancel = %v", err)
	}
}

// A server that cannot be reached is an error, which the page shows as the
// unreachable-server state rather than guessing which methods to offer.
func TestAuthMethodsOfAnUnreachableServerIsAnError(t *testing.T) {
	app, _, _ := signInApp(t)
	if _, err := app.GetAuthMethods("http://127.0.0.1:1"); err == nil {
		t.Fatal("GetAuthMethods succeeded against nothing")
	}
}
