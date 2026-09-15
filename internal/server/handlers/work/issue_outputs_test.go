package work

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coreartifact "github.com/icloudbb/buildmax/internal/core/artifact"
	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/mock"
	artifactsvc "github.com/icloudbb/buildmax/internal/service/artifact"
	"github.com/icloudbb/buildmax/internal/testsupport"
	"github.com/icloudbb/buildmax/internal/util"
)

const outputsTestSecret = "outputs-test-secret"

type outputsFixtures struct {
	mux          *http.ServeMux
	tasks        *mock.MockTaskStore
	workflows    *mock.MockWorkflowStore
	published    *mock.MockArtifactStore
	taskRuns     *mock.MockTaskRunStore
	personalID   string
	otherSpaceID string
}

func newOutputsFixtures(t *testing.T) *outputsFixtures {
	t.Helper()
	personalSpaceID := "tm_personal_u1"
	otherSpaceID := "tm_other"
	issues := &mock.MockIssueStore{
		Issues: []coreissue.Issue{
			{
				ID: "i_1", UserID: "u1", SpaceID: personalSpaceID,
				Title: "I", Status: coreissue.StatusInProgress,
				CreatedBy: "u1", CreatedAt: time.Unix(100, 0).UTC(), UpdatedAt: time.Unix(100, 0).UTC(),
			},
			{
				ID: "i_other", UserID: "u2", SpaceID: otherSpaceID,
				Title: "Other", Status: coreissue.StatusTodo,
				CreatedBy: "u2", CreatedAt: time.Unix(50, 0).UTC(), UpdatedAt: time.Unix(50, 0).UTC(),
			},
		},
	}
	spaces := &mock.MockSpaceStore{
		Spaces: []corespace.Space{
			{ID: personalSpaceID, Name: "My Space", PersonalForUserID: util.Ptr("u1"), CreatedBy: "u1"},
			{ID: otherSpaceID, Name: "Other", CreatedBy: "u2"},
		},
		Members: []corespace.Member{
			{SpaceID: personalSpaceID, UserID: "u1", Role: corespace.RoleOwner},
			{SpaceID: otherSpaceID, UserID: "u2", Role: corespace.RoleOwner},
		},
	}
	tasks := &mock.MockTaskStore{}
	workflows := &mock.MockWorkflowStore{}
	published := &mock.MockArtifactStore{}
	taskRuns := &mock.MockTaskRunStore{}

	h := New(Config{
		JWTSecret:     outputsTestSecret,
		Spaces:        spaces,
		Issues:        issues,
		Agents:        &mock.MockAgentStore{},
		Workflows:     workflows,
		Tasks:         tasks,
		Conversations: &mock.MockConversationStore{},
		TaskRuns:      taskRuns,
		Artifacts:     &artifactsvc.Service{Artifacts: published, Storage: mock.NewMockArtifactStorage()},
	})
	mux := http.NewServeMux()
	h.Register(mux)

	return &outputsFixtures{
		mux:          mux,
		tasks:        tasks,
		workflows:    workflows,
		published:    published,
		taskRuns:     taskRuns,
		personalID:   personalSpaceID,
		otherSpaceID: otherSpaceID,
	}
}

func fetchIssueFlow(t *testing.T, mux *http.ServeMux, spaceID, issueID, userID string) (*httptest.ResponseRecorder, issueFlowResponse) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/spaces/"+spaceID+"/issues/"+issueID+"/flow", nil)
	req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT(userID, outputsTestSecret))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var flow issueFlowResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &flow); err != nil {
			t.Fatalf("decode flow: %v", err)
		}
	}
	return rec, flow
}

func TestIssueFlowOutputs_SpaceScoped(t *testing.T) {
	fx := newOutputsFixtures(t)
	taskID := "t_other"
	runID := "r_other"
	fx.tasks.List = []coretask.Task{{
		ID: taskID, ConversationID: "c_other", SpaceID: fx.otherSpaceID,
		IssueID: util.Ptr("i_other"), Status: "SUCCEEDED",
		CreatedBy: "u2", CreatedAt: time.Unix(200, 0).UTC(), LastRunID: &runID,
	}}

	// u1 reading another space's issue must be forbidden, regardless of outputs.
	rec, _ := fetchIssueFlow(t, fx.mux, fx.otherSpaceID, "i_other", "u1")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 cross-space, got %d", rec.Code)
	}
}

func TestIssueFlowOutputs_EmptyWhenNoRuns(t *testing.T) {
	fx := newOutputsFixtures(t)
	rec, flow := fetchIssueFlow(t, fx.mux, fx.personalID, "i_1", "u1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if flow.LatestResult != nil {
		t.Fatalf("latest_result should be nil, got %+v", flow.LatestResult)
	}
	if flow.Outputs == nil {
		t.Fatalf("outputs should be empty slice, not nil")
	}
	if len(flow.Outputs) != 0 {
		t.Fatalf("outputs len = %d", len(flow.Outputs))
	}
}

