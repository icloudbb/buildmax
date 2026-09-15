package task

import (
	"context"
	"errors"
	"testing"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/util"
)

// retryFixture builds a task whose last run finished in the given status.
func retryFixture(status string) (*Service, *mock.MockTaskRunStore) {
	previous := coretask.Run{
		ID:            "tr_1",
		TaskID:        "t_1",
		Input:         "review the migration plan",
		Status:        status,
		TriggerSource: coretask.RunTriggerSourcePortalTaskRerun,
	}
	runs := &mock.MockTaskRunStore{Runs: []coretask.Run{previous}}
	tasks := &mock.MockTaskStore{List: []coretask.Task{{
		ID:        "t_1",
		SpaceID:   "tm_1",
		Status:    status,
		Input:     "the task's original input",
		LastRunID: util.Ptr("tr_1"),
	}}}
	return &Service{Tasks: tasks, TaskRuns: runs}, runs
}

// The retry repeats what the last run was asked to do, not what the task was
// first created with: a task's later runs can carry follow-up instructions.
func TestRetryRunRepeatsTheLastRunsInput(t *testing.T) {
	svc, runs := retryFixture(string(coretask.RunStatusFailed))

	result, err := svc.RetryRun(context.Background(), RetryRunCmd{UserID: "u1", TaskID: "t_1"})
	if err != nil {
		t.Fatalf("RetryRun: %v", err)
	}
	if result.RetriedRun.ID != "tr_1" {
		t.Errorf("retried run = %q, want tr_1", result.RetriedRun.ID)
	}
	if result.Run.Input != "review the migration plan" {
		t.Errorf("input = %q, want the previous run's input", result.Run.Input)
	}
	if result.Run.TriggerSource != coretask.RunTriggerSourceTaskRetry {
		t.Errorf("trigger_source = %q, want %q", result.Run.TriggerSource, coretask.RunTriggerSourceTaskRetry)
	}
	if result.Run.RetryOfTaskRunID == nil || *result.Run.RetryOfTaskRunID != "tr_1" {
		t.Errorf("retry_of_task_run_id = %v, want tr_1", result.Run.RetryOfTaskRunID)
	}
	if result.Run.CreatedBy != "u1" {
		t.Errorf("created_by = %q, want the user who asked", result.Run.CreatedBy)
	}
	if len(runs.Runs) != 2 {
		t.Fatalf("run count = %d, want 2", len(runs.Runs))
	}
}

// A canceled run is retryable for the same reason a failed one is: it stopped
// without producing the answer someone wanted, and nothing about it says the
// same work cannot be asked for again.
func TestRetryRunAcceptsACanceledRun(t *testing.T) {
	svc, _ := retryFixture(string(coretask.RunStatusCanceled))

	if _, err := svc.RetryRun(context.Background(), RetryRunCmd{UserID: "u1", TaskID: "t_1"}); err != nil {
		t.Fatalf("RetryRun on a canceled run: %v", err)
	}
}

// Retrying a succeeded run is running the same work again, which is a normal
// thing to want and is not the service's business to refuse.
func TestRetryRunAcceptsASucceededRun(t *testing.T) {
	svc, _ := retryFixture(string(coretask.RunStatusSucceeded))

	if _, err := svc.RetryRun(context.Background(), RetryRunCmd{UserID: "u1", TaskID: "t_1"}); err != nil {
		t.Fatalf("RetryRun on a succeeded run: %v", err)
	}
}

// While a run is in flight, last_run_id names that active run. Reporting
// "nothing to retry" would hide the real reason: the current attempt is still
// going and a Task cannot admit another one yet.
func TestRetryRunRefusesWhileARunIsInFlight(t *testing.T) {
	svc, runs := retryFixture(string(coretask.RunStatusSucceeded))
	runs.Runs = append(runs.Runs, coretask.Run{
		ID: "tr_2", TaskID: "t_1", Input: "follow-up", Status: string(coretask.RunStatusRunning),
	})

	_, err := svc.RetryRun(context.Background(), RetryRunCmd{UserID: "u1", TaskID: "t_1"})
	if !errors.Is(err, coretask.ErrRunInProgress) {
		t.Fatalf("err = %v, want ErrRunInProgress", err)
	}
	if len(runs.Runs) != 2 {
		t.Errorf("run count = %d, want the in-flight run left alone", len(runs.Runs))
	}
}

// A task that has never run has nothing to repeat.
func TestRetryRunRefusesATaskThatNeverRan(t *testing.T) {
	svc := &Service{
		Tasks:    &mock.MockTaskStore{List: []coretask.Task{{ID: "t_1", SpaceID: "tm_1", Status: "PENDING"}}},
		TaskRuns: &mock.MockTaskRunStore{},
	}

	if _, err := svc.RetryRun(context.Background(), RetryRunCmd{UserID: "u1", TaskID: "t_1"}); !errors.Is(err, ErrNoRunToRetry) {
		t.Fatalf("err = %v, want ErrNoRunToRetry", err)
	}
}

// The workflow service reacts to a step task's terminal outcome by advancing or
// failing the workflow run. A retry started outside it would report a second
// outcome for a step that is already settled, marking it succeeded and
// dispatching the next step of a workflow run that has already ended.
func TestRetryRunRefusesAWorkflowStepTask(t *testing.T) {
	svc, runs := retryFixture(string(coretask.RunStatusFailed))
	svc.WorkflowSteps = &mock.MockWorkflowStore{NodeRuns: []coreworkflow.NodeRun{{
		ID:            "wsr_1",
		WorkflowRunID: "wr_1",
		TaskID:        util.Ptr("t_1"),
		Status:        string(coreworkflow.NodeRunStatusFailed),
	}}}

	_, err := svc.RetryRun(context.Background(), RetryRunCmd{UserID: "u1", TaskID: "t_1"})
	if !errors.Is(err, ErrRetryOfWorkflowStep) {
		t.Fatalf("err = %v, want ErrRetryOfWorkflowStep", err)
	}
	if len(runs.Runs) != 1 {
		t.Errorf("run count = %d, want no run created", len(runs.Runs))
	}
}

// A deployment with no workflow store has no workflow steps, so the absent
// lookup is an answered question, not an unanswered one.
func TestRetryRunWithoutAWorkflowStoreStillRetries(t *testing.T) {
	svc, _ := retryFixture(string(coretask.RunStatusFailed))
	svc.WorkflowSteps = nil

	if _, err := svc.RetryRun(context.Background(), RetryRunCmd{UserID: "u1", TaskID: "t_1"}); err != nil {
		t.Fatalf("RetryRun: %v", err)
	}
}

func TestRetryRunRefusesAnUnknownTask(t *testing.T) {
	svc, _ := retryFixture(string(coretask.RunStatusFailed))

	if _, err := svc.RetryRun(context.Background(), RetryRunCmd{UserID: "u1", TaskID: "t_missing"}); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("err = %v, want ErrTaskNotFound", err)
	}
}
