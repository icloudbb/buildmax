package taskrun

import (
	"testing"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/infra/workerclient"
)

// The Issue layer follows the Task's Issue: a run working one is pointed at
// `buildmax issue` through the bridge, and a run with no Issue or no server to
// reach is not.
func TestIssueContextFollowsTheTasksIssue(t *testing.T) {
	cfg := workerclient.WorkerAPIClientConfig{BaseURL: "https://server", Token: "run-token"}
	issueID := "issue-1"
	if got := issueContext(cfg, &coretask.Task{ID: "task-1", IssueID: &issueID}); got == nil || got.ID != "" {
		t.Fatalf("issue-linked worker run context = %+v, want one with no id", got)
	}
	if got := issueContext(cfg, &coretask.Task{ID: "task-1"}); got != nil {
		t.Fatalf("a Task with no Issue got an Issue context: %+v", got)
	}
	if got := issueContext(workerclient.WorkerAPIClientConfig{}, &coretask.Task{ID: "task-1", IssueID: &issueID}); got != nil {
		t.Fatalf("a run with no server got an Issue context: %+v", got)
	}
}
