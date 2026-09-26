package main

import (
	"archive/tar"
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
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/icloudbb/buildmax/internal/config"
)

// The paired backup-and-restore drill (docs/deploy/backup-restore.md). It seeds
// data through the API, records what an operator would check afterwards,
// quiesces the server, backs up the database and then the bucket, deletes the
// db, storage, and buildmax namespaces, restores both stores before any server
// starts, and proves the pair agrees: the storage reference check, the same
// fingerprint, the same row counts, the same objects, and restored data that
// still works. It measures the recovery time and names any loss.
//
// It does not call kindUp to rebuild: kindUp ends in the smoke, which would
// write into the restored database before it is compared.

const (
	restoreDrillEmail = "restore-drill@buildmax.local"
	// restoreDrillSchema and restoreDrillBucket are database.name and
	// storage.minio.bucket in deployment/smoke/server.kind.yaml.
	restoreDrillSchema = "buildmax"
	restoreDrillBucket = "bmstore"
	restoreDrillMCPod  = "restore-drill-mc"
	restoreDrillFile   = "restore-drill.txt"
	// restoreDrillSecretCommand prints a digest of the Secret the drill's Agent
	// consumes. The digest, not the value, is what reaches the model: the value
	// itself is redacted from tool results.
	restoreDrillSecretCommand = `printf %s "$DRILL_TOKEN" | sha256sum`
)

// restoreDrillSeed is what the drill created and later compares. Tokens stay in
// memory: they are credentials, and the record is written to disk as evidence.
type restoreDrillSeed struct {
	SpaceID            string `json:"space_id"`
	ConversationID     string `json:"conversation_id"`
	TaskID             string `json:"task_id"`
	FirstRunID         string `json:"first_run_id"`
	ContinuedRunID     string `json:"continued_run_id"`
	RunArtifactID      string `json:"run_artifact_id"`
	UploadedArtifactID string `json:"uploaded_artifact_id"`
	SecretID           string `json:"secret_id"`
	SecretSHA256       string `json:"secret_sha256"`
	AgentID            string `json:"agent_id"`
	SecretTaskID       string `json:"secret_task_id"`
	// Marker is carried only by the first run's input, so it reaches a later
	// run's model only through the restored session history.
	Marker string `json:"marker"`

	token      string
	shareToken string
}

// restoreDrillBackup is the backup's own record, written beside it.
type restoreDrillBackup struct {
	RecoveryPoint  time.Time         `json:"recovery_point"`
	DatabaseFile   string            `json:"database_file"`
	DatabaseSHA256 string            `json:"database_sha256"`
	DatabaseBytes  int64             `json:"database_bytes"`
	BucketFile     string            `json:"bucket_file"`
	BucketSHA256   string            `json:"bucket_sha256"`
	BucketBytes    int64             `json:"bucket_bytes"`
	KEKSHA256      string            `json:"kek_sha256"`
	ServerYAMLHash string            `json:"server_yaml_sha256"`
	RowCounts      map[string]string `json:"row_counts"`
	Objects        map[string]string `json:"objects"`
}

type drillStage struct {
	Name    string  `json:"name"`
	Seconds float64 `json:"seconds"`
}

type restoreDrill struct {
	target smokeTarget
	client *http.Client
	dir    string
	stages []drillStage
}

func (d *restoreDrill) step(name string, fn func() error) error {
	fmt.Printf("[drill] %s...\n", name)
	start := time.Now()
	err := fn()
	elapsed := time.Since(start)
	d.stages = append(d.stages, drillStage{Name: name, Seconds: elapsed.Round(100 * time.Millisecond).Seconds()})
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	fmt.Printf("[drill] %s: done in %s\n", name, elapsed.Round(100*time.Millisecond))
	return nil
}

func (d *restoreDrill) path(name string) string { return filepath.Join(d.dir, name) }

func (d *restoreDrill) writeJSON(name string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(d.path(name), append(data, '\n'), 0o600)
}

