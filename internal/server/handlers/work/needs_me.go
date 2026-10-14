package work

import (
	"net/http"

	"github.com/icloudbb/buildmax/internal/server/httputil"
)

type needsMeIssue struct {
	Issue IssueResponse `json:"issue"`
	// Reason is why the Issue needs its owner: run_failed, awaiting_answer,
	// or workflow_request.
	Reason string `json:"reason"`
}

// needsMeGroup carries at most needsme.GroupLimit items; Total above their
// count says there are more.
type needsMeGroup[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
}

type needsMeResponse struct {
	AttentionIssues     needsMeGroup[needsMeIssue]            `json:"attention_issues"`
	FailedTasks         needsMeGroup[TaskResponse]            `json:"failed_tasks"`
	AwaitingAnswerTasks needsMeGroup[TaskResponse]            `json:"awaiting_answer_tasks"`
	WorkflowRequests    needsMeGroup[workflowRequestResponse] `json:"workflow_requests"`
}

// needsMeHandler serves the work in the Space that waits on the caller. The
// service owns what counts; this only shapes it.
func (h *Handler) needsMeHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Issues, "issues not configured")
	if !ok {
		return
	}
	result, err := h.needsMe.NeedsMe(r.Context(), spaceID, userID)
	if err != nil {
		if httputil.WriteServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "needs_me", "space_id", spaceID)
		return
	}
	out := needsMeResponse{
		AttentionIssues:     needsMeGroup[needsMeIssue]{Items: make([]needsMeIssue, len(result.AttentionIssues.Items)), Total: result.AttentionIssues.Total},
		FailedTasks:         needsMeGroup[TaskResponse]{Items: make([]TaskResponse, len(result.FailedTasks.Items)), Total: result.FailedTasks.Total},
		AwaitingAnswerTasks: needsMeGroup[TaskResponse]{Items: make([]TaskResponse, len(result.AwaitingAnswerTasks.Items)), Total: result.AwaitingAnswerTasks.Total},
		WorkflowRequests:    needsMeGroup[workflowRequestResponse]{Items: workflowRequestsToResponse(result.WorkflowRequests.Items), Total: result.WorkflowRequests.Total},
	}
	for i, item := range result.AttentionIssues.Items {
		out.AttentionIssues.Items[i] = needsMeIssue{Issue: issueToResponse(item.Issue), Reason: string(item.Reason)}
	}
	for i, task := range result.FailedTasks.Items {
		out.FailedTasks.Items[i] = taskToResponse(task)
	}
	for i, task := range result.AwaitingAnswerTasks.Items {
		out.AwaitingAnswerTasks.Items[i] = taskToResponse(task)
	}
	httputil.WriteJSON(w, http.StatusOK, out)
}
