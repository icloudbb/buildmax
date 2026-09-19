package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/core/agent"
	"github.com/icloudbb/buildmax/internal/util"
)

// backend is the platform-specific wrap implementation. One concrete
// backend per (GOOS, sandbox tool) pair lives in its own file behind a
// build tag. backend is intentionally unexported — only Manager calls it.
type backend interface {
	// Name returns the canonical backend label exposed to operators
	// ("bwrap", "seatbelt").
	Name() string
	// Wrap returns the (binary, argv) to exec for the given inner-shell
	// invocation. command is the user's command string; shell is the
	// inner shell (e.g. /bin/bash) the wrap should invoke. workspace
	// is the writable cwd inside the sandbox.
	Wrap(ctx context.Context, p WrapParams) (name string, args []string, err error)
	// Close releases any backend resources (e.g. profile files on disk).
	Close() error
}

// WrapParams is the per-call input every backend needs.
type WrapParams struct {
	Command   string               // user's command string
	Shell     string               // inner shell binary (/bin/bash, /bin/sh)
	Workspace string               // absolute cwd; binds writable
	Cfg       config.SandboxConfig // resolved sandbox config
	ProxyAddr string               // "127.0.0.1:<port>" or "" when no proxy is running
	// RunBridgeSocket is the worker run bridge's Unix socket path, when this run
	// has one. The socket lives under /tmp, which the sandbox masks with a
	// private tmpfs, so the backend re-exposes just this path — otherwise the
	// `buildmax` command an Agent runs cannot reach the Server. Empty off the
	// worker plane. See docs/design/agent-bridge-cli.md.
	RunBridgeSocket string
}

// Manager is the SandboxView implementation. Built by NewManager from a
// resolved config; immutable after construction (Refresh in Phase E).
//
// The Manager always satisfies agent.SandboxView even when the backend is
// unavailable: Enabled() reports false in that case and tools fall back
// to the unsandboxed path. When cfg.FailIfUnavailable is true the caller
// (agentapp / worker bootstrap) is expected to check Unavailable() and
// refuse to start instead of running unsandboxed.
type Manager struct {
	cfg        config.SandboxConfig
	workspace  util.Workspace
	deps       DepsReport
	backend    backend // nil when sandbox is disabled or unavailable
	logger     *slog.Logger
	matcher    *HostMatcher    // shared with proxy; never nil
	proxy      *Proxy          // nil when sandbox is disabled or unavailable
	violations *ViolationStore // always present; non-nil even when disabled
	// unavailableReason explains Unavailable()==true beyond what
	// Deps().FirstMissingRequired() can: that only names a missing binary,
	// not a present one that failed to init or failed probeBackend. Empty
	// when the backend is available or the sandbox was never enabled.
	unavailableReason string
	// allowedEnvNames are the environment variable names this run declared as
	// Space Secret grants, which ScrubEnv admits even though they are
	// secret-shaped. BuildMax's own credentials are never admitted. Empty on
	// every surface that consumes no Secret. See docs/design/space-secrets.md.
	allowedEnvNames map[string]bool
}