func kindRestoreDrill() error {
	if err := requireEphemeralKindCluster(); err != nil {
		return err
	}
	if err := requireCommands("kubectl"); err != nil {
		return err
	}
	cluster := kindClusterName()
	if ok, err := kindClusterExists(cluster); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("kind cluster %q does not exist; run BUILDMAX_KIND_EPHEMERAL=1 %s kind up", cluster, mk())
	}
	// The backup holds the KEK, so the directory is owner-only like .local/.
	dir := filepath.Join(localDir, "drill", "restore-"+time.Now().UTC().Format("20060102T150405Z"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	fmt.Printf("[drill] paired restore on cluster %s; backup and evidence in %s\n", cluster, dir)
	d := &restoreDrill{target: kindSmokeTarget(), client: &http.Client{Timeout: 60 * time.Second}, dir: dir}
	ctx := context.Background()

	var seed restoreDrillSeed
	var before, after map[string]string
	var backup restoreDrillBackup
	var wipedAt time.Time
	var rto time.Duration
	var postToken string

	steps := []struct {
		name string
		fn   func() error
	}{
		{"seed through the API", func() (err error) { seed, err = d.seed(ctx); return err }},
		// A finding before the backup would otherwise read as restore loss.
		{"check stored references before the backup", func() error { return d.storageVerify("storage-verify-before.txt") }},
		{"fingerprint through the API", func() (err error) {
			before, err = d.fingerprint(ctx, seed, seed.token)
			if err == nil {
				err = d.writeJSON("fingerprint-before.json", before)
			}
			return err
		}},
		{"quiesce the server", d.quiesce},
		{"back up the database", func() error { return d.backupDatabase(&backup) }},
		{"back up the bucket", func() error { return d.backupBucket(&backup) }},
		{"back up the KEK and server.yaml", func() error { return d.backupKeyAndConfig(&backup) }},
		{"wipe the db, storage, and buildmax namespaces", func() error {
			if err := d.writeJSON("backup-manifest.json", backup); err != nil {
				return err
			}
			if err := d.wipe(); err != nil {
				return err
			}
			wipedAt = time.Now()
			return nil
		}},
		{"restore the database and bucket services", d.restoreServices},
		{"restore the database", func() error { return d.restoreDatabase(backup) }},
		{"restore the bucket", func() error { return d.restoreBucket(backup) }},
		{"start the server on the restored pair", func() error { return d.restoreServer(backup) }},
		{"storage verify --checksums", func() error { return d.storageVerify("storage-verify-after.txt") }},
		{"sign in and download the first artifact", func() (err error) {
			postToken, err = d.firstArtifact(ctx, seed, before)
			rto = time.Since(wipedAt)
			return err
		}},
		{"re-fingerprint and compare", func() (err error) {
			if after, err = d.fingerprint(ctx, seed, postToken); err != nil {
				return err
			}
			if err := d.writeJSON("fingerprint-after.json", after); err != nil {
				return err
			}
			if diff := diffFingerprints(before, after); diff.lost() {
				return fmt.Errorf("the restore lost data the API showed before the backup:%s", diff)
			}
			fmt.Printf("  %d entries, none missing or changed\n", len(before))
			return nil
		}},
		{"read the restored bucket back", func() error { return d.compareBucket(backup) }},
		{"exercise the restored data", func() error { return d.exerciseRestored(ctx, seed, postToken) }},
	}
	for _, s := range steps {
		if err := d.step(s.name, s.fn); err != nil {
			_ = d.writeJSON("stages.json", d.stages)
			return fmt.Errorf("restore drill failed; evidence so far is in %s: %w", dir, err)
		}
	}

	diff := diffFingerprints(before, after)
	report := struct {
		Cluster       string            `json:"cluster"`
		RecoveryPoint time.Time         `json:"recovery_point"`
		RTOSeconds    float64           `json:"rto_seconds"`
		Stages        []drillStage      `json:"stages"`
		Fingerprint   int               `json:"fingerprint_entries"`
		Diff          fingerprintDiff   `json:"fingerprint_diff"`
		Seed          restoreDrillSeed  `json:"seed"`
		Backup        map[string]string `json:"backup"`
	}{
		Cluster: cluster, RecoveryPoint: backup.RecoveryPoint, RTOSeconds: rto.Round(100 * time.Millisecond).Seconds(),
		Stages: d.stages, Fingerprint: len(before), Diff: diff, Seed: seed,
		Backup: map[string]string{
			"database": fmt.Sprintf("%s (%d bytes, sha256 %s)", backup.DatabaseFile, backup.DatabaseBytes, backup.DatabaseSHA256),
			"bucket":   fmt.Sprintf("%s (%d objects, %d bytes, sha256 %s)", backup.BucketFile, len(backup.Objects), backup.BucketBytes, backup.BucketSHA256),
			"tables":   fmt.Sprintf("%d", len(backup.RowCounts)),
		},
	}
	if err := d.writeJSON("report.json", report); err != nil {
		return err
	}
	fmt.Println()
	fmt.Println("Paired restore drill passed.")
	fmt.Printf("  Recovery point: %s (the database snapshot; the bucket copy followed it)\n", backup.RecoveryPoint.Format(time.RFC3339))
	fmt.Printf("  Backup: %s\n", report.Backup["database"])
	fmt.Printf("          %s\n", report.Backup["bucket"])
	fmt.Printf("  RTO: %s from the wiped namespaces to the first checksum-matching artifact download\n", rto.Round(100*time.Millisecond))
	for _, s := range d.stages {
		fmt.Printf("    %-50s %6.1fs\n", s.Name, s.Seconds)
	}
	fmt.Printf("  Compared: %d fingerprint entries, %d table row counts, %d bucket objects\n", len(before), len(backup.RowCounts), len(backup.Objects))
	fmt.Printf("  Loss: none — no fingerprint entry, row count, or backed-up object is missing or changed\n")
	for _, a := range diff.Added {
		fmt.Printf("  New since the backup (not loss): %s\n", a)
	}
	fmt.Printf("  Evidence: %s\n", dir)
	return nil
}

// ---- seed ------------------------------------------------------------------

func (d *restoreDrill) spaceBase(spaceID string) string {
	return d.target.apiBase + "/api/spaces/" + url.PathEscape(spaceID)
}

func (d *restoreDrill) seed(ctx context.Context) (restoreDrillSeed, error) {
	var s restoreDrillSeed
	token, spaceID, err := smokeSignIn(ctx, d.client, d.target, restoreDrillEmail)
	if err != nil {
		return s, err
	}
	s.token, s.SpaceID = token, spaceID
	if s.Marker, err = randomHex(8); err != nil {
		return s, err
	}
	base := d.spaceBase(spaceID)

	// A Space file lives only in the bucket, and the continued run publishes it.
	if err := postMultipart(ctx, d.client, base+"/upload", token, "files", restoreDrillFile, "restore drill space file "+s.Marker+"\n", http.StatusOK, nil); err != nil {
		return s, fmt.Errorf("upload the Space file: %w", err)
	}
	var uploaded struct {
		ID string `json:"id"`
	}
	if err := postMultipart(ctx, d.client, base+"/artifacts", token, "file", "restore-drill-upload.txt", "restore drill uploaded artifact "+s.Marker+"\n", http.StatusCreated, &uploaded); err != nil {
		return s, fmt.Errorf("upload an artifact: %w", err)
	}
	s.UploadedArtifactID = uploaded.ID
	var share struct {
		Token string `json:"token"`
	}
	if err := requestJSON(ctx, d.client, http.MethodPost, d.target.apiBase+"/api/artifacts/"+url.PathEscape(uploaded.ID)+"/shares", token, nil, &share, http.StatusCreated); err != nil {
		return s, fmt.Errorf("share the artifact: %w", err)
	}
	s.shareToken = share.Token

	// A Task whose first run leaves a trace, a workspace checkpoint, and a
	// session bundle, continued by a run that restores them and publishes an
	// artifact.
	if s.ConversationID, err = coordCreateConversation(ctx, d.client, d.target.apiBase, spaceID, token); err != nil {
		return s, err
	}
	if s.TaskID, err = coordCreateTask(ctx, d.client, d.target.apiBase, spaceID, s.ConversationID, token, "Reply with exactly deployment smoke ok. restore-drill history "+s.Marker); err != nil {
		return s, err
	}
	taskURL := base + "/tasks/" + url.PathEscape(s.TaskID)
	if _, err := waitForTaskSuccess(ctx, d.client, taskURL, token); err != nil {
		return s, err
	}
	if s.FirstRunID, err = lastRunID(ctx, d.client, taskURL, token); err != nil {
		return s, err
	}
	if s.ContinuedRunID, err = d.continueTask(ctx, taskURL, token, "Publish the drill file, then reply.", true); err != nil {
		return s, err
	}
	var prov struct {
		Artifacts []struct {
			ID string `json:"id"`
		} `json:"artifacts"`
	}
	if err := requestJSON(ctx, d.client, http.MethodGet, base+"/task-runs/"+url.PathEscape(s.ContinuedRunID), token, nil, &prov, http.StatusOK); err != nil {
		return s, err
	}
	if len(prov.Artifacts) == 0 {
		return s, fmt.Errorf("the continued run %s published no artifact", s.ContinuedRunID)
	}
	s.RunArtifactID = prov.Artifacts[0].ID

	// A Space Secret sealed under the KEK, consumed by an Agent whose run proves
	// the value decrypts. The same run after the restore is what proves the
	// restored KEK is the one the database was sealed under.
	secretValue, err := randomHex(16)
	if err != nil {
		return s, err
	}
	sum := sha256.Sum256([]byte(secretValue))
	s.SecretSHA256 = hex.EncodeToString(sum[:])
	var secret struct {
		ID string `json:"id"`
	}
	if err := requestJSON(ctx, d.client, http.MethodPost, base+"/secrets", token, map[string]any{
		"name": "restore-drill-" + s.Marker, "description": "Synthetic value for the restore drill.",
		"items": map[string]string{"TOKEN": secretValue},
	}, &secret, http.StatusCreated); err != nil {
		return s, fmt.Errorf("create the Secret: %w", err)
	}
	s.SecretID = secret.ID
	var agent struct {
		ID string `json:"id"`
	}
	if err := requestJSON(ctx, d.client, http.MethodPost, base+"/agents", token, map[string]any{
		"name": "restore-drill-" + s.Marker, "description": "Consumes the restore drill Secret.",
		"instructions":       "Run the command you are given.",
		"secret_consumption": map[string]any{"env": []map[string]string{{"secret": secret.ID, "item": "TOKEN", "env_name": "DRILL_TOKEN"}}},
	}, &agent, http.StatusCreated); err != nil {
		return s, fmt.Errorf("create the Agent: %w", err)
	}
	s.AgentID = agent.ID
	if s.SecretTaskID, err = d.secretRun(ctx, base, token, s.AgentID, s.SecretSHA256); err != nil {
		return s, err
	}

	// A task reads SUCCEEDED before its trace is queryable.
	for _, runID := range []string{s.FirstRunID, s.ContinuedRunID} {
		traceURL := base + "/task-runs/" + url.PathEscape(runID) + "/trace"
		if err := retryFor(ctx, time.Minute, func() error {
			text, err := requestText(ctx, d.client, http.MethodGet, traceURL, token, nil, http.StatusOK)
			if err == nil && strings.TrimSpace(text) == "" {
				err = fmt.Errorf("run %s has an empty trace", runID)
			}
			return err
		}); err != nil {
			return s, err
		}
	}
	if err := d.writeJSON("seed.json", s); err != nil {
		return s, err
	}
	fmt.Printf("  seeded space %s: task %s (runs %s, %s), artifacts %s and %s, Secret %s, Agent task %s\n",
		s.SpaceID, s.TaskID, s.FirstRunID, s.ContinuedRunID, s.RunArtifactID, s.UploadedArtifactID, s.SecretID, s.SecretTaskID)
	return s, nil
}

// continueTask starts the Task's next run and waits for it to succeed with its
// workspace restored from the previous run's checkpoint. publish arms the mock
// to publish the drill file as the run's first action.
func (d *restoreDrill) continueTask(ctx context.Context, taskURL, token, input string, publish bool) (string, error) {
	if publish {
		armed := map[string]any{
			"name": "UploadArtifact", "args": map[string]any{"path": restoreDrillFile, "title": "restore drill"},
			"times": 1,
		}
		if err := requestJSON(ctx, d.client, http.MethodPost, d.target.llmControlToolCallURL, "", armed, nil, http.StatusOK); err != nil {
			return "", fmt.Errorf("arm the artifact publish: %w", err)
		}
		defer func() {
			_ = requestJSON(ctx, d.client, http.MethodPost, d.target.llmControlToolCallURL, "", map[string]any{"clear": true}, nil, http.StatusOK)
		}()
	}
	var run struct {
		ID string `json:"id"`
	}
	if err := requestJSON(ctx, d.client, http.MethodPost, taskURL+"/runs", token, map[string]string{"input": input}, &run, http.StatusCreated); err != nil {
		return "", fmt.Errorf("continue the task: %w", err)
	}
	type runState struct {
		ID                    string  `json:"id"`
		Status                string  `json:"status"`
		ErrorMessage          *string `json:"error_message"`
		RestoreStatus         string  `json:"workspace_restore_status"`
		CheckpointStatus      string  `json:"workspace_checkpoint_status"`
		WorkspaceRestoreError *string `json:"workspace_restore_error"`
	}
	var got runState
	err := retryFor(ctx, 3*time.Minute, func() error {
		var runs struct {
			Runs []runState `json:"runs"`
		}
		if err := requestJSON(ctx, d.client, http.MethodGet, taskURL+"/runs", token, nil, &runs, http.StatusOK); err != nil {
			return err
		}
		for _, r := range runs.Runs {
			if r.ID == run.ID {
				got = r
				// A run reads SUCCEEDED a moment before its result checkpoint
				// is committed.
				if r.Status == "SUCCEEDED" && r.CheckpointStatus != "committed" {
					return fmt.Errorf("run %s result checkpoint is %q", r.ID, r.CheckpointStatus)
				}
				if isTerminalSmokeStatus(r.Status) {
					return nil
				}
				return fmt.Errorf("run %s is still %s", r.ID, r.Status)
			}
		}
		return fmt.Errorf("run %s is not listed", run.ID)
	})
	if err != nil {
		return "", err
	}
	switch {
	case got.Status != "SUCCEEDED":
		return "", fmt.Errorf("continued run %s settled %s: %s", got.ID, got.Status, stringValue(got.ErrorMessage))
	case got.RestoreStatus != "restored":
		return "", fmt.Errorf("continued run %s workspace restore is %q (%s), want restored", got.ID, got.RestoreStatus, stringValue(got.WorkspaceRestoreError))
	case got.CheckpointStatus != "committed":
		return "", fmt.Errorf("continued run %s result checkpoint is %q, want committed", got.ID, got.CheckpointStatus)
	}
	return got.ID, nil
}

// secretRun runs the drill Agent with the mock armed to hash the consumed
// Secret, and requires the digest the run reported to be the seeded value's.
func (d *restoreDrill) secretRun(ctx context.Context, base, token, agentID, wantSHA string) (string, error) {
	var baseline []struct{ Body []byte }
	if err := requestJSON(ctx, d.client, http.MethodGet, d.target.llmControlRequestsURL, "", nil, &baseline, http.StatusOK); err != nil {
		return "", err
	}
	// Armed for more than one call, as the sandbox probe is: a call ahead of the
	// run's own turn may take the first one.
	armed := map[string]any{"name": "Bash", "args": map[string]any{"command": restoreDrillSecretCommand}, "times": smokeSandboxProbeArmCount}
	if err := requestJSON(ctx, d.client, http.MethodPost, d.target.llmControlToolCallURL, "", armed, nil, http.StatusOK); err != nil {
		return "", fmt.Errorf("arm the Secret probe: %w", err)
	}
	defer func() {
		_ = requestJSON(ctx, d.client, http.MethodPost, d.target.llmControlToolCallURL, "", map[string]any{"clear": true}, nil, http.StatusOK)
	}()
	var task struct {
		ID string `json:"id"`
	}
	if err := requestJSON(ctx, d.client, http.MethodPost, base+"/agents/"+url.PathEscape(agentID)+"/tasks", token, map[string]string{"input": "Hash the drill Secret."}, &task, http.StatusCreated); err != nil {
		return "", fmt.Errorf("run the Agent: %w", err)
	}
	if _, err := waitForTaskSuccess(ctx, d.client, base+"/tasks/"+url.PathEscape(task.ID), token); err != nil {
		return "", fmt.Errorf("the Secret-consuming run: %w", err)
	}
	var requests []struct{ Body []byte }
	if err := requestJSON(ctx, d.client, http.MethodGet, d.target.llmControlRequestsURL, "", nil, &requests, http.StatusOK); err != nil {
		return "", err
	}
	if len(requests) < len(baseline) {
		baseline = nil // the mock restarted; every request is new
	}
	results := toolResults(requests[len(baseline):])
	for _, r := range results {
		if strings.Contains(r, wantSHA) {
			return task.ID, nil
		}
	}
	return "", fmt.Errorf("no tool result carried the seeded Secret's digest %s; the run did not receive the value it was sealed with (tool results: %q)", wantSHA, results)
}

// toolResults returns the tool-role message contents in recorded mock requests.
func toolResults(requests []struct{ Body []byte }) []string {
	var out []string
	for _, req := range requests {
		var parsed struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if json.Unmarshal(req.Body, &parsed) != nil {
			continue
		}
		for _, m := range parsed.Messages {
			if m.Role == "tool" {
				out = append(out, m.Content)
			}
		}
	}
	return out
}

func postMultipart(ctx context.Context, client *http.Client, endpoint, token, field, name, content string, want int, out any) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(field, name)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(part, content); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	response, err := request(ctx, client, http.MethodPost, endpoint, token, writer.FormDataContentType(), &body, want)
	if err != nil {
		return err
	}
	defer response.Close()
	if out == nil {
		return nil
	}
	return json.NewDecoder(response).Decode(out)
}

