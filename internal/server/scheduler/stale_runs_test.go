package scheduler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

// fakeStaleStore records what the reaper did and lets a test control what it
// finds.
type fakeStaleStore struct {
	stale            []coretask.Run
	canceled         []coretask.Run
	lost             []coretask.Run
	listErr          error
	cancelListErr    error
	lostListErr      error
	lastCutoff       time.Time
	lastCancelCutoff time.Time
	lastLostCutoff   time.Time
	transitions      []coretask.TransitionRunInput
	updateErr        error
	transitionWon    *bool
}

func (f *fakeStaleStore) ListStaleTaskRuns(_ context.Context, cutoff time.Time, _ int) ([]coretask.Run, error) {
	f.lastCutoff = cutoff
	return f.stale, f.listErr
}

func (f *fakeStaleStore) ListCancelRequestedTaskRuns(_ context.Context, cutoff time.Time, _ int) ([]coretask.Run, error) {
	f.lastCancelCutoff = cutoff
	return f.canceled, f.cancelListErr
}

func (f *fakeStaleStore) ListLostWorkerTaskRuns(_ context.Context, cutoff time.Time, _ int) ([]coretask.Run, error) {
	f.lastLostCutoff = cutoff
	return f.lost, f.lostListErr
}

func (f *fakeStaleStore) TransitionTaskRun(_ context.Context, in coretask.TransitionRunInput) (bool, error) {
	if f.updateErr != nil {
		return false, f.updateErr
	}
	f.transitions = append(f.transitions, in)
	if f.transitionWon != nil {
		return *f.transitionWon, nil
	}
	return true, nil
}

func newStaleFixture(stale ...coretask.Run) (*fakeStaleStore, *StaleRunReaper) {
	f := &fakeStaleStore{stale: stale}
	return f, NewStaleRunReaper(f, nil, 6*time.Hour, time.Hour)
}

// TestReaperClosesAbandonedRuns is the safety net that makes a per-run
// credential viable. Only a worker moves a run out of SCHEDULED or RUNNING, so
// a run whose worker died — or whose token expired before it could report —
// would otherwise sit there forever, showing as in-progress work that will never
// finish.
func TestReaperClosesAbandonedRuns(t *testing.T) {
	store, reaper := newStaleFixture(
		coretask.Run{ID: "r_1", Status: string(coretask.RunStatusRunning)},
		coretask.Run{ID: "r_2", Status: string(coretask.RunStatusScheduled)},
	)

	now := time.Unix(1_800_000_000, 0)
	reaper.Sweep(context.Background(), now)

	if len(store.transitions) != 2 {
		t.Fatalf("finished %d runs, want 2", len(store.transitions))
	}
	for _, in := range store.transitions {
		if in.NewStatus != coretask.RunStatusFailed {
			t.Errorf("run %s status = %q, want FAILED", in.TaskRunID, in.NewStatus)
		}
		if in.EndedAt == nil || !in.EndedAt.Equal(now) {
			t.Errorf("run %s ended_at = %v, want %d", in.TaskRunID, in.EndedAt, now.Unix())
		}
		// The message names the timeout rather than guessing a cause: from the
		// server a dead worker, an evicted pod, and an expired credential are
		// indistinguishable, and inventing one would mislead whoever reads it.
		if in.ErrorMessage == nil || !strings.Contains(*in.ErrorMessage, "6h0m0s") {
			t.Errorf("run %s message = %v, want it to name the timeout", in.TaskRunID, in.ErrorMessage)
		}
		if in.FailureClass != coretask.FailureAbandoned {
			t.Errorf("run %s class = %q, want abandoned", in.TaskRunID, in.FailureClass)
		}
	}
}

