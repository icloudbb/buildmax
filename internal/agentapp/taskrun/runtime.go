// Package taskrun provides task-run execution.
//
// RunTask executes a single run in-process: materialize workspace, optionally restore session
// from the previous run, run the agent runtime, persist outputs, and update run state via TaskRunUpdater.
package taskrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/icloudbb/buildmax/internal/agentapp"
	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/core/agent"
	"github.com/icloudbb/buildmax/internal/core/llm"
	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	blob "github.com/icloudbb/buildmax/internal/infra/objectstore"
	"github.com/icloudbb/buildmax/internal/infra/workerclient"
	tool "github.com/icloudbb/buildmax/internal/tool"
	"github.com/icloudbb/buildmax/internal/util"
	"github.com/icloudbb/buildmax/internal/util/secretscan"
)

// Identity belongs in an attr, not in every message string.
func componentLog() *slog.Logger { return slog.With("component", "runtime") }

// TaskRunUpdater is used by the worker to update run status and register artifacts via HTTP.
// When status is SUCCEEDED with artifact, server creates artifact and syncs task denormalized fields.
// When status is FAILED, server syncs task denormalized from run.
type TaskRunUpdater interface {
	UpdateRunStatus(ctx context.Context, taskRunID string, req *workerclient.PatchTaskRunRequest) error
}

// RunScope identifies a task run inside its owning space.
type RunScope struct {
	SpaceID   string
	TaskID    string
	TaskRunID string
}

// runResult is the evidence taskrun persists and reports after execution.
type runResult struct {
	EndTime   time.Time
	OutputStr string
	Output    []byte
	// Structured is the validated structured-output value as JSON text, nil when
	// the run requested no output schema or the value did not validate. See
	// docs/design/structured-output.md.
	Structured       *string
	PromptTokens     *int
	CompletionTokens *int
	// TracePath locates this run's durable trace inside run-global storage,
	// e.g. "traces/<session>/rt_….jsonl". Empty when no trace was written.
	// Recorded because the trace's file name is the agent run id, which is
	// generated inside the run and is not otherwise persisted — without this
	// the uploaded trace could not be found again.
	TracePath string
}

type runDirs struct {
	runDir       string
	runWorkspace string
	runGlobal    string
	// runOSHome is the run's own operating-system HOME, empty at start and not
	// uploaded. It is separate from runWorkspace (the space's materialized files,
	// the Agent's cwd) and runGlobal (BUILDMAX_HOME): a run must not inherit the shared
	// container HOME, or one run's tool state under ~/.config leaks into the
	// next. It is also where rendered credential files will land -- see
	// docs/design/space-secrets.md §8.
	runOSHome string
}

// ManagedInference is what a run needs to reach the managed LLM gateway instead
// of a provider.
//
// It is separate from Model because it is a credential, not configuration: the
// model entry says which alias to call, and this says what authorizes the call.
// The zero value means the run uses a direct model and holds a provider key.
//
// Mirrors the design in docs/design/worker-run-token.md.
type ManagedInference struct {
	// ServerURL is the BuildMax server that minted RunToken. A managed entry
	// naming any other server is refused rather than sent this credential.
	ServerURL string
	// RunToken authorizes this one run's inference calls.
	RunToken string
}

// Enabled reports whether this run can reach the gateway.
func (m ManagedInference) Enabled() bool { return m.ServerURL != "" && m.RunToken != "" }

// managedSurface labels this run's managed calls. The server sets its own label
// for the ledger; this one only reaches client-side diagnostics.
const managedSurface = "worker"

// tokenFunc supplies the run token to agentapp, or nil when this run has none —
// which is what makes a managed model entry fail outright on a direct-mode
// worker instead of quietly reaching a provider some other way.
//
// It refuses any server but the one that minted the token. A model entry is
// configuration and a run token is a credential for one deployment; without this
// check, an entry naming another host would send it there.
func (m ManagedInference) tokenFunc() agentapp.ManagedTokenFunc {
	if !m.Enabled() {
		return nil
	}
	want := strings.TrimRight(m.ServerURL, "/")
	return func(serverURL string) (string, error) {
		if strings.TrimRight(serverURL, "/") != want {
			return "", fmt.Errorf("this run's token is for %s, not %s", want, serverURL)
		}
		return m.RunToken, nil
	}
}

// managedRunScope returns the task run managed calls are made as, or "" when
// this run has no gateway credential.
func managedRunScope(m ManagedInference, taskRunID string) string {
	if !m.Enabled() {
		return ""
	}
	return taskRunID
}

