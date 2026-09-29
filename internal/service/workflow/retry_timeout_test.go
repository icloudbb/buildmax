package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/util"
)

// nodeRun returns the stored node run for nodeID.
func nodeRun(t *testing.T, store *mock.MockWorkflowStore, runID, nodeID string) *coreworkflow.NodeRun {
	t.Helper()
	for i := range store.NodeRuns {
		if store.NodeRuns[i].WorkflowRunID == runID && store.NodeRuns[i].NodeID == nodeID {
			return &store.NodeRuns[i]
		}
	}
	t.Fatalf("node %q not found", nodeID)
	return nil
}

// endAttempt makes the node's current attempt's TaskRun terminal in place (a
// retry attempt already has a PENDING row) and reconciles the run.
func endAttempt(t *testing.T, svc *Service, store *mock.MockWorkflowStore, taskRuns *mock.MockTaskRunStore, runID, nodeID, status string) {
	t.Helper()
	node := nodeRun(t, store, runID, nodeID)
	if node.TaskRunID == nil {
		t.Fatalf("node %q has no attempt to end (status %q)", nodeID, node.Status)
	}
	out := nodeID + " out"
	updated := false
	for i := range taskRuns.Runs {
		if taskRuns.Runs[i].ID == *node.TaskRunID {
			taskRuns.Runs[i].Status = status
			taskRuns.Runs[i].Output = &out
			updated = true
		}
	}
	if !updated {
		taskRuns.Runs = append(taskRuns.Runs, coretask.Run{ID: *node.TaskRunID, TaskID: *node.TaskID, Status: status, Output: &out, Input: "attempt input"})
	}
	if err := svc.Reconcile(context.Background(), runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
}

// makeRetryDue moves a retry_wait node's backoff into the past and reconciles.
func makeRetryDue(t *testing.T, svc *Service, store *mock.MockWorkflowStore, runID, nodeID string) {
	t.Helper()
	node := nodeRun(t, store, runID, nodeID)
	node.NextAttemptAt = util.Ptr(time.Now().Add(-time.Second))
	if err := svc.Reconcile(context.Background(), runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
}

func runStatus(t *testing.T, store *mock.MockWorkflowStore, runID string) *coreworkflow.Run {
	t.Helper()
	run, err := store.GetWorkflowRun(context.Background(), runID)
	if err != nil || run == nil {
		t.Fatalf("GetWorkflowRun: %v", err)
	}
	return run
}

func TestReconcile_RetriesFailedAttemptOnTheSameTask(t *testing.T) {
	svc, store, taskRuns, runID := concurrencySvc(t, `{"schema_version":1,"nodes":[`+
		`{"id":"a","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"a"},"policy":{"max_attempts":2}}]}`)
	first := *nodeRun(t, store, runID, "a")
	if first.Attempt != 1 || first.MaxAttempts != 2 {
		t.Fatalf("first attempt = %d of %d, want 1 of 2", first.Attempt, first.MaxAttempts)
	}

	endAttempt(t, svc, store, taskRuns, runID, "a", string(coretask.RunStatusFailed))
	node := nodeRun(t, store, runID, "a")
	if node.Status != string(coreworkflow.NodeRunStatusRetryWait) || node.NextAttemptAt == nil {
		t.Fatalf("after a failed attempt node = %s next=%v, want retry_wait with a next attempt time", node.Status, node.NextAttemptAt)
	}
	run := runStatus(t, store, runID)
	if run.Status != string(coreworkflow.RunStatusRunning) {
		t.Fatalf("run = %s, want running while the node waits to retry", run.Status)
	}
	if run.NextReconcileAt == nil || !run.NextReconcileAt.Equal(*node.NextAttemptAt) {
		t.Fatalf("run wakes at %v, want the node's next attempt %v", run.NextReconcileAt, node.NextAttemptAt)
	}

	// Before the backoff ends a pass admits nothing.
	if err := svc.Reconcile(context.Background(), runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got := nodeRun(t, store, runID, "a").Status; got != string(coreworkflow.NodeRunStatusRetryWait) {
		t.Fatalf("node = %s before its backoff ended, want retry_wait", got)
	}

	makeRetryDue(t, svc, store, runID, "a")
	node = nodeRun(t, store, runID, "a")
	if node.Status != string(coreworkflow.NodeRunStatusRunning) || node.Attempt != 2 {
		t.Fatalf("after the backoff node = %s attempt %d, want running attempt 2", node.Status, node.Attempt)
	}
	if *node.TaskID != *first.TaskID || *node.TaskRunID == *first.TaskRunID {
		t.Fatalf("retry task=%s run=%s, want the same Task %s and a new run", *node.TaskID, *node.TaskRunID, *first.TaskID)
	}
	retry, _ := taskRuns.GetTaskRun(context.Background(), *node.TaskRunID)
	if retry.RetryOfTaskRunID == nil || *retry.RetryOfTaskRunID != *first.TaskRunID {
		t.Fatalf("retry_of = %v, want %s", retry.RetryOfTaskRunID, *first.TaskRunID)
	}
	if retry.Input != "attempt input" || retry.TriggerSource != coretask.RunTriggerSourceWorkflowStep {
		t.Fatalf("retry input=%q trigger=%q, want the repeated input and workflow_step", retry.Input, retry.TriggerSource)
	}
	if retry.IdempotencyKey == nil || *retry.IdempotencyKey != coreworkflow.TaskRunAdmissionKey(runID, "a", 2) {
		t.Fatalf("retry key = %v", retry.IdempotencyKey)
	}
	if node.ErrorMessage != nil {
		t.Fatalf("a running attempt kept the previous attempt's error %q", *node.ErrorMessage)
	}

	endAttempt(t, svc, store, taskRuns, runID, "a", string(coretask.RunStatusSucceeded))
	if run := runStatus(t, store, runID); run.Status != string(coreworkflow.RunStatusSucceeded) {
		t.Fatalf("run = %s after the retry succeeded, want succeeded", run.Status)
	}
}

func TestReconcile_ExhaustedAttemptsFailTheRun(t *testing.T) {
	svc, store, taskRuns, runID := concurrencySvc(t, `{"schema_version":1,"nodes":[`+
		`{"id":"a","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"a"},"policy":{"max_attempts":2}},`+
		`{"id":"b","type":"agent_task","needs":["a"],"agent":{"id":"a_1"},"input":{"instruction":"b"}}]}`)
	endAttempt(t, svc, store, taskRuns, runID, "a", string(coretask.RunStatusFailed))
	makeRetryDue(t, svc, store, runID, "a")
	endAttempt(t, svc, store, taskRuns, runID, "a", string(coretask.RunStatusFailed))

	if got := nodeRun(t, store, runID, "a").Status; got != string(coreworkflow.NodeRunStatusFailed) {
		t.Fatalf("node a = %s after its last attempt failed, want failed", got)
	}
	if got := nodeRun(t, store, runID, "b").Status; got != string(coreworkflow.NodeRunStatusBlocked) {
		t.Fatalf("node b = %s, want blocked", got)
	}
	if run := runStatus(t, store, runID); run.Status != string(coreworkflow.RunStatusFailed) {
		t.Fatalf("run = %s, want failed", run.Status)
	}
}

func TestReconcile_CanceledAttemptIsNotRetried(t *testing.T) {
	svc, store, taskRuns, runID := concurrencySvc(t, `{"schema_version":1,"nodes":[`+
		`{"id":"a","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"a"},"policy":{"max_attempts":3}}]}`)
	endAttempt(t, svc, store, taskRuns, runID, "a", string(coretask.RunStatusCanceled))
	if got := nodeRun(t, store, runID, "a").Status; got != string(coreworkflow.NodeRunStatusCanceled) {
		t.Fatalf("node = %s, want canceled: a person stopping the Task is not a failure to retry", got)
	}
	if run := runStatus(t, store, runID); run.Status != string(coreworkflow.RunStatusCanceled) {
		t.Fatalf("run = %s, want canceled", run.Status)
	}
}

func TestReconcile_NodeTimeoutCancelsTheAttemptAndRetries(t *testing.T) {
	svc, store, taskRuns, runID := concurrencySvc(t, `{"schema_version":1,"nodes":[`+
		`{"id":"a","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"a"},"policy":{"timeout_seconds":600,"max_attempts":2}}]}`)
	node := nodeRun(t, store, runID, "a")
	if node.DeadlineAt == nil || node.DeadlineAt.Before(time.Now().Add(590*time.Second)) {
		t.Fatalf("first attempt deadline = %v, want about ten minutes out", node.DeadlineAt)
	}
	taskRuns.Runs = append(taskRuns.Runs, coretask.Run{ID: *node.TaskRunID, TaskID: *node.TaskID, Status: string(coretask.RunStatusRunning), Input: "attempt input"})
	node.DeadlineAt = util.Ptr(time.Now().Add(-time.Second))

	if err := svc.Reconcile(context.Background(), runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	attempt, _ := taskRuns.GetTaskRun(context.Background(), *node.TaskRunID)
	if attempt.CancelRequestedAt == nil || attempt.CancelReason != coretask.CancelReasonWorkflowNodeTimeout {
		t.Fatalf("timed-out attempt cancel=%v reason=%q, want a timeout cancel request", attempt.CancelRequestedAt, attempt.CancelReason)
	}
	if got := nodeRun(t, store, runID, "a").Status; got != string(coreworkflow.NodeRunStatusRunning) {
		t.Fatalf("node = %s while its worker has not stopped, want running", got)
	}

	// The worker honors the cancel; the Workflow counts it as a failed attempt.
	endAttempt(t, svc, store, taskRuns, runID, "a", string(coretask.RunStatusCanceled))
	node = nodeRun(t, store, runID, "a")
	if node.Status != string(coreworkflow.NodeRunStatusRetryWait) {
		t.Fatalf("node = %s after a timed-out attempt, want retry_wait", node.Status)
	}
	if node.ErrorMessage == nil || !strings.Contains(*node.ErrorMessage, "timeout") {
		t.Fatalf("node error = %v, want the timeout named", node.ErrorMessage)
	}
	if node.DeadlineAt != nil {
		t.Fatalf("a waiting node kept its attempt deadline %v", node.DeadlineAt)
	}
}

func TestReconcile_TimeoutOnLastAttemptFailsTheRun(t *testing.T) {
	svc, store, taskRuns, runID := concurrencySvc(t, `{"schema_version":1,"nodes":[`+
		`{"id":"a","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"a"},"policy":{"timeout_seconds":60}}]}`)
	node := nodeRun(t, store, runID, "a")
	taskRuns.Runs = append(taskRuns.Runs, coretask.Run{ID: *node.TaskRunID, TaskID: *node.TaskID, Status: string(coretask.RunStatusRunning)})
	node.DeadlineAt = util.Ptr(time.Now().Add(-time.Second))
	if err := svc.Reconcile(context.Background(), runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	endAttempt(t, svc, store, taskRuns, runID, "a", string(coretask.RunStatusCanceled))
	if got := nodeRun(t, store, runID, "a").Status; got != string(coreworkflow.NodeRunStatusFailed) {
		t.Fatalf("node = %s, want failed: a timeout is a failure, not a cancellation", got)
	}
	if run := runStatus(t, store, runID); run.Status != string(coreworkflow.RunStatusFailed) {
		t.Fatalf("run = %s, want failed", run.Status)
	}
}

func TestReconcile_RunDeadlineStopsAndFailsTheRun(t *testing.T) {
	svc, store, taskRuns, runID := concurrencySvc(t, `{"schema_version":1,"policy":{"timeout_seconds":3600},"nodes":[`+
		`{"id":"a","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"a"}},`+
		`{"id":"b","type":"agent_task","needs":["a"],"agent":{"id":"a_1"},"input":{"instruction":"b"}}]}`)
	run := runStatus(t, store, runID)
	if run.DeadlineAt == nil {
		t.Fatal("run has no deadline from its policy timeout")
	}
	node := nodeRun(t, store, runID, "a")
	taskRuns.Runs = append(taskRuns.Runs, coretask.Run{ID: *node.TaskRunID, TaskID: *node.TaskID, Status: string(coretask.RunStatusRunning)})
	run.DeadlineAt = util.Ptr(time.Now().Add(-time.Second))

	if err := svc.Reconcile(context.Background(), runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	run = runStatus(t, store, runID)
	if run.Status != string(coreworkflow.RunStatusFailing) || run.ErrorMessage == nil || !strings.Contains(*run.ErrorMessage, "timeout") {
		t.Fatalf("run = %s error=%v, want failing with the timeout named", run.Status, run.ErrorMessage)
	}
	if got := nodeRun(t, store, runID, "b").Status; got != string(coreworkflow.NodeRunStatusBlocked) {
		t.Fatalf("node b = %s, want blocked", got)
	}
	attempt, _ := taskRuns.GetTaskRun(context.Background(), *node.TaskRunID)
	if attempt.CancelReason != coretask.CancelReasonWorkflowStopped {
		t.Fatalf("active attempt cancel reason = %q, want workflow_stopped", attempt.CancelReason)
	}
	endAttempt(t, svc, store, taskRuns, runID, "a", string(coretask.RunStatusCanceled))
	if run := runStatus(t, store, runID); run.Status != string(coreworkflow.RunStatusFailed) {
		t.Fatalf("drained run = %s, want failed", run.Status)
	}
}

func TestReconcile_SiblingFailureCancelsAWaitingRetry(t *testing.T) {
	svc, store, taskRuns, runID := concurrencySvc(t, `{"schema_version":1,"nodes":[`+
		`{"id":"a","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"a"},"policy":{"max_attempts":2}},`+
		`{"id":"b","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"b"}}]}`)
	endAttempt(t, svc, store, taskRuns, runID, "a", string(coretask.RunStatusFailed))
	endAttempt(t, svc, store, taskRuns, runID, "b", string(coretask.RunStatusFailed))
	if got := nodeRun(t, store, runID, "a").Status; got != string(coreworkflow.NodeRunStatusCanceled) {
		t.Fatalf("waiting node = %s after a sibling failed, want canceled", got)
	}
	if run := runStatus(t, store, runID); run.Status != string(coreworkflow.RunStatusFailed) {
		t.Fatalf("run = %s, want failed", run.Status)
	}
}

func TestReconcile_RetryAdmissionFailureFailsTheNode(t *testing.T) {
	svc, store, taskRuns, runID := concurrencySvc(t, `{"schema_version":1,"nodes":[`+
		`{"id":"a","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"a"},"policy":{"max_attempts":2}}]}`)
	endAttempt(t, svc, store, taskRuns, runID, "a", string(coretask.RunStatusFailed))
	svc.TaskService.QuotaChecker = refuseQuota{}
	node := nodeRun(t, store, runID, "a")
	node.NextAttemptAt = util.Ptr(time.Now().Add(-time.Second))
	if err := svc.Reconcile(context.Background(), runID); err == nil {
		t.Fatal("Reconcile succeeded though the retry could not be admitted")
	}
	node = nodeRun(t, store, runID, "a")
	if node.Status != string(coreworkflow.NodeRunStatusFailed) || node.ErrorMessage == nil || !strings.Contains(*node.ErrorMessage, "attempt 2") {
		t.Fatalf("node = %s error=%v, want failed naming attempt 2", node.Status, node.ErrorMessage)
	}
}

type refuseQuota struct{}

func (refuseQuota) Check(context.Context, string, int, int) (bool, string, error) {
	return false, "quota exhausted", nil
}

func TestParseDefinition_RetryAndTimeoutPolicyBounds(t *testing.T) {
	svc := &Service{
		Workflows: &mock.MockWorkflowStore{},
		Agents:    &mock.MockAgentStore{Agents: []agentdef.Agent{{ID: "a_1", SpaceID: "tm_1", Name: "A"}}},
	}
	node := func(policy string) string {
		return `{"schema_version":1,"nodes":[{"id":"a","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"a"},"policy":` + policy + `}]}`
	}
	cases := []struct {
		name, definition string
		want             error
	}{
		{"valid node policy", node(`{"max_attempts":5,"timeout_seconds":60}`), nil},
		{"too many attempts", node(`{"max_attempts":6}`), ErrInvalidNodePolicy},
		{"negative attempts", node(`{"max_attempts":-1}`), ErrInvalidNodePolicy},
		{"node timeout below the floor", node(`{"timeout_seconds":30}`), ErrInvalidNodePolicy},
		{"node timeout above the ceiling", node(`{"timeout_seconds":2592001}`), ErrInvalidNodePolicy},
		{"run timeout below the floor", `{"schema_version":1,"policy":{"timeout_seconds":10},"nodes":[{"id":"a","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"a"}}]}`, ErrInvalidPolicy},
		{"valid run timeout", `{"schema_version":1,"policy":{"timeout_seconds":86400},"nodes":[{"id":"a","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"a"}}]}`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CreateWorkflow(context.Background(), CreateWorkflowCmd{SpaceID: "tm_1", UserID: "u1", Name: "WF", Definition: tc.definition})
			if tc.want == nil && err != nil {
				t.Fatalf("CreateWorkflow: %v", err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("CreateWorkflow err = %v, want %v", err, tc.want)
			}
		})
	}
}
