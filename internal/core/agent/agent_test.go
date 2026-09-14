package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/llm"
)

// testMaxIter is a cap wide enough that no test here reaches it. The real
// default lives in config.ResolveMaxIterations, which this package must not
// import.
const testMaxIter = 200

// TestExecuteTool asserts ExecuteTool parses arguments, calls Execute, and returns result or error string.
func TestExecuteTool(t *testing.T) {
	ctx := context.Background()
	mock := &mockTool{name: "echo", result: "ok"}
	t.Run("success", func(t *testing.T) {
		out := ExecuteTool(ctx, mock, llm.ToolCall{ID: "1", Name: "echo", Arguments: `{"a":1}`})
		if out != "ok" {
			t.Errorf("ExecuteTool = %q, want ok", out)
		}
	})
	t.Run("invalid_json", func(t *testing.T) {
		out := ExecuteTool(ctx, mock, llm.ToolCall{ID: "2", Name: "echo", Arguments: `{invalid`})
		if out == "" || !strings.HasPrefix(out, "error:") {
			t.Errorf("ExecuteTool = %q, want error prefix", out)
		}
	})
	t.Run("empty_arguments", func(t *testing.T) {
		out := ExecuteTool(ctx, mock, llm.ToolCall{ID: "3", Name: "echo", Arguments: ""})
		if out != "ok" {
			t.Errorf("ExecuteTool = %q, want ok", out)
		}
	})
}

type testBuffer struct {
	messages []llm.Message
}

func newTestBuffer() *testBuffer { return &testBuffer{} }

func newTestToolRegistry(tools ...llm.Tool) llm.ToolRegistry {
	registry := llm.NewToolRegistry()
	registry.AppendTools(tools...)
	return registry
}

func (b *testBuffer) HistoryMessages() []llm.Message {
	if len(b.messages) == 0 {
		return nil
	}
	out := make([]llm.Message, len(b.messages))
	copy(out, b.messages)
	return out
}

func (b *testBuffer) Append(msg llm.Message) error {
	b.messages = append(b.messages, msg)
	return nil
}

// mockLLMClient is a fake LLM that returns configured content and tool calls.
type mockLLMClient struct {
	mu        sync.Mutex
	calls     int
	responses []mockResponse // per-call response
	// outputs records req.Output seen on each call, so a test can assert which
	// call carried a structured-output request.
	outputs []*llm.OutputSchema
}

type mockResponse struct {
	content   string
	toolCalls []llm.ToolCall
	usage     llm.Usage
	// structured, when set, is returned on the completion as if the Client had
	// validated it — for exercising the structured-output extraction call.
	structured *llm.Structured
}

func (m *mockLLMClient) ChatCompletionBlocking(ctx context.Context, req llm.Request) (llm.Completion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outputs = append(m.outputs, req.Output)
	if m.calls >= len(m.responses) {
		return llm.Completion{}, nil
	}
	r := m.responses[m.calls]
	m.calls++
	return llm.Completion{Content: r.content, ToolCalls: r.toolCalls, Usage: r.usage, Structured: r.structured}, nil
}

func (m *mockLLMClient) ChatCompletionStreaming(ctx context.Context, req llm.Request, onDelta func(string)) (llm.Completion, error) {
	completion, err := m.ChatCompletionBlocking(ctx, req)
	if err == nil && onDelta != nil && completion.Content != "" {
		onDelta(completion.Content)
	}
	return completion, err
}

func (m *mockLLMClient) ContextWindow() int { return 0 }

// Ensure mockLLMClient implements llm.LLMClient.
var _ llm.LLMClient = (*mockLLMClient)(nil)

func (m *mockLLMClient) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// mockTool is a fake tool that records executions and returns a fixed string.
type mockTool struct {
	name        string
	description string
	params      any
	result      string
	executed    int
	mu          sync.Mutex
}

func (t *mockTool) Name() string        { return t.name }
func (t *mockTool) Description() string { return t.description }
func (t *mockTool) Parameters() any     { return t.params }
func (t *mockTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	t.mu.Lock()
	t.executed++
	t.mu.Unlock()
	return t.result, nil
}

func (t *mockTool) executionCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.executed
}

const testSystemPrompt = "You are a test assistant."

