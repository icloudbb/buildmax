package issue

import (
	"context"
	"errors"
	"strings"
	"testing"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	"github.com/icloudbb/buildmax/internal/mock"
)

func commentService(comments ...coreissue.Comment) (*Service, *mock.MockIssueCommentStore) {
	store := &mock.MockIssueCommentStore{Comments: comments}
	return &Service{Issues: &mock.MockIssueStore{}, Comments: store}, store
}

func TestCreateComment(t *testing.T) {
	svc, _ := commentService()
	comment, err := svc.CreateComment(context.Background(), CreateCommentCmd{
		IssueID:    "i_1",
		AuthorKind: coreissue.CommentAuthorUser,
		AuthorID:   "u1",
		Body:       "  blocked on the vendor  ",
	})
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	if comment.Body != "blocked on the vendor" {
		t.Fatalf("comment.Body = %q, want the trimmed text", comment.Body)
	}
	if comment.EditedAt != nil {
		t.Fatalf("a new comment reports edited_at = %v, want nil", *comment.EditedAt)
	}
}

func TestCreateComment_BodyRequired(t *testing.T) {
	svc, store := commentService()
	_, err := svc.CreateComment(context.Background(), CreateCommentCmd{IssueID: "i_1", AuthorID: "u1", Body: "   \n "})
	if !errors.Is(err, ErrCommentBodyRequired) {
		t.Fatalf("err = %v, want %v", err, ErrCommentBodyRequired)
	}
	if len(store.Comments) != 0 {
		t.Fatalf("whitespace-only body still wrote a row")
	}
}

func TestCreateComment_TooLong(t *testing.T) {
	svc, _ := commentService()
	_, err := svc.CreateComment(context.Background(), CreateCommentCmd{
		IssueID:  "i_1",
		AuthorID: "u1",
		Body:     strings.Repeat("x", CommentBodyLimit+1),
	})
	if !errors.Is(err, ErrCommentTooLong) {
		t.Fatalf("err = %v, want %v", err, ErrCommentTooLong)
	}
}

// An Agent's report is held to the stricter AgentCommentBodyLimit, enforced
// here so it binds every Agent client — the runtime tool and the CLI bridge —
// not one of them.
func TestCreateComment_AgentBodyLimit(t *testing.T) {
	for _, kind := range []string{coreissue.CommentAuthorAgent, coreissue.CommentAuthorLocalAgent} {
		svc, store := commentService()
		_, err := svc.CreateComment(context.Background(), CreateCommentCmd{
			IssueID:    "i_1",
			AuthorKind: kind,
			AuthorID:   "a_1",
			Body:       strings.Repeat("x", AgentCommentBodyLimit+1),
		})
		if !errors.Is(err, ErrCommentTooLong) {
			t.Fatalf("%s: err = %v, want %v", kind, err, ErrCommentTooLong)
		}
		if len(store.Comments) != 0 {
			t.Fatalf("%s: an over-limit report still wrote a row", kind)
		}
	}
}

// A person is bound only by the universal CommentBodyLimit, not the stricter
// Agent limit: a human comment longer than an Agent's cap is fine.
func TestCreateComment_UserNotBoundByAgentLimit(t *testing.T) {
	svc, _ := commentService()
	_, err := svc.CreateComment(context.Background(), CreateCommentCmd{
		IssueID:    "i_1",
		AuthorKind: coreissue.CommentAuthorUser,
		AuthorID:   "u1",
		Body:       strings.Repeat("x", AgentCommentBodyLimit+1),
	})
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
}

// The Agent limit measures the author's own words, before the server appends
// Artifact references, so a report at the limit that names an Artifact is not
// rejected by the identifier the server adds.
func TestCreateComment_ArtifactRefsDoNotCountAgainstLimit(t *testing.T) {
	svc, _ := commentService()
	comment, err := svc.CreateComment(context.Background(), CreateCommentCmd{
		IssueID:     "i_1",
		AuthorKind:  coreissue.CommentAuthorAgent,
		AuthorID:    "a_1",
		Body:        strings.Repeat("x", AgentCommentBodyLimit),
		ArtifactIDs: []string{" art_1 ", "", "art_2"},
	})
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	if !strings.Contains(comment.Body, "Artifacts: art_1, art_2") {
		t.Fatalf("body does not record the artifacts by identity: %q", comment.Body)
	}
	if len([]rune(comment.Body)) <= AgentCommentBodyLimit {
		t.Fatalf("expected the stored body to exceed the agent limit once refs are appended")
	}
}

