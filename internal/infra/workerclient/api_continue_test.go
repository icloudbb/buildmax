package workerclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/infra/httpclient"
)

// fastStartFetchRetries shrinks the starting fetch's retry schedule for a test.
func fastStartFetchRetries(t *testing.T, budget time.Duration) {
	t.Helper()
	delay, maxDelay, oldBudget := startFetchRetryDelay, startFetchRetryMaxDelay, startFetchRetryBudget
	startFetchRetryDelay, startFetchRetryMaxDelay, startFetchRetryBudget = time.Millisecond, 2*time.Millisecond, budget
	t.Cleanup(func() {
		startFetchRetryDelay, startFetchRetryMaxDelay, startFetchRetryBudget = delay, maxDelay, oldBudget
	})
}

// A server that cannot yet confirm the run may start answers 503. A starting
// worker waits that out rather than failing the run, and gives up only after
// its budget; any other refusal is not retried.
func TestGetWorkerTaskRunToStartRetriesServiceUnavailable(t *testing.T) {
	serve := func(unavailableFor int, status int) (*httptest.Server, *atomic.Int32) {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if int(calls.Add(1)) <= unavailableFor {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"could not verify that this run's initiator may still run work; retry shortly"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(GetTaskRunResponse{
				Run:  TaskRunRun{ID: "run-1", TaskID: "task-1", Status: "SCHEDULED"},
				Task: TaskRunTask{ID: "task-1", SpaceID: "space-1", UserID: "user-1"},
			})
		}))
		t.Cleanup(server.Close)
		return server, &calls
	}

	t.Run("recovers", func(t *testing.T) {
		fastStartFetchRetries(t, time.Minute)
		server, calls := serve(2, http.StatusServiceUnavailable)
		got, err := GetWorkerTaskRunToStart(context.Background(), WorkerAPIClientConfig{BaseURL: server.URL}, "run-1")
		if err != nil {
			t.Fatalf("GetWorkerTaskRunToStart: %v", err)
		}
		if got == nil || got.Run.ID != "run-1" {
			t.Fatalf("run = %+v, want run-1", got)
		}
		if calls.Load() != 3 {
			t.Errorf("calls = %d, want 3", calls.Load())
		}
	})

	t.Run("gives up after the budget", func(t *testing.T) {
		fastStartFetchRetries(t, 20*time.Millisecond)
		server, _ := serve(1<<30, http.StatusServiceUnavailable)
		_, err := GetWorkerTaskRunToStart(context.Background(), WorkerAPIClientConfig{BaseURL: server.URL}, "run-1")
		var httpErr *httpclient.Error
		if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("err = %v, want the 503", err)
		}
	})

	t.Run("does not retry other failures", func(t *testing.T) {
		fastStartFetchRetries(t, time.Minute)
		server, calls := serve(1<<30, http.StatusInternalServerError)
		if _, err := GetWorkerTaskRunToStart(context.Background(), WorkerAPIClientConfig{BaseURL: server.URL}, "run-1"); err == nil {
			t.Fatal("want an error for a 500")
		}
		if calls.Load() != 1 {
			t.Errorf("calls = %d, want 1", calls.Load())
		}
	})
}

// A Task already projects the current run when its worker starts. The client
// must preserve the immutable predecessor carried by the run or session
// restoration has no source to fetch.
func TestGetWorkerTaskRunPreservesSessionPredecessor(t *testing.T) {
	previousRunID := "run-previous"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/worker/task-runs/run-current" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if err := json.NewEncoder(w).Encode(GetTaskRunResponse{
			Run: TaskRunRun{
				ID: "run-current", TaskID: "task-1", PreviousTaskRunID: &previousRunID,
				Input: "continue", Status: "SCHEDULED", CreatedAt: time.Unix(1, 0).UTC(),
			},
			Task: TaskRunTask{ID: "task-1", SpaceID: "space-1", UserID: "user-1"},
		}); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	got, err := GetWorkerTaskRun(context.Background(), WorkerAPIClientConfig{BaseURL: server.URL}, "run-current")
	if err != nil {
		t.Fatalf("GetWorkerTaskRun: %v", err)
	}
	if got.Run.PreviousTaskRunID == nil || *got.Run.PreviousTaskRunID != previousRunID {
		t.Errorf("previous_task_run_id = %v, want %q", got.Run.PreviousTaskRunID, previousRunID)
	}
}
