package issue

import (
	"context"
	"github.com/icloudbb/buildmax/internal/core/apierr"
	"strings"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
)

var (
	ErrCommentsNotConfigured = apierr.New(apierr.KindNotConfigured, "comments not configured")
	ErrCommentBodyRequired   = apierr.New(apierr.KindInvalid, "comment body required")
	ErrCommentTooLong        = apierr.New(apierr.KindInvalid, "comment too long")
	ErrCommentNotFound       = apierr.New(apierr.KindNotFound, "comment not found")
	ErrCommentNotEditable    = apierr.New(apierr.KindForbidden, "comment not editable")
	// ErrRunCommentBudgetExhausted refuses a run's comment past RunCommentBudget.
	// A run reports a bounded number of times so a loop cannot fill a thread; the
	// terminal summary RunReporter writes is a separate path and is never
	// refused. See docs/design/agent-bridge-cli.md.
	ErrRunCommentBudgetExhausted = apierr.New(apierr.KindQuotaExceeded, "run comment budget exhausted")
	// ErrInvalidCommentAuthorKind refuses an author kind the thread has no
	// rendering for. A kind nobody displays is a comment nobody can attribute.
	ErrInvalidCommentAuthorKind = apierr.New(apierr.KindInvalid, "invalid author_kind")
)

// CommentBodyLimit bounds a comment body in bytes.
//
// A comment is a statement about an issue, not a place to paste a run's output:
// long content belongs in an artifact, and a comment points at one. The limit
// also bounds the size of a thread response, which is otherwise unbounded in
// the number of comments times their length.
const CommentBodyLimit = 16 * 1024

// AgentCommentBodyLimit bounds a comment an Agent authors, in runes. It is
// stricter than CommentBodyLimit, which caps every author: an Agent's report is
// a claim about the work, not a place to paste a run's output — long content
// belongs in an Artifact the comment points at. Enforced here so it binds every
// Agent client alike — the runtime tool, the CLI bridge, and any future one —
// rather than being replicated in each. See docs/design/agent-bridge-cli.md.
const AgentCommentBodyLimit = 2000

// RunCommentBudget bounds how many comments one task run may add to its Issue.
// A run states progress and a result, not a running log; the count is by
// source_task_run_id, so it holds across process restarts and every Agent
// client alike, unlike the in-process counter the runtime tool once kept.
const RunCommentBudget = 3

type CreateCommentCmd struct {
	IssueID    string
	AuthorKind string
	AuthorID   string
	Body       string
	// ArtifactIDs are Artifacts the author names by identity; the server records
	// them in the body (appendArtifactRefs) rather than in a relation of their
	// own. They do not count against AgentCommentBodyLimit.
	ArtifactIDs     []string
	SourceTaskID    *string
	SourceTaskRunID *string
}

type UpdateCommentCmd struct {
	IssueID   string
	CommentID string
	UserID    string
	Body      string
	// CanModerate is true when the caller may edit or delete a comment they did
	// not write. Only deletion honors it; see UpdateComment.
	CanModerate bool
}

type DeleteCommentCmd struct {
	IssueID     string
	CommentID   string
	UserID      string
	CanModerate bool
}

// CreateComment appends a comment to an issue. The caller is responsible for
// having authorized the issue's space.
func (s *Service) CreateComment(ctx context.Context, cmd CreateCommentCmd) (*coreissue.Comment, error) {
	if s.Comments == nil {
		return nil, ErrCommentsNotConfigured
	}
	body, err := validateCommentBody(cmd.Body)
	if err != nil {
		return nil, err
	}
	kind := cmd.AuthorKind
	if kind == "" {
		kind = coreissue.CommentAuthorUser
	}
	if !isKnownCommentAuthorKind(kind) {
		return nil, ErrInvalidCommentAuthorKind
	}
	// The stricter Agent limit applies to the author's own words, before the
	// server appends Artifact references, so a report near the limit is not
	// rejected by the identifiers the server adds.
	if isAgentAuthoredKind(kind) && len([]rune(body)) > AgentCommentBodyLimit {
		return nil, ErrCommentTooLong
	}
	// A run's comments are budgeted by source_task_run_id. Only the worker route
	// names a run at create time; a local_agent comment is runless and unbudgeted
	// (its authority is bounded elsewhere — see the design record).
	if cmd.SourceTaskRunID != nil && *cmd.SourceTaskRunID != "" {
		used, err := s.Comments.CountIssueCommentsBySourceTaskRun(ctx, *cmd.SourceTaskRunID)
		if err != nil {
			return nil, err
		}
		if used >= RunCommentBudget {
			return nil, ErrRunCommentBudgetExhausted
		}
	}
	body = appendArtifactRefs(body, cmd.ArtifactIDs)
	return s.Comments.CreateIssueComment(ctx, coreissue.CreateCommentInput{
		IssueID:         cmd.IssueID,
		AuthorKind:      kind,
		AuthorID:        cmd.AuthorID,
		Body:            body,
		SourceTaskID:    cmd.SourceTaskID,
		SourceTaskRunID: cmd.SourceTaskRunID,
	})
}

