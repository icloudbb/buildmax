package workerclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/infra/httpclient"
	"github.com/icloudbb/buildmax/internal/infra/pluginwire"
)

// StreamSender sends run output deltas to the server (e.g. for live streaming). Optional; when nil, run output is not streamed.
// Flush sends any buffered data; call when the stream ends so the last chunk is not lost.
type StreamSender interface {
	SendDelta(ctx context.Context, taskRunID, delta string) error
	Flush(ctx context.Context, taskRunID string) error
}

// ErrTaskRunAlreadyClaimed is returned when the server responds 409 to PATCH RUNNING (run not SCHEDULED or already RUNNING).
var ErrTaskRunAlreadyClaimed = errors.New("task run already claimed or not scheduled")

// WorkerAPIClientConfig holds base URL, token, and HTTP client for worker API calls.
type WorkerAPIClientConfig struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

// workerDo performs an HTTP request to the worker API (URL build, Bearer auth, default client). Caller must close resp.Body.
func workerDo(ctx context.Context, cfg WorkerAPIClientConfig, method, pathSuffix string, body []byte) (*http.Response, error) {
	url := cfg.BaseURL + pathSuffix
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	} else {
		req, err = http.NewRequestWithContext(ctx, method, url, nil)
	}
	if err != nil {
		return nil, err
	}
	if body != nil && (method == http.MethodPatch || method == http.MethodPost) {
		req.Header.Set("Content-Type", "application/json")
	}
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}
	client := cfg.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// WorkerTaskRun is everything the server tells a worker about the run it is
// about to execute.
type WorkerTaskRun struct {
	Run  *coretask.Run
	Task *coretask.Task
	// LLM is how this run reaches a model. Nil means direct, which is what a
	// server that has not enabled managed worker inference reports.
	LLM *TaskRunLLM
	// AgentInstructions is the text appended to the run's system prompt, resolved by the
	// server from the agent the task names. Empty when the task names none. It is not on
	// coretask.Task because it is not a property of the task: it is resolved per run, so an
	// edited definition applies to the next one.
	AgentInstructions      string
	SpaceAgentInstructions string
	// CancelRequested is true when the run was already asked to stop before
	// this worker picked it up — a cancel that landed between dispatch and
	// start. Such a run is finished without executing anything.
	CancelRequested bool
	// Plugins are the releases this run materializes, resolved by the server.
	// A worker fetches and verifies exactly these and never resolves its own.
	Plugins []coreplugin.Pin
	// PluginError is why this run cannot proceed. A worker that receives one
	// fails the run rather than starting it without the plugin: an agent that
	// names a plugin has declared it needs one.
	PluginError string
	// SandboxNetworkTier and SandboxFilesystemTier are this run's agent-
	// declared sandbox tiers, resolved by the server. Empty means the
	// strictest tier on that axis. See docs/design/agent-sandbox-policy.md.
	SandboxNetworkTier    string
	SandboxFilesystemTier string
}

// GetWorkerTaskRun fetches the run from the server (GET /api/worker/task-runs/{task_run_id}). Returns nil, nil if not found.
func GetWorkerTaskRun(ctx context.Context, cfg WorkerAPIClientConfig, taskRunID string) (*WorkerTaskRun, error) {
	pathSuffix := "/api/worker/task-runs/" + taskRunID
	resp, err := workerDo(ctx, cfg, http.MethodGet, pathSuffix, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, httpclient.DecodeError(resp, "worker API GET "+cfg.BaseURL+pathSuffix)
	}
	var got GetTaskRunResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		return nil, err
	}
	return &WorkerTaskRun{
		Run: &coretask.Run{
			ID:                got.Run.ID,
			TaskID:            got.Run.TaskID,
			PreviousTaskRunID: got.Run.PreviousTaskRunID,
			Input:             got.Run.Input,
			Status:            got.Run.Status,
			CreatedAt:         got.Run.CreatedAt,
		},
		Task: &coretask.Task{
			ID:             got.Task.ID,
			ConversationID: got.Task.ConversationID,
			SpaceID:        got.Task.SpaceID,
			CreatedBy:      got.Task.UserID,
			SessionID:      got.Task.SessionID,
		},
		LLM:                    got.LLM,
		AgentInstructions:      got.Task.AgentInstructions,
		SpaceAgentInstructions: got.Task.SpaceAgentInstructions,
		CancelRequested:        got.Run.CancelRequested,
		Plugins:                toPluginPins(got.Plugins),
		PluginError:            got.PluginError,
		SandboxNetworkTier:     sandboxNetworkTierOf(got.Sandbox),
		SandboxFilesystemTier:  sandboxFilesystemTierOf(got.Sandbox),
	}, nil
}

