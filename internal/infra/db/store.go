// Package db provides persistence for BuildMax backend entities (MySQL via GORM).
package db

import (
	"context"
	"fmt"
	"log"
	"os"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Store implements the backend store interfaces with a MySQL backend.
type Store struct {
	db *gorm.DB
	// credentialCipher encrypts managed-model provider credentials at rest. Nil
	// when the deployment configures no encryption key, in which case storing a
	// credential is refused rather than written in the clear. Injected at
	// bootstrap; see SetCredentialCipher.
	credentialCipher CredentialCipher
}

// CredentialCipher seals and opens a managed-model provider credential. It is
// an interface so this package does not import the crypto implementation; the
// concrete cipher (internal/infra/secret, the deployment's KEK-backed envelope
// encryption) is injected at bootstrap. The blob SealValue returns is opaque
// and stored as one column.
type CredentialCipher interface {
	SealValue(plaintext string, aad []byte) ([]byte, error)
	OpenValue(blob []byte, aad []byte) (string, error)
}

// SetCredentialCipher attaches the credential encryptor the model catalog uses.
// Called at bootstrap only when a deployment encryption key is configured; left
// unset, storing a model credential is refused.
func (s *Store) SetCredentialCipher(c CredentialCipher) { s.credentialCipher = c }

// New opens a MySQL connection with the given DSN and brings the schema up to
// date. When the DSN names a database the server does not have, it is created
// first (see ensureDatabase), and then, in this order:
//
//  1. AutoMigrate over every row struct, which owns additive DDL — new tables,
//     new columns, new indexes. The row structs are the schema.
//  2. Seed the default quota tiers.
//  3. runMigrations, which owns everything a struct cannot express: backfills,
//     drops, and renames. Each one is recorded in schema_migration and runs at
//     most once per database.
//
// The schema moves forward only; see migration.go and
// docs/contribute/architecture/data-model.md.
//
// GORM logger is configured to ignore ErrRecordNotFound so expected "not found" lookups
// (e.g. GetNextPendingTaskRun when idle) do not spam the console.
func New(ctx context.Context, dsn string) (*Store, error) {
	gormLogger := logger.New(log.New(os.Stdout, "\r\n", log.LstdFlags), logger.Config{
		IgnoreRecordNotFoundError: true,
	})
	dsn, err := utcDSN(dsn)
	if err != nil {
		return nil, err
	}
	db, err := gorm.Open(mysqlDialector(dsn), &gorm.Config{Logger: gormLogger})
	if err != nil && missingDatabase(err) {
		// The schema, not just the tables: see ensureDatabase.
		if createErr := ensureDatabase(ctx, dsn); createErr != nil {
			return nil, fmt.Errorf("open mysql: %w (creating the database failed too: %v)", err, createErr)
		}
		db, err = gorm.Open(mysqlDialector(dsn), &gorm.Config{Logger: gormLogger})
	}
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	if err := db.WithContext(ctx).AutoMigrate(&userRow{}, &spaceRow{}, &spaceMemberRow{}, &spaceInvitationRow{}, &workflowRow{}, &workflowRevisionRow{}, &workflowRunRow{}, &workflowStepRunRow{}, &issueRow{}, &issueCommentRow{}, &agentRow{}, &agentRevisionRow{}, &scheduleRow{}, &taskRow{}, &taskRunRow{}, &artifactRow{}, &artifactShareRow{}, &quotaTierRow{}, &conversationRow{}, &conversationMessageRow{}, &userWebhookKeyRow{}, &loginCodeRow{}, &authSessionRow{}, &externalIdentityRow{}, &userRefreshTokenRow{}, &llmCallRow{}, &llmModelRow{}, &auditEventRow{}, &systemGrantRow{}, &pluginRow{}, &pluginReleaseRow{}, &pluginActivationRow{}, &pluginEnvironmentRow{}, &secretRow{}, &taskRunSecretRow{}, &workspaceCheckpointRow{}, &schemaMigrationRow{}); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := (&Store{db: db}).SeedDefaultQuotaTiers(ctx); err != nil {
		return nil, fmt.Errorf("seed quota tiers: %w", err)
	}
	if err := runMigrations(ctx, db); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

// Ping verifies the database is reachable and the pool can hand out a
// connection. Used by the server's readiness probe, so it must stay cheap
// enough to run on every probe interval.
func (s *Store) Ping(ctx context.Context) error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

// Close closes the underlying DB connection. Optional for server lifecycle.
func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// datetimePrecision is the fractional-second precision of every instant column.
//
// GORM's MySQL driver defaults to DATETIME(3); 6 is what
// docs/design/timestamp-representation.md chose, so that a burst of rows inside
// one millisecond still carries distinguishable times. It costs nothing: a
// DATETIME(6) is 8 bytes, the same as the bigint it replaced.
const datetimePrecision = 6

func mysqlDialector(dsn string) gorm.Dialector {
	precision := datetimePrecision
	return mysql.New(mysql.Config{DSN: dsn, DefaultDatetimePrecision: &precision})
}
