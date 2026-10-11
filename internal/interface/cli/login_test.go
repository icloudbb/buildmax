package cli

import (
	"bytes"
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/interface/auth"
	"github.com/icloudbb/buildmax/internal/testsupport/authserver"
)

func TestBrowserOnThisScreen(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		goos string
		want bool
	}{
		{"a Mac", nil, "darwin", true},
		{"Windows", nil, "windows", true},
		{"a Linux desktop", map[string]string{"DISPLAY": ":0"}, "linux", true},
		{"a Wayland session", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, "linux", true},
		{"a Linux host with no display", nil, "linux", false},
		{"a BSD host with no display", nil, "freebsd", false},
		{"a Mac over SSH", map[string]string{"SSH_CONNECTION": "10.0.0.2 51000 10.0.0.1 22"}, "darwin", false},
		{"a forwarded X display over SSH", map[string]string{"SSH_TTY": "/dev/pts/1", "DISPLAY": "localhost:10.0"}, "linux", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(key string) string { return tc.env[key] }
			if got := browserOnThisScreen(getenv, tc.goos); got != tc.want {
				t.Errorf("browserOnThisScreen = %v, want %v", got, tc.want)
			}
		})
	}
}

// The default login against the real sign-in routes: it prints the page, the
// code, and the expiry, opens the browser at the page with the code filled in,
// and once the person confirms there it saves the login and names the account.
func TestBrowserLoginOpensThePageAndSavesTheConfirmedLogin(t *testing.T) {
	t.Setenv(config.EnvKeyBuildmaxHome, t.TempDir())
	t.Setenv(config.EnvKeyBuildmaxCredentialStore, "file")
	server := authserver.Start(t, authserver.Options{LocalLogin: "off", OIDC: true})

	var opened []string
	old := openExternalURL
	openExternalURL = func(raw string) error {
		opened = append(opened, raw)
		u, err := url.Parse(raw)
		if err != nil {
			return err
		}
		// The person confirms in the browser that just opened.
		server.Confirm(strings.TrimPrefix(u.Fragment, "/sign-in/"), "oidc")
		return nil
	}
	t.Cleanup(func() { openExternalURL = old })

	var out bytes.Buffer
	if err := browserLogin(context.Background(), &out, server.URL, true); err != nil {
		t.Fatalf("browserLogin: %v\n%s", err, out.String())
	}
	if len(opened) != 1 || !strings.HasPrefix(opened[0], server.URL+"/#/sign-in/") {
		t.Fatalf("opened %v, want the complete URI once", opened)
	}
	code := strings.TrimPrefix(opened[0], server.URL+"/#/sign-in/")
	for _, want := range []string{
		"open this page in a browser and confirm the request:\n\n  " + server.URL + "/#/sign-in\n",
		"Check that the page shows this code: " + code + "\n",
		"(The request expires in 10 minutes. Press Ctrl-C to cancel.)",
		"Opened your browser at " + opened[0],
		"Waiting for confirmation…",
		"Signed in to " + server.URL + " as " + authserver.Email + ".",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	creds, err := auth.StoredLogin()
	if err != nil || creds == nil || creds.Email != authserver.Email || creds.RefreshToken == "" {
		t.Fatalf("stored login = %+v, %v", creds, err)
	}
	sessions := server.ActiveNativeSessions()
	if len(sessions) != 1 || sessions[0].Platform != "cli" || sessions[0].AuthMethod != "oidc" {
		t.Errorf("sessions = %+v, want one cli session confirmed by SSO", sessions)
	}
}

// Without a browser it prints and waits; a denial ends it with one sentence
// and saves nothing.
func TestBrowserLoginWithoutABrowserReportsADenial(t *testing.T) {
	t.Setenv(config.EnvKeyBuildmaxHome, t.TempDir())
	t.Setenv(config.EnvKeyBuildmaxCredentialStore, "file")
	server := authserver.Start(t, authserver.Options{})
	old := openExternalURL
	openExternalURL = func(string) error {
		t.Error("a browser was opened")
		return nil
	}
	t.Cleanup(func() { openExternalURL = old })

	out := &codeWatcher{found: make(chan string, 1)}
	go func() { server.Deny(<-out.found) }()
	err := browserLogin(context.Background(), out, server.URL, false)
	if err == nil || !strings.HasPrefix(err.Error(), "sign-in was not allowed") {
		t.Fatalf("err = %v, want the denial", err)
	}
	if strings.Contains(out.String(), "Opened your browser") {
		t.Errorf("output claims a browser opened:\n%s", out.String())
	}
	if creds, _ := auth.StoredLogin(); creds != nil {
		t.Errorf("a denied sign-in saved %+v", creds)
	}
}

// codeWatcher is the terminal: it keeps what was printed and hands over the
// code once the line naming it appears.
type codeWatcher struct {
	bytes.Buffer
	found chan string
	sent  bool
}

func (w *codeWatcher) Write(p []byte) (int, error) {
	n, err := w.Buffer.Write(p)
	if !w.sent {
		if _, rest, ok := strings.Cut(w.String(), "this code: "); ok {
			if code, _, ok := strings.Cut(rest, "\n"); ok {
				w.sent = true
				w.found <- code
			}
		}
	}
	return n, err
}

func TestExpiryPhrase(t *testing.T) {
	for seconds, want := range map[int64]string{600: "10 minutes", 60: "1 minute", 90: "90 seconds"} {
		if got := expiryPhrase(seconds); got != want {
			t.Errorf("expiryPhrase(%d) = %q, want %q", seconds, got, want)
		}
	}
}
