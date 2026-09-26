// Package agent provides the core AI agent logic: task planning, tool invocation, and conversation.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/icloudbb/buildmax/internal/core/llm"
)

// How a tool call failed. A denial is not among them: it has its own event and
// its own reason, and a call nobody allowed to run did not fail.
const (
	// ToolErrorInvalidArgs is arguments the model sent that would not parse.
	ToolErrorInvalidArgs = "invalid_args"
	// ToolErrorFailed is the tool itself returning an error.
	ToolErrorFailed = "tool_error"
	// ToolErrorPanic is a tool panicking, which is a defect in BuildMax.
	ToolErrorPanic = "panic"
)

// ErrMaxIterations ends a run that reached its MaxIter bound.
//
// It is a sentinel rather than a message because it is not a failure of the
// same kind as a provider outage: the run spent the budget it was given and
// stopped, and everything it did up to that point stands. A caller that cannot
// tell the two apart reports an exhausted budget as an incapable agent, which
// is the distinction docs/design/evaluation-system.md section 7.4 exists to
// keep.
var ErrMaxIterations = errors.New("max iterations exceeded")

const denyMsgPolicy = "error: tool call %q denied by policy"
const denyMsgLoopGuard = "error: tool call %q blocked — repeated identical call detected (loop guard)"
const denyMsgUser = "error: tool call %q denied by user"
const denyMsgHook = "error: tool call %q denied by hook: %s"

// RunStats holds statistics collected during a single agent run.
//
// CacheReadTokens and CacheWriteTokens break PromptTokens down rather than add
// to it, matching llm.Usage. A surface that sums all three reports a run that
// read more prompt than it sent.
type RunStats struct {
	ToolCalls int
	// WriteToolCalls is how many of ToolCalls actually ran a state-changing
	// tool (DeclaredAccess == AccessWrite). It is what tells a turn that
	// changed something apart from one that only read, without re-deriving the
	// access from the message log; see the turn digest gate in internal/agentapp.
	WriteToolCalls   int
	PromptTokens     int
	CompletionTokens int
	CacheReadTokens  int
	CacheWriteTokens int
	// Cost is the run's estimated spend, nil when no call in it could be
	// priced. CostIncomplete says a call did work that could not be priced, so
	// the total understates the run rather than covering it.
	Cost           *llm.Cost
	CostIncomplete bool
	// Delegated is what subagent runs this one started spent. It is a
	// breakdown of the token and cost fields above, not an addition to them:
	// the totals are what the run cost, whoever executed the calls. Nil when
	// the run delegated nothing.
	Delegated *DelegatedStats
}

// addCall folds one completed call into the run's totals.
func (s *RunStats) addCall(usage llm.Usage, pricing llm.Pricing) *llm.Cost {
	s.PromptTokens += usage.PromptTokens
	s.CompletionTokens += usage.CompletionTokens
	s.CacheReadTokens += usage.CacheReadTokens
	s.CacheWriteTokens += usage.CacheWriteTokens

	call, ok := llm.EstimateCost(usage, pricing)
	if !ok {
		// A call the provider reported no usage for is already unmeasured in
		// the counts above; only one that did work and could not be priced
		// leaves a hole in the money.
		if usage.PromptTokens != 0 || usage.CompletionTokens != 0 {
			s.CostIncomplete = true
		}
		return nil
	}
	if s.Cost == nil {
		total := call
		s.Cost = &total
		return &call
	}
	summed, ok := s.Cost.Add(call)
	if !ok {
		// Two currencies in one run, and there is no exchange rate here to
		// reconcile them with. The earlier total stands and says it is partial.
		s.CostIncomplete = true
		return &call
	}
	*s.Cost = summed
	return &call
}

// MessageHistory is the minimal interface for the agent loop: read the conversation so far and append one message.
// The loop uses it so the same logic works with in-memory session or DB-backed conversation.
type MessageHistory interface {
	HistoryMessages() []llm.Message
	Append(m llm.Message) error
}

// PendingInput supplies messages the user submitted while a run was already
// working. RunLoop drains it at the top of every iteration, where the previous
// iteration's tool results are complete and a user message can be appended
// without breaking the assistant(tool_calls) to tool pairing.
//
// *MessageQueue implements it. A surface that would rather hand queued messages
// to a fresh run leaves RunLoopOpts.PendingInput nil and drains its queue itself.
type PendingInput interface {
	// Dequeue removes and returns the oldest waiting message. The bool is false
	// when nothing is waiting; RunLoop calls it until it reports false.
	Dequeue() (string, bool)
}

// CompactionHistory is an optional extension of MessageHistory implemented by persistent histories
// that can record a compaction boundary across turns. When RunLoop compacts the context, it calls
// AddCompaction so the next turn starts from the compacted view without re-summarizing.
//
// The history owns the summary, and RunLoop reads it back through PriorSummary rather than
// expecting the caller to have folded it into SystemPrompt. That keeps one owner for the
// block: two owners is how the same summary ends up in the prompt twice.
type CompactionHistory interface {
	MessageHistory
	// PriorSummary returns the summary stored by the most recent compaction, or "" when the
	// history has never been compacted.
	PriorSummary() string
	// AddCompaction advances the compaction boundary by summarizedCount messages and stores summary.
	//
	// It returns an error because a durable history commits here. Compaction
	// changes what the model sees, so a boundary that failed to reach storage
	// would leave the next turn reading a different conversation than this one
	// ended with.
	AddCompaction(summary string, summarizedCount int) error
}

