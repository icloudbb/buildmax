package trace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/agent"
)

// TestSummarize_RoundTripsARealTrace runs a trace through the recorder and back
// out through Summarize, so the two halves of the format cannot drift.
func TestSummarize_RoundTripsARealTrace(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(runDirFor(dir, "c_sess1"), Meta{
		RunID:     "rt_sum1",
		SessionID: "c_sess1",
		Model:     "test-model",
		Sandbox:   &agent.SandboxInfo{Enabled: true, Mode: "auto_allow", Backend: "bwrap", Sources: []string{"default:worker"}},
	})
	if rec == nil {
		t.Fatal("expected recorder")
	}
	rec.Record(agent.Event{Kind: agent.EventLLMStart, Iter: 1})
	// The shapes the agent loop emits: arguments on the start only, the result
	// on the end, a denied call started and then denied.
	rec.Record(agent.Event{Kind: agent.EventToolStart, ToolName: "Write", ToolCallID: "c1", ToolArgs: `{"file_path":"/ws/out.md","content":"hi"}`})
	rec.Record(agent.Event{Kind: agent.EventToolEnd, ToolName: "Write", ToolCallID: "c1", ToolResult: "ok"})
	rec.Record(agent.Event{Kind: agent.EventToolStart, ToolName: "Bash", ToolCallID: "c2", ToolArgs: `{"command":"rm -rf /"}`})
	rec.Record(agent.Event{Kind: agent.EventToolDenied, ToolName: "Bash", ToolCallID: "c2", DenyReason: "hook"})
	rec.Record(agent.Event{Kind: agent.EventContextCompacted, Summarized: 3, Kept: 2})
	rec.Record(agent.Event{Kind: agent.EventRunEnd, Stats: agent.RunStats{
		ToolCalls: 2, PromptTokens: 120, CompletionTokens: 30,
		CacheReadTokens: 80, CacheWriteTokens: 10,
	}})
	if err := rec.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	f, err := os.Open(filepath.Join(dir, "c_sess1", "rt_sum1.jsonl"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	got, err := Summarize(f)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}

	if got.RunID != "rt_sum1" || got.Model != "test-model" {
		t.Errorf("run identity wrong: %+v", got)
	}
	if !got.Complete || got.Error != "" {
		t.Errorf("want a complete successful run, got complete=%v err=%q", got.Complete, got.Error)
	}
	if got.LLMCalls != 1 || got.ToolCalls != 2 || got.Compactions != 1 {
		t.Errorf("counts wrong: llm=%d tool=%d compact=%d", got.LLMCalls, got.ToolCalls, got.Compactions)
	}
	if got.PromptTokens != 120 || got.CompletionTokens != 30 {
		t.Errorf("tokens wrong: %d/%d", got.PromptTokens, got.CompletionTokens)
	}
	if got.CacheReadTokens != 80 || got.CacheWriteTokens != 10 {
		t.Errorf("cache tokens wrong: %d/%d", got.CacheReadTokens, got.CacheWriteTokens)
	}
	if got.Boundary == nil || !got.Boundary.Sandboxed || got.Boundary.Backend != "bwrap" {
		t.Errorf("boundary wrong: %+v", got.Boundary)
	}
	if len(got.Tools) != 2 {
		t.Fatalf("want 2 tool entries, got %+v", got.Tools)
	}
	if got.Tools[0].Name != "Write" || got.Tools[0].Path != "/ws/out.md" {
		t.Errorf("file path not surfaced: %+v", got.Tools[0])
	}
	if !got.Tools[1].Denied || got.Tools[1].DenyReason != "hook" {
		t.Errorf("denied call not surfaced: %+v", got.Tools[1])
	}
}

// TestSummarize_ReportsUnsandboxedAndIncomplete covers the two answers a reader
// must not get wrong: a run nothing confined, and a run that never finished.
func TestSummarize_ReportsUnsandboxedAndIncomplete(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(runDirFor(dir, "s"), Meta{RunID: "rt_sum2", SessionID: "s"})
	rec.Record(agent.Event{Kind: agent.EventLLMStart, Iter: 1})
	// No run_end: the run died mid-flight.
	if err := rec.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	f, err := os.Open(filepath.Join(dir, "s", "rt_sum2.jsonl"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	got, err := Summarize(f)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if got.Boundary == nil {
		t.Fatal("boundary must be reported even when nothing confined the run")
	}
	if got.Boundary.Sandboxed {
		t.Error("want sandboxed=false on a surface with no sandbox")
	}
	if got.Complete {
		t.Error("a trace with no run_end must not report complete")
	}
	if got.Error != "" {
		t.Errorf("an unfinished run has no terminal error to report, got %q", got.Error)
	}
}

// TestSummarize_SurvivesATruncatedTrace asserts a trace cut short mid-line
// still yields what was readable. A run that died is exactly when the trace is
// most likely damaged and most worth reading.
func TestSummarize_SurvivesATruncatedTrace(t *testing.T) {
	body := `{"ts":"t0","type":"run_start","run_id":"rt_x","model":"m"}
{"ts":"t1","type":"sandbox_boundary","sandboxed":false,"backend":"none"}
{"ts":"t2","type":"llm_start","iter":1}
{"ts":"t3","type":"tool_end","tool":"Rea`

	got, err := Summarize(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if got.RunID != "rt_x" {
		t.Errorf("run id lost: %+v", got)
	}
	if got.LLMCalls != 1 {
		t.Errorf("llm calls = %d, want 1", got.LLMCalls)
	}
	if got.ToolCalls != 0 {
		t.Errorf("the half-written tool record must not be counted, got %d", got.ToolCalls)
	}
	if got.Complete {
		t.Error("a truncated trace is not complete")
	}
}

// TestSummarize_OmitsFreeTextBodies guards the privacy boundary: the summary
// carries the shape of a run, never its content.
func TestSummarize_OmitsFreeTextBodies(t *testing.T) {
	body := `{"ts":"t0","type":"run_start","run_id":"rt_x"}
{"ts":"t1","type":"llm_end","iter":1,"content":"SECRET-MODEL-OUTPUT"}
{"ts":"t2","type":"tool_start","tool":"Read","tool_call_id":"c1","args":"{\"file_path\":\"/ws/a.txt\"}"}
{"ts":"t2","type":"tool_end","tool":"Read","tool_call_id":"c1","result":"SECRET-FILE-BODY"}
{"ts":"t3","type":"run_end"}`

	got, err := Summarize(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(got.Tools) != 1 || got.Tools[0].Path != "/ws/a.txt" {
		t.Fatalf("want the path surfaced, got %+v", got.Tools)
	}
	// The summary is JSON-encoded to the caller; assert no body reached it.
	for _, banned := range []string{"SECRET-MODEL-OUTPUT", "SECRET-FILE-BODY"} {
		if strings.Contains(dumpJSON(t, got), banned) {
			t.Errorf("summary leaked %q", banned)
		}
	}
}

// Parallel calls interleave, so a call's path comes from its own start, found
// by tool_call_id, not from whichever start came last. A call without an id
// cannot be paired and gets no path rather than a wrong one.
func TestSummarize_PairsPathsByToolCallID(t *testing.T) {
	body := `{"type":"tool_start","tool":"Write","tool_call_id":"a","args":"{\"file_path\":\"/ws/a.md\"}"}
{"type":"tool_start","tool":"Edit","tool_call_id":"b","args":"{\"file_path\":\"/ws/b.md\"}"}
{"type":"tool_end","tool":"Edit","tool_call_id":"b"}
{"type":"tool_end","tool":"Write","tool_call_id":"a"}
{"type":"tool_start","tool":"Write","args":"{\"file_path\":\"/ws/c.md\"}"}
{"type":"tool_end","tool":"Write"}`
	got, err := Summarize(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	var paths []string
	for _, tool := range got.Tools {
		paths = append(paths, tool.Name+"="+tool.Path)
	}
	if want := "Edit=/ws/b.md Write=/ws/a.md Write="; strings.Join(paths, " ") != want {
		t.Errorf("tools = %q, want %q", strings.Join(paths, " "), want)
	}
}

// TestSummarize_BoundsToolList asserts a long run reports truncation instead of
// silently returning a short list.
func TestSummarize_BoundsToolList(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"ts":"t0","type":"run_start","run_id":"rt_x"}` + "\n")
	for i := 0; i < maxSummaryTools+10; i++ {
		b.WriteString(`{"ts":"t","type":"tool_end","tool":"Read"}` + "\n")
	}
	got, err := Summarize(strings.NewReader(b.String()))
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(got.Tools) != maxSummaryTools {
		t.Errorf("tools = %d, want %d", len(got.Tools), maxSummaryTools)
	}
	if !got.ToolsTruncated {
		t.Error("a bounded list must say it was bounded")
	}
	if got.ToolCalls != maxSummaryTools+10 {
		t.Errorf("the call count must stay whole even when the list is cut: got %d", got.ToolCalls)
	}
}

// dumpJSON renders v as JSON for leak assertions.
func dumpJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	return string(b)
}