func runLoopWithUserMsg(ctx context.Context, llmClient llm.LLMClient, registry llm.ToolRegistry, history MessageHistory, userMsg string, opts ...func(*RunLoopOpts)) (string, RunStats, error) {
	if err := history.Append(llm.Message{Role: "user", Content: userMsg}); err != nil {
		return "", RunStats{}, err
	}
	o := RunLoopOpts{
		LLMClient:    llmClient,
		SystemPrompt: testSystemPrompt,
		ToolRegistry: registry,
		MaxIter:      testMaxIter,
		History:      history,
	}
	for _, opt := range opts {
		opt(&o)
	}
	reply, stats, _, err := RunLoop(ctx, o)
	return reply, stats, err
}

// TestProcessWithSession_NoToolCall asserts that when the LLM returns final content with no tool_calls,
// RunLoop returns that content and no tool is executed.
func TestProcessWithSession_NoToolCall(t *testing.T) {
	ctx := context.Background()
	mock := &mockLLMClient{
		responses: []mockResponse{
			{content: "Hello, I am the final reply.", toolCalls: nil},
		},
	}
	mockTool := &mockTool{
		name:        "echo",
		description: "Echoes input",
		params:      map[string]any{"type": "object"},
		result:      "echoed",
	}
	sess := newTestBuffer()
	reply, stats, err := runLoopWithUserMsg(ctx, mock, newTestToolRegistry(mockTool), sess, "Hi")
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if reply != "Hello, I am the final reply." {
		t.Errorf("reply = %q, want %q", reply, "Hello, I am the final reply.")
	}
	if stats.ToolCalls != 0 {
		t.Errorf("stats.ToolCalls = %d, want 0", stats.ToolCalls)
	}
	if mockTool.executionCount() != 0 {
		t.Errorf("tool executed %d times, want 0", mockTool.executionCount())
	}
	if mock.callCount() != 1 {
		t.Errorf("LLM called %d times, want 1", mock.callCount())
	}
}

// TestProcessWithSession_WithToolCall asserts that when the LLM returns a tool_call, the agent
// executes the tool, sends results back, and returns the final content from the next LLM call.
func TestProcessWithSession_WithToolCall(t *testing.T) {
	ctx := context.Background()
	toolResult := "the tool result"
	mock := &mockLLMClient{
		responses: []mockResponse{
			{
				content: "",
				toolCalls: []llm.ToolCall{
					{ID: "call-1", Name: "get_weather", Arguments: `{"location":"Boston"}`},
				},
			},
			{content: "The weather in Boston is nice.", toolCalls: nil},
		},
	}
	mockTool := &mockTool{
		name:        "get_weather",
		description: "Get weather for a location",
		params:      map[string]any{"type": "object", "properties": map[string]any{"location": map[string]any{"type": "string"}}},
		result:      toolResult,
	}
	sess := newTestBuffer()
	reply, stats, err := runLoopWithUserMsg(ctx, mock, newTestToolRegistry(mockTool), sess, "What is the weather in Boston?")
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if reply != "The weather in Boston is nice." {
		t.Errorf("reply = %q, want %q", reply, "The weather in Boston is nice.")
	}
	if stats.ToolCalls != 1 {
		t.Errorf("stats.ToolCalls = %d, want 1", stats.ToolCalls)
	}
	if mockTool.executionCount() != 1 {
		t.Errorf("tool executed %d times, want 1", mockTool.executionCount())
	}
	if mock.callCount() != 2 {
		t.Errorf("LLM called %d times, want 2", mock.callCount())
	}
}

// TestRunLoop_JoinsNarrationAcrossAToolCall asserts the reply keeps the text the
// model wrote before a tool call, joined with what it wrote after, not only the
// final answer. The Agent's TaskRun is the one surface that shows a run's detail,
// so its pre-tool narration must survive into the output.
func TestRunLoop_JoinsNarrationAcrossAToolCall(t *testing.T) {
	ctx := context.Background()
	mock := &mockLLMClient{
		responses: []mockResponse{
			{
				content: "I will check the weather.",
				toolCalls: []llm.ToolCall{
					{ID: "call-1", Name: "get_weather", Arguments: `{"location":"Boston"}`},
				},
			},
			{content: "The weather in Boston is nice.", toolCalls: nil},
		},
	}
	mockTool := &mockTool{
		name:        "get_weather",
		description: "Get weather for a location",
		params:      map[string]any{"type": "object", "properties": map[string]any{"location": map[string]any{"type": "string"}}},
		result:      "the tool result",
	}
	reply, _, err := runLoopWithUserMsg(ctx, mock, newTestToolRegistry(mockTool), newTestBuffer(), "What is the weather in Boston?")
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	want := "I will check the weather.\n\nThe weather in Boston is nice."
	if reply != want {
		t.Errorf("reply = %q, want %q", reply, want)
	}
}

