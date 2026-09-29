package workerclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/icloudbb/buildmax/internal/infra/httpclient"
)

// ErrWorkspaceCheckpointsUnsupported reports that the server this run reached
// does not run the workspace-checkpoint contract: it has no such route (404) or
// has it but no checkpoint storage configured (503). It is not a failure of the
// run — an evaluation control plane and a deployment with checkpoints turned off
// both answer this way, and such a run simply seeds and restores nothing. A real
// server that supports checkpoints always answers a valid run with 204 or 200,
// never 404, because the route is registered unconditionally.
var ErrWorkspaceCheckpointsUnsupported = errors.New("worker: server does not support workspace checkpoints")

// WorkspaceBaseResponse is the checkpoint a run restores its workspace from. It
// carries no storage key: the worker addresses the payload from its own space
// and this digest, the same content-addressed key the server would compute.
type WorkspaceBaseResponse struct {
	CheckpointID      string `json:"checkpoint_id"`
	PayloadFormat     string `json:"payload_format"`
	PayloadSHA256     string `json:"payload_sha256"`
	SizeBytes         int64  `json:"size_bytes"`
	UncompressedBytes int64  `json:"uncompressed_bytes"`
	EntryCount        int64  `json:"entry_count"`
}

// WorkspaceRestoreRequest records how a run's base restoration ended. Status is
// "restored" or "failed"; Error is bounded operator text, set only on failure.
type WorkspaceRestoreRequest struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// SeedCheckpointRequest is the descriptor of a seed payload the worker has
// already uploaded to the object store. The server derives space, task, and run
// from the run token; the worker never sends owner ids.
type SeedCheckpointRequest struct {
	PayloadFormat     string `json:"payload_format"`
	PayloadSHA256     string `json:"payload_sha256"`
	SizeBytes         int64  `json:"size_bytes"`
	UncompressedBytes int64  `json:"uncompressed_bytes"`
	EntryCount        int64  `json:"entry_count"`
}

// SeedCheckpointResponse returns the committed checkpoint's public identity.
type SeedCheckpointResponse struct {
	CheckpointID string `json:"checkpoint_id"`
}

// GetWorkspaceBase fetches the run's base checkpoint descriptor, or (nil, nil)
// when the run has none — the first run of a Task, which seeds instead. A server
// that does not run the checkpoint contract at all returns
// ErrWorkspaceCheckpointsUnsupported (404 for no route, 503 for no storage), so
// the caller can distinguish "no base yet" from "no checkpoints here".
func GetWorkspaceBase(ctx context.Context, cfg WorkerAPIClientConfig, taskRunID string) (*WorkspaceBaseResponse, error) {
	pathSuffix := "/api/worker/task-runs/" + taskRunID + "/workspace-base"
	resp, err := workerDo(ctx, cfg, http.MethodGet, pathSuffix, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusServiceUnavailable {
		return nil, ErrWorkspaceCheckpointsUnsupported
	}
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, httpclient.DecodeError(resp, "worker API GET "+pathSuffix)
	}
	var got WorkspaceBaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		return nil, err
	}
	return &got, nil
}

// RecordWorkspaceRestore reports the outcome of restoring the run's base.
func RecordWorkspaceRestore(ctx context.Context, cfg WorkerAPIClientConfig, taskRunID string, req WorkspaceRestoreRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	pathSuffix := "/api/worker/task-runs/" + taskRunID + "/workspace-restore"
	resp, err := workerDo(ctx, cfg, http.MethodPost, pathSuffix, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return httpclient.DecodeError(resp, "worker API POST "+pathSuffix)
	}
	return nil
}

// FinalizeSeedCheckpoint records a seed the worker captured and uploaded, and
// returns its committed identity. It is idempotent for identical bytes.
func FinalizeSeedCheckpoint(ctx context.Context, cfg WorkerAPIClientConfig, taskRunID string, req SeedCheckpointRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	pathSuffix := "/api/worker/task-runs/" + taskRunID + "/workspace-checkpoints"
	resp, err := workerDo(ctx, cfg, http.MethodPost, pathSuffix, body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", httpclient.DecodeError(resp, "worker API POST "+pathSuffix)
	}
	var got SeedCheckpointResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		return "", err
	}
	return got.CheckpointID, nil
}
