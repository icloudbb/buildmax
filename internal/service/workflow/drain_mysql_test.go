package workflow

import (
	"context"
	"fmt"
	"testing"
	"time"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/util"
)

// A fresh coordinator must recover stop intent committed before the server
// died, and terminal sibling reports must never change the chosen run outcome.
func TestWorkflowDrainRecoversAndWaitsForWorkers(t *testing.T) {
	for _, cause := range []coretask.RunStatus{coretask.RunStatusFailed, coretask.RunStatusCanceled} {
		for _, siblingOutcome := range []coretask.RunStatus{coretask.RunStatusCanceled, coretask.RunStatusSucceeded} {
			t.Run(string(cause)+"/"+string(siblingOutcome), func(t *testing.T) {
				e := newReconcileEnv(t)
				ctx := context.Background()
				definition := fmt.Sprintf(`{"schema_version":1,"nodes":[
					{"id":"a","type":"agent_task","agent":{"id":%q},"input":{"instruction":"a"}},
					{"id":"b","type":"agent_task","agent":{"id":%q},"input":{"instruction":"b"}},
					{"id":"join","type":"agent_task","needs":["a","b"],"agent":{"id":%q},"input":{"instruction":"join"}}]}`, e.agentA, e.agentB, e.agentA)
				if _, err := e.svc.UpdateWorkflow(ctx, UpdateWorkflowCmd{SpaceID: e.spaceID, UserID: e.userID, WorkflowID: e.wfID, Definition: &definition}); err != nil {
					t.Fatal(err)
				}
				run, nodes := e.startRun(t)
				now := time.Now().UTC()
				e.driveTaskRunTerminal(t, nodes[0], cause, nil, util.Ptr("original stop cause"))
				e.mustTransition(t, *nodes[1].TaskRunID, coretask.RunStatusPending, coretask.RunStatusScheduled, coretask.TransitionRunInput{})
				e.mustTransition(t, *nodes[1].TaskRunID, coretask.RunStatusScheduled, coretask.RunStatusRunning, coretask.TransitionRunInput{})
				draining, final := coreworkflow.RunStatusFailing, coreworkflow.RunStatusFailed
				nodeStatus := coreworkflow.NodeRunStatusFailed
				if cause == coretask.RunStatusCanceled {
					draining, final, nodeStatus = coreworkflow.RunStatusCanceling, coreworkflow.RunStatusCanceled, coreworkflow.NodeRunStatusCanceled
				}
				// Crash boundary: intent and blocked nodes committed, no Task cancel sent.
				ok, err := e.store.BeginWorkflowRunDrain(ctx, coreworkflow.BeginRunDrainInput{
					WorkflowRunID: run.ID, NodeRunID: nodes[0].ID, NodeExpected: coreworkflow.NodeRunStatusRunning, NodeStatus: nodeStatus,
					RunExpected: coreworkflow.RunStatusRunning, RunStatus: draining, EndedAt: &now, ErrorMessage: util.Ptr("original stop cause"),
				})
				if err != nil || !ok {
					t.Fatalf("begin drain: %v %v", ok, err)
				}
				fresh := &Service{Workflows: e.store, TaskRuns: e.store, TaskService: e.svc.TaskService}
				for i := 0; i < 2; i++ {
					if err := fresh.Reconcile(ctx, run.ID); err != nil {
						t.Fatal(err)
					}
				}
				stopping := e.run(t, run.ID)
				if stopping.Status != string(draining) || stopping.EndedAt != nil || stopping.NextReconcileAt == nil {
					t.Fatalf("premature termination or lost recovery: %+v", stopping)
				}
				sibling, err := e.store.GetTaskRun(ctx, *nodes[1].TaskRunID)
				if err != nil {
					t.Fatal(err)
				}
				if sibling.Status != string(coretask.RunStatusRunning) || sibling.CancelRequestedAt == nil || sibling.CancelReason != coretask.CancelReasonWorkflowStopped {
					t.Fatalf("missing worker cancellation: %+v", sibling)
				}
				if got := e.steps(t, run.ID); got[1].Status != "running" || got[2].Status != "blocked" || got[2].TaskID != nil {
					t.Fatalf("unexpected node state: %+v", got)
				}
				// Worker report, with no callback: the next recovery pass reads it.
				e.mustTransition(t, sibling.ID, coretask.RunStatusRunning, siblingOutcome, coretask.TransitionRunInput{EndedAt: &now, Output: util.Ptr("partial or completed work")})
				if err := fresh.Reconcile(ctx, run.ID); err != nil {
					t.Fatal(err)
				}
				finished := e.run(t, run.ID)
				if finished.Status != string(final) || finished.EndedAt == nil || finished.ErrorMessage == nil || *finished.ErrorMessage != "original stop cause" {
					t.Fatalf("wrong final outcome: %+v", finished)
				}
				got := e.steps(t, run.ID)
				if got[1].Output == nil || *got[1].Output != "partial or completed work" {
					t.Fatal("lost sibling output")
				}
			})
		}
	}
}
