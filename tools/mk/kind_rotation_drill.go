package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// `./make kind drill rotation` rehearses docs/deploy/credential-rotation.md on
// a kind cluster: it rotates the JWT secret, the database password, the
// object-storage key, a managed model's provider key, and the Space Secret KEK
// the way the runbook tells an operator to — patch the Secret, roll the server —
// and asserts what each rotation promises and records what it costs.
//
// It is separate from `kind smoke` because it is destructive: every credential
// on the cluster ends up replaced, and the in-flight run it holds across the JWT
// rotation is lost on purpose. So it refuses any cluster but this worktree's
// ephemeral one, which is thrown away afterwards with `kind down`.
//
// A kind run is a rehearsal of the procedure, not the Beta evidence: that has
// to come from the pinned candidate against its real MySQL, S3, and ingress
// (docs/deploy/beta-readiness.md).

const rotationDrillEmail = "rotation-drill@buildmax.local"

// rotationDrillModel is the catalog row whose key the drill rotates. It points
// at the in-cluster mock, which records the key each call presented.
const rotationDrillModel = "Rotation drill"

const rotationDrillModelURL = "http://buildmax-smoke-llm.buildmax.svc.cluster.local:8080/v1"

// rotationDrillStall holds the in-flight run's model call across the JWT
// rotation, so it is still running when the secret changes under it. Longer
// than a server rollout, shorter than the reaper's settle, so the job it
// leaves behind drains before the storage step waits on it.
const rotationDrillStall = 3 * time.Minute

// rotationDrillRunLost bounds how long the in-flight run may take to be
// settled FAILED once its run token stops verifying: the reaper's two-minute
// liveness grace plus its sweep interval, with room.
const rotationDrillRunLost = 6 * time.Minute

// kindMySQLRootPassword is the kind MySQL's root password
// (deployment/kind/mysql.yaml). The drill administers the application account
// with it, as an operator's DBA account would.
const kindMySQLRootPassword = "buildmax"

// kindMinIORootUser and kindMinIORootPassword are the kind MinIO's
// administrator (deployment/kind/minio.yaml); the drill manages storage users
// with it, never serves with it.
const (
	kindMinIORootUser     = "minio"
	kindMinIORootPassword = "minio123"
)

// rotationDrill carries what the steps share: the signed-in session, the data
// written before the first rotation that every later step must still find, and
// the measurements the summary prints.
type rotationDrill struct {
	ctx    context.Context
	client *http.Client
	target smokeTarget

	access, refresh, userID, spaceID string

	artifactID, artifactSHA string
	modelID                 string
	secretBefore            string

	rows []drillRow
}

type drillRow struct {
	step, measure, value string
}

func (d *rotationDrill) record(step, measure, format string, args ...any) {
	value := fmt.Sprintf(format, args...)
	d.rows = append(d.rows, drillRow{step, measure, value})
	fmt.Printf("  [%s] %s: %s\n", step, measure, value)
}

func (d *rotationDrill) space() string {
	return d.target.apiBase + "/api/spaces/" + url.PathEscape(d.spaceID)
}

// cmdKindDrill runs one named drill. Each is destructive, so each checks for
// the ephemeral cluster itself.
func cmdKindDrill(args []string) error {
	if len(args) != 1 {
		return usageErrorf("kind", "drill needs one drill name: rotation")
	}
	switch args[0] {
	case "rotation":
		return kindRotationDrill()
	default:
		return usageErrorf("kind", "unknown drill %q; want rotation", args[0])
	}
}

// requireEphemeralKindCluster refuses to drill a cluster this worktree does not
// own: the resident buildmaxdev, one named by BUILDMAX_KIND_CLUSTER, or none.
func requireEphemeralKindCluster() error {
	e, ok := readEphemeralKind()
	if !ok {
		return fmt.Errorf("the rotation drill replaces every credential on the cluster it runs against, so it runs only on this worktree's ephemeral cluster, and %s records none; create one with BUILDMAX_KIND_EPHEMERAL=1 %s kind up", ephemeralKindMarker, mk())
	}
	if cluster := kindClusterName(); cluster != e.cluster {
		return fmt.Errorf("BUILDMAX_KIND_CLUSTER selects %q, not this worktree's ephemeral cluster %q; the rotation drill runs only on the ephemeral one", cluster, e.cluster)
	}
	return nil
}

func kindRotationDrill() error {
	if err := requireEphemeralKindCluster(); err != nil {
		return err
	}
	if err := requireCommands("kubectl"); err != nil {
		return err
	}
	if exists, err := kindClusterExists(kindClusterName()); err != nil {
		return err
	} else if !exists {
		return fmt.Errorf("kind cluster %q does not exist; run BUILDMAX_KIND_EPHEMERAL=1 %s kind up", kindClusterName(), mk())
	}

	d := &rotationDrill{ctx: context.Background(), client: &http.Client{Timeout: 30 * time.Second}, target: kindSmokeTarget()}
	started := time.Now()
	fmt.Printf("Credential rotation drill on %s (%s)\n", kindClusterName(), started.UTC().Format(time.RFC3339))

	steps := []struct {
		name string
		run  func() error
	}{
		{"setup", d.setup},
		{"jwt", d.rotateJWT},
		{"database", d.rotateDatabasePassword},
		{"storage", d.rotateStorageKey},
		{"model", d.rotateModelKey},
		{"kek", d.rotateKEK},
		{"final", d.final},
	}
	for _, step := range steps {
		fmt.Printf("\n== %s ==\n", step.name)
		begun := time.Now()
		if err := step.run(); err != nil {
			return fmt.Errorf("rotation drill, %s step: %w", step.name, err)
		}
		d.record(step.name, "step took", "%s", time.Since(begun).Round(time.Second))
	}

	fmt.Printf("\nCredential rotation drill passed in %s. Measurements:\n\n", time.Since(started).Round(time.Second))
	fmt.Println("| Step | Measure | Value |")
	fmt.Println("|---|---|---|")
	for _, r := range d.rows {
		fmt.Printf("| %s | %s | %s |\n", r.step, r.measure, r.value)
	}
	fmt.Printf("\nEvery credential on %s has been replaced; remove the cluster with %s kind down.\n", kindClusterName(), mk())
	return nil
}

