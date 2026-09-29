package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Workflow fixtures for durable human requests and retry/timeout policy, named
// so --runs finds the definitions the data fixtures made.
const (
	fixtureSignoffWorkflow  = "QA Release Sign-off"
	fixtureClarifyWorkflow  = "QA Clarify Scope"
	fixtureExpiringWorkflow = "QA Expiring Approval"
	fixtureDeadlineWorkflow = "QA Run Deadline"
	fixtureFlakyWorkflow    = "QA Flaky Gate"
	fixtureSlowWorkflow     = "QA Slow Step"

	// Publication refuses a timeout below a minute, so the fixtures that end on
	// a timeout take at least that long.
	fixtureShortTimeout = 60
	// A week keeps the pending sign-off open for manual testing.
	fixtureLongTimeout = 7 * 24 * 60 * 60

	fixtureTaskQuestionInput = "[kind fixture] Ask which checklist to use before summarizing."
	fixtureTaskAnsweredInput = "[kind fixture] Ask which archive to read, then summarize it."
	fixtureTaskAnswer        = "Use the 2026 release checklist."
)

// fixtureApprovalSchema is what a person's sign-off must satisfy.
var fixtureApprovalSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"approved": map[string]string{"type": "boolean"},
		"note":     map[string]string{"type": "string", "description": "What the publisher should know."},
	},
	"required":             []string{"approved", "note"},
	"additionalProperties": false,
}

func fixtureHumanNode(id, instruction string, needs []string, bindings []map[string]string, schema map[string]any, timeout int) fxNode {
	node := fxNode{"id": id, "type": "human_input", "input": map[string]any{"instruction": instruction}}
	if len(needs) > 0 {
		node["needs"] = needs
	}
	if len(bindings) > 0 {
		node["input"].(map[string]any)["bindings"] = bindings
	}
	if schema != nil {
		node["output_schema"] = schema
	}
	if timeout > 0 {
		node["policy"] = map[string]int{"timeout_seconds": timeout}
	}
	return node
}

// ensureFixturePolicyWorkflows seeds one published Workflow per human-request
// and policy outcome, so each run's status alone identifies its fixture:
// sign-off (answered, declined, pending, canceled), an Agent step that asks, a
// request that expires, a run deadline, a retried gate whose failure drains a
// sibling, and a step that times out.
func ensureFixturePolicyWorkflows(ctx context.Context, client *http.Client, base, token, writer, reviewer, blocked string) error {
	signoff := map[string]any{
		"schema_version": 1,
		"nodes": []fxNode{
			fixtureNode("draft", writer, "Draft the release notes from fixtures/docs/brief.md.", nil, nil, ""),
			fixtureHumanNode("approve", "Approve the draft release notes, or decline them with a reason.", []string{"draft"},
				[]map[string]string{fixtureBinding("draft", "node.draft.output", "/text")}, fixtureApprovalSchema, fixtureLongTimeout),
			fixtureNode("publish", writer, "Publish the approved notes, applying the approver's note.", []string{"approve"},
				[]map[string]string{fixtureBinding("note", "node.approve.output", "/structured/note")}, ""),
		},
		"result": map[string]string{"source": "node.publish.output", "pointer": "/text"},
	}
	clarify := map[string]any{
		"schema_version": 1,
		"nodes":          []fxNode{fixtureNode("scope", writer, "Ask which area to check if the brief does not say, then check it.", nil, nil, "")},
	}
	expiring := map[string]any{
		"schema_version": 1,
		"nodes":          []fxNode{fixtureHumanNode("confirm", "Confirm the release window within a minute.", nil, nil, nil, fixtureShortTimeout)},
	}
	deadline := map[string]any{
		"schema_version": 1,
		"policy":         map[string]int{"timeout_seconds": fixtureShortTimeout},
		"nodes":          []fxNode{fixtureHumanNode("confirm", "Confirm the release owner. The whole run has one minute.", nil, nil, nil, 0)},
	}
	gate := fixtureNode("gate", blocked, "Report the configured endpoint.", nil, nil, "")
	gate["policy"] = map[string]int{"max_attempts": 2}
	flaky := map[string]any{
		"schema_version": 1,
		"nodes": []fxNode{
			gate,
			fixtureNode("notes", writer, "Draft the release notes.", nil, nil, ""),
			fixtureNode("ship", reviewer, "Ship once the gate and notes both pass.", []string{"gate", "notes"}, nil, ""),
		},
	}
	step := fixtureNode("check", writer, "Run the long installation check.", nil, nil, "")
	step["policy"] = map[string]int{"timeout_seconds": fixtureShortTimeout}
	slow := map[string]any{"schema_version": 1, "nodes": []fxNode{step}}

	for _, spec := range []struct {
		name, description string
		definition        any
	}{
		{fixtureSignoffWorkflow, "Synthetic draft, human approval with a response schema, and publish.", signoff},
		{fixtureClarifyWorkflow, "Synthetic Agent step that stops to ask a question.", clarify},
		{fixtureExpiringWorkflow, "Synthetic human request that expires after a minute.", expiring},
		{fixtureDeadlineWorkflow, "Synthetic run with a one-minute run deadline.", deadline},
		{fixtureFlakyWorkflow, "Synthetic retried gate that fails and drains its sibling.", flaky},
		{fixtureSlowWorkflow, "Synthetic step with a one-minute attempt timeout.", slow},
	} {
		if _, err := ensureNamedWorkflow(ctx, client, base, token, spec.name, spec.description, spec.definition, "published"); err != nil {
			return err
		}
	}
	return nil
}