// ContextCompactor summarizes a slice of messages into a short text that can replace them.
// The returned summary is injected into the system prompt so the LLM retains prior context.
type ContextCompactor interface {
	// Usage is what the summarization itself cost. It is returned rather than
	// left to the implementation to report because compaction is a model call
	// the run caused: a total that omits it understates exactly the long
	// sessions where it matters most. A compactor that does not call a model
	// returns the zero Usage.
	Compact(ctx context.Context, msgs []llm.Message) (summary string, usage llm.Usage, err error)
}

// StateCheckpointer is handed the messages a compaction is about to discard, and gets one turn
// to move anything still needed into durable session state before they are gone.
//
// This exists because a tool the model has to remember to call will be forgotten exactly when
// context pressure is highest. Compaction is the one moment where the runtime knows
// information is being destroyed, so it is the runtime, not the model, that decides the
// checkpoint happens.
//
// Failure is not fatal: a checkpoint that errors is logged and compaction proceeds.
type StateCheckpointer interface {
	Checkpoint(ctx context.Context, discarded []llm.Message) error
}

// RunLoopOpts configures a single run of the shared agent loop (used by both CLI agent and conversation).
type RunLoopOpts struct {
	LLMClient    llm.LLMClient
	SystemPrompt string
	// Pricing prices each call as it completes. Zero leaves every cost nil,
	// which is what a model nobody priced — or a managed one, where the server
	// holds the rates and records what it charged — reports.
	//
	// It is priced here rather than after the run because a run is where the
	// rates are known to still be the ones that applied: recomputing later from
	// whatever is configured then restates money already spent.
	Pricing      llm.Pricing
	ToolRegistry llm.ToolRegistry
	// MaxIter caps how many times the loop may call the model. The caller
	// settles it — config.ResolveMaxIterations is where a surface gets one —
	// because the bound belongs to the run being asked for, not to the loop.
	// Zero runs nothing and reports the cap as exceeded.
	MaxIter    int
	History    MessageHistory
	StreamSink llm.StreamSink
	// Policy is consulted before each tool execution. Nil defaults to AllowAllPolicy().
	Policy ToolPolicy
	// Approval is invoked when Policy returns ToolActionAsk.
	// Nil approval with ToolActionAsk denies the call: an unattended surface
	// must not widen Ask into Allow.
	Approval ApprovalHandler
	// PendingInput carries messages the user submitted after this run started.
	// They are appended to the history at the next iteration boundary, so they
	// reach the model after the current batch of tool calls rather than after the
	// whole run. Nil disables mid-run injection entirely.
	PendingInput PendingInput
	// Grants holds approvals the user chose to keep for the session. It is owned
	// by the caller because a session outlives one RunLoop; nil grants nothing,
	// which makes every Ask a fresh prompt.
	Grants *SessionGrants
	// MaxParallelTools bounds how many calls from one assistant message may be
	// grouped to run together. Zero or one keeps every call in its own group,
	// which is the sequential behaviour and the current default on every
	// surface. See docs/design/parallel-tool-execution.md.
	MaxParallelTools int
	// Compactor summarizes old messages when the context window is filling up.
	// Nil disables compaction; TrimHistory is used as a fallback.
	Compactor ContextCompactor
	// Checkpointer is given one turn to save durable state before a compaction discards
	// messages. Nil skips the checkpoint; compaction is unaffected either way.
	Checkpointer StateCheckpointer
	// Invariants is the hard-constraint section of the run's additional system prompt, restated
	// after the message list on every call. That text is already in SystemPrompt and never
	// leaves it; this is about proximity, not storage, so it carries only the part the author
	// marked as non-negotiable. Empty is the normal case.
	Invariants string
	// Memory supplies the resident index of cross-session recall, rendered
	// after the message list and before the session-state anchor. Nil is the
	// normal case for a run that has no such scope -- a worker, an evaluation,
	// a subagent, a session whose user turned memory off -- and costs nothing.
	Memory MemoryStore
	// EventSink receives structured runtime events from the agent loop.
	// Nil disables event emission entirely (zero overhead).
	// The callback may be invoked from the RunLoop goroutine or from a tool
	// worker. The runtime serialises the calls, so a sink sees one event at a
	// time, but it must not block and must not assume one tool is in flight:
	// pair EventToolStart with EventToolEnd by ToolCallID, not by arrival.
	EventSink func(Event)
	// Hooks runs lifecycle hooks at fixed points (PreToolUse, PostToolUse,
	// PostToolUseFailure, Notification, PreCompact, PostCompact, Stop /
	// SubagentStop / StopFailure). Nil disables hooks.
	// PreToolUse and PreCompact hooks may block their respective actions.
	Hooks HookRunner
	// SessionID is forwarded to hook payloads so external scripts can correlate runs.
	// Optional; an empty value is omitted from hook input.
	SessionID string
	// Workspace is forwarded to hook payloads so external scripts can locate files
	// under the active workspace. Optional.
	Workspace string
	// IsSubagent is true when this RunLoop is a subagent execution. It
	// flips the lifecycle event on success from Stop to SubagentStop and
	// is stamped on every event from this run for audit attribution.
	IsSubagent bool
	// AgentType is the subagent definition name when IsSubagent is true.
	// Empty for main-agent runs.
	AgentType string
	// RedactResult removes a run's materialized Space Secret values from a tool
	// result before it enters the model context, the trace, the hooks, and the
	// application log. It is a plain function so this package depends on no
	// redactor; agentapp supplies one built from the run's grant values. Nil
	// leaves results unredacted, which is what every surface with no Secret
	// grants passes. See docs/design/space-secrets.md §12.
	RedactResult func(string) string
	// Output, when set, asks the run for a machine-readable final answer that
	// satisfies the schema. It constrains only the terminating answer, not the
	// tool-calling turns on the way there: once the model produces an answer with
	// no tool calls, RunLoop makes one additional constrained call to render that
	// answer as the structured value (docs/design/structured-output.md §5). Nil is
	// free text — today's behavior. The value comes back from RunLoop beside the
	// reply, validated or a typed failure.
	Output *llm.OutputSchema
}

