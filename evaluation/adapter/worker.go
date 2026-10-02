package adapter

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/icloudbb/buildmax/evaluation/contract"
	"github.com/icloudbb/buildmax/evaluation/trace"
	"github.com/icloudbb/buildmax/internal/infra/workerclient"
)

// WorkerAdapterVersion changes when this adapter changes how it dispatches a
// run. Like the CLI's, it lands on the subject manifest: a comparison spanning
// an adapter change is not paired.
const WorkerAdapterVersion = 2

// Worker runs trials through the built buildmax-worker binary.
//
// The worker is dispatched the way a scheduler dispatches it — a run id on the
// command line, a run token in the environment, and a server to fetch the run
// from — against a control plane this adapter serves. What it exercises that
// the CLI adapter cannot is the part of the product only a worker has:
// materializing the space's persistent workspace into a run-scoped directory,
// executing with no interactive surface, and reporting an outcome over the API
// rather than to a terminal.
type Worker struct {
	// Binary is the built buildmax-worker executable under evaluation.
	Binary string
	// Credential is the provider access written into the run's server.yaml.
	Credential ModelAccess
	// Retention is how much free text bundles keep.
	Retention contract.RetentionLevel
	// SpaceID and UserID scope the run's directories. They are identifiers in a
	// control plane no real deployment sees, so they only need to be stable.
	SpaceID string
	UserID  string
}

const (
	defaultEvalSpaceID = "tm_evaluation"
	defaultEvalUserID  = "us_evaluation"
)

