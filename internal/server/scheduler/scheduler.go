// Scheduler polls for pending task runs and runs the worker via a WorkerRunner (local process or k8s Job).
// It does not perform run execution; the worker process calls runtime.RunTask.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/icloudbb/buildmax/internal/core/eligibility"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	buildmaxlog "github.com/icloudbb/buildmax/internal/infra/log"
	"github.com/icloudbb/buildmax/internal/server/authtoken"
)

// The independent scheduler loops tag their records by subsystem. Loggers are
// built per call so infra/log can replace slog.Default during startup.
func componentLog(name string) *slog.Logger { return slog.With("component", name) }

func (s *Scheduler) log() *slog.Logger         { return componentLog("scheduler") }
func (c *CredentialCleaner) log() *slog.Logger { return componentLog("credential_cleaner") }
func (c *StaleRunReaper) log() *slog.Logger    { return componentLog("stale_run_reaper") }
func (a *AuditRetainer) log() *slog.Logger     { return componentLog("audit_retention") }
func (a *ArtifactRetainer) log() *slog.Logger  { return componentLog("artifact_retention") }
func (c *CheckpointOrphanSweeper) log() *slog.Logger {
	return componentLog("checkpoint_orphan_sweeper")
}

const (
	defaultPollInterval   = 5 * time.Second
	maxErrorMessageLength = 500
)

// MintRunToken signs the credential a worker presents for one run's managed
// inference calls.
//
// It only signs. The scheduler builds the claims from the run and its task, so
// a worker's identity comes from what the server already knows about the run
// rather than from anything the worker or its model could influence.
type MintRunToken func(authtoken.RunClaims) (string, error)

// Scheduler polls the task run store for PENDING runs and runs the worker via the configured runner.
type Scheduler struct {
	taskRuns coretask.RunStore
	// eligible answers whether the account that initiated a run may still have
	// work executed for it in the run's Space. Nil skips the check, which is what
	// a deployment that wires no authority stores has.
	eligible     eligibility.Checker
	runner       WorkerRunner
	mintRunToken MintRunToken
	pollInterval time.Duration
	stopCh       chan struct{}
	doneCh       chan struct{}
	// dispatchCtx is what a dispatch in flight is cancelled through. In
	// local_process mode the runner blocks for the whole run, so this is the
	// only way to tell that worker the process it lives in is going away.
	dispatchCtx    context.Context
	cancelDispatch context.CancelFunc
	// inflight counts dispatches that have started and not returned, so a stop
	// can wait for them while the poll loop exits immediately.
	inflight sync.WaitGroup
	// slots caps concurrent dispatches. One: it is what this scheduler has
	// always done — the loop dispatched inline — and raising it is a throughput
	// decision, not a shutdown one.
	slots chan struct{}
}

// maxConcurrentDispatch is how many runs one scheduler dispatches at a time.
const maxConcurrentDispatch = 1

// NewScheduler creates a Scheduler that polls for pending task runs and runs the worker via the given runner. Call Start() to begin polling.
//
// mint may be nil, which is every deployment that has not enabled managed worker
// inference: its workers reach a provider directly and have nothing to
// authenticate to.
func NewScheduler(taskRunStore coretask.RunStore, runner WorkerRunner, mint MintRunToken) (*Scheduler, error) {
	return NewSchedulerWithPollInterval(taskRunStore, runner, mint, defaultPollInterval)
}

// NewSchedulerWithPollInterval is like NewScheduler but allows setting the poll interval (e.g. for tests). Use 0 for default.
func NewSchedulerWithPollInterval(taskRunStore coretask.RunStore, runner WorkerRunner, mint MintRunToken, pollInterval time.Duration) (*Scheduler, error) {
	if taskRunStore == nil {
		return nil, errors.New("scheduler: taskRunStore must not be nil")
	}
	if runner == nil {
		return nil, errors.New("scheduler: runner must not be nil")
	}
	if pollInterval == 0 {
		pollInterval = defaultPollInterval
	}
	dispatchCtx, cancelDispatch := context.WithCancel(context.Background())
	return &Scheduler{
		taskRuns:       taskRunStore,
		runner:         runner,
		mintRunToken:   mint,
		pollInterval:   pollInterval,
		stopCh:         make(chan struct{}),
		doneCh:         make(chan struct{}),
		dispatchCtx:    dispatchCtx,
		cancelDispatch: cancelDispatch,
		slots:          make(chan struct{}, maxConcurrentDispatch),
	}, nil
}

// WithEligibility lets the scheduler refuse work whose initiating account has
// been disabled, deleted, or removed from the run's Space since it was queued.
//
// It is a setter rather than a constructor parameter because the check is
// optional: a deployment that wires no authority stores schedules exactly as it
// did before, and the existing call sites do not have to learn about accounts to
// keep compiling.
func (s *Scheduler) WithEligibility(c eligibility.Checker) *Scheduler {
	s.eligible = c
	return s
}

