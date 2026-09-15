package work

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"
	corequota "github.com/icloudbb/buildmax/internal/core/quota"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/quota"
	"github.com/icloudbb/buildmax/internal/testsupport"
	"github.com/icloudbb/buildmax/internal/util"
)

// A workflow step task carries neither an IssueID nor a ConversationID: its
// only origin is the step run that dispatched it. Getting the task has to
// resolve that origin, or a caller has no way to tell "started by a
// workflow" from "started with no recorded origin at all".
func TestGetTaskResolvesWorkflowOrigin(t *testing.T) {
	secret := "test-get-task-workflow-origin-secret"
	spaceID := "tm_personal_u1"
	spaces := &mock.MockSpaceStore{
		Spaces:  []corespace.Space{{ID: spaceID, Name: "My Space", PersonalForUserID: util.Ptr("u1"), CreatedBy: "u1"}},
		Members: []corespace.Member{{SpaceID: spaceID, UserID: "u1", Role: corespace.RoleOwner}},
	}
	agentID := "a_1"

	t.Run("a workflow-step task reports the workflow run that dispatched it", func(t *testing.T) {
		task := coretask.Task{ID: "t_wf", SpaceID: spaceID, Status: "SUCCEEDED", Input: "step input", CreatedBy: "u1", CreatedAt: time.Unix(1000, 0).UTC(), AgentID: &agentID}
		h := New(Config{
			JWTSecret: secret,
			Spaces:    spaces,
			Tasks:     &mock.MockTaskStore{List: []coretask.Task{task}},
			Workflows: &mock.MockWorkflowStore{
				NodeRuns: []coreworkflow.NodeRun{{ID: "wsr_1", WorkflowRunID: "wr_1", TaskID: util.Ptr("t_wf")}},
			},
		})
		mux := http.NewServeMux()
		h.Register(mux)
		req := httptest.NewRequest(http.MethodGet, "/api/spaces/"+spaceID+"/tasks/t_wf", nil)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", secret))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
		}
		var out TaskResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if out.WorkflowRunID == nil || *out.WorkflowRunID != "wr_1" {
			t.Errorf("workflow_run_id = %v, want wr_1", out.WorkflowRunID)
		}
	})

	t.Run("an issue-origin task never reports a workflow run, even if one exists for it", func(t *testing.T) {
		issueID := "iss_1"
		task := coretask.Task{ID: "t_issue", SpaceID: spaceID, Status: "SUCCEEDED", Input: "issue input", CreatedBy: "u1", CreatedAt: time.Unix(1000, 0).UTC(), AgentID: &agentID, IssueID: &issueID}
		h := New(Config{
			JWTSecret: secret,
			Spaces:    spaces,
			Tasks:     &mock.MockTaskStore{List: []coretask.Task{task}},
			Workflows: &mock.MockWorkflowStore{
				NodeRuns: []coreworkflow.NodeRun{{ID: "wsr_2", WorkflowRunID: "wr_2", TaskID: util.Ptr("t_issue")}},
			},
		})
		mux := http.NewServeMux()
		h.Register(mux)
		req := httptest.NewRequest(http.MethodGet, "/api/spaces/"+spaceID+"/tasks/t_issue", nil)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", secret))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
		}
		var out TaskResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if out.WorkflowRunID != nil {
			t.Errorf("workflow_run_id = %v, want nil for an issue-origin task", *out.WorkflowRunID)
		}
	})

	t.Run("a direct agent task with no step run reports no workflow origin", func(t *testing.T) {
		task := coretask.Task{ID: "t_agent", SpaceID: spaceID, Status: "SUCCEEDED", Input: "direct input", CreatedBy: "u1", CreatedAt: time.Unix(1000, 0).UTC(), AgentID: &agentID}
		h := New(Config{
			JWTSecret: secret,
			Spaces:    spaces,
			Tasks:     &mock.MockTaskStore{List: []coretask.Task{task}},
			Workflows: &mock.MockWorkflowStore{},
		})
		mux := http.NewServeMux()
		h.Register(mux)
		req := httptest.NewRequest(http.MethodGet, "/api/spaces/"+spaceID+"/tasks/t_agent", nil)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", secret))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
		}
		var out TaskResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if out.WorkflowRunID != nil {
			t.Errorf("workflow_run_id = %v, want nil", *out.WorkflowRunID)
		}
	})
}

