package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Exercise the public API, Workflow reconciler, Task cancel poll, and real
// worker processes together. At least one root reaches a real worker and runs a
// long Bash call; canceling it must stop the admitted sibling and prevent the
// join from ever being admitted.
func assertWorkflowCancellationSettles(ctx context.Context, client *http.Client, target smokeTarget, spaceID, token string) error {
	if target.llmControlToolCallURL == "" {
		return fmt.Errorf("workflow drain requires the mock tool-call control endpoint")
	}
	base := target.apiBase + "/api/spaces/" + url.PathEscape(spaceID)
	var agent, wf struct {
		ID string `json:"id"`
	}
	if err := requestJSON(ctx, client, http.MethodPost, base+"/agents", token, map[string]string{
		"name": "Workflow cancellation smoke", "instructions": "Execute the requested tool call.",
	}, &agent, http.StatusCreated); err != nil {
		return err
	}
	definition := fmt.Sprintf(`{"schema_version":1,"nodes":[
		{"id":"a","type":"agent_task","agent":{"id":%q},"input":{"instruction":"Wait for cancellation."}},
		{"id":"b","type":"agent_task","agent":{"id":%q},"input":{"instruction":"Wait for cancellation."}},
		{"id":"join","type":"agent_task","needs":["a","b"],"agent":{"id":%q},"input":{"instruction":"Must never start."}}]}`, agent.ID, agent.ID, agent.ID)
	if err := requestJSON(ctx, client, http.MethodPost, base+"/workflows", token, map[string]string{"name": "Workflow cancellation smoke", "definition": definition}, &wf, http.StatusCreated); err != nil {
		return err
	}
	wfURL := base + "/workflows/" + wf.ID
	if err := requestJSON(ctx, client, http.MethodPatch, wfURL, token, map[string]string{"status": "published"}, nil, http.StatusOK); err != nil {
		return err
	}
	// Title requests may consume an override too. Extra arms stay bounded and
	// are cleared on every exit; none reach a subsequent smoke case.
	if err := requestJSON(ctx, client, http.MethodPost, target.llmControlToolCallURL, "", map[string]any{
		"name": "Bash", "args": map[string]any{"command": "sleep 120"}, "times": 16,
	}, nil, http.StatusOK); err != nil {
		return err
	}
	defer func() {
		_ = requestJSON(ctx, client, http.MethodPost, target.llmControlToolCallURL, "", map[string]any{"clear": true}, nil, http.StatusOK)
	}()
	var detail struct {
		Run struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"run"`
		Steps []workflowDrainSmokeNode `json:"steps"`
	}
	if err := requestJSON(ctx, client, http.MethodPost, wfURL+"/runs", token, map[string]any{}, &detail, http.StatusCreated); err != nil {
		return err
	}
	if len(detail.Steps) != 3 || detail.Steps[0].TaskID == "" || detail.Steps[0].TaskRunID == "" || detail.Steps[1].TaskID == "" || detail.Steps[1].TaskRunID == "" {
		return fmt.Errorf("workflow smoke did not admit both roots: %+v", detail)
	}
	runningNode, err := waitForAnyWorkflowTaskRunStatus(ctx, client, base, token, detail.Steps[:2], "RUNNING", 2*time.Minute)
	if err != nil {
		return err
	}
	if err := requestJSON(ctx, client, http.MethodPost, base+"/tasks/"+runningNode.TaskID+"/cancel", token, nil, nil, http.StatusAccepted); err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		if err := requestJSON(ctx, client, http.MethodGet, base+"/workflow-runs/"+detail.Run.ID, token, nil, &detail, http.StatusOK); err != nil {
			return err
		}
		if detail.Run.Status == "canceled" {
			break
		}
		if detail.Run.Status == "failed" || detail.Run.Status == "succeeded" || time.Now().After(deadline) {
			return fmt.Errorf("workflow drain settled incorrectly or timed out: %+v", detail)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	for _, node := range detail.Steps {
		if node.NodeID == "join" {
			if node.Status != "blocked" || node.TaskID != "" {
				return fmt.Errorf("workflow admitted blocked join: %+v", node)
			}
			continue
		}
		var task struct {
			Status string `json:"status"`
		}
		if err := requestJSON(ctx, client, http.MethodGet, base+"/tasks/"+node.TaskID, token, nil, &task, http.StatusOK); err != nil {
			return err
		}
		if node.Status != "canceled" || task.Status != "CANCELED" {
			return fmt.Errorf("workflow ended before worker stopped: node=%+v task=%+v", node, task)
		}
	}
	fmt.Printf("Workflow cancellation passed: admitted root Tasks stopped; join was never admitted (run %s)\n", detail.Run.ID)
	return nil
}

type workflowDrainSmokeNode struct {
	NodeID    string `json:"node_id"`
	TaskID    string `json:"task_id"`
	TaskRunID string `json:"task_run_id"`
	Status    string `json:"status"`
}

func waitForAnyWorkflowTaskRunStatus(ctx context.Context, client *http.Client, base, token string, nodes []workflowDrainSmokeNode, want string, timeout time.Duration) (workflowDrainSmokeNode, error) {
	deadline := time.Now().Add(timeout)
	current := make(map[string]string, len(nodes))
	for {
		for _, node := range nodes {
			var run struct {
				Status string `json:"status"`
			}
			if err := requestJSON(ctx, client, http.MethodGet, base+"/task-runs/"+node.TaskRunID, token, nil, &run, http.StatusOK); err != nil {
				return workflowDrainSmokeNode{}, err
			}
			current[node.NodeID] = run.Status
			if run.Status == want {
				return node, nil
			}
		}
		allTerminal := true
		for _, status := range current {
			if !isTerminalSmokeStatus(status) {
				allTerminal = false
				break
			}
		}
		if allTerminal {
			return workflowDrainSmokeNode{}, fmt.Errorf("workflow root TaskRuns settled before reaching %s: %+v", want, current)
		}
		if time.Now().After(deadline) {
			return workflowDrainSmokeNode{}, fmt.Errorf("workflow root TaskRuns did not reach %s within %s: %+v", want, timeout, current)
		}
		select {
		case <-ctx.Done():
			return workflowDrainSmokeNode{}, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
