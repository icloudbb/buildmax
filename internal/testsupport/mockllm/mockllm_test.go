package mockllm_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/icloudbb/buildmax/internal/infra/llm"
	"github.com/icloudbb/buildmax/internal/testsupport/mockllm"
)

// The suites this harness serves drive the real adapters, so the tests do too:
// a mock validated against a hand-written parser would only prove the mock
// agrees with itself.

func start(t *testing.T, scenario mockllm.Scenario) *mockllm.Server {
	t.Helper()
	server, err := mockllm.Start(scenario)
	if err != nil {
		t.Fatalf("start mockllm: %v", err)
	}
	t.Cleanup(server.Close)
	return server
}

func client(t *testing.T, server *mockllm.Server, protocol string) *llm.Client {
	t.Helper()
	c, err := llm.NewClient(llm.Config{
		Provider: protocol,
		APIKey:   "mock-key",
		BaseURL:  server.BaseURL(protocol),
		Model:    "mock-model",
	})
	if err != nil {
		t.Fatalf("new client for %s: %v", protocol, err)
	}
	return c
}

var protocols = []string{
	mockllm.ProtocolOpenAIChat,
	mockllm.ProtocolOpenAIResponses,
	mockllm.ProtocolAnthropic,
}

func TestEveryProtocolReplaysTheScenario(t *testing.T) {
	scenario := mockllm.Scenario{Steps: []mockllm.Step{
		{
			Text:      "writing it now",
			ToolCalls: []mockllm.ToolCall{{Name: "Write", Args: map[string]any{"file_path": "notes.txt", "content": "hello"}}},
			Usage:     &mockllm.Usage{PromptTokens: 11, CompletionTokens: 7},
		},
		{Text: "done", Usage: &mockllm.Usage{PromptTokens: 13, CompletionTokens: 2}},
	}}

	for _, protocol := range protocols {
		t.Run(protocol, func(t *testing.T) {
			server := start(t, scenario)
			c := client(t, server, protocol)
			history := []cllm.Message{{Role: "user", Content: "write notes.txt"}}
			tools := []cllm.ToolDef{{Name: "Write", Description: "write a file", Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"file_path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}},
				"required":   []string{"file_path", "content"},
			}}}

			first, err := c.ChatCompletionBlocking(context.Background(), cllm.Request{Messages: history, Tools: tools})
			if err != nil {
				t.Fatalf("first call: %v", err)
			}
			if first.Content != "writing it now" {
				t.Fatalf("content = %q, want %q", first.Content, "writing it now")
			}
			if len(first.ToolCalls) != 1 {
				t.Fatalf("tool calls = %d, want 1", len(first.ToolCalls))
			}
			call := first.ToolCalls[0]
			if call.Name != "Write" || call.ID == "" {
				t.Fatalf("tool call = %+v, want a named call with an id", call)
			}
			var args map[string]any
			if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
				t.Fatalf("arguments %q: %v", call.Arguments, err)
			}
			if args["file_path"] != "notes.txt" || args["content"] != "hello" {
				t.Fatalf("arguments = %v, want the scripted ones", args)
			}
			if first.Usage.PromptTokens != 11 || first.Usage.CompletionTokens != 7 {
				t.Fatalf("usage = %+v, want the scripted counts", first.Usage)
			}

			history = append(history,
				cllm.Message{Role: "assistant", Content: first.Content, ToolCalls: first.ToolCalls, ProviderState: first.ProviderState},
				cllm.Message{Role: "tool", ToolCallID: call.ID, Content: "wrote notes.txt"},
			)
			second, err := c.ChatCompletionBlocking(context.Background(), cllm.Request{Messages: history, Tools: tools})
			if err != nil {
				t.Fatalf("second call: %v", err)
			}
			if second.Content != "done" || len(second.ToolCalls) != 0 {
				t.Fatalf("second completion = %+v, want the final text and no calls", second)
			}
			if server.Remaining() != 0 {
				t.Fatalf("remaining steps = %d, want 0", server.Remaining())
			}
			calls := server.Requests()
			if len(calls) != 2 {
				t.Fatalf("recorded calls = %d, want 2", len(calls))
			}
			if calls[0].Protocol != protocol {
				t.Fatalf("recorded protocol = %q, want %q", calls[0].Protocol, protocol)
			}
			if calls[0].Credential != "mock-key" {
				t.Fatalf("recorded credential = %q, want the client's key", calls[0].Credential)
			}
			// The second request has to carry the result the tool produced, or
			// the run under test never closed the loop.
			if !strings.Contains(string(calls[1].Body), "wrote notes.txt") {
				t.Fatalf("second request did not carry the tool result:\n%s", calls[1].Body)
			}
		})
	}
}

