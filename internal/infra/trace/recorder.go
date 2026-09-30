// Package trace persists a durable, bounded, redacted record of a single Agent
// run by consuming the core/agent event stream. See docs/design/durable-run-trace.md.
package trace

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/icloudbb/buildmax/internal/core/agent"
	"github.com/icloudbb/buildmax/internal/core/plugin"
	"github.com/icloudbb/buildmax/internal/util/secretscan"
)

// Identity belongs in an attr, not in every message string.
func componentLog() *slog.Logger { return slog.With("component", "trace") }

// defaultMaxRecords guards a runaway loop: once this many event records are
// written, further events are dropped (run_end is still attempted).
const defaultMaxRecords = 10000

// Meta describes the run a Recorder is opened for. It seeds the run_start
// record.
type Meta struct {
	RunID      string
	SessionID  string
	Workspace  string
	Model      string
	IsSubagent bool
	// ParentRunID names the immediate trace run that started this one. It is
	// empty for a top-level run and deliberately stays optional so trace
	// recording remains fail-open when no parent trace was created.
	ParentRunID string
	// ParentToolCallID names the tool call in the parent run that launched
	// this one, so an inspection can walk parent run → tool call → child run.
	// Empty when the launch context carried none.
	ParentToolCallID string
	// CreatedBy, CreatedByType, TriggerSource, and RetryOfTaskRunID are this
	// run's TaskRun provenance -- who or what started it and why -- when it
	// has one. Left empty by every surface with no TaskRun to report.
	CreatedBy        string
	CreatedByType    string
	TriggerSource    string
	RetryOfTaskRunID string
	// Sandbox is the execution boundary resolved for this run. Nil is recorded
	// as unsandboxed rather than unknown — see boundaryRecord.
	Sandbox *agent.SandboxInfo
	// MCP is how this run's MCP transports were treated. Nil means the surface
	// computed none and no mcp_boundary record is written; a reader then sees
	// the treatment as unknown rather than as stdio-allowed.
	MCP *MCPTreatment
	// Sources are the instruction, memory, and history-projection inputs this
	// run started with.
	Sources agent.ContextSources
	// Plugins is the plugin inventory active for this run.
	Plugins []plugin.Provenance
	// SecretValues are this run's materialized Space Secret values, redacted from
	// every record's free-text fields so a durable trace does not carry them.
	// Empty when the run consumes no Secret. See docs/design/space-secrets.md §12.
	SecretValues []string
}

// Recorder appends trace records for one run to a JSONL file. All methods are
// safe for concurrent use and fail open: any error is logged at warn and
// dropped so tracing never breaks or slows a run.
type Recorder struct {
	runID string
	path  string

	mu        sync.Mutex
	f         *os.File
	w         *bufio.Writer
	maxField  int
	maxRecord int
	count     int
	dropped   bool
	// redactor redacts this run's exact Secret values on top of the shape-based
	// scan every record already gets. Non-nil for every recorder.
	redactor *secretscan.Redactor
}

// NewRecorder opens <dir>/<session_id>/<run_id>.jsonl and writes the run_start
// and sandbox_boundary records. On any failure it logs a warning and returns a
// nil *Recorder, whose methods are all no-ops — callers do not need to
// nil-check beyond normal use.
func NewRecorder(dir string, meta Meta) *Recorder {
	if meta.RunID == "" {
		componentLog().Warn("missing run id; tracing disabled for this run")
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		componentLog().Warn("create dir failed; tracing disabled for this run", "dir", dir, "err", err)
		return nil
	}
	path := filepath.Join(dir, meta.RunID+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		componentLog().Warn("open file failed; tracing disabled for this run", "path", path, "err", err)
		return nil
	}
	r := &Recorder{
		runID:     meta.RunID,
		path:      path,
		f:         f,
		w:         bufio.NewWriter(f),
		maxField:  defaultMaxFieldBytes,
		maxRecord: defaultMaxRecords,
		redactor:  secretscan.NewRedactor(meta.SecretValues),
	}
	r.write(Record{
		TS:               now(),
		Type:             "run_start",
		RunID:            meta.RunID,
		SessionID:        meta.SessionID,
		Workspace:        meta.Workspace,
		Model:            meta.Model,
		IsSubagent:       meta.IsSubagent,
		ParentRunID:      meta.ParentRunID,
		ParentToolCallID: meta.ParentToolCallID,
		TraceVersion:     traceVersion,
		CreatedBy:        meta.CreatedBy,
		CreatedByType:    meta.CreatedByType,
		TriggerSource:    meta.TriggerSource,
		RetryOfTaskRunID: meta.RetryOfTaskRunID,
	})
	r.write(boundaryRecord(meta.Sandbox))
	if meta.MCP != nil {
		r.write(mcpBoundaryRecord(meta.MCP))
	}
	r.write(sourcesRecord(meta.Sources))
	r.write(pluginsRecord(meta.Plugins))
	return r
}

