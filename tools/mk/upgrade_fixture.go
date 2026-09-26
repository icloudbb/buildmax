package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// The upgrade fixture is the schema, data, and schema_migration ledger a
// released server wrote, dumped so the MySQL scope can upgrade it with the
// candidate's db.New (internal/infra/db/upgrade_test.go). A test that only
// simulates the old shape — adding retired columns back to the current schema —
// proves the migration's SQL, not that the candidate opens a database the
// predecessor actually left behind: its column types, defaults, indexes, and
// rows written through its own code.
//
// The directory holds exactly one dump, named for the release a deployment
// upgrades from. Refreshing it for a new source replaces the old one; the
// release process in docs/contribute/releasing.md says when.
const upgradeFixtureDir = "internal/infra/db/testdata/schema"

// upgradeFixtureMySQLImage is the MySQL the deployment manifests run
// (deployment/kind/mysql.yaml), so the dump carries the DDL that server emits.
const upgradeFixtureMySQLImage = "mysql:8.0.40"

// upgradeFixtureImage is the released server image a tag names. Release tags
// carry a v prefix; image tags do not.
const upgradeFixtureImage = "ghcr.io/icloudbb/buildmax"

var upgradeFixtureTag = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[a-z]+\.[0-9]+)?$`)

// upgradeFixtureOwner is the account the dataset belongs to. The test reads the
// manifest rather than this constant, so it holds no assumptions of its own
// about names.
const upgradeFixtureOwner = "upgrade-owner@buildmax.local"

// upgradeFixtureArtifact is the one artifact's content; its SHA-256 goes into
// the manifest so the test can prove the row survived unchanged.
const upgradeFixtureArtifact = "Upgrade fixture artifact.\n"

// upgradeFixtureManifest is what the fixture's seed created, by public ID. It is
// written into the dump's header, and the test asserts each entity through the
// candidate's store. Adding an entity means adding it here, seeding it, and
// asserting it.
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

// upgradeFixtureManifestPrefix marks the header line the test parses.
const upgradeFixtureManifestPrefix = "-- manifest: "

func cmdReleaseUpgradeFixture(args []string) error {
	if len(args) != 1 {
		return usageErrorf("release", "release upgrade-fixture needs the tag a deployment upgrades from, such as 0.2.0-alpha.14")
	}
	tag := strings.TrimPrefix(args[0], "v")
	if !upgradeFixtureTag.MatchString(tag) {
		return usageErrorf("release", "%q is not a release tag such as 0.2.0-alpha.14", args[0])
	}
	if err := requireCommands("docker"); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	dump, err := snapshotUpgradeFixture(ctx, tag)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(upgradeFixtureDir, 0o755); err != nil {
		return err
	}
	out := filepath.Join(upgradeFixtureDir, tag+".sql")
	previous, err := filepath.Glob(filepath.Join(upgradeFixtureDir, "*.sql"))
	if err != nil {
		return err
	}
	for _, path := range previous {
		if path == out {
			continue
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		fmt.Printf("  removed %s: the fixture names one upgrade source\n", path)
	}
	if err := os.WriteFile(out, dump, 0o644); err != nil {
		return err
	}
	fmt.Printf("Wrote %s (%d bytes). Prove the upgrade with: BUILDMAX_TEST_DSN=... %s test mysql -run TestUpgradeFromPredecessorSchema\n",
		out, len(dump), mk())
	return nil
}

// snapshotUpgradeFixture runs the tag's server against a MySQL of its own, seeds
// the dataset through that server's API, and returns the normalized dump. The
// containers and network are removed whatever happens.
func snapshotUpgradeFixture(ctx context.Context, tag string) ([]byte, error) {
	suffix, err := randomHex(4)
	if err != nil {
		return nil, err
	}
	network := "buildmax-upgrade-" + suffix
	mysql := network + "-mysql"
	server := network + "-server"
	image := upgradeFixtureImage + ":" + tag
	defer func() {
		_ = exec.Command("docker", "rm", "-f", "-v", server, mysql).Run()
		_ = exec.Command("docker", "network", "rm", network).Run()
	}()

	fmt.Printf("Snapshotting %s's schema (network %s)...\n", image, network)
	// Pulled in the open rather than inside `docker run`, so a slow or stuck
	// registry shows its progress instead of looking like a hung command.
	for _, ref := range []string{upgradeFixtureMySQLImage, image} {
		if err := runCmd("docker", "pull", "--quiet", ref); err != nil {
			return nil, err
		}
	}
	if _, err := captureErr("docker", "network", "create", network); err != nil {
		return nil, err
	}
	if _, err := captureErr("docker", "run", "-d", "--name", mysql, "--network", network,
		"-e", "MYSQL_ROOT_PASSWORD=buildmax", "-e", "MYSQL_DATABASE=buildmax", upgradeFixtureMySQLImage); err != nil {
		return nil, err
	}
	if err := waitFor(ctx, 3*time.Minute, "MySQL", func() error {
		_, err := captureErr("docker", "exec", "-e", "MYSQL_PWD=buildmax", mysql,
			"mysql", "-h127.0.0.1", "-uroot", "-e", "SELECT 1", "buildmax")
		return err
	}); err != nil {
		return nil, err
	}

	jwtSecret, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	// The worker is /bin/false: a fired schedule and a workflow run need a Task
	// and a TaskRun row, not a model, and a run that fails at once leaves the
	// dump no trace files or model calls that differ between snapshots.
	serverYAML := fmt.Sprintf(`log_level: info
