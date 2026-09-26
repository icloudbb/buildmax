package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// The upgrade drill rehearses a Compose operator's release-day upgrade with
// real images: the predecessor serves real data, the candidate replaces it,
// the predecessor is brought back against the upgraded database, and the
// pre-upgrade backup recovers it. TestUpgradeFromPredecessorSchema proves the
// candidate's db.New opens what the predecessor wrote; it cannot show that the
// candidate's server serves that data, that the old binary refuses what the
// candidate left, or that a paired restore brings the old binary back.

// upgradeDrillRecord is the result a run leaves behind, pass or fail. The
// workflow publishes it as the job summary and an artifact.
const upgradeDrillRecord = ".artifacts/upgrade-drill/result.md"

// newerSchemaRefusalSince is the first release whose binary refuses a database
// that records a migration it does not know. 0.2.0-alpha.15 and earlier
// predate the refusal, so the drill cannot hold them to it.
const newerSchemaRefusalSince = "0.2.0-alpha.16"

// newerSchemaRefusal opens db.NewerSchemaError's message, which the drill
// looks for in the source server's log. TestNewerSchemaRefusalMatchesServer
// keeps the two in step; the task runner does not import the database layer.
const newerSchemaRefusal = "database schema is newer than this binary"

// upgradeDrillServerConfig is the server.yaml the smoke overlay mounts. The
// drill uses that overlay because it puts the deterministic model in front of
// the server, which is what lets "a new run works" be asserted.
const upgradeDrillServerConfig = "deployment/smoke/server.compose.yaml"

type upgradeDrillOptions struct {
	// from is the release tag the deployment upgrades from.
	from string
	// to is an image tag to upgrade to. Empty builds this checkout.
	to string
}

func parseUpgradeDrillArgs(args []string) (upgradeDrillOptions, error) {
	var opts upgradeDrillOptions
	fs := flag.NewFlagSet("compose upgrade-drill", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.from, "from", "", "release tag to upgrade from")
	fs.StringVar(&opts.to, "to", "", "image tag to upgrade to")
	if err := fs.Parse(args); err != nil {
		return opts, usageErrorf("compose", "compose upgrade-drill: %v", err)
	}
	if fs.NArg() != 0 {
		return opts, usageErrorf("compose", "compose upgrade-drill takes only --from and --to, not %q", fs.Arg(0))
	}
	opts.from = strings.TrimPrefix(opts.from, "v")
	if opts.from != "" && !upgradeFixtureTag.MatchString(opts.from) {
		return opts, usageErrorf("compose", "--from %q is not a release tag such as 0.2.0-alpha.15", opts.from)
	}
	opts.to = strings.TrimPrefix(opts.to, "v")
	if strings.ContainsAny(opts.to, ":/@ ") {
		return opts, usageErrorf("compose", "--to %q is an image tag of %s, such as 0.2.0-alpha.16", opts.to, upgradeFixtureImage)
	}
	return opts, nil
}

// releaseVersion is a release tag such as 0.2.0-alpha.15, ordered the way the
// release line is: a prerelease sorts before its release, and prerelease
// labels compare alphabetically (alpha < beta < rc) before their numbers.
type releaseVersion struct {
	core  [3]int
	label string
	pre   int
}

func parseReleaseVersion(tag string) (releaseVersion, error) {
	tag = strings.TrimPrefix(tag, "v")
	if !upgradeFixtureTag.MatchString(tag) {
		return releaseVersion{}, fmt.Errorf("%q is not a release tag such as 0.2.0-alpha.15", tag)
	}
	var v releaseVersion
	core, pre, _ := strings.Cut(tag, "-")
	for i, part := range strings.Split(core, ".") {
		v.core[i], _ = strconv.Atoi(part)
	}
	if pre != "" {
		label, n, _ := strings.Cut(pre, ".")
		v.label = label
		v.pre, _ = strconv.Atoi(n)
	}
	return v, nil
}

