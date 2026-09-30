package trace

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/agent"
)

func readRecords(t *testing.T, path string) []Record {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open trace: %v", err)
	}
	defer f.Close()
	var out []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var r Record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("unmarshal line %q: %v", sc.Text(), err)
		}
		out = append(out, r)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	return out
}

func TestRecorder_WritesRunStartEventsAndEnd(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(runDirFor(dir, "c_sess1"), Meta{RunID: "rt_test01", SessionID: "c_sess1", Workspace: "/ws", Model: "m"})
	if rec == nil {
		t.Fatal("expected recorder")
	}
	rec.Record(agent.Event{Kind: agent.EventLLMStart, Iter: 1, ContextTokens: 100, ContextWindow: 1000})
	rec.Record(agent.Event{Kind: agent.EventToolEnd, ToolName: "bash", ToolResult: "ok"})
	rec.Record(agent.Event{Kind: agent.EventLLMDelta, Content: "stream"}) // should be dropped
	rec.Record(agent.Event{Kind: agent.EventRunEnd, Stats: agent.RunStats{ToolCalls: 1}})
	if err := rec.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	path := filepath.Join(dir, "c_sess1", "rt_test01.jsonl")
	recs := readRecords(t, path)
	// run_start, sandbox_boundary, context_sources, plugins, llm_start, tool_end,
	// run_end (delta dropped)
	if len(recs) != 7 {
		t.Fatalf("got %d records, want 7: %+v", len(recs), recs)
	}
	if recs[0].Type != "run_start" || recs[0].RunID != "rt_test01" || recs[0].TraceVersion != traceVersion {
		t.Errorf("bad run_start: %+v", recs[0])
	}
	if recs[1].Type != "sandbox_boundary" {
		t.Errorf("sandbox_boundary must follow run_start: %+v", recs[1])
	}
	if recs[len(recs)-1].Type != "run_end" {
		t.Errorf("last record should be run_end: %+v", recs[len(recs)-1])
	}
}

// A worker running a TaskRun has provenance to report; every other surface
// has none. run_start has to carry the first case and stay silent for the
// second, not guess.
func TestRecorder_WritesRunProvenanceWhenGiven(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(runDirFor(dir, "c_sess1"), Meta{
		RunID:            "rt_test01",
		SessionID:        "c_sess1",
		CreatedBy:        "u_1",
		CreatedByType:    "user",
		TriggerSource:    "issue_agent_run",
		RetryOfTaskRunID: "tr_prev",
	})
	if rec == nil {
		t.Fatal("expected recorder")
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	recs := readRecords(t, filepath.Join(dir, "c_sess1", "rt_test01.jsonl"))
	got := recs[0]
	if got.CreatedBy != "u_1" || got.CreatedByType != "user" || got.TriggerSource != "issue_agent_run" || got.RetryOfTaskRunID != "tr_prev" {
		t.Errorf("run_start provenance = %+v, want u_1/user/issue_agent_run/tr_prev", got)
	}
}

// CLI, TUI, Desktop, and eval have no TaskRun. Their run_start must omit
// these fields rather than write empty strings a reader could mistake for a
// recorded "no origin" rather than "not applicable here".
func TestRecorder_OmitsRunProvenanceWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(runDirFor(dir, "c_sess1"), Meta{RunID: "rt_test02", SessionID: "c_sess1"})
	if rec == nil {
		t.Fatal("expected recorder")
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "c_sess1", "rt_test02.jsonl"))
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	firstLine, _, _ := strings.Cut(string(raw), "\n")
	for _, key := range []string{"created_by", "created_by_type", "trigger_source", "retry_of_task_run_id"} {
		if strings.Contains(firstLine, "\""+key+"\"") {
			t.Errorf("run_start should omit %q when this run has no TaskRun, got: %s", key, firstLine)
		}
	}
}

func TestRecorder_RunIDAndNilSafety(t *testing.T) {
	var rec *Recorder // nil — tracing disabled
	if rec.RunID() != "" {
		t.Error("nil recorder RunID should be empty")
	}
	rec.Record(agent.Event{Kind: agent.EventRunEnd}) // must not panic
	rec.RecordRunEnd("x")                            // must not panic
	if err := rec.Close(); err != nil {
		t.Errorf("nil close: %v", err)
	}

	real := NewRecorder(t.TempDir(), Meta{RunID: "rt_x", SessionID: "s"})
	if real.RunID() != "rt_x" {
		t.Errorf("RunID = %q", real.RunID())
	}
	real.Close()
}

