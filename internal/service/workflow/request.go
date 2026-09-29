package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	"github.com/icloudbb/buildmax/internal/core/jsonschema"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/service/task"
	"github.com/icloudbb/buildmax/internal/util"
)

var (
	ErrWorkflowRequestNotFound   = apierr.New(apierr.KindNotFound, "workflow request not found")
	ErrWorkflowRequestResolved   = apierr.New(apierr.KindConflict, "this request was already answered, declined, expired, or canceled")
	ErrInvalidRequestResponse    = apierr.New(apierr.KindInvalid, "invalid response: it must satisfy the request's response schema, or be a non-empty JSON string when the request has none")
	ErrInvalidRequestAction      = apierr.New(apierr.KindInvalid, "invalid action: must be answer or decline")
	ErrWorkflowRunFinished       = apierr.New(apierr.KindConflict, "this workflow run has already finished")
	ErrWorkflowRunStoppingFailed = apierr.New(apierr.KindConflict, "this workflow run is already stopping after a failure")
)

// Respond actions.
const (
	RequestActionAnswer  = "answer"
	RequestActionDecline = "decline"
)

// RespondToRequestCmd answers or declines one pending request.
type RespondToRequestCmd struct {
	SpaceID   string
	UserID    string
	RequestID string
	Action    string
	// Response is the answer as JSON: a value satisfying the request's
	// response schema, or a JSON string when it has none.
	Response json.RawMessage
	// Reason explains a decline; it becomes the node's failure message.
	Reason string
}

// hasWaitingNode reports whether any node waits on a request.
func hasWaitingNode(steps []coreworkflow.NodeRun) bool {
	for i := range steps {
		if steps[i].Status == string(coreworkflow.NodeRunStatusWaiting) {
			return true
		}
	}
	return false
}

// openInputRequest opens a ready human_input node's request, with its bound
// values rendered into the prompt the responder reads.
func (s *Service) openInputRequest(ctx context.Context, run *coreworkflow.Run, node coreworkflow.NodeRun, steps []coreworkflow.NodeRun, now time.Time) error {
	bound, err := s.resolveStepBindings(ctx, run, node, steps)
	if err != nil {
		startedAt := now
		_, drainErr := s.Workflows.BeginWorkflowRunDrain(ctx, coreworkflow.BeginRunDrainInput{
			WorkflowRunID: run.ID, NodeRunID: node.ID,
			NodeExpected: coreworkflow.NodeRunStatusPending, NodeStatus: coreworkflow.NodeRunStatusFailed,
			RunExpected: coreworkflow.RunStatusRunning, RunStatus: coreworkflow.RunStatusFailing,
			ErrorMessage: ptrError(err), StartedAt: &startedAt, EndedAt: &startedAt,
		})
		if drainErr != nil {
			return drainErr
		}
		return err
	}
	prompt := buildHumanInputPrompt(node.Prompt, bound)
	_, _, err = s.Workflows.OpenWorkflowRequest(ctx, coreworkflow.OpenRequestInput{
		WorkflowRunID:  run.ID,
		NodeRunID:      node.ID,
		NodeExpected:   coreworkflow.NodeRunStatusPending,
		Key:            "node/" + node.NodeID,
		Kind:           coreworkflow.RequestKindInput,
		Prompt:         prompt,
		ResponseSchema: node.OutputSchema,
		ExpiresAt:      coreworkflow.Deadline(now, node.TimeoutSeconds),
		ResolvedInput:  &prompt,
		Now:            now,
	})
	return err
}

// openQuestionRequest records the questions an agent attempt ended on and
// moves its node to waiting. The attempt's deadline stops: a person's time to
// answer is not the Agent's time to work.
func (s *Service) openQuestionRequest(ctx context.Context, run *coreworkflow.Run, node coreworkflow.NodeRun, taskRun *coretask.Run, now time.Time) error {
	questions := string(taskRun.Questions)
	_, _, err := s.Workflows.OpenWorkflowRequest(ctx, coreworkflow.OpenRequestInput{
		WorkflowRunID: run.ID,
		NodeRunID:     node.ID,
		NodeExpected:  coreworkflow.NodeRunStatusRunning,
		Key:           "task_run/" + taskRun.ID,
		Kind:          coreworkflow.RequestKindQuestion,
		Questions:     &questions,
		TaskRunID:     &taskRun.ID,
		Now:           now,
	})
	return err
}

