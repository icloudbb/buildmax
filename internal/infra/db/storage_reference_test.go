package db

import (
	"context"
	"testing"
	"time"

	coreartifact "github.com/icloudbb/buildmax/internal/core/artifact"
	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/service/storagecheck"
)

// These cover the keyset walks behind `buildmax-server storage verify` against
// real MySQL: the ordering and the `>` predicate over a binary-collated handle
// are what make a page boundary lose or repeat no row, and only a real
// database proves that. The shared test database holds other tests' rows, so
// each test walks the whole table and asserts on its own rows within it.

// The store is the check's only record source.
var _ storagecheck.Records = (*Store)(nil)

// walkAll pages through one listing two rows at a time, asserting no row comes
// back twice, and returns every row keyed by handle. ordered also asserts the
// handles strictly increase in byte order, which holds for the ascii_bin
// public ids but not for a plugin name under the table's default collation.
func walkAll[T any](t *testing.T, ordered bool, next func(after T) ([]T, error), key func(T) string) map[string]T {
	t.Helper()
	seen := map[string]T{}
	var last T
	first := true
	for pages := 0; ; pages++ {
		if pages > 100000 {
			t.Fatal("walk did not terminate")
		}
		rows, err := next(last)
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		if len(rows) == 0 {
			return seen
		}
		if len(rows) > 2 {
			t.Fatalf("page of %d rows, asked for 2", len(rows))
		}
		for _, r := range rows {
			k := key(r)
			if ordered && !first && k <= key(last) {
				t.Fatalf("handle %q after %q: the walk must strictly increase", k, key(last))
			}
			if _, dup := seen[k]; dup {
				t.Fatalf("handle %q returned twice", k)
			}
			seen[k] = r
			last, first = r, false
		}
	}
}

func TestListLiveArtifactsAfterWalksLiveRowsOnce(t *testing.T) {
	s, ctx := newTestStore(t)
	spaceID := newTestSpace(t, s, newTestUser(t, s, "storage-ref-artifact"))
	live := []string{
		newTestArtifact(t, s, spaceID, 10, nil),
		newTestArtifact(t, s, spaceID, 20, nil),
		newTestArtifact(t, s, spaceID, 30, nil),
	}
	tombstoned := newTestArtifact(t, s, spaceID, 40, nil)
	if ok, err := s.SoftDeleteArtifact(ctx, tombstoned, time.Now()); err != nil || !ok {
		t.Fatalf("SoftDeleteArtifact: ok=%v err=%v", ok, err)
	}

	seen := walkAll(t, true, func(after coreartifact.Artifact) ([]coreartifact.Artifact, error) {
		return s.ListLiveArtifactsAfter(ctx, after.ID, 2)
	}, func(a coreartifact.Artifact) string { return a.ID })

	for i, id := range live {
		got, ok := seen[id]
		if !ok {
			t.Errorf("live artifact %s missing from the walk", id)
			continue
		}
		if got.SpaceID != spaceID || got.SizeBytes != int64(10*(i+1)) || got.SHA256 != "abc" {
			t.Errorf("artifact %s = %+v, want its space, size, and digest", id, got)
		}
	}
	if _, ok := seen[tombstoned]; ok {
		t.Errorf("tombstoned artifact %s was walked", tombstoned)
	}
}

func TestListWorkspaceCheckpointPayloadsAfterCarriesTheStorageKey(t *testing.T) {
	s, spaceID, taskID, runID := seedTaskForCheckpoint(t)
	ctx := context.Background()
	cp, err := s.FinalizeWorkspaceCheckpoint(ctx, coretask.FinalizeCheckpointInput{
		SpaceID: spaceID, TaskID: taskID, SourceTaskRunID: runID,
		Kind: coretask.CheckpointKindSeed, PayloadFormat: coretask.PayloadFormatTarZstV1,
		PayloadSHA256: hex64('b'), StorageKey: "k/storage-ref", SizeBytes: 11, UncompressedBytes: 22, EntryCount: 1,
	})
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}

	seen := walkAll(t, true, func(after coretask.CheckpointPayloadRef) ([]coretask.CheckpointPayloadRef, error) {
		return s.ListWorkspaceCheckpointPayloadsAfter(ctx, after.CheckpointID, 2)
	}, func(c coretask.CheckpointPayloadRef) string { return c.CheckpointID })

	want := coretask.CheckpointPayloadRef{
		CheckpointID: cp.ID, SpaceID: spaceID, StorageKey: "k/storage-ref", PayloadSHA256: hex64('b'), SizeBytes: 11,
	}
	if got := seen[cp.ID]; got != want {
		t.Errorf("checkpoint ref = %+v, want %+v", got, want)
	}
}

