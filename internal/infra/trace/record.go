package trace

import (
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/icloudbb/buildmax/internal/core/agent"
	"github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/icloudbb/buildmax/internal/core/plugin"
	"github.com/icloudbb/buildmax/internal/util/secretscan"
)

// traceVersion is stamped on the run_start record so future readers can detect
// the schema generation.
const traceVersion = 1

// defaultMaxFieldBytes bounds each free-text field (content/args/result) so a
// single large tool result cannot bloat the trace file.
const defaultMaxFieldBytes = 4096

// Record is one line in a run's JSONL trace. Fields irrelevant to a given
// record type are omitted. Keys are snake_case per CLAUDE.md §6.1.
type Record struct {
	TS   string `json:"ts"`
	Type string `json:"type"`

	// run_start
	RunID      string `json:"run_id,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	Workspace  string `json:"workspace,omitempty"`
	Model      string `json:"model,omitempty"`
	IsSubagent bool   `json:"is_subagent,omitempty"`
	// ParentRunID is the immediate trace run that delegated this subagent.
	// Top-level runs leave it absent: an empty value would look like a broken
	// link rather than a run that has no parent.
	ParentRunID string `json:"parent_run_id,omitempty"`
	// ParentToolCallID is the tool call in the parent run that launched this
	// one, closing the walk parent run → tool call → child run.
	ParentToolCallID string `json:"parent_tool_call_id,omitempty"`
	TraceVersion     int    `json:"trace_version,omitempty"`
	// CreatedBy, CreatedByType, TriggerSource, and RetryOfTaskRunID are a
	// TaskRun's provenance, carried through verbatim from the worker that
	// executed it. A subagent's own run_start omits them: its parent_run_id
	// already reaches the top-level record that carries them, and a TaskRun
	// has one origin, not one per delegated call. Every surface without a
	// TaskRun -- CLI, TUI, Desktop, eval -- leaves all four empty, which is
	// the honest answer, not a guess. See
	// docs/design/portal-work-and-execution-experience.md.
	CreatedBy        string `json:"created_by,omitempty"`
	CreatedByType    string `json:"created_by_type,omitempty"`
	TriggerSource    string `json:"trigger_source,omitempty"`
	RetryOfTaskRunID string `json:"retry_of_task_run_id,omitempty"`

	// sandbox_boundary
	//
	// Sandboxed is a pointer so an unsandboxed run records "sandboxed": false
	// rather than omitting the field. A boundary that goes unreported is
	// indistinguishable from one that was never resolved, and a reader
	// resolving that silence in the run's favor would credit a run with
	// protection it did not have.
	Sandboxed   *bool    `json:"sandboxed,omitempty"`
	SandboxMode string   `json:"sandbox_mode,omitempty"`
	Backend     string   `json:"backend,omitempty"`
	Sources     []string `json:"sources,omitempty"`
	Downgraded  bool     `json:"downgraded,omitempty"`

	// mcp_boundary
	//
	// How this run's MCP transports were treated, recorded beside the sandbox
	// boundary so an operator reads it from the trace rather than re-deriving it
	// from configuration. Like sandbox_boundary the record is written for every
	// run that reaches this point, so an absent record reads as "written before
	// this existed", not as "stdio was allowed". It names transport kinds only,
	// never a command, argument, environment value, or url.
	MCPStdioDisabled    bool     `json:"mcp_stdio_disabled,omitempty"`
	MCPRemoteTransports []string `json:"mcp_remote_transports,omitempty"`

	// context_sources
	//
	// What the run was given before its first model call, each source named by
	// its own kind: instruction layers, memory, and whether a compaction
	// summary stood in for messages. Like sandbox_boundary, the record is
	// written even when there is nothing beyond the runtime prompt -- an absent
	// record would read as "nobody looked" rather than "there was nothing
	// else" -- and it carries sizes and revisions rather than any raw text.
	ProjectID         string                   `json:"project_id,omitempty"`
	Instructions      []agent.PromptLayer      `json:"instructions,omitempty"`
	Memory            []agent.MemorySourceInfo `json:"memory,omitempty"`
	HistoryProjection *agent.HistoryProjection `json:"history_projection,omitempty"`

	// plugins
	//
	// Plugins names what a run loaded from outside the workspace and the user's
	// own configuration, and for a repository plugin whether that input could
	// still change under it. The record is written even when nothing was
	// loaded, for the same reason as the two above: absence would read as
	// "nobody looked".
	Plugins []plugin.Provenance `json:"plugins,omitempty"`

	// iter_start, llm_start, llm_end, context_compacted, user_input,
	// user_input_blocked
	Iter int `json:"iter,omitempty"`

	// llm_start, llm_end
	ContextTokens int `json:"context_tokens,omitempty"`
	ContextWindow int `json:"context_window,omitempty"`

	// llm_end, run_end
	HasToolCalls     bool `json:"has_tool_calls,omitempty"`
	PromptTokens     int  `json:"prompt_tokens,omitempty"`
	CompletionTokens int  `json:"completion_tokens,omitempty"`
	// CacheReadTokens and CacheWriteTokens are the provider-reported cached
	// parts of the prompt so far. They break PromptTokens down rather than add
	// to it. Absent means the provider reported none, which a reader must not
	// read as a cache miss: a provider that reports nothing is not a provider
	// that missed.
	CacheReadTokens  int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`

	// llm_end
	//
	// The token fields above are the run's totals so far; these are what this
	// one call did. Both are recorded because a reader asking which turn was
	// expensive would otherwise have to subtract consecutive records, which
	// goes wrong the moment a call in between failed and wrote none.
	CallPromptTokens     int `json:"call_prompt_tokens,omitempty"`
	CallCompletionTokens int `json:"call_completion_tokens,omitempty"`
	CallCacheReadTokens  int `json:"call_cache_read_tokens,omitempty"`
	CallCacheWriteTokens int `json:"call_cache_write_tokens,omitempty"`

	// llm_end (this call), run_end (the run)
	//
	// Absent when the model was unpriced, which is not the same fact as a call
	// that cost nothing.
	Cost *RecordCost `json:"cost,omitempty"`

	// llm_end
	Content string `json:"content,omitempty"`

	// tool_start, tool_end, tool_denied
	Tool       string `json:"tool,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	// tool_start only: a call's arguments are recorded once, and a reader
	// pairs them with the call's tool_end or tool_denied by tool_call_id.
	Args string `json:"args,omitempty"`

	// tool_end
	Result     string `json:"result,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	// ErrorKind names how the call failed, absent when it did not. It reports
	// a call that could not complete, never a task that completed badly: a
	// command exiting non-zero is a successful Bash call here.
	ErrorKind string `json:"error_kind,omitempty"`

	// tool_denied
	DenyReason string `json:"deny_reason,omitempty"`

	// context_compacted
	//
	// The call_* token fields and cost above are also written here: compaction
	// is a model call the run paid for, and a trace that recorded only that it
	// happened would leave the money unaccounted.
	Summarized int `json:"summarized,omitempty"`
	Kept       int `json:"kept,omitempty"`

	// run_end
	ToolCalls int `json:"tool_calls,omitempty"`
	// Delegated is what subagent runs this one started spent. Its token and
	// cost figures are a breakdown of the run totals above, not an addition to
	// them; its tool calls are additional, because a delegation is one call of
	// the parent and the child's calls are counted nowhere else.
	Delegated *RecordDelegated `json:"delegated,omitempty"`
	// CostIncomplete says a call in this run did work that could not be
	// priced, so Cost understates it rather than covering it.
	CostIncomplete bool   `json:"cost_incomplete,omitempty"`
	Error          string `json:"error,omitempty"`
}