func (v releaseVersion) less(o releaseVersion) bool {
	if v.core != o.core {
		for i := range v.core {
			if v.core[i] != o.core[i] {
				return v.core[i] < o.core[i]
			}
		}
	}
	switch {
	case v.label == o.label:
		return v.pre < o.pre
	case v.label == "":
		return false
	case o.label == "":
		return true
	default:
		return v.label < o.label
	}
}

// rollbackExpectation is what the source binary should do when started again
// against the database the candidate upgraded.
type rollbackExpectation int

const (
	// expectStart: the candidate recorded no migration the source lacks, so
	// there is nothing for the source to refuse.
	expectStart rollbackExpectation = iota
	// expectRefusal: the candidate recorded migrations the source does not
	// know, and the source has the refusal.
	expectRefusal
	// expectUnguarded: the candidate recorded migrations the source does not
	// know, and the source predates the refusal. Whatever it does is recorded,
	// not judged; this is the hazard the refusal closes.
	expectUnguarded
)

// expectRollback derives the expectation from the two migration ledgers and
// the source's version, and returns the migrations the source does not know.
func expectRollback(sourceTag string, source, candidate []string) (rollbackExpectation, []string, error) {
	known := make(map[string]bool, len(source))
	for _, id := range source {
		known[id] = true
	}
	var unknown []string
	for _, id := range candidate {
		if !known[id] {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) == 0 {
		return expectStart, nil, nil
	}
	version, err := parseReleaseVersion(sourceTag)
	if err != nil {
		return 0, nil, err
	}
	since, err := parseReleaseVersion(newerSchemaRefusalSince)
	if err != nil {
		return 0, nil, err
	}
	if version.less(since) {
		return expectUnguarded, unknown, nil
	}
	return expectRefusal, unknown, nil
}

// judgeRollback compares what the source did with what it should have done.
func judgeRollback(expect rollbackExpectation, unknown []string, refused bool) (string, error) {
	did := "started and served /healthz"
	if refused {
		did = "refused to start with the newer-schema error"
	}
	switch expect {
	case expectRefusal:
		if !refused {
			return "", fmt.Errorf("the source started against a database recording %s, which it does not know; it must refuse", strings.Join(unknown, ", "))
		}
		return fmt.Sprintf("%s (unknown migrations: %s)", did, strings.Join(unknown, ", ")), nil
	case expectStart:
		if refused {
			return "", errors.New("the source refused a database whose ledger records no migration it does not know")
		}
		return did + "; the candidate recorded no migration the source lacks, so there was nothing to refuse", nil
	default:
		return fmt.Sprintf("%s; not judged: the source predates the refusal (first in %s) and the candidate recorded %s, which it does not know",
			did, newerSchemaRefusalSince, strings.Join(unknown, ", ")), nil
	}
}

// drillStep is one row of the record.
type drillStep struct {
	name   string
	took   time.Duration
	detail string
	err    error
}

type drillRecord struct {
	source    string
	candidate string
	project   string
	started   time.Time
	steps     []drillStep
}

// markdown renders the record as the job summary shows it. A step's error is
// its detail, so a failed run says where and why in the same table.
func (r drillRecord) markdown() string {
	result := "passed"
	for _, s := range r.steps {
		if s.err != nil {
			result = "FAILED at " + s.name
			break
		}
	}
	cell := func(s string) string {
		s = strings.ReplaceAll(s, "|", "/")
		return strings.Join(strings.Fields(s), " ")
	}
	var b strings.Builder
	b.WriteString("## Compose upgrade drill\n\n")
	b.WriteString("| | |\n|---|---|\n")
	fmt.Fprintf(&b, "| Source | %s |\n", cell(r.source))
	fmt.Fprintf(&b, "| Candidate | %s |\n", cell(r.candidate))
	fmt.Fprintf(&b, "| Compose project | %s |\n", cell(r.project))
	fmt.Fprintf(&b, "| Started | %s |\n", r.started.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "| Result | %s |\n\n", result)
	b.WriteString("| Step | Outcome | Time | Detail |\n|---|---|---|---|\n")
	for _, s := range r.steps {
		outcome, detail := "passed", s.detail
		if s.err != nil {
			outcome, detail = "FAILED", s.err.Error()
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", cell(s.name), outcome, s.took.Round(time.Second), cell(detail))
	}
	return b.String()
}

type upgradeDrill struct {
	opts           upgradeDrillOptions
	sourceImage    string
	candidateImage string
	// builtCandidate says the drill tagged the candidate itself, so teardown
	// removes the tag.
	builtCandidate bool
	backupDir      string
	client         *http.Client
	record         drillRecord

	manifest     upgradeFixtureManifest
	sourceLedger []string
	// newTaskID is the task the candidate ran. The restored database predates
	// it, so the recovered source must not know it.
	newTaskID string
}

func cmdComposeUpgradeDrill(args []string) (err error) {
	opts, err := parseUpgradeDrillArgs(args)
	if err != nil {
		return err
	}
	if err := requireCommands("docker"); err != nil {
		return err
	}
	if opts.from == "" {
		// The release deployments upgrade from is the newest tag behind this
		// checkout, the same one release-prepare computes the next version from.
		tag, tagErr := capture("git", "describe", "--tags", "--match", "v[0-9]*", "--abbrev=0")
		if tagErr != nil || tag == "" {
			return errors.New("no release tag is reachable from HEAD; name the source with --from")
		}
		opts.from = strings.TrimPrefix(tag, "v")
		if !upgradeFixtureTag.MatchString(opts.from) {
			return fmt.Errorf("the newest tag %s is not a release tag; name the source with --from", tag)
		}
	}
	if err := ensureComposeEnv(); err != nil {
		return err
	}
	if err := ephemeralComposeEnv("buildmax-upgrade"); err != nil {
		return err
	}
	backupDir, err := os.MkdirTemp("", "buildmax-upgrade-drill-")
	if err != nil {
		return err
	}
	d := &upgradeDrill{
		opts:        opts,
		sourceImage: upgradeFixtureImage + ":" + opts.from,
		backupDir:   backupDir,
		client:      &http.Client{Timeout: 30 * time.Second},
		record:      drillRecord{project: composeProjectName(), started: time.Now()},
	}
	fmt.Printf("Upgrade drill: %s -> %s (Compose project %s)\n", d.sourceImage, d.candidateLabel(), composeProjectName())

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	// Registered before anything starts, so a failure partway still removes
	// the stack, its volumes, and the backup, and still writes the record.
	defer func() {
		if downErr := d.teardown(); downErr != nil && err == nil {
			err = downErr
		}
		if writeErr := d.writeRecord(); writeErr != nil && err == nil {
			err = writeErr
		}
	}()
	return d.run(ctx)
}

func (d *upgradeDrill) candidateLabel() string {
	if d.opts.to != "" {
		return upgradeFixtureImage + ":" + d.opts.to
	}
	return "this checkout (" + resolveCommitSHA() + ")"
}

func (d *upgradeDrill) run(ctx context.Context) error {
	target := composeSmokeTarget(false)
	steps := []struct {
		name string
		do   func() (string, error)
	}{
		{"Prepare images", d.prepareImages},
		{"Start the source", func() (string, error) { return d.start(d.opts.from) }},
		{"Seed through the source's API", func() (string, error) { return d.seed(ctx, target) }},
		{"Back up", d.backUp},
		{"Upgrade to the candidate", func() (string, error) { return d.start(d.candidateTag()) }},
		{"Verify seeded data on the candidate", func() (string, error) { return d.verifySeeded(ctx, target) }},
		{"Run a new task on the candidate", func() (string, error) { return d.runNewTask(ctx, target) }},
		{"Start the source against the upgraded database", func() (string, error) { return d.rollBack(ctx) }},
		{"Restore the backup and start the source", func() (string, error) { return d.restore(ctx, target) }},
	}
	for _, s := range steps {
		fmt.Printf("\n==> %s\n", s.name)
		began := time.Now()
		detail, err := s.do()
		d.record.steps = append(d.record.steps, drillStep{name: s.name, took: time.Since(began), detail: detail, err: err})
		if err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
		fmt.Printf("    %s\n", detail)
	}
	fmt.Println("\nUpgrade drill passed.")
	return nil
}

// candidateTag is the image tag Compose runs as the candidate. A built
// candidate is tagged for this run alone, so it replaces no one's dev image.
func (d *upgradeDrill) candidateTag() string {
	if d.opts.to != "" {
		return d.opts.to
	}
	return strings.TrimPrefix(composeProjectName(), "buildmax-")
}

func (d *upgradeDrill) prepareImages() (string, error) {
	sourceID, err := ensureImage(d.sourceImage)
	if err != nil {
		return "", err
	}
	d.record.source = d.sourceImage + " (" + sourceID + ")"
	// The deterministic model the smoke overlay runs, built from this checkout.
	if err := d.compose(nil, nil, "build", "mock-llm"); err != nil {
		return "", err
	}
	d.candidateImage = upgradeFixtureImage + ":" + d.candidateTag()
	if d.opts.to != "" {
		candidateID, err := ensureImage(d.candidateImage)
		if err != nil {
			return "", err
		}
		d.record.candidate = d.candidateImage + " (" + candidateID + ")"
		return "source and candidate images present", nil
	}
	if err := os.Setenv("BUILDMAX_VERSION", d.candidateTag()); err != nil {
		return "", err
	}
	d.builtCandidate = true
	if err := d.compose(nil, nil, "build", "server"); err != nil {
		return "", err
	}
	id, err := captureErr("docker", "image", "inspect", "-f", "{{.Id}}", d.candidateImage)
	if err != nil {
		return "", err
	}
	d.record.candidate = fmt.Sprintf("%s built from %s (%s)", d.candidateImage, resolveCommitSHA(), id)
	return "source image present, candidate built from this checkout", nil
}

// ensureImage uses a local image when there is one and pulls otherwise, and
// returns what identifies it: the registry digest when it has one.
func ensureImage(ref string) (string, error) {
	if !succeeds("docker", "image", "inspect", ref) {
		// In the open, so a slow registry shows progress instead of a hang.
		if err := runCmd("docker", "pull", ref); err != nil {
			return "", err
		}
	}
	out, err := captureErr("docker", "image", "inspect", "-f", "{{range .RepoDigests}}{{.}} {{end}}{{.Id}}", ref)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(out)
	return fields[0], nil
}

// start runs the server at tag and waits for Compose to call it healthy.
func (d *upgradeDrill) start(tag string) (string, error) {
	// compose.yaml names the server image by BUILDMAX_VERSION, and every
	// docker child, the admin exec included, inherits it from here.
	if err := os.Setenv("BUILDMAX_VERSION", tag); err != nil {
		return "", err
	}
	if err := d.compose(nil, nil, "up", "-d", "--wait", "--no-build", "server"); err != nil {
		return "", err
	}
	ledger, err := d.ledger()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%s healthy; schema_migration: %s", upgradeFixtureImage, tag, describeLedger(ledger)), nil
}

func describeLedger(ids []string) string {
	if len(ids) == 0 {
		return "(empty)"
	}
	return strings.Join(ids, ", ")
}

func (d *upgradeDrill) seed(ctx context.Context, target smokeTarget) (string, error) {
	m, err := seedUpgradeFixture(ctx, d.client, target)
	if err != nil {
		return "", err
	}
	m.SourceImage = d.sourceImage
	d.manifest = m
	return fmt.Sprintf("space %s: agent, issue with comment, published workflow and run, fired and idle schedules, artifact", m.SpaceID), nil
}

// backUp takes the pair an operator restores together — the database and the
// server-data volume, which holds localfs artifacts and run state — with the
// server stopped so neither moves while the other is copied, plus the
// server.yaml they were written under.
func (d *upgradeDrill) backUp() (string, error) {
	ledger, err := d.ledger()
	if err != nil {
		return "", err
	}
	d.sourceLedger = ledger
	if err := d.compose(nil, nil, "stop", "server"); err != nil {
		return "", err
	}
	dumpPath := filepath.Join(d.backupDir, "buildmax.sql")
	if err := d.mysqlToFile(dumpPath, "mysqldump -ubuildmax --single-transaction --no-tablespaces --hex-blob buildmax"); err != nil {
		return "", err
	}
	volumePath := filepath.Join(d.backupDir, "server-data.tar")
	out, err := os.Create(volumePath)
	if err != nil {
		return "", err
	}
	err = d.volume(nil, out, "tar -cf - -C /data .")
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	config, err := os.ReadFile(upgradeDrillServerConfig)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(d.backupDir, "server.yaml"), config, 0o600); err != nil {
		return "", err
	}
	return fmt.Sprintf("server stopped; mysqldump %s, server-data volume %s, server.yaml %s",
		fileSize(dumpPath), fileSize(volumePath), shortSum(config)), nil
}

func fileSize(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "(unreadable)"
	}
	return strconv.FormatInt(info.Size(), 10) + " bytes"
}