// RunLoop runs the LLM loop once: build messages from history, call LLM, handle tool_calls, append to history, repeat until final reply.
// It is used by Agent.processLoop (with session history) and by conversation.Run (with DB-backed history).
// When ctx is cancelled mid-run, RunLoop returns the last assistant content produced (if any) and a nil error,
// so callers receive a partial result rather than an empty failure.
func RunLoop(ctx context.Context, opts RunLoopOpts) (reply string, stats RunStats, structured *llm.Structured, err error) {
	opts.EventSink = serializedSink(opts.EventSink)
	var s RunStats
	// Installed before any tool can run. A subagent reports its totals here
	// rather than through the tool interface, and its own delegations accrue
	// to its own loop's accumulator, not this one.
	delegated := &DelegatedUsage{}
	ctx = CtxWithDelegatedUsage(ctx, delegated)
	guard := newLoopGuard(defaultMaxRepeatedCalls)
	// Most recent compaction summary, rendered into the system prompt when non-empty.
	// Seeded from the history so a session compacted in an earlier turn keeps its summary
	// and feeds it back into the next compaction.
	compactionSummary := ""
	if ch, ok := opts.History.(CompactionHistory); ok {
		compactionSummary = ch.PriorSummary()
	}
	// Every non-empty assistant turn, in order. The reply is their join, not just
	// the last: an Agent's TaskRun is the one surface that shows a run's detail,
	// so text the model wrote before a tool call (its narration of what it is
	// about to do) must survive into the run output, not only the final answer.
	// Orchestrators above the Agent — a Conversation, a future Assistant — show
	// their own history and drill into this for the detail; the Agent itself has
	// nothing lower to defer to, so it keeps everything.
	var assistantTexts []string

	for i := 0; i < opts.MaxIter; i++ {
		slog.Debug("agent run loop iteration", "iter", i+1, "max", opts.MaxIter)
		emit(opts.EventSink, Event{Kind: EventIterStart, Iter: i + 1})

		// Before the history is read, so a message that arrived mid-run is part of
		// what this iteration reasons about — and before compaction, so it counts
		// toward the context pressure that decides whether to compact.
		if err := injectPendingInput(ctx, opts, i+1); err != nil {
			return "", s, nil, err
		}

		history := opts.History.HistoryMessages()

		// Context compaction: summarize old messages before the context window fills up.
		if opts.Compactor != nil {
			if cw := opts.LLMClient.ContextWindow(); cw > 0 {
				sysTokens := EstimateMessageTokens(llm.Message{Role: "system", Content: opts.SystemPrompt})
				if sysTokens+EstimateTokens(history) > int(float64(cw)*compactionThreshold) {
					kept, res, cerr := compactOnce(ctx, opts, &s, i+1, compactionSummary, history, int(float64(cw)*compactionReserve))
					switch {
					case errors.Is(cerr, ErrCompactionNotPersisted):
						// The summary landed nowhere, so the next turn would
						// re-send the messages it covers as if it never ran.
						return "", s, nil, cerr
					case cerr != nil:
						slog.Warn("context compaction failed, falling back to trim", "err", cerr)
					case res.Compacted():
						history = kept
						compactionSummary = res.Summary
					}
				}
			}
		}

		// Build effective system prompt: base + compaction summary when present.
		// SystemPrompt must not already carry the block — RunLoop is its only renderer.
		effectiveSysPrompt := opts.SystemPrompt + RenderCompactionBlock(compactionSummary)

		completion, err := callLLM(ctx, opts, history, effectiveSysPrompt, i+1, s)
		content, toolCalls := completion.Content, completion.ToolCalls
		if err != nil {
			if ctx.Err() != nil {
				slog.Warn("agent run interrupted by context cancellation", "iter", i+1, "turns", len(assistantTexts))
				emit(opts.EventSink, Event{Kind: EventRunEnd, Stats: s})
				fireRunEndHook(ctx, opts, s, nil)
				return strings.Join(assistantTexts, "\n\n"), s, nil, nil
			}
			slog.Error("LLM call failed", "err", err)
			runErr := fmt.Errorf("llm call: %w", err)
			emit(opts.EventSink, Event{Kind: EventRunEnd, Stats: s, Err: runErr})
			fireRunEndHook(ctx, opts, s, runErr)
			return "", s, nil, runErr
		}
		callCost := s.addCall(completion.Usage, opts.Pricing)

		if content != "" {
			assistantTexts = append(assistantTexts, content)
			// Mirror the join separator into the live stream, so a run that
			// narrates before a tool call and reports after it reads as two
			// blocks while streaming, matching the persisted output rather than
			// running the two together.
			if len(toolCalls) > 0 && opts.StreamSink != nil {
				opts.StreamSink.OnDelta("\n\n")
			}
		}

		emit(opts.EventSink, Event{
			Kind:             EventLLMEnd,
			Iter:             i + 1,
			Content:          content,
			HasToolCalls:     len(toolCalls) > 0,
			PromptTokens:     s.PromptTokens,
			CompletionTokens: s.CompletionTokens,
			CacheReadTokens:  s.CacheReadTokens,
			CacheWriteTokens: s.CacheWriteTokens,
			CallUsage:        completion.Usage,
			CallCost:         callCost,
		})

		if len(toolCalls) == 0 {
			slog.Debug("agent reply", "content", content)
			if err := opts.History.Append(completion.AssistantMessage()); err != nil {
				return "", s, nil, err
			}
			// The model reached a terminating answer. When the run asked for
			// structured output, render that settled answer as the schema value
			// with one more constrained call (§5) — done here, not on the loop's
			// calls, so it never fights tool use on a provider that maps output to
			// a forced tool.
			if opts.Output != nil {
				structured = extractStructured(ctx, opts, effectiveSysPrompt, &s)
			}
			emit(opts.EventSink, Event{Kind: EventRunEnd, Stats: s})
			fireRunEndHook(ctx, opts, s, nil)
			return strings.Join(assistantTexts, "\n\n"), s, structured, nil
		}

		slog.Debug("tool calls", "n", len(toolCalls), "content", content, "calls", toolCallsSummary(toolCalls))
		if err := opts.History.Append(completion.AssistantMessage()); err != nil {
			return "", s, nil, err
		}

		// Tools that write durable state stamp entries with the iteration they were written at.
		n, writes, err := executeToolCalls(CtxWithIteration(ctx, i+1), opts, toolCalls, guard)
		s.ToolCalls += n
		s.WriteToolCalls += writes
		// Drained here rather than at the exits so every path out of the loop
		// — reply, cancellation, error, iteration cap — reports the same
		// totals, including whatever a delegation spent on the way.
		s.absorb(delegated.Drain())
		if err != nil {
			return "", s, nil, err
		}
	}
	slog.Warn("agent max iterations exceeded", "max", opts.MaxIter)
	emit(opts.EventSink, Event{Kind: EventRunEnd, Stats: s, Err: ErrMaxIterations})
	fireRunEndHook(ctx, opts, s, ErrMaxIterations)
	return "", s, nil, ErrMaxIterations
}

