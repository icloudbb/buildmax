package httpclient

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// gate answers 200 to the current token and 401 to anything else, recording
// the body of each accepted request.
type gate struct {
	current  string
	requests atomic.Int32
	bodies   chan string
}

func newGate(t *testing.T, current string) (*gate, *httptest.Server) {
	t.Helper()
	g := &gate{current: current, bodies: make(chan string, 4)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+g.current {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		g.bodies <- string(body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return g, server
}

func renewingClient(renew RenewFunc) *http.Client {
	return &http.Client{Transport: RenewOnUnauthorized(nil, renew)}
}

func TestRenewOnUnauthorizedRetriesOnceWithTheRenewedToken(t *testing.T) {
	g, server := newGate(t, "fresh")
	var rejected []string
	c := renewingClient(func(tok string) (string, error) {
		rejected = append(rejected, tok)
		return "fresh", nil
	})

	req, _ := http.NewRequest(http.MethodPost, server.URL, strings.NewReader(`{"a":1}`))
	req.Header.Set("Authorization", "Bearer stale")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want the retry's 200", resp.StatusCode)
	}
	if len(rejected) != 1 || rejected[0] != "stale" {
		t.Errorf("renew saw %v, want exactly the refused token", rejected)
	}
	// The replay carries the whole body, not whatever the first attempt left.
	if got := <-g.bodies; got != `{"a":1}` {
		t.Errorf("replayed body = %q", got)
	}
	if req.Header.Get("Authorization") != "Bearer stale" {
		t.Error("the caller's request was modified")
	}
}

// A failed renewal hands the caller the server's own refusal, so its existing
// classification of a 401 — an ended login — still applies.
func TestRenewOnUnauthorizedReturnsTheRefusalWhenRenewalFails(t *testing.T) {
	g, server := newGate(t, "fresh")
	c := renewingClient(func(string) (string, error) { return "", errors.New("login has expired") })

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	req.Header.Set("Authorization", "Bearer stale")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if msg := DecodeError(resp, "").Message; msg != "unauthorized" {
		t.Errorf("the server's refusal did not survive: %q", msg)
	}
	if got := g.requests.Load(); got != 1 {
		t.Errorf("requests = %d, want no retry without a new token", got)
	}
}

// One retry only: a renewed token the server also refuses is the caller's to
// report, not a reason to loop.
func TestRenewOnUnauthorizedRetriesAtMostOnce(t *testing.T) {
	g, server := newGate(t, "never")
	renewals := 0
	c := renewingClient(func(string) (string, error) {
		renewals++
		return "also-refused", nil
	})

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	req.Header.Set("Authorization", "Bearer stale")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized || renewals != 1 || g.requests.Load() != 2 {
		t.Errorf("status %d after %d renewals and %d requests, want 401, 1, 2",
			resp.StatusCode, renewals, g.requests.Load())
	}
}

func TestRenewOnUnauthorizedLeavesAnonymousRequestsAlone(t *testing.T) {
	_, server := newGate(t, "fresh")
	c := renewingClient(func(string) (string, error) {
		t.Error("renewed for a request that presented no credential")
		return "fresh", nil
	})
	resp, err := c.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

// A streamed upload is replayable too, so an artifact publish survives a
// rotated secret instead of failing once.
func TestUploadFileIsReplayedAfterRenewal(t *testing.T) {
	g, server := newGate(t, "fresh")
	path := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(path, []byte("artifact body"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := renewingClient(func(string) (string, error) { return "fresh", nil })

	resp, err := UploadFile(t.Context(), c, server.URL, "stale", "file", path, "report.txt")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want the replay's 200", resp.StatusCode)
	}
	if got := <-g.bodies; !strings.Contains(got, "artifact body") {
		t.Errorf("replayed upload lost the file: %q", got)
	}
}