func shortSum(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])[:12]
}

// verifySeeded reads every manifest entity back through the running server's
// API and checks the fields the upgrade migrations touch.
func (d *upgradeDrill) verifySeeded(ctx context.Context, target smokeTarget) (string, error) {
	if err := verifyUpgradeFixture(ctx, d.client, target, d.manifest); err != nil {
		return "", err
	}
	return "owner signs in; space, agent, issue owner/executor and comment, published workflow and its run, fired schedule and its task, idle schedule, artifact checksum", nil
}

func (d *upgradeDrill) runNewTask(ctx context.Context, target smokeTarget) (string, error) {
	token, _, err := upgradeFixtureLogin(ctx, d.client, target)
	if err != nil {
		return "", err
	}
	spaceURL := target.apiBase + "/api/spaces/" + url.PathEscape(d.manifest.SpaceID)
	var conversation struct {
		ID string `json:"conversation_id"`
	}
	if err := requestJSON(ctx, d.client, http.MethodPost, spaceURL+"/conversations", token,
		map[string]string{"channel": "portal"}, &conversation, http.StatusCreated); err != nil {
		return "", err
	}
	var task struct {
		ID string `json:"id"`
	}
	if err := requestJSON(ctx, d.client, http.MethodPost, spaceURL+"/conversations/"+url.PathEscape(conversation.ID)+"/tasks", token,
		map[string]string{"input": "Reply with exactly deployment smoke ok."}, &task, http.StatusCreated); err != nil {
		return "", err
	}
	output, err := waitForTaskSuccess(ctx, d.client, spaceURL+"/tasks/"+url.PathEscape(task.ID), token)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(output) != smokeReply {
		return "", fmt.Errorf("task output = %q, want %q", output, smokeReply)
	}
	d.newTaskID = task.ID
	return fmt.Sprintf("task %s in the seeded space SUCCEEDED with %q", task.ID, smokeReply), nil
}

