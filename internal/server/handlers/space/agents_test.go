package space

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/testsupport"
	"github.com/icloudbb/buildmax/internal/util"
)

const agentTestSecret = "agent-test-secret"

// TestAgentRevisionEndpoints walks one agent through an edit, its history, and a
// restore, which is the loop the endpoints exist to support.
func TestAgentRevisionEndpoints(t *testing.T) {
	spaceID := "tm_1"
	agentStore := &mock.MockAgentStore{}
	spaceStore := &mock.MockSpaceStore{
		Spaces:  []corespace.Space{{ID: spaceID, Name: "Space", CreatedBy: "u1"}},
		Members: []corespace.Member{{SpaceID: spaceID, UserID: "u1", Role: corespace.RoleOwner}},
	}
	created, err := agentStore.CreateAgentInSpace(t.Context(), agentdef.CreateInput{SpaceID: spaceID, UserID: "u1",
		Def: agentdef.Definition{Name: "Collector", Description: "collects", Instructions: "collect carefully"}})
	if err != nil {
		t.Fatalf("CreateAgentInSpace: %v", err)
	}
	if _, err := agentStore.UpdateAgentInSpace(t.Context(), agentdef.UpdateInput{AgentID: created.ID, SpaceID: spaceID, UpdatedBy: "u1",
		Def: agentdef.Definition{Name: "Collector", Description: "collects", Instructions: "collect faster"}}); err != nil {
		t.Fatalf("UpdateAgentInSpace: %v", err)
	}

	h := New(Config{JWTSecret: agentTestSecret, Spaces: spaceStore, Agents: agentStore})
	mux := http.NewServeMux()
	h.Register(mux)
	token := "Bearer " + testsupport.SignJWT("u1", agentTestSecret)
	base := "/api/spaces/" + spaceID + "/agents/" + created.ID

	req := httptest.NewRequest(http.MethodGet, base+"/revisions", nil)
	req.Header.Set("Authorization", token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list revisions status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Revisions []struct {
			Revision     int    `json:"revision"`
			Instructions string `json:"instructions"`
			CreatedBy    string `json:"created_by"`
		} `json:"revisions"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode revisions: %v", err)
	}
	if list.Total != 2 {
		t.Fatalf("total revisions = %d, want 2", list.Total)
	}
	if list.Revisions[0].Revision != 2 || list.Revisions[0].Instructions != "collect faster" {
		t.Fatalf("newest revision = %d %q, want 2 \"collect faster\"", list.Revisions[0].Revision, list.Revisions[0].Instructions)
	}

	req = httptest.NewRequest(http.MethodPost, base+"/revisions/1/restore", nil)
	req.Header.Set("Authorization", token)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("restore status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var restored struct {
		Instructions string `json:"instructions"`
		Revision     int    `json:"revision"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &restored); err != nil {
		t.Fatalf("decode restore: %v", err)
	}
	if restored.Instructions != "collect carefully" {
		t.Fatalf("restored instructions = %q, want the revision 1 text", restored.Instructions)
	}
	if restored.Revision != 3 {
		t.Fatalf("restored revision = %d, want 3 — a restore appends", restored.Revision)
	}

	req = httptest.NewRequest(http.MethodPost, base+"/revisions/99/restore", nil)
	req.Header.Set("Authorization", token)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("restore of missing revision status = %d, want 404", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, base+"/revisions/abc/restore", nil)
	req.Header.Set("Authorization", token)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("restore with non-numeric revision status = %d, want 400", rec.Code)
	}
}

