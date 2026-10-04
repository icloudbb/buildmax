package taskrun

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/core/agent"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/testsupport/mockllm"
)

// The unattended AskUser path end to end through the worker runtime: the run
// that asks ends its turn after one model call and reports the questions, both
// as data and in the output every reader shows; the Continue run carries the
// user's plain-text answer, and the model sees the question, the tool result,
// and the answer in order.
func TestDeferredQuestionEndsTheRunAndTheAnswerContinuesIt(t *testing.T) {
	ctx := context.Background()
	var args map[string]any
	if err := json.Unmarshal([]byte(`{"questions":[{"header":"Database","question":"Which database?","options":[{"label":"Postgres"},{"label":"SQLite"}]},{"question":"What is the service called?"}]}`), &args); err != nil {
		t.Fatal(err)
	}
	server, err := mockllm.Start(mockllm.Scenario{Steps: []mockllm.Step{
		{Text: "I read the repo; two decisions are yours.", ToolCalls: []mockllm.ToolCall{{ID: "call_q", Name: agent.ToolNameAskUser, Args: args}}},
		{Text: "Using Postgres for orders-api."},
	}})
	if err != nil {
		t.Fatalf("start mock model: %v", err)
	}
	t.Cleanup(server.Close)
	model := config.ModelEntry{
		Model: "mock-model", Name: "mock", APIURL: server.BaseURL(mockllm.ProtocolOpenAIChat),
		APIKey: "mock-key", ContextWindow: 128000,
	}
	persist := newFakePersistStorage()
	sessionID := "sid-questions"
	task := &coretask.Task{ID: "task1", SpaceID: "tm1", SessionID: &sessionID}

	firstRun := &coretask.Run{ID: "run1", Input: "scaffold a small service"}
	firstDirs := testRunDirs(t)
	first, err := runAgentTask(ctx, firstRun, firstDirs.runDir, firstDirs.runGlobal, firstDirs.runOSHome,
		sessionID, nil, model, "", ManagedInference{}, nil, "", "", nil, nil, nil, "", "", nil, nil, true)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if calls := len(server.Requests()); calls != 1 {
		t.Fatalf("model calls = %d, want 1: asking must end the turn", calls)
	}
	if first.questions == nil {
		t.Fatal("the run reported no questions")
	}
	var reported []agent.Question
	if err := json.Unmarshal([]byte(*first.questions), &reported); err != nil || len(reported) != 2 || reported[0].Options[0].Label != "Postgres" {
		t.Fatalf("reported questions = %s (%v)", *first.questions, err)
	}
	output := string(first.output)
	for _, want := range []string{"two decisions are yours", "Waiting for your answer", "1. Which database? (Postgres or SQLite)", "2. What is the service called?", "Reply to this task"} {
		if !strings.Contains(output, want) {
			t.Errorf("output is missing %q:\n%s", want, output)
		}
	}
	if _, err := uploadTaskGlobal(ctx, firstDirs.runGlobal, RunScope{SpaceID: task.SpaceID, TaskID: task.ID, TaskRunID: firstRun.ID}, persist, "", nil); err != nil {
		t.Fatal(err)
	}

	secondRun := &coretask.Run{ID: "run2", PreviousTaskRunID: &firstRun.ID, Input: "Postgres, and call it orders-api"}
	secondDirs := testRunDirs(t)
	restoreSessionFromPreviousRun(ctx, task, secondRun, secondDirs.runGlobal, persist)
	second, err := runAgentTask(ctx, secondRun, secondDirs.runDir, secondDirs.runGlobal, secondDirs.runOSHome,
		sessionID, nil, model, "", ManagedInference{}, nil, "", "", nil, nil, nil, "", "", nil, nil, true)
	if err != nil {
		t.Fatalf("continued run: %v", err)
	}
	if second.questions != nil || string(second.output) != "Using Postgres for orders-api." {
		t.Fatalf("continued run output = %q, questions = %v", second.output, second.questions)
	}
	calls := server.Requests()
	body := string(calls[len(calls)-1].Body)
	iQuestion := strings.Index(body, "Which database?")
	iResult := strings.Index(body, "The questions were sent to the user")
	iAnswer := strings.Index(body, "Postgres, and call it orders-api")
	if iQuestion < 0 || iResult < iQuestion || iAnswer < iResult {
		t.Fatalf("the continued request does not carry question, result, answer in order:\n%s", body)
	}
}

// Off unless the server allows it: without the flag the run has no AskUser
// tool to call at all.
func TestAskUserIsOffUnlessTheServerAllowsIt(t *testing.T) {
	ctx := context.Background()
	server, err := mockllm.Start(mockllm.Scenario{Steps: []mockllm.Step{{Text: "done"}}})
	if err != nil {
		t.Fatalf("start mock model: %v", err)
	}
	t.Cleanup(server.Close)
	model := config.ModelEntry{
		Model: "mock-model", Name: "mock", APIURL: server.BaseURL(mockllm.ProtocolOpenAIChat),
		APIKey: "mock-key", ContextWindow: 128000,
	}
	dirs := testRunDirs(t)
	if _, err := runAgentTask(ctx, &coretask.Run{ID: "run1", Input: "go"}, dirs.runDir, dirs.runGlobal, dirs.runOSHome,
		"sid-off", nil, model, "", ManagedInference{}, nil, "", "", nil, nil, nil, "", "", nil, nil, false); err != nil {
		t.Fatalf("run: %v", err)
	}
	if body := string(server.Requests()[0].Body); strings.Contains(body, `"`+agent.ToolNameAskUser+`"`) {
		t.Fatal("a run the server did not allow was offered AskUser")
	}
}