func TestOneTurnCarriesEveryScriptedCall(t *testing.T) {
	scenario := mockllm.Scenario{Steps: []mockllm.Step{{
		ToolCalls: []mockllm.ToolCall{
			{Name: "Read", Args: map[string]any{"file_path": "a.txt"}},
			{Name: "Read", Args: map[string]any{"file_path": "b.txt"}},
			{Name: "Read", Args: map[string]any{"file_path": "c.txt"}},
		},
	}}}
	for _, protocol := range protocols {
		t.Run(protocol, func(t *testing.T) {
			server := start(t, scenario)
			completion, err := client(t, server, protocol).ChatCompletionBlocking(
				context.Background(), cllm.Request{Messages: []cllm.Message{{Role: "user", Content: "read them"}}})
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			if len(completion.ToolCalls) != 3 {
				t.Fatalf("tool calls = %d, want 3", len(completion.ToolCalls))
			}
			seen := map[string]bool{}
			for i, call := range completion.ToolCalls {
				if call.ID == "" || seen[call.ID] {
					t.Fatalf("call %d has a missing or repeated id: %q", i, call.ID)
				}
				seen[call.ID] = true
			}
			if !strings.Contains(completion.ToolCalls[0].Arguments, "a.txt") ||
				!strings.Contains(completion.ToolCalls[2].Arguments, "c.txt") {
				t.Fatalf("calls arrived out of order: %+v", completion.ToolCalls)
			}
		})
	}
}

func TestEveryProtocolStreamsTheSameReply(t *testing.T) {
	scenario := mockllm.Scenario{Steps: []mockllm.Step{{
		Text:      "streamed answer",
		ToolCalls: []mockllm.ToolCall{{Name: "Read", Args: map[string]any{"file_path": "a.txt"}}},
		Usage:     &mockllm.Usage{PromptTokens: 5, CompletionTokens: 9},
	}}}
	for _, protocol := range protocols {
		t.Run(protocol, func(t *testing.T) {
			server := start(t, scenario)
			var deltas []string
			completion, err := client(t, server, protocol).ChatCompletionStreaming(
				context.Background(),
				cllm.Request{Messages: []cllm.Message{{Role: "user", Content: "answer"}}},
				func(delta string) { deltas = append(deltas, delta) },
			)
			if err != nil {
				t.Fatalf("streaming call: %v", err)
			}
			// More than one delta: a stream delivered in one piece would pass an
			// assertion on the joined text while proving nothing about deltas.
			if len(deltas) < 2 {
				t.Fatalf("deltas = %v, want the text split across chunks", deltas)
			}
			if strings.Join(deltas, "") != "streamed answer" || completion.Content != "streamed answer" {
				t.Fatalf("streamed content = %q / %q, want %q", strings.Join(deltas, ""), completion.Content, "streamed answer")
			}
			if len(completion.ToolCalls) != 1 || completion.ToolCalls[0].Name != "Read" {
				t.Fatalf("tool calls = %+v, want the scripted Read", completion.ToolCalls)
			}
			if !strings.Contains(completion.ToolCalls[0].Arguments, "a.txt") {
				t.Fatalf("arguments = %q, want the scripted path", completion.ToolCalls[0].Arguments)
			}
			if completion.Usage.PromptTokens != 5 || completion.Usage.CompletionTokens != 9 {
				t.Fatalf("usage = %+v, want the scripted counts", completion.Usage)
			}
			if !server.Requests()[0].Stream {
				t.Fatal("the recorded call should be marked as streaming")
			}
		})
	}
}