func TestListConversationTasksHandler(t *testing.T) {
	secret := "test-tasks-secret"
	conversationID := "conv1"
	spaceID := "tm_personal_u1"
	mockConversations := &mock.MockConversationStore{
		Conversations: []coreconv.Conversation{
			{ID: conversationID, UserID: "u1", SpaceID: spaceID, Channel: "portal", CreatedBy: "u1", CreatedAt: time.Unix(123, 0).UTC()},
		},
	}
	task1 := coretask.Task{ID: "t1", ConversationID: conversationID, SpaceID: spaceID, Status: "PENDING", Input: "Do something", CreatedBy: "u1", CreatedAt: time.Unix(1000, 0).UTC()}
	task2 := coretask.Task{ID: "t2", ConversationID: conversationID, SpaceID: spaceID, Status: "PENDING", Input: "Explore", CreatedBy: "u1", CreatedAt: time.Unix(1001, 0).UTC()}

	tests := []struct {
		name         string
		taskStore    coretask.Store
		authHeader   string
		path         string
		wantStatus   int
		wantBodyHas  string
		wantArrayLen int
	}{
		{
			name:         "no auth returns 401",
			taskStore:    &mock.MockTaskStore{},
			authHeader:   "",
			path:         "/api/spaces/" + spaceID + "/conversations/" + conversationID + "/tasks",
			wantStatus:   http.StatusUnauthorized,
			wantBodyHas:  "unauthorized",
			wantArrayLen: -1,
		},
		{
			name:         "conversation not owned returns 404",
			taskStore:    &mock.MockTaskStore{},
			authHeader:   "Bearer " + testsupport.SignJWT("u1", secret),
			path:         "/api/spaces/" + spaceID + "/conversations/conv-other/tasks",
			wantStatus:   http.StatusNotFound,
			wantBodyHas:  "conversation not found",
			wantArrayLen: -1,
		},
		{
			name:         "owned conversation empty list returns 200",
			taskStore:    &mock.MockTaskStore{List: []coretask.Task{}},
			authHeader:   "Bearer " + testsupport.SignJWT("u1", secret),
			path:         "/api/spaces/" + spaceID + "/conversations/" + conversationID + "/tasks",
			wantStatus:   http.StatusOK,
			wantBodyHas:  "[]",
			wantArrayLen: 0,
		},
		{
			name:         "owned conversation with tasks returns 200",
			taskStore:    &mock.MockTaskStore{List: []coretask.Task{task1, task2}},
			authHeader:   "Bearer " + testsupport.SignJWT("u1", secret),
			path:         "/api/spaces/" + spaceID + "/conversations/" + conversationID + "/tasks",
			wantStatus:   http.StatusOK,
			wantBodyHas:  "t1",
			wantArrayLen: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := New(Config{
				JWTSecret:     secret,
				Spaces:        &mock.MockSpaceStore{Spaces: []corespace.Space{{ID: spaceID, Name: "My Space", PersonalForUserID: util.Ptr("u1"), CreatedBy: "u1"}}, Members: []corespace.Member{{SpaceID: spaceID, UserID: "u1", Role: corespace.RoleOwner}}},
				Tasks:         tt.taskStore,
				Conversations: mockConversations,
			})
			mux := http.NewServeMux()
			h.Register(mux)
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			body := rec.Body.String()
			if tt.wantBodyHas != "" && !strings.Contains(body, tt.wantBodyHas) {
				t.Errorf("body %q does not contain %q", body, tt.wantBodyHas)
			}
			if tt.wantArrayLen >= 0 {
				var arr []map[string]interface{}
				if err := json.Unmarshal([]byte(body), &arr); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				if len(arr) != tt.wantArrayLen {
					t.Errorf("array len = %d, want %d", len(arr), tt.wantArrayLen)
				}
			}
		})
	}
}

