package work

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
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

const issueTestSecret = "issue-test-secret"

func TestIssueHandlers(t *testing.T) {
	agentID := "a_1"
	personalSpaceID := "tm_personal_u1"
	otherSpaceID := "tm_other"
	workflowID := "w_1"
	store := &mock.MockIssueStore{
		Issues: []coreissue.Issue{
			{
				ID:           "i_1",
				UserID:       "u1",
				SpaceID:      personalSpaceID,
				Title:        "Initial issue",
				Description:  "Initial description",
				Status:       coreissue.StatusTodo,
				CreatedBy:    "u1",
				CreatedAt:    time.Unix(100, 0).UTC(),
				UpdatedAt:    time.Unix(100, 0).UTC(),
				OwnerID:      nil,
				ExecutorKind: nil,
				ExecutorID:   nil,
				Version:      1,
			},
		},
	}
	agents := &mock.MockAgentStore{
		Agents: []agentdef.Agent{{ID: agentID, UserID: "u1", SpaceID: personalSpaceID, Name: "Agent 1"}},
	}
	workflows := &mock.MockWorkflowStore{
		Workflows: []coreworkflow.Workflow{{ID: workflowID, SpaceID: personalSpaceID, Name: "Workflow 1", Definition: `{"schema_version":1,"nodes":[{"id":"s1","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"do it"}}]}`, Status: coreworkflow.StatusPublished}},
	}
	tasks := &mock.MockTaskStore{}
	spaces := &mock.MockSpaceStore{
		Spaces: []corespace.Space{
			{ID: personalSpaceID, Name: "My Space", PersonalForUserID: util.Ptr("u1"), CreatedBy: "u1"},
			{ID: otherSpaceID, Name: "Other", CreatedBy: "u2"},
		},
		Members: []corespace.Member{
			{SpaceID: personalSpaceID, UserID: "u1", Role: corespace.RoleOwner},
			{SpaceID: personalSpaceID, UserID: "u2", Role: corespace.RoleMember},
			{SpaceID: personalSpaceID, UserID: "u3", Role: corespace.RoleAdmin},
			{SpaceID: otherSpaceID, UserID: "u2", Role: corespace.RoleOwner},
		},
	}
	h := New(Config{
		JWTSecret:     issueTestSecret,
		Spaces:        spaces,
		Issues:        store,
		Agents:        agents,
		Workflows:     workflows,
		Tasks:         tasks,
		Conversations: &mock.MockConversationStore{},
	})
	mux := http.NewServeMux()
	h.Register(mux)

	t.Run("GET list issues", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/spaces/"+personalSpaceID+"/issues", nil)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", issueTestSecret))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		var out issueListResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		if len(out.Issues) != 1 || out.Issues[0].ID != "i_1" {
			t.Fatalf("issues = %+v", out.Issues)
		}
	})

	t.Run("POST create issue", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/spaces/"+personalSpaceID+"/issues", strings.NewReader(`{"title":"New issue","description":"Desc"}`))
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", issueTestSecret))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusCreated, rec.Body.String())
		}
		var out IssueResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode create: %v", err)
		}
		if out.Title != "New issue" || out.Status != coreissue.StatusTodo {
			t.Fatalf("created = %+v", out)
		}
		if out.SpaceID != personalSpaceID {
			t.Fatalf("created space_id = %q, want %q", out.SpaceID, personalSpaceID)
		}
	})

	t.Run("POST create issue missing title returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/spaces/"+personalSpaceID+"/issues", strings.NewReader(`{"title":"","description":"Desc"}`))
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", issueTestSecret))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	})

	t.Run("GET issue detail", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/spaces/"+personalSpaceID+"/issues/i_1", nil)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", issueTestSecret))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("PATCH issue assign executor to agent", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/api/spaces/"+personalSpaceID+"/issues/i_1", strings.NewReader(`{"version":1,"status":"in_progress","executor_kind":"agent","executor_id":"a_1"}`))
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", issueTestSecret))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var out IssueResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode patch: %v", err)
		}
		if out.Status != coreissue.StatusInProgress || out.ExecutorKind == nil || *out.ExecutorKind != coreissue.ExecutorAgent {
			t.Fatalf("patched = %+v", out)
		}
	})

	t.Run("POST issue agent run", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/spaces/"+personalSpaceID+"/issues/i_1/agent-runs", nil)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", issueTestSecret))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusCreated, rec.Body.String())
		}
		var out TaskResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode agent run: %v", err)
		}
		if out.IssueID == nil || *out.IssueID != "i_1" || out.AgentID == nil || *out.AgentID != agentID {
			t.Fatalf("agent run = %+v", out)
		}
		if len(tasks.List) == 0 {
			t.Fatal("expected created task to be persisted")
		}
		created := tasks.List[len(tasks.List)-1]
		if created.SpaceID != personalSpaceID {
			t.Fatalf("created task space_id = %q, want %q", created.SpaceID, personalSpaceID)
		}
		if created.IssueID == nil || *created.IssueID != "i_1" {
			t.Fatalf("created task issue_id = %v, want i_1", created.IssueID)
		}
		flowReq := httptest.NewRequest(http.MethodGet, "/api/spaces/"+personalSpaceID+"/issues/i_1/flow", nil)
		flowReq.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", issueTestSecret))
		flowRec := httptest.NewRecorder()
		mux.ServeHTTP(flowRec, flowReq)
		if flowRec.Code != http.StatusOK {
			t.Fatalf("flow status = %d, want %d body=%s", flowRec.Code, http.StatusOK, flowRec.Body.String())
		}
		var flow issueFlowResponse
		if err := json.Unmarshal(flowRec.Body.Bytes(), &flow); err != nil {
			t.Fatalf("decode flow: %v", err)
		}
		if len(flow.AgentTasks) == 0 {
			t.Fatal("expected issue flow to include created agent task")
		}
	})

	t.Run("PATCH invalid status returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/api/spaces/"+personalSpaceID+"/issues/i_1", strings.NewReader(`{"version":2,"status":"blocked"}`))
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", issueTestSecret))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	})

	t.Run("PATCH issue assign executor to workflow", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/api/spaces/"+personalSpaceID+"/issues/i_1", strings.NewReader(`{"version":2,"executor_kind":"workflow","executor_id":"w_1"}`))
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", issueTestSecret))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var out IssueResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode patch: %v", err)
		}
		if out.ExecutorKind == nil || *out.ExecutorKind != coreissue.ExecutorWorkflow {
			t.Fatalf("patched = %+v", out)
		}
	})

	t.Run("PATCH issue assign executor to workflow forbidden for member", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/api/spaces/"+personalSpaceID+"/issues/i_1", strings.NewReader(`{"version":3,"executor_kind":"workflow","executor_id":"w_1"}`))
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u2", issueTestSecret))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
		}
	})

	// Version 3 is current by now: the two accepted PATCHes above each bumped it.
	t.Run("PATCH with a stale version returns 409", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/api/spaces/"+personalSpaceID+"/issues/i_1", strings.NewReader(`{"version":1,"title":"Written from a stale copy"}`))
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", issueTestSecret))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusConflict, rec.Body.String())
		}
		if store.Issues[0].Title == "Written from a stale copy" {
			t.Fatalf("refused patch still wrote the title")
		}
	})

	t.Run("PATCH without a version returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/api/spaces/"+personalSpaceID+"/issues/i_1", strings.NewReader(`{"title":"No precondition"}`))
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", issueTestSecret))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	})

	t.Run("GET issue unauthorized returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/spaces/"+personalSpaceID+"/issues/i_1", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("GET issues forbidden for non-member explicit space", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/spaces/"+otherSpaceID+"/issues", nil)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", issueTestSecret))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
		}
	})
}

