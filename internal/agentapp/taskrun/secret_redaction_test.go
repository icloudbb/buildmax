package taskrun

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	blob "github.com/icloudbb/buildmax/internal/infra/objectstore"
	"github.com/icloudbb/buildmax/internal/testsupport/mockllm"
	"github.com/icloudbb/buildmax/internal/tool"
	"github.com/icloudbb/buildmax/internal/util/secretscan"
)

// recordingStreamSender keeps every delta the run streams, in order.
type recordingStreamSender struct {
	mu     sync.Mutex
	deltas []string
}

func (r *recordingStreamSender) SendDelta(_ context.Context, _, delta string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deltas = append(r.deltas, delta)
	return nil
}

func (r *recordingStreamSender) Flush(context.Context, string) error { return nil }

func (r *recordingStreamSender) joined() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.deltas, "")
}

// The adapter never hands the sender a value split across deltas, and releases
// what it held when the model call ends rather than when the next text arrives.
func TestStreamSinkAdapterRedactsAcrossDeltas(t *testing.T) {
	const secret = "ghp_splitAcrossTokens42"
	sender := &recordingStreamSender{}
	adapter := &streamSinkAdapter{ctx: context.Background(), streamSender: sender, taskRunID: "run1",
		redact: secretscan.NewRedactor([]string{secret}).Stream()}
	for _, d := range []string{"token: ghp_", "split", "Across", "Tokens42", " ok; then ghp_sp"} {
		adapter.OnDelta(d)
	}
	if got := sender.joined(); got != "token: [redacted] ok; then " {
		t.Fatalf("before the call ends the watcher has %q", got)
	}
	adapter.OnStreamEnd()
	if got := sender.joined(); got != "token: [redacted] ok; then ghp_sp" {
		t.Fatalf("after the call ends the watcher has %q", got)
	}
}

// The reported defect end to end through the worker runtime: a granted Secret
// value the model writes reaches the stream split across deltas (the mock model
// splits each reply in two, through the value). None of what leaves the run --
// the stream, the reported output, or the session journal a Continue run
// restores -- may carry it, and the Continue run must still resume.
func TestRunKeepsSecretValuesOutOfStreamOutputAndStoredSession(t *testing.T) {
	ctx := context.Background()
	const secret = "harbor-canary-value"
	grants := map[string]string{"DEPLOY_TOKEN": secret}
	// Each text is symmetric around the value, so the mock's split falls inside it.
	first := "Restored: " + secret + " as asked."
	final := "Answer: " + secret + " -- done"
	server, err := mockllm.Start(mockllm.Scenario{Steps: []mockllm.Step{
		{Text: first, ToolCalls: []mockllm.ToolCall{{ID: "call_g", Name: tool.ToolNameGlob, Args: map[string]any{"pattern": secret + "*.txt"}}}},
		{Text: final},
		{Text: "Continued."},
	}})
	if err != nil {
		t.Fatalf("start mock model: %v", err)
	}
	t.Cleanup(server.Close)
	model := config.ModelEntry{
		Model: "mock-model", Name: "mock", APIURL: server.BaseURL(mockllm.ProtocolOpenAIChat),
		APIKey: "mock-key", ContextWindow: 128000,
	}
	sessionID := "sid-secret"
	task := &coretask.Task{ID: "task1", SpaceID: "tm1", SessionID: &sessionID}
	firstRun := &coretask.Run{ID: "run1", Input: "print the deploy token"}
	firstDirs := testRunDirs(t)
	sender := &recordingStreamSender{}
	out, err := runAgentTask(ctx, firstRun, firstDirs.runDir, firstDirs.runGlobal, firstDirs.runOSHome,
		sessionID, sender, model, ManagedInference{}, nil, "", "", nil, nil, "", "", grants, nil, false)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	streamed := sender.joined()
	if strings.Contains(streamed, secret) {
		t.Fatalf("the stream carried the Secret value: %q", streamed)
	}
	wantStream := strings.ReplaceAll(first+"\n\n"+final, secret, "[redacted]")
	if streamed != wantStream {
		t.Fatalf("stream = %q, want %q", streamed, wantStream)
	}
	if output := string(out.output); strings.Contains(output, secret) || !strings.Contains(output, "Answer: [redacted] -- done") {
		t.Fatalf("reported output = %q", output)
	}

	// The run's own journal keeps the value while it works; the stored copy
	// does not.
	local, err := os.ReadFile(filepath.Join(firstDirs.runGlobal, "sessions", sessionID, sessionJournalFile))
	if err != nil || !strings.Contains(string(local), secret) {
		t.Fatalf("expected the run-local journal to hold the value (err %v)", err)
	}
	persist := newFakePersistStorage()
	scope := RunScope{SpaceID: task.SpaceID, TaskID: task.ID, TaskRunID: firstRun.ID}
	if _, err := uploadTaskGlobal(ctx, firstDirs.runGlobal, scope, persist, "", secretscan.NewRedactor(mapValues(grants))); err != nil {
		t.Fatal(err)
	}
	stored, err := persist.GetRunGlobal(ctx, blob.RunObjectRef{SpaceID: task.SpaceID, TaskID: task.ID, TaskRunID: firstRun.ID,
		RelPath: "sessions/" + sessionID + "/" + sessionJournalFile})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), secret) || !strings.Contains(string(stored), "[redacted]") {
		t.Fatalf("stored journal:\n%s", stored)
	}

	secondRun := &coretask.Run{ID: "run2", PreviousTaskRunID: &firstRun.ID, Input: "go on"}
	secondDirs := testRunDirs(t)
	restoreSessionFromPreviousRun(ctx, task, secondRun, secondDirs.runGlobal, persist)
	second, err := runAgentTask(ctx, secondRun, secondDirs.runDir, secondDirs.runGlobal, secondDirs.runOSHome,
		sessionID, nil, model, ManagedInference{}, nil, "", "", nil, nil, "", "", grants, nil, false)
	if err != nil {
		t.Fatalf("continued run: %v", err)
	}
	if string(second.output) != "Continued." {
		t.Fatalf("continued run output = %q", second.output)
	}
	calls := server.Requests()
	body := string(calls[len(calls)-1].Body)
	if strings.Contains(body, secret) || !strings.Contains(body, "Answer: [redacted] -- done") {
		t.Fatalf("the continued request does not carry the redacted conversation:\n%s", body)
	}
}
