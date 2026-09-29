package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/testsupport/mockllm"
)

// mockChat sends one OpenAI-style chat request carrying content and reports
// whether the mock answered it with a tool call.
func mockChat(t *testing.T, base, content string) bool {
	t.Helper()
	body := `{"model":"m","messages":[{"role":"user","content":"` + content + `"}]}`
	resp, err := http.Post(base+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	defer resp.Body.Close()
	var out struct {
		Choices []struct {
			Message struct {
				ToolCalls []any `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode chat reply: %v", err)
	}
	return len(out.Choices) > 0 && len(out.Choices[0].Message.ToolCalls) > 0
}

// The flake this guards: an arm the fixtures meant for their own run was
// consumed by another caller — a schedule firing on the cluster — and the run
// finished without asking. A reserved arm lets that call through untouched
// and answers only the run whose request carries the fixture's own text.
func TestFixtureArmAnswersOnlyItsOwnRun(t *testing.T) {
	server, err := mockllm.Start(mockllm.Scenario{Steps: []mockllm.Step{{Text: "scripted"}}, Repeat: true})
	if err != nil {
		t.Fatalf("start mock: %v", err)
	}
	defer server.Close()
	target := smokeTarget{
		llmControlToolCallURL: server.URL() + mockllm.ControlToolCallPath,
		llmControlRequestsURL: server.URL() + mockllm.ControlRequestsPath,
	}
	ctx := context.Background()
	client := http.DefaultClient

	if _, err := armFixtureToolCall(ctx, client, target, "AskUser", fixtureAskUserArgs("Which?"), ""); err == nil {
		t.Fatal("an unreserved fixture arm was accepted")
	}
	release, err := armFixtureToolCall(ctx, client, target, "AskUser", fixtureAskUserArgs("Which?"), fixtureClarifyInstruction)
	if err != nil {
		t.Fatalf("arm: %v", err)
	}
	defer release()

	if mockChat(t, server.URL(), "[kind fixture] Scheduled run 01 over the synthetic QA workspace.") {
		t.Fatal("a schedule's call consumed the arm reserved for the Clarify run")
	}
	var calls []fxModelCall
	if err := requestJSON(ctx, client, http.MethodGet, target.llmControlRequestsURL, "", nil, &calls, http.StatusOK); err != nil {
		t.Fatalf("read requests: %v", err)
	}
	if modelCalledWith(calls, 0, fixtureClarifyInstruction) {
		t.Fatal("another caller's request was mistaken for the fixture run's")
	}
	before := len(calls)

	if !mockChat(t, server.URL(), "Instruction: "+fixtureClarifyInstruction) {
		t.Fatal("the fixture run's own call was not answered with the armed tool call")
	}
	if err := requestJSON(ctx, client, http.MethodGet, target.llmControlRequestsURL, "", nil, &calls, http.StatusOK); err != nil {
		t.Fatalf("read requests: %v", err)
	}
	if !modelCalledWith(calls, before, fixtureClarifyInstruction) {
		t.Fatal("the fixture run's call was not found in the mock's request log")
	}
	if mockChat(t, server.URL(), "Instruction: "+fixtureClarifyInstruction) {
		t.Fatal("a one-shot arm answered twice")
	}
}
