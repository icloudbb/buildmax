package db

import (
	"sync"
	"testing"
	"time"

	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/util"
)

func openInput(s *Store, t *testing.T, runID, nodeRunID string, expires *time.Time) (*coreworkflow.Request, bool, error) {
	t.Helper()
	return s.OpenWorkflowRequest(t.Context(), coreworkflow.OpenRequestInput{
		WorkflowRunID: runID, NodeRunID: nodeRunID, NodeExpected: coreworkflow.NodeRunStatusPending,
		Key: "node/a", Kind: coreworkflow.RequestKindInput, Prompt: "Approve?",
		ResponseSchema: util.Ptr(`{"type":"boolean"}`), ExpiresAt: expires,
		ResolvedInput: util.Ptr("Approve?"), Now: time.Now().UTC(),
	})
}

// A request opens under the run lock stop intent takes: it opens on a running
// run or not at all, and a stop cancels whatever request it finds open.
func TestWorkflowRequestOpenAndStopHaveOneWinner(t *testing.T) {
	for _, order := range []string{"open-first", "stop-first", "concurrent"} {
		t.Run(order, func(t *testing.T) {
			s, runID, ids := workflowRunFixture(t, 2)
			var req *coreworkflow.Request
			var opened, stopped bool
			var openErr, stopErr error
			open := func() { req, opened, openErr = openInput(s, t, runID, ids[0], nil) }
			stop := func() {
				stopped, stopErr = s.StopWorkflowRun(t.Context(), coreworkflow.StopRunInput{
					WorkflowRunID: runID, RunExpected: coreworkflow.RunStatusRunning, RunStatus: coreworkflow.RunStatusCanceling,
				})
			}
			switch order {
			case "open-first":
				open()
				stop()
			case "stop-first":
				stop()
				open()
			default:
				var wg sync.WaitGroup
				wg.Add(2)
				go func() { defer wg.Done(); open() }()
				go func() { defer wg.Done(); stop() }()
				wg.Wait()
			}
			if openErr != nil || stopErr != nil || !stopped {
				t.Fatalf("open err=%v stop=%v err=%v", openErr, stopped, stopErr)
			}
			nodes, _ := s.ListWorkflowNodeRuns(t.Context(), runID)
			requests, _ := s.ListWorkflowRequestsByRun(t.Context(), runID)
			if opened {
				if order == "stop-first" {
					t.Fatal("opened a request on a stopped run")
				}
				if len(requests) != 1 || requests[0].ID != req.ID || requests[0].Status != coreworkflow.RequestStatusCanceled {
					t.Fatalf("an open request survived the stop: %+v", requests)
				}
				if nodes[0].Status != string(coreworkflow.NodeRunStatusCanceled) {
					t.Fatalf("waiting node = %s after stop, want canceled", nodes[0].Status)
				}
			} else {
				if order == "open-first" {
					t.Fatal("open-first did not open")
				}
				if len(requests) != 0 || nodes[0].Status != string(coreworkflow.NodeRunStatusBlocked) {
					t.Fatalf("refused open left requests=%v node=%s", requests, nodes[0].Status)
				}
			}
		})
	}
}

func TestWorkflowRequestFirstResponseWinsAndExpiryRefusesAnswers(t *testing.T) {
	s, runID, ids := workflowRunFixture(t, 2)
	ctx := t.Context()
	req, opened, err := openInput(s, t, runID, ids[0], nil)
	if err != nil || !opened {
		t.Fatalf("open: %v %v", opened, err)
	}
	again, reopened, err := openInput(s, t, runID, ids[0], nil)
	if err != nil || !reopened || again.ID != req.ID {
		t.Fatalf("a repeated open = %+v %v %v, want the same request", again, reopened, err)
	}
	nodes, _ := s.ListWorkflowNodeRuns(ctx, runID)
	if nodes[0].Status != "waiting" || nodes[0].ResolvedInput == nil || nodes[0].StartedAt == nil {
		t.Fatalf("waiting node = %+v", nodes[0])
	}
	run, _ := s.GetWorkflowRun(ctx, runID)
	wf, _ := s.GetWorkflow(ctx, run.WorkflowID)
	pending, total, err := s.ListPendingWorkflowRequestsBySpace(ctx, wf.SpaceID, 10, 0)
	if err != nil || total != 1 || len(pending) != 1 || pending[0].NodeID != "a" {
		t.Fatalf("pending = %+v total=%d err=%v", pending, total, err)
	}

	var wins int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, answer := range []string{"true", "false"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := s.ResolveWorkflowRequest(ctx, coreworkflow.ResolveRequestInput{
				RequestID: req.ID, Status: coreworkflow.RequestStatusAnswered, Response: &answer,
				RespondedBy: &run.CreatedBy, Now: time.Now().UTC(),
			})
			if err != nil {
				t.Error(err)
			}
			if ok {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("%d answers won, want exactly one", wins)
	}
	got, _ := s.GetWorkflowRequest(ctx, req.ID)
	if got.Status != "answered" || got.RespondedBy == nil || *got.RespondedBy != run.CreatedBy || got.RespondedAt == nil {
		t.Fatalf("answered request = %+v", got)
	}

	past := time.Now().UTC().Add(-time.Minute)
	expiring, _, err := s.OpenWorkflowRequest(ctx, coreworkflow.OpenRequestInput{
		WorkflowRunID: runID, NodeRunID: ids[1], NodeExpected: coreworkflow.NodeRunStatusPending,
		Key: "node/b", Kind: coreworkflow.RequestKindInput, Prompt: "Late?", ExpiresAt: &past, Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.ResolveWorkflowRequest(ctx, coreworkflow.ResolveRequestInput{
		RequestID: expiring.ID, Status: coreworkflow.RequestStatusAnswered, Response: util.Ptr(`"late"`), Now: time.Now().UTC(),
	}); ok {
		t.Fatal("answered a request past its expiry")
	}
	if ok, err := s.ResolveWorkflowRequest(ctx, coreworkflow.ResolveRequestInput{
		RequestID: expiring.ID, Status: coreworkflow.RequestStatusExpired, Now: time.Now().UTC(),
	}); err != nil || !ok {
		t.Fatalf("expire: %v %v", ok, err)
	}
}
