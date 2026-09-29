package agent

import (
	"sync"
	"time"

	"github.com/icloudbb/buildmax/internal/core/llm"
)

// EventKind identifies the type of a runtime event emitted during a RunLoop execution.
type EventKind uint8

const (
	// EventIterStart fires at the top of each agent loop iteration.
	EventIterStart EventKind = iota

	// EventLLMStart fires just before an LLM API call is dispatched.
	EventLLMStart

	// EventLLMDelta fires for each content delta when streaming is active.
	// Only emitted when StreamSink is also set (streaming mode).
	EventLLMDelta

	// EventLLMEnd fires after the LLM call returns with the full content
	// and whether the response contains tool calls.
	EventLLMEnd

	// EventToolStart fires before a tool call is evaluated (allow, deny, or execute).
	EventToolStart

	// EventToolEnd fires after a tool executes — whether it returned a result or an error string.
	EventToolEnd

	// EventToolDenied fires when a tool call is blocked before execution.
	EventToolDenied

	// EventContextCompacted fires when the message history is compacted to free context space.
	EventContextCompacted

	// EventRunEnd fires when RunLoop exits, whether successfully, on error, or on cancellation.
	EventRunEnd

	// EventUserInput fires when a message the user submitted while the run was
	// working is appended to the history at an iteration boundary. Content holds
	// the message.
	EventUserInput

	// EventUserInputBlocked fires when a UserPromptSubmit hook refuses such a
	// message. Content holds the message and DenyReason the hook's reason; the
	// message is not appended to the history.
	EventUserInputBlocked
)

// Deny reason labels for EventToolDenied.
const (
	DenyReasonPolicy    = "policy"     // blocked by ToolPolicy or tool's own DefaultAction
	DenyReasonUser      = "user"       // user denied the approval prompt
	DenyReasonLoopGuard = "loop_guard" // repeated identical call blocked by loop guard
	DenyReasonHook      = "hook"       // blocked by a PreToolUse hook
	DenyReasonUnknown   = "unknown_tool"
)

// Event is a structured runtime event emitted by RunLoop.
// Passed by value to EventSink; fields not relevant to a given Kind are zero.
type Event struct {
	Kind EventKind

	// EventIterStart, EventLLMStart, EventLLMEnd, EventUserInput, EventUserInputBlocked
	Iter int

	// EventLLMDelta, EventLLMEnd, EventUserInput, EventUserInputBlocked
	Content string

	// EventLLMEnd
	HasToolCalls bool

	// EventLLMStart, EventLLMEnd
	ContextTokens    int
	ContextWindow    int
	PromptTokens     int
	CompletionTokens int
	// CacheReadTokens and CacheWriteTokens are the run's cached prompt so far.
	// They are a breakdown of PromptTokens, not an addition to it.
	CacheReadTokens  int
	CacheWriteTokens int

	// EventLLMEnd
	//
	// The counts above are the run's totals so far; these are what this one
	// call did. Both are carried because they answer different questions —
	// what the run has spent, and which turn spent it — and deriving the
	// second by subtracting consecutive records is a trap for anyone reading a
	// trace where a call failed in between.
	CallUsage llm.Usage
	// CallCost is what this call is estimated to have cost, nil when the model
	// was unpriced. Zero would read as a free call.
	CallCost *llm.Cost

	// EventToolStart, EventToolEnd, EventToolDenied
	ToolName   string
	ToolCallID string
	// ToolArgs is the JSON-encoded arguments, set on EventToolStart only; a
	// consumer pairs them with the later events by ToolCallID.
	ToolArgs string

	// EventToolEnd
	ToolResult   string
	ToolDuration time.Duration
	// ToolErrorKind names how the call failed, empty when it did not.
	//
	// It reports a call that could not complete, not a task that went badly.
	// A tool that ran and reported a bad outcome — a command exiting non-zero,
	// a search matching nothing — succeeded at this boundary, and reading this
	// field as a failure rate would flatter exactly the runs that are going
	// worst.
	ToolErrorKind string

	// EventToolDenied, EventUserInputBlocked
	DenyReason string

	// EventContextCompacted
	//
	// CallUsage and CallCost above are set here too: compaction is a model
	// call the run paid for, so it is priced like any other.
	Summarized int
	Kept       int

	// EventRunEnd
	Stats RunStats
	Err   error // nil on success or graceful cancellation
}

// emit calls sink with e when sink is non-nil.
func emit(sink func(Event), e Event) {
	if sink != nil {
		sink(e)
	}
}

// serializedSink guards a sink so tool workers and the loop goroutine cannot
// call it at once. The guarantee lives here rather than in each consumer --
// the TUI, Desktop, the trace recorder, and Portal would otherwise each have to
// solve it, and one of them would get it wrong.
//
// A nil sink stays nil: no allocation, no lock, no cost for a run that emits
// nothing.
func serializedSink(sink func(Event)) func(Event) {
	if sink == nil {
		return nil
	}
	var mu sync.Mutex
	return func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		sink(e)
	}
}
