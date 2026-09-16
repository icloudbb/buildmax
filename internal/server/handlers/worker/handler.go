// Package worker serves the routes a running worker calls back on.
//
// Its boundary is a different credential, not a different feature: every route
// here authenticates with the run token that names one task run, never with a
// user's access token. Sharing a Handler with the user-facing routes meant one
// type answered to both credentials, and the only thing keeping a worker route
// from reading a user's session was that nobody had written it.
package worker

import (
	"context"
	"github.com/icloudbb/buildmax/internal/server/handlers/runterminal"
	"net/http"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	"github.com/icloudbb/buildmax/internal/core/eligibility"
	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/infra/workerclient"
	"github.com/icloudbb/buildmax/internal/server/websocket"
	artifactsvc "github.com/icloudbb/buildmax/internal/service/artifact"
	"github.com/icloudbb/buildmax/internal/service/llmgateway"
	pluginsvc "github.com/icloudbb/buildmax/internal/service/plugin"
	workspacesvc "github.com/icloudbb/buildmax/internal/service/workspace"
)

// SpaceSandboxDefaultsReader is the only space capability a run token receives
// beyond plugin activation: the tiers an agent that declares neither inherits.
// See resolveSandboxTiers. In particular, the worker surface cannot read
// membership or change anything about the space.
type SpaceSandboxDefaultsReader interface {
	GetSpace(ctx context.Context, spaceID string) (*corespace.Space, error)
}

type Config struct {
	// JWTSecret verifies the run token every route here requires. Empty means
	// this deployment mints none, so no worker call can be authenticated.
	JWTSecret string
	// WorkerLLM tells a worker how to reach a model. Nil means direct.
	WorkerLLM *workerclient.TaskRunLLM

	TaskRuns coretask.RunStore
	Agents   agentdef.Store
	// Eligible re-checks, when a worker fetches its run, that the run's initiator
	// may still run work in its Space. It closes the race where an account is
	// disabled or removed between the scheduler's dispatch check and the worker
	// starting. Nil skips the check, matching a deployment that wires no
	// authority stores. See docs/proposals/personnel-deactivation-lifecycle.md §7.
	Eligible eligibility.Checker
	// Spaces resolves a run's space default sandbox tiers -- what an agent that
	// declares neither inherits. Nil means no space falls through beyond the
	// agent's own declaration.
	Spaces SpaceSandboxDefaultsReader
	// Activations resolves what a run's space activated. Nil means this
	// deployment cannot, which is a refusal only for an agent that names a
	// plugin.
	Activations ActivationReader
	// Plugins serves the package bytes a run's pins name. Nil answers 503 on
	// the download route.
	Plugins *pluginsvc.Service
	Gateway *llmgateway.Service
	Hub     websocket.StreamHub
	// Artifacts lets a run's agent keep a file for the space. Nil means this
	// deployment has no artifact store, and the route answers 503 — which is
	// also what makes the worker leave the tool unregistered.
	Artifacts *artifactsvc.Service
	// Issues lets a run's agent read the Issue its task names and add one
	// comment to it. Nil answers 503, and a run whose task names no Issue gets
	// 404 from a configured one — either way the worker leaves the tools
	// unregistered rather than offering ones that always fail.
	Issues IssueAccess
	// Secrets decrypts a run's declared Space Secret grants. Nil means the
	// feature is off; the secrets route then returns an empty grant set, which
	// is correct because no agent could have saved a consumption config.
	Secrets SecretMaterializer
	// SecretAudit records what a run was granted. Nil records nothing, which is
	// fail-open: a run that got its grant is not failed for a missing audit.
	SecretAudit SecretGrantRecorder

	// Checkpoints finalizes a seed a worker captured and uploaded. Nil disables
	// the workspace-checkpoint route, which is what a deployment with no
	// checkpoint storage has.
	Checkpoints *workspacesvc.Service
	// WorkspaceRuns reads a run's base checkpoint and records its restore
	// outcome. Nil disables the base and restore routes.
	WorkspaceRuns WorkspaceRunStore

	// OnTerminal is fired once a run reaches a terminal status, after the hub
	// has been told. The server supplies it; this package does not know who is
	// listening.
	OnTerminal func(ctx context.Context, info coretask.RunTerminalInfo)
	// TerminalGroup owns those callbacks so a shutdown waits for them instead
	// of dropping them.
	TerminalGroup *runterminal.Group
}

