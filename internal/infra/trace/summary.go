package trace

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// maxSummaryTools bounds how many tool entries a Summary carries. A run that
// hits the recorder's own record cap can still hold thousands of tool calls,
// and a diagnostic view needs the shape of a run, not every call in it.
const maxSummaryTools = 500

// Summary is a run trace reduced to what a reader needs to answer "what did
// this run do, and why did it end that way".
//
// It deliberately omits the free-text bodies the trace carries — model output,
// tool arguments, tool results. Those are bounded and redacted on the way in,
// but they are still the run's content; a summary is about its shape. Callers
// that genuinely need a body should read the trace file itself.
type Summary struct {
	RunID     string `json:"run_id,omitempty"`
	Model     string `json:"model,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	EndedAt   string `json:"ended_at,omitempty"`

	// Boundary is the execution boundary the run ran under. Nil only for a
	// trace written before the sandbox_boundary record existed — a current
	// trace always has one, including for an unsandboxed run.
	Boundary *BoundarySummary `json:"boundary,omitempty"`

	// MCP is how the run's MCP transports were treated. Nil for a trace written
	// before the mcp_boundary record existed — unknown, which a reader must not
	// resolve as stdio-allowed or as confined.
	MCP *MCPTreatment `json:"mcp,omitempty"`

	LLMCalls  int `json:"llm_calls"`
	ToolCalls int `json:"tool_calls"`
	// ToolFailures counts calls that could not complete. It is not a count of
	// work that went badly: a command exiting non-zero is a successful call.
	ToolFailures     int `json:"tool_failures"`
	Compactions      int `json:"compactions"`
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	// CacheReadTokens and CacheWriteTokens are the cached parts of
	// PromptTokens, not extra tokens on top of it.
	CacheReadTokens  int `json:"cache_read_tokens"`
	CacheWriteTokens int `json:"cache_write_tokens"`

	Tools []ToolSummary `json:"tools,omitempty"`
	// ToolsTruncated reports that Tools holds only the first maxSummaryTools
	// entries. Named rather than silent so a short list is never mistaken for a
	// short run.
	ToolsTruncated bool `json:"tools_truncated,omitempty"`

	// Error is the run's terminal error, empty when it succeeded.
	Error string `json:"error,omitempty"`
	// Complete reports that the trace ends with a run_end record. False means
	// the run died without writing one — the process was killed, or the file
	// was cut short. A caller must not read false as success.
	Complete bool `json:"complete"`
}

// BoundarySummary is the sandbox_boundary record in reader-facing form.
type BoundarySummary struct {
	// Sandboxed is false when nothing confined the run's Bash commands.
	Sandboxed bool   `json:"sandboxed"`
	Mode      string `json:"mode,omitempty"`
	Backend   string `json:"backend,omitempty"`
	// Sources is the layer chain that decided the boundary, e.g.
	// ["default:worker", "settings", "policy"].
	Sources []string `json:"sources,omitempty"`
	// Downgraded reports the boundary resolved weaker than configured.
	Downgraded bool `json:"downgraded,omitempty"`
}

// ToolSummary is one tool call: what ran, how long it took, and whether it was
// allowed to run at all.
type ToolSummary struct {
	Name       string `json:"name"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	// Path is the file_path argument when the call carried one. Present for
	// file tools regardless of which tool it was, so the reader — not this
	// package — decides which of them count as a change.
	Path string `json:"path,omitempty"`
	// Denied reports the call was blocked, by a hook or by policy.
	Denied     bool   `json:"denied,omitempty"`
	DenyReason string `json:"deny_reason,omitempty"`
	// ErrorKind names how the call failed, empty when it did not.
	ErrorKind string `json:"error_kind,omitempty"`
}