// A streamed turn and a blocking one describe the same reply, so a suite that
// switches between them must not have to script it twice.
func TestStreamingAndBlockingAgreeOnTheSameStep(t *testing.T) {
	scenario := mockllm.Scenario{Steps: []mockllm.Step{{
		Text:      "same either way",
		ToolCalls: []mockllm.ToolCall{{Name: "Write", Args: map[string]any{"file_path": "a.txt", "content": "x"}}},
		Usage:     &mockllm.Usage{PromptTokens: 3, CompletionTokens: 4},
	}}}
	for _, protocol := range protocols {
		t.Run(protocol, func(t *testing.T) {
			history := []cllm.Message{{Role: "user", Content: "go"}}
			blocking, err := client(t, start(t, scenario), protocol).ChatCompletionBlocking(context.Background(), cllm.Request{Messages: history})
			if err != nil {
				t.Fatalf("blocking call: %v", err)
			}
			streamed, err := client(t, start(t, scenario), protocol).ChatCompletionStreaming(context.Background(), cllm.Request{Messages: history}, nil)
			if err != nil {
				t.Fatalf("streaming call: %v", err)
			}
			if blocking.Content != streamed.Content {
				t.Fatalf("content: blocking %q, streaming %q", blocking.Content, streamed.Content)
			}
			if blocking.Usage != streamed.Usage {
				t.Fatalf("usage: blocking %+v, streaming %+v", blocking.Usage, streamed.Usage)
			}
			if len(blocking.ToolCalls) != len(streamed.ToolCalls) {
				t.Fatalf("tool calls: blocking %d, streaming %d", len(blocking.ToolCalls), len(streamed.ToolCalls))
			}
			for i := range blocking.ToolCalls {
				if blocking.ToolCalls[i].Name != streamed.ToolCalls[i].Name ||
					blocking.ToolCalls[i].ID != streamed.ToolCalls[i].ID ||
					blocking.ToolCalls[i].Arguments != streamed.ToolCalls[i].Arguments {
					t.Fatalf("tool call %d: blocking %+v, streaming %+v", i, blocking.ToolCalls[i], streamed.ToolCalls[i])
				}
			}
		})
	}
}

func TestExhaustedScenarioFailsTheCall(t *testing.T) {
	server := start(t, mockllm.Scenario{Steps: []mockllm.Step{{Text: "only reply"}}})
	c := client(t, server, mockllm.ProtocolOpenAIChat)
	history := []cllm.Message{{Role: "user", Content: "hi"}}
	if _, err := c.ChatCompletionBlocking(context.Background(), cllm.Request{Messages: history}); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := c.ChatCompletionBlocking(context.Background(), cllm.Request{Messages: history}); err == nil {
		t.Fatal("a call past the end of the scenario should fail")
	}
}

func TestUnconsumedStepsAreVisible(t *testing.T) {
	server := start(t, mockllm.Scenario{Steps: []mockllm.Step{{Text: "one"}, {Text: "two"}}})
	if _, err := client(t, server, mockllm.ProtocolOpenAIChat).ChatCompletionBlocking(
		context.Background(), cllm.Request{Messages: []cllm.Message{{Role: "user", Content: "hi"}}}); err != nil {
		t.Fatalf("call: %v", err)
	}
	if server.Remaining() != 1 {
		t.Fatalf("remaining steps = %d, want 1", server.Remaining())
	}
}

// Repeat is what lets the deployment smoke share this harness. It has to stay
// opt-in, because everywhere else the call past the end of the script is the
// finding rather than something to answer.
func TestRepeatAnswersEveryCallPastTheEnd(t *testing.T) {
	server := start(t, mockllm.Scenario{Repeat: true, Steps: []mockllm.Step{
		{Text: "first"},
		{Text: "always this"},
	}})
	c := client(t, server, mockllm.ProtocolOpenAIChat)
	history := []cllm.Message{{Role: "user", Content: "hi"}}
	want := []string{"first", "always this", "always this", "always this"}
	for i, expected := range want {
		completion, err := c.ChatCompletionBlocking(context.Background(), cllm.Request{Messages: history})
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if completion.Content != expected {
			t.Fatalf("call %d content = %q, want %q", i, completion.Content, expected)
		}
	}
	if server.Remaining() != 0 {
		t.Fatalf("remaining steps = %d, want 0", server.Remaining())
	}
}