// foldWaitingNodes advances every waiting node from its latest request. It
// returns the earliest pending expiry, and whether a decline or expiry started
// the drain, in which case the caller re-enters the pass.
func (s *Service) foldWaitingNodes(ctx context.Context, run *coreworkflow.Run, steps []coreworkflow.NodeRun, now time.Time) (*time.Time, bool, error) {
	if !hasWaitingNode(steps) {
		return nil, false, nil
	}
	requests, err := s.Workflows.ListWorkflowRequestsByRun(ctx, run.ID)
	if err != nil {
		return nil, false, err
	}
	latest := make(map[string]coreworkflow.Request, len(requests))
	for _, req := range requests {
		latest[req.NodeRunID] = req // listed oldest first, so the last wins
	}
	var wake *time.Time
	for i := range steps {
		node := steps[i]
		if node.Status != string(coreworkflow.NodeRunStatusWaiting) {
			continue
		}
		req, ok := latest[node.ID]
		if !ok {
			return nil, false, fmt.Errorf("waiting node %s has no request", node.NodeID)
		}
		if req.Status == coreworkflow.RequestStatusPending && req.ExpiresAt != nil && !now.Before(*req.ExpiresAt) {
			if _, err := s.Workflows.ResolveWorkflowRequest(ctx, coreworkflow.ResolveRequestInput{
				RequestID: req.ID, Status: coreworkflow.RequestStatusExpired, Now: now,
			}); err != nil {
				return nil, false, err
			}
			fresh, err := s.Workflows.GetWorkflowRequest(ctx, req.ID)
			if err != nil || fresh == nil {
				return nil, false, err
			}
			req = *fresh
		}
		switch req.Status {
		case coreworkflow.RequestStatusPending:
			wake = earliest(wake, req.ExpiresAt)
		case coreworkflow.RequestStatusAnswered:
			if err := s.applyAnswer(ctx, run, node, req, now); err != nil {
				return nil, false, err
			}
		case coreworkflow.RequestStatusDeclined, coreworkflow.RequestStatusExpired:
			message := fmt.Sprintf("the request was %s", req.Status)
			if req.Status == coreworkflow.RequestStatusDeclined {
				message = "the request was declined"
				if reason := decodeJSONString(req.Response); reason != "" {
					message += ": " + reason
				}
			}
			if _, err := s.Workflows.BeginWorkflowRunDrain(ctx, coreworkflow.BeginRunDrainInput{
				WorkflowRunID: run.ID, NodeRunID: node.ID,
				NodeExpected: coreworkflow.NodeRunStatusWaiting, NodeStatus: coreworkflow.NodeRunStatusFailed,
				RunExpected: coreworkflow.RunStatusRunning, RunStatus: coreworkflow.RunStatusFailing,
				ErrorMessage: &message, EndedAt: &now,
			}); err != nil {
				return nil, false, err
			}
			return nil, true, nil
		}
	}
	return wake, false, nil
}

// applyAnswer completes a human_input node with its answer, or resumes an
// agent node whose attempt asked, by continuing its Task with the answer.
func (s *Service) applyAnswer(ctx context.Context, run *coreworkflow.Run, node coreworkflow.NodeRun, req coreworkflow.Request, now time.Time) error {
	if req.Kind == coreworkflow.RequestKindInput {
		text, structured := answerOutput(req)
		_, err := s.Workflows.TransitionWorkflowNodeRun(ctx, coreworkflow.TransitionNodeRunInput{
			NodeRunID: node.ID, ExpectedStatus: coreworkflow.NodeRunStatusWaiting, NewStatus: coreworkflow.NodeRunStatusSucceeded,
			Output: &text, Structured: structured, EndedAt: &now,
		})
		return err
	}
	if req.TaskRunID == nil {
		return fmt.Errorf("question request %s names no attempt", req.ID)
	}
	asked, err := s.TaskRuns.GetTaskRun(ctx, *req.TaskRunID)
	if err != nil {
		return err
	}
	if asked == nil {
		return fmt.Errorf("question request %s: attempt %s not found", req.ID, *req.TaskRunID)
	}
	attempt := max(node.Attempt, 1)
	next, err := s.TaskService.AdmitWorkflowAnswerRun(ctx, task.WorkflowAnswerCmd{
		UserID: run.CreatedBy, WorkflowRunID: run.ID, WorkflowNodeRunID: node.ID, NodeID: node.NodeID,
		RequestID: req.ID, Attempt: attempt, Asked: asked, Answer: decodeJSONString(req.Response),
	})
	if err != nil {
		_, drainErr := s.Workflows.BeginWorkflowRunDrain(ctx, coreworkflow.BeginRunDrainInput{
			WorkflowRunID: run.ID, NodeRunID: node.ID,
			NodeExpected: coreworkflow.NodeRunStatusWaiting, NodeStatus: coreworkflow.NodeRunStatusFailed,
			RunExpected: coreworkflow.RunStatusRunning, RunStatus: coreworkflow.RunStatusFailing,
			ErrorMessage: util.Ptr(fmt.Sprintf("the answer could not resume the step: %v", err)), EndedAt: &now,
		})
		if drainErr != nil {
			return drainErr
		}
		return err
	}
	_, err = s.Workflows.TransitionWorkflowNodeRun(ctx, coreworkflow.TransitionNodeRunInput{
		NodeRunID: node.ID, ExpectedStatus: coreworkflow.NodeRunStatusWaiting, NewStatus: coreworkflow.NodeRunStatusRunning,
		TaskRunID: &next.ID, DeadlineAt: coreworkflow.Deadline(now, node.TimeoutSeconds), ErrorMessage: util.Ptr(""),
	})
	return err
}

