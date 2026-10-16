// Package needsme answers one question for one person in one Space: what is
// waiting on them. It is the single definition Portal's landing section and
// Desktop's Home both read, so neither recombines list filters of its own.
// See docs/design/portal-navigation-and-space-context.md, "Landing on the work
// that needs you".
package needsme

import (
	"context"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
)

// GroupLimit bounds every group. A landing section lists what to act on next;
// the total says how much more there is.
const GroupLimit = 10

var (
	ErrNotConfigured = apierr.New(apierr.KindNotConfigured, "issues not configured")
	// ErrNotMember is refused like every other Space read: the caller learns
	// nothing about a Space they do not belong to.
	ErrNotMember = apierr.New(apierr.KindForbidden, "space not found")
)

// The narrow reads this service needs. Each store owns its query; this
// package owns which of them make up "needs me".
type (
	IssueReader interface {
		ListIssuesNeedingOwner(ctx context.Context, spaceID, ownerID string, limit int) ([]coreissue.Attention, int, error)
	}
	TaskReader interface {
		ListTasksWaitingOn(ctx context.Context, spaceID, userID string, wait coretask.Wait, limit int) ([]coretask.Task, int, error)
	}
	RequestReader interface {
		ListPendingWorkflowRequestsBySpace(ctx context.Context, spaceID string, limit, offset int) ([]coreworkflow.Request, int, error)
	}
	MemberReader interface {
		ListSpaceMembers(ctx context.Context, spaceID string) ([]corespace.Member, error)
	}
)

type Service struct {
	Issues IssueReader
	// Tasks and Requests may be nil on a deployment without them, which
	// leaves their groups empty rather than failing the read.
	Tasks    TaskReader
	Requests RequestReader
	Members  MemberReader
}

// Group is up to GroupLimit items and how many matched in all.
type Group[T any] struct {
	Items []T
	Total int
}

// Result is the four groups. A run that both failed and belongs to an
// attention Issue appears in both: one names the work, the other the run.
type Result struct {
	// AttentionIssues are open Issues the caller owns whose latest run failed
	// or awaits an answer, or that have a pending Workflow request.
	AttentionIssues Group[coreissue.Attention]
	// FailedTasks and AwaitingAnswerTasks are the caller's Tasks whose latest
	// run failed, or ended on questions. "The caller's" is created by them,
	// requested by them through a Space Assistant, or started on an Issue they
	// own.
	FailedTasks         Group[coretask.Task]
	AwaitingAnswerTasks Group[coretask.Task]
	// WorkflowRequests are every pending request in the Space, oldest first,
	// when the caller may answer them: no request names a responder, and the
	// first answer wins.
	WorkflowRequests Group[coreworkflow.Request]
}

// NeedsMe returns what in spaceID is waiting on userID.
func (s *Service) NeedsMe(ctx context.Context, spaceID, userID string) (*Result, error) {
	if s.Issues == nil || s.Members == nil {
		return nil, ErrNotConfigured
	}
	members, err := s.Members.ListSpaceMembers(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	role := corespace.EffectiveRoleOf(members, userID)
	if role == "" {
		return nil, ErrNotMember
	}

	out := &Result{}
	issues, total, err := s.Issues.ListIssuesNeedingOwner(ctx, spaceID, userID, GroupLimit)
	if err != nil {
		return nil, err
	}
	out.AttentionIssues = Group[coreissue.Attention]{Items: issues, Total: total}
	if s.Tasks != nil {
		if out.FailedTasks, err = s.waitingTasks(ctx, spaceID, userID, coretask.WaitFailed); err != nil {
			return nil, err
		}
		if out.AwaitingAnswerTasks, err = s.waitingTasks(ctx, spaceID, userID, coretask.WaitAnswer); err != nil {
			return nil, err
		}
	}
	if s.Requests != nil && corespace.Allows(role, corespace.ActionRunWorkflow) {
		requests, total, err := s.Requests.ListPendingWorkflowRequestsBySpace(ctx, spaceID, GroupLimit, 0)
		if err != nil {
			return nil, err
		}
		out.WorkflowRequests = Group[coreworkflow.Request]{Items: requests, Total: total}
	}
	return out, nil
}

func (s *Service) waitingTasks(ctx context.Context, spaceID, userID string, wait coretask.Wait) (Group[coretask.Task], error) {
	tasks, total, err := s.Tasks.ListTasksWaitingOn(ctx, spaceID, userID, wait, GroupLimit)
	return Group[coretask.Task]{Items: tasks, Total: total}, err
}
