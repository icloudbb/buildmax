package work

import (
	"context"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"net/http"
	"time"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	"github.com/icloudbb/buildmax/internal/server/httputil"
	assistantsvc "github.com/icloudbb/buildmax/internal/service/assistant"
	"github.com/icloudbb/buildmax/internal/service/issue"
)

type issueCommentResponse struct {
	ID              string     `json:"id"`
	IssueID         string     `json:"issue_id"`
	AuthorKind      string     `json:"author_kind"`
	AuthorID        string     `json:"author_id"`
	Body            string     `json:"body"`
	SourceTaskID    *string    `json:"source_task_id,omitempty"`
	SourceTaskRunID *string    `json:"source_task_run_id,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	EditedAt        *time.Time `json:"edited_at,omitempty"`
}

type issueCommentListResponse struct {
	Comments []issueCommentResponse `json:"comments"`
	Total    int                    `json:"total"`
}

type issueCommentRequest struct {
	Body string `json:"body"`
	// AuthorKind is empty for a person writing on the thread, or "local_agent"
	// when the caller is relaying what an agent on their machine said. No other
	// value is accepted here: "agent" and "system" are the deployment's own
	// voices, written by a run token and by the server, and a person's session
	// may not borrow either.
	AuthorKind string `json:"author_kind"`
}

func issueCommentToResponse(comment coreissue.Comment) issueCommentResponse {
	return issueCommentResponse{
		ID:              comment.ID,
		IssueID:         comment.IssueID,
		AuthorKind:      comment.AuthorKind,
		AuthorID:        comment.AuthorID,
		Body:            comment.Body,
		SourceTaskID:    comment.SourceTaskID,
		SourceTaskRunID: comment.SourceTaskRunID,
		CreatedAt:       comment.CreatedAt,
		EditedAt:        comment.EditedAt,
	}
}

// resolveCommentIssue authorizes a comment request through its issue, which is
// what owns the space. A comment carries no space of its own, so every route
// starts here.
func (h *Handler) resolveCommentIssue(w http.ResponseWriter, r *http.Request) (userID, spaceID, issueID string, ok bool) {
	userID, spaceID, ok = h.guard().UserAndPathSpace(w, r, h.cfg.Issues, "issues not configured")
	if !ok {
		return "", "", "", false
	}
	if !httputil.RequireStore(w, h.cfg.IssueComments, "comments not configured") {
		return "", "", "", false
	}
	issueID, ok = httputil.PathValue(w, r, "issue_id")
	if !ok {
		return "", "", "", false
	}
	found, err := h.cfg.Issues.GetIssue(r.Context(), issueID)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "resolve_comment_issue", "issue_id", issueID)
		return "", "", "", false
	}
	if found == nil || found.SpaceID != spaceID {
		httputil.WriteJSONError(w, http.StatusNotFound, "issue not found")
		return "", "", "", false
	}
	return userID, spaceID, issueID, true
}

func (h *Handler) listIssueCommentsHandler(w http.ResponseWriter, r *http.Request) {
	_, _, issueID, ok := h.resolveCommentIssue(w, r)
	if !ok {
		return
	}
	limit, offset := httputil.LimitOffset(r.URL.Query(), "limit", "offset", httputil.BulkPageDefault, httputil.BulkPageMax)
	list, total, err := h.issueService().ListComments(r.Context(), issueID, limit, offset)
	if err != nil {
		if h.writeIssueServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "list_issue_comments", "issue_id", issueID)
		return
	}
	out := make([]issueCommentResponse, len(list))
	for i := range list {
		out[i] = issueCommentToResponse(list[i])
	}
	httputil.WriteJSON(w, http.StatusOK, issueCommentListResponse{Comments: out, Total: total})
}

func (h *Handler) createIssueCommentHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, issueID, ok := h.resolveCommentIssue(w, r)
	if !ok {
		return
	}
	if _, ok := h.guard().SpaceAction(w, r, userID, spaceID, corespace.ActionCommentIssue); !ok {
		return
	}
	var req issueCommentRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	authorKind := coreissue.CommentAuthorUser
	if req.AuthorKind != "" {
		if req.AuthorKind != coreissue.CommentAuthorLocalAgent {
			httputil.WriteJSONError(w, http.StatusBadRequest,
				"author_kind may only be omitted or \"local_agent\": agent and system comments are written by the deployment, not by a session")
			return
		}
		authorKind = coreissue.CommentAuthorLocalAgent
	}
	// The author is the caller either way. A local agent report is a claim, and
	// the person who made it is the one identity this route verified.
	created, err := h.issueService().CreateComment(r.Context(), issue.CreateCommentCmd{
		IssueID:    issueID,
		AuthorKind: authorKind,
		AuthorID:   userID,
		Body:       req.Body,
	})
	if err != nil {
		if h.writeIssueServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "create_issue_comment", "issue_id", issueID)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, issueCommentToResponse(*created))
}

func (h *Handler) patchIssueCommentHandler(w http.ResponseWriter, r *http.Request) {
	userID, _, issueID, ok := h.resolveCommentIssue(w, r)
	if !ok {
		return
	}
	commentID, ok := httputil.PathValue(w, r, "comment_id")
	if !ok {
		return
	}
	var req issueCommentRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	updated, err := h.issueService().UpdateComment(r.Context(), issue.UpdateCommentCmd{
		IssueID:   issueID,
		CommentID: commentID,
		UserID:    userID,
		Body:      req.Body,
	})
	if err != nil {
		if h.writeIssueServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "patch_issue_comment", "comment_id", commentID)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, issueCommentToResponse(*updated))
}

func (h *Handler) deleteIssueCommentHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, issueID, ok := h.resolveCommentIssue(w, r)
	if !ok {
		return
	}
	commentID, ok := httputil.PathValue(w, r, "comment_id")
	if !ok {
		return
	}
	// Moderation is checked without writing a response: deleting your own
	// comment needs no permission, so a member without it is not being refused.
	err := h.issueService().DeleteComment(r.Context(), issue.DeleteCommentCmd{
		IssueID:     issueID,
		CommentID:   commentID,
		UserID:      userID,
		CanModerate: h.guard().MemberAllows(r.Context(), userID, spaceID, corespace.ActionModerateIssueComments),
	})
	if err != nil {
		if h.writeIssueServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "delete_issue_comment", "comment_id", commentID)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RequesterReplier answers an escalated Issue's requester. The Space
// Assistant front door implements it.
type RequesterReplier interface {
	ReplyToRequester(ctx context.Context, cmd assistantsvc.ReplyCmd) (*coreissue.Comment, error)
}

type requesterReplyRequest struct {
	Text string `json:"text"`
}

// replyToRequesterHandler serves POST /api/spaces/{space_id}/issues/{issue_id}/requester-replies.
// Anyone who may comment on the Issue may answer its requester; the reply is
// sent through the Assistant's bot and recorded as their comment.
func (h *Handler) replyToRequesterHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, issueID, ok := h.resolveCommentIssue(w, r)
	if !ok {
		return
	}
	if _, ok := h.guard().SpaceAction(w, r, userID, spaceID, corespace.ActionCommentIssue); !ok {
		return
	}
	if h.cfg.RequesterReplies == nil {
		httputil.WriteJSONError(w, http.StatusServiceUnavailable, "assistants not configured")
		return
	}
	var req requesterReplyRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	created, err := h.cfg.RequesterReplies.ReplyToRequester(r.Context(), assistantsvc.ReplyCmd{
		SpaceID: spaceID, ActorID: userID, IssueID: issueID, Text: req.Text,
	})
	if err != nil {
		if h.writeIssueServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "reply_to_requester", "issue_id", issueID)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, issueCommentToResponse(*created))
}
