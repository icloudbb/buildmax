package tool

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/core/agent"
)

// okEcho returns 200 OK with a tiny HTML body so WebFetch's HTML→text
// path runs end-to-end.
func okEcho(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	_, _ = w.Write([]byte("<html><body>ok</body></html>"))
}

// denyAllSandbox is a SandboxView that denies every host. Tests use it to
// verify WebFetch honors the allow-list before issuing the HTTP request.
type denyAllSandbox struct{}

func (denyAllSandbox) Enabled() bool                       { return true }
func (denyAllSandbox) Mode() string                        { return "auto_allow" }
func (denyAllSandbox) Backend() string                     { return "stub" }
func (denyAllSandbox) NetworkIsolated() bool               { return false }
func (denyAllSandbox) ShouldSandboxCommand(_ string) bool  { return false }
func (denyAllSandbox) HostAllowed(_ string) (bool, string) { return false, "sandbox: blocked" }
func (denyAllSandbox) ProxyAddress() string                { return "" }
func (denyAllSandbox) ChildEnv() []string                  { return nil }
func (denyAllSandbox) ScrubEnv(env []string) []string      { return env }
func (denyAllSandbox) AllowUnsandboxed() bool              { return true }
func (denyAllSandbox) WrapBashCommand(_ context.Context, _, _ string) (string, []string, error) {
	return "", nil, nil
}

// allowAllSandbox returns true for every host; used to confirm the test
// path is the only thing changing the outcome.
type allowAllSandbox struct{}

func (allowAllSandbox) Enabled() bool                       { return true }
func (allowAllSandbox) Mode() string                        { return "auto_allow" }
func (allowAllSandbox) Backend() string                     { return "stub" }
func (allowAllSandbox) NetworkIsolated() bool               { return false }
func (allowAllSandbox) ShouldSandboxCommand(_ string) bool  { return false }
func (allowAllSandbox) HostAllowed(_ string) (bool, string) { return true, "" }
func (allowAllSandbox) ProxyAddress() string                { return "" }
func (allowAllSandbox) ChildEnv() []string                  { return nil }
func (allowAllSandbox) ScrubEnv(env []string) []string      { return env }
func (allowAllSandbox) AllowUnsandboxed() bool              { return true }
func (allowAllSandbox) WrapBashCommand(_ context.Context, _, _ string) (string, []string, error) {
	return "", nil, nil
}

// TestWebFetch_HostFilter_Deny asserts WebFetch refuses to fetch a host
// that the sandbox does not allow, and that the error message is LLM-readable.
func TestWebFetch_HostFilter_Deny(t *testing.T) {
	wf := NewWebFetch(nil, time.Minute).WithSandbox(denyAllSandbox{})
	_, err := wf.Execute(context.Background(), map[string]any{"url": "http://localhost:1/some-path"})
	if err == nil {
		t.Fatal("expected denial error, got nil")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("error = %v; want a sandbox-deny reason", err)
	}
}

// TestWebFetch_HostFilter_AllowAll asserts WebFetch falls through to the
// real fetch when the sandbox allows the host. Uses httptest so no real
// network is touched.
func TestWebFetch_HostFilter_AllowAll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(okEcho))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	if u.Hostname() != "127.0.0.1" {
		t.Skip("httptest did not bind 127.0.0.1; skipping")
	}
	wf := NewWebFetch(nil, time.Minute).WithSandbox(allowAllSandbox{})
	got, err := wf.Execute(context.Background(), map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_ = got // body content varies; presence is enough
}

// TestWebFetch_NoopSandboxIsTransparent confirms NoopSandbox does not
// gate WebFetch — existing behavior preserved.
func TestWebFetch_NoopSandboxIsTransparent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(okEcho))
	defer srv.Close()
	if !strings.HasPrefix(srv.URL, "http://127.0.0.1:") {
		t.Skip("httptest did not bind 127.0.0.1; skipping")
	}
	wf := NewWebFetch(nil, time.Minute).WithSandbox(agent.NoopSandbox{})
	if _, err := wf.Execute(context.Background(), map[string]any{"url": srv.URL}); err != nil {
		t.Fatalf("Execute with NoopSandbox: %v", err)
	}
}
