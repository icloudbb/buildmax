package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/llm"
)

// pricedCompactor reports what its summarization cost, the way the real one does.
type pricedCompactor struct{ usage llm.Usage }

func (c *pricedCompactor) Compact(context.Context, []llm.Message) (string, llm.Usage, error) {
	return "summary", c.usage, nil
}

// Compaction is a model call the run caused. Before it was metered the call
// appeared nowhere — not in RunStats, not in the trace — so a long session
// under-reported itself exactly where compaction runs most.
func TestRunLoop_CompactionIsPriced(t *testing.T) {
	client := &windowedClient{window: testContextWindow}
	history := &compactingHistory{}
	fillToThreshold(history)

	comp := &pricedCompactor{usage: llm.Usage{PromptTokens: 3000, CompletionTokens: 200}}
	var compacted []Event
	_, stats, _, err := RunLoop(context.Background(), RunLoopOpts{
		LLMClient:    client,
		SystemPrompt: testSystemPrompt,
		ToolRegistry: newTestToolRegistry(),
		MaxIter:      testMaxIter,
		History:      history,
		Compactor:    comp,
		Pricing:      testPricing(),
		EventSink: func(e Event) {
			if e.Kind == EventContextCompacted {
				compacted = append(compacted, e)
			}
		},
	})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if len(compacted) == 0 {
		t.Fatal("no compaction happened, so the test asserts nothing")
	}
	if stats.PromptTokens < 3000 {
		t.Errorf("PromptTokens = %d, want at least the compaction's 3000", stats.PromptTokens)
	}
	if stats.CompletionTokens < 200 {
		t.Errorf("CompletionTokens = %d, want at least the compaction's 200", stats.CompletionTokens)
	}
	if stats.Cost == nil || stats.Cost.Total == 0 {
		t.Fatal("Cost is unset on a priced run that compacted")
	}
	if compacted[0].CallUsage.PromptTokens != 3000 {
		t.Errorf("context_compacted CallUsage.PromptTokens = %d, want 3000",
			compacted[0].CallUsage.PromptTokens)
	}
	if compacted[0].CallCost == nil {
		t.Error("context_compacted carries no cost, so a trace reader cannot see what compaction cost")
	}
}

// erroringTool returns an error, which the loop flattens into result text. The
// flattening is why the kind has to travel as a field: "error: …" in a result
// is indistinguishable from a tool that legitimately reports a bad outcome.
type erroringTool struct{}

func (erroringTool) Name() string        { return "failer" }
func (erroringTool) Description() string { return "fails" }
func (erroringTool) Parameters() any     { return map[string]any{} }
func (erroringTool) Execute(context.Context, map[string]any) (string, error) {
	return "", errors.New("disk on fire")
}

func toolEndEvents(t *testing.T, tool llm.Tool, call llm.ToolCall) []Event {
	t.Helper()
	client := &mockLLMClient{responses: []mockResponse{
		{toolCalls: []llm.ToolCall{call}},
		{content: "done"},
	}}
	var ends []Event
	_, _, err := runLoopWithUserMsg(context.Background(), client,
		newTestToolRegistry(tool), &testBuffer{}, "go",
		func(o *RunLoopOpts) {
			o.EventSink = func(e Event) {
				if e.Kind == EventToolEnd {
					ends = append(ends, e)
				}
			}
		})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	return ends
}

func TestToolEnd_ReportsHowTheCallFailed(t *testing.T) {
	ends := toolEndEvents(t, erroringTool{}, llm.ToolCall{ID: "c1", Name: "failer", Arguments: "{}"})
	if len(ends) != 1 {
		t.Fatalf("got %d tool_end events, want 1", len(ends))
	}
	if ends[0].ToolErrorKind != ToolErrorFailed {
		t.Errorf("ToolErrorKind = %q, want %q", ends[0].ToolErrorKind, ToolErrorFailed)
	}
}

// A tool that ran and reported a bad outcome succeeded at this boundary. Bash
// returns exactly this shape for a non-zero exit, and counting it as a failure
// would flatter the runs going worst.
func TestToolEnd_BadOutcomeIsNotAFailure(t *testing.T) {
	tool := &mockTool{name: "runner", result: "Command failed with exit code 1.\nboom"}
	ends := toolEndEvents(t, tool, llm.ToolCall{ID: "c1", Name: "runner", Arguments: "{}"})
	if len(ends) != 1 {
		t.Fatalf("got %d tool_end events, want 1", len(ends))
	}
	if ends[0].ToolErrorKind != "" {
		t.Errorf("ToolErrorKind = %q, want empty — the call completed", ends[0].ToolErrorKind)
	}
}

// Arguments the model could not form are the signal that a tool's schema or
// the prompt is wrong. The call used to be appended to the history and emit no
// events at all, leaving it invisible in the one place someone would look.
func TestToolEnd_InvalidArgumentsAreReported(t *testing.T) {
	tool := &mockTool{name: "runner", result: "ok"}
	ends := toolEndEvents(t, tool, llm.ToolCall{ID: "c1", Name: "runner", Arguments: "{not json"})
	if len(ends) != 1 {
		t.Fatalf("got %d tool_end events, want 1", len(ends))
	}
	if ends[0].ToolErrorKind != ToolErrorInvalidArgs {
		t.Errorf("ToolErrorKind = %q, want %q", ends[0].ToolErrorKind, ToolErrorInvalidArgs)
	}
	if tool.executed != 0 {
		t.Errorf("tool executed %d times, want 0 — the call never became one", tool.executed)
	}
}
