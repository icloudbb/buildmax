package workflow

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/config"
	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/infra/db"
	"github.com/icloudbb/buildmax/internal/service/task"
)

// The Workflow reconciler is authoritative only against real storage: the crash
// window it recovers, the guarded compare-and-set transitions it folds, and the
// two reconcilers it lets race are cross-transaction behavior a mock cannot
// prove. These tests run in the MySQL scope (`./make test mysql`); the ordinary
// suite skips them for want of a DSN, matching the store tests' guard.

// reconcileEnv is the real service stack a reconciliation test drives, plus the
// seeded space, agents, and published two-step workflow.
type reconcileEnv struct {
	store   *db.Store
	svc     *Service
	spaceID string
	userID  string
	agentA  string
	agentB  string
	wfID    string
}

func newReconcileEnv(t *testing.T) *reconcileEnv {
	t.Helper()
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	store, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}

	// A unique email per run keeps parallel or repeated runs from colliding on
	// the same throwaway user; the MySQL scope's database is dropped after it.
	email := fmt.Sprintf("workflow-reconcile-%d@example.com", time.Now().UnixNano())
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
		Def: agentdef.Definition{Name: "Collector", Instructions: "collect carefully"},
	})
	if err != nil {
		t.Fatalf("CreateAgentInSpace A: %v", err)
	}
	agentB, err := store.CreateAgentInSpace(ctx, agentdef.CreateInput{
		SpaceID: space.ID, UserID: user.ID,
		Def: agentdef.Definition{Name: "Summarizer", Instructions: "summarize carefully"},
	})
	if err != nil {
		t.Fatalf("CreateAgentInSpace B: %v", err)
	}

	taskSvc := &task.Service{Agents: store, Tasks: store, TaskRuns: store}
	svc := &Service{
		Workflows:   store,
		Agents:      store,
		Issues:      store,
		TaskService: taskSvc,
		TaskRuns:    store,
	}

	definition := fmt.Sprintf(
		`{"schema_version":1,"steps":[{"step_id":"collect","type":"agent_task","target_agent_id":%q,"prompt":"collect data"},{"step_id":"summarize","type":"agent_task","target_agent_id":%q,"prompt":"summarize"}]}`,
		agentA.ID, agentB.ID)
	wf, err := svc.CreateWorkflow(ctx, CreateWorkflowCmd{
		SpaceID: space.ID, UserID: user.ID, Name: "WF", Definition: definition,
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	published := coreworkflow.StatusPublished
	if _, err := svc.UpdateWorkflow(ctx, UpdateWorkflowCmd{
		SpaceID: space.ID, UserID: user.ID, WorkflowID: wf.ID, Status: &published,
	}); err != nil {
		t.Fatalf("publish workflow: %v", err)
	}

	return &reconcileEnv{
		store: store, svc: svc,
		spaceID: space.ID, userID: user.ID,
		agentA: agentA.ID, agentB: agentB.ID, wfID: wf.ID,
	}
}

func (e *reconcileEnv) startRun(t *testing.T) (*coreworkflow.Run, []coreworkflow.StepRun) {
	t.Helper()
	run, steps, err := e.svc.StartWorkflowRun(context.Background(), StartWorkflowRunCmd{
		SpaceID: e.spaceID, UserID: e.userID, WorkflowID: e.wfID,
	})
	if err != nil {
		t.Fatalf("StartWorkflowRun: %v", err)
	}
	return run, steps
}

func (e *reconcileEnv) steps(t *testing.T, runID string) []coreworkflow.StepRun {
	t.Helper()
	_, steps, err := e.svc.GetWorkflowRunDetail(context.Background(), e.spaceID, runID)
	if err != nil {
		t.Fatalf("GetWorkflowRunDetail: %v", err)
	}
	return steps
}

func (e *reconcileEnv) run(t *testing.T, runID string) *coreworkflow.Run {
	t.Helper()
	run, err := e.store.GetWorkflowRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetWorkflowRun: %v", err)
	}
	return run
}

