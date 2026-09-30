package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/audit"
	"github.com/icloudbb/buildmax/internal/util"
)

// leakMarker stands for everything a Space's members wrote or an Agent
// produced. None of it may reach an administration response.
const leakMarker = "LEAKMARK-NIGHTJAR"

func runtimeMux(t *testing.T, runs coretask.RunStore) *http.ServeMux {
	t.Helper()
	users := &mock.MockUserStore{}
	seedUser(t, users, adminUser, "admin@example.com")
	seedUser(t, users, "u_alice", "alice@example.com")
	seedUser(t, users, "u_bob", "bob@example.com")
	grants := &mock.MockSystemGrantStore{}
	grants.GrantForTest(adminUser, coreidentity.SystemRoleAdmin)
	spaces := &mock.MockSpaceStore{
		Spaces: []corespace.Space{
			{ID: "sp_team", Name: "Payments"},
			{ID: "sp_personal", Name: "My Space", PersonalForUserID: util.Ptr("u_bob")},
			{ID: "sp_approvals", Name: "Approvals"},
		},
		Members: []corespace.Member{
			{SpaceID: "sp_team", UserID: "u_alice", Role: corespace.RoleOwner},
			{SpaceID: "sp_team", UserID: "u_bob", Role: corespace.RoleMember},
			{SpaceID: "sp_personal", UserID: "u_bob", Role: corespace.RoleOwner},
			{SpaceID: "sp_approvals", UserID: "u_alice", Role: corespace.RoleOwner},
		},
	}
	audits := &mock.MockAuditStore{}
	h := New(Config{
		JWTSecret: testSecret, Grants: grants, Users: users, Spaces: spaces,
		TaskRuns: runs, Audits: audits, Audit: audit.NewRecorder(audits),
	})
	mux := http.NewServeMux()
	h.Register(mux)
	return mux
}

func drillRuns() *mock.MockTaskRunStore {
	now := time.Now().UTC()
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	ended := func(d time.Duration) *time.Time { t := ago(d); return &t }
	secret := leakMarker + " https://buildmax-worker-api.internal:5679/api/worker/task-runs/x"
	return &mock.MockTaskRunStore{
		TaskList: []coretask.Task{
			{ID: "t_team", SpaceID: "sp_team", Input: leakMarker, Title: leakMarker},
			{ID: "t_personal", SpaceID: "sp_personal", Input: leakMarker},
		},
		Runs: []coretask.Run{
			{ID: "r_waiting", TaskID: "t_team", Status: "SCHEDULED", Input: leakMarker, CreatedAt: ago(30 * time.Minute)},
			{ID: "r_pending", TaskID: "t_team", Status: "PENDING", Input: leakMarker, CreatedAt: ago(5 * time.Minute)},
			{ID: "r_storage", TaskID: "t_personal", Status: "FAILED", Input: leakMarker, ErrorMessage: &secret,
				FailureClass: string(coretask.FailureInfrastructure), CreatedAt: ago(2 * time.Hour), EndedAt: ended(time.Hour)},
			{ID: "r_old", TaskID: "t_team", Status: "FAILED", ErrorMessage: &secret,
				FailureClass: string(coretask.FailureModel), CreatedAt: ago(72 * time.Hour), EndedAt: ended(71 * time.Hour)},
			{ID: "r_done", TaskID: "t_team", Status: "SUCCEEDED", Output: util.Ptr(leakMarker), CreatedAt: ago(time.Hour)},
		},
		Workflows: drillWorkflows(),
	}
}

