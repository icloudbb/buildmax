package taskrun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/infra/workerclient"
	"github.com/icloudbb/buildmax/internal/infra/wsarchive"
)

// CheckpointPayloadStore reads and writes checkpoint payloads in the object
// store, content-addressed by the Space and the payload's SHA-256. The worker
// uploads and downloads bytes by digest and never names their key; the server
// derives the one canonical key when it records the pointer, and the worker
// derives the same one to read a base back. See
// docs/design/task-workspace-checkpoints.md §8.
type CheckpointPayloadStore interface {
	Put(ctx context.Context, spaceID, sha256hex string, src io.Reader) (string, error)
	Open(ctx context.Context, storageKey string) (io.ReadCloser, int64, error)
	Key(spaceID, sha256hex string) (string, error)
}

// Provisional checkpoint capture limits. §12.3 requires benchmark-derived
// defaults over representative source, build, and data workspaces before they
// are fixed; these are conservative stand-ins that bound an enumeration or
// expansion blow-up without cutting off an ordinary tree. wsarchive enforces the
// three dimensions it supports today; the stored-byte, per-file, and
// expansion-ratio limits §12.3 also names arrive with the codec that enforces
// them.
var checkpointLimits = wsarchive.Limits{
	MaxUncompressedBytes: 4 << 30, // 4 GiB of regular-file bytes
	MaxEntries:           500_000,
	MaxPathDepth:         64,
}

// prepareWorkspaceFilesystem fills workspace/ with the run's starting files and
// records what it did, before any model or tool call. A run with a base restores
// it — the checkpoint is authoritative and already holds the space files its
// seed captured, so the space snapshot is not materialized over it. A first run
// materializes the space files and captures them as the Task's seed. A
// deployment that does not run the checkpoint contract just materializes.
//
// Restore and seed both fail the run closed: a run that could not restore the
// workspace it was told to continue, or whose seed did not commit, has no
// coherent tree to execute against and must refuse rather than run against the
// wrong one or lose the work silently (§13).
func prepareWorkspaceFilesystem(ctx context.Context, input RunTaskInput, task *coretask.Task, run *coretask.Run, dirs runDirs) error {
	base, supported, err := resolveWorkspaceBase(ctx, input, run.ID)
	if err != nil {
		return err
	}
	if base != nil {
		return restoreWorkspaceBase(ctx, input, run.ID, dirs, task.SpaceID, base)
	}
	if err := input.Persist.MaterializeToDir(ctx, task.SpaceID, dirs.runWorkspace); err != nil {
		componentLog().Error("failed to materialize space files", "task_run_id", run.ID, "space_id", task.SpaceID, "err", err)
		return err
	}
	if supported {
		stagingDir := filepath.Join(dirs.runDir, "checkpoint-staging")
		return captureAndFinalizeSeed(ctx, input.Checkpoints, input.WorkerAPI, stagingDir, dirs.runWorkspace, task.SpaceID, run.ID)
	}
	return nil
}

// resolveWorkspaceBase asks the server for this run's base checkpoint. It
// returns (base, supported, err): supported is false when this deployment does
// not run the checkpoint contract — no payload store, or a server that answers
// the base lookup with 404/503 — in which case the run neither restores nor
// seeds. A non-nil base is the checkpoint the run must restore.
func resolveWorkspaceBase(ctx context.Context, input RunTaskInput, taskRunID string) (*workerclient.WorkspaceBaseResponse, bool, error) {
	if input.Checkpoints == nil || input.WorkerAPI.BaseURL == "" || input.WorkerAPI.Token == "" {
		return nil, false, nil
	}
	base, err := workerclient.GetWorkspaceBase(ctx, input.WorkerAPI, taskRunID)
	if errors.Is(err, workerclient.ErrWorkspaceCheckpointsUnsupported) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read workspace base: %w", err)
	}
	return base, true, nil
}

