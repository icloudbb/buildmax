package work

import (
	"context"
	"sort"
	"time"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/util"
)

type outputSourceResponse struct {
	SourceType        string  `json:"source_type"`
	TaskID            string  `json:"task_id,omitempty"`
	TaskRunID         string  `json:"task_run_id,omitempty"`
	ConversationID    string  `json:"conversation_id,omitempty"`
	WorkflowRunID     *string `json:"workflow_run_id,omitempty"`
	WorkflowNodeRunID *string `json:"workflow_node_run_id,omitempty"`
	WorkflowNodeID    *string `json:"workflow_node_id,omitempty"`
}

type issueOutputResponse struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Kind  string `json:"kind"`
	// ArtifactID is the whole address: a client opens it at
	// /api/artifacts/{id} without needing the run that produced it.
	ArtifactID string               `json:"artifact_id"`
	Filename   string               `json:"filename,omitempty"`
	MediaType  string               `json:"media_type,omitempty"`
	SizeBytes  int64                `json:"size_bytes,omitempty"`
	Source     outputSourceResponse `json:"source"`
	CreatedAt  time.Time            `json:"created_at"`
}

// aggregateIssueOutputs collects an issue's produced outputs: the artifacts its
// runs published through UploadArtifact. Workflow step provenance is attached
// via stepsByTaskID (built from all workflow step runs across the issue's
// workflow runs). Returns outputs sorted by created_at DESC, plus the
// latest pointer. Missing or unreadable artifacts are silently skipped — they
// must not fail the overall issue flow response. The agent's reply text is not
// an output here; it lives on the task thread.
func (h *Handler) aggregateIssueOutputs(
	ctx context.Context,
	agentTasks []coretask.Task,
	stepsByTaskID map[string]coreworkflow.NodeRun,
) ([]issueOutputResponse, *issueOutputResponse) {
	// Never nil: the flow response serializes this as a JSON array, and a reader
	// distinguishes "no outputs" from a missing field.
	outputs := []issueOutputResponse{}
	outputs = append(outputs, h.artifactOutputs(ctx, agentTasks, stepsByTaskID)...)
	sort.SliceStable(outputs, func(i, j int) bool {
		return outputs[i].CreatedAt.After(outputs[j].CreatedAt)
	})
	var latest *issueOutputResponse
	if len(outputs) > 0 {
		l := outputs[0]
		latest = &l
	}
	return outputs, latest
}

// artifactOutputs lists what the issue's runs published as artifacts.
//
// They are looked up by the run that produced them, not owned by it: an
// artifact outlives the run and keeps its own address, and this is only the
// issue asking what its work produced. A deployment with no artifact store
// returns none, which is the same shape as runs that published nothing.
func (h *Handler) artifactOutputs(
	ctx context.Context,
	agentTasks []coretask.Task,
	stepsByTaskID map[string]coreworkflow.NodeRun,
) []issueOutputResponse {
	if h.cfg.Artifacts == nil || !h.cfg.Artifacts.Available() {
		return nil
	}
	// Every run of every task, not each task's last one. A retried task has
	// earlier runs, and an artifact one of them published is still a thing the
	// space keeps — it does not stop being this issue's output because the task
	// was run again.
	taskIDs := make([]string, 0, len(agentTasks))
	tasksByID := make(map[string]coretask.Task, len(agentTasks))
	for _, t := range agentTasks {
		taskIDs = append(taskIDs, t.ID)
		tasksByID[t.ID] = t
	}
	runsByTask, err := h.runIDsByTask(ctx, taskIDs)
	if err != nil {
		return nil
	}
	runIDs := make([]string, 0, len(taskIDs))
	runToTask := make(map[string]coretask.Task, len(taskIDs))
	for taskID, runs := range runsByTask {
		for _, runID := range runs {
			runIDs = append(runIDs, runID)
			runToTask[runID] = tasksByID[taskID]
		}
	}
	bySource, err := h.cfg.Artifacts.ListBySource(ctx, runIDs)
	if err != nil {
		// Tolerated: an issue's flow response must not fail because one part of
		// it could not be read.
		return nil
	}
	var out []issueOutputResponse
	for runID, artifacts := range bySource {
		t := runToTask[runID]
		source := outputSourceResponse{
			SourceType:     "task_run",
			TaskID:         t.ID,
			TaskRunID:      runID,
			ConversationID: t.ConversationID,
		}
		if step, ok := stepsByTaskID[t.ID]; ok {
			source.WorkflowRunID = util.Ptr(step.WorkflowRunID)
			source.WorkflowNodeRunID = util.Ptr(step.ID)
			source.WorkflowNodeID = util.Ptr(step.NodeID)
		}
		for i := range artifacts {
			a := artifacts[i]
			title := a.Title
			if title == "" {
				title = a.Filename
			}
			out = append(out, issueOutputResponse{
				ID:         a.ID,
				Title:      title,
				Kind:       "artifact",
				ArtifactID: a.ID,
				Filename:   a.Filename,
				MediaType:  a.MediaType,
				SizeBytes:  a.SizeBytes,
				Source:     source,
				CreatedAt:  a.CreatedAt,
			})
		}
	}
	return out
}

// runIDsByTask lists every run of the given tasks, falling back to each task's
// last run when the store cannot answer. The fallback is not equivalent — it
// loses a retried task's earlier runs — but a degraded output list beats an
// issue page that will not load.
func (h *Handler) runIDsByTask(ctx context.Context, taskIDs []string) (map[string][]string, error) {
	if h.cfg.TaskRuns != nil {
		return h.cfg.TaskRuns.ListTaskRunIDsByTasks(ctx, taskIDs)
	}
	return map[string][]string{}, nil
}
