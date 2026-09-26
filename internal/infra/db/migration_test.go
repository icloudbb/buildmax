package db

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/icloudbb/buildmax/internal/config"
	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	coreschedule "github.com/icloudbb/buildmax/internal/core/schedule"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
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

// TestUnknownMigrations pins what the newer-schema refusal names: only the IDs
// this binary lacks, sorted so the message is stable across starts.
func TestUnknownMigrations(t *testing.T) {
	got := unknownMigrations(map[string]bool{
		"zz_from_a_newer_release":    true,
		"system_grant_live_marker":   true,
		"aa_from_a_newer_release":    true,
		"schedule_agent_to_executor": true,
	})
	want := []string{"aa_from_a_newer_release", "zz_from_a_newer_release"}
	if !slices.Equal(got, want) {
		t.Errorf("unknownMigrations = %v, want %v", got, want)
	}
	if got := unknownMigrations(nil); len(got) != 0 {
		t.Errorf("unknownMigrations(nil) = %v, want none", got)
	}
}

// siblingDatabase creates an empty database of the calling test's own beside
// the scope's shared one, drops it afterward, and returns its DSN. The
// newer-schema test plants an unknown migration ID, which in the shared ledger
// would make every later test's New refuse.
func siblingDatabase(t *testing.T, suffix string) string {
	t.Helper()
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping migration integration test")
	}
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	name := cfg.DBName + "_" + suffix
	serverCfg := *cfg
	serverCfg.DBName = ""
	conn, err := sql.Open("mysql", serverCfg.FormatDSN())
	if err != nil {
		t.Fatalf("open server connection: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := conn.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	t.Cleanup(func() {
		if _, err := conn.Exec("DROP DATABASE IF EXISTS `" + name + "`"); err != nil {
			t.Errorf("drop %s: %v", name, err)
		}
	})
	cfg.DBName = name
	return cfg.FormatDSN()
}

