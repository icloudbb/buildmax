package scheduler

import (
	"context"
	"time"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
	buildmaxlog "github.com/icloudbb/buildmax/internal/infra/log"
)

// defaultRunTimeout is how long a run may sit in SCHEDULED or RUNNING before it
// is treated as abandoned.
//
// It is generous on purpose. Failing a run that is merely slow destroys real
// work, while failing one that is genuinely gone only replaces a row nobody can
// act on with one that says what happened.
const defaultRunTimeout = 6 * time.Hour

// defaultStaleSweepInterval is how often runs are checked.
//
// Abandoned runs do not need this cadence — they have already stopped
// progressing. A cancel does: someone is waiting for the run to stop, and this
// sweep is what finishes the ones no worker will report.
const defaultStaleSweepInterval = time.Minute

// defaultCancelGrace is how long a cancel request may go unreported before the
// server finishes the run itself.
//
// A worker notices a cancel on its next poll and then still has to stop its
// agent, upload what it produced, and report — so the grace has to outlast an
// orderly stop, or this would race the worker and mark runs canceled that were
// seconds from saying so themselves.
const defaultCancelGrace = 2 * time.Minute

// defaultLivenessGrace is how long a RUNNING run may go without its worker
// reporting before the server treats the worker as gone.
//
// The worker polls its own run route every DefaultCancelPollInterval — five
// seconds — for as long as it is RUNNING, so this is two dozen missed polls. It
// is that generous on purpose: a worker whose polls fail keeps working (a
// server it cannot reach is not one that canceled it), so reaping early does
// not stop the run, it only throws away the result the run was about to report.
// A partition has to look like a dead process before this fires. The value is
// coretask.WorkerLivenessGrace, which administration reads too.
const defaultLivenessGrace = coretask.WorkerLivenessGrace

// staleRunLimit bounds one sweep so a backlog cannot hold the loop or the
// database for an unbounded time. The next tick continues.
const staleRunLimit = 100

// StaleRunStore is the narrow store surface the reaper needs.
type StaleRunStore interface {
	ListStaleTaskRuns(ctx context.Context, cutoff time.Time, limit int) ([]coretask.Run, error)
	ListCancelRequestedTaskRuns(ctx context.Context, cutoff time.Time, limit int) ([]coretask.Run, error)
	ListLostWorkerTaskRuns(ctx context.Context, cutoff time.Time, limit int) ([]coretask.Run, error)
	TransitionTaskRun(ctx context.Context, in coretask.TransitionRunInput) (bool, error)
}

// StaleRunReaper finishes runs that nothing else will finish.
//
// Three cases, one loop. A run whose worker never reported an outcome stays
// SCHEDULED or RUNNING forever, because the only thing that moves it is the
// worker itself: that was already possible when a pod was evicted or a process
// was killed, and run tokens add one more way, since a run outliving its token
// can no longer report anything. A run someone canceled has the same problem
// for the same reason — the cancel is a request its worker honors, and a worker
// that is gone honors nothing. Either way the run is over and the record should
// say so.
//
// The third case is the same fact observed sooner. A RUNNING run's worker polls
// its own route every few seconds, so silence there says the process is gone
// long before the run timeout would. The timeout stays as the backstop for
// everything that sweep cannot see: a run that never reached RUNNING, and one
// with no recorded signal at all.
//
// Nothing here re-runs anything. A reaped run is recorded as over, not retried:
// a worker may have executed arbitrary side effects before it died, and the
// server cannot know whether the task was safe to repeat.
//
// See docs/design/worker-run-token.md.
type StaleRunReaper struct {
	runs          StaleRunStore
	timeout       time.Duration
	cancelGrace   time.Duration
	livenessGrace time.Duration
	interval      time.Duration
	stopCh        chan struct{}
	doneCh        chan struct{}
}

// NewStaleRunReaper returns a reaper for runs, or nil when there is no store to
// sweep — so a caller does not need to check before starting it. Zero values
// use the defaults.
func NewStaleRunReaper(runs StaleRunStore, timeout, interval time.Duration) *StaleRunReaper {
	if runs == nil {
		return nil
	}
	if timeout <= 0 {
		timeout = defaultRunTimeout
	}
	if interval <= 0 {
		interval = defaultStaleSweepInterval
	}
	return &StaleRunReaper{
		runs:          runs,
		timeout:       timeout,
		cancelGrace:   defaultCancelGrace,
		livenessGrace: defaultLivenessGrace,
		interval:      interval,
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
	}
}

// Start launches the sweep loop. Calling it on a nil reaper is a no-op.
func (c *StaleRunReaper) Start() {
	if c == nil {
		return
	}
	go c.loop()
	c.log().Info("started", "run_timeout", c.timeout, "cancel_grace", c.cancelGrace,
		"liveness_grace", c.livenessGrace, "interval", c.interval)
}

