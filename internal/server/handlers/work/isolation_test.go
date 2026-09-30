package work

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/testsupport"
)

// A resource is reached under a Space path, and a resource that belongs to
// another Space must read as 404 even to a member of the Space in the path —
// the id is not an existence oracle, and this is the scoping that keeps one
// tenant's work invisible to another. These pin that a Space-B member gets 404
// for a Space-A task / workflow / workflow run / conversation, so a dropped
// `SpaceID != spaceID` check fails a test rather than silently leaking.
func TestCrossSpaceResourceReadsAre404(t *testing.T) {
	const secret = "iso-secret"
	const spA, spB = "tm_a", "tm_b"
	// The caller u2 is a member of Space B only, never of Space A.
	spaces := &mock.MockSpaceStore{
		Spaces: []corespace.Space{
			{ID: spA, Name: "A", CreatedBy: "u1"},
			{ID: spB, Name: "B", CreatedBy: "u2"},
		},
		Members: []corespace.Member{
			{SpaceID: spA, UserID: "u1", Role: corespace.RoleOwner},
			{SpaceID: spB, UserID: "u2", Role: corespace.RoleOwner},
		},
	}
	agentID := "a_1"
	taskA := coretask.Task{ID: "t_a", SpaceID: spA, Status: "SUCCEEDED", Input: "a", CreatedBy: "u1", CreatedAt: time.Unix(1, 0).UTC(), AgentID: &agentID}
	wfA := coreworkflow.Workflow{ID: "wf_a", SpaceID: spA, Name: "A", Status: coreworkflow.StatusPublished}
	runA := coreworkflow.Run{ID: "wr_a", WorkflowID: "wf_a", Status: string(coreworkflow.RunStatusRunning)}
	convA := coreconv.Conversation{ID: "cv_a", SpaceID: spA, UserID: "u1"}

	h := New(Config{
		JWTSecret:     secret,
		Spaces:        spaces,
		Tasks:         &mock.MockTaskStore{List: []coretask.Task{taskA}},
		TaskRuns:      &mock.MockTaskRunStore{},
		Workflows:     &mock.MockWorkflowStore{Workflows: []coreworkflow.Workflow{wfA}, Runs: []coreworkflow.Run{runA}},
		Conversations: &mock.MockConversationStore{Conversations: []coreconv.Conversation{convA}},
		Messages:      &mock.MockConversationMessageStore{},
	})
	mux := http.NewServeMux()
	h.Register(mux)

	// u2 (Space B member) asks for Space A's resources through Space B's path.
	cases := []struct {
		name string
		path string
	}{
		{"task", "/api/spaces/" + spB + "/tasks/t_a"},
		{"task runs", "/api/spaces/" + spB + "/tasks/t_a/runs"},
		{"workflow", "/api/spaces/" + spB + "/workflows/wf_a"},
		{"workflow run", "/api/spaces/" + spB + "/workflow-runs/wr_a"},
		{"conversation messages", "/api/spaces/" + spB + "/conversations/cv_a/messages"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u2", secret))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s: status = %d, want 404 (a cross-space read must not succeed or leak existence); body = %s", tc.name, rec.Code, rec.Body.String())
			}
		})
	}

	// And a non-member of Space A is refused Space A's own path outright.
	t.Run("non-member is refused Space A's path", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/spaces/"+spA+"/workflows/wf_a", nil)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u2", secret))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 403 or 404 for a non-member", rec.Code)
		}
	})

	// The owner of Space A still reads its own resources.
	t.Run("owner reads its own workflow", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/spaces/"+spA+"/workflows/wf_a", nil)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", secret))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("owner status = %d, want 200; body = %s", rec.Code, rec.Body.String())
		}
	})

}
