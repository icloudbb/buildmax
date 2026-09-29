package workflow

import (
	"context"
	"encoding/json"
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

const approvalSchema = `{"type":"object","properties":{"approved":{"type":"boolean"}},"required":["approved"],"additionalProperties":false}`

// approvalDefinition drafts, asks a person to approve the draft, then publishes.
func approvalDefinition(humanPolicy string) string {
	return `{"schema_version":1,"nodes":[` +
		`{"id":"draft","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"draft"}},` +
		`{"id":"approve","type":"human_input","needs":["draft"],"input":{"instruction":"Approve this draft?","bindings":[{"name":"draft","source":"node.draft.output","pointer":"/text"}]},"output_schema":` + approvalSchema + humanPolicy + `},` +
		`{"id":"publish","type":"agent_task","needs":["approve"],"agent":{"id":"a_1"},"input":{"instruction":"publish","bindings":[{"name":"approved","source":"node.approve.output","pointer":"/structured/approved"}]}}` +
		`]}`
}

func pendingRequest(t *testing.T, store *mock.MockWorkflowStore, runID string) *coreworkflow.Request {
	t.Helper()
	for i := range store.Requests {
		if store.Requests[i].WorkflowRunID == runID && store.Requests[i].Status == coreworkflow.RequestStatusPending {
			return &store.Requests[i]
		}
	}
	t.Fatal("no pending request")
	return nil
}

func respond(svc *Service, requestID, action, response string) (*coreworkflow.Request, error) {
	return svc.RespondToRequest(context.Background(), RespondToRequestCmd{
		SpaceID: "tm_1", UserID: "u1", RequestID: requestID, Action: action,
		Response: json.RawMessage(response), Reason: "not ready yet",
	})
}

func TestHumanInput_OpensARequestAndItsAnswerBecomesTheOutput(t *testing.T) {
	svc, store, taskRuns, runID := concurrencySvc(t, approvalDefinition(""))
	endAttempt(t, svc, store, taskRuns, runID, "draft", string(coretask.RunStatusSucceeded))

	node := nodeRun(t, store, runID, "approve")
	if node.Status != string(coreworkflow.NodeRunStatusWaiting) || node.TaskID != nil {
		t.Fatalf("approve = %s task=%v, want waiting with no Task", node.Status, node.TaskID)
	}
	req := pendingRequest(t, store, runID)
	if req.Kind != coreworkflow.RequestKindInput || !strings.Contains(req.Prompt, "draft out") || req.ResponseSchema == nil {
		t.Fatalf("request = %+v, want an input request carrying the bound draft and the schema", req)
	}
	pending, total, err := svc.ListPendingRequests(context.Background(), "tm_1", 10, 0)
	if err != nil || total != 1 || pending[0].ID != req.ID {
		t.Fatalf("pending = %v total=%d err=%v", pending, total, err)
	}
	if run := runStatus(t, store, runID); run.Status != string(coreworkflow.RunStatusRunning) || run.NextReconcileAt == nil {
		t.Fatalf("run = %s next=%v, want running and scheduled for recovery", run.Status, run.NextReconcileAt)
	}

	if _, err := respond(svc, req.ID, RequestActionAnswer, `{"approved":"yes"}`); !errors.Is(err, ErrInvalidRequestResponse) {
		t.Fatalf("an answer outside the schema err = %v", err)
	}
	if _, err := respond(svc, req.ID, RequestActionAnswer, `{"approved":true}`); err != nil {
		t.Fatalf("answer: %v", err)
	}
	node = nodeRun(t, store, runID, "approve")
	if node.Status != string(coreworkflow.NodeRunStatusSucceeded) || node.Structured == nil || *node.Structured != `{"approved":true}` {
		t.Fatalf("approve after the answer = %s structured=%v", node.Status, node.Structured)
	}
	publish := nodeRun(t, store, runID, "publish")
	if publish.Status != string(coreworkflow.NodeRunStatusRunning) || !strings.Contains(*publish.ResolvedInput, "true") {
		t.Fatalf("publish = %s input=%v, want running with the approval bound", publish.Status, publish.ResolvedInput)
	}
	if _, err := respond(svc, req.ID, RequestActionAnswer, `{"approved":false}`); !errors.Is(err, ErrWorkflowRequestResolved) {
		t.Fatalf("a second answer err = %v, want already resolved", err)
	}
}

func TestHumanInput_DeclineFailsTheRun(t *testing.T) {
	svc, store, taskRuns, runID := concurrencySvc(t, approvalDefinition(""))
	endAttempt(t, svc, store, taskRuns, runID, "draft", string(coretask.RunStatusSucceeded))
	req := pendingRequest(t, store, runID)
	if _, err := respond(svc, req.ID, RequestActionDecline, ""); err != nil {
		t.Fatalf("decline: %v", err)
	}
	node := nodeRun(t, store, runID, "approve")
	if node.Status != string(coreworkflow.NodeRunStatusFailed) || node.ErrorMessage == nil || !strings.Contains(*node.ErrorMessage, "not ready yet") {
		t.Fatalf("declined node = %s error=%v, want failed with the reason", node.Status, node.ErrorMessage)
	}
	if got := nodeRun(t, store, runID, "publish").Status; got != string(coreworkflow.NodeRunStatusBlocked) {
		t.Fatalf("publish = %s, want blocked", got)
	}
	if run := runStatus(t, store, runID); run.Status != string(coreworkflow.RunStatusFailed) {
		t.Fatalf("run = %s, want failed", run.Status)
	}
}

func TestHumanInput_ExpiresAfterItsTimeout(t *testing.T) {
	svc, store, taskRuns, runID := concurrencySvc(t, approvalDefinition(`,"policy":{"timeout_seconds":3600}`))
	endAttempt(t, svc, store, taskRuns, runID, "draft", string(coretask.RunStatusSucceeded))
	req := pendingRequest(t, store, runID)
	if req.ExpiresAt == nil || req.ExpiresAt.Before(time.Now().Add(59*time.Minute)) {
		t.Fatalf("expires_at = %v, want about an hour out", req.ExpiresAt)
	}
	if run := runStatus(t, store, runID); run.NextReconcileAt == nil || run.NextReconcileAt.After(*req.ExpiresAt) {
		t.Fatalf("run wakes at %v, want no later than the expiry %v", run.NextReconcileAt, req.ExpiresAt)
	}
	req.ExpiresAt = util.Ptr(time.Now().Add(-time.Second))
	if _, err := respond(svc, req.ID, RequestActionAnswer, `{"approved":true}`); !errors.Is(err, ErrWorkflowRequestResolved) {
		t.Fatalf("an answer after expiry err = %v, want refused", err)
	}
	if err := svc.Reconcile(context.Background(), runID); err != nil {
		t.Fatal(err)
	}
	if req.Status != coreworkflow.RequestStatusExpired {
		t.Fatalf("request = %s, want expired", req.Status)
	}
	if got := nodeRun(t, store, runID, "approve").Status; got != string(coreworkflow.NodeRunStatusFailed) {
		t.Fatalf("expired node = %s, want failed", got)
	}
	if run := runStatus(t, store, runID); run.Status != string(coreworkflow.RunStatusFailed) {
		t.Fatalf("run = %s, want failed", run.Status)
	}
}

func TestHumanInput_FreeTextRootWaitsFromTheStart(t *testing.T) {
	svc, store, _, runID := concurrencySvc(t, `{"schema_version":1,"nodes":[`+
		`{"id":"brief","type":"human_input","input":{"instruction":"What should we research?"}}],`+
		`"result":{"source":"node.brief.output","pointer":"/text"}}`)
	req := pendingRequest(t, store, runID)
	if req.ResponseSchema != nil {
		t.Fatal("a node with no output_schema asks for free text")
	}
	if _, err := respond(svc, req.ID, RequestActionAnswer, `{"topic":"x"}`); !errors.Is(err, ErrInvalidRequestResponse) {
		t.Fatalf("a non-string free-text answer err = %v", err)
	}
	if _, err := respond(svc, req.ID, RequestActionAnswer, `"solar panels"`); err != nil {
		t.Fatal(err)
	}
	run := runStatus(t, store, runID)
	if run.Status != string(coreworkflow.RunStatusSucceeded) || run.Result == nil || *run.Result != `"solar panels"` {
		t.Fatalf("run = %s result=%v, want the answer as its result", run.Status, run.Result)
	}
}

func TestAgentQuestion_WaitsAndTheAnswerResumesTheSameTask(t *testing.T) {
	svc, store, taskRuns, runID := concurrencySvc(t, `{"schema_version":1,"nodes":[`+
		`{"id":"a","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"a"},"policy":{"timeout_seconds":600}}]}`)
	first := *nodeRun(t, store, runID, "a")
	questions := json.RawMessage(`[{"question":"Which color?","options":["blue","red"]}]`)
	taskRuns.Runs = append(taskRuns.Runs, coretask.Run{ID: *first.TaskRunID, TaskID: *first.TaskID, Status: string(coretask.RunStatusSucceeded), Questions: questions})
	if err := svc.Reconcile(context.Background(), runID); err != nil {
		t.Fatal(err)
	}
	node := nodeRun(t, store, runID, "a")
	if node.Status != string(coreworkflow.NodeRunStatusWaiting) || node.DeadlineAt != nil {
		t.Fatalf("asking node = %s deadline=%v, want waiting with its timeout paused", node.Status, node.DeadlineAt)
	}
	req := pendingRequest(t, store, runID)
	if req.Kind != coreworkflow.RequestKindQuestion || req.TaskRunID == nil || *req.TaskRunID != *first.TaskRunID || req.Questions == nil {
		t.Fatalf("request = %+v", req)
	}

	if _, err := respond(svc, req.ID, RequestActionAnswer, `"blue"`); err != nil {
		t.Fatal(err)
	}
	node = nodeRun(t, store, runID, "a")
	if node.Status != string(coreworkflow.NodeRunStatusRunning) || node.Attempt != 1 || *node.TaskID != *first.TaskID ||
		*node.TaskRunID == *first.TaskRunID || node.DeadlineAt == nil {
		t.Fatalf("answered node = %+v, want the same attempt running a new run on the same Task", node)
	}
	resumed, _ := taskRuns.GetTaskRun(context.Background(), *node.TaskRunID)
	if resumed.Input != "blue" || resumed.RetryOfTaskRunID != nil {
		t.Fatalf("resumed run input=%q retry_of=%v, want the answer as a continuation", resumed.Input, resumed.RetryOfTaskRunID)
	}
	endAttempt(t, svc, store, taskRuns, runID, "a", string(coretask.RunStatusSucceeded))
	if run := runStatus(t, store, runID); run.Status != string(coreworkflow.RunStatusSucceeded) {
		t.Fatalf("run = %s, want succeeded", run.Status)
	}
}

func TestCancelWorkflowRun_StopsWorkAndClosesRequests(t *testing.T) {
	svc, store, taskRuns, runID := concurrencySvc(t, `{"schema_version":1,"nodes":[`+
		`{"id":"a","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"a"}},`+
		`{"id":"ask","type":"human_input","input":{"instruction":"Anything to add?"}},`+
		`{"id":"after","type":"agent_task","needs":["a","ask"],"agent":{"id":"a_1"},"input":{"instruction":"after"}}]}`)
	a := nodeRun(t, store, runID, "a")
	taskRuns.Runs = append(taskRuns.Runs, coretask.Run{ID: *a.TaskRunID, TaskID: *a.TaskID, Status: string(coretask.RunStatusRunning)})
	req := pendingRequest(t, store, runID)

	run, err := svc.CancelWorkflowRun(context.Background(), "tm_1", "u1", runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != string(coreworkflow.RunStatusCanceling) {
		t.Fatalf("run = %s, want canceling while a step still runs", run.Status)
	}
	if req.Status != coreworkflow.RequestStatusCanceled || nodeRun(t, store, runID, "ask").Status != string(coreworkflow.NodeRunStatusCanceled) {
		t.Fatalf("request=%s ask=%s, want both canceled", req.Status, nodeRun(t, store, runID, "ask").Status)
	}
	if got := nodeRun(t, store, runID, "after").Status; got != string(coreworkflow.NodeRunStatusBlocked) {
		t.Fatalf("after = %s, want blocked", got)
	}
	active, _ := taskRuns.GetTaskRun(context.Background(), *a.TaskRunID)
	if active.CancelReason != coretask.CancelReasonWorkflowStopped {
		t.Fatalf("active step cancel reason = %q", active.CancelReason)
	}
	if _, err := respond(svc, req.ID, RequestActionAnswer, `"late"`); !errors.Is(err, ErrWorkflowRequestResolved) {
		t.Fatalf("answering a canceled run's request err = %v", err)
	}
	if again, err := svc.CancelWorkflowRun(context.Background(), "tm_1", "u1", runID); err != nil || again.Status != string(coreworkflow.RunStatusCanceling) {
		t.Fatalf("a repeated cancel = %v %v, want a no-op", again, err)
	}
	endAttempt(t, svc, store, taskRuns, runID, "a", string(coretask.RunStatusCanceled))
	if run := runStatus(t, store, runID); run.Status != string(coreworkflow.RunStatusCanceled) {
		t.Fatalf("run = %s, want canceled", run.Status)
	}
	if _, err := svc.CancelWorkflowRun(context.Background(), "tm_1", "u1", runID); !errors.Is(err, ErrWorkflowRunFinished) {
		t.Fatalf("canceling a finished run err = %v", err)
	}
}

func TestCancelWorkflowRun_KeepsAFailureAndStaysInItsSpace(t *testing.T) {
	svc, store, taskRuns, runID := concurrencySvc(t, `{"schema_version":1,"nodes":[`+
		`{"id":"a","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"a"}},`+
		`{"id":"b","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"b"}}]}`)
	if _, err := svc.CancelWorkflowRun(context.Background(), "tm_other", "u1", runID); err == nil {
		t.Fatal("canceled a run from another space")
	}
	endAttempt(t, svc, store, taskRuns, runID, "a", string(coretask.RunStatusFailed))
	if _, err := svc.CancelWorkflowRun(context.Background(), "tm_1", "u1", runID); !errors.Is(err, ErrWorkflowRunStoppingFailed) {
		t.Fatalf("canceling a failing run err = %v", err)
	}
}

func TestParseDefinition_HumanInputRules(t *testing.T) {
	svc := &Service{
		Workflows: &mock.MockWorkflowStore{},
		Agents:    &mock.MockAgentStore{Agents: []agentdef.Agent{{ID: "a_1", SpaceID: "tm_1", Name: "A"}}},
	}
	human := func(extra string) string {
		return `{"schema_version":1,"nodes":[{"id":"h","type":"human_input","input":{"instruction":"ok?"}` + extra + `}]}`
	}
	cases := []struct {
		name, definition string
		want             error
	}{
		{"valid", human(`,"output_schema":{"type":"string"},"policy":{"timeout_seconds":600}`), nil},
		{"names an agent", human(`,"agent":{"id":"a_1"}`), ErrInvalidHumanInputNode},
		{"retries", human(`,"policy":{"max_attempts":2}`), ErrInvalidHumanInputNode},
		{"reaches an Issue", human(`,"issue_access":"if_bound"`), ErrInvalidHumanInputNode},
		{"no instruction", `{"schema_version":1,"nodes":[{"id":"h","type":"human_input","input":{}}]}`, ErrInvalidHumanInputNode},
		{"unknown type", `{"schema_version":1,"nodes":[{"id":"h","type":"approval","input":{"instruction":"ok?"}}]}`, ErrInvalidNodeType},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wf, err := svc.CreateWorkflow(context.Background(), CreateWorkflowCmd{SpaceID: "tm_1", UserID: "u1", Name: "WF", Definition: tc.definition})
			if tc.want == nil {
				if err != nil {
					t.Fatalf("CreateWorkflow: %v", err)
				}
				if strings.Contains(wf.Definition, `"agent"`) {
					t.Fatalf("a human_input node stored an agent: %s", wf.Definition)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}
