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

// TestEligibilityCancelPersistsReasonWithoutRequester covers the reconciler's
// store surface: an active run is listed with its Space and initiator, a cancel
// with no requester records the reason, and the run then drops out of the scan.
func TestEligibilityCancelPersistsReasonWithoutRequester(t *testing.T) {
	s, ctx := newTestStore(t)
	user := newTestUser(t, s, "elig-store")

	conv, err := s.CreateConversation(ctx, user, "portal", user)
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	task, err := s.CreateTask(ctx, &coretask.CreateInput{
		SpaceID:        conv.SpaceID,
		ConversationID: conv.ID,
		Input:          "input",
		CreatedBy:      user,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() {
		_ = s.db.WithContext(ctx).Delete(&taskRunRow{}, "task_id = ?", task.ID)
		_ = s.db.WithContext(ctx).Delete(&taskRow{}, "task_id = ?", task.ID)
		_ = s.db.WithContext(ctx).Delete(&conversationRow{}, "conversation_id = ?", conv.ID)
	})
	runID := *task.LastRunID

	refs, err := s.ListActiveTaskRunsForEligibility(ctx, "", 100)
	if err != nil {
		t.Fatalf("ListActiveTaskRunsForEligibility: %v", err)
	}
	var found *coretask.ActiveRunRef
	for i := range refs {
		if refs[i].TaskRunID == runID {
			found = &refs[i]
		}
	}
	if found == nil {
		t.Fatal("the active run was not listed for eligibility")
	}
	if found.SpaceID != conv.SpaceID || found.CreatedBy != user || found.Status != string(coretask.RunStatusPending) {
		t.Errorf("ref = %+v, want space %q initiator %q status PENDING", *found, conv.SpaceID, user)
	}

	// The per-creator scan a deactivation uses finds the same run.
	byCreator, err := s.ListActiveTaskRunsByCreator(ctx, user)
	if err != nil {
		t.Fatalf("ListActiveTaskRunsByCreator: %v", err)
	}
	if !containsRef(byCreator, runID) {
		t.Error("the run was not listed for its creator")
	}

	// A reconciler-driven cancel: no requester, a recorded reason.
	requested, err := s.RequestTaskRunCancel(ctx, runID, "", coretask.CancelReasonCreatorDisabled, time.Unix(1_800_000_000, 0).UTC())
	if err != nil || !requested {
		t.Fatalf("RequestTaskRunCancel: requested=%v err=%v", requested, err)
	}
	stored, err := s.GetTaskRun(ctx, runID)
	if err != nil || stored == nil {
		t.Fatalf("GetTaskRun: %v", err)
	}
	if stored.CancelReason != coretask.CancelReasonCreatorDisabled {
		t.Errorf("cancel_reason = %q, want creator_disabled", stored.CancelReason)
	}
	if stored.CancelRequestedBy != nil {
		t.Errorf("cancel_requested_by = %v, want nil for a reconciler cancel", stored.CancelRequestedBy)
	}

	// A requested run drops out of the eligibility scan, so repeated sweeps are
	// idempotent rather than piling up requests.
	refs, err = s.ListActiveTaskRunsForEligibility(ctx, "", 100)
	if err != nil {
		t.Fatalf("ListActiveTaskRunsForEligibility after cancel: %v", err)
	}
	for i := range refs {
		if refs[i].TaskRunID == runID {
			t.Error("a run already asked to stop is still scanned")
		}
	}

	// The reason survives the terminal transition the backstop applies.
	endedAt := time.Unix(1_800_000_100, 0).UTC()
	if _, err := s.TransitionTaskRun(ctx, coretask.TransitionRunInput{
		TaskRunID:      runID,
		ExpectedStatus: coretask.RunStatusPending,
		NewStatus:      coretask.RunStatusCanceled,
		EndedAt:        &endedAt,
	}); err != nil {
		t.Fatalf("TransitionTaskRun to CANCELED: %v", err)
	}
	stored, err = s.GetTaskRun(ctx, runID)
	if err != nil || stored == nil {
		t.Fatalf("GetTaskRun after cancel: %v", err)
	}
	if stored.CancelReason != coretask.CancelReasonCreatorDisabled {
		t.Errorf("cancel_reason after settling = %q, want creator_disabled preserved", stored.CancelReason)
	}
}

func containsRef(refs []coretask.ActiveRunRef, taskRunID string) bool {
	for _, r := range refs {
		if r.TaskRunID == taskRunID {
			return true
		}
	}
	return false
}

func containsRun(runs []coretask.Run, taskRunID string) bool {
	for _, r := range runs {
		if r.ID == taskRunID {
			return true
		}
	}
	return false
}
