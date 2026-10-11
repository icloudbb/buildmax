package clie2e

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/testsupport/authserver"
)

// `buildmax login` end to end: the released binary against the real sign-in
// routes on a server it shares no memory with. The person's half — confirming
// or denying in the Portal — is made through the API from a Portal session,
// the way design enterprise-identity-and-access §14 has a browser make it.
//
// No test here may open a real browser. Each run either passes --no-browser or
// sets SSH_CONNECTION, which is itself the headless case under test.

// session is a running `buildmax` whose stdout a test reads as it arrives and
// whose stdin it types into.
type session struct {
	t      *testing.T
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	mu     sync.Mutex
	stdout strings.Builder
	stderr strings.Builder
	done   chan struct{}
	err    error
}

func startCLI(t *testing.T, home string, env []string, args ...string) *session {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(), append([]string{"BUILDMAX_HOME=" + home, "BUILDMAX_CREDENTIAL_STORE=file"}, env...)...)
	s := &session{t: t, cmd: cmd, done: make(chan struct{})}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	s.stdin = stdin
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = &lockedWriter{mu: &s.mu, b: &s.stderr}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", binary, err)
	}
	go func() {
		reader := bufio.NewReader(stdout)
		buf := make([]byte, 4096)
		for {
			n, readErr := reader.Read(buf)
			s.mu.Lock()
			s.stdout.Write(buf[:n])
			s.mu.Unlock()
			if readErr != nil {
				break
			}
		}
		s.err = cmd.Wait()
		close(s.done)
	}()
	t.Cleanup(func() {
		select {
		case <-s.done:
		default:
			_ = cmd.Process.Kill()
			<-s.done
		}
	})
	return s
}

type lockedWriter struct {
	mu *sync.Mutex
	b  *strings.Builder
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

func (s *session) output() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stdout.String(), s.stderr.String()
}

// waitFor blocks until stdout contains want, and returns everything after it
// up to the end of that line.
func (s *session) waitFor(want string) string {
	s.t.Helper()
	return s.await(want, true)
}

// waitForPrompt blocks until stdout shows a prompt, which ends no line.
func (s *session) waitForPrompt(prompt string) {
	s.t.Helper()
	s.await(prompt, false)
}

