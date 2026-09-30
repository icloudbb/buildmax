package main

import (
	"os"
	"strings"
	"sync"
	"testing"
)

func TestKindForwardTargets(t *testing.T) {
	for _, target := range kindForwardTargets() {
		if target.name == "" || target.namespace == "" || target.service == "" {
			t.Errorf("target %+v is missing a name, namespace, or service", target)
		}
		if len(target.ports) == 0 {
			t.Errorf("target %q forwards no port", target.name)
		}
		if len(target.hints) == 0 {
			t.Errorf("target %q prints no way to use the forward", target.name)
		}
		for _, mapping := range target.ports {
			host, service, found := strings.Cut(mapping, ":")
			if !found {
				t.Errorf("target %q port %q is not hostPort:servicePort", target.name, mapping)
				continue
			}
			// A forwarded address that reads the same as the in-cluster one is
			// what lets the hints name a single port number.
			if host != service {
				t.Errorf("target %q maps %s to a different host port; the hints assume they match", target.name, mapping)
			}
		}
	}
}

func TestPlaceholderPorts(t *testing.T) {
	target := kindForwardTarget{ports: []string{"9000:9000", "9001:9001"}}
	got := strings.Join(placeholderPorts(target), " ")
	// Every port the service needs, not only the one that collided.
	if want := "<port>:9000 <port>:9001"; got != want {
		t.Errorf("placeholderPorts() = %q, want %q", got, want)
	}
}

func TestPrefixWriterTagsWholeLines(t *testing.T) {
	var lines sync.Mutex
	writer := &prefixWriter{prefix: "mysql", lines: &lines}

	// kubectl is not obliged to write a line per call, and a line still being
	// written is not a line to print.
	for _, chunk := range []string{"Forwarding ", "from 127.0.0.1:3306\r\nHandling", " connection\n", "partial"} {
		n, err := writer.Write([]byte(chunk))
		if err != nil || n != len(chunk) {
			t.Fatalf("Write(%q) = %d, %v", chunk, n, err)
		}
	}
	if got := string(writer.pending); got != "partial" {
		t.Errorf("pending = %q, want the unterminated line to be held", got)
	}
}

func TestKindPortalURLDefaultsAndOverride(t *testing.T) {
	if got, want := kindPortalURL(), "http://localhost:8080"; got != want {
		t.Errorf("kindPortalURL() = %q, want %q", got, want)
	}
	t.Setenv("BUILDMAX_KIND_PORTAL_PORT", "9090")
	if got, want := kindPortalURL(), "http://localhost:9090"; got != want {
		t.Errorf("kindPortalURL() with BUILDMAX_KIND_PORTAL_PORT set = %q, want %q", got, want)
	}
}

// TestRenderKindConfigSubstitutesPorts proves the two ports a second cluster
// needs — BUILDMAX_KIND_PORTAL_PORT and BUILDMAX_KIND_TLS_PORT — land in the
// rendered file, and only there: nothing else in the committed config should
// move just because it happens to contain the same digits as a chosen port.
func TestRenderKindConfigSubstitutesPorts(t *testing.T) {
	t.Chdir("../..")
	t.Setenv("BUILDMAX_KIND_PORTAL_PORT", "18080")
	t.Setenv("BUILDMAX_KIND_TLS_PORT", "18443")

	path, cleanup, err := renderKindConfig()
	if err != nil {
		t.Fatalf("renderKindConfig() error = %v", err)
	}
	t.Cleanup(cleanup)

	rendered, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read rendered config: %v", err)
	}
	content := string(rendered)
	if !strings.Contains(content, "hostPort: 18080") {
		t.Errorf("rendered config does not contain the portal port:\n%s", content)
	}
	if !strings.Contains(content, "hostPort: 18443") {
		t.Errorf("rendered config does not contain the TLS port:\n%s", content)
	}
	if strings.Contains(content, "hostPort: 8080") || strings.Contains(content, "hostPort: 8443") {
		t.Errorf("rendered config still has a default hostPort:\n%s", content)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("rendered file %s does not exist before cleanup: %v", path, err)
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("cleanup() left %s behind", path)
	}
}