// restoreWorkspaceBase replaces workspace/ with the base checkpoint's verified
// contents and records the outcome. It fails the run closed on any restore
// error. The outcome record is best-effort observability — the run's fate is
// decided by whether the extraction succeeded, not by whether the server heard
// about it — so a failure to record is logged, not fatal, the same as a trace.
func restoreWorkspaceBase(ctx context.Context, input RunTaskInput, taskRunID string, dirs runDirs, spaceID string, base *workerclient.WorkspaceBaseResponse) error {
	if err := extractBaseIntoWorkspace(ctx, input.Checkpoints, dirs, spaceID, base); err != nil {
		if recErr := workerclient.RecordWorkspaceRestore(ctx, input.WorkerAPI, taskRunID, workerclient.WorkspaceRestoreRequest{
			Status: string(coretask.WorkspaceRestoreFailed),
			Error:  boundedRestoreError(err),
		}); recErr != nil {
			componentLog().Error("could not record a failed workspace restore", "task_run_id", taskRunID, "err", recErr)
		}
		return fmt.Errorf("restore workspace base: %w", err)
	}
	if err := workerclient.RecordWorkspaceRestore(ctx, input.WorkerAPI, taskRunID, workerclient.WorkspaceRestoreRequest{
		Status: string(coretask.WorkspaceRestoreRestored),
	}); err != nil {
		componentLog().Error("could not record a restored workspace", "task_run_id", taskRunID, "err", err)
	}
	return nil
}

// extractBaseIntoWorkspace downloads the base payload, verifies its digest
// covers the exact bytes read, and swaps the extracted tree into workspace/. It
// extracts into staging and swaps only after the digest checks out, so the Agent
// never sees a partial or corrupt workspace — object-store corruption fails the
// restore visibly rather than executing against a wrong tree (§13).
func extractBaseIntoWorkspace(ctx context.Context, store CheckpointPayloadStore, dirs runDirs, spaceID string, base *workerclient.WorkspaceBaseResponse) error {
	if base.PayloadFormat != wsarchive.PayloadFormat {
		return fmt.Errorf("unsupported base payload format %q", base.PayloadFormat)
	}
	key, err := store.Key(spaceID, base.PayloadSHA256)
	if err != nil {
		return fmt.Errorf("derive base payload key: %w", err)
	}
	rc, _, err := store.Open(ctx, key)
	if err != nil {
		return fmt.Errorf("open base payload: %w", err)
	}
	defer func() { _ = rc.Close() }()

	staging := filepath.Join(dirs.runDir, "restore-staging")
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("clear restore staging: %w", err)
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return fmt.Errorf("create restore staging: %w", err)
	}

	// Hash every byte read, not only what the extractor consumed: draining after
	// extraction covers the compressed frame's tail so the digest is over the
	// whole stored payload, matching how the seed was digested (§8).
	hash := sha256.New()
	tee := io.TeeReader(rc, hash)
	if _, err := wsarchive.Extract(tee, staging, checkpointLimits); err != nil {
		return fmt.Errorf("extract base payload: %w", err)
	}
	if _, err := io.Copy(io.Discard, tee); err != nil {
		return fmt.Errorf("read base payload: %w", err)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != base.PayloadSHA256 {
		return fmt.Errorf("base payload digest mismatch: read %s, want %s", got, base.PayloadSHA256)
	}

	// Swap the verified tree into place. workspace/ exists and is empty here
	// (the base path does not materialize the space snapshot), and staging is on
	// the same filesystem, so the rename is atomic.
	if err := os.RemoveAll(dirs.runWorkspace); err != nil {
		return fmt.Errorf("clear workspace before restore: %w", err)
	}
	if err := os.Rename(staging, dirs.runWorkspace); err != nil {
		return fmt.Errorf("swap restored workspace into place: %w", err)
	}
	return nil
}

// boundedRestoreError trims a restore failure to a length fit for an operator-
// facing status field, keeping the leading cause.
func boundedRestoreError(err error) string {
	const max = 500
	msg := err.Error()
	if len(msg) > max {
		return msg[:max]
	}
	return msg
}

