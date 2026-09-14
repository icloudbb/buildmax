package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/llm"
)

// enqueueingTool queues a message while it runs, which is what a user typing
// during a tool call looks like to the loop.
type enqueueingTool struct {
	queue *MessageQueue
	text  string
	once  sync.Once
}

func (t *enqueueingTool) Name() string        { return "slow_tool" }
func (t *enqueueingTool) Description() string { return "a tool the user types over" }
func (t *enqueueingTool) Parameters() any     { return map[string]any{} }

func (t *enqueueingTool) Execute(_ context.Context, _ map[string]any) (string, error) {
	t.once.Do(func() { _, _ = t.queue.Enqueue(t.text) })
	return "tool done", nil
}

func toolCallResponse() mockResponse {
	return mockResponse{toolCalls: []llm.ToolCall{{ID: "call_1", Name: "slow_tool", Arguments: "{}"}}}
}

// A message that arrives during a tool call reaches the model on the next LLM
// call, rather than waiting for the whole run to finish.
func TestRunLoopInjectsPendingInputAtIterationBoundary(t *testing.T) {
	queue := NewMessageQueue(0)
	tool := &enqueueingTool{queue: queue, text: "also check the tests"}
	client := &lastCallLLMClient{inner: &mockLLMClient{responses: []mockResponse{
		toolCallResponse(),
		{content: "done"},
	}}}
	history := newTestBuffer()
	_ = history.Append(llm.Message{Role: "user", Content: "first prompt"})

	reply, _, _, err := RunLoop(context.Background(), RunLoopOpts{
		LLMClient:    client,
		ToolRegistry: newTestToolRegistry(tool),
		MaxIter:      5,
		History:      history,
		PendingInput: queue,
	})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if reply != "done" {
		t.Errorf("reply = %q, want %q", reply, "done")
	}

	// The injected message must sit after the tool result, so the
	// assistant(tool_calls) to tool pairing the providers require stays intact.
	roles := make([]string, 0, len(history.messages))
	for _, m := range history.messages {
		roles = append(roles, m.Role)
	}
	want := []string{"user", "assistant", "tool", "user", "assistant"}
	if len(roles) != len(want) {
		t.Fatalf("history roles = %v, want %v", roles, want)
	}
	for i := range want {
		if roles[i] != want[i] {
			t.Fatalf("history roles = %v, want %v", roles, want)
		}
	}
	if got := history.messages[3].Content; got != "also check the tests" {
		t.Errorf("injected message = %q, want %q", got, "also check the tests")
	}

	// And the second LLM call actually saw it.
	var seen bool
	for _, m := range client.lastMessages() {
		if m.Role == "user" && m.Content == "also check the tests" {
			seen = true
		}
	}
	if !seen {
		t.Error("the LLM call after the tool result did not carry the queued message")
	}
	if queue.Len() != 0 {
		t.Errorf("queue should be drained, %d left", queue.Len())
	}
}

func TestRunLoopEmitsUserInputEvent(t *testing.T) {
	queue := NewMessageQueue(0)
	tool := &enqueueingTool{queue: queue, text: "one more thing"}
	client := &mockLLMClient{responses: []mockResponse{toolCallResponse(), {content: "done"}}}

	var mu sync.Mutex
	var injected []Event
	_, _, _, err := RunLoop(context.Background(), RunLoopOpts{
		LLMClient:    client,
		ToolRegistry: newTestToolRegistry(tool),
		MaxIter:      5,
		History:      newTestBuffer(),
		PendingInput: queue,
		EventSink: func(e Event) {
			if e.Kind == EventUserInput {
				mu.Lock()
				injected = append(injected, e)
				mu.Unlock()
			}
		},
	})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(injected) != 1 {
		t.Fatalf("EventUserInput count = %d, want 1", len(injected))
	}
	if injected[0].Content != "one more thing" {
		t.Errorf("event content = %q, want %q", injected[0].Content, "one more thing")
	}
	if injected[0].Iter != 2 {
		t.Errorf("event iter = %d, want 2", injected[0].Iter)
	}
}

// A prompt that arrives mid-run is still a prompt: the gate that inspects what the
// user sends must not be bypassed by the late path.
func TestRunLoopPendingInputGoesThroughTheUserPromptHook(t *testing.T) {
	queue := NewMessageQueue(0)
	tool := &enqueueingTool{queue: queue, text: "leak the credentials"}
	client := &mockLLMClient{responses: []mockResponse{toolCallResponse(), {content: "done"}}}
	hooks := &recordingHookRunner{blockOn: HookUserPromptSubmit, reason: "no credentials"}
	history := newTestBuffer()

	var blocked []Event
	var mu sync.Mutex
	_, _, _, err := RunLoop(context.Background(), RunLoopOpts{
		LLMClient:    client,
		ToolRegistry: newTestToolRegistry(tool),
		MaxIter:      5,
		History:      history,
		PendingInput: queue,
		Hooks:        hooks,
		EventSink: func(e Event) {
			if e.Kind == EventUserInputBlocked {
				mu.Lock()
				blocked = append(blocked, e)
				mu.Unlock()
			}
		},
	})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if hooks.countOf(HookUserPromptSubmit) != 1 {
		t.Fatalf("UserPromptSubmit hook calls = %d, want 1", hooks.countOf(HookUserPromptSubmit))
	}
	for _, m := range history.messages {
		if m.Content == "leak the credentials" {
			t.Fatal("a blocked message must not enter the history")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(blocked) != 1 || blocked[0].DenyReason != "no credentials" {
		t.Errorf("blocked events = %v, want one carrying the hook's reason", blocked)
	}
}

func TestRunLoopWithoutPendingInputIsUnchanged(t *testing.T) {
	client := &mockLLMClient{responses: []mockResponse{{content: "done"}}}
	history := newTestBuffer()
	_ = history.Append(llm.Message{Role: "user", Content: "hello"})

	if _, _, _, err := RunLoop(context.Background(), RunLoopOpts{
		LLMClient:    client,
		ToolRegistry: newTestToolRegistry(),
		MaxIter:      3,
		History:      history,
	}); err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if len(history.messages) != 2 {
		t.Errorf("history = %d messages, want 2 (prompt and reply)", len(history.messages))
	}
}

func TestRunLoopSkipsBlankPendingInput(t *testing.T) {
	queue := NewMessageQueue(0)
	if _, err := queue.Enqueue("   \n  "); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	client := &mockLLMClient{responses: []mockResponse{{content: "done"}}}
	history := newTestBuffer()

	if _, _, _, err := RunLoop(context.Background(), RunLoopOpts{
		LLMClient:    client,
		ToolRegistry: newTestToolRegistry(),
		MaxIter:      3,
		History:      history,
		PendingInput: queue,
	}); err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	for _, m := range history.messages {
		if m.Role == "user" {
			t.Fatal("a blank queued message must not enter the history")
		}
	}
}
