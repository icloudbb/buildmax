//go:build linux

package sandbox

import (
	"context"
	"errors"
	"os/exec"

	"github.com/icloudbb/buildmax/internal/config"
)

// newBackend returns the platform backend for the resolved deps report.
// Called by Manager when the sandbox is enabled and required deps pass.
func newBackend(name string) (backend, error) {
	if name != "bwrap" {
		return nil, errors.New("sandbox: only bwrap is supported on linux")
	}
	path, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, err
	}
	return &bwrapBackend{path: path}, nil
}

// bwrapBackend wraps bash via bubblewrap (https://github.com/containers/bubblewrap).
// Phase B enforces filesystem isolation only:
//   - read-only-bind / (entire host) so commands see a normal-looking FS
//   - read-write bind the workspace + every entry in filesystem.allow_write
//   - read-only-bind every entry in filesystem.deny_write (overrides the
//     earlier writable bind)
//   - mount /dev/null over every entry in filesystem.deny_read so reads
//     return empty
//   - tmpfs /tmp inside the sandbox
//   - re-bind /proc from the parent rather than mount a fresh instance, so
//     bwrap can run inside a container's own PID namespace (see
//     buildBwrapArgs)
//   - --die-with-parent so a stuck bwrap can't outlive the agent
//
// Network and env scrubbing land in Phase C and Phase D. The sandbox
// inherits the parent's network namespace today.
type bwrapBackend struct {
	path string
}

func (b *bwrapBackend) Name() string { return "bwrap" }

func (b *bwrapBackend) Wrap(_ context.Context, p WrapParams) (string, []string, error) {
	args := buildBwrapArgs(p)
	return b.path, args, nil
}

func (b *bwrapBackend) Close() error { return nil }

// buildBwrapArgs constructs bwrap argv from WrapParams. Extracted so a
// golden test can exercise it without invoking bwrap.
func buildBwrapArgs(p WrapParams) []string {
	args := []string{
		// Baseline mounts. Read-only / so allow_write entries are the
		// only writable surface.
		"--ro-bind", "/", "/",
		// /proc is re-bound from the parent rather than mounted fresh
		// (--proc) because a fresh procfs mount inside --unshare-pid, run
		// from inside a container whose own runtime already masks parts of
		// /proc, trips the kernel's "mount too revealing" check
		// (SB_I_USERNS_VISIBLE in fs/namespace.c) and fails outright --
		// confirmed against a real worker pod's PodSecurityContext, not
		// merely a bwrap or seccomp limitation. A re-bound /proc shows the
		// parent's process list rather than an isolated one; that is the
		// accepted cost of the sandbox being able to run inside a
		// container's own PID namespace at all.
		"--ro-bind", "/proc", "/proc",
		"--dev", "/dev",
		// Private /tmp so subprocesses can't read each other's temp files.
		"--tmpfs", "/tmp",
		// Workspace is always writable.
		"--bind", p.Workspace, p.Workspace,
		"--chdir", p.Workspace,
	}
	// Re-expose the run bridge socket after the tmpfs above masked /tmp: it is
	// how the `buildmax` command an Agent runs reaches the Server, and connect()
	// needs the socket writable, so bind it read-write. Binding only the socket
	// keeps the rest of the worker's private /tmp hidden. See
	// docs/design/agent-bridge-cli.md.
	if p.RunBridgeSocket != "" {
		args = append(args, "--bind", p.RunBridgeSocket, p.RunBridgeSocket)
	}
	// Additional writable paths from settings.
	for _, w := range expandPaths(p.Cfg.Filesystem.AllowWrite, p.Workspace) {
		args = append(args, "--bind", w, w)
	}
	// Deny-write: re-mount read-only over any writable bind.
	for _, w := range expandPaths(p.Cfg.Filesystem.DenyWrite, p.Workspace) {
		args = append(args, "--ro-bind", w, w)
	}
	// Deny-read: shadow with /dev/null so reads see an empty file.
	for _, r := range expandPaths(p.Cfg.Filesystem.DenyRead, p.Workspace) {
		args = append(args, "--ro-bind", "/dev/null", r)
	}
	// HTTP_PROXY env is set on cmd.Env by the bash tool itself
	// (sandbox.SandboxView.ChildEnv). Phase C does NOT yet add
	// --unshare-net + a socat unix-socket bridge, so a malicious child
	// can still reach the network directly. The env-based routing
	// is sufficient for cooperating tools (curl, wget, git http) and
	// matches the docs' "advisory" stance for Linux until the
	// namespace hardening lands as a follow-up.
	args = append(args,
		"--die-with-parent",
		"--unshare-pid",
		"--unshare-ipc",
		"--unshare-uts",
		"--",
		shellOrDefault(p.Shell),
		"-c",
		ulimitPrefix(p.Cfg)+p.Command,
	)
	return args
}

// expandPaths trims empty entries from a settings array. Phase B leaves
// path semantics (`~/`, `./`, absolute) to the OS; Phase D moves to a
// full resolver that honors docs/design/sandbox-boundaries.md §4.3.
func expandPaths(in []string, _ string) []string {
	out := make([]string, 0, len(in))
	for _, p := range in {
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// _ keeps the config import live when other files in the package don't.
var _ = config.SandboxConfig{}
