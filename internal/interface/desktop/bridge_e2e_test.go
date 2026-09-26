package desktop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/testsupport/mockllm"
)

// The Desktop bridge end to end: the bound methods, the events the frontend
// listens to, the approval round trip, the local runtime, and the session left
// on disk. Nothing below this level assembles them together, and the React app
// above it can only be as right as what these events say.
//
// It stops at the bridge. The Wails window, the webview, and the React app are
// not here: driving those needs a running `wails dev` and a display, which is
// the packaged-app smoke this design defers. See
// docs/design/end-to-end-testing.md §6.

// uiEvents records what the frontend would have received. The run emits from
// its own goroutine, so every read and write is locked.
type uiEvents struct {
	mu     sync.Mutex
	events []recordedEvent
}

type recordedEvent struct {
	name string
	data any
}

func (u *uiEvents) emit(_ context.Context, name string, data any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.events = append(u.events, recordedEvent{name: name, data: data})
}

// waitFor blocks until an event of this name has arrived, and returns its data.
// The failure prints every event seen: "timed out waiting for X" with no
// account of what did happen is the least useful message a suite can give.
func (u *uiEvents) waitFor(t *testing.T, name string) any {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		u.mu.Lock()
		for _, e := range u.events {
			if e.name == name {
				data := e.data
				u.mu.Unlock()
				return data
			}
		}
		u.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no %q event within 20s. What the frontend was sent:\n%s", name, u.summary())
	return nil
}

// waitForNth blocks until the nth event of this name has arrived, 1-based, and
// returns its data. A suite that drives two turns cannot use waitFor for the
// second: it would match the first turn's event and return before the second
// turn has done anything.
func (u *uiEvents) waitForNth(t *testing.T, name string, n int) any {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		u.mu.Lock()
		var seen int
		for _, e := range u.events {
			if e.name != name {
				continue
			}
			seen++
			if seen == n {
				data := e.data
				u.mu.Unlock()
				return data
			}
		}
		u.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("fewer than %d %q events within 20s. What the frontend was sent:\n%s", n, name, u.summary())
	return nil
}

func (u *uiEvents) seen(name string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, e := range u.events {
		if e.name == name {
			return true
		}
	}
	return false
}

func (u *uiEvents) summary() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	var b strings.Builder
	for _, e := range u.events {
		fmt.Fprintf(&b, "  %s %+v\n", e.name, e.data)
	}
	if b.Len() == 0 {
		return "  (nothing)"
	}
	return b.String()
}

// bridge starts an app against a scripted model in a temporary home.
func bridge(t *testing.T, scenario mockllm.Scenario, permissions map[string]string) (*App, *uiEvents, *mockllm.Server, string) {
	t.Helper()
	server, err := mockllm.Start(scenario)
	if err != nil {
		t.Fatalf("start mockllm: %v", err)
	}
	t.Cleanup(server.Close)

	home := t.TempDir()
	settings := strings.Builder{}
	settings.WriteString("log_level: error\nmodels:\n  - model: mock-model\n    name: mock\n")
	fmt.Fprintf(&settings, "    api_url: %q\n", server.BaseURL(mockllm.ProtocolOpenAIChat))
	settings.WriteString("    api_key: mock-key\n    context_window: 128000\n")
	if len(permissions) > 0 {
		settings.WriteString("tools:\n  permissions:\n")
		for tool, action := range permissions {
			fmt.Fprintf(&settings, "    %s: %s\n", tool, action)
		}
	}
	if err := os.WriteFile(filepath.Join(home, "settings.yaml"), []byte(settings.String()), 0o600); err != nil {
		t.Fatalf("write settings.yaml: %v", err)
	}
	t.Setenv("BUILDMAX_HOME", home)

	events := &uiEvents{}
	app := NewApp()
	app.emit = events.emit
	app.Startup(context.Background())
	t.Cleanup(func() { app.Shutdown(context.Background()) })

	workspace := t.TempDir()
	project, err := app.OpenProject(workspace, "bridge probe")
	if err != nil {
		t.Fatalf("open project: %v", err)
	}
	return app, events, server, project.ID
}

func writeScenario() mockllm.Scenario {
	return mockllm.Scenario{Steps: []mockllm.Step{
		{
			Text:      "writing it now",
			ToolCalls: []mockllm.ToolCall{{Name: "Write", Args: map[string]any{"file_path": "notes.txt", "content": "scripted content\n"}}},
			Usage:     &mockllm.Usage{PromptTokens: 120, CompletionTokens: 18},
		},
		{Text: "wrote notes.txt", Usage: &mockllm.Usage{PromptTokens: 140, CompletionTokens: 4}},
		// A turn that ran a tool also asks for a recap, which is a model call of
		// its own. These runs use the shipped default configuration, so the
		// scenario has to describe it.
		{Text: `{"recap": "Wrote notes.txt."}`, Usage: &mockllm.Usage{PromptTokens: 200, CompletionTokens: 12}},
	}}
}