func (s *session) await(want string, wholeLine bool) string {
	s.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		out, _ := s.output()
		if _, rest, ok := strings.Cut(out, want); ok {
			if !wholeLine {
				return ""
			}
			if line, _, ok := strings.Cut(rest, "\n"); ok {
				return line
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	out, errOut := s.output()
	s.t.Fatalf("no %q within 20s\nstdout:\n%s\nstderr:\n%s", want, out, errOut)
	return ""
}

func (s *session) type_(line string) {
	s.t.Helper()
	if _, err := io.WriteString(s.stdin, line+"\n"); err != nil {
		s.t.Fatalf("type %q: %v", line, err)
	}
}

// exit waits for the process and returns its exit code.
func (s *session) exit() int {
	s.t.Helper()
	select {
	case <-s.done:
	case <-time.After(60 * time.Second):
		out, errOut := s.output()
		s.t.Fatalf("buildmax did not exit within 60s\nstdout:\n%s\nstderr:\n%s", out, errOut)
	}
	if s.err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if !asExitError(s.err, &exitErr) {
		s.t.Fatalf("wait: %v", s.err)
	}
	return exitErr.ExitCode()
}

// storedTokens reads the login the binary saved, or nil when there is none.
func storedTokens(t *testing.T, home string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var creds map[string]any
	if err := json.Unmarshal(data, &creds); err != nil {
		t.Fatalf("auth.json: %v", err)
	}
	return creds
}

// expectLoginLikeAnyOther proves a stored login behaves as a password login's
// does: `buildmax me` reaches the deployment, renewing the short-lived access
// token with a rotated refresh token, and `buildmax logout` ends the session on
// the server and removes the credentials.
func expectLoginLikeAnyOther(t *testing.T, server *authserver.Server, home string) {
	t.Helper()
	before := storedTokens(t, home)
	if before == nil || before["refresh_token"] == "" {
		t.Fatalf("no stored login with a refresh token: %v", before)
	}
	me := startCLI(t, home, nil, "me")
	if code := me.exit(); code != 0 {
		out, errOut := me.output()
		t.Fatalf("me exited %d\n%s\n%s", code, out, errOut)
	}
	if out, _ := me.output(); !strings.Contains(out, "Logged in as "+authserver.Email+" on "+server.URL) {
		t.Errorf("me:\n%s", out)
	}
	after := storedTokens(t, home)
	if after["refresh_token"] == before["refresh_token"] || after["token"] == before["token"] {
		t.Errorf("the login was not renewed: before %v, after %v", before, after)
	}

	logout := startCLI(t, home, nil, "logout")
	if code := logout.exit(); code != 0 {
		t.Fatalf("logout exited %d", code)
	}
	if _, errOut := logout.output(); errOut != "" {
		t.Errorf("logout warned: %s", errOut)
	}
	if left := server.ActiveNativeSessions(); len(left) != 0 {
		t.Errorf("sessions after logout = %+v, want none", left)
	}
	if storedTokens(t, home) != nil {
		t.Error("logout left auth.json behind")
	}
}

// With local_login off and SSO on, the browser sign-in is the only way in. On
// an SSH host the CLI opens nothing, prints the page and the code, and a
// confirmation from a Portal SSO session signs it in.
func TestLoginConfirmedInThePortalSignsInOverSSH(t *testing.T) {
	server := authserver.Start(t, authserver.Options{LocalLogin: "off", OIDC: true, AccessTokenTTL: time.Minute})
	home := t.TempDir()

	login := startCLI(t, home, []string{"SSH_CONNECTION=10.0.0.2 51000 10.0.0.1 22"}, "login")
	login.waitForPrompt("Server URL [")
	login.type_(server.URL)
	page := login.waitFor("confirm the request:\n\n  ")
	code := login.waitFor("Check that the page shows this code: ")
	if page != server.URL+"/#/sign-in" || len(code) != 9 {
		t.Fatalf("page %q, code %q", page, code)
	}
	login.waitFor("Waiting for confirmation…")
	server.Confirm(code, "oidc")
	if exit := login.exit(); exit != 0 {
		out, errOut := login.output()
		t.Fatalf("login exited %d\nstdout:\n%s\nstderr:\n%s", exit, out, errOut)
	}
	out, _ := login.output()
	if strings.Contains(out, "Opened your browser") {
		t.Errorf("a browser was opened over SSH:\n%s", out)
	}
	if !strings.Contains(out, "Signed in to "+server.URL+" as "+authserver.Email+".") {
		t.Errorf("the account is not named:\n%s", out)
	}
	sessions := server.ActiveNativeSessions()
	if len(sessions) != 1 || sessions[0].Platform != "cli" || sessions[0].AuthMethod != "oidc" || sessions[0].ClientLabel == "" {
		t.Fatalf("sessions = %+v, want one labelled cli session confirmed by SSO", sessions)
	}
	expectLoginLikeAnyOther(t, server, home)
}

// Denied and expired requests each end in one sentence and a non-zero exit,
// and leave nothing stored.
func TestLoginReportsADenialAndAnExpiry(t *testing.T) {
	for name, tc := range map[string]struct {
		decide func(*authserver.Server, string)
		want   string
	}{
		"denied": {
			decide: func(s *authserver.Server, code string) { s.Deny(code) },
			want:   "error: sign-in was not allowed: it was denied in the browser",
		},
		"expired": {
			decide: func(s *authserver.Server, _ string) { s.Advance(11 * time.Minute) },
			want:   "error: the sign-in request expired before it was confirmed",
		},
	} {
		t.Run(name, func(t *testing.T) {
			server := authserver.Start(t, authserver.Options{})
			home := t.TempDir()
			login := startCLI(t, home, nil, "login", "--no-browser")
			login.waitForPrompt("Server URL [")
			login.type_(server.URL)
			code := login.waitFor("this code: ")
			tc.decide(server, code)
			if exit := login.exit(); exit == 0 {
				t.Fatal("login exited 0")
			}
			out, errOut := login.output()
			if !strings.HasPrefix(errOut, tc.want) || strings.Count(strings.TrimSpace(errOut), "\n") != 0 {
				t.Errorf("stderr = %q, want one line starting %q", errOut, tc.want)
			}
			if strings.Contains(out, "Opened your browser") || strings.Contains(out, "Signed in") {
				t.Errorf("stdout:\n%s", out)
			}
			if storedTokens(t, home) != nil {
				t.Error("credentials were saved")
			}
			if left := server.ActiveNativeSessions(); len(left) != 0 {
				t.Errorf("sessions = %+v, want none", left)
			}
		})
	}
}

// --password is the login as it was before browser sign-in: email, then a
// password, against POST /api/auth/login. It now reports the client label too.
func TestPasswordLoginKeepsItsPrompts(t *testing.T) {
	server := authserver.Start(t, authserver.Options{AccessTokenTTL: time.Minute})
	home := t.TempDir()

	login := startCLI(t, home, nil, "login", "--password")
	login.waitForPrompt("Server URL [")
	login.type_(server.URL)
	login.waitForPrompt("Email: ")
	login.type_(authserver.Email)
	login.waitForPrompt("Password (leave blank to use a login code): ")
	login.type_(authserver.Password)
	if exit := login.exit(); exit != 0 {
		out, errOut := login.output()
		t.Fatalf("login exited %d\nstdout:\n%s\nstderr:\n%s", exit, out, errOut)
	}
	out, _ := login.output()
	if !strings.Contains(out, "Logged in as "+authserver.Email+" on "+server.URL+"\n") || strings.Contains(out, "this code") {
		t.Errorf("stdout:\n%s", out)
	}
	sessions := server.ActiveNativeSessions()
	if len(sessions) != 1 || sessions[0].Platform != "cli" || sessions[0].AuthMethod != "password" || sessions[0].ClientLabel == "" {
		t.Fatalf("sessions = %+v, want one labelled cli password session", sessions)
	}
	expectLoginLikeAnyOther(t, server, home)
}
