package work

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/testsupport"
	"github.com/icloudbb/buildmax/internal/util"
)

const workflowTestSecret = "workflow-test-secret"

func TestWorkflowHandlers(t *testing.T) {
	spaceID := "tm_personal_u1"
	workflowStore := &mock.MockWorkflowStore{
		Workflows: []coreworkflow.Workflow{{
			ID:          "w_1",
			SpaceID:     spaceID,
			Name:        "WF",
			Description: "desc",
			Definition:  `{"schema_version":1,"nodes":[{"id":"s1","type":"agent_task","target_agent_id":"a_1","prompt":"do it"}]}`,
			Status:      coreworkflow.StatusPublished,
			CreatedBy:   "u1",
			CreatedAt:   time.Unix(100, 0).UTC(),
			UpdatedAt:   time.Unix(100, 0).UTC(),
		}},
	}
	agentStore := &mock.MockAgentStore{
		Agents: []agentdef.Agent{{ID: "a_1", UserID: "u1", SpaceID: spaceID, Name: "Agent 1", Instructions: "Do things"}},
	}
	spaceStore := &mock.MockSpaceStore{
		Spaces:  []corespace.Space{{ID: spaceID, Name: "My Space", PersonalForUserID: util.Ptr("u1"), CreatedBy: "u1"}},
		Members: []corespace.Member{{SpaceID: spaceID, UserID: "u1", Role: corespace.RoleOwner}, {SpaceID: spaceID, UserID: "u2", Role: corespace.RoleMember}, {SpaceID: spaceID, UserID: "u3", Role: corespace.RoleAdmin}},
	}
	taskStore := &mock.MockTaskStore{}
	issueStore := &mock.MockIssueStore{
		Issues: []coreissue.Issue{{
			ID:           "i_1",
			UserID:       "u1",
			SpaceID:      spaceID,
			Title:        "Issue",
			Description:  "Desc",
			Status:       coreissue.StatusTodo,
			ExecutorKind: util.Ptr(coreissue.ExecutorWorkflow),
			ExecutorID:   util.Ptr("w_1"),
			CreatedBy:    "u1",
		}},
	}
	h := New(Config{
		JWTSecret:     workflowTestSecret,
		Spaces:        spaceStore,
		Workflows:     workflowStore,
		Agents:        agentStore,
		Tasks:         taskStore,
		Issues:        issueStore,
		Conversations: &mock.MockConversationStore{},
	})
	mux := http.NewServeMux()
	h.Register(mux)

	t.Run("GET list workflows", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/spaces/"+spaceID+"/workflows", nil)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", workflowTestSecret))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		var out workflowListResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(out.Workflows) != 1 || out.Workflows[0].ID != "w_1" {
			t.Fatalf("workflows = %+v", out.Workflows)
		}
	})

	t.Run("POST create workflow", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/spaces/"+spaceID+"/workflows", strings.NewReader(`{"name":"WF 2","description":"Desc","definition":"{\"schema_version\":1,\"nodes\":[{\"id\":\"s1\",\"type\":\"agent_task\",\"target_agent_id\":\"a_1\",\"prompt\":\"do it\"}]}"}`))
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", workflowTestSecret))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusCreated, rec.Body.String())
		}
		var out workflowResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if out.Status != coreworkflow.StatusDraft {
			t.Fatalf("workflow status = %q, want %q", out.Status, coreworkflow.StatusDraft)
		}
	})

	t.Run("POST direct workflow run", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/spaces/"+spaceID+"/workflows/w_1/runs", nil)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", workflowTestSecret))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusCreated, rec.Body.String())
		}
		var out workflowRunDetailResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if out.Run.ID == "" || len(out.Steps) != 1 {
			t.Fatalf("run detail = %+v", out)
		}
	})

	t.Run("POST direct workflow run rejects input when no input_schema", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/spaces/"+spaceID+"/workflows/w_1/runs", strings.NewReader(`{"input":{"x":1}}`))
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", workflowTestSecret))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	})

	t.Run("POST create workflow forbidden for member", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/spaces/"+spaceID+"/workflows", strings.NewReader(`{"name":"WF 3","description":"Desc","definition":"{\"schema_version\":1,\"nodes\":[{\"id\":\"s1\",\"type\":\"agent_task\",\"target_agent_id\":\"a_1\",\"prompt\":\"do it\"}]}"}`))
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u2", workflowTestSecret))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
		}
	})

	t.Run("PATCH publish workflow by admin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/api/spaces/"+spaceID+"/workflows/w_1", strings.NewReader(`{"status":"published"}`))
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u3", workflowTestSecret))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
	})

	t.Run("POST issue workflow run", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/spaces/"+spaceID+"/issues/i_1/workflow-runs", nil)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", workflowTestSecret))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusCreated, rec.Body.String())
		}
	})

	t.Run("GET issue flow", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/spaces/"+spaceID+"/issues/i_1/flow", nil)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", workflowTestSecret))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var out issueFlowResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if out.Issue.ID != "i_1" || out.Workflow == nil || out.Workflow.ID != "w_1" || len(out.Runs) == 0 {
			t.Fatalf("issue flow = %+v", out)
		}
	})
}