// An issue shows what its runs published as artifacts, addressed by the
// artifact's own id. The run is where it came from, not what owns it.
func TestIssueFlowOutputs_ArtifactsPublishedByARun(t *testing.T) {
	fx := newOutputsFixtures(t)
	taskID := "t_pub"
	runID := "r_pub"
	fx.tasks.List = []coretask.Task{{
		ID: taskID, ConversationID: "c_1", SpaceID: fx.personalID,
		IssueID: util.Ptr("i_1"), Status: "SUCCEEDED", Input: "do work",
		CreatedBy: "u1", CreatedAt: time.Unix(200, 0).UTC(), LastRunID: &runID,
	}}
	fx.taskRuns.Runs = []coretask.Run{{ID: runID, TaskID: taskID, Status: "SUCCEEDED", CreatedAt: time.Unix(200, 0).UTC()}}
	if _, err := fx.published.CreateArtifact(context.Background(), coreartifact.CreateInput{
		SpaceID: fx.personalID, ArtifactID: "tsyt7at6cjfr33d73mta", Filename: "report.pdf",
		MediaType: "application/pdf", SizeBytes: 2048,
		SourceType: coreartifact.SourceAgent, SourceID: runID,
		CreatedByType: coreartifact.CreatorAgent, Title: "Quarterly report",
	}); err != nil {
		t.Fatal(err)
	}

	rec, flow := fetchIssueFlow(t, fx.mux, fx.personalID, "i_1", "u1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var found *issueOutputResponse
	for i := range flow.Outputs {
		if flow.Outputs[i].Kind == "artifact" {
			found = &flow.Outputs[i]
		}
	}
	if found == nil {
		t.Fatalf("no artifact output in %+v", flow.Outputs)
	}
	if found.ArtifactID != "tsyt7at6cjfr33d73mta" {
		t.Errorf("artifact id = %q", found.ArtifactID)
	}
	if found.Title != "Quarterly report" || found.Filename != "report.pdf" || found.SizeBytes != 2048 {
		t.Errorf("output does not describe the file: %+v", found)
	}
	if found.Source.TaskRunID != runID || found.Source.TaskID != taskID {
		t.Errorf("provenance lost: %+v", found.Source)
	}
	// The latest (and only) output is what a reader lands on.
	if flow.LatestResult == nil || flow.LatestResult.ArtifactID != "tsyt7at6cjfr33d73mta" {
		t.Errorf("latest_result = %+v, want the published artifact", flow.LatestResult)
	}
	// The storage key must not reach a client here either.
	if strings.Contains(rec.Body.String(), "storage_key") {
		t.Error("the flow response serialized a storage key")
	}
}

// An artifact published by a workflow step's run carries that step's provenance.
func TestIssueFlowOutputs_WorkflowStepProvenance(t *testing.T) {
	fx := newOutputsFixtures(t)
	taskID := "t_step"
	runID := "r_step"
	workflowRunID := "wr_1"
	stepRunID := "wsr_1"
	stepID := "s1"
	wfID := "w_1"

	fx.workflows.Workflows = []coreworkflow.Workflow{{
		ID: wfID, SpaceID: fx.personalID, Name: "WF",
		Definition: `{"schema_version":1,"nodes":[]}`, Status: coreworkflow.StatusPublished,
	}}
	fx.workflows.Runs = []coreworkflow.Run{{
		ID: workflowRunID, WorkflowID: wfID,
		IssueID: util.Ptr("i_1"),
		Status:  string(coreworkflow.RunStatusSucceeded), CreatedBy: "u1", CreatedAt: time.Unix(300, 0).UTC(),
	}}
	fx.workflows.NodeRuns = []coreworkflow.NodeRun{{
		ID: stepRunID, WorkflowRunID: workflowRunID,
		NodeID: stepID, NodeIndex: 0, NodeType: coreworkflow.NodeTypeAgentTask,
		Status: string(coreworkflow.NodeRunStatusSucceeded),
		TaskID: &taskID, TaskRunID: &runID, CreatedAt: time.Unix(305, 0).UTC(),
	}}
	fx.tasks.List = []coretask.Task{{
		ID: taskID, ConversationID: "c_1", SpaceID: fx.personalID,
		IssueID: util.Ptr("i_1"), Status: "SUCCEEDED",
		CreatedBy: "u1", CreatedAt: time.Unix(305, 0).UTC(), LastRunID: &runID,
	}}
	fx.taskRuns.Runs = []coretask.Run{{ID: runID, TaskID: taskID, Status: "SUCCEEDED", CreatedAt: time.Unix(305, 0).UTC()}}
	if _, err := fx.published.CreateArtifact(context.Background(), coreartifact.CreateInput{
		SpaceID: fx.personalID, ArtifactID: "wsyt7at6cjfr33d73mta", Filename: "step.pdf",
		SourceType: coreartifact.SourceAgent, SourceID: runID,
		CreatedByType: coreartifact.CreatorAgent,
	}); err != nil {
		t.Fatal(err)
	}

	rec, flow := fetchIssueFlow(t, fx.mux, fx.personalID, "i_1", "u1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if flow.LatestResult == nil {
		t.Fatalf("latest_result is nil")
	}
	src := flow.LatestResult.Source
	if src.WorkflowRunID == nil || *src.WorkflowRunID != workflowRunID {
		t.Fatalf("workflow_run_id = %v", src.WorkflowRunID)
	}
	if src.WorkflowNodeRunID == nil || *src.WorkflowNodeRunID != stepRunID {
		t.Fatalf("workflow_node_run_id = %v", src.WorkflowNodeRunID)
	}
	if src.WorkflowNodeID == nil || *src.WorkflowNodeID != stepID {
		t.Fatalf("workflow_node_id = %v", src.WorkflowNodeID)
	}
}

// An issue whose runs published nothing reports no artifact outputs, and a
// deployment with no artifact store answers the same way rather than failing.
func TestIssueFlowOutputs_NoArtifactStore(t *testing.T) {
	fx := newOutputsFixtures(t)
	fx.published = nil
	runID := "r_none"
	fx.tasks.List = []coretask.Task{{
		ID: "t_none", ConversationID: "c_1", SpaceID: fx.personalID,
		IssueID: util.Ptr("i_1"), Status: "SUCCEEDED", Input: "do work",
		CreatedBy: "u1", CreatedAt: time.Unix(200, 0).UTC(), LastRunID: &runID,
	}}
	fx.taskRuns.Runs = []coretask.Run{{ID: runID, TaskID: "t_none", Status: "SUCCEEDED", CreatedAt: time.Unix(200, 0).UTC()}}
	rec, flow := fetchIssueFlow(t, fx.mux, fx.personalID, "i_1", "u1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	for _, o := range flow.Outputs {
		if o.Kind == "artifact" {
			t.Errorf("unexpected artifact output %+v", o)
		}
	}
}

// A retried task keeps what its earlier runs published. The task's last run is
// not its only run, and an artifact does not stop being the issue's output
// because the work was attempted again.
func TestIssueFlowOutputs_ArtifactsSurviveARetry(t *testing.T) {
	fx := newOutputsFixtures(t)
	taskID := "t_retry"
	firstRun, secondRun := "r_first", "r_second"
	fx.tasks.List = []coretask.Task{{
		ID: taskID, ConversationID: "c_1", SpaceID: fx.personalID,
		IssueID: util.Ptr("i_1"), Status: "SUCCEEDED", Input: "do work",
		CreatedBy: "u1", CreatedAt: time.Unix(200, 0).UTC(), LastRunID: &secondRun,
	}}
	fx.taskRuns.Runs = []coretask.Run{
		{ID: firstRun, TaskID: taskID, Status: "FAILED", CreatedAt: time.Unix(200, 0).UTC()},
		{ID: secondRun, TaskID: taskID, Status: "SUCCEEDED", CreatedAt: time.Unix(300, 0).UTC()},
	}
	for _, c := range []struct{ id, run, name string }{
		{"usyt7at6cjfr33d73mta", firstRun, "draft.pdf"},
		{"vsyt7at6cjfr33d73mta", secondRun, "final.pdf"},
	} {
		if _, err := fx.published.CreateArtifact(context.Background(), coreartifact.CreateInput{
			SpaceID: fx.personalID, ArtifactID: c.id, Filename: c.name,
			SourceType: coreartifact.SourceAgent, SourceID: c.run,
			CreatedByType: coreartifact.CreatorAgent,
		}); err != nil {
			t.Fatal(err)
		}
	}

	_, flow := fetchIssueFlow(t, fx.mux, fx.personalID, "i_1", "u1")
	seen := map[string]bool{}
	for _, o := range flow.Outputs {
		if o.ArtifactID != "" {
			seen[o.ArtifactID] = true
		}
	}
	if !seen["vsyt7at6cjfr33d73mta"] {
		t.Error("the latest run's artifact is missing")
	}
	if !seen["usyt7at6cjfr33d73mta"] {
		t.Error("an earlier run's artifact was dropped; a retry must not hide what the first attempt published")
	}
}
