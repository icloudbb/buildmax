package workerclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/tool"
)

const internalWorkerAPI = "https://buildmax-worker-api.buildmax.svc.cluster.local:5679"

func TestRedactServerAddressRemovesURLAndHost(t *testing.T) {
	cases := map[string]string{
		"url":       `Get "` + internalWorkerAPI + `/api/worker/task-runs/r1/secrets": EOF`,
		"host:port": "dial tcp buildmax-worker-api.buildmax.svc.cluster.local:5679: connect: refused",
		"host":      "lookup buildmax-worker-api.buildmax.svc.cluster.local on 10.96.0.10:53: no such host",
	}
	for name, msg := range cases {
		t.Run(name, func(t *testing.T) {
			got := redactServerAddress(msg, internalWorkerAPI+"/")
			if strings.Contains(got, "buildmax-worker-api") {
				t.Errorf("redacted = %q, still names the worker API host", got)
			}
			if !strings.Contains(got, serverAddressPlaceholder) {
				t.Errorf("redacted = %q, want the placeholder in its place", got)
			}
		})
	}
	if got := redactServerAddress("secret s is disabled", internalWorkerAPI); got != "secret s is disabled" {
		t.Errorf("a message without the address changed: %q", got)
	}
}

// The redacted error still unwraps to its cause, so callers that branch on
// context cancellation keep working.
func TestHideServerAddressKeepsTheCause(t *testing.T) {
	cause := errors.Join(errors.New(`Post "`+internalWorkerAPI+`/x"`), context.Canceled)
	err := hideServerAddress(cause, internalWorkerAPI)
	if strings.Contains(err.Error(), internalWorkerAPI) {
		t.Errorf("err = %q, still names the worker API", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, lost its cause", err)
	}
}

// The observed case: a disabled Space Secret refused by the server became the
// run's error message with the worker API URL in it.
func TestRefusedSecretFetchDoesNotNameTheServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"secret API_KEY is disabled"}`))
	}))
	t.Cleanup(srv.Close)

	_, err := GetWorkerTaskRunSecrets(context.Background(), WorkerAPIClientConfig{BaseURL: srv.URL, Token: "t"}, "r1")
	if err == nil {
		t.Fatal("a refused secret fetch returned no error")
	}
	if strings.Contains(err.Error(), srv.URL) || strings.Contains(err.Error(), strings.TrimPrefix(srv.URL, "http://")) {
		t.Errorf("err = %q, names the worker API address", err)
	}
	if !strings.Contains(err.Error(), "secret API_KEY is disabled") {
		t.Errorf("err = %q, lost the server's reason", err)
	}
}

// A transport failure — the server unreachable — names the address in Go's
// own error text; the worker must not pass that on.
func TestUnreachableServerErrorDoesNotNameTheServer(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close()

	_, err := GetWorkerTaskRunSecrets(context.Background(), WorkerAPIClientConfig{BaseURL: base, Token: "t"}, "r1")
	if err == nil {
		t.Fatal("an unreachable server returned no error")
	}
	if strings.Contains(err.Error(), strings.TrimPrefix(base, "http://")) {
		t.Errorf("err = %q, names the worker API address", err)
	}
}

// Whatever a run's error came from, the terminal report is where a person
// reads it, so the updater removes the worker API's address there.
func TestUpdateRunStatusRedactsTheErrorMessage(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	msg := `managed gateway unreachable: Post "` + srv.URL + `/api/worker/llm": EOF`
	req := &PatchTaskRunRequest{Status: "FAILED", ErrorMessage: &msg}
	up := &WorkerHTTPUpdater{BaseURL: srv.URL, Token: "t"}
	if err := up.UpdateRunStatus(context.Background(), "r1", req); err != nil {
		t.Fatalf("UpdateRunStatus: %v", err)
	}
	if strings.Contains(body, strings.TrimPrefix(srv.URL, "http://")) {
		t.Errorf("reported body %s names the worker API address", body)
	}
	if !strings.Contains(body, "managed gateway unreachable") {
		t.Errorf("reported body %s lost the cause", body)
	}
	if *req.ErrorMessage != msg {
		t.Error("the caller's request was modified")
	}
}

// The artifact tool tells the model the server's Portal link, never an
// address built from the worker API it uploaded through.
func TestArtifactPublisherRelaysTheServersLink(t *testing.T) {
	for name, tc := range map[string]struct {
		response string
		wantURL  string
	}{
		"public origin":    {`{"id":"a1","filename":"r.md","size_bytes":2,"url":"https://buildmax.example.com/#/artifact/a1"}`, "https://buildmax.example.com/#/artifact/a1"},
		"no public origin": {`{"id":"a1","filename":"r.md","size_bytes":2}`, ""},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(tc.response))
			}))
			t.Cleanup(srv.Close)
			path := filepath.Join(t.TempDir(), "r.md")
			if err := os.WriteFile(path, []byte("hi"), 0o644); err != nil {
				t.Fatal(err)
			}
			pub := NewArtifactPublisher(WorkerAPIClientConfig{BaseURL: srv.URL, Token: "t"}, "r1")
			got, err := pub.PublishArtifact(context.Background(), tool.ArtifactUpload{Path: path, Filename: "r.md"})
			if err != nil {
				t.Fatalf("PublishArtifact: %v", err)
			}
			if got.URL != tc.wantURL {
				t.Errorf("url = %q, want %q", got.URL, tc.wantURL)
			}
		})
	}
}
