package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
)

// fakeClock replaces the wait's clock: sleeping advances it, so hours of
// polling run instantly and every wait is recorded.
type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	waits []time.Duration
}

func useFakeClock(t *testing.T) *fakeClock {
	t.Helper()
	c := &fakeClock{now: time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)}
	oldNow, oldSleep := pollNow, pollSleep
	pollNow = func() time.Time {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.now
	}
	pollSleep = func(ctx context.Context, d time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		c.waits = append(c.waits, d)
		c.now = c.now.Add(d)
		return nil
	}
	t.Cleanup(func() { pollNow, pollSleep = oldNow, oldSleep })
	return c
}

func (c *fakeClock) recorded() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.waits...)
}

// answer is one scripted poll response: a status and body, or a transport
// failure when status is zero.
type answer struct {
	status int
	body   string
}

func pending() answer     { return answer{http.StatusBadRequest, `{"error":"authorization_pending"}`} }
func slowDown() answer    { return answer{http.StatusBadRequest, `{"error":"slow_down"}`} }
func unreachable() answer { return answer{} }
func serverError() answer { return answer{http.StatusBadGateway, `<html>bad gateway</html>`} }
func redeemed() answer {
	return answer{http.StatusOK, `{"access_token":"at","refresh_token":"rt","expires_in":900,"user":{"id":"u_1","email":"alice@example.com"}}`}
}
func denied() answer       { return answer{http.StatusBadRequest, `{"error":"access_denied"}`} }
func expiredToken() answer { return answer{http.StatusBadRequest, `{"error":"expired_token"}`} }

// scriptedPolls answers each poll with the next answer, repeating the last.
type scriptedPolls struct {
	t       *testing.T
	mu      sync.Mutex
	answers []answer
	polls   int
	secrets []string
}

func (s *scriptedPolls) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path != "/api/auth/native-sign-ins/poll" || r.Method != http.MethodPost {
		s.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	}
	var body map[string]string
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secrets = append(s.secrets, body["poll_secret"])
	a := s.answers[min(s.polls, len(s.answers)-1)]
	s.polls++
	if a.status == 0 {
		return nil, errors.New("connection refused")
	}
	return &http.Response{StatusCode: a.status, Body: io.NopCloser(strings.NewReader(a.body)), Header: http.Header{}}, nil
}

func (s *scriptedPolls) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.polls
}

func scriptedClient(t *testing.T, answers ...answer) (*Client, *scriptedPolls) {
	script := &scriptedPolls{t: t, answers: answers}
	c := NewClient("http://buildmax.test")
	c.HTTPClient = &http.Client{Transport: script}
	return c, script
}

func started() *NativeSignIn {
	return &NativeSignIn{UserCode: "WDJB-MJHT", PollSecret: "bmxsignin_secret", ExpiresIn: 600, Interval: 5}
}

func seconds(values ...int) []time.Duration {
	out := make([]time.Duration, len(values))
	for i, v := range values {
		out[i] = time.Duration(v) * time.Second
	}
	return out
}

func equalWaits(got, want []time.Duration) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// The pacing §14.3 asks for, in one run: wait the interval before the first
// poll, keep it while pending, add five seconds per slow_down, double through
// network errors and 5xx up to 30 seconds, and drop back to the interval once
// the server answers again.
func TestWaitPacesPollsAsTheServerAsks(t *testing.T) {
	clock := useFakeClock(t)
	c, script := scriptedClient(t,
		pending(), slowDown(), pending(),
		unreachable(), serverError(), unreachable(), unreachable(),
		pending(), redeemed(),
	)

	lr, err := c.WaitForNativeSignIn(context.Background(), started())
	if err != nil {
		t.Fatalf("WaitForNativeSignIn: %v", err)
	}
	if lr.Access() != "at" || lr.User.Email != "alice@example.com" {
		t.Errorf("login = %+v", lr)
	}
	// 5 (pending) 5 (slow_down → 10) 10 (pending) 10 (unreachable → backoff 20)
	// 20 (5xx → 30) 30 (unreachable, capped) 30 (unreachable) 30 (pending → back
	// to the interval) 10 (redeemed).
	want := seconds(5, 5, 10, 10, 20, 30, 30, 30, 10)
	if got := clock.recorded(); !equalWaits(got, want) {
		t.Errorf("waits = %v, want %v", got, want)
	}
	for _, secret := range script.secrets {
		if secret != "bmxsignin_secret" {
			t.Errorf("polled with %q", secret)
		}
	}
}

// It never polls after the request's lifetime: a person who never decides
// gets an expiry exactly at the end, not a poll the server would refuse.
func TestWaitStopsAtExpiryWithoutPollingPastIt(t *testing.T) {
	clock := useFakeClock(t)
	c, script := scriptedClient(t, pending())
	start := clock.now
	req := started()
	req.ExpiresIn = 23

	_, err := c.WaitForNativeSignIn(context.Background(), req)
	if !errors.Is(err, ErrSignInExpired) {
		t.Fatalf("err = %v, want ErrSignInExpired", err)
	}
	// Polls at 5, 10, 15, 20; the next would be at 25, past 23.
	if script.count() != 4 {
		t.Errorf("polls = %d, want 4", script.count())
	}
	if elapsed := clock.now.Sub(start); elapsed != 23*time.Second {
		t.Errorf("gave up after %v, want the request's 23s lifetime", elapsed)
	}
}