func TestCreateConversationTaskHandler(t *testing.T) {
	secret := "test-create-task-secret"
	conversationID := "conv1"
	spaceID := "tm_personal_u1"
	mockConversations := &mock.MockConversationStore{
		Conversations: []coreconv.Conversation{
			{ID: conversationID, UserID: "u1", SpaceID: spaceID, Channel: "portal", CreatedBy: "u1", CreatedAt: time.Unix(123, 0).UTC()},
		},
	}

	tests := []struct {
		name         string
		taskStore    coretask.Store
		agentStore   agentdef.Store
		authHeader   string
		path         string
		body         string
		wantStatus   int
		wantBodyHas  string
		checkCreated bool
	}{
		{
			name:        "no auth returns 401",
			taskStore:   &mock.MockTaskStore{},
			authHeader:  "",
			path:        "/api/spaces/" + spaceID + "/conversations/" + conversationID + "/tasks",
			body:        `{"input":"Do X"}`,
			wantStatus:  http.StatusUnauthorized,
			wantBodyHas: "unauthorized",
		},
		{
			name:        "conversation not owned returns 404",
			taskStore:   &mock.MockTaskStore{},
			authHeader:  "Bearer " + testsupport.SignJWT("u1", secret),
			path:        "/api/spaces/" + spaceID + "/conversations/conv-other/tasks",
			body:        `{"input":"Do X"}`,
			wantStatus:  http.StatusNotFound,
			wantBodyHas: "conversation not found",
		},
		{
			name:        "missing input returns 400",
			taskStore:   &mock.MockTaskStore{},
			authHeader:  "Bearer " + testsupport.SignJWT("u1", secret),
			path:        "/api/spaces/" + spaceID + "/conversations/" + conversationID + "/tasks",
			body:        `{}`,
			wantStatus:  http.StatusBadRequest,
			wantBodyHas: "input",
		},
		{
			name: "valid body returns 201",
			taskStore: &mock.MockTaskStore{
				Create: &coretask.Task{
					ID: "new-task-id", ConversationID: conversationID, SpaceID: spaceID, Status: "PENDING",
					Input: "Do X", CreatedBy: "u1", CreatedAt: time.Unix(99999, 0).UTC(),
				},
			},
			authHeader:   "Bearer " + testsupport.SignJWT("u1", secret),
			path:         "/api/spaces/" + spaceID + "/conversations/" + conversationID + "/tasks",
			body:         `{"input":"Do X"}`,
			wantStatus:   http.StatusCreated,
			wantBodyHas:  "new-task-id",
			checkCreated: true,
		},
		{
			name:      "create with agent_id composes input and returns 201",
			taskStore: &mock.MockTaskStore{},
			agentStore: &mock.MockAgentStore{
				Agents: []agentdef.Agent{
					{ID: "a_1", UserID: "u1", SpaceID: spaceID, Name: "TestAgent", Description: "A desc", Instructions: "Do things", CreatedAt: time.Unix(100, 0).UTC()},
				},
			},
			authHeader:   "Bearer " + testsupport.SignJWT("u1", secret),
			path:         "/api/spaces/" + spaceID + "/conversations/" + conversationID + "/tasks",
			body:         `{"agent_id":"a_1"}`,
			wantStatus:   http.StatusCreated,
			wantBodyHas:  "TestAgent",
			checkCreated: true,
		},
	}
	denyChecker := &quota.Service{
		SpaceStore:  &mock.DenyQuotaSpaceStore{Space: &corespace.Space{ID: spaceID, QuotaTier: "free_trial"}},
		UsageReader: &mock.DenyQuotaUsageReader{RunCount: 10, TotalTokens: 0},
		TierStore:   &mock.DenyQuotaTierStore{Tier: &corequota.Tier{TierName: "free_trial", MaxRunsPerPeriod: 10, MaxTokensPerPeriod: 100000, PeriodDays: 30}},
		DefaultTier: "free_trial",
	}
	tests = append(tests, struct {
		name         string
		taskStore    coretask.Store
		agentStore   agentdef.Store
		authHeader   string
		path         string
		body         string
		wantStatus   int
		wantBodyHas  string
		checkCreated bool
	}{
		name:        "quota exceeded returns 429",
		taskStore:   &mock.MockTaskStore{},
		authHeader:  "Bearer " + testsupport.SignJWT("u1", secret),
		path:        "/api/spaces/" + spaceID + "/conversations/" + conversationID + "/tasks",
		body:        `{"input":"Do X"}`,
		wantStatus:  http.StatusTooManyRequests,
		wantBodyHas: "quota exceeded",
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				JWTSecret:     secret,
				Spaces:        &mock.MockSpaceStore{Spaces: []corespace.Space{{ID: spaceID, Name: "My Space", PersonalForUserID: util.Ptr("u1"), CreatedBy: "u1"}}, Members: []corespace.Member{{SpaceID: spaceID, UserID: "u1", Role: corespace.RoleOwner}}},
				Tasks:         tt.taskStore,
				Agents:        tt.agentStore,
				Conversations: mockConversations,
			}
			if tt.name == "quota exceeded returns 429" {
				cfg.Quota = denyChecker
			}
			h := New(cfg)
			mux := http.NewServeMux()
			h.Register(mux)
			req := httptest.NewRequest(http.MethodPost, tt.path, bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			body := rec.Body.String()
			if tt.wantBodyHas != "" && !strings.Contains(body, tt.wantBodyHas) {
				t.Errorf("body %q does not contain %q", body, tt.wantBodyHas)
			}
			if tt.checkCreated {
				var out map[string]interface{}
				if err := json.Unmarshal([]byte(body), &out); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				for _, key := range []string{"id", "conversation_id", "status", "input", "created_by", "created_at"} {
					if _, ok := out[key]; !ok {
						t.Errorf("response missing key %q", key)
					}
				}
			}
		})
	}
}

