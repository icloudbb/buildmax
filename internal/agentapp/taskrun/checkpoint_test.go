package taskrun

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/infra/workerclient"
	"github.com/icloudbb/buildmax/internal/infra/wsarchive"
)

// storedBlob is one payload a fakeCheckpointStore received.
type storedBlob struct {
	spaceID string
	sha     string
	bytes   []byte
}

// fakeCheckpointStore records what a capture uploaded and serves it back by the
// content-addressed key, so a test can prove the reported digest covers the
// stored bytes and that a restore reads the same bytes back.
type fakeCheckpointStore struct {
	puts   []storedBlob
	blobs  map[string][]byte // key -> bytes
	putErr error
}

func fakeCheckpointKey(spaceID, sha256hex string) string {
	return spaceID + "/workspace/blobs/sha256/" + sha256hex
}

func (f *fakeCheckpointStore) Put(_ context.Context, spaceID, sha256hex string, src io.Reader) (string, error) {
	if f.putErr != nil {
		return "", f.putErr
	}
	raw, err := io.ReadAll(src)
	if err != nil {
		return "", err
	}
	f.puts = append(f.puts, storedBlob{spaceID: spaceID, sha: sha256hex, bytes: raw})
	key := fakeCheckpointKey(spaceID, sha256hex)
	if f.blobs == nil {
		f.blobs = map[string][]byte{}
	}
	f.blobs[key] = raw
	return key, nil
}

func (f *fakeCheckpointStore) Key(spaceID, sha256hex string) (string, error) {
	return fakeCheckpointKey(spaceID, sha256hex), nil
}

func (f *fakeCheckpointStore) Open(_ context.Context, storageKey string) (io.ReadCloser, int64, error) {
	raw, ok := f.blobs[storageKey]
	if !ok {
		return nil, 0, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(raw)), int64(len(raw)), nil
}

// putBlob stores raw bytes under the key for (spaceID, sha) so a restore test can
// stage a base payload the store will serve.
func (f *fakeCheckpointStore) putBlob(spaceID, sha string, raw []byte) {
	if f.blobs == nil {
		f.blobs = map[string][]byte{}
	}
	f.blobs[fakeCheckpointKey(spaceID, sha)] = raw
}

// seedRecorder is a worker API stand-in: it answers the base lookup (with a
// configured base, or 204 when there is none) and records the seed-finalize and
// restore-outcome calls a run makes.
type seedRecorder struct {
	base     *workerclient.WorkspaceBaseResponse
	finalize *workerclient.SeedCheckpointRequest
	restore  *workerclient.WorkspaceRestoreRequest
}

