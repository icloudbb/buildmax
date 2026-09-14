package db

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/config"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
)

// reconcileFixture creates a workflow and returns the store plus the handles a
// reconciliation test needs to create runs under it. It cleans up the runs and
// the workflow before the space fixture removes the user.
func reconcileFixture(t *testing.T) (s *Store, userID, workflowID string) {
	t.Helper()
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	var err error
	s, err = New(ctx, dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var spaceID string
	userID, spaceID = secretTestSpace(t, s, "workflow-reconcile@example.com")
	wf, err := s.CreateWorkflow(ctx, spaceID, userID, "wf", "", `{"schema_version":1,"steps":[]}`)
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	t.Cleanup(func() {
		s.db.Exec(`DELETE wr FROM workflow_run wr
			JOIN workflow w ON w.id = wr.workflow_id WHERE w.public_id = ?`, wf.ID)
		s.db.Where("workflow_id IN (SELECT id FROM workflow WHERE public_id = ?)", wf.ID).Delete(&workflowRevisionRow{})
		s.db.Where("public_id = ?", wf.ID).Delete(&workflowRow{})
	})
	return s, userID, wf.ID
}

// createReconcileRun creates a run in status and returns its id.
func createReconcileRun(t *testing.T, s *Store, workflowID, userID string, status coreworkflow.RunStatus) string {
	t.Helper()
	now := time.Now().UTC()
	run, err := s.CreateWorkflowRun(context.Background(), coreworkflow.CreateRunInput{
		WorkflowID: workflowID,
		Status:     string(status),
		CreatedBy:  userID,
		StartedAt:  &now,
	})
	if err != nil {
		t.Fatalf("CreateWorkflowRun: %v", err)
	}
	return run.ID
}

// setSchedule writes the reconciliation columns directly, so a test can place a
// run in a precise scheduling state without depending on the methods under test.
func setSchedule(t *testing.T, s *Store, runID string, next, leaseExpires *time.Time, owner *string) {
	t.Helper()
	err := s.db.Model(&workflowRunRow{}).
		Where("public_id = ?", canonicalPublicID(runID)).
		Updates(map[string]interface{}{
			"next_reconcile_at": next,
			"lease_expires_at":  leaseExpires,
			"reconcile_owner":   owner,
		}).Error
	if err != nil {
		t.Fatalf("setSchedule: %v", err)
	}
}

func TestWorkflowReconciliationLease_DueSelection(t *testing.T) {
	s, userID, wf := reconcileFixture(t)
	ctx := context.Background()
	base := time.Unix(1_800_000_000, 0).UTC()
	past := base.Add(-10 * time.Second)
	future := base.Add(time.Hour)
	expiredLease := base.Add(-5 * time.Second)

	// never: no schedule at all -> due, and sorts first (NULL leads).
	never := createReconcileRun(t, s, wf, userID, coreworkflow.RunStatusRunning)
	// pastDue: scheduled in the past -> due.
	pastDue := createReconcileRun(t, s, wf, userID, coreworkflow.RunStatusRunning)
	setSchedule(t, s, pastDue, &past, nil, nil)
	// leaseExpired: scheduled in the future but its lease expired -> due.
	leaseExpired := createReconcileRun(t, s, wf, userID, coreworkflow.RunStatusRunning)
	owner := "old"
	setSchedule(t, s, leaseExpired, &future, &expiredLease, &owner)
	// notDue: scheduled in the future, no lease -> not due.
	notDue := createReconcileRun(t, s, wf, userID, coreworkflow.RunStatusRunning)
	setSchedule(t, s, notDue, &future, nil, nil)
	// terminalPast: terminal with a past schedule -> excluded by the status filter.
	terminalPast := createReconcileRun(t, s, wf, userID, coreworkflow.RunStatusRunning)
	setSchedule(t, s, terminalPast, &past, nil, nil)
	if err := s.db.Model(&workflowRunRow{}).
		Where("public_id = ?", canonicalPublicID(terminalPast)).
		Update("status", string(coreworkflow.RunStatusSucceeded)).Error; err != nil {
		t.Fatalf("force terminal: %v", err)
	}

	due, err := s.ListDueWorkflowRuns(ctx, base, 0)
	if err != nil {
		t.Fatalf("ListDueWorkflowRuns: %v", err)
	}
	// ListDueWorkflowRuns is a global scanner, and this scope shares its database
	// with the service-layer reconciliation tests, so the due set may hold their
	// runs too. Restrict the exact comparison to the runs this test created; the
	// query cannot assume it is the only writer in the schema.
	mine := []string{never, pastDue, leaseExpired, notDue, terminalPast}
	got := filterIDs(runIDs(due), mine)
	want := []string{never, pastDue, leaseExpired}
	if !equalIDs(got, want) {
		t.Fatalf("due (this test's runs) = %v, want %v (never/pastDue/leaseExpired in oldest-due order)", got, want)
	}
	if contains(got, notDue) {
		t.Errorf("a future-scheduled run with no lease appeared in the due set")
	}
	if contains(got, terminalPast) {
		t.Errorf("a terminal run with a past schedule appeared in the due set")
	}

	// The batch bound caps the returned rows. Ordering — that the NULL-scheduled
	// run leads — is already proven by got above; here only the cap is at stake,
	// and the globally oldest-due row may belong to another writer in the schema.
	one, err := s.ListDueWorkflowRuns(ctx, base, 1)
	if err != nil {
		t.Fatalf("ListDueWorkflowRuns limit=1: %v", err)
	}
	if len(one) != 1 {
		t.Fatalf("bounded due returned %d rows, want the batch bound of 1", len(one))
	}
}

// filterIDs keeps the ids that appear in keep, preserving order. A shared scope
// database means a global query can return rows another test wrote; a test that
// asserts an exact set restricts the result to the ids it owns first.
func filterIDs(ids, keep []string) []string {
	want := make(map[string]bool, len(keep))
	for _, id := range keep {
		want[id] = true
	}
	var out []string
	for _, id := range ids {
		if want[id] {
			out = append(out, id)
		}
	}
	return out
}

func TestWorkflowReconciliationLease_ConcurrentClaimHasOneWinner(t *testing.T) {
	s, userID, wf := reconcileFixture(t)
	ctx := context.Background()
	base := time.Unix(1_800_000_000, 0).UTC()
	run := createReconcileRun(t, s, wf, userID, coreworkflow.RunStatusRunning)

	winners := race(t, func(i int) (bool, error) {
		return s.ClaimWorkflowRunLease(ctx, coreworkflow.ClaimLeaseInput{
			WorkflowRunID:  run,
			Owner:          fmt.Sprintf("owner-%d", i),
			Now:            base,
			LeaseExpiresAt: base.Add(30 * time.Second),
		})
	})
	if winners != 1 {
		t.Errorf("%d of %d concurrent claims won the lease, want exactly 1", winners, raceCount)
	}

	stored, err := s.GetWorkflowRun(ctx, run)
	if err != nil {
		t.Fatalf("GetWorkflowRun: %v", err)
	}
	if stored.ReconcileOwner == nil || stored.LeaseExpiresAt == nil {
		t.Fatalf("winner did not record an owner and expiry: %+v", stored)
	}
}

func TestWorkflowReconciliationLease_ExpiryTakeoverAndStaleOwner(t *testing.T) {
	s, userID, wf := reconcileFixture(t)
	ctx := context.Background()
	base := time.Unix(1_800_000_000, 0).UTC()
	run := createReconcileRun(t, s, wf, userID, coreworkflow.RunStatusRunning)

	// A claims the lease.
	won, err := s.ClaimWorkflowRunLease(ctx, coreworkflow.ClaimLeaseInput{
		WorkflowRunID: run, Owner: "A", Now: base, LeaseExpiresAt: base.Add(30 * time.Second),
	})
	if err != nil || !won {
		t.Fatalf("A claim = %v, %v; want true, nil", won, err)
	}
	// B cannot claim before the lease expires.
	won, err = s.ClaimWorkflowRunLease(ctx, coreworkflow.ClaimLeaseInput{
		WorkflowRunID: run, Owner: "B", Now: base.Add(10 * time.Second), LeaseExpiresAt: base.Add(40 * time.Second),
	})
	if err != nil || won {
		t.Fatalf("B early claim = %v, %v; want false, nil", won, err)
	}
	// After expiry B takes over.
	won, err = s.ClaimWorkflowRunLease(ctx, coreworkflow.ClaimLeaseInput{
		WorkflowRunID: run, Owner: "B", Now: base.Add(31 * time.Second), LeaseExpiresAt: base.Add(61 * time.Second),
	})
	if err != nil || !won {
		t.Fatalf("B takeover = %v, %v; want true, nil", won, err)
	}
	// Stale owner A can neither renew nor release B's lease.
	renewed, err := s.RenewWorkflowRunLease(ctx, coreworkflow.RenewLeaseInput{
		WorkflowRunID: run, Owner: "A", LeaseExpiresAt: base.Add(120 * time.Second),
	})
	if err != nil || renewed {
		t.Fatalf("stale A renew = %v, %v; want false, nil", renewed, err)
	}
	released, err := s.ReleaseWorkflowRunLease(ctx, coreworkflow.ReleaseLeaseInput{
		WorkflowRunID: run, Owner: "A",
	})
	if err != nil || released {
		t.Fatalf("stale A release = %v, %v; want false, nil", released, err)
	}
	// B's lease is intact after A's failed attempts.
	stored, err := s.GetWorkflowRun(ctx, run)
	if err != nil {
		t.Fatalf("GetWorkflowRun: %v", err)
	}
	if stored.ReconcileOwner == nil || *stored.ReconcileOwner != "B" {
		t.Fatalf("owner = %v, want B still holding the lease", stored.ReconcileOwner)
	}

	// B renews and then releases with a next schedule.
	renewed, err = s.RenewWorkflowRunLease(ctx, coreworkflow.RenewLeaseInput{
		WorkflowRunID: run, Owner: "B", LeaseExpiresAt: base.Add(90 * time.Second),
	})
	if err != nil || !renewed {
		t.Fatalf("B renew = %v, %v; want true, nil", renewed, err)
	}
	next := base.Add(5 * time.Minute)
	released, err = s.ReleaseWorkflowRunLease(ctx, coreworkflow.ReleaseLeaseInput{
		WorkflowRunID: run, Owner: "B", NextReconcileAt: &next,
	})
	if err != nil || !released {
		t.Fatalf("B release = %v, %v; want true, nil", released, err)
	}
	stored, err = s.GetWorkflowRun(ctx, run)
	if err != nil {
		t.Fatalf("GetWorkflowRun: %v", err)
	}
	if stored.ReconcileOwner != nil || stored.LeaseExpiresAt != nil {
		t.Errorf("release left a lease behind: %+v", stored)
	}
	if stored.NextReconcileAt == nil || !stored.NextReconcileAt.Equal(next) {
		t.Errorf("next_reconcile_at = %v, want %v", stored.NextReconcileAt, next)
	}
}

func TestWorkflowReconciliationLease_TerminalRemovesFromDue(t *testing.T) {
	s, userID, wf := reconcileFixture(t)
	ctx := context.Background()
	base := time.Unix(1_800_000_000, 0).UTC()
	past := base.Add(-10 * time.Second)
	run := createReconcileRun(t, s, wf, userID, coreworkflow.RunStatusRunning)

	// Give it a lease and a past schedule, then confirm it is due.
	if _, err := s.ClaimWorkflowRunLease(ctx, coreworkflow.ClaimLeaseInput{
		WorkflowRunID: run, Owner: "A", Now: base, LeaseExpiresAt: base.Add(30 * time.Second),
	}); err != nil {
		t.Fatalf("claim: %v", err)
	}
	setSchedule(t, s, run, &past, nil, nil)
	if !contains(runIDs(mustDue(t, s, base)), run) {
		t.Fatalf("run was not due before it finished")
	}

	// Finish the run; the transition must clear its scheduling and lease.
	ended := base
	ok, err := s.TransitionWorkflowRun(ctx, coreworkflow.TransitionRunInput{
		WorkflowRunID:  run,
		ExpectedStatus: coreworkflow.RunStatusRunning,
		NewStatus:      coreworkflow.RunStatusSucceeded,
		EndedAt:        &ended,
	})
	if err != nil || !ok {
		t.Fatalf("TransitionWorkflowRun = %v, %v; want true, nil", ok, err)
	}
	stored, err := s.GetWorkflowRun(ctx, run)
	if err != nil {
		t.Fatalf("GetWorkflowRun: %v", err)
	}
	if stored.ReconcileOwner != nil || stored.LeaseExpiresAt != nil || stored.NextReconcileAt != nil {
		t.Errorf("terminal run kept scheduling/ownership state: %+v", stored)
	}
	if contains(runIDs(mustDue(t, s, base)), run) {
		t.Errorf("a terminal run remained in the due set")
	}
}

func mustDue(t *testing.T, s *Store, now time.Time) []coreworkflow.Run {
	t.Helper()
	due, err := s.ListDueWorkflowRuns(context.Background(), now, 0)
	if err != nil {
		t.Fatalf("ListDueWorkflowRuns: %v", err)
	}
	return due
}

func runIDs(runs []coreworkflow.Run) []string {
	out := make([]string, len(runs))
	for i := range runs {
		out[i] = runs[i].ID
	}
	return out
}

func equalIDs(a, b []string) bool {
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

func contains(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}
