package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/config"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
)

// workflowRunFixture creates a running workflow run with n pending step runs and
// returns the store, run id, and the step ids in index order. It registers
// cleanup of the rows it writes so it runs before the space fixture's cleanup.
func workflowRunFixture(t *testing.T, n int) (*Store, string, []string) {
	t.Helper()
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	userID, spaceID := secretTestSpace(t, s, "workflow-transition@example.com")

	wf, err := s.CreateWorkflow(ctx, spaceID, userID, "wf", "", `{"schema_version":1,"steps":[]}`)
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	t.Cleanup(func() {
		s.db.Exec(`DELETE wsr FROM workflow_step_run wsr
			JOIN workflow_run wr ON wr.id = wsr.workflow_run_id
			JOIN workflow w ON w.id = wr.workflow_id WHERE w.public_id = ?`, wf.ID)
		s.db.Exec(`DELETE wr FROM workflow_run wr
			JOIN workflow w ON w.id = wr.workflow_id WHERE w.public_id = ?`, wf.ID)
		s.db.Where("workflow_id IN (SELECT id FROM workflow WHERE public_id = ?)", wf.ID).Delete(&workflowRevisionRow{})
		s.db.Where("public_id = ?", wf.ID).Delete(&workflowRow{})
	})

	now := time.Now().UTC()
	run, err := s.CreateWorkflowRun(ctx, coreworkflow.CreateRunInput{
		WorkflowID: wf.ID,
		Status:     string(coreworkflow.RunStatusRunning),
		CreatedBy:  userID,
		StartedAt:  &now,
	})
	if err != nil {
		t.Fatalf("CreateWorkflowRun: %v", err)
	}
	stepsIn := make([]coreworkflow.CreateStepRunInput, n)
	for i := range stepsIn {
		stepsIn[i] = coreworkflow.CreateStepRunInput{
			StepID:    string(rune('a' + i)),
			StepIndex: i,
			StepType:  coreworkflow.StepTypeAgentTask,
			Prompt:    "do",
			Status:    string(coreworkflow.StepRunStatusPending),
		}
	}
	steps, err := s.CreateWorkflowStepRuns(ctx, run.ID, stepsIn)
	if err != nil {
		t.Fatalf("CreateWorkflowStepRuns: %v", err)
	}
	ids := make([]string, len(steps))
	for i := range steps {
		ids[i] = steps[i].ID
	}
	return s, run.ID, ids
}

// workflowFixture creates a store and one draft workflow (revision 1) and
// returns the store, the owning space's user id, the space id, and the workflow
// id. It registers cleanup of the revision and workflow rows.
func workflowFixture(t *testing.T, email string) (s *Store, userID, spaceID, workflowID string) {
	t.Helper()
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	userID, spaceID = secretTestSpace(t, s, email)
	wf, err := s.CreateWorkflow(ctx, spaceID, userID, "wf", "desc", `{"schema_version":1,"steps":[]}`)
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	t.Cleanup(func() {
		s.db.Where("workflow_id IN (SELECT id FROM workflow WHERE public_id = ?)", wf.ID).Delete(&workflowRevisionRow{})
		s.db.Where("public_id = ?", wf.ID).Delete(&workflowRow{})
	})
	return s, userID, spaceID, wf.ID
}

// revisionRows returns a workflow's revisions oldest first so the sequence and
// the immutable history can both be inspected directly.
func revisionRows(t *testing.T, s *Store, workflowID string) []coreworkflow.Revision {
	t.Helper()
	revs, total, err := s.ListWorkflowRevisions(context.Background(), workflowID, 0, 0)
	if err != nil {
		t.Fatalf("ListWorkflowRevisions: %v", err)
	}
	if total != len(revs) {
		t.Fatalf("revision total = %d but returned %d rows", total, len(revs))
	}
	// ListWorkflowRevisions returns newest first; reverse for oldest-first reading.
	out := make([]coreworkflow.Revision, len(revs))
	for i := range revs {
		out[len(revs)-1-i] = revs[i]
	}
	return out
}