// A run may add at most RunCommentBudget comments to its Issue, counted by
// source_task_run_id so the limit holds across restarts and every client.
func TestCreateComment_RunBudget(t *testing.T) {
	run := "run-1"
	seed := make([]coreissue.Comment, 0, RunCommentBudget)
	for i := range RunCommentBudget {
		r := run
		seed = append(seed, coreissue.Comment{
			ID: "ic_" + string(rune('a'+i)), IssueID: "i_1",
			AuthorKind: coreissue.CommentAuthorAgent, AuthorID: "a_1", Body: "progress",
			SourceTaskRunID: &r,
		})
	}
	svc, store := commentService(seed...)

	_, err := svc.CreateComment(context.Background(), CreateCommentCmd{
		IssueID: "i_1", AuthorKind: coreissue.CommentAuthorAgent, AuthorID: "a_1",
		Body: "one more", SourceTaskRunID: &run,
	})
	if !errors.Is(err, ErrRunCommentBudgetExhausted) {
		t.Fatalf("err = %v, want %v", err, ErrRunCommentBudgetExhausted)
	}
	if len(store.Comments) != RunCommentBudget {
		t.Fatalf("a refused comment still wrote a row: have %d", len(store.Comments))
	}

	// A different run is budgeted independently.
	other := "run-2"
	if _, err := svc.CreateComment(context.Background(), CreateCommentCmd{
		IssueID: "i_1", AuthorKind: coreissue.CommentAuthorAgent, AuthorID: "a_1",
		Body: "first from run-2", SourceTaskRunID: &other,
	}); err != nil {
		t.Fatalf("another run's first comment was refused: %v", err)
	}
}

// A runless comment (no source run) is not budgeted: the local path has no run
// to count against.
func TestCreateComment_RunlessIsUnbudgeted(t *testing.T) {
	svc, _ := commentService()
	for i := range RunCommentBudget + 2 {
		if _, err := svc.CreateComment(context.Background(), CreateCommentCmd{
			IssueID: "i_1", AuthorKind: coreissue.CommentAuthorLocalAgent, AuthorID: "a_1",
			Body: "local report",
		}); err != nil {
			t.Fatalf("runless comment %d refused: %v", i, err)
		}
	}
}

func TestCreateComment_NotConfigured(t *testing.T) {
	svc := &Service{Issues: &mock.MockIssueStore{}}
	_, err := svc.CreateComment(context.Background(), CreateCommentCmd{IssueID: "i_1", AuthorID: "u1", Body: "hi"})
	if !errors.Is(err, ErrCommentsNotConfigured) {
		t.Fatalf("err = %v, want %v", err, ErrCommentsNotConfigured)
	}
}

func TestUpdateComment_StampsEditedAt(t *testing.T) {
	svc, _ := commentService(coreissue.Comment{
		ID: "ic_1", IssueID: "i_1",
		AuthorKind: coreissue.CommentAuthorUser, AuthorID: "u1", Body: "first",
	})
	updated, err := svc.UpdateComment(context.Background(), UpdateCommentCmd{
		IssueID: "i_1", CommentID: "ic_1", UserID: "u1", Body: "second",
	})
	if err != nil {
		t.Fatalf("UpdateComment: %v", err)
	}
	if updated.Body != "second" {
		t.Fatalf("updated.Body = %q", updated.Body)
	}
	if updated.EditedAt == nil {
		t.Fatal("updated.EditedAt is nil; an edited comment must say so")
	}
}

func TestUpdateComment_OnlyTheAuthor(t *testing.T) {
	svc, _ := commentService(coreissue.Comment{
		ID: "ic_1", IssueID: "i_1",
		AuthorKind: coreissue.CommentAuthorUser, AuthorID: "u1", Body: "first",
	})
	_, err := svc.UpdateComment(context.Background(), UpdateCommentCmd{
		IssueID: "i_1", CommentID: "ic_1", UserID: "u2", Body: "rewritten",
	})
	if !errors.Is(err, ErrCommentNotEditable) {
		t.Fatalf("err = %v, want %v", err, ErrCommentNotEditable)
	}
}

