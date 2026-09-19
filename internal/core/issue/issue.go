package issue

import (
	"context"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
)

const (
	StatusTodo       = "todo"
	StatusInProgress = "in_progress"
	StatusDone       = "done"
	// ExecutorAgent and ExecutorWorkflow are the only Executor kinds: a person
	// cannot be an Executor. Accountability for a person is Owner, a separate
	// field that can be set at the same time. See
	// docs/design/portal-work-and-execution-experience.md.
	ExecutorAgent    = "agent"
	ExecutorWorkflow = "workflow"
)

// Issue comment author kinds. A comment is written by a person, reported by an
// agent run, stated by the server itself, or reported by an agent the server
// never observed.
//
// CommentAuthorLocalAgent is the last of those, and it is deliberately not
// CommentAuthorAgent. An agent comment is evidence: the run token that wrote it
// is the agent's own credential, and the task and run it names are records the
// deployment holds. A local agent's report is a claim made by the person whose
// machine ran it, over that person's session, about work no worker scheduled,
// no quota admitted, and no trace recorded. Both are worth having on the
// thread. Storing them under one name would make a Portal reader believe the
// deployment vouched for something it never saw.
const (
	CommentAuthorUser  = "user"
	CommentAuthorAgent = "agent"
	// CommentAuthorLocalAgent is authored by the person who ran it: that is the
	// one identity the server verified, and the accountable one.
	CommentAuthorLocalAgent = "local_agent"
	CommentAuthorSystem     = "system"
)