func TestScriptedProviderErrorReachesTheCaller(t *testing.T) {
	server := start(t, mockllm.Scenario{Steps: []mockllm.Step{{Status: 400, Error: "scripted refusal"}}})
	_, err := client(t, server, mockllm.ProtocolOpenAIChat).ChatCompletionBlocking(
		context.Background(), cllm.Request{Messages: []cllm.Message{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("a scripted provider error should fail the call")
	}
	if !strings.Contains(err.Error(), "scripted refusal") {
		t.Fatalf("error = %v, want it to carry the scripted message", err)
	}
}

func TestLoadScenarioReadsACommittedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scenario.json")
	body := `{"name":"write once","steps":[{"tool_calls":[{"name":"Write","args":{"file_path":"a.txt","content":"hi"}}]},{"text":"done"}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	scenario, err := mockllm.LoadScenario(path)
	if err != nil {
		t.Fatalf("load scenario: %v", err)
	}
	if scenario.Name != "write once" || len(scenario.Steps) != 2 {
		t.Fatalf("scenario = %+v, want the two scripted steps", scenario)
	}
	if scenario.Steps[0].ToolCalls[0].Args["file_path"] != "a.txt" {
		t.Fatalf("arguments = %v, want the scripted path", scenario.Steps[0].ToolCalls[0].Args)
	}

	empty := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(empty, []byte(`{"steps":[]}`), 0o600); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	if _, err := mockllm.LoadScenario(empty); err == nil {
		t.Fatal("a scenario with no steps should not load")
	}
}

// TestScriptedDelayHoldsTheReply covers the property the cancellation drill
// rests on: while a step is stalling, the call is still open, so the run that
// made it is still going and can be acted on.
func TestScriptedDelayHoldsTheReply(t *testing.T) {
	const delay = 300 * time.Millisecond
	server := start(t, mockllm.Scenario{Steps: []mockllm.Step{{Text: "slow", DelayMS: int(delay.Milliseconds())}}})

	started := time.Now()
	reply, err := client(t, server, mockllm.ProtocolOpenAIChat).ChatCompletionBlocking(
		context.Background(), cllm.Request{Messages: []cllm.Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("blocking call: %v", err)
	}
	if elapsed := time.Since(started); elapsed < delay {
		t.Errorf("the call returned in %s, before the scripted %s had passed", elapsed, delay)
	}
	if !strings.Contains(reply.Content, "slow") {
		t.Errorf("reply = %q, want the scripted text", reply.Content)
	}
}

// TestStalledReplyEndsWhenTheCallerGivesUp keeps a stall from outliving what it
// was stalling for. A suite cancels a run mid-turn and then tears the stack
// down; a mock still sleeping on that turn would hold the shutdown open.
func TestStalledReplyEndsWhenTheCallerGivesUp(t *testing.T) {
	server := start(t, mockllm.Scenario{Steps: []mockllm.Step{{Text: "never arrives", DelayMS: 60_000}}})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := client(t, server, mockllm.ProtocolOpenAIChat).ChatCompletionBlocking(
		ctx, cllm.Request{Messages: []cllm.Message{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("a call abandoned mid-stall should fail rather than wait out the stall")
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Errorf("the call took %s; the handler waited out its own stall instead of the caller", elapsed)
	}
}

// TestControlStallArmsADeployedMock covers the route a deployment smoke uses.
// It cannot rescript a mock that is already running in a container, so arming
// the stall over HTTP is the only way it can ask for a slow turn.
func TestControlStallArmsADeployedMock(t *testing.T) {
	server := start(t, mockllm.Scenario{Steps: []mockllm.Step{{Text: "ok"}}, Repeat: true})
	call := func() time.Duration {
		t.Helper()
		started := time.Now()
		if _, err := client(t, server, mockllm.ProtocolOpenAIChat).ChatCompletionBlocking(
			context.Background(), cllm.Request{Messages: []cllm.Message{{Role: "user", Content: "hi"}}}); err != nil {
			t.Fatalf("blocking call: %v", err)
		}
		return time.Since(started)
	}
	arm := func(ms int) *http.Response {
		t.Helper()
		body := strings.NewReader(fmt.Sprintf(`{"ms":%d}`, ms))
		resp, err := http.Post(server.URL()+mockllm.ControlStallPath, "application/json", body)
		if err != nil {
			t.Fatalf("arm stall: %v", err)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })
		return resp
	}

	if resp := arm(300); resp.StatusCode != http.StatusOK {
		t.Fatalf("arming the stall returned %d, want 200", resp.StatusCode)
	}
	if elapsed := call(); elapsed < 300*time.Millisecond {
		t.Errorf("the armed call returned in %s, before the armed 300ms had passed", elapsed)
	}

	// Clearing matters as much as arming: a suite that stalls one turn and then
	// waits for the run to settle would otherwise wait out its own stall again.
	if resp := arm(0); resp.StatusCode != http.StatusOK {
		t.Fatalf("clearing the stall returned %d, want 200", resp.StatusCode)
	}
	if elapsed := call(); elapsed > 250*time.Millisecond {
		t.Errorf("the call took %s after the stall was cleared", elapsed)
	}

	if resp := arm(-1); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a negative stall returned %d, want 400", resp.StatusCode)
	}
}

// TestControlToolCallArmsOneReply asserts the armed call answers with the
// named tool and nothing else, and that the very next call — unarmed — falls
// straight through to the mounted scenario, proving arming never perturbed
// its step index.
func TestControlToolCallArmsOneReply(t *testing.T) {
	server := start(t, mockllm.Scenario{Steps: []mockllm.Step{{Text: "scripted reply"}}, Repeat: true})
	armBody := strings.NewReader(`{"name":"Bash","args":{"command":"id"}}`)
	resp, err := http.Post(server.URL()+mockllm.ControlToolCallPath, "application/json", armBody)
	if err != nil {
		t.Fatalf("arm tool call: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("arming returned %d, want 200", resp.StatusCode)
	}

	c := client(t, server, mockllm.ProtocolOpenAIChat)
	armed, err := c.ChatCompletionBlocking(context.Background(),
		cllm.Request{Messages: []cllm.Message{{Role: "user", Content: "run it"}}})
	if err != nil {
		t.Fatalf("armed call: %v", err)
	}
	if len(armed.ToolCalls) != 1 || armed.ToolCalls[0].Name != "Bash" {
		t.Fatalf("armed reply = %+v, want one Bash call", armed.ToolCalls)
	}
	if !strings.Contains(armed.ToolCalls[0].Arguments, "id") {
		t.Errorf("armed call arguments = %q, want the command it was armed with", armed.ToolCalls[0].Arguments)
	}

	next, err := c.ChatCompletionBlocking(context.Background(),
		cllm.Request{Messages: []cllm.Message{{Role: "user", Content: "and then"}}})
	if err != nil {
		t.Fatalf("call after arming: %v", err)
	}
	if next.Content != "scripted reply" || len(next.ToolCalls) != 0 {
		t.Errorf("call after arming = %+v, want the scenario's own scripted reply unchanged", next)
	}
}

// TestControlToolCallTimesQueuesSeveral asserts arming with times covers a
// preliminary call ahead of the one a suite actually cares about, and that a
// still-queued arm never leaks into a later, unrelated call once cleared.
func TestControlToolCallTimesQueuesSeveral(t *testing.T) {
	server := start(t, mockllm.Scenario{Steps: []mockllm.Step{{Text: "scripted reply"}}, Repeat: true})
	arm := func(body string) {
		t.Helper()
		resp, err := http.Post(server.URL()+mockllm.ControlToolCallPath, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("post control/toolcall: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("control/toolcall returned %d, want 200", resp.StatusCode)
		}
	}
	call := func() cllm.Completion {
		t.Helper()
		resp, err := client(t, server, mockllm.ProtocolOpenAIChat).ChatCompletionBlocking(
			context.Background(), cllm.Request{Messages: []cllm.Message{{Role: "user", Content: "hi"}}})
		if err != nil {
			t.Fatalf("call: %v", err)
		}
		return resp
	}

	arm(`{"name":"Bash","args":{"command":"first"},"times":3}`)
	for i := range 3 {
		got := call()
		if len(got.ToolCalls) != 1 || got.ToolCalls[0].Name != "Bash" {
			t.Fatalf("call %d = %+v, want a queued Bash call", i, got.ToolCalls)
		}
	}
	if got := call(); len(got.ToolCalls) != 0 || got.Content != "scripted reply" {
		t.Errorf("call after the queue drained = %+v, want the scenario's own reply", got)
	}

	// Arm more than a run consumes, then clear: nothing should leak forward.
	arm(`{"name":"Bash","args":{},"times":5}`)
	arm(`{"clear":true}`)
	if got := call(); len(got.ToolCalls) != 0 || got.Content != "scripted reply" {
		t.Errorf("call after clearing = %+v, want the scenario's own reply, not a leaked arm", got)
	}
}

// TestControlRequestsReportsEveryCall asserts a suite that can only reach a
// deployed mock over HTTP can still see what it was actually sent, which is
// the only way to check a tool result a scripted final reply never echoes.
func TestControlRequestsReportsEveryCall(t *testing.T) {
	server := start(t, mockllm.Scenario{Steps: []mockllm.Step{{Text: "ok"}}, Repeat: true})
	if _, err := client(t, server, mockllm.ProtocolOpenAIChat).ChatCompletionBlocking(
		context.Background(), cllm.Request{Messages: []cllm.Message{{Role: "user", Content: "marker-content"}}}); err != nil {
		t.Fatalf("call: %v", err)
	}

	resp, err := http.Get(server.URL() + mockllm.ControlRequestsPath)
	if err != nil {
		t.Fatalf("get requests: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("control requests returned %d, want 200", resp.StatusCode)
	}
	var got []mockllm.Request
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode requests: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("recorded requests = %d, want 1", len(got))
	}
	if !strings.Contains(string(got[0].Body), "marker-content") {
		t.Errorf("recorded request body missing the call's own content: %s", got[0].Body)
	}
}
