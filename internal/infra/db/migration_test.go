package db

import (
	"context"
	"os"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	"gorm.io/gorm"
)

// TestMigrationsAreWellFormed runs without a database, because the properties
// it checks are what make the list safe to append to and are easy to break in
// a hurry.
func TestMigrationsAreWellFormed(t *testing.T) {
	seen := make(map[string]bool, len(migrations))
	for i, m := range migrations {
		if m.ID == "" {
			t.Errorf("migration %d has no ID; schema_migration cannot record it", i)
		}
		if m.Apply == nil {
			t.Errorf("migration %q has no Apply", m.ID)
		}
		// A duplicate ID means the second one is recorded as already applied
		// and silently never runs.
		if seen[m.ID] {
			t.Errorf("duplicate migration ID %q", m.ID)
		}
		seen[m.ID] = true
	}
}

// TestMigrationsArePermanent is the append-only guard.
//
// Every ID here has been recorded in deployed databases, so editing, removing,
// or reordering one changes what an upgraded database gets relative to a fresh
// one — the divergence the list exists to prevent. A contributor who appends a
// migration adds its ID to the end of want. The list restarted empty at the
// identity cutover; system_grant_live_marker is the first entry appended after.
func TestMigrationsArePermanent(t *testing.T) {
	want := []string{
		"system_grant_live_marker",
		"llm_model_credential_encryption",
		"issue_owner_executor_split",
		"workflow_step_run_to_node_run",
		"schedule_agent_to_executor",
	}
	if len(migrations) != len(want) {
		t.Fatalf("migrations = %d entries, permanent list has %d; append the new ID to want", len(migrations), len(want))
	}
	for i := range want {
		if migrations[i].ID != want[i] {
			t.Errorf("migration %d ID = %q, want %q", i, migrations[i].ID, want[i])
		}
	}
}

// TestWarnIfSchemaIsAhead exercises the N-1 promise: a binary one release
// behind a migrated database must keep running, so an unknown migration is a
// warning rather than a refusal. The function returning normally is the
// assertion — a future refusal here would break every rolling upgrade.
func TestWarnIfSchemaIsAhead(t *testing.T) {
	warnIfSchemaIsAhead(map[string]bool{
		"0001_artifact_tables_to_task_run_artifact": true,
		"9999_from_a_newer_release":                 true,
	})
	warnIfSchemaIsAhead(nil)
}

