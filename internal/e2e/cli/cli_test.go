package clie2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/testsupport/mockllm"
)

// binary is built once for the whole suite: the boundary under test is the
// released binary, and rebuilding it per test would spend the budget in
// docs/design/end-to-end-testing.md §5.1 on the compiler.
var binary string

func TestMain(m *testing.M) {
	root, err := moduleRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	dir, err := os.MkdirTemp("", "buildmax-cli-e2e")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// Windows will not execute a file without the extension, whatever -o names.
	binary = filepath.Join(dir, exeName("buildmax"))
	if _, statErr := os.Stat(filepath.Join(root, "go.mod")); statErr != nil {
		fmt.Fprintln(os.Stderr, statErr)
		os.Exit(1)
	}
	build := exec.Command("go", "build", "-o", binary, "./cmd/buildmax")
	build.Dir = root
	if out, buildErr := build.CombinedOutput(); buildErr != nil {
		fmt.Fprintf(os.Stderr, "build buildmax: %v\n%s", buildErr, out)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above %s", dir)
		}
		dir = parent
	}
}

// TestAPinnedWriteRunsAndReportsBack is the CLI golden path: the model asks for
// a write, the write happens in the workspace, and the model's own summary of
// it reaches stdout. Nothing below this level assembles all three.
func TestAPinnedWriteRunsAndReportsBack(t *testing.T) {
	server := startModel(t, "write-a-file.json")
	workspace := t.TempDir()
	home := writeHome(t, server, map[string]string{"Write": "allow"})

	result := run(t, home, workspace, "-p", "write notes.txt", "--output", "jsonl")

	if result.exitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout:\n%s\nstderr:\n%s", result.exitCode, result.stdout, result.stderr)
	}
	written, err := os.ReadFile(filepath.Join(workspace, "notes.txt"))
	if err != nil {
		t.Fatalf("the scripted write did not reach the workspace: %v", err)
	}
	if string(written) != "scripted content\n" {
		t.Fatalf("file content = %q, want the scripted content", written)
	}
	// The reply keeps what the model said before the tool call as well as after:
	// the run output is the whole assistant turn, not only its closing text.
	if reply := result.field("result", "reply"); reply != "writing it now\n\nwrote notes.txt" {
		t.Fatalf("reply = %q, want the model's full narration", reply)
	}
	if ended := result.events("tool_end"); len(ended) != 1 || ended[0]["tool"] != "Write" {
		t.Fatalf("tool_end events = %v, want one Write", ended)
	}
	if denied := result.events("tool_denied"); len(denied) != 0 {
		t.Fatalf("tool_denied events = %v, want none for a pinned allow", denied)
	}
	// The run has to have consumed the whole script. A run that stopped a turn
	// early would still have written the file and still look like a pass.
	if remaining := server.Remaining(); remaining != 0 {
		t.Fatalf("unconsumed scenario steps = %d, want 0", remaining)
	}
}

// TestTheReplyStreamsToStdout covers the path the CLI takes by default. Text
// output with streaming on is a different call into the model than the jsonl
// runs above make, and it is the one a person actually sees.
func TestTheReplyStreamsToStdout(t *testing.T) {
	server := startModel(t, "write-a-file.json")
	workspace := t.TempDir()
	home := writeHome(t, server, map[string]string{"Write": "allow"})

	result := run(t, home, workspace, "-p", "write notes.txt")

	if result.exitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout:\n%s\nstderr:\n%s", result.exitCode, result.stdout, result.stderr)
	}
	if !strings.Contains(result.stdout, "wrote notes.txt") {
		t.Fatalf("stdout does not carry the streamed reply:\n%s", result.stdout)
	}
	if !strings.Contains(result.stdout, "Tool calls: 1") {
		t.Fatalf("stdout does not report the tool call:\n%s", result.stdout)
	}
	// Without this the test would pass on a blocking call that printed the same
	// text, which is the thing it exists to tell apart.
	for i, call := range server.Requests() {
		if !call.Stream {
			t.Fatalf("model call %d was blocking, want streaming", i)
		}
	}
	if remaining := server.Remaining(); remaining != 0 {
		t.Fatalf("unconsumed scenario steps = %d, want 0", remaining)
	}
}

