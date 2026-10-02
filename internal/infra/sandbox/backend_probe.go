package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/icloudbb/buildmax/internal/config"
)

// probeMarker and probeEscaped are printed by probeCommand and never appear
// in a legitimate backend error, so their presence in the probe's own
// output is unambiguous evidence of what happened.
const (
	probeMarker  = "buildmax_sandbox_probe_ok"
	probeEscaped = "buildmax_sandbox_probe_escaped"
)

// probeTimeout bounds the one-time backend probe run at Manager
// construction. Generous: a hung probe is itself proof the backend does not
// work here, and this runs once per run, not per Bash call.
const probeTimeout = 10 * time.Second

// probeBackend runs a real command through b and confirms it actually
// confines a write to the probe's own scratch workspace, denying one outside
// it. CheckDeps only proves the backend binary exists on PATH; on a host
// whose seccomp policy or kernel configuration blocks the syscalls the
// backend needs, or that otherwise cannot enforce the confinement it claims,
// the binary still runs but the sandbox it builds does not isolate anything.
// A silent pass-through there is exactly what fail_if_unavailable exists to
// catch — see docs/design/sandbox-boundaries.md §13 and the deployment
// smoke's own organic probe (tools/mk/deploy_smoke.go) which caught this in
// production before this in-process check existed.
func probeBackend(ctx context.Context, b backend, cfg config.SandboxConfig) error {
	workspace, err := os.MkdirTemp("", "buildmax-sandbox-probe-ws-")
	if err != nil {
		return fmt.Errorf("create probe workspace: %w", err)
	}
	defer os.RemoveAll(workspace)
	outsideDir, err := os.MkdirTemp("", "buildmax-sandbox-probe-outside-")
	if err != nil {
		return fmt.Errorf("create probe outside dir: %w", err)
	}
	defer os.RemoveAll(outsideDir)
	outsideFile := filepath.Join(outsideDir, "should-not-be-written")

	command := "echo " + probeMarker +
		" && (echo x > " + shellQuoteProbePath(outsideFile) +
		" 2>/dev/null && echo " + probeEscaped + " || true)"

	name, args, err := b.Wrap(ctx, WrapParams{
		Command:   command,
		Shell:     "/bin/sh",
		Workspace: workspace,
		Cfg:       cfg,
	})
	if err != nil {
		return fmt.Errorf("build probe command: %w", err)
	}

	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	runErr := cmd.Run()
	output := out.String()

	if !strings.Contains(output, probeMarker) {
		if runErr != nil {
			return fmt.Errorf("backend did not run a command inside the sandbox: %w (%s)", runErr, strings.TrimSpace(output))
		}
		return fmt.Errorf("backend did not run a command inside the sandbox: %s", strings.TrimSpace(output))
	}
	if strings.Contains(output, probeEscaped) {
		return fmt.Errorf("backend ran the command but did not confine a write outside the workspace")
	}
	if _, statErr := os.Stat(outsideFile); statErr == nil {
		return fmt.Errorf("backend ran the command but a write outside the workspace landed on disk")
	}
	return nil
}

// probeNetworkIsolation runs one command, with its proxy bridge, in an isolated
// network namespace. bwrap brings up
// loopback in the new namespace, which needs CAP_NET_ADMIN where it runs; a
// container without it fails here instead of in every command.
func probeNetworkIsolation(ctx context.Context, b backend, cfg config.SandboxConfig, p *Proxy) error {
	workspace, err := os.MkdirTemp("", "buildmax-sandbox-netprobe-ws-")
	if err != nil {
		return fmt.Errorf("create probe workspace: %w", err)
	}
	defer os.RemoveAll(workspace)
	name, args, err := b.Wrap(ctx, WrapParams{
		Command:        "echo " + probeMarker,
		Shell:          "/bin/sh",
		Workspace:      workspace,
		Cfg:            cfg,
		ProxyAddr:      p.Addr(),
		ProxySocket:    p.SocketPath(),
		IsolateNetwork: true,
	})
	if err != nil {
		return fmt.Errorf("build probe command: %w", err)
	}
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, runErr := exec.CommandContext(probeCtx, name, args...).CombinedOutput()
	if runErr != nil || !strings.Contains(string(out), probeMarker) {
		return fmt.Errorf("isolated command did not run: %v (%s)", runErr, strings.TrimSpace(string(out)))
	}
	return nil
}

// shellQuoteProbePath single-quotes a path for embedding in the probe's own
// shell command. The probe only ever quotes paths this package created
// under os.TempDir(), never external input.
func shellQuoteProbePath(path string) string {
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}