// RunID returns the run id this recorder writes for, or "" when the recorder is
// nil (tracing disabled).
func (r *Recorder) RunID() string {
	if r == nil {
		return ""
	}
	return r.runID
}

// Path returns the trace file this recorder writes to, or "" when the recorder
// is nil (tracing disabled). Callers that persist a pointer to the trace derive
// it from this rather than rebuilding the layout, so a stored reference and the
// written file cannot drift apart.
func (r *Recorder) Path() string {
	if r == nil {
		return ""
	}
	return r.path
}

// Record maps a runtime event to a trace record and appends it. Events that are
// not persisted (e.g. streaming deltas) and nil recorders are ignored.
func (r *Recorder) Record(e agent.Event) {
	if r == nil {
		return
	}
	rec, ok := recordFromEvent(e, r.maxField, r.redactor)
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// run_end is always allowed through even past the cap so a trace has a
	// terminal record.
	if r.count >= r.maxRecord && rec.Type != "run_end" {
		if !r.dropped {
			r.dropped = true
			componentLog().Warn("record cap reached; dropping further records", "run_id", r.runID, "cap", r.maxRecord)
		}
		return
	}
	r.write(rec)
}

// RecordRunEnd writes a synthetic run_end record carrying an error string. Used
// for paths that never reach RunLoop (e.g. a prompt blocked by a hook).
func (r *Recorder) RecordRunEnd(errMsg string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.write(Record{TS: now(), Type: "run_end", Error: r.redactor.Redact(errMsg)})
}

// Close flushes and closes the trace file. Safe to call on a nil recorder.
func (r *Recorder) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.w != nil {
		if err := r.w.Flush(); err != nil {
			componentLog().Warn("flush failed", "run_id", r.runID, "err", err)
		}
		r.w = nil
	}
	if r.f != nil {
		err := r.f.Close()
		r.f = nil
		return err
	}
	return nil
}

// write encodes rec as one JSON line. Caller holds r.mu.
func (r *Recorder) write(rec Record) {
	if r.w == nil {
		return
	}
	b, err := json.Marshal(rec)
	if err != nil {
		componentLog().Warn("marshal record failed", "run_id", r.runID, "type", rec.Type, "err", err)
		return
	}
	if _, err := r.w.Write(append(b, '\n')); err != nil {
		componentLog().Warn("write record failed", "run_id", r.runID, "type", rec.Type, "err", err)
		return
	}
	// Flush per record rather than letting bufio decide. A trace earns its
	// keep on the runs that end badly — a kill, a crash, a hang someone gave
	// up on — and those are exactly the runs where Close never gets to flush.
	// Buffering there does not just lose the tail, it moves the last recorded
	// event minutes before the real one and points the reader at the wrong
	// place. Trace volume is a handful of records per model call, so the extra
	// write costs nothing next to what it is recording.
	if err := r.w.Flush(); err != nil {
		componentLog().Warn("flush record failed", "run_id", r.runID, "type", rec.Type, "err", err)
		return
	}
	r.count++
}