// TestProcessWithSession_AccumulatesUsage asserts that RunStats accumulates prompt and completion tokens across LLM calls.
func TestProcessWithSession_AccumulatesUsage(t *testing.T) {
	ctx := context.Background()
	mock := &mockLLMClient{
		responses: []mockResponse{
			{content: "", toolCalls: []llm.ToolCall{{ID: "1", Name: "echo", Arguments: "{}"}}, usage: llm.Usage{PromptTokens: 10, CompletionTokens: 5}},
			{content: "Done.", toolCalls: nil, usage: llm.Usage{PromptTokens: 20, CompletionTokens: 8}},
		},
	}
	mockTool := &mockTool{name: "echo", description: "Echo", params: map[string]any{"type": "object"}, result: "ok"}
	sess := newTestBuffer()
	reply, stats, err := runLoopWithUserMsg(ctx, mock, newTestToolRegistry(mockTool), sess, "Hi")
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if reply != "Done." {
		t.Errorf("reply = %q, want %q", reply, "Done.")
	}
	if stats.ToolCalls != 1 {
		t.Errorf("stats.ToolCalls = %d, want 1", stats.ToolCalls)
	}
	if stats.PromptTokens != 30 {
		t.Errorf("stats.PromptTokens = %d, want 30", stats.PromptTokens)
	}
	if stats.CompletionTokens != 13 {
		t.Errorf("stats.CompletionTokens = %d, want 13", stats.CompletionTokens)
	}
}

// A run's cache counts accumulate the same way its prompt does, and stay a
// breakdown of it. Reporting them as extra input would make a cached run look
// more expensive than the uncached one it replaced, which is the opposite of
// what caching does.
func TestRunStatsAccumulatesCacheUsageAsPartOfThePrompt(t *testing.T) {
	ctx := context.Background()
	mock := &mockLLMClient{
		responses: []mockResponse{
			// First call writes the prefix, second reads it back.
			{content: "", toolCalls: []llm.ToolCall{{ID: "1", Name: "echo", Arguments: "{}"}},
				usage: llm.Usage{PromptTokens: 100, CompletionTokens: 5, CacheWriteTokens: 90}},
			{content: "Done.",
				usage: llm.Usage{PromptTokens: 120, CompletionTokens: 8, CacheReadTokens: 90}},
		},
	}
	mockTool := &mockTool{name: "echo", description: "Echo", params: map[string]any{"type": "object"}, result: "ok"}
	_, stats, err := runLoopWithUserMsg(ctx, mock, newTestToolRegistry(mockTool), newTestBuffer(), "Hi")
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if stats.CacheWriteTokens != 90 {
		t.Errorf("stats.CacheWriteTokens = %d, want 90", stats.CacheWriteTokens)
	}
	if stats.CacheReadTokens != 90 {
		t.Errorf("stats.CacheReadTokens = %d, want 90", stats.CacheReadTokens)
	}
	if stats.PromptTokens != 220 {
		t.Errorf("stats.PromptTokens = %d, want 220", stats.PromptTokens)
	}
	if stats.CacheReadTokens+stats.CacheWriteTokens > stats.PromptTokens {
		t.Error("cached tokens exceed the prompt they are part of")
	}
}

// recordingLLMClient wraps an llm.LLMClient and records the messages from the first ChatCompletionBlocking call.
type recordingLLMClient struct {
	inner    *mockLLMClient
	firstMsg []llm.Message
	once     sync.Once
}

func (r *recordingLLMClient) ChatCompletionBlocking(ctx context.Context, req llm.Request) (llm.Completion, error) {
	r.once.Do(func() {
		r.firstMsg = make([]llm.Message, len(req.Messages))
		copy(r.firstMsg, req.Messages)
	})
	return r.inner.ChatCompletionBlocking(ctx, req)
}

