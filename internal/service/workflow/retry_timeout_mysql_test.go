package workflow

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/util"
)

func (e *reconcileEnv) setDefinition(t *testing.T, collectPolicy, runPolicy string) {
	t.Helper()
	definition := fmt.Sprintf(`{"schema_version":1,%s"nodes":[
		{"id":"collect","type":"agent_task","agent":{"id":%q},"input":{"instruction":"collect data"},"policy":%s},
		{"id":"summarize","type":"agent_task","needs":["collect"],"agent":{"id":%q},"input":{"instruction":"summarize"}}]}`,
		runPolicy, e.agentA, collectPolicy, e.agentB)
	if _, err := e.svc.UpdateWorkflow(context.Background(), UpdateWorkflowCmd{SpaceID: e.spaceID, UserID: e.userID, WorkflowID: e.wfID, Definition: &definition}); err != nil {
		t.Fatalf("UpdateWorkflow: %v", err)
	}
}

func (e *reconcileEnv) reconcileAt(t *testing.T, runID string, offset time.Duration) {
	t.Helper()
	e.svc.now = func() time.Time { return time.Now().Add(offset) }
	if err := e.svc.Reconcile(context.Background(), runID); err != nil {
		t.Fatalf("Reconcile at +%s: %v", offset, err)
	}
}

// A failed attempt waits out its backoff durably, the next attempt runs on the
// same Task through the real admission path, and the run deadline then stops
// the rest of the graph.
func TestWorkflowRetryThenRunDeadlineThroughTheStore(t *testing.T) {
	e := newReconcileEnv(t)
	ctx := context.Background()
	e.setDefinition(t, `{"max_attempts":2,"timeout_seconds":600}`, `"policy":{"timeout_seconds":3600},`)
	run, nodes := e.startRun(t)
	if run.DeadlineAt == nil {
		t.Fatal("run has no deadline")
	}
	first := nodes[0]
	e.driveTaskRunTerminal(t, first, coretask.RunStatusFailed, nil, util.Ptr("provider unavailable"))
	e.reconcileAt(t, run.ID, 0)

	collect := e.steps(t, run.ID)[0]
	if collect.Status != string(coreworkflow.NodeRunStatusRetryWait) || collect.NextAttemptAt == nil ||
		collect.ErrorMessage == nil || *collect.ErrorMessage != "provider unavailable" {
		t.Fatalf("after a failed attempt: %+v", collect)
	}
	if got := e.run(t, run.ID).NextReconcileAt; got == nil || got.Sub(*collect.NextAttemptAt).Abs() > time.Millisecond {
		t.Fatalf("run wakes at %v, want the retry time %v", got, collect.NextAttemptAt)
	}

	e.reconcileAt(t, run.ID, 5*time.Minute)
	collect = e.steps(t, run.ID)[0]
	if collect.Status != "running" || collect.Attempt != 2 || *collect.TaskID != *first.TaskID || *collect.TaskRunID == *first.TaskRunID || collect.DeadlineAt == nil {
		t.Fatalf("second attempt not admitted on the node's Task: %+v", collect)
	}
	retry, err := e.store.GetTaskRun(ctx, *collect.TaskRunID)
	if err != nil {
		t.Fatal(err)
	}
	if retry.RetryOfTaskRunID == nil || *retry.RetryOfTaskRunID != *first.TaskRunID || retry.TriggerSource != coretask.RunTriggerSourceWorkflowStep {
		t.Fatalf("retry lineage: %+v", retry)
	}
	// A second pass over the same attempt admits nothing new.
	e.reconcileAt(t, run.ID, 5*time.Minute)
	if again := e.steps(t, run.ID)[0]; *again.TaskRunID != *collect.TaskRunID {
		t.Fatal("a repeated pass admitted another attempt")
	}

	e.driveTaskRunTerminal(t, collect, coretask.RunStatusSucceeded, util.Ptr("collected"), nil)
	e.reconcileAt(t, run.ID, 5*time.Minute)
	summarize := e.steps(t, run.ID)[1]
	if summarize.Status != "running" {
		t.Fatalf("summarize = %s after collect succeeded", summarize.Status)
	}
	now := time.Now().UTC()
	e.mustTransition(t, *summarize.TaskRunID, coretask.RunStatusPending, coretask.RunStatusScheduled, coretask.TransitionRunInput{StartedAt: &now})
	e.mustTransition(t, *summarize.TaskRunID, coretask.RunStatusScheduled, coretask.RunStatusRunning, coretask.TransitionRunInput{StartedAt: &now})

	e.reconcileAt(t, run.ID, 2*time.Hour)
	stopping := e.run(t, run.ID)
	if stopping.Status != string(coreworkflow.RunStatusFailing) || stopping.ErrorMessage == nil || !strings.Contains(*stopping.ErrorMessage, "timeout") {
		t.Fatalf("after the run deadline: %+v", stopping)
	}
	active, _ := e.store.GetTaskRun(ctx, *summarize.TaskRunID)
	if active.CancelRequestedAt == nil || active.CancelReason != coretask.CancelReasonWorkflowStopped {
		t.Fatalf("active attempt not asked to stop: %+v", active)
	}
	e.mustTransition(t, active.ID, coretask.RunStatusRunning, coretask.RunStatusCanceled, coretask.TransitionRunInput{EndedAt: &now})
	e.reconcileAt(t, run.ID, 2*time.Hour)
	if final := e.run(t, run.ID); final.Status != string(coreworkflow.RunStatusFailed) ||
		final.FailureClass != string(coreworkflow.FailureRunDeadline) {
		t.Fatalf("drained run = %s class %q, want failed by run_deadline", final.Status, final.FailureClass)
	}
}

