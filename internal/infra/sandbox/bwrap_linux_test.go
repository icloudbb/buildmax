//go:build linux

package sandbox

import (
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
)

// TestBwrapArgs_Golden asserts the documented argv shape: ro-bind / first,
// proc/dev/tmpfs, workspace bind, then allow_write and deny_write, then
// --die-with-parent and the inner shell invocation.
func TestBwrapArgs_Golden(t *testing.T) {
	p := WrapParams{
		Command:   "echo hi",
		Shell:     "/bin/bash",
		Workspace: "/home/dev/proj",
		Cfg: config.SandboxConfig{
			Enabled: true,
			Filesystem: config.SandboxFSConfig{
				AllowWrite: []string{"/tmp/build"},
				DenyWrite:  []string{"/home/dev/proj/secrets"},
				DenyRead:   []string{"/etc/shadow"},
			},
		},
	}
	args := buildBwrapArgs(p)
	joined := strings.Join(args, " ")
	mustContain := []string{
		"--ro-bind / /",
		"--ro-bind /proc /proc",
		"--dev /dev",
		"--tmpfs /tmp",
		"--bind /home/dev/proj /home/dev/proj",
		"--chdir /home/dev/proj",
		"--bind /tmp/build /tmp/build",
		"--ro-bind /home/dev/proj/secrets /home/dev/proj/secrets",
		"--ro-bind /dev/null /etc/shadow",
		"--die-with-parent",
		"--unshare-pid",
		"-- /bin/bash -c echo hi",
	}
	// Without ProxyAddr, no --setenv HTTP_PROXY should appear.
	if strings.Contains(joined, "HTTP_PROXY") {
		t.Errorf("argv unexpectedly set HTTP_PROXY without proxy:\n%s", joined)
	}
	for _, want := range mustContain {
		if !strings.Contains(joined, want) {
			t.Errorf("argv missing %q\nfull: %s", want, joined)
		}
	}
}

// TestBwrapArgs_RunBridgeSocketReExposed asserts the run bridge socket is bound
// back into the sandbox after the tmpfs masks /tmp — otherwise the `buildmax`
// command an Agent runs cannot reach the Server through the bridge. The bind is
// read-write because connect() needs the socket writable, and it names the
// socket itself so the rest of the worker's /tmp stays hidden.
func TestBwrapArgs_RunBridgeSocketReExposed(t *testing.T) {
	sock := "/tmp/buildmax-bridge-1234/s"
	p := WrapParams{Command: "buildmax issue show", Workspace: "/tmp/ws", RunBridgeSocket: sock}
	joined := strings.Join(buildBwrapArgs(p), " ")
	if !strings.Contains(joined, "--tmpfs /tmp") {
		t.Fatalf("expected the private /tmp tmpfs:\n%s", joined)
	}
	bind := "--bind " + sock + " " + sock
	if !strings.Contains(joined, bind) {
		t.Errorf("argv missing the bridge socket bind %q\nfull: %s", bind, joined)
	}
	// The bind must come after the tmpfs, or it would be masked by it.
	if strings.Index(joined, bind) < strings.Index(joined, "--tmpfs /tmp") {
		t.Errorf("bridge socket bound before the tmpfs that masks /tmp:\n%s", joined)
	}
	// A run with no bridge adds no such bind.
	none := strings.Join(buildBwrapArgs(WrapParams{Command: "id", Workspace: "/tmp/ws"}), " ")
	if strings.Contains(none, "buildmax-bridge") {
		t.Errorf("a run with no bridge should bind no socket:\n%s", none)
	}
}

// TestBwrapArgs_ShellDefault asserts /bin/sh is the default inner shell.
func TestBwrapArgs_ShellDefault(t *testing.T) {
	p := WrapParams{Command: "id", Workspace: "/tmp/ws"}
	args := buildBwrapArgs(p)
	last3 := strings.Join(args[len(args)-3:], " ")
	if last3 != "/bin/sh -c id" {
		t.Errorf("tail = %q, want \"/bin/sh -c id\"", last3)
	}
}

