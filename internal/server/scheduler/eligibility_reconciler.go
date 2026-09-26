package scheduler

import (
	"context"
	"errors"
	"time"

	"github.com/icloudbb/buildmax/internal/core/eligibility"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	buildmaxlog "github.com/icloudbb/buildmax/internal/infra/log"
)

// defaultEligibilitySweepInterval is how often active runs are re-checked for an
// initiator that lost authority. It is coarse: a disable or membership removal
// stops new work at once through the admission gates, and this sweep exists only
// to reach work already running, which no admission path can revisit.
const defaultEligibilitySweepInterval = time.Minute

// eligibilityScanBatch bounds one sweep so a large backlog cannot hold the loop
// or the database for an unbounded time. The next tick continues from where this
// one stopped.
const eligibilityScanBatch = 200

// EligibilityReconcilerStore is the narrow store surface the reconciler needs.
type EligibilityReconcilerStore interface {
	ListActiveTaskRunsForEligibility(ctx context.Context, afterID string, limit int) ([]coretask.ActiveRunRef, error)
	RequestTaskRunCancel(ctx context.Context, taskRunID, requestedBy, reason string, requestedAt time.Time) (bool, error)
}

// EligibilityReconciler is the durable backstop that stops work already under
// way once its initiator can no longer run work in its Space. The admission
// gates refuse new work at once; a run already handed to a worker is only
// reachable here. It scans active runs, re-checks each initiator's eligibility,
// and requests cancellation for those that lost it — recording why, and leaving
// the worker's graceful stop and the stale-run backstop to settle the run as
// CANCELED. Every action is idempotent: a run already asked to stop is skipped
// by the query, so repeated sweeps converge rather than pile up.
//
// It needs no offboarding record because eligibility itself is the durable
// unfinished-work predicate: as long as an ineligible initiator owns active
// work, a sweep finds and stops it. See
// docs/design/system-administration.md §8.2.
type EligibilityReconciler struct {
	runs     EligibilityReconcilerStore
	eligible eligibility.Checker
	interval time.Duration
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// NewEligibilityReconciler returns a reconciler, or nil when either dependency
// is missing — so a deployment that wires no authority stores skips it and a
// caller need not check before starting. Zero interval uses the default.
func NewEligibilityReconciler(runs EligibilityReconcilerStore, eligible eligibility.Checker, interval time.Duration) *EligibilityReconciler {
	if runs == nil || eligible == nil {
		return nil
	}
	if interval <= 0 {
		interval = defaultEligibilitySweepInterval
	}
	return &EligibilityReconciler{
		runs:     runs,
		eligible: eligible,
		interval: interval,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// Start launches the sweep loop. Calling it on a nil reconciler is a no-op.
func (c *EligibilityReconciler) Start() {
	if c == nil {
		return
	}
	go c.loop()
	c.log().Info("started", "interval", c.interval)
}

// Stop signals the loop to exit and blocks until it has finished.
func (c *EligibilityReconciler) Stop() {
	if c == nil {
		return
	}
	close(c.stopCh)
	<-c.doneCh
	c.log().Info("stopped")
}

func (c *EligibilityReconciler) loop() {
	defer close(c.doneCh)
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.Sweep(context.Background(), time.Now().UTC())
		}
	}
}

// Sweep requests cancellation for every active run whose initiator is no longer
// eligible. It pages through active runs in bounded batches and is exported so a
// test can drive one pass. An authority-store error for one run leaves that run
// alone — a transient outage must not cancel work on a guess — and the sweep
// moves on.
func (c *EligibilityReconciler) Sweep(ctx context.Context, now time.Time) {
	if c == nil {
		return
	}
	afterID := ""
	for {
		refs, err := c.runs.ListActiveTaskRunsForEligibility(ctx, afterID, eligibilityScanBatch)
		if err != nil {
			c.log().WarnContext(ctx, "eligibility sweep failed to list active runs", "err", err)
			return
		}
		if len(refs) == 0 {
			return
		}
		for _, ref := range refs {
			c.reconcileRun(ctx, ref, now)
		}
		if len(refs) < eligibilityScanBatch {
			return
		}
		afterID = refs[len(refs)-1].TaskRunID
	}
}

func (c *EligibilityReconciler) reconcileRun(ctx context.Context, ref coretask.ActiveRunRef, now time.Time) {
	if ref.CreatedBy == "" {
		return
	}
	reason := coretask.CancelReasonCreatorNotMember
	switch err := c.eligible.Check(ctx, ref.CreatedBy, ref.SpaceID); {
	case err == nil:
		return
	case errors.Is(err, eligibility.ErrUnavailable):
		// Authority unknown: leave the run alone rather than cancel on a guess.
		return
	case errors.Is(err, eligibility.ErrAccountDisabled):
		reason = coretask.CancelReasonCreatorDisabled
	}
	ctx = buildmaxlog.With(ctx, "task_run_id", ref.TaskRunID)
	// No requester: a background reconciler, not a person, asked. The reason
	// carries why. The worker honors the request; the stale-run backstop settles
	// a run whose worker never confirms as CANCELED.
	requested, err := c.runs.RequestTaskRunCancel(ctx, ref.TaskRunID, "", reason, now)
	if err != nil {
		c.log().ErrorContext(ctx, "could not request cancel for an ineligible run", "err", err)
		return
	}
	if requested {
		c.log().InfoContext(ctx, "requested cancel for a run whose initiator lost authority", "user_id", ref.CreatedBy, "reason", reason)
	}
}
