package work

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/testsupport"
	"github.com/icloudbb/buildmax/internal/util"
)

const needsMeSecret = "needs-me-secret"

// The route serves the four groups the service decides, in the shapes the
// existing Issue, Task, and Workflow request reads use, and refuses a
// stranger like every Space route.
func TestNeedsMeHandler(t *testing.T) {
	const space = "tm_needs"
	now := time.Unix(1_700_000_000, 0).UTC()
	issues := &mock.MockIssueStore{
		Issues: []coreissue.Issue{
			{ID: "i_failed", SpaceID: space, Title: "Fix the release notes", Status: coreissue.StatusInProgress,
				OwnerID: util.Ptr("u_alice"), CreatedBy: "u_alice", CreatedAt: now, UpdatedAt: now, Version: 1},
			{ID: "i_quiet", SpaceID: space, Title: "Nothing waiting", Status: coreissue.StatusTodo,
				OwnerID: util.Ptr("u_alice"), CreatedBy: "u_alice", CreatedAt: now, UpdatedAt: now, Version: 1},
		},
		Attention: map[string]coreissue.AttentionReason{"i_failed": coreissue.AttentionRunFailed},
	}
	tasks := &mock.MockTaskStore{List: []coretask.Task{
		{ID: "t_failed", SpaceID: space, Status: string(coretask.RunStatusFailed), CreatedBy: "u_alice", CreatedAt: now,
			FailureClass: string(coretask.FailureRun)},
		{ID: "t_asking", SpaceID: space, Status: string(coretask.RunStatusSucceeded), AwaitingAnswer: true,
			CreatedBy: "u_bob", RequestedBy: "u_alice", CreatedAt: now},
		{ID: "t_bobs", SpaceID: space, Status: string(coretask.RunStatusFailed), CreatedBy: "u_bob", CreatedAt: now},
	}}
	workflows := &mock.MockWorkflowStore{
		Workflows: []coreworkflow.Workflow{{ID: "w_signoff", SpaceID: space, Name: "Sign-off"}},
		Runs:      []coreworkflow.Run{{ID: "wr_1", WorkflowID: "w_signoff", Status: string(coreworkflow.RunStatusRunning)}},
		NodeRuns: []coreworkflow.NodeRun{{ID: "nr_1", WorkflowRunID: "wr_1", NodeID: "approve",
			NodeType: coreworkflow.NodeTypeHumanInput, Status: string(coreworkflow.NodeRunStatusPending)}},
	}
	if _, opened, err := workflows.OpenWorkflowRequest(t.Context(), coreworkflow.OpenRequestInput{
		WorkflowRunID: "wr_1", NodeRunID: "nr_1", NodeExpected: coreworkflow.NodeRunStatusPending,
		Key: "node/approve", Kind: coreworkflow.RequestKindInput, Prompt: "Approve the notes?", Now: now,
	}); err != nil || !opened {
		t.Fatalf("open request: %v %v", opened, err)
	}
	spaces := &mock.MockSpaceStore{
		Spaces: []corespace.Space{{ID: space, Name: "QA", CreatedBy: "u_alice"}},
		Members: []corespace.Member{
			{SpaceID: space, UserID: "u_alice", Role: corespace.RoleOwner},
			{SpaceID: space, UserID: "u_bob", Role: corespace.RoleMember},
		},
	}
	mux := http.NewServeMux()
	New(Config{JWTSecret: needsMeSecret, Spaces: spaces, Issues: issues, Tasks: tasks, Workflows: workflows}).Register(mux)

	get := func(user string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/spaces/"+space+"/needs-me", nil)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT(user, needsMeSecret))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	rec := get("u_alice")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var out needsMeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.AttentionIssues.Total != 1 || len(out.AttentionIssues.Items) != 1 ||
		out.AttentionIssues.Items[0].Issue.ID != "i_failed" || out.AttentionIssues.Items[0].Reason != "run_failed" {
		t.Errorf("attention issues = %+v", out.AttentionIssues)
	}
	if out.FailedTasks.Total != 1 || out.FailedTasks.Items[0].ID != "t_failed" || out.FailedTasks.Items[0].FailureClass != "run" {
		t.Errorf("failed tasks = %+v", out.FailedTasks)
	}
	if out.AwaitingAnswerTasks.Total != 1 || out.AwaitingAnswerTasks.Items[0].ID != "t_asking" {
		t.Errorf("awaiting answer = %+v", out.AwaitingAnswerTasks)
	}
	if out.WorkflowRequests.Total != 1 || out.WorkflowRequests.Items[0].WorkflowID != "w_signoff" ||
		out.WorkflowRequests.Items[0].WorkflowRunID != "wr_1" {
		t.Errorf("workflow requests = %+v", out.WorkflowRequests)
	}

	// An empty group is an empty list, not null: a client renders it as
	// "nothing waiting" without a special case.
	var raw map[string]struct {
		Items json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(get("u_bob").Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	if string(raw["attention_issues"].Items) != "[]" {
		t.Errorf("bob's attention issues = %s, want []", raw["attention_issues"].Items)
	}

	if rec := get("u_stranger"); rec.Code != http.StatusForbidden {
		t.Errorf("a non-member got %d, want 403", rec.Code)
	}
}
