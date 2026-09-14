package agent

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/llm"
)

// recordingEventSink collects all events emitted during a RunLoop call.
type recordingEventSink struct {
	events []Event
}

func (r *recordingEventSink) sink() func(Event) {
	return func(e Event) { r.events = append(r.events, e) }
}

func (r *recordingEventSink) kinds() []EventKind {
	kinds := make([]EventKind, len(r.events))
	for i, e := range r.events {
		kinds[i] = e.Kind
	}
	return kinds
}

func (r *recordingEventSink) findAll(kind EventKind) []Event {
	var out []Event
	for _, e := range r.events {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func (r *recordingEventSink) last() Event {
	if len(r.events) == 0 {
		return Event{}
	}
	return r.events[len(r.events)-1]
}

// runWithSink is a test helper that adds a prompt to a fresh buffer and runs RunLoop
// with opts (EventSink must be set by the caller via rec.sink()).
func runWithSink(ctx context.Context, llmClient llm.LLMClient, reg llm.ToolRegistry, sink func(Event), tools ...llm.Tool) (string, RunStats, error) {
	buf := newTestBuffer()
	_ = buf.Append(llm.Message{Role: "user", Content: "go"})
	reply, stats, _, err := RunLoop(ctx, RunLoopOpts{
		LLMClient:    llmClient,
		SystemPrompt: "test",
		ToolRegistry: reg,
		MaxIter:      testMaxIter,
		History:      buf,
		EventSink:    sink,
	})
	return reply, stats, err
}

// --- Test 1: simple final reply ---

func TestEvents_FinalReply(t *testing.T) {
	mock := &mockLLMClient{responses: []mockResponse{{content: "hello"}}}
	rec := &recordingEventSink{}

	reply, _, err := runWithSink(context.Background(), mock, newTestToolRegistry(), rec.sink())
	if err != nil || reply != "hello" {
		t.Fatalf("unexpected: reply=%q err=%v", reply, err)
	}

	// Expected sequence: IterStart → LLMStart → LLMEnd → RunEnd
	kinds := rec.kinds()
	want := []EventKind{EventIterStart, EventLLMStart, EventLLMEnd, EventRunEnd}
	if !kindsEqual(kinds, want) {
		t.Errorf("event kinds = %v, want %v", kinds, want)
	}

	// LLMEnd should carry the content
	llmEnd := rec.findAll(EventLLMEnd)
	if len(llmEnd) != 1 || llmEnd[0].Content != "hello" || llmEnd[0].HasToolCalls {
		t.Errorf("LLMEnd = %+v, want Content=hello HasToolCalls=false", llmEnd)
	}

	// RunEnd should have no error
	runEnd := rec.last()
	if runEnd.Kind != EventRunEnd || runEnd.Err != nil {
		t.Errorf("RunEnd.Err = %v, want nil", runEnd.Err)
	}
}

// --- Test 2: one tool call then final reply ---

func TestEvents_ToolCall(t *testing.T) {
	tc := llm.ToolCall{ID: "c1", Name: "echo", Arguments: `{"msg":"hi"}`}
	mock := &mockLLMClient{responses: []mockResponse{
		{content: "calling echo", toolCalls: []llm.ToolCall{tc}},
		{content: "done"},
	}}
	tool := &mockTool{name: "echo", result: "echoed"}
	rec := &recordingEventSink{}

	_, _, err := runWithSink(context.Background(), mock, newTestToolRegistry(tool), rec.sink())
	if err != nil {
		t.Fatal(err)
	}

	// Must have exactly one ToolStart and one ToolEnd
	starts := rec.findAll(EventToolStart)
	ends := rec.findAll(EventToolEnd)
	if len(starts) != 1 || len(ends) != 1 {
		t.Errorf("ToolStart=%d ToolEnd=%d, want 1 each", len(starts), len(ends))
	}
	if starts[0].ToolName != "echo" || starts[0].ToolCallID != "c1" {
		t.Errorf("ToolStart = %+v", starts[0])
	}
	// Duration may legitimately be 0: the Windows clock ticks in ~15ms steps,
	// so a mock tool can start and finish inside one tick.
	if ends[0].ToolResult != "echoed" || ends[0].ToolDuration < 0 {
		t.Errorf("ToolEnd = %+v", ends[0])
	}

	// ToolStart must come before ToolEnd in the event stream
	assertBefore(t, rec.events, EventToolStart, EventToolEnd)

	// Two iterations: IterStart must appear twice
	if len(rec.findAll(EventIterStart)) != 2 {
		t.Errorf("expected 2 IterStart events, got %d", len(rec.findAll(EventIterStart)))
	}
}

// --- Test 3: policy deny ---

func TestEvents_PolicyDeny(t *testing.T) {
	tc := llm.ToolCall{ID: "c2", Name: "bash", Arguments: `{"command":"ls"}`}
	mock := &mockLLMClient{responses: []mockResponse{
		{toolCalls: []llm.ToolCall{tc}},
		{content: "blocked"},
	}}
	denyAll := denyAllPolicy{}
	tool := &mockTool{name: "bash", result: "should not run"}
	rec := &recordingEventSink{}

	buf := newTestBuffer()
	_ = buf.Append(llm.Message{Role: "user", Content: "go"})
	_, _, _, err := RunLoop(context.Background(), RunLoopOpts{
		LLMClient:    mock,
		SystemPrompt: "test",
		ToolRegistry: newTestToolRegistry(tool),
		MaxIter:      10,
		History:      buf,
		Policy:       denyAll,
		EventSink:    rec.sink(),
	})
	if err != nil {
		t.Fatal(err)
	}

	denied := rec.findAll(EventToolDenied)
	if len(denied) != 1 {
		t.Fatalf("expected 1 EventToolDenied, got %d", len(denied))
	}
	if denied[0].DenyReason != DenyReasonPolicy {
		t.Errorf("DenyReason = %q, want %q", denied[0].DenyReason, DenyReasonPolicy)
	}
	if denied[0].ToolName != "bash" {
		t.Errorf("ToolName = %q, want bash", denied[0].ToolName)
	}
	// Tool should never have executed
	if tool.executionCount() != 0 {
		t.Error("tool was executed despite deny")
	}
}

// --- Test 4: loop guard ---

func TestEvents_LoopGuard(t *testing.T) {
	tc := llm.ToolCall{ID: "c3", Name: "ping", Arguments: `{}`}
	// Provide 5 identical calls; the 4th should be blocked by loop guard (max=3)
	var responses []mockResponse
	for i := 0; i < 5; i++ {
		responses = append(responses, mockResponse{toolCalls: []llm.ToolCall{tc}})
	}
	responses = append(responses, mockResponse{content: "done"})

	mock := &mockLLMClient{responses: responses}
	tool := &mockTool{name: "ping", result: "pong"}
	rec := &recordingEventSink{}

	_, _, err := runWithSink(context.Background(), mock, newTestToolRegistry(tool), rec.sink())
	if err != nil {
		t.Fatal(err)
	}

	loopDenied := rec.findAll(EventToolDenied)
	hasLoopGuard := false
	for _, e := range loopDenied {
		if e.DenyReason == DenyReasonLoopGuard {
			hasLoopGuard = true
		}
	}
	if !hasLoopGuard {
		t.Errorf("expected EventToolDenied with reason %q, got %v", DenyReasonLoopGuard, loopDenied)
	}
}

// --- Test 5: context compaction ---

func TestEvents_ContextCompacted(t *testing.T) {
	// Use a tiny context window (200 tokens) and a history large enough to exceed 80%.
	smallCW := &smallWindowLLMClient{
		inner:         &mockLLMClient{responses: []mockResponse{{content: "ok"}}},
		contextWindow: 200,
	}
	compactor := &alwaysCompactor{summary: "prior context"}

	buf := newTestBuffer()
	// Pre-fill with many messages to exceed the threshold.
	for i := 0; i < 30; i++ {
		_ = buf.Append(llm.Message{Role: "user", Content: strings.Repeat("x", 20)})
		_ = buf.Append(llm.Message{Role: "assistant", Content: strings.Repeat("y", 20)})
	}
	_ = buf.Append(llm.Message{Role: "user", Content: "go"})

	rec := &recordingEventSink{}
	_, _, _, err := RunLoop(context.Background(), RunLoopOpts{
		LLMClient:    smallCW,
		SystemPrompt: "test",
		ToolRegistry: newTestToolRegistry(),
		MaxIter:      5,
		History:      buf,
		Compactor:    compactor,
		EventSink:    rec.sink(),
	})
	if err != nil {
		t.Fatal(err)
	}

	compacted := rec.findAll(EventContextCompacted)
	if len(compacted) == 0 {
		t.Fatal("expected at least one EventContextCompacted")
	}
	if compacted[0].Summarized <= 0 || compacted[0].Kept <= 0 {
		t.Errorf("EventContextCompacted = %+v, want positive Summarized and Kept", compacted[0])
	}
}

// --- Test 6: max iterations exceeded ---

func TestEvents_MaxIter(t *testing.T) {
	tc := llm.ToolCall{ID: "c4", Name: "echo", Arguments: `{}`}
	var responses []mockResponse
	for i := 0; i < 10; i++ {
		responses = append(responses, mockResponse{toolCalls: []llm.ToolCall{tc}})
	}
	mock := &mockLLMClient{responses: responses}
	tool := &mockTool{name: "echo", result: "ok"}
	rec := &recordingEventSink{}

	buf := newTestBuffer()
	_ = buf.Append(llm.Message{Role: "user", Content: "go"})
	_, _, _, err := RunLoop(context.Background(), RunLoopOpts{
		LLMClient:    mock,
		SystemPrompt: "test",
		ToolRegistry: newTestToolRegistry(tool),
		MaxIter:      3,
		History:      buf,
		EventSink:    rec.sink(),
	})
	if err == nil || !strings.Contains(err.Error(), "max iterations") {
		t.Fatalf("expected max iterations error, got %v", err)
	}

	runEnd := rec.last()
	if runEnd.Kind != EventRunEnd {
		t.Errorf("last event = %v, want EventRunEnd", runEnd.Kind)
	}
	if runEnd.Err == nil || !strings.Contains(runEnd.Err.Error(), "max iterations") {
		t.Errorf("RunEnd.Err = %v, want max iterations error", runEnd.Err)
	}
}

// --- Test 7: context cancellation returns partial content ---

func TestEvents_Cancellation(t *testing.T) {
	firstContent := "I'll look into that."
	ctx, cancelCtx := context.WithCancel(context.Background())

	// The client returns a valid first response, then cancels ctx and returns context.Canceled.
	mock := &cancellingClient{
		first:     mockResponse{content: firstContent, toolCalls: []llm.ToolCall{{ID: "c5", Name: "echo", Arguments: "{}"}}},
		cancelCtx: cancelCtx,
	}

	tool := &mockTool{name: "echo", result: "ok"}
	rec := &recordingEventSink{}

	reply, _, err := runWithSink(ctx, mock, newTestToolRegistry(tool), rec.sink())
	if err != nil {
		t.Fatalf("cancellation should return nil error, got %v", err)
	}
	if reply != firstContent {
		t.Errorf("reply = %q, want %q", reply, firstContent)
	}

	runEnd := rec.last()
	if runEnd.Kind != EventRunEnd {
		t.Errorf("last event = %v, want EventRunEnd", runEnd.Kind)
	}
	if runEnd.Err != nil {
		t.Errorf("RunEnd.Err = %v, want nil (graceful cancellation)", runEnd.Err)
	}
}

// --- helpers ---

func kindsEqual(got, want []EventKind) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// assertBefore checks that the first occurrence of a comes before the first occurrence of b.
func assertBefore(t *testing.T, events []Event, a, b EventKind) {
	t.Helper()
	ai, bi := -1, -1
	for i, e := range events {
		if ai < 0 && e.Kind == a {
			ai = i
		}
		if bi < 0 && e.Kind == b {
			bi = i
		}
	}
	if ai < 0 || bi < 0 || ai >= bi {
		t.Errorf("%v (at %d) should come before %v (at %d)", a, ai, b, bi)
	}
}

// denyAllPolicy denies every tool call.
type denyAllPolicy struct{}

func (denyAllPolicy) Check(_, _ string, _ map[string]any) (llm.ToolAction, bool) {
	return llm.ToolActionDeny, true
}

// smallWindowLLMClient wraps a mock with a small context window to trigger compaction.
type smallWindowLLMClient struct {
	inner         *mockLLMClient
	contextWindow int
}

func (c *smallWindowLLMClient) ChatCompletionBlocking(ctx context.Context, req llm.Request) (llm.Completion, error) {
	msgs := req.Messages
	return c.inner.ChatCompletionBlocking(ctx, llm.Request{Messages: msgs, Tools: req.Tools})
}
func (c *smallWindowLLMClient) ChatCompletionStreaming(ctx context.Context, req llm.Request, onDelta func(string)) (llm.Completion, error) {
	msgs := req.Messages
	tools := req.Tools
	return c.inner.ChatCompletionStreaming(ctx, llm.Request{Messages: msgs, Tools: tools}, onDelta)
}
func (c *smallWindowLLMClient) ContextWindow() int { return c.contextWindow }

// alwaysCompactor always returns the given summary string.
type alwaysCompactor struct{ summary string }

func (a *alwaysCompactor) Compact(_ context.Context, _ []llm.Message) (string, llm.Usage, error) {
	return a.summary, llm.Usage{PromptTokens: 40, CompletionTokens: 8, TotalTokens: 48}, nil
}

// cancellingClient returns a first response then cancels the context and returns context.Canceled.
// This ensures ctx.Err() != nil when RunLoop checks it after the LLM error.
type cancellingClient struct {
	first     mockResponse
	cancelCtx context.CancelFunc
	calls     int
}

func (c *cancellingClient) ChatCompletionBlocking(ctx context.Context, req llm.Request) (llm.Completion, error) {
	c.calls++
	if c.calls == 1 {
		return llm.Completion{Content: c.first.content, ToolCalls: c.first.toolCalls, Usage: c.first.usage}, nil
	}
	c.cancelCtx() // cancel before returning so ctx.Err() is set
	return llm.Completion{}, context.Canceled
}
func (c *cancellingClient) ChatCompletionStreaming(ctx context.Context, req llm.Request, _ func(string)) (llm.Completion, error) {
	msgs := req.Messages
	tools := req.Tools
	return c.ChatCompletionBlocking(ctx, llm.Request{Messages: msgs, Tools: tools})
}
func (c *cancellingClient) ContextWindow() int { return 0 }

// TestSerializedSink_ConcurrentEmitsDoNotRace covers the contract RunLoopOpts
// now states: a sink may be called from a tool worker, and the runtime — not
// each consumer — is what keeps those calls from overlapping. Meaningful under
// `./make test race`.
func TestSerializedSink_ConcurrentEmitsDoNotRace(t *testing.T) {
	var got []Event
	sink := serializedSink(func(e Event) { got = append(got, e) })

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sink(Event{Kind: EventToolEnd, ToolCallID: strconv.Itoa(i)})
		}(i)
	}
	wg.Wait()

	if len(got) != 32 {
		t.Errorf("received %d events, want 32", len(got))
	}
}

// TestSerializedSink_NilStaysNil keeps a run that emits nothing paying nothing.
func TestSerializedSink_NilStaysNil(t *testing.T) {
	if serializedSink(nil) != nil {
		t.Error("a nil sink must stay nil rather than becoming a locked no-op")
	}
}