// TestReaperClosesUnconfirmedCancels covers the case a cooperative cancel
// cannot: the worker that was supposed to stop is gone. Without this the run
// keeps showing as in progress after someone pressed stop, which reads as the
// cancel having done nothing.
func TestReaperClosesUnconfirmedCancels(t *testing.T) {
	store := &fakeStaleStore{
		canceled: []coretask.Run{{ID: "r_1", Status: string(coretask.RunStatusRunning)}},
	}
	reaper := NewStaleRunReaper(store, nil, 6*time.Hour, time.Hour)

	now := time.Unix(1_800_000_000, 0)
	reaper.Sweep(context.Background(), now)

	if len(store.transitions) != 1 {
		t.Fatalf("finished %d runs, want 1", len(store.transitions))
	}
	got := store.transitions[0]
	if got.NewStatus != coretask.RunStatusCanceled {
		t.Errorf("status = %q, want CANCELED — a stop that was asked for is not a failure", got.NewStatus)
	}
	if got.EndedAt == nil || !got.EndedAt.Equal(now) {
		t.Errorf("ended_at = %v, want %v", got.EndedAt, now)
	}
	if got.ErrorMessage == nil || !strings.Contains(*got.ErrorMessage, defaultCancelGrace.String()) {
		t.Errorf("message = %v, want it to name the grace period", got.ErrorMessage)
	}
	if want := now.Add(-defaultCancelGrace); !store.lastCancelCutoff.Equal(want) {
		t.Errorf("cancel cutoff = %v, want %v — a run asked to stop a moment ago is still the worker's to end", store.lastCancelCutoff, want)
	}
}

// A failing cancel query must not stop the abandoned sweep: they answer
// different questions and one being unavailable is no reason to skip the other.
func TestReaperSweepsAbandonedRunsWhenTheCancelQueryFails(t *testing.T) {
	store := &fakeStaleStore{
		stale:         []coretask.Run{{ID: "r_1", Status: string(coretask.RunStatusRunning)}},
		cancelListErr: errors.New("database is away"),
	}
	reaper := NewStaleRunReaper(store, nil, 6*time.Hour, time.Hour)

	reaper.Sweep(context.Background(), time.Unix(1_800_000_000, 0))

	if len(store.transitions) != 1 || store.transitions[0].NewStatus != coretask.RunStatusFailed {
		t.Errorf("transitions = %v, want the abandoned run failed", store.transitions)
	}
}

func TestReaperCutoffIsTheTimeoutAgo(t *testing.T) {
	store, reaper := newStaleFixture()
	now := time.Unix(1_800_000_000, 0)

	reaper.Sweep(context.Background(), now)

	if want := now.Add(-6 * time.Hour); !store.lastCutoff.Equal(want) {
		t.Errorf("cutoff = %v, want %v", store.lastCutoff, want)
	}
}

// TestReaperSurvivesFailures keeps a sweep from taking the server down with it.
// Every run it touches has already stopped progressing, so a failed sweep costs
// nothing that the next tick cannot recover.
func TestReaperSurvivesFailures(t *testing.T) {
	t.Run("the query fails", func(t *testing.T) {
		store, reaper := newStaleFixture(coretask.Run{ID: "r_1"})
		store.listErr = errors.New("database is away")
		reaper.Sweep(context.Background(), time.Now())
		if len(store.transitions) != 0 {
			t.Error("runs were failed from a query that errored")
		}
	})

	t.Run("the update fails", func(t *testing.T) {
		store, reaper := newStaleFixture(coretask.Run{ID: "r_1"})
		store.updateErr = errors.New("database is away")
		reaper.Sweep(context.Background(), time.Now())
		if len(store.transitions) != 0 {
			t.Error("a failed transition was recorded")
		}
	})
}

func TestReaperDoesNotOverwriteAConcurrentWorkerOutcome(t *testing.T) {
	won := false
	store := &fakeStaleStore{
		stale:         []coretask.Run{{ID: "r_1", Status: string(coretask.RunStatusRunning)}},
		transitionWon: &won,
	}
	reaper := NewStaleRunReaper(store, nil, 6*time.Hour, time.Hour)

	reaper.Sweep(context.Background(), time.Unix(1_800_000_000, 0))

	if len(store.transitions) != 1 {
		t.Fatalf("transitions = %d, want one conditional attempt", len(store.transitions))
	}
	if store.transitions[0].ExpectedStatus != coretask.RunStatusRunning {
		t.Errorf("expected status = %q, want RUNNING", store.transitions[0].ExpectedStatus)
	}
}

// TestNilReaperIsInert covers the deployment with no store: Start, Stop, and
// Sweep all have to be safe so the caller does not need a nil check.
func TestNilReaperIsInert(t *testing.T) {
	var reaper *StaleRunReaper
	if got := NewStaleRunReaper(nil, nil, 0, 0); got != nil {
		t.Error("a reaper was built with no store to sweep")
	}
	reaper.Start()
	reaper.Sweep(context.Background(), time.Now())
	reaper.Stop()
}