// TestWorkflowRevisionContention proves the compare-and-set that guards revision
// advancement: a sequential edit advances and preserves prior history, and two
// edits that start from the same revision cannot both commit -- one wins and the
// rest get ErrRevisionConflict rather than overwriting the winner or leaking the
// duplicate-key error the append would otherwise raise.
//
// Removing the `AND revision = ?` predicate from UpdateWorkflow's guarded update
// makes the concurrent case fail: every writer's row update then succeeds and the
// losing appends collide on the unique (workflow_id, revision) index, so the
// race reports raw duplicate-key errors instead of clean conflicts.
func TestWorkflowRevisionContention(t *testing.T) {
	s, userID, spaceID, wfID := workflowFixture(t, "workflow-contention@example.com")
	ctx := context.Background()

	// Sequential advancement: an edit from revision 1 commits as revision 2, and
	// the winning row and the appended revision agree on every content field.
	name2, desc2, def2 := "renamed", "new desc", `{"schema_version":1,"steps":[{"one":1}]}`
	updated, err := s.UpdateWorkflow(ctx, wfID, spaceID, coreworkflow.UpdateInput{
		Name: &name2, Description: &desc2, Definition: &def2, Status: ptrStr(coreworkflow.StatusPublished),
		ExpectedRevision: 1, UpdatedBy: userID,
	})
	if err != nil {
		t.Fatalf("sequential update: %v", err)
	}
	if updated.Revision != 2 {
		t.Fatalf("updated revision = %d, want 2", updated.Revision)
	}
	revs := revisionRows(t, s, wfID)
	if len(revs) != 2 {
		t.Fatalf("revisions = %d, want 2", len(revs))
	}
	got := revs[1]
	if got.Revision != 2 || got.Name != name2 || got.Description != desc2 ||
		got.Definition != def2 || got.Status != coreworkflow.StatusPublished || got.CreatedBy != userID {
		t.Fatalf("revision 2 row = %+v, want it to match the winning workflow", got)
	}

	// A retried edit from the re-read revision advances again and leaves the prior
	// immutable revision intact.
	name3 := "renamed again"
	if _, err := s.UpdateWorkflow(ctx, wfID, spaceID, coreworkflow.UpdateInput{
		Name: &name3, ExpectedRevision: 2, UpdatedBy: userID,
	}); err != nil {
		t.Fatalf("retried update: %v", err)
	}
	revs = revisionRows(t, s, wfID)
	if len(revs) != 3 || revs[1].Name != name2 || revs[1].Definition != def2 {
		t.Fatalf("revision 2 was rewritten by the third edit: %+v", revs)
	}

	// Contention: many edits start from the current revision at once. They are
	// released together from the race barrier, and each carries an independent
	// name so the winner can be identified afterward.
	current, err := s.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	baseRev := current.Revision
	names := make([]string, raceCount)
	for i := range names {
		names[i] = fmt.Sprintf("contender-%d", i)
	}
	winners := race(t, func(i int) (bool, error) {
		_, err := s.UpdateWorkflow(ctx, wfID, spaceID, coreworkflow.UpdateInput{
			Name: &names[i], ExpectedRevision: baseRev, UpdatedBy: userID,
		})
		if errors.Is(err, coreworkflow.ErrRevisionConflict) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return true, nil
	})
	if winners != 1 {
		t.Fatalf("%d of %d concurrent edits committed, want exactly 1", winners, raceCount)
	}

	// The live row advanced exactly once and holds one contender's name.
	row, err := s.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow after race: %v", err)
	}
	if row.Revision != baseRev+1 {
		t.Fatalf("row revision = %d, want %d", row.Revision, baseRev+1)
	}
	if !slicesContains(names, row.Name) {
		t.Fatalf("row name = %q, want one of the contenders", row.Name)
	}

	// History gained exactly one immutable revision, and it matches the live row.
	revs = revisionRows(t, s, wfID)
	if len(revs) != baseRev+1 {
		t.Fatalf("revision count = %d, want %d", len(revs), baseRev+1)
	}
	winner := revs[len(revs)-1]
	if winner.Revision != row.Revision || winner.Name != row.Name ||
		winner.Description != row.Description || winner.Definition != row.Definition ||
		winner.Status != row.Status || winner.CreatedBy != row.CreatedBy {
		t.Fatalf("appended revision %+v disagrees with the live workflow %+v", winner, row)
	}
}

func ptrStr(s string) *string { return &s }

func slicesContains(s []string, v string) bool {
	for i := range s {
		if s[i] == v {
			return true
		}
	}
	return false
}