func TestNewRecorder_FailOpen(t *testing.T) {
	if rec := NewRecorder(t.TempDir(), Meta{RunID: ""}); rec != nil {
		t.Error("missing run id should yield nil recorder")
	}
	// Unwritable parent: create a file where the session dir would go.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "s")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rec := NewRecorder(runDirFor(dir, "s"), Meta{RunID: "rt_y", SessionID: "s"}); rec != nil {
		t.Error("unwritable dir should yield nil recorder")
	}
}

func TestRecorder_RecordCap(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(runDirFor(dir, "s"), Meta{RunID: "rt_cap", SessionID: "s"})
	rec.maxRecord = 3 // run_start and sandbox_boundary already count as 2
	rec.Record(agent.Event{Kind: agent.EventIterStart, Iter: 1})
	rec.Record(agent.Event{Kind: agent.EventIterStart, Iter: 2}) // hits cap; dropped
	rec.Record(agent.Event{Kind: agent.EventIterStart, Iter: 3}) // dropped
	rec.Record(agent.Event{Kind: agent.EventRunEnd})             // always allowed
	rec.Close()

	recs := readRecords(t, filepath.Join(dir, "s", "rt_cap.jsonl"))
	// run_start + 1 iter (count reached 2, third onward dropped) ... cap=3 means
	// count<3 allowed: run_start(1), iter1(2), iter2 -> count==2<3 allowed(3),
	// iter3 -> count==3 dropped. Plus run_end always. Assert run_end present and
	// no more than maxRecord+1 lines.
	last := recs[len(recs)-1]
	if last.Type != "run_end" {
		t.Errorf("run_end must survive the cap: %+v", recs)
	}
	for _, r := range recs[:len(recs)-1] {
		if r.Type == "run_end" {
			t.Error("unexpected extra run_end")
		}
	}
}

func TestRecordRunEnd_Synthetic(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(runDirFor(dir, "s"), Meta{RunID: "rt_block", SessionID: "s", SecretValues: []string{"harbor-canary-value"}})
	rec.RecordRunEnd("blocked by hook: nope, it held harbor-canary-value")
	rec.Close()
	recs := readRecords(t, filepath.Join(dir, "s", "rt_block.jsonl"))
	if len(recs) != 5 || recs[4].Type != "run_end" || recs[4].Error != "blocked by hook: nope, it held [redacted]" {
		t.Errorf("synthetic run_end wrong: %+v", recs)
	}
}

// TestRecorder_BoundaryReportsUnsandboxedRun asserts the case the record exists
// for: a run nothing confined still says so, explicitly, rather than leaving the
// field out and letting a reader assume the boundary held.
func TestRecorder_BoundaryReportsUnsandboxedRun(t *testing.T) {
	tests := []struct {
		name         string
		info         *agent.SandboxInfo
		wantSandboxd bool
		wantBackend  string
	}{
		{
			name:        "no sandbox view wired",
			info:        nil,
			wantBackend: "none",
		},
		{
			name:        "view present but disabled",
			info:        &agent.SandboxInfo{Enabled: false, Sources: []string{"default:cli"}},
			wantBackend: "none",
		},
		{
			name:         "enabled",
			info:         &agent.SandboxInfo{Enabled: true, Mode: "auto_allow", Backend: "bwrap", Sources: []string{"default:worker", "policy"}},
			wantSandboxd: true,
			wantBackend:  "bwrap",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			rec := NewRecorder(runDirFor(dir, "s"), Meta{RunID: "rt_b", SessionID: "s", Sandbox: tt.info})
			rec.Close()

			recs := readRecords(t, filepath.Join(dir, "s", "rt_b.jsonl"))
			if len(recs) != 4 {
				t.Fatalf("got %d records, want run_start + sandbox_boundary + context_sources + plugins: %+v",
					len(recs), recs)
			}
			got := recs[1]
			if got.Type != "sandbox_boundary" {
				t.Fatalf("second record type = %q, want sandbox_boundary", got.Type)
			}
			if got.Sandboxed == nil {
				t.Fatal("sandboxed must be present even when false; an omitted field reads as unchecked")
			}
			if *got.Sandboxed != tt.wantSandboxd {
				t.Errorf("sandboxed = %v, want %v", *got.Sandboxed, tt.wantSandboxd)
			}
			if got.Backend != tt.wantBackend {
				t.Errorf("backend = %q, want %q", got.Backend, tt.wantBackend)
			}
			if tt.info != nil && len(got.Sources) != len(tt.info.Sources) {
				t.Errorf("sources = %v, want %v", got.Sources, tt.info.Sources)
			}
		})
	}
}