// RunTaskInput holds all inputs for RunTask. Callers build this struct and pass it to RunTask.
type RunTaskInput struct {
	Task *coretask.Task
	// AdditionalSystemPrompt is the instruction text of the agent this task names, resolved by
	// the server for this run. It becomes the last layer of the system prompt, where it is
	// re-sent whole on every call, instead of riding in the task input where compaction
	// eventually drops it.
	AdditionalSystemPrompt string
	SpaceAgentInstructions string
	Run                    *coretask.Run
	SessionID              string
	Paths                  RuntimePaths
	Persist                blob.PersistStorage
	Updater                TaskRunUpdater
	StreamSender           workerclient.StreamSender
	Model                  config.ModelEntry
	Managed                ManagedInference
	// ManagedHTTPClient carries the worker's server trust to managed inference,
	// which reaches the gateway on the same internal listener as every other
	// worker call. Nil uses http.DefaultClient, which is correct only for a
	// plain-HTTP development server. See
	// docs/design/worker-api-network-boundary.md §6.
	ManagedHTTPClient *http.Client
	// WorkerAPI is how this run reaches the server it was dispatched by. Its
	// zero value leaves the run without the artifact capability, so the agent
	// gets no artifact tool rather than one that always fails.
	WorkerAPI workerclient.WorkerAPIClientConfig
	// Plugins are the releases the server resolved for this run. They are
	// materialized into the run's BUILDMAX_HOME before the runtime is
	// assembled; a pin that cannot be materialized fails the run.
	Plugins []coreplugin.Pin
	// Checkpoints writes captured workspace-checkpoint payloads to the object
	// store. Nil leaves the run without workspace continuity — a CLI or eval run
	// that has no server to record the pointer with — so the run seeds and
	// restores nothing rather than failing. See
	// docs/design/task-workspace-checkpoints.md §8.
	Checkpoints CheckpointPayloadStore
	// SandboxNetworkTier and SandboxFilesystemTier are this run's agent-
	// declared sandbox tiers, resolved by the server when the worker claimed
	// the run. Empty means the strictest tier on that axis. See
	// docs/design/agent-sandbox-policy.md.
	SandboxNetworkTier    config.SandboxNetworkTier
	SandboxFilesystemTier config.SandboxFilesystemTier
	// SecretEnvGrants are this run's resolved Space Secret grants, variable name
	// to value, computed by the server from the agent's consumption config.
	// They are set in the run's environment and their names are allow-listed
	// past env scrubbing. Empty when the agent consumes no Secret. See
	// docs/design/space-secrets.md §8.2.
	SecretEnvGrants map[string]string
	// InterruptGrace is how long this run may spend reporting after its process
	// is asked to stop. Zero uses interruptReportTimeout. A dispatcher that will
	// kill the worker on its own deadline passes that deadline here, so the run
	// stops reporting before it is killed mid-upload rather than after.
	InterruptGrace time.Duration
}

// artifactPublisher gives a run the artifact capability, or nil when it has no
// way to reach a server.
//
// A worker holds object-store credentials and could write the bytes itself.
// Going through the server is the point: one code path creates artifacts, and a
// worker is never told which space it is writing to — the run token names the
// run, and the server derives the rest.
func artifactPublisher(cfg workerclient.WorkerAPIClientConfig, taskRunID string) tool.ArtifactPublisher {
	if cfg.BaseURL == "" || cfg.Token == "" || taskRunID == "" {
		return nil
	}
	return workerclient.NewArtifactPublisher(cfg, taskRunID, cfg.BaseURL)
}

// issueClient gives a run the Issue capability, or nil when the run has no
// Issue or no way to reach a server.
//
// The server derives the Issue from the run token and would answer 404 for a
// run whose task names none. The task is checked here anyway, because a tool
// that can only fail should never appear in the tool list at all.
func issueClient(cfg workerclient.WorkerAPIClientConfig, task *coretask.Task, taskRunID string) tool.IssueClient {
	if cfg.BaseURL == "" || cfg.Token == "" || taskRunID == "" {
		return nil
	}
	if task == nil || task.IssueID == nil || *task.IssueID == "" {
		return nil
	}
	return workerclient.NewIssueClient(cfg, taskRunID)
}

