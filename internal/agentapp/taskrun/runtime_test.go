package taskrun

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/agentapp"
	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/core/apierr"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	blob "github.com/icloudbb/buildmax/internal/infra/objectstore"
	"github.com/icloudbb/buildmax/internal/testsupport/mockllm"
)

func TestRunProvenance_CarriesTheRunsTrigger(t *testing.T) {
	retryOf := "tr_previous"
	run := &coretask.Run{
		ID:               "tr_current",
		CreatedBy:        "u_1",
		CreatedByType:    coretask.RunCreatedByTypeUser,
		TriggerSource:    coretask.RunTriggerSourceIssueAgentRun,
		RetryOfTaskRunID: &retryOf,
	}
	got := runProvenance(run)
	want := agentapp.RunProvenance{
		CreatedBy:        "u_1",
		CreatedByType:    coretask.RunCreatedByTypeUser,
		TriggerSource:    coretask.RunTriggerSourceIssueAgentRun,
		RetryOfTaskRunID: "tr_previous",
	}
	if got != want {
		t.Errorf("runProvenance(run) = %+v, want %+v", got, want)
	}
}

// A first run repeats nothing, and RetryOfTaskRunID is nil for it -- the
// provenance passed to the trace has to say "" rather than dereference a nil
// pointer or fabricate an id.
func TestRunProvenance_LeavesRetryOfEmptyForAFirstRun(t *testing.T) {
	run := &coretask.Run{ID: "tr_1", CreatedBy: "u_1", CreatedByType: coretask.RunCreatedByTypeUser, TriggerSource: coretask.RunTriggerSourceTaskCreate}
	got := runProvenance(run)
	if got.RetryOfTaskRunID != "" {
		t.Errorf("RetryOfTaskRunID = %q, want empty for a run that repeats nothing", got.RetryOfTaskRunID)
	}
}

// fakePersistStorage is an in-memory PersistStorage for tests.
type fakePersistStorage struct {
	files      map[string]map[string][]byte // workspaceID -> relPath -> content (persist)
	taskGlobal map[string][]byte            // "workspaceID/taskID/taskRunID/relPath" -> content
}

func newFakePersistStorage() *fakePersistStorage {
	return &fakePersistStorage{
		files:      make(map[string]map[string][]byte),
		taskGlobal: make(map[string][]byte),
	}
}

func (f *fakePersistStorage) Put(ctx context.Context, workspaceID, relPath string, r io.Reader) error {
	if f.files[workspaceID] == nil {
		f.files[workspaceID] = make(map[string][]byte)
	}
	data, _ := io.ReadAll(r)
	f.files[workspaceID][relPath] = data
	return nil
}

func (f *fakePersistStorage) Get(ctx context.Context, workspaceID, relPath string) ([]byte, error) {
	if f.files[workspaceID] == nil {
		return nil, os.ErrNotExist
	}
	data, ok := f.files[workspaceID][relPath]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}

func (f *fakePersistStorage) ListFiles(ctx context.Context, workspaceID string) ([]string, error) {
	if f.files[workspaceID] == nil {
		return nil, nil
	}
	var out []string
	for k := range f.files[workspaceID] {
		out = append(out, k)
	}
	return out, nil
}