// NewManager builds a Manager for the given config and workspace. workspace
// reports the writable cwd inside the sandbox, and is read per wrap: the root
// moves when a session enters a worktree, and a profile built from the launch
// directory would deny every write in the new tree.
//
// Returns a Manager even when the backend is unavailable so callers always
// have a SandboxView to inject. Check Unavailable() to detect the case.
func NewManager(cfg config.SandboxConfig, workspace util.Workspace, logger *slog.Logger) (*Manager, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if workspace == nil {
		return nil, fmt.Errorf("sandbox: no workspace")
	}
	m := &Manager{
		cfg:        cfg,
		workspace:  workspace,
		deps:       CheckDeps(),
		logger:     logger,
		matcher:    NewHostMatcher(cfg.Network.AllowedDomains, cfg.Network.DeniedDomains),
		violations: NewViolationStore(DefaultViolationCapacity),
	}
	if !cfg.Enabled {
		return m, nil
	}
	if !m.deps.AllRequiredOK() {
		miss := m.deps.FirstMissingRequired()
		logger.Warn("sandbox enabled but backend unavailable",
			"backend", m.deps.Backend,
			"missing", miss.Name,
			"hint", miss.Hint)
		return m, nil
	}
	b, err := newBackend(m.deps.Backend)
	if err != nil {
		m.unavailableReason = fmt.Sprintf("%s backend init failed: %v", m.deps.Backend, err)
		logger.Warn("sandbox backend init failed; falling back to unsandboxed",
			"backend", m.deps.Backend, "err", err)
		return m, nil
	}
	// CheckDeps only proved the backend binary is on PATH; it may still be
	// unable to actually confine a command on this host (a container whose
	// seccomp policy blocks the syscalls bwrap needs, for example) --
	// proven in production, where a worker ran the backend and it let a
	// write outside the workspace through. Probing here makes that the same
	// "unavailable" Unavailable()/FailIfUnavailable already know how to
	// refuse to start on, instead of a silent pass-through.
	if err := probeBackend(context.Background(), b, cfg); err != nil {
		m.unavailableReason = fmt.Sprintf("%s backend probe failed: %v", m.deps.Backend, err)
		logger.Warn("sandbox backend probe failed; falling back to unsandboxed",
			"backend", m.deps.Backend, "err", err)
		_ = b.Close()
		return m, nil
	}
	m.backend = b
	// Start the in-process HTTP proxy so the bash sandbox can route
	// outbound traffic through a single host-filtered choke point.
	filter := NewIgnoreFilter(m.violations, func(tool string) []string {
		return m.cfg.IgnoredViolationsFor(tool)
	})
	prox, err := NewProxy(m.matcher, &proxyViolator{filter: filter})
	if err != nil {
		// A proxy startup failure is not fatal: the sandbox can still
		// enforce filesystem isolation. Log so operators see it, but
		// leave the manager active.
		logger.Warn("sandbox proxy: start failed; bash will run without network filter",
			"err", err)
	} else {
		m.proxy = prox
	}
	return m, nil
}

// Deps returns the dependency report.
func (m *Manager) Deps() DepsReport {
	if m == nil {
		return DepsReport{}
	}
	return m.deps
}

// Config returns the resolved sandbox config the manager runs against.
func (m *Manager) Config() config.SandboxConfig {
	if m == nil {
		return config.SandboxConfig{}
	}
	return m.cfg
}

// Unavailable reports whether the sandbox is enabled in settings but the
// host backend cannot run. Used by FailIfUnavailable gating at startup.
func (m *Manager) Unavailable() bool {
	if m == nil {
		return false
	}
	if !m.cfg.Enabled {
		return false
	}
	return m.backend == nil
}

// UnavailableReason explains why Unavailable() is true, when the cause is
// something Deps().FirstMissingRequired() cannot name: a backend binary that
// was present but failed to initialize, or failed its own confinement
// probe. Empty whenever the reason is fully covered by a missing required
// dependency, or the backend is available.
func (m *Manager) UnavailableReason() string {
	if m == nil {
		return ""
	}
	return m.unavailableReason
}

// --- agent.SandboxView ---

// Enabled reports whether the sandbox is active on this run. False when
// disabled in settings, or enabled but backend unavailable.
func (m *Manager) Enabled() bool {
	if m == nil {
		return false
	}
	return m.cfg.Enabled && m.backend != nil
}

// Mode returns "auto_allow" or "regular" based on the resolved config.
// Returns "" when Enabled() is false.
func (m *Manager) Mode() string {
	if !m.Enabled() {
		return ""
	}
	return m.cfg.EffectiveMode()
}

// Backend returns the active backend label ("bwrap", "seatbelt", "none").
func (m *Manager) Backend() string {
	if m == nil || m.backend == nil {
		return "none"
	}
	return m.backend.Name()
}

// ShouldSandboxCommand reports whether the command should be wrapped.
// Honors the excluded_commands list and the enabled state.
func (m *Manager) ShouldSandboxCommand(command string) bool {
	if !m.Enabled() {
		return false
	}
	if MatchesExcluded(command, m.cfg.ExcludedCommands) {
		return false
	}
	return true
}

// HostAllowed reports whether a host is reachable per the sandbox network
// policy. When Enabled() is false this returns (true, "") — no enforcement.
//
// Non-bash tools (WebFetch, the http hook driver) should consult this
// before issuing requests; the bash sandbox enforces the same policy at
// the OS level via the in-process proxy.
func (m *Manager) HostAllowed(host string) (bool, string) {
	if !m.Enabled() {
		return true, ""
	}
	return m.matcher.Allowed(host)
}

// ProxyAddress returns the listen address of the in-process HTTP proxy,
// or "" when no proxy is running. Backends use this to inject HTTP_PROXY
// env into the sandboxed shell.
func (m *Manager) ProxyAddress() string {
	if m == nil || m.proxy == nil {
		return ""
	}
	return m.proxy.Addr()
}