// RunTask runs a single task run: materialize workspace, optionally restore session from previous run, execute agent in-process, upload run state to blob, update run and task via updater.
// If input.StreamSender is non-nil, stdout is streamed to the server as deltas; full output is still accumulated for persist and PATCH.
//
// The cause on a dead ctx says which of three things happened. ErrRunCanceled
// means someone asked this run to stop: it is recorded as CANCELED, keeps the
// output and artifacts it had produced, and RunTask returns ErrRunCanceled.
// ErrRunInterrupted means the process was asked to stop while the run was
// working: it keeps the same evidence but is recorded as FAILED, because
// nothing chose to stop it and it did not finish. Any other end of ctx is the
// process going away without warning, which is not this run's outcome to
// report — the stale-run reaper closes those.
func RunTask(ctx context.Context, input RunTaskInput) error {
	task, run := input.Task, input.Run
	if task == nil || run == nil {
		return errors.New("runtime: task and run must not be nil")
	}
	if input.Paths == nil || input.Persist == nil || input.Updater == nil {
		return errors.New("runtime: paths, persist and updater must not be nil")
	}
	dirs := resolveRunDirs(input.Paths, task, run)
	scope := RunScope{SpaceID: task.SpaceID, TaskID: task.ID, TaskRunID: run.ID}

	if err := prepareRunWorkspace(ctx, input, task, run, dirs); err != nil {
		if stopped, stopErr := reportStoppedRun(ctx, scope, runResult{}, dirs, input); stopped {
			return stopErr
		}
		// No partial checkpoint here: preparation failed before execution, so
		// there is no run-produced workspace to preserve — only the seed or the
		// restored base, which are already durable.
		reportRunFailure(ctx, run.ID, err, "", nil, input.Updater)
		return err
	}
	result, err := executeRunTask(ctx, input, task, run, dirs)
	// The stop check comes first because the agent loop treats cancellation as
	// an ordinary end: it returns what it had produced and no error. Judging by
	// err alone would file a stopped run as a completed one.
	if stopped, stopErr := reportStoppedRun(ctx, scope, result, dirs, input); stopped {
		return stopErr
	}
	if err != nil {
		reportPersistedRunState(ctx, input.Persist, scope, dirs, result)
		componentLog().Error("run failed", "task_run_id", run.ID, "err", err, "output_len", len(result.OutputStr))
		// Capture what the failed run produced as a partial checkpoint. It rides
		// the terminal report like a result but never advances the head; it
		// preserves the work for an operator to recover from. Fail-open.
		partial := captureWorkspaceCheckpoint(ctx, input, task, dirs)
		reportRunFailure(ctx, run.ID, err, result.TracePath, partial, input.Updater)
		return err
	}

	reportPersistedRunState(ctx, input.Persist, scope, dirs, result)
	// Capture the successful run's workspace as its result checkpoint and carry it
	// on the terminal report, so the server commits it and advances the Task head
	// as it accepts the outcome. Fail-open: a capture failure leaves the head
	// where it was and the run still succeeds (§13).
	resultCheckpoint := captureWorkspaceCheckpoint(ctx, input, task, dirs)
	if err := reportRunOutcome(ctx, scope, result, coretask.RunStatusSucceeded, "", resultCheckpoint, input.Updater); err != nil {
		return err
	}
	componentLog().Info("run succeeded", "task_run_id", run.ID)
	return nil
}

// reportFinishTimeout bounds the work a canceled run is still allowed to do:
// uploading what it produced and telling the server it stopped. It is generous
// enough for an artifact upload and short enough that a worker asked to stop
// actually stops.
const reportFinishTimeout = 60 * time.Second

// interruptReportTimeout is the same window for a run whose process is being
// shut down, and it is much shorter for a reason it does not choose: something
// else is already counting. Kubernetes gives a pod 30 seconds by default before
// SIGKILL, so a run that spends a cancel's full minute reporting is killed
// mid-upload and reports nothing at all. See docs/design/graceful-shutdown.md §6.3.
const interruptReportTimeout = 15 * time.Second

// runCanceled reports whether this run's context was ended by a cancel request
// rather than by the process shutting down or a deadline passing.
func runCanceled(ctx context.Context) bool {
	return errors.Is(context.Cause(ctx), coretask.ErrRunCanceled)
}

// runInterrupted reports whether this run's context was ended because the
// process executing it was asked to stop.
func runInterrupted(ctx context.Context) bool {
	return errors.Is(context.Cause(ctx), coretask.ErrRunInterrupted)
}

// reportStoppedRun finishes a run that stopped for a reason it can name, and
// reports whether it was one. Cancellation is checked first: a run that was
// cancelled and then caught a shutdown was still cancelled, and that is the
// outcome someone is waiting to see.
func reportStoppedRun(ctx context.Context, scope RunScope, result runResult, dirs runDirs, input RunTaskInput) (bool, error) {
	switch {
	case runCanceled(ctx):
		return true, reportCanceledRun(ctx, scope, result, dirs, input)
	case runInterrupted(ctx):
		return true, reportInterruptedRun(ctx, scope, result, dirs, input)
	default:
		return false, nil
	}
}