// TestNewRefusesNewerSchema proves the rollback guard: a database whose ledger
// records a migration this binary does not know is refused before AutoMigrate
// issues any DDL, and the explicit override lets it start. A database with no
// ledger is the other side of the guard, and every scope run exercises it on
// its first New.
func TestNewRefusesNewerSchema(t *testing.T) {
	dsn := siblingDatabase(t, "newer")
	ctx := context.Background()
	gdb := testDBAt(t, dsn)

	// A database a newer release migrated: its ledger holds every migration
	// this binary knows plus one it does not. The ledger is the only table, so
	// any other one after the refusal is DDL the refusal failed to prevent.
	if err := gdb.AutoMigrate(&schemaMigrationRow{}); err != nil {
		t.Fatalf("create ledger: %v", err)
	}
	const newer = "zz_migration_from_a_newer_release"
	ids := []string{newer}
	for _, m := range migrations {
		ids = append(ids, m.ID)
	}
	for _, id := range ids {
		if err := gdb.Create(&schemaMigrationRow{ID: id, AppliedAt: time.Now().UTC()}).Error; err != nil {
			t.Fatalf("record %s: %v", id, err)
		}
	}

	_, err := New(ctx, dsn, Options{})
	var refusal *NewerSchemaError
	if !errors.As(err, &refusal) {
		t.Fatalf("New against a newer schema = %v, want *NewerSchemaError", err)
	}
	if !slices.Equal(refusal.Unknown, []string{newer}) {
		t.Errorf("refusal names %v, want [%s]", refusal.Unknown, newer)
	}
	for _, mention := range []string{newer, "restore", "database.allow_newer_schema"} {
		if !strings.Contains(err.Error(), mention) {
			t.Errorf("refusal %q does not mention %q", err, mention)
		}
	}
	var tables []string
	if err := gdb.Raw("SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE()").Scan(&tables).Error; err != nil {
		t.Fatalf("list tables: %v", err)
	}
	if !slices.Equal(tables, []string{"schema_migration"}) {
		t.Fatalf("tables after the refusal = %v, want only schema_migration; the guard must run before any DDL", tables)
	}

	allowed, err := New(ctx, dsn, Options{AllowNewerSchema: true})
	if err != nil {
		t.Fatalf("New with AllowNewerSchema: %v", err)
	}
	t.Cleanup(func() { _ = allowed.Close() })
	if !allowed.db.Migrator().HasTable(&scheduleRow{}) {
		t.Error("the override started without building the schema")
	}
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
	s, err := New(ctx, dsn, Options{})
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

// TestScheduleAgentToExecutorMigration proves the schedule backfill the way
// TestIssueOwnerExecutorSplitMigration proves the issue one: a pre-cutover
// schedule's resolved agent_id becomes an agent executor holding that agent's
// public id, its last_task_id becomes last_fire_ref holding the task's public
// id, a schedule that never fired keeps no reference, and both old columns are
// gone afterward. It calls Apply directly for the same reason.
func TestScheduleAgentToExecutorMigration(t *testing.T) {
	s, ctx := newTestStore(t)
	f := newScheduleFixture(t, s, "migration-schedule")
	next := time.Unix(1_800_000_000, 0).UTC()
	fired := newTestSchedule(t, s, f, next, true)
	neverFired := newTestSchedule(t, s, f, next, true)
	task, err := s.CreateTask(ctx, &coretask.CreateInput{SpaceID: f.spaceID, AgentID: &f.agentID, Input: "x", CreatedBy: f.userID})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() {
		_ = s.db.Delete(&taskRow{}, "public_id = ?", canonicalPublicID(task.ID)).Error
		// The scope's later tests share this table; a failure before Apply must
		// not leave them the retired columns.
		for _, column := range []string{"agent_id", "last_task_id"} {
			if s.db.Migrator().HasColumn(&scheduleRow{}, column) {
				_ = s.db.Migrator().DropColumn(&scheduleRow{}, column)
			}
		}
	})

	// Simulate an alpha.14 schedule: the retired columns back, holding internal
	// keys, and the executor columns as AutoMigrate adds them to an existing row.
	// The scope's database is created for this run, so the current AutoMigrate
	// never added these columns.
	for _, stmt := range []string{
		"ALTER TABLE schedule ADD COLUMN agent_id BIGINT UNSIGNED NULL",
		"ALTER TABLE schedule ADD COLUMN last_task_id BIGINT UNSIGNED NULL",
	} {
		if err := s.db.WithContext(ctx).Exec(stmt).Error; err != nil {
			t.Fatalf("simulate pre-cutover columns (%s): %v", stmt, err)
		}
	}
	for _, id := range []string{fired.ID, neverFired.ID} {
		if err := s.db.WithContext(ctx).Exec(
			"UPDATE schedule s JOIN agent a ON a.public_id = ? "+
				"SET s.agent_id = a.id, s.executor_kind = '', s.executor_id = '', s.last_fire_ref = NULL "+
				"WHERE s.public_id = ?", f.agentID, id,
		).Error; err != nil {
			t.Fatalf("seed agent_id: %v", err)
		}
	}
	if err := s.db.WithContext(ctx).Exec(
		"UPDATE schedule s JOIN task t ON t.public_id = ? SET s.last_task_id = t.id WHERE s.public_id = ?",
		task.ID, fired.ID,
	).Error; err != nil {
		t.Fatalf("seed last_task_id: %v", err)
	}

	var target Migration
	for _, candidate := range migrations {
		if candidate.ID == "schedule_agent_to_executor" {
			target = candidate
		}
	}
	if target.Apply == nil {
		t.Fatal("schedule_agent_to_executor migration not found")
	}
	if err := target.Apply(ctx, s.db); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for _, tc := range []struct {
		id      string
		wantRef *string
	}{
		{fired.ID, &task.ID},
		{neverFired.ID, nil},
	} {
		got, err := s.GetSchedule(ctx, tc.id)
		if err != nil || got == nil {
			t.Fatalf("GetSchedule %s: %v (got %v)", tc.id, err, got)
		}
		if got.ExecutorKind != coreschedule.ExecutorAgent || got.ExecutorID != f.agentID {
			t.Errorf("schedule %s executor = (%q, %q), want (%q, %q)", tc.id, got.ExecutorKind, got.ExecutorID, coreschedule.ExecutorAgent, f.agentID)
		}
		switch {
		case tc.wantRef == nil && got.LastFireRef != nil:
			t.Errorf("schedule %s LastFireRef = %q, want none", tc.id, *got.LastFireRef)
		case tc.wantRef != nil && (got.LastFireRef == nil || *got.LastFireRef != *tc.wantRef):
			t.Errorf("schedule %s LastFireRef = %v, want %q", tc.id, got.LastFireRef, *tc.wantRef)
		}
	}

	m := s.db.Migrator()
	for _, column := range []string{"agent_id", "last_task_id"} {
		if m.HasColumn(&scheduleRow{}, column) {
			t.Errorf("%s column still exists after the migration", column)
		}
	}
}

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping migration integration test")
	}
	return testDBAt(t, dsn)
}

// testDBAt opens dsn without New's AutoMigrate and migrations, and closes it
// when the test ends.
func testDBAt(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
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
	t.Cleanup(func() { _ = (&Store{db: db}).Close() })
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