// setup signs in and writes what later steps must still find: an artifact in
// object storage, a managed model with a sealed key, and a Space Secret sealed
// under the current KEK.
func (d *rotationDrill) setup() error {
	if err := d.signIn(); err != nil {
		return err
	}

	content := fmt.Sprintf("credential rotation drill %d\n", time.Now().UnixNano())
	sum := sha256.Sum256([]byte(content))
	d.artifactSHA = hex.EncodeToString(sum[:])
	id, err := d.uploadArtifact(content)
	if err != nil {
		return err
	}
	d.artifactID = id
	d.record("setup", "artifact", "%s sha256 %s…", id, d.artifactSHA[:12])

	modelID, err := d.ensureDrillModel()
	if err != nil {
		return err
	}
	d.modelID = modelID
	if err := d.expectModelKey("drill-key-initial"); err != nil {
		return err
	}

	d.secretBefore = fmt.Sprintf("rotation_drill_%d", time.Now().Unix())
	if err := d.createSpaceSecret(d.secretBefore); err != nil {
		return err
	}
	keys, err := d.secretKeyIDs()
	if err != nil {
		return err
	}
	d.record("setup", "sealed rows by KEK", "%s", formatKeyCounts(keys))
	return nil
}

// rotateJWT replaces the signing key. Every access and run token dies with it;
// refresh tokens are rows and survive, and the run in flight is lost.
func (d *rotationDrill) rotateJWT() error {
	oldSecret, err := kindSecretValue("buildmax-secret", "BUILDMAX_JWT_SECRET")
	if err != nil {
		return err
	}
	newSecret, err := randomHex(32)
	if err != nil {
		return err
	}

	// A run held in its model call, so the rotation lands mid-run.
	inflightURL, err := d.startStalledRun()
	if err != nil {
		return err
	}

	rotated := time.Now()
	if err := kindPatchSecret("buildmax-secret", map[string]string{"BUILDMAX_JWT_SECRET": newSecret}); err != nil {
		return err
	}
	if err := d.rollServer("jwt"); err != nil {
		return err
	}

	spacesURL := d.target.apiBase + "/api/spaces"
	if err := expectStatusWithToken(d.ctx, d.client, spacesURL, d.access, http.StatusUnauthorized); err != nil {
		return fmt.Errorf("the access token issued before the rotation: %w", err)
	}
	d.record("jwt", "old access token", "401")

	forgedOld, err := reSignAccessToken(d.access, oldSecret)
	if err != nil {
		return err
	}
	if err := expectStatusWithToken(d.ctx, d.client, spacesURL, forgedOld, http.StatusUnauthorized); err != nil {
		return fmt.Errorf("a fresh token signed with the old secret: %w", err)
	}
	// The same forgery under the new secret is accepted, so the 401 above is the
	// key being refused and not a malformed token.
	forgedNew, err := reSignAccessToken(d.access, newSecret)
	if err != nil {
		return err
	}
	if err := expectStatusWithToken(d.ctx, d.client, spacesURL, forgedNew, http.StatusOK); err != nil {
		return fmt.Errorf("the control token signed with the new secret: %w", err)
	}
	d.record("jwt", "token signed with old secret", "401 (same claims under the new secret: 200)")

	if err := d.refreshSession(); err != nil {
		return fmt.Errorf("the refresh token issued before the rotation: %w", err)
	}
	if err := expectStatusWithToken(d.ctx, d.client, spacesURL, d.access, http.StatusOK); err != nil {
		return fmt.Errorf("the access token refreshed after the rotation: %w", err)
	}
	d.record("jwt", "old refresh token", "200, and its new access token reads /api/spaces")

	if err := d.runTaskToSuccess("jwt"); err != nil {
		return err
	}

	message, err := waitForTaskFailure(d.ctx, d.client, inflightURL, d.access, rotationDrillRunLost)
	if err != nil {
		return fmt.Errorf("the run in flight across the rotation: %w", err)
	}
	d.record("jwt", "in-flight run (measured disruption)", "FAILED %s after the Secret patch: %q", time.Since(rotated).Round(time.Second), message)
	return nil
}

