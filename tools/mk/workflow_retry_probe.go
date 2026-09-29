package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// The workflow-retry probe proves, against the running kind deployment, that a
// Workflow step allowed a second attempt survives losing its worker: the lost
// attempt settles FAILED, the node waits out its backoff, and the next attempt
// runs on a fresh worker on the same Task and finishes the run. Store and
// service tests prove the state machine; only a real worker, the reaper, and
// the recovery loop together prove the retry actually executes.
// See docs/design/workflow-runtime.md §12.2.

const workflowRetryProbeEmail = "workflow-retry-probe@buildmax.local"

// workflowRetrySettleDeadline bounds the wait for the retried run to succeed:
// the lost attempt's report, the 30-second first backoff, a recovery-loop
// interval, a worker start, and the second attempt itself.
const workflowRetrySettleDeadline = 4 * time.Minute

type probeNodeRun struct {
	Status    string `json:"status"`
	Attempt   int    `json:"attempt"`
	TaskID    string `json:"task_id"`
	TaskRunID string `json:"task_run_id"`
}

type probeRunDetail struct {
	Run struct {
		Status       string  `json:"status"`
		ErrorMessage *string `json:"error_message"`
	} `json:"run"`
	Steps []probeNodeRun `json:"steps"`
}

func kindWorkflowRetryProbe() error {
	fmt.Println("Probing Workflow retry after worker loss...")
	target := kindSmokeTarget()
	ctx := context.Background()
	client := &http.Client{Timeout: 30 * time.Second}

	token, spaceID, err := smokeSignIn(ctx, client, target, workflowRetryProbeEmail)
	if err != nil {
		return err
	}
	space := target.apiBase + "/api/spaces/" + url.PathEscape(spaceID)

	var agent struct {
		ID string `json:"id"`
	}
	if err := requestJSON(ctx, client, http.MethodPost, space+"/agents", token,
		map[string]string{"name": "Retry probe", "instructions": "Answer briefly."}, &agent, http.StatusCreated); err != nil {
		return fmt.Errorf("create the probe agent: %w", err)
	}
	definition, err := json.Marshal(map[string]any{
		"schema_version": 1,
		"nodes": []any{map[string]any{
			"id": "work", "type": "agent_task",
			"agent":  map[string]any{"id": agent.ID},
			"input":  map[string]any{"instruction": "Run until the probe kills the worker, then run again."},
			"policy": map[string]any{"max_attempts": 2},
		}},
	})
	if err != nil {
		return err
	}
	var workflow struct {
		ID string `json:"id"`
	}
	if err := requestJSON(ctx, client, http.MethodPost, space+"/workflows", token,
		map[string]string{"name": "Retry probe", "definition": string(definition)}, &workflow, http.StatusCreated); err != nil {
		return fmt.Errorf("create the probe workflow: %w", err)
	}
	workflowURL := space + "/workflows/" + url.PathEscape(workflow.ID)
	if err := requestJSON(ctx, client, http.MethodPatch, workflowURL, token,
		map[string]string{"status": "published"}, nil, http.StatusOK); err != nil {
		return fmt.Errorf("publish the probe workflow: %w", err)
	}

	var started struct {
		Run struct {
			ID string `json:"id"`
		} `json:"run"`
	}
	if err := requestJSON(ctx, client, http.MethodPost, workflowURL+"/runs", token, map[string]any{}, &started, http.StatusCreated); err != nil {
		return fmt.Errorf("start the probe run: %w", err)
	}
	// Arm after the start: dispatch generates the Task title with its own
	// synchronous model call, which a stall would hold past the request timeout.
	if err := armLLMStall(ctx, client, target, workerLossStall); err != nil {
		return err
	}
	defer func() { _ = armLLMStall(ctx, client, target, 0) }()

	runURL := space + "/workflow-runs/" + url.PathEscape(started.Run.ID)
	first, err := waitForNode(ctx, client, runURL, token, time.Minute, func(n probeNodeRun) bool { return n.TaskRunID != "" })
	if err != nil {
		return fmt.Errorf("the first attempt was never dispatched: %w", err)
	}
	taskURL := space + "/tasks/" + url.PathEscape(first.TaskID)
	if err := waitForTaskStatus(ctx, client, taskURL, token, "RUNNING", 90*time.Second); err != nil {
		return fmt.Errorf("the first attempt did not reach RUNNING before the kill: %w", err)
	}
	job, err := workerJobForRun(ctx, first.TaskRunID)
	if err != nil {
		return err
	}
	fmt.Printf("  deleting worker job %s mid-attempt...\n", job)
	if err := kindKubectl("delete", "job", job, "-n", "buildmax", "--wait=false"); err != nil {
		return fmt.Errorf("delete the worker job: %w", err)
	}
	// The second attempt must not stall: only the first was meant to be lost.
	if err := armLLMStall(ctx, client, target, 0); err != nil {
		return err
	}

	final, err := waitForRunEnd(ctx, client, runURL, token, workflowRetrySettleDeadline)
	if err != nil {
		return err
	}
	if final.Run.Status != "succeeded" || len(final.Steps) != 1 {
		return fmt.Errorf("the retried run ended %s (error %q), want succeeded", final.Run.Status, stringValue(final.Run.ErrorMessage))
	}
	node := final.Steps[0]
	if node.Attempt != 2 || node.TaskID != first.TaskID || node.TaskRunID == first.TaskRunID {
		return fmt.Errorf("the run succeeded without a second attempt on the same Task: %+v (first attempt %+v)", node, first)
	}
	if err := assertRunProvenanceTerminal(ctx, client, target, spaceID, first.TaskRunID, token); err != nil {
		return fmt.Errorf("the lost attempt: %w", err)
	}
	var retry struct {
		Status           string `json:"status"`
		RetryOfTaskRunID string `json:"retry_of_task_run_id"`
	}
	if err := requestJSON(ctx, client, http.MethodGet, space+"/task-runs/"+url.PathEscape(node.TaskRunID), token, nil, &retry, http.StatusOK); err != nil {
		return fmt.Errorf("read the second attempt: %w", err)
	}
	if retry.Status != "SUCCEEDED" || retry.RetryOfTaskRunID != first.TaskRunID {
		return fmt.Errorf("second attempt = %+v, want SUCCEEDED retrying %s", retry, first.TaskRunID)
	}
	fmt.Println("Workflow retry verified: a step that lost its worker ran again on the same Task and finished the run.")
	return nil
}