// ---- fingerprint -----------------------------------------------------------

// fingerprint records, through the API an operator uses, what must survive the
// restore: identifiers, statuses, and content digests, as flat key/value pairs.
func (d *restoreDrill) fingerprint(ctx context.Context, seed restoreDrillSeed, token string) (map[string]string, error) {
	fp := map[string]string{}
	api := d.target.apiBase
	base := d.spaceBase(seed.SpaceID)
	get := func(endpoint string, out any) error {
		return requestJSON(ctx, d.client, http.MethodGet, endpoint, token, nil, out, http.StatusOK)
	}
	digest := func(endpoint, auth string) string {
		text, err := requestText(ctx, d.client, http.MethodGet, endpoint, auth, nil, http.StatusOK)
		if err != nil {
			return "unreadable"
		}
		return sha256Hex(text)
	}

	var spaces []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := get(api+"/api/spaces", &spaces); err != nil {
		return nil, err
	}
	for _, s := range spaces {
		fp["space/"+s.ID] = s.Name
	}

	var convTasks []struct {
		ID string `json:"id"`
	}
	if err := get(base+"/conversations/"+url.PathEscape(seed.ConversationID)+"/tasks", &convTasks); err != nil {
		return nil, err
	}
	taskIDs := []string{seed.SecretTaskID}
	for _, t := range convTasks {
		taskIDs = append(taskIDs, t.ID)
	}
	sort.Strings(taskIDs)
	for _, id := range taskIDs {
		var t struct {
			Status    string  `json:"status"`
			Output    *string `json:"output"`
			LastRunID *string `json:"last_run_id"`
		}
		taskURL := base + "/tasks/" + url.PathEscape(id)
		if err := get(taskURL, &t); err != nil {
			return nil, err
		}
		p := "task/" + id + "/"
		fp[p+"status"] = t.Status
		fp[p+"output_sha256"] = sha256Hex(stringValue(t.Output))
		fp[p+"last_run_id"] = stringValue(t.LastRunID)

		var runs struct {
			Runs []struct {
				ID               string  `json:"id"`
				Status           string  `json:"status"`
				Output           *string `json:"output"`
				TracePath        *string `json:"trace_path"`
				PromptTokens     *int    `json:"prompt_tokens"`
				CompletionTokens *int    `json:"completion_tokens"`
				Restore          string  `json:"workspace_restore_status"`
				Checkpoint       string  `json:"workspace_checkpoint_status"`
			} `json:"runs"`
		}
		if err := get(taskURL+"/runs", &runs); err != nil {
			return nil, err
		}
		for _, r := range runs.Runs {
			p := "run/" + r.ID + "/"
			fp[p+"task"] = id
			fp[p+"status"] = r.Status
			fp[p+"output_sha256"] = sha256Hex(stringValue(r.Output))
			fp[p+"trace_path"] = stringValue(r.TracePath)
			fp[p+"tokens"] = fmt.Sprintf("%d/%d", intValue(r.PromptTokens), intValue(r.CompletionTokens))
			fp[p+"workspace_status"] = r.Restore + "/" + r.Checkpoint
			runURL := base + "/task-runs/" + url.PathEscape(r.ID)
			if r.TracePath != nil {
				fp[p+"trace_sha256"] = digest(runURL+"/trace", token)
			}
			var calls []struct {
				ID string `json:"id"`
			}
			if err := get(runURL+"/llm-calls", &calls); err != nil {
				return nil, err
			}
			fp[p+"llm_calls"] = joinSorted(calls, func(c struct {
				ID string `json:"id"`
			}) string {
				return c.ID
			})
			var prov struct {
				Artifacts []struct {
					ID string `json:"id"`
				} `json:"artifacts"`
			}
			if err := get(runURL, &prov); err != nil {
				return nil, err
			}
			fp[p+"artifacts"] = joinSorted(prov.Artifacts, func(a struct {
				ID string `json:"id"`
			}) string {
				return a.ID
			})
		}
	}

	type artifact struct {
		ID        string `json:"id"`
		Filename  string `json:"filename"`
		SizeBytes int64  `json:"size_bytes"`
		SHA256    string `json:"sha256"`
	}
	artifacts, err := fixturePage[artifact](ctx, d.client, base+"/artifacts", token, "items")
	if err != nil {
		return nil, err
	}
	for _, a := range artifacts {
		p := "artifact/" + a.ID + "/"
		fp[p+"filename"] = a.Filename
		fp[p+"size"] = fmt.Sprintf("%d", a.SizeBytes)
		fp[p+"sha256"] = a.SHA256
		fp[p+"download_sha256"] = digest(api+"/api/artifacts/"+url.PathEscape(a.ID)+"/content", token)
		var shares struct {
			Items []struct {
				ShareID string `json:"share_id"`
			} `json:"items"`
		}
		if err := get(api+"/api/artifacts/"+url.PathEscape(a.ID)+"/shares", &shares); err != nil {
			return nil, err
		}
		fp[p+"shares"] = joinSorted(shares.Items, func(s struct {
			ShareID string `json:"share_id"`
		}) string {
			return s.ShareID
		})
	}
	if seed.shareToken != "" {
		fp["share/"+seed.UploadedArtifactID+"/public_sha256"] = digest(api+"/shared/artifacts/"+url.PathEscape(seed.shareToken)+"/raw", "")
	}

	events, err := fixturePage[struct {
		ID     string `json:"id"`
		Action string `json:"action"`
	}](ctx, d.client, base+"/audit-events", token, "events")
	if err != nil {
		return nil, err
	}
	for _, e := range events {
		fp["audit/"+e.ID] = e.Action
	}

	var usage struct {
		RunCount     int   `json:"run_count"`
		TotalTokens  int   `json:"total_tokens"`
		StorageBytes int64 `json:"storage_bytes"`
	}
	if err := get(base+"/usage", &usage); err != nil {
		return nil, err
	}
	fp["usage/run_count"] = fmt.Sprintf("%d", usage.RunCount)
	fp["usage/total_tokens"] = fmt.Sprintf("%d", usage.TotalTokens)
	fp["usage/storage_bytes"] = fmt.Sprintf("%d", usage.StorageBytes)

	var tree fileTreeNode
	if err := get(base+"/files", &tree); err != nil {
		return nil, err
	}
	for _, path := range tree.files() {
		fp["file/"+path+"/sha256"] = digest(base+"/files/"+escapeFilePath(path), token)
	}

	var secrets struct {
		Secrets []struct {
			ID        string   `json:"id"`
			Name      string   `json:"name"`
			State     string   `json:"state"`
			ItemNames []string `json:"item_names"`
		} `json:"secrets"`
	}
	if err := get(base+"/secrets", &secrets); err != nil {
		return nil, err
	}
	for _, s := range secrets.Secrets {
		fp["secret/"+s.ID] = s.Name + " " + s.State + " " + strings.Join(s.ItemNames, ",")
	}
	var agents []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := get(base+"/agents", &agents); err != nil {
		return nil, err
	}
	for _, a := range agents {
		fp["agent/"+a.ID] = a.Name
	}
	return fp, nil
}

