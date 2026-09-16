package worker

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	"github.com/icloudbb/buildmax/internal/core/eligibility"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/infra/workerclient"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/util"
)

func TestGetWorkerTaskRunHandler_RequiresWorkerAuth(t *testing.T) {
	taskRunID := "run-1"
	run := coretask.Run{ID: taskRunID, TaskID: "task-1", Input: "input", Status: "SCHEDULED", CreatedAt: time.Unix(1, 0).UTC()}
	task := coretask.Task{ID: "task-1", ConversationID: "conv-1", SpaceID: "tm_1", CreatedBy: "u1"}
	mockRun := &mock.MockTaskRunStore{Runs: []coretask.Run{run}, TaskList: []coretask.Task{task}}
	cfg := Config{
		JWTSecret: workerTestSecret,
		TaskRuns:  mockRun,
	}
	h := New(cfg)
	mux := http.NewServeMux()
	h.Register(mux)

	// Without token: 401
	req := httptest.NewRequest(http.MethodGet, "/api/worker/task-runs/"+taskRunID, nil)
	req.SetPathValue("task_run_id", taskRunID)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("without token: got status %d, want 401", w.Code)
	}

	// With correct token: 200 and run+chat body
	req = httptest.NewRequest(http.MethodGet, "/api/worker/task-runs/"+taskRunID, nil)
	req.SetPathValue("task_run_id", taskRunID)
	req.Header.Set("Authorization", "Bearer "+runTokenFor(t, taskRunID, "task-1"))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("with token: got status %d, want 200", w.Code)
	}
	if body := w.Body.String(); body == "" || len(body) < 10 {
		t.Errorf("with token: body too short: %q", body)
	}
	if body := w.Body.String(); !strings.Contains(body, `"space_id":"tm_1"`) {
		t.Errorf("with token: body missing space_id: %q", body)
	}
}

