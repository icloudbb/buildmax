package space

import (
	"encoding/json"
	"net/http"
	"testing"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/testsupport"
)

const scheduleTestSecret = "schedule-test-secret"

// scheduleTestHandler wires a space with an owner and a plain member, one agent,
// and an empty schedule store. It returns the mux and the member's bearer token,
// because schedule management is a member-level action and the member is the
// interesting caller.
func scheduleTestHandler(t *testing.T) (*http.ServeMux, string, string) {
	t.Helper()
	spaceID := "tm_1"
	spaceStore := &mock.MockSpaceStore{
		Spaces: []corespace.Space{{ID: spaceID, Name: "Space", CreatedBy: "u1"}},
		Members: []corespace.Member{
			{SpaceID: spaceID, UserID: "u1", Role: corespace.RoleOwner},
			{SpaceID: spaceID, UserID: "u2", Role: corespace.RoleMember},
		},
	}
	agentStore := &mock.MockAgentStore{}
	agent, err := agentStore.CreateAgentInSpace(t.Context(), agentdef.CreateInput{
		SpaceID: spaceID, UserID: "u1",
		Def: agentdef.Definition{Name: "Summarizer", Description: "d", Instructions: "i"},
	})
	if err != nil {
		t.Fatalf("CreateAgentInSpace: %v", err)
	}
	// Published workflows (schedulable) -- one that takes typed input, one that
	// takes none, one that needs an Issue a firing cannot supply -- and a draft
	// (not schedulable) in the same space.
	node := `{"id":"one","type":"agent_task","agent":{"id":"` + agent.ID + `"},"input":{"instruction":"go"}}`
	workflowStore := &mock.MockWorkflowStore{Workflows: []coreworkflow.Workflow{
		{ID: publishedWorkflowID, SpaceID: spaceID, Name: "Pub", Status: coreworkflow.StatusPublished, Revision: 1, CreatedBy: "u1",
			Definition: `{"schema_version":1,"input_schema":{"type":"object","properties":{"k":{"type":"integer"}},"additionalProperties":false},"nodes":[` + node + `]}`},
		{ID: noInputWorkflowID, SpaceID: spaceID, Name: "NoInput", Status: coreworkflow.StatusPublished, Revision: 1, CreatedBy: "u1",
			Definition: `{"schema_version":1,"nodes":[` + node + `]}`},
		{ID: issueWorkflowID, SpaceID: spaceID, Name: "NeedsIssue", Status: coreworkflow.StatusPublished, Revision: 1, CreatedBy: "u1",
			Definition: `{"schema_version":1,"nodes":[{"id":"one","type":"agent_task","agent":{"id":"` + agent.ID + `"},"issue_access":"required","input":{"instruction":"go"}}]}`},
		{ID: draftWorkflowID, SpaceID: spaceID, Name: "Draft", Status: coreworkflow.StatusDraft, Revision: 1, CreatedBy: "u1"},
	}}
	h := New(Config{JWTSecret: scheduleTestSecret, Spaces: spaceStore, Agents: agentStore, Workflows: workflowStore, Schedules: &mock.MockScheduleStore{}})
	mux := http.NewServeMux()
	h.Register(mux)
	token := "Bearer " + testsupport.SignJWT("u2", scheduleTestSecret)
	return mux, token, agent.ID
}

const (
	publishedWorkflowID = "w_pub"
	noInputWorkflowID   = "w_noinput"
	issueWorkflowID     = "w_issue"
	draftWorkflowID     = "w_draft"
)

