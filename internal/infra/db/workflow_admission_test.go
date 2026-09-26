package db

import (
	"sync"
	"testing"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
)

func TestWorkflowAdmissionAndStopHaveOneWinner(t *testing.T) {
	for _, order := range []string{"admit-first", "stop-first", "concurrent"} {
		t.Run(order, func(t *testing.T) {
			s, runID, ids := workflowRunFixture(t, 2)
			ctx := t.Context()
			run, err := s.GetWorkflowRun(ctx, runID)
			if err != nil {
				t.Fatal(err)
			}
			wf, err := s.GetWorkflow(ctx, run.WorkflowID)
			if err != nil {
				t.Fatal(err)
			}
			input := &coretask.CreateInput{SpaceID: wf.SpaceID, CreatedBy: run.CreatedBy, InitialRunCreatedBy: run.CreatedBy,
				Input: "work", Title: "work", AdmissionKey: "workflow/" + runID + "/node/b", WorkflowNodeRunID: ids[1]}
			var admitted *coretask.Task
			var admitErr, stopErr error
			var stopped bool
			admit := func() { admitted, admitErr = s.AdmitTask(ctx, input) }
			stop := func() {
				stopped, stopErr = s.BeginWorkflowRunDrain(ctx, coreworkflow.BeginRunDrainInput{
					WorkflowRunID: runID, NodeRunID: ids[0], NodeExpected: coreworkflow.NodeRunStatusPending, NodeStatus: coreworkflow.NodeRunStatusFailed,
					RunExpected: coreworkflow.RunStatusRunning, RunStatus: coreworkflow.RunStatusFailing,
				})
			}
			switch order {
			case "admit-first":
				admit()
				stop()
			case "stop-first":
				stop()
				admit()
			default:
				var wg sync.WaitGroup
				wg.Add(2)
				go func() { defer wg.Done(); admit() }()
				go func() { defer wg.Done(); stop() }()
				wg.Wait()
			}
			if stopErr != nil || !stopped {
				t.Fatalf("stop: %v %v", stopped, stopErr)
			}
			nodes, err := s.ListWorkflowNodeRuns(ctx, runID)
			if err != nil {
				t.Fatal(err)
			}
			if admitErr == nil {
				cleanupTask(t, s, admitted.ID)
				if order == "stop-first" {
					t.Fatal("admitted work after stop")
				}
				if nodes[1].TaskID == nil || *nodes[1].TaskID != admitted.ID || nodes[1].TaskRunID == nil || nodes[1].Status != "running" || nodes[1].ResolvedInput == nil {
					t.Fatalf("admitted Task invisible to drain: %+v", nodes[1])
				}
				replay, err := s.AdmitTask(ctx, input)
				if err != nil || replay.ID != admitted.ID {
					t.Fatalf("replay: %+v %v", replay, err)
				}
			} else {
				if order == "admit-first" {
					t.Fatalf("unexpected admission error: %v", admitErr)
				}
				if nodes[1].TaskID != nil || nodes[1].Status != "blocked" {
					t.Fatalf("refused node: %+v", nodes[1])
				}
				existing, err := s.taskByAdmissionKey(ctx, wf.SpaceID, input.AdmissionKey)
				if err != nil || existing != nil {
					t.Fatalf("orphan admission: %+v %v", existing, err)
				}
			}
			// A stale second stop cannot change the first cause or sibling state.
			stop()
			if stopErr != nil || stopped {
				t.Fatalf("duplicate stop: %v %v", stopped, stopErr)
			}
		})
	}
}
