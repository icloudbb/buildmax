package db

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/util"
)

// A retry attempt is admitted and linked under the same run lock stop intent
// takes, so an attempt can never execute behind a node the stop canceled.
func TestWorkflowRetryAdmissionAndStopHaveOneWinner(t *testing.T) {
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
			if err := s.db.Model(&workflowNodeRunRow{}).Where("public_id = ?", ids[0]).
				Updates(map[string]any{"timeout_seconds": 600, "max_attempts": 2}).Error; err != nil {
				t.Fatal(err)
			}
			first, err := s.AdmitTask(ctx, &coretask.CreateInput{SpaceID: wf.SpaceID, CreatedBy: run.CreatedBy, InitialRunCreatedBy: run.CreatedBy,
				Input: "work", Title: "work", AdmissionKey: coreworkflow.TaskAdmissionKey(runID, "a"), WorkflowNodeRunID: ids[0]})
			if err != nil {
				t.Fatal(err)
			}
			cleanupTask(t, s, first.ID)
			nodes, err := s.ListWorkflowNodeRuns(ctx, runID)
			if err != nil {
				t.Fatal(err)
			}
			if nodes[0].Attempt != 1 || nodes[0].DeadlineAt == nil || nodes[0].DeadlineAt.Before(time.Now().Add(590*time.Second)) {
				t.Fatalf("first attempt = %d deadline %v, want attempt 1 due in about ten minutes", nodes[0].Attempt, nodes[0].DeadlineAt)
			}
			if err := s.db.Model(&taskRunRow{}).Where("public_id = ?", *first.LastRunID).Update("status", string(coretask.RunStatusFailed)).Error; err != nil {
				t.Fatal(err)
			}
			next := time.Now().UTC().Add(time.Minute)
			ok, err := s.TransitionWorkflowNodeRun(ctx, coreworkflow.TransitionNodeRunInput{
				NodeRunID: ids[0], ExpectedStatus: coreworkflow.NodeRunStatusRunning, NewStatus: coreworkflow.NodeRunStatusRetryWait,
				ErrorMessage: util.Ptr("attempt 1 failed"), NextAttemptAt: &next,
			})
			if err != nil || !ok {
				t.Fatalf("to retry_wait: %v %v", ok, err)
			}
			nodes, _ = s.ListWorkflowNodeRuns(ctx, runID)
			if nodes[0].DeadlineAt != nil || nodes[0].NextAttemptAt == nil {
				t.Fatalf("waiting node deadline=%v next=%v, want only a next attempt time", nodes[0].DeadlineAt, nodes[0].NextAttemptAt)
			}

			key := coreworkflow.TaskRunAdmissionKey(runID, "a", 2)
			retryIn := coretask.CreateRunInput{TaskID: first.ID, Input: "work", CreatedBy: run.CreatedBy,
				CreatedByType: coretask.RunCreatedByTypeUser, TriggerSource: coretask.RunTriggerSourceWorkflowStep,
				RetryOfTaskRunID: first.LastRunID, IdempotencyKey: &key, WorkflowNodeRunID: ids[0], WorkflowNodeFrom: "retry_wait", WorkflowAttempt: 2}
			var retry *coretask.Run
			var admitErr, stopErr error
			var stopped bool
			admit := func() { retry, admitErr = s.CreateTaskRun(ctx, retryIn) }
			stop := func() {
				stopped, stopErr = s.StopWorkflowRun(ctx, coreworkflow.StopRunInput{
					WorkflowRunID: runID, RunExpected: coreworkflow.RunStatusRunning, RunStatus: coreworkflow.RunStatusFailing,
					ErrorMessage: util.Ptr("workflow run exceeded its timeout"),
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
			nodes, err = s.ListWorkflowNodeRuns(ctx, runID)
			if err != nil {
				t.Fatal(err)
			}
			if nodes[1].Status != string(coreworkflow.NodeRunStatusBlocked) {
				t.Fatalf("pending node = %s after stop, want blocked", nodes[1].Status)
			}
			if admitErr == nil {
				if order == "stop-first" {
					t.Fatal("admitted a retry after stop")
				}
				if nodes[0].Status != "running" || nodes[0].Attempt != 2 || nodes[0].TaskRunID == nil || *nodes[0].TaskRunID != retry.ID ||
					nodes[0].NextAttemptAt != nil || nodes[0].DeadlineAt == nil || nodes[0].ErrorMessage != nil {
					t.Fatalf("admitted retry not linked for the drain: %+v", nodes[0])
				}
				if retry.RetryOfTaskRunID == nil || *retry.RetryOfTaskRunID != *first.LastRunID {
					t.Fatalf("retry_of = %v", retry.RetryOfTaskRunID)
				}
				replay, err := s.CreateTaskRun(ctx, retryIn)
				if err != nil || replay.ID != retry.ID {
					t.Fatalf("replay: %+v %v", replay, err)
				}
			} else {
				if order == "admit-first" {
					t.Fatalf("unexpected admission error: %v", admitErr)
				}
				if nodes[0].Status != string(coreworkflow.NodeRunStatusCanceled) || nodes[0].NextAttemptAt != nil {
					t.Fatalf("refused waiting node: %+v", nodes[0])
				}
				runs, err := s.ListTaskRunsByTask(ctx, first.ID)
				if err != nil || len(runs) != 1 {
					t.Fatalf("orphan retry run: %d runs, %v", len(runs), err)
				}
			}
		})
	}
}

func TestWorkflowRetryAdmissionRefusesAForeignKey(t *testing.T) {
	s, runID, ids := workflowRunFixture(t, 1)
	ctx := t.Context()
	run, _ := s.GetWorkflowRun(ctx, runID)
	wf, _ := s.GetWorkflow(ctx, run.WorkflowID)
	first, err := s.AdmitTask(ctx, &coretask.CreateInput{SpaceID: wf.SpaceID, CreatedBy: run.CreatedBy, InitialRunCreatedBy: run.CreatedBy,
		Input: "work", Title: "work", AdmissionKey: coreworkflow.TaskAdmissionKey(runID, "a"), WorkflowNodeRunID: ids[0]})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTask(t, s, first.ID)
	key := "workflow/elsewhere/node/a/attempt/2"
	_, err = s.CreateTaskRun(ctx, coretask.CreateRunInput{TaskID: first.ID, Input: "work", CreatedBy: run.CreatedBy,
		CreatedByType: coretask.RunCreatedByTypeUser, TriggerSource: coretask.RunTriggerSourceWorkflowStep,
		IdempotencyKey: &key, WorkflowNodeRunID: ids[0], WorkflowNodeFrom: "retry_wait", WorkflowAttempt: 2})
	if !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("foreign attempt key err = %v, want not found", err)
	}
}