// driveTaskRunTerminal walks a step's TaskRun through the store's valid
// transitions (PENDING->SCHEDULED->RUNNING->terminal), so a reconciliation folds
// a terminal fact the real store actually produced rather than one written by
// hand.
func (e *reconcileEnv) driveTaskRunTerminal(t *testing.T, step coreworkflow.StepRun, terminal coretask.RunStatus, output, errMsg *string) {
	t.Helper()
	if step.TaskRunID == nil {
		t.Fatalf("step %q has no task run to drive", step.StepID)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	e.mustTransition(t, *step.TaskRunID, coretask.RunStatusPending, coretask.RunStatusScheduled, coretask.TransitionRunInput{StartedAt: &now})
	e.mustTransition(t, *step.TaskRunID, coretask.RunStatusScheduled, coretask.RunStatusRunning, coretask.TransitionRunInput{StartedAt: &now})
	e.mustTransition(t, *step.TaskRunID, coretask.RunStatusRunning, terminal, coretask.TransitionRunInput{EndedAt: &now, Output: output, ErrorMessage: errMsg})
	// Confirm the fact is durable before the reconciler is asked to read it.
	got, err := e.store.GetTaskRun(ctx, *step.TaskRunID)
	if err != nil {
		t.Fatalf("GetTaskRun: %v", err)
	}
	if got == nil || got.Status != string(terminal) {
		t.Fatalf("task run status = %v, want %s", got, terminal)
	}
}

func (e *reconcileEnv) mustTransition(t *testing.T, taskRunID string, from, to coretask.RunStatus, in coretask.TransitionRunInput) {
	t.Helper()
	in.TaskRunID = taskRunID
	in.ExpectedStatus = from
	in.NewStatus = to
	ok, err := e.store.TransitionTaskRun(context.Background(), in)
	if err != nil {
		t.Fatalf("TransitionTaskRun %s->%s: %v", from, to, err)
	}
	if !ok {
		t.Fatalf("TransitionTaskRun %s->%s was not applied", from, to)
	}
}

func (e *reconcileEnv) tasksForAgent(t *testing.T, agentID string) int {
	t.Helper()
	_, total, err := e.store.ListTasksByAgent(context.Background(), e.spaceID, agentID, 0, 0)
	if err != nil {
		t.Fatalf("ListTasksByAgent: %v", err)
	}
	return total
}

// TestWorkflowReconcileSucceedsThroughBothSteps proves the linear happy path
// over real storage: reconciling a running step whose TaskRun succeeded advances
// to the next step, and reconciling the final success terminalizes the run. It
// drives progress only through direct Reconcile calls — never HandleTaskRunTerminal
// — so it is also the lost-callback recovery case: the outcome comes from
// persisted TaskRun facts, not a pushed payload.
func TestWorkflowReconcileSucceedsThroughBothSteps(t *testing.T) {
	e := newReconcileEnv(t)
	run, steps := e.startRun(t)
	if steps[0].Status != string(coreworkflow.StepRunStatusRunning) {
		t.Fatalf("step[0] status = %q, want running", steps[0].Status)
	}

	// Step 0 finishes; a lost callback is simulated by never invoking
	// HandleTaskRunTerminal and reconciling from the durable state alone.
	out0 := "collected"
	e.driveTaskRunTerminal(t, steps[0], coretask.RunStatusSucceeded, &out0, nil)
	if err := e.svc.Reconcile(context.Background(), run.ID); err != nil {
		t.Fatalf("Reconcile after step 0: %v", err)
	}

	after0 := e.steps(t, run.ID)
	if after0[0].Status != string(coreworkflow.StepRunStatusSucceeded) {
		t.Fatalf("step[0] status = %q, want succeeded", after0[0].Status)
	}
	if after0[0].OutputSummary == nil || *after0[0].OutputSummary != out0 {
		t.Fatalf("step[0] summary = %v, want %q", after0[0].OutputSummary, out0)
	}
	if after0[1].Status != string(coreworkflow.StepRunStatusRunning) {
		t.Fatalf("step[1] status = %q, want running (next step dispatched)", after0[1].Status)
	}
	if e.tasksForAgent(t, e.agentB) != 1 {
		t.Fatalf("agent B tasks = %d, want exactly 1 dispatched", e.tasksForAgent(t, e.agentB))
	}

	// Step 1 finishes; reconciling the last success makes the run succeeded.
	out1 := "summarized"
	e.driveTaskRunTerminal(t, after0[1], coretask.RunStatusSucceeded, &out1, nil)
	if err := e.svc.Reconcile(context.Background(), run.ID); err != nil {
		t.Fatalf("Reconcile after step 1: %v", err)
	}
	after1 := e.steps(t, run.ID)
	if after1[1].Status != string(coreworkflow.StepRunStatusSucceeded) {
		t.Fatalf("step[1] status = %q, want succeeded", after1[1].Status)
	}
	if final := e.run(t, run.ID); final.Status != string(coreworkflow.RunStatusSucceeded) {
		t.Fatalf("run status = %q, want succeeded", final.Status)
	}
}

// TestWorkflowReconcileFailedTaskRunFailsRun proves a failed TaskRun folds to a
// failed run and blocks the later pending step.
func TestWorkflowReconcileFailedTaskRunFailsRun(t *testing.T) {
	e := newReconcileEnv(t)
	run, steps := e.startRun(t)

	msg := "collector crashed"
	e.driveTaskRunTerminal(t, steps[0], coretask.RunStatusFailed, nil, &msg)
	if err := e.svc.Reconcile(context.Background(), run.ID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	after := e.steps(t, run.ID)
	if after[0].Status != string(coreworkflow.StepRunStatusFailed) {
		t.Fatalf("step[0] status = %q, want failed", after[0].Status)
	}
	if after[1].Status != string(coreworkflow.StepRunStatusBlocked) {
		t.Fatalf("step[1] status = %q, want blocked", after[1].Status)
	}
	if final := e.run(t, run.ID); final.Status != string(coreworkflow.RunStatusFailed) {
		t.Fatalf("run status = %q, want failed", final.Status)
	}
	if e.tasksForAgent(t, e.agentB) != 0 {
		t.Fatalf("agent B tasks = %d, want 0 — a blocked step dispatches nothing", e.tasksForAgent(t, e.agentB))
	}
}

// TestWorkflowReconcileCanceledTaskRunCancelsRun proves a canceled TaskRun stops
// the run with the documented cancel-vs-fail distinction and blocks later steps.
func TestWorkflowReconcileCanceledTaskRunCancelsRun(t *testing.T) {
	e := newReconcileEnv(t)
	run, steps := e.startRun(t)

	e.driveTaskRunTerminal(t, steps[0], coretask.RunStatusCanceled, nil, nil)
	if err := e.svc.Reconcile(context.Background(), run.ID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	after := e.steps(t, run.ID)
	if after[0].Status != string(coreworkflow.StepRunStatusCanceled) {
		t.Fatalf("step[0] status = %q, want canceled", after[0].Status)
	}
	if after[1].Status != string(coreworkflow.StepRunStatusBlocked) {
		t.Fatalf("step[1] status = %q, want blocked", after[1].Status)
	}
	if final := e.run(t, run.ID); final.Status != string(coreworkflow.RunStatusCanceled) {
		t.Fatalf("run status = %q, want canceled, not failed", final.Status)
	}
}

// TestWorkflowReconcileConcurrentPassesHaveOneOutcome proves the lease plus the
// guarded compare-and-set transitions leave one accepted outcome, one next Task,
// and one legal run state when two reconcilers observe the same terminal step at
// once. Repeated reconciliation is the same claim over time; racing it is the
// same claim in parallel.
func TestWorkflowReconcileConcurrentPassesHaveOneOutcome(t *testing.T) {
	e := newReconcileEnv(t)
	run, steps := e.startRun(t)

	out0 := "collected"
	e.driveTaskRunTerminal(t, steps[0], coretask.RunStatusSucceeded, &out0, nil)

	const racers = 6
	var wg sync.WaitGroup
	errs := make([]error, racers)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = e.svc.Reconcile(context.Background(), run.ID)
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent Reconcile %d: %v", i, err)
		}
	}

	after := e.steps(t, run.ID)
	if after[0].Status != string(coreworkflow.StepRunStatusSucceeded) {
		t.Fatalf("step[0] status = %q, want succeeded once", after[0].Status)
	}
	running := 0
	for i := range after {
		if after[i].Status == string(coreworkflow.StepRunStatusRunning) {
			running++
		}
	}
	if running != 1 {
		t.Fatalf("running steps = %d, want exactly 1", running)
	}
	if got := e.tasksForAgent(t, e.agentB); got != 1 {
		t.Fatalf("agent B tasks = %d, want exactly 1 — racing reconcilers must not duplicate the next Task", got)
	}
	if final := e.run(t, run.ID); final.Status != string(coreworkflow.RunStatusRunning) {
		t.Fatalf("run status = %q, want a single legal running state", final.Status)
	}
}
