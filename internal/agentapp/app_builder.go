package agentapp

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/icloudbb/buildmax/internal/agentapp/job"
	"github.com/icloudbb/buildmax/internal/agentapp/worktree"
	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/core/agent"
	corehook "github.com/icloudbb/buildmax/internal/core/hook"
	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/icloudbb/buildmax/internal/core/localproject"
	"github.com/icloudbb/buildmax/internal/infra/hook"
	"github.com/icloudbb/buildmax/internal/infra/runrelay"
	"github.com/icloudbb/buildmax/internal/util/secretscan"
)

// resolvedAgentAppConfig is the immutable input to runtime construction. File,
// environment, plugin, hook, and sandbox precedence are all settled before a
// resource-owning AgentApp exists.
type resolvedAgentAppConfig struct {
	workspaceRoot string
	// project is the zero value when the surface did not ask for one, and
	// projects is then nil.
	project           localproject.Project
	projects          *ProjectManager
	projectReport     ProjectReport
	memoryUnavailable string
	settings          config.Settings
	plugins           PluginSnapshot
	loadedPlugins     []config.DiscoveredPlugin
	hooks             corehook.Config
	pluginHooks       corehook.Config
	sandbox           config.SandboxResolution
}

func resolveAgentAppConfig(cfg AppConfig) (resolvedAgentAppConfig, error) {
	workspaceRoot, err := resolveWorkspaceRoot(cfg.WorkspaceDir)
	if err != nil {
		return resolvedAgentAppConfig{}, err
	}
	if err := ValidateInstructionLayers(cfg.SpaceAgentInstructions, cfg.AdditionalSystemPrompt); err != nil {
		return resolvedAgentAppConfig{}, err
	}
	settings, err := config.LoadSettings()
	if err != nil {
		return resolvedAgentAppConfig{}, fmt.Errorf("load settings: %w", err)
	}
	if len(cfg.ModelEntries) > 0 {
		settings.Models = append([]config.ModelEntry(nil), cfg.ModelEntries...)
		settings.DefaultModel = cfg.DefaultModel
	}

	plugins := discoverPlugins()
	plugins.resolveBase(context.Background())
	loadedPlugins := plugins.Loadable()
	workspaceHooks, err := config.LoadWorkspaceHooks(workspaceRoot)
	if err != nil {
		return resolvedAgentAppConfig{}, fmt.Errorf("load workspace hooks: %w", err)
	}
	pluginHooks := config.ResolvePluginHooks(loadedPlugins)
	plugins.addFindings(pluginHooks.Findings...)

	policySandbox, err := config.LoadPolicySandbox()
	if err != nil {
		return resolvedAgentAppConfig{}, fmt.Errorf("load policy sandbox: %w", err)
	}
	surface := cfg.SandboxSurface
	if surface == "" {
		surface = config.SandboxSurfaceCLI
	}

	// Resolved here, before anything owning a resource exists, so a Project
	// that could not be persisted stops construction rather than producing a
	// runtime whose sessions have an identity nothing can resolve.
	var (
		project           localproject.Project
		projects          *ProjectManager
		projectReport     ProjectReport
		memoryUnavailable string
	)
	if cfg.EnableLocalProject {
		projects = NewProjectManager(config.ProjectsDir())
		project, projectReport, err = projects.ResolveReporting(context.Background(), workspaceRoot)
		if err != nil {
			return resolvedAgentAppConfig{}, fmt.Errorf("resolve local project: %w", err)
		}
		// Probed once, here, because it decides whether the tools are
		// registered at all and the tool registry is built once per model. A
		// store that cannot be read is a directory-level failure, not the
		// per-file kind that can change under a run.
		if _, memErr := projects.Store().Memories(context.Background(), project.ID); memErr != nil {
			memoryUnavailable = memErr.Error()
		}
	}

	return resolvedAgentAppConfig{
		workspaceRoot:     workspaceRoot,
		project:           project,
		projects:          projects,
		projectReport:     projectReport,
		memoryUnavailable: memoryUnavailable,
		settings:          settings,
		plugins:           plugins,
		loadedPlugins:     loadedPlugins,
		hooks:             config.MergeHooks(settings.Hooks, pluginHooks.Config, workspaceHooks),
		pluginHooks:       pluginHooks.Config,
		sandbox: config.ResolveSandboxForRun(settings.Sandbox, cfg.SandboxRunOverride, policySandbox, surface,
			config.TierSandboxConfig(cfg.SandboxNetworkTier, cfg.SandboxFilesystemTier, cfg.SandboxSharedPaths)),
	}, nil
}

