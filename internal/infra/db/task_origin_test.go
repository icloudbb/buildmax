package db

import (
	"testing"
	"time"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	coreschedule "github.com/icloudbb/buildmax/internal/core/schedule"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
)

// Every Task read names what the Task serves, in the same query as the Task:
// the Workflow run and step a step's Task belongs to (even when it carries the
// run's Issue), the Issue, the Conversation, or the Schedule. A Task with none
// has no origin.
// The listing proves the Workflow join cannot repeat a row.
func TestTaskReadsNameTheirOrigin(t *testing.T) {
	s, ctx := newTestStore(t)
	userID := newTestUser(t, s, "task-origin")
	spaceID := newTestSpace(t, s, userID)

	issue, err := s.CreateIssueInSpace(ctx, spaceID, userID, coreissue.CreateInput{Title: "Fix import failures"})
	if err != nil {
		t.Fatalf("CreateIssueInSpace: %v", err)
	}
	t.Cleanup(func() {
		_ = s.db.Delete(&issueRow{}, "public_id = ?", issue.ID).Error
	})
	conv, err := s.CreateConversationInSpace(ctx, spaceID, userID, "portal", userID)
	if err != nil {
		t.Fatalf("CreateConversationInSpace: %v", err)
	}
	t.Cleanup(func() {
		_ = s.db.Delete(&conversationRow{}, "public_id = ?", canonicalPublicID(conv.ID)).Error
	})
	if err := s.UpdateConversationTitle(ctx, conv.ID, "Release chat"); err != nil {
		t.Fatalf("UpdateConversationTitle: %v", err)
	}

	agentID := newTestAgent(t, s, spaceID, userID, "Reviewer")
	create := func(in coretask.CreateInput) *coretask.Task {
		t.Helper()
		in.SpaceID, in.CreatedBy, in.InitialRunCreatedBy, in.AgentID = spaceID, userID, userID, &agentID
		created, err := s.CreateTask(ctx, &in)
		if err != nil {
			t.Fatalf("CreateTask: %v", err)
		}
		cleanupTask(t, s, created.ID)
		return created
	}
	issueTask := create(coretask.CreateInput{IssueID: &issue.ID, Input: "work the issue", Title: "deployment smoke ok"})
	convTask := create(coretask.CreateInput{ConversationID: conv.ID, Input: "asked in chat", Title: "deployment smoke ok"})
	directTask := create(coretask.CreateInput{Input: "run directly", Title: "deployment smoke ok"})
	sched, err := s.CreateSchedule(ctx, &coreschedule.CreateInput{
		SpaceID: spaceID, ExecutorKind: coreschedule.ExecutorAgent, ExecutorID: agentID, CreatedBy: userID,
		Name: "Nightly digest", Input: "summarize", CronExpr: "0 9 * * *", Timezone: "UTC",
		NextFireAt: time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	t.Cleanup(func() { _ = s.db.Delete(&scheduleRow{}, "public_id = ?", canonicalPublicID(sched.ID)).Error })
	schedTask := create(coretask.CreateInput{ScheduleID: &sched.ID, Input: "summarize", Title: "deployment smoke ok"})

	wf, err := s.CreateWorkflow(ctx, spaceID, userID, "QA review", "", `{"schema_version":1,"nodes":[]}`)
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
	steps, err := s.CreateWorkflowNodeRuns(ctx, run.ID, []coreworkflow.CreateNodeRunInput{
		{NodeID: "draft", NodeIndex: 0, NodeType: coreworkflow.NodeTypeAgentTask, Prompt: "draft", Status: string(coreworkflow.NodeRunStatusSucceeded)},
		{NodeID: "review", NodeIndex: 1, NodeType: coreworkflow.NodeTypeAgentTask, Prompt: "review", Status: string(coreworkflow.NodeRunStatusPending)},
	})
	if err != nil {
		t.Fatalf("CreateWorkflowNodeRuns: %v", err)
	}
	stepTask, err := s.AdmitTask(ctx, &coretask.CreateInput{
		SpaceID: spaceID, IssueID: &issue.ID, AgentID: &agentID, Input: "review the draft", Title: "deployment smoke ok",
		CreatedBy: userID, InitialRunCreatedBy: userID,
		AdmissionKey: coreworkflow.TaskAdmissionKey(run.ID, "review"), WorkflowNodeRunID: steps[1].ID,
	})
	if err != nil {
		t.Fatalf("AdmitTask: %v", err)
	}
	cleanupTask(t, s, stepTask.ID)

	want := map[string]*coretask.Origin{
		issueTask.ID:  {Kind: coretask.OriginIssue, ID: issue.ID, Name: "Fix import failures"},
		convTask.ID:   {Kind: coretask.OriginConversation, ID: conv.ID, Name: "Release chat"},
		directTask.ID: nil,
		schedTask.ID:  {Kind: coretask.OriginSchedule, ID: sched.ID, Name: "Nightly digest"},
		stepTask.ID:   {Kind: coretask.OriginWorkflow, ID: run.ID, Name: "QA review", Step: 2},
	}
	check := func(where string, task *coretask.Task) {
		t.Helper()
		expected, ok := want[task.ID]
		if !ok {
			return
		}
		got := task.Origin
		if (got == nil) != (expected == nil) || (got != nil && *got != *expected) {
			t.Errorf("%s: task %s origin = %+v, want %+v", where, task.ID, got, expected)
		}
	}
	for id := range want {
		got, err := s.GetTask(ctx, id)
		if err != nil || got == nil {
			t.Fatalf("GetTask(%s) = %v, %v", id, got, err)
		}
		check("GetTask", got)
	}

	// A listing reads the same joins: five Tasks, each once and named.
	list, total, err := s.ListTasksByAgent(ctx, spaceID, agentID, 20, 0)
	if err != nil {
		t.Fatalf("ListTasksByAgent: %v", err)
	}
	if total != 5 || len(list) != 5 {
		t.Fatalf("ListTasksByAgent = %d rows of %d, want 5 of 5", len(list), total)
	}
	seen := map[string]bool{}
	for i := range list {
		if seen[list[i].ID] {
			t.Errorf("ListTasksByAgent repeats task %s", list[i].ID)
		}
		seen[list[i].ID] = true
		check("ListTasksByAgent", &list[i])
	}
}
