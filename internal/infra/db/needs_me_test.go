package db

import (
	"context"
	"sort"
	"testing"
	"time"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/util"
)

// needsMeFixture is one Space with two people and the helpers the needs-me
// store tests build their runs with.
type needsMeFixture struct {
	t       *testing.T
	s       *Store
	ctx     context.Context
	alice   string
	bob     string
	spaceID string
	wfID    string
}

func newNeedsMeFixture(t *testing.T) *needsMeFixture {
	t.Helper()
	s, ctx := newTestStore(t)
	f := &needsMeFixture{t: t, s: s, ctx: ctx}
	f.alice = newTestUser(t, s, "needs-me-alice")
	f.bob = newTestUser(t, s, "needs-me-bob")
	f.spaceID = newTestSpace(t, s, f.alice)
	wf, err := s.CreateWorkflow(ctx, f.spaceID, f.alice, "wf", "", `{"schema_version":1,"nodes":[]}`)
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	f.wfID = wf.ID
	t.Cleanup(func() {
		s.db.Exec(`DELETE rq FROM workflow_request rq
			JOIN workflow_run wr ON wr.id = rq.workflow_run_id
			JOIN workflow w ON w.id = wr.workflow_id WHERE w.public_id = ?`, wf.ID)
		s.db.Exec(`DELETE wsr FROM workflow_node_run wsr
			JOIN workflow_run wr ON wr.id = wsr.workflow_run_id
			JOIN workflow w ON w.id = wr.workflow_id WHERE w.public_id = ?`, wf.ID)
		s.db.Exec(`DELETE wr FROM workflow_run wr
			JOIN workflow w ON w.id = wr.workflow_id WHERE w.public_id = ?`, wf.ID)
		s.db.Where("workflow_id IN (SELECT id FROM workflow WHERE public_id = ?)", wf.ID).Delete(&workflowRevisionRow{})
		s.db.Where("public_id = ?", wf.ID).Delete(&workflowRow{})
	})
	return f
}

func (f *needsMeFixture) issue(title, owner, status string) string {
	f.t.Helper()
	issue, err := f.s.CreateIssueInSpace(f.ctx, f.spaceID, f.alice, coreissue.CreateInput{Title: title, OwnerID: owner, Status: status})
	if err != nil {
		f.t.Fatalf("CreateIssueInSpace: %v", err)
	}
	f.t.Cleanup(func() { _ = f.s.db.Delete(&issueRow{}, "public_id = ?", issue.ID).Error })
	return issue.ID
}

func (f *needsMeFixture) task(in coretask.CreateInput) *coretask.Task {
	f.t.Helper()
	if in.SpaceID == "" {
		in.SpaceID = f.spaceID
	}
	in.Input = "work"
	in.InitialRunCreatedBy = in.CreatedBy
	task, err := f.s.CreateTask(f.ctx, &in)
	if err != nil {
		f.t.Fatalf("CreateTask: %v", err)
	}
	cleanupTask(f.t, f.s, task.ID)
	return task
}

// end moves a pending run to the status it ends in, with questions when it
// asks them.
func (f *needsMeFixture) end(runID string, status coretask.RunStatus, questions *string) {
	f.t.Helper()
	steps := [][2]coretask.RunStatus{{coretask.RunStatusPending, coretask.RunStatusScheduled}}
	if status == coretask.RunStatusFailed {
		steps = append(steps, [2]coretask.RunStatus{coretask.RunStatusScheduled, coretask.RunStatusFailed})
	} else {
		steps = append(steps, [2]coretask.RunStatus{coretask.RunStatusScheduled, coretask.RunStatusRunning},
			[2]coretask.RunStatus{coretask.RunStatusRunning, status})
	}
	for i, step := range steps {
		in := coretask.TransitionRunInput{TaskRunID: runID, ExpectedStatus: step[0], NewStatus: step[1]}
		if i == len(steps)-1 {
			in.Questions = questions
			now := time.Now().UTC()
			in.EndedAt = &now
		}
		if ok, err := f.s.TransitionTaskRun(f.ctx, in); err != nil || !ok {
			f.t.Fatalf("TransitionTaskRun %s→%s: %v %v", step[0], step[1], ok, err)
		}
	}
}

func (f *needsMeFixture) workflowRun(issueID string, status coreworkflow.RunStatus) *coreworkflow.Run {
	f.t.Helper()
	now := time.Now().UTC()
	run, err := f.s.CreateWorkflowRun(f.ctx, coreworkflow.CreateRunInput{
		WorkflowID: f.wfID, IssueID: &issueID, Status: string(status), CreatedBy: f.alice, StartedAt: &now,
	})
	if err != nil {
		f.t.Fatalf("CreateWorkflowRun: %v", err)
	}
	return run
}