// reportCanceledRun finishes a run that was stopped on request.
//
// It does the same reporting a finished run does — upload the run state, keep
// the artifacts, record the outcome — because a canceled run is not a wasted
// one: whatever it produced before stopping is the reason someone will look at
// it. The one difference is the context: the run's own is already dead, so the
// reporting gets a fresh, bounded one, or the cancel would also destroy the
// evidence of what the run had done.
func reportCanceledRun(ctx context.Context, scope RunScope, result runResult, dirs runDirs, input RunTaskInput) error {
	if err := finishStoppedRun(ctx, scope, result, dirs, input, coretask.RunStatusCanceled, "", reportFinishTimeout); err != nil {
		componentLog().Error("could not report a canceled run", "task_run_id", scope.TaskRunID, "err", err)
		return err
	}
	componentLog().Info("run canceled", "task_run_id", scope.TaskRunID, "output_len", len(result.OutputStr))
	return coretask.ErrRunCanceled
}

// reportInterruptedRun finishes a run whose process is shutting down.
//
// It keeps everything a canceled run keeps, and differs in the status and in
// what the record says happened. FAILED rather than a status of its own:
// terminal is what the Portal, the report path, the workflow step machine, and
// quota all need, and a fourth terminal status whose only correct handling is
// "retry it" costs more than it buys until retry exists. The error message is
// what tells a reader this was the cluster and not the agent.
func reportInterruptedRun(ctx context.Context, scope RunScope, result runResult, dirs runDirs, input RunTaskInput) error {
	grace := input.InterruptGrace
	if grace <= 0 {
		grace = interruptReportTimeout
	}
	if err := finishStoppedRun(ctx, scope, result, dirs, input, coretask.RunStatusFailed, coretask.ErrRunInterrupted.Error(), grace); err != nil {
		componentLog().Error("could not report an interrupted run", "task_run_id", scope.TaskRunID, "err", err)
		return err
	}
	componentLog().Info("run interrupted by shutdown", "task_run_id", scope.TaskRunID, "output_len", len(result.OutputStr))
	return coretask.ErrRunInterrupted
}

// finishStoppedRun uploads what a stopped run produced and records its outcome
// on a context of its own.
//
// The detached context is the whole point: the run's own is dead by definition
// here, and reporting on it would destroy the evidence of the work along with
// the run.
func finishStoppedRun(ctx context.Context, scope RunScope, result runResult, dirs runDirs, input RunTaskInput, status coretask.RunStatus, errMessage string, timeout time.Duration) error {
	reportCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	if result.EndTime.IsZero() {
		result.EndTime = time.Now().UTC()
	}
	reportPersistedRunState(reportCtx, input.Persist, scope, dirs, result)
	// A stopped run's workspace is a partial checkpoint: capture it within the
	// same bounded reporting budget as everything else this run still does, and
	// carry it on the terminal report. A partial preserves the work but never
	// advances the Task head. Fail-open — if it does not fit the budget, the run
	// still reports its stop.
	partial := captureWorkspaceCheckpoint(reportCtx, input, input.Task, dirs)
	return reportRunOutcome(reportCtx, scope, result, status, errMessage, partial, input.Updater)
}

func resolveRunDirs(paths RuntimePaths, task *coretask.Task, run *coretask.Run) runDirs {
	runDir := paths.RuntimeTaskRunDir(task.SpaceID, task.ID, run.ID)
	return runDirs{
		runDir:       runDir,
		runWorkspace: paths.RuntimeTaskRunWorkspaceDir(task.SpaceID, task.ID, run.ID),
		runGlobal:    paths.RuntimeTaskRunGlobalDir(task.SpaceID, task.ID, run.ID),
		// Derived here rather than through RuntimePaths: nothing outside this
		// package needs to locate the run's OS HOME.
		runOSHome: filepath.Join(runDir, "oshome"),
	}
}

func prepareRunWorkspace(ctx context.Context, input RunTaskInput, task *coretask.Task, run *coretask.Run, dirs runDirs) error {
	persist := input.Persist
	if err := ensureRunDirs(dirs.runWorkspace, dirs.runGlobal, dirs.runOSHome); err != nil {
		return err
	}
	// Before the runtime is assembled, because agentapp discovers plugins from
	// BUILDMAX_HOME once and keeps that snapshot.
	if err := materializePlugins(ctx, dirs.runGlobal, input.Plugins,
		httpPackageFetcher(input.WorkerAPI, run.ID)); err != nil {
		componentLog().Error("failed to materialize this run's plugins", "task_run_id", run.ID, "err", err)
		return err
	}
	restoreSessionFromPreviousRun(ctx, task, run, dirs.runGlobal, persist)
	// Fill workspace/ — the Agent's cwd and single tool root — either by
	// restoring the Task's base checkpoint or by materializing the space files
	// and seeding them, before any model or tool call. A workspace AGENTS.md is
	// discovered there by the runtime's normal workspace-root prompt layer; the
	// run writes no synthesized AGENTS.md above it. See
	// docs/design/task-workspace-checkpoints.md §4.1 and §6.
	if err := prepareWorkspaceFilesystem(ctx, input, task, run, dirs); err != nil {
		componentLog().Error("failed to prepare the task workspace", "task_run_id", run.ID, "space_id", task.SpaceID, "err", err)
		return err
	}
	return nil
}

