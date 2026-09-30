package db

import (
	"context"
	"errors"
	"testing"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

// seedTaskForCheckpoint creates a space, agent, and task, and returns the store,
// context, space handle, task handle, and the first run's handle.
func seedTaskForCheckpoint(t *testing.T) (*Store, string, string, string) {
	t.Helper()
	s, ctx := newTestStore(t)
	userID := newTestUser(t, s, "ckpt")
	spaceID := newTestSpace(t, s, userID)
	agent, err := s.CreateAgentInSpace(ctx, agentdef.CreateInput{
		SpaceID: spaceID, UserID: userID, Def: agentdef.Definition{Name: "ckpt-runner"},
	})
	if err != nil {
		t.Fatalf("CreateAgentInSpace: %v", err)
	}
	task, err := s.CreateTask(ctx, &coretask.CreateInput{
		SpaceID: spaceID, AgentID: &agent.ID, Input: "do the work", CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() {
		_ = s.db.WithContext(ctx).Delete(&workspaceCheckpointRow{}, "task_id = (SELECT id FROM task WHERE public_id = ?)", task.ID)
		_ = s.db.WithContext(ctx).Delete(&pluginEnvironmentRow{}, "task_id = (SELECT id FROM task WHERE public_id = ?)", task.ID)
		_ = s.db.WithContext(ctx).Delete(&taskRunRow{}, "task_id = (SELECT id FROM task WHERE public_id = ?)", task.ID)
		_ = s.db.WithContext(ctx).Delete(&taskRow{}, "public_id = ?", task.ID)
	})
	if task.LastRunID == nil {
		t.Fatal("CreateTask did not create the first run")
	}
	return s, spaceID, task.ID, *task.LastRunID
}

func TestFinalizeSeedCheckpointEstablishesTheTaskHead(t *testing.T) {
	s, spaceID, taskID, runID := seedTaskForCheckpoint(t)
	ctx := t.Context()

	seed, err := s.FinalizeWorkspaceCheckpoint(ctx, coretask.FinalizeCheckpointInput{
		SpaceID: spaceID, TaskID: taskID, SourceTaskRunID: runID,
		Kind: coretask.CheckpointKindSeed, PayloadFormat: coretask.PayloadFormatTarZstV1,
		PayloadSHA256: hex64('a'), StorageKey: "k/seed", SizeBytes: 10, UncompressedBytes: 20, EntryCount: 3,
	})
	if err != nil {
		t.Fatalf("finalize seed: %v", err)
	}
	if seed.Kind != coretask.CheckpointKindSeed {
		t.Errorf("kind = %q, want seed", seed.Kind)
	}

	// The Task head is the seed, and the run's base points at it.
	var headPub string
	s.db.WithContext(ctx).Raw(
		"SELECT c.public_id FROM task t JOIN workspace_checkpoint c ON c.id = t.workspace_head_checkpoint_id WHERE t.public_id = ?", taskID,
	).Scan(&headPub)
	if headPub != seed.ID {
		t.Errorf("task head = %q, want the seed %q", headPub, seed.ID)
	}

	// Idempotent: the same run, kind, and bytes return the same checkpoint.
	again, err := s.FinalizeWorkspaceCheckpoint(ctx, coretask.FinalizeCheckpointInput{
		SpaceID: spaceID, TaskID: taskID, SourceTaskRunID: runID,
		Kind: coretask.CheckpointKindSeed, PayloadFormat: coretask.PayloadFormatTarZstV1,
		PayloadSHA256: hex64('a'), StorageKey: "k/seed", SizeBytes: 10, UncompressedBytes: 20, EntryCount: 3,
	})
	if err != nil {
		t.Fatalf("finalize seed (idempotent): %v", err)
	}
	if again.ID != seed.ID {
		t.Errorf("idempotent finalize returned %q, want the first checkpoint %q", again.ID, seed.ID)
	}

	// Conflict: the same run and kind with different bytes never rewrites.
	if _, err := s.FinalizeWorkspaceCheckpoint(ctx, coretask.FinalizeCheckpointInput{
		SpaceID: spaceID, TaskID: taskID, SourceTaskRunID: runID,
		Kind: coretask.CheckpointKindSeed, PayloadFormat: coretask.PayloadFormatTarZstV1,
		PayloadSHA256: hex64('b'), StorageKey: "k/seed2", SizeBytes: 11, UncompressedBytes: 21, EntryCount: 4,
	}); !errors.Is(err, ErrCheckpointConflict) {
		t.Errorf("conflicting seed error = %v, want ErrCheckpointConflict", err)
	}
}

func TestFinalizeSuccessfulAdvancesHeadAndPartialDoesNot(t *testing.T) {
	s, spaceID, taskID, run1 := seedTaskForCheckpoint(t)
	ctx := t.Context()

	seed, err := s.FinalizeWorkspaceCheckpoint(ctx, coretask.FinalizeCheckpointInput{
		SpaceID: spaceID, TaskID: taskID, SourceTaskRunID: run1,
		Kind: coretask.CheckpointKindSeed, PayloadFormat: coretask.PayloadFormatTarZstV1,
		PayloadSHA256: hex64('a'), StorageKey: "k/seed", SizeBytes: 10, UncompressedBytes: 20, EntryCount: 3,
	})
	if err != nil {
		t.Fatalf("finalize seed: %v", err)
	}

	// Continue is refused while a run is active, so the first run finishes
	// before the second is created — as it does in the real flow.
	finishRun(t, s, ctx, run1)

	// A Continue run whose base is the seed (set here as Phase 2 will at restore).
	run2, err := s.CreateTaskRun(ctx, coretask.CreateRunInput{TaskID: taskID, Input: "continue", CreatedBy: newTestUser(t, s, "c2")})
	if err != nil {
		t.Fatalf("CreateTaskRun: %v", err)
	}
	if err := s.db.WithContext(ctx).Exec(
		"UPDATE task_run SET workspace_base_checkpoint_id = (SELECT id FROM workspace_checkpoint WHERE public_id = ?) WHERE public_id = ?",
		seed.ID, run2.ID,
	).Error; err != nil {
		t.Fatalf("set run2 base: %v", err)
	}

	result, err := s.FinalizeWorkspaceCheckpoint(ctx, coretask.FinalizeCheckpointInput{
		SpaceID: spaceID, TaskID: taskID, SourceTaskRunID: run2.ID, BaseCheckpointID: &seed.ID,
		Kind: coretask.CheckpointKindSuccessful, PayloadFormat: coretask.PayloadFormatTarZstV1,
		PayloadSHA256: hex64('c'), StorageKey: "k/result", SizeBytes: 12, UncompressedBytes: 22, EntryCount: 5,
	})
	if err != nil {
		t.Fatalf("finalize successful: %v", err)
	}

	var headPub string
	s.db.WithContext(ctx).Raw(
		"SELECT c.public_id FROM task t JOIN workspace_checkpoint c ON c.id = t.workspace_head_checkpoint_id WHERE t.public_id = ?", taskID,
	).Scan(&headPub)
	if headPub != result.ID {
		t.Errorf("task head after successful = %q, want the result %q", headPub, result.ID)
	}

	// A partial checkpoint on a later run never advances the head.
	finishRun(t, s, ctx, run2.ID)
	run3, err := s.CreateTaskRun(ctx, coretask.CreateRunInput{TaskID: taskID, Input: "again", CreatedBy: newTestUser(t, s, "c3")})
	if err != nil {
		t.Fatalf("CreateTaskRun: %v", err)
	}
	if _, err := s.FinalizeWorkspaceCheckpoint(ctx, coretask.FinalizeCheckpointInput{
		SpaceID: spaceID, TaskID: taskID, SourceTaskRunID: run3.ID, BaseCheckpointID: &result.ID,
		Kind: coretask.CheckpointKindPartial, PayloadFormat: coretask.PayloadFormatTarZstV1,
		PayloadSHA256: hex64('d'), StorageKey: "k/partial", SizeBytes: 9, UncompressedBytes: 15, EntryCount: 2,
	}); err != nil {
		t.Fatalf("finalize partial: %v", err)
	}
	headPub = ""
	s.db.WithContext(ctx).Raw(
		"SELECT c.public_id FROM task t JOIN workspace_checkpoint c ON c.id = t.workspace_head_checkpoint_id WHERE t.public_id = ?", taskID,
	).Scan(&headPub)
	if headPub != result.ID {
		t.Errorf("task head after partial = %q, want it unchanged at %q", headPub, result.ID)
	}
}

// A run read exposes its workspace-checkpoint provenance by public handle: the
// base it started from and the result/partial it produced. This is what
// GET /tasks/{id}/runs returns and what the worker's checkpoint finalizer reads
// back to record which checkpoint a result was built from — both were nil before
// toTaskRun resolved these columns, so a result recorded no base.
func TestTaskRunReadExposesCheckpointProvenance(t *testing.T) {
	s, spaceID, taskID, run1 := seedTaskForCheckpoint(t)
	ctx := t.Context()

	seed, err := s.FinalizeWorkspaceCheckpoint(ctx, coretask.FinalizeCheckpointInput{
		SpaceID: spaceID, TaskID: taskID, SourceTaskRunID: run1,
		Kind: coretask.CheckpointKindSeed, PayloadFormat: coretask.PayloadFormatTarZstV1,
		PayloadSHA256: hex64('a'), StorageKey: "k/seed", SizeBytes: 10, UncompressedBytes: 20, EntryCount: 3,
	})
	if err != nil {
		t.Fatalf("finalize seed: %v", err)
	}
	finishRun(t, s, ctx, run1)

	run2, err := s.CreateTaskRun(ctx, coretask.CreateRunInput{TaskID: taskID, Input: "continue", CreatedBy: newTestUser(t, s, "prov")})
	if err != nil {
		t.Fatalf("CreateTaskRun: %v", err)
	}
	// A Continue run starts from the Task's committed head, the seed here.
	if err := s.db.WithContext(ctx).Exec(
		"UPDATE task_run SET workspace_base_checkpoint_id = (SELECT id FROM workspace_checkpoint WHERE public_id = ?) WHERE public_id = ?",
		seed.ID, run2.ID,
	).Error; err != nil {
		t.Fatalf("set run2 base: %v", err)
	}

	// The base resolves to a public handle on a read, so the worker finalizer
	// reads a real base instead of nil.
	got, err := s.GetTaskRun(ctx, run2.ID)
	if err != nil || got == nil {
		t.Fatalf("GetTaskRun: %v", err)
	}
	if got.WorkspaceBaseCheckpointID == nil || *got.WorkspaceBaseCheckpointID != seed.ID {
		t.Fatalf("run base = %v, want the seed %q", got.WorkspaceBaseCheckpointID, seed.ID)
	}
	if got.WorkspaceResultCheckpointID != nil {
		t.Errorf("run result checkpoint = %v before any result, want nil", got.WorkspaceResultCheckpointID)
	}

	result, err := s.FinalizeWorkspaceCheckpoint(ctx, coretask.FinalizeCheckpointInput{
		SpaceID: spaceID, TaskID: taskID, SourceTaskRunID: run2.ID, BaseCheckpointID: got.WorkspaceBaseCheckpointID,
		Kind: coretask.CheckpointKindSuccessful, PayloadFormat: coretask.PayloadFormatTarZstV1,
		PayloadSHA256: hex64('c'), StorageKey: "k/result", SizeBytes: 12, UncompressedBytes: 22, EntryCount: 5,
	})
	if err != nil {
		t.Fatalf("finalize successful: %v", err)
	}

	// The list read now carries the result checkpoint the run produced.
	runs, err := s.ListTaskRunsByTask(ctx, taskID)
	if err != nil {
		t.Fatalf("ListTaskRunsByTask: %v", err)
	}
	var seen *coretask.Run
	for i := range runs {
		if runs[i].ID == run2.ID {
			seen = &runs[i]
		}
	}
	if seen == nil {
		t.Fatalf("run %s not in the list", run2.ID)
	}
	if seen.WorkspaceResultCheckpointID == nil || *seen.WorkspaceResultCheckpointID != result.ID {
		t.Errorf("listed run result checkpoint = %v, want %q", seen.WorkspaceResultCheckpointID, result.ID)
	}

	// The committed result records the base it was built from — the provenance
	// that stayed NULL while the base never reached the finalizer.
	rc, err := s.GetWorkspaceCheckpoint(ctx, result.ID)
	if err != nil || rc == nil {
		t.Fatalf("GetWorkspaceCheckpoint: %v", err)
	}
	if rc.BaseCheckpointID == nil || *rc.BaseCheckpointID != seed.ID {
		t.Errorf("result checkpoint base = %v, want the seed %q", rc.BaseCheckpointID, seed.ID)
	}
}

// finishRun moves a run to a terminal status so Continue, which is refused
// while a run is active, may create the next one.
func finishRun(t *testing.T, s *Store, ctx context.Context, runPublicID string) {
	t.Helper()
	if err := s.db.WithContext(ctx).Model(&taskRunRow{}).
		Where("public_id = ?", runPublicID).
		Update("status", string(coretask.RunStatusSucceeded)).Error; err != nil {
		t.Fatalf("finish run %s: %v", runPublicID, err)
	}
}

// hex64 makes a 64-character lowercase hex string of one repeated nibble, a
// stand-in digest distinct per byte.
func hex64(c byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = c
	}
	return string(b)
}

// TestReferencedCheckpointStorageKeys pins that the orphan sweep's liveness read
// returns every storage key a checkpoint row names, and only those.
func TestReferencedCheckpointStorageKeys(t *testing.T) {
	s, spaceID, taskID, runID := seedTaskForCheckpoint(t)
	ctx := t.Context()

	if _, err := s.FinalizeWorkspaceCheckpoint(ctx, coretask.FinalizeCheckpointInput{
		SpaceID: spaceID, TaskID: taskID, SourceTaskRunID: runID,
		Kind: coretask.CheckpointKindSeed, PayloadFormat: coretask.PayloadFormatTarZstV1,
		PayloadSHA256: hex64('a'), StorageKey: "k/seed", SizeBytes: 10, UncompressedBytes: 20, EntryCount: 3,
	}); err != nil {
		t.Fatalf("finalize seed: %v", err)
	}
	if _, err := s.FinalizeWorkspaceCheckpoint(ctx, coretask.FinalizeCheckpointInput{
		SpaceID: spaceID, TaskID: taskID, SourceTaskRunID: runID,
		Kind: coretask.CheckpointKindSuccessful, PayloadFormat: coretask.PayloadFormatTarZstV1,
		PayloadSHA256: hex64('c'), StorageKey: "k/result", SizeBytes: 12, UncompressedBytes: 22, EntryCount: 5,
	}); err != nil {
		t.Fatalf("finalize result: %v", err)
	}

	keys, err := s.ReferencedCheckpointStorageKeys(ctx)
	if err != nil {
		t.Fatalf("ReferencedCheckpointStorageKeys: %v", err)
	}
	if _, ok := keys["k/seed"]; !ok {
		t.Error("seed storage key not reported as referenced")
	}
	if _, ok := keys["k/result"]; !ok {
		t.Error("result storage key not reported as referenced")
	}
	if _, ok := keys["k/never-stored"]; ok {
		t.Error("an unstored key must not be reported as referenced")
	}
}
