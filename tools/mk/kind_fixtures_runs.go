package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Execution fixtures must never silently spend a contributor's provider quota.
// Accept only the reference mock config and no model-selection overrides. This
// deliberately refuses customized clusters instead of changing their settings.
func requireFixtureMock() error {
	actual, err := captureKindKubectl("get", "configmap", "buildmax-config", "-n", "buildmax", "-o", "jsonpath={.data.server\\.yaml}")
	if err != nil {
		return err
	}
	path, cleanup, err := renderKindSmokeConfig("deployment/smoke/server.kind.yaml")
	if err != nil {
		return err
	}
	defer cleanup()
	expected, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if strings.TrimSpace(actual) != strings.TrimSpace(string(expected)) {
		return fmt.Errorf("execution fixtures require the reference mock config; run %s kind up or omit --runs", mk())
	}
	raw, err := captureKindKubectl("get", "deployment", "buildmax-server", "-n", "buildmax", "-o", "json")
	if err != nil {
		return err
	}
	if err := validateFixtureModelEnv(raw); err != nil {
		return err
	}
	// Wait for any already-requested model switch to finish; do not execute on
	// replicas still carrying the previous model selection during a rollout.
	return kindKubectl("rollout", "status", "deployment/buildmax-server", "-n", "buildmax", "--timeout=60s")
}

