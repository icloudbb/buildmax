package db

import (
	"errors"
	"sync"
	"testing"
	"time"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/util"
)

// Four store methods claim, in their own comments or their own rules, that
// exactly one of several simultaneous callers may win: ClaimTask ("exactly one
// row was updated"), TransitionTaskRun ("a worker and a recovery loop cannot
// overwrite one another's outcome"), RequestTaskRunCancel, and CreateTaskRun
// ("at most one active TaskRun exists per Task"). The first three implement
// that as a conditional UPDATE resting on the server serializing two writes to
// one row; CreateTaskRun implements it as a locking read of the task row
// followed by a count-then-insert inside one transaction, because the
// guarantee it needs is "at most one row matching a predicate", which a
// conditional UPDATE on an existing row cannot express for a row that does not
// exist yet.
//
// They are grouped here rather than filed under their four subjects because
// the property, the harness, and the way they fail are one thing: a sequential
// test passes against any implementation that merely reads before it writes,
// and only contention tells the two apart. The existing sequential cases stay
// where they are -- this file is what those cannot cover.
//
// See docs/design/verification-program.md §4.2.

// raceCount is how many callers contend. Enough that a lost update is likely
// rather than lucky, small enough to stay quick against a real server.
const raceCount = 8

// race runs attempt raceCount times at once, releasing every goroutine from one
// channel so they arrive together rather than in the order they started.
func race(t *testing.T, attempt func(i int) (bool, error)) int {
	t.Helper()
	won := make([]bool, raceCount)
	errs := make([]error, raceCount)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range raceCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			won[i], errs[i] = attempt(i)
		}()
	}
	close(start)
	wg.Wait()

	winners := 0
	for i := range raceCount {
		if errs[i] != nil {
			t.Errorf("attempt %d: %v", i, errs[i])
			continue
		}
		if won[i] {
			winners++
		}
	}
	return winners
}