// answerOutput is a human_input node's output: the answer as text, and as the
// structured value when the request declared a response schema.
func answerOutput(req coreworkflow.Request) (string, *string) {
	if req.Response == nil {
		return "", nil
	}
	if req.ResponseSchema != nil {
		structured := *req.Response
		if text := decodeJSONString(req.Response); text != "" {
			return text, &structured
		}
		return structured, &structured
	}
	return decodeJSONString(req.Response), nil
}

// decodeJSONString reads a JSON string value, or "" for anything else.
func decodeJSONString(raw *string) string {
	if raw == nil {
		return ""
	}
	var out string
	if err := json.Unmarshal([]byte(*raw), &out); err != nil {
		return ""
	}
	return out
}

// buildHumanInputPrompt renders a human_input node's instruction and the values
// bound into it, the context a responder needs to answer.
func buildHumanInputPrompt(instruction string, bound []boundValue) string {
	var b strings.Builder
	b.WriteString(instruction)
	for _, bv := range bound {
		fmt.Fprintf(&b, "\n\n%s:\n%s", bv.Name, bv.Value)
	}
	return b.String()
}

// runInSpace reads a run and confirms its workflow belongs to spaceID.
func (s *Service) runInSpace(ctx context.Context, spaceID, workflowRunID string) (*coreworkflow.Run, *coreworkflow.Workflow, error) {
	if s.Workflows == nil {
		return nil, nil, ErrWorkflowsNotConfigured
	}
	run, err := s.Workflows.GetWorkflowRun(ctx, workflowRunID)
	if err != nil {
		return nil, nil, err
	}
	if run == nil {
		return nil, nil, ErrWorkflowRunNotFound
	}
	wf, err := s.GetWorkflow(ctx, spaceID, run.WorkflowID)
	if err != nil {
		return nil, nil, err
	}
	return run, wf, nil
}

// ListWorkflowRunRequests returns a run's requests, oldest first.
func (s *Service) ListWorkflowRunRequests(ctx context.Context, spaceID, workflowRunID string) ([]coreworkflow.Request, error) {
	if _, _, err := s.runInSpace(ctx, spaceID, workflowRunID); err != nil {
		return nil, err
	}
	return s.Workflows.ListWorkflowRequestsByRun(ctx, workflowRunID)
}

// ListPendingRequests returns the space's requests waiting on a person.
func (s *Service) ListPendingRequests(ctx context.Context, spaceID string, limit, offset int) ([]coreworkflow.Request, int, error) {
	if s.Workflows == nil {
		return nil, 0, ErrWorkflowsNotConfigured
	}
	return s.Workflows.ListPendingWorkflowRequestsBySpace(ctx, spaceID, limit, offset)
}

