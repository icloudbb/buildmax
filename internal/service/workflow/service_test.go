package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/task"
	"github.com/icloudbb/buildmax/internal/util"
)

func TestCreateWorkflow_ValidateDefinition(t *testing.T) {
	svc := &Service{
		Workflows: &mock.MockWorkflowStore{},
		Agents: &mock.MockAgentStore{
			Agents: []agentdef.Agent{{ID: "a_1", SpaceID: "tm_1", Name: "Agent 1"}},
		},
	}
	workflow, err := svc.CreateWorkflow(context.Background(), CreateWorkflowCmd{
		SpaceID:    "tm_1",
		UserID:     "u1",
		Name:       "WF",
		Definition: `{"schema_version":1,"nodes":[{"id":"collect","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"collect data"}}]}`,
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if workflow.ID == "" {
		t.Fatal("expected workflow id")
	}
	if workflow.Status != coreworkflow.StatusDraft {
		t.Fatalf("workflow status = %q, want draft", workflow.Status)
	}
}

func TestStartWorkflowRunAndAdvanceOnTerminal(t *testing.T) {
	workflowStore := &mock.MockWorkflowStore{
		Workflows: []coreworkflow.Workflow{{
			ID:          "w_1",
			SpaceID:     "tm_1",
			Name:        "WF",
			Definition:  `{"schema_version":1,"nodes":[{"id":"collect","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"collect data"}},{"id":"summarize","type":"agent_task","needs":["collect"],"agent":{"id":"a_2"},"input":{"instruction":"summarize"}}]}`,
			Description: "desc",
			Status:      coreworkflow.StatusPublished,
		}},
	}
	taskStore := &mock.MockTaskStore{}
	taskRuns := &mock.MockTaskRunStore{}
	agentStore := &mock.MockAgentStore{
		Agents: []agentdef.Agent{
			{ID: "a_1", SpaceID: "tm_1", Name: "Collector", Instructions: "collect"},
			{ID: "a_2", SpaceID: "tm_1", Name: "Summarizer", Instructions: "summarize"},
		},
	}
	svc := &Service{
		Workflows: workflowStore,
		Agents:    agentStore,
		TaskRuns:  taskRuns,
		TaskService: &task.Service{
			Agents:   agentStore,
			Tasks:    taskStore,
			TaskRuns: taskRuns,
		},
	}
	run, steps, err := svc.StartWorkflowRun(context.Background(), StartWorkflowRunCmd{
		SpaceID:    "tm_1",
		UserID:     "u1",
		WorkflowID: "w_1",
	})
	if err != nil {
		t.Fatalf("StartWorkflowRun: %v", err)
	}
	if run.Status != string(coreworkflow.RunStatusRunning) {
		t.Fatalf("run status = %q, want running", run.Status)
	}
	if len(steps) != 2 {
		t.Fatalf("steps len = %d, want 2", len(steps))
	}
	if steps[0].Status != string(coreworkflow.NodeRunStatusRunning) {
		t.Fatalf("step[0] status = %q, want running", steps[0].Status)
	}
	if steps[0].TaskRunID == nil {
		t.Fatal("expected first step task run id")
	}

	// The fold reads the step's terminal outcome from the TaskRun store, not the
	// callback payload, so the run must be terminal there before the wake-up.
	output := "done"
	taskRuns.Runs = append(taskRuns.Runs, coretask.Run{
		ID:     *steps[0].TaskRunID,
		TaskID: *steps[0].TaskID,
		Status: string(coretask.RunStatusSucceeded),
		Output: &output,
	})
	if err := svc.HandleTaskRunTerminal(context.Background(), coretask.RunTerminalInfo{
		TaskRunID: *steps[0].TaskRunID,
		TaskID:    *steps[0].TaskID,
		UserID:    "u1",
		Status:    string(coretask.RunStatusSucceeded),
		Output:    &output,
	}); err != nil {
		t.Fatalf("HandleTaskRunTerminal first step: %v", err)
	}
	updatedSteps, err := workflowStore.ListWorkflowNodeRuns(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("ListWorkflowNodeRuns: %v", err)
	}
	if updatedSteps[0].Status != string(coreworkflow.NodeRunStatusSucceeded) {
		t.Fatalf("step[0] status = %q, want succeeded", updatedSteps[0].Status)
	}
	if updatedSteps[1].Status != string(coreworkflow.NodeRunStatusRunning) {
		t.Fatalf("step[1] status = %q, want running", updatedSteps[1].Status)
	}
}

// TestStartWorkflowRun_DiamondRespectsNeeds proves the graph -- not array
// position -- decides execution: a fan-out node's two dependents each wait on
// it, and the fan-in node runs only after both dependents succeed. A
// max_parallel_nodes of 1 pins the run to one node at a time, so this isolates
// readiness from concurrency.
func TestStartWorkflowRun_DiamondRespectsNeeds(t *testing.T) {
	workflowStore := &mock.MockWorkflowStore{
		Workflows: []coreworkflow.Workflow{{
			ID:      "w_1",
			SpaceID: "tm_1",
			Name:    "Diamond",
			Definition: `{"schema_version":1,"policy":{"max_parallel_nodes":1},"nodes":[` +
				`{"id":"research","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"research"}},` +
				`{"id":"analyze","type":"agent_task","needs":["research"],"agent":{"id":"a_1"},"input":{"instruction":"analyze"}},` +
				`{"id":"summarize","type":"agent_task","needs":["research"],"agent":{"id":"a_1"},"input":{"instruction":"summarize"}},` +
				`{"id":"report","type":"agent_task","needs":["analyze","summarize"],"agent":{"id":"a_1"},"input":{"instruction":"report"}}` +
				`]}`,
			Status: coreworkflow.StatusPublished,
		}},
	}
	taskRuns := &mock.MockTaskRunStore{}
	agentStore := &mock.MockAgentStore{Agents: []agentdef.Agent{{ID: "a_1", SpaceID: "tm_1", Name: "Agent", Instructions: "work"}}}
	svc := &Service{
		Workflows:   workflowStore,
		Agents:      agentStore,
		TaskRuns:    taskRuns,
		TaskService: &task.Service{Agents: agentStore, Tasks: &mock.MockTaskStore{}, TaskRuns: taskRuns},
	}
	ctx := context.Background()
	run, _, err := svc.StartWorkflowRun(ctx, StartWorkflowRunCmd{SpaceID: "tm_1", UserID: "u1", WorkflowID: "w_1"})
	if err != nil {
		t.Fatalf("StartWorkflowRun: %v", err)
	}

	// runningNode returns the single node currently running, asserting exactly one
	// is (concurrency is one), and the set of nodes still pending.
	runningNode := func() (coreworkflow.NodeRun, map[string]string) {
		steps, err := workflowStore.ListWorkflowNodeRuns(ctx, run.ID)
		if err != nil {
			t.Fatalf("ListWorkflowNodeRuns: %v", err)
		}
		var running []coreworkflow.NodeRun
		status := make(map[string]string, len(steps))
		for _, s := range steps {
			status[s.NodeID] = s.Status
			if s.Status == string(coreworkflow.NodeRunStatusRunning) {
				running = append(running, s)
			}
		}
		if len(running) != 1 {
			t.Fatalf("running nodes = %v, want exactly 1 (status=%v)", running, status)
		}
		return running[0], status
	}
	succeed := func(node coreworkflow.NodeRun) {
		out := node.NodeID + " done"
		taskRuns.Runs = append(taskRuns.Runs, coretask.Run{ID: *node.TaskRunID, TaskID: *node.TaskID, Status: string(coretask.RunStatusSucceeded), Output: &out})
		if err := svc.HandleTaskRunTerminal(ctx, coretask.RunTerminalInfo{TaskRunID: *node.TaskRunID, TaskID: *node.TaskID, UserID: "u1", Status: string(coretask.RunStatusSucceeded), Output: &out}); err != nil {
			t.Fatalf("HandleTaskRunTerminal %s: %v", node.NodeID, err)
		}
	}

	// research runs first as the only root.
	first, _ := runningNode()
	if first.NodeID != "research" {
		t.Fatalf("first running node = %q, want research", first.NodeID)
	}
	succeed(first)

	// A dependent runs next; report must still be pending until both dependents
	// finish, proving the fan-in waits on every predecessor.
	second, status := runningNode()
	if second.NodeID != "analyze" && second.NodeID != "summarize" {
		t.Fatalf("second running node = %q, want a research dependent", second.NodeID)
	}
	if status["report"] != string(coreworkflow.NodeRunStatusPending) {
		t.Fatalf("report status = %q, want pending while a dependent is unfinished", status["report"])
	}
	succeed(second)

	third, status := runningNode()
	if third.NodeID == "report" {
		t.Fatalf("report ran before both dependents finished (status=%v)", status)
	}
	succeed(third)

	// Both dependents done: report is the last node to run.
	last, _ := runningNode()
	if last.NodeID != "report" {
		t.Fatalf("last running node = %q, want report", last.NodeID)
	}
	succeed(last)

	final, err := workflowStore.GetWorkflowRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetWorkflowRun: %v", err)
	}
	if final.Status != string(coreworkflow.RunStatusSucceeded) {
		t.Fatalf("run status = %q, want succeeded", final.Status)
	}
}

// concurrencySvc builds a service over mock stores with one agent (a_1) and one
// published workflow, and returns the service, its workflow store, its task-run
// store, and the started run's id. It is the harness the concurrency tests drive
// by making node TaskRuns terminal and re-reconciling.
func concurrencySvc(t *testing.T, definition string) (*Service, *mock.MockWorkflowStore, *mock.MockTaskRunStore, string) {
	t.Helper()
	workflowStore := &mock.MockWorkflowStore{Workflows: []coreworkflow.Workflow{{
		ID: "w_1", SpaceID: "tm_1", Name: "WF", Definition: definition, Status: coreworkflow.StatusPublished,
	}}}
	taskRuns := &mock.MockTaskRunStore{}
	agentStore := &mock.MockAgentStore{Agents: []agentdef.Agent{{ID: "a_1", SpaceID: "tm_1", Name: "Agent", Instructions: "work"}}}
	svc := &Service{
		Workflows:   workflowStore,
		Agents:      agentStore,
		TaskRuns:    taskRuns,
		TaskService: &task.Service{Agents: agentStore, Tasks: &mock.MockTaskStore{}, TaskRuns: taskRuns},
	}
	run, _, err := svc.StartWorkflowRun(context.Background(), StartWorkflowRunCmd{SpaceID: "tm_1", UserID: "u1", WorkflowID: "w_1"})
	if err != nil {
		t.Fatalf("StartWorkflowRun: %v", err)
	}
	return svc, workflowStore, taskRuns, run.ID
}

// nodesByStatus groups a run's node ids by status for concise assertions.
func nodesByStatus(t *testing.T, store *mock.MockWorkflowStore, runID string) map[string][]string {
	t.Helper()
	steps, err := store.ListWorkflowNodeRuns(context.Background(), runID)
	if err != nil {
		t.Fatalf("ListWorkflowNodeRuns: %v", err)
	}
	out := map[string][]string{}
	for _, s := range steps {
		out[s.Status] = append(out[s.Status], s.NodeID)
	}
	return out
}

// finishNode makes the given node's TaskRun terminal with status and folds it.
func finishNode(t *testing.T, svc *Service, store *mock.MockWorkflowStore, taskRuns *mock.MockTaskRunStore, runID, nodeID, status string) {
	t.Helper()
	steps, err := store.ListWorkflowNodeRuns(context.Background(), runID)
	if err != nil {
		t.Fatalf("ListWorkflowNodeRuns: %v", err)
	}
	for _, s := range steps {
		if s.NodeID != nodeID {
			continue
		}
		if s.TaskRunID == nil {
			t.Fatalf("node %q has no task run to finish (status %q)", nodeID, s.Status)
		}
		out := nodeID + " out"
		taskRuns.Runs = append(taskRuns.Runs, coretask.Run{ID: *s.TaskRunID, TaskID: *s.TaskID, Status: status, Output: &out})
		if err := svc.HandleTaskRunTerminal(context.Background(), coretask.RunTerminalInfo{TaskRunID: *s.TaskRunID, TaskID: *s.TaskID, UserID: "u1", Status: status, Output: &out}); err != nil {
			t.Fatalf("HandleTaskRunTerminal %s: %v", nodeID, err)
		}
		return
	}
	t.Fatalf("node %q not found", nodeID)
}

// TestReconcile_ConcurrentDispatchDiamond proves a fan-out dispatches both ready
// dependents at once and the fan-in waits for both.
func TestReconcile_ConcurrentDispatchDiamond(t *testing.T) {
	svc, store, taskRuns, runID := concurrencySvc(t, `{"schema_version":1,"nodes":[`+
		`{"id":"research","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"r"}},`+
		`{"id":"analyze","type":"agent_task","needs":["research"],"agent":{"id":"a_1"},"input":{"instruction":"a"}},`+
		`{"id":"summarize","type":"agent_task","needs":["research"],"agent":{"id":"a_1"},"input":{"instruction":"s"}},`+
		`{"id":"report","type":"agent_task","needs":["analyze","summarize"],"agent":{"id":"a_1"},"input":{"instruction":"rep"}}`+
		`]}`)

	finishNode(t, svc, store, taskRuns, runID, "research", string(coretask.RunStatusSucceeded))
	// Both dependents dispatch together; report waits.
	got := nodesByStatus(t, store, runID)
	if len(got["running"]) != 2 {
		t.Fatalf("running = %v, want analyze and summarize both running", got["running"])
	}
	if len(got["pending"]) != 1 || got["pending"][0] != "report" {
		t.Fatalf("pending = %v, want [report]", got["pending"])
	}

	finishNode(t, svc, store, taskRuns, runID, "analyze", string(coretask.RunStatusSucceeded))
	if got := nodesByStatus(t, store, runID); len(got["running"]) != 1 || got["running"][0] != "summarize" {
		t.Fatalf("after analyze, running = %v, want [summarize] (report must wait on summarize)", got["running"])
	}
	finishNode(t, svc, store, taskRuns, runID, "summarize", string(coretask.RunStatusSucceeded))
	if got := nodesByStatus(t, store, runID); len(got["running"]) != 1 || got["running"][0] != "report" {
		t.Fatalf("after both dependents, running = %v, want [report]", got["running"])
	}
	finishNode(t, svc, store, taskRuns, runID, "report", string(coretask.RunStatusSucceeded))
	if run, _ := store.GetWorkflowRun(context.Background(), runID); run.Status != string(coreworkflow.RunStatusSucceeded) {
		t.Fatalf("run status = %q, want succeeded", run.Status)
	}
}

// TestReconcile_ConcurrencyLimitBinds proves max_parallel_nodes caps how many
// ready nodes run at once, and a freed slot admits the next ready node.
func TestReconcile_ConcurrencyLimitBinds(t *testing.T) {
	svc, store, taskRuns, runID := concurrencySvc(t, `{"schema_version":1,"policy":{"max_parallel_nodes":2},"nodes":[`+
		`{"id":"a","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"a"}},`+
		`{"id":"b","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"b"}},`+
		`{"id":"c","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"c"}}`+
		`]}`)

	// Three roots, limit 2: two run, one waits on the limit.
	got := nodesByStatus(t, store, runID)
	if len(got["running"]) != 2 || len(got["pending"]) != 1 {
		t.Fatalf("initial running=%v pending=%v, want 2 running and 1 pending", got["running"], got["pending"])
	}
	waiting := got["pending"][0]
	// Finishing a running node frees the slot for the waiting one.
	finishNode(t, svc, store, taskRuns, runID, got["running"][0], string(coretask.RunStatusSucceeded))
	after := nodesByStatus(t, store, runID)
	if len(after["running"]) != 2 {
		t.Fatalf("after freeing a slot, running=%v, want 2", after["running"])
	}
	found := false
	for _, id := range after["running"] {
		if id == waiting {
			found = true
		}
	}
	if !found {
		t.Fatalf("the waiting node %q did not start after a slot freed (running=%v)", waiting, after["running"])
	}
}

// TestReconcile_FailFastCancelsRunningSiblings proves one node's failure starts
// the drain and waits for siblings that were running concurrently.
func TestReconcile_FailFastCancelsRunningSiblings(t *testing.T) {
	svc, store, taskRuns, runID := concurrencySvc(t, `{"schema_version":1,"nodes":[`+
		`{"id":"a","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"a"}},`+
		`{"id":"b","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"b"}}`+
		`]}`)
	if got := nodesByStatus(t, store, runID); len(got["running"]) != 2 {
		t.Fatalf("initial running=%v, want a and b both running", got["running"])
	}
	finishNode(t, svc, store, taskRuns, runID, "a", string(coretask.RunStatusFailed))
	run, _ := store.GetWorkflowRun(context.Background(), runID)
	if run.Status != string(coreworkflow.RunStatusFailing) {
		t.Fatalf("run status = %q, want failing", run.Status)
	}
	got := nodesByStatus(t, store, runID)
	if len(got["failed"]) != 1 || got["failed"][0] != "a" {
		t.Fatalf("failed = %v, want [a]", got["failed"])
	}
	if len(got["running"]) != 1 || got["running"][0] != "b" {
		t.Fatalf("running = %v, want [b] while waiting for its worker", got["running"])
	}
	finishNode(t, svc, store, taskRuns, runID, "b", string(coretask.RunStatusCanceled))
	run, _ = store.GetWorkflowRun(context.Background(), runID)
	if run.Status != string(coreworkflow.RunStatusFailed) {
		t.Fatalf("drained run = %s", run.Status)
	}
}

// TestStartWorkflowRun_IssueAccess proves issue_access governs the run's
// relationship to its Issue: "required" refuses a run with no Issue, "if_bound"
// attaches the run's Issue to the node's Task, and "none" withholds it.
func TestStartWorkflowRun_IssueAccess(t *testing.T) {
	buildSvc := func(access string) (*Service, *mock.MockTaskStore) {
		workflowStore := &mock.MockWorkflowStore{Workflows: []coreworkflow.Workflow{{
			ID: "w_1", SpaceID: "tm_1", Name: "WF", Status: coreworkflow.StatusPublished,
			Definition: `{"schema_version":1,"nodes":[{"id":"a","type":"agent_task","issue_access":"` + access + `","agent":{"id":"a_1"},"input":{"instruction":"do"}}]}`,
		}}}
		taskStore := &mock.MockTaskStore{}
		taskRuns := &mock.MockTaskRunStore{}
		agentStore := &mock.MockAgentStore{Agents: []agentdef.Agent{{ID: "a_1", SpaceID: "tm_1", Name: "Agent", Instructions: "work"}}}
		issueStore := &mock.MockIssueStore{Issues: []coreissue.Issue{{
			ID: "iss_1", SpaceID: "tm_1",
			ExecutorKind: util.Ptr(coreissue.ExecutorWorkflow), ExecutorID: util.Ptr("w_1"),
		}}}
		svc := &Service{
			Workflows: workflowStore, Agents: agentStore, TaskRuns: taskRuns, Issues: issueStore,
			TaskService: &task.Service{Agents: agentStore, Tasks: taskStore, TaskRuns: taskRuns},
		}
		return svc, taskStore
	}
	taskIssueID := func(t *testing.T, svc *Service, tasks *mock.MockTaskStore, runID string) *string {
		t.Helper()
		steps, err := svc.Workflows.ListWorkflowNodeRuns(context.Background(), runID)
		if err != nil || len(steps) == 0 || steps[0].TaskID == nil {
			t.Fatalf("node run has no task: steps=%v err=%v", steps, err)
		}
		task, err := tasks.GetTask(context.Background(), *steps[0].TaskID)
		if err != nil {
			t.Fatalf("GetTask: %v", err)
		}
		return task.IssueID
	}

	t.Run("required refuses a run without an issue", func(t *testing.T) {
		svc, _ := buildSvc("required")
		_, _, err := svc.StartWorkflowRun(context.Background(), StartWorkflowRunCmd{SpaceID: "tm_1", UserID: "u1", WorkflowID: "w_1"})
		if !errors.Is(err, ErrIssueRequired) {
			t.Fatalf("err = %v, want ErrIssueRequired", err)
		}
	})
	t.Run("if_bound attaches the run's issue to the task", func(t *testing.T) {
		svc, tasks := buildSvc("if_bound")
		run, _, err := svc.StartWorkflowRun(context.Background(), StartWorkflowRunCmd{SpaceID: "tm_1", UserID: "u1", WorkflowID: "w_1", IssueID: util.Ptr("iss_1")})
		if err != nil {
			t.Fatalf("StartWorkflowRun: %v", err)
		}
		if got := taskIssueID(t, svc, tasks, run.ID); got == nil || *got != "iss_1" {
			t.Fatalf("task issue id = %v, want iss_1", got)
		}
	})
	t.Run("none withholds the issue even when the run has one", func(t *testing.T) {
		svc, tasks := buildSvc("none")
		run, _, err := svc.StartWorkflowRun(context.Background(), StartWorkflowRunCmd{SpaceID: "tm_1", UserID: "u1", WorkflowID: "w_1", IssueID: util.Ptr("iss_1")})
		if err != nil {
			t.Fatalf("StartWorkflowRun: %v", err)
		}
		if got := taskIssueID(t, svc, tasks, run.ID); got != nil {
			t.Fatalf("task issue id = %v, want nil (none withholds it)", *got)
		}
	})
}

func TestStartWorkflowRun_StepsUseAgentSnapshot(t *testing.T) {
	workflowStore := &mock.MockWorkflowStore{
		Workflows: []coreworkflow.Workflow{{
			ID:         "w_1",
			SpaceID:    "tm_1",
			Name:       "WF",
			Definition: `{"schema_version":1,"nodes":[{"id":"collect","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"collect data"}},{"id":"summarize","type":"agent_task","needs":["collect"],"agent":{"id":"a_2"},"input":{"instruction":"summarize"}}]}`,
			Status:     coreworkflow.StatusPublished,
			Revision:   3,
		}},
	}
	taskStore := &mock.MockTaskStore{}
	taskRuns := &mock.MockTaskRunStore{}
	agentStore := &mock.MockAgentStore{
		Agents: []agentdef.Agent{
			{ID: "a_1", SpaceID: "tm_1", Name: "Collector", Description: "collects", Instructions: "collect carefully", Revision: 1},
			{ID: "a_2", SpaceID: "tm_1", Name: "Summarizer", Description: "summarizes", Instructions: "summarize carefully", Revision: 2},
		},
	}
	svc := &Service{
		Workflows: workflowStore,
		Agents:    agentStore,
		TaskRuns:  taskRuns,
		TaskService: &task.Service{
			Agents:   agentStore,
			Tasks:    taskStore,
			TaskRuns: taskRuns,
		},
	}
	run, steps, err := svc.StartWorkflowRun(context.Background(), StartWorkflowRunCmd{
		SpaceID:    "tm_1",
		UserID:     "u1",
		WorkflowID: "w_1",
	})
	if err != nil {
		t.Fatalf("StartWorkflowRun: %v", err)
	}
	if steps[1].AgentName != "Summarizer" || steps[1].AgentInstructions != "summarize carefully" {
		t.Fatalf("step[1] snapshot = %q/%q, want Summarizer/summarize carefully", steps[1].AgentName, steps[1].AgentInstructions)
	}
	if run.WorkflowRevision != 3 {
		t.Fatalf("run workflow revision = %d, want 3", run.WorkflowRevision)
	}
	if steps[1].AgentRevision != 2 {
		t.Fatalf("step[1] agent revision = %d, want 2", steps[1].AgentRevision)
	}

	// Editing the agent after the run started must not change a step still pending.
	for i := range agentStore.Agents {
		if agentStore.Agents[i].ID == "a_2" {
			agentStore.Agents[i].Name = "Renamed"
			agentStore.Agents[i].Instructions = "rewritten mid-run"
		}
	}

	output := "done"
	taskRuns.Runs = append(taskRuns.Runs, coretask.Run{
		ID:     *steps[0].TaskRunID,
		TaskID: *steps[0].TaskID,
		Status: string(coretask.RunStatusSucceeded),
		Output: &output,
	})
	if err := svc.HandleTaskRunTerminal(context.Background(), coretask.RunTerminalInfo{
		TaskRunID: *steps[0].TaskRunID,
		TaskID:    *steps[0].TaskID,
		UserID:    "u1",
		Status:    string(coretask.RunStatusSucceeded),
		Output:    &output,
	}); err != nil {
		t.Fatalf("HandleTaskRunTerminal: %v", err)
	}

	updated, err := workflowStore.ListWorkflowNodeRuns(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("ListWorkflowNodeRuns: %v", err)
	}
	if updated[1].Status != string(coreworkflow.NodeRunStatusRunning) {
		t.Fatalf("step[1] status = %q, want running", updated[1].Status)
	}
	if len(taskStore.List) != 2 {
		t.Fatalf("tasks created = %d, want 2", len(taskStore.List))
	}
	secondInput := taskStore.List[1].Input
	if !strings.Contains(secondInput, "summarize carefully") {
		t.Fatalf("second task input lost the snapshot: %q", secondInput)
	}
	if strings.Contains(secondInput, "rewritten mid-run") {
		t.Fatalf("second task input used the edited agent: %q", secondInput)
	}
}

func TestUpdateWorkflow_RecordsRevisions(t *testing.T) {
	workflowStore := &mock.MockWorkflowStore{}
	agentStore := &mock.MockAgentStore{
		Agents: []agentdef.Agent{{ID: "a_1", SpaceID: "tm_1", Name: "Agent 1", Revision: 1}},
	}
	svc := &Service{Workflows: workflowStore, Agents: agentStore}
	first := `{"schema_version":1,"nodes":[{"id":"collect","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"collect data"}}]}`
	created, err := svc.CreateWorkflow(context.Background(), CreateWorkflowCmd{
		SpaceID: "tm_1", UserID: "u1", Name: "WF", Definition: first,
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if created.Revision != 1 {
		t.Fatalf("created revision = %d, want 1", created.Revision)
	}

	second := `{"schema_version":1,"nodes":[{"id":"collect","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"collect more data"}}]}`
	updated, err := svc.UpdateWorkflow(context.Background(), UpdateWorkflowCmd{
		SpaceID: "tm_1", UserID: "u2", WorkflowID: created.ID, Definition: &second,
	})
	if err != nil {
		t.Fatalf("UpdateWorkflow: %v", err)
	}
	if updated.Revision != 2 {
		t.Fatalf("updated revision = %d, want 2", updated.Revision)
	}

	// Saving the same content again is not a revision.
	if _, err := svc.UpdateWorkflow(context.Background(), UpdateWorkflowCmd{
		SpaceID: "tm_1", UserID: "u2", WorkflowID: created.ID, Definition: &second,
	}); err != nil {
		t.Fatalf("UpdateWorkflow no-op: %v", err)
	}

	revisions, total, err := svc.ListWorkflowRevisions(context.Background(), "tm_1", created.ID, 0, 0)
	if err != nil {
		t.Fatalf("ListWorkflowRevisions: %v", err)
	}
	if total != 2 {
		t.Fatalf("revisions = %d, want 2", total)
	}
	if revisions[0].Revision != 2 || revisions[0].CreatedBy != "u2" {
		t.Fatalf("newest revision = %d by %q, want 2 by u2", revisions[0].Revision, revisions[0].CreatedBy)
	}
	if revisions[1].Definition != first {
		t.Fatalf("revision 1 definition = %q, want the original", revisions[1].Definition)
	}
}

func TestRestoreWorkflowRevision_AppendsAndKeepsStatus(t *testing.T) {
	workflowStore := &mock.MockWorkflowStore{}
	agentStore := &mock.MockAgentStore{
		Agents: []agentdef.Agent{{ID: "a_1", SpaceID: "tm_1", Name: "Agent 1", Revision: 1}},
	}
	svc := &Service{Workflows: workflowStore, Agents: agentStore}
	first := `{"schema_version":1,"nodes":[{"id":"collect","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"collect data"}}]}`
	created, err := svc.CreateWorkflow(context.Background(), CreateWorkflowCmd{
		SpaceID: "tm_1", UserID: "u1", Name: "WF", Definition: first,
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	published := coreworkflow.StatusPublished
	if _, err := svc.UpdateWorkflow(context.Background(), UpdateWorkflowCmd{
		SpaceID: "tm_1", UserID: "u1", WorkflowID: created.ID, Status: &published,
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	second := `{"schema_version":1,"nodes":[{"id":"collect","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"collect more data"}}]}`
	if _, err := svc.UpdateWorkflow(context.Background(), UpdateWorkflowCmd{
		SpaceID: "tm_1", UserID: "u1", WorkflowID: created.ID, Definition: &second,
	}); err != nil {
		t.Fatalf("UpdateWorkflow: %v", err)
	}

	restored, err := svc.RestoreWorkflowRevision(context.Background(), RestoreWorkflowRevisionCmd{
		SpaceID: "tm_1", UserID: "u3", WorkflowID: created.ID, Revision: 1,
	})
	if err != nil {
		t.Fatalf("RestoreWorkflowRevision: %v", err)
	}
	if restored.Definition != first {
		t.Fatalf("restored definition = %q, want the revision 1 definition", restored.Definition)
	}
	if restored.Revision != 4 {
		t.Fatalf("restored revision = %d, want 4 — a restore appends rather than rewinds", restored.Revision)
	}
	// Revision 1 was a draft; restoring its content must not unpublish the workflow.
	if restored.Status != coreworkflow.StatusPublished {
		t.Fatalf("restored status = %q, want published", restored.Status)
	}

	if _, err := svc.RestoreWorkflowRevision(context.Background(), RestoreWorkflowRevisionCmd{
		SpaceID: "tm_1", UserID: "u3", WorkflowID: created.ID, Revision: 99,
	}); err != ErrWorkflowRevisionNotFound {
		t.Fatalf("restore of missing revision err = %v, want ErrWorkflowRevisionNotFound", err)
	}
}

// TestDeletedAgent_RunFinishesButNextStepIsRefused pins the admission boundary:
// a TaskRun already in flight completes, but a later workflow step cannot
// create a new Task for an agent deleted in the meantime.
func TestDeletedAgent_RunFinishesButNextStepIsRefused(t *testing.T) {
	definition := `{"schema_version":1,"nodes":[{"id":"collect","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"collect data"}},{"id":"summarize","type":"agent_task","needs":["collect"],"agent":{"id":"a_2"},"input":{"instruction":"summarize"}}]}`
	workflowStore := &mock.MockWorkflowStore{
		Workflows: []coreworkflow.Workflow{{
			ID:         "w_1",
			SpaceID:    "tm_1",
			Name:       "WF",
			Definition: definition,
			Status:     coreworkflow.StatusPublished,
			Revision:   1,
		}},
	}
	taskStore := &mock.MockTaskStore{}
	taskRuns := &mock.MockTaskRunStore{}
	agentStore := &mock.MockAgentStore{
		Agents: []agentdef.Agent{
			{ID: "a_1", SpaceID: "tm_1", Name: "Collector", Instructions: "collect carefully", Revision: 1},
			{ID: "a_2", SpaceID: "tm_1", Name: "Summarizer", Instructions: "summarize carefully", Revision: 1},
		},
	}
	svc := &Service{
		Workflows: workflowStore,
		Agents:    agentStore,
		TaskRuns:  taskRuns,
		TaskService: &task.Service{
			Agents:   agentStore,
			Tasks:    taskStore,
			TaskRuns: taskRuns,
		},
	}
	run, steps, err := svc.StartWorkflowRun(context.Background(), StartWorkflowRunCmd{
		SpaceID: "tm_1", UserID: "u1", WorkflowID: "w_1",
	})
	if err != nil {
		t.Fatalf("StartWorkflowRun: %v", err)
	}

	if err := agentStore.DeleteAgentInSpace(context.Background(), "a_2", "tm_1"); err != nil {
		t.Fatalf("DeleteAgentInSpace: %v", err)
	}

	output := "done"
	taskRuns.Runs = append(taskRuns.Runs, coretask.Run{
		ID:     *steps[0].TaskRunID,
		TaskID: *steps[0].TaskID,
		Status: string(coretask.RunStatusSucceeded),
		Output: &output,
	})
	if err := svc.HandleTaskRunTerminal(context.Background(), coretask.RunTerminalInfo{
		TaskRunID: *steps[0].TaskRunID,
		TaskID:    *steps[0].TaskID,
		UserID:    "u1",
		Status:    string(coretask.RunStatusSucceeded),
		Output:    &output,
	}); err != task.ErrAgentNotFound {
		t.Fatalf("HandleTaskRunTerminal err = %v, want ErrAgentNotFound", err)
	}
	updated, err := workflowStore.ListWorkflowNodeRuns(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("ListWorkflowNodeRuns: %v", err)
	}
	if updated[1].Status != string(coreworkflow.NodeRunStatusFailed) {
		t.Fatalf("step[1] status = %q, want failed after admission refusal", updated[1].Status)
	}
	if len(taskStore.List) != 1 {
		t.Fatalf("created %d tasks, want only the already-admitted first task", len(taskStore.List))
	}

	// Nothing new may name the deleted agent: not a fresh run of the workflow
	// that still references it, and not a definition written from now on.
	if _, _, err := svc.StartWorkflowRun(context.Background(), StartWorkflowRunCmd{
		SpaceID: "tm_1", UserID: "u1", WorkflowID: "w_1",
	}); err != ErrInvalidTargetAgent {
		t.Fatalf("start with deleted agent err = %v, want ErrInvalidTargetAgent", err)
	}
	if _, err := svc.CreateWorkflow(context.Background(), CreateWorkflowCmd{
		SpaceID: "tm_1", UserID: "u1", Name: "New", Definition: definition,
	}); err != ErrInvalidTargetAgent {
		t.Fatalf("create referencing deleted agent err = %v, want ErrInvalidTargetAgent", err)
	}
}

func TestPublishedWorkflowsUsingAgent(t *testing.T) {
	using := `{"schema_version":1,"nodes":[{"id":"s","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"p"}}]}`
	other := `{"schema_version":1,"nodes":[{"id":"s","type":"agent_task","agent":{"id":"a_2"},"input":{"instruction":"p"}}]}`
	workflowStore := &mock.MockWorkflowStore{
		Workflows: []coreworkflow.Workflow{
			{ID: "w_pub", SpaceID: "tm_1", Name: "Published", Definition: using, Status: coreworkflow.StatusPublished},
			{ID: "w_draft", SpaceID: "tm_1", Name: "Draft", Definition: using, Status: coreworkflow.StatusDraft},
			{ID: "w_arch", SpaceID: "tm_1", Name: "Archived", Definition: using, Status: coreworkflow.StatusArchived},
			{ID: "w_other", SpaceID: "tm_1", Name: "Other agent", Definition: other, Status: coreworkflow.StatusPublished},
			{ID: "w_broken", SpaceID: "tm_1", Name: "Broken", Definition: "not json", Status: coreworkflow.StatusPublished},
		},
	}
	svc := &Service{Workflows: workflowStore}
	found, err := svc.PublishedWorkflowsUsingAgent(context.Background(), "tm_1", "a_1")
	if err != nil {
		t.Fatalf("PublishedWorkflowsUsingAgent: %v", err)
	}
	if len(found) != 1 || found[0].ID != "w_pub" {
		t.Fatalf("found = %v, want only w_pub — a draft or archived workflow cannot run, and a broken one never could", found)
	}
}

func TestStepAgent_FallsBackToLiveAgentForLegacyNodeRun(t *testing.T) {
	agentStore := &mock.MockAgentStore{
		Agents: []agentdef.Agent{{ID: "a_1", SpaceID: "tm_1", Name: "Collector", Instructions: "collect"}},
	}
	svc := &Service{Agents: agentStore}
	agent, err := svc.stepAgent(context.Background(), "tm_1", "a_1", coreworkflow.NodeRun{})
	if err != nil {
		t.Fatalf("stepAgent: %v", err)
	}
	if agent.Name != "Collector" || agent.Instructions != "collect" {
		t.Fatalf("agent = %q/%q, want Collector/collect", agent.Name, agent.Instructions)
	}
	if _, err := svc.stepAgent(context.Background(), "tm_other", "a_1", coreworkflow.NodeRun{}); err != ErrInvalidTargetAgent {
		t.Fatalf("cross-space stepAgent err = %v, want ErrInvalidTargetAgent", err)
	}
}

// A canceled step stops the run without calling it a failure. The engine reacts
// to a cancel exactly as it does to a failure — later steps are blocked, the run
// ends — but a reader has to be able to tell "someone stopped this" from
// "something went wrong".
func TestHandleTaskRunTerminal_CancelStopsTheRunWithoutFailingIt(t *testing.T) {
	workflowStore := &mock.MockWorkflowStore{
		Workflows: []coreworkflow.Workflow{{
			ID:         "w_1",
			SpaceID:    "tm_1",
			Name:       "WF",
			Definition: `{"schema_version":1,"nodes":[{"id":"collect","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"collect data"}},{"id":"summarize","type":"agent_task","needs":["collect"],"agent":{"id":"a_2"},"input":{"instruction":"summarize"}}]}`,
			Status:     coreworkflow.StatusPublished,
		}},
	}
	agentStore := &mock.MockAgentStore{
		Agents: []agentdef.Agent{
			{ID: "a_1", SpaceID: "tm_1", Name: "Collector", Instructions: "collect"},
			{ID: "a_2", SpaceID: "tm_1", Name: "Summarizer", Instructions: "summarize"},
		},
	}
	taskRuns := &mock.MockTaskRunStore{}
	svc := &Service{
		Workflows:   workflowStore,
		Agents:      agentStore,
		TaskRuns:    taskRuns,
		TaskService: &task.Service{Agents: agentStore, Tasks: &mock.MockTaskStore{}, TaskRuns: taskRuns},
	}
	run, steps, err := svc.StartWorkflowRun(context.Background(), StartWorkflowRunCmd{
		SpaceID: "tm_1", UserID: "u1", WorkflowID: "w_1",
	})
	if err != nil {
		t.Fatalf("StartWorkflowRun: %v", err)
	}

	taskRuns.Runs = append(taskRuns.Runs, coretask.Run{
		ID:     *steps[0].TaskRunID,
		TaskID: *steps[0].TaskID,
		Status: string(coretask.RunStatusCanceled),
	})
	if err := svc.HandleTaskRunTerminal(context.Background(), coretask.RunTerminalInfo{
		TaskRunID: *steps[0].TaskRunID,
		TaskID:    *steps[0].TaskID,
		UserID:    "u1",
		Status:    string(coretask.RunStatusCanceled),
	}); err != nil {
		t.Fatalf("HandleTaskRunTerminal: %v", err)
	}

	updatedSteps, err := workflowStore.ListWorkflowNodeRuns(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("ListWorkflowNodeRuns: %v", err)
	}
	if updatedSteps[0].Status != string(coreworkflow.NodeRunStatusCanceled) {
		t.Errorf("step[0] status = %q, want canceled", updatedSteps[0].Status)
	}
	if updatedSteps[1].Status != string(coreworkflow.NodeRunStatusBlocked) {
		t.Errorf("step[1] status = %q, want blocked — a canceled step must not start the next one", updatedSteps[1].Status)
	}
	updatedRun, err := workflowStore.GetWorkflowRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetWorkflowRun: %v", err)
	}
	if updatedRun.Status != string(coreworkflow.RunStatusCanceled) {
		t.Errorf("run status = %q, want canceled", updatedRun.Status)
	}
}

// twoStepReconcileSvc builds a service over in-memory doubles for a published
// two-step workflow, starts a run, and returns the handles a reconciliation test
// drives. The started run has step 0 running against its TaskRun.
func twoStepReconcileSvc(t *testing.T) (svc *Service, workflowStore *mock.MockWorkflowStore, taskStore *mock.MockTaskStore, taskRuns *mock.MockTaskRunStore, run *coreworkflow.Run, steps []coreworkflow.NodeRun) {
	t.Helper()
	workflowStore = &mock.MockWorkflowStore{
		Workflows: []coreworkflow.Workflow{{
			ID:         "w_1",
			SpaceID:    "tm_1",
			Name:       "WF",
			Definition: `{"schema_version":1,"nodes":[{"id":"collect","type":"agent_task","agent":{"id":"a_1"},"input":{"instruction":"collect data"}},{"id":"summarize","type":"agent_task","needs":["collect"],"agent":{"id":"a_2"},"input":{"instruction":"summarize"}}]}`,
			Status:     coreworkflow.StatusPublished,
		}},
	}
	taskStore = &mock.MockTaskStore{}
	taskRuns = &mock.MockTaskRunStore{}
	agentStore := &mock.MockAgentStore{
		Agents: []agentdef.Agent{
			{ID: "a_1", SpaceID: "tm_1", Name: "Collector", Instructions: "collect"},
			{ID: "a_2", SpaceID: "tm_1", Name: "Summarizer", Instructions: "summarize"},
		},
	}
	svc = &Service{
		Workflows: workflowStore,
		Agents:    agentStore,
		TaskRuns:  taskRuns,
		TaskService: &task.Service{
			Agents:   agentStore,
			Tasks:    taskStore,
			TaskRuns: taskRuns,
		},
	}
	var err error
	run, steps, err = svc.StartWorkflowRun(context.Background(), StartWorkflowRunCmd{
		SpaceID: "tm_1", UserID: "u1", WorkflowID: "w_1",
	})
	if err != nil {
		t.Fatalf("StartWorkflowRun: %v", err)
	}
	if steps[0].TaskRunID == nil || steps[0].TaskID == nil {
		t.Fatal("expected first step to be dispatched with a task and run id")
	}
	return svc, workflowStore, taskStore, taskRuns, run, steps
}

// TestReconcile_RecoversLostCallback proves the fold reads persisted TaskRun
// facts: with step 0's TaskRun already succeeded, a direct Reconcile — never a
// callback — advances the run. A lost callback costs a wake-up, not the outcome.
func TestReconcile_RecoversLostCallback(t *testing.T) {
	svc, workflowStore, _, taskRuns, run, steps := twoStepReconcileSvc(t)

	output := "collected"
	taskRuns.Runs = append(taskRuns.Runs, coretask.Run{
		ID:     *steps[0].TaskRunID,
		TaskID: *steps[0].TaskID,
		Status: string(coretask.RunStatusSucceeded),
		Output: &output,
	})

	if err := svc.Reconcile(context.Background(), run.ID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	updated, err := workflowStore.ListWorkflowNodeRuns(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("ListWorkflowNodeRuns: %v", err)
	}
	if updated[0].Status != string(coreworkflow.NodeRunStatusSucceeded) {
		t.Fatalf("step[0] status = %q, want succeeded from persisted state", updated[0].Status)
	}
	if updated[1].Status != string(coreworkflow.NodeRunStatusRunning) {
		t.Fatalf("step[1] status = %q, want running after recovery", updated[1].Status)
	}
}

// TestReconcile_UnwiredTaskRunReaderErrors proves a service that dispatches but
// was built without a TaskRun reader fails a running step's fold loudly instead
// of masquerading it as still-executing. That silent nil once left every run of
// the deployed Server stranded in running because the terminal callback's
// service was constructed without the reader.
func TestReconcile_UnwiredTaskRunReaderErrors(t *testing.T) {
	svc, workflowStore, _, taskRuns, run, steps := twoStepReconcileSvc(t)

	output := "collected"
	taskRuns.Runs = append(taskRuns.Runs, coretask.Run{
		ID:     *steps[0].TaskRunID,
		TaskID: *steps[0].TaskID,
		Status: string(coretask.RunStatusSucceeded),
		Output: &output,
	})
	// Drop the reader the way the buggy wiring did: dispatch still works, but the
	// running step can never be observed.
	svc.TaskRuns = nil

	if err := svc.Reconcile(context.Background(), run.ID); err == nil {
		t.Fatal("Reconcile with no TaskRun reader succeeded, want an error")
	}
	updated, err := workflowStore.ListWorkflowNodeRuns(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("ListWorkflowNodeRuns: %v", err)
	}
	if updated[0].Status != string(coreworkflow.NodeRunStatusRunning) {
		t.Fatalf("step[0] status = %q, want still running (unfolded)", updated[0].Status)
	}
}

// TestReconcile_IdempotentDoesNotDoubleDispatch proves repeating a pass over the
// same terminal fact does not accept the outcome twice or admit a second Task.
func TestReconcile_IdempotentDoesNotDoubleDispatch(t *testing.T) {
	svc, workflowStore, taskStore, taskRuns, run, steps := twoStepReconcileSvc(t)

	output := "collected"
	taskRuns.Runs = append(taskRuns.Runs, coretask.Run{
		ID:     *steps[0].TaskRunID,
		TaskID: *steps[0].TaskID,
		Status: string(coretask.RunStatusSucceeded),
		Output: &output,
	})

	for pass := 0; pass < 2; pass++ {
		if err := svc.Reconcile(context.Background(), run.ID); err != nil {
			t.Fatalf("Reconcile pass %d: %v", pass, err)
		}
	}

	// Step 0's task plus step 1's next task: exactly two, never a duplicate from
	// the second pass. The admission key names the logical node, so re-admission
	// resolves to the one task.
	if len(taskStore.List) != 2 {
		t.Fatalf("tasks created = %d, want 2 (no double dispatch)", len(taskStore.List))
	}
	updated, err := workflowStore.ListWorkflowNodeRuns(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("ListWorkflowNodeRuns: %v", err)
	}
	if updated[0].Status != string(coreworkflow.NodeRunStatusSucceeded) {
		t.Fatalf("step[0] status = %q, want succeeded", updated[0].Status)
	}
	runningCount := 0
	for i := range updated {
		if updated[i].Status == string(coreworkflow.NodeRunStatusRunning) {
			runningCount++
		}
	}
	if runningCount != 1 {
		t.Fatalf("running steps = %d, want exactly 1", runningCount)
	}
	updatedRun, err := workflowStore.GetWorkflowRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetWorkflowRun: %v", err)
	}
	if updatedRun.Status != string(coreworkflow.RunStatusRunning) {
		t.Fatalf("run status = %q, want a legal running state", updatedRun.Status)
	}
}

// TestReconcile_FailedTaskRunFailsRunAndBlocksLaterSteps proves a failed TaskRun
// folds to a failed run and blocks the later pending step.
func TestReconcile_FailedTaskRunFailsRunAndBlocksLaterSteps(t *testing.T) {
	svc, workflowStore, taskStore, taskRuns, run, steps := twoStepReconcileSvc(t)

	msg := "collector crashed"
	taskRuns.Runs = append(taskRuns.Runs, coretask.Run{
		ID:           *steps[0].TaskRunID,
		TaskID:       *steps[0].TaskID,
		Status:       string(coretask.RunStatusFailed),
		ErrorMessage: &msg,
	})

	if err := svc.Reconcile(context.Background(), run.ID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	updated, err := workflowStore.ListWorkflowNodeRuns(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("ListWorkflowNodeRuns: %v", err)
	}
	if updated[0].Status != string(coreworkflow.NodeRunStatusFailed) {
		t.Fatalf("step[0] status = %q, want failed", updated[0].Status)
	}
	if updated[1].Status != string(coreworkflow.NodeRunStatusBlocked) {
		t.Fatalf("step[1] status = %q, want blocked after an upstream failure", updated[1].Status)
	}
	// No task was dispatched for the blocked step.
	if len(taskStore.List) != 1 {
		t.Fatalf("tasks created = %d, want only the failed first step's task", len(taskStore.List))
	}
	updatedRun, err := workflowStore.GetWorkflowRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetWorkflowRun: %v", err)
	}
	if updatedRun.Status != string(coreworkflow.RunStatusFailed) {
		t.Fatalf("run status = %q, want failed", updatedRun.Status)
	}
}