// newRunForTest creates a conversation, a task, and its first run, and returns
// the task and run handles.
func newRunForTest(t *testing.T, s *Store, label string) (task *coretask.Task, runID, conversationID string) {
	t.Helper()
	ctx := t.Context()
	userID := newTestUser(t, s, label)
	spaceID := newTestSpace(t, s, userID)
	conversation, err := s.CreateConversationInSpace(ctx, spaceID, userID, "portal", userID)
	if err != nil {
		t.Fatalf("CreateConversationInSpace: %v", err)
	}
	task, err = s.CreateTask(ctx, &coretask.CreateInput{
		SpaceID:        spaceID,
		ConversationID: conversation.ID,
		Input:          "input",
		CreatedBy:      userID,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.LastRunID == nil {
		t.Fatal("CreateTask did not create its first run")
	}
	t.Cleanup(func() {
		_ = s.db.Delete(&taskRunRow{}, "public_id = ?", canonicalPublicID(*task.LastRunID)).Error
		_ = s.db.Delete(&taskRow{}, "public_id = ?", canonicalPublicID(task.ID)).Error
		_ = s.db.Delete(&conversationRow{}, "public_id = ?", canonicalPublicID(conversation.ID)).Error
	})
	return task, *task.LastRunID, conversation.ID
}

// Two workers claiming one task is the case the scheduler's correctness rests
// on: a second winner means the same run executes twice, doubling its side
// effects and its token spend.
func TestClaimTaskHasOneWinnerUnderContention(t *testing.T) {
	s, ctx := newTestStore(t)
	task, _, _ := newRunForTest(t, s, "claim-race")

	winners := race(t, func(int) (bool, error) {
		return s.ClaimTask(ctx, coretask.ClaimInput{
			TaskID:         task.ID,
			ExpectedStatus: "PENDING",
			NewStatus:      "SCHEDULED",
		})
	})
	if winners != 1 {
		t.Errorf("%d of %d concurrent claims succeeded, want exactly 1", winners, raceCount)
	}

	stored, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if stored.Status != "SCHEDULED" {
		t.Errorf("task status = %q, want SCHEDULED", stored.Status)
	}
}

// A worker reporting success and a stale-run reaper reporting failure can reach
// the row at the same moment. One outcome must be committed whole -- including
// the task projection and the artifact rows written in the same transaction --
// and the other must be refused rather than half-applied over it.
func TestTransitionTaskRunHasOneWinnerUnderContention(t *testing.T) {
	s, ctx := newTestStore(t)
	task, runID, _ := newRunForTest(t, s, "transition-race")
	startTaskRunForTest(t, s, ctx, runID)

	endedAt := time.Unix(1_800_000_000, 0).UTC()
	winners := race(t, func(i int) (bool, error) {
		in := coretask.TransitionRunInput{
			TaskRunID:      runID,
			ExpectedStatus: coretask.RunStatusRunning,
			EndedAt:        &endedAt,
		}
		// Half report success, half report failure. Whichever commits, the run
		// and its task must tell the same story afterwards.
		if i%2 == 0 {
			in.NewStatus = coretask.RunStatusSucceeded
			in.Output = util.Ptr("worker result")
		} else {
			in.NewStatus = coretask.RunStatusFailed
			in.ErrorMessage = util.Ptr("reaper outcome")
		}
		return s.TransitionTaskRun(ctx, in)
	})
	if winners != 1 {
		t.Errorf("%d of %d concurrent transitions succeeded, want exactly 1", winners, raceCount)
	}

	run, err := s.GetTaskRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetTaskRun: %v", err)
	}
	if run == nil || !coretask.RunStatusTerminal(run.Status) {
		t.Fatalf("run = %+v, want a terminal status", run)
	}
	stored, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if stored.Status != run.Status {
		t.Errorf("task status = %q but its run says %q; the projection was written by a losing caller",
			stored.Status, run.Status)
	}
}

// Two concurrent Continue requests -- a doubled client retry, or a person
// clicking twice -- are the case the one-active-run-per-task rule exists for.
// A second winner means two TaskRuns execute the same Task's session at once.
func TestCreateTaskRunHasOneActiveWinnerUnderContention(t *testing.T) {
	s, ctx := newTestStore(t)
	task, firstRunID, _ := newRunForTest(t, s, "create-run-race")
	// The task's own first run is PENDING and already active; finish it so the
	// race below starts from zero active runs, same as a real Continue would.
	startTaskRunForTest(t, s, ctx, firstRunID)
	if updated, err := s.TransitionTaskRun(ctx, coretask.TransitionRunInput{
		TaskRunID:      firstRunID,
		ExpectedStatus: coretask.RunStatusRunning,
		NewStatus:      coretask.RunStatusSucceeded,
	}); err != nil || !updated {
		t.Fatalf("TransitionTaskRun to SUCCEEDED: updated=%v err=%v", updated, err)
	}

	var mu sync.Mutex
	var createdRunIDs []string
	winners := race(t, func(int) (bool, error) {
		run, err := s.CreateTaskRun(ctx, coretask.CreateRunInput{
			TaskID: task.ID, Input: "continue", CreatedBy: task.CreatedBy,
		})
		if err != nil {
			if errors.Is(err, coretask.ErrRunInProgress) {
				return false, nil
			}
			return false, err
		}
		mu.Lock()
		createdRunIDs = append(createdRunIDs, run.ID)
		mu.Unlock()
		return true, nil
	})
	t.Cleanup(func() {
		for _, id := range createdRunIDs {
			_ = s.db.Delete(&taskRunRow{}, "public_id = ?", canonicalPublicID(id)).Error
		}
	})
	if winners != 1 {
		t.Errorf("%d of %d concurrent Continue requests created a run, want exactly 1", winners, raceCount)
	}
	if len(createdRunIDs) != winners {
		t.Fatalf("created %d run rows but counted %d winners", len(createdRunIDs), winners)
	}
}

// A cancel records a request; it does not end the run. Writing a terminal
// status here would describe a run that is still executing, and the worker's
// own report would overwrite it moments later -- the hazard
// RequestTaskRunCancel's comment names.
//
// Deterministic on purpose: the racing test below cannot pin this, because
// whether a status-writing cancel is visible there depends on which caller
// reaches the row first.
func TestCancelDoesNotWriteATerminalStatus(t *testing.T) {
	s, ctx := newTestStore(t)
	task, runID, _ := newRunForTest(t, s, "cancel-request")
	canceller := newTestUser(t, s, "cancel-request-user")
	startTaskRunForTest(t, s, ctx, runID)

	accepted, err := s.RequestTaskRunCancel(ctx, runID, canceller, coretask.CancelReasonUserRequested, time.Now().UTC())
	if err != nil {
		t.Fatalf("RequestTaskRunCancel: %v", err)
	}
	if !accepted {
		t.Fatal("a cancel was refused for a running run")
	}
	run, err := s.GetTaskRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetTaskRun: %v", err)
	}
	if run.Status != string(coretask.RunStatusRunning) {
		t.Errorf("run status = %q after a cancel request, want it still RUNNING: "+
			"the worker has not reported yet", run.Status)
	}
	if run.CancelRequestedAt == nil {
		t.Error("the cancel request was not recorded")
	}

	// The worker's report still lands, from the status it last observed.
	endedAt := time.Unix(1_800_000_000, 0).UTC()
	updated, err := s.TransitionTaskRun(ctx, coretask.TransitionRunInput{
		TaskRunID:      runID,
		ExpectedStatus: coretask.RunStatusRunning,
		NewStatus:      coretask.RunStatusCanceled,
		EndedAt:        &endedAt,
		Output:         util.Ptr("partial work"),
	})
	if err != nil || !updated {
		t.Fatalf("worker report after a cancel request: updated=%v err=%v", updated, err)
	}
	stored, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if stored.Status != string(coretask.RunStatusCanceled) {
		t.Errorf("task status = %q, want CANCELED once the worker reported it", stored.Status)
	}
}