func TestBridgeApprovesAToolCallAndFinishesTheRun(t *testing.T) {
	app, events, server, projectID := bridge(t, writeScenario(), map[string]string{"Write": "ask"})
	workspace := app.mustProjectFolder(t, projectID)

	if _, err := app.SendMessageStream(projectID, "", "write notes.txt"); err != nil {
		t.Fatalf("send: %v", err)
	}

	// The prompt is gated, so the frontend is asked before anything is written.
	request, ok := events.waitFor(t, eventApprovalRequest).(*ApprovalRequestPayload)
	if !ok || request.ToolName != "Write" {
		t.Fatalf("approval request = %+v, want one for Write", request)
	}
	if _, err := os.Stat(filepath.Join(workspace, "notes.txt")); !os.IsNotExist(err) {
		t.Fatalf("the file existed before the approval was answered (stat err = %v)", err)
	}

	if err := app.RespondApproval(request.ApprovalID, "once"); err != nil {
		t.Fatalf("answer the approval: %v", err)
	}

	done, ok := events.waitFor(t, eventStreamDone).(*ReplyPayload)
	// The reply keeps the narration before the tool call as well as the text
	// after it: the run output is the whole assistant turn, not only its close.
	if !ok || done.Reply != "writing it now\n\nwrote notes.txt" {
		t.Fatalf("stream-done = %+v, want the model's full narration", done)
	}
	written, err := os.ReadFile(filepath.Join(workspace, "notes.txt"))
	if err != nil {
		t.Fatalf("the approved write did not reach the project folder: %v\n%s", err, events.summary())
	}
	if string(written) != "scripted content\n" {
		t.Fatalf("file content = %q, want the scripted content", written)
	}
	// A new chat's prompt is routed to the session the run adopted, not to an
	// empty id another new chat could also claim.
	adopted, ok := events.waitFor(t, eventSessionAdopted).(*SessionAdoptedPayload)
	if !ok || request.SessionID == "" || request.SessionID != adopted.SessionID || request.SessionID != done.SessionID {
		t.Fatalf("approval session = %q, want the adopted session (%+v, done %q)", request.SessionID, adopted, done.SessionID)
	}
	if request.ProjectID != projectID {
		t.Fatalf("approval project = %q, want %q", request.ProjectID, projectID)
	}
	if !events.seen(eventStreamDelta) {
		t.Fatalf("no streamed delta reached the frontend:\n%s", events.summary())
	}
	if remaining := server.Remaining(); remaining != 0 {
		t.Fatalf("unconsumed scenario steps = %d, want 0", remaining)
	}

	// The session survives the run, which is what lets the app reopen it.
	sessions, err := app.ListSessions()
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) == 0 {
		t.Fatal("the finished run left no session to reopen")
	}
	detail, err := app.GetSession(done.SessionID)
	if err != nil {
		t.Fatalf("reopen the session: %v", err)
	}
	if len(detail.Messages) == 0 {
		t.Fatal("the reopened session carries no messages")
	}
}

func TestBridgeDeniesAToolCallAndSaysSo(t *testing.T) {
	// Unlike writeScenario there is no recap step: a denied write changed
	// nothing, so the turn has no change to describe and the digest gate makes
	// no recap call. The closing reply asks nothing, so no suggestion is made
	// either — the run ends after these two steps.
	scenario := mockllm.Scenario{Steps: []mockllm.Step{
		{
			Text:      "writing it now",
			ToolCalls: []mockllm.ToolCall{{Name: "Write", Args: map[string]any{"file_path": "notes.txt", "content": "scripted content\n"}}},
			Usage:     &mockllm.Usage{PromptTokens: 120, CompletionTokens: 18},
		},
		{Text: "The write was denied, so nothing changed.", Usage: &mockllm.Usage{PromptTokens: 140, CompletionTokens: 8}},
	}}
	app, events, server, projectID := bridge(t, scenario, map[string]string{"Write": "ask"})
	workspace := app.mustProjectFolder(t, projectID)

	if _, err := app.SendMessageStream(projectID, "", "write notes.txt"); err != nil {
		t.Fatalf("send: %v", err)
	}
	request, ok := events.waitFor(t, eventApprovalRequest).(*ApprovalRequestPayload)
	if !ok {
		t.Fatalf("no approval request:\n%s", events.summary())
	}
	if err := app.RespondApproval(request.ApprovalID, "deny"); err != nil {
		t.Fatalf("answer the approval: %v", err)
	}

	events.waitFor(t, eventStreamDone)
	if _, err := os.Stat(filepath.Join(workspace, "notes.txt")); !os.IsNotExist(err) {
		t.Fatalf("a denied write must not touch the project folder (stat err = %v)", err)
	}
	// The denial has to reach the frontend as a finished tool, or the UI shows a
	// call that never ends.
	denied := false
	events.mu.Lock()
	for _, e := range events.events {
		if payload, ok := e.data.(*ToolEndPayload); ok && payload.Denied {
			denied = true
		}
	}
	events.mu.Unlock()
	if !denied {
		t.Fatalf("no denied tool-end reached the frontend:\n%s", events.summary())
	}
	if remaining := server.Remaining(); remaining != 0 {
		t.Fatalf("unconsumed scenario steps = %d, want 0", remaining)
	}
}