func TestWaitEndsOnTheServersTerminalAnswers(t *testing.T) {
	for name, tc := range map[string]struct {
		answer answer
		want   error
	}{
		"denied":  {denied(), ErrSignInDenied},
		"expired": {expiredToken(), ErrSignInExpired},
	} {
		t.Run(name, func(t *testing.T) {
			useFakeClock(t)
			c, script := scriptedClient(t, pending(), tc.answer, redeemed())
			_, err := c.WaitForNativeSignIn(context.Background(), started())
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if script.count() != 2 {
				t.Errorf("polls = %d, want it to stop at the terminal answer", script.count())
			}
		})
	}
}

// A refusal that is not one of the four answers, such as a 4xx for a body the
// server cannot read, is the server's to explain, and retrying cannot change it.
func TestWaitReportsAnotherRefusal(t *testing.T) {
	useFakeClock(t)
	c, script := scriptedClient(t, answer{http.StatusBadRequest, `{"error":"poll_secret is required"}`})
	_, err := c.WaitForNativeSignIn(context.Background(), started())
	if err == nil || !strings.Contains(err.Error(), "poll_secret is required") {
		t.Fatalf("err = %v, want the server's message", err)
	}
	if script.count() != 1 {
		t.Errorf("polls = %d, want 1", script.count())
	}
}

func TestWaitStopsWhenCancelled(t *testing.T) {
	useFakeClock(t)
	ctx, cancel := context.WithCancel(context.Background())
	c, script := scriptedClient(t, pending())
	// Cancel from inside the second poll, as Ctrl-C or Desktop's Cancel would.
	c.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if script.count() == 1 {
			cancel()
		}
		return script.RoundTrip(r)
	})}

	_, err := c.WaitForNativeSignIn(ctx, started())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if script.count() != 2 {
		t.Errorf("polls = %d, want none after the cancellation", script.count())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestStartNativeSignInSendsPlatformAndLabel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/native-sign-ins" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected request %s %s (Authorization %q)", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["platform"] != "desktop" || body["client_label"] != "alice-mbp · macOS" {
			t.Errorf("body = %v", body)
		}
		_, _ = w.Write([]byte(`{"user_code":"WDJB-MJHT","poll_secret":"bmxsignin_x","expires_in":600,"interval":5,` +
			`"verification_uri":"https://b.example/#/sign-in","verification_uri_complete":"https://b.example/#/sign-in/WDJB-MJHT"}`))
	}))
	defer srv.Close()

	got, err := NewClient(srv.URL).StartNativeSignIn(context.Background(), "desktop", "alice-mbp · macOS")
	if err != nil {
		t.Fatalf("StartNativeSignIn: %v", err)
	}
	if got.UserCode != "WDJB-MJHT" || got.Interval != 5 || got.ExpiresIn != 600 ||
		got.VerificationURIComplete != "https://b.example/#/sign-in/WDJB-MJHT" {
		t.Errorf("started = %+v", got)
	}
}

func TestStartNativeSignInReportsTheServersRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"too many pending sign-in requests"}`))
	}))
	defer srv.Close()
	_, err := NewClient(srv.URL).StartNativeSignIn(context.Background(), "cli", "")
	if err == nil || !strings.Contains(err.Error(), "too many pending sign-in requests") {
		t.Fatalf("err = %v", err)
	}
}

func TestAuthMethodsReadsTheDeploymentsWaysIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/methods" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"local_login":"system_admins","oidc":{"enabled":true,"display_name":"Okta"}}`))
	}))
	defer srv.Close()
	m, err := NewClient(srv.URL).AuthMethods(context.Background())
	if err != nil {
		t.Fatalf("AuthMethods: %v", err)
	}
	if m.LocalLogin != "system_admins" || !m.OIDC.Enabled || m.OIDC.DisplayName != "Okta" {
		t.Errorf("methods = %+v", m)
	}
}

func TestClientLabelIsHostAndOperatingSystem(t *testing.T) {
	long := strings.Repeat("h", 80)
	for _, tc := range []struct {
		host, goos, want string
	}{
		{"alice-mbp.local", "darwin", "alice-mbp · macOS"},
		{"build-01.corp.example.com", "linux", "build-01 · Linux"},
		{"DESKTOP-7Q2", "windows", "DESKTOP-7Q2 · Windows"},
		{"", "linux", "Linux"},
		{"tab\there", "freebsd", "tabhere · FreeBSD"},
		// " · macOS" is 8 characters, which leaves 56 for the host.
		{long, "darwin", strings.Repeat("h", 56) + " · macOS"},
	} {
		got := composeClientLabel(tc.host, tc.goos)
		if got != tc.want {
			t.Errorf("composeClientLabel(%q, %q) = %q, want %q", tc.host, tc.goos, got, tc.want)
		}
		// Whatever the host is called, the server must accept the label.
		if n := utf8.RuneCountInString(got); n > coreidentity.ClientLabelMaxLength {
			t.Errorf("label %q is %d characters", got, n)
		}
		if _, err := coreidentity.NormalizeClientLabel(got); err != nil {
			t.Errorf("the server would refuse %q: %v", got, err)
		}
	}
}
