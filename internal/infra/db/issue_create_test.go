package db

import (
	"testing"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
)

// TestCreateIssueWritesItsDetailsInOneRow covers the create Portal relies on:
// status, owner, and executor land with the row, and an owner that does not
// resolve leaves no row behind.
func TestCreateIssueWritesItsDetailsInOneRow(t *testing.T) {
	s, ctx := newTestStore(t)
	owner := newTestUser(t, s, "issue-atomic-owner")
	space := newTestSpace(t, s, owner)

	created, err := s.CreateIssueInSpace(ctx, space, owner, coreissue.CreateInput{
		Title:        "Assigned at birth",
		Status:       coreissue.StatusInProgress,
		OwnerID:      owner,
		ExecutorKind: coreissue.ExecutorAgent,
		ExecutorID:   "ag_opaque",
	})
	if err != nil {
		t.Fatalf("CreateIssueInSpace: %v", err)
	}
	stored, err := s.GetIssue(ctx, created.ID)
	if err != nil || stored == nil {
		t.Fatalf("GetIssue = %v, %v", stored, err)
	}
	for _, got := range []*coreissue.Issue{created, stored} {
		if got.Status != coreissue.StatusInProgress {
			t.Errorf("status = %q, want %q", got.Status, coreissue.StatusInProgress)
		}
		if got.OwnerID == nil || *got.OwnerID != owner {
			t.Errorf("owner = %v, want %s", got.OwnerID, owner)
		}
		if got.ExecutorKind == nil || *got.ExecutorKind != coreissue.ExecutorAgent ||
			got.ExecutorID == nil || *got.ExecutorID != "ag_opaque" {
			t.Errorf("executor = %v/%v, want agent/ag_opaque", got.ExecutorKind, got.ExecutorID)
		}
		if got.Version != 1 {
			t.Errorf("version = %d, want 1", got.Version)
		}
	}

	_, before, err := s.ListIssuesBySpace(ctx, space, coreissue.ListFilter{}, 50, 0)
	if err != nil {
		t.Fatalf("ListIssuesBySpace: %v", err)
	}
	if _, err := s.CreateIssueInSpace(ctx, space, owner, coreissue.CreateInput{Title: "No such owner", OwnerID: "us_missing"}); err == nil {
		t.Fatal("create with an unknown owner succeeded")
	}
	_, after, err := s.ListIssuesBySpace(ctx, space, coreissue.ListFilter{}, 50, 0)
	if err != nil {
		t.Fatalf("ListIssuesBySpace: %v", err)
	}
	if after != before {
		t.Fatalf("issue count went %d -> %d after a refused create", before, after)
	}
}