// A timed-out attempt still waiting for a worker is ended by the cancel itself,
// and with no attempt left the node fails rather than reading as canceled.
func TestWorkflowNodeTimeoutThroughTheStore(t *testing.T) {
	e := newReconcileEnv(t)
	e.setDefinition(t, `{"timeout_seconds":600}`, "")
	run, _ := e.startRun(t)
	e.reconcileAt(t, run.ID, 20*time.Minute)
	steps := e.steps(t, run.ID)
	if steps[0].Status != string(coreworkflow.NodeRunStatusFailed) || steps[0].ErrorMessage == nil || !strings.Contains(*steps[0].ErrorMessage, "timeout") {
		t.Fatalf("timed-out node: %+v", steps[0])
	}
	if steps[1].Status != string(coreworkflow.NodeRunStatusBlocked) {
		t.Fatalf("dependent = %s, want blocked", steps[1].Status)
	}
	attempt, err := e.store.GetTaskRun(context.Background(), *steps[0].TaskRunID)
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Status != string(coretask.RunStatusCanceled) || attempt.CancelReason != coretask.CancelReasonWorkflowNodeTimeout {
		t.Fatalf("attempt: %+v", attempt)
	}
	if final := e.run(t, run.ID); final.Status != string(coreworkflow.RunStatusFailed) ||
		final.FailureClass != string(coreworkflow.FailureNodeTimeout) {
		t.Fatalf("run = %s class %q, want failed by node_timeout", final.Status, final.FailureClass)
	}
}