func fixtureAgentByName(ctx context.Context, client *http.Client, base, token, name string) (string, error) {
	var agents []fxAgent
	if err := requestJSON(ctx, client, http.MethodGet, base+"/agents", token, nil, &agents, http.StatusOK); err != nil {
		return "", err
	}
	for _, a := range agents {
		if a.Name == name {
			return a.ID, nil
		}
	}
	return "", fmt.Errorf("fixture agent %q is missing", name)
}

type fxRequest struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	NodeID string `json:"node_id"`
}

type fxRunView struct {
	Run struct {
		ID           string  `json:"id"`
		Status       string  `json:"status"`
		ErrorMessage *string `json:"error_message"`
	} `json:"run"`
	Steps []struct {
		NodeID  string `json:"node_id"`
		Status  string `json:"status"`
		Attempt int    `json:"attempt"`
	} `json:"steps"`
	Requests []fxRequest `json:"requests"`
}

// fixtureWorkflowIDs maps the space's Workflow names to IDs.
func fixtureWorkflowIDs(ctx context.Context, client *http.Client, base, token string) (map[string]string, error) {
	var list fxWorkflowList
	if err := requestJSON(ctx, client, http.MethodGet, base+"/workflows", token, nil, &list, http.StatusOK); err != nil {
		return nil, err
	}
	ids := make(map[string]string, len(list.Workflows))
	for _, w := range list.Workflows {
		ids[w.Name] = w.ID
	}
	return ids, nil
}

// fixtureRunStatuses counts a Workflow's runs by status. "running" includes a
// run left waiting on a person, which is the point of some fixtures.
func fixtureRunStatuses(ctx context.Context, client *http.Client, base, token, workflowID string) (map[string]int, error) {
	var runs struct {
		Runs []fxWorkflowRun `json:"runs"`
	}
	if err := requestJSON(ctx, client, http.MethodGet, base+"/workflows/"+url.PathEscape(workflowID)+"/runs?limit=100", token, nil, &runs, http.StatusOK); err != nil {
		return nil, err
	}
	seen := map[string]int{}
	for _, r := range runs.Runs {
		seen[r.Status]++
	}
	return seen, nil
}

func startFixtureWorkflowRun(ctx context.Context, client *http.Client, base, token, workflowID string) (string, error) {
	var created fxWorkflowRunDetail
	if err := requestJSON(ctx, client, http.MethodPost, base+"/workflows/"+url.PathEscape(workflowID)+"/runs", token, map[string]any{}, &created, http.StatusCreated); err != nil {
		return "", err
	}
	return created.Run.ID, nil
}

