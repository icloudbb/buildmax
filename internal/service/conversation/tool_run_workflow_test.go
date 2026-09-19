package conversation

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNewRunWorkflowTool_nilRunner(t *testing.T) {
	tool := newRunWorkflowTool(nil)
	_, err := tool.Execute(context.Background(), map[string]any{"workflow_id": "wf_1"})
	if err == nil || err.Error() != "RunWorkflow not configured" {
		t.Fatalf("Execute error = %v, want RunWorkflow not configured", err)
	}
}

func TestNewRunWorkflowTool_missingWorkflowID(t *testing.T) {
	tool := newRunWorkflowTool(runWorkflowRunnerFunc(func(ctx context.Context, workflowID, input string, issueID *string) (string, string, error) {
		return "wr_1", "running", nil
	}))
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil || err.Error() != "workflow_id is required" {
		t.Fatalf("Execute error = %v, want workflow_id is required", err)
	}
}

func TestNewRunWorkflowTool_invalidInputSurfacesError(t *testing.T) {
	tool := newRunWorkflowTool(runWorkflowRunnerFunc(func(ctx context.Context, workflowID, input string, issueID *string) (string, string, error) {
		return "", "", errors.New("invalid workflow run input")
	}))
	_, err := tool.Execute(context.Background(), map[string]any{"workflow_id": "wf_1", "input": "{bad}"})
	if err == nil {
		t.Fatal("Execute: expected the runner's typed error to surface to the model")
	}
}

func TestNewRunWorkflowTool_success(t *testing.T) {
	var gotWorkflowID, gotInput string
	var gotIssue *string
	tool := newRunWorkflowTool(runWorkflowRunnerFunc(func(ctx context.Context, workflowID, input string, issueID *string) (string, string, error) {
		gotWorkflowID, gotInput, gotIssue = workflowID, input, issueID
		return "wr_9", "running", nil
	}))
	out, err := tool.Execute(context.Background(), map[string]any{
		"workflow_id": "wf_9",
		"input":       `{"topic":"x"}`,
		"issue_id":    "iss_2",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotWorkflowID != "wf_9" || gotInput != `{"topic":"x"}` {
		t.Errorf("runner got workflow_id=%q input=%q", gotWorkflowID, gotInput)
	}
	if gotIssue == nil || *gotIssue != "iss_2" {
		t.Errorf("runner issue = %v, want iss_2", gotIssue)
	}
	if !strings.Contains(out, "wr_9") || !strings.Contains(out, "running") {
		t.Errorf("Execute = %q, want it to name the run id and status", out)
	}
}

func TestNewRunWorkflowTool_noIssueByDefault(t *testing.T) {
	tool := newRunWorkflowTool(runWorkflowRunnerFunc(func(ctx context.Context, workflowID, input string, issueID *string) (string, string, error) {
		if issueID != nil {
			t.Errorf("issue must not be inferred; got %v", *issueID)
		}
		return "wr_1", "running", nil
	}))
	if _, err := tool.Execute(context.Background(), map[string]any{"workflow_id": "wf_1"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

// runWorkflowRunnerFunc adapts a function to runWorkflowRunner.
type runWorkflowRunnerFunc func(ctx context.Context, workflowID, input string, issueID *string) (string, string, error)

func (f runWorkflowRunnerFunc) RunWorkflow(ctx context.Context, workflowID, input string, issueID *string) (string, string, error) {
	return f(ctx, workflowID, input, issueID)
}
