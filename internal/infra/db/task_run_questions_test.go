package db

import (
	"testing"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

// TestRunQuestionsAndTaskAwaitingAnswer pins the AskUser persistence chain: a
// run that ends on questions stores them, its Task projects that it is waiting
// for the answer, and the Continue run that answers them clears the wait.
func TestRunQuestionsAndTaskAwaitingAnswer(t *testing.T) {
	s, ctx := newTestStore(t)
	userID := newTestUser(t, s, "questions")
	conversation, err := s.CreateConversation(ctx, userID, "portal", userID)
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	task, err := s.CreateTask(ctx, &coretask.CreateInput{
		SpaceID: conversation.SpaceID, ConversationID: conversation.ID, Input: "scaffold a service", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	finish := func(runID string, questions *string) {
		t.Helper()
		for _, step := range [][2]coretask.RunStatus{
			{coretask.RunStatusPending, coretask.RunStatusScheduled},
			{coretask.RunStatusScheduled, coretask.RunStatusRunning},
		} {
			if _, err := s.TransitionTaskRun(ctx, coretask.TransitionRunInput{TaskRunID: runID, ExpectedStatus: step[0], NewStatus: step[1]}); err != nil {
				t.Fatalf("TransitionTaskRun: %v", err)
			}
		}
		output := "done"
		if _, err := s.TransitionTaskRun(ctx, coretask.TransitionRunInput{
			TaskRunID: runID, ExpectedStatus: coretask.RunStatusRunning, NewStatus: coretask.RunStatusSucceeded,
			Output: &output, Questions: questions,
		}); err != nil {
			t.Fatalf("TransitionTaskRun to SUCCEEDED: %v", err)
		}
	}

	questions := `[{"question":"Which database?","options":[{"label":"Postgres"}]}]`
	finish(*task.LastRunID, &questions)

	run, err := s.GetTaskRun(ctx, *task.LastRunID)
	if err != nil {
		t.Fatalf("GetTaskRun: %v", err)
	}
	if string(run.Questions) != questions {
		t.Fatalf("run.Questions = %s, want %s", run.Questions, questions)
	}
	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if !got.AwaitingAnswer {
		t.Fatal("a task whose run ended on questions is not awaiting an answer")
	}

	// Continuing the Task is the answer: the wait clears as the run is created,
	// and a run that asks nothing leaves it clear.
	next, err := s.CreateTaskRun(ctx, coretask.CreateRunInput{TaskID: task.ID, Input: "Postgres", CreatedBy: userID})
	if err != nil {
		t.Fatalf("CreateTaskRun: %v", err)
	}
	if got, _ = s.GetTask(ctx, task.ID); got.AwaitingAnswer {
		t.Fatal("continuing the task did not clear the wait")
	}
	finish(next.ID, nil)
	if got, _ = s.GetTask(ctx, task.ID); got.AwaitingAnswer {
		t.Fatal("a run that asked nothing left the task awaiting an answer")
	}
	if run, _ = s.GetTaskRun(ctx, next.ID); run.Questions != nil {
		t.Fatalf("a run that asked nothing stored questions %s", run.Questions)
	}
}
