package db

import (
	"testing"
	"time"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/util"
)

// The store tests share one database, so deployment-wide answers are checked as
// deltas, and this test's rows are dated before anything else can be.
func TestRuntimeAggregatesReadStallsFailuresAndSpaces(t *testing.T) {
	s, ctx := newTestStore(t)
	userID := newTestUser(t, s, "runtime-aggregates")
	conversation, err := s.CreateConversation(ctx, userID, "portal", userID)
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	spaceID := conversation.SpaceID
	now := time.Now().UTC()
	staleBefore := now.Add(-coretask.WorkerLivenessGrace)
	failedSince := now.Add(-24 * time.Hour)
	before, err := s.RuntimeSummary(ctx, staleBefore, failedSince)
	if err != nil {
		t.Fatalf("RuntimeSummary: %v", err)
	}

	newRun := func() string {
		t.Helper()
		task, err := s.CreateTask(ctx, &coretask.CreateInput{
			SpaceID: spaceID, ConversationID: conversation.ID, Input: "input", CreatedBy: userID,
		})
		if err != nil {
			t.Fatalf("CreateTask: %v", err)
		}
		return *task.LastRunID
	}
	set := func(runID string, cols map[string]any) {
		t.Helper()
		id, _ := util.CanonicalPublicID(runID)
		if err := s.db.WithContext(ctx).Model(&taskRunRow{}).Where("public_id = ?", id).Updates(cols).Error; err != nil {
			t.Fatalf("update run: %v", err)
		}
	}
	transition := func(runID string, from, to coretask.RunStatus, class coretask.FailureClass) {
		t.Helper()
		ended := now.Add(-time.Hour)
		in := coretask.TransitionRunInput{TaskRunID: runID, ExpectedStatus: from, NewStatus: to, FailureClass: class}
		if coretask.RunStatusTerminal(string(to)) {
			in.EndedAt = &ended
		}
		if ok, err := s.TransitionTaskRun(ctx, in); err != nil || !ok {
			t.Fatalf("transition %s -> %s: ok=%v err=%v", from, to, ok, err)
		}
	}
	early := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

	pending := newRun()
	set(pending, map[string]any{"created_at": early})

	unstarted := newRun()
	transition(unstarted, coretask.RunStatusPending, coretask.RunStatusScheduled, "")
	set(unstarted, map[string]any{"created_at": early.Add(time.Minute)})

	silent := newRun()
	startTaskRunForTest(t, s, ctx, silent)
	set(silent, map[string]any{"last_seen_at": now.Add(-time.Hour)})

	failed := newRun()
	startTaskRunForTest(t, s, ctx, failed)
	transition(failed, coretask.RunStatusRunning, coretask.RunStatusFailed, coretask.FailureInfrastructure)

	old := newRun()
	startTaskRunForTest(t, s, ctx, old)
	transition(old, coretask.RunStatusRunning, coretask.RunStatusFailed, coretask.FailureModel)
	set(old, map[string]any{"ended_at": now.Add(-72 * time.Hour)})

	after, err := s.RuntimeSummary(ctx, staleBefore, failedSince)
	if err != nil {
		t.Fatalf("RuntimeSummary: %v", err)
	}
	if after.OldestPendingAt == nil || !after.OldestPendingAt.Equal(early) {
		t.Errorf("oldest pending = %v, want %v", after.OldestPendingAt, early)
	}
	if after.OldestUnstartedAt == nil || !after.OldestUnstartedAt.Equal(early.Add(time.Minute)) {
		t.Errorf("oldest unstarted = %v, want %v", after.OldestUnstartedAt, early.Add(time.Minute))
	}
	if got := after.StaleRunning - before.StaleRunning; got != 1 {
		t.Errorf("stale running grew by %d, want 1", got)
	}
	infra := string(coretask.FailureInfrastructure)
	if got := after.FailuresByClass[infra] - before.FailuresByClass[infra]; got != 1 {
		t.Errorf("infrastructure failures grew by %d, want 1", got)
	}
	model := string(coretask.FailureModel)
	if got := after.FailuresByClass[model] - before.FailuresByClass[model]; got != 0 {
		t.Errorf("model failures grew by %d, want 0: that failure is outside the window", got)
	}

	// This Space's active run is the oldest anywhere, so it leads the list.
	spaces, total, err := s.ListSpaceRunActivity(ctx, failedSince, 5, 0)
	if err != nil {
		t.Fatalf("ListSpaceRunActivity: %v", err)
	}
	if total < 1 || len(spaces) == 0 || spaces[0].SpaceID != spaceID {
		t.Fatalf("spaces = %+v (total %d), want %s first", spaces, total, spaceID)
	}
	got := spaces[0]
	want := map[string]int{"PENDING": 1, "SCHEDULED": 1, "RUNNING": 1}
	for status, n := range want {
		if got.Active[status] != n {
			t.Errorf("active[%s] = %d, want %d (all: %v)", status, got.Active[status], n, got.Active)
		}
	}
	if got.OldestActiveAt == nil || !got.OldestActiveAt.Equal(early) {
		t.Errorf("oldest active = %v, want %v", got.OldestActiveAt, early)
	}
	if got.FailuresByClass[infra] != 1 || got.FailuresByClass[model] != 0 {
		t.Errorf("failures = %v, want one infrastructure failure only", got.FailuresByClass)
	}
}
