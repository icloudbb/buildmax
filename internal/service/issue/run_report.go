package issue

import (
	"context"
	"strings"
	"unicode/utf8"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

// runSummaryLimit bounds the body of an agent-authored comment.
//
// The thread carries a statement that a run finished and what it said, not the
// run's output. The full text stays where it already lives — the task's output
// and its artifacts, aggregated into the issue's outputs — so raising this
// limit would duplicate content rather than reveal any.
const runSummaryLimit = 2000

// RunReporter posts an agent comment on the issue a finished task run belongs
// to.
//
// It is driven by the terminal-run callback, which fires after the worker has
// already been answered. Every failure path here returns without an error
// reaching that response: a comment that could not be written must not turn a
// completed run into a failed one.
type RunReporter struct {
	Tasks    coretask.Store
	Comments coreissue.CommentStore
}

// ReportRunTerminal writes one comment for a run that reached a terminal
// status, and nothing at all when there is no issue, no store, or nothing to
// say.
//
// One comment per terminal run is the whole budget. A run that streamed for
// twenty minutes still produces one line in the thread.
func (r *RunReporter) ReportRunTerminal(ctx context.Context, info coretask.RunTerminalInfo) error {
	if r == nil || r.Tasks == nil || r.Comments == nil {
		return nil
	}
	task, err := r.Tasks.GetTask(ctx, info.TaskID)
	if err != nil {
		return err
	}
	if task == nil || task.IssueID == nil || *task.IssueID == "" {
		return nil
	}
	body := runSummaryBody(info)
	if body == "" {
		return nil
	}
	taskID := info.TaskID
	runID := info.TaskRunID
	_, err = r.Comments.CreateIssueComment(ctx, coreissue.CreateCommentInput{
		IssueID:         *task.IssueID,
		AuthorKind:      coreissue.CommentAuthorAgent,
		AuthorID:        agentIDOf(task),
		Body:            body,
		SourceTaskID:    &taskID,
		SourceTaskRunID: &runID,
	})
	return err
}

func agentIDOf(task *coretask.Task) string {
	if task.AgentID != nil {
		return *task.AgentID
	}
	return ""
}

// runSummaryBody renders what the run reported. It returns "" for a run that
// said nothing — a silent success is not worth a comment, and a thread of
// content-free notifications is what makes people stop reading one.
func runSummaryBody(info coretask.RunTerminalInfo) string {
	if info.Status == string(coretask.RunStatusFailed) {
		if info.ErrorMessage == nil {
			return ""
		}
		detail := strings.TrimSpace(*info.ErrorMessage)
		if detail == "" {
			return ""
		}
		return "Run failed.\n\n" + truncateRunes(detail, runSummaryLimit)
	}
	if info.Output == nil {
		return ""
	}
	detail := strings.TrimSpace(*info.Output)
	if detail == "" {
		return ""
	}
	if info.AwaitingAnswer {
		// The questions ask for a reply, and the thread is where they appear,
		// but a comment does not reach the run. Say where the answer goes.
		return waitingLead + keepEnd(detail, runSummaryLimit)
	}
	return truncateRunes(detail, runSummaryLimit)
}

// waitingLead opens the report of a run that stopped on questions.
const waitingLead = "The agent is waiting for your answer. Reply by continuing this run's task (Open Task); a comment on this issue does not reach the agent.\n\n"

// keepEnd keeps the end of a run's output that is waiting on the user: its
// questions close the output, and they are what the reader has to act on.
func keepEnd(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := s[len(s)-limit:]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[1:]
	}
	return "…earlier output truncated; see the issue's results for all of it.\n\n" + cut
}

// truncateRunes cuts on a rune boundary so a multi-byte character is never
// split in half.
func truncateRunes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	trimmed := s[:limit]
	for len(trimmed) > 0 && !utf8.ValidString(trimmed) {
		trimmed = trimmed[:len(trimmed)-1]
	}
	return trimmed + "\n\n…truncated; see the issue's results for the full output."
}