func executeRunTask(ctx context.Context, input RunTaskInput, task *coretask.Task, run *coretask.Run, dirs runDirs) (runResult, error) {
	effectiveSessionID := input.SessionID
	if task.SessionID != nil {
		effectiveSessionID = *task.SessionID
	}
	agentRun, err := runAgentTask(ctx, run, dirs.runWorkspace, dirs.runGlobal, dirs.runOSHome, effectiveSessionID, input.StreamSender, input.Model, input.Managed, input.ManagedHTTPClient, input.SpaceAgentInstructions, input.AdditionalSystemPrompt,
		artifactPublisher(input.WorkerAPI, run.ID), issueClient(input.WorkerAPI, task, run.ID),
		input.SandboxNetworkTier, input.SandboxFilesystemTier, input.SecretEnvGrants, task.OutputSchema)
	result := runResult{
		EndTime:          time.Now().UTC(),
		OutputStr:        string(agentRun.output),
		Output:           agentRun.output,
		Structured:       agentRun.structured,
		PromptTokens:     agentRun.promptTokens,
		CompletionTokens: agentRun.completionTokens,
		TracePath:        traceRelPath(dirs.runGlobal, agentRun.tracePath),
	}
	if err != nil {
		return result, err
	}
	return result, nil
}

func reportPersistedRunState(ctx context.Context, persist blob.RunStorage, scope RunScope, dirs runDirs, result runResult) {
	uploadTaskGlobal(ctx, dirs.runGlobal, scope, persist, result.TracePath)
}

func ensureRunDirs(runWorkspace, runGlobal, runOSHome string) error {
	if err := os.MkdirAll(runWorkspace, 0755); err != nil {
		return fmt.Errorf("create run workspace dir: %w", err)
	}
	if err := os.MkdirAll(runGlobal, 0755); err != nil {
		return fmt.Errorf("create run global dir: %w", err)
	}
	// 0700: the run's HOME will hold credential files, so it is private even
	// though the run is the only principal on this filesystem.
	if err := os.MkdirAll(runOSHome, 0700); err != nil {
		return fmt.Errorf("create run OS home dir: %w", err)
	}
	return nil
}

// sessionBundleFiles are the parts of a session bundle a resumed run needs.
//
// Only these two: meta.json carries the session's current selections and
// running totals, history.jsonl carries the conversation. Traces are
// diagnostics rather than resume input, so a run that recovers a session
// without them resumes correctly and simply has no record of what the previous
// run did — which is the right trade when the alternative is fetching an
// unbounded set of files whose names this side does not know.
var sessionBundleFiles = []string{"meta.json", "history.jsonl"}

func restoreSessionFromPreviousRun(ctx context.Context, task *coretask.Task, run *coretask.Run, runGlobalDir string, persist blob.RunStorage) {
	if task.SessionID == nil || run.PreviousTaskRunID == nil {
		return
	}
	bundleDir := filepath.Join(runGlobalDir, "sessions", *task.SessionID)
	for _, name := range sessionBundleFiles {
		data, err := persist.GetRunGlobal(ctx, blob.RunObjectRef{
			SpaceID: task.SpaceID, TaskID: task.ID, TaskRunID: *run.PreviousTaskRunID,
			RelPath: "sessions/" + *task.SessionID + "/" + name,
		})
		if err != nil {
			// Best-effort: a run that cannot recover the previous session
			// starts fresh rather than failing. Half a bundle is worse than
			// none, though — a history with no metadata resumes under the
			// wrong model — so a missing part abandons the whole restore.
			_ = os.RemoveAll(bundleDir)
			return
		}
		// Whole or not at all: the next run reads these as the conversation's
		// only copy, and a torn one would refuse to open the session.
		if err := util.WriteFileAtomic(filepath.Join(bundleDir, name), data, 0o600); err != nil {
			_ = os.RemoveAll(bundleDir)
			return
		}
	}
}

// agentRunOutput is what one in-process agent run yields back to the task-run
// reporting path. Grouped rather than returned positionally because a failed
// run still carries a usable trace path, so the error and non-error paths need
// the same fields.
type agentRunOutput struct {
	output           []byte
	structured       *string
	promptTokens     *int
	completionTokens *int
	// tracePath is the trace file's absolute path on the worker's disk, before
	// it is made relative to the run global dir.
	tracePath string
}

