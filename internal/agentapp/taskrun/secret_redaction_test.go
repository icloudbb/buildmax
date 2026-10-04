package taskrun

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/core/session"
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

// Everything the run wrote as it worked that carries model or tool text leaves
// the worker without the run's Secret values: the text logs (raw and in the
// quoted form slog writes a multi-line value), the session metadata whose
// title can come from the prompt, and the picker index that repeats it. The
// JSON stays readable, and the trace -- redacted by its own recorder -- is
// stored exactly as written rather than redacted a second time.
func TestUploadTaskGlobal_KeepsSecretValuesOutOfLogsAndSessionMetadata(t *testing.T) {
	const secret = "harbor-canary-value"
	const multiline = "first line of the harbor key\nsecond line of the harbor key"
	redactor := secretscan.NewRedactor([]string{secret, multiline})
	globalDir := t.TempDir()

	var logLine bytes.Buffer
	slog.New(slog.NewTextHandler(&logLine, nil)).Info("tool said", "text", "echo "+secret, "file", multiline)
	writeRunGlobalFile(t, globalDir, "logs/buildmax.log", logLine.String())
	writeRunGlobalFile(t, globalDir, "logs/buildmax-worker.log", "level=ERROR msg=\"run failed\" err=\"model wrote "+secret+"\"\n")
	meta := session.Meta{Version: 1, ID: "sid", Kind: session.KindUser, Title: "Deploy with " + secret, SelectedModel: "mock"}
	metaJSON, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeRunGlobalFile(t, globalDir, "sessions/sid/meta.json", string(metaJSON))
	writeRunGlobalFile(t, globalDir, "sessions/index.json", `[{"id":"sid","kind":"user","title":"Deploy with `+secret+`"}]`)
	const traceKey = "sessions/sid/traces/rt_1.jsonl"
	const trace = `{"type":"tool_end","result":"[redacted]"}` + "\n"
	writeRunGlobalFile(t, globalDir, traceKey, trace)

	persist := newFakePersistStorage()
	scope := RunScope{SpaceID: "s", TaskID: "t", TaskRunID: "r"}
	if _, err := uploadTaskGlobal(context.Background(), globalDir, scope, persist, traceKey, redactor); err != nil {
		t.Fatal(err)
	}
	stored := func(rel string) string {
		t.Helper()
		data, ok := persist.taskGlobal["s/t/r/"+rel]
		if !ok {
			t.Fatalf("%s was not uploaded", rel)
		}
		return string(data)
	}
	for _, rel := range []string{"logs/buildmax.log", "logs/buildmax-worker.log", "sessions/sid/meta.json", "sessions/index.json"} {
		got := stored(rel)
		if strings.Contains(got, secret) || strings.Contains(got, "harbor key") || !strings.Contains(got, "[redacted]") {
			t.Errorf("stored %s:\n%s", rel, got)
		}
	}
	var restored session.Meta
	if err := json.Unmarshal([]byte(stored("sessions/sid/meta.json")), &restored); err != nil || restored.Validate() != nil {
		t.Fatalf("stored meta.json no longer reads as session metadata: %v", err)
	}
	if restored.Title != "Deploy with [redacted]" || restored.SelectedModel != "mock" {
		t.Errorf("restored meta = %+v", restored)
	}
	if got := stored(traceKey); got != trace {
		t.Errorf("trace was rewritten on upload: %q", got)
	}
}

// A run whose failure text quotes a granted value reports it without the value:
// the error message is read by Space members and kept on the TaskRun.
func TestRunTask_KeepsSecretValuesOutOfTheErrorMessage(t *testing.T) {
	const secret = "harbor-canary-value"
	server, err := mockllm.Start(mockllm.Scenario{Steps: []mockllm.Step{
		{Status: 400, Error: "the request quoted " + secret + " and was refused"},
	}, Repeat: true})
	if err != nil {
		t.Fatalf("start mock model: %v", err)
	}
	t.Cleanup(server.Close)

	updater := &fakeUpdater{}
	persist := newFakePersistStorage()
	sessionID := "sid-error"
	err = RunTask(context.Background(), RunTaskInput{
		Task:            &coretask.Task{ID: "task1", SpaceID: "space1", SessionID: &sessionID},
		Run:             &coretask.Run{ID: "run1", Input: "use " + secret + " to deploy"},
		SessionID:       sessionID,
		Paths:           NewRuntimePathsFromRoot(t.TempDir()),
		Persist:         persist,
		Updater:         updater,
		SecretEnvGrants: map[string]string{"DEPLOY_TOKEN": secret},
		Model: config.ModelEntry{
			Model: "mock-model", Name: "mock", APIURL: server.BaseURL(mockllm.ProtocolOpenAIChat),
			APIKey: "mock-key", ContextWindow: 128000,
		},
	})
	if err == nil || !strings.Contains(err.Error(), secret) {
		t.Fatalf("RunTask err = %v, want the provider failure quoting the value (else this test proves nothing)", err)
	}
	req := updater.req
	if req == nil || req.Status != string(coretask.RunStatusFailed) || req.ErrorMessage == nil {
		t.Fatalf("report = %+v, want FAILED with an error message", req)
	}
	if msg := *req.ErrorMessage; strings.Contains(msg, secret) || !strings.Contains(msg, "the request quoted [redacted] and was refused") {
		t.Errorf("error message = %q", msg)
	}
	for key, data := range persist.taskGlobal {
		if bytes.Contains(data, []byte(secret)) {
			t.Errorf("stored %s carries the value:\n%s", key, data)
		}
	}
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
		sessionID, sender, model, "", ManagedInference{}, nil, "", "", nil, nil, nil, "", "", grants, nil, false)
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
		sessionID, nil, model, "", ManagedInference{}, nil, "", "", nil, nil, nil, "", "", grants, nil, false)
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
