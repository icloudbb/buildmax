package issue

import (
	"context"
	"github.com/icloudbb/buildmax/internal/core/apierr"
	"log/slog"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
)

var (
	ErrIssuesNotConfigured = apierr.New(apierr.KindNotConfigured, "issues not configured")
	ErrSpacesNotConfigured = apierr.New(apierr.KindNotConfigured, "spaces not configured")
	ErrTitleRequired       = apierr.New(apierr.KindInvalid, "title required")
	ErrInvalidStatus       = apierr.New(apierr.KindInvalid, "invalid status")
	ErrInvalidOwnerID      = apierr.New(apierr.KindInvalid, "invalid owner_id")
	ErrInvalidExecutorKind = apierr.New(apierr.KindInvalid, "invalid executor_kind")
	ErrInvalidExecutorID   = apierr.New(apierr.KindInvalid, "invalid executor_id")
	ErrIssueNotFound       = apierr.New(apierr.KindNotFound, "issue not found")
	// ErrVersionRequired means the caller sent no precondition. Refusing is the
	// point: an update with no version is the unconditional overwrite this
	// contract exists to remove, so it cannot be the permissive default.
	ErrVersionRequired        = apierr.New(apierr.KindInvalid, "version is required: read the issue and send the version it returned")
	ErrAgentsNotConfigured    = apierr.New(apierr.KindNotConfigured, "agents not configured")
	ErrAgentNotFound          = apierr.New(apierr.KindInvalid, "agent not found")
	ErrWorkflowsNotConfigured = apierr.New(apierr.KindNotConfigured, "workflows not configured")
	ErrWorkflowNotFound       = apierr.New(apierr.KindInvalid, "workflow not found")
	ErrWorkflowNotPublished   = apierr.New(apierr.KindInvalid, "workflow not published")
	// ErrParentNotFound covers both a parent that does not exist and one that
	// belongs to another space. The two are reported identically on purpose:
	// distinguishing them would confirm that an issue ID exists somewhere the
	// caller cannot see, and issue IDs are what Portal puts in URLs.
	ErrParentNotFound   = apierr.New(apierr.KindInvalid, "parent issue not found")
	ErrHierarchyTooDeep = apierr.New(apierr.KindInvalid, "issue hierarchy too deep")
	ErrIssueHasChildren = apierr.New(apierr.KindInvalid, "issue has sub-issues")
	ErrInvalidParent    = apierr.New(apierr.KindInvalid, "invalid parent_issue_id")
)

type Service struct {
	Issues    coreissue.Store
	Comments  coreissue.CommentStore
	Agents    agentdef.Store
	Spaces    corespace.Store
	Workflows coreworkflow.Store
}

type CreateIssueCmd struct {
	UserID        string
	SpaceID       string
	Title         string
	Description   string
	ParentIssueID *string
	// Status, OwnerID, ExecutorKind, and ExecutorID are optional. Empty means
	// the default status and no owner or executor.
	Status       string
	OwnerID      string
	ExecutorKind string
	ExecutorID   string
}

type UpdateIssueCmd struct {
	UserID  string
	SpaceID string
	IssueID string
	// IfVersion is the Version the caller read. See coreissue.UpdateInput.
	IfVersion     uint64
	Title         *string
	Description   *string
	Status        *string
	OwnerID       *string
	ExecutorKind  *string
	ExecutorID    *string
	ParentIssueID *string
}

func (s *Service) CreateIssue(ctx context.Context, cmd CreateIssueCmd) (*coreissue.Issue, error) {
	if s.Issues == nil {
		return nil, ErrIssuesNotConfigured
	}
	if cmd.Title == "" {
		return nil, ErrTitleRequired
	}
	if cmd.SpaceID == "" {
		return nil, ErrSpacesNotConfigured
	}
	if cmd.Status != "" && !isValidStatus(cmd.Status) {
		return nil, ErrInvalidStatus
	}
	if err := s.validateOwner(ctx, cmd.SpaceID, &cmd.OwnerID); err != nil {
		return nil, err
	}
	if err := s.validateExecutor(ctx, cmd.SpaceID, &cmd.ExecutorKind, &cmd.ExecutorID); err != nil {
		return nil, err
	}
	parent, err := s.normalizeParent(ctx, cmd.SpaceID, "", cmd.ParentIssueID)
	if err != nil {
		return nil, err
	}
	return s.Issues.CreateIssueInSpace(ctx, cmd.SpaceID, cmd.UserID, coreissue.CreateInput{
		Title:         cmd.Title,
		Description:   cmd.Description,
		ParentIssueID: parent,
		Status:        cmd.Status,
		OwnerID:       cmd.OwnerID,
		ExecutorKind:  cmd.ExecutorKind,
		ExecutorID:    cmd.ExecutorID,
	})
}