// stepTask is a Task a Workflow step dispatched with the Issue attached.
func (f *needsMeFixture) stepTask(run *coreworkflow.Run, issueID, createdBy string) *coretask.Task {
	f.t.Helper()
	steps, err := f.s.CreateWorkflowNodeRuns(f.ctx, run.ID, []coreworkflow.CreateNodeRunInput{{
		NodeID: "step", NodeType: coreworkflow.NodeTypeAgentTask, Prompt: "do", Status: string(coreworkflow.NodeRunStatusPending),
	}})
	if err != nil {
		f.t.Fatalf("CreateWorkflowNodeRuns: %v", err)
	}
	task, err := f.s.AdmitTask(f.ctx, &coretask.CreateInput{
		SpaceID: f.spaceID, IssueID: &issueID, Input: "step work", CreatedBy: createdBy, InitialRunCreatedBy: createdBy,
		AdmissionKey: coreworkflow.TaskAdmissionKey(run.ID, "step"), WorkflowNodeRunID: steps[0].ID,
	})
	if err != nil {
		f.t.Fatalf("AdmitTask: %v", err)
	}
	cleanupTask(f.t, f.s, task.ID)
	return task
}

const needsMeQuestions = `[{"question":"Which checklist?"}]`

// An owned Issue needs attention while its latest run — the newest Agent run
// or Workflow run, a step's Task not counting — failed or awaits an answer,
// or while one of its Workflow runs waits on a request. A newer run clears it,
// and so does marking the Issue done.
func TestListIssuesNeedingOwner(t *testing.T) {
	f := newNeedsMeFixture(t)
	want := map[string]coreissue.AttentionReason{}

	failed := f.issue("latest agent run failed", f.alice, "")
	f.end(*f.task(coretask.CreateInput{IssueID: &failed, CreatedBy: f.bob}).LastRunID, coretask.RunStatusFailed, nil)
	want[failed] = coreissue.AttentionRunFailed

	recovered := f.issue("failed, then a newer run succeeded", f.alice, "")
	f.end(*f.task(coretask.CreateInput{IssueID: &recovered, CreatedBy: f.alice}).LastRunID, coretask.RunStatusFailed, nil)
	f.end(*f.task(coretask.CreateInput{IssueID: &recovered, CreatedBy: f.alice}).LastRunID, coretask.RunStatusSucceeded, nil)

	asking := f.issue("latest agent run asks", f.alice, "")
	questions := needsMeQuestions
	f.end(*f.task(coretask.CreateInput{IssueID: &asking, CreatedBy: f.alice}).LastRunID, coretask.RunStatusSucceeded, &questions)
	want[asking] = coreissue.AttentionAwaitingAnswer

	waiting := f.issue("workflow run waits on a request", f.alice, "")
	run := f.workflowRun(waiting, coreworkflow.RunStatusRunning)
	nodes, err := f.s.CreateWorkflowNodeRuns(f.ctx, run.ID, []coreworkflow.CreateNodeRunInput{{
		NodeID: "approve", NodeType: coreworkflow.NodeTypeHumanInput, Prompt: "Approve?", Status: string(coreworkflow.NodeRunStatusPending),
	}})
	if err != nil {
		t.Fatalf("CreateWorkflowNodeRuns: %v", err)
	}
	if _, opened, err := openInput(f.s, t, run.ID, nodes[0].ID, nil); err != nil || !opened {
		t.Fatalf("open request: %v %v", opened, err)
	}
	want[waiting] = coreissue.AttentionWorkflowRequest

	workflowFailed := f.issue("latest workflow run failed", f.alice, "")
	f.workflowRun(workflowFailed, coreworkflow.RunStatusFailed)
	want[workflowFailed] = coreissue.AttentionRunFailed

	superseded := f.issue("failed workflow run, then a newer agent run", f.alice, "")
	f.workflowRun(superseded, coreworkflow.RunStatusFailed)
	f.end(*f.task(coretask.CreateInput{IssueID: &superseded, CreatedBy: f.alice}).LastRunID, coretask.RunStatusSucceeded, nil)

	stepOnly := f.issue("only a workflow step's task failed", f.alice, "")
	f.end(*f.stepTask(f.workflowRun(stepOnly, coreworkflow.RunStatusRunning), stepOnly, f.alice).LastRunID, coretask.RunStatusFailed, nil)

	done := f.issue("done issue with a failed run", f.alice, coreissue.StatusDone)
	f.end(*f.task(coretask.CreateInput{IssueID: &done, CreatedBy: f.alice}).LastRunID, coretask.RunStatusFailed, nil)

	bobs := f.issue("someone else's failed issue", f.bob, "")
	f.end(*f.task(coretask.CreateInput{IssueID: &bobs, CreatedBy: f.alice}).LastRunID, coretask.RunStatusFailed, nil)

	got, total, err := f.s.ListIssuesNeedingOwner(f.ctx, f.spaceID, f.alice, 20)
	if err != nil {
		t.Fatalf("ListIssuesNeedingOwner: %v", err)
	}
	gotReasons := map[string]coreissue.AttentionReason{}
	for _, item := range got {
		gotReasons[item.Issue.ID] = item.Reason
	}
	if total != len(want) || len(gotReasons) != len(want) {
		t.Fatalf("listed %d of total %d: %v, want %v", len(got), total, gotReasons, want)
	}
	for id, reason := range want {
		if gotReasons[id] != reason {
			t.Errorf("issue %s: reason %q, want %q", id, gotReasons[id], reason)
		}
	}

	page, total, err := f.s.ListIssuesNeedingOwner(f.ctx, f.spaceID, f.alice, 2)
	if err != nil || len(page) != 2 || total != len(want) {
		t.Fatalf("limit 2 = %d items of total %d (%v), want 2 of %d", len(page), total, err, len(want))
	}
}

