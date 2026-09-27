package issue

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/util"
)

func reporterFor(task coretask.Task, comments *mock.MockIssueCommentStore) *RunReporter {
	return &RunReporter{
		Tasks:    &mock.MockTaskStore{List: []coretask.Task{task}},
		Comments: comments,
	}
}

func TestReportRunTerminal_WritesOneAgentComment(t *testing.T) {
	comments := &mock.MockIssueCommentStore{}
	reporter := reporterFor(coretask.Task{
		ID:      "t_1",
		IssueID: util.Ptr("i_1"),
		AgentID: util.Ptr("a_1"),
	}, comments)
	err := reporter.ReportRunTerminal(context.Background(), coretask.RunTerminalInfo{
		TaskRunID: "r_1",
		TaskID:    "t_1",
		Status:    string(coretask.RunStatusSucceeded),
		Output:    util.Ptr("Shipped the migration."),
	})
	if err != nil {
		t.Fatalf("ReportRunTerminal: %v", err)
	}
	if len(comments.Comments) != 1 {
		t.Fatalf("wrote %d comments, want exactly 1 per terminal run", len(comments.Comments))
	}
	got := comments.Comments[0]
	if got.AuthorKind != coreissue.CommentAuthorAgent || got.AuthorID != "a_1" {
		t.Fatalf("author = %s/%s, want agent/a_1", got.AuthorKind, got.AuthorID)
	}
	if got.SourceTaskRunID == nil || *got.SourceTaskRunID != "r_1" {
		t.Fatalf("source_task_run_id = %v, want r_1", got.SourceTaskRunID)
	}
	if got.Body != "Shipped the migration." {
		t.Fatalf("body = %q", got.Body)
	}
}

// A task with no issue has nowhere to report, and a run that said nothing is
// not worth a line in the thread.
func TestReportRunTerminal_SilentCases(t *testing.T) {
	cases := []struct {
		name string
		task coretask.Task
		info coretask.RunTerminalInfo
	}{
		{
			name: "task not on an issue",
			task: coretask.Task{ID: "t_1"},
			info: coretask.RunTerminalInfo{TaskID: "t_1", Status: string(coretask.RunStatusSucceeded), Output: util.Ptr("done")},
		},
		{
			name: "success with no output",
			task: coretask.Task{ID: "t_1", IssueID: util.Ptr("i_1")},
			info: coretask.RunTerminalInfo{TaskID: "t_1", Status: string(coretask.RunStatusSucceeded)},
		},
		{
			name: "success with blank output",
			task: coretask.Task{ID: "t_1", IssueID: util.Ptr("i_1")},
			info: coretask.RunTerminalInfo{TaskID: "t_1", Status: string(coretask.RunStatusSucceeded), Output: util.Ptr("  \n ")},
		},
		{
			name: "failure with no message",
			task: coretask.Task{ID: "t_1", IssueID: util.Ptr("i_1")},
			info: coretask.RunTerminalInfo{TaskID: "t_1", Status: string(coretask.RunStatusFailed)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			comments := &mock.MockIssueCommentStore{}
			if err := reporterFor(tc.task, comments).ReportRunTerminal(context.Background(), tc.info); err != nil {
				t.Fatalf("ReportRunTerminal: %v", err)
			}
			if len(comments.Comments) != 0 {
				t.Fatalf("wrote %d comments, want none", len(comments.Comments))
			}
		})
	}
}

func TestReportRunTerminal_FailureReportsTheError(t *testing.T) {
	comments := &mock.MockIssueCommentStore{}
	reporter := reporterFor(coretask.Task{ID: "t_1", IssueID: util.Ptr("i_1"), AgentID: util.Ptr("a_1")}, comments)
	if err := reporter.ReportRunTerminal(context.Background(), coretask.RunTerminalInfo{
		TaskRunID:    "r_1",
		TaskID:       "t_1",
		Status:       string(coretask.RunStatusFailed),
		ErrorMessage: util.Ptr("model refused the tool call"),
	}); err != nil {
		t.Fatalf("ReportRunTerminal: %v", err)
	}
	if len(comments.Comments) != 1 {
		t.Fatalf("wrote %d comments, want 1", len(comments.Comments))
	}
	if !strings.Contains(comments.Comments[0].Body, "model refused the tool call") {
		t.Fatalf("body = %q, want the error message", comments.Comments[0].Body)
	}
}