func (s *Service) UpdateIssue(ctx context.Context, cmd UpdateIssueCmd) (*coreissue.Issue, error) {
	if s.Issues == nil {
		return nil, ErrIssuesNotConfigured
	}
	if cmd.IfVersion == 0 {
		return nil, ErrVersionRequired
	}
	if cmd.Status != nil && !isValidStatus(*cmd.Status) {
		return nil, ErrInvalidStatus
	}
	if cmd.SpaceID == "" {
		return nil, ErrSpacesNotConfigured
	}
	if err := s.validateOwner(ctx, cmd.SpaceID, cmd.OwnerID); err != nil {
		return nil, err
	}
	if err := s.validateExecutor(ctx, cmd.SpaceID, cmd.ExecutorKind, cmd.ExecutorID); err != nil {
		return nil, err
	}
	parent := cmd.ParentIssueID
	if parent != nil {
		var err error
		if parent, err = s.normalizeParent(ctx, cmd.SpaceID, cmd.IssueID, cmd.ParentIssueID); err != nil {
			return nil, err
		}
		// normalizeParent returns nil for a cleared parent; the store needs an
		// empty string to distinguish "clear it" from "leave it alone".
		if parent == nil {
			parent = new(string)
		}
	}
	issue, err := s.Issues.UpdateIssueInSpace(ctx, cmd.IssueID, cmd.SpaceID, coreissue.UpdateInput{
		IfVersion:     cmd.IfVersion,
		Title:         cmd.Title,
		Description:   cmd.Description,
		Status:        cmd.Status,
		OwnerID:       cmd.OwnerID,
		ExecutorKind:  cmd.ExecutorKind,
		ExecutorID:    cmd.ExecutorID,
		ParentIssueID: parent,
	})
	if err != nil {
		return nil, err
	}
	if issue == nil {
		return nil, ErrIssueNotFound
	}
	return issue, nil
}

// normalizeParent enforces the hierarchy invariants and returns the parent to
// persist: nil for "no parent", a pointer otherwise.
//
// childID is the issue being reparented, or "" when the child does not exist
// yet. A new issue cannot have children, so only the update path checks H3.
func (s *Service) normalizeParent(ctx context.Context, spaceID, childID string, parentIssueID *string) (*string, error) {
	if parentIssueID == nil || *parentIssueID == "" {
		return nil, nil
	}
	// H4: an issue cannot be its own parent.
	if childID != "" && *parentIssueID == childID {
		return nil, ErrInvalidParent
	}
	parent, err := s.Issues.GetIssue(ctx, *parentIssueID)
	if err != nil {
		return nil, err
	}
	// H1: the parent must exist and belong to the same space.
	if parent == nil || parent.SpaceID != spaceID {
		return nil, ErrParentNotFound
	}
	// H2: the hierarchy is two levels deep, so the parent must be top-level.
	if parent.ParentIssueID != nil && *parent.ParentIssueID != "" {
		return nil, ErrHierarchyTooDeep
	}
	// H3: an issue that already has children cannot become a child itself.
	if childID != "" {
		children, err := s.Issues.ListIssueChildren(ctx, childID)
		if err != nil {
			return nil, err
		}
		if len(children) > 0 {
			return nil, ErrIssueHasChildren
		}
	}
	return parentIssueID, nil
}

func isValidStatus(status string) bool {
	switch status {
	case coreissue.StatusTodo, coreissue.StatusInProgress, coreissue.StatusDone:
		return true
	default:
		return false
	}
}

// validateOwner confirms a nil, cleared, or non-empty OwnerID names a member
// of the space. Owner accountability is scoped to people who can actually be
// held to it.
func (s *Service) validateOwner(ctx context.Context, spaceID string, ownerID *string) error {
	if ownerID == nil || *ownerID == "" {
		return nil
	}
	if s.Spaces == nil {
		return ErrSpacesNotConfigured
	}
	members, err := s.Spaces.ListSpaceMembers(ctx, spaceID)
	if err != nil {
		return err
	}
	for _, member := range members {
		if member.UserID == *ownerID {
			return nil
		}
	}
	return ErrInvalidOwnerID
}

