package conversation

import (
	"context"
	"fmt"

	"github.com/icloudbb/buildmax/internal/core/llm"
)

// runWorkflowRunner is the interface used by the RunWorkflow tool. The runner
// binds the turn's space and user; issueID is optional and never inferred.
type runWorkflowRunner interface {
	RunWorkflow(ctx context.Context, workflowID, input string, issueID *string) (runID, status string, err error)
}

type runWorkflowTool struct {
	runner runWorkflowRunner
}

const toolNameRunWorkflow = "RunWorkflow"

func (t *runWorkflowTool) Name() string { return toolNameRunWorkflow }

func (t *runWorkflowTool) Description() string {
	return "Start a run of a published workflow by workflow_id. Call ListWorkflows first to find the id and its input_schema. Provide input as a JSON string that satisfies that input_schema; omit input when the workflow takes none. The run executes in the background like a task; tell the user it has started and do not expose internal IDs. Fails with a clear error if the workflow is not published or the input does not satisfy its schema."
}

func (t *runWorkflowTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"workflow_id": map[string]any{
				"type":        "string",
				"description": "The workflow id to run (required).",
			},
			"input": map[string]any{
				"type":        "string",
				"description": "Optional run input as a JSON string satisfying the workflow's input_schema. Omit when the workflow declares no input_schema.",
			},
			"issue_id": map[string]any{
				"type":        "string",
				"description": "Optional issue id to link the run to. Set it only when the user explicitly ties the run to an issue.",
			},
		},
		"required": []any{"workflow_id"},
	}
}

func (t *runWorkflowTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.runner == nil {
		return "", fmt.Errorf("%s not configured", toolNameRunWorkflow)
	}
	workflowID, _ := args["workflow_id"].(string)
	if workflowID == "" {
		return "", fmt.Errorf("workflow_id is required")
	}
	input, _ := args["input"].(string)
	var issueID *string
	if iid, ok := args["issue_id"].(string); ok && iid != "" {
		issueID = &iid
	}
	runID, status, err := t.runner.RunWorkflow(ctx, workflowID, input, issueID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Workflow run started (workflow_run_id: %s, status: %s). It is now running in the background.", runID, status), nil
}

// newRunWorkflowTool returns a llm.Tool that starts a published workflow run.
// If runner is nil, Execute returns "not configured".
func newRunWorkflowTool(runner runWorkflowRunner) llm.Tool {
	return &runWorkflowTool{runner: runner}
}