// runtimeModelEntries is the model list the run's app is assembled with.
//
// A managed run keeps its entry even when the model name is empty. Empty means
// the deployment's default, which only the gateway can resolve — but the entry
// is still how the runtime learns that a model exists at all, and it carries
// the context window the session compacts against. Dropping it left every
// deployment that names no worker model with no models at all, and failed its
// runs with `model not found: ""`.
//
// A direct run with no model stays empty: there the name is the whole entry,
// and an unnamed one would send the prompt nowhere.
func runtimeModelEntries(runtimeModel config.ModelEntry, managed ManagedInference) []config.ModelEntry {
	if runtimeModel.Model == "" && !managed.Enabled() {
		return nil
	}
	return []config.ModelEntry{runtimeModel}
}

// runProvenance restates a TaskRun's origin as the plain-string shape
// agentapp accepts, so its trace carries who or what started this run and
// why. RetryOfTaskRunID is left empty rather than dereferenced when nil.
func runProvenance(run *coretask.Run) agentapp.RunProvenance {
	retryOf := ""
	if run.RetryOfTaskRunID != nil {
		retryOf = *run.RetryOfTaskRunID
	}
	return agentapp.RunProvenance{
		CreatedBy:        run.CreatedBy,
		CreatedByType:    run.CreatedByType,
		TriggerSource:    run.TriggerSource,
		RetryOfTaskRunID: retryOf,
	}
}

func runAgentTask(ctx context.Context, run *coretask.Run, runWorkspaceDir, runGlobalDir, runOSHome, sessionID string, streamSender workerclient.StreamSender, runtimeModel config.ModelEntry, managed ManagedInference, managedHTTPClient *http.Client, spaceAgentInstructions, additionalSystemPrompt string, publisher tool.ArtifactPublisher, issues tool.IssueClient, sandboxNetworkTier config.SandboxNetworkTier, sandboxFilesystemTier config.SandboxFilesystemTier, secretGrants map[string]string, outputSchema *string) (agentRunOutput, error) {
	var sink llm.StreamSink
	if streamSender != nil {
		sink = &streamSinkAdapter{ctx: ctx, streamSender: streamSender, taskRunID: run.ID,
			redactor: secretscan.NewRedactor(mapValues(secretGrants))}
	}

	var out agentapp.RunResult
	err := withRunEnv(runOSHome, runGlobalDir, secretGrants, func() error {
		app, err := agentapp.NewAgentApp(agentapp.AppConfig{
			WorkspaceDir:                runWorkspaceDir,
			EnableMCP:                   true,
			Policy:                      agent.AllowAllPolicy(),
			ModelEntries:                runtimeModelEntries(runtimeModel, managed),
			ManagedServerURL:            managed.ServerURL,
			ManagedToken:                managed.tokenFunc(),
			ManagedHTTPClient:           managedHTTPClient,
			ManagedTaskRunID:            managedRunScope(managed, run.ID),
			Surface:                     managedSurface,
			AdditionalSystemPrompt:      additionalSystemPrompt,
			AdditionalSystemPromptLayer: "agent_instructions",
			SpaceAgentInstructions:      spaceAgentInstructions,
			RunProvenance:               runProvenance(run),
			ArtifactPublisher:           publisher,
			IssueClient:                 issues,
			// A worker executes model-chosen shell commands, so it resolves
			// the stricter worker sandbox baseline whenever it is running
			// from an image that actually installs the OS backend -- see
			// docs/design/sandbox-boundaries.md §13.1 gap 1,
			// docs/design/agent-sandbox-policy.md, and
			// config.WorkerSandboxSurface's own comment for why this is
			// conditional rather than unconditional. The two tiers are this
			// run's agent-declared exception to that baseline, not a
			// replacement for it.
			SandboxSurface:        config.WorkerSandboxSurface(),
			SandboxNetworkTier:    sandboxNetworkTier,
			SandboxFilesystemTier: sandboxFilesystemTier,
			// This is the official unattended-worker profile, whatever the
			// sandbox backend marker resolved: a resolved stdio MCP server fails
			// the run before any child process or model call, because the worker
			// would otherwise launch it outside the Bash boundary. Remote MCP
			// transports and local surfaces are unaffected. See
			// docs/design/trust-harness.md §3.9.
			UnattendedWorker: true,
			// The grants are set in the run's environment by withRunEnv above;
			// their names are admitted past env scrubbing so a secret-shaped
			// grant like GH_TOKEN actually reaches the agent's commands, and
			// their values are registered with the trace redactor.
			SecretEnvNames:  mapKeys(secretGrants),
			SecretEnvValues: mapValues(secretGrants),
		})
		if err != nil {
			return err
		}
		defer func() { _ = app.Close() }()
		sess, err := app.OpenOrCreateSession(sessionID)
		if err != nil {
			return err
		}
		// Released before the run's global directory is uploaded: the journal
		// has to be closed and its lock dropped before anything reads the
		// bundle back off disk.
		defer app.CloseSession(sess)
		out, err = app.RunPrompt(ctx, sess, run.Input, agentapp.RunPromptOpts{Stream: sink, Output: outputSchemaFor(outputSchema)})
		return err
	})
	if streamSender != nil {
		// Detached: on a cancel this context is already dead, and the buffered
		// tail is the part of the reply the reader has not seen yet.
		flushCtx, cancelFlush := context.WithTimeout(context.WithoutCancel(ctx), reportFinishTimeout)
		defer cancelFlush()
		if flushErr := streamSender.Flush(flushCtx, run.ID); flushErr != nil {
			componentLog().Warn("stream flush failed", "task_run_id", run.ID, "err", flushErr)
		}
	}
	// RunPrompt carries the trace path out on its error paths too, so a failed
	// run stays diagnosable — which is when the trace matters most.
	if err != nil {
		return agentRunOutput{tracePath: out.TracePath}, err
	}
	promptTokens := out.PromptTokens
	completionTokens := out.CompletionTokens
	return agentRunOutput{
		output:           []byte(out.Reply),
		structured:       structuredValueJSON(out.Structured),
		promptTokens:     &promptTokens,
		completionTokens: &completionTokens,
		tracePath:        out.TracePath,
	}, nil
}

