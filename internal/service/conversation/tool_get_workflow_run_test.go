package conversation

import (
	"context"
	"testing"
)

func TestNewGetWorkflowRunTool_nilRunner(t *testing.T) {
	tool := newGetWorkflowRunTool(nil)
	_, err := tool.Execute(context.Background(), map[string]any{"workflow_run_id": "wr_1"})
	if err == nil || err.Error() != "GetWorkflowRun not configured" {
		t.Fatalf("Execute error = %v, want GetWorkflowRun not configured", err)
	}
}

func TestNewGetWorkflowRunTool_missingRunID(t *testing.T) {
	tool := newGetWorkflowRunTool(getWorkflowRunRunnerFunc(func(ctx context.Context, runID string) (string, error) {
		return "detail", nil
	}))
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil || err.Error() != "workflow_run_id is required" {
		t.Fatalf("Execute error = %v, want workflow_run_id is required", err)
	}
}

func TestNewGetWorkflowRunTool_success(t *testing.T) {
	tool := newGetWorkflowRunTool(getWorkflowRunRunnerFunc(func(ctx context.Context, runID string) (string, error) {
		if runID != "wr_abc" {
			return "", context.Canceled
		}
		return "workflow_run_id: wr_abc\nstatus: succeeded", nil
	}))
	out, err := tool.Execute(context.Background(), map[string]any{"workflow_run_id": "wr_abc"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "workflow_run_id: wr_abc\nstatus: succeeded" {
		t.Errorf("Execute = %q", out)
	}
}

// getWorkflowRunRunnerFunc adapts a function to getWorkflowRunRunner.
type getWorkflowRunRunnerFunc func(ctx context.Context, runID string) (string, error)

func (f getWorkflowRunRunnerFunc) GetWorkflowRun(ctx context.Context, runID string) (string, error) {
	return f(ctx, runID)
}