func (s *seedRecorder) server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/workspace-base") && r.Method == http.MethodGet:
			if s.base == nil {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			_ = json.NewEncoder(w).Encode(*s.base)
		case strings.HasSuffix(r.URL.Path, "/workspace-checkpoints") && r.Method == http.MethodPost:
			var req workerclient.SeedCheckpointRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			s.finalize = &req
			_ = json.NewEncoder(w).Encode(workerclient.SeedCheckpointResponse{CheckpointID: "wc_seed"})
		case strings.HasSuffix(r.URL.Path, "/workspace-restore") && r.Method == http.MethodPost:
			var req workerclient.WorkspaceRestoreRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			s.restore = &req
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// seedBase archives srcDir with the checkpoint codec, stages the payload in
// store, and returns the base descriptor a run would restore from — the
// server's answer to the base lookup for a continuing run.
func seedBase(t *testing.T, store *fakeCheckpointStore, spaceID, srcDir string) *workerclient.WorkspaceBaseResponse {
	t.Helper()
	var buf bytes.Buffer
	hash := sha256.New()
	res, err := wsarchive.Create(io.MultiWriter(&buf, hash), srcDir, checkpointLimits)
	if err != nil {
		t.Fatalf("build base archive: %v", err)
	}
	sha := hex.EncodeToString(hash.Sum(nil))
	store.putBlob(spaceID, sha, buf.Bytes())
	return &workerclient.WorkspaceBaseResponse{
		CheckpointID:      "wc_base",
		PayloadFormat:     wsarchive.PayloadFormat,
		PayloadSHA256:     sha,
		SizeBytes:         int64(buf.Len()),
		UncompressedBytes: res.UncompressedBytes,
		EntryCount:        res.EntryCount,
	}
}

func writeWorkspace(t *testing.T) string {
	t.Helper()
	ws := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(filepath.Join(ws, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "top.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "sub", "nested.txt"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}
	return ws
}

// TestCaptureAndFinalizeSeed_UploadsBytesThenRecordsMatchingDigest pins the
// commit protocol: the bytes reach the store first, and the digest the finalize
// records is the digest of exactly those bytes.
func TestCaptureAndFinalizeSeed_UploadsBytesThenRecordsMatchingDigest(t *testing.T) {
	ctx := context.Background()
	ws := writeWorkspace(t)
	staging := filepath.Join(t.TempDir(), "staging")

	rec := &seedRecorder{}
	srv := rec.server(t)
	defer srv.Close()
	store := &fakeCheckpointStore{}
	cfg := workerclient.WorkerAPIClientConfig{BaseURL: srv.URL, Token: "t", Client: srv.Client()}

	if err := captureAndFinalizeSeed(ctx, store, cfg, staging, ws, "sp_1", "rt_1"); err != nil {
		t.Fatalf("captureAndFinalizeSeed: %v", err)
	}

	if len(store.puts) != 1 {
		t.Fatalf("expected exactly one payload upload, got %d", len(store.puts))
	}
	put := store.puts[0]
	if put.spaceID != "sp_1" {
		t.Fatalf("payload uploaded for space %q, want sp_1", put.spaceID)
	}
	// The digest the caller passed to Put must be the digest of the bytes Put
	// actually received — the tee, not a value computed from something else.
	sum := sha256.Sum256(put.bytes)
	want := hex.EncodeToString(sum[:])
	if put.sha != want {
		t.Fatalf("upload digest %q does not match its bytes %q", put.sha, want)
	}
	if rec.finalize == nil {
		t.Fatal("seed was never finalized")
	}
	if rec.finalize.PayloadSHA256 != want {
		t.Fatalf("finalize digest %q != uploaded digest %q", rec.finalize.PayloadSHA256, want)
	}
	if rec.finalize.PayloadFormat != wsarchive.PayloadFormat {
		t.Fatalf("finalize format %q, want %q", rec.finalize.PayloadFormat, wsarchive.PayloadFormat)
	}
	if rec.finalize.SizeBytes != int64(len(put.bytes)) {
		t.Fatalf("finalize size %d != uploaded byte count %d", rec.finalize.SizeBytes, len(put.bytes))
	}
	// top.txt, sub/, sub/nested.txt.
	if rec.finalize.EntryCount != 3 {
		t.Fatalf("finalize entry count %d, want 3", rec.finalize.EntryCount)
	}

	// The uploaded archive round-trips to the same tree.
	dest := filepath.Join(t.TempDir(), "restored")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := wsarchive.Extract(bytes.NewReader(put.bytes), dest, checkpointLimits); err != nil {
		t.Fatalf("uploaded archive does not extract: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "sub", "nested.txt"))
	if err != nil || string(got) != "world" {
		t.Fatalf("restored nested file = %q, err %v", got, err)
	}
}

// TestCaptureAndFinalizeSeed_StagesArchiveOutsideWorkspace pins §12.1: the
// archive is never written inside the tree it captures, and its staging is
// cleaned up.
func TestCaptureAndFinalizeSeed_StagesArchiveOutsideWorkspace(t *testing.T) {
	ctx := context.Background()
	ws := writeWorkspace(t)
	staging := filepath.Join(t.TempDir(), "staging")

	rec := &seedRecorder{}
	srv := rec.server(t)
	defer srv.Close()
	cfg := workerclient.WorkerAPIClientConfig{BaseURL: srv.URL, Token: "t", Client: srv.Client()}

	if err := captureAndFinalizeSeed(ctx, &fakeCheckpointStore{}, cfg, staging, ws, "sp_1", "rt_1"); err != nil {
		t.Fatalf("captureAndFinalizeSeed: %v", err)
	}

	entries, err := os.ReadDir(ws)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "seed-") {
			t.Fatalf("archive staged inside the captured workspace: %s", e.Name())
		}
	}
	// The staging directory holds no leftover archive after a successful capture.
	staged, err := os.ReadDir(staging)
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) != 0 {
		t.Fatalf("staging dir should be empty after capture, has %d entries", len(staged))
	}
}

// prepareInput builds a RunTaskInput wired to a store and worker API for the
// prepareWorkspaceFilesystem tests, with a fresh persist that holds one space
// file so a test can tell "materialized" from "restored".
func prepareInput(t *testing.T, store CheckpointPayloadStore, srv *httptest.Server) (RunTaskInput, runDirs, *coretask.Task, *coretask.Run) {
	t.Helper()
	persist := newFakePersistStorage()
	if err := persist.Put(context.Background(), "sp_1", "space.txt", bytes.NewReader([]byte("space"))); err != nil {
		t.Fatal(err)
	}
	runDir := t.TempDir()
	dirs := runDirs{runDir: runDir, runWorkspace: filepath.Join(runDir, "workspace")}
	input := RunTaskInput{Persist: persist}
	if store != nil {
		input.Checkpoints = store
	}
	if srv != nil {
		input.WorkerAPI = workerclient.WorkerAPIClientConfig{BaseURL: srv.URL, Token: "t", Client: srv.Client()}
	}
	return input, dirs, &coretask.Task{ID: "t1", SpaceID: "sp_1"}, &coretask.Run{ID: "rt_1"}
}

// TestPrepareWorkspaceFilesystem_RestoresBaseInsteadOfMaterializing pins that a
// run with a base gets the checkpoint's tree, not the space snapshot, and that
// the restore outcome is recorded.
func TestPrepareWorkspaceFilesystem_RestoresBaseInsteadOfMaterializing(t *testing.T) {
	ctx := context.Background()
	store := &fakeCheckpointStore{}
	// The base holds a file the space snapshot does not, so its presence proves
	// the restore, and the absence of the space file proves materialize was
	// skipped.
	baseSrc := filepath.Join(t.TempDir(), "base")
	if err := os.MkdirAll(baseSrc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(baseSrc, "from_base.txt"), []byte("base work"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := &seedRecorder{base: seedBase(t, store, "sp_1", baseSrc)}
	srv := rec.server(t)
	defer srv.Close()
	input, dirs, task, run := prepareInput(t, store, srv)

	if err := prepareWorkspaceFilesystem(ctx, input, task, run, dirs); err != nil {
		t.Fatalf("prepareWorkspaceFilesystem: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dirs.runWorkspace, "from_base.txt")); err != nil || string(got) != "base work" {
		t.Fatalf("restored base file = %q, err %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dirs.runWorkspace, "space.txt")); !os.IsNotExist(err) {
		t.Fatalf("space snapshot must not be materialized over a restored base, stat err = %v", err)
	}
	if len(store.puts) != 0 {
		t.Fatalf("a restoring run must not seed, uploaded %d payloads", len(store.puts))
	}
	if rec.restore == nil || rec.restore.Status != string(coretask.WorkspaceRestoreRestored) {
		t.Fatalf("restore outcome = %+v, want status restored", rec.restore)
	}
}

// TestPrepareWorkspaceFilesystem_SeedsFirstRunFromMaterializedFiles pins that a
// run with no base materializes the space snapshot and seeds it, and records no
// restore.
func TestPrepareWorkspaceFilesystem_SeedsFirstRunFromMaterializedFiles(t *testing.T) {
	ctx := context.Background()
	store := &fakeCheckpointStore{}
	rec := &seedRecorder{} // no base -> 204
	srv := rec.server(t)
	defer srv.Close()
	input, dirs, task, run := prepareInput(t, store, srv)

	if err := prepareWorkspaceFilesystem(ctx, input, task, run, dirs); err != nil {
		t.Fatalf("prepareWorkspaceFilesystem: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dirs.runWorkspace, "space.txt")); err != nil || string(got) != "space" {
		t.Fatalf("first run should materialize the space file, got %q err %v", got, err)
	}
	if rec.finalize == nil {
		t.Fatal("first run should finalize a seed")
	}
	if rec.restore != nil {
		t.Fatal("first run has no base to restore, so must record no restore")
	}
}

// TestPrepareWorkspaceFilesystem_MaterializesOnlyWhenUnsupported pins that a
// server with no checkpoint route (an evaluation control plane) makes the run
// materialize and neither seed nor restore.
func TestPrepareWorkspaceFilesystem_MaterializesOnlyWhenUnsupported(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	store := &fakeCheckpointStore{}
	input, dirs, task, run := prepareInput(t, store, srv)

	if err := prepareWorkspaceFilesystem(ctx, input, task, run, dirs); err != nil {
		t.Fatalf("prepareWorkspaceFilesystem: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dirs.runWorkspace, "space.txt")); err != nil {
		t.Fatalf("unsupported deployment should still materialize space files: %v", err)
	}
	if len(store.puts) != 0 {
		t.Fatalf("must not seed against a server with no checkpoint route, uploaded %d", len(store.puts))
	}
}

// TestPrepareWorkspaceFilesystem_NoopStoreMaterializesOnly pins that a run
// without a checkpoint store (a CLI or eval run) materializes and does not fail.
func TestPrepareWorkspaceFilesystem_NoopStoreMaterializesOnly(t *testing.T) {
	ctx := context.Background()
	input, dirs, task, run := prepareInput(t, nil, nil)

	if err := prepareWorkspaceFilesystem(ctx, input, task, run, dirs); err != nil {
		t.Fatalf("prepareWorkspaceFilesystem without a store should materialize and succeed, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dirs.runWorkspace, "space.txt")); err != nil {
		t.Fatalf("space file should be materialized: %v", err)
	}
}

// TestExtractBaseIntoWorkspace_FailsOnDigestMismatch pins that a payload whose
// bytes do not match the base digest fails the restore visibly (§13) rather than
// populating the workspace with corrupt content.
func TestExtractBaseIntoWorkspace_FailsOnDigestMismatch(t *testing.T) {
	ctx := context.Background()
	store := &fakeCheckpointStore{}
	baseSrc := filepath.Join(t.TempDir(), "base")
	if err := os.MkdirAll(baseSrc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(baseSrc, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	base := seedBase(t, store, "sp_1", baseSrc)
	// Corrupt the descriptor's digest so it no longer matches the stored bytes.
	wrong := strings.Repeat("b", 64)
	store.putBlob("sp_1", wrong, store.blobs[fakeCheckpointKey("sp_1", base.PayloadSHA256)])
	base.PayloadSHA256 = wrong

	dirs := runDirs{runDir: t.TempDir(), runWorkspace: filepath.Join(t.TempDir(), "workspace")}
	err := extractBaseIntoWorkspace(ctx, store, dirs, "sp_1", base)
	if err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("expected a digest mismatch error, got %v", err)
	}
}

// TestRestoreWorkspaceBase_RecordsFailedThenFailsClosed pins that a restore that
// cannot read its payload records a failed outcome and fails the run closed.
func TestRestoreWorkspaceBase_RecordsFailedThenFailsClosed(t *testing.T) {
	ctx := context.Background()
	store := &fakeCheckpointStore{} // empty: Open will not find the blob
	rec := &seedRecorder{}
	srv := rec.server(t)
	defer srv.Close()
	input, dirs, task, _ := prepareInput(t, store, srv)
	base := &workerclient.WorkspaceBaseResponse{
		CheckpointID: "wc_base", PayloadFormat: wsarchive.PayloadFormat,
		PayloadSHA256: strings.Repeat("c", 64), SizeBytes: 1,
	}

	err := restoreWorkspaceBase(ctx, input, "rt_1", dirs, task.SpaceID, base)
	if err == nil {
		t.Fatal("a restore that cannot read its payload must fail the run closed")
	}
	if rec.restore == nil || rec.restore.Status != string(coretask.WorkspaceRestoreFailed) {
		t.Fatalf("restore outcome = %+v, want status failed", rec.restore)
	}
	if rec.restore.Error == "" {
		t.Fatal("a failed restore must record a bounded error")
	}
}

// TestCaptureResultCheckpoint_ReturnsDescriptorWithMatchingDigest pins that a
// captured result uploads its bytes and returns a descriptor whose digest covers
// them, ready to ride the terminal report.
func TestCaptureResultCheckpoint_ReturnsDescriptorWithMatchingDigest(t *testing.T) {
	ctx := context.Background()
	store := &fakeCheckpointStore{}
	srv := (&seedRecorder{}).server(t)
	defer srv.Close()
	input := RunTaskInput{
		Checkpoints: store,
		WorkerAPI:   workerclient.WorkerAPIClientConfig{BaseURL: srv.URL, Token: "t", Client: srv.Client()},
	}
	runDir := t.TempDir()
	dirs := runDirs{runDir: runDir, runWorkspace: writeWorkspace(t)}
	task := &coretask.Task{ID: "t1", SpaceID: "sp_1"}

	desc := captureWorkspaceCheckpoint(ctx, input, task, dirs)
	if desc == nil {
		t.Fatal("expected a result descriptor")
	}
	if len(store.puts) != 1 {
		t.Fatalf("expected one upload, got %d", len(store.puts))
	}
	sum := sha256.Sum256(store.puts[0].bytes)
	if desc.PayloadSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("descriptor digest does not match uploaded bytes")
	}
	if desc.PayloadFormat != wsarchive.PayloadFormat || desc.EntryCount != 3 {
		t.Fatalf("descriptor = %+v", desc)
	}
}

// TestCaptureResultCheckpoint_FailOpen pins that result capture never fails the
// run: a missing workspace, or no checkpoint store, yields a nil descriptor
// rather than an error.
func TestCaptureResultCheckpoint_FailOpen(t *testing.T) {
	ctx := context.Background()
	task := &coretask.Task{ID: "t1", SpaceID: "sp_1"}

	// No store: nothing to capture to.
	if desc := captureWorkspaceCheckpoint(ctx, RunTaskInput{}, task, runDirs{runDir: t.TempDir(), runWorkspace: t.TempDir()}); desc != nil {
		t.Fatal("no checkpoint store should yield a nil descriptor")
	}

	// Store present, but the workspace directory does not exist: capture fails and
	// is swallowed.
	srv := (&seedRecorder{}).server(t)
	defer srv.Close()
	input := RunTaskInput{
		Checkpoints: &fakeCheckpointStore{},
		WorkerAPI:   workerclient.WorkerAPIClientConfig{BaseURL: srv.URL, Token: "t", Client: srv.Client()},
	}
	dirs := runDirs{runDir: t.TempDir(), runWorkspace: filepath.Join(t.TempDir(), "does-not-exist")}
	if desc := captureWorkspaceCheckpoint(ctx, input, task, dirs); desc != nil {
		t.Fatal("a failed capture should yield a nil descriptor, not fail the run")
	}
}

// TestReportRunFailure_CarriesPartialDescriptor pins that a failed run's terminal
// report carries the partial checkpoint descriptor it captured.
func TestReportRunFailure_CarriesPartialDescriptor(t *testing.T) {
	up := &fakeUpdater{}
	desc := &workerclient.WorkspaceCheckpointDescriptor{
		PayloadFormat: wsarchive.PayloadFormat, PayloadSHA256: "abc", SizeBytes: 5,
	}
	err := reportRunFailure(context.Background(), "rt_1", context.DeadlineExceeded, coretask.FailureRun, "traces/x.jsonl", desc, up)
	if up.req == nil || up.req.Status != "FAILED" {
		t.Fatalf("failure not reported: %+v", up.req)
	}
	if up.req.WorkspaceCheckpoint != desc {
		t.Fatalf("failure report did not carry the partial descriptor: %+v", up.req.WorkspaceCheckpoint)
	}
	if !errors.Is(err, coretask.ErrRunFailed) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the cause marked ErrRunFailed", err)
	}
}

// A failure the server never received is not a reported outcome: the worker
// must not exit as if the run were terminal.
func TestReportRunFailure_UnreportedFailureIsNotMarked(t *testing.T) {
	up := &fakeUpdater{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := reportRunFailure(ctx, "rt_1", errors.New("the model refused"), coretask.FailureRun, "", nil, up)
	if err == nil || errors.Is(err, coretask.ErrRunFailed) {
		t.Fatalf("err = %v, want the report error without ErrRunFailed", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want it to carry the report error", err)
	}
}
