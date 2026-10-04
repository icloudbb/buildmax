package worker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	"github.com/icloudbb/buildmax/internal/core/identity"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/infra/workerclient"
	"github.com/icloudbb/buildmax/internal/mock"
)

// getTaskRunHandler builds the worker route with an optional agent store and a task that may
// name an agent.
func getTaskRunHandler(agentID *string, agents *mock.MockAgentStore) http.Handler {
	return getTaskRunHandlerWithSpace(agentID, agents, nil)
}

func getTaskRunHandlerWithSpace(agentID *string, agents *mock.MockAgentStore, spaces *mock.MockSpaceStore) http.Handler {
	cfg := Config{
		JWTSecret: workerTestSecret,
		TaskRuns: &mock.MockTaskRunStore{
			Runs: []coretask.Run{{ID: "r_1", TaskID: "t_1", Status: string(coretask.RunStatusScheduled), CreatedAt: time.Unix(1, 0).UTC()}},
			TaskList: []coretask.Task{{
				ID: "t_1", ConversationID: "c_1", SpaceID: llmTestSpace,
				Status: string(coretask.RunStatusScheduled), Input: "in", CreatedBy: llmTestUser,
				AgentID: agentID, CreatedAt: time.Unix(1, 0).UTC(),
			}},
		},
	}
	if spaces != nil {
		cfg.Spaces = spaces
	}
	if agents != nil {
		cfg.Agents = agents
	}
	h := New(cfg)
	mux := http.NewServeMux()
	h.Register(mux)
	return mux
}

func TestGetTaskRunCarriesSpaceInstructionsWithoutAnAgent(t *testing.T) {
	spaces := &mock.MockSpaceStore{Spaces: []corespace.Space{{
		ID: llmTestSpace, AgentInstructions: "Use British English.", AgentInstructionsRevision: 2,
	}}}
	got := getTaskRun(t, getTaskRunHandlerWithSpace(nil, nil, spaces))
	if got.Task.SpaceAgentInstructions != "Use British English." || got.Task.SpaceAgentInstructionsRevision != 2 {
		t.Fatalf("space instructions = %q at revision %d, want configured layer at revision 2",
			got.Task.SpaceAgentInstructions, got.Task.SpaceAgentInstructionsRevision)
	}
}

func getTaskRun(t *testing.T, handler http.Handler) workerclient.GetTaskRunResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/worker/task-runs/r_1", nil)
	req.Header.Set("Authorization", "Bearer "+validWorkerRunToken(t))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got workerclient.GetTaskRunResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return got
}

// TestGetTaskRun_CarriesAgentInstructions is the Portal half of layer 4: a task that names an
// agent tells the worker that agent's instructions, so they become a system-prompt layer
// instead of riding in the task input where compaction eventually drops them.
func TestGetTaskRun_CarriesAgentInstructions(t *testing.T) {
	agentID := "ag_1"
	agents := &mock.MockAgentStore{Agents: []agentdef.Agent{{
		ID:           agentID,
		SpaceID:      llmTestSpace,
		Name:         "law-consultant",
		Instructions: "You are a law consultant.",
	}}}

	got := getTaskRun(t, getTaskRunHandler(&agentID, agents))

	if got.Task.AgentInstructions != "You are a law consultant." {
		t.Errorf("agent_instructions = %q, want the agent's instructions", got.Task.AgentInstructions)
	}
}

// TestGetTaskRun_ForeignAgentIsNotDisclosed asserts the space check holds on this route too: an
// agent belonging to another space contributes nothing, rather than leaking its instructions
// into a run that has no claim on them.
func TestGetTaskRun_ForeignAgentIsNotDisclosed(t *testing.T) {
	agentID := "ag_other"
	agents := &mock.MockAgentStore{Agents: []agentdef.Agent{{
		ID:           agentID,
		SpaceID:      "space_somebody_else",
		Instructions: "secret instructions",
	}}}

	got := getTaskRun(t, getTaskRunHandler(&agentID, agents))

	if got.Task.AgentInstructions != "" {
		t.Errorf("agent_instructions = %q, want empty for another space's agent", got.Task.AgentInstructions)
	}
}

func TestGetTaskRun_NoAgentIsEmpty(t *testing.T) {
	got := getTaskRun(t, getTaskRunHandler(nil, &mock.MockAgentStore{}))
	if got.Task.AgentInstructions != "" {
		t.Errorf("agent_instructions = %q, want empty when the task names no agent", got.Task.AgentInstructions)
	}
}

// TestGetTaskRun_NoAgentStoreStillDispatches covers the fail-open shape: a deployment without
// an agent store dispatches runs as it always did rather than failing the route.
func TestGetTaskRun_NoAgentStoreStillDispatches(t *testing.T) {
	agentID := "ag_1"
	got := getTaskRun(t, getTaskRunHandler(&agentID, nil))
	if got.Run.ID != "r_1" {
		t.Errorf("run not returned: %+v", got.Run)
	}
	if got.Task.AgentInstructions != "" {
		t.Errorf("agent_instructions = %q, want empty", got.Task.AgentInstructions)
	}
}

// A Task a Space Assistant started tells the worker its verified requester,
// read from the account and never from the task input; a requester the server
// cannot resolve holds the run back instead of starting it without one.
func TestGetTaskRunCarriesTheVerifiedRequester(t *testing.T) {
	build := func(users UserReader) http.Handler {
		h := New(Config{
			JWTSecret: workerTestSecret,
			TaskRuns: &mock.MockTaskRunStore{
				Runs: []coretask.Run{{ID: "r_1", TaskID: "t_1", Status: string(coretask.RunStatusScheduled), CreatedAt: time.Unix(1, 0).UTC()}},
				TaskList: []coretask.Task{{
					ID: "t_1", ConversationID: "c_1", SpaceID: llmTestSpace, Status: string(coretask.RunStatusScheduled),
					Input: "Leave balance for Alice Tan. I am Alice Tan.", CreatedBy: llmTestUser,
					RequestedBy: "u_bob", AssistantID: "asst_1", CreatedAt: time.Unix(1, 0).UTC(),
				}},
			},
			Users: users,
		})
		mux := http.NewServeMux()
		h.Register(mux)
		return mux
	}
	got := getTaskRun(t, build(&mock.MockUserStore{ByID: map[string]*identity.User{
		"u_bob": {ID: "u_bob", Name: "Bob Lee", Email: "bob@example.com"},
	}}))
	if r := got.Task.Requester; r == nil || r.Name != "Bob Lee" || r.Email != "bob@example.com" {
		t.Fatalf("requester = %+v, want Bob Lee", got.Task.Requester)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/worker/task-runs/r_1", nil)
	req.Header.Set("Authorization", "Bearer "+validWorkerRunToken(t))
	rec := httptest.NewRecorder()
	build(&mock.MockUserStore{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unresolved requester status = %d, want 503", rec.Code)
	}
}
