package runbridge

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// unixClient dials the bridge's Unix socket, the way the CLI subprocess would.
func unixClient(socket string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socket)
			},
		},
	}
}

func TestBridgeInjectsTokenAndForwardsWorkerRoutes(t *testing.T) {
	var gotAuth, gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	b, err := Serve(upstream.URL, "run-token-abc", nil)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	defer func() { _ = b.Close() }()

	resp, err := unixClient(b.SocketPath()).Get("http://bridge/api/worker/task-runs/run-1/issue")
	if err != nil {
		t.Fatalf("request through bridge: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if gotAuth != "Bearer run-token-abc" {
		t.Fatalf("upstream Authorization = %q, want the injected run token", gotAuth)
	}
	if gotPath != "/api/worker/task-runs/run-1/issue" {
		t.Fatalf("upstream path = %q", gotPath)
	}
}

// A path outside the worker surface is refused at the bridge and never reaches
// the listener, even though the run token would fail there anyway.
func TestBridgeRefusesNonWorkerRoutes(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("bridge forwarded a non-worker route to the listener")
	}))
	defer upstream.Close()

	b, err := Serve(upstream.URL, "tok", nil)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	defer func() { _ = b.Close() }()

	resp, err := unixClient(b.SocketPath()).Get("http://bridge/api/spaces/tm_1/issues")
	if err != nil {
		t.Fatalf("request through bridge: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

// The CLI supplies no credential; anything it sends is replaced, so a spoofed
// Authorization header cannot ride through.
func TestBridgeOverwritesCallerAuthorization(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
	}))
	defer upstream.Close()

	b, err := Serve(upstream.URL, "real-token", nil)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	defer func() { _ = b.Close() }()

	req, _ := http.NewRequest(http.MethodGet, "http://bridge/api/worker/task-runs/run-1", nil)
	req.Header.Set("Authorization", "Bearer forged")
	resp, err := unixClient(b.SocketPath()).Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if gotAuth != "Bearer real-token" {
		t.Fatalf("upstream Authorization = %q, want the injected token to win", gotAuth)
	}
}