// structuredValueJSON is the validated structured value as JSON text to persist,
// or nil when the run requested no output schema or the model's answer did not
// validate. Only a validated value is stored; a typed failure leaves the column
// nil, which is how a Workflow node that required output learns the run did not
// satisfy its schema (docs/design/structured-output.md §9).
func structuredValueJSON(s *llm.Structured) *string {
	if s == nil || s.Err != nil || len(s.Value) == 0 {
		return nil
	}
	v := string(s.Value)
	return &v
}

// outputSchemaFor turns the task's stored schema text into the run's output
// request, or nil for a free-text task. The name is stable; providers that
// require one use it (docs/design/structured-output.md §7).
func outputSchemaFor(schema *string) *llm.OutputSchema {
	if schema == nil || *schema == "" {
		return nil
	}
	return &llm.OutputSchema{Name: "output", Schema: json.RawMessage(*schema)}
}

// traceRelPath converts a trace's absolute path into the key it is uploaded
// under. It mirrors walkAndUploadFiles: relative to the run global dir, slash
// separated. Returns "" when there is no trace, or when the file sits outside
// the uploaded tree — a stored reference that cannot resolve is worse than
// none, because a reader would report the trace as missing rather than as
// never written.
func traceRelPath(runGlobalDir, tracePath string) string {
	if tracePath == "" {
		return ""
	}
	rel, err := filepath.Rel(runGlobalDir, tracePath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		componentLog().Warn("trace written outside the uploaded run dir; not recording a path",
			"trace_path", tracePath, "run_global_dir", runGlobalDir, "err", err)
		return ""
	}
	return filepath.ToSlash(rel)
}

type streamSinkAdapter struct {
	ctx          context.Context
	streamSender workerclient.StreamSender
	taskRunID    string
	// redactor removes this run's exact Secret values from a streamed delta
	// before it reaches the watcher. Nil is a no-op. See
	// docs/design/space-secrets.md §12.
	redactor *secretscan.Redactor
}

func (s *streamSinkAdapter) OnDelta(delta string) {
	if s.streamSender == nil || delta == "" {
		return
	}
	delta = s.redactor.RedactExact(delta)
	if err := s.streamSender.SendDelta(s.ctx, s.taskRunID, delta); err != nil {
		componentLog().Warn("stream send delta failed", "task_run_id", s.taskRunID, "err", err)
	}
}

// withRunEnv scopes BUILDMAX_HOME, the operating-system HOME, and this run's
// Space Secret grants to the process for the duration of fn. A worker process
// runs one run, so setting the process environment is safe -- the same
// assumption withBuildmaxHome already made for BUILDMAX_HOME alone. USERPROFILE
// mirrors HOME so tools that read the Windows home variable land in the same
// run-private directory. The grants are placed under the names the agent
// declared; env scrubbing admits those names (see AppConfig.SecretEnvNames).
func withRunEnv(osHome, buildmaxHome string, grants map[string]string, fn func() error) error {
	vars := map[string]string{
		config.EnvKeyBuildmaxHome: buildmaxHome,
		"HOME":                    osHome,
		"USERPROFILE":             osHome,
	}
	maps.Copy(vars, grants)
	return util.WithEnvVars(vars, fn)
}