func validateFixtureModelEnv(raw string) error {
	var deployment struct {
		Spec struct {
			Template struct {
				Spec struct {
					Containers []struct {
						Env []struct {
							Name string `json:"name"`
						} `json:"env"`
						EnvFrom []json.RawMessage `json:"envFrom"`
					} `json:"containers"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
	}
	if err := json.Unmarshal([]byte(raw), &deployment); err != nil {
		return err
	}
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if len(container.EnvFrom) > 0 {
			return fmt.Errorf("execution fixtures cannot verify envFrom model overrides; omit --runs")
		}
		for _, env := range container.Env {
			if env.Name == "BUILDMAX_CONVERSATION_MODEL_API_KEY" {
				continue
			}
			if strings.Contains(env.Name, "MODEL") || strings.Contains(env.Name, "LLM") {
				return fmt.Errorf("execution fixtures refuse model override %s; run %s kind mock or omit --runs", env.Name, mk())
			}
		}
	}
	return nil
}

type fxTask struct {
	ID     string `json:"id"`
	Input  string `json:"input"`
	Status string `json:"status"`
}

func seedFixtureRuns(ctx context.Context, client *http.Client, target smokeTarget, base, token string) error {
	// Match the first user message, not the generated conversation title: titles
	// are mutable and model generated, so they cannot identify fixture ownership.
	const message = "[kind fixture] Plan a QA release using the synthetic workspace files."
	conversations, err := fixturePage[struct {
		ID string `json:"id"`
	}](ctx, client, base+"/conversations", token, "conversations")
	if err != nil {
		return err
	}
	conversationID := ""
	for _, conversation := range conversations {
		var messages struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := requestJSON(ctx, client, http.MethodGet, base+"/conversations/"+conversation.ID+"/messages", token, nil, &messages, http.StatusOK); err != nil {
			return err
		}
		if len(messages.Messages) > 0 && messages.Messages[0].Role == "user" && messages.Messages[0].Content == message {
			conversationID = conversation.ID
			break
		}
	}
	if conversationID == "" {
		var created struct {
			ID string `json:"conversation_id"`
		}
		if err := requestJSON(ctx, client, http.MethodPost, base+"/conversations", token, map[string]string{"channel": "portal", "message": message}, &created, http.StatusCreated); err != nil {
			return err
		}
		conversationID = created.ID
	}
	taskBase := base + "/conversations/" + conversationID + "/tasks"
	var tasks []fxTask
	if err := requestJSON(ctx, client, http.MethodGet, taskBase, token, nil, &tasks, http.StatusOK); err != nil {
		return err
	}
	const input = "[kind fixture] Summarize the QA release brief in three bullets."
	var task fxTask
	for _, t := range tasks {
		if t.Input == input {
			task = t
			break
		}
	}
	if task.ID == "" {
		if err := requestJSON(ctx, client, http.MethodPost, taskBase, token, map[string]string{"input": input}, &task, http.StatusCreated); err != nil {
			return err
		}
	}
	taskURL := base + "/tasks/" + task.ID
	if err := waitForTaskStatus(ctx, client, taskURL, token, "SUCCEEDED", 2*time.Minute); err != nil {
		return err
	}
	// Continue's native idempotency key covers a lost POST response too.
	var runs struct {
		Runs []struct {
			Input   string `json:"input"`
			RetryOf string `json:"retry_of_task_run_id"`
		} `json:"runs"`
	}
	if err := requestJSON(ctx, client, http.MethodGet, taskURL+"/runs", token, nil, &runs, http.StatusOK); err != nil {
		return err
	}
	const followup = "[kind fixture] Add acceptance criteria to the QA summary."
	continued := false
	for _, run := range runs.Runs {
		if run.Input == followup {
			continued = true
		}
	}
	if !continued {
		if err := requestJSON(ctx, client, http.MethodPost, taskURL+"/runs", token, map[string]string{"input": followup, "idempotency_key": "kind-fixture-qa-continue"}, nil, http.StatusCreated); err != nil {
			return err
		}
		if err := waitForTaskStatus(ctx, client, taskURL, token, "SUCCEEDED", 2*time.Minute); err != nil {
			return err
		}
	}
	retried := false
	for _, run := range runs.Runs {
		if run.RetryOf != "" {
			retried = true
		}
	}
	if !retried {
		if err := requestJSON(ctx, client, http.MethodPost, taskURL+"/retry", token, nil, nil, http.StatusCreated); err != nil {
			return err
		}
		if err := waitForTaskStatus(ctx, client, taskURL, token, "SUCCEEDED", 2*time.Minute); err != nil {
			return err
		}
	}
	if err := seedFixtureIssueRuns(ctx, client, base, token); err != nil {
		return err
	}
	if err := seedFixtureOutcomes(ctx, client, target, base, token, taskBase); err != nil {
		return err
	}
	if err := seedFixtureWebhookConversation(ctx, client, target); err != nil {
		return err
	}
	if err := seedFixturePolicyRuns(ctx, client, target, base, token); err != nil {
		return err
	}
	// Last, because its armed Bash call must answer the worker's first turn and
	// nothing else.
	writer, err := fixtureAgentByName(ctx, client, base, token, "QA Writer")
	if err != nil {
		return err
	}
	if err := seedFixtureStalledTask(ctx, client, target, base, writer); err != nil {
		return err
	}
	fmt.Printf("    execution: conversation %s, Task %s with Continue/Retry, traces and workspace checkpoints; Workflow graph run, failed and canceled outcomes, webhook conversation; human requests, AskUser questions, retry, timeout, deadline, and a RUNNING Task\n", conversationID, task.ID)
	return nil
}

func seedFixtureIssueRuns(ctx context.Context, client *http.Client, base, token string) error {
	// Named fixture agents may have been edited since seeding. Verify every
	// target still inherits the free deployment model before dispatching it.
	var agents []struct {
		ID    string `json:"id"`
		Model string `json:"model"`
	}
	if err := requestJSON(ctx, client, http.MethodGet, base+"/agents", token, nil, &agents, http.StatusOK); err != nil {
		return err
	}
	for _, agent := range agents {
		if agent.Model != "" {
			return fmt.Errorf("execution fixtures require QA-space agents to inherit the mock model")
		}
	}
	issues, err := fixturePage[fxIssue](ctx, client, base+"/issues", token, "issues")
	if err != nil {
		return err
	}
	for _, issue := range issues {
		if issue.Title != "Draft QA release notes" && issue.Title != "Run QA review workflow" {
			continue
		}
		issueURL := base + "/issues/" + issue.ID
		type run struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		var flow struct {
			Tasks []fxTask `json:"agent_tasks"`
			Runs  []struct {
				Run run `json:"run"`
			} `json:"runs"`
		}
		if err := requestJSON(ctx, client, http.MethodGet, issueURL+"/flow", token, nil, &flow, http.StatusOK); err != nil {
			return err
		}
		switch issue.ExecutorKind {
		case "agent":
			var task fxTask
			if len(flow.Tasks) > 0 {
				task = flow.Tasks[0]
			} else {
				if err := requestJSON(ctx, client, http.MethodPost, issueURL+"/agent-runs", token, map[string]string{"input": "[kind fixture] Draft release notes from the synthetic QA brief."}, &task, http.StatusCreated); err != nil {
					return err
				}
			}
			if err := waitForTaskStatus(ctx, client, base+"/tasks/"+task.ID, token, "SUCCEEDED", 2*time.Minute); err != nil {
				return err
			}
		case "workflow":
			var current struct {
				Run run `json:"run"`
			}
			if len(flow.Runs) > 0 {
				current.Run = flow.Runs[0].Run
			} else {
				if err := requestJSON(ctx, client, http.MethodPost, issueURL+"/workflow-runs", token, nil, &current, http.StatusCreated); err != nil {
					return err
				}
			}
			if err := waitForWorkflowRun(ctx, client, base, token, current.Run.ID, "succeeded", 3*time.Minute); err != nil {
				return err
			}
		}
	}
	return nil
}
