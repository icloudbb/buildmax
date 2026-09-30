package workerclient

import (
	"context"
	"encoding/json"
	"github.com/icloudbb/buildmax/internal/infra/httpclient"
	"log/slog"
	"net/http"
	"time"
)

// DefaultCancelPollInterval is how often a worker asks whether its run has been
// canceled.
//
// It trades one small request per run against how long a user waits after
// pressing stop. Five seconds keeps the wait short enough to feel like an
// answer while costing a fraction of what the run's own inference calls do.
const DefaultCancelPollInterval = 5 * time.Second

// IsCancelRequested reports whether the server has recorded a cancel request
// for this run.
//
// A run that no longer exists reports false rather than an error: the caller is
// a running worker, and a missing run is not a reason to stop mid-turn.
func IsCancelRequested(ctx context.Context, cfg WorkerAPIClientConfig, taskRunID string) (bool, error) {
	pathSuffix := "/api/worker/task-runs/" + taskRunID
	resp, err := workerDo(ctx, cfg, http.MethodGet, pathSuffix, nil)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, httpclient.DecodeError(resp, "worker API GET "+pathSuffix)
	}
	var got GetTaskRunResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		return false, err
	}
	return got.Run.CancelRequested, nil
}

// WatchCancel polls until the run is canceled or ctx ends, then calls onCancel
// once and returns.
//
// Polling failures are logged and retried rather than treated as a cancel. A
// server the worker cannot reach is the same server that cannot have been told
// to stop this run, and ending a user's work on a network blip would destroy
// more than it protects. The run's own timeout is what bounds the other case.
//
// Use interval 0 for DefaultCancelPollInterval. Run it in a goroutine; it
// returns as soon as ctx is done.
func WatchCancel(ctx context.Context, cfg WorkerAPIClientConfig, taskRunID string, interval time.Duration, onCancel func()) {
	if taskRunID == "" || onCancel == nil {
		return
	}
	if interval <= 0 {
		interval = DefaultCancelPollInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			canceled, err := IsCancelRequested(ctx, cfg, taskRunID)
			if err != nil {
				if ctx.Err() == nil {
					cancelPollLog().Warn("cancel poll failed", "task_run_id", taskRunID, "err", err)
				}
				continue
			}
			if canceled {
				cancelPollLog().Info("cancel requested, stopping this run", "task_run_id", taskRunID)
				onCancel()
				return
			}
		}
	}
}

// Identity belongs in an attr, not in every message string.
func cancelPollLog() *slog.Logger { return slog.With("component", "worker_cancel_poll") }