func (r *recordingLLMClient) ChatCompletionStreaming(ctx context.Context, req llm.Request, onDelta func(string)) (llm.Completion, error) {
	return r.inner.ChatCompletionStreaming(ctx, req, onDelta)
}

func (r *recordingLLMClient) ContextWindow() int { return 0 }

// lastCallLLMClient records the messages from the last ChatCompletionBlocking call (for ProcessWithSession tests).
type lastCallLLMClient struct {
	inner   *mockLLMClient
	lastMsg []llm.Message
	lastMu  sync.Mutex
}

func (r *lastCallLLMClient) ChatCompletionBlocking(ctx context.Context, req llm.Request) (llm.Completion, error) {
	r.lastMu.Lock()
	r.lastMsg = make([]llm.Message, len(req.Messages))
	copy(r.lastMsg, req.Messages)
	r.lastMu.Unlock()
	return r.inner.ChatCompletionBlocking(ctx, req)
}

func (r *lastCallLLMClient) ChatCompletionStreaming(ctx context.Context, req llm.Request, onDelta func(string)) (llm.Completion, error) {
	return r.inner.ChatCompletionStreaming(ctx, req, onDelta)
}

func (r *lastCallLLMClient) ContextWindow() int { return 0 }

func (r *lastCallLLMClient) lastMessages() []llm.Message {
	r.lastMu.Lock()
	defer r.lastMu.Unlock()
	return r.lastMsg
}

// TestProcessWithSession_SystemPromptPrepend verifies that the first ChatCompletionBlocking call receives
// a system message with testSystemPrompt first, then the user message.
func TestProcessWithSession_SystemPromptPrepend(t *testing.T) {
	ctx := context.Background()
	userMsg := "Hello, assistant."
	inner := &mockLLMClient{
		responses: []mockResponse{
			{content: "Hi there.", toolCalls: nil},
		},
	}
	rec := &recordingLLMClient{inner: inner}
	sess := newTestBuffer()
	_, _, err := runLoopWithUserMsg(ctx, rec, newTestToolRegistry(), sess, userMsg)
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if len(rec.firstMsg) < 2 {
		t.Fatalf("first call: len(messages) = %d, want at least 2", len(rec.firstMsg))
	}
	if rec.firstMsg[0].Role != "system" {
		t.Errorf("messages[0].Role = %q, want \"system\"", rec.firstMsg[0].Role)
	}
	if rec.firstMsg[0].Content != testSystemPrompt {
		t.Errorf("messages[0].Content = %q, want %q", rec.firstMsg[0].Content, testSystemPrompt)
	}
	if rec.firstMsg[1].Role != "user" {
		t.Errorf("messages[1].Role = %q, want \"user\"", rec.firstMsg[1].Role)
	}
	if rec.firstMsg[1].Content != userMsg {
		t.Errorf("messages[1].Content = %q, want %q", rec.firstMsg[1].Content, userMsg)
	}
}

// TestProcessWithSession_MaxIterationsExceeded asserts that when the LLM keeps returning tool_calls
// without ever returning final content, RunLoop returns an error after max iterations.
func TestProcessWithSession_MaxIterationsExceeded(t *testing.T) {
	ctx := context.Background()
	var responses []mockResponse
	for i := 0; i < 15; i++ {
		responses = append(responses, mockResponse{
			content: "",
			toolCalls: []llm.ToolCall{
				{ID: "call-1", Name: "ping", Arguments: "{}"},
			},
		})
	}
	mock := &mockLLMClient{responses: responses}
	mockTool := &mockTool{
		name:        "ping",
		description: "Ping",
		params:      map[string]any{"type": "object"},
		result:      "pong",
	}
	sess := newTestBuffer()
	if err := sess.Append(llm.Message{Role: "user", Content: "ping"}); err != nil {
		t.Fatal(err)
	}
	_, stats, _, err := RunLoop(ctx, RunLoopOpts{
		LLMClient:    mock,
		SystemPrompt: testSystemPrompt,
		ToolRegistry: newTestToolRegistry(mockTool),
		MaxIter:      3,
		History:      sess,
	})
	if err == nil {
		t.Fatal("RunLoop: expected error for max iterations exceeded")
	}
	if stats.ToolCalls != 3 {
		t.Errorf("stats.ToolCalls = %d, want 3", stats.ToolCalls)
	}
	if mock.callCount() != 3 {
		t.Errorf("LLM called %d times, want 3 (max iter)", mock.callCount())
	}
}

