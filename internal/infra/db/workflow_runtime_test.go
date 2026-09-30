package db

import (
	"testing"
	"time"

	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/util"
)

// A Workflow run waiting on people holds no active TaskRun, and one can fail
// while its TaskRuns succeeded, so administration reads both from Workflow
// rows. The store tests share one database: deployment-wide answers are
// deltas, and this test's request is dated before anything else can be.
func TestRuntimeAggregatesReadWorkflowWaitsAndFailures(t *testing.T) {
	s, waitingRunID, nodes := workflowRunFixture(t, 2)
	ctx := t.Context()
	waitingRun, err := s.GetWorkflowRun(ctx, waitingRunID)
	if err != nil {
		t.Fatalf("GetWorkflowRun: %v", err)
	}
	wf, err := s.GetWorkflow(ctx, waitingRun.WorkflowID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	now := time.Now().UTC()
	failedSince := now.Add(-24 * time.Hour)
	before, err := s.RuntimeSummary(ctx, now, failedSince)
	if err != nil {
		t.Fatalf("RuntimeSummary: %v", err)
	}

	var runIDs []string
	t.Cleanup(func() {
		for _, id := range append(runIDs, waitingRunID) {
			key, _ := util.CanonicalPublicID(id)
			s.db.Exec(`DELETE r FROM workflow_request r JOIN workflow_run wr ON wr.id = r.workflow_run_id WHERE wr.public_id = ?`, key)
		}
	})

	// Two requests wait: a human_input approval with an expiry, and an Agent's
	// question.
	expires := now.Add(2 * time.Hour).Truncate(time.Second)
	approval, opened, err := openInput(s, t, waitingRunID, nodes[0], &expires)
	if err != nil || !opened {
		t.Fatalf("open input: opened=%v err=%v", opened, err)
	}
	early := time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)
	approvalKey, _ := util.CanonicalPublicID(approval.ID)
	if err := s.db.Model(&workflowRequestRow{}).Where("public_id = ?", approvalKey).Update("created_at", early).Error; err != nil {
		t.Fatalf("date request: %v", err)
	}
	if _, err := s.TransitionWorkflowNodeRun(ctx, coreworkflow.TransitionNodeRunInput{
		NodeRunID: nodes[1], ExpectedStatus: coreworkflow.NodeRunStatusPending, NewStatus: coreworkflow.NodeRunStatusRunning,
	}); err != nil {
		t.Fatalf("start node: %v", err)
	}
	if _, opened, err := s.OpenWorkflowRequest(ctx, coreworkflow.OpenRequestInput{
		WorkflowRunID: waitingRunID, NodeRunID: nodes[1], NodeExpected: coreworkflow.NodeRunStatusRunning,
		Key: "task_run/q", Kind: coreworkflow.RequestKindQuestion, Questions: util.Ptr(`[]`), Now: now,
	}); err != nil || !opened {
		t.Fatalf("open question: opened=%v err=%v", opened, err)
	}

	// stop starts a run, records stop intent, and ends it at endedAt.
	stop := func(status coreworkflow.RunStatus, class coreworkflow.FailureClass, endedAt time.Time) string {
		t.Helper()
		run, err := s.CreateWorkflowRun(ctx, coreworkflow.CreateRunInput{
			WorkflowID: wf.ID, Status: string(coreworkflow.RunStatusRunning), CreatedBy: wf.CreatedBy, StartedAt: &now,
		})
		if err != nil {
			t.Fatalf("CreateWorkflowRun: %v", err)
		}
		runIDs = append(runIDs, run.ID)
		if ok, err := s.StopWorkflowRun(ctx, coreworkflow.StopRunInput{
			WorkflowRunID: run.ID, RunExpected: coreworkflow.RunStatusRunning, RunStatus: status,
			ErrorMessage: util.Ptr("private detail"), FailureClass: class,
		}); err != nil || !ok {
			t.Fatalf("StopWorkflowRun: ok=%v err=%v", ok, err)
		}
		final := coreworkflow.RunStatusFailed
		if status == coreworkflow.RunStatusCanceling {
			final = coreworkflow.RunStatusCanceled
		}
		if ok, err := s.TransitionWorkflowRun(ctx, coreworkflow.TransitionRunInput{
			WorkflowRunID: run.ID, ExpectedStatus: status, NewStatus: final, EndedAt: &endedAt,
		}); err != nil || !ok {
			t.Fatalf("finish run: ok=%v err=%v", ok, err)
		}
		return run.ID
	}
	deadline := stop(coreworkflow.RunStatusFailing, coreworkflow.FailureRunDeadline, now.Add(-time.Hour))
	unnamed := stop(coreworkflow.RunStatusFailing, "", now.Add(-72*time.Hour))
	canceled := stop(coreworkflow.RunStatusCanceling, coreworkflow.FailureRunDeadline, now.Add(-time.Hour))
	direct, err := s.CreateWorkflowRun(ctx, coreworkflow.CreateRunInput{
		WorkflowID: wf.ID, Status: string(coreworkflow.RunStatusRunning), CreatedBy: wf.CreatedBy, StartedAt: &now,
	})
	if err != nil {
		t.Fatalf("CreateWorkflowRun: %v", err)
	}
	runIDs = append(runIDs, direct.ID)
	longAgo := now.Add(-72 * time.Hour)
	if ok, err := s.TransitionWorkflowRun(ctx, coreworkflow.TransitionRunInput{
		WorkflowRunID: direct.ID, ExpectedStatus: coreworkflow.RunStatusRunning, NewStatus: coreworkflow.RunStatusFailed, EndedAt: &longAgo,
	}); err != nil || !ok {
		t.Fatalf("fail run directly: ok=%v err=%v", ok, err)
	}

	for id, want := range map[string]coreworkflow.FailureClass{
		deadline: coreworkflow.FailureRunDeadline,
		// A failure nobody named is still one of the enum.
		unnamed: coreworkflow.FailureUnclassified,
		// A cancellation is not a failure, whatever the caller passed.
		canceled: "",
		// Nor does a failure that skipped failing go uncounted.
		direct.ID: coreworkflow.FailureUnclassified,
	} {
		run, err := s.GetWorkflowRun(ctx, id)
		if err != nil || run.FailureClass != string(want) {
			t.Errorf("run %s class = %q (err %v), want %q", id, run.FailureClass, err, want)
		}
	}

	after, err := s.RuntimeSummary(ctx, now, failedSince)
	if err != nil {
		t.Fatalf("RuntimeSummary: %v", err)
	}
	for kind, want := range map[string]int{coreworkflow.RequestKindInput: 1, coreworkflow.RequestKindQuestion: 1} {
		if got := after.WaitingRequests[kind] - before.WaitingRequests[kind]; got != want {
			t.Errorf("waiting %s grew by %d, want %d", kind, got, want)
		}
	}
	if after.OldestWaitingRequestAt == nil || !after.OldestWaitingRequestAt.Equal(early) {
		t.Errorf("oldest waiting = %v, want %v", after.OldestWaitingRequestAt, early)
	}
	for class, want := range map[coreworkflow.FailureClass]int{
		coreworkflow.FailureRunDeadline:  1,
		coreworkflow.FailureUnclassified: 0, // outside the window
	} {
		if got := after.WorkflowFailuresByClass[string(class)] - before.WorkflowFailuresByClass[string(class)]; got != want {
			t.Errorf("workflow %s failures grew by %d, want %d", class, got, want)
		}
	}

	// The request has waited longer than any run anywhere, so its Space leads
	// though it has no active TaskRun.
	spaces, total, err := s.ListSpaceRunActivity(ctx, failedSince, 5, 0)
	if err != nil {
		t.Fatalf("ListSpaceRunActivity: %v", err)
	}
	if total < 1 || len(spaces) == 0 || spaces[0].SpaceID != wf.SpaceID {
		t.Fatalf("spaces = %+v (total %d), want %s first", spaces, total, wf.SpaceID)
	}
	got := spaces[0]
	if len(got.Active) != 0 || got.OldestActiveAt != nil {
		t.Errorf("active = %v since %v, want none", got.Active, got.OldestActiveAt)
	}
	if got.WaitingRequests[coreworkflow.RequestKindInput] != 1 || got.WaitingRequests[coreworkflow.RequestKindQuestion] != 1 {
		t.Errorf("waiting = %v, want one input and one question", got.WaitingRequests)
	}
	if got.OldestWaitingRequestAt == nil || !got.OldestWaitingRequestAt.Equal(early) ||
		got.NextRequestExpiryAt == nil || !got.NextRequestExpiryAt.Equal(expires) {
		t.Errorf("oldest waiting = %v, next expiry = %v; want %v and %v",
			got.OldestWaitingRequestAt, got.NextRequestExpiryAt, early, expires)
	}
	if got.OldestWaitingWorkflowRunID != waitingRunID || got.LatestFailedWorkflowRunID != deadline {
		t.Errorf("runs = waiting %q, failed %q; want %q and %q",
			got.OldestWaitingWorkflowRunID, got.LatestFailedWorkflowRunID, waitingRunID, deadline)
	}
	if len(got.WorkflowFailuresByClass) != 1 || got.WorkflowFailuresByClass[string(coreworkflow.FailureRunDeadline)] != 1 {
		t.Errorf("workflow failures = %v, want one run_deadline failure", got.WorkflowFailuresByClass)
	}
}