// drillWorkflows is the Workflow half of the drill: a human_input approval in
// a Space with no active TaskRun, which has waited three days, and a personal
// Space's Workflow run that failed on its output schema although its node's
// TaskRun succeeded. Neither shows up in TaskRun state.
func drillWorkflows() *mock.MockWorkflowStore {
	now := time.Now().UTC()
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	ended := func(d time.Duration) *time.Time { t := ago(d); return &t }
	expires := ago(-2 * time.Hour)
	return &mock.MockWorkflowStore{
		Workflows: []coreworkflow.Workflow{
			{ID: "wf_approvals", SpaceID: "sp_approvals", Name: leakMarker, Definition: leakMarker},
			{ID: "wf_personal", SpaceID: "sp_personal", Name: leakMarker},
		},
		Runs: []coreworkflow.Run{
			{ID: "wr_waiting", WorkflowID: "wf_approvals", Status: "running", Input: util.Ptr(leakMarker), CreatedAt: ago(80 * time.Hour)},
			{ID: "wr_schema", WorkflowID: "wf_personal", Status: "failed", ErrorMessage: util.Ptr(leakMarker),
				FailureClass: string(coreworkflow.FailureOutputSchema), CreatedAt: ago(3 * time.Hour), EndedAt: ended(2 * time.Hour)},
			{ID: "wr_old", WorkflowID: "wf_personal", Status: "failed", ErrorMessage: util.Ptr(leakMarker),
				FailureClass: string(coreworkflow.FailureRunDeadline), CreatedAt: ago(90 * time.Hour), EndedAt: ended(80 * time.Hour)},
		},
		Requests: []coreworkflow.Request{
			{ID: "req_approve", WorkflowRunID: "wr_waiting", NodeID: leakMarker, Kind: coreworkflow.RequestKindInput,
				Prompt: leakMarker, Status: coreworkflow.RequestStatusPending, ExpiresAt: &expires, CreatedAt: ago(72 * time.Hour)},
			{ID: "req_question", WorkflowRunID: "wr_waiting", Kind: coreworkflow.RequestKindQuestion,
				Questions: util.Ptr(leakMarker), Status: coreworkflow.RequestStatusPending, CreatedAt: ago(time.Hour)},
			{ID: "req_answered", WorkflowRunID: "wr_waiting", Kind: coreworkflow.RequestKindInput,
				Response: util.Ptr(leakMarker), Status: coreworkflow.RequestStatusAnswered, CreatedAt: ago(79 * time.Hour)},
		},
	}
}

func TestRuntimeSpacesListsAffectedSpacesWithOwnersAndNoContent(t *testing.T) {
	mux := runtimeMux(t, drillRuns())
	code, body := getAs(t, mux, "/api/admin/runtime/spaces", adminUser)
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", code, body)
	}
	if strings.Contains(body, leakMarker) || strings.Contains(body, "worker-api.internal") {
		t.Fatalf("response carries Space content: %s", body)
	}
	var resp AdminSpacesAttentionResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Total != 3 || len(resp.Spaces) != 3 {
		t.Fatalf("spaces = %+v, want the approvals, team, and personal Spaces", resp.Spaces)
	}
	// The approval has waited longest, though no TaskRun is active there.
	approvals, team, personal := resp.Spaces[0], resp.Spaces[1], resp.Spaces[2]
	if approvals.SpaceID != "sp_approvals" || len(approvals.Active) != 0 {
		t.Fatalf("first = %+v, want the Space waiting on a person", approvals)
	}
	if approvals.WaitingRequests[coreworkflow.RequestKindInput] != 1 || approvals.WaitingRequests[coreworkflow.RequestKindQuestion] != 1 {
		t.Errorf("waiting = %v, want one input and one question, not the answered one", approvals.WaitingRequests)
	}
	if approvals.OldestWaitingRequestAt == nil || time.Since(*approvals.OldestWaitingRequestAt) < 71*time.Hour {
		t.Errorf("oldest waiting = %v, want the three-day-old approval", approvals.OldestWaitingRequestAt)
	}
	if approvals.NextRequestExpiryAt == nil || approvals.OldestWaitingWorkflowRunID != "wr_waiting" {
		t.Errorf("approvals = %+v, want its expiry and the waiting run's id", approvals)
	}
	if len(approvals.Owners) != 1 || approvals.Owners[0].Email != "alice@example.com" {
		t.Errorf("approvals owners = %+v, want alice", approvals.Owners)
	}
	if team.SpaceID != "sp_team" || team.Active["SCHEDULED"] != 1 || team.Active["PENDING"] != 1 {
		t.Errorf("team = %+v, want its SCHEDULED and PENDING runs", team)
	}
	if len(team.Owners) != 1 || team.Owners[0].Email != "alice@example.com" {
		t.Errorf("team owners = %+v, want alice only", team.Owners)
	}
	// A failure outside the window is history, not something needing attention.
	if len(team.Failures) != 0 {
		t.Errorf("team failures = %v, want none inside the window", team.Failures)
	}
	if !personal.Personal || personal.Failures[string(coretask.FailureInfrastructure)] != 1 {
		t.Errorf("personal = %+v, want the personal Space with its infrastructure failure", personal)
	}
	if len(personal.Owners) != 1 || personal.Owners[0].Email != "bob@example.com" {
		t.Errorf("personal owners = %+v, want bob", personal.Owners)
	}
	// The Workflow failure is counted apart from TaskRun failures, and one
	// outside the window is not.
	schema, deadline := string(coreworkflow.FailureOutputSchema), string(coreworkflow.FailureRunDeadline)
	if personal.WorkflowFailures[schema] != 1 || personal.WorkflowFailures[deadline] != 0 {
		t.Errorf("personal workflow failures = %v, want one output_schema failure only", personal.WorkflowFailures)
	}
	if personal.LatestFailedWorkflowRunID != "wr_schema" {
		t.Errorf("latest failed = %q, want wr_schema", personal.LatestFailedWorkflowRunID)
	}
	if len(team.WaitingRequests) != 0 || len(team.WorkflowFailures) != 0 {
		t.Errorf("team = %+v, want no Workflow facts", team)
	}
}

