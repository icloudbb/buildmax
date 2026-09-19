package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/service/workflow"
	"github.com/icloudbb/buildmax/internal/util"
)

// The workflow tools are space-scoped, not conversation-scoped: a Workflow is a
// reusable Space resource, so each runner binds the turn's space (and, for a
// run, the user) rather than the conversation. A nil service or empty space
// returns a nil runner, which the tool reports as "not configured".

func newListWorkflowsServiceRunner(svc *workflow.Service, spaceID string) listWorkflowsRunner {
	if svc == nil || spaceID == "" {
		return nil
	}
	return &listWorkflowsServiceRunner{svc: svc, spaceID: spaceID}
}

func newRunWorkflowServiceRunner(svc *workflow.Service, spaceID, userID string) runWorkflowRunner {
	if svc == nil || spaceID == "" {
		return nil
	}
	return &runWorkflowServiceRunner{svc: svc, spaceID: spaceID, userID: userID}
}

func newGetWorkflowRunServiceRunner(svc *workflow.Service, spaceID string) getWorkflowRunRunner {
	if svc == nil || spaceID == "" {
		return nil
	}
	return &getWorkflowRunServiceRunner{svc: svc, spaceID: spaceID}
}

type listWorkflowsServiceRunner struct {
	svc     *workflow.Service
	spaceID string
}

func (r *listWorkflowsServiceRunner) ListWorkflows(ctx context.Context) (string, error) {
	list, err := r.svc.ListWorkflows(ctx, r.spaceID)
	if err != nil {
		return "", err
	}
	var lines []string
	for _, wf := range list {
		// Only a published workflow is a callable unit; a draft or archived
		// definition is not runnable, so the model never sees it.
		if wf.Status != coreworkflow.StatusPublished {
			continue
		}
		line := fmt.Sprintf("%d. %s | %s", len(lines)+1, wf.ID, wf.Name)
		if desc := util.TruncateRunes(wf.Description, 100); desc != "" {
			line += " | " + desc
		}
		if schema := inputSchemaOf(wf.Definition); schema != "" {
			line += "\n   input_schema: " + schema
		} else {
			line += "\n   input: none"
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return "No published workflows in this space.", nil
	}
	return strings.Join(lines, "\n"), nil
}

// inputSchemaOf returns the definition's input_schema as compact JSON, or "" when
// the definition declares none or cannot be parsed. A run supplies input only
// when this is non-empty.
func inputSchemaOf(definition string) string {
	var def coreworkflow.Definition
	if err := json.Unmarshal([]byte(definition), &def); err != nil {
		return ""
	}
	if len(def.InputSchema) == 0 {
		return ""
	}
	return string(def.InputSchema)
}

type runWorkflowServiceRunner struct {
	svc     *workflow.Service
	spaceID string
	userID  string
}

func (r *runWorkflowServiceRunner) RunWorkflow(ctx context.Context, workflowID, input string, issueID *string) (runID, status string, err error) {
	run, _, err := r.svc.StartWorkflowRun(ctx, workflow.StartWorkflowRunCmd{
		SpaceID:    r.spaceID,
		UserID:     r.userID,
		WorkflowID: workflowID,
		IssueID:    issueID,
		Input:      input,
	})
	if err != nil {
		return "", "", err
	}
	return run.ID, run.Status, nil
}

type getWorkflowRunServiceRunner struct {
	svc     *workflow.Service
	spaceID string
}

func (r *getWorkflowRunServiceRunner) GetWorkflowRun(ctx context.Context, workflowRunID string) (string, error) {
	run, nodes, err := r.svc.GetWorkflowRunDetail(ctx, r.spaceID, workflowRunID)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "workflow_run_id: %s\nworkflow_id: %s\nstatus: %s\ncreated_at: %s\n",
		run.ID, run.WorkflowID, run.Status, util.FormatMinute(run.CreatedAt))
	if run.Result != nil && *run.Result != "" {
		fmt.Fprintf(&b, "result: %s\n", util.TruncateRunes(*run.Result, 300))
	}
	if run.ErrorMessage != nil && *run.ErrorMessage != "" {
		fmt.Fprintf(&b, "error: %s\n", util.TruncateRunes(*run.ErrorMessage, 200))
	}
	if len(nodes) > 0 {
		b.WriteString("nodes:\n")
		for _, n := range nodes {
			fmt.Fprintf(&b, "  - %s | %s", n.NodeID, n.Status)
			if n.ErrorMessage != nil && *n.ErrorMessage != "" {
				fmt.Fprintf(&b, " | %s", util.TruncateRunes(*n.ErrorMessage, 120))
			}
			b.WriteByte('\n')
		}
	}
	return b.String(), nil
}