// A cancel and a worker's final report are not mutually exclusive: a cancel
// that lands while the run is still active is recorded and the worker's report
// still commits. What must not happen is a task projection that disagrees with
// its run, or a report whose output was torn by the writer beside it.
func TestCancelRacingAReportLeavesOneConsistentOutcome(t *testing.T) {
	s, ctx := newTestStore(t)
	task, runID, _ := newRunForTest(t, s, "cancel-race")
	canceller := newTestUser(t, s, "cancel-race-user")
	startTaskRunForTest(t, s, ctx, runID)

	endedAt := time.Unix(1_800_000_000, 0).UTC()
	// Odd callers report the outcome, even callers ask to stop, all at once.
	race(t, func(i int) (bool, error) {
		if i%2 == 0 {
			return s.RequestTaskRunCancel(ctx, runID, canceller, coretask.CancelReasonUserRequested, time.Now().UTC())
		}
		return s.TransitionTaskRun(ctx, coretask.TransitionRunInput{
			TaskRunID:      runID,
			ExpectedStatus: coretask.RunStatusRunning,
			NewStatus:      coretask.RunStatusSucceeded,
			EndedAt:        &endedAt,
			Output:         util.Ptr("worker result"),
		})
	})

	run, err := s.GetTaskRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetTaskRun: %v", err)
	}
	if run == nil || run.Status != string(coretask.RunStatusSucceeded) {
		t.Fatalf("run = %+v, want the reported SUCCEEDED outcome", run)
	}
	if run.Output == nil || *run.Output != "worker result" {
		t.Errorf("run output = %v, want the worker's result intact", run.Output)
	}
	stored, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if stored.Status != run.Status {
		t.Errorf("task status = %q but its run says %q", stored.Status, run.Status)
	}
}

// A run that already finished must not acquire a stop request: the reaper
// treats a cancel-requested run as one it may still have to close, and a
// pending request against a terminal run is work it can never finish.
//
// This is deliberately its own run rather than an assertion appended to the
// race above. There, a cancel from the race has already set
// cancel_requested_at, and RequestTaskRunCancel's `IS NULL` condition would
// refuse the late one for that reason alone -- masking whether the status
// condition this test exists for is present at all.
func TestCancelIsRefusedOnAFinishedRun(t *testing.T) {
	s, ctx := newTestStore(t)
	_, runID, _ := newRunForTest(t, s, "cancel-late")
	canceller := newTestUser(t, s, "cancel-late-user")
	startTaskRunForTest(t, s, ctx, runID)

	endedAt := time.Unix(1_800_000_000, 0).UTC()
	if updated, err := s.TransitionTaskRun(ctx, coretask.TransitionRunInput{
		TaskRunID:      runID,
		ExpectedStatus: coretask.RunStatusRunning,
		NewStatus:      coretask.RunStatusSucceeded,
		EndedAt:        &endedAt,
	}); err != nil || !updated {
		t.Fatalf("TransitionTaskRun to SUCCEEDED: updated=%v err=%v", updated, err)
	}

	accepted, err := s.RequestTaskRunCancel(ctx, runID, canceller, coretask.CancelReasonUserRequested, time.Now().UTC())
	if err != nil {
		t.Fatalf("RequestTaskRunCancel: %v", err)
	}
	if accepted {
		t.Error("a cancel was accepted against a run that had already finished")
	}
	run, err := s.GetTaskRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetTaskRun: %v", err)
	}
	if run.CancelRequestedAt != nil {
		t.Errorf("finished run carries cancel_requested_at = %v, want none", run.CancelRequestedAt)
	}
	if run.Status != string(coretask.RunStatusSucceeded) {
		t.Errorf("run status = %q, want the outcome it had before the cancel", run.Status)
	}
}
