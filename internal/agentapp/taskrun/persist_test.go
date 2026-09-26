package taskrun

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	blob "github.com/icloudbb/buildmax/internal/infra/objectstore"
	"github.com/icloudbb/buildmax/internal/testsupport/mockllm"
)

// errWriteDenied stands in for a bucket that refuses the worker's writes.
var errWriteDenied = errors.New("PutObject: StatusCode: 403, Access denied")

// deniedPersistStorage reads like the fake and refuses every run-state write,
// counting the attempts.
type deniedPersistStorage struct {
	*fakePersistStorage
	puts int
}

func (d *deniedPersistStorage) PutRunGlobal(context.Context, blob.RunObjectRef, io.Reader) error {
	d.puts++
	return errWriteDenied
}

func writeRunGlobalFile(t *testing.T, globalDir, relPath, content string) {
	t.Helper()
	full := filepath.Join(globalDir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A refused write must not leave the trace pointer behind, and must not be
// retried file by file: under an outage every upload times out, and the run's
// report would wait for each of them.
func TestUploadTaskGlobal_StopsAtTheFirstRefusedWrite(t *testing.T) {
	globalDir := t.TempDir()
	const traceKey = "traces/c_sess/rt_1.jsonl"
	writeRunGlobalFile(t, globalDir, traceKey, `{"type":"run_start"}`+"\n")
	writeRunGlobalFile(t, globalDir, "sessions/sid/meta.json", "{}")
	writeRunGlobalFile(t, globalDir, "sessions/sid/history.jsonl", "{}\n")
	writeRunGlobalFile(t, globalDir, "settings.yaml", "{}")

	denied := &deniedPersistStorage{fakePersistStorage: newFakePersistStorage()}
	stored, err := uploadTaskGlobal(context.Background(), globalDir, RunScope{SpaceID: "s", TaskID: "t", TaskRunID: "r"}, denied, traceKey)

	if !errors.Is(err, errWriteDenied) {
		t.Fatalf("err = %v, want the storage refusal", err)
	}
	if !strings.Contains(err.Error(), traceKey) {
		t.Errorf("err = %q, want it to name the object it could not store", err)
	}
	if stored != "" {
		t.Errorf("stored trace = %q, want none for a refused write", stored)
	}
	if denied.puts != 1 {
		t.Errorf("upload attempts = %d, want 1: the rest hit the same refusal", denied.puts)
	}
}

// runMockTask runs one whole task through RunTask against a mock model that
// answers reply, with persist as the run-state store.
func runMockTask(t *testing.T, persist blob.PersistStorage, reply string) (*fakeUpdater, error) {
	t.Helper()
	server, err := mockllm.Start(mockllm.Scenario{Steps: []mockllm.Step{{Text: reply}}, Repeat: true})
	if err != nil {
		t.Fatalf("start mock model: %v", err)
	}
	t.Cleanup(server.Close)
	t.Setenv(config.EnvKeyBuildmaxTraceDisabled, "")

	updater := &fakeUpdater{}
	sessionID := "sid-persist"
	err = RunTask(context.Background(), RunTaskInput{
		Task:      &coretask.Task{ID: "task1", SpaceID: "space1", SessionID: &sessionID},
		Run:       &coretask.Run{ID: "run1", Input: "say something"},
		SessionID: sessionID,
		Paths:     NewRuntimePathsFromRoot(t.TempDir()),
		Persist:   persist,
		Updater:   updater,
		Model: config.ModelEntry{
			Model: "mock-model", Name: "mock", APIURL: server.BaseURL(mockllm.ProtocolOpenAIChat),
			APIKey: "mock-key", ContextWindow: 128000,
		},
	})
	return updater, err
}

// A run whose state never reached storage must not report success: the next
// turn would resume from a session bundle that is not there. It fails with a
// cause naming the storage, keeps the reply and usage the database holds, and
// records no trace pointer storage cannot resolve.
func TestRunTask_FailsARunWhoseStateCannotBeStored(t *testing.T) {
	denied := &deniedPersistStorage{fakePersistStorage: newFakePersistStorage()}
	updater, err := runMockTask(t, denied, "all done")

	if !errors.Is(err, errWriteDenied) {
		t.Fatalf("RunTask err = %v, want the storage refusal", err)
	}
	req := updater.req
	if req == nil {
		t.Fatal("the run never reported an outcome")
	}
	if req.Status != string(coretask.RunStatusFailed) {
		t.Errorf("status = %q, want FAILED", req.Status)
	}
	if req.ErrorMessage == nil || !strings.Contains(*req.ErrorMessage, "object storage") || !strings.Contains(*req.ErrorMessage, "403") {
		t.Errorf("error message = %v, want the storage failure named", req.ErrorMessage)
	}
	if req.Output == nil || *req.Output != "all done" {
		t.Errorf("output = %v, want the reply the run produced", req.Output)
	}
	if req.TracePath != nil {
		t.Errorf("trace path = %q, want none: the trace never reached storage", *req.TracePath)
	}
}

// The same run with working storage succeeds and points at a trace that is
// actually stored under the recorded key.
func TestRunTask_RecordsTheTraceItStored(t *testing.T) {
	persist := newFakePersistStorage()
	updater, err := runMockTask(t, persist, "all done")
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	req := updater.req
	if req == nil || req.Status != string(coretask.RunStatusSucceeded) {
		t.Fatalf("report = %+v, want SUCCEEDED", req)
	}
	if req.TracePath == nil || *req.TracePath == "" {
		t.Fatal("a stored trace was not recorded")
	}
	if _, ok := persist.taskGlobal["space1/task1/run1/"+*req.TracePath]; !ok {
		t.Errorf("recorded trace %q is not in storage", *req.TracePath)
	}
}
