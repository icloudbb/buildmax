package conversation

import (
	"context"
	"fmt"

	"github.com/icloudbb/buildmax/internal/core/llm"
)

// listWorkflowsRunner is the interface used by the ListWorkflows tool. The runner
// binds the turn's space, so the tool takes no scope argument.
type listWorkflowsRunner interface {
	ListWorkflows(ctx context.Context) (summary string, err error)
}

type listWorkflowsTool struct {
	runner listWorkflowsRunner
}

const toolNameListWorkflows = "ListWorkflows"

// Access implements llm.AccessDeclarer. Listing reads the workflow store and
// returns; it changes nothing, so it may overlap its neighbours.
func (t *listWorkflowsTool) Access(_ map[string]any) llm.Access { return llm.AccessReadOnly }

func (t *listWorkflowsTool) Name() string { return toolNameListWorkflows }

func (t *listWorkflowsTool) Description() string {
	return "List the published workflows in the current space. A workflow is a reusable multi-step plan the user can run. Use this when the user asks what workflows exist or wants to run one. Returns workflow_id, name, description, and the input_schema each run must satisfy (\"input: none\" means the workflow takes no input)."
}

func (t *listWorkflowsTool) Parameters() any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
		"required":   []any{},
	}
}

func (t *listWorkflowsTool) Execute(ctx context.Context, _ map[string]any) (string, error) {
	if t.runner == nil {
		return "", fmt.Errorf("%s not configured", toolNameListWorkflows)
	}
	return t.runner.ListWorkflows(ctx)
}

// newListWorkflowsTool returns a llm.Tool that lists the space's published
// workflows. If runner is nil, Execute returns "not configured".
func newListWorkflowsTool(runner listWorkflowsRunner) llm.Tool {
	return &listWorkflowsTool{runner: runner}
}