// rollBack starts the source image against what the candidate left and
// judges what it did against what the ledgers say it should do.
func (d *upgradeDrill) rollBack(ctx context.Context) (string, error) {
	candidateLedger, err := d.ledger()
	if err != nil {
		return "", err
	}
	expect, unknown, err := expectRollback(d.opts.from, d.sourceLedger, candidateLedger)
	if err != nil {
		return "", err
	}
	if err := d.compose(nil, nil, "stop", "server"); err != nil {
		return "", err
	}
	if err := os.Setenv("BUILDMAX_VERSION", d.opts.from); err != nil {
		return "", err
	}
	// No --wait: a refusing server never turns healthy, and restart:
	// unless-stopped keeps retrying it.
	if err := d.compose(nil, nil, "up", "-d", "--no-build", "server"); err != nil {
		return "", err
	}
	var refused bool
	err = waitFor(ctx, 2*time.Minute, "the source to start or refuse", func() error {
		var logs bytes.Buffer
		if err := d.composeQuiet(nil, &logs, "logs", "--no-log-prefix", "server"); err != nil {
			return err
		}
		if strings.Contains(logs.String(), newerSchemaRefusal) {
			refused = true
			return nil
		}
		return expectStatusWithToken(ctx, d.client, composeServerURL()+"/healthz", "", http.StatusOK)
	})
	if err != nil {
		return "", err
	}
	if stopErr := d.compose(nil, nil, "stop", "server"); stopErr != nil {
		return "", stopErr
	}
	return judgeRollback(expect, unknown, refused)
}