// rotateDatabasePassword uses MySQL's dual password: the new one is added while
// the old one stays valid, the server rolls onto it, and only then is the old
// one discarded. The server's pooled connections are already authenticated, so
// nothing should notice.
func (d *rotationDrill) rotateDatabasePassword() error {
	oldPassword, err := kindSecretValue("buildmax-secret", "BUILDMAX_DATABASE_PASSWORD")
	if err != nil {
		return err
	}
	newPassword, err := randomHex(16)
	if err != nil {
		return err
	}

	stopAPI := d.sampleAPI()
	if _, err := kindMySQL(fmt.Sprintf("ALTER USER 'buildmax'@'%%' IDENTIFIED BY '%s' RETAIN CURRENT PASSWORD", newPassword)); err != nil {
		return fmt.Errorf("add the new password: %w", err)
	}
	if err := kindPatchSecret("buildmax-secret", map[string]string{"BUILDMAX_DATABASE_PASSWORD": newPassword}); err != nil {
		return err
	}
	if err := d.rollServer("database"); err != nil {
		return err
	}

	// From here the server runs on the new password alone: discarding the old
	// one must not restart a pod or flip its readiness.
	pods, err := settledServerPods()
	if err != nil {
		return err
	}
	restarts, err := serverPodRestarts()
	if err != nil {
		return err
	}
	stopReadyz, err := d.sampleReadyz(pods)
	if err != nil {
		return err
	}
	if _, err := kindMySQL("ALTER USER 'buildmax'@'%' DISCARD OLD PASSWORD"); err != nil {
		stopReadyz()
		return fmt.Errorf("discard the old password: %w", err)
	}
	// Long enough for readiness probes and a request-driven reconnect to hit it.
	time.Sleep(20 * time.Second)
	readyzTotal, readyzBad, readyzSample := stopReadyz()
	apiTotal, apiBad, apiSample := stopAPI()

	if out, err := kindMySQLAs("buildmax", oldPassword, "SELECT 1"); err == nil || !strings.Contains(out, "Access denied") {
		return fmt.Errorf("the old database password still signs in (err=%v): %s", err, out)
	}
	if _, err := kindMySQLAs("buildmax", newPassword, "SELECT 1"); err != nil {
		return fmt.Errorf("the new database password does not sign in: %w", err)
	}
	d.record("database", "old password", "Access denied; new password signs in")

	if err := assertServerPodsUnrestarted(restarts); err != nil {
		return fmt.Errorf("discarding the old password: %w", err)
	}
	d.record("database", "server restarts after discard", "0")
	d.record("database", "/readyz non-200 after discard", "%d of %d samples%s", readyzBad, readyzTotal, sampleNote(readyzSample))
	d.record("database", "API non-200 through the ingress", "%d of %d samples%s", apiBad, apiTotal, sampleNote(apiSample))
	if readyzBad > 0 {
		return fmt.Errorf("/readyz answered non-200 %d times after the old password was discarded: %s", readyzBad, readyzSample)
	}
	if apiBad > 0 {
		return fmt.Errorf("the API answered non-200 %d times through the database rotation: %s", apiBad, apiSample)
	}
	return nil
}

// rotateStorageKey overlaps two storage identities: the new user is added, the
// server rolls onto it, the runs holding the old key by value drain, and only
// then is the old user disabled.
func (d *rotationDrill) rotateStorageKey() error {
	oldUser, err := kindSecretValue("buildmax-secret", "BUILDMAX_STORAGE_MINIO_ACCESS_KEY")
	if err != nil {
		return err
	}
	oldKey, err := kindSecretValue("buildmax-secret", "BUILDMAX_STORAGE_MINIO_SECRET_KEY")
	if err != nil {
		return err
	}
	suffix, err := randomHex(3)
	if err != nil {
		return err
	}
	newUser := "buildmax-" + suffix
	newKey, err := randomHex(20)
	if err != nil {
		return err
	}

	if out, err := kindMinIOAdmin("drill-storage-add", fmt.Sprintf(
		"mc admin user add root %s %s && mc admin policy attach root bmstore-rw --user %s", newUser, newKey, newUser)); err != nil {
		return fmt.Errorf("add the new storage user: %w\n%s", err, out)
	}
	stopAPI := d.sampleAPI()
	if err := kindPatchSecret("buildmax-secret", map[string]string{
		"BUILDMAX_STORAGE_MINIO_ACCESS_KEY": newUser,
		"BUILDMAX_STORAGE_MINIO_SECRET_KEY": newKey,
	}); err != nil {
		stopAPI()
		return err
	}
	if err := d.rollServer("storage"); err != nil {
		stopAPI()
		return err
	}
	apiTotal, apiBad, apiSample := stopAPI()
	d.record("storage", "API non-200 through the ingress during the roll", "%d of %d samples%s", apiBad, apiTotal, sampleNote(apiSample))

	// A worker Job carries the storage key it was created with, by value, so the
	// old user stays enabled until every Job created before the roll is gone.
	drained := time.Now()
	if err := waitForNoActiveWorkerJobs(d.ctx, 10*time.Minute); err != nil {
		return err
	}
	d.record("storage", "drain of worker Jobs holding the old key", "%s", time.Since(drained).Round(time.Second))

	if out, err := kindMinIOAdmin("drill-storage-disable", "mc admin user disable root "+oldUser); err != nil {
		return fmt.Errorf("disable the old storage user: %w\n%s", err, out)
	}
	refusal, err := expectStorageKeyRefused(oldUser, oldKey)
	if err != nil {
		return err
	}
	d.record("storage", "old key", "403 after the old user is disabled (%q); new key lists the bucket", refusal)

	content, err := requestText(d.ctx, d.client, http.MethodGet, d.target.apiBase+"/api/artifacts/"+url.PathEscape(d.artifactID)+"/content", d.access, nil, http.StatusOK)
	if err != nil {
		return fmt.Errorf("download the artifact stored before the rotation: %w", err)
	}
	if sum := sha256.Sum256([]byte(content)); hex.EncodeToString(sum[:]) != d.artifactSHA {
		return fmt.Errorf("the artifact stored before the rotation downloads with a different checksum")
	}
	d.record("storage", "pre-rotation artifact", "downloads with the same sha256")

	// A worker created now is handed the new key; a run that seeds and stores
	// its state proves it works.
	return d.runTaskToSuccess("storage")
}