// Summarize reads a JSONL trace and reduces it to a Summary.
//
// Unparseable lines are skipped rather than failing the whole read: a trace can
// be cut short mid-line by a killed process, and a partial answer about a run
// that died is more useful than no answer. A read error is returned, because
// that means the caller got nothing.
func Summarize(r io.Reader) (Summary, error) {
	var s Summary
	// A call's arguments are recorded once, on its tool_start; tool_end carries
	// the result. So a call's file_path is held here from its start until its
	// end or denial, keyed by tool_call_id because parallel calls interleave.
	openPaths := make(map[string]string)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		s.apply(rec, openPaths)
	}
	if err := sc.Err(); err != nil {
		return s, fmt.Errorf("read trace: %w", err)
	}
	return s, nil
}

// apply folds one record into the summary. openPaths holds the file_path of
// each call that has started and not yet ended, by tool_call_id.
func (s *Summary) apply(rec Record, openPaths map[string]string) {
	switch rec.Type {
	case "run_start":
		s.RunID = rec.RunID
		s.Model = rec.Model
		s.StartedAt = rec.TS
	case "sandbox_boundary":
		b := BoundarySummary{
			Mode:       rec.SandboxMode,
			Backend:    rec.Backend,
			Sources:    rec.Sources,
			Downgraded: rec.Downgraded,
		}
		if rec.Sandboxed != nil {
			b.Sandboxed = *rec.Sandboxed
		}
		s.Boundary = &b
	case "mcp_boundary":
		s.MCP = &MCPTreatment{
			StdioDisabled:    rec.MCPStdioDisabled,
			RemoteTransports: rec.MCPRemoteTransports,
		}
	case "llm_start":
		s.LLMCalls++
	case "tool_start":
		// A call without an id cannot be told apart from a parallel one, so it
		// gets no path rather than possibly another call's.
		if path := filePathArg(rec.Args); path != "" && rec.ToolCallID != "" {
			openPaths[rec.ToolCallID] = path
		}
	case "tool_end":
		s.ToolCalls++
		if rec.ErrorKind != "" {
			s.ToolFailures++
		}
		s.addTool(ToolSummary{
			Name:       rec.Tool,
			DurationMS: rec.DurationMS,
			Path:       takePath(openPaths, rec.ToolCallID),
			ErrorKind:  rec.ErrorKind,
		})
	case "tool_denied":
		s.addTool(ToolSummary{
			Name:       rec.Tool,
			Path:       takePath(openPaths, rec.ToolCallID),
			Denied:     true,
			DenyReason: rec.DenyReason,
		})
	case "context_compacted":
		s.Compactions++
	case "run_end":
		s.Complete = true
		s.EndedAt = rec.TS
		s.Error = rec.Error
		// run_end carries the run's totals; per-call records would double count
		// a retried call.
		s.PromptTokens = rec.PromptTokens
		s.CompletionTokens = rec.CompletionTokens
		s.CacheReadTokens = rec.CacheReadTokens
		s.CacheWriteTokens = rec.CacheWriteTokens
		if rec.ToolCalls > 0 {
			s.ToolCalls = rec.ToolCalls
		}
	}
}

func (s *Summary) addTool(t ToolSummary) {
	if len(s.Tools) >= maxSummaryTools {
		s.ToolsTruncated = true
		return
	}
	s.Tools = append(s.Tools, t)
}

// takePath returns and forgets the file_path recorded at a call's start.
func takePath(openPaths map[string]string, toolCallID string) string {
	if toolCallID == "" {
		return ""
	}
	path := openPaths[toolCallID]
	delete(openPaths, toolCallID)
	return path
}

// filePathArg pulls the file_path argument out of a tool call's recorded
// arguments. Arguments are bounded on the way into the trace, so a large call
// may not parse as JSON at all — that yields no path rather than an error,
// since a missing path is the honest answer.
func filePathArg(args string) string {
	if args == "" {
		return ""
	}
	var parsed struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal([]byte(args), &parsed); err != nil {
		return ""
	}
	return parsed.FilePath
}