// TestRenderKindSmokeConfigRewritesCorsOrigin proves the server config a
// second cluster mounts agrees with the port its browser client actually
// uses. A mismatch here does not fail preflight or cluster creation — it only
// shows up as a rejected WebSocket upgrade once a real conversation turn
// runs, which is what made this worth a test rather than trusting the port
// substitution in the kind config to be the whole fix.
func TestRenderKindSmokeConfigRewritesCorsOrigin(t *testing.T) {
	t.Chdir("../..")
	t.Setenv("BUILDMAX_KIND_PORTAL_PORT", "18080")

	for _, path := range []string{"deployment/smoke/server.kind.yaml", "deployment/smoke/server.kind.managed.yaml"} {
		t.Run(path, func(t *testing.T) {
			renderedPath, cleanup, err := renderKindSmokeConfig(path)
			if err != nil {
				t.Fatalf("renderKindSmokeConfig(%q) error = %v", path, err)
			}
			t.Cleanup(cleanup)

			rendered, err := os.ReadFile(renderedPath)
			if err != nil {
				t.Fatalf("read rendered config: %v", err)
			}
			content := string(rendered)
			if !strings.Contains(content, "cors_origin: http://localhost:18080") {
				t.Errorf("rendered config does not carry the requested origin:\n%s", content)
			}
			if strings.Contains(content, "cors_origin: http://localhost:8080") {
				t.Errorf("rendered config still has the default origin:\n%s", content)
			}
			// public_base_url is what artifact share links and OIDC callbacks are
			// built from, so it has to name the ephemeral port too, not the 8080
			// default a shared link would otherwise point at.
			if !strings.Contains(content, "public_base_url: http://localhost:18080") {
				t.Errorf("rendered config does not carry the requested public_base_url:\n%s", content)
			}
			if strings.Contains(content, "public_base_url: http://localhost:8080") {
				t.Errorf("rendered config still has the default public_base_url:\n%s", content)
			}
		})
	}
}

func TestKindLogServices(t *testing.T) {
	seen := map[string]bool{}
	for _, svc := range kindLogServices {
		if svc.name == "" || svc.namespace == "" {
			t.Errorf("service %+v is missing a name or namespace", svc)
		}
		if seen[svc.name] {
			t.Errorf("service name %q is listed more than once", svc.name)
		}
		seen[svc.name] = true
		if len(svc.commands) == 0 {
			t.Errorf("service %q has no log commands", svc.name)
		}
	}
}

func TestKindLogsRejectsUnknownService(t *testing.T) {
	// An unknown name is a usage error caught before requireCommands, so this
	// must not need kubectl on the machine running the test.
	err := kindLogs([]string{"nope"})
	if err == nil || !strings.Contains(err.Error(), "unknown service") {
		t.Errorf("kindLogs([nope]) error = %v, want an unknown service error", err)
	}
}

func TestKindLogsRejectsTooManyArgs(t *testing.T) {
	err := kindLogs([]string{"server", "portal"})
	if err == nil || !strings.Contains(err.Error(), "at most one service name") {
		t.Errorf("kindLogs([server, portal]) error = %v, want an at-most-one-argument error", err)
	}
}

func TestKindReloadServices(t *testing.T) {
	seen := map[string]bool{}
	for _, svc := range kindReloadServices {
		if svc.name == "" || svc.deployment == "" || svc.image.tag == "" || svc.image.dockerfile == "" {
			t.Errorf("service %+v is missing a name, deployment, or image", svc)
		}
		if seen[svc.name] {
			t.Errorf("service name %q is listed more than once", svc.name)
		}
		seen[svc.name] = true
	}
}

func TestCmdKindReloadRejectsUnknownService(t *testing.T) {
	// An unknown name fails before docker or kubectl are touched, so this must
	// not need either on the machine running the test.
	err := cmdKindReload([]string{"nope"})
	if err == nil || !strings.Contains(err.Error(), "unknown service") {
		t.Errorf("cmdKindReload([nope]) error = %v, want an unknown service error", err)
	}
}

func TestCmdKindReloadRejectsTooManyArgs(t *testing.T) {
	err := cmdKindReload([]string{"server", "portal"})
	if err == nil || !strings.Contains(err.Error(), "at most one service name") {
		t.Errorf("cmdKindReload([server, portal]) error = %v, want an at-most-one-argument error", err)
	}
}

func TestCheckKindHostPortsAcceptsFreePorts(t *testing.T) {
	t.Setenv("BUILDMAX_KIND_PORTAL_PORT", "18081")
	t.Setenv("BUILDMAX_KIND_TLS_PORT", "18444")
	// Nothing binds these in a test environment, so a fresh pair the preflight
	// has never seen before must pass — this is exactly the case a second,
	// concurrently running cluster relies on.
	if err := checkKindHostPorts("test-cluster"); err != nil {
		t.Errorf("checkKindHostPorts() with free ports = %v, want nil", err)
	}
}