// TestIssueOwnerExecutorSplitMigration proves the backfill itself, not just
// that runMigrations records it: a person's old assignee_id becomes owner_id,
// resolved from the opaque public_id string into the internal key that column
// now holds; an agent's or workflow's carries straight over into
// executor_kind/executor_id, which stay opaque; and the old columns are gone
// afterward. It calls the migration's Apply directly, bypassing the
// schema_migration recorded-once guard, because a database that already
// migrated on the way to this test run would otherwise make Apply a no-op
// before it had anything pre-split to transform.
func TestIssueOwnerExecutorSplitMigration(t *testing.T) {
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping migration integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ownerPublicID := newTestUser(t, s, "migration-owner")
	spaceID := newTestSpace(t, s, ownerPublicID)
	personIssue, err := s.CreateIssueInSpace(ctx, spaceID, ownerPublicID, coreissue.CreateInput{Title: "Owned by a person"})
	if err != nil {
		t.Fatalf("CreateIssueInSpace (person): %v", err)
	}
	agentIssue, err := s.CreateIssueInSpace(ctx, spaceID, ownerPublicID, coreissue.CreateInput{Title: "Assigned to an agent"})
	if err != nil {
		t.Fatalf("CreateIssueInSpace (agent): %v", err)
	}

	// Simulate a pre-split deployment: add the retired columns back and fill
	// them the way the old assignee field did, keyed by the opaque public_id
	// string every *_id column in that era used.
	// This test's own database is created fresh for this run (see
	// docs/contribute/testing.md), so the issue table's current AutoMigrate
	// never added these columns in the first place -- nothing to guard with
	// IF NOT EXISTS.
	if err := s.db.WithContext(ctx).Exec(
		"ALTER TABLE issue ADD COLUMN assignee_kind VARCHAR(32)",
	).Error; err != nil {
		t.Fatalf("simulate pre-split columns (assignee_kind): %v", err)
	}
	if err := s.db.WithContext(ctx).Exec(
		"ALTER TABLE issue ADD COLUMN assignee_id VARCHAR(64)",
	).Error; err != nil {
		t.Fatalf("simulate pre-split columns (assignee_id): %v", err)
	}
	if err := s.db.WithContext(ctx).Exec(
		"UPDATE issue SET assignee_kind = 'person', assignee_id = ? WHERE public_id = ?", ownerPublicID, personIssue.ID,
	).Error; err != nil {
		t.Fatalf("seed person assignee: %v", err)
	}
	const fakeAgentID = "a_migration_test"
	if err := s.db.WithContext(ctx).Exec(
		"UPDATE issue SET assignee_kind = 'agent', assignee_id = ? WHERE public_id = ?", fakeAgentID, agentIssue.ID,
	).Error; err != nil {
		t.Fatalf("seed agent assignee: %v", err)
	}

	var target Migration
	for _, candidate := range migrations {
		if candidate.ID == "issue_owner_executor_split" {
			target = candidate
		}
	}
	if target.Apply == nil {
		t.Fatal("issue_owner_executor_split migration not found")
	}
	if err := target.Apply(ctx, s.db); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	afterPerson, err := s.GetIssue(ctx, personIssue.ID)
	if err != nil {
		t.Fatalf("GetIssue (person): %v", err)
	}
	if afterPerson.OwnerID == nil || *afterPerson.OwnerID != ownerPublicID {
		t.Errorf("person issue OwnerID = %v, want %q", afterPerson.OwnerID, ownerPublicID)
	}
	if afterPerson.ExecutorKind != nil || afterPerson.ExecutorID != nil {
		t.Errorf("person issue executor = (%v, %v), want (nil, nil)", afterPerson.ExecutorKind, afterPerson.ExecutorID)
	}

	afterAgent, err := s.GetIssue(ctx, agentIssue.ID)
	if err != nil {
		t.Fatalf("GetIssue (agent): %v", err)
	}
	if afterAgent.ExecutorKind == nil || *afterAgent.ExecutorKind != coreissue.ExecutorAgent {
		t.Errorf("agent issue ExecutorKind = %v, want %q", afterAgent.ExecutorKind, coreissue.ExecutorAgent)
	}
	if afterAgent.ExecutorID == nil || *afterAgent.ExecutorID != fakeAgentID {
		t.Errorf("agent issue ExecutorID = %v, want %q", afterAgent.ExecutorID, fakeAgentID)
	}
	if afterAgent.OwnerID != nil {
		t.Errorf("agent issue OwnerID = %v, want nil", afterAgent.OwnerID)
	}

	m := s.db.Migrator()
	if m.HasColumn(&issueRow{}, "assignee_kind") {
		t.Error("assignee_kind column still exists after the migration")
	}
	if m.HasColumn(&issueRow{}, "assignee_id") {
		t.Error("assignee_id column still exists after the migration")
	}
}

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping migration integration test")
	}
	// Open the way New does — utcDSN turns on parseTime, so reading a recorded
	// migration's applied_at scans into time.Time rather than []byte. The raw
	// driver DSN this test used lacked it, which stayed invisible only while the
	// migrations list was empty and nothing was ever recorded to read back.
	utc, err := utcDSN(dsn)
	if err != nil {
		t.Fatalf("dsn: %v", err)
	}
	db, err := gorm.Open(mysqlDialector(utc), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return db
}

// TestRunMigrationsRecordsAndSkips asserts the property the schema_migration
// table exists for: a migration runs once, and a second start does not run it
// again.
func TestRunMigrationsRecordsAndSkips(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	if err := runMigrations(ctx, db); err != nil {
		t.Fatalf("first run: %v", err)
	}
	applied, err := appliedMigrations(ctx, db)
	if err != nil {
		t.Fatalf("appliedMigrations: %v", err)
	}
	for _, m := range migrations {
		if !applied[m.ID] {
			t.Errorf("migration %q was not recorded", m.ID)
		}
	}

	// A second start must be a no-op. Applying twice is what the recording
	// prevents, and an Apply that ran again would be visible here as an error
	// from a repeated DDL statement.
	if err := runMigrations(ctx, db); err != nil {
		t.Fatalf("second run should be a no-op: %v", err)
	}
	after, err := appliedMigrations(ctx, db)
	if err != nil {
		t.Fatalf("appliedMigrations: %v", err)
	}
	if len(after) != len(applied) {
		t.Errorf("second run changed the record count: %d then %d", len(applied), len(after))
	}
}