// A person's waiting Tasks are the ones they created, asked for through an
// Assistant, or started on an Issue they own. A newer run clears a failure,
// and a Workflow step's Task is its Workflow run's to resolve.
func TestListTasksWaitingOn(t *testing.T) {
	f := newNeedsMeFixture(t)
	owned := f.issue("alice's issue", f.alice, "")
	otherSpace := newTestSpace(t, f.s, f.bob)

	created := f.task(coretask.CreateInput{CreatedBy: f.alice})
	f.end(*created.LastRunID, coretask.RunStatusFailed, nil)
	onOwnedIssue := f.task(coretask.CreateInput{CreatedBy: f.bob, IssueID: &owned})
	f.end(*onOwnedIssue.LastRunID, coretask.RunStatusFailed, nil)
	// A Space Assistant's dispatch records its requester; set it directly
	// rather than standing up an Assistant for one column.
	requested := f.task(coretask.CreateInput{CreatedBy: f.bob})
	aliceKey, err := lookupKey(f.ctx, f.s.db, "user", f.alice)
	if err != nil {
		t.Fatalf("lookupKey: %v", err)
	}
	if err := f.s.db.Model(&taskRow{}).Where("public_id = ?", requested.ID).Update("requested_by", aliceKey).Error; err != nil {
		t.Fatalf("set requested_by: %v", err)
	}
	f.end(*requested.LastRunID, coretask.RunStatusFailed, nil)

	unrelated := f.task(coretask.CreateInput{CreatedBy: f.bob})
	f.end(*unrelated.LastRunID, coretask.RunStatusFailed, nil)
	elsewhere := f.task(coretask.CreateInput{CreatedBy: f.alice, SpaceID: otherSpace})
	f.end(*elsewhere.LastRunID, coretask.RunStatusFailed, nil)
	retried := f.task(coretask.CreateInput{CreatedBy: f.alice})
	f.end(*retried.LastRunID, coretask.RunStatusFailed, nil)
	if _, err := f.s.CreateTaskRun(f.ctx, coretask.CreateRunInput{TaskID: retried.ID, Input: "again", CreatedBy: f.alice,
		RetryOfTaskRunID: retried.LastRunID}); err != nil {
		t.Fatalf("CreateTaskRun: %v", err)
	}
	step := f.stepTask(f.workflowRun(owned, coreworkflow.RunStatusRunning), owned, f.alice)
	f.end(*step.LastRunID, coretask.RunStatusFailed, nil)

	questions := needsMeQuestions
	asking := f.task(coretask.CreateInput{CreatedBy: f.alice})
	f.end(*asking.LastRunID, coretask.RunStatusSucceeded, &questions)
	answered := f.task(coretask.CreateInput{CreatedBy: f.alice})
	f.end(*answered.LastRunID, coretask.RunStatusSucceeded, util.Ptr(needsMeQuestions))
	if _, err := f.s.CreateTaskRun(f.ctx, coretask.CreateRunInput{TaskID: answered.ID, Input: "the 2026 one", CreatedBy: f.alice}); err != nil {
		t.Fatalf("CreateTaskRun: %v", err)
	}

	ids := func(wait coretask.Wait, limit int) ([]string, int) {
		t.Helper()
		tasks, total, err := f.s.ListTasksWaitingOn(f.ctx, f.spaceID, f.alice, wait, limit)
		if err != nil {
			t.Fatalf("ListTasksWaitingOn(%s): %v", wait, err)
		}
		out := make([]string, len(tasks))
		for i := range tasks {
			out[i] = tasks[i].ID
		}
		sort.Strings(out)
		return out, total
	}
	sorted := func(in ...string) []string { sort.Strings(in); return in }

	if got, total := ids(coretask.WaitFailed, 20); total != 3 || !equalStrings(got, sorted(created.ID, onOwnedIssue.ID, requested.ID)) {
		t.Errorf("failed = %v (total %d), want created, on the owned issue, and requested", got, total)
	}
	if got, total := ids(coretask.WaitAnswer, 20); total != 1 || !equalStrings(got, []string{asking.ID}) {
		t.Errorf("awaiting answer = %v (total %d), want only the unanswered task", got, total)
	}
	if got, total := ids(coretask.WaitFailed, 1); total != 3 || len(got) != 1 {
		t.Errorf("limit 1 = %v of total %d, want 1 of 3", got, total)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
