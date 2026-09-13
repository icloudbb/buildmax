package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestServer starts a server with the given config and closes it with the test.
func newTestServer(t *testing.T, cfg Config) *httptest.Server {
	t.Helper()
	cfg.Addr = ":0"
	ts := httptest.NewServer(New(cfg).Handler())
	t.Cleanup(ts.Close)
	return ts
}

// getJSON returns the response body and status for a GET.
func getJSON(t *testing.T, url string) (string, int) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(body), resp.StatusCode
}

func TestHealthz(t *testing.T) {
	s := New(Config{Addr: ":0"})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /healthz status = %d; want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q; want application/json", ct)
	}
	// Body should be {"status":"ok"}
	var out struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Status != "ok" {
		t.Errorf("status = %q; want ok", out.Status)
	}
}

func TestOpenAPI(t *testing.T) {
	const wantVersion = "9.9.9-test"
	s := New(Config{Addr: ":0", Version: wantVersion})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /openapi.json status = %d; want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q; want application/json", ct)
	}
	var spec map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&spec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	paths, _ := spec["paths"].(map[string]interface{})
	if paths == nil {
		t.Fatal("openapi spec missing paths")
	}
	if _, ok := paths["/healthz"]; !ok {
		t.Error("openapi spec missing path /healthz")
	}
	// info.version is stamped from cfg.Version (the build's version source), not
	// the document's placeholder, so there is one source for the version.
	info, _ := spec["info"].(map[string]interface{})
	if info == nil {
		t.Fatal("openapi spec missing info")
	}
	if got := info["version"]; got != wantVersion {
		t.Errorf("openapi info.version = %v; want %q", got, wantVersion)
	}
}

func TestSwaggerUI(t *testing.T) {
	s := New(Config{Addr: ":0"})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	for _, path := range []string{"/swagger/", "/swagger", "/swagger/index.html"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s status = %d; want 200", path, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
			t.Errorf("GET %s Content-Type = %q; want text/html", path, ct)
		}
		_ = resp.Body.Close()
	}
}

// TestReadyz covers what separates readiness from the liveness endpoint: it
// reflects dependencies, and it refuses to describe them.
func TestReadyz(t *testing.T) {
	t.Run("reports ready when every dependency answers", func(t *testing.T) {
		ts := newTestServer(t, Config{Readiness: []ReadinessCheck{
			{Name: "database", Probe: func(context.Context) error { return nil }},
			{Name: "object_storage", Probe: func(context.Context) error { return nil }},
		}})
		body, code := getJSON(t, ts.URL+"/readyz")
		if code != http.StatusOK {
			t.Fatalf("status = %d, want 200", code)
		}
		if !strings.Contains(body, `"status":"ready"`) {
			t.Errorf("body %q should report ready", body)
		}
		if !strings.Contains(body, `"name":"database"`) || !strings.Contains(body, `"name":"object_storage"`) {
			t.Errorf("body %q should name every dependency it checked", body)
		}
	})

	t.Run("refuses traffic when a dependency is down", func(t *testing.T) {
		ts := newTestServer(t, Config{Readiness: []ReadinessCheck{
			{Name: "database", Probe: func(context.Context) error {
				return errors.New("dial tcp 10.0.0.5:3306: connect: connection refused, dsn=root:hunter2@tcp(db)/buildmax")
			}},
			{Name: "object_storage", Probe: func(context.Context) error { return nil }},
		}})
		body, code := getJSON(t, ts.URL+"/readyz")
		if code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503 so Kubernetes stops routing to this pod", code)
		}
		if !strings.Contains(body, `"name":"database","status":"failed"`) {
			t.Errorf("body %q should name the failing dependency", body)
		}
		// The endpoint is unauthenticated and connection errors carry DSNs,
		// endpoints, and bucket names. The reason belongs in the server log.
		for _, leaked := range []string{"hunter2", "10.0.0.5", "dsn=", "connection refused"} {
			if strings.Contains(body, leaked) {
				t.Errorf("readiness body leaked %q: %s", leaked, body)
			}
		}
	})

	t.Run("an unchecked server says so rather than claiming health", func(t *testing.T) {
		ts := newTestServer(t, Config{})
		body, code := getJSON(t, ts.URL+"/readyz")
		if code != http.StatusOK {
			t.Fatalf("status = %d, want 200", code)
		}
		// An empty list is visible evidence that nothing was verified — which
		// is the failure this endpoint exists to replace.
		if !strings.Contains(body, `"checks":[]`) {
			t.Errorf("body %q should show an empty check list", body)
		}
	})
}

// TestHealthzIgnoresDependencies pins liveness apart from readiness. A liveness
// probe that failed on a dependency outage would restart a healthy server and
// turn a recoverable outage into a crash loop.
func TestHealthzIgnoresDependencies(t *testing.T) {
	ts := newTestServer(t, Config{Readiness: []ReadinessCheck{
		{Name: "database", Probe: func(context.Context) error { return errors.New("down") }},
	}})
	_, code := getJSON(t, ts.URL+"/healthz")
	if code != http.StatusOK {
		t.Errorf("healthz status = %d, want 200 even with a dependency down", code)
	}
}