port: 5678
workspaces_dir: /data/workspaces
database:
  host: %s
  port: 3306
  user: root
  name: buildmax
worker:
  binary: /bin/false
  run_mode: local_process
  server_url: http://127.0.0.1:5679
`, mysql)
	if _, err := captureErr("docker", "create", "--name", server, "--network", network,
		"-p", "127.0.0.1::5678",
		"-e", "BUILDMAX_HOME=/buildmax",
		"-e", "BUILDMAX_JWT_SECRET="+jwtSecret,
		"-e", "BUILDMAX_DATABASE_PASSWORD=buildmax",
		"--entrypoint", "/usr/local/bin/buildmax-server", image); err != nil {
		return nil, err
	}
	// docker cp rather than a bind mount, so the file reaches the container
	// whatever directories Docker Desktop shares with its VM.
	dir, err := os.MkdirTemp("", "buildmax-upgrade-fixture-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if err := os.Mkdir(filepath.Join(dir, "buildmax"), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "buildmax", "server.yaml"), []byte(serverYAML), 0o644); err != nil {
		return nil, err
	}
	if _, err := captureErr("docker", "cp", filepath.Join(dir, "buildmax"), server+":/"); err != nil {
		return nil, err
	}
	if _, err := captureErr("docker", "start", server); err != nil {
		return nil, err
	}
	port, err := captureErr("docker", "port", server, "5678/tcp")
	if err != nil {
		return nil, err
	}
	apiBase := "http://" + strings.TrimSpace(strings.Split(port, "\n")[0])
	client := &http.Client{Timeout: 30 * time.Second}
	if err := waitFor(ctx, 2*time.Minute, "the server's /healthz", func() error {
		if running, _ := capture("docker", "inspect", "-f", "{{.State.Running}}", server); running != "true" {
			return errStopWaiting
		}
		return expectStatusWithToken(ctx, client, apiBase+"/healthz", "", http.StatusOK)
	}); err != nil {
		logs, _ := exec.Command("docker", "logs", "--tail", "40", server).CombinedOutput()
		return nil, fmt.Errorf("%w\nserver log:\n%s", err, logs)
	}

	target := smokeTarget{apiBase: apiBase, admin: func(args ...string) (string, error) {
		out, err := exec.Command("docker", append([]string{"exec", server, "buildmax-server"}, args...)...).CombinedOutput()
		return string(out), err
	}}
	manifest, err := seedUpgradeFixture(ctx, client, target)
	if err != nil {
		return nil, err
	}
	manifest.SourceImage = image

	// Stopped before the dump, so no loop writes while it reads.
	if _, err := captureErr("docker", "stop", server); err != nil {
		return nil, err
	}
	raw, err := captureErr("docker", "exec", "-e", "MYSQL_PWD=buildmax", mysql, "mysqldump", "-uroot",
		"--single-transaction", "--skip-comments", "--skip-add-locks", "--skip-extended-insert",
		"--order-by-primary", "--hex-blob", "--no-tablespaces", "--set-gtid-purged=OFF", "buildmax")
	if err != nil {
		return nil, err
	}
	return normalizeUpgradeFixture(tag, manifest, raw)
}

// autoIncrementCounter is the next-key counter mysqldump writes into CREATE
// TABLE. It is noise to a reviewer and changes with every seed.
var autoIncrementCounter = regexp.MustCompile(` AUTO_INCREMENT=[0-9]+`)

// normalizeUpgradeFixture prefixes the dump with the header the test reads and
// strips what varies between two snapshots of the same data. Rows keep their
// own values — public IDs and timestamps are what the predecessor wrote.
func normalizeUpgradeFixture(tag string, manifest upgradeFixtureManifest, dump string) ([]byte, error) {
	if !strings.Contains(dump, "CREATE TABLE `schema_migration`") {
		return nil, errors.New("the dump has no schema_migration table; the source release predates the migration ledger")
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "-- BuildMax upgrade fixture: what %s left in MySQL after\n", manifest.SourceImage)
	fmt.Fprintf(&b, "-- `./make release upgrade-fixture %s` seeded it through that release's API.\n", tag)
	b.WriteString("-- Generated; regenerate rather than edit. TestUpgradeFromPredecessorSchema\n")
	b.WriteString("-- upgrades it with the candidate and asserts every manifest entity survives.\n")
	b.WriteString(upgradeFixtureManifestPrefix)
	b.Write(encoded)
	b.WriteString("\n")
	for _, line := range strings.Split(strings.TrimRight(dump, "\n"), "\n") {
		b.WriteString(autoIncrementCounter.ReplaceAllString(line, ""))
		b.WriteString("\n")
	}
	return b.Bytes(), nil
}

// seedUpgradeFixture creates the fixed dataset through the source server's own
// API, so every row has the shape that release writes. It sends what both the
// source and later releases accept where the two differ — a schedule's agent_id
// alongside executor_kind/executor_id — so the next refresh does not have to
// start by rewriting it.
func seedUpgradeFixture(ctx context.Context, client *http.Client, target smokeTarget) (upgradeFixtureManifest, error) {
	m := upgradeFixtureManifest{OwnerEmail: upgradeFixtureOwner}
	if output, err := target.admin("user", "create", upgradeFixtureOwner); err != nil {
		return m, fmt.Errorf("create %s: %w: %s", upgradeFixtureOwner, err, output)
	}
	codeOutput, err := target.admin("user", "login-code", upgradeFixtureOwner)
	if err != nil {
		return m, fmt.Errorf("issue a login code: %w: %s", err, codeOutput)
	}
	code := loginCodePattern.FindString(codeOutput)
	if code == "" {
		return m, errors.New("the login-code command returned no bmxlogin_ code")
	}
	var login struct {
		Token string `json:"token"`
		User  struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := requestJSON(ctx, client, http.MethodPost, target.apiBase+"/api/auth/login", "", map[string]string{
		"email": upgradeFixtureOwner, "otp": code, "platform": "upgrade-fixture",
	}, &login, http.StatusOK); err != nil {
		return m, err
	}
	token := login.Token
	m.OwnerID = login.User.ID

	var space struct {
		ID string `json:"id"`
	}
	if err := requestJSON(ctx, client, http.MethodPost, target.apiBase+"/api/spaces", token,
		map[string]string{"name": "Upgrade fixture"}, &space, http.StatusCreated); err != nil {
		return m, err
	}
	m.SpaceID = space.ID
	base := target.apiBase + "/api/spaces/" + url.PathEscape(space.ID)

	var agent struct {
		ID string `json:"id"`
	}
	if err := requestJSON(ctx, client, http.MethodPost, base+"/agents", token, map[string]string{
		"name": "Upgrade fixture agent", "description": "Seeded by the upgrade fixture.", "instructions": "Summarize the issue.",
	}, &agent, http.StatusCreated); err != nil {
		return m, err
	}
	m.AgentID = agent.ID

	// Owner and executor together: the Issue shape issue_owner_executor_split
	// produced, written here by the release that runs it.
	var issue struct {
		ID string `json:"id"`
	}
	if err := requestJSON(ctx, client, http.MethodPost, base+"/issues", token, map[string]string{
		"title": "Upgrade fixture issue", "description": "Owned by a person, executed by an agent.", "status": "in_progress",
		"owner_id": m.OwnerID, "executor_kind": "agent", "executor_id": agent.ID,
	}, &issue, http.StatusCreated); err != nil {
		return m, err
	}
	m.IssueID = issue.ID
	if err := requestJSON(ctx, client, http.MethodPost, base+"/issues/"+url.PathEscape(issue.ID)+"/comments", token,
		map[string]string{"body": "A comment the upgrade must keep."}, nil, http.StatusCreated); err != nil {
		return m, err
	}

	definition, err := json.Marshal(map[string]any{
		"schema_version": 1,
		"nodes": []map[string]any{{
			"id": "summarize", "type": "agent_task", "agent": map[string]string{"id": agent.ID},
			"input": map[string]string{"instruction": "Summarize the issue."},
		}},
	})
	if err != nil {
		return m, err
	}
	var workflow struct {
		ID string `json:"id"`
	}
	if err := requestJSON(ctx, client, http.MethodPost, base+"/workflows", token, map[string]string{
		"name": "Upgrade fixture workflow", "description": "One agent step.", "definition": string(definition),
	}, &workflow, http.StatusCreated); err != nil {
		return m, err
	}
	m.WorkflowID = workflow.ID
	if err := requestJSON(ctx, client, http.MethodPatch, base+"/workflows/"+url.PathEscape(workflow.ID), token,
		map[string]string{"status": "published"}, nil, http.StatusOK); err != nil {
		return m, err
	}
	var run struct {
		Run struct {
			ID string `json:"id"`
		} `json:"run"`
	}
	if err := requestJSON(ctx, client, http.MethodPost, base+"/workflows/"+url.PathEscape(workflow.ID)+"/runs", token,
		map[string]any{}, &run, http.StatusCreated); err != nil {
		return m, err
	}
	m.WorkflowRunID = run.Run.ID

	fired, err := createFixtureSchedule(ctx, client, base, token, agent.ID, "Every minute", "* * * * *")
	if err != nil {
		return m, err
	}
	m.FiredScheduleID = fired
	// The first of January: never due while the snapshot runs.
	idle, err := createFixtureSchedule(ctx, client, base, token, agent.ID, "Yearly", "0 0 1 1 *")
	if err != nil {
		return m, err
	}
	m.IdleScheduleID = idle

	artifactID, err := uploadUpgradeFixtureArtifact(ctx, client, base, token)
	if err != nil {
		return m, err
	}
	m.ArtifactID = artifactID
	sum := sha256.Sum256([]byte(upgradeFixtureArtifact))
	m.ArtifactSHA256 = hex.EncodeToString(sum[:])

	// The dispatcher polls once a minute, so the every-minute schedule fires
	// within two. It is paused as soon as it has, so the dump holds one firing.
	fmt.Println("  waiting for the schedule to fire (up to three minutes)...")
	scheduleURL := base + "/schedules/" + url.PathEscape(fired)
	if err := waitFor(ctx, 3*time.Minute, "the schedule's first firing", func() error {
		var s struct {
			LastTaskID  *string `json:"last_task_id"`
			LastFireRef *string `json:"last_fire_ref"`
		}
		if err := requestJSON(ctx, client, http.MethodGet, scheduleURL, token, nil, &s, http.StatusOK); err != nil {
			return err
		}
		switch {
		case s.LastTaskID != nil:
			m.FiredTaskID = *s.LastTaskID
		case s.LastFireRef != nil:
			m.FiredTaskID = *s.LastFireRef
		default:
			return errors.New("not fired yet")
		}
		return nil
	}); err != nil {
		return m, err
	}
	if err := requestJSON(ctx, client, http.MethodPatch, scheduleURL, token, map[string]bool{"enabled": false}, nil, http.StatusOK); err != nil {
		return m, err
	}
	// Let the fired task's run settle, so the dump holds a finished run rather
	// than whichever state the stop happened to catch.
	taskURL := base + "/tasks/" + url.PathEscape(m.FiredTaskID)
	if err := waitFor(ctx, 2*time.Minute, "the fired task to finish", func() error {
		var t struct {
			Status string `json:"status"`
		}
		if err := requestJSON(ctx, client, http.MethodGet, taskURL, token, nil, &t, http.StatusOK); err != nil {
			return err
		}
		switch strings.ToUpper(t.Status) {
		case "", "QUEUED", "PENDING", "SCHEDULED", "RUNNING":
			return fmt.Errorf("task is %s", t.Status)
		}
		return nil
	}); err != nil {
		return m, err
	}
	fmt.Printf("  seeded space %s: agent, issue, workflow run, two schedules (one fired), artifact\n", m.SpaceID)
	return m, nil
}

func createFixtureSchedule(ctx context.Context, client *http.Client, base, token, agentID, name, cron string) (string, error) {
	var s struct {
		ID string `json:"id"`
	}
	err := requestJSON(ctx, client, http.MethodPost, base+"/schedules", token, map[string]string{
		"agent_id": agentID, "executor_kind": "agent", "executor_id": agentID,
		"name": name, "input": "Summarize new issues.", "cron_expr": cron, "timezone": "UTC",
	}, &s, http.StatusCreated)
	return s.ID, err
}

func uploadUpgradeFixtureArtifact(ctx context.Context, client *http.Client, base, token string) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "upgrade-fixture.txt")
	if err != nil {
		return "", err
	}
	if _, err := io.WriteString(part, upgradeFixtureArtifact); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	response, err := request(ctx, client, http.MethodPost, base+"/artifacts", token, writer.FormDataContentType(), &body, http.StatusCreated)
	if err != nil {
		return "", err
	}
	defer response.Close()
	var artifact struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(response).Decode(&artifact); err != nil {
		return "", err
	}
	return artifact.ID, nil
}

// errStopWaiting is a check's way to say that retrying cannot help.
var errStopWaiting = errors.New("the container exited")

// waitFor retries check until it succeeds or budget runs out, reporting the
// last failure so a timeout says what it was waiting on.
func waitFor(ctx context.Context, budget time.Duration, what string, check func() error) error {
	deadline := time.Now().Add(budget)
	for {
		err := check()
		if err == nil {
			return nil
		}
		if errors.Is(err, errStopWaiting) {
			return fmt.Errorf("waiting for %s: %w", what, err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("gave up waiting for %s after %s: %w", what, budget, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