// rotateModelKey replaces a managed model's provider key in place. The gateway
// keys its client cache by the row's revision, so the next call uses the new
// key with no restart.
func (d *rotationDrill) rotateModelKey() error {
	before, err := d.modelRow()
	if err != nil {
		return err
	}
	suffix, err := randomHex(4)
	if err != nil {
		return err
	}
	newKey := "drill-key-" + suffix
	if out, err := kindServerCommand(newKey+"\n", "model", "set-key", "--id", d.modelID); err != nil {
		return fmt.Errorf("set the new model key: %w\n%s", err, out)
	}
	after, err := d.modelRow()
	if err != nil {
		return err
	}
	if after.updatedAt == before.updatedAt || after.sealedSum == before.sealedSum {
		return fmt.Errorf("model set-key left the row's revision or sealed key unchanged (updated_at %s -> %s)", before.updatedAt, after.updatedAt)
	}
	d.record("model", "row revision", "updated_at %s -> %s, sealed key replaced", before.updatedAt, after.updatedAt)
	restarts, err := serverPodRestarts()
	if err != nil {
		return err
	}
	if err := d.expectModelKey(newKey); err != nil {
		return err
	}
	if err := assertServerPodsUnrestarted(restarts); err != nil {
		return err
	}
	d.record("model", "next gateway call", "sent the new key upstream, no restart")
	return nil
}

// rotateKEK walks the key-encryption key through the runbook: add the new key,
// roll; make it current, roll; rewrap every sealed row; remove the old key,
// roll. The server refuses to start without a key a row still names, so the
// last roll coming up is itself the proof no row was left behind.
func (d *rotationDrill) rotateKEK() error {
	file, err := kindKEKFile()
	if err != nil {
		return err
	}
	oldID := file.Current
	newID := fmt.Sprintf("kek-drill-%d", time.Now().Unix())
	material := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, material); err != nil {
		return err
	}
	file.Keys[newID] = base64.StdEncoding.EncodeToString(material)
	if err := applyKEKFile(file); err != nil {
		return err
	}
	if err := d.rollServer("kek add"); err != nil {
		return err
	}

	file.Current = newID
	if err := applyKEKFile(file); err != nil {
		return err
	}
	if err := d.rollServer("kek switch"); err != nil {
		return err
	}

	secretAfter := fmt.Sprintf("rotation_drill_after_%d", time.Now().Unix())
	if err := d.createSpaceSecret(secretAfter); err != nil {
		return err
	}
	keyID, err := kindMySQL(fmt.Sprintf("SELECT key_id FROM secret WHERE name = '%s'", secretAfter))
	if err != nil {
		return err
	}
	if strings.TrimSpace(keyID) != newID {
		return fmt.Errorf("a Secret written after the switch is sealed under %q, want %q", strings.TrimSpace(keyID), newID)
	}
	d.record("kek", "Secret written after the switch", "sealed under %s", newID)

	out, err := kindServerCommand("", "secret", "rewrap")
	if err != nil {
		return fmt.Errorf("rewrap: %w\n%s", err, out)
	}
	fmt.Println(indent(out, "    "))
	keys, err := d.secretKeyIDs()
	if err != nil {
		return err
	}
	for id, n := range keys {
		if id != newID && n > 0 {
			return fmt.Errorf("after rewrap %d sealed rows still name %s: %v", n, id, keys)
		}
	}
	d.record("kek", "sealed rows by KEK after rewrap", "%s", formatKeyCounts(keys))

	delete(file.Keys, oldID)
	if err := applyKEKFile(file); err != nil {
		return err
	}
	if err := d.rollServer("kek retire"); err != nil {
		return fmt.Errorf("the server did not start without the retired key %s: %w", oldID, err)
	}
	restarts, err := serverPodRestarts()
	if err != nil {
		return err
	}
	for pod, n := range restarts {
		if n > 0 {
			return fmt.Errorf("server pod %s restarted %d times after the old key was removed", pod, n)
		}
	}
	// The managed key was sealed under the old KEK at setup; opening it now
	// proves the rewrap moved it rather than stranding it.
	if err := d.expectModelKey(""); err != nil {
		return err
	}
	d.record("kek", "old key removed", "%s retired, server restarted cleanly, sealed model key still opens", oldID)
	return nil
}

// final confirms the session, the pre-rotation data, and new work all survive
// the whole sequence.
func (d *rotationDrill) final() error {
	var secrets struct {
		Secrets []struct {
			Name string `json:"name"`
		} `json:"secrets"`
	}
	if err := requestJSON(d.ctx, d.client, http.MethodGet, d.space()+"/secrets", d.access, nil, &secrets, http.StatusOK); err != nil {
		return err
	}
	found := false
	for _, s := range secrets.Secrets {
		found = found || s.Name == d.secretBefore
	}
	if !found {
		return fmt.Errorf("the Space Secret %s written before the rotations is not listed", d.secretBefore)
	}
	return d.runTaskToSuccess("final")
}

// --- session ---

