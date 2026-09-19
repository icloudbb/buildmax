package conversation

import (
	"context"
	"fmt"

	"github.com/icloudbb/buildmax/internal/core/llm"
)

// getWorkflowRunRunner is the interface used by the GetWorkflowRun tool. The
// runner binds the turn's space, so a run in another space is not found.
type getWorkflowRunRunner interface {
	GetWorkflowRun(ctx context.Context, workflowRunID string) (detail string, err error)
}

type getWorkflowRunTool struct {
	runner getWorkflowRunRunner
}

const toolNameGetWorkflowRun = "GetWorkflowRun"

// Access implements llm.AccessDeclarer. Reading one run's detail changes
// nothing, so it may overlap its neighbours.
func (t *getWorkflowRunTool) Access(_ map[string]any) llm.Access { return llm.AccessReadOnly }

func (t *getWorkflowRunTool) Name() string { return toolNameGetWorkflowRun }

func (t *getWorkflowRunTool) Description() string {
	return "Get the status and result of one workflow run by workflow_run_id (returned by RunWorkflow). Use this when the user asks how a workflow run is doing. Returns the run status, its result or error, and per-node status. Fails with a clear error if the run is not in the current space."
}

func (t *getWorkflowRunTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"workflow_run_id": map[string]any{
				"type":        "string",
				"description": "The workflow run id (required).",
			},
		},
		"required": []any{"workflow_run_id"},
	}
}

func (t *getWorkflowRunTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.runner == nil {
		return "", fmt.Errorf("%s not configured", toolNameGetWorkflowRun)
	}
	runID, _ := args["workflow_run_id"].(string)
	if runID == "" {
		return "", fmt.Errorf("workflow_run_id is required")
	}
	return t.runner.GetWorkflowRun(ctx, runID)
}

// newGetWorkflowRunTool returns a llm.Tool that reads one workflow run's detail.
// If runner is nil, Execute returns "not configured".
func newGetWorkflowRunTool(runner getWorkflowRunRunner) llm.Tool {
	return &getWorkflowRunTool{runner: runner}
}