// Run executes one trial through the worker and writes its evidence under
// bundleRoot.
func (w *Worker) Run(ctx context.Context, tr Trial, bundleRoot string) (Result, error) {
	started := time.Now()
	bundle := contract.TrialBundle{
		TrialID:      tr.TrialID,
		ExperimentID: tr.ExperimentID,
		TaskID:       tr.Task.ID,
		TaskVersion:  tr.Task.Version,
		Suite:        tr.Task.Suite,
		SubjectID:    tr.Subject.ID,
		Index:        tr.Index,
		Domain:       tr.Task.Domain,
		Surface:      contract.SurfaceWorker,
		StartedAt:    started,
		Retention:    w.retention(),
		Reproduce:    contract.Reproduction{Dataset: tr.Subject.Dataset},
	}

	trialDir, err := contract.TrialDir(bundleRoot, tr.Task.ID, tr.Index)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(trialDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create trial dir: %w", err)
	}

	root, err := os.MkdirTemp("", "buildmax-worker-trial-*")
	if err != nil {
		return Result{}, fmt.Errorf("create trial root: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(root) }

	layout := w.layout(root, tr)
	fail := func(status contract.TrialStatus, cause error) (Result, error) {
		bundle.Status = status
		bundle.Error = cause.Error()
		bundle.Duration = contract.FromDuration(time.Since(started))
		return Result{Bundle: bundle, Workspace: layout.runWorkspace, TrialDir: trialDir, Cleanup: cleanup}, nil
	}

	if len(tr.Task.Turns) == 0 {
		return fail(contract.StatusInvalidTask, errors.New("task has no turns"))
	}
	if len(tr.Task.Turns) > 1 {
		// A worker run is one non-interactive execution. A multi-turn task is
		// not a worker task, and running only its first turn would report a
		// score for something the task did not ask.
		return fail(contract.StatusInvalidTask,
			fmt.Errorf("task has %d turns; a worker run executes one", len(tr.Task.Turns)))
	}

	// The initial state goes into the space's persistent workspace, not into the
	// run directory. Materializing it is the worker's job, so putting it where
	// a real space's files live is what puts that step under test.
	if err := Materialize(tr.TaskDir, layout.spaceHome); err != nil {
		return fail(contract.StatusInvalidTask, err)
	}
	leaked, err := VerifyBoundary(tr.TaskDir, layout.spaceHome)
	if err != nil {
		return fail(contract.StatusInfrastructureError, err)
	}
	if len(leaked) > 0 {
		return fail(contract.StatusInvalidTask,
			fmt.Errorf("hidden task material reachable in the workspace: %s", strings.Join(leaked, ", ")))
	}
	initial, err := DigestDir(layout.spaceHome)
	if err != nil {
		return fail(contract.StatusInfrastructureError, err)
	}
	bundle.InitialStateDigest = initial

	token, err := runToken()
	if err != nil {
		return fail(contract.StatusInfrastructureError, err)
	}
	plane, err := startControlPlane(token, w.describeRun(tr, layout))
	if err != nil {
		return fail(contract.StatusInfrastructureError, err)
	}
	defer plane.Close()

	if err := w.writeServerConfig(layout, plane.URL(), tr.Subject); err != nil {
		return fail(contract.StatusInfrastructureError, err)
	}

	runCtx := ctx
	if tr.Task.Limits.WallSeconds > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(tr.Task.Limits.WallSeconds)*time.Second)
		defer cancel()
	}

	args := []string{"--task-run-id", layout.runID}
	bundle.Reproduce.Command = append([]string{w.Binary}, args...)
	bundle.Reproduce.Environment = map[string]string{
		"BUILDMAX_HOME":      "<trial home built from the subject>",
		"BUILDMAX_RUN_TOKEN": "<per-run token>",
		"worker.server_url":  "<the evaluation control plane>",
		"storage.*_backend":  "local_fs",
	}

	execErr := w.exec(runCtx, layout, token, args)

	// The outcome is what the worker reported to its server, never its exit
	// code. A worker's exit status is a dispatch-level signal — a run that
	// reported its own outcome exits zero however it ended, so a failed run and
	// a successful one are indistinguishable from outside. What separates them,
	// and separates both from a worker that was killed before it could report
	// anything, is the control plane.
	outcome, reported := plane.Outcome()
	if !reported {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return fail(contract.StatusTimedOut, fmt.Errorf("the worker never reported: %w", execErr))
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return fail(contract.StatusCanceled, fmt.Errorf("the worker never reported: %w", execErr))
		}
		return fail(contract.StatusInfrastructureError,
			fmt.Errorf("the worker exited without reporting an outcome: %v", execErr))
	}
	if plane.Unauthorized() > 0 {
		return fail(contract.StatusInfrastructureError,
			fmt.Errorf("%d worker request(s) arrived without the run token", plane.Unauthorized()))
	}

	bundle.Usage = usageFromPatch(outcome)
	if outcome.Output != nil {
		bundle.Reply = w.reply(*outcome.Output)
	}

	switch outcome.Status {
	case "SUCCEEDED":
	case "CANCELED":
		return fail(contract.StatusCanceled, errors.New("the run was canceled"))
	case "FAILED":
		message := "the worker reported FAILED"
		if outcome.ErrorMessage != nil {
			message = *outcome.ErrorMessage
		}
		// A failed run's trace is the most useful thing it produced, so it is
		// collected before reporting. Failing to collect it changes nothing
		// about the outcome already known.
		_ = w.collectTrace(&bundle, layout, outcome, trialDir)
		return fail(contract.StatusAgentError, errors.New(message))
	default:
		return fail(contract.StatusInfrastructureError,
			fmt.Errorf("the worker reported an unexpected status %q", outcome.Status))
	}

	// The final state is the agent's workspace/. The initial digest above is the
	// space's persistent directory the run materialized that workspace from, so
	// the two describe the same tree lineage: an unchanged run digests equal and
	// a run that wrote a file digests different, the same way the CLI surface
	// compares them.
	final, err := DigestDir(layout.runWorkspace)
	if err != nil {
		return fail(contract.StatusInfrastructureError, err)
	}
	bundle.FinalStateDigest = final

	if err := w.collectTrace(&bundle, layout, outcome, trialDir); err != nil {
		bundle.Error = err.Error()
		if bundle.TracePath == "" {
			bundle.FailureClass = "trace-unavailable"
		}
	}
	if err := w.collectArtifacts(&bundle, layout, trialDir); err != nil {
		bundle.Error = err.Error()
	}

	bundle.Duration = contract.FromDuration(time.Since(started))
	return Result{
		Bundle:   bundle,
		Gradable: true,
		// Graders read the agent's workspace. A path assertion resolves against
		// its root, and a file the space supplied is at `<name>` there — the same
		// place the CLI surface puts it, so one grader reads both.
		Workspace: layout.runWorkspace,
		TrialDir:  trialDir,
		Cleanup:   cleanup,
	}, nil
}

// workerLayout is where one trial's directories live.
type workerLayout struct {
	home       string // BUILDMAX_HOME for the worker process
	workspaces string // server.yaml workspaces_dir
	spaceHome  string // the space's persistent workspace
	// runDir is the run's root. The agent's workspace is its `workspace`
	// subdirectory: the space's files are materialized there and the agent works
	// there, so its root and what it inherited are one tree — the same shape the
	// CLI surface presents.
	runDir         string
	runWorkspace   string // runDir/workspace: the agent's cwd and tool root
	runGlobal      string // run-global state, including the trace
	runID          string
	taskID         string
	conversationID string
}