func (d *rotationDrill) signIn() error {
	if out, err := d.target.admin("user", "create", rotationDrillEmail); err != nil && !strings.Contains(out, "already has an account") {
		return fmt.Errorf("create the drill account: %w", err)
	}
	code, err := issueLoginCode(d.target, rotationDrillEmail)
	if err != nil {
		return err
	}
	var login struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		User         struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := requestJSON(d.ctx, d.client, http.MethodPost, d.target.apiBase+"/api/auth/login", "", map[string]string{
		"email": rotationDrillEmail, "otp": code, "platform": "rotation-drill",
	}, &login, http.StatusOK); err != nil {
		return fmt.Errorf("sign in: %w", err)
	}
	if login.AccessToken == "" || login.RefreshToken == "" {
		return errors.New("the login returned no access or refresh token")
	}
	d.access, d.refresh, d.userID = login.AccessToken, login.RefreshToken, login.User.ID
	var spaces []struct {
		ID string `json:"id"`
	}
	if err := requestJSON(d.ctx, d.client, http.MethodGet, d.target.apiBase+"/api/spaces", d.access, nil, &spaces, http.StatusOK); err != nil {
		return err
	}
	if len(spaces) == 0 {
		return errors.New("the drill account has no space")
	}
	d.spaceID = spaces[0].ID
	return nil
}

func (d *rotationDrill) refreshSession() error {
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := requestJSON(d.ctx, d.client, http.MethodPost, d.target.apiBase+"/api/auth/token/refresh", "",
		map[string]string{"refresh_token": d.refresh}, &out, http.StatusOK); err != nil {
		return err
	}
	if out.AccessToken == "" {
		return errors.New("the refresh returned no access token")
	}
	d.access, d.refresh = out.AccessToken, out.RefreshToken
	return nil
}

// reSignAccessToken copies an access token's claims into a fresh, unexpired
// token signed with secret. Signed with the retired secret it is exactly what
// someone holding the old key could mint.
func reSignAccessToken(token, secret string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errors.New("the access token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode the access token: %w", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("decode the access token claims: %w", err)
	}
	jti, err := randomHex(10)
	if err != nil {
		return "", err
	}
	now := time.Now()
	claims["jti"] = jti
	claims["iat"] = now.Unix()
	claims["exp"] = now.Add(10 * time.Minute).Unix()
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	unsigned := header + "." + base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// --- runs ---

// startStalledRun creates a task and holds its worker's model call, returning
// once that call is waiting in the mock. The stall is cleared at once: a stall
// is fixed when a call arrives, so the held call stays held while every later
// call — the new runs the step starts — answers normally.
func (d *rotationDrill) startStalledRun() (string, error) {
	convID, err := coordCreateConversation(d.ctx, d.client, d.target.apiBase, d.spaceID, d.access)
	if err != nil {
		return "", err
	}
	suffix, err := randomHex(6)
	if err != nil {
		return "", err
	}
	marker := "rotation-drill-inflight-" + suffix
	taskID, err := coordCreateTask(d.ctx, d.client, d.target.apiBase, d.spaceID, convID, d.access, "Hold this run across the key rotation: "+marker)
	if err != nil {
		return "", err
	}
	// The title call made while creating the task already carried the marker.
	seen, err := coordMockRequestsContaining(d.ctx, d.client, d.target, marker)
	if err != nil {
		return "", err
	}
	if err := armLLMStall(d.ctx, d.client, d.target, rotationDrillStall); err != nil {
		return "", err
	}
	defer func() { _ = armLLMStall(d.ctx, d.client, d.target, 0) }()
	taskURL := d.space() + "/tasks/" + url.PathEscape(taskID)
	if err := waitForTaskStatus(d.ctx, d.client, taskURL, d.access, "RUNNING", 90*time.Second); err != nil {
		return "", fmt.Errorf("the in-flight run did not start: %w", err)
	}
	if err := retryFor(d.ctx, 90*time.Second, func() error {
		n, err := coordMockRequestsContaining(d.ctx, d.client, d.target, marker)
		if err != nil {
			return err
		}
		if n <= seen {
			return errors.New("the worker has not called the model yet")
		}
		return nil
	}); err != nil {
		return "", fmt.Errorf("the in-flight run never reached its model call: %w", err)
	}
	fmt.Println("  a run is held in its model call; rotating under it")
	return taskURL, nil
}

func (d *rotationDrill) runTaskToSuccess(step string) error {
	convID, err := coordCreateConversation(d.ctx, d.client, d.target.apiBase, d.spaceID, d.access)
	if err != nil {
		return err
	}
	taskID, err := coordCreateTask(d.ctx, d.client, d.target.apiBase, d.spaceID, convID, d.access, "Reply with exactly deployment smoke ok.")
	if err != nil {
		return err
	}
	begun := time.Now()
	if _, err := waitForTaskSuccess(d.ctx, d.client, d.space()+"/tasks/"+url.PathEscape(taskID), d.access); err != nil {
		return fmt.Errorf("a new run after the %s rotation: %w", step, err)
	}
	d.record(step, "new run", "SUCCEEDED in %s", time.Since(begun).Round(time.Second))
	return nil
}

// --- data ---

func (d *rotationDrill) uploadArtifact(content string) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "rotation-drill.txt")
	if err != nil {
		return "", err
	}
	if _, err := io.WriteString(part, content); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	resp, err := request(d.ctx, d.client, http.MethodPost, d.space()+"/artifacts", d.access, writer.FormDataContentType(), &body, http.StatusCreated)
	if err != nil {
		return "", fmt.Errorf("upload the pre-rotation artifact: %w", err)
	}
	defer resp.Close()
	var created struct {
		ID     string `json:"id"`
		SHA256 string `json:"sha256"`
	}
	if err := json.NewDecoder(resp).Decode(&created); err != nil {
		return "", err
	}
	if created.SHA256 != d.artifactSHA {
		return "", fmt.Errorf("the server recorded sha256 %s for the artifact, want %s", created.SHA256, d.artifactSHA)
	}
	return created.ID, nil
}