// Moderation permits deletion, not rewriting: an agent comment is the record of
// what a run reported.
func TestUpdateComment_AgentCommentIsNotEditable(t *testing.T) {
	svc, _ := commentService(coreissue.Comment{
		ID: "ic_1", IssueID: "i_1",
		AuthorKind: coreissue.CommentAuthorAgent, AuthorID: "a_1", Body: "run finished",
	})
	_, err := svc.UpdateComment(context.Background(), UpdateCommentCmd{
		IssueID: "i_1", CommentID: "ic_1", UserID: "a_1", Body: "run failed",
	})
	if !errors.Is(err, ErrCommentNotEditable) {
		t.Fatalf("err = %v, want %v", err, ErrCommentNotEditable)
	}
}

// A comment ID that belongs to another issue is not found, rather than a
// successful write to somewhere the caller was never authorized against.
func TestUpdateComment_WrongIssue(t *testing.T) {
	svc, _ := commentService(coreissue.Comment{
		ID: "ic_1", IssueID: "i_other",
		AuthorKind: coreissue.CommentAuthorUser, AuthorID: "u1", Body: "first",
	})
	_, err := svc.UpdateComment(context.Background(), UpdateCommentCmd{
		IssueID: "i_1", CommentID: "ic_1", UserID: "u1", Body: "second",
	})
	if !errors.Is(err, ErrCommentNotFound) {
		t.Fatalf("err = %v, want %v", err, ErrCommentNotFound)
	}
}

func TestDeleteComment_Author(t *testing.T) {
	svc, store := commentService(coreissue.Comment{
		ID: "ic_1", IssueID: "i_1",
		AuthorKind: coreissue.CommentAuthorUser, AuthorID: "u1", Body: "first",
	})
	if err := svc.DeleteComment(context.Background(), DeleteCommentCmd{
		IssueID: "i_1", CommentID: "ic_1", UserID: "u1",
	}); err != nil {
		t.Fatalf("DeleteComment: %v", err)
	}
	if len(store.Comments) != 0 {
		t.Fatalf("comment survived its author's delete")
	}
}

func TestDeleteComment_StrangerRefused(t *testing.T) {
	svc, store := commentService(coreissue.Comment{
		ID: "ic_1", IssueID: "i_1",
		AuthorKind: coreissue.CommentAuthorUser, AuthorID: "u1", Body: "first",
	})
	err := svc.DeleteComment(context.Background(), DeleteCommentCmd{
		IssueID: "i_1", CommentID: "ic_1", UserID: "u2",
	})
	if !errors.Is(err, ErrCommentNotEditable) {
		t.Fatalf("err = %v, want %v", err, ErrCommentNotEditable)
	}
	if len(store.Comments) != 1 {
		t.Fatalf("refused delete still removed the comment")
	}
}

func TestDeleteComment_ModeratorMayRemoveAnother(t *testing.T) {
	svc, store := commentService(coreissue.Comment{
		ID: "ic_1", IssueID: "i_1",
		AuthorKind: coreissue.CommentAuthorAgent, AuthorID: "a_1", Body: "run finished",
	})
	if err := svc.DeleteComment(context.Background(), DeleteCommentCmd{
		IssueID: "i_1", CommentID: "ic_1", UserID: "u_owner", CanModerate: true,
	}); err != nil {
		t.Fatalf("DeleteComment: %v", err)
	}
	if len(store.Comments) != 0 {
		t.Fatalf("moderator delete left the comment in place")
	}
}

func TestListComments(t *testing.T) {
	svc, _ := commentService(
		coreissue.Comment{ID: "ic_1", IssueID: "i_1", Body: "one"},
		coreissue.Comment{ID: "ic_2", IssueID: "i_other", Body: "two"},
		coreissue.Comment{ID: "ic_3", IssueID: "i_1", Body: "three"},
	)
	list, total, err := svc.ListComments(context.Background(), "i_1", 50, 0)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Fatalf("total = %d, len = %d, want 2 and 2", total, len(list))
	}
	if list[0].Body != "one" || list[1].Body != "three" {
		t.Fatalf("thread order = %q, %q; want oldest first", list[0].Body, list[1].Body)
	}
}
