package db

import (
	"testing"
	"time"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
)

// An Issue's Agent runs are the Tasks started on it directly. A Task a Workflow
// step dispatched with the Issue attached carries the same issue_id, but it is
// part of its Workflow run and must not be listed or counted a second time.
func TestListIssueAgentTasksLeavesOutWorkflowSteps(t *testing.T) {
	s, ctx := newTestStore(t)
	userID := newTestUser(t, s, "issue-agent-tasks")
	spaceID := newTestSpace(t, s, userID)

	issue, err := s.CreateIssueInSpace(ctx, spaceID, userID, coreissue.CreateInput{Title: "Issue with runs"})
	if err != nil {
		t.Fatalf("CreateIssueInSpace: %v", err)
	}
	t.Cleanup(func() {
		_ = s.db.Delete(&issueRow{}, "public_id = ?", issue.ID).Error
	})

	direct, err := s.CreateTask(ctx, &coretask.CreateInput{
		SpaceID: spaceID, IssueID: &issue.ID, Input: "work the issue", CreatedBy: userID, InitialRunCreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	cleanupTask(t, s, direct.ID)

	wf, err := s.CreateWorkflow(ctx, spaceID, userID, "wf", "", `{"schema_version":1,"nodes":[]}`)
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	t.Cleanup(func() {
		s.db.Exec(`DELETE wsr FROM workflow_node_run wsr
			JOIN workflow_run wr ON wr.id = wsr.workflow_run_id
			JOIN workflow w ON w.id = wr.workflow_id WHERE w.public_id = ?`, wf.ID)
		s.db.Exec(`DELETE wr FROM workflow_run wr
			JOIN workflow w ON w.id = wr.workflow_id WHERE w.public_id = ?`, wf.ID)
		s.db.Where("workflow_id IN (SELECT id FROM workflow WHERE public_id = ?)", wf.ID).Delete(&workflowRevisionRow{})
		s.db.Where("public_id = ?", wf.ID).Delete(&workflowRow{})
	})
	now := time.Now().UTC()
	run, err := s.CreateWorkflowRun(ctx, coreworkflow.CreateRunInput{
		WorkflowID: wf.ID, IssueID: &issue.ID, Status: string(coreworkflow.RunStatusRunning), CreatedBy: userID, StartedAt: &now,
	})
	if err != nil {
		t.Fatalf("CreateWorkflowRun: %v", err)
	}
	steps, err := s.CreateWorkflowNodeRuns(ctx, run.ID, []coreworkflow.CreateNodeRunInput{{
		NodeID: "a", NodeType: coreworkflow.NodeTypeAgentTask, Prompt: "do", Status: string(coreworkflow.NodeRunStatusPending),
	}})
	if err != nil {
		t.Fatalf("CreateWorkflowNodeRuns: %v", err)
	}
	step, err := s.AdmitTask(ctx, &coretask.CreateInput{
		SpaceID: spaceID, IssueID: &issue.ID, Input: "step work", Title: "step work",
		CreatedBy: userID, InitialRunCreatedBy: userID,
		AdmissionKey: coreworkflow.TaskAdmissionKey(run.ID, "a"), WorkflowNodeRunID: steps[0].ID,
	})
	if err != nil {
		t.Fatalf("AdmitTask: %v", err)
	}
	cleanupTask(t, s, step.ID)

	got, total, err := s.ListIssueAgentTasks(ctx, issue.ID, 20, 0)
	if err != nil {
		t.Fatalf("ListIssueAgentTasks: %v", err)
	}
	if total != 1 || len(got) != 1 || got[0].ID != direct.ID {
		ids := make([]string, len(got))
		for i := range got {
			ids[i] = got[i].ID
		}
		t.Fatalf("ListIssueAgentTasks = %v (total %d), want only the direct run %s", ids, total, direct.ID)
	}
}
