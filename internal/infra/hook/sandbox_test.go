package hook

import (
	"context"
)

// stubSandbox is a SandboxView whose WrapBashCommand and HostAllowed return
// chosen results, so a test can prove CommandDriver/HTTPDriver route
// through it rather than bypassing it. Mirrors internal/tool's own
// stubSandbox (bash_sandbox_test.go) and denyAllSandbox/allowAllSandbox
// (web_fetch_sandbox_test.go) -- the pattern already proven for the two
// tools this package's drivers are meant to match.
type stubSandbox struct {
	enabled     bool
	wrapName    string
	wrapArgs    []string
	wrapErr     error
	wrapCalled  *bool
	hostAllowed bool
	hostReason  string
	childEnv    []string
	scrubbed    []string
}

func (s *stubSandbox) Enabled() bool                      { return s.enabled }
func (s *stubSandbox) Mode() string                       { return "auto_allow" }
func (s *stubSandbox) Backend() string                    { return "stub" }
func (s *stubSandbox) NetworkIsolated() bool              { return false }
func (s *stubSandbox) ShouldSandboxCommand(_ string) bool { return s.enabled }
func (s *stubSandbox) ProxyAddress() string               { return "" }
func (s *stubSandbox) AllowUnsandboxed() bool             { return true }

func (s *stubSandbox) WrapBashCommand(_ context.Context, _, _ string) (string, []string, error) {
	if s.wrapCalled != nil {
		*s.wrapCalled = true
	}
	return s.wrapName, s.wrapArgs, s.wrapErr
}

func (s *stubSandbox) HostAllowed(_ string) (bool, string) { return s.hostAllowed, s.hostReason }

func (s *stubSandbox) ChildEnv() []string { return s.childEnv }

func (s *stubSandbox) ScrubEnv(env []string) []string {
	if s.scrubbed != nil {
		return s.scrubbed
	}
	return env
}
