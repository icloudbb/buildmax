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
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/quota"
	"github.com/icloudbb/buildmax/internal/testsupport"
	"github.com/icloudbb/buildmax/internal/util"
)

// A Task names what it serves on every read, the single-task read and a list
// alike, so Portal labels a run by its Issue or Workflow without a lookup per
// row. The store resolves the origin; the response carries it unchanged.
func TestTaskResponsesCarryOrigin(t *testing.T) {
	secret := "test-task-origin-secret"
	spaceID := "tm_personal_u1"
	agentID := "a_1"
	issueID := "iss_1"
	origin := &coretask.Origin{Kind: coretask.OriginIssue, ID: issueID, Name: "Fix import failures"}
	tasks := []coretask.Task{
		{ID: "t_issue", SpaceID: spaceID, Status: "SUCCEEDED", Input: "issue input", Title: "deployment smoke ok", CreatedBy: "u1", CreatedAt: time.Unix(2000, 0).UTC(), AgentID: &agentID, IssueID: &issueID, Origin: origin},
		{ID: "t_direct", SpaceID: spaceID, Status: "SUCCEEDED", Input: "direct input", CreatedBy: "u1", CreatedAt: time.Unix(1000, 0).UTC(), AgentID: &agentID},
	}
	h := New(Config{
		JWTSecret: secret,
		Spaces: &mock.MockSpaceStore{
			Spaces:  []corespace.Space{{ID: spaceID, Name: "My Space", PersonalForUserID: util.Ptr("u1"), CreatedBy: "u1"}},
			Members: []corespace.Member{{SpaceID: spaceID, UserID: "u1", Role: corespace.RoleOwner}},
		},
		Agents: &mock.MockAgentStore{Agents: []agentdef.Agent{{ID: agentID, SpaceID: spaceID, Name: "Reviewer"}}},
		Tasks:  &mock.MockTaskStore{List: tasks},
	})
	mux := http.NewServeMux()
	h.Register(mux)
	get := func(t *testing.T, path string, out any) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/spaces/"+spaceID+path, nil)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", secret))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200; body = %s", path, rec.Code, rec.Body.String())
		}
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}

	t.Run("the single-task read", func(t *testing.T) {
		var out TaskResponse
		get(t, "/tasks/t_issue", &out)
		if out.Origin == nil || *out.Origin != *origin {
			t.Errorf("origin = %+v, want %+v", out.Origin, origin)
		}
	})

	t.Run("an Agent's run list, with no origin for a direct run", func(t *testing.T) {
		var out tasksListResponse
		get(t, "/agents/"+agentID+"/tasks", &out)
		if len(out.Tasks) != 2 {
			t.Fatalf("tasks = %d, want 2", len(out.Tasks))
		}
		for _, task := range out.Tasks {
			switch task.ID {
			case "t_issue":
				if task.Origin == nil || *task.Origin != *origin {
					t.Errorf("issue task origin = %+v, want %+v", task.Origin, origin)
				}
			case "t_direct":
				if task.Origin != nil {
					t.Errorf("direct task origin = %+v, want none", task.Origin)
				}
			}
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

// A Task list explains its failed rows: the latest run's class and cause ride
// the Task, so the Issue's latest run can lead with the fix without a run read.
func TestTaskResponseCarriesTheLatestRunFailure(t *testing.T) {
	failed := coretask.Task{
		ID: "tk_1", SpaceID: "tm_1", Status: "FAILED", CreatedBy: "u1",
		FailureClass: string(coretask.FailureSpaceConfiguration),
		FailureCause: &coretask.FailureCause{Kind: coretask.FailureCauseSecretGrant, SecretID: "sec_gh", SecretProblem: coretask.SecretDisabled},
	}
	raw, err := json.Marshal(taskToResponse(failed))
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, want := range []string{
		`"failure_class":"space_configuration"`,
		`"failure_cause":{"kind":"secret_grant","secret_id":"sec_gh","secret_problem":"disabled"}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("task response %s lacks %s", body, want)
		}
	}

	ok, err := json.Marshal(taskToResponse(coretask.Task{ID: "tk_2", SpaceID: "tm_1", Status: "SUCCEEDED", CreatedBy: "u1"}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(ok), "failure_") {
		t.Errorf("a succeeded task carries a failure: %s", ok)
	}
}

// The Task page reads each run's failure from the runs list.
func TestListTaskRunsCarriesEachRunFailure(t *testing.T) {
	secret := "test-task-runs-failure-secret"
	task := coretask.Task{ID: "tk_1", SpaceID: "tm_1", Status: "FAILED", Input: "x", CreatedBy: "u1"}
	run := coretask.Run{
		ID: "tr_1", TaskID: "tk_1", Status: "FAILED", CreatedAt: time.Unix(1000, 0).UTC(),
		FailureClass: string(coretask.FailureSpaceConfiguration),
		FailureCause: &coretask.FailureCause{Kind: coretask.FailureCauseSecretGrant, SecretID: "sec_gh", SecretProblem: coretask.SecretDisabled},
	}
	h := New(Config{
		JWTSecret: secret,
		Spaces: &mock.MockSpaceStore{
			Spaces:  []corespace.Space{{ID: "tm_1", Name: "Team", CreatedBy: "u1"}},
			Members: []corespace.Member{{SpaceID: "tm_1", UserID: "u1", Role: corespace.RoleMember}},
		},
		Tasks:    &mock.MockTaskStore{List: []coretask.Task{task}},
		TaskRuns: &mock.MockTaskRunStore{Runs: []coretask.Run{run}, TaskList: []coretask.Task{task}},
	})
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/spaces/tm_1/tasks/tk_1/runs", nil)
	req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", secret))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`"failure_class":"space_configuration"`,
		`"failure_cause":{"kind":"secret_grant","secret_id":"sec_gh","secret_problem":"disabled"}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("runs response %s lacks %s", body, want)
		}
	}
}