func (d *rotationDrill) createSpaceSecret(name string) error {
	return requestJSON(d.ctx, d.client, http.MethodPost, d.space()+"/secrets", d.access, map[string]any{
		"name":  name,
		"items": map[string]string{"DRILL_VALUE": "sealed before and after rotation"},
	}, nil, http.StatusCreated)
}

// secretKeyIDs counts the sealed rows by the KEK that wraps them: Space Secrets
// name it in a column, managed model keys inside their sealed JSON blob.
func (d *rotationDrill) secretKeyIDs() (map[string]int, error) {
	out, err := kindMySQL("SELECT key_id FROM secret WHERE key_id <> '' UNION ALL " +
		"SELECT JSON_UNQUOTE(JSON_EXTRACT(CAST(api_key_sealed AS CHAR), '$.k')) FROM llm_model WHERE api_key_sealed IS NOT NULL AND LENGTH(api_key_sealed) > 0")
	if err != nil {
		return nil, err
	}
	keys := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			keys[line]++
		}
	}
	return keys, nil
}

// ensureDrillModel adds the drill's catalog row, or reuses it on a rerun, and
// sets its key to the known initial value.
func (d *rotationDrill) ensureDrillModel() (string, error) {
	ids, err := kindCatalogIDs(d.target)
	if err != nil {
		return "", err
	}
	if id, ok := ids[rotationDrillModel]; ok {
		if out, err := kindServerCommand("drill-key-initial\n", "model", "set-key", "--id", id); err != nil {
			return "", fmt.Errorf("reset the drill model key: %w\n%s", err, out)
		}
		return id, nil
	}
	out, err := kindServerCommand("drill-key-initial\n", "model", "add", "--name", rotationDrillModel,
		"--model", "buildmax-smoke", "--api-url", rotationDrillModelURL, "--api-key", "-")
	if err != nil {
		return "", fmt.Errorf("add the drill model: %w", err)
	}
	match := addedModelPattern.FindStringSubmatch(out)
	if match == nil {
		return "", fmt.Errorf("model add printed no model ID: %s", out)
	}
	return match[1], nil
}

type modelRowState struct{ updatedAt, sealedSum string }

func (d *rotationDrill) modelRow() (modelRowState, error) {
	out, err := kindMySQL(fmt.Sprintf("SELECT updated_at, SHA2(api_key_sealed, 256) FROM llm_model WHERE public_id = '%s'", d.modelID))
	if err != nil {
		return modelRowState{}, err
	}
	fields := strings.Split(strings.TrimSpace(out), "\t")
	if len(fields) != 2 {
		return modelRowState{}, fmt.Errorf("unexpected llm_model row for %s: %q", d.modelID, out)
	}
	return modelRowState{fields[0], fields[1]}, nil
}

// expectModelKey calls the drill model through the managed gateway and, when
// want is set, confirms the mock received that key.
func (d *rotationDrill) expectModelKey(want string) error {
	suffix, err := randomHex(6)
	if err != nil {
		return err
	}
	marker := "rotation-drill-model-" + suffix
	if err := requestJSON(d.ctx, d.client, http.MethodPost, d.target.apiBase+"/api/llm/completions", d.access, map[string]any{
		"model":    rotationDrillModel,
		"messages": []map[string]string{{"role": "user", "content": marker}},
	}, nil, http.StatusOK); err != nil {
		return fmt.Errorf("call the drill model through the gateway: %w", err)
	}
	if want == "" {
		return nil
	}
	var reqs []struct {
		Body       []byte
		Credential string
	}
	if err := requestJSON(d.ctx, d.client, http.MethodGet, d.target.llmControlRequestsURL, "", nil, &reqs, http.StatusOK); err != nil {
		return fmt.Errorf("read the mock's request log: %w", err)
	}
	for _, r := range reqs {
		if strings.Contains(string(r.Body), marker) {
			if r.Credential != want {
				return fmt.Errorf("the gateway sent a key other than the one just set")
			}
			return nil
		}
	}
	return errors.New("the mock recorded no call carrying the drill marker")
}

// --- cluster operations ---

// rollServer restarts the server Deployment so it rereads its Secrets, and
// waits until only pods of the new ReplicaSet remain.
func (d *rotationDrill) rollServer(step string) error {
	begun := time.Now()
	if out, err := captureKindKubectl("rollout", "restart", "deployment/buildmax-server", "-n", "buildmax"); err != nil {
		return fmt.Errorf("restart the server: %w\n%s", err, out)
	}
	if out, err := captureCombined("kubectl", "--context", kindContext(), "rollout", "status",
		"deployment/buildmax-server", "-n", "buildmax", "--timeout=300s"); err != nil {
		dumpKindNamespace("buildmax")
		return fmt.Errorf("server rollout: %w\n%s", err, out)
	}
	if _, err := settledServerPods(); err != nil {
		return err
	}
	d.record(step, "server roll", "%s", time.Since(begun).Round(time.Second))
	return nil
}