// restore is the documented recovery: put back the database and volume taken
// before the upgrade, then run the binary that matches them.
func (d *upgradeDrill) restore(ctx context.Context, target smokeTarget) (string, error) {
	began := time.Now()
	if err := d.compose(nil, nil, "stop", "server"); err != nil {
		return "", err
	}
	if err := d.mysql(nil, nil, `mysql -ubuildmax -e "DROP DATABASE buildmax; CREATE DATABASE buildmax"`); err != nil {
		return "", err
	}
	dump, err := os.Open(filepath.Join(d.backupDir, "buildmax.sql"))
	if err != nil {
		return "", err
	}
	err = d.mysql(dump, nil, "mysql -ubuildmax buildmax")
	dump.Close()
	if err != nil {
		return "", err
	}
	archive, err := os.Open(filepath.Join(d.backupDir, "server-data.tar"))
	if err != nil {
		return "", err
	}
	err = d.volume(archive, nil, "find /data -mindepth 1 -delete && tar -xf - -C /data")
	archive.Close()
	if err != nil {
		return "", err
	}
	backedUp, err := os.ReadFile(filepath.Join(d.backupDir, "server.yaml"))
	if err != nil {
		return "", err
	}
	live, err := os.ReadFile(upgradeDrillServerConfig)
	if err != nil {
		return "", err
	}
	if !bytes.Equal(backedUp, live) {
		return "", fmt.Errorf("%s changed since the backup; restore it before starting the source", upgradeDrillServerConfig)
	}
	if _, err := d.start(d.opts.from); err != nil {
		return "", err
	}
	if err := verifyUpgradeFixture(ctx, d.client, target, d.manifest); err != nil {
		return "", err
	}
	// The candidate's task postdates the backup, so its absence is what shows
	// the database was taken back rather than left as the candidate had it.
	token, _, err := upgradeFixtureLogin(ctx, d.client, target)
	if err != nil {
		return "", err
	}
	taskURL := target.apiBase + "/api/spaces/" + url.PathEscape(d.manifest.SpaceID) + "/tasks/" + url.PathEscape(d.newTaskID)
	if err := expectStatusWithToken(ctx, d.client, taskURL, token, http.StatusNotFound); err != nil {
		return "", fmt.Errorf("the candidate's task survived the restore: %w", err)
	}
	return fmt.Sprintf("database and server-data volume restored; the source serves every seeded entity and not the candidate's task %s; recovery took %s",
		d.newTaskID, time.Since(began).Round(time.Second)), nil
}