func isKnownCommentAuthorKind(kind string) bool {
	switch kind {
	case coreissue.CommentAuthorUser, coreissue.CommentAuthorAgent,
		coreissue.CommentAuthorLocalAgent, coreissue.CommentAuthorSystem:
		return true
	}
	return false
}

// isAgentAuthoredKind reports whether a comment is written by an Agent rather
// than a person, and so is bound by AgentCommentBodyLimit and the per-run
// budget. A worker run authors `agent`; a local run authors `local_agent`.
func isAgentAuthoredKind(kind string) bool {
	return kind == coreissue.CommentAuthorAgent || kind == coreissue.CommentAuthorLocalAgent
}

// appendArtifactRefs records the Artifacts an author named by identity in the
// comment body. An Artifact is reachable by its own handle, so a second, weaker
// reference would be one more thing to keep true. Composed here, at the one
// choke point every write path reaches, rather than in each route.
func appendArtifactRefs(body string, ids []string) string {
	refs := make([]string, 0, len(ids))
	for _, id := range ids {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			refs = append(refs, trimmed)
		}
	}
	if len(refs) == 0 {
		return body
	}
	return strings.TrimSpace(body + "\n\nArtifacts: " + strings.Join(refs, ", "))
}

// ListComments returns an issue's thread, oldest first.
func (s *Service) ListComments(ctx context.Context, issueID string, limit, offset int) ([]coreissue.Comment, int, error) {
	if s.Comments == nil {
		return nil, 0, ErrCommentsNotConfigured
	}
	return s.Comments.ListIssueComments(ctx, issueID, limit, offset)
}

// UpdateComment replaces a comment's body.
//
// Only the person who wrote a comment may edit it. Moderation permits deletion,
// not rewriting: an edit puts words in another person's mouth, and an agent or
// system comment is the record of what a run reported — a record anyone can
// rewrite is not one.
func (s *Service) UpdateComment(ctx context.Context, cmd UpdateCommentCmd) (*coreissue.Comment, error) {
	comment, err := s.loadComment(ctx, cmd.IssueID, cmd.CommentID)
	if err != nil {
		return nil, err
	}
	if comment.AuthorKind != coreissue.CommentAuthorUser || comment.AuthorID != cmd.UserID {
		return nil, ErrCommentNotEditable
	}
	body, err := validateCommentBody(cmd.Body)
	if err != nil {
		return nil, err
	}
	updated, err := s.Comments.UpdateIssueComment(ctx, cmd.CommentID, body)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, ErrCommentNotFound
	}
	return updated, nil
}

// DeleteComment removes a comment. The author may delete their own; a moderator
// may delete any comment on the issue, including one an agent wrote.
func (s *Service) DeleteComment(ctx context.Context, cmd DeleteCommentCmd) error {
	comment, err := s.loadComment(ctx, cmd.IssueID, cmd.CommentID)
	if err != nil {
		return err
	}
	own := comment.AuthorKind == coreissue.CommentAuthorUser && comment.AuthorID == cmd.UserID
	if !own && !cmd.CanModerate {
		return ErrCommentNotEditable
	}
	return s.Comments.DeleteIssueComment(ctx, cmd.CommentID)
}

// loadComment resolves a comment and verifies it belongs to the issue the
// caller was authorized against. A comment ID from another issue is not found,
// not a successful write to somewhere else.
func (s *Service) loadComment(ctx context.Context, issueID, commentID string) (*coreissue.Comment, error) {
	if s.Comments == nil {
		return nil, ErrCommentsNotConfigured
	}
	comment, err := s.Comments.GetIssueComment(ctx, commentID)
	if err != nil {
		return nil, err
	}
	if comment == nil || comment.IssueID != issueID {
		return nil, ErrCommentNotFound
	}
	return comment, nil
}

func validateCommentBody(body string) (string, error) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return "", ErrCommentBodyRequired
	}
	if len(trimmed) > CommentBodyLimit {
		return "", ErrCommentTooLong
	}
	return trimmed, nil
}