// settledServerPods waits until every server pod left is a live member of the
// current rollout — none still terminating — and returns their names.
func settledServerPods() ([]string, error) {
	var pods []string
	err := retryFor(context.Background(), 2*time.Minute, func() error {
		want, err := captureKindKubectl("get", "deployment/buildmax-server", "-n", "buildmax", "-o", "jsonpath={.spec.replicas}")
		if err != nil {
			return err
		}
		out, err := captureKindKubectl("get", "pods", "-n", "buildmax", "-l", "app=buildmax-server",
			"-o", `jsonpath={range .items[*]}{.metadata.name}{" "}{.metadata.deletionTimestamp}{"\n"}{end}`)
		if err != nil {
			return err
		}
		pods = pods[:0]
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			if len(fields) > 1 {
				return fmt.Errorf("server pod %s is still terminating", fields[0])
			}
			pods = append(pods, fields[0])
		}
		if fmt.Sprint(len(pods)) != strings.TrimSpace(want) {
			return fmt.Errorf("%d server pods, want %s", len(pods), want)
		}
		return nil
	})
	return pods, err
}

func kindSecretValue(name, key string) (string, error) {
	out, err := captureKindKubectl("get", "secret", name, "-n", "buildmax",
		"-o", "jsonpath={.data."+strings.ReplaceAll(key, ".", `\.`)+"}")
	if err != nil {
		return "", fmt.Errorf("read %s from Secret %s: %w", key, name, err)
	}
	value, err := base64.StdEncoding.DecodeString(strings.TrimSpace(out))
	if err != nil {
		return "", fmt.Errorf("decode %s from Secret %s: %w", key, name, err)
	}
	return string(value), nil
}

// kindPatchSecret writes values into a Secret without echoing them: kubectl's
// error would carry the patch, so only its output is reported.
func kindPatchSecret(name string, values map[string]string) error {
	patch, err := json.Marshal(map[string]any{"stringData": values})
	if err != nil {
		return err
	}
	if out, err := captureCombined("kubectl", "--context", kindContext(), "patch", "secret", name,
		"-n", "buildmax", "--type", "merge", "-p", string(patch)); err != nil {
		return fmt.Errorf("patch Secret %s: %s", name, strings.TrimSpace(out))
	}
	return nil
}

type kekFile struct {
	Current string            `json:"current"`
	Keys    map[string]string `json:"keys"`
}

func kindKEKFile() (kekFile, error) {
	raw, err := kindSecretValue("buildmax-kek", "kek.json")
	if err != nil {
		return kekFile{}, err
	}
	var f kekFile
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		return kekFile{}, fmt.Errorf("parse the KEK file: %w", err)
	}
	if f.Current == "" || f.Keys[f.Current] == "" {
		return kekFile{}, errors.New("the KEK file names no usable current key")
	}
	return f, nil
}

func applyKEKFile(f kekFile) error {
	body, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return kindPatchSecret("buildmax-kek", map[string]string{"kek.json": string(body)})
}

// kindMySQL runs a statement as root in the kind MySQL pod and returns its
// tab-separated rows without a header.
func kindMySQL(statement string) (string, error) {
	return kindMySQLAs("root", kindMySQLRootPassword, statement)
}

// kindMySQLAs signs in over TCP, so the account's '%' host entry — the one the
// server uses — is what is tested.
func kindMySQLAs(user, password, statement string) (string, error) {
	out, err := captureCombined("kubectl", "--context", kindContext(), "exec", "-n", "db", "deploy/mysql", "--",
		"env", "MYSQL_PWD="+password, "mysql", "-h", "127.0.0.1", "-u"+user, "-N", "-B", "buildmax", "-e", statement)
	if err != nil {
		return out, fmt.Errorf("mysql as %s: %w: %s", user, err, strings.TrimSpace(out))
	}
	return out, nil
}

var mcImagePattern = regexp.MustCompile(`(?m)^\s*image:\s*(\S+)`)

// kindMCImage is the MinIO client image deployment/kind/minio-init.yaml pins,
// already on the node because that Job ran it.
func kindMCImage() (string, error) {
	data, err := os.ReadFile("deployment/kind/minio-init.yaml")
	if err != nil {
		return "", err
	}
	match := mcImagePattern.FindSubmatch(data)
	if match == nil {
		return "", errors.New("deployment/kind/minio-init.yaml names no image")
	}
	return string(match[1]), nil
}

// kindMinIOAdmin runs mc commands against the kind MinIO with an alias `root`
// for its administrator.
func kindMinIOAdmin(name, script string) (string, error) {
	image, err := kindMCImage()
	if err != nil {
		return "", err
	}
	full := fmt.Sprintf("mc alias set root http://minio.storage.svc.cluster.local:9000 %s %s >/dev/null && %s",
		kindMinIORootUser, kindMinIORootPassword, script)
	return captureCombined("kubectl", "--context", kindContext(), "run", name, "-n", "storage",
		"--rm", "-i", "--restart=Never", "--quiet", "--image="+image, "--command", "--", "sh", "-c", full)
}

