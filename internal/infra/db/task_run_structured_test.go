package db

import (
	"testing"
	"time"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

// TestTaskOutputSchemaAndRunStructuredRoundTrip pins the structured-output
// persistence chain: a task's output schema and a run's validated structured
// value survive a write and read.
func TestTaskOutputSchemaAndRunStructuredRoundTrip(t *testing.T) {
	s, ctx := newTestStore(t)
	userID := newTestUser(t, s, "structured")
	conversation, err := s.CreateConversation(ctx, userID, "portal", userID)
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	schema := `{"type":"object","additionalProperties":false,"properties":{"n":{"type":"integer"}},"required":["n"]}`
	task, err := s.CreateTask(ctx, &coretask.CreateInput{
		SpaceID:        conversation.SpaceID,
		ConversationID: conversation.ID,
		Input:          "input",
		CreatedBy:      userID,
		OutputSchema:   &schema,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.OutputSchema == nil || *task.OutputSchema != schema {
		t.Fatalf("task.OutputSchema = %v, want the schema", task.OutputSchema)
	}
	if task.LastRunID == nil {
		t.Fatal("CreateTask did not create its first run")
	}
	runID := *task.LastRunID

	transition := func(from, to coretask.RunStatus, fields func(*coretask.TransitionRunInput)) {
		t.Helper()
		in := coretask.TransitionRunInput{TaskRunID: runID, ExpectedStatus: from, NewStatus: to}
		if fields != nil {
			fields(&in)
		}
		if _, err := s.TransitionTaskRun(ctx, in); err != nil {
			t.Fatalf("TransitionTaskRun %s -> %s: %v", from, to, err)
		}
	}

	endedAt := time.Unix(1_800_000_000, 0).UTC()
	output := "the answer is 42"
	structured := `{"n":42}`
	transition(coretask.RunStatusPending, coretask.RunStatusScheduled, nil)
	transition(coretask.RunStatusScheduled, coretask.RunStatusRunning, nil)
	transition(coretask.RunStatusRunning, coretask.RunStatusSucceeded, func(in *coretask.TransitionRunInput) {
		in.EndedAt = &endedAt
		in.Output = &output
		in.Structured = &structured
	})

	run, err := s.GetTaskRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetTaskRun: %v", err)
	}
	if run.Structured == nil || *run.Structured != structured {
		t.Fatalf("run.Structured = %v, want %q", run.Structured, structured)
	}

	// A task without an output schema keeps the columns nil.
	plain, err := s.CreateTask(ctx, &coretask.CreateInput{
		SpaceID:        conversation.SpaceID,
		ConversationID: conversation.ID,
		Input:          "plain",
		CreatedBy:      userID,
	})
	if err != nil {
		t.Fatalf("CreateTask (plain): %v", err)
	}
	if plain.OutputSchema != nil {
		t.Errorf("plain task OutputSchema = %v, want nil", plain.OutputSchema)
	}
}