func TestBridgeSurfacesAProviderFailure(t *testing.T) {
	scenario := mockllm.Scenario{Steps: []mockllm.Step{{Status: 400, Error: "scripted provider refusal"}}}
	app, events, _, projectID := bridge(t, scenario, nil)

	if _, err := app.SendMessageStream(projectID, "", "anything"); err != nil {
		t.Fatalf("send: %v", err)
	}
	failure, ok := events.waitFor(t, eventStreamError).(*StreamErrorPayload)
	if !ok || failure.Message == "" {
		t.Fatalf("stream-error = %+v, want a message the UI can show", failure)
	}
	// The reason has to survive to the frontend. A run that fails with "error"
	// leaves the user with nothing to act on.
	if !strings.Contains(failure.Message, "scripted provider refusal") {
		t.Fatalf("stream-error message = %q, want the provider's own words", failure.Message)
	}
	if events.seen(eventStreamDone) {
		t.Fatalf("a failed run must not also report done:\n%s", events.summary())
	}
}

// mustProjectFolder returns the folder a project was created against.
func (a *App) mustProjectFolder(t *testing.T, projectID string) string {
	t.Helper()
	projects, err := a.ListProjects()
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	for _, p := range projects {
		if p.ID == projectID {
			return p.DefaultWorkspace
		}
	}
	t.Fatalf("project %s is not listed", projectID)
	return ""
}

// The turn digest reaches the frontend as its own event and stays out of the
// session. Both halves matter: the recap is only useful if the UI is told, and
// the whole premise is that neither half is something the model reads back.
func TestBridgeSendsTheTurnDigestWithoutPuttingItInTheSession(t *testing.T) {
	scenario := writeScenario()
	// The turn has to end by asking the user something, or there is no
	// suggestion to predict and the digest is asked for the recap alone.
	scenario.Steps[1].Text = "wrote notes.txt — commit it?"
	// The digest step writeScenario already carries, answered with both halves.
	scenario.Steps[2].Text = `{"recap": "Wrote notes.txt with the scripted content.", "suggestion": "yes, commit it"}`
	app, events, server, projectID := bridge(t, scenario, map[string]string{"Write": "allow"})

	if _, err := app.SendMessageStream(projectID, "", "write notes.txt"); err != nil {
		t.Fatalf("send: %v", err)
	}

	digest, ok := events.waitFor(t, eventTurnDigest).(*TurnDigestPayload)
	if !ok {
		t.Fatalf("turn-digest payload = %T, want *TurnDigestPayload", digest)
	}
	if digest.Recap != "Wrote notes.txt with the scripted content." {
		t.Errorf("recap = %q", digest.Recap)
	}
	if digest.Suggestion != "yes, commit it" {
		t.Errorf("suggestion = %q", digest.Suggestion)
	}

	done, ok := events.waitFor(t, eventStreamDone).(*ReplyPayload)
	if !ok {
		t.Fatalf("stream-done payload = %T, want *ReplyPayload", done)
	}
	if remaining := server.Remaining(); remaining != 0 {
		t.Fatalf("unconsumed scenario steps = %d, want 0 — the digest call is one of them", remaining)
	}

	detail, err := app.GetSession(done.SessionID)
	if err != nil {
		t.Fatalf("reopen the session: %v", err)
	}
	for _, m := range detail.Messages {
		if strings.Contains(m.Content, "Wrote notes.txt with the scripted content.") {
			t.Fatalf("the recap was written into the conversation as a %s message", m.Role)
		}
		if strings.Contains(m.Content, "yes, commit it") {
			t.Fatalf("the suggestion was written into the conversation as a %s message", m.Role)
		}
	}
}
