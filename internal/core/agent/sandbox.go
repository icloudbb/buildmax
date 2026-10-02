package agent

import "context"

// SandboxView is the read-only contract tools see for sandbox state.
//
// Mirrors the design in docs/design/sandbox-boundaries.md: the
// sandbox isolates Bash subprocesses; non-bash tools (Read/Write/Edit/etc.)
// use the existing permission system, not this contract.
//
// Implementations live in internal/infra/sandbox (Phase B). The Bash tool
// and the command hook transport depend on this contract; nothing in
// core/agent should import the implementation.
//
// SandboxView intentionally exposes no enforcement primitives in Phase A.
// Phase B adds WrapBashCommand and ShouldSandboxCommand; Phase D adds the
// per-call dangerously_disable_sandbox escape hatch.
type SandboxView interface {
	// Enabled reports whether the sandbox is active for this run. When
	// false, tools should fall back to current (pre-sandbox) behavior.
	Enabled() bool

	// Mode is "auto_allow" or "regular". See docs/design/sandbox-boundaries.md §5.
	// Returns "" when Enabled() is false.
	Mode() string

	// Backend identifies the OS backend providing isolation
	// ("seatbelt", "bwrap", "none"). Returns "none" when the sandbox
	// is enabled in settings but the OS backend is unavailable.
	Backend() string

	// NetworkIsolated reports whether wrapped commands run in a network
	// namespace of their own whose only way out is the sandbox's proxy.
	// False means a command that ignores the proxy reaches the network
	// directly.
	NetworkIsolated() bool

	// WrapBashCommand returns the (binary, argv) the Bash tool should
	// exec to run the given command isolated by the active backend.
	// `shell` is the inner shell the backend should invoke
	// (e.g. "/bin/bash"); on unsupported platforms or when not wrapping
	// the caller should fall back to its own default invocation.
	//
	// When (name == "" && err == nil) the caller must run the command
	// unwrapped. This is how NoopSandbox signals "do nothing." Phase B
	// fills in the real wrap; Phase A only ships NoopSandbox.
	//
	// ctx allows cancellation of any backend preparation (writing a
	// Seatbelt profile to disk, etc.).
	WrapBashCommand(ctx context.Context, command, shell string) (name string, args []string, err error)

	// ShouldSandboxCommand reports whether the command should be wrapped.
	// Honors the excluded_commands list (commands the user has opted
	// out of the sandbox). Returns false when Enabled() is false.
	ShouldSandboxCommand(command string) bool

	// HostAllowed reports whether outbound network requests to host are
	// permitted by the sandbox policy, plus a short reason when denied.
	// When Enabled() is false this returns (true, "") — no enforcement.
	//
	// Non-bash tools (WebFetch, the http hook driver) consult this
	// before issuing requests so the same allow-list governs every
	// network egress path in the agent. The bash sandbox enforces the
	// same policy at the OS level via the in-process proxy.
	HostAllowed(host string) (ok bool, reason string)

	// ProxyAddress returns the listen address of the in-process HTTP
	// proxy ("127.0.0.1:<port>") when one is running, otherwise "".
	// Surfaces in `buildmax sandbox status`; backends use it to direct
	// child processes via HTTP_PROXY env.
	ProxyAddress() string

	// ScrubEnv returns env with secret-shaped variables removed. Applied
	// by the Bash tool before composing the sandboxed child env so
	// agent-process secrets (API keys, tokens, worker auth) do not
	// leak into untrusted subprocesses. Returns input unchanged when
	// Enabled() is false.
	ScrubEnv(env []string) []string

	// AllowUnsandboxed reports whether the per-call
	// `dangerously_disable_sandbox` arg is honored. When false ("strict
	// sandbox mode"), the arg is ignored and the call is wrapped
	// regardless. Mirrors Claude Code's allowUnsandboxedCommands.
	AllowUnsandboxed() bool

	// ChildEnv returns env-var "KEY=VALUE" entries the caller should add
	// to spawned child processes when the sandbox is active. Includes
	// HTTP_PROXY routing so cooperating tools (curl, wget, git http)
	// reach the network through the host-filtered proxy. Returns nil
	// when no injection is needed. Unlike backend-specific env knobs
	// (bwrap --setenv, sandbox-exec -D), this entry point works on every
	// platform because the bash tool sets cmd.Env itself.
	ChildEnv() []string
}

// NoopSandbox is a SandboxView whose Enabled() is always false. It is the
// default when no sandbox is configured; tools that hold a NoopSandbox
// behave exactly as they did before the sandbox subsystem existed.
type NoopSandbox struct{}

// Enabled returns false. NoopSandbox represents "sandbox subsystem is
// inactive on this run" — tools should fall back to pre-sandbox behavior.
func (NoopSandbox) Enabled() bool { return false }

// Mode returns "" because no sandbox is active.
func (NoopSandbox) Mode() string { return "" }

// Backend returns "none" because no OS backend is providing isolation.
func (NoopSandbox) Backend() string { return "none" }

// NetworkIsolated is false: nothing is wrapped.
func (NoopSandbox) NetworkIsolated() bool { return false }

// WrapBashCommand returns ("", nil, nil) — the caller falls back to its
// own default invocation, leaving today's behavior unchanged.
func (NoopSandbox) WrapBashCommand(_ context.Context, _, _ string) (string, []string, error) {
	return "", nil, nil
}

// ShouldSandboxCommand always returns false when the sandbox is inactive.
func (NoopSandbox) ShouldSandboxCommand(_ string) bool { return false }

// HostAllowed always returns (true, "") — no enforcement when the sandbox
// is inactive.
func (NoopSandbox) HostAllowed(_ string) (bool, string) { return true, "" }

// ProxyAddress returns "" — NoopSandbox runs no proxy.
func (NoopSandbox) ProxyAddress() string { return "" }

// ChildEnv returns nil — no env injection when the sandbox is inactive.
func (NoopSandbox) ChildEnv() []string { return nil }

// ScrubEnv returns env unchanged — no scrubbing when the sandbox is inactive.
func (NoopSandbox) ScrubEnv(env []string) []string { return env }

// AllowUnsandboxed returns true — there's no sandbox to opt out of.
func (NoopSandbox) AllowUnsandboxed() bool { return true }

// SandboxInfo is the snapshot of sandbox state stamped onto HookInput so
// hooks can attribute and policy-check without reading settings themselves.
//
// Populated by RunLoop from the active SandboxView (see Phase E). Fields
// are zero-value when the sandbox is inactive.
type SandboxInfo struct {
	Enabled    bool     `json:"enabled,omitempty"`
	Mode       string   `json:"mode,omitempty"`
	Backend    string   `json:"backend,omitempty"`
	Sources    []string `json:"sources,omitempty"`
	Downgraded bool     `json:"downgraded,omitempty"`
	// NetworkIsolated is true when Bash ran in its own network namespace,
	// reaching the network only through the sandbox proxy.
	NetworkIsolated bool `json:"network_isolated,omitempty"`
}