// TestAPinnedAskIsRefusedWithoutAHuman covers the other half of the gate: print
// mode has no approval handler, so a configured Ask is a refusal rather than a
// prompt nobody can answer. The run still finishes, and the file stays absent.
func TestAPinnedAskIsRefusedWithoutAHuman(t *testing.T) {
	server := startModel(t, "write-a-file.json")
	workspace := t.TempDir()
	home := writeHome(t, server, map[string]string{"Write": "ask"})

	result := run(t, home, workspace, "-p", "write notes.txt", "--output", "jsonl")

	if _, err := os.Stat(filepath.Join(workspace, "notes.txt")); !os.IsNotExist(err) {
		t.Fatalf("a refused write must not touch the workspace (stat err = %v)", err)
	}
	denied := result.events("tool_denied")
	if len(denied) != 1 || denied[0]["tool"] != "Write" {
		t.Fatalf("tool_denied events = %v, want one Write\nstdout:\n%s", denied, result.stdout)
	}
	if reason, _ := denied[0]["reason"].(string); reason == "" {
		t.Fatalf("denial carries no reason: %v", denied[0])
	}
	if remaining := server.Remaining(); remaining != 0 {
		t.Fatalf("unconsumed scenario steps = %d, want 0", remaining)
	}
}

func startModel(t *testing.T, scenarioFile string) *mockllm.Server {
	t.Helper()
	scenario, err := mockllm.LoadScenario(filepath.Join("testdata", scenarioFile))
	if err != nil {
		t.Fatalf("load scenario: %v", err)
	}
	server, err := mockllm.Start(scenario)
	if err != nil {
		t.Fatalf("start mockllm: %v", err)
	}
	t.Cleanup(server.Close)
	return server
}

// writeHome builds the throwaway BUILDMAX_HOME a run reads: one model entry
// pointing at the scripted server, and the permissions the test depends on
// written down rather than inherited from whatever the tool defaults to today.
func writeHome(t *testing.T, server *mockllm.Server, permissions map[string]string) string {
	t.Helper()
	return writeHomeWith(t, server, permissions, "")
}

// writeHomeWith is writeHome plus extra top-level settings, for a test whose
// subject is a setting rather than a permission. extra is appended verbatim, so
// it carries its own key and indentation.
func writeHomeWith(t *testing.T, server *mockllm.Server, permissions map[string]string, extra string) string {
	t.Helper()
	home := t.TempDir()
	settings := strings.Builder{}
	settings.WriteString("log_level: error\n")
	settings.WriteString("models:\n")
	settings.WriteString("  - model: mock-model\n")
	settings.WriteString("    name: mock\n")
	fmt.Fprintf(&settings, "    api_url: %q\n", server.BaseURL(mockllm.ProtocolOpenAIChat))
	settings.WriteString("    api_key: mock-key\n")
	settings.WriteString("    context_window: 128000\n")
	if len(permissions) > 0 {
		settings.WriteString("tools:\n  permissions:\n")
		// Sorted, so two homes written from the same map are the same file. A
		// test that compares two runs cannot have its own fixture vary.
		for _, tool := range slices.Sorted(maps.Keys(permissions)) {
			fmt.Fprintf(&settings, "    %s: %s\n", tool, permissions[tool])
		}
	}
	settings.WriteString(extra)
	if err := os.WriteFile(filepath.Join(home, "settings.yaml"), []byte(settings.String()), 0o600); err != nil {
		t.Fatalf("write settings.yaml: %v", err)
	}
	return home
}

type runResult struct {
	stdout   string
	stderr   string
	exitCode int
	lines    []map[string]any
}

// events returns the jsonl records of one type, in order.
func (r runResult) events(kind string) []map[string]any {
	var out []map[string]any
	for _, line := range r.lines {
		if line["type"] == kind {
			out = append(out, line)
		}
	}
	return out
}

// field reads one field of the first record of a type, as a string.
func (r runResult) field(kind, name string) string {
	for _, line := range r.lines {
		if line["type"] == kind {
			s, _ := line[name].(string)
			return s
		}
	}
	return ""
}

func run(t *testing.T, home, workspace string, args ...string) runResult {
	t.Helper()
	cmd := exec.Command(binary, append(args, "--workspace", workspace)...)
	cmd.Env = append(os.Environ(), "BUILDMAX_HOME="+home)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", binary, err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		result := runResult{stdout: stdout.String(), stderr: stderr.String()}
		var exitErr *exec.ExitError
		switch {
		case err == nil:
		case asExitError(err, &exitErr):
			result.exitCode = exitErr.ExitCode()
		default:
			t.Fatalf("run %s: %v", binary, err)
		}
		result.lines = parseJSONL(result.stdout)
		return result
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("the run did not finish in 30s\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
		return runResult{}
	}
}

func asExitError(err error, target **exec.ExitError) bool {
	exitErr, ok := err.(*exec.ExitError)
	if ok {
		*target = exitErr
	}
	return ok
}

