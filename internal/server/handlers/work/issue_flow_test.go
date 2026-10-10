package work

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/testsupport"
	"github.com/icloudbb/buildmax/internal/util"
)

// issueRunsFixture gives issue i_1 two Workflow runs and two Agent runs,
// interleaved in time, plus the Task the newer Workflow run's step dispatched
// with the Issue attached.
func issueRunsFixture(t *testing.T) *outputsFixtures {
	t.Helper()
	fx := newOutputsFixtures(t)
	fx.workflows.Workflows = []coreworkflow.Workflow{{
		ID: "w_1", SpaceID: fx.personalID, Name: "WF",
		Definition: `{"schema_version":1,"nodes":[]}`, Status: coreworkflow.StatusPublished,
	}}
	// The mock store lists runs in slice order, which is newest first here.
	fx.workflows.Runs = []coreworkflow.Run{
		{ID: "wr_new", WorkflowID: "w_1", IssueID: util.Ptr("i_1"), Status: string(coreworkflow.RunStatusSucceeded), CreatedBy: "u1", CreatedAt: time.Unix(300, 0).UTC()},
		{ID: "wr_old", WorkflowID: "w_1", IssueID: util.Ptr("i_1"), Status: string(coreworkflow.RunStatusFailed), CreatedBy: "u1", CreatedAt: time.Unix(100, 0).UTC()},
	}
	ctx := t.Context()
	stepTask, err := fx.tasks.AdmitTask(ctx, &coretask.CreateInput{
		SpaceID: fx.personalID, Input: "step", IssueID: util.Ptr("i_1"),
		AdmissionKey: "workflow/wr_new/node/a", WorkflowNodeRunID: "wsr_new",
	})
	if err != nil {
		t.Fatal(err)
	}
	fx.workflows.NodeRuns = []coreworkflow.NodeRun{{
		ID: "wsr_new", WorkflowRunID: "wr_new", NodeID: "a", NodeType: coreworkflow.NodeTypeAgentTask,
		Status: string(coreworkflow.NodeRunStatusSucceeded), TaskID: &stepTask.ID, CreatedAt: time.Unix(301, 0).UTC(),
	}}
	// The mock store lists tasks newest last.
	fx.tasks.List = append([]coretask.Task{
		{ID: "t_old", SpaceID: fx.personalID, IssueID: util.Ptr("i_1"), Status: "SUCCEEDED", CreatedBy: "u1", CreatedAt: time.Unix(200, 0).UTC()},
		{ID: "t_new", SpaceID: fx.personalID, IssueID: util.Ptr("i_1"), Status: "RUNNING", CreatedBy: "u1", CreatedAt: time.Unix(400, 0).UTC()},
	}, fx.tasks.List...)
	return fx
}

func runIDs(runs []issueRunResponse) []string {
	out := make([]string, len(runs))
	for i, run := range runs {
		switch run.Kind {
		case issueRunKindAgent:
			out[i] = "agent:" + run.Task.ID
		case issueRunKindWorkflow:
			out[i] = "workflow:" + run.Run.ID
		}
	}
	return out
}

// An Issue's Agent runs and Workflow runs are one list with one count, newest
// first, so the latest run is the first one whatever its kind. A Task a
// Workflow step dispatched belongs to its Workflow run and is not listed again.
func TestIssueFlowRuns_OneListNewestFirst(t *testing.T) {
	fx := issueRunsFixture(t)
	rec, flow := fetchIssueFlow(t, fx.mux, fx.personalID, "i_1", "u1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	got := runIDs(flow.Runs)
	want := []string{"agent:t_new", "workflow:wr_new", "agent:t_old", "workflow:wr_old"}
	if len(got) != len(want) {
		t.Fatalf("runs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("runs = %v, want %v", got, want)
		}
	}
	if flow.Total != len(want) {
		t.Errorf("total = %d, want %d", flow.Total, len(want))
	}
	if len(flow.Runs[1].Steps) != 1 || flow.Runs[1].Steps[0].ID != "wsr_new" {
		t.Errorf("workflow run steps = %+v", flow.Runs[1].Steps)
	}
	if flow.Runs[0].Run != nil || flow.Runs[0].Steps != nil || flow.Runs[1].Task != nil {
		t.Errorf("a run carries the other kind's fields: %+v", flow.Runs[:2])
	}
}

// A page of the merged list is the same slice of it whichever kind each run
// on the page is.
func TestIssueFlowRuns_PagesTheMergedList(t *testing.T) {
	fx := issueRunsFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/api/spaces/"+fx.personalID+"/issues/i_1/flow?limit=2&offset=1", nil)
	req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", outputsTestSecret))
	rec := httptest.NewRecorder()
	fx.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var flow issueFlowResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &flow); err != nil {
		t.Fatalf("decode flow: %v", err)
	}
	got := runIDs(flow.Runs)
	if len(got) != 2 || got[0] != "workflow:wr_new" || got[1] != "agent:t_old" {
		t.Fatalf("page = %v, want [workflow:wr_new agent:t_old]", got)
	}
	if flow.Total != 4 {
		t.Errorf("total = %d, want 4", flow.Total)
	}
}