// Violations returns the manager's bounded ring buffer of allow/deny
// decisions. Never nil — even when the sandbox is disabled, the store
// exists (and stays empty). Surfaced by `buildmax sandbox status`.
func (m *Manager) Violations() *ViolationStore {
	if m == nil {
		return nil
	}
	return m.violations
}

// Proxy returns the underlying *Proxy (or nil). Exposed for the CLI's
// status display and tests; not for tools.
func (m *Manager) Proxy() *Proxy {
	if m == nil {
		return nil
	}
	return m.proxy
}

// ScrubEnv removes secret-shaped variables from env, keeping the names this
// run declared as Secret grants (AllowEnvNames). When the sandbox is disabled
// this returns env unchanged (no enforcement).
func (m *Manager) ScrubEnv(env []string) []string {
	if !m.Enabled() {
		return env
	}
	return ScrubEnvList(env, m.allowedEnvNames)
}

// AllowEnvNames records the environment variable names this run declared as
// Space Secret grants, so ScrubEnv admits them. Called once as the runtime is
// assembled; BuildMax's own credentials are never admitted, whatever is passed.
func (m *Manager) AllowEnvNames(names []string) {
	if len(names) == 0 {
		m.allowedEnvNames = nil
		return
	}
	allowed := make(map[string]bool, len(names))
	for _, n := range names {
		if n != "" && !isAlwaysDenyEnvName(n) {
			allowed[n] = true
		}
	}
	m.allowedEnvNames = allowed
}

// AllowUnsandboxed reports whether the per-call dangerously_disable_sandbox
// arg is honored. Mirrors Claude Code's allowUnsandboxedCommands.
func (m *Manager) AllowUnsandboxed() bool {
	if m == nil {
		return true
	}
	return m.cfg.EffectiveAllowUnsandboxed()
}

// ChildEnv returns the env-var entries the bash tool should add to
// sandboxed child processes. Phase C populates the HTTP_PROXY family so
// cooperating tools (curl, wget, git http) reach the network via the
// in-process proxy.
//
// NO_PROXY is set to the proxy address itself only — every other
// destination, including 127.0.0.1, must route through the proxy so the
// host-allow-list applies. Operators who need direct loopback access
// can opt in via sandbox.network.allow_local_binding (deferred).
func (m *Manager) ChildEnv() []string {
	if !m.Enabled() || m.proxy == nil {
		return nil
	}
	proxyURL := "http://" + m.proxy.Addr()
	return []string{
		"HTTP_PROXY=" + proxyURL,
		"HTTPS_PROXY=" + proxyURL,
		"http_proxy=" + proxyURL,
		"https_proxy=" + proxyURL,
		"ALL_PROXY=" + proxyURL,
		"all_proxy=" + proxyURL,
		// Empty NO_PROXY so even localhost requests are filtered by
		// the allow-list. Setting it explicitly so any operator
		// environment value does not bleed in.
		"NO_PROXY=",
		"no_proxy=",
	}
}

// WrapBashCommand returns the (name, args) to exec for an isolated run of
// the given command. Returns ("", nil, nil) when the caller should not
// wrap — either because the sandbox is disabled, the command is in
// excluded_commands, or the backend is unavailable. Tools should treat
// ("", nil, nil) as "fall back to your own invocation."
func (m *Manager) WrapBashCommand(ctx context.Context, command, shell string) (string, []string, error) {
	if !m.ShouldSandboxCommand(command) {
		return "", nil, nil
	}
	// Absolute here rather than at construction: the root can move, and the
	// backend profile is written against the directory in force right now.
	workspace, err := filepath.Abs(m.workspace.Root())
	if err != nil {
		return "", nil, fmt.Errorf("sandbox: resolve workspace: %w", err)
	}
	return m.backend.Wrap(ctx, WrapParams{
		Command:   command,
		Shell:     shell,
		Workspace: workspace,
		Cfg:       m.cfg,
		ProxyAddr: m.ProxyAddress(),
		// Read per wrap, not at construction: the worker exports it after the
		// Manager is built, and a run without a bridge leaves it empty.
		RunBridgeSocket: os.Getenv(config.EnvKeyBuildmaxBridgeSock),
	})
}

// Close releases backend and proxy resources. Safe to call on nil.
func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	var firstErr error
	if m.proxy != nil {
		if err := m.proxy.Close(); err != nil {
			firstErr = err
		}
	}
	if m.backend != nil {
		if err := m.backend.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Compile-time assertion that *Manager satisfies the agent contract.
var _ agent.SandboxView = (*Manager)(nil)
