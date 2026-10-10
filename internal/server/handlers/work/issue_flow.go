package work

import (
	"context"
	"fmt"
	"log/slog"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
)

// issueFlow is what the flow view shows, gathered before any of it becomes a
// response.
//
// The view joins several queries across three entities -- the issue's place in
// the hierarchy, the workflow it is assigned to, its Agent runs and Workflow
// runs, and each Workflow run's steps -- and none of that is a rule. It is a
// read model, so it is assembled in one place and rendered in another, rather
// than interleaved through ninety lines of a handler.
type issueFlow struct {
	Issue    coreissue.Issue
	Parent   *coreissue.Issue
	Children []coreissue.Issue
	Workflow *coreworkflow.Workflow
	// Runs is one page of the issue's runs, newest first: Agent runs started on
	// the issue and Workflow runs, in one sequence with one Total. The first
	// entry of the first page is the issue's latest run whatever its kind, so
	// no reader has to decide between two "latest" answers.
	Runs  []issueRun
	Total int
	// StepsByTaskID lets the output aggregation attribute a task's result to
	// the step that dispatched it.
	StepsByTaskID map[string]coreworkflow.NodeRun
}

// issueRun is one run of an issue: an Agent run (Task set) or a Workflow run
// (WorkflowRun and its Steps set), never both.
type issueRun struct {
	Task        *coretask.Task
	WorkflowRun *coreworkflow.Run
	Steps       []coreworkflow.NodeRun
}

// loadIssueFlow gathers the view. A failure names the query that failed,
// because "the flow did not load" is not something an operator can act on.
func (h *Handler) loadIssueFlow(ctx context.Context, spaceID, issueID string, limit, offset int) (*issueFlow, error) {
	issue, err := h.issueService().GetIssue(ctx, spaceID, issueID)
	if err != nil {
		return nil, err
	}
	flow := &issueFlow{Issue: *issue, StepsByTaskID: map[string]coreworkflow.NodeRun{}}

	// The hierarchy is two levels deep, so an issue has a parent or children,
	// never both. Neither failing is worth losing the page over: a flow view
	// missing its siblings still shows the runs it was opened for.
	if issue.ParentIssueID != nil && *issue.ParentIssueID != "" {
		parent, err := h.cfg.Issues.GetIssue(ctx, *issue.ParentIssueID)
		if err != nil {
			slog.WarnContext(ctx, "issue parent not loaded", "err", err, "issue_id", issueID)
		} else if parent != nil && parent.SpaceID == spaceID {
			flow.Parent = parent
		}
	} else if children, err := h.cfg.Issues.ListIssueChildren(ctx, issue.ID); err != nil {
		slog.WarnContext(ctx, "issue children not loaded", "err", err, "issue_id", issueID)
	} else {
		flow.Children = children
	}

	if issue.ExecutorKind != nil && issue.ExecutorID != nil && *issue.ExecutorKind == coreissue.ExecutorWorkflow {
		workflow, err := h.cfg.Workflows.GetWorkflow(ctx, *issue.ExecutorID)
		if err != nil {
			return nil, fmt.Errorf("load the issue's workflow: %w", err)
		}
		if workflow != nil && workflow.SpaceID == spaceID {
			flow.Workflow = workflow
		}
	}

	runs, total, err := h.loadIssueRuns(ctx, issueID, limit, offset)
	if err != nil {
		return nil, err
	}
	flow.Runs, flow.Total = runs, total
	for i := range flow.Runs {
		if flow.Runs[i].WorkflowRun == nil {
			continue
		}
		steps, err := h.cfg.Workflows.ListWorkflowNodeRuns(ctx, flow.Runs[i].WorkflowRun.ID)
		if err != nil {
			return nil, fmt.Errorf("load steps for workflow run %s: %w", flow.Runs[i].WorkflowRun.ID, err)
		}
		flow.Runs[i].Steps = steps
		for j := range steps {
			if steps[j].TaskID != nil && *steps[j].TaskID != "" {
				flow.StepsByTaskID[*steps[j].TaskID] = steps[j]
			}
		}
	}
	return flow, nil
}

// loadIssueRuns pages the issue's Agent runs and Workflow runs as one list.
// The two live in different tables, so each is read newest first up to the end
// of the requested page and the two are merged: any run on that page is among
// the first limit+offset of its own kind.
func (h *Handler) loadIssueRuns(ctx context.Context, issueID string, limit, offset int) ([]issueRun, int, error) {
	window := limit + offset
	workflowRuns, workflowTotal, err := h.cfg.Workflows.ListWorkflowRunsByIssue(ctx, issueID, window, 0)
	if err != nil {
		return nil, 0, fmt.Errorf("load the issue's workflow runs: %w", err)
	}
	var tasks []coretask.Task
	taskTotal := 0
	if h.cfg.Tasks != nil {
		tasks, taskTotal, err = h.cfg.Tasks.ListIssueAgentTasks(ctx, issueID, window, 0)
		if err != nil {
			return nil, 0, fmt.Errorf("load the issue's agent runs: %w", err)
		}
	}
	merged := make([]issueRun, 0, len(workflowRuns)+len(tasks))
	i, j := 0, 0
	for i < len(tasks) || j < len(workflowRuns) {
		if j == len(workflowRuns) || (i < len(tasks) && !tasks[i].CreatedAt.Before(workflowRuns[j].CreatedAt)) {
			merged = append(merged, issueRun{Task: &tasks[i]})
			i++
		} else {
			merged = append(merged, issueRun{WorkflowRun: &workflowRuns[j]})
			j++
		}
	}
	start := min(offset, len(merged))
	end := min(offset+limit, len(merged))
	return merged[start:end], workflowTotal + taskTotal, nil
}
