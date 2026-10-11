package db

import (
	"testing"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

func TestTransitionTaskRunRecordsAFailureClassOnlyOnFailure(t *testing.T) {
	s, ctx := newTestStore(t)
	userID := newTestUser(t, s, "failure-class")
	conversation, err := s.CreateConversation(ctx, userID, "portal", userID)
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	newRun := func() string {
		t.Helper()
		task, err := s.CreateTask(ctx, &coretask.CreateInput{
			SpaceID: conversation.SpaceID, ConversationID: conversation.ID, Input: "input", CreatedBy: userID,
		})
		if err != nil {
			t.Fatalf("CreateTask: %v", err)
		}
		startTaskRunForTest(t, s, ctx, *task.LastRunID)
		return *task.LastRunID
	}
	finish := func(runID string, status coretask.RunStatus, class coretask.FailureClass) *coretask.Run {
		t.Helper()
		if ok, err := s.TransitionTaskRun(ctx, coretask.TransitionRunInput{
			TaskRunID: runID, ExpectedStatus: coretask.RunStatusRunning, NewStatus: status, FailureClass: class,
		}); err != nil || !ok {
			t.Fatalf("TransitionTaskRun: ok=%v err=%v", ok, err)
		}
		run, err := s.GetTaskRun(ctx, runID)
		if err != nil {
			t.Fatalf("GetTaskRun: %v", err)
		}
		return run
	}

	if run := finish(newRun(), coretask.RunStatusFailed, coretask.FailureWorkerLost); run.FailureClass != string(coretask.FailureWorkerLost) {
		t.Errorf("named class = %q, want worker_lost", run.FailureClass)
	}
	if run := finish(newRun(), coretask.RunStatusFailed, "made_up"); run.FailureClass != string(coretask.FailureUnclassified) {
		t.Errorf("unknown class = %q, want unclassified", run.FailureClass)
	}
	if run := finish(newRun(), coretask.RunStatusFailed, ""); run.FailureClass != string(coretask.FailureUnclassified) {
		t.Errorf("missing class = %q, want unclassified", run.FailureClass)
	}
	run := finish(newRun(), coretask.RunStatusSucceeded, coretask.FailureModel)
	if run.FailureClass != "" {
		t.Errorf("a success recorded class %q, want none", run.FailureClass)
	}
	// Server logs name the Space from the run read, without a second query.
	if run.SpaceID != conversation.SpaceID {
		t.Errorf("run space = %q, want %q", run.SpaceID, conversation.SpaceID)
	}
}

// The cause a refusal recorded reads back on the run and, through the last-run
// join, on its Task; the first cause recorded is the one kept.
func TestRecordTaskRunFailureCauseReadsBackOnTheRunAndTask(t *testing.T) {
	s, ctx := newTestStore(t)
	userID := newTestUser(t, s, "failure-cause")
	conversation, err := s.CreateConversation(ctx, userID, "portal", userID)
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	task, err := s.CreateTask(ctx, &coretask.CreateInput{
		SpaceID: conversation.SpaceID, ConversationID: conversation.ID, Input: "input", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	runID := *task.LastRunID
	startTaskRunForTest(t, s, ctx, runID)
	first := coretask.FailureCause{Kind: coretask.FailureCauseSecretGrant, SecretID: "sec_first", SecretProblem: coretask.SecretDisabled}
	if err := s.RecordTaskRunFailureCause(ctx, runID, first); err != nil {
		t.Fatalf("RecordTaskRunFailureCause: %v", err)
	}
	if err := s.RecordTaskRunFailureCause(ctx, runID, coretask.FailureCause{Kind: coretask.FailureCauseSecretGrant, SecretID: "sec_second"}); err != nil {
		t.Fatalf("second RecordTaskRunFailureCause: %v", err)
	}
	if ok, err := s.TransitionTaskRun(ctx, coretask.TransitionRunInput{
		TaskRunID: runID, ExpectedStatus: coretask.RunStatusRunning, NewStatus: coretask.RunStatusFailed,
		FailureClass: coretask.FailureSpaceConfiguration,
	}); err != nil || !ok {
		t.Fatalf("TransitionTaskRun: ok=%v err=%v", ok, err)
	}

	run, err := s.GetTaskRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetTaskRun: %v", err)
	}
	if run.FailureCause == nil || *run.FailureCause != first {
		t.Errorf("run cause = %+v, want %+v", run.FailureCause, first)
	}
	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.FailureClass != string(coretask.FailureSpaceConfiguration) {
		t.Errorf("task class = %q, want space_configuration", got.FailureClass)
	}
	if got.FailureCause == nil || *got.FailureCause != first {
		t.Errorf("task cause = %+v, want %+v", got.FailureCause, first)
	}
}