func TestReportRunTerminal_TruncatesLongOutput(t *testing.T) {
	comments := &mock.MockIssueCommentStore{}
	reporter := reporterFor(coretask.Task{ID: "t_1", IssueID: util.Ptr("i_1")}, comments)
	if err := reporter.ReportRunTerminal(context.Background(), coretask.RunTerminalInfo{
		TaskRunID: "r_1",
		TaskID:    "t_1",
		Status:    string(coretask.RunStatusSucceeded),
		Output:    util.Ptr(strings.Repeat("é", runSummaryLimit)),
	}); err != nil {
		t.Fatalf("ReportRunTerminal: %v", err)
	}
	body := comments.Comments[0].Body
	if !strings.Contains(body, "truncated") {
		t.Fatalf("long output was not marked truncated: %q", body[max(0, len(body)-80):])
	}
	// The cut lands on a rune boundary, never inside a multi-byte character.
	if !utf8ValidPrefix(body) {
		t.Fatalf("truncation split a multi-byte character")
	}
}

// A run waiting on the user closes its output with the questions, so a long
// output keeps its end in the comment rather than cutting the questions off.
func TestReportRunTerminal_WaitingRunKeepsItsQuestions(t *testing.T) {
	comments := &mock.MockIssueCommentStore{}
	reporter := reporterFor(coretask.Task{ID: "t_1", IssueID: util.Ptr("i_1")}, comments)
	out := strings.Repeat("é", runSummaryLimit) + "\n\n**Waiting for your answer**\n\n1. Which database?"
	if err := reporter.ReportRunTerminal(context.Background(), coretask.RunTerminalInfo{
		TaskRunID: "r_1", TaskID: "t_1", Status: string(coretask.RunStatusSucceeded), Output: &out, AwaitingAnswer: true,
	}); err != nil {
		t.Fatalf("ReportRunTerminal: %v", err)
	}
	body := comments.Comments[0].Body
	if !strings.Contains(body, "1. Which database?") || !strings.Contains(body, "earlier output truncated") {
		t.Fatalf("waiting run lost its questions: %q", body[:min(len(body), 120)])
	}
	if !utf8.ValidString(body) {
		t.Fatal("truncation split a multi-byte character")
	}
}

// A failed comment write must not surface as a failed run. The reporter returns
// the error so the caller can log it; the caller is what decides it is not
// fatal, and internal/server/server.go logs and continues.
func TestReportRunTerminal_StoreFailureIsReturnedNotSwallowed(t *testing.T) {
	sentinel := errors.New("comment store down")
	comments := &mock.MockIssueCommentStore{CreateErr: sentinel}
	reporter := reporterFor(coretask.Task{ID: "t_1", IssueID: util.Ptr("i_1")}, comments)
	err := reporter.ReportRunTerminal(context.Background(), coretask.RunTerminalInfo{
		TaskRunID: "r_1",
		TaskID:    "t_1",
		Status:    string(coretask.RunStatusSucceeded),
		Output:    util.Ptr("done"),
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want %v", err, sentinel)
	}
}

func TestReportRunTerminal_NoStoreIsANoop(t *testing.T) {
	reporter := &RunReporter{}
	if err := reporter.ReportRunTerminal(context.Background(), coretask.RunTerminalInfo{TaskID: "t_1"}); err != nil {
		t.Fatalf("ReportRunTerminal with no stores: %v", err)
	}
}

func utf8ValidPrefix(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}
