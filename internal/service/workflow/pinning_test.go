package workflow

import (
	"context"
	"fmt"
	"strings"
	"testing"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/task"
)

func pinningSvc() (*Service, *mock.MockWorkflowStore, *mock.MockAgentStore) {
	agentStore := &mock.MockAgentStore{}
	workflowStore := &mock.MockWorkflowStore{}
	taskRuns := &mock.MockTaskRunStore{}
	svc := &Service{
		Workflows: workflowStore, Agents: agentStore, TaskRuns: taskRuns,
		TaskService: &task.Service{Agents: agentStore, Tasks: &mock.MockTaskStore{}, TaskRuns: taskRuns},
	}
	return svc, workflowStore, agentStore
}

// TestPublishPinsAndRunUsesAgentRevision proves the pinning lifecycle: publishing
// rewrites the definition to pin each node's agent to its current revision, and a
// run started later snapshots that pinned revision's content even after the Agent
// has been edited -- so a published plan is reproducible.
func TestPublishPinsAndRunUsesAgentRevision(t *testing.T) {
	svc, _, agentStore := pinningSvc()
	ctx := context.Background()

	agent, err := agentStore.CreateAgentInSpace(ctx, agentdef.CreateInput{SpaceID: "tm_1", UserID: "u1",
		Def: agentdef.Definition{Name: "A", Instructions: "v1 instructions"}})
	if err != nil {
		t.Fatalf("CreateAgentInSpace: %v", err)
	}
	wf, err := svc.CreateWorkflow(ctx, CreateWorkflowCmd{SpaceID: "tm_1", UserID: "u1", Name: "WF",
		Definition: fmt.Sprintf(`{"schema_version":1,"nodes":[{"id":"a","type":"agent_task","agent":{"id":%q},"input":{"instruction":"do"}}]}`, agent.ID)})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	// Publishing pins the (unset) agent revision to the agent's current revision 1.
	published := coreworkflow.StatusPublished
	pub, err := svc.UpdateWorkflow(ctx, UpdateWorkflowCmd{SpaceID: "tm_1", UserID: "u1", WorkflowID: wf.ID, Status: &published})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !strings.Contains(pub.Definition, `"revision":1`) {
		t.Fatalf("published definition did not pin the agent revision: %s", pub.Definition)
	}

	// Edit the agent to revision 2 with new content.
	if _, err := agentStore.UpdateAgentInSpace(ctx, agentdef.UpdateInput{AgentID: agent.ID, SpaceID: "tm_1", UpdatedBy: "u1",
		Def: agentdef.Definition{Name: "A", Instructions: "v2 instructions"}}); err != nil {
		t.Fatalf("UpdateAgentInSpace: %v", err)
	}

	// A run started now must use the pinned revision 1 content, not the current v2.
	_, steps, err := svc.StartWorkflowRun(ctx, StartWorkflowRunCmd{SpaceID: "tm_1", UserID: "u1", WorkflowID: wf.ID})
	if err != nil {
		t.Fatalf("StartWorkflowRun: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(steps))
	}
	if steps[0].AgentRevision != 1 {
		t.Fatalf("snapshot revision = %d, want 1 (pinned)", steps[0].AgentRevision)
	}
	if steps[0].AgentInstructions != "v1 instructions" {
		t.Fatalf("snapshot instructions = %q, want the pinned v1 content", steps[0].AgentInstructions)
	}
}

// TestCreateWorkflow_RejectsUnknownPinnedRevision proves publication (and every
// definition write) refuses a node that pins an agent revision that does not exist.
func TestCreateWorkflow_RejectsUnknownPinnedRevision(t *testing.T) {
	svc, _, agentStore := pinningSvc()
	ctx := context.Background()
	agent, err := agentStore.CreateAgentInSpace(ctx, agentdef.CreateInput{SpaceID: "tm_1", UserID: "u1",
		Def: agentdef.Definition{Name: "A", Instructions: "v1"}})
	if err != nil {
		t.Fatalf("CreateAgentInSpace: %v", err)
	}
	_, err = svc.CreateWorkflow(ctx, CreateWorkflowCmd{SpaceID: "tm_1", UserID: "u1", Name: "WF",
		Definition: fmt.Sprintf(`{"schema_version":1,"nodes":[{"id":"a","type":"agent_task","agent":{"id":%q,"revision":99},"input":{"instruction":"do"}}]}`, agent.ID)})
	if err == nil {
		t.Fatal("CreateWorkflow accepted a node pinning a non-existent agent revision")
	}
}
