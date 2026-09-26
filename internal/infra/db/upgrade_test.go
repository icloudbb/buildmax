package db

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	coreschedule "github.com/icloudbb/buildmax/internal/core/schedule"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
)

// upgradeFixtureManifest mirrors what `./make release upgrade-fixture` writes
// into a dump's header (tools/mk/upgrade_fixture.go): the public IDs of the
// entities it seeded through the source release's API.
type upgradeFixtureManifest struct {
	SourceImage     string `json:"source_image"`
	OwnerID         string `json:"owner_id"`
	OwnerEmail      string `json:"owner_email"`
	SpaceID         string `json:"space_id"`
	AgentID         string `json:"agent_id"`
	IssueID         string `json:"issue_id"`
	WorkflowID      string `json:"workflow_id"`
	WorkflowRunID   string `json:"workflow_run_id"`
	FiredScheduleID string `json:"fired_schedule_id"`
	FiredTaskID     string `json:"fired_task_id"`
	IdleScheduleID  string `json:"idle_schedule_id"`
	ArtifactID      string `json:"artifact_id"`
	ArtifactSHA256  string `json:"artifact_sha256"`
}

// TestUpgradeFromPredecessorSchema upgrades what a released server actually
// left in MySQL — its DDL, its rows, its migration ledger — with this binary's
// New, and asserts every seeded entity reads back through the store.
//
// The backfill tests above simulate the old shape by adding retired columns to
// the current schema, which proves a migration's SQL but not that the
// candidate opens the predecessor's database: its column types, indexes, and
// rows its own code wrote. The fixture is the declared upgrade source; the
// release process refreshes it (docs/contribute/releasing.md).
func TestUpgradeFromPredecessorSchema(t *testing.T) {
	dumps, err := filepath.Glob(filepath.Join("testdata", "schema", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	// An empty directory would pass by testing nothing.
	if len(dumps) == 0 {
		t.Fatal("no upgrade fixture in testdata/schema; run `./make release upgrade-fixture <tag>` for the release deployments upgrade from")
	}
	for _, dump := range dumps {
		tag := strings.TrimSuffix(filepath.Base(dump), ".sql")
		t.Run(tag, func(t *testing.T) {
			testUpgradeFrom(t, dump, tag)
		})
	}
}

var nonIdentifier = regexp.MustCompile(`[^A-Za-z0-9_]`)

func testUpgradeFrom(t *testing.T, dump, tag string) {
	body, err := os.ReadFile(dump)
	if err != nil {
		t.Fatal(err)
	}
	manifest := readUpgradeManifest(t, body)
	dsn := siblingDatabase(t, "upgrade_"+nonIdentifier.ReplaceAllString(tag, "_"))
	ctx := context.Background()
	loadDump(t, dsn, body)

	s, err := New(ctx, dsn, Options{})
	if err != nil {
		t.Fatalf("New against the %s schema: %v", tag, err)
	}
	t.Cleanup(func() { _ = s.Close() })

	applied, err := appliedMigrations(ctx, s.db)
	if err != nil {
		t.Fatalf("read schema_migration: %v", err)
	}
	for _, m := range migrations {
		if !applied[m.ID] {
			t.Errorf("ledger lacks %s after the upgrade", m.ID)
		}
	}
	if unknown := unknownMigrations(applied); len(unknown) != 0 {
		t.Errorf("ledger records %v, which this binary does not know", unknown)
	}

	owner, err := s.GetUser(ctx, manifest.OwnerID)
	if err != nil || owner.Email != manifest.OwnerEmail {
		t.Errorf("owner %s = %+v, %v; want email %s", manifest.OwnerID, owner, err, manifest.OwnerEmail)
	}
	if space, err := s.GetSpace(ctx, manifest.SpaceID); err != nil {
		t.Errorf("space %s: %v", manifest.SpaceID, err)
	} else if space.CreatedBy != manifest.OwnerID {
		t.Errorf("space %s created by %q, want %q", manifest.SpaceID, space.CreatedBy, manifest.OwnerID)
	}
	if agent, err := s.GetAgent(ctx, manifest.AgentID); err != nil {
		t.Errorf("agent %s: %v", manifest.AgentID, err)
	} else if agent.SpaceID != manifest.SpaceID {
		t.Errorf("agent %s is in space %q, want %q", manifest.AgentID, agent.SpaceID, manifest.SpaceID)
	}

	if issue, err := s.GetIssue(ctx, manifest.IssueID); err != nil {
		t.Errorf("issue %s: %v", manifest.IssueID, err)
	} else {
		if issue.OwnerID == nil || *issue.OwnerID != manifest.OwnerID {
			t.Errorf("issue OwnerID = %v, want %s", issue.OwnerID, manifest.OwnerID)
		}
		if issue.ExecutorKind == nil || *issue.ExecutorKind != coreissue.ExecutorAgent ||
			issue.ExecutorID == nil || *issue.ExecutorID != manifest.AgentID {
			t.Errorf("issue executor = (%v, %v), want (%s, %s)", issue.ExecutorKind, issue.ExecutorID, coreissue.ExecutorAgent, manifest.AgentID)
		}
	}
	if comments, total, err := s.ListIssueComments(ctx, manifest.IssueID, 10, 0); err != nil || total != 1 || len(comments) != 1 {
		t.Errorf("issue comments = %d of %d, %v; want the one seeded", len(comments), total, err)
	}

	// The fired schedule is the schedule_agent_to_executor backfill on rows the
	// source wrote: its agent becomes the executor and its last task the fire
	// reference. The idle one never fired and keeps no reference.
	for _, tc := range []struct {
		id      string
		wantRef string
	}{
		{manifest.FiredScheduleID, manifest.FiredTaskID},
		{manifest.IdleScheduleID, ""},
	} {
		got, err := s.GetSchedule(ctx, tc.id)
		if err != nil {
			t.Errorf("schedule %s: %v", tc.id, err)
			continue
		}
		if got.ExecutorKind != coreschedule.ExecutorAgent || got.ExecutorID != manifest.AgentID {
			t.Errorf("schedule %s executor = (%q, %q), want (%q, %q)", tc.id, got.ExecutorKind, got.ExecutorID, coreschedule.ExecutorAgent, manifest.AgentID)
		}
		switch {
		case tc.wantRef == "" && got.LastFireRef != nil:
			t.Errorf("schedule %s LastFireRef = %q, want none", tc.id, *got.LastFireRef)
		case tc.wantRef != "" && (got.LastFireRef == nil || *got.LastFireRef != tc.wantRef):
			t.Errorf("schedule %s LastFireRef = %v, want %s", tc.id, got.LastFireRef, tc.wantRef)
		}
	}
	if task, err := s.GetTask(ctx, manifest.FiredTaskID); err != nil {
		t.Errorf("fired task %s: %v", manifest.FiredTaskID, err)
	} else if task.SpaceID != manifest.SpaceID {
		t.Errorf("fired task is in space %q, want %q", task.SpaceID, manifest.SpaceID)
	}

	if workflow, err := s.GetWorkflow(ctx, manifest.WorkflowID); err != nil {
		t.Errorf("workflow %s: %v", manifest.WorkflowID, err)
	} else if workflow.Status != coreworkflow.StatusPublished {
		t.Errorf("workflow status = %q, want %q", workflow.Status, coreworkflow.StatusPublished)
	}
	if run, err := s.GetWorkflowRun(ctx, manifest.WorkflowRunID); err != nil {
		t.Errorf("workflow run %s: %v", manifest.WorkflowRunID, err)
	} else if run.WorkflowID != manifest.WorkflowID {
		t.Errorf("workflow run belongs to %q, want %q", run.WorkflowID, manifest.WorkflowID)
	}
	if nodes, err := s.ListWorkflowNodeRuns(ctx, manifest.WorkflowRunID); err != nil || len(nodes) != 1 {
		t.Errorf("workflow node runs = %d, %v; want the one step", len(nodes), err)
	}

	if artifact, err := s.GetArtifact(ctx, manifest.ArtifactID); err != nil {
		t.Errorf("artifact %s: %v", manifest.ArtifactID, err)
	} else if artifact.SpaceID != manifest.SpaceID || artifact.SHA256 != manifest.ArtifactSHA256 {
		t.Errorf("artifact = space %q sha256 %q, want %q %q", artifact.SpaceID, artifact.SHA256, manifest.SpaceID, manifest.ArtifactSHA256)
	}

	// The upgraded schema accepts the candidate's writes. A retired NOT NULL
	// column left behind — schedule.agent_id is the one refuseNewerSchema's
	// comment describes — fails exactly here.
	if _, err := s.CreateSchedule(ctx, &coreschedule.CreateInput{
		SpaceID: manifest.SpaceID, ExecutorKind: coreschedule.ExecutorAgent, ExecutorID: manifest.AgentID,
		CreatedBy: manifest.OwnerID, Name: "after upgrade", Input: "x", CronExpr: "0 9 * * *", Timezone: "UTC",
		Enabled: true, NextFireAt: time.Unix(1_800_000_000, 0).UTC(),
	}); err != nil {
		t.Errorf("CreateSchedule on the upgraded schema: %v", err)
	}
	if _, err := s.CreateIssueInSpace(ctx, manifest.SpaceID, manifest.OwnerID, coreissue.CreateInput{Title: "after upgrade"}); err != nil {
		t.Errorf("CreateIssueInSpace on the upgraded schema: %v", err)
	}

	// The next start of the same binary finds nothing left to do.
	again, err := New(ctx, dsn, Options{})
	if err != nil {
		t.Fatalf("second New after the upgrade: %v", err)
	}
	_ = again.Close()
}

func readUpgradeManifest(t *testing.T, body []byte) upgradeFixtureManifest {
	t.Helper()
	const prefix = "-- manifest: "
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "--") {
			break
		}
		if encoded, ok := strings.CutPrefix(line, prefix); ok {
			var m upgradeFixtureManifest
			if err := json.Unmarshal([]byte(encoded), &m); err != nil {
				t.Fatalf("manifest: %v", err)
			}
			for name, value := range map[string]string{
				"owner_id": m.OwnerID, "space_id": m.SpaceID, "agent_id": m.AgentID, "issue_id": m.IssueID,
				"workflow_id": m.WorkflowID, "workflow_run_id": m.WorkflowRunID, "fired_schedule_id": m.FiredScheduleID,
				"fired_task_id": m.FiredTaskID, "idle_schedule_id": m.IdleScheduleID, "artifact_id": m.ArtifactID,
			} {
				if value == "" {
					t.Fatalf("manifest has no %s; regenerate the fixture", name)
				}
			}
			return m
		}
	}
	t.Fatal("the dump's header has no manifest line; regenerate it with `./make release upgrade-fixture`")
	return upgradeFixtureManifest{}
}

// loadDump replays a mysqldump file into the database dsn names, the way an
// operator restoring it with the mysql client would.
func loadDump(t *testing.T, dsn string, body []byte) {
	t.Helper()
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MultiStatements = true
	conn, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// One Exec on one connection: the dump's session settings (foreign key
	// checks, time zone) must hold for every statement after them.
	if _, err := conn.Exec(string(body)); err != nil {
		t.Fatalf("load the dump: %v", err)
	}
}