// ledger reads the schema_migration IDs the database records.
func (d *upgradeDrill) ledger() ([]string, error) {
	var out bytes.Buffer
	if err := d.mysql(nil, &out, `mysql -ubuildmax -N -B -e "SELECT id FROM schema_migration ORDER BY id" buildmax`); err != nil {
		return nil, err
	}
	return strings.Fields(out.String()), nil
}

// mysql runs a MySQL client command in the stack's MySQL container, as the
// application user whose password that container already holds.
func (d *upgradeDrill) mysql(stdin io.Reader, stdout io.Writer, command string) error {
	return d.composeQuiet(stdin, stdout, "exec", "-T", "mysql", "sh", "-c", `MYSQL_PWD="$MYSQL_PASSWORD" `+command)
}

func (d *upgradeDrill) mysqlToFile(path, command string) error {
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	err = d.mysql(nil, out, command)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	return err
}

// volume runs a shell command in a throwaway container with the stack's
// server-data volume at /data. The source image is the helper because the
// drill already holds it and it has a shell and tar.
func (d *upgradeDrill) volume(stdin io.Reader, stdout io.Writer, command string) error {
	args := []string{"run", "--rm", "-v", composeProjectName() + "_server-data:/data", "--entrypoint", "sh"}
	if stdin != nil {
		args = append(args, "-i")
	}
	args = append(args, d.sourceImage, "-c", command)
	return runDocker(stdin, stdout, args...)
}