// validateExecutor confirms a nil-or-cleared Executor, or one naming a
// same-space Agent or a same-space published Workflow. It never admits a
// person: that is what OwnerID is for.
func (s *Service) validateExecutor(ctx context.Context, spaceID string, kind, id *string) error {
	if kind == nil && id == nil {
		return nil
	}
	if kind == nil || id == nil {
		return ErrInvalidExecutorID
	}
	if *kind == "" && *id == "" {
		return nil
	}
	switch *kind {
	case coreissue.ExecutorAgent:
		if *id == "" {
			return ErrInvalidExecutorID
		}
		if s.Agents == nil {
			return ErrAgentsNotConfigured
		}
		agent, err := s.Agents.GetAgent(ctx, *id)
		if err != nil {
			return err
		}
		if agent == nil || agent.SpaceID != spaceID {
			return ErrAgentNotFound
		}
		return nil
	case coreissue.ExecutorWorkflow:
		if *id == "" {
			return ErrInvalidExecutorID
		}
		if s.Workflows == nil {
			return ErrWorkflowsNotConfigured
		}
		workflow, err := s.Workflows.GetWorkflow(ctx, *id)
		if err != nil {
			return err
		}
		if workflow == nil || workflow.SpaceID != spaceID {
			return ErrWorkflowNotFound
		}
		if workflow.Status != coreworkflow.StatusPublished {
			return ErrWorkflowNotPublished
		}
		return nil
	default:
		return ErrInvalidExecutorKind
	}
}

// Counts are the derived numbers a list of issues carries: how many sub-issues
// each has, how many are done, how many comments.
type Counts struct {
	Children     int
	DoneChildren int
	Comments     int
}

// GetIssue resolves an issue the space owns.
//
// An issue belonging to another space reads as not found rather than forbidden,
// so the answer does not confirm that an id exists elsewhere.
func (s *Service) GetIssue(ctx context.Context, spaceID, issueID string) (*coreissue.Issue, error) {
	if s.Issues == nil {
		return nil, ErrIssuesNotConfigured
	}
	found, err := s.Issues.GetIssue(ctx, issueID)
	if err != nil {
		return nil, err
	}
	if found == nil || found.SpaceID != spaceID {
		return nil, ErrIssueNotFound
	}
	return found, nil
}

// ListChildren returns one issue's sub-issues, oldest first, after checking the
// parent belongs to the space. Callers that already hold the parent still go
// through here: the space check is the authorization, not a convenience.
func (s *Service) ListChildren(ctx context.Context, spaceID, issueID string) ([]coreissue.Issue, error) {
	if _, err := s.GetIssue(ctx, spaceID, issueID); err != nil {
		return nil, err
	}
	return s.Issues.ListIssueChildren(ctx, issueID)
}

func (s *Service) ListIssues(ctx context.Context, spaceID string, filter coreissue.ListFilter, limit, offset int) ([]coreissue.Issue, int, error) {
	if s.Issues == nil {
		return nil, 0, ErrIssuesNotConfigured
	}
	return s.Issues.ListIssuesBySpace(ctx, spaceID, filter, limit, offset)
}

// CountsFor loads the derived counts for a page of issues with one grouped
// query each, rather than a count per row.
//
// A failure degrades to zero for that count instead of failing the page: a
// missing progress badge is a worse-looking list, an error is no list at all.
// Both stores are optional, and a deployment without one simply reports zero.
func (s *Service) CountsFor(ctx context.Context, issueIDs []string) map[string]Counts {
	out := make(map[string]Counts, len(issueIDs))
	if len(issueIDs) == 0 {
		return out
	}
	if s.Issues != nil {
		stats, err := s.Issues.ChildStatsForIssues(ctx, issueIDs)
		if err != nil {
			slog.WarnContext(ctx, "issue child stats not loaded", "err", err)
		} else {
			for id, st := range stats {
				c := out[id]
				c.Children, c.DoneChildren = st.Total, st.Done
				out[id] = c
			}
		}
	}
	if s.Comments != nil {
		counts, err := s.Comments.CountIssueComments(ctx, issueIDs)
		if err != nil {
			slog.WarnContext(ctx, "issue comment counts not loaded", "err", err)
		} else {
			for id, n := range counts {
				c := out[id]
				c.Comments = n
				out[id] = c
			}
		}
	}
	return out
}