// The worker polls this route while it executes, so the run's cancel request
// has to be visible in it. Nothing else can reach a started run.
func TestGetWorkerTaskRunHandler_ReportsACancelRequest(t *testing.T) {
	taskRunID := "run-cancel"
	askedAt := time.Unix(1_800_000_000, 0).UTC()
	runs := &mock.MockTaskRunStore{
		Runs: []coretask.Run{{
			ID: taskRunID, TaskID: "task-1", Input: "input",
			Status: string(coretask.RunStatusRunning), CancelRequestedAt: &askedAt,
		}},
		TaskList: []coretask.Task{{ID: "task-1", ConversationID: "conv-1", SpaceID: "tm_1", CreatedBy: "u1"}},
	}
	h := New(Config{JWTSecret: workerTestSecret, TaskRuns: runs})
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/worker/task-runs/"+taskRunID, nil)
	req.Header.Set("Authorization", "Bearer "+runTokenFor(t, taskRunID, "task-1"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var got workerclient.GetTaskRunResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Run.CancelRequested {
		t.Errorf("cancel_requested = false for a run that was asked to stop; body = %s", w.Body.String())
	}
}

// A worker fetching its run re-checks the initiator's authority: if the account
// was disabled or removed from the Space after dispatch, the run is asked to
// stop at fetch, with the reason recorded, so it never begins executing.
func TestGetWorkerTaskRunHandler_ReChecksEligibilityAtFetch(t *testing.T) {
	const taskRunID, space = "run-elig", "tm_1"
	disabledAt := time.Unix(1, 0).UTC()
	runs := &mock.MockTaskRunStore{
		Runs: []coretask.Run{{
			ID: taskRunID, TaskID: "task-1", Input: "input",
			Status: string(coretask.RunStatusScheduled), CreatedBy: "u_gone",
		}},
		TaskList: []coretask.Task{{ID: "task-1", SpaceID: space, CreatedBy: "u_gone"}},
	}
	users := &mock.MockUserStore{ByID: map[string]*coreidentity.User{
		"u_gone": {ID: "u_gone", DisabledAt: &disabledAt},
	}}
	spaces := &mock.MockSpaceStore{Members: []corespace.Member{
		{SpaceID: space, UserID: "u_gone", Role: corespace.RoleMember},
	}}
	h := New(Config{JWTSecret: workerTestSecret, TaskRuns: runs, Eligible: eligibility.New(users, spaces)})
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/worker/task-runs/"+taskRunID, nil)
	req.Header.Set("Authorization", "Bearer "+runTokenFor(t, taskRunID, "task-1"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var got workerclient.GetTaskRunResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Run.CancelRequested {
		t.Errorf("cancel_requested = false for a disabled initiator; body = %s", w.Body.String())
	}
	if runs.Runs[0].CancelReason != coretask.CancelReasonCreatorDisabled {
		t.Errorf("cancel_reason = %q, want creator_disabled", runs.Runs[0].CancelReason)
	}
}

// The Task projection already points at the run being claimed. Continuity must
// therefore travel on the run's immutable predecessor rather than task.last_run_id.
func TestGetWorkerTaskRunHandler_ReportsSessionPredecessor(t *testing.T) {
	taskRunID, previousRunID := "run-current", "run-previous"
	runs := &mock.MockTaskRunStore{
		Runs: []coretask.Run{{
			ID: taskRunID, TaskID: "task-1", Input: "continue",
			Status: string(coretask.RunStatusScheduled), PreviousTaskRunID: &previousRunID,
		}},
		TaskList: []coretask.Task{{
			ID: "task-1", SpaceID: "tm_1", CreatedBy: "u1", LastRunID: &taskRunID,
		}},
	}
	h := New(Config{JWTSecret: workerTestSecret, TaskRuns: runs})
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/worker/task-runs/"+taskRunID, nil)
	req.Header.Set("Authorization", "Bearer "+runTokenFor(t, taskRunID, "task-1"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var got workerclient.GetTaskRunResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Run.PreviousTaskRunID == nil || *got.Run.PreviousTaskRunID != previousRunID {
		t.Errorf("previous_task_run_id = %v, want %q", got.Run.PreviousTaskRunID, previousRunID)
	}
	if strings.Contains(w.Body.String(), "last_run_id") {
		t.Errorf("worker response exposed mutable task.last_run_id: %s", w.Body.String())
	}
}

// A canceled run is registered like a finished one: its reply is kept and its
// task follows it out of "running". Losing either would make cancelling cost
// more than waiting.
func TestPatchWorkerTaskRun_CanceledKeepsReplyAndSyncsTheTask(t *testing.T) {
	taskRunID := "run-canceled"
	runs := &mock.MockTaskRunStore{
		Runs:     []coretask.Run{{ID: taskRunID, TaskID: "task-1", Status: string(coretask.RunStatusRunning)}},
		TaskList: []coretask.Task{{ID: "task-1", ConversationID: "conv-1", SpaceID: "tm_1", CreatedBy: "u1", Status: string(coretask.RunStatusRunning)}},
	}
	h := New(Config{JWTSecret: workerTestSecret, TaskRuns: runs})
	mux := http.NewServeMux()
	h.Register(mux)

	endedAt := time.Unix(1_800_000_010, 0).UTC()
	body, err := json.Marshal(workerclient.PatchTaskRunRequest{
		Status:  string(coretask.RunStatusCanceled),
		EndedAt: &endedAt,
		Output:  util.Ptr("as far as I got"),
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/worker/task-runs/"+taskRunID, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+runTokenFor(t, taskRunID, "task-1"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	if runs.Runs[0].Status != string(coretask.RunStatusCanceled) {
		t.Errorf("run status = %q, want CANCELED", runs.Runs[0].Status)
	}
	if runs.TaskList[0].Status != string(coretask.RunStatusCanceled) {
		t.Errorf("task status = %q, want CANCELED", runs.TaskList[0].Status)
	}
	if runs.Runs[0].Output == nil || *runs.Runs[0].Output != "as far as I got" {
		t.Errorf("run output = %v, want the partial reply the run produced", runs.Runs[0].Output)
	}
}

// A run interrupted by its worker shutting down reports FAILED, because nothing
// chose to stop it and it did not finish — but it produced real work first, and
// the status must not be what decides whether that work is kept.
func TestPatchWorkerTaskRun_InterruptedFailedKeepsReply(t *testing.T) {
	taskRunID := "run-interrupted"
	runs := &mock.MockTaskRunStore{
		Runs:     []coretask.Run{{ID: taskRunID, TaskID: "task-1", Status: string(coretask.RunStatusRunning)}},
		TaskList: []coretask.Task{{ID: "task-1", ConversationID: "conv-1", SpaceID: "tm_1", CreatedBy: "u1", Status: string(coretask.RunStatusRunning)}},
	}
	h := New(Config{JWTSecret: workerTestSecret, TaskRuns: runs})
	mux := http.NewServeMux()
	h.Register(mux)

	endedAt := time.Unix(1_800_000_010, 0).UTC()
	body, err := json.Marshal(workerclient.PatchTaskRunRequest{
		Status:       string(coretask.RunStatusFailed),
		EndedAt:      &endedAt,
		Output:       util.Ptr("as far as I got"),
		ErrorMessage: util.Ptr(coretask.ErrRunInterrupted.Error()),
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/worker/task-runs/"+taskRunID, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+runTokenFor(t, taskRunID, "task-1"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	if runs.Runs[0].Status != string(coretask.RunStatusFailed) {
		t.Errorf("run status = %q, want FAILED", runs.Runs[0].Status)
	}
	if runs.TaskList[0].Status != string(coretask.RunStatusFailed) {
		t.Errorf("task status = %q, want FAILED", runs.TaskList[0].Status)
	}
	if runs.Runs[0].Output == nil || *runs.Runs[0].Output != "as far as I got" {
		t.Errorf("run output = %v, want the partial reply the run produced before it was stopped", runs.Runs[0].Output)
	}
}

// A run that failed at its own work still syncs its task to FAILED.
func TestPatchWorkerTaskRun_PlainFailureSyncsTheTask(t *testing.T) {
	taskRunID := "run-failed"
	runs := &mock.MockTaskRunStore{
		Runs:     []coretask.Run{{ID: taskRunID, TaskID: "task-1", Status: string(coretask.RunStatusRunning)}},
		TaskList: []coretask.Task{{ID: "task-1", ConversationID: "conv-1", SpaceID: "tm_1", CreatedBy: "u1", Status: string(coretask.RunStatusRunning)}},
	}
	h := New(Config{JWTSecret: workerTestSecret, TaskRuns: runs})
	mux := http.NewServeMux()
	h.Register(mux)

	body, err := json.Marshal(workerclient.PatchTaskRunRequest{
		Status:       string(coretask.RunStatusFailed),
		ErrorMessage: util.Ptr("the model refused"),
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/worker/task-runs/"+taskRunID, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+runTokenFor(t, taskRunID, "task-1"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	if runs.TaskList[0].Status != string(coretask.RunStatusFailed) {
		t.Errorf("task status = %q, want FAILED", runs.TaskList[0].Status)
	}
}

func TestGetWorkerTaskRunHandler_NotFound(t *testing.T) {
	cfg := Config{
		JWTSecret: workerTestSecret,
		TaskRuns:  &mock.MockTaskRunStore{Runs: []coretask.Run{}},
	}
	h := New(cfg)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/worker/task-runs/nonexistent", nil)
	req.SetPathValue("task_run_id", "nonexistent")
	req.Header.Set("Authorization", "Bearer "+runTokenFor(t, "nonexistent", "task-1"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("got status %d, want 404", w.Code)
	}
}

func TestPatchWorkerTaskRun_RUNNING_WhenScheduled_Returns200(t *testing.T) {
	taskRunID := "run-scheduled"
	run := coretask.Run{ID: taskRunID, TaskID: "task1", Input: "input", Status: "SCHEDULED", CreatedAt: time.Unix(1, 0).UTC()}
	task := coretask.Task{ID: "task1", ConversationID: "conv-1"}
	mockRun := &mock.MockTaskRunStore{Runs: []coretask.Run{run}, TaskList: []coretask.Task{task}}
	cfg := Config{
		JWTSecret: workerTestSecret,
		TaskRuns:  mockRun,
	}
	h := New(cfg)
	mux := http.NewServeMux()
	h.Register(mux)

	body := map[string]interface{}{"status": "RUNNING", "session_id": "sess-1", "started_at": time.Unix(123, 0).UTC().Format(time.RFC3339)}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPatch, "/api/worker/task-runs/"+taskRunID, bytes.NewReader(raw))
	req.SetPathValue("task_run_id", taskRunID)
	req.Header.Set("Authorization", "Bearer "+runTokenFor(t, taskRunID, "task1"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("PATCH RUNNING when SCHEDULED: got status %d, want 200", w.Code)
	}
	if len(mockRun.Runs) != 1 || mockRun.Runs[0].Status != "RUNNING" {
		t.Errorf("PATCH RUNNING when SCHEDULED: run status = %q, want RUNNING", mockRun.Runs[0].Status)
	}
}

func TestPatchWorkerTaskRun_RUNNING_WhenPending_Returns409(t *testing.T) {
	taskRunID := "run-pending"
	run := coretask.Run{ID: taskRunID, TaskID: "task1", Input: "input", Status: "PENDING", CreatedAt: time.Unix(1, 0).UTC()}
	task := coretask.Task{ID: "task1", ConversationID: "conv-1"}
	mockRun := &mock.MockTaskRunStore{Runs: []coretask.Run{run}, TaskList: []coretask.Task{task}}
	cfg := Config{
		JWTSecret: workerTestSecret,
		TaskRuns:  mockRun,
	}
	h := New(cfg)
	mux := http.NewServeMux()
	h.Register(mux)

	body := map[string]interface{}{"status": "RUNNING", "session_id": "sess-1"}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPatch, "/api/worker/task-runs/"+taskRunID, bytes.NewReader(raw))
	req.SetPathValue("task_run_id", taskRunID)
	req.Header.Set("Authorization", "Bearer "+runTokenFor(t, taskRunID, "task1"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("PATCH RUNNING when PENDING: got status %d, want 409", w.Code)
	}
	if len(mockRun.Runs) != 1 || mockRun.Runs[0].Status != "PENDING" {
		t.Errorf("PATCH RUNNING when PENDING: run status = %q, want PENDING (unchanged)", mockRun.Runs[0].Status)
	}
}

// TestGetWorkerTaskRunHandler_TellsTheRunHowToReachAModel covers the descriptor
// a worker uses to decide its transport. The server states it so the worker —
// which executes model-chosen code — does not choose its own model, and it
// states only the alias, because endpoint, upstream identifier, and credential
// stay on this side.
func TestGetWorkerTaskRunHandler_TellsTheRunHowToReachAModel(t *testing.T) {
	store := &mock.MockTaskRunStore{
		Runs:     []coretask.Run{{ID: "run-1", TaskID: "task-1", Input: "input", Status: "SCHEDULED", CreatedAt: time.Unix(1, 0).UTC()}},
		TaskList: []coretask.Task{{ID: "task-1", ConversationID: "conv-1", SpaceID: "tm_1", CreatedBy: "u1"}},
	}

	get := func(t *testing.T, cfg Config) workerclient.GetTaskRunResponse {
		t.Helper()
		mux := http.NewServeMux()
		New(cfg).Register(mux)
		req := httptest.NewRequest(http.MethodGet, "/api/worker/task-runs/run-1", nil)
		req.Header.Set("Authorization", "Bearer "+runTokenFor(t, "run-1", "task-1"))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
		}
		var got workerclient.GetTaskRunResponse
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return got
	}

	t.Run("managed", func(t *testing.T) {
		got := get(t, Config{
			JWTSecret: workerTestSecret,
			TaskRuns:  store,
			WorkerLLM: &workerclient.TaskRunLLM{Transport: "buildmax", Model: "Deep", ContextWindow: 128000},
		})
		if got.LLM == nil {
			t.Fatal("a managed deployment told the run nothing about models")
		}
		if got.LLM.Transport != "buildmax" || got.LLM.Model != "Deep" {
			t.Errorf("llm = %+v", *got.LLM)
		}
	})

	// Absent means direct, so a worker built before this field behaves as it
	// always did.
	t.Run("direct omits the field entirely", func(t *testing.T) {
		mux := http.NewServeMux()
		New(Config{JWTSecret: workerTestSecret, TaskRuns: store}).Register(mux)
		req := httptest.NewRequest(http.MethodGet, "/api/worker/task-runs/run-1", nil)
		req.Header.Set("Authorization", "Bearer "+runTokenFor(t, "run-1", "task-1"))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if strings.Contains(w.Body.String(), `"llm"`) {
			t.Errorf("a direct deployment sent an llm field: %s", w.Body.String())
		}
	})
}

// TestGetWorkerTaskRunHandler_AgentModelOverridesTheAlias covers per-agent model
// selection: a run whose agent names a model reaches that model, not the
// deployment default, while an agent that names none keeps the default. The
// override rides the same managed descriptor, so the transport is unchanged.
func TestGetWorkerTaskRunHandler_AgentModelOverridesTheAlias(t *testing.T) {
	get := func(t *testing.T, agentModel string) workerclient.GetTaskRunResponse {
		t.Helper()
		store := &mock.MockTaskRunStore{
			Runs:     []coretask.Run{{ID: "run-1", TaskID: "task-1", Input: "input", Status: "SCHEDULED", CreatedAt: time.Unix(1, 0).UTC()}},
			TaskList: []coretask.Task{{ID: "task-1", ConversationID: "conv-1", SpaceID: "tm_1", CreatedBy: "u1", AgentID: util.Ptr("a_1")}},
		}
		agents := &mock.MockAgentStore{Agents: []agentdef.Agent{{ID: "a_1", SpaceID: "tm_1", Name: "picker", Model: agentModel}}}
		mux := http.NewServeMux()
		New(Config{
			JWTSecret: workerTestSecret,
			TaskRuns:  store,
			Agents:    agents,
			WorkerLLM: &workerclient.TaskRunLLM{Transport: "buildmax", Model: "Deep", ContextWindow: 128000},
		}).Register(mux)
		req := httptest.NewRequest(http.MethodGet, "/api/worker/task-runs/run-1", nil)
		req.Header.Set("Authorization", "Bearer "+runTokenFor(t, "run-1", "task-1"))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
		}
		var got workerclient.GetTaskRunResponse
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return got
	}

	t.Run("agent names a model", func(t *testing.T) {
		got := get(t, "Fast")
		if got.LLM == nil || got.LLM.Transport != "buildmax" || got.LLM.Model != "Fast" {
			t.Errorf("llm = %+v, want transport buildmax model Fast", got.LLM)
		}
	})

	t.Run("agent names none keeps the deployment default", func(t *testing.T) {
		got := get(t, "")
		if got.LLM == nil || got.LLM.Model != "Deep" {
			t.Errorf("llm = %+v, want the deployment default Deep", got.LLM)
		}
	})
}

// An agent that declares neither tier inherits the space's default -- see
// docs/design/agent-sandbox-policy.md §9 M3. Once resolved, the tiers pin to
// the run so a later change to the space's default cannot alter a run already
// under way.
func TestGetWorkerTaskRunHandler_FallsBackToSpaceSandboxDefaults(t *testing.T) {
	runStore := &mock.MockTaskRunStore{
		Runs: []coretask.Run{{ID: "run-1", TaskID: "task-1", Input: "input", Status: "SCHEDULED", CreatedAt: time.Unix(1, 0).UTC()}},
		TaskList: []coretask.Task{
			{ID: "task-1", ConversationID: "conv-1", SpaceID: "tm_1", CreatedBy: "u1", AgentID: util.Ptr("a_1")},
		},
	}
	agentStore := &mock.MockAgentStore{
		Agents: []agentdef.Agent{{ID: "a_1", SpaceID: "tm_1", Name: "no-tiers"}},
	}
	spaceStore := &mock.MockSpaceStore{
		Spaces: []corespace.Space{{ID: "tm_1", DefaultSandboxNetworkTier: "registries", DefaultSandboxFilesystemTier: "workspace_plus_shared_read"}},
	}
	cfg := Config{JWTSecret: workerTestSecret, TaskRuns: runStore, Agents: agentStore, Spaces: spaceStore}

	get := func(t *testing.T) workerclient.GetTaskRunResponse {
		t.Helper()
		mux := http.NewServeMux()
		New(cfg).Register(mux)
		req := httptest.NewRequest(http.MethodGet, "/api/worker/task-runs/run-1", nil)
		req.Header.Set("Authorization", "Bearer "+runTokenFor(t, "run-1", "task-1"))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
		}
		var got workerclient.GetTaskRunResponse
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return got
	}

	got := get(t)
	if got.Sandbox == nil || got.Sandbox.NetworkTier != "registries" || got.Sandbox.FilesystemTier != "workspace_plus_shared_read" {
		t.Fatalf("sandbox = %+v, want the space's defaults", got.Sandbox)
	}

	// The space raises its default after this run already resolved once; the
	// pin already written must not move.
	spaceStore.Spaces[0].DefaultSandboxNetworkTier = "open"
	got = get(t)
	if got.Sandbox.NetworkTier != "registries" {
		t.Errorf("network tier = %q after the space default changed, want the pinned value registries", got.Sandbox.NetworkTier)
	}
}