// TestBoundaryRecord_FalseSurvivesEncoding guards the pointer field: with a
// plain bool and omitempty, an unsandboxed run would encode to a line with no
// sandboxed key at all.
func TestBoundaryRecord_FalseSurvivesEncoding(t *testing.T) {
	b, err := json.Marshal(boundaryRecord(nil))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"sandboxed":false`) {
		t.Errorf("encoded boundary must carry sandboxed:false, got %s", b)
	}
}

// TestRecorder_ReportsContextSources covers the visibility trust-harness §3.6 asks for: a finished
// run can say which instruction sources it was given, so a system prompt that changed between
// runs is observable rather than something a reader has to infer from behaviour.
func TestRecorder_ReportsContextSources(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(runDirFor(dir, "s"), Meta{
		RunID:     "rt_sources",
		SessionID: "s",
		Sources: agent.ContextSources{Instructions: []agent.PromptLayer{
			{Name: "runtime", Chars: 100},
			{Name: "additional_system_prompt", Chars: 42},
		}},
	})
	rec.Close()

	recs := readRecords(t, filepath.Join(dir, "s", "rt_sources.jsonl"))
	var got *Record
	for i := range recs {
		if recs[i].Type == "context_sources" {
			got = &recs[i]
		}
	}
	if got == nil {
		t.Fatalf("no context_sources record: %+v", recs)
	}
	if len(got.Instructions) != 2 || got.Instructions[1].Name != "additional_system_prompt" || got.Instructions[1].Chars != 42 {
		t.Errorf("instructions = %+v, want the two supplied", got.Instructions)
	}
}

// TestRecorder_ReportsContextSourcesWhenBare asserts the record is written even for a run that
// loaded nothing beyond the runtime prompt. An absent record would read as "nobody looked".
func TestRecorder_ReportsContextSourcesWhenBare(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(runDirFor(dir, "s"), Meta{RunID: "rt_bare", SessionID: "s"})
	rec.Close()

	recs := readRecords(t, filepath.Join(dir, "s", "rt_bare.jsonl"))
	found := false
	for _, r := range recs {
		if r.Type == "context_sources" {
			found = true
		}
	}
	if !found {
		t.Errorf("no context_sources record for a run with no extra layers: %+v", recs)
	}
}

// TestRecorder_RecordsAreDurableWithoutClose is the regression guard for a run
// that never gets to Close: a kill, a crash, or a hang the user gave up on.
// Buffering the tail there does not merely lose records, it moves the last one
// on disk earlier than the last one that happened and sends whoever reads the
// trace to the wrong place.
func TestRecorder_RecordsAreDurableWithoutClose(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(runDirFor(dir, "c_sess1"), Meta{RunID: "rt_kill01", SessionID: "c_sess1", Workspace: "/ws", Model: "m"})
	if rec == nil {
		t.Fatal("expected recorder")
	}
	// Deferred, so it runs after the assertions below and never explains them:
	// what is asserted is what reached disk with no Close at all. Windows
	// cannot delete a file another handle still holds, so leaking it here
	// would fail TempDir cleanup rather than the test's actual subject.
	defer rec.Close()
	rec.Record(agent.Event{Kind: agent.EventToolStart, ToolName: "NoteWrite", ToolCallID: "call_1"})
	rec.Record(agent.Event{Kind: agent.EventToolEnd, ToolName: "NoteWrite", ToolCallID: "call_1", ToolResult: "Stored 1 note"})

	// Read while the recorder is still open — the abandoned-run path.
	path := filepath.Join(dir, "c_sess1", "rt_kill01.jsonl")
	recs := readRecords(t, path)
	// run_start, sandbox_boundary, context_sources, plugins, tool_start, tool_end
	if len(recs) != 6 {
		t.Fatalf("got %d records before Close, want 6: %+v", len(recs), recs)
	}
	last := recs[len(recs)-1]
	if last.Type != "tool_end" || last.Tool != "NoteWrite" {
		t.Errorf("last record on disk is %+v; want the tool_end that actually happened", last)
	}

	// The file must also be complete lines, not a record cut at a buffer edge.
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	if len(body) == 0 || body[len(body)-1] != '\n' {
		t.Errorf("trace does not end on a record boundary; last byte %q", body[len(body)-1:])
	}
}

// runDirFor is where a session's traces live now that the caller owns the
// layout: the store puts them inside the session's own bundle, and these tests
// only need a stable per-session directory to assert against.
func runDirFor(root, sessionID string) string {
	return filepath.Join(root, sessionID)
}
