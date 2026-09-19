package db

import (
	"testing"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
)

// CountIssueCommentsBySourceTaskRun backs the per-run comment budget: it must
// count exactly the comments a given run authored, treat an unknown run as
// zero, and keep two runs independent.
func TestCountIssueCommentsBySourceTaskRun(t *testing.T) {
	s, ctx := newTestStore(t)
	task, runID, _ := newRunForTest(t, s, "comment-budget")

	author := newTestUser(t, s, "issue-author")
	issue, err := s.CreateIssueInSpace(ctx, task.SpaceID, author, coreissue.CreateInput{Title: "Ship it"})
	if err != nil {
		t.Fatalf("CreateIssueInSpace: %v", err)
	}

	for range 2 {
		if _, err := s.CreateIssueComment(ctx, coreissue.CreateCommentInput{
			IssueID:         issue.ID,
			AuthorKind:      coreissue.CommentAuthorAgent,
			AuthorID:        "a_1",
			Body:            "progress",
			SourceTaskID:    &task.ID,
			SourceTaskRunID: &runID,
		}); err != nil {
			t.Fatalf("CreateIssueComment: %v", err)
		}
	}
	// A runless comment on the same issue must not count against the run.
	if _, err := s.CreateIssueComment(ctx, coreissue.CreateCommentInput{
		IssueID: issue.ID, AuthorKind: coreissue.CommentAuthorUser, AuthorID: author, Body: "a person's note",
	}); err != nil {
		t.Fatalf("CreateIssueComment (runless): %v", err)
	}

	got, err := s.CountIssueCommentsBySourceTaskRun(ctx, runID)
	if err != nil {
		t.Fatalf("CountIssueCommentsBySourceTaskRun: %v", err)
	}
	if got != 2 {
		t.Fatalf("count = %d, want 2", got)
	}

	// An unknown run has authored nothing, and is zero rather than an error.
	if n, err := s.CountIssueCommentsBySourceTaskRun(ctx, "tr_doesnotexist000000"); err != nil || n != 0 {
		t.Fatalf("unknown run count = %d, err = %v; want 0, nil", n, err)
	}
}
