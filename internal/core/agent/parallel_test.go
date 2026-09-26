package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/core/llm"
)

// span records when a call entered and left Execute, which is how the tests
// below tell overlap from sequence.
type span struct {
	name       string
	start, end time.Time
}

// timedTool sleeps, records its span, and reports the access it was built with.
type timedTool struct {
	name   string
	access llm.Access
	sleep  time.Duration
	panics bool

	mu    sync.Mutex
	spans *[]span
}

func (t *timedTool) Name() string                       { return t.name }
func (t *timedTool) Description() string                { return "timed" }
func (t *timedTool) Parameters() any                    { return map[string]any{} }
func (t *timedTool) Access(_ map[string]any) llm.Access { return t.access }

func (t *timedTool) Execute(_ context.Context, args map[string]any) (string, error) {
	if t.panics {
		panic("tool exploded")
	}
	start := time.Now()
	time.Sleep(t.sleep)
	t.mu.Lock()
	id, _ := args["id"].(string)
	*t.spans = append(*t.spans, span{name: t.name + ":" + id, start: start, end: time.Now()})
	t.mu.Unlock()
	return "ok", nil
}

func callsFor(name string, ids ...string) []llm.ToolCall {
	out := make([]llm.ToolCall, 0, len(ids))
	for _, id := range ids {
		out = append(out, llm.ToolCall{ID: id, Name: name, Arguments: fmt.Sprintf(`{"id":%q}`, id)})
	}
	return out
}

