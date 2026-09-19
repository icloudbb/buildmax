package conversation

import (
	"context"
	"testing"
)

func TestNewListWorkflowsTool_nilRunner(t *testing.T) {
	tool := newListWorkflowsTool(nil)
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("Execute: expected error when runner is nil")
	}
	if err.Error() != "ListWorkflows not configured" {
		t.Errorf("Execute error = %q, want ListWorkflows not configured", err.Error())
	}
}

func TestNewListWorkflowsTool_success(t *testing.T) {
	tool := newListWorkflowsTool(listWorkflowsRunnerFunc(func(ctx context.Context) (string, error) {
		return "1. wf_a | Release | ships a build\n   input: none", nil
	}))
	out, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "1. wf_a | Release | ships a build\n   input: none" {
		t.Errorf("Execute = %q", out)
	}
}

// listWorkflowsRunnerFunc adapts a function to listWorkflowsRunner.
type listWorkflowsRunnerFunc func(ctx context.Context) (string, error)

func (f listWorkflowsRunnerFunc) ListWorkflows(ctx context.Context) (string, error) {
	return f(ctx)
}