// Stop signals the loop to exit and blocks until it has finished.
func (c *StaleRunReaper) Stop() {
	if c == nil {
		return
	}
	close(c.stopCh)
	<-c.doneCh
	c.log().Info("stopped")
}

func (c *StaleRunReaper) loop() {
	defer close(c.doneCh)
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.Sweep(context.Background(), time.Now())
		}
	}
}

// Sweep finishes every run the reaper is responsible for: those asked to stop
// longer ago than the cancel grace, those whose worker stopped reporting, and
// those dispatched longer ago than the timeout. It is exported so a test can
// drive one pass without a clock.
//
// The order is most-informative first. A run that qualifies for more than one
// sweep is finished by the first that reaches it, and each writes a different
// answer to why it ended.
func (c *StaleRunReaper) Sweep(ctx context.Context, now time.Time) {
	if c == nil {
		return
	}
	c.sweepCanceled(ctx, now)
	c.sweepLostWorkers(ctx, now)
	c.sweepAbandoned(ctx, now)
}

// sweepLostWorkers fails runs whose worker stopped reporting mid-run.
//
// It runs after the cancel sweep for the reason that one runs first, and before
// the abandoned sweep because both end in FAILED and this one can say more: the
// run was executing and its worker went silent, rather than the run simply
// never finishing.
func (c *StaleRunReaper) sweepLostWorkers(ctx context.Context, now time.Time) {
	lost, err := c.runs.ListLostWorkerTaskRuns(ctx, now.Add(-c.livenessGrace), staleRunLimit)
	if err != nil {
		c.log().WarnContext(ctx, "lost worker sweep failed", "err", err)
		return
	}
	message := "this run lost its worker: nothing was heard from it for " + c.livenessGrace.String()
	for _, run := range lost {
		c.finish(ctx, run, coretask.RunStatusFailed, coretask.FailureWorkerLost, message, now, "failed a task run whose worker stopped reporting")
	}
}

// sweepAbandoned fails runs whose worker never reported an outcome.
func (c *StaleRunReaper) sweepAbandoned(ctx context.Context, now time.Time) {
	stale, err := c.runs.ListStaleTaskRuns(ctx, now.Add(-c.timeout), staleRunLimit)
	if err != nil {
		c.log().WarnContext(ctx, "stale run sweep failed", "err", err)
		return
	}
	message := "this run was abandoned: no worker reported an outcome within " + c.timeout.String()
	for _, run := range stale {
		c.finish(ctx, run, coretask.RunStatusFailed, coretask.FailureAbandoned, message, now, "failed an abandoned task run")
	}
}

// sweepCanceled finishes runs whose cancel request nobody honored.
//
// It runs before the abandoned sweep so a run that is both canceled and
// abandoned ends as CANCELED. That is the more informative of the two answers:
// it names why the run stopped, and the person who asked is the one waiting.
func (c *StaleRunReaper) sweepCanceled(ctx context.Context, now time.Time) {
	canceled, err := c.runs.ListCancelRequestedTaskRuns(ctx, now.Add(-c.cancelGrace), staleRunLimit)
	if err != nil {
		c.log().WarnContext(ctx, "canceled run sweep failed", "err", err)
		return
	}
	message := "this run was canceled: no worker confirmed the cancel within " + c.cancelGrace.String()
	for _, run := range canceled {
		c.finish(ctx, run, coretask.RunStatusCanceled, "", message, now, "canceled a task run whose worker never confirmed")
	}
}

// finish writes one terminal outcome and its task projection atomically.
//
// The message names what the server observed rather than guessing a cause,
// because from here a dead worker, an evicted pod, and an expired credential
// look identical.
func (c *StaleRunReaper) finish(ctx context.Context, run coretask.Run, status coretask.RunStatus, class coretask.FailureClass, message string, now time.Time, logMsg string) {
	ctx = buildmaxlog.With(ctx, "task_run_id", run.ID, "space_id", run.SpaceID)
	endedAt := now
	updated, err := c.runs.TransitionTaskRun(ctx, coretask.TransitionRunInput{
		TaskRunID:      run.ID,
		ExpectedStatus: coretask.RunStatus(run.Status),
		NewStatus:      status,
		EndedAt:        &endedAt,
		ErrorMessage:   &message,
		FailureClass:   class,
	})
	if err != nil {
		c.log().ErrorContext(ctx, "could not finish an unreported run", "status", status, "err", err)
		return
	}
	if !updated {
		c.log().InfoContext(ctx, "run outcome changed during stale-run sweep", "status_was", run.Status)
		return
	}
	c.log().WarnContext(ctx, logMsg, "status_was", run.Status)
}
