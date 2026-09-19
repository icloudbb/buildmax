package issue

import (
	"context"
	"errors"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/agentdef"
	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/mock"
)

func createService(store *mock.MockIssueStore) *Service {
	return &Service{
		Issues: store,
		Spaces: &mock.MockSpaceStore{Members: []corespace.Member{{SpaceID: "tm_1", UserID: "u1", Role: corespace.RoleOwner}}},
		Agents: &mock.MockAgentStore{
			Agents: []agentdef.Agent{
				{ID: "a_1", UserID: "u1", SpaceID: "tm_1", Name: "Agent 1"},
				{ID: "a_other", UserID: "u2", SpaceID: "tm_2", Name: "Other Agent"},
			},
		},
	}
}

func TestCreateIssue_WithStatusOwnerAndExecutor(t *testing.T) {
	issue, err := createService(&mock.MockIssueStore{}).CreateIssue(context.Background(), CreateIssueCmd{
		UserID:       "u1",
		SpaceID:      "tm_1",
		Title:        "Ship it",
		Status:       coreissue.StatusInProgress,
		OwnerID:      "u1",
		ExecutorKind: coreissue.ExecutorAgent,
		ExecutorID:   "a_1",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if issue.Status != coreissue.StatusInProgress {
		t.Errorf("status = %q, want %q", issue.Status, coreissue.StatusInProgress)
	}
	if issue.OwnerID == nil || *issue.OwnerID != "u1" {
		t.Errorf("owner = %v, want u1", issue.OwnerID)
	}
	if issue.ExecutorKind == nil || *issue.ExecutorKind != coreissue.ExecutorAgent ||
		issue.ExecutorID == nil || *issue.ExecutorID != "a_1" {
		t.Errorf("executor = %v/%v, want agent/a_1", issue.ExecutorKind, issue.ExecutorID)
	}
}

// Create is one store write, so a refused detail must leave nothing behind for
// the caller to find and finish.
func TestCreateIssue_RefusedDetailsCreateNothing(t *testing.T) {
	cases := []struct {
		name string
		cmd  CreateIssueCmd
		want error
	}{
		{"status", CreateIssueCmd{Status: "blocked"}, ErrInvalidStatus},
		{"owner outside the space", CreateIssueCmd{OwnerID: "u2"}, ErrInvalidOwnerID},
		{"agent in another space", CreateIssueCmd{ExecutorKind: coreissue.ExecutorAgent, ExecutorID: "a_other"}, ErrAgentNotFound},
		{"executor kind without id", CreateIssueCmd{ExecutorKind: coreissue.ExecutorAgent}, ErrInvalidExecutorID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &mock.MockIssueStore{}
			cmd := tc.cmd
			cmd.UserID, cmd.SpaceID, cmd.Title = "u1", "tm_1", "Ship it"
			_, err := createService(store).CreateIssue(context.Background(), cmd)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(store.Issues) != 0 {
				t.Fatalf("store holds %d issues after a refused create, want 0", len(store.Issues))
			}
		})
	}
}
