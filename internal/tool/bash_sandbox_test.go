package tool

import (
	"context"
	"github.com/icloudbb/buildmax/internal/util"
	"runtime"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/agent"
	"github.com/icloudbb/buildmax/internal/core/llm"
)

// stubSandbox is a SandboxView whose WrapBashCommand returns a chosen
// (name, args) so we can prove Bash routes through it.
type stubSandbox struct {
	enabled bool
	name    string
	args    []string
	called  *bool
}

func (s *stubSandbox) Enabled() bool                       { return s.enabled }
func (s *stubSandbox) Mode() string                        { return "auto_allow" }
func (s *stubSandbox) Backend() string                     { return "stub" }
func (s *stubSandbox) NetworkIsolated() bool               { return false }
func (s *stubSandbox) ShouldSandboxCommand(_ string) bool  { return s.enabled }
func (s *stubSandbox) HostAllowed(_ string) (bool, string) { return true, "" }
func (s *stubSandbox) ProxyAddress() string                { return "" }
func (s *stubSandbox) ChildEnv() []string                  { return nil }
func (s *stubSandbox) ScrubEnv(env []string) []string      { return env }
func (s *stubSandbox) AllowUnsandboxed() bool              { return true }
func (s *stubSandbox) WrapBashCommand(_ context.Context, _, _ string) (string, []string, error) {
	if s.called != nil {
		*s.called = true
	}
	return s.name, s.args, nil
}