// TestWorkflowStepRunBindingsRoundTrip proves a step's snapshotted input
// bindings survive the store: they persist to the bindings column and read back
// intact, and a step that binds nothing reads back with none.
func TestWorkflowStepRunBindingsRoundTrip(t *testing.T) {
	s, runID, _ := workflowRunFixture(t, 0)
	ctx := context.Background()

	stepsIn := []coreworkflow.CreateStepRunInput{
		{StepID: "collect", StepIndex: 0, StepType: coreworkflow.StepTypeAgentTask, Prompt: "do", Status: string(coreworkflow.StepRunStatusPending)},
		{
			StepID: "summarize", StepIndex: 1, StepType: coreworkflow.StepTypeAgentTask, Prompt: "do",
			Status:   string(coreworkflow.StepRunStatusPending),
			Bindings: []coreworkflow.StepBinding{{Name: "research", FromStep: "collect"}},
		},
	}
	if _, err := s.CreateWorkflowStepRuns(ctx, runID, stepsIn); err != nil {
		t.Fatalf("CreateWorkflowStepRuns: %v", err)
	}

	got, err := s.ListWorkflowStepRuns(ctx, runID)
	if err != nil {
		t.Fatalf("ListWorkflowStepRuns: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("steps = %d, want 2", len(got))
	}
	if len(got[0].Bindings) != 0 {
		t.Errorf("step[0] bindings = %v, want none", got[0].Bindings)
	}
	if len(got[1].Bindings) != 1 || got[1].Bindings[0].Name != "research" || got[1].Bindings[0].FromStep != "collect" {
		t.Errorf("step[1] bindings = %v, want [{research collect}]", got[1].Bindings)
	}
}

func TestWorkflowStepRunTransition_CAS(t *testing.T) {
	s, _, steps := workflowRunFixture(t, 1)
	ctx := context.Background()

	// A valid transition from the expected status applies.
	applied, err := s.TransitionWorkflowStepRun(ctx, coreworkflow.TransitionStepRunInput{
		StepRunID:      steps[0],
		ExpectedStatus: coreworkflow.StepRunStatusPending,
		NewStatus:      coreworkflow.StepRunStatusRunning,
	})
	if err != nil || !applied {
		t.Fatalf("pending->running = %v, %v; want true, nil", applied, err)
	}

	// The same transition again finds the step no longer pending: no write, no
	// error -- another actor won.
	applied, err = s.TransitionWorkflowStepRun(ctx, coreworkflow.TransitionStepRunInput{
		StepRunID:      steps[0],
		ExpectedStatus: coreworkflow.StepRunStatusPending,
		NewStatus:      coreworkflow.StepRunStatusRunning,
	})
	if err != nil || applied {
		t.Fatalf("stale pending->running = %v, %v; want false, nil", applied, err)
	}

	// An illegal transition is a programming error, not a lost race.
	_, err = s.TransitionWorkflowStepRun(ctx, coreworkflow.TransitionStepRunInput{
		StepRunID:      steps[0],
		ExpectedStatus: coreworkflow.StepRunStatusRunning,
		NewStatus:      coreworkflow.StepRunStatusPending,
	})
	if !errors.Is(err, coreworkflow.ErrInvalidStepRunTransition) {
		t.Fatalf("running->pending err = %v, want ErrInvalidStepRunTransition", err)
	}
}

func TestFinalizeFailedWorkflowRun_BlocksLaterSteps(t *testing.T) {
	s, runID, steps := workflowRunFixture(t, 3)
	ctx := context.Background()

	// Start the first step, then fail it: the run fails and every later step
	// still pending is blocked, atomically.
	if _, err := s.TransitionWorkflowStepRun(ctx, coreworkflow.TransitionStepRunInput{
		StepRunID:      steps[0],
		ExpectedStatus: coreworkflow.StepRunStatusPending,
		NewStatus:      coreworkflow.StepRunStatusRunning,
	}); err != nil {
		t.Fatalf("start step 0: %v", err)
	}
	now := time.Now().UTC()
	applied, err := s.FinalizeFailedWorkflowRun(ctx, coreworkflow.FinalizeFailedRunInput{
		WorkflowRunID: runID,
		StepRunID:     steps[0],
		StepIndex:     0,
		StepExpected:  coreworkflow.StepRunStatusRunning,
		StepStatus:    coreworkflow.StepRunStatusFailed,
		RunExpected:   coreworkflow.RunStatusRunning,
		RunStatus:     coreworkflow.RunStatusFailed,
		EndedAt:       &now,
	})
	if err != nil || !applied {
		t.Fatalf("finalize = %v, %v; want true, nil", applied, err)
	}

	got, err := s.ListWorkflowStepRuns(ctx, runID)
	if err != nil {
		t.Fatalf("ListWorkflowStepRuns: %v", err)
	}
	want := []string{
		string(coreworkflow.StepRunStatusFailed),
		string(coreworkflow.StepRunStatusBlocked),
		string(coreworkflow.StepRunStatusBlocked),
	}
	for i := range got {
		if got[i].Status != want[i] {
			t.Errorf("step %d status = %s, want %s", i, got[i].Status, want[i])
		}
	}
	run, err := s.GetWorkflowRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetWorkflowRun: %v", err)
	}
	if run.Status != string(coreworkflow.RunStatusFailed) {
		t.Errorf("run status = %s, want failed", run.Status)
	}
}