// Issue is the user-facing work-management object. It is intentionally separate
// from low-level task/task_run execution records.
//
// ParentIssueID makes the issue a sub-issue of another issue in the same space.
// The hierarchy is capped at two levels — a parent must itself have no parent —
// which is enforced in internal/service/issue, not by the schema. See
// docs/contribute/architecture/data-model.md.
type Issue struct {
	ID            string  `json:"id"`
	UserID        string  `json:"user_id"`
	SpaceID       string  `json:"space_id,omitempty"`
	ParentIssueID *string `json:"parent_issue_id,omitempty"`
	Title         string  `json:"title"`
	Description   string  `json:"description"`
	Status        string  `json:"status"`
	// OwnerID is the accountable person for this Issue: a space member's user
	// ID, or nil when nobody is accountable yet. It is independent of
	// ExecutorKind/ExecutorID -- a human owner and an Agent or Workflow
	// executor can both be set at once, which one combined field could not
	// express. See docs/design/portal-work-and-execution-experience.md.
	OwnerID *string `json:"owner_id,omitempty"`
	// ExecutorKind and ExecutorID name what is selected to perform the work --
	// an Agent or a Workflow, never a person. Both nil means no executor is
	// selected. Like OwnerID, ExecutorID stays an opaque handle: ExecutorKind
	// says which table it names, because one numeric column cannot reference
	// rows in two.
	ExecutorKind *string   `json:"executor_kind,omitempty"`
	ExecutorID   *string   `json:"executor_id,omitempty"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	// Version counts accepted updates, starting at 1. It is the precondition an
	// update must carry, so a reader that acts on a stale copy is refused
	// instead of overwriting whatever it never saw.
	Version uint64 `json:"version"`
}

type CreateInput struct {
	Title         string
	Description   string
	ParentIssueID *string
}

type UpdateInput struct {
	// IfVersion is the Version the caller read. The update applies only while
	// the row still carries it, and fails with ErrVersionConflict otherwise.
	//
	// Not a pointer: the zero value matches no row, so a caller that forgets it
	// is refused rather than granted the unconditional write this exists to
	// remove.
	IfVersion     uint64
	Title         *string
	Description   *string
	Status        *string
	OwnerID       *string
	ExecutorKind  *string
	ExecutorID    *string
	ParentIssueID *string
}

// ListFilter narrows a space issue listing. The zero value lists every
// issue in the space, which is what callers predating sub-issues expect.
type ListFilter struct {
	// TopLevelOnly restricts the listing to issues with no parent.
	TopLevelOnly bool
	// ParentIssueID restricts the listing to one parent's children. It is
	// ignored when TopLevelOnly is set.
	ParentIssueID string
	// OwnerID restricts the listing to one accountable person.
	OwnerID string
	// ExecutorKind and ExecutorID restrict the listing to one executor. Both
	// must be set to narrow anything: an executor_id means nothing without the
	// kind that says which table to read it against.
	ExecutorKind string
	ExecutorID   string
	// Status restricts the listing to one status. Empty lists every status.
	Status string
}

// ChildStats is a parent's sub-issue progress. It is always computed, never
// stored: a persisted counter is a second source of truth that drifts the first
// time a status write fails between the two rows.
type ChildStats struct {
	Total int `json:"total"`
	Done  int `json:"done"`
}

// ErrVersionConflict means the issue moved on between the read and the write.
// Both update methods return it rather than applying a change built from a
// version of the issue that no longer exists.
var ErrVersionConflict = apierr.New(apierr.KindConflict, "issue changed since it was read")

// Store provides issue persistence. Issues are user-scoped.
type Store interface {
	CreateIssue(ctx context.Context, userID string, in CreateInput) (*Issue, error)
	CreateIssueInSpace(ctx context.Context, spaceID, createdBy string, in CreateInput) (*Issue, error)
	ListIssuesByUser(ctx context.Context, userID string, limit, offset int) ([]Issue, int, error)
	ListIssuesBySpace(ctx context.Context, spaceID string, filter ListFilter, limit, offset int) ([]Issue, int, error)
	ListIssueChildren(ctx context.Context, parentIssueID string) ([]Issue, error)
	// ChildStatsForIssues returns sub-issue progress keyed by parent issue ID.
	// Parents with no children are absent from the map rather than present with
	// a zero value, so callers must treat a miss as "no sub-issues".
	ChildStatsForIssues(ctx context.Context, issueIDs []string) (map[string]ChildStats, error)
	GetIssue(ctx context.Context, issueID string) (*Issue, error)
	UpdateIssue(ctx context.Context, issueID, userID string, in UpdateInput) (*Issue, error)
	UpdateIssueInSpace(ctx context.Context, issueID, spaceID string, in UpdateInput) (*Issue, error)
}

// Comment is one statement about an issue, addressed to people.
//
// It is deliberately not a conversation_message: that table is LLM turn history
// carrying roles and tool traffic, replayed into a model context. A comment
// outlives any particular conversation and is never replayed. See
// docs/contribute/architecture/data-model.md.
type Comment struct {
	ID              string    `json:"id"`
	IssueID         string    `json:"issue_id"`
	AuthorKind      string    `json:"author_kind"`
	AuthorID        string    `json:"author_id"`
	Body            string    `json:"body"`
	SourceTaskID    *string   `json:"source_task_id,omitempty"`
	SourceTaskRunID *string   `json:"source_task_run_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	// EditedAt is nil until the body is changed. Its absence is meaningful:
	// it means the text is as first written.
	EditedAt *time.Time `json:"edited_at,omitempty"`
}

type CreateCommentInput struct {
	IssueID         string
	AuthorKind      string
	AuthorID        string
	Body            string
	SourceTaskID    *string
	SourceTaskRunID *string
}

// CommentStore provides issue comment persistence. A comment's space is its
// issue's space; the row carries no space_id of its own, so every caller
// authorizes through the issue.
type CommentStore interface {
	CreateIssueComment(ctx context.Context, in CreateCommentInput) (*Comment, error)
	// ListIssueComments returns comments oldest first — a thread reads in the
	// order it was written.
	ListIssueComments(ctx context.Context, issueID string, limit, offset int) ([]Comment, int, error)
	GetIssueComment(ctx context.Context, commentID string) (*Comment, error)
	UpdateIssueComment(ctx context.Context, commentID, body string) (*Comment, error)
	DeleteIssueComment(ctx context.Context, commentID string) error
	// CountIssueComments returns comment totals keyed by issue ID. Issues with
	// no comments are absent from the map.
	CountIssueComments(ctx context.Context, issueIDs []string) (map[string]int, error)
	// CountIssueCommentsBySourceTaskRun returns how many comments name the given
	// task run as their source. It bounds a run's comments to a budget; an
	// unknown run has none.
	CountIssueCommentsBySourceTaskRun(ctx context.Context, taskRunID string) (int, error)
}