func TestListTaskRunTracesAfterSkipsRunsWithoutATrace(t *testing.T) {
	s, spaceID, taskID, runID := seedTaskForCheckpoint(t)
	ctx := context.Background()

	// Before the run reports a trace it is not a reference.
	seen := walkAll(t, true, func(after coretask.RunTraceRef) ([]coretask.RunTraceRef, error) {
		return s.ListTaskRunTracesAfter(ctx, after.TaskRunID, 2)
	}, func(r coretask.RunTraceRef) string { return r.TaskRunID })
	if _, ok := seen[runID]; ok {
		t.Fatalf("run %s has no trace yet but was walked", runID)
	}

	tracePath := "sessions/s1/traces/" + runID + ".jsonl"
	ended := time.Now().UTC()
	for _, step := range []struct{ from, to coretask.RunStatus }{
		{coretask.RunStatusPending, coretask.RunStatusScheduled},
		{coretask.RunStatusScheduled, coretask.RunStatusRunning},
	} {
		if ok, err := s.TransitionTaskRun(ctx, coretask.TransitionRunInput{
			TaskRunID: runID, ExpectedStatus: step.from, NewStatus: step.to,
		}); err != nil || !ok {
			t.Fatalf("transition %s -> %s: ok=%v err=%v", step.from, step.to, ok, err)
		}
	}
	if ok, err := s.TransitionTaskRun(ctx, coretask.TransitionRunInput{
		TaskRunID: runID, ExpectedStatus: coretask.RunStatusRunning, NewStatus: coretask.RunStatusSucceeded,
		EndedAt: &ended, TracePath: &tracePath,
	}); err != nil || !ok {
		t.Fatalf("transition to SUCCEEDED: ok=%v err=%v", ok, err)
	}

	seen = walkAll(t, true, func(after coretask.RunTraceRef) ([]coretask.RunTraceRef, error) {
		return s.ListTaskRunTracesAfter(ctx, after.TaskRunID, 2)
	}, func(r coretask.RunTraceRef) string { return r.TaskRunID })
	want := coretask.RunTraceRef{SpaceID: spaceID, TaskID: taskID, TaskRunID: runID, TracePath: tracePath}
	if got := seen[runID]; got != want {
		t.Errorf("trace ref = %+v, want %+v", got, want)
	}

	// A pointer retention cleared is no longer a reference.
	if err := s.ClearTaskRunTracePath(ctx, runID); err != nil {
		t.Fatalf("ClearTaskRunTracePath: %v", err)
	}
	seen = walkAll(t, true, func(after coretask.RunTraceRef) ([]coretask.RunTraceRef, error) {
		return s.ListTaskRunTracesAfter(ctx, after.TaskRunID, 2)
	}, func(r coretask.RunTraceRef) string { return r.TaskRunID })
	if _, ok := seen[runID]; ok {
		t.Errorf("run %s with a cleared trace pointer was walked", runID)
	}
}

func TestListPluginReleasesAfterIncludesYankedReleases(t *testing.T) {
	s, ctx := newPluginStore(t)
	const name = "store-test-storage-ref"
	makeCatalogEntry(t, s, ctx, name)
	publisher := newTestUser(t, s, "publisher")
	for _, v := range []string{"1.0.0", "1.1.0", "2.0.0"} {
		if _, err := s.CreatePluginRelease(ctx, coreplugin.CreateReleaseInput{
			PluginName: name, Version: v, Digest: "sha256:" + hex64('c'),
			ObjectKey: "plugins/" + name + "/" + v, SizeBytes: 7, PublishedBy: publisher,
		}); err != nil {
			t.Fatalf("CreatePluginRelease %s: %v", v, err)
		}
	}
	if err := s.YankPluginRelease(ctx, name, "1.1.0", publisher, "broken"); err != nil {
		t.Fatalf("YankPluginRelease: %v", err)
	}

	seen := walkAll(t, false, func(after coreplugin.Release) ([]coreplugin.Release, error) {
		return s.ListPluginReleasesAfter(ctx, after.PluginName, after.Version, 2)
	}, func(r coreplugin.Release) string { return r.PluginName + "\x00" + r.Version })

	for _, v := range []string{"1.0.0", "1.1.0", "2.0.0"} {
		got, ok := seen[name+"\x00"+v]
		if !ok {
			t.Errorf("release %s@%s missing from the walk", name, v)
			continue
		}
		if got.Digest != "sha256:"+hex64('c') || got.ObjectKey != "plugins/"+name+"/"+v || got.SizeBytes != 7 {
			t.Errorf("release %s@%s = %+v, want its storage fields", name, v, got)
		}
	}
}