// RecordCost is an estimated spend, in nano-units of Currency: one currency
// unit is 1e9 of them, held as integers so a reader sums a run exactly.
//
// It is declared here rather than reusing core/llm.Cost because this is a
// durable file format. A field added to the domain type should not silently
// change what a trace written last month is expected to contain.
type RecordCost struct {
	Currency   string `json:"currency"`
	Uncached   int64  `json:"uncached"`
	CacheRead  int64  `json:"cache_read"`
	CacheWrite int64  `json:"cache_write"`
	Output     int64  `json:"output"`
	Total      int64  `json:"total"`
	// Baseline is what the same tokens would have cost with no caching at all.
	// It is what makes the saving readable: comparing Total against zero would
	// report a win on a call that only ever wrote.
	Baseline int64 `json:"baseline"`
}

// RecordDelegated is the delegated-spend breakdown of one run, declared here
// rather than reusing the domain type for the same reason as RecordCost: this
// is a durable file format, and a field added to the domain should not change
// what a trace written last month is expected to contain.
type RecordDelegated struct {
	Runs             int         `json:"runs"`
	PromptTokens     int         `json:"prompt_tokens,omitempty"`
	CompletionTokens int         `json:"completion_tokens,omitempty"`
	CacheReadTokens  int         `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int         `json:"cache_write_tokens,omitempty"`
	ToolCalls        int         `json:"tool_calls,omitempty"`
	Cost             *RecordCost `json:"cost,omitempty"`
	CostIncomplete   bool        `json:"cost_incomplete,omitempty"`
}

func recordDelegated(d *agent.DelegatedStats) *RecordDelegated {
	if d == nil {
		return nil
	}
	return &RecordDelegated{
		Runs:             d.Runs,
		PromptTokens:     d.PromptTokens,
		CompletionTokens: d.CompletionTokens,
		CacheReadTokens:  d.CacheReadTokens,
		CacheWriteTokens: d.CacheWriteTokens,
		ToolCalls:        d.ToolCalls,
		Cost:             recordCost(d.Cost),
		CostIncomplete:   d.CostIncomplete,
	}
}

func recordCost(cost *llm.Cost) *RecordCost {
	if cost == nil {
		return nil
	}
	return &RecordCost{
		Currency:   cost.Currency,
		Uncached:   cost.Uncached,
		CacheRead:  cost.CacheRead,
		CacheWrite: cost.CacheWrite,
		Output:     cost.Output,
		Total:      cost.Total,
		Baseline:   cost.Baseline,
	}
}

// recordTypes maps each agent.EventKind to its trace record type string.
// EventLLMDelta has no entry: streaming deltas are not persisted (redundant
// with llm_end content).
var recordTypes = map[agent.EventKind]string{
	agent.EventIterStart:        "iter_start",
	agent.EventLLMStart:         "llm_start",
	agent.EventLLMEnd:           "llm_end",
	agent.EventToolStart:        "tool_start",
	agent.EventToolEnd:          "tool_end",
	agent.EventToolDenied:       "tool_denied",
	agent.EventContextCompacted: "context_compacted",
	agent.EventRunEnd:           "run_end",
	agent.EventUserInput:        "user_input",
	agent.EventUserInputBlocked: "user_input_blocked",
}

// recordFromEvent maps a runtime event to a trace Record, applying bounding and
// redaction to free-text fields. It returns ok=false for events that are not
// persisted (e.g. EventLLMDelta).
func recordFromEvent(e agent.Event, maxField int, red *secretscan.Redactor) (Record, bool) {
	typ, ok := recordTypes[e.Kind]
	if !ok {
		return Record{}, false
	}
	r := Record{TS: now(), Type: typ}
	switch e.Kind {
	case agent.EventIterStart:
		r.Iter = e.Iter
	case agent.EventLLMStart:
		r.Iter = e.Iter
		r.ContextTokens = e.ContextTokens
		r.ContextWindow = e.ContextWindow
	case agent.EventLLMEnd:
		r.Iter = e.Iter
		r.HasToolCalls = e.HasToolCalls
		r.PromptTokens = e.PromptTokens
		r.CompletionTokens = e.CompletionTokens
		r.CacheReadTokens = e.CacheReadTokens
		r.CacheWriteTokens = e.CacheWriteTokens
		r.CallPromptTokens = e.CallUsage.PromptTokens
		r.CallCompletionTokens = e.CallUsage.CompletionTokens
		r.CallCacheReadTokens = e.CallUsage.CacheReadTokens
		r.CallCacheWriteTokens = e.CallUsage.CacheWriteTokens
		r.Cost = recordCost(e.CallCost)
		r.ContextTokens = e.ContextTokens
		r.ContextWindow = e.ContextWindow
		r.Content = bound(red.Redact(e.Content), maxField)
	case agent.EventToolStart:
		r.Tool = e.ToolName
		r.ToolCallID = e.ToolCallID
		r.Args = bound(red.Redact(e.ToolArgs), maxField)
	case agent.EventToolEnd:
		r.Tool = e.ToolName
		r.ToolCallID = e.ToolCallID
		r.Result = bound(red.Redact(e.ToolResult), maxField)
		r.DurationMS = e.ToolDuration.Milliseconds()
		r.ErrorKind = e.ToolErrorKind
	case agent.EventToolDenied:
		r.Tool = e.ToolName
		r.ToolCallID = e.ToolCallID
		r.DenyReason = e.DenyReason
	case agent.EventContextCompacted:
		r.Iter = e.Iter
		r.Summarized = e.Summarized
		r.Kept = e.Kept
		// Compaction is a model call the run paid for. It is recorded here
		// rather than as an llm_end because it is not a turn: the summary it
		// produced never entered the conversation as a reply.
		r.CallPromptTokens = e.CallUsage.PromptTokens
		r.CallCompletionTokens = e.CallUsage.CompletionTokens
		r.CallCacheReadTokens = e.CallUsage.CacheReadTokens
		r.CallCacheWriteTokens = e.CallUsage.CacheWriteTokens
		r.Cost = recordCost(e.CallCost)
	case agent.EventUserInput:
		// A message that entered the run after it started is part of what the run
		// was told to do, so a trace that omitted it would misreport its instructions.
		r.Iter = e.Iter
		r.Content = bound(red.Redact(e.Content), maxField)
	case agent.EventUserInputBlocked:
		r.Iter = e.Iter
		r.Content = bound(red.Redact(e.Content), maxField)
		r.DenyReason = e.DenyReason
	case agent.EventRunEnd:
		r.ToolCalls = e.Stats.ToolCalls
		r.PromptTokens = e.Stats.PromptTokens
		r.CompletionTokens = e.Stats.CompletionTokens
		r.CacheReadTokens = e.Stats.CacheReadTokens
		r.CacheWriteTokens = e.Stats.CacheWriteTokens
		r.Cost = recordCost(e.Stats.Cost)
		r.CostIncomplete = e.Stats.CostIncomplete
		r.Delegated = recordDelegated(e.Stats.Delegated)
		if e.Err != nil {
			r.Error = e.Err.Error()
		}
	}
	return r, true
}

// sourcesRecord reports every context source this run loaded. Written for every
// run, including one that loaded nothing beyond the runtime prompt: trust-harness
// §3.6 asks that a run be able to say which sources it received, and silence is
// not an answer.
func sourcesRecord(sources agent.ContextSources) Record {
	projection := sources.HistoryProjection
	return Record{
		TS:                now(),
		Type:              "context_sources",
		ProjectID:         sources.ProjectID,
		Workspace:         sources.Workspace,
		Instructions:      append([]agent.PromptLayer(nil), sources.Instructions...),
		Memory:            append([]agent.MemorySourceInfo(nil), sources.Memory...),
		HistoryProjection: &projection,
	}
}

// pluginsRecord builds the plugins record written immediately after run_start.
func pluginsRecord(plugins []plugin.Provenance) Record {
	return Record{TS: now(), Type: "plugins", Plugins: append([]plugin.Provenance(nil), plugins...)}
}

// boundaryRecord builds the sandbox_boundary record written immediately after
// run_start.
//
// It is written for every run, including one that was not confined at all.
// Portal run diagnostics and `buildmax sandbox overrides` both answer "what
// contained this run" from this record, and an absent record would be read as
// "nobody checked" rather than "nothing contained it".
func boundaryRecord(info *agent.SandboxInfo) Record {
	rec := Record{TS: now(), Type: "sandbox_boundary", Backend: "none"}
	sandboxed := false
	// A nil view means the surface wired no sandbox, which gives the run no
	// boundary. Recording that as unknown would let an unprotected run read as
	// an unexamined one, so it is reported as unsandboxed.
	if info != nil {
		sandboxed = info.Enabled
		rec.SandboxMode = info.Mode
		if info.Backend != "" {
			rec.Backend = info.Backend
		}
		rec.Sources = append([]string(nil), info.Sources...)
		rec.Downgraded = info.Downgraded
	}
	rec.Sandboxed = &sandboxed
	return rec
}

// MCPTreatment is how a run's MCP transports were treated. It is both the
// recorder input (Meta.MCP) and the reader-facing summary field (Summary.MCP):
// the shape is the same on the way in and out, so it is one type rather than
// two. A nil *MCPTreatment on a summary means the trace predates the record —
// unknown, not "stdio allowed".
type MCPTreatment struct {
	// StdioDisabled reports that the unattended-worker profile refused stdio MCP
	// for this run. False on a local surface, where stdio is allowed.
	StdioDisabled bool `json:"stdio_disabled"`
	// RemoteTransports are the resolved remote transport kinds active for the
	// run, a sorted subset of "http" and "sse". Empty when none were configured.
	RemoteTransports []string `json:"remote_transports,omitempty"`
}

// mcpBoundaryRecord builds the mcp_boundary record written immediately after
// sandbox_boundary. It is skipped only when the surface computed no treatment,
// which keeps an absent record meaning "written before this existed".
func mcpBoundaryRecord(t *MCPTreatment) Record {
	rec := Record{TS: now(), Type: "mcp_boundary"}
	if t != nil {
		rec.MCPStdioDisabled = t.StdioDisabled
		rec.MCPRemoteTransports = append([]string(nil), t.RemoteTransports...)
	}
	return rec
}

// bound truncates s to at most max bytes, appending a marker noting how many
// bytes were dropped. max <= 0 disables bounding. The cut backs off to a rune
// boundary so a multi-byte character is never split in half — a partial rune
// would be re-encoded as U+FFFD by the JSON encoder, mangling the last
// character of any non-ASCII field.
func bound(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	dropped := len(s) - cut
	return s[:cut] + fmt.Sprintf(" … [truncated %d bytes]", dropped)
}

// now is overridable in tests.
var now = func() string { return time.Now().UTC().Format(time.RFC3339Nano) }