func TestReaperDefaultsAreApplied(t *testing.T) {
	store := &fakeStaleStore{}
	reaper := NewStaleRunReaper(store, nil, 0, 0)
	if reaper.timeout != defaultRunTimeout {
		t.Errorf("timeout = %v, want %v", reaper.timeout, defaultRunTimeout)
	}
	if reaper.interval != defaultStaleSweepInterval {
		t.Errorf("interval = %v, want %v", reaper.interval, defaultStaleSweepInterval)
	}
	if reaper.cancelGrace != defaultCancelGrace {
		t.Errorf("cancel grace = %v, want %v", reaper.cancelGrace, defaultCancelGrace)
	}
	if reaper.livenessGrace != defaultLivenessGrace {
		t.Errorf("liveness grace = %v, want %v", reaper.livenessGrace, defaultLivenessGrace)
	}
}

// TestReaperClosesRunsWhoseWorkerWentSilent is the whole point of recording the
// poll a worker already makes: a SIGKILLed pod reports nothing, and before this
// the run sat RUNNING until the six-hour timeout while a user watched it.
func TestReaperClosesRunsWhoseWorkerWentSilent(t *testing.T) {
	store := &fakeStaleStore{
		lost: []coretask.Run{{ID: "r_1", Status: string(coretask.RunStatusRunning)}},
	}
	reaper := NewStaleRunReaper(store, nil, 6*time.Hour, time.Hour)

	now := time.Unix(1_800_000_000, 0)
	reaper.Sweep(context.Background(), now)

	if len(store.transitions) != 1 {
		t.Fatalf("finished %d runs, want 1", len(store.transitions))
	}
	got := store.transitions[0]
	if got.NewStatus != coretask.RunStatusFailed {
		t.Errorf("status = %q, want FAILED", got.NewStatus)
	}
	// Only from RUNNING: the worker's own terminal report must still win if it
	// arrives first, and ExpectedStatus is what makes that a race nobody loses.
	if got.ExpectedStatus != coretask.RunStatusRunning {
		t.Errorf("expected status = %q, want RUNNING", got.ExpectedStatus)
	}
	if got.EndedAt == nil || !got.EndedAt.Equal(now) {
		t.Errorf("ended_at = %v, want %v", got.EndedAt, now)
	}
	// The message says what was observed — silence for a stated window — rather
	// than naming a cause the server cannot tell apart from a partition.
	if got.ErrorMessage == nil || !strings.Contains(*got.ErrorMessage, defaultLivenessGrace.String()) {
		t.Errorf("message = %v, want it to name the liveness grace", got.ErrorMessage)
	}
	if got.FailureClass != coretask.FailureWorkerLost {
		t.Errorf("class = %q, want worker_lost", got.FailureClass)
	}
	if want := now.Add(-defaultLivenessGrace); !store.lastLostCutoff.Equal(want) {
		t.Errorf("liveness cutoff = %v, want %v", store.lastLostCutoff, want)
	}
}

// A run that is both canceled and silent ends as CANCELED. Someone asked it to
// stop and is waiting for that answer; "the worker went away" is true but is
// not what they need to be told.
func TestReaperAnswersACancelBeforeALostWorker(t *testing.T) {
	run := coretask.Run{ID: "r_1", Status: string(coretask.RunStatusRunning)}
	store := &fakeStaleStore{canceled: []coretask.Run{run}, lost: []coretask.Run{run}}
	won := true
	store.transitionWon = &won
	reaper := NewStaleRunReaper(store, nil, 6*time.Hour, time.Hour)

	reaper.Sweep(context.Background(), time.Unix(1_800_000_000, 0))

	if len(store.transitions) == 0 || store.transitions[0].NewStatus != coretask.RunStatusCanceled {
		t.Fatalf("transitions = %v, want the cancel to be written first", store.transitions)
	}
}

// The three sweeps answer different questions, so one query being unavailable
// is no reason to skip the others.
func TestReaperSweepsOnWhenTheLivenessQueryFails(t *testing.T) {
	store := &fakeStaleStore{
		stale:       []coretask.Run{{ID: "r_1", Status: string(coretask.RunStatusRunning)}},
		lostListErr: errors.New("database is away"),
	}
	reaper := NewStaleRunReaper(store, nil, 6*time.Hour, time.Hour)

	reaper.Sweep(context.Background(), time.Unix(1_800_000_000, 0))

	if len(store.transitions) != 1 || store.transitions[0].NewStatus != coretask.RunStatusFailed {
		t.Errorf("transitions = %v, want the abandoned run still failed", store.transitions)
	}
}

