package agent

import (
	"context"
	"encoding/json"
	"testing"

	llm "github.com/icloudbb/buildmax/internal/core/llm"
)

func structuredOutputSchema() *llm.OutputSchema {
	return &llm.OutputSchema{
		Name:   "answer",
		Schema: []byte(`{"type":"object","additionalProperties":false,"properties":{"n":{"type":"integer"}},"required":["n"]}`),
	}
}

// runLoopStructured runs a loop with an output schema and returns the reply plus
// the structured value, building opts directly since the shared helper drops it.
func runLoopStructured(ctx context.Context, mock *mockLLMClient, output *llm.OutputSchema) (string, *llm.Structured, error) {
	sess := newTestBuffer()
	if err := sess.Append(llm.Message{Role: "user", Content: "go"}); err != nil {
		return "", nil, err
	}
	reply, _, structured, err := RunLoop(ctx, RunLoopOpts{
		LLMClient:    mock,
		SystemPrompt: testSystemPrompt,
		ToolRegistry: newTestToolRegistry(),
		MaxIter:      testMaxIter,
		History:      sess,
		Output:       output,
	})
	return reply, structured, err
}

// TestRunLoopStructuredOutputReissuesOnTermination pins §5: the loop runs free,
// and once the model produces a terminating answer the runtime makes one extra
// constrained call to render it as the structured value.
func TestRunLoopStructuredOutputReissuesOnTermination(t *testing.T) {
	value := json.RawMessage(`{"n":42}`)
	mock := &mockLLMClient{
		responses: []mockResponse{
			{content: "The answer is 42."}, // terminating answer
			{structured: &llm.Structured{Value: value, Mode: llm.StructuredNative, Enforced: true}}, // extraction
		},
	}

	reply, structured, err := runLoopStructured(context.Background(), mock, structuredOutputSchema())
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	// The text reply is still the whole terminating turn.
	if reply != "The answer is 42." {
		t.Errorf("reply = %q, want the terminating turn", reply)
	}
	if structured == nil {
		t.Fatal("structured is nil, want the extracted value")
	}
	if string(structured.Value) != string(value) {
		t.Errorf("structured.Value = %s, want %s", structured.Value, value)
	}
	if mock.callCount() != 2 {
		t.Fatalf("LLM called %d times, want 2 (answer + extraction)", mock.callCount())
	}
	// Output travels only on the extraction call, never on the loop's own turn,
	// so a provider that maps output to a forced tool can still use tools (§5).
	if mock.outputs[0] != nil {
		t.Error("first (loop) call carried Output, want nil")
	}
	if mock.outputs[1] == nil {
		t.Error("extraction call did not carry Output")
	}
}

// TestRunLoopNoOutputMakesNoExtraCall pins that a run without an output schema is
// unchanged: no extraction call, no structured value.
func TestRunLoopNoOutputMakesNoExtraCall(t *testing.T) {
	mock := &mockLLMClient{
		responses: []mockResponse{{content: "plain answer"}},
	}
	reply, structured, err := runLoopStructured(context.Background(), mock, nil)
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if reply != "plain answer" {
		t.Errorf("reply = %q", reply)
	}
	if structured != nil {
		t.Errorf("structured = %+v, want nil without an output schema", structured)
	}
	if mock.callCount() != 1 {
		t.Errorf("LLM called %d times, want 1", mock.callCount())
	}
}

// TestRunLoopStructuredExtractionAfterToolCalls pins that extraction happens
// after the tool-calling turns, on the settled answer, not during them.
func TestRunLoopStructuredExtractionAfterToolCalls(t *testing.T) {
	tool := &mockTool{name: "echo", description: "echo", params: map[string]any{"type": "object"}, result: "ok"}
	value := json.RawMessage(`{"n":1}`)
	mock := &mockLLMClient{
		responses: []mockResponse{
			{toolCalls: []llm.ToolCall{{ID: "c1", Name: "echo", Arguments: "{}"}}}, // acts
			{content: "done"}, // terminating answer
			{structured: &llm.Structured{Value: value, Mode: llm.StructuredNative, Enforced: true}}, // extraction
		},
	}
	sess := newTestBuffer()
	if err := sess.Append(llm.Message{Role: "user", Content: "go"}); err != nil {
		t.Fatal(err)
	}
	reply, _, structured, err := RunLoop(context.Background(), RunLoopOpts{
		LLMClient:    mock,
		SystemPrompt: testSystemPrompt,
		ToolRegistry: newTestToolRegistry(tool),
		MaxIter:      testMaxIter,
		History:      sess,
		Output:       structuredOutputSchema(),
	})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if reply != "done" {
		t.Errorf("reply = %q, want %q", reply, "done")
	}
	if structured == nil || string(structured.Value) != string(value) {
		t.Fatalf("structured = %+v, want value %s", structured, value)
	}
	if tool.executionCount() != 1 {
		t.Errorf("tool executed %d times, want 1", tool.executionCount())
	}
	if mock.callCount() != 3 {
		t.Errorf("LLM called %d times, want 3 (tool turn + answer + extraction)", mock.callCount())
	}
}

// TestRunLoopStructuredExtractionErrorDoesNotFailRun pins that a broken
// extraction is a typed failure on the value, not a failed run: the text answer
// already stands.
func TestRunLoopStructuredExtractionErrorDoesNotFailRun(t *testing.T) {
	mock := &mockLLMClient{
		responses: []mockResponse{
			{content: "the answer"}, // terminating answer
			// extraction returns no structured value (provider produced none)
			{content: "not structured"},
		},
	}
	reply, structured, err := runLoopStructured(context.Background(), mock, structuredOutputSchema())
	if err != nil {
		t.Fatalf("RunLoop should not fail on a broken extraction: %v", err)
	}
	if reply != "the answer" {
		t.Errorf("reply = %q, want the text answer to stand", reply)
	}
	if structured == nil || structured.Err == nil {
		t.Fatalf("structured = %+v, want a typed failure", structured)
	}
}