// TestProcessWithSession verifies that two turns with the same session result in session
// history containing both user messages and assistant reply(ies) in order, and that the
// second LLM call receives the full history (system + first user + first assistant + second user).
func TestProcessWithSession(t *testing.T) {
	ctx := context.Background()
	firstReply := "First reply."
	secondReply := "Second reply."
	inner := &mockLLMClient{
		responses: []mockResponse{
			{content: firstReply, toolCalls: nil},
			{content: secondReply, toolCalls: nil},
		},
	}
	rec := &lastCallLLMClient{inner: inner}
	sess := newTestBuffer()

	reply1, _, err := runLoopWithUserMsg(ctx, rec, newTestToolRegistry(), sess, "First message")
	if err != nil {
		t.Fatalf("first RunLoop: %v", err)
	}
	if reply1 != firstReply {
		t.Errorf("first reply = %q, want %q", reply1, firstReply)
	}

	reply2, _, err := runLoopWithUserMsg(ctx, rec, newTestToolRegistry(), sess, "Second message")
	if err != nil {
		t.Fatalf("second RunLoop: %v", err)
	}
	if reply2 != secondReply {
		t.Errorf("second reply = %q, want %q", reply2, secondReply)
	}

	msgs := sess.messages
	// Expect: user1, assistant1, user2, assistant2 (4 messages)
	if len(msgs) != 4 {
		t.Fatalf("session messages length = %d, want 4", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "First message" {
		t.Errorf("msgs[0] = %q %q", msgs[0].Role, msgs[0].Content)
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != firstReply {
		t.Errorf("msgs[1] = %q %q", msgs[1].Role, msgs[1].Content)
	}
	if msgs[2].Role != "user" || msgs[2].Content != "Second message" {
		t.Errorf("msgs[2] = %q %q", msgs[2].Role, msgs[2].Content)
	}
	if msgs[3].Role != "assistant" || msgs[3].Content != secondReply {
		t.Errorf("msgs[3] = %q %q", msgs[3].Role, msgs[3].Content)
	}

	// Second LLM call should receive: system, first user, first assistant, second user (4 messages)
	lastMsg := rec.lastMessages()
	if len(lastMsg) != 4 {
		t.Fatalf("last LLM call messages length = %d, want 4 (system + user1 + assistant1 + user2)", len(lastMsg))
	}
	if lastMsg[0].Role != "system" || lastMsg[0].Content != testSystemPrompt {
		t.Errorf("lastMsg[0]: role=%q content mismatch", lastMsg[0].Role)
	}
	if lastMsg[1].Role != "user" || lastMsg[1].Content != "First message" {
		t.Errorf("lastMsg[1] = %q %q", lastMsg[1].Role, lastMsg[1].Content)
	}
	if lastMsg[2].Role != "assistant" || lastMsg[2].Content != firstReply {
		t.Errorf("lastMsg[2] = %q %q", lastMsg[2].Role, lastMsg[2].Content)
	}
	if lastMsg[3].Role != "user" || lastMsg[3].Content != "Second message" {
		t.Errorf("lastMsg[3] = %q %q", lastMsg[3].Role, lastMsg[3].Content)
	}
}

// recordingStreamSink records each OnDelta call for tests.
var _ llm.StreamSink = (*recordingStreamSink)(nil)

type recordingStreamSink struct {
	deltas []string
	mu     sync.Mutex
}

func (r *recordingStreamSink) OnDelta(delta string) {
	r.mu.Lock()
	r.deltas = append(r.deltas, delta)
	r.mu.Unlock()
}

func (r *recordingStreamSink) getDeltas() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.deltas...)
}

// TestProcess_Streaming asserts that when StreamSink is set, the sink receives content deltas
// and the session ends with the full assistant message.
func TestProcess_Streaming(t *testing.T) {
	ctx := context.Background()
	fullReply := "Hello, I am the streamed reply."
	mock := &mockLLMClient{
		responses: []mockResponse{
			{content: fullReply, toolCalls: nil},
		},
	}
	sink := &recordingStreamSink{}
	sess := newTestBuffer()
	reply, _, err := runLoopWithUserMsg(ctx, mock, newTestToolRegistry(), sess, "Hi", func(o *RunLoopOpts) {
		o.StreamSink = sink
	})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if reply != fullReply {
		t.Errorf("reply = %q, want %q", reply, fullReply)
	}
	deltas := sink.getDeltas()
	if len(deltas) == 0 {
		t.Error("streaming sink received no deltas")
	}
	if got := strings.Join(deltas, ""); got != fullReply {
		t.Errorf("deltas joined = %q, want %q", got, fullReply)
	}
	msgs := sess.messages
	if len(msgs) != 2 {
		t.Fatalf("session has %d messages, want 2", len(msgs))
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != fullReply {
		t.Errorf("session last message = %q %q", msgs[1].Role, msgs[1].Content)
	}
}

// TestRunLoop_CancellationReturnsLastContent verifies that when the run context is cancelled
// mid-loop, RunLoop returns the last assistant content produced (if any) and a nil error.
func TestRunLoop_CancellationReturnsLastContent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// First call returns a tool call + some content. Second call blocks until ctx is done.
	firstContent := "I will check that for you."
	cancellingClient := &cancelOnSecondCallClient{
		first:    mockResponse{content: firstContent, toolCalls: []llm.ToolCall{{ID: "c1", Name: "echo", Arguments: "{}"}}},
		cancelFn: cancel,
	}
	tool := &mockTool{name: "echo", result: "ok"}
	sess := newTestBuffer()
	if err := sess.Append(llm.Message{Role: "user", Content: "do something"}); err != nil {
		t.Fatal(err)
	}
	reply, _, _, err := RunLoop(ctx, RunLoopOpts{
		LLMClient:    cancellingClient,
		SystemPrompt: testSystemPrompt,
		ToolRegistry: newTestToolRegistry(tool),
		MaxIter:      10,
		History:      sess,
	})
	if err != nil {
		t.Fatalf("RunLoop should return nil error on cancellation, got: %v", err)
	}
	if reply != firstContent {
		t.Errorf("reply = %q, want last content %q", reply, firstContent)
	}
}

// cancelOnSecondCallClient returns a configured response for the first call,
// then cancels the context and returns context.Canceled for subsequent calls.
type cancelOnSecondCallClient struct {
	first    mockResponse
	cancelFn context.CancelFunc
	calls    int
	mu       sync.Mutex
}

func (c *cancelOnSecondCallClient) ChatCompletionBlocking(ctx context.Context, req llm.Request) (llm.Completion, error) {
	c.mu.Lock()
	n := c.calls
	c.calls++
	c.mu.Unlock()
	if n == 0 {
		return llm.Completion{Content: c.first.content, ToolCalls: c.first.toolCalls, Usage: c.first.usage}, nil
	}
	c.cancelFn()
	return llm.Completion{}, context.Canceled
}

func (c *cancelOnSecondCallClient) ChatCompletionStreaming(ctx context.Context, req llm.Request, onDelta func(string)) (llm.Completion, error) {
	return c.ChatCompletionBlocking(ctx, req)
}

func (c *cancelOnSecondCallClient) ContextWindow() int { return 0 }

// TestSystemPromptOption verifies that a custom SystemPrompt in RunLoopOpts is sent as the first message.
func TestSystemPromptOption(t *testing.T) {
	ctx := context.Background()
	customPrompt := "You are a specialized sub-agent for code exploration."
	inner := &mockLLMClient{
		responses: []mockResponse{
			{content: "Done.", toolCalls: nil},
		},
	}
	rec := &recordingLLMClient{inner: inner}
	sess := newTestBuffer()
	_, _, err := runLoopWithUserMsg(ctx, rec, newTestToolRegistry(), sess, "explore the code", func(o *RunLoopOpts) {
		o.SystemPrompt = customPrompt
	})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if len(rec.firstMsg) < 2 {
		t.Fatalf("first call: len(messages) = %d, want at least 2", len(rec.firstMsg))
	}
	if rec.firstMsg[0].Role != "system" {
		t.Errorf("messages[0].Role = %q, want \"system\"", rec.firstMsg[0].Role)
	}
	if rec.firstMsg[0].Content != customPrompt {
		t.Errorf("messages[0].Content = %q, want custom prompt", rec.firstMsg[0].Content)
	}
}

// multimodalTool returns content text cannot hold, the way the MCP gateway does.
type multimodalTool struct{ mockTool }

func (m *multimodalTool) ExecuteMultimodal(_ context.Context, _ map[string]any) (llm.ToolResult, error) {
	return llm.ToolResult{
		Text:  "(image: image/png, 3 B)",
		Parts: []llm.ContentPart{{Type: llm.ContentPartImage, MediaType: "image/png", Data: "aGk="}},
	}, nil
}

// A tool that has more than text to say puts it on the history message, while
// everything that reads the result text keeps reading text.
func TestMultimodalToolResultReachesTheHistory(t *testing.T) {
	tool := &multimodalTool{mockTool{name: "screenshot", result: "unused"}}
	client := &mockLLMClient{responses: []mockResponse{
		{toolCalls: []llm.ToolCall{{ID: "call_1", Name: "screenshot", Arguments: "{}"}}},
		{content: "done"},
	}}
	history := newTestBuffer()
	if _, _, _, err := RunLoop(context.Background(), RunLoopOpts{
		LLMClient: client, ToolRegistry: newTestToolRegistry(tool), History: history, MaxIter: 3,
	}); err != nil {
		t.Fatalf("RunLoop: %v", err)
	}

	messages := history.HistoryMessages()
	var toolMsg *llm.Message
	for i := range messages {
		if messages[i].Role == "tool" {
			toolMsg = &messages[i]
		}
	}
	if toolMsg == nil {
		t.Fatal("no tool message was appended")
	}
	if len(toolMsg.Images()) != 1 {
		t.Fatalf("tool message parts = %+v, want one image", toolMsg.Parts)
	}
	if toolMsg.Content != "(image: image/png, 3 B)" {
		t.Errorf("tool message content = %q, want the text describing the image", toolMsg.Content)
	}
}

// A tool that only returns text is unaffected: the optional upgrade exists so
// sixteen text-only tools do not have to carry a field they never set.
func TestTextOnlyToolResultCarriesNoParts(t *testing.T) {
	client := &mockLLMClient{responses: []mockResponse{
		{toolCalls: []llm.ToolCall{{ID: "call_1", Name: "echo", Arguments: "{}"}}},
		{content: "done"},
	}}
	history := newTestBuffer()
	if _, _, _, err := RunLoop(context.Background(), RunLoopOpts{
		LLMClient: client, ToolRegistry: newTestToolRegistry(&mockTool{name: "echo", result: "plain"}),
		History: history, MaxIter: 3,
	}); err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	for _, msg := range history.HistoryMessages() {
		if msg.Role == "tool" && len(msg.Parts) != 0 {
			t.Errorf("tool message = %+v, want no parts", msg)
		}
	}
}

// profileClient records the call profile every request arrived with.
type profileClient struct {
	mu       sync.Mutex
	profiles []llm.CallProfile
	replies  []mockResponse
}

func (c *profileClient) ChatCompletionBlocking(ctx context.Context, req llm.Request) (llm.Completion, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.profiles = append(c.profiles, req.Profile)
	if len(c.profiles) > len(c.replies) {
		return llm.Completion{}, nil
	}
	r := c.replies[len(c.profiles)-1]
	return llm.Completion{Content: r.content, ToolCalls: r.toolCalls, Usage: r.usage}, nil
}

func (c *profileClient) ChatCompletionStreaming(ctx context.Context, req llm.Request, onDelta func(string)) (llm.Completion, error) {
	return c.ChatCompletionBlocking(ctx, req)
}

func (c *profileClient) ContextWindow() int { return 0 }

// Every iteration of the loop is an agent turn, including the ones that only
// carry tool results. The profile decides whether the call asks a provider to
// write a cache entry, and a turn labelled anything else would either pay for a
// write nothing reads or skip the one write the rest of the run reads back.
func TestRunLoopLabelsEveryIterationAnAgentTurn(t *testing.T) {
	client := &profileClient{replies: []mockResponse{
		{toolCalls: []llm.ToolCall{{ID: "1", Name: "echo", Arguments: "{}"}}},
		{content: "Done."},
	}}
	mockTool := &mockTool{name: "echo", description: "Echo", params: map[string]any{"type": "object"}, result: "ok"}
	if _, _, err := runLoopWithUserMsg(context.Background(), client,
		newTestToolRegistry(mockTool), newTestBuffer(), "Hi"); err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if len(client.profiles) != 2 {
		t.Fatalf("saw %d calls, want 2", len(client.profiles))
	}
	for i, got := range client.profiles {
		if got != llm.ProfileAgentTurn {
			t.Errorf("call %d profile = %q, want %q", i+1, got, llm.ProfileAgentTurn)
		}
	}
}

func testRunPricing() llm.Pricing {
	return llm.Pricing{
		Currency: "USD", InputPerMTok: 3_000_000_000, CacheReadPerMTok: 300_000_000,
		CacheWritePerMTok: 3_750_000_000, OutputPerMTok: 15_000_000_000,
	}
}

// A run prices each call as it completes, at the rates in force then. Pricing
// the run's totals afterwards would give a different answer the moment a run
// spanned a rate change, and the per-call figure is what says which turn was
// expensive.
func TestRunStatsPricesEachCall(t *testing.T) {
	mock := &mockLLMClient{responses: []mockResponse{
		// A turn that writes the cache, then one that reads it back.
		{toolCalls: []llm.ToolCall{{ID: "1", Name: "echo", Arguments: "{}"}},
			usage: llm.Usage{PromptTokens: 100_000, CompletionTokens: 1_000, CacheWriteTokens: 90_000}},
		{content: "Done.",
			usage: llm.Usage{PromptTokens: 100_000, CompletionTokens: 1_000, CacheReadTokens: 90_000}},
	}}
	tool := &mockTool{name: "echo", description: "Echo", params: map[string]any{"type": "object"}, result: "ok"}
	_, stats, err := runLoopWithUserMsg(context.Background(), mock,
		newTestToolRegistry(tool), newTestBuffer(), "Hi", func(o *RunLoopOpts) { o.Pricing = testRunPricing() })
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if stats.Cost == nil {
		t.Fatal("a priced run produced no cost")
	}
	if stats.CostIncomplete {
		t.Error("a fully priced run should not be marked incomplete")
	}
	write, _ := llm.EstimateCost(mock.responses[0].usage, testRunPricing())
	read, _ := llm.EstimateCost(mock.responses[1].usage, testRunPricing())
	if stats.Cost.Total != write.Total+read.Total {
		t.Errorf("total = %d, want the two calls summed (%d)", stats.Cost.Total, write.Total+read.Total)
	}
	// The write cost more than the same call uncached; the read more than made
	// it back. Only the run as a whole is ahead, which is the fact a per-run
	// figure alone would hide.
	if write.Total <= write.Baseline {
		t.Errorf("a cache write should cost more than the same call uncached: %d vs %d", write.Total, write.Baseline)
	}
	if stats.Cost.Total >= stats.Cost.Baseline {
		t.Errorf("the run should be ahead overall: %d vs %d", stats.Cost.Total, stats.Cost.Baseline)
	}
}

// An unpriced model leaves the cost nil rather than zero, and says the total is
// missing work rather than covering it.
func TestRunStatsLeavesAnUnpricedRunUnpriced(t *testing.T) {
	mock := &mockLLMClient{responses: []mockResponse{
		{content: "Done.", usage: llm.Usage{PromptTokens: 100, CompletionTokens: 10}},
	}}
	_, stats, err := runLoopWithUserMsg(context.Background(), mock,
		newTestToolRegistry(), newTestBuffer(), "Hi")
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if stats.Cost != nil {
		t.Errorf("an unpriced run produced a cost: %+v", stats.Cost)
	}
	if !stats.CostIncomplete {
		t.Error("a run that did work it could not price should say the total is partial")
	}
}

// A call the provider reported nothing for is already unmeasured in the token
// counts. Marking the money partial as well would present one gap as two.
func TestRunStatsDoesNotCallAnUnmeasuredTurnUnpriced(t *testing.T) {
	mock := &mockLLMClient{responses: []mockResponse{{content: "Done."}}}
	_, stats, err := runLoopWithUserMsg(context.Background(), mock,
		newTestToolRegistry(), newTestBuffer(), "Hi", func(o *RunLoopOpts) { o.Pricing = testRunPricing() })
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if stats.Cost != nil || stats.CostIncomplete {
		t.Errorf("an unmeasured turn produced cost=%v incomplete=%v", stats.Cost, stats.CostIncomplete)
	}
}