// Start launches the poll loop in a background goroutine.
func (s *Scheduler) Start() {
	go s.loop()
	s.log().Info("started", "poll_interval", s.pollInterval)
}

// Stop stops claiming runs, then gives the dispatches already in flight until
// ctx expires to finish. It reports whether they all finished.
//
// Two phases because they need different answers. Claiming must stop at once —
// a run started by a process that is going away is a run nobody will report on.
// A dispatch already made is the opposite: in local_process mode it *is* the
// running agent, and the worker it holds needs the server's API alive long
// enough to say what it produced. Cancelling the dispatch context is what asks
// that worker to stop; see docs/design/graceful-shutdown.md §6.1.
func (s *Scheduler) Stop(ctx context.Context) bool {
	close(s.stopCh)
	<-s.doneCh
	s.log().Info("no longer claiming runs")

	s.cancelDispatch()
	drained := make(chan struct{})
	go func() {
		s.inflight.Wait()
		close(drained)
	}()
	select {
	case <-drained:
		s.log().Info("stopped")
		return true
	case <-ctx.Done():
		s.log().Warn("a dispatched run did not stop within the shutdown budget")
		return false
	}
}

// loop is the main poll loop: on each tick it fetches the next PENDING run, claims it (PENDING→SCHEDULED), and hands it to a dispatch.
// State machine: PENDING → SCHEDULED → RUNNING → SUCCEEDED/FAILED. If spawn fails, run is set to FAILED (no revert to PENDING).
//
// Dispatch runs on its own goroutine, bounded by slots, so that a runner which
// blocks for the whole run — which local_process does — cannot hold the loop.
// Holding it would mean a stop request waits for an agent to finish, which is
// the case this design exists to remove.
func (s *Scheduler) loop() {
	defer close(s.doneCh)
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.pollOnce()
		}
	}
}

// pollOnce claims at most one run and dispatches it. It returns as soon as the
// dispatch has started, or immediately when every slot is busy.
func (s *Scheduler) pollOnce() {
	select {
	case s.slots <- struct{}{}:
	default:
		return // a dispatch is already running; the next tick tries again
	}
	released := false
	release := func() {
		if !released {
			released = true
			<-s.slots
		}
	}
	defer func() {
		if !released {
			release()
		}
	}()

	ctx := context.Background()
	run, err := s.taskRuns.GetNextPendingTaskRun(ctx)
	if err != nil {
		s.log().WarnContext(ctx, "poll failed", "err", err)
		return
	}
	if run == nil {
		return
	}
	updated, err := s.taskRuns.TransitionTaskRun(ctx, coretask.TransitionRunInput{
		TaskRunID:      run.ID,
		ExpectedStatus: coretask.RunStatusPending,
		NewStatus:      coretask.RunStatusScheduled,
	})
	if err != nil {
		s.log().WarnContext(ctx, "claim failed", "err", err)
		return
	}
	if !updated {
		return // another scheduler claimed it
	}
	// From here the run is ours, so its id goes on the context once and
	// every record below -- including failRun's -- carries it.
	ctx = buildmaxlog.With(ctx, "task_run_id", run.ID)

	// The task carries the Space this run belongs to, which the eligibility
	// check and the run token both need. Load it once, when either wants it.
	var task *coretask.Task
	if s.eligible != nil || s.mintRunToken != nil {
		_, t, err := s.taskRuns.GetTaskRunWithTask(ctx, run.ID)
		if err != nil {
			s.log().ErrorContext(ctx, "could not load the task behind this run; marking run FAILED", "err", err)
			s.failRun(ctx, run.ID, fmt.Errorf("load the task behind this run: %w", err))
			return
		}
		if t == nil {
			s.failRun(ctx, run.ID, fmt.Errorf("run %s has no task", run.ID))
			return
		}
		task = t
	}

	// Work whose initiating account was disabled, deleted, or removed from the
	// Space while it waited does not start: authority withdrawn after queueing
	// must not be spent. It fails here rather than being left with no worker
	// coming for it — a run nobody will dispatch, with no explanation, is worse
	// than a terminal one that says why. A store outage leaves eligibility
	// unknown; rather than lose the run or spend authority on a guess, dispatch
	// and let the worker's own fetch re-check once the store is reachable.
	if s.eligible != nil && run.CreatedBy != "" {
		switch err := s.eligible.Check(ctx, run.CreatedBy, task.SpaceID); {
		case err == nil:
		case errors.Is(err, eligibility.ErrUnavailable):
			s.log().WarnContext(ctx, "could not verify run eligibility; the worker will re-check", "err", err)
		default:
			s.log().WarnContext(ctx, "run initiator is no longer eligible; marking run FAILED", "user_id", run.CreatedBy, "err", err)
			s.failRun(ctx, run.ID, err)
			return
		}
	}
	// A run that cannot be given its credential fails here rather than
	// starting and failing at its first inference call, where the cause
	// would read as a model error instead of a dispatch one.
	runToken, err := s.runTokenFor(run, task)
	if err != nil {
		s.log().ErrorContext(ctx, "could not mint a run token; marking run FAILED", "err", err)
		s.failRun(ctx, run.ID, err)
		return
	}

	s.inflight.Add(1)
	released = true // the dispatch owns the slot from here
	go func() {
		defer s.inflight.Done()
		defer func() { <-s.slots }()
		s.dispatch(ctx, *run, runToken)
	}()
}