func runBatch(t *testing.T, limit int, tools []llm.Tool, calls []llm.ToolCall) (*testBuffer, time.Duration) {
	t.Helper()
	registry := llm.NewToolRegistry()
	registry.AppendTools(tools...)
	sess := newTestBuffer()
	mock := &mockLLMClient{responses: []mockResponse{{toolCalls: calls}, {content: "done"}}}

	start := time.Now()
	_, _, err := runLoopWithUserMsg(context.Background(), mock, registry, sess, "go", func(o *RunLoopOpts) {
		o.MaxParallelTools = limit
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	return sess, elapsed
}

// TestParallel_ReadsOverlap is the point of the whole design: independent
// read-only calls finish in about the time of one, not the sum.
func TestParallel_ReadsOverlap(t *testing.T) {
	const sleep = 100 * time.Millisecond
	var spans []span
	tool := &timedTool{name: "read", access: llm.AccessReadOnly, sleep: sleep, spans: &spans}

	_, elapsed := runBatch(t, 4, []llm.Tool{tool}, callsFor("read", "a", "b", "c"))

	if elapsed > sleep*2 {
		t.Errorf("three %v calls took %v; want them overlapped, not summed", sleep, elapsed)
	}
	if len(spans) != 3 {
		t.Fatalf("recorded %d spans, want 3", len(spans))
	}
	if !overlaps(spans[0], spans[1]) || !overlaps(spans[1], spans[2]) {
		t.Errorf("spans did not overlap: %v", spans)
	}
}

// TestParallel_SequentialWhenLimitIsOne is D6: the escape hatch has to work.
func TestParallel_SequentialWhenLimitIsOne(t *testing.T) {
	const sleep = 60 * time.Millisecond
	var spans []span
	tool := &timedTool{name: "read", access: llm.AccessReadOnly, sleep: sleep, spans: &spans}

	_, elapsed := runBatch(t, 1, []llm.Tool{tool}, callsFor("read", "a", "b", "c"))

	if elapsed < sleep*3 {
		t.Errorf("limit 1 finished in %v; want the calls run one after another", elapsed)
	}
}

// TestParallel_WriteIsABarrier: a write must never overlap a neighbour, or
// [Write a, Read a] would stop meaning what the model wrote.
func TestParallel_WriteIsABarrier(t *testing.T) {
	const sleep = 50 * time.Millisecond
	var spans []span
	read := &timedTool{name: "read", access: llm.AccessReadOnly, sleep: sleep, spans: &spans}
	write := &timedTool{name: "write", access: llm.AccessWrite, sleep: sleep, spans: &spans}

	calls := []llm.ToolCall{
		{ID: "1", Name: "read", Arguments: `{"id":"1"}`},
		{ID: "2", Name: "read", Arguments: `{"id":"2"}`},
		{ID: "3", Name: "write", Arguments: `{"id":"3"}`},
		{ID: "4", Name: "read", Arguments: `{"id":"4"}`},
	}
	runBatch(t, 8, []llm.Tool{read, write}, calls)

	byName := map[string]span{}
	for _, s := range spans {
		byName[s.name] = s
	}
	w := byName["write:3"]
	for _, other := range []string{"read:1", "read:2", "read:4"} {
		if overlaps(w, byName[other]) {
			t.Errorf("write overlapped %s: %v vs %v", other, w, byName[other])
		}
	}
	if !overlaps(byName["read:1"], byName["read:2"]) {
		t.Error("the two reads before the write should still have overlapped")
	}
}

// TestParallel_PanicIsContained keeps one bad tool from taking the run down or
// stranding the siblings running beside it.
func TestParallel_PanicIsContained(t *testing.T) {
	var spans []span
	ok := &timedTool{name: "read", access: llm.AccessReadOnly, sleep: 10 * time.Millisecond, spans: &spans}
	boom := &timedTool{name: "boom", access: llm.AccessReadOnly, panics: true, spans: &spans}

	calls := []llm.ToolCall{
		{ID: "1", Name: "read", Arguments: `{"id":"1"}`},
		{ID: "2", Name: "boom", Arguments: `{"id":"2"}`},
		{ID: "3", Name: "read", Arguments: `{"id":"3"}`},
	}
	sess, _ := runBatch(t, 8, []llm.Tool{ok, boom}, calls)

	results := toolResults(sess)
	if len(results) != 3 {
		t.Fatalf("got %d tool results, want one per call even with a panic", len(results))
	}
	if got := results["2"]; got == "" || !contains(got, "panicked") {
		t.Errorf("result for the panicking call = %q, want it to report the panic", got)
	}
	if results["1"] != "ok" || results["3"] != "ok" {
		t.Errorf("siblings were stranded: %v", results)
	}
}

// TestParallel_EveryCallGetsExactlyOneResult is D4. A batch that half-executes
// still has to leave a well-formed history, or the next LLM call is malformed.
func TestParallel_EveryCallGetsExactlyOneResult(t *testing.T) {
	var spans []span
	read := &timedTool{name: "read", access: llm.AccessReadOnly, sleep: time.Millisecond, spans: &spans}

	calls := append(callsFor("read", "a", "b"),
		llm.ToolCall{ID: "c", Name: "nonexistent", Arguments: `{}`},
		llm.ToolCall{ID: "d", Name: "read", Arguments: `{bad json`},
	)
	sess, _ := runBatch(t, 8, []llm.Tool{read}, calls)

	results := toolResults(sess)
	for _, id := range []string{"a", "b", "c", "d"} {
		if _, ok := results[id]; !ok {
			t.Errorf("call %q got no tool result", id)
		}
	}
	if len(results) != 4 {
		t.Errorf("got %d results for 4 calls", len(results))
	}
}

func overlaps(a, b span) bool { return a.start.Before(b.end) && b.start.Before(a.end) }

func toolResults(sess *testBuffer) map[string]string {
	out := map[string]string{}
	for _, m := range sess.messages {
		if m.Role == "tool" {
			out[m.ToolCallID] = m.Content
		}
	}
	return out
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// askFor returns Ask for one tool name and abstains otherwise.
type askFor struct{ name string }

func (p askFor) Check(name, _ string, _ map[string]any) (llm.ToolAction, bool) {
	if name == p.name {
		return llm.ToolActionAsk, true
	}
	return llm.ToolActionAllow, false
}

// orderedApproval records the order it was asked in and answers per tool.
type orderedApproval struct {
	mu      sync.Mutex
	asked   []string
	approve bool
}

func (a *orderedApproval) RequestApproval(_ context.Context, name string, args map[string]any, _ string) ApprovalDecision {
	a.mu.Lock()
	id, _ := args["id"].(string)
	a.asked = append(a.asked, name+":"+id)
	a.mu.Unlock()
	if a.approve {
		return ApprovalAllowOnce
	}
	return ApprovalDeny
}

// TestParallel_AskInsideAGroupPromptsOnceInOrder: approval stays on the loop
// goroutine, so prompts do not interleave even when the calls they gate do.
func TestParallel_AskInsideAGroupPromptsOnceInOrder(t *testing.T) {
	var spans []span
	read := &timedTool{name: "read", access: llm.AccessReadOnly, sleep: 20 * time.Millisecond, spans: &spans}
	approval := &orderedApproval{approve: true}

	registry := llm.NewToolRegistry()
	registry.AppendTools(read)
	sess := newTestBuffer()
	mock := &mockLLMClient{responses: []mockResponse{
		{toolCalls: callsFor("read", "a", "b", "c")},
		{content: "done"},
	}}
	_, _, err := runLoopWithUserMsg(context.Background(), mock, registry, sess, "go", func(o *RunLoopOpts) {
		o.MaxParallelTools = 8
		o.Policy = askFor{name: "read"}
		o.Approval = approval
	})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}

	want := []string{"read:a", "read:b", "read:c"}
	if len(approval.asked) != len(want) {
		t.Fatalf("prompted %v; want one prompt per call, in call order", approval.asked)
	}
	for i := range want {
		if approval.asked[i] != want[i] {
			t.Errorf("prompt %d = %q, want %q", i, approval.asked[i], want[i])
		}
	}
}

// TestParallel_DenialLeavesSiblingsRunning: a refused call must not cancel the
// group it happened to be scheduled with.
func TestParallel_DenialLeavesSiblingsRunning(t *testing.T) {
	var spans []span
	read := &timedTool{name: "read", access: llm.AccessReadOnly, sleep: 10 * time.Millisecond, spans: &spans}
	deny := &timedTool{name: "deny", access: llm.AccessReadOnly, sleep: 10 * time.Millisecond, spans: &spans}

	registry := llm.NewToolRegistry()
	registry.AppendTools(read, deny)
	sess := newTestBuffer()
	mock := &mockLLMClient{responses: []mockResponse{
		{toolCalls: []llm.ToolCall{
			{ID: "1", Name: "read", Arguments: `{"id":"1"}`},
			{ID: "2", Name: "deny", Arguments: `{"id":"2"}`},
			{ID: "3", Name: "read", Arguments: `{"id":"3"}`},
		}},
		{content: "done"},
	}}
	_, _, err := runLoopWithUserMsg(context.Background(), mock, registry, sess, "go", func(o *RunLoopOpts) {
		o.MaxParallelTools = 8
		o.Policy = askFor{name: "deny"}
		o.Approval = &orderedApproval{approve: false}
	})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}

	results := toolResults(sess)
	if !contains(results["2"], "denied by user") {
		t.Errorf("result for the denied call = %q", results["2"])
	}
	if results["1"] != "ok" || results["3"] != "ok" {
		t.Errorf("siblings did not run: %v", results)
	}
}

// TestParallel_LoopGuardIsSchedulerIndependent: the guard counts in the gate,
// which is serial, so repeating a call must be caught identically at any limit.
func TestParallel_LoopGuardIsSchedulerIndependent(t *testing.T) {
	run := func(limit int) map[string]string {
		t.Helper()
		var spans []span
		read := &timedTool{name: "read", access: llm.AccessReadOnly, sleep: time.Millisecond, spans: &spans}
		registry := llm.NewToolRegistry()
		registry.AppendTools(read)

		// The same call, repeated past the guard threshold, in one batch.
		var calls []llm.ToolCall
		for i := 0; i < defaultMaxRepeatedCalls+2; i++ {
			calls = append(calls, llm.ToolCall{ID: fmt.Sprintf("c%d", i), Name: "read", Arguments: `{"id":"same"}`})
		}
		sess := newTestBuffer()
		mock := &mockLLMClient{responses: []mockResponse{{toolCalls: calls}, {content: "done"}}}
		_, _, err := runLoopWithUserMsg(context.Background(), mock, registry, sess, "go", func(o *RunLoopOpts) {
			o.MaxParallelTools = limit
		})
		if err != nil {
			t.Fatalf("RunLoop(limit=%d): %v", limit, err)
		}
		return toolResults(sess)
	}

	sequential, grouped := run(1), run(8)
	if len(sequential) != len(grouped) {
		t.Fatalf("result counts differ: %d vs %d", len(sequential), len(grouped))
	}
	for id, want := range sequential {
		if grouped[id] != want {
			t.Errorf("call %s: limit 1 gave %q, limit 8 gave %q", id, want, grouped[id])
		}
	}
}

// TestParallel_CancellationStillCommitsEveryCall is D4 under the worst case: a
// batch abandoned mid-flight must still leave one tool message per tool_call,
// or the next LLM request is malformed.
func TestParallel_CancellationStillCommitsEveryCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	blocking := &ctxTool{name: "block", entered: make(chan struct{}), once: &sync.Once{}}

	registry := llm.NewToolRegistry()
	registry.AppendTools(blocking)
	sess := newTestBuffer()
	mock := &mockLLMClient{responses: []mockResponse{
		{toolCalls: callsFor("block", "a", "b", "c")},
		{content: "done"},
	}}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = runLoopWithUserMsg(ctx, mock, registry, sess, "go", func(o *RunLoopOpts) {
			o.MaxParallelTools = 8
		})
	}()

	<-blocking.entered
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunLoop did not return after cancelling mid-group")
	}

	results := toolResults(sess)
	for _, id := range []string{"a", "b", "c"} {
		if _, ok := results[id]; !ok {
			t.Errorf("call %q left the history without a tool result", id)
		}
	}
}