// fileTreeNode is the Space file listing's shape.
type fileTreeNode struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Children []fileTreeNode `json:"children"`
}

func (n fileTreeNode) files() []string {
	if n.Type == "file" {
		return []string{n.ID}
	}
	var out []string
	for _, c := range n.Children {
		out = append(out, c.files()...)
	}
	return out
}

func escapeFilePath(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

func joinSorted[T any](items []T, key func(T) string) string {
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, key(item))
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func intValue(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// fingerprintDiff compares two flat records. Missing and Changed are loss;
// Added is what the restored deployment wrote since, reported but not loss.
type fingerprintDiff struct {
	Missing []string `json:"missing,omitempty"`
	Changed []string `json:"changed,omitempty"`
	Added   []string `json:"added,omitempty"`
}

func (d fingerprintDiff) lost() bool { return len(d.Missing) > 0 || len(d.Changed) > 0 }

func (d fingerprintDiff) String() string {
	var b strings.Builder
	for _, k := range d.Missing {
		fmt.Fprintf(&b, "\n  missing: %s", k)
	}
	for _, k := range d.Changed {
		fmt.Fprintf(&b, "\n  changed: %s", k)
	}
	return b.String()
}

func diffFingerprints(before, after map[string]string) fingerprintDiff {
	var d fingerprintDiff
	for k, v := range before {
		got, ok := after[k]
		switch {
		case !ok:
			d.Missing = append(d.Missing, k+" = "+v)
		case got != v:
			d.Changed = append(d.Changed, fmt.Sprintf("%s: %s -> %s", k, v, got))
		}
	}
	for k, v := range after {
		if _, ok := before[k]; !ok {
			d.Added = append(d.Added, k+" = "+v)
		}
	}
	sort.Strings(d.Missing)
	sort.Strings(d.Changed)
	sort.Strings(d.Added)
	return d
}

// ---- quiesce and backup ----------------------------------------------------

// quiesce stops every server replica, so nothing admits, dispatches, reaps, or
// fires a schedule while the backup is taken, then confirms no worker is left
// running a run.
func (d *restoreDrill) quiesce() error {
	if err := kindKubectl("scale", "deployment/buildmax-server", "-n", "buildmax", "--replicas=0"); err != nil {
		return err
	}
	if err := kindKubectl("wait", "--for=delete", "pod", "-l", "app=buildmax-server", "-n", "buildmax", "--timeout=120s"); err != nil {
		return err
	}
	return retryFor(context.Background(), 2*time.Minute, func() error {
		out, err := captureKindKubectl("get", "pods", "-n", "buildmax", "-l", "app.kubernetes.io/name=buildmax-worker",
			"--field-selector=status.phase=Running", "-o", "name")
		if err != nil {
			return err
		}
		if strings.TrimSpace(out) != "" {
			return fmt.Errorf("worker pods still running: %s", strings.Join(strings.Fields(out), ", "))
		}
		return nil
	})
}

func (d *restoreDrill) backupDatabase(b *restoreDrillBackup) error {
	counts, err := kindRowCounts()
	if err != nil {
		return err
	}
	b.RowCounts = counts
	// The recovery point is the snapshot: --single-transaction reads every table
	// as of the moment the dump starts.
	b.RecoveryPoint = time.Now().UTC()
	b.DatabaseFile = "database.sql"
	b.DatabaseBytes, b.DatabaseSHA256, err = kindExecToFile(d.path(b.DatabaseFile), "exec", "-n", "db", "deploy/mysql", "--",
		"env", "MYSQL_PWD="+kindMySQLRootPassword, "mysqldump", "-uroot",
		"--single-transaction", "--routines", "--triggers", "--events", "--set-gtid-purged=OFF",
		"--databases", restoreDrillSchema)
	if err != nil {
		return fmt.Errorf("mysqldump: %w", err)
	}
	fmt.Printf("  %d tables, %d bytes, snapshot at %s\n", len(counts), b.DatabaseBytes, b.RecoveryPoint.Format(time.RFC3339))
	return nil
}

func (d *restoreDrill) backupBucket(b *restoreDrillBackup) error {
	if err := startDrillMCPod(); err != nil {
		return err
	}
	b.BucketFile = "bucket.tar"
	var err error
	b.BucketBytes, b.BucketSHA256, b.Objects, err = mirrorBucketOut(d.path(b.BucketFile))
	if err != nil {
		return err
	}
	fmt.Printf("  %d objects, %d bytes\n", len(b.Objects), b.BucketBytes)
	return nil
}

// backupKeyAndConfig keeps the KEK and server.yaml beside the backup. A real
// deployment stores the KEK separately from the database dump, since either
// alone is harmless and together they unseal every Secret.
func (d *restoreDrill) backupKeyAndConfig(b *restoreDrillBackup) error {
	kek, err := captureKindKubectl("get", "secret", "buildmax-kek", "-n", "buildmax", "-o", "go-template={{index .data \"kek.json\" | base64decode}}")
	if err != nil {
		return fmt.Errorf("read the KEK: %w", err)
	}
	if err := os.WriteFile(d.path("kek.json"), []byte(kek), 0o600); err != nil {
		return err
	}
	b.KEKSHA256 = sha256Hex(kek)
	serverYAML, err := captureKindKubectl("get", "configmap", "buildmax-config", "-n", "buildmax", "-o", "jsonpath={.data.server\\.yaml}")
	if err != nil {
		return fmt.Errorf("read server.yaml: %w", err)
	}
	if err := os.WriteFile(d.path("server.yaml"), []byte(serverYAML+"\n"), 0o600); err != nil {
		return err
	}
	b.ServerYAMLHash = sha256Hex(serverYAML + "\n")
	return nil
}

// kindRowCounts counts every table's rows exactly. information_schema's
// table_rows is an InnoDB estimate and cannot prove a restore.
func kindRowCounts() (map[string]string, error) {
	tables, err := kindMySQL(fmt.Sprintf("SELECT table_name FROM information_schema.tables WHERE table_schema = '%s' AND table_type = 'BASE TABLE' ORDER BY table_name", restoreDrillSchema))
	if err != nil {
		return nil, err
	}
	var selects []string
	for _, t := range strings.Fields(tables) {
		selects = append(selects, fmt.Sprintf("SELECT '%s', COUNT(*) FROM `%s`.`%s`", t, restoreDrillSchema, t))
	}
	if len(selects) == 0 {
		return nil, fmt.Errorf("schema %s has no tables", restoreDrillSchema)
	}
	out, err := kindMySQL(strings.Join(selects, " UNION ALL "))
	if err != nil {
		return nil, err
	}
	return parseRowCounts(out)
}

// parseRowCounts reads `mysql -N -B` output of (table, count) rows.
func parseRowCounts(out string) (map[string]string, error) {
	counts := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		table, count, ok := strings.Cut(line, "\t")
		if !ok {
			return nil, fmt.Errorf("unexpected row count line %q", line)
		}
		counts["rows/"+table] = count
	}
	return counts, nil
}

// kindExecToFile runs kubectl with stdout written to path, and returns the
// byte count and SHA-256 of what it wrote.
func kindExecToFile(path string, args ...string) (int64, string, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	hash := sha256.New()
	counter := &countingWriter{}
	var stderr bytes.Buffer
	cmd := exec.Command("kubectl", append([]string{"--context", kindContext()}, args...)...)
	cmd.Stdout = io.MultiWriter(f, hash, counter)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return 0, "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if err := f.Close(); err != nil {
		return 0, "", err
	}
	return counter.n, hex.EncodeToString(hash.Sum(nil)), nil
}

