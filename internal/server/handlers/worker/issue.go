package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/server/httputil"
	issuesvc "github.com/icloudbb/buildmax/internal/service/issue"
)

// issueCommentWindow is how much of the thread a run reads. A long-running
// issue's discussion can exceed any sane context budget, and an agent that
// spends its window reading it has less left for the work, so the route returns
// the most recent slice and says how much it left out.
const issueCommentWindow = 20

// IssueAccess is the Issue capability a run token receives: read the one Issue
// its task names, and add one comment to it.
//
// Narrow on purpose. There is no update method here, so no worker route can
// change an Issue's status, owner, executor, or hierarchy however the run's agent is
// prompted. See docs/design/issue-agent-access.md section 6.
type IssueAccess interface {
	GetIssue(ctx context.Context, spaceID, issueID string) (*coreissue.Issue, error)
	ListChildren(ctx context.Context, spaceID, issueID string) ([]coreissue.Issue, error)
	ListComments(ctx context.Context, issueID string, limit, offset int) ([]coreissue.Comment, int, error)
	CreateComment(ctx context.Context, cmd issuesvc.CreateCommentCmd) (*coreissue.Comment, error)
}

type runIssueChildResponse struct {
	Title  string `json:"title"`
	Status string `json:"status"`
}

type runIssueCommentResponse struct {
	AuthorKind string    `json:"author_kind"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
}

type runIssueResponse struct {
	Title           string                    `json:"title"`
	Description     string                    `json:"description"`
	Status          string                    `json:"status"`
	ExecutorKind    string                    `json:"executor_kind,omitempty"`
	Children        []runIssueChildResponse   `json:"children"`
	Comments        []runIssueCommentResponse `json:"comments"`
	OmittedComments int                       `json:"omitted_comments"`
}

type postRunIssueCommentRequest struct {
	Body        string   `json:"body"`
	ArtifactIDs []string `json:"artifact_ids"`
}

// runIssue resolves the one Issue this run is entitled to, from the run token
// alone.
//
// The run token names the run, the run names the task, and the task names both
// the space and the Issue. A worker never says which Issue it wants, so a stolen
// run token cannot be pointed at another one — the same reason the artifact
// route derives its space rather than accepting one.
func (h *Handler) runIssue(w http.ResponseWriter, r *http.Request) (*coretask.Task, string, bool) {
	taskRunID := r.PathValue("task_run_id")
	if taskRunID == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "task_run_id required")
		return nil, "", false
	}
	if h.cfg.Issues == nil {
		httputil.WriteJSONError(w, http.StatusServiceUnavailable, "issues not configured")
		return nil, "", false
	}
	if h.cfg.TaskRuns == nil {
		httputil.WriteJSONError(w, http.StatusServiceUnavailable, "task runs not configured")
		return nil, "", false
	}
	run, task, err := h.cfg.TaskRuns.GetTaskRunWithTask(r.Context(), taskRunID)
	if err != nil {
		httputil.WriteInternalError(w, err, "worker handler error", "handler", "run_issue", "task_run_id", taskRunID)
		return nil, "", false
	}
	if run == nil || task == nil {
		httputil.WriteJSONError(w, http.StatusNotFound, "run not found")
		return nil, "", false
	}
	// The issue tool is the running agent's, both to read and to comment: a
	// terminal run has no agent, and must not add to an issue after it is over.
	if !requireRunning(w, run.Status) {
		return nil, "", false
	}
	if task.IssueID == nil || *task.IssueID == "" || task.SpaceID == "" {
		httputil.WriteJSONError(w, http.StatusNotFound, "this run is not working an issue")
		return nil, "", false
	}
	return task, taskRunID, true
}

// getRunIssue returns the bounded view of the Issue this run is working.
func (h *Handler) getRunIssue(w http.ResponseWriter, r *http.Request) {
	task, _, ok := h.runIssue(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	issue, err := h.cfg.Issues.GetIssue(ctx, task.SpaceID, *task.IssueID)
	if err != nil {
		if httputil.WriteServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "worker handler error", "handler", "get_run_issue", "issue_id", *task.IssueID)
		return
	}
	out := runIssueResponse{
		Title:       issue.Title,
		Description: issue.Description,
		Status:      issue.Status,
		Children:    []runIssueChildResponse{},
		Comments:    []runIssueCommentResponse{},
	}
	if issue.ExecutorKind != nil {
		out.ExecutorKind = *issue.ExecutorKind
	}
	// Children and comments are context, not the answer. A failure to load
	// either leaves the issue readable rather than failing the whole call: an
	// agent that gets the description and no thread can still work.
	if children, err := h.cfg.Issues.ListChildren(ctx, task.SpaceID, *task.IssueID); err == nil {
		for _, child := range children {
			out.Children = append(out.Children, runIssueChildResponse{Title: child.Title, Status: child.Status})
		}
	}
	comments, omitted := h.recentComments(ctx, *task.IssueID)
	out.Comments, out.OmittedComments = comments, omitted
	httputil.WriteJSON(w, http.StatusOK, out)
}

// recentComments reads the tail of the thread. The first call learns the total;
// only a thread longer than the window costs a second one.
func (h *Handler) recentComments(ctx context.Context, issueID string) ([]runIssueCommentResponse, int) {
	out := []runIssueCommentResponse{}
	list, total, err := h.cfg.Issues.ListComments(ctx, issueID, issueCommentWindow, 0)
	if err != nil {
		return out, 0
	}
	omitted := 0
	if total > issueCommentWindow {
		omitted = total - issueCommentWindow
		if tail, _, err := h.cfg.Issues.ListComments(ctx, issueID, issueCommentWindow, omitted); err == nil {
			list = tail
		} else {
			omitted = 0
		}
	}
	for _, c := range list {
		out = append(out, runIssueCommentResponse{AuthorKind: c.AuthorKind, Body: c.Body, CreatedAt: c.CreatedAt})
	}
	return out, omitted
}

// postRunIssueComment appends one agent-authored comment to the run's Issue.
//
// Authored exactly as RunReporter authors the summary of a finished run: kind
// agent, the task's agent as author, and the task and run recorded as the
// source. A reader of the thread should not have to tell a report the agent
// chose to write from one the server wrote for it by any means other than what
// it says.
func (h *Handler) postRunIssueComment(w http.ResponseWriter, r *http.Request) {
	task, taskRunID, ok := h.runIssue(w, r)
	if !ok {
		return
	}
	var req postRunIssueCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if _, err := h.cfg.Issues.GetIssue(r.Context(), task.SpaceID, *task.IssueID); err != nil {
		if httputil.WriteServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "worker handler error", "handler", "post_run_issue_comment", "issue_id", *task.IssueID)
		return
	}
	agentID := ""
	if task.AgentID != nil {
		agentID = *task.AgentID
	}
	comment, err := h.cfg.Issues.CreateComment(r.Context(), issuesvc.CreateCommentCmd{
		IssueID:         *task.IssueID,
		AuthorKind:      coreissue.CommentAuthorAgent,
		AuthorID:        agentID,
		Body:            req.Body,
		ArtifactIDs:     req.ArtifactIDs,
		SourceTaskID:    &task.ID,
		SourceTaskRunID: &taskRunID,
	})
	if err != nil {
		if httputil.WriteServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "worker handler error", "handler", "post_run_issue_comment", "issue_id", *task.IssueID)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, map[string]string{"id": comment.ID})
}