// ctxTool blocks until the run's context ends.
type ctxTool struct {
	name    string
	entered chan struct{}
	once    *sync.Once
}

func (t *ctxTool) Name() string                       { return t.name }
func (t *ctxTool) Description() string                { return "blocks" }
func (t *ctxTool) Parameters() any                    { return map[string]any{} }
func (t *ctxTool) Access(_ map[string]any) llm.Access { return llm.AccessReadOnly }
func (t *ctxTool) Execute(ctx context.Context, _ map[string]any) (string, error) {
	t.once.Do(func() { close(t.entered) })
	<-ctx.Done()
	return "", ctx.Err()
}

// perCallMultimodalTool returns a part identifying the call that produced it.
type perCallMultimodalTool struct{ name string }

func (t *perCallMultimodalTool) Name() string                       { return t.name }
func (t *perCallMultimodalTool) Description() string                { return "multimodal" }
func (t *perCallMultimodalTool) Parameters() any                    { return map[string]any{} }
func (t *perCallMultimodalTool) Access(_ map[string]any) llm.Access { return llm.AccessReadOnly }

func (t *perCallMultimodalTool) Execute(_ context.Context, _ map[string]any) (string, error) {
	return "text only", nil
}

func (t *perCallMultimodalTool) ExecuteMultimodal(_ context.Context, args map[string]any) (llm.ToolResult, error) {
	id, _ := args["id"].(string)
	time.Sleep(10 * time.Millisecond) // long enough for the group to overlap
	return llm.ToolResult{
		Text:  "text-" + id,
		Parts: []llm.ContentPart{{Type: llm.ContentPartText, Text: "part-" + id}},
	}, nil
}