// A member walks a schedule through its whole lifecycle: create, read, list,
// pause, and delete.
func TestScheduleLifecycle(t *testing.T) {
	mux, token, agentID := scheduleTestHandler(t)
	base := "/api/spaces/tm_1/schedules"

	rec := doJSON(t, mux, http.MethodPost, base, token,
		`{"executor_kind":"agent","executor_id":"`+agentID+`","name":"nightly","input":"summarize","cron_expr":"0 9 * * *","timezone":"UTC"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var created ScheduleResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if !created.Enabled || created.ExecutorKind != "agent" || created.ExecutorID != agentID || created.CreatedBy != "u2" {
		t.Fatalf("created schedule = %+v, want enabled, the agent, and the member as creator", created)
	}
	if created.NextFireAt.IsZero() {
		t.Error("created schedule has no next fire time")
	}

	rec = doJSON(t, mux, http.MethodGet, base+"/"+created.ID, token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, mux, http.MethodGet, base, token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", rec.Code)
	}
	var list scheduleListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if list.Total != 1 || len(list.Schedules) != 1 {
		t.Fatalf("list total = %d, want 1", list.Total)
	}

	rec = doJSON(t, mux, http.MethodPatch, base+"/"+created.ID, token, `{"enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var patched ScheduleResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &patched); err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	if patched.Enabled {
		t.Error("schedule still enabled after a disable patch")
	}

	rec = doJSON(t, mux, http.MethodDelete, base+"/"+created.ID, token, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", rec.Code)
	}
	rec = doJSON(t, mux, http.MethodGet, base+"/"+created.ID, token, "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("get after delete = %d, want 404", rec.Code)
	}
}

// Create refuses an invalid cron expression, an agent that is not in the space,
// and an empty input — each as a 400, not a 500.
func TestCreateScheduleValidation(t *testing.T) {
	mux, token, agentID := scheduleTestHandler(t)
	base := "/api/spaces/tm_1/schedules"

	cases := []struct {
		name string
		body string
	}{
		{"bad cron", `{"executor_kind":"agent","executor_id":"` + agentID + `","input":"x","cron_expr":"nonsense","timezone":"UTC"}`},
		{"unknown agent", `{"executor_kind":"agent","executor_id":"a_nope","input":"x","cron_expr":"0 9 * * *","timezone":"UTC"}`},
		{"empty input", `{"executor_kind":"agent","executor_id":"` + agentID + `","input":"","cron_expr":"0 9 * * *","timezone":"UTC"}`},
		{"bad timezone", `{"executor_kind":"agent","executor_id":"` + agentID + `","input":"x","cron_expr":"0 9 * * *","timezone":"Mars/Phobos"}`},
		{"missing executor kind", `{"executor_id":"` + agentID + `","input":"x","cron_expr":"0 9 * * *","timezone":"UTC"}`},
		{"unknown workflow", `{"executor_kind":"workflow","executor_id":"w_nope","input":"x","cron_expr":"0 9 * * *","timezone":"UTC"}`},
		{"draft workflow", `{"executor_kind":"workflow","executor_id":"` + draftWorkflowID + `","input":"x","cron_expr":"0 9 * * *","timezone":"UTC"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := doJSON(t, mux, http.MethodPost, base, token, c.body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// A schedule can fire a published workflow: creating one records the workflow as
// the executor.
func TestCreateWorkflowSchedule(t *testing.T) {
	mux, token, _ := scheduleTestHandler(t)
	base := "/api/spaces/tm_1/schedules"

	rec := doJSON(t, mux, http.MethodPost, base, token,
		`{"executor_kind":"workflow","executor_id":"`+publishedWorkflowID+`","input":"{}","cron_expr":"0 9 * * *","timezone":"UTC"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var created ScheduleResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.ExecutorKind != "workflow" || created.ExecutorID != publishedWorkflowID {
		t.Fatalf("created schedule executor = (%q,%q), want (workflow,%q)", created.ExecutorKind, created.ExecutorID, publishedWorkflowID)
	}
}

// A workflow that declares no input_schema takes empty run input, so scheduling
// it with an empty input is valid — unlike an agent, whose input is its prompt.
func TestCreateWorkflowScheduleWithoutInput(t *testing.T) {
	mux, token, _ := scheduleTestHandler(t)
	base := "/api/spaces/tm_1/schedules"

	rec := doJSON(t, mux, http.MethodPost, base, token,
		`{"executor_kind":"workflow","executor_id":"`+noInputWorkflowID+`","input":"","cron_expr":"0 9 * * *","timezone":"UTC"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
}

// A workflow schedule's fixed input is checked against the workflow's
// admission when it is saved: input it could never accept, or a workflow that
// needs an Issue a firing cannot supply, would fail every fire until the
// schedule paused itself.
func TestWorkflowScheduleRefusesInputItsWorkflowCannotAccept(t *testing.T) {
	mux, token, _ := scheduleTestHandler(t)
	base := "/api/spaces/tm_1/schedules"
	body := func(workflowID, input string) string {
		in, _ := json.Marshal(input)
		return `{"executor_kind":"workflow","executor_id":"` + workflowID + `","input":` + string(in) + `,"cron_expr":"0 9 * * *","timezone":"UTC"}`
	}
	for name, b := range map[string]string{
		"input to a workflow without input_schema": body(noInputWorkflowID, "Reply with exactly: TICK"),
		"input that is not JSON":                   body(publishedWorkflowID, "not json"),
		"input that violates the input_schema":     body(publishedWorkflowID, "[1,2]"),
		"empty input where input_schema is set":    body(publishedWorkflowID, ""),
		"workflow that requires an Issue":          body(issueWorkflowID, ""),
	} {
		if rec := doJSON(t, mux, http.MethodPost, base, token, b); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", name, rec.Code, rec.Body.String())
		}
	}

	rec := doJSON(t, mux, http.MethodPost, base, token, body(publishedWorkflowID, `{"k":1}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var created ScheduleResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if rec := doJSON(t, mux, http.MethodPatch, base+"/"+created.ID, token, `{"input":"[1]"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("update to invalid input: status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, mux, http.MethodPatch, base+"/"+created.ID, token, `{"input":"{\"k\":2}"}`); rec.Code != http.StatusOK {
		t.Errorf("update to valid input: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

// A schedule id from another space is reported as not found, not read across the
// space boundary.
func TestGetScheduleFromAnotherSpaceIsNotFound(t *testing.T) {
	mux, token, agentID := scheduleTestHandler(t)
	base := "/api/spaces/tm_1/schedules"
	rec := doJSON(t, mux, http.MethodPost, base, token,
		`{"executor_kind":"agent","executor_id":"`+agentID+`","input":"x","cron_expr":"0 9 * * *","timezone":"UTC"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", rec.Code)
	}
	var created ScheduleResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// The same id under a space the caller does not belong to is refused before
	// any lookup (403), never leaked as 200.
	rec = doJSON(t, mux, http.MethodGet, "/api/spaces/tm_other/schedules/"+created.ID, token, "")
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-space get = %d, want 403", rec.Code)
	}
}
