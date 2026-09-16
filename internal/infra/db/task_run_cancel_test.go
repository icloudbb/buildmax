package db

import (
	"testing"
	"time"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

// TestTaskRunCancelQueries covers the three queries a cancel depends on. They
// are guarded by status rather than by the caller, because the caller is an
// HTTP handler racing a scheduler and a worker for the same row.
func TestTaskRunCancelQueries(t *testing.T) {
	s, ctx := newTestStore(t)
	cancelTestUser := newTestUser(t, s, "cancel-store")

	conv, err := s.CreateConversation(ctx, cancelTestUser, "portal", cancelTestUser)
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	task, err := s.CreateTask(ctx, &coretask.CreateInput{
		SpaceID:        conv.SpaceID,
		ConversationID: conv.ID,
		Input:          "input",
		CreatedBy:      cancelTestUser,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() {
		_ = s.db.WithContext(ctx).Delete(&taskRunRow{}, "task_id = ?", task.ID)
		_ = s.db.WithContext(ctx).Delete(&taskRow{}, "task_id = ?", task.ID)
		_ = s.db.WithContext(ctx).Delete(&conversationRow{}, "conversation_id = ?", conv.ID)
	})
	if task.LastRunID == nil {
		t.Fatal("CreateTask should create the first run")
	}
	runID := *task.LastRunID

	active, err := s.GetActiveTaskRunByTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetActiveTaskRunByTask: %v", err)
	}
	if active == nil || active.ID != runID {
		t.Fatalf("active run = %+v, want the task's pending run %s", active, runID)
	}

	requested, err := s.RequestTaskRunCancel(ctx, runID, cancelTestUser, coretask.CancelReasonUserRequested, time.Unix(1_800_000_000, 0).UTC())
	if err != nil {
		t.Fatalf("RequestTaskRunCancel: %v", err)
	}
	if !requested {
		t.Fatal("the first cancel request was not recorded")
	}
	// A second request must not overwrite the first: the stored name is whoever
	// asked, and the stored time is what the backstop measures against.
	again, err := s.RequestTaskRunCancel(ctx, runID, newTestUser(t, s, "cancel-other"), coretask.CancelReasonUserRequested, time.Unix(1_800_009_999, 0).UTC())
	if err != nil {
		t.Fatalf("RequestTaskRunCancel again: %v", err)
	}
	if again {
		t.Error("a second cancel request overwrote the first")
	}
	stored, err := s.GetTaskRun(ctx, runID)
	if err != nil || stored == nil {
		t.Fatalf("GetTaskRun: %v", err)
	}
	if stored.CancelRequestedAt == nil || !stored.CancelRequestedAt.Equal(time.Unix(1_800_000_000, 0).UTC()) {
		t.Errorf("cancel_requested_at = %v, want the first request's time", stored.CancelRequestedAt)
	}
	if stored.CancelRequestedBy == nil || *stored.CancelRequestedBy != cancelTestUser {
		t.Errorf("cancel_requested_by = %v, want %q", stored.CancelRequestedBy, cancelTestUser)
	}
	if stored.Status != string(coretask.RunStatusPending) {
		t.Errorf("status = %q, want the request to leave it alone", stored.Status)
	}

	// The backstop only sees requests older than the cutoff, so a run asked to
	// stop a moment ago is still its worker's to end.
	early, err := s.ListCancelRequestedTaskRuns(ctx, time.Unix(1_799_999_999, 0).UTC(), 10)
	if err != nil {
		t.Fatalf("ListCancelRequestedTaskRuns: %v", err)
	}
	for _, r := range early {
		if r.ID == runID {
			t.Error("a cancel request newer than the cutoff was swept")
		}
	}
	due, err := s.ListCancelRequestedTaskRuns(ctx, time.Unix(1_800_000_000, 0).UTC(), 10)
	if err != nil {
		t.Fatalf("ListCancelRequestedTaskRuns: %v", err)
	}
	if !containsRun(due, runID) {
		t.Error("a cancel request older than the cutoff was not swept")
	}

	// Once the run is terminal nothing may cancel it again, and the backstop
	// must stop seeing it.
	endedAt := time.Unix(1_800_000_100, 0).UTC()
	if updated, err := s.TransitionTaskRun(ctx, coretask.TransitionRunInput{
		TaskRunID:      runID,
		ExpectedStatus: coretask.RunStatusPending,
		NewStatus:      coretask.RunStatusCanceled,
		EndedAt:        &endedAt,
	}); err != nil || !updated {
		t.Fatalf("TransitionTaskRun to CANCELED: updated=%v err=%v", updated, err)
	}
	if got, err := s.RequestTaskRunCancel(ctx, runID, cancelTestUser, coretask.CancelReasonUserRequested, time.Unix(1_800_000_200, 0).UTC()); err != nil || got {
		t.Errorf("RequestTaskRunCancel on a finished run = %v, %v; want false, nil", got, err)
	}
	after, err := s.ListCancelRequestedTaskRuns(ctx, time.Unix(1_800_000_300, 0).UTC(), 10)
	if err != nil {
		t.Fatalf("ListCancelRequestedTaskRuns: %v", err)
	}
	if containsRun(after, runID) {
		t.Error("a finished run is still listed as awaiting its cancel")
	}
	active, err = s.GetActiveTaskRunByTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetActiveTaskRunByTask: %v", err)
	}
	if active != nil {
		t.Errorf("active run = %+v, want none once the run is canceled", active)
	}
}

func containsRun(runs []coretask.Run, taskRunID string) bool {
	for _, r := range runs {
		if r.ID == taskRunID {
			return true
		}
	}
	return false
}