// captureAndFinalizeSeed archives workspaceDir, uploads the bytes, and records
// the pointer, in that order — the commit protocol's bytes-before-pointer rule,
// so a recorded checkpoint never points at bytes that are not there (§8).
func captureAndFinalizeSeed(ctx context.Context, store CheckpointPayloadStore, cfg workerclient.WorkerAPIClientConfig, stagingDir, workspaceDir, spaceID, taskRunID string) error {
	desc, err := archiveWorkspace(ctx, store, stagingDir, workspaceDir, spaceID)
	if err != nil {
		return err
	}
	// The seed body and the result descriptor are the same five fields; a seed
	// just finalizes through its own call instead of riding a terminal report.
	if _, err := workerclient.FinalizeSeedCheckpoint(ctx, cfg, taskRunID, workerclient.SeedCheckpointRequest(desc)); err != nil {
		return fmt.Errorf("finalize seed checkpoint: %w", err)
	}
	return nil
}

// archiveWorkspace archives workspaceDir to a bounded temporary file staged
// outside workspaceDir (so it is never part of what it captures, §12.1), teeing
// through a hash so the digest covers the exact stored bytes (§8), uploads the
// bytes to their content-addressed key, and returns the payload descriptor.
func archiveWorkspace(ctx context.Context, store CheckpointPayloadStore, stagingDir, workspaceDir, spaceID string) (workerclient.WorkspaceCheckpointDescriptor, error) {
	var desc workerclient.WorkspaceCheckpointDescriptor
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return desc, fmt.Errorf("create checkpoint staging dir: %w", err)
	}
	tmp, err := os.CreateTemp(stagingDir, "ckpt-*.tar.zst")
	if err != nil {
		return desc, fmt.Errorf("create checkpoint archive: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	hash := sha256.New()
	res, createErr := wsarchive.Create(io.MultiWriter(tmp, hash), workspaceDir, checkpointLimits)
	closeErr := tmp.Close()
	if createErr != nil {
		return desc, fmt.Errorf("capture checkpoint archive: %w", createErr)
	}
	if closeErr != nil {
		return desc, fmt.Errorf("finish checkpoint archive: %w", closeErr)
	}
	info, err := os.Stat(tmpName)
	if err != nil {
		return desc, fmt.Errorf("stat checkpoint archive: %w", err)
	}
	sha := hex.EncodeToString(hash.Sum(nil))

	f, err := os.Open(tmpName)
	if err != nil {
		return desc, fmt.Errorf("open checkpoint archive: %w", err)
	}
	_, putErr := store.Put(ctx, spaceID, sha, f)
	_ = f.Close()
	if putErr != nil {
		return desc, fmt.Errorf("upload checkpoint payload to object storage: %w", putErr)
	}
	return workerclient.WorkspaceCheckpointDescriptor{
		PayloadFormat:     wsarchive.PayloadFormat,
		PayloadSHA256:     sha,
		SizeBytes:         info.Size(),
		UncompressedBytes: res.UncompressedBytes,
		EntryCount:        res.EntryCount,
	}, nil
}

// captureWorkspaceCheckpoint archives the run's workspace and returns the
// descriptor to carry on the terminal report, for either a successful result or
// a failed run's partial — the server decides the kind from the run's terminal
// status. It is fail-open: it returns nil when this deployment does not
// checkpoint or when capture fails, because a checkpoint it could not store must
// not change the run's outcome (§13). A nil descriptor simply commits no
// checkpoint and leaves the Task head where it was.
func captureWorkspaceCheckpoint(ctx context.Context, input RunTaskInput, task *coretask.Task, dirs runDirs) *workerclient.WorkspaceCheckpointDescriptor {
	if input.Checkpoints == nil || input.WorkerAPI.BaseURL == "" || input.WorkerAPI.Token == "" {
		return nil
	}
	stagingDir := filepath.Join(dirs.runDir, "checkpoint-staging")
	desc, err := archiveWorkspace(ctx, input.Checkpoints, stagingDir, dirs.runWorkspace, task.SpaceID)
	if err != nil {
		componentLog().Error("failed to capture the workspace checkpoint; run outcome stands", "space_id", task.SpaceID, "err", err)
		return nil
	}
	return &desc
}
