package trace

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/agent"
	"github.com/icloudbb/buildmax/internal/core/llm"
)

// oneCallClient asks for the given tool calls once, then answers.
type oneCallClient struct {
	mu    sync.Mutex
	turn  int
	calls []llm.ToolCall
}

func (c *oneCallClient) ChatCompletionBlocking(context.Context, llm.Request) (llm.Completion, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.turn++
	if c.turn == 1 {
		return llm.Completion{ToolCalls: c.calls}, nil
	}
	return llm.Completion{Content: "done"}, nil
}

func (c *oneCallClient) ChatCompletionStreaming(ctx context.Context, req llm.Request, _ func(string)) (llm.Completion, error) {
	return c.ChatCompletionBlocking(ctx, req)
}

func (c *oneCallClient) ContextWindow() int { return 0 }

// fileTool stands in for a file tool: only its name and file_path argument
// matter to a trace.
type fileTool struct{ name string }

func (t fileTool) Name() string        { return t.name }
func (t fileTool) Description() string { return t.name }
func (t fileTool) Parameters() any     { return map[string]any{"type": "object"} }
func (t fileTool) Execute(context.Context, map[string]any) (string, error) {
	return "ok", nil
}

type loopHistory struct{ msgs []llm.Message }

func (h *loopHistory) HistoryMessages() []llm.Message { return h.msgs }
func (h *loopHistory) Append(m llm.Message) error {
	h.msgs = append(h.msgs, m)
	return nil
}

// The regression this guards: the agent loop records a call's arguments on
// tool_start only, so a summarizer that looked for file_path on tool_end
// reported no changed files for every run. Driving the real loop into the
// real recorder keeps the emitter and the reader from drifting apart again.
func TestSummarize_FindsTheFilesTheAgentLoopTouched(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(runDirFor(dir, "s"), Meta{RunID: "rt_loop", SessionID: "s"})
	if rec == nil {
		t.Fatal("expected recorder")
	}
	registry := llm.NewToolRegistry()
	registry.AppendTools(fileTool{name: "Write"}, fileTool{name: "Edit"})
	client := &oneCallClient{calls: []llm.ToolCall{
		{ID: "c1", Name: "Write", Arguments: `{"file_path":"/ws/out.md","content":"hi"}`},
		{ID: "c2", Name: "Edit", Arguments: `{"file_path":"/ws/in.md","old_string":"a","new_string":"b"}`},
	}}
	history := &loopHistory{msgs: []llm.Message{{Role: "user", Content: "go"}}}
	if _, _, _, err := agent.RunLoop(context.Background(), agent.RunLoopOpts{
		LLMClient:    client,
		ToolRegistry: registry,
		MaxIter:      3,
		History:      history,
		EventSink:    rec.Record,
	}); err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	f, err := os.Open(filepath.Join(dir, "s", "rt_loop.jsonl"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	got, err := Summarize(f)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	paths := map[string]string{}
	for _, tool := range got.Tools {
		paths[tool.Name] = tool.Path
	}
	if paths["Write"] != "/ws/out.md" || paths["Edit"] != "/ws/in.md" {
		t.Errorf("tool paths = %v, want each call's own file_path", paths)
	}
}