func (w *Worker) layout(root string, tr Trial) workerLayout {
	workspaces := filepath.Join(root, "workspaces")
	runID := fmt.Sprintf("rt_%s_%d", sanitizeID(tr.Task.ID), tr.Index)
	taskID := "tk_" + sanitizeID(tr.Task.ID)
	conversationID := "cv_evaluation"
	space := w.spaceID()

	// This mirrors taskrun.NewRuntimePathsFromRoot and
	// config.PersistentWorkspaceDir. It is duplicated rather than imported
	// because the adapter must describe where it expects the worker to write:
	// computing both sides from one function would make a layout change look
	// like agreement.
	runDir := filepath.Join(workspaces, space, "tasks", taskID, runID)
	return workerLayout{
		home:           filepath.Join(root, "home"),
		workspaces:     workspaces,
		spaceHome:      filepath.Join(workspaces, space, "home"),
		runDir:         runDir,
		runWorkspace:   filepath.Join(runDir, "workspace"),
		runGlobal:      filepath.Join(runDir, "global"),
		runID:          runID,
		taskID:         taskID,
		conversationID: conversationID,
	}
}

func (w *Worker) spaceID() string {
	if w.SpaceID != "" {
		return w.SpaceID
	}
	return defaultEvalSpaceID
}

func (w *Worker) userID() string {
	if w.UserID != "" {
		return w.UserID
	}
	return defaultEvalUserID
}

// Describe reports the worker surface and this adapter's version.
func (w *Worker) Describe() (contract.Surface, int) {
	return contract.SurfaceWorker, WorkerAdapterVersion
}

func (w *Worker) retention() contract.RetentionLevel {
	if w.Retention == "" {
		return contract.RetentionBounded
	}
	return w.Retention
}

func (w *Worker) reply(reply string) string {
	if w.retention() == contract.RetentionMetadata {
		return ""
	}
	return reply
}

// describeRun is what the control plane tells the worker about this run.
func (w *Worker) describeRun(tr Trial, layout workerLayout) workerclient.GetTaskRunResponse {
	return workerclient.GetTaskRunResponse{
		Run: workerclient.TaskRunRun{
			ID:     layout.runID,
			TaskID: layout.taskID,
			Input:  tr.Task.Turns[0],
			// The worker refuses a run that is not scheduled, which is the
			// state a dispatcher leaves it in.
			Status: "SCHEDULED",
			// The server offers every Task run the deferred AskUser tool; a
			// worker evaluated without it measured a run no deployment
			// performs.
			AskUser:   true,
			CreatedAt: time.Now().UTC(),
		},
		Task: workerclient.TaskRunTask{
			ID:             layout.taskID,
			ConversationID: layout.conversationID,
			SpaceID:        w.spaceID(),
			UserID:         w.userID(),
		},
		// LLM is left absent, which the contract defines as direct: the run
		// reads its model from the server.yaml written below. A managed run
		// would measure a transport the subject manifest does not describe.
		LLM: nil,
		// No plugins, and that is a decision rather than an omission. A server
		// resolves a run's plugin pins from what its agent names and its space
		// has activated; an evaluation subject declares its extensions in the
		// manifest, so a trial that quietly loaded one would be measuring a
		// configuration the result does not describe. Evaluating pinned plugins
		// means naming them on the subject and serving them here.
		Plugins: nil,
	}
}

// writeServerConfig writes the server.yaml a worker reads on startup.
func (w *Worker) writeServerConfig(layout workerLayout, controlPlaneURL string, subject contract.SubjectManifest) error {
	if subject.Model.Target == "" {
		return fmt.Errorf("subject %q names no model target", subject.Name)
	}
	if err := os.MkdirAll(layout.home, 0o755); err != nil {
		return fmt.Errorf("create trial home: %w", err)
	}
	if err := os.MkdirAll(layout.spaceHome, 0o755); err != nil {
		return fmt.Errorf("create space workspace: %w", err)
	}

	// Only what a run needs. Everything absent is a deployment setting a
	// subject did not ask for, and one that changed a result without appearing
	// in the manifest would make the measurement unattributable.
	cfg := map[string]any{
		"log_level":      "error",
		"workspaces_dir": layout.workspaces,
		"worker": map[string]any{
			"server_url": controlPlaneURL,
		},
		"storage": map[string]any{
			"persist_backend":  "local_fs",
			"artifact_backend": "local_fs",
		},
		// A direct run reads one model from conversation.model, not from a
		// settings-style list: server.yaml has no `models:` array, and a worker
		// executes one run against one model rather than offering a choice.
		"conversation": map[string]any{
			"model": map[string]any{
				"model":          subject.Model.Target,
				"name":           subject.Model.Target,
				"provider":       subject.Model.Transport,
				"api_url":        w.Credential.APIURL,
				"api_key":        w.Credential.APIKey,
				"context_window": subject.Model.ContextWindow,
				"max_tokens":     subject.Model.MaxOutput,
				"reasoning":      subject.Model.Reasoning,
			},
		},
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode server config: %w", err)
	}
	// 0o600: it holds a provider credential.
	if err := os.WriteFile(filepath.Join(layout.home, "server.yaml"), data, 0o600); err != nil {
		return fmt.Errorf("write server config: %w", err)
	}
	return nil
}

