package scheduler

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/infra/db"
	tasksvc "github.com/icloudbb/buildmax/internal/service/task"
	workflowsvc "github.com/icloudbb/buildmax/internal/service/workflow"
)

// TestWorkflowRestartRecovery proves the recovery loop finishes a run stranded
// by a lost terminal callback: a step's TaskRun is driven terminal through the
// store, the callback is never delivered, and a fresh loop's sweep -- reading
// only durable state -- folds it and dispatches the next step. It runs in the
// MySQL scope; the ordinary suite skips it for want of a DSN.
//
// A run whose only pass ran at StartWorkflowRun is scheduled a short interval
// out, so the loop is given a clock past that time to make it due without the
// test waiting on the wall clock. The recovery is real: the loop's sweep calls
// the real service against real storage.
func TestWorkflowRestartRecovery(t *testing.T) {
	// The literal rather than config.EnvKeyBuildmaxTestDSN: the server layer must
	// not import internal/config, and this is only a test's DSN gate.
	const dsnEnv = "BUILDMAX_TEST_DSN"
	dsn := os.Getenv(dsnEnv)
	if dsn == "" {
		t.Skip(dsnEnv + " not set, skipping store integration test")
	}
	ctx := context.Background()
	store, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}

	email := fmt.Sprintf("workflow-restart-%d@example.com", time.Now().UnixNano())
	user, err := store.CreateUser(ctx, email, "free_trial")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	space, err := store.GetPersonalSpaceByUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetPersonalSpaceByUser: %v", err)
	}
	agentA, err := store.CreateAgentInSpace(ctx, agentdef.CreateInput{
		SpaceID: space.ID, UserID: user.ID,
		Def: agentdef.Definition{Name: "Collector", Instructions: "collect"},
	})
	if err != nil {
		t.Fatalf("CreateAgentInSpace A: %v", err)
	}
	agentB, err := store.CreateAgentInSpace(ctx, agentdef.CreateInput{
		SpaceID: space.ID, UserID: user.ID,
		Def: agentdef.Definition{Name: "Summarizer", Instructions: "summarize"},
	})
	if err != nil {
		t.Fatalf("CreateAgentInSpace B: %v", err)
	}

	taskSvc := &tasksvc.Service{Agents: store, Tasks: store, TaskRuns: store}
	svc := &workflowsvc.Service{
		Workflows:   store,
		Agents:      store,
		Issues:      store,
		TaskService: taskSvc,
		TaskRuns:    store,
	}
	definition := fmt.Sprintf(
		`{"schema_version":1,"nodes":[{"id":"collect","type":"agent_task","target_agent_id":%q,"prompt":"collect"},{"id":"summarize","type":"agent_task","needs":["collect"],"target_agent_id":%q,"prompt":"summarize"}]}`,
		agentA.ID, agentB.ID)
	wf, err := svc.CreateWorkflow(ctx, workflowsvc.CreateWorkflowCmd{
		SpaceID: space.ID, UserID: user.ID, Name: "WF", Definition: definition,
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	published := coreworkflow.StatusPublished
	if _, err := svc.UpdateWorkflow(ctx, workflowsvc.UpdateWorkflowCmd{
		SpaceID: space.ID, UserID: user.ID, WorkflowID: wf.ID, Status: &published,
	}); err != nil {
		t.Fatalf("publish workflow: %v", err)
	}

	run, steps, err := svc.StartWorkflowRun(ctx, workflowsvc.StartWorkflowRunCmd{
		SpaceID: space.ID, UserID: user.ID, WorkflowID: wf.ID,
	})
	if err != nil {
		t.Fatalf("StartWorkflowRun: %v", err)
	}
	if steps[0].Status != string(coreworkflow.NodeRunStatusRunning) || steps[0].TaskRunID == nil {
		t.Fatalf("step[0] = %+v, want running with a task run", steps[0])
	}

	// The step's worker finished, but its callback was lost: drive the TaskRun
	// terminal through the store's valid transitions and never call the callback.
	out := "collected"
	driveTaskRunTerminalForTest(t, store, *steps[0].TaskRunID, coretask.RunStatusSucceeded, &out)

	// A brand-new loop, as a restarted Server builds. Its clock is an hour ahead,
	// so the run scheduled a short interval out is due on this sweep. Calling
	// sweep directly keeps the assertion deterministic; the Start path is covered
	// by the loop's unit tests.
	loop, err := NewWorkflowRecoveryLoop(svc, time.Hour)
	if err != nil {
		t.Fatalf("NewWorkflowRecoveryLoop: %v", err)
	}
	loop.WithClock(func() time.Time { return time.Now().UTC().Add(time.Hour) })
	loop.sweep(ctx)

	after, err := store.ListWorkflowNodeRuns(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListWorkflowNodeRuns: %v", err)
	}
	if after[0].Status != string(coreworkflow.NodeRunStatusSucceeded) {
		t.Errorf("step[0] status = %q, want succeeded after recovery", after[0].Status)
	}
	if after[0].Output == nil || *after[0].Output != out {
		t.Errorf("step[0] summary = %v, want %q", after[0].Output, out)
	}
	if after[1].Status != string(coreworkflow.NodeRunStatusRunning) {
		t.Errorf("step[1] status = %q, want running (next step dispatched)", after[1].Status)
	}
	if _, total, err := store.ListTasksByAgent(ctx, space.ID, agentB.ID, 0, 0); err != nil {
		t.Fatalf("ListTasksByAgent: %v", err)
	} else if total != 1 {
		t.Errorf("agent B tasks = %d, want exactly 1 (no duplicate dispatch)", total)
	}
	if got, err := store.GetWorkflowRun(ctx, run.ID); err != nil {
		t.Fatalf("GetWorkflowRun: %v", err)
	} else if got.Status != string(coreworkflow.RunStatusRunning) {
		t.Errorf("run status = %q, want running (step 1 in flight)", got.Status)
	}
}

// driveTaskRunTerminalForTest walks a run through the store's valid transitions
// to a terminal status, so a reconciliation folds a fact the store produced.
func driveTaskRunTerminalForTest(t *testing.T, store *db.Store, taskRunID string, terminal coretask.RunStatus, output *string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	for _, step := range []struct{ from, to coretask.RunStatus }{
		{coretask.RunStatusPending, coretask.RunStatusScheduled},
		{coretask.RunStatusScheduled, coretask.RunStatusRunning},
		{coretask.RunStatusRunning, terminal},
	} {
		in := coretask.TransitionRunInput{
			TaskRunID: taskRunID, ExpectedStatus: step.from, NewStatus: step.to, StartedAt: &now,
		}
		if step.to == terminal {
			in.StartedAt = nil
			in.EndedAt = &now
			in.Output = output
		}
		ok, err := store.TransitionTaskRun(ctx, in)
		if err != nil || !ok {
			t.Fatalf("TransitionTaskRun %s->%s: ok=%v err=%v", step.from, step.to, ok, err)
		}
	}
}