// TestDeleteAgentRefusedWhilePublishedWorkflowUsesIt keeps deletion from quietly
// breaking automation: a published workflow that names the agent has to be fixed
// or archived first, and the response says which one.
func TestDeleteAgentRefusedWhilePublishedWorkflowUsesIt(t *testing.T) {
	spaceID := "tm_1"
	agentStore := &mock.MockAgentStore{
		Agents: []agentdef.Agent{{ID: "a_1", UserID: "u1", SpaceID: spaceID, Name: "Collector", Revision: 1}},
	}
	workflowStore := &mock.MockWorkflowStore{
		Workflows: []coreworkflow.Workflow{{
			ID:         "w_1",
			SpaceID:    spaceID,
			Name:       "Nightly report",
			Definition: `{"schema_version":1,"steps":[{"step_id":"s","type":"agent_task","target_agent_id":"a_1","prompt":"p"}]}`,
			Status:     coreworkflow.StatusPublished,
		}},
	}
	spaceStore := &mock.MockSpaceStore{
		Spaces:  []corespace.Space{{ID: spaceID, Name: "Space", CreatedBy: "u1"}},
		Members: []corespace.Member{{SpaceID: spaceID, UserID: "u1", Role: corespace.RoleOwner}},
	}
	h := New(Config{
		JWTSecret: agentTestSecret,
		Spaces:    spaceStore,
		Agents:    agentStore,
		Workflows: workflowStore,
	})
	mux := http.NewServeMux()
	h.Register(mux)
	token := "Bearer " + testsupport.SignJWT("u1", agentTestSecret)
	url := "/api/spaces/" + spaceID + "/agents/a_1"

	req := httptest.NewRequest(http.MethodDelete, url, nil)
	req.Header.Set("Authorization", token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Nightly report (w_1)") {
		t.Errorf("body %q should name the workflow blocking the delete", rec.Body.String())
	}
	live, err := agentStore.ListAgentsBySpace(t.Context(), spaceID)
	if err != nil {
		t.Fatalf("ListAgentsBySpace: %v", err)
	}
	if len(live) != 1 {
		t.Fatalf("agents after refused delete = %d, want 1", len(live))
	}

	// Archiving the workflow releases the agent.
	archived := coreworkflow.StatusArchived
	if _, err := workflowStore.UpdateWorkflow(t.Context(), "w_1", spaceID, coreworkflow.UpdateInput{Status: &archived}); err != nil {
		t.Fatalf("archive workflow: %v", err)
	}
	req = httptest.NewRequest(http.MethodDelete, url, nil)
	req.Header.Set("Authorization", token)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status after archiving = %d, want 204: %s", rec.Code, rec.Body.String())
	}
}