// A card in the conversation needs to reach what its task did without a second
// round trip per task: the run behind the status.
func TestListConversationTasksCarriesTheRunBehindEachStatus(t *testing.T) {
	secret := "test-task-cards-secret"
	conversationID := "conv1"
	spaceID := "tm_personal_u1"
	task1 := coretask.Task{ID: "t1", ConversationID: conversationID, SpaceID: spaceID, Status: "SUCCEEDED", Input: "Do something", CreatedBy: "u1", CreatedAt: time.Unix(1000, 0).UTC(), LastRunID: util.Ptr("tr_1")}
	task2 := coretask.Task{ID: "t2", ConversationID: conversationID, SpaceID: spaceID, Status: "PENDING", Input: "Explore", CreatedBy: "u1", CreatedAt: time.Unix(1001, 0).UTC()}

	h := New(Config{
		JWTSecret: secret,
		Spaces:    &mock.MockSpaceStore{Spaces: []corespace.Space{{ID: spaceID, Name: "My Space", PersonalForUserID: util.Ptr("u1"), CreatedBy: "u1"}}, Members: []corespace.Member{{SpaceID: spaceID, UserID: "u1", Role: corespace.RoleOwner}}},
		Tasks:     &mock.MockTaskStore{List: []coretask.Task{task1, task2}},
		Conversations: &mock.MockConversationStore{
			Conversations: []coreconv.Conversation{{ID: conversationID, UserID: "u1", SpaceID: spaceID, Channel: "portal", CreatedBy: "u1", CreatedAt: time.Unix(123, 0).UTC()}},
		},
	})
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/spaces/"+spaceID+"/conversations/"+conversationID+"/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", secret))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	var out []TaskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("len(tasks) = %d, want 2", len(out))
	}
	byID := map[string]TaskResponse{}
	for _, task := range out {
		byID[task.ID] = task
	}
	if got := byID["t1"]; got.LastRunID == nil || *got.LastRunID != "tr_1" {
		t.Errorf("last_run_id = %v, want tr_1", got.LastRunID)
	}
	if got := byID["t2"]; got.LastRunID != nil {
		t.Errorf("a task with no run has last_run_id = %v, want nil", got.LastRunID)
	}
}