// dispatch starts the worker and records what ran it.
//
// It runs on the dispatch context rather than the poll one so that stopping the
// scheduler reaches the worker: a local process is asked to stop, and a Job that
// has already been created is unaffected, which is right — it outlives the
// server that dispatched it.
func (s *Scheduler) dispatch(ctx context.Context, run coretask.Run, runToken string) {
	dispatchCtx := buildmaxlog.With(s.dispatchCtx, "task_run_id", run.ID)
	workerType, k8sName, k8sAt, err := s.runner.Run(dispatchCtx, run, runToken)
	if err != nil {
		// A worker stopped because this process is going away has already
		// reported its own outcome. Overwriting that with a dispatch failure
		// would replace what the run produced with a message about the server.
		if s.dispatchCtx.Err() != nil {
			s.log().InfoContext(ctx, "worker stopped with the server", "err", err)
			return
		}
		s.log().ErrorContext(ctx, "worker spawn failed; marking run FAILED", "err", err)
		s.failRun(ctx, run.ID, err)
		return
	}
	if err := s.taskRuns.UpdateTaskRunWorkerInfo(ctx, run.ID, workerType, k8sName, k8sAt); err != nil {
		s.log().WarnContext(ctx, "could not persist worker info", "err", err)
	}
}

// runTokenFor builds this run's gateway credential from server state.
//
// Returns "" when the deployment mints none. The user claim is the run's own
// initiator, not the Task creator: a Continue by another member runs under that
// member, and disabling the original Task creator does not re-attribute a later
// run to them. The Space comes from the Task, which is what the gateway
// authorizes against.
func (s *Scheduler) runTokenFor(run *coretask.Run, task *coretask.Task) (string, error) {
	if s.mintRunToken == nil {
		return "", nil
	}
	if task == nil {
		return "", fmt.Errorf("run %s has no task", run.ID)
	}
	return s.mintRunToken(authtoken.RunClaims{
		UserID:    run.CreatedBy,
		SpaceID:   task.SpaceID,
		TaskRunID: run.ID,
		TaskID:    task.ID,
	})
}

// failRun records a dispatch failure.
//
// A run that already reached a terminal status is left alone. The worker
// process reports its own outcome and then exits non-zero on failure, so the
// exit status arrives here after the record is already written — and a run its
// worker reported as CANCELED or FAILED must not be overwritten with the
// process error, which says less and is about the wrong thing.
func (s *Scheduler) failRun(ctx context.Context, taskRunID string, cause error) {
	ctx = buildmaxlog.With(ctx, "task_run_id", taskRunID)
	run, err := s.taskRuns.GetTaskRun(ctx, taskRunID)
	if err != nil {
		s.log().ErrorContext(ctx, "could not read run before marking it FAILED", "err", err)
		return
	}
	if run == nil {
		s.log().ErrorContext(ctx, "could not mark missing run FAILED")
		return
	}
	if coretask.RunStatusTerminal(run.Status) {
		s.log().InfoContext(ctx, "worker exited non-zero but the run already reported an outcome",
			"status", run.Status, "err", cause)
		return
	}
	errorMsg := cause.Error()
	if len(errorMsg) > maxErrorMessageLength {
		errorMsg = errorMsg[:maxErrorMessageLength]
	}
	endedAt := time.Now().UTC()
	updated, err := s.taskRuns.TransitionTaskRun(ctx, coretask.TransitionRunInput{
		TaskRunID:      taskRunID,
		ExpectedStatus: coretask.RunStatus(run.Status),
		NewStatus:      coretask.RunStatusFailed,
		EndedAt:        &endedAt,
		ErrorMessage:   &errorMsg,
	})
	if err != nil {
		s.log().ErrorContext(ctx, "could not set run to FAILED", "err", err)
		return
	}
	if !updated {
		s.log().InfoContext(ctx, "run outcome changed while recording dispatch failure")
	}
}