// injectPendingInput appends every message waiting in opts.PendingInput to the
// history as a user message.
//
// Each one goes through the UserPromptSubmit hook first. A prompt that arrives
// mid-run is still a prompt: a hook that inspects what the user sends must not be
// bypassed by the path that happens to arrive late. A blocked message is dropped
// with an event rather than appended, which leaves the run working on what it
// already had.
func injectPendingInput(ctx context.Context, opts RunLoopOpts, iter int) error {
	if opts.PendingInput == nil {
		return nil
	}
	for {
		text, ok := opts.PendingInput.Dequeue()
		if !ok {
			return nil
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		in := baseHookInput(opts, HookUserPromptSubmit)
		in.Prompt = text
		if out := runHook(ctx, opts.Hooks, in); out.Blocked() {
			reason := out.Reason
			if reason == "" {
				reason = "prompt blocked by hook"
			}
			slog.Info("queued message blocked by hook", "iter", iter, "reason", reason)
			emit(opts.EventSink, Event{
				Kind:       EventUserInputBlocked,
				Iter:       iter,
				Content:    text,
				DenyReason: reason,
			})
			continue
		}
		if err := opts.History.Append(llm.Message{Role: "user", Content: text}); err != nil {
			return fmt.Errorf("append queued message: %w", err)
		}
		slog.Info("queued message injected", "iter", iter)
		emit(opts.EventSink, Event{Kind: EventUserInput, Iter: iter, Content: text})
	}
}

// checkpointAndCompact gives the checkpointer one turn to move anything still needed out of the
// messages about to be discarded, then summarizes them.
//
// The checkpoint runs first because after Compact returns, the material is only reachable
// through a lossy summary. Its failure is logged and ignored: losing the checkpoint costs some
// context, but skipping the compaction it guards would cost the run.
func checkpointAndCompact(ctx context.Context, opts RunLoopOpts, iter int, priorSummary string, toSummarize []llm.Message) (string, llm.Usage, error) {
	if opts.Checkpointer != nil {
		// The iteration goes on the context so notes written here are stamped like any other.
		if err := opts.Checkpointer.Checkpoint(CtxWithIteration(ctx, iter), toSummarize); err != nil {
			slog.Warn("state checkpoint before compaction failed", "iter", iter, "err", err)
		}
	}
	return opts.Compactor.Compact(ctx, withPriorSummary(priorSummary, toSummarize))
}

// fireRunEndHook invokes the lifecycle hook for a finished run. Routes to
// HookStop (main success), HookSubagentStop (subagent success), or
// HookStopFailure (any error). Decisions are ignored — these events are
// advisory; hook failures are swallowed by the runner.
func fireRunEndHook(ctx context.Context, opts RunLoopOpts, stats RunStats, err error) {
	if opts.Hooks == nil {
		return
	}
	event := HookStop
	switch {
	case err != nil:
		event = HookStopFailure
	case opts.IsSubagent:
		event = HookSubagentStop
	}
	in := HookInput{
		Event:      event,
		SessionID:  opts.SessionID,
		Workspace:  opts.Workspace,
		IsSubagent: opts.IsSubagent,
		AgentType:  opts.AgentType,
		Stats:      &stats,
	}
	if err != nil {
		in.Error = err.Error()
	}
	runHook(ctx, opts.Hooks, in)
}

// callLLM dispatches to streaming or blocking LLM call based on whether a StreamSink is set.
// When EventSink is also set, content deltas are forwarded to it as EventLLMDelta events.
// history and systemPrompt are passed explicitly so the caller can inject a compacted view.
func callLLM(ctx context.Context, opts RunLoopOpts, history []llm.Message, systemPrompt string, iter int, stats RunStats) (llm.Completion, error) {
	// Durable session state is rendered fresh on every call and placed after the messages, so
	// it is never subject to trimming and never accumulates in the history. An empty block
	// renders nothing, which is what a run that keeps no state should cost.
	stateMsg := sessionStateMessages(opts)

	systemTokens := EstimateMessageTokens(llm.Message{Role: "system", Content: systemPrompt}) + EstimateTokens(stateMsg)
	contextWindow := opts.LLMClient.ContextWindow()
	if contextWindow > 0 {
		history = TrimHistory(history, systemTokens, contextWindow, 0)
	}
	contextTokens := systemTokens + EstimateTokens(history)
	emit(opts.EventSink, Event{
		Kind:             EventLLMStart,
		Iter:             iter,
		ContextTokens:    contextTokens,
		ContextWindow:    contextWindow,
		PromptTokens:     stats.PromptTokens,
		CompletionTokens: stats.CompletionTokens,
		CacheReadTokens:  stats.CacheReadTokens,
		CacheWriteTokens: stats.CacheWriteTokens,
	})
	messages := assembleMessages(systemPrompt, history, stateMsg)
	// The loop is the one caller whose prefix is sent again on the next
	// iteration, which is what makes a cache write here worth its price.
	call := llm.Request{Messages: messages, Tools: opts.ToolRegistry.GetDefs(), Profile: llm.ProfileAgentTurn}
	if opts.StreamSink != nil {
		onDelta := opts.StreamSink.OnDelta
		if opts.EventSink != nil {
			sink := opts.EventSink
			onDelta = func(delta string) {
				opts.StreamSink.OnDelta(delta)
				sink(Event{Kind: EventLLMDelta, Content: delta})
			}
		}
		return opts.LLMClient.ChatCompletionStreaming(ctx, call, onDelta)
	}
	return opts.LLMClient.ChatCompletionBlocking(ctx, call)
}

// sessionStateMessages renders the durable state placed after the history on
// every call: shared memory first, then this session's invariants, notes, and
// todos. Both are rebuilt per call so another session's committed write is
// visible on the next call rather than at the end of the run.
func sessionStateMessages(opts RunLoopOpts) []llm.Message {
	var notes []Note
	var todos []Todo
	if nh, ok := opts.History.(NotesHistory); ok {
		notes, todos = nh.Notes(), nh.Todos()
	}
	var stateMsg []llm.Message
	if opts.Memory != nil {
		if block := RenderMemoryIndex(opts.Memory.Index()); block != "" {
			stateMsg = append(stateMsg, llm.Message{Role: "user", Content: block})
		}
	}
	if block := RenderSessionState(opts.Invariants, notes, todos); block != "" {
		stateMsg = append(stateMsg, llm.Message{Role: "user", Content: block})
	}
	return stateMsg
}

// assembleMessages builds the request message list: the system prompt, the
// history, then the durable state block that must never be trimmed.
func assembleMessages(systemPrompt string, history, stateMsg []llm.Message) []llm.Message {
	messages := append([]llm.Message{{Role: "system", Content: systemPrompt}}, history...)
	return append(messages, stateMsg...)
}

// extractStructured makes the one additional call that renders the run's settled
// answer as the schema-constrained value (docs/design/structured-output.md §5).
// It runs after the model produced a terminating answer, so history already
// carries that answer; the model is asked only to express it in the required
// shape. No tools are offered — the run is reporting, not acting — and the call
// does not stream, since a half-built value is never presented (§10). The Client
// validates the result, so this returns a validated value or a typed failure;
// a transport error becomes a typed failure too, because the text answer already
// stands and a broken extraction must not fail the whole run.
func extractStructured(ctx context.Context, opts RunLoopOpts, systemPrompt string, stats *RunStats) *llm.Structured {
	history := opts.History.HistoryMessages()
	if cw := opts.LLMClient.ContextWindow(); cw > 0 {
		sysTokens := EstimateMessageTokens(llm.Message{Role: "system", Content: systemPrompt})
		history = TrimHistory(history, sysTokens, cw, 0)
	}
	messages := assembleMessages(systemPrompt, history, sessionStateMessages(opts))
	// ProfileProbe: this prefix is never sent again, so it must not pay to write
	// a cache it can only read from.
	call := llm.Request{Messages: messages, Profile: llm.ProfileProbe, Output: opts.Output}
	completion, err := opts.LLMClient.ChatCompletionBlocking(ctx, call)
	if err != nil {
		return &llm.Structured{Err: &llm.StructuredError{Message: "structured-output extraction call failed: " + err.Error()}}
	}
	stats.addCall(completion.Usage, opts.Pricing)
	if completion.Structured == nil {
		return &llm.Structured{Err: &llm.StructuredError{Message: "provider returned no structured value"}}
	}
	return completion.Structured
}

// pendingCall is one tool call moving through the four stages below. Workers
// write only to their own element, so the stages need no lock between them.
type pendingCall struct {
	call   llm.ToolCall
	args   map[string]any
	tool   llm.Tool // nil when the registry has no such tool
	result string
	// decided is true once result is final without executing: bad arguments,
	// an unknown tool, the loop guard, or a permission denial.
	decided bool
	// parts is non-text content the tool returned, carried to the history
	// message. Only the MCP gateway sets it today.
	parts []llm.ContentPart
	// executed records what the run stage did, so the commit stage can fire
	// the right post hook without re-deriving it from the result.
	executed bool
	// errKind names how the call failed, empty when it did not. It is a
	// classification rather than a flag because the kinds mean different
	// things to whoever reads them: bad arguments are the model misusing a
	// tool, a panic is a defect here, and an error is usually the environment.
	errKind string
}

// executeToolCalls runs the calls from one assistant message in four stages:
// parse, gate, run, commit. Parse has no side effects, so it covers the whole
// batch; the other three run per group.
//
// The shape exists for docs/design/parallel-tool-execution.md, where a group's
// calls overlap. Today every group holds one call and the effect is the
// sequential loop this replaced.
func executeToolCalls(ctx context.Context, opts RunLoopOpts, toolCalls []llm.ToolCall, guard *loopGuard) (count, writes int, err error) {
	policy := opts.Policy
	if policy == nil {
		policy = AllowAllPolicy()
	}
	pending := parseCalls(opts, toolCalls)

	for _, group := range groupCalls(pending, opts.MaxParallelTools) {
		for i := range group {
			gateCall(ctx, opts, policy, guard, &group[i])
		}
		// Between gate and run: the approved calls are recorded as about to
		// cross into their tools, and that record is durable before any of them
		// does. A failure here stops the turn rather than running a tool whose
		// outcome could not be classified afterwards.
		if err := recordToolBoundary(opts, group); err != nil {
			return count, writes, err
		}
		runGroup(ctx, opts, group)
		for i := range group {
			c := &group[i]
			firePostHook(ctx, opts, c)
			logToolResult(c.call.Name, c.result)
			if err := appendToolOutcome(opts, c); err != nil {
				return count, writes, err
			}
			count++
			// Counted only when the tool actually ran: a denied or errored-out
			// write changed nothing, and the digest gate reads this to decide
			// whether the turn had a change worth naming.
			if c.executed && DeclaredAccess(c.tool, c.args) == llm.AccessWrite {
				writes++
			}
		}
	}
	return count, writes, nil
}

// parseCalls unmarshals arguments and resolves tools. Both are side-effect
// free, which is what lets them run ahead of any execution: grouping needs the
// arguments to ask a tool what the call does.
func parseCalls(opts RunLoopOpts, toolCalls []llm.ToolCall) []pendingCall {
	out := make([]pendingCall, len(toolCalls))
	for i, tc := range toolCalls {
		c := pendingCall{call: tc}
		if tc.Arguments != "" {
			if err := json.Unmarshal([]byte(tc.Arguments), &c.args); err != nil {
				c.result = fmt.Sprintf("error: invalid arguments: %v", err)
				c.decided = true
				c.errKind = ToolErrorInvalidArgs
				out[i] = c
				continue
			}
		}
		if c.args == nil {
			c.args = make(map[string]any)
		}
		c.tool = opts.ToolRegistry.Lookup(tc.Name)
		out[i] = c
	}
	return out
}

// groupCalls cuts the batch into units that execute together. Groups are
// windows into calls, not copies, so the stages mutate the original elements.
//
// Calls are never reordered: only adjacent read-only calls merge, and every
// other call is a barrier. Reordering would change what a batch means —
// [Write a, Read a] is not [Read a, Write a].
func groupCalls(calls []pendingCall, maxParallel int) [][]pendingCall {
	if maxParallel <= 1 {
		groups := make([][]pendingCall, 0, len(calls))
		for i := range calls {
			groups = append(groups, calls[i:i+1])
		}
		return groups
	}
	var groups [][]pendingCall
	start := 0
	for i := range calls {
		if eligibleForGroup(&calls[i]) && i+1 < len(calls) && eligibleForGroup(&calls[i+1]) {
			continue
		}
		groups = append(groups, calls[start:i+1])
		start = i + 1
	}
	return groups
}

// eligibleForGroup reports whether a call may overlap its neighbours. A call
// already decided keeps its own group so its ordering is unambiguous.
func eligibleForGroup(c *pendingCall) bool {
	if c.decided || c.tool == nil {
		return false
	}
	return DeclaredAccess(c.tool, c.args) == llm.AccessReadOnly
}

// executeTool runs a tool, taking its multimodal result when it has one.
//
// A tool that returns non-text content still returns text describing it, so the
// caller does not branch: everything downstream reads the text, and only the
// history message carries the parts.
func executeTool(ctx context.Context, tool llm.Tool, args map[string]any) (string, []llm.ContentPart, error) {
	if multimodal, ok := tool.(llm.MultimodalTool); ok {
		out, err := multimodal.ExecuteMultimodal(ctx, args)
		return out.Text, out.Parts, err
	}
	result, err := tool.Execute(ctx, args)
	return result, nil, err
}

// gateCall makes every decision that must happen in call order, on the loop
// goroutine: the tool exists, the loop guard has not fired, the permission
// layer allows it, the user approved it, and no PreToolUse hook blocked it.
//
// Deciding serially is what keeps concurrency cheap. The loop guard needs no
// lock, approval prompts stay one at a time so neither UI handler has to become
// re-entrant, and hooks still see calls in the order the model made them. Only
// Execute overlaps. See docs/design/parallel-tool-execution.md §5.4.
func gateCall(ctx context.Context, opts RunLoopOpts, policy ToolPolicy, guard *loopGuard, c *pendingCall) {
	if c.decided {
		// Parsing decided this one before it could be gated. Both records are
		// still written: tool_start already means "the model issued this call"
		// rather than "execution began" — a denied call emits one too — and a
		// failure that appears in the history but not the trace is invisible
		// exactly where someone would look for it.
		if c.errKind != "" {
			emit(opts.EventSink, Event{
				Kind:       EventToolStart,
				ToolName:   c.call.Name,
				ToolCallID: c.call.ID,
				ToolArgs:   c.call.Arguments,
			})
			emit(opts.EventSink, Event{
				Kind:          EventToolEnd,
				ToolName:      c.call.Name,
				ToolCallID:    c.call.ID,
				ToolResult:    c.result,
				ToolErrorKind: c.errKind,
			})
		}
		return
	}
	name, callID := c.call.Name, c.call.ID
	emit(opts.EventSink, Event{
		Kind:       EventToolStart,
		ToolName:   name,
		ToolCallID: callID,
		ToolArgs:   c.call.Arguments,
	})
	deny := func(reason, result string) {
		emit(opts.EventSink, Event{Kind: EventToolDenied, ToolName: name, ToolCallID: callID, DenyReason: reason})
		c.result = result
		c.decided = true
	}
	switch {
	case c.tool == nil:
		deny(DenyReasonUnknown, fmt.Sprintf("error: unknown tool %q", name))
		return
	case guard.exceeded(name, c.args):
		slog.Warn("loop guard triggered", "tool", name)
		deny(DenyReasonLoopGuard, fmt.Sprintf(denyMsgLoopGuard, name))
		return
	}

	target := grantTarget(c.tool, c.args)
	scope := grantScope(name, target)
	action := resolveAction(policy, c.tool, name, scope, c.args, opts.interactive())
	// A session grant answers an Ask that was already put to the user. It is
	// applied here rather than before resolution so it can never soften a Deny.
	if action == llm.ToolActionAsk && opts.Grants.granted(scope) {
		action = llm.ToolActionAllow
	}
	switch action {
	case llm.ToolActionDeny:
		slog.Info("tool denied by policy", "tool", name)
		deny(DenyReasonPolicy, fmt.Sprintf(denyMsgPolicy, name))
		return
	case llm.ToolActionAsk:
		// Notify hooks before invoking the approval handler so external
		// systems (Slack, desktop badge, audit) see the prompt.
		fireNotification(ctx, opts, NotificationApprovalRequired, name, callID, c.args, "")
		if opts.Approval == nil {
			slog.Info("tool denied: Ask with no approval handler", "tool", name)
			deny(DenyReasonPolicy, fmt.Sprintf(denyMsgPolicy, name))
			fireNotification(ctx, opts, NotificationPermissionDenied, name, callID, c.args, "no approval handler configured")
			return
		}
		decision := opts.Approval.RequestApproval(ctx, name, c.args, target)
		if decision == ApprovalDeny {
			slog.Info("tool denied by user", "tool", name)
			deny(DenyReasonUser, fmt.Sprintf(denyMsgUser, name))
			fireNotification(ctx, opts, NotificationPermissionDenied, name, callID, c.args, "denied by user")
			return
		}
		if decision == ApprovalAllowSession {
			opts.Grants.grant(scope)
			slog.Info("tool granted for session", "tool", name, "scope", scope)
		}
	case llm.ToolActionAllow:
	default:
		c.result = fmt.Sprintf("error: unknown policy action for %q", name)
		c.decided = true
		return
	}

	pre := baseHookInput(opts, HookPreToolUse)
	pre.ToolName = name
	pre.ToolCallID = callID
	pre.ToolArgs = c.args
	if preOut := runHook(ctx, opts.Hooks, pre); preOut.Blocked() {
		reason := preOut.Reason
		if reason == "" {
			reason = "blocked by hook"
		}
		slog.Info("tool denied by hook", "tool", name, "reason", reason)
		deny(DenyReasonHook, fmt.Sprintf(denyMsgHook, name, reason))
	}
}

// runGroup executes the calls the gate let through, up to maxParallel at once.
//
// Each worker writes only to its own element and wg.Wait is the happens-before
// edge before the commit stage reads them, so the results need no lock.
func runGroup(ctx context.Context, opts RunLoopOpts, group []pendingCall) {
	limit := opts.MaxParallelTools
	if limit < 1 || len(group) == 1 {
		for i := range group {
			executeCall(ctx, opts, &group[i])
		}
		return
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i := range group {
		if group[i].decided {
			continue
		}
		wg.Add(1)
		go func(c *pendingCall) {
			defer wg.Done()
			defer func() {
				// A panicking tool must not take the run down or strand its
				// siblings, which are mid-flight in their own goroutines.
				if r := recover(); r != nil {
					slog.Error("tool panicked", "tool", c.call.Name, "panic", r)
					c.result = fmt.Sprintf("error: tool %q panicked: %v", c.call.Name, r)
					c.executed, c.errKind = true, ToolErrorPanic
				}
			}()
			sem <- struct{}{}
			defer func() { <-sem }()
			executeCall(ctx, opts, c)
		}(&group[i])
	}
	wg.Wait()
}

// executeCall runs one tool and records what happened. Emitted from whichever
// goroutine ran it, because the event stream is live: holding a completion
// until the slowest sibling returns would misreport what is still running.
func executeCall(ctx context.Context, opts RunLoopOpts, c *pendingCall) {
	if c.decided {
		return
	}
	// Stamp the call identity so work the tool detaches (a subagent trace, a
	// background job) can record which call launched it.
	ctx = CtxWithToolCall(ctx, c.call.ID)
	start := time.Now()
	result, parts, err := executeTool(ctx, c.tool, c.args)
	dur := time.Since(start)
	c.executed = true
	if err != nil {
		c.errKind = ToolErrorFailed
		c.result = fmt.Sprintf("error: %v", err)
	} else {
		c.result, c.parts = result, parts
	}
	// Redact the run's Secret values before c.result reaches anything: the
	// EventToolEnd below, the model context, the hooks, and the log all read
	// it, so redacting once here covers every downstream sink. See
	// docs/design/space-secrets.md §12.
	if opts.RedactResult != nil {
		c.result = opts.RedactResult(c.result)
	}
	emit(opts.EventSink, Event{
		Kind:          EventToolEnd,
		ToolName:      c.call.Name,
		ToolCallID:    c.call.ID,
		ToolResult:    c.result,
		ToolDuration:  dur,
		ToolErrorKind: c.errKind,
	})
}

// firePostHook reports one finished call. Fired at the join in call order
// rather than from the worker: a post hook is advisory, but it is also an audit
// surface, and one that reorders under load is worse than one that arrives a
// few hundred milliseconds late.
func firePostHook(ctx context.Context, opts RunLoopOpts, c *pendingCall) {
	if !c.executed {
		return
	}
	event := HookPostToolUse
	if c.errKind != "" {
		event = HookPostToolUseFailure
	}
	in := baseHookInput(opts, event)
	in.ToolName = c.call.Name
	in.ToolCallID = c.call.ID
	in.ToolArgs = c.args
	if c.errKind != "" {
		in.ToolError = c.result
	} else {
		in.ToolResult = c.result
	}
	runHook(ctx, opts.Hooks, in)
}

// fireNotification emits a HookNotification event tied to a tool/approval
// flow. Notification is advisory — the returned decision is ignored.
func fireNotification(ctx context.Context, opts RunLoopOpts, kind, toolName, callID string, args map[string]any, reason string) {
	if opts.Hooks == nil {
		return
	}
	in := baseHookInput(opts, HookNotification)
	in.NotificationKind = kind
	in.NotificationReason = reason
	in.ToolName = toolName
	in.ToolCallID = callID
	in.ToolArgs = args
	runHook(ctx, opts.Hooks, in)
}

// resolveAction applies the layered policy resolution for one tool call.
//
// interactive gates layer 4 and nothing else: a category default is a question
// for a user, so a surface with nobody attached does not ask it rather than
// answering it with a default. See docs/design/tool-permissions.md §5.3.
func resolveAction(policy ToolPolicy, tool llm.Tool, name, scope string, args map[string]any, interactive bool) llm.ToolAction {
	configured, hasConfig := llm.ToolActionAllow, false
	if policy != nil {
		configured, hasConfig = policy.Check(name, scope, args)
	}

	// 1. A configured deny is a prohibition and wins outright.
	if hasConfig && configured == llm.ToolActionDeny {
		return llm.ToolActionDeny
	}
	// 2. Tool arg-level check. Deliberately above the configured allow: "stop
	//    asking me about Read" is a statement about the category, not consent to
	//    open ~/.ssh/id_rsa unannounced. Only a configured deny outranks it.
	if checker, ok := tool.(llm.ArgChecker); ok {
		if action := checker.CheckArgs(args); action != llm.ToolActionAllow {
			return action
		}
	}
	// 3. Configured preference for the category, above the tool's own default
	//    because the user outranks the tool author.
	//
	//    A configured Ask is not gated on the surface the way the derived tier
	//    is. The derived tier is the runtime guessing, and guessing a worker
	//    into uselessness helps nobody; a configured Ask is a person saying
	//    "somebody look at this", and where nobody can, the honest answer is to
	//    refuse. It reaches the existing Ask-without-a-handler denial.
	if hasConfig {
		return configured
	}
	// 4. Explicit tool default, overriding the derivation below.
	if provider, ok := tool.(llm.PolicyProvider); ok {
		return provider.DefaultAction()
	}
	// 5. Derived from the tool's declared access: writing calls ask.
	if interactive && DeclaredAccess(tool, args) == llm.AccessWrite {
		return llm.ToolActionAsk
	}
	return llm.ToolActionAllow
}

// ResolveToolAction reports the action one tool call resolves to, without
// executing or prompting. An Ask returned here is a question that would be
// asked, not an outcome: with no handler the loop turns it into a denial.
func ResolveToolAction(policy ToolPolicy, tool llm.Tool, args map[string]any, interactive bool) llm.ToolAction {
	if args == nil {
		args = map[string]any{}
	}
	name := tool.Name()
	return resolveAction(policy, tool, name, grantScope(name, grantTarget(tool, args)), args, interactive)
}

// grantTarget is the tool's own narrowing of a session grant — an MCP
// server/tool, a browser origin — or empty when a grant covers every call.
func grantTarget(tool llm.Tool, args map[string]any) string {
	if s, ok := tool.(llm.GrantScoper); ok {
		return s.GrantScope(args)
	}
	return ""
}

// grantScope is what one session grant covers. Defaults to the tool name, which
// is what the prompt showed the user; a dispatching tool narrows it so one
// approval does not cover every target it can reach, and the prompt shows that
// target so the user sees what "allow for session" covers.
func grantScope(name, target string) string {
	if target != "" {
		return name + ":" + target
	}
	return name
}

// DeclaredAccess returns what a tool says this call does. A tool that declares
// nothing is llm.AccessWrite.
func DeclaredAccess(tool llm.Tool, args map[string]any) llm.Access {
	if d, ok := tool.(llm.AccessDeclarer); ok {
		return d.Access(args)
	}
	return llm.AccessWrite
}

func logToolResult(name, result string) {
	if len(result) > 500 {
		slog.Debug("tool result", "tool", name, "content", result[:500]+"...")
	} else {
		slog.Debug("tool result", "tool", name, "content", result)
	}
}

// ExecuteTool parses tc.Arguments and calls tool.Execute, returning the result or an error string.
// Useful for testing and single-call invocations that bypass the policy layer.
func ExecuteTool(ctx context.Context, t llm.Tool, tc llm.ToolCall) string {
	var args map[string]any
	if tc.Arguments != "" {
		if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
			return fmt.Sprintf("error: invalid arguments: %v", err)
		}
	}
	if args == nil {
		args = make(map[string]any)
	}
	result, err := t.Execute(ctx, args)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	return result
}

func toolCallsSummary(calls []llm.ToolCall) []string {
	s := make([]string, 0, len(calls))
	for _, tc := range calls {
		args := tc.Arguments
		if len(args) > 80 {
			args = args[:80] + "..."
		}
		s = append(s, tc.Name+": "+strings.TrimSpace(args))
	}
	return s
}