// compose runs a Compose command against the drill's project with its output
// streamed, and composeQuiet with stdout captured by the caller.
func (d *upgradeDrill) compose(stdin io.Reader, stdout io.Writer, args ...string) error {
	if stdout == nil {
		stdout = os.Stdout
	}
	return runDocker(stdin, stdout, append(composeSmokeArgs(false), args...)...)
}

func (d *upgradeDrill) composeQuiet(stdin io.Reader, stdout io.Writer, args ...string) error {
	if stdout == nil {
		stdout = io.Discard
	}
	return runDocker(stdin, stdout, append(composeSmokeArgs(false), args...)...)
}

func runDocker(stdin io.Reader, stdout io.Writer, args ...string) error {
	var stderr bytes.Buffer
	cmd := exec.Command("docker", args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w: %s", commandLine("docker", args), err, lastLine(stderr.String()))
	}
	return nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

func (d *upgradeDrill) teardown() error {
	fmt.Println("\n==> Tear down")
	var errs []error
	if err := d.compose(nil, nil, "down", "-v", "--remove-orphans"); err != nil {
		errs = append(errs, err)
	}
	if d.builtCandidate {
		if err := runDocker(nil, io.Discard, "image", "rm", d.candidateImage); err != nil {
			errs = append(errs, err)
		}
	}
	if err := os.RemoveAll(d.backupDir); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (d *upgradeDrill) writeRecord() error {
	if d.record.candidate == "" {
		d.record.candidate = d.candidateLabel()
	}
	if d.record.source == "" {
		d.record.source = d.sourceImage
	}
	if err := os.MkdirAll(filepath.Dir(upgradeDrillRecord), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(upgradeDrillRecord, []byte(d.record.markdown()), 0o644); err != nil {
		return err
	}
	fmt.Printf("Recorded the result in %s\n", upgradeDrillRecord)
	return nil
}

// verifyUpgradeFixture reads each entity seedUpgradeFixture created back
// through a server's API.
func verifyUpgradeFixture(ctx context.Context, client *http.Client, target smokeTarget, m upgradeFixtureManifest) error {
	token, _, err := upgradeFixtureLogin(ctx, client, target)
	if err != nil {
		return err
	}
	get := func(path string, out any) error {
		return requestJSON(ctx, client, http.MethodGet, target.apiBase+path, token, nil, out, http.StatusOK)
	}
	var spaces []struct {
		ID string `json:"id"`
	}
	if err := get("/api/spaces", &spaces); err != nil {
		return err
	}
	found := false
	for _, s := range spaces {
		found = found || s.ID == m.SpaceID
	}
	if !found {
		return fmt.Errorf("the owner no longer belongs to space %s", m.SpaceID)
	}
	base := "/api/spaces/" + url.PathEscape(m.SpaceID)

	var agent struct {
		ID string `json:"id"`
	}
	if err := get(base+"/agents/"+url.PathEscape(m.AgentID), &agent); err != nil {
		return err
	}
	var issue struct {
		OwnerID      string `json:"owner_id"`
		ExecutorKind string `json:"executor_kind"`
		ExecutorID   string `json:"executor_id"`
	}
	if err := get(base+"/issues/"+url.PathEscape(m.IssueID), &issue); err != nil {
		return err
	}
	if issue.OwnerID != m.OwnerID || issue.ExecutorKind != "agent" || issue.ExecutorID != m.AgentID {
		return fmt.Errorf("issue %s has owner %q and executor %s %q, want owner %q and agent %q",
			m.IssueID, issue.OwnerID, issue.ExecutorKind, issue.ExecutorID, m.OwnerID, m.AgentID)
	}
	var comments struct {
		Comments []struct {
			Body string `json:"body"`
		} `json:"comments"`
	}
	if err := get(base+"/issues/"+url.PathEscape(m.IssueID)+"/comments", &comments); err != nil {
		return err
	}
	if len(comments.Comments) != 1 || comments.Comments[0].Body != upgradeFixtureComment {
		return fmt.Errorf("issue %s has comments %+v, want the one seeded", m.IssueID, comments.Comments)
	}
	var workflow struct {
		Status string `json:"status"`
	}
	if err := get(base+"/workflows/"+url.PathEscape(m.WorkflowID), &workflow); err != nil {
		return err
	}
	if workflow.Status != "published" {
		return fmt.Errorf("workflow %s is %q, want published", m.WorkflowID, workflow.Status)
	}
	var run struct {
		Run struct {
			WorkflowID string `json:"workflow_id"`
		} `json:"run"`
	}
	if err := get(base+"/workflow-runs/"+url.PathEscape(m.WorkflowRunID), &run); err != nil {
		return err
	}
	if run.Run.WorkflowID != m.WorkflowID {
		return fmt.Errorf("workflow run %s belongs to %q, want %s", m.WorkflowRunID, run.Run.WorkflowID, m.WorkflowID)
	}
	var fired, idle fixtureSchedule
	if err := get(base+"/schedules/"+url.PathEscape(m.FiredScheduleID), &fired); err != nil {
		return err
	}
	if fired.Enabled || fired.lastFire() != m.FiredTaskID || fired.agent() != m.AgentID {
		return fmt.Errorf("fired schedule %s reads %+v, want disabled, agent %s, last fire %s", m.FiredScheduleID, fired, m.AgentID, m.FiredTaskID)
	}
	if err := get(base+"/schedules/"+url.PathEscape(m.IdleScheduleID), &idle); err != nil {
		return err
	}
	if !idle.Enabled || idle.lastFire() != "" || idle.agent() != m.AgentID {
		return fmt.Errorf("idle schedule %s reads %+v, want enabled, agent %s, never fired", m.IdleScheduleID, idle, m.AgentID)
	}
	if err := get(base+"/tasks/"+url.PathEscape(m.FiredTaskID), nil); err != nil {
		return err
	}
	content, err := requestText(ctx, client, http.MethodGet, target.apiBase+"/api/artifacts/"+url.PathEscape(m.ArtifactID)+"/content", token, nil, http.StatusOK)
	if err != nil {
		return err
	}
	if sum := sha256.Sum256([]byte(content)); hex.EncodeToString(sum[:]) != m.ArtifactSHA256 {
		return fmt.Errorf("artifact %s content changed: sha256 %x, want %s", m.ArtifactID, sum, m.ArtifactSHA256)
	}
	return nil
}

// fixtureSchedule reads a schedule in either API shape. Releases before
// schedule_agent_to_executor name the agent and the last task directly, and
// the drill reads the source's API after the restore as well as the
// candidate's.
type fixtureSchedule struct {
	Enabled      bool   `json:"enabled"`
	ExecutorKind string `json:"executor_kind"`
	ExecutorID   string `json:"executor_id"`
	LastFireRef  string `json:"last_fire_ref"`
	AgentID      string `json:"agent_id"`
	LastTaskID   string `json:"last_task_id"`
}

func (s fixtureSchedule) agent() string {
	switch s.ExecutorKind {
	case "":
		return s.AgentID
	case "agent":
		return s.ExecutorID
	default:
		return ""
	}
}

func (s fixtureSchedule) lastFire() string {
	if s.LastFireRef != "" {
		return s.LastFireRef
	}
	return s.LastTaskID
}