// The listing filters openapi.json has always described now exist. `owner=me`
// is the inbox; the explicit params answer for anyone else, and for Executor.
func TestListIssuesFilters(t *testing.T) {
	const space = "tm_filter"
	agent := coreissue.ExecutorAgent
	mine, theirs, bot := "u1", "u2", "a_1"
	store := &mock.MockIssueStore{
		Issues: []coreissue.Issue{
			{ID: "i_mine_open", SpaceID: space, UserID: "u1", Title: "Mine, open", Status: coreissue.StatusTodo, OwnerID: &mine, Version: 1},
			{ID: "i_mine_done", SpaceID: space, UserID: "u1", Title: "Mine, done", Status: coreissue.StatusDone, OwnerID: &mine, Version: 1},
			{ID: "i_theirs", SpaceID: space, UserID: "u1", Title: "Someone else's", Status: coreissue.StatusTodo, OwnerID: &theirs, Version: 1},
			{ID: "i_agent", SpaceID: space, UserID: "u1", Title: "An agent's", Status: coreissue.StatusTodo, ExecutorKind: &agent, ExecutorID: &bot, Version: 1},
			{ID: "i_unassigned", SpaceID: space, UserID: "u1", Title: "Nobody's", Status: coreissue.StatusTodo, Version: 1},
		},
	}
	h := New(Config{
		JWTSecret: issueTestSecret,
		Issues:    store,
		Spaces: &mock.MockSpaceStore{
			Spaces:  []corespace.Space{{ID: space, Name: "Filters", CreatedBy: "u1"}},
			Members: []corespace.Member{{SpaceID: space, UserID: "u1", Role: corespace.RoleOwner}},
		},
	})
	mux := http.NewServeMux()
	h.Register(mux)

	list := func(t *testing.T, query string) []string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/spaces/"+space+"/issues?"+query, nil)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", issueTestSecret))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
		}
		var out issueListResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		ids := make([]string, 0, len(out.Issues))
		for _, issue := range out.Issues {
			ids = append(ids, issue.ID)
		}
		if out.Total != len(ids) {
			t.Fatalf("total = %d but %d issues returned; the count must be filtered too", out.Total, len(ids))
		}
		return ids
	}

	for _, tc := range []struct {
		name  string
		query string
		want  []string
	}{
		{"unfiltered", "", []string{"i_mine_open", "i_mine_done", "i_theirs", "i_agent", "i_unassigned"}},
		{"owned by me", "owner=me", []string{"i_mine_open", "i_mine_done"}},
		{"my open work", "owner=me&status=todo", []string{"i_mine_open"}},
		{"owned by someone else", "owner_id=u2", []string{"i_theirs"}},
		{"executor is an agent", "executor_kind=agent&executor_id=a_1", []string{"i_agent"}},
		{"one status", "status=done", []string{"i_mine_done"}},
		// An id with no kind cannot say which table to read it against, so it
		// narrows nothing rather than guessing.
		{"executor_id alone narrows nothing", "executor_id=u1", []string{"i_mine_open", "i_mine_done", "i_theirs", "i_agent", "i_unassigned"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := list(t, tc.query)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for _, want := range tc.want {
				if !slices.Contains(got, want) {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}
