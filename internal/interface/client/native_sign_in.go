package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	"github.com/icloudbb/buildmax/internal/infra/httpclient"
)

// Native sign-in confirmed in the Portal (design enterprise-identity-and-access
// §14): the client starts a request, shows its code, and polls until a person
// confirms or denies it in the Portal. The CLI and Desktop both wait through
// WaitForNativeSignIn, so the pacing rules exist once.

// The answers a poll can end on. Pending and slow_down are handled inside
// WaitForNativeSignIn and never reach its caller.
var (
	// ErrSignInDenied is access_denied: the person denied the request, or the
	// account or Portal session that confirmed it stopped being usable.
	ErrSignInDenied = errors.New("sign-in was not allowed")
	// ErrSignInExpired is expired_token, or the request's lifetime running out
	// on this side first.
	ErrSignInExpired = errors.New("the sign-in request expired")

	errAuthorizationPending = errors.New("authorization_pending")
	errSlowDown             = errors.New("slow_down")
)

// AuthMethods is GET /api/auth/methods: which ways in a deployment offers.
type AuthMethods struct {
	// LocalLogin is "all", "system_admins", or "off": who may use the password
	// and login-code form.
	LocalLogin string `json:"local_login"`
	OIDC       struct {
		Enabled     bool   `json:"enabled"`
		DisplayName string `json:"display_name,omitempty"`
	} `json:"oidc"`
}

// NativeSignIn is a started request: what to show, and how to poll for it.
type NativeSignIn struct {
	UserCode   string `json:"user_code"`
	PollSecret string `json:"poll_secret"`
	// ExpiresIn and Interval are seconds.
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int64  `json:"interval"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
}

// AuthMethods reads the sign-in methods the server offers. The route is
// anonymous.
func (c *Client) AuthMethods(ctx context.Context) (*AuthMethods, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/auth/methods", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, httpclient.DecodeError(resp, "")
	}
	var out AuthMethods
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}

// StartNativeSignIn starts a request for platform ("cli" or "desktop") under
// the label this client reports about itself.
func (c *Client) StartNativeSignIn(ctx context.Context, platform, clientLabel string) (*NativeSignIn, error) {
	resp, err := c.postAnonymous(ctx, "/api/auth/native-sign-ins", map[string]string{
		"platform":     platform,
		"client_label": clientLabel,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, httpclient.DecodeError(resp, "")
	}
	var out NativeSignIn
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if out.PollSecret == "" || out.UserCode == "" {
		return nil, errors.New("the server started no sign-in request")
	}
	return &out, nil
}

// pollNativeSignIn polls once. It returns the login on a redemption, one of
// the four RFC 8628 answers as an error, or the server's refusal.
func (c *Client) pollNativeSignIn(ctx context.Context, pollSecret string) (*LoginResponse, error) {
	resp, err := c.postAnonymous(ctx, "/api/auth/native-sign-ins/poll", map[string]string{"poll_secret": pollSecret})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		var lr LoginResponse
		if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		return &lr, nil
	}
	refused := httpclient.DecodeError(resp, "")
	if refused.StatusCode == http.StatusBadRequest {
		switch refused.Message {
		case "authorization_pending":
			return nil, errAuthorizationPending
		case "slow_down":
			return nil, errSlowDown
		case "access_denied":
			return nil, ErrSignInDenied
		case "expired_token":
			return nil, ErrSignInExpired
		}
	}
	return nil, refused
}

func (c *Client) postAnonymous(ctx context.Context, path string, payload any) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	return resp, nil
}

// Pacing (design §14.3): start at the server's interval, add 5 seconds on each
// slow_down, and on a network error or 5xx double the wait up to 30 seconds.
const (
	defaultPollInterval = 5 * time.Second
	slowDownStep        = 5 * time.Second
	maxPollBackoff      = 30 * time.Second
	// pollRequestTimeout bounds one poll, so a request that hangs is retried
	// as a network error rather than stalling the wait until the code expires.
	pollRequestTimeout = 30 * time.Second
)

// The clock the wait runs on. Tests replace both to step through hours of
// polling instantly.
var (
	pollNow   = time.Now
	pollSleep = func(ctx context.Context, d time.Duration) error {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-timer.C:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
)

// WaitForNativeSignIn polls a started request until it is redeemed, denied, or
// expired, or ctx ends. It never polls after the request's lifetime: when the
// next poll would fall past it, it waits out the lifetime and reports
// ErrSignInExpired. A refusal other than the RFC 8628 answers, such as a 4xx
// for a malformed secret, ends the wait with the server's error.
func (c *Client) WaitForNativeSignIn(ctx context.Context, started *NativeSignIn) (*LoginResponse, error) {
	interval := time.Duration(started.Interval) * time.Second
	if interval <= 0 {
		interval = defaultPollInterval
	}
	deadline := pollNow().Add(time.Duration(started.ExpiresIn) * time.Second)
	var backoff time.Duration
	for {
		wait := max(interval, backoff)
		if remaining := deadline.Sub(pollNow()); wait >= remaining {
			if err := pollSleep(ctx, max(remaining, 0)); err != nil {
				return nil, err
			}
			return nil, ErrSignInExpired
		}
		if err := pollSleep(ctx, wait); err != nil {
			return nil, err
		}
		pollCtx, cancel := context.WithTimeout(ctx, pollRequestTimeout)
		lr, err := c.pollNativeSignIn(pollCtx, started.PollSecret)
		cancel()
		switch {
		case err == nil:
			return lr, nil
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case errors.Is(err, errAuthorizationPending):
			backoff = 0
		case errors.Is(err, errSlowDown):
			interval += slowDownStep
			backoff = 0
		case errors.Is(err, ErrSignInDenied), errors.Is(err, ErrSignInExpired):
			return nil, err
		case transient(err):
			backoff = min(max(2*backoff, 2*interval), maxPollBackoff)
		default:
			return nil, err
		}
	}
}

// transient is a failure worth polling through: no answer at all, or a 5xx.
func transient(err error) bool {
	var refused *httpclient.Error
	if errors.As(err, &refused) {
		return refused.StatusCode >= 500
	}
	return true
}

// ClientLabel is how this machine describes itself to the person confirming a
// sign-in and in session lists: its host name and operating system, such as
// "alice-mbp · macOS". The server shows it as unverified, so it is a hint for a
// person to notice something unexpected, nothing more.
func ClientLabel() string {
	host, _ := os.Hostname()
	return composeClientLabel(host, runtime.GOOS)
}

func composeClientLabel(host, goos string) string {
	osName := operatingSystemName(goos)
	// The first DNS label: "alice-mbp.local" and "alice-mbp.corp.example.com"
	// are both "alice-mbp" to the person reading it.
	host, _, _ = strings.Cut(strings.TrimSpace(host), ".")
	host = strings.Map(func(r rune) rune {
		if r == utf8.RuneError || !unicode.IsPrint(r) {
			return -1
		}
		return r
	}, host)
	host = strings.TrimSpace(host)
	if host == "" {
		return osName
	}
	suffix := " · " + osName
	room := coreidentity.ClientLabelMaxLength - utf8.RuneCountInString(suffix)
	if runes := []rune(host); len(runes) > room {
		host = strings.TrimSpace(string(runes[:room]))
	}
	return host + suffix
}

func operatingSystemName(goos string) string {
	switch goos {
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	case "freebsd":
		return "FreeBSD"
	case "openbsd":
		return "OpenBSD"
	case "netbsd":
		return "NetBSD"
	default:
		return goos
	}
}