// ActivationReader is the only space-plugin capability a run token receives.
// In particular, the worker surface cannot activate or repin a plugin.
type ActivationReader interface {
	GetPluginActivation(ctx context.Context, spaceID, pluginName string) (*coreplugin.Activation, error)
}

type Handler struct{ cfg Config }

// New builds the worker API. A nil Hub gets one of its own, which is what the
// unified handler did: a deployment with nobody watching still has runs to
// stream.
func New(cfg Config) *Handler {
	if cfg.Hub == nil {
		cfg.Hub = websocket.NewStreamHub()
	}
	return &Handler{cfg: cfg}
}

// Register adds the worker API.
//
// Every route is scoped to one run, so every route authenticates with that
// run's token.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/worker/task-runs/{task_run_id}", h.runScopedWorkerMiddleware(http.HandlerFunc(h.getTaskRun)))
	mux.Handle("PATCH /api/worker/task-runs/{task_run_id}", h.runScopedWorkerMiddleware(http.HandlerFunc(h.patchTaskRun)))
	mux.Handle("POST /api/worker/task-runs/{task_run_id}/stream", h.runScopedWorkerMiddleware(http.HandlerFunc(h.postStream)))
	mux.Handle("POST /api/worker/task-runs/{task_run_id}/artifacts", h.runScopedWorkerMiddleware(http.HandlerFunc(h.postArtifact)))
	// Workspace checkpoints: the base a run restores from, how that restore
	// ended, and the seed it captures before executing. See
	// docs/design/task-workspace-checkpoints.md §14.1.
	mux.Handle("GET /api/worker/task-runs/{task_run_id}/workspace-base", h.runScopedWorkerMiddleware(http.HandlerFunc(h.getWorkspaceBase)))
	mux.Handle("POST /api/worker/task-runs/{task_run_id}/workspace-restore", h.runScopedWorkerMiddleware(http.HandlerFunc(h.postWorkspaceRestore)))
	mux.Handle("POST /api/worker/task-runs/{task_run_id}/workspace-checkpoints", h.runScopedWorkerMiddleware(http.HandlerFunc(h.postWorkspaceCheckpoint)))
	// The Issue this run's task names, and one comment on it. There is no
	// update route: what an agent may not say about its work is decided by the
	// absence of the route, not by the tool that would have called it.
	// The run's resolved Secret env grants, on their own route so the values
	// ride a no-store, unlogged response. See docs/design/space-secrets.md §7.
	mux.Handle("GET /api/worker/task-runs/{task_run_id}/secrets", h.runScopedWorkerMiddleware(http.HandlerFunc(h.getTaskRunSecrets)))
	mux.Handle("GET /api/worker/task-runs/{task_run_id}/issue", h.runScopedWorkerMiddleware(http.HandlerFunc(h.getRunIssue)))
	mux.Handle("POST /api/worker/task-runs/{task_run_id}/issue/comments", h.runScopedWorkerMiddleware(http.HandlerFunc(h.postRunIssueComment)))
	// Inference authenticates the same way but reads the claims itself: it
	// attributes the call to the token's user and space rather than only
	// admitting it.
	mux.HandleFunc("POST /api/worker/task-runs/{task_run_id}/llm/completions", h.workerLLMCompletionsHandler)
	// The bytes of a release this run is pinned to. Scoped like everything else
	// here, and to the run's own pins besides.
	mux.Handle("GET /api/worker/task-runs/{task_run_id}/plugins/{plugin_name}/{version}/download",
		h.runScopedWorkerMiddleware(http.HandlerFunc(h.downloadPluginPackage)))
}

func (h *Handler) announcer() *runterminal.Announcer {
	return &runterminal.Announcer{Runs: h.cfg.TaskRuns, Hub: h.cfg.Hub, On: h.cfg.OnTerminal, Group: h.cfg.TerminalGroup}
}