func TestPatchAgentHandler(t *testing.T) {
	personalSpaceID := "tm_personal_u1"
	agentStore := &mock.MockAgentStore{
		Agents: []agentdef.Agent{
			{ID: "a_1", UserID: "u1", SpaceID: personalSpaceID, Name: "Old", Description: "d1", Instructions: "i1", CreatedAt: time.Unix(100, 0).UTC()},
		},
	}
	spaceStore := &mock.MockSpaceStore{
		Spaces:  []corespace.Space{{ID: personalSpaceID, Name: "My Space", PersonalForUserID: util.Ptr("u1"), CreatedBy: "u1"}},
		Members: []corespace.Member{{SpaceID: personalSpaceID, UserID: "u1", Role: corespace.RoleOwner}, {SpaceID: personalSpaceID, UserID: "u2", Role: corespace.RoleMember}},
	}

	tests := []struct {
		name        string
		method      string
		url         string
		body        string
		authHeader  string
		wantStatus  int
		wantBodyHas string
	}{
		{
			name:        "PATCH success",
			method:      http.MethodPatch,
			url:         "/api/spaces/" + personalSpaceID + "/agents/a_1",
			body:        `{"name":"Updated","description":"d2","instructions":"i2","model":"Fast"}`,
			authHeader:  "Bearer " + testsupport.SignJWT("u1", agentTestSecret),
			wantStatus:  http.StatusOK,
			wantBodyHas: "Updated",
		},
		{
			name:        "PATCH empty name returns 400",
			method:      http.MethodPatch,
			url:         "/api/spaces/" + personalSpaceID + "/agents/a_1",
			body:        `{"name":""}`,
			authHeader:  "Bearer " + testsupport.SignJWT("u1", agentTestSecret),
			wantStatus:  http.StatusBadRequest,
			wantBodyHas: "name required",
		},
		{
			name:        "PATCH forbidden for member",
			method:      http.MethodPatch,
			url:         "/api/spaces/" + personalSpaceID + "/agents/a_1",
			body:        `{"name":"Updated","description":"d2","instructions":"i2"}`,
			authHeader:  "Bearer " + testsupport.SignJWT("u2", agentTestSecret),
			wantStatus:  http.StatusForbidden,
			wantBodyHas: "forbidden",
		},
		{
			name:        "PATCH non-existent agent returns 404",
			method:      http.MethodPatch,
			url:         "/api/spaces/" + personalSpaceID + "/agents/a_999",
			body:        `{"name":"X","description":"","instructions":""}`,
			authHeader:  "Bearer " + testsupport.SignJWT("u1", agentTestSecret),
			wantStatus:  http.StatusNotFound,
			wantBodyHas: "not found",
		},
		{
			name:       "PATCH no auth returns 401",
			method:     http.MethodPatch,
			url:        "/api/spaces/" + personalSpaceID + "/agents/a_1",
			body:       `{"name":"X"}`,
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := New(Config{
				JWTSecret: agentTestSecret,
				Spaces:    spaceStore,
				Agents:    agentStore,
			})
			mux := http.NewServeMux()
			h.Register(mux)
			req := httptest.NewRequest(tt.method, tt.url, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantBodyHas != "" && !strings.Contains(rec.Body.String(), tt.wantBodyHas) {
				t.Errorf("body %q does not contain %q", rec.Body.String(), tt.wantBodyHas)
			}
			if tt.wantStatus == http.StatusOK {
				var out struct {
					ID           string `json:"id"`
					Name         string `json:"name"`
					Description  string `json:"description"`
					Instructions string `json:"instructions"`
					Model        string `json:"model"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				if out.ID != "a_1" || out.Name != "Updated" || out.Description != "d2" || out.Instructions != "i2" || out.Model != "Fast" {
					t.Errorf("response id=%q name=%q description=%q instructions=%q model=%q", out.ID, out.Name, out.Description, out.Instructions, out.Model)
				}
			}
		})
	}
}

func TestDeleteAgentHandler(t *testing.T) {
	personalSpaceID := "tm_personal_u1"
	tests := []struct {
		name        string
		url         string
		authHeader  string
		wantStatus  int
		wantBodyHas string
	}{
		{
			name:       "DELETE success returns 204",
			url:        "/api/spaces/" + personalSpaceID + "/agents/a_1",
			authHeader: "Bearer " + testsupport.SignJWT("u1", agentTestSecret),
			wantStatus: http.StatusNoContent,
		},
		{
			name:        "DELETE non-existent agent returns 404",
			url:         "/api/spaces/" + personalSpaceID + "/agents/a_999",
			authHeader:  "Bearer " + testsupport.SignJWT("u1", agentTestSecret),
			wantStatus:  http.StatusNotFound,
			wantBodyHas: "not found",
		},
		{
			name:       "DELETE no auth returns 401",
			url:        "/api/spaces/" + personalSpaceID + "/agents/a_1",
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &mock.MockAgentStore{
				Agents: []agentdef.Agent{
					{ID: "a_1", UserID: "u1", SpaceID: personalSpaceID, Name: "ToDelete", CreatedAt: time.Unix(100, 0).UTC()},
				},
			}
			spaceStore := &mock.MockSpaceStore{
				Spaces:  []corespace.Space{{ID: personalSpaceID, Name: "My Space", PersonalForUserID: util.Ptr("u1"), CreatedBy: "u1"}},
				Members: []corespace.Member{{SpaceID: personalSpaceID, UserID: "u1", Role: corespace.RoleOwner}, {SpaceID: personalSpaceID, UserID: "u2", Role: corespace.RoleMember}},
			}
			h := New(Config{
				JWTSecret: agentTestSecret,
				Spaces:    spaceStore,
				Agents:    store,
			})
			mux := http.NewServeMux()
			h.Register(mux)
			req := httptest.NewRequest(http.MethodDelete, tt.url, nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantBodyHas != "" && !strings.Contains(rec.Body.String(), tt.wantBodyHas) {
				t.Errorf("body %q does not contain %q", rec.Body.String(), tt.wantBodyHas)
			}
			if tt.wantStatus == http.StatusNoContent {
				// The row survives so records that name the agent still resolve;
				// what must go is its visibility as something to use.
				live, err := store.ListAgentsBySpace(t.Context(), personalSpaceID)
				if err != nil {
					t.Fatalf("ListAgentsBySpace: %v", err)
				}
				if len(live) != 0 {
					t.Errorf("after DELETE, space lists %d agents, want 0", len(live))
				}
				kept, err := store.GetAgentIncludingDeleted(t.Context(), "a_1")
				if err != nil {
					t.Fatalf("GetAgentIncludingDeleted: %v", err)
				}
				if kept == nil || kept.DeletedAt == nil {
					t.Error("after DELETE, the agent row should remain with deleted_at set")
				}
			}
		})
	}
}