// TestBwrapArgs_ProcessLimitsPrefixTheCommand asserts a configured process
// limit becomes a `ulimit` statement prefixed onto the inner shell's -c
// argument, ahead of the user's own command, rather than a separate bwrap
// flag — bwrap has no per-process resource-limit flag of its own.
func TestBwrapArgs_ProcessLimitsPrefixTheCommand(t *testing.T) {
	p := WrapParams{
		Command:   "id",
		Workspace: "/tmp/ws",
		Cfg: config.SandboxConfig{
			Process: config.SandboxProcessConfig{MaxCPUSeconds: 10, MaxOpenFiles: 64},
		},
	}
	args := buildBwrapArgs(p)
	last := args[len(args)-1]
	if !strings.HasPrefix(last, "ulimit -t 10 2>/dev/null; ulimit -n 64 2>/dev/null; ") {
		t.Fatalf("-c argument = %q, want it to start with the ulimit statements", last)
	}
	if !strings.HasSuffix(last, "id") {
		t.Errorf("-c argument = %q, want the user's own command last", last)
	}
}

// TestBwrapArgs_ProxyEnvNotInArgv asserts proxy env is **not** stamped
// onto the bwrap argv (it's set on cmd.Env by the Bash tool itself).
func TestBwrapArgs_ProxyEnvNotInArgv(t *testing.T) {
	p := WrapParams{
		Command:   "curl example.com",
		Workspace: "/tmp/ws",
		ProxyAddr: "127.0.0.1:54321",
	}
	joined := strings.Join(buildBwrapArgs(p), " ")
	if strings.Contains(joined, "HTTP_PROXY") {
		t.Errorf("argv unexpectedly carries HTTP_PROXY; expected cmd.Env path\n%s", joined)
	}
}

// With IsolateNetwork the command gets its own network namespace whose only
// way out is the proxy socket, bridged to the loopback port HTTP_PROXY names.
func TestBwrapArgs_IsolatedNetworkBridgesOnlyTheProxySocket(t *testing.T) {
	p := WrapParams{Command: "curl https://example.com", Workspace: "/tmp/ws", ProxyAddr: "127.0.0.1:3128",
		ProxySocket: "/tmp/buildmax-proxy-1/proxy.sock", IsolateNetwork: true}
	args := buildBwrapArgs(p)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--unshare-net") || !strings.Contains(joined, "--bind /tmp/buildmax-proxy-1/proxy.sock /tmp/buildmax-proxy-1/proxy.sock") {
		t.Fatalf("isolated argv lacks the namespace or the socket bind: %s", joined)
	}
	cmd := args[len(args)-1]
	if !strings.HasPrefix(cmd, "socat TCP-LISTEN:3128,bind=127.0.0.1,fork,reuseaddr UNIX-CONNECT:'/tmp/buildmax-proxy-1/proxy.sock'") ||
		!strings.Contains(cmd, ":0C38 00000000:0000 0A") || !strings.HasSuffix(cmd, "curl https://example.com") {
		t.Fatalf("command does not start the bridge, wait for it, then run: %q", cmd)
	}
	for _, unisolated := range []WrapParams{
		{Command: "id", Workspace: "/tmp/ws", ProxyAddr: "127.0.0.1:3128", ProxySocket: "/tmp/s.sock"},
		{Command: "id", Workspace: "/tmp/ws", ProxyAddr: "127.0.0.1:3128", IsolateNetwork: true},
		{Command: "id", Workspace: "/tmp/ws", ProxySocket: "/tmp/s.sock", IsolateNetwork: true},
	} {
		if j := strings.Join(buildBwrapArgs(unisolated), " "); strings.Contains(j, "--unshare-net") || strings.Contains(j, "socat") {
			t.Errorf("isolated without everything isolation needs: %s", j)
		}
	}
}

// bwrap run as root keeps its capabilities for the command unless told; the
// command needs none, and in a worker pod they are SYS_ADMIN and NET_ADMIN.
func TestBwrapArgs_DropsEveryCapabilityWhenRoot(t *testing.T) {
	defer func(f func() int) { geteuid = f }(geteuid)
	geteuid = func() int { return 0 }
	args := buildBwrapArgs(WrapParams{Command: "id", Workspace: "/tmp/ws"})
	if !strings.Contains(strings.Join(args, " "), "--cap-drop ALL --die-with-parent") {
		t.Fatalf("root argv does not drop capabilities before the command: %v", args)
	}
	geteuid = func() int { return 1000 }
	if strings.Contains(strings.Join(buildBwrapArgs(WrapParams{Command: "id", Workspace: "/tmp/ws"}), " "), "--cap-drop") {
		t.Fatal("unprivileged bwrap rejects --cap-drop; it must not be passed")
	}
}