// The starting fetch's retry schedule: the first wait, its cap as it doubles,
// and the total a worker spends before giving up. Two minutes rides out a
// database failover without holding a dispatch slot through a real outage.
// Variables so a test does not wait in real time.
var (
	startFetchRetryDelay    = time.Second
	startFetchRetryMaxDelay = 15 * time.Second
	startFetchRetryBudget   = 2 * time.Minute
)

// GetWorkerTaskRunToStart is GetWorkerTaskRun for a worker about to start its
// run. A 503 means the server cannot hand the run out yet — typically it
// cannot confirm the initiator may still run work — so it is retried with
// backoff within a bounded budget, letting a transient outage delay the run
// rather than fail it. Any other outcome returns at once.
func GetWorkerTaskRunToStart(ctx context.Context, cfg WorkerAPIClientConfig, taskRunID string) (*WorkerTaskRun, error) {
	deadline := time.Now().Add(startFetchRetryBudget)
	delay := startFetchRetryDelay
	for {
		run, err := GetWorkerTaskRun(ctx, cfg, taskRunID)
		var httpErr *httpclient.Error
		if err == nil || !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusServiceUnavailable {
			return run, err
		}
		if time.Now().Add(delay).After(deadline) {
			return nil, err
		}
		slog.Warn("server cannot hand out this run yet; retrying", "task_run_id", taskRunID, "retry_in", delay, "err", err)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		delay = min(delay*2, startFetchRetryMaxDelay)
	}
}

// GetWorkerTaskRunSecrets fetches the run's resolved Secret env grants. An
// empty map (or a 404 from a server built before this route) means the run's
// agent consumes no Secret. A non-2xx means the server could not produce a
// required grant, which the caller surfaces as a run failure -- a run must not
// proceed without a credential its definition declared.
func GetWorkerTaskRunSecrets(ctx context.Context, cfg WorkerAPIClientConfig, taskRunID string) (map[string]string, error) {
	pathSuffix := "/api/worker/task-runs/" + taskRunID + "/secrets"
	resp, err := workerDo(ctx, cfg, http.MethodGet, pathSuffix, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, httpclient.DecodeError(resp, "worker API GET "+cfg.BaseURL+pathSuffix)
	}
	var got TaskRunSecretsResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		return nil, err
	}
	return got.Env, nil
}

// sandboxNetworkTierOf and sandboxFilesystemTierOf read the GET response's
// optional Sandbox field. Absent means a server built before this field
// existed, so a worker reads both tiers as "" -- the strictest, exactly what
// it applied before the field existed.
func sandboxNetworkTierOf(s *TaskRunSandbox) string {
	if s == nil {
		return ""
	}
	return s.NetworkTier
}

func sandboxFilesystemTierOf(s *TaskRunSandbox) string {
	if s == nil {
		return ""
	}
	return s.FilesystemTier
}

func toPluginPins(wire []TaskRunPlugin) []coreplugin.Pin {
	if len(wire) == 0 {
		return nil
	}
	out := make([]coreplugin.Pin, 0, len(wire))
	for _, p := range wire {
		out = append(out, coreplugin.Pin{PluginName: p.Name, Version: p.Version, Digest: p.Digest})
	}
	return out
}

// DownloadPluginPackage streams one pinned release into w and returns the
// digest the server sent with it.
//
// The route is scoped to this run and serves only the releases its own pins
// name, so a worker cannot reach the catalog or another run's packages.
func DownloadPluginPackage(ctx context.Context, cfg WorkerAPIClientConfig, taskRunID, name, version string, w io.Writer) (string, error) {
	pathSuffix := fmt.Sprintf("/api/worker/task-runs/%s/plugins/%s/%s/download",
		taskRunID, url.PathEscape(name), url.PathEscape(version))
	resp, err := workerDo(ctx, cfg, http.MethodGet, pathSuffix, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", httpclient.DecodeError(resp, "worker API GET "+cfg.BaseURL+pathSuffix)
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return "", fmt.Errorf("read package: %w", err)
	}
	return resp.Header.Get(pluginwire.DigestHeader), nil
}