func (f *fakePersistStorage) MaterializeToDir(ctx context.Context, workspaceID, dstDir string) error {
	if f.files[workspaceID] == nil {
		return nil
	}
	for relPath, data := range f.files[workspaceID] {
		full := filepath.Join(dstDir, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(full, data, 0644); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakePersistStorage) PutRunGlobal(ctx context.Context, ref blob.RunObjectRef, r io.Reader) error {
	key := ref.SpaceID + "/" + ref.TaskID + "/" + ref.TaskRunID + "/" + ref.RelPath
	data, _ := io.ReadAll(r)
	f.taskGlobal[key] = data
	return nil
}

// taskGlobalRelPaths returns the set of relPaths uploaded for one task run.
func (f *fakePersistStorage) taskGlobalRelPaths(spaceID, taskID, taskRunID string) []string {
	prefix := spaceID + "/" + taskID + "/" + taskRunID + "/"
	var out []string
	for k := range f.taskGlobal {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			out = append(out, k[len(prefix):])
		}
	}
	return out
}

func (f *fakePersistStorage) GetRunGlobal(ctx context.Context, ref blob.RunObjectRef) ([]byte, error) {
	key := ref.SpaceID + "/" + ref.TaskID + "/" + ref.TaskRunID + "/" + ref.RelPath
	data, ok := f.taskGlobal[key]
	if !ok {
		return nil, apierr.ErrNotFound
	}
	return data, nil
}

func (f *fakePersistStorage) DeleteRunGlobal(ctx context.Context, ref blob.RunObjectRef) error {
	delete(f.taskGlobal, ref.SpaceID+"/"+ref.TaskID+"/"+ref.TaskRunID+"/"+ref.RelPath)
	return nil
}

func (f *fakePersistStorage) PutRunArtifacts(ctx context.Context, ref blob.RunObjectRef, r io.Reader) error {
	key := ref.SpaceID + "/" + ref.TaskID + "/" + ref.TaskRunID + "/artifacts/" + ref.RelPath
	data, _ := io.ReadAll(r)
	if f.taskGlobal == nil {
		f.taskGlobal = make(map[string][]byte)
	}
	f.taskGlobal[key] = data
	return nil
}

func (f *fakePersistStorage) GetRunArtifacts(ctx context.Context, ref blob.RunObjectRef) ([]byte, error) {
	key := ref.SpaceID + "/" + ref.TaskID + "/" + ref.TaskRunID + "/artifacts/" + ref.RelPath
	data, ok := f.taskGlobal[key]
	if !ok {
		return nil, apierr.ErrNotFound
	}
	return data, nil
}

func TestFakePersistStorage_MaterializeToDir(t *testing.T) {
	ctx := context.Background()
	f := newFakePersistStorage()
	ws := "ws1"
	if err := f.Put(ctx, ws, "a.txt", bytes.NewReader([]byte("hello"))); err != nil {
		t.Fatal(err)
	}
	if err := f.Put(ctx, ws, "sub/b.txt", bytes.NewReader([]byte("world"))); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if err := f.MaterializeToDir(ctx, ws, dst); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dst, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Errorf("got %q", data)
	}
	data, err = os.ReadFile(filepath.Join(dst, "sub", "b.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "world" {
		t.Errorf("got %q", data)
	}
}

func TestUploadTaskGlobal_UploadsPresentFiles(t *testing.T) {
	ctx := context.Background()
	globalDir := t.TempDir()
	// Create a subset of global dir files (no log; sessions dir with two files)
	if err := os.WriteFile(filepath.Join(globalDir, "settings.yaml"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	// A session is a directory now, with its traces nested a further level
	// down, so a flat read of sessions/ would upload none of it.
	bundle := filepath.Join(globalDir, "sessions", "sid-1")
	if err := os.MkdirAll(filepath.Join(bundle, "traces"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "sessions", "index.json"), []byte("[]"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "meta.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "history.jsonl"), []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "traces", "run1.jsonl"), []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	fake := newFakePersistStorage()
	if _, err := uploadTaskGlobal(ctx, globalDir, RunScope{SpaceID: "tm1", TaskID: "task1", TaskRunID: "run1"}, fake, ""); err != nil {
		t.Fatal(err)
	}

	got := fake.taskGlobalRelPaths("tm1", "task1", "run1")
	if len(got) != 5 {
		t.Fatalf("want 5 uploaded relPaths, got %d: %v", len(got), got)
	}
	wantSet := map[string]bool{
		"settings.yaml":                    true,
		"sessions/index.json":              true,
		"sessions/sid-1/meta.json":         true,
		"sessions/sid-1/history.jsonl":     true,
		"sessions/sid-1/traces/run1.jsonl": true,
	}
	for _, p := range got {
		if !wantSet[p] {
			t.Errorf("unexpected relPath %q", p)
		}
	}
	// Content sanity
	key := "tm1/task1/run1/settings.yaml"
	if string(fake.taskGlobal[key]) != "{}" {
		t.Errorf("settings.yaml content mismatch")
	}
}

func TestUploadTaskGlobal_SkipsMissingFiles(t *testing.T) {
	ctx := context.Background()
	globalDir := t.TempDir()
	// Empty global dir: no files created
	fake := newFakePersistStorage()
	if _, err := uploadTaskGlobal(ctx, globalDir, RunScope{SpaceID: "tm1", TaskID: "task1", TaskRunID: "run1"}, fake, ""); err != nil {
		t.Fatal(err)
	}
	got := fake.taskGlobalRelPaths("tm1", "task1", "run1")
	if len(got) != 0 {
		t.Errorf("want 0 uploads for empty dir, got %v", got)
	}
}

func TestPrepareRunWorkspace_MaterializesSpaceFiles(t *testing.T) {
	ctx := context.Background()
	persist := newFakePersistStorage()
	if err := persist.Put(ctx, "tm_shared", "shared.txt", bytes.NewReader([]byte("space"))); err != nil {
		t.Fatal(err)
	}
	if err := persist.Put(ctx, "u_creator", "private.txt", bytes.NewReader([]byte("user"))); err != nil {
		t.Fatal(err)
	}

	dirs := runDirs{
		runDir:       t.TempDir(),
		runWorkspace: filepath.Join(t.TempDir(), "workspace"),
		runGlobal:    filepath.Join(t.TempDir(), "global"),
		runOSHome:    filepath.Join(t.TempDir(), "oshome"),
	}
	task := &coretask.Task{
		ID:             "t1",
		ConversationID: "c1",
		SpaceID:        "tm_shared",
		CreatedBy:      "u_creator",
	}
	run := &coretask.Run{ID: "r1"}

	if err := prepareRunWorkspace(ctx, RunTaskInput{Persist: persist}, task, run, dirs); err != nil {
		t.Fatal(err)
	}

	spaceData, err := os.ReadFile(filepath.Join(dirs.runWorkspace, "shared.txt"))
	if err != nil {
		t.Fatalf("read shared file: %v", err)
	}
	if string(spaceData) != "space" {
		t.Fatalf("shared file = %q, want %q", spaceData, "space")
	}
	if _, err := os.Stat(filepath.Join(dirs.runWorkspace, "private.txt")); !os.IsNotExist(err) {
		t.Fatalf("private creator file should not be materialized, stat err = %v", err)
	}

	// The run's OS HOME exists and is empty: it must not inherit space files
	// (those go to runWorkspace) or anything from a previous run.
	entries, err := os.ReadDir(dirs.runOSHome)
	if err != nil {
		t.Fatalf("read run OS home: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("run OS home should start empty, has %d entries", len(entries))
	}
}

// TestResolveRunDirs_OSHomeIsRunPrivate pins that the OS HOME is a dedicated
// per-run directory, distinct from the space home and BUILDMAX_HOME, so one
// run's tool state cannot leak into another and rendered credential files do
// not land among uploaded files.
func TestResolveRunDirs_OSHomeIsRunPrivate(t *testing.T) {
	paths := NewRuntimePathsFromRoot(t.TempDir())
	task := &coretask.Task{ID: "t1", ConversationID: "c1", CreatedBy: "u1"}
	run := &coretask.Run{ID: "r1"}
	dirs := resolveRunDirs(paths, task, run)

	if dirs.runOSHome == dirs.runWorkspace || dirs.runOSHome == dirs.runGlobal || dirs.runOSHome == dirs.runDir {
		t.Fatalf("OS home %q must differ from workspace/global/run dirs", dirs.runOSHome)
	}
	if filepath.Dir(dirs.runOSHome) != dirs.runDir {
		t.Fatalf("OS home %q should live under the run dir %q", dirs.runOSHome, dirs.runDir)
	}

	// Two different runs get different OS homes.
	other := resolveRunDirs(paths, task, &coretask.Run{ID: "r2"})
	if other.runOSHome == dirs.runOSHome {
		t.Fatalf("distinct runs share an OS home: %q", dirs.runOSHome)
	}
}

// TestTraceRelPath_MatchesUploadedKey pins the invariant the stored path exists
// for: what a run records must be exactly the key its trace was uploaded under.
// The two are computed in different functions, so this test couples them —
// a stored path that does not resolve would report every trace as missing.
func TestTraceRelPath_MatchesUploadedKey(t *testing.T) {
	ctx := context.Background()
	globalDir := t.TempDir()
	traceDir := filepath.Join(globalDir, "traces", "c_sess1")
	if err := os.MkdirAll(traceDir, 0755); err != nil {
		t.Fatal(err)
	}
	tracePath := filepath.Join(traceDir, "rt_abc123.jsonl")
	if err := os.WriteFile(tracePath, []byte(`{"type":"run_start"}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	recorded := traceRelPath(globalDir, tracePath)
	if recorded != "traces/c_sess1/rt_abc123.jsonl" {
		t.Fatalf("traceRelPath = %q, want traces/c_sess1/rt_abc123.jsonl", recorded)
	}

	fake := newFakePersistStorage()
	scope := RunScope{SpaceID: "tm1", TaskID: "task1", TaskRunID: "run1"}
	stored, err := uploadTaskGlobal(ctx, globalDir, scope, fake, recorded)
	if err != nil {
		t.Fatal(err)
	}
	if stored != recorded {
		t.Errorf("stored trace key = %q, want %q", stored, recorded)
	}
	uploaded := fake.taskGlobalRelPaths("tm1", "task1", "run1")
	for _, p := range uploaded {
		if p == recorded {
			return
		}
	}
	t.Errorf("recorded trace path %q is not among the uploaded keys %v", recorded, uploaded)
}

// TestTraceRelPath_RefusesUnresolvablePaths asserts the two cases that must
// record nothing rather than a path a reader cannot fetch.
func TestTraceRelPath_RefusesUnresolvablePaths(t *testing.T) {
	globalDir := t.TempDir()
	if got := traceRelPath(globalDir, ""); got != "" {
		t.Errorf("no trace should record no path, got %q", got)
	}
	outside := filepath.Join(t.TempDir(), "traces", "s", "rt_x.jsonl")
	if got := traceRelPath(globalDir, outside); got != "" {
		t.Errorf("trace outside the uploaded dir should record no path, got %q", got)
	}
}

// TestRestoreSessionFromPreviousRun_RoundTripsTheBundle proves the two halves
// agree: what upload writes for a session is what restore can fetch back.
func TestRestoreSessionFromPreviousRun_RoundTripsTheBundle(t *testing.T) {
	ctx := context.Background()
	fake := newFakePersistStorage()

	prevDir := t.TempDir()
	bundle := filepath.Join(prevDir, "sessions", "sid-1")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "meta.json"), []byte(`{"id":"sid-1"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "history.jsonl"), []byte("{\"type\":\"history\"}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	scope := RunScope{SpaceID: "tm1", TaskID: "task1", TaskRunID: "run1"}
	if _, err := uploadTaskGlobal(ctx, prevDir, scope, fake, ""); err != nil {
		t.Fatal(err)
	}

	nextDir := t.TempDir()
	sessionID, previousRun, currentRun := "sid-1", "run1", "run2"
	restoreSessionFromPreviousRun(ctx,
		&coretask.Task{SpaceID: "tm1", ID: "task1", SessionID: &sessionID, LastRunID: &currentRun},
		&coretask.Run{ID: currentRun, PreviousTaskRunID: &previousRun}, nextDir, fake)

	for _, name := range sessionBundleFiles {
		if _, err := os.Stat(filepath.Join(nextDir, "sessions", "sid-1", name)); err != nil {
			t.Errorf("%s not restored: %v", name, err)
		}
	}
}

// This is the worker-runtime continuity proof: two runs use different local
// directories, the second downloads the first one's object-store bundle, and
// the model receives the earlier exchange. A unit test of the copy alone could
// pass while OpenOrCreateSession still opened a fresh conversation.
func TestContinueRunSendsRestoredHistoryToModel(t *testing.T) {
	ctx := context.Background()
	server, err := mockllm.Start(mockllm.Scenario{Steps: []mockllm.Step{
		{Text: "noted: the code word is albatross"},
		{Text: "the code word was albatross"},
	}})
	if err != nil {
		t.Fatalf("start mock model: %v", err)
	}
	t.Cleanup(server.Close)
	model := config.ModelEntry{
		Model: "mock-model", Name: "mock", APIURL: server.BaseURL(mockllm.ProtocolOpenAIChat),
		APIKey: "mock-key", ContextWindow: 128000,
	}
	persist := newFakePersistStorage()
	sessionID := "sid-continue"
	task := &coretask.Task{ID: "task1", SpaceID: "tm1", SessionID: &sessionID}

	firstRun := &coretask.Run{ID: "run1", Input: "remember the code word: albatross"}
	firstDirs := testRunDirs(t)
	if _, err := runAgentTask(ctx, firstRun, firstDirs.runDir, firstDirs.runGlobal, firstDirs.runOSHome,
		sessionID, nil, model, ManagedInference{}, nil, "", "", nil, nil, "", "", nil, nil); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if _, err := uploadTaskGlobal(ctx, firstDirs.runGlobal, RunScope{SpaceID: task.SpaceID, TaskID: task.ID, TaskRunID: firstRun.ID}, persist, ""); err != nil {
		t.Fatal(err)
	}

	secondRun := &coretask.Run{ID: "run2", PreviousTaskRunID: &firstRun.ID, Input: "what was the code word?"}
	secondDirs := testRunDirs(t)
	restoreSessionFromPreviousRun(ctx, task, secondRun, secondDirs.runGlobal, persist)
	if _, err := runAgentTask(ctx, secondRun, secondDirs.runDir, secondDirs.runGlobal, secondDirs.runOSHome,
		sessionID, nil, model, ManagedInference{}, nil, "", "", nil, nil, "", "", nil, nil); err != nil {
		t.Fatalf("continued run: %v", err)
	}

	calls := server.Requests()
	if len(calls) != 2 {
		t.Fatalf("model calls = %d, want one per run", len(calls))
	}
	secondRequest := string(calls[1].Body)
	for _, want := range []string{firstRun.Input, "noted: the code word is albatross", secondRun.Input} {
		if !strings.Contains(secondRequest, want) {
			t.Errorf("continued request omitted %q:\n%s", want, secondRequest)
		}
	}
}

func testRunDirs(t *testing.T) runDirs {
	t.Helper()
	runDir := t.TempDir()
	dirs := runDirs{
		runDir:       runDir,
		runWorkspace: filepath.Join(runDir, "workspace"),
		runGlobal:    filepath.Join(runDir, "global"),
		runOSHome:    filepath.Join(runDir, "oshome"),
	}
	if err := ensureRunDirs(dirs.runWorkspace, dirs.runGlobal, dirs.runOSHome); err != nil {
		t.Fatalf("prepare run dirs: %v", err)
	}
	return dirs
}

// A bundle missing one of its parts must not be half-restored: a history with
// no metadata would resume under the wrong model rather than fail visibly.
func TestRestoreSessionFromPreviousRun_PartialBundleRestoresNothing(t *testing.T) {
	ctx := context.Background()
	fake := newFakePersistStorage()

	prevDir := t.TempDir()
	bundle := filepath.Join(prevDir, "sessions", "sid-1")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "history.jsonl"), []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	scope := RunScope{SpaceID: "tm1", TaskID: "task1", TaskRunID: "run1"}
	if _, err := uploadTaskGlobal(ctx, prevDir, scope, fake, ""); err != nil {
		t.Fatal(err)
	}

	nextDir := t.TempDir()
	sessionID, previousRun := "sid-1", "run1"
	restoreSessionFromPreviousRun(ctx,
		&coretask.Task{SpaceID: "tm1", ID: "task1", SessionID: &sessionID},
		&coretask.Run{ID: "run2", PreviousTaskRunID: &previousRun}, nextDir, fake)

	if _, err := os.Stat(filepath.Join(nextDir, "sessions", "sid-1")); !os.IsNotExist(err) {
		t.Errorf("a partial bundle was left behind (stat err = %v)", err)
	}
}

// TestWithRunEnv_InjectsGrantsAndRestores proves a run's Secret grants are set
// in the process environment for the duration of the run and cleared after,
// alongside HOME and BUILDMAX_HOME.
func TestWithRunEnv_InjectsGrantsAndRestores(t *testing.T) {
	const name = "GH_TOKEN_TEST_GRANT"
	_ = os.Unsetenv(name)
	grants := map[string]string{name: "ghs_secret"}
	err := withRunEnv(t.TempDir(), t.TempDir(), grants, func() error {
		if got := os.Getenv(name); got != "ghs_secret" {
			t.Fatalf("inside run: %s = %q, want the grant value", name, got)
		}
		if os.Getenv("HOME") == "" {
			t.Fatal("inside run: HOME should be set")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("withRunEnv: %v", err)
	}
	if _, ok := os.LookupEnv(name); ok {
		t.Fatalf("after run: %s should be cleared", name)
	}
}