// TestBash_RoutesThroughSandbox asserts that when WithSandbox is set and
// WrapBashCommand returns a non-empty name, the bash tool execs that
// instead of the direct shell invocation.
func TestBash_RoutesThroughSandbox(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test relies on /usr/bin/env shipping with the OS")
	}
	b := NewBash(util.FixedRoot(testWorkspace(t, "")))
	called := false
	// Use `env` to print an identifying string when invoked. This proves
	// the sandbox-supplied argv is what actually ran.
	stub := &stubSandbox{
		enabled: true,
		name:    "/usr/bin/env",
		args:    []string{"sh", "-c", "echo SANDBOX_WRAPPED"},
		called:  &called,
	}
	b = b.WithSandbox(stub)
	got, err := b.Execute(context.Background(), map[string]any{"command": "echo NOT_WRAPPED"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !called {
		t.Errorf("WrapBashCommand never called")
	}
	if !strings.Contains(got, "SANDBOX_WRAPPED") || strings.Contains(got, "NOT_WRAPPED") {
		t.Errorf("output = %q; want sandbox-wrapped command to run", got)
	}
}

// TestBash_NoopSandboxIsTransparent asserts NoopSandbox does not alter
// the spawn path, so existing behavior is preserved.
func TestBash_NoopSandboxIsTransparent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test relies on echo working without sandbox-style wrap")
	}
	b := NewBash(util.FixedRoot(testWorkspace(t, ""))).WithSandbox(agent.NoopSandbox{})
	got, err := b.Execute(context.Background(), map[string]any{"command": "echo direct"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(got, "direct") {
		t.Errorf("output = %q; want direct shell to have run", got)
	}
}

// TestBash_DangerouslyDisableSandbox_Honored asserts the per-call flag
// skips the sandbox wrap when AllowUnsandboxed is true.
func TestBash_DangerouslyDisableSandbox_Honored(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("not applicable on Windows")
	}
	called := false
	stub := &stubSandbox{
		enabled: true,
		name:    "/usr/bin/env",
		args:    []string{"sh", "-c", "echo SANDBOX_WRAPPED"},
		called:  &called,
	}
	b := NewBash(util.FixedRoot(testWorkspace(t, ""))).WithSandbox(stub)
	got, err := b.Execute(context.Background(), map[string]any{
		"command":                     "echo DIRECT",
		"dangerously_disable_sandbox": true,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if called {
		t.Error("WrapBashCommand should not have been called (escape hatch honored)")
	}
	if !strings.Contains(got, "DIRECT") || strings.Contains(got, "SANDBOX_WRAPPED") {
		t.Errorf("output = %q; want direct shell", got)
	}
}

// strictStubSandbox is a stubSandbox variant whose AllowUnsandboxed()
// returns false ("strict sandbox mode"). Used to verify the per-call
// disable flag is ignored.
type strictStubSandbox struct{ *stubSandbox }

func (strictStubSandbox) AllowUnsandboxed() bool { return false }

// TestBash_DangerouslyDisableSandbox_Strict asserts the per-call flag is
// ignored when AllowUnsandboxed is false.
func TestBash_DangerouslyDisableSandbox_Strict(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("not applicable on Windows")
	}
	called := false
	strict := strictStubSandbox{
		stubSandbox: &stubSandbox{
			enabled: true,
			name:    "/usr/bin/env",
			args:    []string{"sh", "-c", "echo SANDBOX_WRAPPED"},
			called:  &called,
		},
	}
	b := NewBash(util.FixedRoot(testWorkspace(t, ""))).WithSandbox(strict)
	got, err := b.Execute(context.Background(), map[string]any{
		"command":                     "echo DIRECT",
		"dangerously_disable_sandbox": true,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !called {
		t.Error("WrapBashCommand should have been called (strict mode ignores flag)")
	}
	if !strings.Contains(got, "SANDBOX_WRAPPED") {
		t.Errorf("output = %q; want sandbox-wrapped invocation", got)
	}
}

// scrubbingSandbox forces ScrubEnv to drop FOO_KEY so we can test the
// bash tool actually applies the scrubbed env.
type scrubbingSandbox struct{ *stubSandbox }

func (scrubbingSandbox) ScrubEnv(env []string) []string {
	out := env[:0:0]
	for _, e := range env {
		if !strings.HasPrefix(e, "FOO_KEY=") {
			out = append(out, e)
		}
	}
	return out
}
func (scrubbingSandbox) ChildEnv() []string { return []string{"INJECTED=yes"} }

// autoAllowSandbox is a stubSandbox that reports auto_allow mode and
// claims to sandbox every command. Used to exercise CheckArgs demotion.
type autoAllowSandbox struct{ *stubSandbox }

func (autoAllowSandbox) Mode() string                       { return "auto_allow" }
func (autoAllowSandbox) ShouldSandboxCommand(_ string) bool { return true }

// regularSandbox is a stubSandbox in regular-permissions mode.
type regularSandbox struct{ *stubSandbox }

func (regularSandbox) Mode() string                       { return "regular" }
func (regularSandbox) ShouldSandboxCommand(_ string) bool { return true }

// TestBash_AutoAllow_DemotesAsk asserts auto-allow mode demotes Ask → Allow
// for risky commands when the command will actually be sandboxed.
func TestBash_AutoAllow_DemotesAsk(t *testing.T) {
	b := NewBash(util.FixedRoot(testWorkspace(t, ""))).WithSandbox(autoAllowSandbox{&stubSandbox{enabled: true}})
	if got := b.CheckArgs(map[string]any{"command": "curl example.com"}); got != llm.ToolActionAllow {
		t.Errorf("auto-allow risky curl: got %v, want Allow", got)
	}
	// Catastrophic still denies even under auto-allow.
	if got := b.CheckArgs(map[string]any{"command": "rm -rf /"}); got != llm.ToolActionDeny {
		t.Errorf("auto-allow rm -rf /: got %v, want Deny", got)
	}
}

// TestBash_AutoAllow_RegularModeKeepsAsk asserts that when sandbox mode is
// "regular", Ask is preserved (sandbox is enforcement only, not approval).
func TestBash_AutoAllow_RegularModeKeepsAsk(t *testing.T) {
	b := NewBash(util.FixedRoot(testWorkspace(t, ""))).WithSandbox(regularSandbox{&stubSandbox{enabled: true}})
	if got := b.CheckArgs(map[string]any{"command": "curl example.com"}); got != llm.ToolActionAsk {
		t.Errorf("regular-mode curl: got %v, want Ask", got)
	}
}

// TestBash_AutoAllow_DisabledKeepsAsk asserts that with sandbox off, the
// original Ask behavior is preserved (no regression for users not opting in).
func TestBash_AutoAllow_DisabledKeepsAsk(t *testing.T) {
	b := NewBash(util.FixedRoot(testWorkspace(t, ""))) // NoopSandbox by default
	if got := b.CheckArgs(map[string]any{"command": "curl example.com"}); got != llm.ToolActionAsk {
		t.Errorf("sandbox-off curl: got %v, want Ask", got)
	}
}

// TestBash_AutoAllow_DisableFlagReinstatesAsk asserts that passing
// dangerously_disable_sandbox=true (when honored) reinstates the Ask
// prompt — the OS boundary is no longer there to contain the call.
func TestBash_AutoAllow_DisableFlagReinstatesAsk(t *testing.T) {
	b := NewBash(util.FixedRoot(testWorkspace(t, ""))).WithSandbox(autoAllowSandbox{&stubSandbox{enabled: true}})
	got := b.CheckArgs(map[string]any{
		"command":                     "curl example.com",
		"dangerously_disable_sandbox": true,
	})
	if got != llm.ToolActionAsk {
		t.Errorf("auto-allow + disable flag: got %v, want Ask (escape hatch reinstates approval)", got)
	}
}

// TestBash_ScrubAndInjectEnv asserts the bash tool removes secret-shaped
// env and adds the sandbox's ChildEnv.
func TestBash_ScrubAndInjectEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("not applicable on Windows")
	}
	t.Setenv("FOO_KEY", "should-not-leak")
	b := NewBash(util.FixedRoot(testWorkspace(t, ""))).WithSandbox(scrubbingSandbox{&stubSandbox{enabled: true}})
	got, err := b.Execute(context.Background(), map[string]any{
		"command": "echo FOO_KEY=$FOO_KEY INJECTED=$INJECTED",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(got, "should-not-leak") {
		t.Errorf("FOO_KEY leaked into sandbox: %q", got)
	}
	if !strings.Contains(got, "INJECTED=yes") {
		t.Errorf("ChildEnv not applied: %q", got)
	}
}
