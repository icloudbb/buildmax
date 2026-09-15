package db

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	coreschema "github.com/icloudbb/buildmax/internal/core/schema"
	"gorm.io/gorm"
)

// schemaMigrationRow records one applied migration. Rows are never deleted:
// the table is the answer to "what has this database had done to it", and a
// missing row means a migration runs again.
type schemaMigrationRow struct {
	// ID is the migration's permanent identifier. 191 characters is the
	// longest indexable varchar under MySQL's utf8mb4 index limit.
	ID        string    `gorm:"column:id;type:varchar(191);primaryKey"`
	AppliedAt time.Time `gorm:"column:applied_at;not null"`
}

func (schemaMigrationRow) TableName() string { return "schema_migration" }

// AppliedMigrations implements coreschema.Store.
//
// AutoMigrate's additive DDL is not in this table — only the steps a struct
// cannot express. A reader should take it as "what has been done beyond the row
// structs", not as a complete schema version.
func (s *Store) AppliedMigrations(ctx context.Context) ([]coreschema.Migration, error) {
	var rows []schemaMigrationRow
	if err := s.db.WithContext(ctx).Order("applied_at ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]coreschema.Migration, 0, len(rows))
	for _, row := range rows {
		out = append(out, coreschema.Migration{ID: row.ID, AppliedAt: row.AppliedAt})
	}
	return out, nil
}

// Migration is one forward step in the schema's history.
//
// There is deliberately no Down. BuildMax's schema moves forward only:
// compatibility with the previous release is carried by each change being
// additive, not by an undo path. A rollback is a rollback of the binary, and
// the schema it left behind must keep serving it — see
// docs/contribute/architecture/data-model.md.
type Migration struct {
	// ID is permanent and unique. It is what schema_migration records, so
	// renaming one makes it run a second time on every existing database.
	ID string
	// Apply performs the change.
	//
	// It must tolerate being run against a database that already has the
	// change: a crash between applying and recording leaves the migration
	// pending, and the next start will retry it.
	Apply func(ctx context.Context, db *gorm.DB) error
}

// migrations run in the order listed.
//
// Append only. An existing entry has already been recorded in deployments, so
// editing or reordering one changes what an upgraded database gets relative to
// a fresh one — which is exactly the divergence this list exists to prevent.
//
// The identity cutover reset every database that had entries in it; its three
// predecessors described a schema that no longer exists and data no deployment
// kept. Append-only restarts from there.
var migrations = []Migration{
	{
		// system_grant's live-uniqueness index moved off revoked_at, whose NULLs
		// MySQL treats as distinct — so it never stopped a second live grant —
		// onto live_marker, which is one fixed value while active and NULL once
		// revoked. AutoMigrate adds the column and builds the index on a fresh
		// database, but it will not rebuild an index of the same name on an
		// existing one, so the change is expressed here for those.
		ID: "system_grant_live_marker",
		Apply: func(ctx context.Context, db *gorm.DB) error {
			m := db.WithContext(ctx).Migrator()
			if !m.HasTable(&systemGrantRow{}) {
				// AutoMigrate builds the table and the correct index from the row
				// struct on a fresh database, so there is nothing to convert.
				return nil
			}
			// Set the marker on the live rows before the unique index exists, so
			// a second live grant collides once it does. Guarded on IS NULL so a
			// retry after a crash is a no-op.
			if err := db.WithContext(ctx).Exec(
				"UPDATE system_grant SET live_marker = 1 WHERE revoked_at IS NULL AND live_marker IS NULL").Error; err != nil {
				return err
			}
			if m.HasIndex(&systemGrantRow{}, "idx_system_grant_live") {
				if err := m.DropIndex(&systemGrantRow{}, "idx_system_grant_live"); err != nil {
					return err
				}
			}
			return m.CreateIndex(&systemGrantRow{}, "idx_system_grant_live")
		},
	},
	{
		// Model provider credentials became encrypted at rest: the plaintext
		// api_key column gave way to api_key_sealed (an envelope blob under the
		// deployment KEK). AutoMigrate adds the new column from the row struct;
		// this drops the old one. Existing plaintext keys are not carried over --
		// at Alpha there is no data to preserve, so those models are re-added.
		ID: "llm_model_credential_encryption",
		Apply: func(ctx context.Context, db *gorm.DB) error {
			m := db.WithContext(ctx).Migrator()
			if m.HasColumn(&llmModelRow{}, "api_key") {
				return m.DropColumn(&llmModelRow{}, "api_key")
			}
			return nil
		},
	},
	{
		// Issue's combined assignee (assignee_kind admitting person, agent, or
		// workflow) could not represent a human owner and an Agent/Workflow
		// executor at once. owner_id and executor_kind/executor_id, added by
		// AutoMigrate from the row struct, replace it: this backfills the split
		// from the old columns, then drops them. A person's assignee_id was the
		// public_id string opaque columns use, resolved here against `user` into
		// the internal key owner_id actually stores -- unlike ExecutorID, an
		// owner is always a user row, so it is a resolved reference, not another
		// opaque handle. An agent's or workflow's assignee_id becomes
		// executor_id verbatim, since ExecutorID stays opaque. See
		// docs/design/portal-work-and-execution-experience.md.
		ID: "issue_owner_executor_split",
		Apply: func(ctx context.Context, db *gorm.DB) error {
			m := db.WithContext(ctx).Migrator()
			if !m.HasColumn(&issueRow{}, "assignee_kind") {
				// Fresh database: AutoMigrate never created the old columns.
				return nil
			}
			if err := db.WithContext(ctx).Exec(
				"UPDATE issue i JOIN `user` u ON u.public_id = i.assignee_id " +
					"SET i.owner_id = u.id " +
					"WHERE i.assignee_kind = 'person' AND i.assignee_id IS NOT NULL AND i.owner_id IS NULL").Error; err != nil {
				return err
			}
			if err := db.WithContext(ctx).Exec(
				"UPDATE issue SET executor_kind = assignee_kind, executor_id = assignee_id " +
					"WHERE assignee_kind IN ('agent', 'workflow') AND executor_kind IS NULL").Error; err != nil {
				return err
			}
			if err := m.DropColumn(&issueRow{}, "assignee_kind"); err != nil {
				return err
			}
			return m.DropColumn(&issueRow{}, "assignee_id")
		},
	},
	{
		// A WorkflowRun's per-step records became node runs: the row struct is now
		// workflowNodeRunRow (table workflow_node_run) with node_id/node_index/
		// node_type and the persisted resolved_input/output columns. AutoMigrate
		// builds the new table from the struct; this drops the old
		// workflow_step_run. At Alpha there is no data to preserve, so runs
		// recorded under the old table are abandoned rather than migrated -- the
		// new table starts empty. See docs/design/workflow-runtime.md §19.
		ID: "workflow_step_run_to_node_run",
		Apply: func(ctx context.Context, db *gorm.DB) error {
			m := db.WithContext(ctx).Migrator()
			if m.HasTable("workflow_step_run") {
				return m.DropTable("workflow_step_run")
			}
			return nil
		},
	},
}

// runMigrations applies every migration this binary knows and the database has
// not recorded.
//
// AutoMigrate has already run by this point and owns additive DDL — new tables,
// new columns, new indexes. This list owns everything AutoMigrate cannot
// express: backfills, drops, and renames. Keeping the split sharp is what makes
// "the row structs are the schema" true while still leaving a record of the
// changes a struct cannot describe.
func runMigrations(ctx context.Context, db *gorm.DB) error {
	if err := db.WithContext(ctx).AutoMigrate(&schemaMigrationRow{}); err != nil {
		return fmt.Errorf("create schema_migration: %w", err)
	}
	applied, err := appliedMigrations(ctx, db)
	if err != nil {
		return err
	}
	for _, m := range migrations {
		if applied[m.ID] {
			continue
		}
		if err := m.Apply(ctx, db); err != nil {
			return fmt.Errorf("migration %s: %w", m.ID, err)
		}
		row := schemaMigrationRow{ID: m.ID, AppliedAt: time.Now().UTC()}
		if err := db.WithContext(ctx).Create(&row).Error; err != nil {
			return fmt.Errorf("record migration %s: %w", m.ID, err)
		}
		slog.Info("applied schema migration", "id", m.ID)
	}
	warnIfSchemaIsAhead(applied)
	return nil
}

func appliedMigrations(ctx context.Context, db *gorm.DB) (map[string]bool, error) {
	var rows []schemaMigrationRow
	if err := db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("read schema_migration: %w", err)
	}
	out := make(map[string]bool, len(rows))
	for _, r := range rows {
		out[r.ID] = true
	}
	return out, nil
}

// warnIfSchemaIsAhead reports migrations the database has and this binary does
// not.
//
// It warns rather than refuses, because that state is the N-1 promise working
// as intended: a server one release behind a migrated database is supposed to
// keep serving. A server several releases behind has no such promise, and this
// log line is the only signal an operator gets that they are in that position.
func warnIfSchemaIsAhead(applied map[string]bool) {
	known := make(map[string]bool, len(migrations))
	for _, m := range migrations {
		known[m.ID] = true
	}
	var unknown []string
	for id := range applied {
		if !known[id] {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) == 0 {
		return
	}
	slog.Warn("database schema is ahead of this binary; supported one release back, not more",
		"unknown_migrations", unknown)
}