// exec dispatches the worker the way a scheduler does.
func (w *Worker) exec(ctx context.Context, layout workerLayout, token string, args []string) error {
	// CommandContext kills rather than signalling, and that is deliberate. A
	// worker asked to stop politely reports FAILED with "the worker was shut
	// down", which is true of the process but not of the subject: a stop the
	// harness imposed is not the agent failing the task. Killing it leaves no
	// report, which is exactly how Run tells a timeout from an outcome.
	cmd := exec.CommandContext(ctx, w.Binary, args...)
	cmd.WaitDelay = killGraceSeconds * time.Second
	// The run token travels in the environment rather than on the command line
	// for the reason the product gives: argv is readable by every process on
	// the machine. Nothing else is inherited, so a contributor's own exported
	// settings cannot join the subject.
	cmd.Env = []string{
		"BUILDMAX_HOME=" + layout.home,
		"HOME=" + layout.home,
		"PATH=" + os.Getenv("PATH"),
		"BUILDMAX_RUN_TOKEN=" + token,
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w (output: %s)", err, bound(strings.TrimSpace(stderr.String()), 2000))
	}
	return nil
}

// collectTrace copies the run's durable trace into the bundle.
func (w *Worker) collectTrace(bundle *contract.TrialBundle, layout workerLayout,
	outcome workerclient.PatchTaskRunRequest, trialDir string) error {

	if outcome.TracePath == nil || *outcome.TracePath == "" {
		return errors.New("the worker reported no trace path")
	}
	// The path is relative to run-global storage, which is what the worker
	// uploads and what a server would later serve.
	source := filepath.Join(layout.runGlobal, filepath.FromSlash(*outcome.TracePath))
	if err := copyFile(source, filepath.Join(trialDir, contract.TraceFile), 0o644); err != nil {
		return fmt.Errorf("copy trace: %w", err)
	}
	bundle.TracePath = contract.TraceFile

	calls, err := trace.CountLLMCalls(filepath.Join(trialDir, contract.TraceFile))
	if err != nil {
		return fmt.Errorf("count model calls: %w", err)
	}
	bundle.Usage.LLMCalls = calls
	return nil
}

// collectArtifacts records what the run left in its workspace, by hash. The
// workspace is the run's output: there is no separate output directory, and a
// checkpoint of it is the durable record. The top-level files are recorded here
// for a grader that reads them by name.
func (w *Worker) collectArtifacts(bundle *contract.TrialBundle, layout workerLayout, trialDir string) error {
	entries, err := os.ReadDir(layout.runWorkspace)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read workspace: %w", err)
	}
	dest := filepath.Join(trialDir, contract.ArtifactsDir)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		source := filepath.Join(layout.runWorkspace, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		digest, err := digestFile(source)
		if err != nil {
			continue
		}
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return fmt.Errorf("create artifacts dir: %w", err)
		}
		if err := copyFile(source, filepath.Join(dest, e.Name()), 0o644); err != nil {
			return fmt.Errorf("copy artifact %s: %w", e.Name(), err)
		}
		bundle.Artifacts = append(bundle.Artifacts, contract.ArtifactRef{
			Name:   e.Name(),
			Digest: "sha256:" + hex.EncodeToString(digest),
			Bytes:  info.Size(),
		})
	}
	return nil
}

// usageFromPatch reads what the worker reported it consumed. A worker reports
// token counts and nothing else, so tool calls and cost stay absent rather than
// zero: a zero would read as a run that called no tools.
func usageFromPatch(patch workerclient.PatchTaskRunRequest) contract.Usage {
	var u contract.Usage
	if patch.PromptTokens != nil {
		u.PromptTokens = *patch.PromptTokens
	}
	if patch.CompletionTokens != nil {
		u.CompletionTokens = *patch.CompletionTokens
	}
	return u
}

// runToken mints the credential this run reports with.
func runToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate run token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// sanitizeID makes a task id safe as one path element.
func sanitizeID(id string) string {
	out := make([]rune, 0, len(id))
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}