// mapKeys returns a map's keys, or nil for an empty map.
func mapKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// mapValues returns a map's values, or nil for an empty map.
func mapValues(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// reportRunFailure records the failure. tracePath may be empty — the run can
// fail before an agent ever starts — but when a trace exists it is recorded
// here too: diagnosing a failure is the trace's main job.
func reportRunFailure(ctx context.Context, taskRunID string, err error, tracePath string, checkpoint *workerclient.WorkspaceCheckpointDescriptor, updater TaskRunUpdater) {
	endTime := time.Now().UTC()
	errMsg := fmt.Sprintf("%v", err)
	req := &workerclient.PatchTaskRunRequest{
		Status:       string(coretask.RunStatusFailed),
		EndedAt:      &endTime,
		ErrorMessage: &errMsg,
	}
	if tracePath != "" {
		req.TracePath = &tracePath
	}
	req.WorkspaceCheckpoint = checkpoint
	_ = updater.UpdateRunStatus(ctx, taskRunID, req)
}

// reportRunOutcome records a run's terminal status and reply.
//
// Every outcome that leaves something behind shares it — succeeded, canceled,
// and interrupted — because they leave the same thing: the reply and the tokens
// it spent. The status is what tells a reader whether the output is the answer
// or as far as the run got, and errMessage, when there is one, is what tells
// them why it is the latter.
//
// The reply, carried on the status patch, is the run's one persisted output.
// Files a run means to keep are published deliberately through UploadArtifact to
// the Artifact service; the runtime neither scans a directory for incidental
// output nor stores a separate result file. See
// docs/design/task-workspace-checkpoints.md §4.
func reportRunOutcome(ctx context.Context, scope RunScope, result runResult, status coretask.RunStatus, errMessage string, checkpoint *workerclient.WorkspaceCheckpointDescriptor, updater TaskRunUpdater) error {
	req := &workerclient.PatchTaskRunRequest{
		Status:     string(status),
		EndedAt:    &result.EndTime,
		Output:     &result.OutputStr,
		Structured: result.Structured,
	}
	if result.PromptTokens != nil {
		req.PromptTokens = result.PromptTokens
	}
	if result.CompletionTokens != nil {
		req.CompletionTokens = result.CompletionTokens
	}
	if result.TracePath != "" {
		req.TracePath = &result.TracePath
	}
	if errMessage != "" {
		req.ErrorMessage = &errMessage
	}
	req.WorkspaceCheckpoint = checkpoint
	return updater.UpdateRunStatus(ctx, scope.TaskRunID, req)
}

// uploadTaskGlobal uploads the run's global dir (logs, sessions, settings) to blob storage for the run.
// uploadTaskGlobal uploads the run's global dir to blob storage. It is an
// allowlist, not a directory walk: the run-scoped BUILDMAX_HOME accumulates
// state the server has no use for, so each upload is named.
//
// traceKey is this run's trace, relative to globalDir, or "" when none was
// written. It is passed in rather than discovered because its file name is the
// agent run id — a directory scan would find it, but only the caller knows
// which file the run actually recorded a pointer to.
func uploadTaskGlobal(ctx context.Context, globalDir string, scope RunScope, persist blob.RunStorage, traceKey string) {
	relPaths := []string{"logs/buildmax.log", "logs/buildmax-worker.log", "settings.yaml"}
	if traceKey != "" {
		relPaths = append(relPaths, traceKey)
	}
	// Walked rather than listed: a session is a directory now — metadata,
	// journal, and its own traces — so a flat read would upload nothing.
	sessionsDir := filepath.Join(globalDir, "sessions")
	_ = filepath.WalkDir(sessionsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(globalDir, path)
		if relErr != nil {
			return nil
		}
		relPaths = append(relPaths, filepath.ToSlash(rel))
		return nil
	})
	for _, relPath := range relPaths {
		fullPath := filepath.Join(globalDir, filepath.FromSlash(relPath))
		info, err := os.Stat(fullPath)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		f, err := os.Open(fullPath)
		if err != nil {
			componentLog().Warn("upload run global open failed", "task_run_id", scope.TaskRunID, "rel_path", relPath, "err", err)
			continue
		}
		putErr := persist.PutRunGlobal(ctx, blob.RunObjectRef{
			SpaceID: scope.SpaceID, TaskID: scope.TaskID, TaskRunID: scope.TaskRunID,
			RelPath: filepath.ToSlash(relPath),
		}, f)
		_ = f.Close()
		if putErr != nil {
			componentLog().Warn("upload run global put failed", "task_run_id", scope.TaskRunID, "rel_path", relPath, "err", putErr)
		}
	}
}