func TestSystemStatusReportsRuntimeWithoutContent(t *testing.T) {
	mux := runtimeMux(t, drillRuns())
	code, body := getAs(t, mux, "/api/admin/system", adminUser)
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", code, body)
	}
	if strings.Contains(body, leakMarker) {
		t.Fatalf("response carries Space content: %s", body)
	}
	var resp AdminSystemResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	rt := resp.Runtime
	if rt == nil {
		t.Fatal("runtime missing")
	}
	if rt.OldestPendingAt == nil || rt.OldestUnstartedAt == nil {
		t.Errorf("runtime = %+v, want both stall timestamps", rt)
	}
	if rt.Failures[string(coretask.FailureInfrastructure)] != 1 || rt.Failures[string(coretask.FailureModel)] != 0 {
		t.Errorf("failures = %v, want only the in-window infrastructure failure", rt.Failures)
	}
	if rt.StaleAfterSeconds != int(coretask.WorkerLivenessGrace/time.Second) || rt.FailureWindowHours != 24 {
		t.Errorf("runtime bounds = %+v", rt)
	}
	if rt.WaitingRequests[coreworkflow.RequestKindInput] != 1 || rt.WaitingRequests[coreworkflow.RequestKindQuestion] != 1 ||
		rt.OldestWaitingRequestAt == nil || rt.NextRequestExpiryAt == nil {
		t.Errorf("waiting = %+v, want the pending input and question with their age and expiry", rt.adminWorkflowRuntime)
	}
	if rt.WorkflowFailures[string(coreworkflow.FailureOutputSchema)] != 1 || rt.WorkflowFailures[string(coreworkflow.FailureRunDeadline)] != 0 {
		t.Errorf("workflow failures = %v, want only the in-window output_schema failure", rt.WorkflowFailures)
	}
}

type unavailableRuntime struct{ *mock.MockTaskRunStore }

func (unavailableRuntime) RuntimeSummary(context.Context, time.Time, time.Time) (coretask.RuntimeSummary, error) {
	return coretask.RuntimeSummary{}, errors.New("database unavailable")
}

// Unavailable is not the same answer as "nothing stalled": the field is left
// out so Portal can say so.
func TestSystemStatusOmitsRuntimeItCannotRead(t *testing.T) {
	mux := runtimeMux(t, unavailableRuntime{drillRuns()})
	code, body := getAs(t, mux, "/api/admin/system", adminUser)
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", code, body)
	}
	if strings.Contains(body, `"runtime"`) {
		t.Errorf("body = %s, want no runtime when it could not be read", body)
	}
}