// waitForNode polls a run until its single node satisfies ok.
func waitForNode(ctx context.Context, client *http.Client, runURL, token string, timeout time.Duration, ok func(probeNodeRun) bool) (probeNodeRun, error) {
	deadline := time.Now().Add(timeout)
	for {
		var detail probeRunDetail
		if err := requestJSON(ctx, client, http.MethodGet, runURL, token, nil, &detail, http.StatusOK); err != nil {
			return probeNodeRun{}, err
		}
		if len(detail.Steps) == 1 && ok(detail.Steps[0]) {
			return detail.Steps[0], nil
		}
		if time.Now().After(deadline) {
			return probeNodeRun{}, fmt.Errorf("node never reached the expected state within %s (last %+v)", timeout, detail.Steps)
		}
		select {
		case <-ctx.Done():
			return probeNodeRun{}, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// waitForRunEnd polls a workflow run until it is terminal.
func waitForRunEnd(ctx context.Context, client *http.Client, runURL, token string, timeout time.Duration) (probeRunDetail, error) {
	deadline := time.Now().Add(timeout)
	for {
		var detail probeRunDetail
		if err := requestJSON(ctx, client, http.MethodGet, runURL, token, nil, &detail, http.StatusOK); err != nil {
			return detail, err
		}
		switch detail.Run.Status {
		case "succeeded", "failed", "canceled":
			return detail, nil
		}
		if time.Now().After(deadline) {
			return detail, fmt.Errorf("the run stayed %s past %s (steps %+v)", detail.Run.Status, timeout, detail.Steps)
		}
		select {
		case <-ctx.Done():
			return detail, ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}