// fakeJobDeleter records which worker Jobs the reaper asked to delete.
type fakeJobDeleter struct {
	deleted []string
	err     error
}

func (f *fakeJobDeleter) DeleteWorkerJob(_ context.Context, jobName string) error {
	f.deleted = append(f.deleted, jobName)
	return f.err
}

func runWithJob(id string) coretask.Run {
	job := "buildmax-worker-" + id + "-1"
	return coretask.Run{ID: id, Status: string(coretask.RunStatusRunning), K8sJobName: &job}
}

// A hung or partitioned worker still holds its pod after its run is reaped, so
// every sweep that ends a run deletes its Job.
func TestReaperDeletesTheJobOfEachRunItEnds(t *testing.T) {
	for name, store := range map[string]*fakeStaleStore{
		"unconfirmed cancel": {canceled: []coretask.Run{runWithJob("r_1")}},
		"lost worker":        {lost: []coretask.Run{runWithJob("r_1")}},
		"abandoned":          {stale: []coretask.Run{runWithJob("r_1")}},
	} {
		t.Run(name, func(t *testing.T) {
			jobs := &fakeJobDeleter{}
			NewStaleRunReaper(store, jobs, 6*time.Hour, time.Hour).Sweep(context.Background(), time.Unix(1_800_000_000, 0))

			if len(store.transitions) != 1 {
				t.Fatalf("transitions = %d, want 1", len(store.transitions))
			}
			if len(jobs.deleted) != 1 || jobs.deleted[0] != "buildmax-worker-r_1-1" {
				t.Errorf("deleted jobs = %v, want the run's own Job", jobs.deleted)
			}
		})
	}
}

// The reaper deletes only the Jobs of runs it ended. A run someone else
// finished is theirs; a run it failed to finish is retried next sweep.
func TestReaperLeavesJobsOfRunsItDidNotEnd(t *testing.T) {
	lost := false
	for name, store := range map[string]*fakeStaleStore{
		"no job was recorded":   {stale: []coretask.Run{{ID: "r_1", Status: string(coretask.RunStatusRunning)}}},
		"the race was lost":     {stale: []coretask.Run{runWithJob("r_1")}, transitionWon: &lost},
		"the transition failed": {stale: []coretask.Run{runWithJob("r_1")}, updateErr: errors.New("database is away")},
	} {
		t.Run(name, func(t *testing.T) {
			jobs := &fakeJobDeleter{}
			NewStaleRunReaper(store, jobs, 6*time.Hour, time.Hour).Sweep(context.Background(), time.Unix(1_800_000_000, 0))
			if len(jobs.deleted) != 0 {
				t.Errorf("deleted jobs = %v, want none", jobs.deleted)
			}
		})
	}
}

// Deleting the Job is cleanup after the outcome, not part of it: a failure is
// logged and the run stays ended.
func TestReaperJobDeletionFailureLeavesTheRunTerminal(t *testing.T) {
	store := &fakeStaleStore{lost: []coretask.Run{runWithJob("r_1")}}
	jobs := &fakeJobDeleter{err: errors.New("the API server is away")}

	NewStaleRunReaper(store, jobs, 6*time.Hour, time.Hour).Sweep(context.Background(), time.Unix(1_800_000_000, 0))

	if len(jobs.deleted) != 1 {
		t.Fatalf("deletion attempts = %d, want 1", len(jobs.deleted))
	}
	if len(store.transitions) != 1 || store.transitions[0].NewStatus != coretask.RunStatusFailed {
		t.Errorf("transitions = %v, want the run failed and nothing after", store.transitions)
	}
}

// A local-process deployment has no Job runner; a recorded name must not stop
// the reaper from ending the run.
func TestReaperWithoutAJobRunnerStillEndsRuns(t *testing.T) {
	store := &fakeStaleStore{stale: []coretask.Run{runWithJob("r_1")}}
	NewStaleRunReaper(store, nil, 6*time.Hour, time.Hour).Sweep(context.Background(), time.Unix(1_800_000_000, 0))
	if len(store.transitions) != 1 {
		t.Errorf("transitions = %d, want 1", len(store.transitions))
	}
}
