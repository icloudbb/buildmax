package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// The inputs that identify each outcome fixture on a rerun.
const (
	fixtureFailedInput   = "[kind fixture] Report the configured endpoint."
	fixtureCanceledInput = "[kind fixture] Stall until this run is canceled."
	fixtureWebhookInput  = "[kind fixture] Webhook: summarize the latest QA status."
)

type fxWorkflowRun struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type fxWorkflowRunDetail struct {
	Run   fxWorkflowRun `json:"run"`
	Steps []struct {
		NodeID string  `json:"node_id"`
		TaskID *string `json:"task_id"`
	} `json:"steps"`
}

// waitForWorkflowRun polls a WorkflowRun until it is terminal and reports any
// terminal status other than want as the failure it is.
func waitForWorkflowRun(ctx context.Context, client *http.Client, base, token, id, want string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		var current fxWorkflowRunDetail
		if err := requestJSON(ctx, client, http.MethodGet, base+"/workflow-runs/"+url.PathEscape(id), token, nil, &current, http.StatusOK); err != nil {
			return err
		}
		switch current.Run.Status {
		case want:
			return nil
		case "succeeded", "failed", "canceled":
			return fmt.Errorf("fixture workflow run %s ended %s, not %s", id, current.Run.Status, want)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("fixture workflow run %s did not reach %s within %s (status %s)", id, want, timeout, current.Run.Status)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// seedFixtureOutcomes adds the run states the happy-path fixtures never reach:
// a finished run of the branching Workflow, a Task that fails before its Agent
// starts, and a canceled Task and WorkflowRun. Each is matched by its input or
// terminal status, so a rerun adds only what is missing.
func seedFixtureOutcomes(ctx context.Context, client *http.Client, target smokeTarget, base, token, conversationTasks string) error {
	var workflows fxWorkflowList
	if err := requestJSON(ctx, client, http.MethodGet, base+"/workflows", token, nil, &workflows, http.StatusOK); err != nil {
		return err
	}
	var agents []fxAgent
	if err := requestJSON(ctx, client, http.MethodGet, base+"/agents", token, nil, &agents, http.StatusOK); err != nil {
		return err
	}
	dagID, blockedID := "", ""
	for _, w := range workflows.Workflows {
		if w.Name == fixtureDAGWorkflow {
			dagID = w.ID
		}
	}
	for _, a := range agents {
		if a.Name == fixtureBlockedAgent {
			blockedID = a.ID
		}
	}
	if dagID == "" || blockedID == "" {
		return fmt.Errorf("outcome fixtures need %q and %q", fixtureDAGWorkflow, fixtureBlockedAgent)
	}

	var runs struct {
		Runs []fxWorkflowRun `json:"runs"`
	}
	if err := requestJSON(ctx, client, http.MethodGet, base+"/workflows/"+dagID+"/runs?limit=100", token, nil, &runs, http.StatusOK); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, r := range runs.Runs {
		seen[r.Status] = true
	}
	runsURL := base + "/workflows/" + dagID + "/runs"
	body := map[string]any{"input": json.RawMessage(fixtureDAGInput)}
	if !seen["succeeded"] {
		var created fxWorkflowRunDetail
		if err := requestJSON(ctx, client, http.MethodPost, runsURL, token, body, &created, http.StatusCreated); err != nil {
			return err
		}
		if err := waitForWorkflowRun(ctx, client, base, token, created.Run.ID, "succeeded", 5*time.Minute); err != nil {
			return err
		}
	}

	// The disabled Secret refuses the required grant when the run starts, so
	// this Task fails for a reason the Portal can show, not a timeout.
	agentTasks := base + "/agents/" + blockedID + "/tasks"
	var failed struct {
		Tasks []fxTask `json:"tasks"`
	}
	if err := requestJSON(ctx, client, http.MethodGet, agentTasks, token, nil, &failed, http.StatusOK); err != nil {
		return err
	}
	var task fxTask
	if len(failed.Tasks) > 0 {
		task = failed.Tasks[0]
	} else if err := requestJSON(ctx, client, http.MethodPost, agentTasks, token, map[string]string{"input": fixtureFailedInput}, &task, http.StatusCreated); err != nil {
		return err
	}
	if err := waitForTaskStatus(ctx, client, base+"/tasks/"+task.ID, token, "FAILED", 2*time.Minute); err != nil {
		return fmt.Errorf("%s task: %w", fixtureBlockedAgent, err)
	}

	return seedFixtureCancellations(ctx, target, base, token, conversationTasks, runsURL, !seen["canceled"])
}

// seedFixtureCancellations cancels a conversation Task and a WorkflowRun while
// they are mid-turn. A run on the mock is over before anything can cancel it,
// so this holds the mock's replies the way the smoke's cancellation case does,
// and always releases them before returning.
func seedFixtureCancellations(ctx context.Context, target smokeTarget, base, token, conversationTasks, workflowRunsURL string, needWorkflow bool) error {
	var tasks []fxTask
	patient := &http.Client{Timeout: cancelStall + 30*time.Second}
	if err := requestJSON(ctx, patient, http.MethodGet, conversationTasks, token, nil, &tasks, http.StatusOK); err != nil {
		return err
	}
	needTask := true
	for _, t := range tasks {
		if t.Input == fixtureCanceledInput {
			needTask = false
		}
	}
	if !needTask && !needWorkflow {
		return nil
	}
	if err := armLLMStall(ctx, patient, target, cancelStall); err != nil {
		return err
	}
	defer func() { _ = armLLMStall(context.Background(), patient, target, 0) }()

	if needTask {
		// Creating the Task titles it with a model call, which waits out the stall.
		var task fxTask
		if err := requestJSON(ctx, patient, http.MethodPost, conversationTasks, token, map[string]string{"input": fixtureCanceledInput}, &task, http.StatusCreated); err != nil {
			return err
		}
		if err := cancelFixtureTask(ctx, patient, base, token, task.ID); err != nil {
			return err
		}
	}
	if needWorkflow {
		var created fxWorkflowRunDetail
		if err := requestJSON(ctx, patient, http.MethodPost, workflowRunsURL, token, map[string]any{"input": json.RawMessage(fixtureDAGInput)}, &created, http.StatusCreated); err != nil {
			return err
		}
		// Only the root node is admitted at first, so its Task is the one to
		// cancel; the pending nodes behind it end blocked.
		taskID, err := waitForWorkflowNodeTask(ctx, patient, base, token, created.Run.ID)
		if err != nil {
			return err
		}
		if err := cancelFixtureTask(ctx, patient, base, token, taskID); err != nil {
			return err
		}
		if err := waitForWorkflowRun(ctx, patient, base, token, created.Run.ID, "canceled", 3*time.Minute); err != nil {
			return err
		}
	}
	return nil
}

func waitForWorkflowNodeTask(ctx context.Context, client *http.Client, base, token, runID string) (string, error) {
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		var detail fxWorkflowRunDetail
		if err := requestJSON(ctx, client, http.MethodGet, base+"/workflow-runs/"+url.PathEscape(runID), token, nil, &detail, http.StatusOK); err != nil {
			return "", err
		}
		for _, step := range detail.Steps {
			if step.TaskID != nil {
				return *step.TaskID, nil
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return "", fmt.Errorf("fixture workflow run %s admitted no node Task", runID)
}

// cancelFixtureTask waits for the Task to be executing, cancels it, and waits
// for CANCELED. Waiting for RUNNING first means a worker really gives the run up,
// rather than the cancel landing on a run no worker had claimed.
func cancelFixtureTask(ctx context.Context, client *http.Client, base, token, taskID string) error {
	taskURL := base + "/tasks/" + url.PathEscape(taskID)
	if err := waitForTaskStatus(ctx, client, taskURL, token, "RUNNING", 2*time.Minute); err != nil {
		return err
	}
	response, err := request(ctx, client, http.MethodPost, taskURL+"/cancel", token, "", nil, http.StatusAccepted)
	if err != nil {
		return err
	}
	if err := response.Close(); err != nil {
		return err
	}
	return waitForTaskStatus(ctx, client, taskURL, token, "CANCELED", 2*time.Minute)
}

// seedFixtureWebhookConversation sends one message through the inbound webhook
// so Alice's personal space lists a conversation from a non-Portal channel. The
// sending key exists only for the call: its plaintext is shown once, and a
// fixture has no business leaving a live credential behind.
func seedFixtureWebhookConversation(ctx context.Context, client *http.Client, target smokeTarget) error {
	token, spaceID, err := fixtureSignIn(ctx, client, target, "alice@buildmax.local")
	if err != nil {
		return err
	}
	base := target.apiBase + "/api/spaces/" + url.PathEscape(spaceID)
	conversations, err := fixturePage[struct {
		ID      string `json:"id"`
		Channel string `json:"channel"`
	}](ctx, client, base+"/conversations", token, "conversations")
	if err != nil {
		return err
	}
	// A webhook turn starts a Task without writing a conversation message, so
	// the Task input is what identifies the fixture conversation.
	for _, c := range conversations {
		if c.Channel != "webhook" {
			continue
		}
		var tasks []fxTask
		if err := requestJSON(ctx, client, http.MethodGet, base+"/conversations/"+c.ID+"/tasks", token, nil, &tasks, http.StatusOK); err != nil {
			return err
		}
		for _, t := range tasks {
			if t.Input == fixtureWebhookInput {
				return nil
			}
		}
	}

	var key struct {
		Key string `json:"key"`
		ID  string `json:"id"`
	}
	if err := requestJSON(ctx, client, http.MethodPost, target.apiBase+"/api/webhook-keys", token, map[string]string{"name": "QA Webhook Fixture Sender"}, &key, http.StatusCreated); err != nil {
		return err
	}
	defer func() {
		if response, err := request(context.Background(), client, http.MethodDelete, target.apiBase+"/api/webhook-keys/"+url.PathEscape(key.ID), token, "", nil, http.StatusNoContent); err == nil {
			_ = response.Close()
		}
	}()
	var spawned struct {
		TaskID string `json:"task_id"`
	}
	if err := requestJSON(ctx, client, http.MethodPost, target.apiBase+"/api/webhook", key.Key, map[string]string{"message": fixtureWebhookInput}, &spawned, http.StatusAccepted); err != nil {
		return err
	}
	return waitForTaskStatus(ctx, client, base+"/tasks/"+url.PathEscape(spawned.TaskID), token, "SUCCEEDED", 2*time.Minute)
}