// expectStorageKeyRefused confirms MinIO answers the disabled key with 403 and
// still serves the key the Secret now holds.
func expectStorageKeyRefused(oldUser, oldKey string) (string, error) {
	newUser, err := kindSecretValue("buildmax-secret", "BUILDMAX_STORAGE_MINIO_ACCESS_KEY")
	if err != nil {
		return "", err
	}
	newKey, err := kindSecretValue("buildmax-secret", "BUILDMAX_STORAGE_MINIO_SECRET_KEY")
	if err != nil {
		return "", err
	}
	image, err := kindMCImage()
	if err != nil {
		return "", err
	}
	// mc alias set itself validates a key, so the old one is given through the
	// environment instead and its first request is the one observed. The image
	// has a shell but no grep, so the match is a case pattern.
	script := `out=$(mc --debug ls old/bmstore 2>&1); echo "BM_OLD_EXIT=$?"; ` +
		`case "$out" in *" 403 "*) echo BM_OLD_403;; esac; ` +
		`echo "BM_OLD_ERROR=$(mc ls old/bmstore 2>&1)"; ` +
		`mc ls new/bmstore >/dev/null 2>&1 && echo BM_NEW_OK`
	out, err := captureCombined("kubectl", "--context", kindContext(), "run", "drill-storage-probe", "-n", "storage",
		"--rm", "-i", "--restart=Never", "--quiet", "--image="+image,
		"--env=MC_HOST_old=http://"+oldUser+":"+oldKey+"@minio.storage.svc.cluster.local:9000",
		"--env=MC_HOST_new=http://"+newUser+":"+newKey+"@minio.storage.svc.cluster.local:9000",
		"--command", "--", "sh", "-c", script)
	if err != nil {
		return "", fmt.Errorf("storage key probe: %w\n%s", err, out)
	}
	if !strings.Contains(out, "BM_OLD_403") || strings.Contains(out, "BM_OLD_EXIT=0") {
		return "", fmt.Errorf("the disabled storage key was not refused with 403:\n%s", out)
	}
	if !strings.Contains(out, "BM_NEW_OK") {
		return "", fmt.Errorf("the new storage key cannot list the bucket:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if refusal, ok := strings.CutPrefix(strings.TrimSpace(line), "BM_OLD_ERROR="); ok {
			return refusal, nil
		}
	}
	return "", nil
}

// waitForNoActiveWorkerJobs waits until no worker Job has a running pod.
func waitForNoActiveWorkerJobs(ctx context.Context, timeout time.Duration) error {
	return retryFor(ctx, timeout, func() error {
		out, err := captureKindKubectl("get", "jobs", "-n", "buildmax", "-l", "app.kubernetes.io/name=buildmax-worker",
			"-o", `jsonpath={range .items[*]}{.metadata.name}{"="}{.status.active}{"\n"}{end}`)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			name, active, _ := strings.Cut(strings.TrimSpace(line), "=")
			if active != "" && active != "0" {
				return fmt.Errorf("worker Job %s is still active", name)
			}
		}
		return nil
	})
}

// --- sampling ---

// sampleAPI reads /api/spaces through the ingress twice a second until the
// returned stop is called, which reports the samples, the non-200 ones, and the
// first failure.
func (d *rotationDrill) sampleAPI() func() (int, int, string) {
	endpoint := d.target.apiBase + "/api/spaces"
	token := d.access
	return sampleUntilStopped(func(ctx context.Context) error {
		body, err := request(ctx, &http.Client{Timeout: 5 * time.Second}, http.MethodGet, endpoint, token, "", nil, http.StatusOK)
		if err != nil {
			return err
		}
		return body.Close()
	})
}

// sampleReadyz reads /readyz straight from each pod — it is not routed through
// the ingress — twice a second until stopped.
func (d *rotationDrill) sampleReadyz(pods []string) (func() (int, int, string), error) {
	var stops []func()
	var urls []string
	for i, pod := range pods {
		stop, base, err := startPodForward(d.ctx, pod, fmt.Sprint(18120+i))
		if err != nil {
			for _, s := range stops {
				s()
			}
			return nil, err
		}
		stops = append(stops, stop)
		urls = append(urls, base+"/readyz")
	}
	stopSampling := sampleUntilStopped(func(ctx context.Context) error {
		for _, u := range urls {
			if err := expectHTTPStatus(ctx, &http.Client{Timeout: 5 * time.Second}, u, http.StatusOK); err != nil {
				return err
			}
		}
		return nil
	})
	return func() (int, int, string) {
		total, bad, first := stopSampling()
		for _, s := range stops {
			s()
		}
		return total, bad, first
	}, nil
}

func sampleUntilStopped(probe func(context.Context) error) func() (int, int, string) {
	ctx, cancel := context.WithCancel(context.Background())
	var (
		mu         sync.Mutex
		total, bad int
		first      string
		done       = make(chan struct{})
	)
	go func() {
		defer close(done)
		for {
			err := probe(ctx)
			if ctx.Err() != nil {
				return
			}
			mu.Lock()
			total++
			if err != nil {
				bad++
				if first == "" {
					first = err.Error()
				}
			}
			mu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
		}
	}()
	return func() (int, int, string) {
		cancel()
		<-done
		mu.Lock()
		defer mu.Unlock()
		return total, bad, first
	}
}

func formatKeyCounts(keys map[string]int) string {
	ids := make([]string, 0, len(keys))
	for id := range keys {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf("%s: %d", id, keys[id]))
	}
	return strings.Join(parts, ", ")
}

func sampleNote(first string) string {
	if first == "" {
		return ""
	}
	return "; first: " + first
}

func indent(text, prefix string) string {
	return prefix + strings.ReplaceAll(strings.TrimSpace(text), "\n", "\n"+prefix)
}
