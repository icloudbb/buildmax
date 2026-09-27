package objectstore

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/apierr"
)

func TestLocalFSPersistStorage_PutGetListMaterialize(t *testing.T) {
	root := t.TempDir()
	persistRoot := func(workspaceID string) string {
		return filepath.Join(root, workspaceID, "persist")
	}
	runGlobalDir := func(spaceID, taskID, taskRunID string) string {
		return filepath.Join(root, spaceID, "tasks", taskID, taskRunID, "global")
	}
	s := NewLocalFSPersistStorage(persistRoot, runGlobalDir)
	ctx := context.Background()
	ws := "ws1"

	if err := s.Put(ctx, ws, "f1.txt", bytes.NewReader([]byte("hello"))); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, ws, "sub/f2.txt", bytes.NewReader([]byte("world"))); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListFiles(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("list len = %d, want 2", len(list))
	}
	data, err := s.Get(ctx, ws, "f1.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Errorf("got %q", data)
	}
	dst := t.TempDir()
	if err := s.MaterializeToDir(ctx, ws, dst); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dst, "f1.txt"))
	if string(b) != "hello" {
		t.Errorf("materialized f1 = %q", b)
	}
	b, _ = os.ReadFile(filepath.Join(dst, "sub", "f2.txt"))
	if string(b) != "world" {
		t.Errorf("materialized f2 = %q", b)
	}
}

func TestLocalFSPersistStorage_DeleteRunGlobal(t *testing.T) {
	root := t.TempDir()
	runGlobalDir := func(spaceID, taskID, taskRunID string) string {
		return filepath.Join(root, spaceID, "tasks", taskID, taskRunID, "global")
	}
	s := NewLocalFSPersistStorage(func(string) string { return root }, runGlobalDir)
	ctx := context.Background()
	ref := RunObjectRef{SpaceID: "sp", TaskID: "t", TaskRunID: "r", RelPath: "sessions/s/traces/r.jsonl"}

	// Seed the trace file the way a worker leaves it on disk.
	full := filepath.Join(runGlobalDir(ref.SpaceID, ref.TaskID, ref.TaskRunID), "sessions", "s", "traces", "r.jsonl")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(`{"type":"run_start"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteRunGlobal(ctx, ref); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(full); !os.IsNotExist(err) {
		t.Errorf("trace file must be gone, stat err = %v", err)
	}
	// A second delete of the now-missing file is not an error: the sweep is
	// idempotent across restarts.
	if err := s.DeleteRunGlobal(ctx, ref); err != nil {
		t.Errorf("deleting a missing trace must not error, got %v", err)
	}
}

// Without a resolver the store does not know the run-global layout, so it
// removes nothing rather than guessing a path.
func TestLocalFSPersistStorage_DeleteRunGlobalNoResolver(t *testing.T) {
	s := NewLocalFSPersistStorage(func(string) string { return t.TempDir() }, nil)
	if err := s.DeleteRunGlobal(context.Background(), RunObjectRef{SpaceID: "sp", TaskID: "t", TaskRunID: "r", RelPath: "x.jsonl"}); err != nil {
		t.Errorf("nil resolver must be a no-op, got %v", err)
	}
}

// A Continue run restores the previous run's session bundle through
// GetRunGlobal. On local_fs that bundle is on the worker disk, so the store has
// to read it from there; reporting it missing silently started every Continue
// run with no history.
func TestLocalFSPersistStorage_GetRunGlobalReadsTheWorkerDisk(t *testing.T) {
	root := t.TempDir()
	runGlobalDir := func(spaceID, taskID, taskRunID string) string {
		return filepath.Join(root, spaceID, "tasks", taskID, taskRunID, "global")
	}
	s := NewLocalFSPersistStorage(func(string) string { return root }, runGlobalDir)
	ctx := context.Background()
	ref := RunObjectRef{SpaceID: "sp", TaskID: "t", TaskRunID: "r1", RelPath: "sessions/s1/history.jsonl"}
	full := filepath.Join(runGlobalDir("sp", "t", "r1"), "sessions", "s1", "history.jsonl")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(`{"type":"history"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetRunGlobal(ctx, ref)
	if err != nil || string(got) != `{"type":"history"}` {
		t.Fatalf("GetRunGlobal = %q, %v", got, err)
	}
	missing := ref
	missing.RelPath = "sessions/s1/meta.json"
	if _, err := s.GetRunGlobal(ctx, missing); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("a missing file = %v, want ErrNotFound", err)
	}
	escape := ref
	escape.RelPath = "../../r2/global/secret"
	if _, err := s.GetRunGlobal(ctx, escape); err == nil {
		t.Error("a path out of the run directory was read")
	}
	if _, err := NewLocalFSPersistStorage(func(string) string { return root }, nil).GetRunGlobal(ctx, ref); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("no resolver = %v, want ErrNotFound", err)
	}
}