// RespondToRequest answers or declines a pending request and advances its run.
// The first response wins; a later one, or one after expiry, is a conflict.
func (s *Service) RespondToRequest(ctx context.Context, cmd RespondToRequestCmd) (*coreworkflow.Request, error) {
	if s.Workflows == nil {
		return nil, ErrWorkflowsNotConfigured
	}
	req, err := s.Workflows.GetWorkflowRequest(ctx, cmd.RequestID)
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, ErrWorkflowRequestNotFound
	}
	run, wf, err := s.runInSpace(ctx, cmd.SpaceID, req.WorkflowRunID)
	if err != nil {
		if kind, ok := apierr.KindOf(err); ok && kind == apierr.KindNotFound {
			return nil, ErrWorkflowRequestNotFound
		}
		return nil, err
	}
	if req.Status != coreworkflow.RequestStatusPending {
		return nil, ErrWorkflowRequestResolved
	}
	var status string
	var response string
	switch cmd.Action {
	case RequestActionAnswer:
		status = coreworkflow.RequestStatusAnswered
		normalized, err := validateResponse(req, cmd.Response)
		if err != nil {
			return nil, err
		}
		response = normalized
	case RequestActionDecline:
		status = coreworkflow.RequestStatusDeclined
		encoded, _ := json.Marshal(strings.TrimSpace(cmd.Reason))
		response = string(encoded)
	default:
		return nil, ErrInvalidRequestAction
	}
	resolved, err := s.Workflows.ResolveWorkflowRequest(ctx, coreworkflow.ResolveRequestInput{
		RequestID: req.ID, Status: status, Response: &response, RespondedBy: &cmd.UserID, Now: time.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	if !resolved {
		return nil, ErrWorkflowRequestResolved
	}
	action := coreaudit.WorkflowRequestAnswered
	if status == coreworkflow.RequestStatusDeclined {
		action = coreaudit.WorkflowRequestDeclined
	}
	s.Audit.UserAction(ctx, cmd.UserID, wf.SpaceID, action, "workflow_request", req.ID, wf.Name+" / "+req.NodeID)
	// Advance at once on a detached context; recovery re-runs a pass this
	// request loses, so the answer is already durable either way.
	if err := s.Reconcile(context.WithoutCancel(ctx), run.ID); err != nil {
		return nil, err
	}
	return s.Workflows.GetWorkflowRequest(ctx, req.ID)
}

// validateResponse checks an answer against the request and returns it as the
// compact JSON text to store.
func validateResponse(req *coreworkflow.Request, raw json.RawMessage) (string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return "", ErrInvalidRequestResponse
	}
	if req.ResponseSchema != nil {
		schema, err := jsonschema.Compile(json.RawMessage(*req.ResponseSchema))
		if err != nil {
			return "", apierr.Detail(ErrInvalidOutputSchema, "%v", err)
		}
		if err := schema.Validate(json.RawMessage(trimmed)); err != nil {
			return "", apierr.Detail(ErrInvalidRequestResponse, "%v", err)
		}
		return trimmed, nil
	}
	var text string
	if err := json.Unmarshal([]byte(trimmed), &text); err != nil || strings.TrimSpace(text) == "" {
		return "", ErrInvalidRequestResponse
	}
	return trimmed, nil
}

// CancelWorkflowRun stops a run a person no longer wants: no node starts, open
// requests close, and active attempts are asked to stop; the run ends canceled
// once they have. Canceling a canceling run is a no-op; a run already failing
// keeps its failure.
func (s *Service) CancelWorkflowRun(ctx context.Context, spaceID, userID, workflowRunID string) (*coreworkflow.Run, error) {
	run, wf, err := s.runInSpace(ctx, spaceID, workflowRunID)
	if err != nil {
		return nil, err
	}
	switch coreworkflow.RunStatus(run.Status) {
	case coreworkflow.RunStatusCanceling:
		return run, nil
	case coreworkflow.RunStatusFailing:
		return nil, ErrWorkflowRunStoppingFailed
	}
	if coreworkflow.RunStatusTerminal(coreworkflow.RunStatus(run.Status)) {
		return nil, ErrWorkflowRunFinished
	}
	stopped, err := s.Workflows.StopWorkflowRun(ctx, coreworkflow.StopRunInput{
		WorkflowRunID: run.ID,
		RunExpected:   coreworkflow.RunStatus(run.Status),
		RunStatus:     coreworkflow.RunStatusCanceling,
		ErrorMessage:  util.Ptr("the run was canceled"),
	})
	if err != nil {
		return nil, err
	}
	if !stopped {
		// Another outcome committed first; report what the run now is.
		current, err := s.Workflows.GetWorkflowRun(ctx, run.ID)
		if err != nil {
			return nil, err
		}
		if current.Status != string(coreworkflow.RunStatusCanceling) {
			return nil, ErrWorkflowRunFinished
		}
		return current, nil
	}
	s.Audit.UserAction(ctx, userID, wf.SpaceID, coreaudit.WorkflowRunCanceled, "workflow_run", run.ID, wf.Name)
	if err := s.Reconcile(context.WithoutCancel(ctx), run.ID); err != nil {
		return nil, err
	}
	return s.Workflows.GetWorkflowRun(ctx, run.ID)
}