// WorkerHTTPUpdater implements TaskRunUpdater by calling the server's worker API (PATCH /api/worker/task-runs/{task_run_id}).
type WorkerHTTPUpdater struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

// UpdateRunStatus sends PATCH to the server to update run status and optional fields.
func (u *WorkerHTTPUpdater) UpdateRunStatus(ctx context.Context, taskRunID string, req *PatchTaskRunRequest) error {
	if req == nil {
		req = &PatchTaskRunRequest{}
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return err
	}
	cfg := WorkerAPIClientConfig{BaseURL: u.BaseURL, Token: u.Token, Client: u.Client}
	pathSuffix := "/api/worker/task-runs/" + taskRunID
	resp, err := workerDo(ctx, cfg, http.MethodPatch, pathSuffix, raw)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusConflict {
		return ErrTaskRunAlreadyClaimed
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return httpclient.DecodeError(resp, "worker API PATCH "+cfg.BaseURL+pathSuffix)
	}
	return nil
}

// WorkerHTTPStreamSender implements StreamSender by POSTing each delta to the server's worker stream endpoint.
type WorkerHTTPStreamSender struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

// SendDelta POSTs the delta to POST /api/worker/task-runs/{task_run_id}/stream.
func (u *WorkerHTTPStreamSender) SendDelta(ctx context.Context, taskRunID, delta string) error {
	if taskRunID == "" {
		return nil
	}
	body := StreamDeltaRequest{Delta: delta}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	cfg := WorkerAPIClientConfig{BaseURL: u.BaseURL, Token: u.Token, Client: u.Client}
	pathSuffix := "/api/worker/task-runs/" + taskRunID + "/stream"
	resp, err := workerDo(ctx, cfg, http.MethodPost, pathSuffix, raw)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return httpclient.DecodeError(resp, "worker API POST stream "+cfg.BaseURL+pathSuffix)
	}
	return nil
}

// Flush is a no-op; WorkerHTTPStreamSender sends each delta immediately.
func (u *WorkerHTTPStreamSender) Flush(ctx context.Context, taskRunID string) error {
	return nil
}

// DebouncedStreamSender wraps a StreamSender and buffers deltas, flushing after an interval or when buffer size is reached.
// Reduces server API pressure and gives a smoother typewriter-like experience on the client.
const (
	DebounceIntervalMs = 80  // flush at most every 80ms when there is buffered data
	DebounceMaxBytes   = 512 // flush when buffer reaches 512 bytes
)

// DebouncedStreamSender implements StreamSender with debouncing.
type DebouncedStreamSender struct {
	Inner      StreamSender
	IntervalMs int // override default when > 0
	MaxBytes   int // override default when > 0

	mu               sync.Mutex
	buf              strings.Builder
	currentTaskRunID string
	timer            *time.Timer
}

func (d *DebouncedStreamSender) SendDelta(ctx context.Context, taskRunID, delta string) error {
	return d.sendOrFlush(ctx, taskRunID, delta, false)
}

func (d *DebouncedStreamSender) Flush(ctx context.Context, taskRunID string) error {
	return d.sendOrFlush(ctx, taskRunID, "", true)
}

func (d *DebouncedStreamSender) sendOrFlush(ctx context.Context, taskRunID, delta string, forceFlush bool) error {
	intervalMs := d.IntervalMs
	if intervalMs <= 0 {
		intervalMs = DebounceIntervalMs
	}
	maxBytes := d.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DebounceMaxBytes
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if forceFlush {
		d.flushLocked(ctx)
		return nil
	}

	if delta == "" {
		return nil
	}
	if d.buf.Len() == 0 {
		d.currentTaskRunID = taskRunID
	}
	d.buf.WriteString(delta)

	if d.buf.Len() >= maxBytes {
		d.flushLocked(ctx)
		return nil
	}

	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	d.timer = time.AfterFunc(time.Duration(intervalMs)*time.Millisecond, func() {
		d.mu.Lock()
		d.flushLocked(context.Background())
		d.mu.Unlock()
	})
	return nil
}

// flushLocked sends the current buffer via Inner and clears it. Caller must hold d.mu.
func (d *DebouncedStreamSender) flushLocked(ctx context.Context) {
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	if d.buf.Len() == 0 {
		return
	}
	payload := d.buf.String()
	runID := d.currentTaskRunID
	d.buf.Reset()
	d.mu.Unlock()
	defer d.mu.Lock()
	_ = d.Inner.SendDelta(ctx, runID, payload)
}
