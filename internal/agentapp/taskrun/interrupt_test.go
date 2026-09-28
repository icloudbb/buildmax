package taskrun

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

// A run stopped because its worker is going away is neither a cancel nor a
// silent disappearance. It has to be recognised as its own reason, or the
// outcome on the record is wrong in one direction or the other.
func TestRunInterruptedOnlyRecognisesAShutdownCause(t *testing.T) {
	shutdown, cancelShutdown := context.WithCancelCause(context.Background())
	cancelShutdown(coretask.ErrRunInterrupted)
	if !runInterrupted(shutdown) {
		t.Error("a context canceled with ErrRunInterrupted does not read as an interruption")
	}
	if runCanceled(shutdown) {
		t.Error("an interrupted run reads as canceled")
	}

	asked, cancelAsked := context.WithCancelCause(context.Background())
	cancelAsked(coretask.ErrRunCanceled)
	if runInterrupted(asked) {
		t.Error("a canceled run reads as interrupted")
	}

	plain, cancelPlain := context.WithCancel(context.Background())
	cancelPlain()
	if runInterrupted(plain) {
		t.Error("an ordinary cancellation reads as an interruption")
	}
}

// The whole point of catching the signal: an interrupted run reports a terminal
// status while it still can, and keeps what it produced, instead of staying
// RUNNING until the stale-run reaper closes it.
func TestReportInterruptedRunReportsFailedAndKeepsPartialWork(t *testing.T) {
	updater := &fakeUpdater{}
	scope := RunScope{SpaceID: "tm1", TaskID: "t1", TaskRunID: "r1"}
	result := runResult{
		EndTime:   time.Unix(1_800_000_000, 0).UTC(),
		OutputStr: "as far as I got",
		Output:    []byte("as far as I got"),
	}

	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(coretask.ErrRunInterrupted)
	err := reportInterruptedRun(ctx, scope, result, runDirs{runGlobal: t.TempDir()}, RunTaskInput{
		Persist: newFakePersistStorage(),
		Updater: updater,
	})

	if !errors.Is(err, coretask.ErrRunInterrupted) {
		t.Fatalf("err = %v, want ErrRunInterrupted", err)
	}
	if updater.req == nil {
		t.Fatal("the run never reported an outcome")
	}
	if updater.req.Status != string(coretask.RunStatusFailed) {
		t.Errorf("status = %q, want FAILED", updater.req.Status)
	}
	// FAILED is shared with a run that failed at its work, so the message is
	// the only thing that tells a reader which happened.
	if updater.req.ErrorMessage == nil || !strings.Contains(*updater.req.ErrorMessage, "shut down") {
		t.Errorf("error_message = %v, want one naming the shutdown", updater.req.ErrorMessage)
	}
	if c := updater.req.FailureClass; c == nil || *c != string(coretask.FailureInterrupted) {
		t.Errorf("failure_class = %v, want interrupted", c)
	}
	// The reply is the run's one persisted output, carried on the status patch.
	if updater.req.Output == nil || *updater.req.Output != "as far as I got" {
		t.Errorf("output = %v, want the partial reply the run had produced", updater.req.Output)
	}
}

// A run someone cancelled, which then catches the shutdown on its way out, was
// still cancelled. That is the outcome the person who pressed stop is waiting
// to see.
func TestCancelWinsOverAnInterruptionOnTheSameRun(t *testing.T) {
	updater := &fakeUpdater{}
	scope := RunScope{SpaceID: "tm1", TaskID: "t1", TaskRunID: "r1"}
	dirs := runDirs{runGlobal: t.TempDir()}
	input := RunTaskInput{
		Persist: newFakePersistStorage(),
		Updater: updater,
	}

	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(coretask.ErrRunCanceled)
	// A second cause never replaces the first, which is what makes the order
	// here a real question rather than a formality.
	cancel(coretask.ErrRunInterrupted)

	stopped, err := reportStoppedRun(ctx, scope, runResult{}, dirs, input)
	if !stopped {
		t.Fatal("a stopped run was not recognised as stopped")
	}
	if !errors.Is(err, coretask.ErrRunCanceled) {
		t.Fatalf("err = %v, want ErrRunCanceled", err)
	}
	if updater.req == nil || updater.req.Status != string(coretask.RunStatusCanceled) {
		t.Fatalf("status = %v, want CANCELED", updater.req)
	}
}

// A run that is simply still going must not be reported as stopped.
func TestReportStoppedRunIgnoresALiveRun(t *testing.T) {
	updater := &fakeUpdater{}
	stopped, err := reportStoppedRun(context.Background(), RunScope{TaskRunID: "r1"}, runResult{}, runDirs{}, RunTaskInput{
		Persist: newFakePersistStorage(),
		Updater: updater,
	})
	if stopped || err != nil {
		t.Fatalf("reportStoppedRun(live) = (%v, %v), want (false, nil)", stopped, err)
	}
	if updater.req != nil {
		t.Errorf("a live run reported %v", updater.req)
	}
}
