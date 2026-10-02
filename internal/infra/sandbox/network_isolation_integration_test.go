//go:build linux

package sandbox

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/util"
)

// With a restrictive allow-list a command has its own network namespace: the
// proxy is reachable and filters, and a command that ignores HTTP_PROXY
// reaches nothing. Skipped where bwrap, socat, curl, or the namespace (which
// needs CAP_NET_ADMIN as root, or user namespaces otherwise) is unavailable.
func TestManagerIsolatesTheNetworkBehindTheProxy(t *testing.T) {
	for _, bin := range []string{"bwrap", "socat", "curl"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not installed", bin)
		}
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("origin-reached"))
	}))
	defer origin.Close()

	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.SandboxConfig{Enabled: true}
	cfg.Network.AllowedDomains = []string{"127.0.0.1"}
	m, err := NewManager(cfg, util.FixedRoot(workspace), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	if !m.Enabled() || !m.NetworkIsolated() {
		t.Skip("this host cannot build the sandbox's network namespace")
	}

	run := func(command string) string {
		name, args, err := m.WrapBashCommand(context.Background(), command, "/bin/sh")
		if err != nil {
			t.Fatalf("WrapBashCommand: %v", err)
		}
		cmd := exec.Command(name, args...)
		cmd.Dir = workspace
		cmd.Env = append(os.Environ(), m.ChildEnv()...)
		out, _ := cmd.CombinedOutput()
		return string(out)
	}
	if out := run("curl -s -m 5 " + origin.URL); !strings.Contains(out, "origin-reached") {
		t.Fatalf("an allowed host through the proxy: %q", out)
	}
	if out := run("curl -s -m 5 --noproxy '*' " + origin.URL + " || echo DIRECT_FAILED"); !strings.Contains(out, "DIRECT_FAILED") {
		t.Fatalf("a command ignoring the proxy reached the network: %q", out)
	}
	if out := run("curl -s -m 5 http://not-allowed.invalid/ -o /dev/null -w '%{http_code}'"); !strings.Contains(out, "403") {
		t.Fatalf("a host off the allow-list through the proxy: %q, want 403", out)
	}
	if out := run("grep CapEff /proc/self/status"); os.Geteuid() == 0 && !strings.Contains(out, "0000000000000000") {
		t.Fatalf("the command kept capabilities: %q", out)
	}
}

// An allow-everything policy keeps the shared network: there is nothing for a
// namespace to enforce, and tools that do not speak HTTP keep working.
func TestManagerKeepsTheSharedNetworkWhenEverythingIsAllowed(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.SandboxConfig{Enabled: true}
	cfg.Network.AllowedDomains = []string{"*"}
	m, err := NewManager(cfg, util.FixedRoot(workspace), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	if m.NetworkIsolated() {
		t.Fatal("an allow-everything policy isolated the network")
	}
}