// parseJSONL keeps only the records: anything else on stdout is log noise that
// a failure message should still show, but that no assertion depends on.
func parseJSONL(out string) []map[string]any {
	var lines []map[string]any
	scanner := bufio.NewScanner(strings.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var record map[string]any
		if json.Unmarshal(scanner.Bytes(), &record) != nil {
			continue
		}
		if _, ok := record["type"]; ok {
			lines = append(lines, record)
		}
	}
	return lines
}

// TestARunWritesASessionBundle is the end-to-end proof that the storage layout
// the documentation describes is the one the built binary actually produces.
// The unit tests cover each layer; this covers the stack, through a real
// process, which is where a wiring mistake would otherwise survive.
func TestARunWritesASessionBundle(t *testing.T) {
	server := startModel(t, "write-a-file.json")
	workspace := t.TempDir()
	home := writeHome(t, server, map[string]string{"Write": "allow"})

	result := run(t, home, workspace, "-p", "write notes.txt", "--output", "jsonl")
	if result.exitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr:\n%s", result.exitCode, result.stderr)
	}
	sessionID := result.field("result", "session_id")
	if sessionID == "" {
		t.Fatal("the run reported no session id")
	}

	bundle := filepath.Join(home, "sessions", sessionID)
	for _, name := range []string{"meta.json", "history.jsonl"} {
		if _, err := os.Stat(filepath.Join(bundle, name)); err != nil {
			t.Errorf("%s missing from the bundle: %v", name, err)
		}
	}
	// The picker projection has to name the session, or /sessions would not
	// find a conversation that exists.
	index, err := os.ReadFile(filepath.Join(home, "sessions", "index.json"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	if !strings.Contains(string(index), sessionID) {
		t.Errorf("index.json does not list the session:\n%s", index)
	}

	// The journal records the tool boundary before the result, which is the
	// distinction the whole format exists for. Asserting the order here proves
	// the agent loop, the committing context, and the codec agree about it.
	journal, err := os.ReadFile(filepath.Join(bundle, "history.jsonl"))
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	started := strings.Index(string(journal), `"type":"tool_execution_started"`)
	done := strings.Index(string(journal), `"type":"tool_result"`)
	switch {
	case started < 0:
		t.Errorf("no tool_execution_started record:\n%s", journal)
	case done < 0:
		t.Errorf("no tool_result record:\n%s", journal)
	case started > done:
		t.Error("the tool result was recorded before the boundary that precedes it")
	}

	// Traces live inside the bundle now, not under a second root.
	if entries, err := os.ReadDir(filepath.Join(bundle, "traces")); err != nil || len(entries) == 0 {
		t.Errorf("no trace in the bundle (err = %v)", err)
	}
	if _, err := os.Stat(filepath.Join(home, "traces")); !os.IsNotExist(err) {
		t.Errorf("the old traces root still exists (stat err = %v)", err)
	}
}

// TestResumingASessionSendsTheEarlierTurnBackToTheModel is what -r has to mean.
// The bundle test above proves a run writes its journal; this proves a second
// process reads that journal back and hands it to the model as the conversation
// so far. Only two real runs over one directory assemble both halves.
func TestResumingASessionSendsTheEarlierTurnBackToTheModel(t *testing.T) {
	server := startModel(t, "resume-a-session.json")
	workspace := t.TempDir()
	home := writeHome(t, server, nil)

	first := run(t, home, workspace, "-p", "remember the code word: albatross", "--output", "jsonl")
	if first.exitCode != 0 {
		t.Fatalf("first run exit code = %d, want 0\nstderr:\n%s", first.exitCode, first.stderr)
	}
	sessionID := first.field("result", "session_id")
	if sessionID == "" {
		t.Fatal("the first run reported no session id")
	}

	second := run(t, home, workspace, "-r", sessionID, "-p", "what was the code word?", "--output", "jsonl")
	if second.exitCode != 0 {
		t.Fatalf("resumed run exit code = %d, want 0\nstderr:\n%s", second.exitCode, second.stderr)
	}
	// A resume that quietly started a new conversation would answer just as
	// well and abandon the session, so the id is the assertion, not the reply.
	if got := second.field("result", "session_id"); got != sessionID {
		t.Fatalf("resumed session id = %q, want the session it was told to resume, %q", got, sessionID)
	}

	calls := server.Requests()
	if len(calls) != 2 {
		t.Fatalf("model calls = %d, want 2, one per run", len(calls))
	}
	// What the second run sent is the whole proof: the earlier exchange has to
	// be in the request, or the model is answering with no memory of it.
	sent := string(calls[1].Body)
	for _, want := range []string{
		"remember the code word: albatross",
		"noted: the code word is albatross",
		"what was the code word?",
	} {
		if !strings.Contains(sent, want) {
			t.Errorf("the resumed run did not send %q to the model:\n%s", want, sent)
		}
	}

	// Both turns land in the one journal. A resume that appended somewhere else
	// would still have sent the right history and still lose the next run.
	journal, err := os.ReadFile(filepath.Join(home, "sessions", sessionID, "history.jsonl"))
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if !strings.Contains(string(journal), "albatross") || !strings.Contains(string(journal), "what was the code word?") {
		t.Errorf("the journal does not hold both turns:\n%s", journal)
	}
	if remaining := server.Remaining(); remaining != 0 {
		t.Fatalf("unconsumed scenario steps = %d, want 0", remaining)
	}
}

// TestASessionIDCreatesTheSessionThenReusesIt is what --session-id has to mean,
// as its help promises: load if it exists, else create. Unlike -r, which errors
// on an unknown id, the first run must create the named session; a second run
// under the same id must continue it rather than start an empty one.
func TestASessionIDCreatesTheSessionThenReusesIt(t *testing.T) {
	server := startModel(t, "resume-a-session.json")
	workspace := t.TempDir()
	home := writeHome(t, server, nil)

	const id = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"

	first := run(t, home, workspace, "--session-id", id, "-p", "remember the code word: albatross", "--output", "jsonl")
	if first.exitCode != 0 {
		t.Fatalf("first run exit code = %d, want 0 (a missing --session-id must be created)\nstderr:\n%s", first.exitCode, first.stderr)
	}
	if got := first.field("result", "session_id"); got != id {
		t.Fatalf("created session id = %q, want the id it was given, %q", got, id)
	}

	second := run(t, home, workspace, "--session-id", id, "-p", "what was the code word?", "--output", "jsonl")
	if second.exitCode != 0 {
		t.Fatalf("reused run exit code = %d, want 0\nstderr:\n%s", second.exitCode, second.stderr)
	}
	if got := second.field("result", "session_id"); got != id {
		t.Fatalf("reused session id = %q, want the id it continued, %q", got, id)
	}

	// The second run sending the first turn back is the proof it continued the
	// created session rather than opening an empty one under the same id.
	calls := server.Requests()
	if len(calls) != 2 {
		t.Fatalf("model calls = %d, want 2, one per run", len(calls))
	}
	if sent := string(calls[1].Body); !strings.Contains(sent, "albatross") {
		t.Errorf("the reused run did not send the earlier turn to the model:\n%s", sent)
	}
	if remaining := server.Remaining(); remaining != 0 {
		t.Fatalf("unconsumed scenario steps = %d, want 0", remaining)
	}
}

// TestContinueResumesTheMostRecentSession covers the flag a person actually
// types: -r needs an id from somewhere, -c has to find the session itself.
// Choosing the wrong one is invisible until the model answers out of the wrong
// conversation, which is why this asserts on what was sent rather than on the
// reply.
func TestContinueResumesTheMostRecentSession(t *testing.T) {
	server := startModel(t, "three-short-answers.json")
	workspace := t.TempDir()
	home := writeHome(t, server, nil)

	older := run(t, home, workspace, "-p", "this is the older session", "--output", "jsonl")
	newer := run(t, home, workspace, "-p", "this is the newer session", "--output", "jsonl")
	olderID, newerID := older.field("result", "session_id"), newer.field("result", "session_id")
	if olderID == "" || newerID == "" || olderID == newerID {
		t.Fatalf("the two runs did not leave two sessions: %q and %q", olderID, newerID)
	}

	continued := run(t, home, workspace, "-c", "-p", "which session is this?", "--output", "jsonl")
	if continued.exitCode != 0 {
		t.Fatalf("continued run exit code = %d, want 0\nstderr:\n%s", continued.exitCode, continued.stderr)
	}
	if got := continued.field("result", "session_id"); got != newerID {
		t.Fatalf("-c resumed %q, want the most recent session %q (the other was %q)", got, newerID, olderID)
	}

	calls := server.Requests()
	if len(calls) != 3 {
		t.Fatalf("model calls = %d, want 3, one per run", len(calls))
	}
	sent := string(calls[2].Body)
	if !strings.Contains(sent, "this is the newer session") {
		t.Errorf("-c did not send the most recent conversation to the model:\n%s", sent)
	}
	if strings.Contains(sent, "this is the older session") {
		t.Errorf("-c sent the other session's history too:\n%s", sent)
	}
	if remaining := server.Remaining(); remaining != 0 {
		t.Fatalf("unconsumed scenario steps = %d, want 0", remaining)
	}
}