// buildAgentApp opens runtime resources from already-resolved configuration.
// Any partial construction is closed before an error is returned.
func buildAgentApp(cfg AppConfig, resolved resolvedAgentAppConfig) (_ *AgentApp, err error) {
	workspace := NewMovableRoot(resolved.workspaceRoot)
	sandboxManager, err := buildSandboxManager(resolved.sandbox, workspace)
	if err != nil {
		return nil, err
	}
	// This run's Secret grant names pass env scrubbing; BuildMax's own
	// credentials never do, whatever is passed. See docs/design/space-secrets.md.
	sandboxManager.AllowEnvNames(cfg.SecretEnvNames)

	app := &AgentApp{
		workspace:                   workspace,
		project:                     resolved.project,
		projects:                    resolved.projects,
		projectReport:               resolved.projectReport,
		memoryUnavailable:           resolved.memoryUnavailable,
		memoryDisabled:              cfg.DisableProjectMemory,
		settings:                    resolved.settings,
		toolRegistries:              make(map[string]cllm.ToolRegistry),
		sessionManager:              NewSessionManager(config.SessionsDir()).ForProject(resolved.project.ID),
		skillsRegistry:              &SkillRegistry{},
		subagentsRegistry:           &SubAgentRegistry{},
		policy:                      NewConfiguredPolicy(config.ResolvePermissions(resolved.settings.Tools), cfg.Policy),
		additionalSystemPrompt:      cfg.AdditionalSystemPrompt,
		additionalSystemPromptLayer: cfg.AdditionalSystemPromptLayer,
		spaceAgentInstructions:      cfg.SpaceAgentInstructions,
		runProvenance:               cfg.RunProvenance,
		artifactPublisher:           cfg.ArtifactPublisher,
		issue:                       cfg.Issue,
		sandbox:                     agent.SandboxView(sandboxManager),
		sandboxManager:              sandboxManager,
		sandboxResolved:             resolved.sandbox,
		unattendedWorker:            cfg.UnattendedWorker,
		maxIterations:               config.ResolveMaxIterations(resolved.settings.Agent, cfg.MaxIterations),
		plugins:                     resolved.plugins,
		secretEnvValues:             cfg.SecretEnvValues,
		secretRedactor:              secretscan.NewRedactor(cfg.SecretEnvValues),
		webSearchAPIKey:             resolved.settings.WebSearch.APIKey,
	}
	if cfg.WebSearchAPIKey != "" {
		app.webSearchAPIKey = cfg.WebSearchAPIKey
	}
	// A worker that resolves weaker than its own surface's baseline says so
	// out loud, not only in the trace: docs/design/sandbox-boundaries.md §10.
	// info is nil only when app.sandbox is nil, which sandboxManager above
	// never leaves it.
	if info := app.sandboxInfo(); info != nil && info.Downgraded {
		slog.Warn("sandbox resolved weaker than this surface's own baseline",
			"enabled", info.Enabled, "mode", info.Mode, "backend", info.Backend, "sources", info.Sources)
	}

	complete := false
	defer func() {
		if !complete {
			_ = app.Close()
		}
	}()

	app.llmClients = &LLMClientCache{
		settings:          app.settings,
		managedServerURL:  cfg.ManagedServerURL,
		managedToken:      cfg.ManagedToken,
		managedTaskRunID:  cfg.ManagedTaskRunID,
		managedHTTPClient: cfg.ManagedHTTPClient,
		surface:           cfg.Surface,
		clients:           make(map[string]cllm.LLMClient),
	}
	// Remote Control: if this session opted in and has a managed server to reach,
	// dial out and register so another device can watch it. Fail-open — a relay
	// that cannot connect never disturbs the local run.
	if cfg.RemoteControl && cfg.ManagedServerURL != "" && cfg.ManagedToken != nil {
		host, _ := os.Hostname()
		name := cfg.RemoteControlName
		if name == "" {
			name = host
		}
		serverURL := cfg.ManagedServerURL
		app.remoteRelay = runrelay.New(runrelay.Config{
			ServerURL:   serverURL,
			TokenFunc:   func() (string, error) { return cfg.ManagedToken(serverURL) },
			HTTPClient:  cfg.ManagedHTTPClient,
			DisplayName: name,
			Platform:    cfg.Surface,
			Host:        host,
			OnRegistered: func(sessionID string) {
				slog.Info("remote control active — this session is now reachable from another device",
					"session_id", sessionID, "server", serverURL)
			},
			OnRemotePrompt:   cfg.RemotePromptHandler,
			OnRemoteApproval: cfg.RemoteApprovalHandler,
			OnRemoteCancel:   cfg.RemoteCancelHandler,
		})
		app.remoteRelay.Start(context.Background())
	}
	if cfg.EnableMCP {
		mcpResolution, resolveErr := config.ResolveMCPConfig(app.workspace.Root(), resolved.loadedPlugins)
		if resolveErr != nil {
			return nil, fmt.Errorf("load mcp config: %w", resolveErr)
		}
		app.plugins.addFindings(mcpResolution.Findings...)
		app.plugins.addShadowed(mcpResolution.Shadowed...)
		// The supported unattended-worker profile disables stdio MCP: refuse the
		// run here, before NewMCPManager starts any child process and before the
		// first model call. See docs/design/trust-harness.md §3.9.
		if cfg.UnattendedWorker {
			if rejectErr := rejectWorkerStdioMCP(mcpResolution.Config); rejectErr != nil {
				return nil, rejectErr
			}
		}
		app.mcpRemoteTransports = resolvedRemoteTransports(mcpResolution.Config)
		app.mcpManager, err = NewMCPManager(context.Background(), mcpResolution.Config)
		if err != nil {
			return nil, err
		}
	}

	hookDeps := hook.Deps{
		LLMCaller: &llmCaller{cache: app.llmClients, defaultModel: app.DefaultModelName},
		Sandbox:   app.sandbox,
	}
	if app.mcpManager != nil {
		hookDeps.MCPCaller = &mcpCaller{m: app.mcpManager}
	}
	app.hookManager = NewHookManager(resolved.hooks, hook.NewDriverRegistry(hookDeps))
	app.hooks = app.hookManager
	app.pluginHooks = resolved.pluginHooks

	if err = app.skillsRegistry.Load(app.workspace.Root(), resolved.loadedPlugins); err != nil {
		return nil, err
	}
	if err = app.subagentsRegistry.Load(app.workspace.Root(), resolved.loadedPlugins); err != nil {
		return nil, err
	}
	app.plugins.addFindings(app.skillsRegistry.findings...)
	app.plugins.addShadowed(app.skillsRegistry.shadowed...)
	app.plugins.addFindings(app.subagentsRegistry.findings...)
	app.plugins.addShadowed(app.subagentsRegistry.shadowed...)

	if cfg.EnableWorktrees {
		// sessionRoot, not the bare root: moving it must re-resolve the
		// configuration the root decides, or the session runs one tree's hooks
		// and skills against another tree's files.
		app.worktrees = worktree.NewManager(sessionRoot{app: app}).WithHooks(app.hooks)
	}
	if cfg.EnableBackgroundJobs {
		app.jobs = job.NewManager()
		if config.TraceEnabled() {
			events, _ := app.jobs.Subscribe("")
			app.jobTraceDone = make(chan struct{})
			go func() {
				defer close(app.jobTraceDone)
				logJobEvents(config.TracesDir(), events)
			}()
		}
	}
	complete = true
	return app, nil
}