// An agent step that asks waits without a worker; the answer resumes its Task
// through the real node-linked admission, and a human_input step's answer is
// its output.
func TestWorkflowQuestionAndHumanInputThroughTheStore(t *testing.T) {
	e := newReconcileEnv(t)
	ctx := context.Background()
	definition := fmt.Sprintf(`{"schema_version":1,"nodes":[
		{"id":"collect","type":"agent_task","agent":{"id":%q},"input":{"instruction":"collect data"}},
		{"id":"approve","type":"human_input","needs":["collect"],"input":{"instruction":"Ship it?"},"output_schema":{"type":"boolean"}}],
		"result":{"source":"node.approve.output","pointer":"/structured"}}`, e.agentA)
	if _, err := e.svc.UpdateWorkflow(ctx, UpdateWorkflowCmd{SpaceID: e.spaceID, UserID: e.userID, WorkflowID: e.wfID, Definition: &definition}); err != nil {
		t.Fatal(err)
	}
	run, nodes := e.startRun(t)
	asked := nodes[0]
	now := time.Now().UTC()
	e.mustTransition(t, *asked.TaskRunID, coretask.RunStatusPending, coretask.RunStatusScheduled, coretask.TransitionRunInput{StartedAt: &now})
	e.mustTransition(t, *asked.TaskRunID, coretask.RunStatusScheduled, coretask.RunStatusRunning, coretask.TransitionRunInput{StartedAt: &now})
	e.mustTransition(t, *asked.TaskRunID, coretask.RunStatusRunning, coretask.RunStatusSucceeded, coretask.TransitionRunInput{
		EndedAt: &now, Output: util.Ptr("Which source?"), Questions: util.Ptr(`[{"question":"Which source?"}]`)})
	e.reconcileAt(t, run.ID, 0)
	if got := e.steps(t, run.ID)[0]; got.Status != string(coreworkflow.NodeRunStatusWaiting) {
		t.Fatalf("asking step = %s, want waiting", got.Status)
	}
	requests, err := e.svc.ListWorkflowRunRequests(ctx, e.spaceID, run.ID)
	if err != nil || len(requests) != 1 || requests[0].Kind != coreworkflow.RequestKindQuestion {
		t.Fatalf("requests = %+v %v", requests, err)
	}
	if _, err := e.svc.RespondToRequest(ctx, RespondToRequestCmd{SpaceID: e.spaceID, UserID: e.userID, RequestID: requests[0].ID,
		Action: RequestActionAnswer, Response: []byte(`"the archive"`)}); err != nil {
		t.Fatalf("answer: %v", err)
	}
	resumed := e.steps(t, run.ID)[0]
	if resumed.Status != "running" || *resumed.TaskID != *asked.TaskID || *resumed.TaskRunID == *asked.TaskRunID {
		t.Fatalf("resumed step = %+v", resumed)
	}
	continuation, err := e.store.GetTaskRun(ctx, *resumed.TaskRunID)
	if err != nil || continuation.Input != "the archive" || continuation.PreviousTaskRunID == nil || *continuation.PreviousTaskRunID != *asked.TaskRunID {
		t.Fatalf("continuation = %+v %v", continuation, err)
	}
	e.driveTaskRunTerminal(t, resumed, coretask.RunStatusSucceeded, util.Ptr("collected"), nil)
	e.reconcileAt(t, run.ID, 0)

	requests, _ = e.svc.ListWorkflowRunRequests(ctx, e.spaceID, run.ID)
	if len(requests) != 2 || requests[1].Kind != coreworkflow.RequestKindInput || requests[1].Status != coreworkflow.RequestStatusPending {
		t.Fatalf("requests after collect = %+v", requests)
	}
	if _, err := e.svc.RespondToRequest(ctx, RespondToRequestCmd{SpaceID: e.spaceID, UserID: e.userID, RequestID: requests[1].ID,
		Action: RequestActionAnswer, Response: []byte(`true`)}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	final := e.run(t, run.ID)
	if final.Status != string(coreworkflow.RunStatusSucceeded) || final.Result == nil || *final.Result != "true" {
		t.Fatalf("run = %s result=%v", final.Status, final.Result)
	}
	answered, _ := e.store.GetWorkflowRequest(ctx, requests[1].ID)
	if answered.RespondedBy == nil || *answered.RespondedBy != e.userID {
		t.Fatalf("responder not recorded: %+v", answered)
	}
}