// waitForFixtureRequest polls a run until it waits on a pending request of kind.
func waitForFixtureRequest(ctx context.Context, client *http.Client, base, token, runID, kind string, timeout time.Duration) (fxRequest, error) {
	deadline := time.Now().Add(timeout)
	for {
		var view fxRunView
		if err := requestJSON(ctx, client, http.MethodGet, base+"/workflow-runs/"+url.PathEscape(runID), token, nil, &view, http.StatusOK); err != nil {
			return fxRequest{}, err
		}
		for _, r := range view.Requests {
			if r.Kind == kind && r.Status == "pending" {
				return r, nil
			}
		}
		switch view.Run.Status {
		case "succeeded", "failed", "canceled":
			return fxRequest{}, fmt.Errorf("fixture workflow run %s ended %s before opening a %s request: %s", runID, view.Run.Status, kind, stringValue(view.Run.ErrorMessage))
		}
		if time.Now().After(deadline) {
			return fxRequest{}, fmt.Errorf("fixture workflow run %s opened no %s request within %s (status %s)", runID, kind, timeout, view.Run.Status)
		}
		select {
		case <-ctx.Done():
			return fxRequest{}, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func respondFixtureRequest(ctx context.Context, client *http.Client, base, token, requestID string, body map[string]any) error {
	return requestJSON(ctx, client, http.MethodPost, base+"/workflow-requests/"+url.PathEscape(requestID)+"/respond", token, body, nil, http.StatusOK)
}

// armFixtureToolCall makes the mock answer its next model calls with a tool
// call. The returned release clears whatever the run did not consume, so an
// arm cannot leak into later fixtures or a person's own testing.
func armFixtureToolCall(ctx context.Context, client *http.Client, target smokeTarget, name string, args map[string]any, times int) (func(), error) {
	if target.llmControlToolCallURL == "" {
		return nil, fmt.Errorf("this stack does not publish its mock model's tool-call route")
	}
	if err := requestJSON(ctx, client, http.MethodPost, target.llmControlToolCallURL, "", map[string]any{"name": name, "args": args, "times": times}, nil, http.StatusOK); err != nil {
		return nil, fmt.Errorf("arm a %s tool call: %w", name, err)
	}
	return func() {
		_ = requestJSON(context.Background(), client, http.MethodPost, target.llmControlToolCallURL, "", map[string]any{"clear": true}, nil, http.StatusOK)
	}, nil
}

func fixtureAskUserArgs(question string) map[string]any {
	return map[string]any{"questions": []any{map[string]any{"question": question}}}
}

// seedFixturePolicyRuns produces the human-request and policy outcomes. Runs
// that only wait on the clock start first and are collected last. Every step
// that arms the mock runs alone, because an armed tool call answers whichever
// model call comes next.
func seedFixturePolicyRuns(ctx context.Context, client *http.Client, target smokeTarget, base, token string) error {
	ids, err := fixtureWorkflowIDs(ctx, client, base, token)
	if err != nil {
		return err
	}
	for _, name := range []string{fixtureSignoffWorkflow, fixtureClarifyWorkflow, fixtureExpiringWorkflow, fixtureDeadlineWorkflow, fixtureFlakyWorkflow, fixtureSlowWorkflow} {
		if ids[name] == "" {
			return fmt.Errorf("policy run fixtures need the %q Workflow", name)
		}
	}

	var clockRuns []string
	for _, name := range []string{fixtureExpiringWorkflow, fixtureDeadlineWorkflow} {
		seen, err := fixtureRunStatuses(ctx, client, base, token, ids[name])
		if err != nil {
			return err
		}
		if seen["failed"] > 0 {
			continue
		}
		runID, err := startFixtureWorkflowRun(ctx, client, base, token, ids[name])
		if err != nil {
			return fmt.Errorf("start %q: %w", name, err)
		}
		clockRuns = append(clockRuns, runID)
	}

	if err := seedFixtureClarifyRuns(ctx, client, target, base, token, ids[fixtureClarifyWorkflow]); err != nil {
		return err
	}
	if err := seedFixtureTaskQuestions(ctx, client, target, base, token); err != nil {
		return err
	}
	if err := seedFixtureSignoffRuns(ctx, client, base, token, ids[fixtureSignoffWorkflow]); err != nil {
		return err
	}
	if err := seedFixtureFlakyRun(ctx, client, base, token, ids[fixtureFlakyWorkflow]); err != nil {
		return err
	}
	if err := seedFixtureSlowRun(ctx, client, target, base, token, ids[fixtureSlowWorkflow]); err != nil {
		return err
	}

	// An expiry or deadline is noticed by the reconciler after it passes, so
	// allow a few passes beyond the minute.
	for _, runID := range clockRuns {
		if err := waitForWorkflowRun(ctx, client, base, token, runID, "failed", 4*time.Minute); err != nil {
			return err
		}
	}
	return nil
}

// seedFixtureClarifyRuns leaves one Agent question answered and one pending.
func seedFixtureClarifyRuns(ctx context.Context, client *http.Client, target smokeTarget, base, token, workflowID string) error {
	seen, err := fixtureRunStatuses(ctx, client, base, token, workflowID)
	if err != nil {
		return err
	}
	for _, answer := range []bool{true, false} {
		if answer && seen["succeeded"] > 0 || !answer && seen["running"] > 0 {
			continue
		}
		runID, err := startFixtureWorkflowRun(ctx, client, base, token, workflowID)
		if err != nil {
			return err
		}
		// Armed after the start, whose Task title was its one synchronous model
		// call; the next call is the worker's first turn, which then asks.
		release, err := armFixtureToolCall(ctx, client, target, "AskUser", fixtureAskUserArgs("Which area should this check cover: docs, install, or upgrade?"), 1)
		if err != nil {
			return err
		}
		req, err := waitForFixtureRequest(ctx, client, base, token, runID, "question", 3*time.Minute)
		release()
		if err != nil {
			return err
		}
		if !answer {
			continue
		}
		if err := respondFixtureRequest(ctx, client, base, token, req.ID, map[string]any{"action": "answer", "response": "install"}); err != nil {
			return err
		}
		if err := waitForWorkflowRun(ctx, client, base, token, runID, "succeeded", 3*time.Minute); err != nil {
			return err
		}
	}
	return nil
}

// seedFixtureTaskQuestions leaves one direct Task awaiting an answer and one
// answered through Continue, whose successor run carries the answer.
func seedFixtureTaskQuestions(ctx context.Context, client *http.Client, target smokeTarget, base, token string) error {
	writer, err := fixtureAgentByName(ctx, client, base, token, "QA Writer")
	if err != nil {
		return err
	}
	agentTasks := base + "/agents/" + url.PathEscape(writer) + "/tasks"
	var existing struct {
		Tasks []fxTask `json:"tasks"`
	}
	if err := requestJSON(ctx, client, http.MethodGet, agentTasks+"?limit=100", token, nil, &existing, http.StatusOK); err != nil {
		return err
	}
	byInput := map[string]fxTask{}
	for _, t := range existing.Tasks {
		byInput[t.Input] = t
	}
	for _, spec := range []struct {
		input, question string
		answer          bool
	}{
		{fixtureTaskQuestionInput, "Which checklist should the summary follow?", false},
		{fixtureTaskAnsweredInput, "Which archive should I read?", true},
	} {
		task, ok := byInput[spec.input]
		if !ok {
			if err := requestJSON(ctx, client, http.MethodPost, agentTasks, token, map[string]string{"input": spec.input}, &task, http.StatusCreated); err != nil {
				return err
			}
			release, err := armFixtureToolCall(ctx, client, target, "AskUser", fixtureAskUserArgs(spec.question), 1)
			if err != nil {
				return err
			}
			err = waitForTaskAwaiting(ctx, client, base+"/tasks/"+url.PathEscape(task.ID), token, true, 3*time.Minute)
			release()
			if err != nil {
				return err
			}
		}
		if !spec.answer {
			continue
		}
		taskURL := base + "/tasks/" + url.PathEscape(task.ID)
		if err := requestJSON(ctx, client, http.MethodPost, taskURL+"/runs", token, map[string]string{"input": fixtureTaskAnswer, "idempotency_key": "kind-fixture-qa-answer"}, nil, http.StatusCreated); err != nil {
			return err
		}
		if err := waitForTaskAwaiting(ctx, client, taskURL, token, false, 3*time.Minute); err != nil {
			return err
		}
	}
	return nil
}

// waitForTaskAwaiting waits for a SUCCEEDED Task whose awaiting_answer is want.
// A Task that asks still succeeds; the flag is what says it needs an answer.
func waitForTaskAwaiting(ctx context.Context, client *http.Client, taskURL, token string, want bool, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		var current struct {
			Status         string  `json:"status"`
			AwaitingAnswer bool    `json:"awaiting_answer"`
			ErrorMessage   *string `json:"error_message"`
		}
		if err := requestJSON(ctx, client, http.MethodGet, taskURL, token, nil, &current, http.StatusOK); err != nil {
			return err
		}
		switch {
		case current.Status == "SUCCEEDED" && current.AwaitingAnswer == want:
			return nil
		case current.Status == "FAILED" || current.Status == "CANCELED":
			return fmt.Errorf("fixture task ended %s: %s", current.Status, stringValue(current.ErrorMessage))
		case current.Status == "SUCCEEDED" && want:
			return fmt.Errorf("fixture task succeeded without asking; the armed AskUser call reached another model call")
		case time.Now().After(deadline):
			return fmt.Errorf("fixture task did not settle with awaiting_answer=%t within %s (status %s)", want, timeout, current.Status)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// seedFixtureSignoffRuns gives the sign-off Workflow one run per outcome of its
// human request: answered, declined, still pending, and canceled while waiting.
func seedFixtureSignoffRuns(ctx context.Context, client *http.Client, base, token, workflowID string) error {
	seen, err := fixtureRunStatuses(ctx, client, base, token, workflowID)
	if err != nil {
		return err
	}
	for _, outcome := range []string{"succeeded", "failed", "canceled", "running"} {
		if seen[outcome] > 0 {
			continue
		}
		runID, err := startFixtureWorkflowRun(ctx, client, base, token, workflowID)
		if err != nil {
			return err
		}
		req, err := waitForFixtureRequest(ctx, client, base, token, runID, "input", 3*time.Minute)
		if err != nil {
			return err
		}
		switch outcome {
		case "succeeded":
			err = respondFixtureRequest(ctx, client, base, token, req.ID, map[string]any{"action": "answer", "response": map[string]any{"approved": true, "note": "Lead with the installation fix."}})
		case "failed":
			err = respondFixtureRequest(ctx, client, base, token, req.ID, map[string]any{"action": "decline", "reason": "The draft omits the known limitations."})
		case "canceled":
			err = requestJSON(ctx, client, http.MethodPost, base+"/workflow-runs/"+url.PathEscape(runID)+"/cancel", token, nil, nil, http.StatusOK)
		case "running":
			continue
		}
		if err != nil {
			return err
		}
		if err := waitForWorkflowRun(ctx, client, base, token, runID, outcome, 3*time.Minute); err != nil {
			return err
		}
	}
	return nil
}

// seedFixtureFlakyRun fails a gate twice: the blocked Agent's run is refused
// each time, so the node spends its second attempt after the backoff, the run
// drains the sibling that was admitted beside it, and the join stays blocked.
func seedFixtureFlakyRun(ctx context.Context, client *http.Client, base, token, workflowID string) error {
	seen, err := fixtureRunStatuses(ctx, client, base, token, workflowID)
	if err != nil {
		return err
	}
	if seen["failed"] > 0 {
		return nil
	}
	runID, err := startFixtureWorkflowRun(ctx, client, base, token, workflowID)
	if err != nil {
		return err
	}
	// Two refused attempts, the 30-second backoff, and reconciler passes.
	if err := waitForWorkflowRun(ctx, client, base, token, runID, "failed", 5*time.Minute); err != nil {
		return err
	}
	var view fxRunView
	if err := requestJSON(ctx, client, http.MethodGet, base+"/workflow-runs/"+url.PathEscape(runID), token, nil, &view, http.StatusOK); err != nil {
		return err
	}
	for _, step := range view.Steps {
		if step.NodeID == "gate" && step.Attempt != 2 {
			return fmt.Errorf("the flaky gate failed after %d attempts, want 2", step.Attempt)
		}
	}
	return nil
}

// seedFixtureSlowRun times out a step whose Agent is still working: the mock
// answers its first turn with a long Bash call, which the attempt deadline
// cancels.
func seedFixtureSlowRun(ctx context.Context, client *http.Client, target smokeTarget, base, token, workflowID string) error {
	seen, err := fixtureRunStatuses(ctx, client, base, token, workflowID)
	if err != nil {
		return err
	}
	if seen["failed"] > 0 {
		return nil
	}
	runID, err := startFixtureWorkflowRun(ctx, client, base, token, workflowID)
	if err != nil {
		return err
	}
	release, err := armFixtureToolCall(ctx, client, target, "Bash", map[string]any{"command": "sleep 300", "timeout": 310000}, 1)
	if err != nil {
		return err
	}
	defer release()
	return waitForWorkflowRun(ctx, client, base, token, runID, "failed", 4*time.Minute)
}