// TestParallel_MultimodalPartsStayWithTheirCall covers the interaction the two
// designs created when they met: tool results grew non-text parts at the same
// time as calls started overlapping. Each worker writes only its own element,
// but a shared buffer or a misplaced index would land one call's image on
// another's result and nothing downstream would notice.
func TestParallel_MultimodalPartsStayWithTheirCall(t *testing.T) {
	registry := llm.NewToolRegistry()
	registry.AppendTools(&perCallMultimodalTool{name: "shot"})
	sess := newTestBuffer()
	mock := &mockLLMClient{responses: []mockResponse{
		{toolCalls: callsFor("shot", "a", "b", "c")},
		{content: "done"},
	}}
	_, _, err := runLoopWithUserMsg(context.Background(), mock, registry, sess, "go", func(o *RunLoopOpts) {
		o.MaxParallelTools = 8
	})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}

	seen := 0
	for _, m := range sess.messages {
		if m.Role != "tool" {
			continue
		}
		seen++
		if len(m.Parts) != 1 {
			t.Errorf("call %s carried %d parts, want 1", m.ToolCallID, len(m.Parts))
			continue
		}
		if want := "part-" + m.ToolCallID; m.Parts[0].Text != want {
			t.Errorf("call %s carried %q, want %q", m.ToolCallID, m.Parts[0].Text, want)
		}
		if want := "text-" + m.ToolCallID; m.Content != want {
			t.Errorf("call %s text = %q, want %q", m.ToolCallID, m.Content, want)
		}
	}
	if seen != 3 {
		t.Errorf("saw %d tool messages, want 3", seen)
	}
}