type countingWriter struct{ n int64 }

func (c *countingWriter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

// kindExecFromFile runs kubectl with path on stdin.
func kindExecFromFile(path string, args ...string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var out bytes.Buffer
	cmd := exec.Command("kubectl", append([]string{"--context", kindContext()}, args...)...)
	cmd.Stdin = f
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(out.String()))
	}
	return nil
}

// ---- the mc pod ------------------------------------------------------------

// startDrillMCPod runs mc beside a shell with tar, sharing one scratch volume:
// mc mirrors the bucket to and from the volume, and tar streams the volume to
// and from this machine, because the mc image has no tar.
func startDrillMCPod() error {
	mcImage, err := manifestImage("deployment/kind/minio-init.yaml")
	if err != nil {
		return err
	}
	manifest := fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: storage
spec:
  restartPolicy: Never
  containers:
  - name: mc
    image: %s
    command: ["/bin/sh", "-c", "sleep 86400"]
    volumeMounts: [{name: scratch, mountPath: /drill}]
  - name: files
    image: buildmax:local
    imagePullPolicy: Never
    command: ["/bin/sh", "-c", "sleep 86400"]
    volumeMounts: [{name: scratch, mountPath: /drill}]
  volumes:
  - name: scratch
    emptyDir: {}
`, restoreDrillMCPod, mcImage)
	_ = kindKubectl("delete", "pod", restoreDrillMCPod, "-n", "storage", "--ignore-not-found", "--wait=true")
	if err := runStdin(manifest, "kubectl", "--context", kindContext(), "apply", "-f", "-"); err != nil {
		return err
	}
	return kindKubectl("wait", "--for=condition=Ready", "pod/"+restoreDrillMCPod, "-n", "storage", "--timeout=180s")
}

func drillPodExec(container string, script string) error {
	if _, err := captureCombined("kubectl", "--context", kindContext(), "exec", "-n", "storage", restoreDrillMCPod, "-c", container, "--", "/bin/sh", "-c", script); err != nil {
		return fmt.Errorf("%s: %w", container, err)
	}
	return nil
}

const drillMCAlias = "mc alias set drill http://minio.storage.svc.cluster.local:9000 minio minio123 >/dev/null"

// mirrorBucketOut copies the whole bucket with mc mirror and streams it to
// path as a tar, returning its size, digest, and object manifest.
func mirrorBucketOut(path string) (int64, string, map[string]string, error) {
	if err := drillPodExec("files", "rm -rf /drill/"+restoreDrillBucket+" && mkdir -p /drill/"+restoreDrillBucket); err != nil {
		return 0, "", nil, err
	}
	if err := drillPodExec("mc", drillMCAlias+" && mc mirror --quiet drill/"+restoreDrillBucket+" /drill/"+restoreDrillBucket+" >/dev/null"); err != nil {
		return 0, "", nil, fmt.Errorf("mc mirror: %w", err)
	}
	size, sum, err := kindExecToFile(path, "exec", "-n", "storage", restoreDrillMCPod, "-c", "files", "--", "tar", "-cf", "-", "-C", "/drill", restoreDrillBucket)
	if err != nil {
		return 0, "", nil, fmt.Errorf("stream the mirror: %w", err)
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, "", nil, err
	}
	defer f.Close()
	objects, err := bucketManifest(f, restoreDrillBucket)
	return size, sum, objects, err
}

// bucketManifest reads a tar of a mirrored bucket rooted at root/ and returns
// each object key's size and SHA-256.
func bucketManifest(r io.Reader, root string) (map[string]string, error) {
	objects := map[string]string{}
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return objects, nil
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		key, ok := strings.CutPrefix(strings.TrimPrefix(h.Name, "./"), root+"/")
		if !ok {
			return nil, fmt.Errorf("tar entry %q is outside %s/", h.Name, root)
		}
		hash := sha256.New()
		n, err := io.Copy(hash, tr)
		if err != nil {
			return nil, err
		}
		objects["object/"+key] = fmt.Sprintf("%d bytes, sha256 %s", n, hex.EncodeToString(hash.Sum(nil)))
	}
}

// manifestImage returns the one image a manifest names, so the drill runs
// exactly the mc build the cluster already pulled.
func manifestImage(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var images []string
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "image:"); ok {
			images = append(images, strings.TrimSpace(v))
		}
	}
	if len(images) != 1 {
		return "", fmt.Errorf("%s names %d images, want exactly one", path, len(images))
	}
	return images[0], nil
}

// ---- wipe and restore ------------------------------------------------------

func (d *restoreDrill) wipe() error {
	if err := kindKubectl("delete", "namespace", "buildmax", "storage", "db", "--wait=true", "--timeout=600s"); err != nil {
		return err
	}
	for _, ns := range []string{"buildmax", "storage", "db"} {
		if succeeds("kubectl", "--context", kindContext(), "get", "namespace", ns) {
			return fmt.Errorf("namespace %s still exists after the wipe", ns)
		}
	}
	return nil
}

// restoreServices brings up an empty MySQL and MinIO, as kindUp does.
func (d *restoreDrill) restoreServices() error {
	if err := ensureKindNamespace("storage"); err != nil {
		return err
	}
	for _, manifest := range []string{"deployment/kind/mysql.yaml", "deployment/kind/minio.yaml"} {
		if err := kindKubectl("apply", "-f", manifest); err != nil {
			return err
		}
	}
	if err := waitForKindDeployment("db", "mysql", "360s"); err != nil {
		return err
	}
	if err := initializeKindDatabase(); err != nil {
		return err
	}
	if err := waitForKindDeployment("storage", "minio", "360s"); err != nil {
		return err
	}
	return initializeKindBucket()
}

func (d *restoreDrill) restoreDatabase(b restoreDrillBackup) error {
	if err := kindExecFromFile(d.path(b.DatabaseFile), "exec", "-i", "-n", "db", "deploy/mysql", "--",
		"env", "MYSQL_PWD="+kindMySQLRootPassword, "mysql", "-uroot"); err != nil {
		return fmt.Errorf("load the dump: %w", err)
	}
	counts, err := kindRowCounts()
	if err != nil {
		return err
	}
	if diff := diffFingerprints(b.RowCounts, counts); diff.lost() || len(diff.Added) > 0 {
		return fmt.Errorf("restored row counts differ from the backup:%s%s", diff, strings.Join(diff.Added, "\n  added: "))
	}
	fmt.Printf("  %d tables, every row count matches the backup\n", len(counts))
	return nil
}

func (d *restoreDrill) restoreBucket(b restoreDrillBackup) error {
	if err := startDrillMCPod(); err != nil {
		return err
	}
	if err := kindExecFromFile(d.path(b.BucketFile), "exec", "-i", "-n", "storage", restoreDrillMCPod, "-c", "files", "--", "tar", "-xf", "-", "-C", "/drill"); err != nil {
		return fmt.Errorf("stream the backup in: %w", err)
	}
	if err := drillPodExec("mc", drillMCAlias+" && mc mirror --quiet /drill/"+restoreDrillBucket+" drill/"+restoreDrillBucket+" >/dev/null"); err != nil {
		return fmt.Errorf("mc mirror: %w", err)
	}
	fmt.Printf("  %d objects mirrored into a fresh %s\n", len(b.Objects), restoreDrillBucket)
	return nil
}

// restoreServer recreates the buildmax namespace around the restored stores:
// the original KEK and server.yaml from the backup, a new JWT secret, and new
// worker TLS. The restored ConfigMap is in place before the Deployment exists,
// so no server ever starts on the manifest's default configuration.
func (d *restoreDrill) restoreServer(b restoreDrillBackup) error {
	serverYAML, err := os.ReadFile(d.path("server.yaml"))
	if err != nil {
		return err
	}
	if problems := recoveryConfigProblems(string(serverYAML)); len(problems) > 0 {
		return fmt.Errorf("refusing to start a recovery server: %s", strings.Join(problems, "; "))
	}
	if err := ensureKindNamespace("buildmax"); err != nil {
		return err
	}
	if err := applyKindSecret(); err != nil {
		return err
	}
	for _, create := range [][]string{
		{"secret", "generic", "buildmax-kek", "--from-file=kek.json=" + d.path("kek.json")},
		{"configmap", "buildmax-config", "--from-file=server.yaml=" + d.path("server.yaml")},
	} {
		args := append(append([]string{"create"}, create...), "-n", "buildmax", "--dry-run=client", "-o", "yaml")
		manifest, err := captureKindKubectl(args...)
		if err != nil {
			return fmt.Errorf("render %s: %w", create[0], err)
		}
		if err := runStdin(manifest, "kubectl", "--context", kindContext(), "apply", "-f", "-"); err != nil {
			return err
		}
	}
	if err := applyKindWorkerAPITLS(); err != nil {
		return err
	}
	if err := applyKindWorkerSeccompProfile(); err != nil {
		return err
	}
	deploy, err := os.ReadFile("deployment/buildmax-deploy.yaml")
	if err != nil {
		return err
	}
	stripped, err := withoutManifestDocument(string(deploy), "ConfigMap", "buildmax-config")
	if err != nil {
		return err
	}
	if err := runStdin(stripped, "kubectl", "--context", kindContext(), "apply", "-f", "-"); err != nil {
		return err
	}
	if err := kindKubectl("rollout", "status", "daemonset/buildmax-worker-seccomp", "-n", "buildmax", "--timeout=120s"); err != nil {
		return err
	}
	if err := kindKubectl("apply", "-f", "deployment/smoke/mock-llm.kind.yaml"); err != nil {
		return err
	}
	for _, deployment := range []string{"buildmax-smoke-llm", "buildmax-server", "buildmax-portal"} {
		if err := kindKubectl("rollout", "status", "deployment/"+deployment, "-n", "buildmax", "--timeout=300s"); err != nil {
			dumpKindNamespace("buildmax")
			return err
		}
	}
	for _, deployment := range []string{"buildmax-server", "buildmax-portal"} {
		if err := stampKindDeploymentIdentity(deployment); err != nil {
			return err
		}
	}
	return waitForHTTP(context.Background(), d.client, d.target.apiBase+"/healthz", 2*time.Minute)
}

// recoveryConfigProblems names what a recovery server must not start with. A
// Telegram bot token would make it long-poll the live deployment's bot and take
// its users' messages.
func recoveryConfigProblems(serverYAML string) []string {
	var cfg struct {
		Channels struct {
			Telegram struct {
				BotToken string `yaml:"bot_token"`
			} `yaml:"telegram"`
		} `yaml:"channels"`
	}
	if err := yaml.Unmarshal([]byte(serverYAML), &cfg); err != nil {
		return []string{fmt.Sprintf("server.yaml does not parse: %v", err)}
	}
	var problems []string
	if cfg.Channels.Telegram.BotToken != "" {
		problems = append(problems, "server.yaml sets channels.telegram.bot_token; withhold it from a recovery environment")
	}
	if os.Getenv(config.EnvKeyBuildmaxTelegramBotToken) != "" {
		problems = append(problems, config.EnvKeyBuildmaxTelegramBotToken+" is set; withhold it from a recovery environment")
	}
	return problems
}

// withoutManifestDocument drops the one document of the given kind and name
// from a multi-document manifest.
func withoutManifestDocument(manifest, kind, name string) (string, error) {
	docs := strings.Split(manifest, "\n---")
	kept := make([]string, 0, len(docs))
	dropped := 0
	for _, doc := range docs {
		var meta struct {
			Kind     string `yaml:"kind"`
			Metadata struct {
				Name string `yaml:"name"`
			} `yaml:"metadata"`
		}
		if err := yaml.Unmarshal([]byte(doc), &meta); err != nil {
			return "", fmt.Errorf("parse manifest document: %w", err)
		}
		if meta.Kind == kind && meta.Metadata.Name == name {
			dropped++
			continue
		}
		kept = append(kept, doc)
	}
	if dropped != 1 {
		return "", fmt.Errorf("manifest has %d %s/%s documents, want exactly one", dropped, kind, name)
	}
	return strings.Join(kept, "\n---"), nil
}

// ---- verify ----------------------------------------------------------------

// storageVerify runs the operator's reference check in a server pod and keeps
// its output as evidence.
func (d *restoreDrill) storageVerify(name string) error {
	pods, err := serverPodNames()
	if err != nil {
		return err
	}
	if len(pods) == 0 {
		return errors.New("no running buildmax-server pod to run the check in")
	}
	out, runErr := captureCombined("kubectl", "--context", kindContext(), "exec", "-n", "buildmax", "pod/"+pods[0], "--",
		"buildmax-server", "storage", "verify", "--checksums")
	if err := os.WriteFile(d.path(name), []byte(out+"\n"), 0o600); err != nil {
		return err
	}
	for _, line := range strings.Split(out, "\n") {
		// The S3 SDK warns once per object that MinIO sent no checksum header;
		// the file keeps them, the console shows the check's own report.
		if strings.Contains(line, "Response has no supported checksum") {
			continue
		}
		fmt.Printf("  | %s\n", line)
	}
	if runErr != nil {
		return fmt.Errorf("buildmax-server storage verify reported findings or failed: %w", runErr)
	}
	return nil
}

// firstArtifact signs in with a fresh login code and downloads the artifact the
// continued run published, requiring its bytes to match the backup's record.
// It returns the new session's token.
func (d *restoreDrill) firstArtifact(ctx context.Context, seed restoreDrillSeed, before map[string]string) (string, error) {
	token, spaceID, err := smokeSignIn(ctx, d.client, d.target, restoreDrillEmail)
	if err != nil {
		return "", err
	}
	if spaceID != seed.SpaceID {
		return "", fmt.Errorf("the account's personal space is %s after the restore, was %s", spaceID, seed.SpaceID)
	}
	want := before["artifact/"+seed.RunArtifactID+"/download_sha256"]
	content, err := requestText(ctx, d.client, http.MethodGet, d.target.apiBase+"/api/artifacts/"+url.PathEscape(seed.RunArtifactID)+"/content", token, nil, http.StatusOK)
	if err != nil {
		return "", err
	}
	if got := sha256Hex(content); got != want {
		return "", fmt.Errorf("artifact %s downloaded with sha256 %s, want %s", seed.RunArtifactID, got, want)
	}
	return token, nil
}

// compareBucket mirrors the restored bucket back out after the server has run
// on it, so an object its startup removed shows as loss.
func (d *restoreDrill) compareBucket(b restoreDrillBackup) error {
	if err := startDrillMCPod(); err != nil {
		return err
	}
	_, _, objects, err := mirrorBucketOut(d.path("bucket-after.tar"))
	if err != nil {
		return err
	}
	diff := diffFingerprints(b.Objects, objects)
	if diff.lost() {
		return fmt.Errorf("the restored bucket lost objects:%s", diff)
	}
	fmt.Printf("  %d backed-up objects present with the same digest; %d new since\n", len(b.Objects), len(diff.Added))
	return kindKubectl("delete", "pod", restoreDrillMCPod, "-n", "storage", "--ignore-not-found", "--wait=false")
}

// exerciseRestored proves the restored data works, not only that it is there.
func (d *restoreDrill) exerciseRestored(ctx context.Context, seed restoreDrillSeed, token string) error {
	base := d.spaceBase(seed.SpaceID)
	// The JWT secret was regenerated, so a session from before the backup is gone.
	if err := expectStatusWithToken(ctx, d.client, d.target.apiBase+"/api/spaces", seed.token, http.StatusUnauthorized); err != nil {
		return fmt.Errorf("a pre-backup session token: %w", err)
	}
	// A Continue restores the backed-up checkpoint and session bundle. The mock
	// was redeployed empty, so the first run's marker reaching it now can only
	// have come from the restored session history.
	runID, err := d.continueTask(ctx, base+"/tasks/"+url.PathEscape(seed.TaskID), token, "Reply after the restore.", false)
	if err != nil {
		return err
	}
	var requests []struct{ Body []byte }
	if err := requestJSON(ctx, d.client, http.MethodGet, d.target.llmControlRequestsURL, "", nil, &requests, http.StatusOK); err != nil {
		return err
	}
	found := false
	for _, r := range requests {
		if strings.Contains(string(r.Body), "restore-drill history "+seed.Marker) {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("continued run %s did not carry the first run's history; the session bundle did not restore", runID)
	}
	fmt.Printf("  Continue %s restored the backed-up workspace checkpoint and session history\n", runID)
	// The restored Secret decrypts under the restored KEK.
	taskID, err := d.secretRun(ctx, base, token, seed.AgentID, seed.SecretSHA256)
	if err != nil {
		return err
	}
	fmt.Printf("  Agent task %s received the pre-backup Secret value under the restored KEK\n", taskID)
	return nil
}
