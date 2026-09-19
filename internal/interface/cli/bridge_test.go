package cli

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
)

func TestInWorkerRunDetection(t *testing.T) {
	t.Setenv(config.EnvKeyBuildmaxBridgeSock, "")
	t.Setenv(config.EnvKeyBuildmaxTaskRunID, "")
	if inWorkerRun() != nil {
		t.Fatal("no bridge env should be a local session")
	}
	t.Setenv(config.EnvKeyBuildmaxBridgeSock, "/tmp/x.sock")
	t.Setenv(config.EnvKeyBuildmaxTaskRunID, "run-1")
	if inWorkerRun() == nil {
		t.Fatal("bridge env should be a worker run")
	}
}

// bridgeStub serves a Unix socket that stands in for the run bridge, capturing
// the request the CLI makes so a test can assert it hit the worker route.
func bridgeStub(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	// A short dir, not t.TempDir(): the test name makes that path long enough to
	// exceed the OS socket-name limit (sun_path), the same reason the real bridge
	// puts its socket under a fresh short temp dir.
	dir, err := os.MkdirTemp("", "bmx")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: handler}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return sock
}

// In a worker run, `issue comment` posts to the run's issue through the bridge,
// on the worker route, taking no issue id.
func TestIssueCommentRoutesThroughBridgeInWorkerRun(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	sock := bridgeStub(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"ic_1"}`))
	})
	t.Setenv(config.EnvKeyBuildmaxBridgeSock, sock)
	t.Setenv(config.EnvKeyBuildmaxTaskRunID, "run-1")

	root := NewRootCommand()
	root.SetArgs([]string{"issue", "comment", "-m", "adapter shipped"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/api/worker/task-runs/run-1/issue/comments" {
		t.Fatalf("path = %q, want the worker issue-comment route", gotPath)
	}
	if gotBody["body"] != "adapter shipped" {
		t.Fatalf("body = %v", gotBody["body"])
	}
}

// An issue id inside a worker run is refused: the run may only address its own
// issue, which the worker route names for it.
func TestIssueCommentRefusesIDInWorkerRun(t *testing.T) {
	sock := bridgeStub(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("a request should not be sent when the id is refused")
		w.WriteHeader(http.StatusCreated)
	})
	t.Setenv(config.EnvKeyBuildmaxBridgeSock, sock)
	t.Setenv(config.EnvKeyBuildmaxTaskRunID, "run-1")

	root := NewRootCommand()
	root.SetArgs([]string{"issue", "comment", "i_1", "-m", "x"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "drop the issue id") {
		t.Fatalf("err = %v, want a refusal to name an issue id in a run", err)
	}
}
