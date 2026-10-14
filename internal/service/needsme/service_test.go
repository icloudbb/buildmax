package needsme

import (
	"context"
	"errors"
	"testing"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
)

type fakeReads struct {
	members []corespace.Member
	limits  []int
	waits   []coretask.Wait
}

func (f *fakeReads) ListSpaceMembers(context.Context, string) ([]corespace.Member, error) {
	return f.members, nil
}

func (f *fakeReads) ListIssuesNeedingOwner(_ context.Context, _, ownerID string, limit int) ([]coreissue.Attention, int, error) {
	f.limits = append(f.limits, limit)
	return []coreissue.Attention{{Issue: coreissue.Issue{ID: "i_" + ownerID}, Reason: coreissue.AttentionRunFailed}}, 12, nil
}

func (f *fakeReads) ListTasksWaitingOn(_ context.Context, _, _ string, wait coretask.Wait, limit int) ([]coretask.Task, int, error) {
	f.limits = append(f.limits, limit)
	f.waits = append(f.waits, wait)
	return []coretask.Task{{ID: "t_" + string(wait)}}, 1, nil
}

func (f *fakeReads) ListPendingWorkflowRequestsBySpace(_ context.Context, _ string, limit, offset int) ([]coreworkflow.Request, int, error) {
	f.limits = append(f.limits, limit)
	return []coreworkflow.Request{{ID: "wrq_1"}}, 1, nil
}

func TestNeedsMeReadsEachGroupBoundedForTheCaller(t *testing.T) {
	reads := &fakeReads{members: []corespace.Member{{UserID: "u_alice", Role: corespace.RoleMember}}}
	svc := &Service{Issues: reads, Tasks: reads, Requests: reads, Members: reads}
	got, err := svc.NeedsMe(t.Context(), "tm_1", "u_alice")
	if err != nil {
		t.Fatalf("NeedsMe: %v", err)
	}
	if got.AttentionIssues.Total != 12 || got.AttentionIssues.Items[0].Issue.ID != "i_u_alice" {
		t.Errorf("attention issues = %+v, want the caller's with the store's total", got.AttentionIssues)
	}
	if got.FailedTasks.Items[0].ID != "t_failed" || got.AwaitingAnswerTasks.Items[0].ID != "t_answer" {
		t.Errorf("failed = %+v, awaiting = %+v", got.FailedTasks, got.AwaitingAnswerTasks)
	}
	if got.WorkflowRequests.Total != 1 {
		t.Errorf("workflow requests = %+v, want the Space's pending request", got.WorkflowRequests)
	}
	for _, limit := range reads.limits {
		if limit != GroupLimit {
			t.Errorf("a group was read with limit %d, want %d", limit, GroupLimit)
		}
	}
}

func TestNeedsMeRefusesANonMember(t *testing.T) {
	reads := &fakeReads{members: []corespace.Member{{UserID: "u_alice", Role: corespace.RoleOwner}}}
	svc := &Service{Issues: reads, Tasks: reads, Requests: reads, Members: reads}
	if _, err := svc.NeedsMe(t.Context(), "tm_1", "u_stranger"); !errors.Is(err, ErrNotMember) {
		t.Fatalf("err = %v, want ErrNotMember", err)
	}
	if len(reads.limits) != 0 {
		t.Fatal("a non-member's read reached the stores")
	}
}

// A deployment without Tasks or Workflows still answers, with those groups
// empty rather than the whole read failing.
func TestNeedsMeWithoutTasksOrWorkflows(t *testing.T) {
	reads := &fakeReads{members: []corespace.Member{{UserID: "u_alice"}}}
	got, err := (&Service{Issues: reads, Members: reads}).NeedsMe(t.Context(), "tm_1", "u_alice")
	if err != nil {
		t.Fatalf("NeedsMe: %v", err)
	}
	if got.FailedTasks.Total != 0 || got.AwaitingAnswerTasks.Total != 0 || got.WorkflowRequests.Total != 0 {
		t.Fatalf("got %+v, want empty run and request groups", got)
	}
}
