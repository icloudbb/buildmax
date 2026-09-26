package cli

import (
	"context"
	"errors"
	"github.com/icloudbb/buildmax/internal/util"
	"time"

	"github.com/icloudbb/buildmax/internal/agentapp"
	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/core/agent"
	"github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/icloudbb/buildmax/internal/core/session"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcp "github.com/icloudbb/buildmax/internal/infra/mcp"
	"github.com/icloudbb/buildmax/internal/infra/sessionstore"

	tea "charm.land/bubbletea/v2"
)

func testSessionContext() *agentapp.SessionContext {
	return agentapp.NewSessionContext("")
}

func testAgentApp(t *testing.T, workspace string) *agentapp.AgentApp {
	t.Helper()
	app, err := agentapp.NewAgentApp(agentapp.AppConfig{
		WorkspaceDir: workspace,
		EnableMCP:    true,
	})
	if err != nil {
		t.Fatalf("NewAgentApp: %v", err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Fatalf("AgentApp.Close: %v", err)
		}
	})
	return app
}

func writeTestSettings(t *testing.T, raw string) {
	t.Helper()
	path := config.SettingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll settings dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatalf("WriteFile settings: %v", err)
	}
}

func TestModelFocusInput(t *testing.T) {
	opts := TUIOpts{
		Session: testSessionContext(),
	}
	m := NewModel(opts)
	if !m.FocusInput() {
		t.Error("initial focus should be on input")
	}
	// Tab is a no-op in transcript mode; focus stays on input.
	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	mod, ok := m2.(*Model)
	if !ok {
		t.Fatalf("Update returned %T, want *Model", m2)
	}
	if !mod.FocusInput() {
		t.Error("after Tab, focus should still be on input (transcript mode has no viewport toggle)")
	}
}

func TestViewFooterPresent(t *testing.T) {
	sess := agentapp.NewSessionContext("")
	if err := sess.Append(llm.Message{Role: "assistant", Content: "short"}); err != nil {
		t.Fatal(err)
	}
	m := NewModel(TUIOpts{
		Session:   sess,
		ModelName: "test-model",
		Workspace: util.FixedRoot("/tmp/workspace"),
	})

	m2, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	mod, ok := m2.(*Model)
	if !ok {
		t.Fatalf("Update returned %T, want *Model", m2)
	}

	// In transcript mode the view is only the bottom strip, not terminal-height lines.
	view := mod.View().Content
	if !strings.Contains(view, "model: test-model") {
		t.Fatalf("view should contain model name, got %q", view)
	}
	if !strings.Contains(view, "ctrl+c: quit") {
		t.Fatalf("view should contain shortcuts, got %q", view)
	}
}

func TestViewFooterShowsOnlyContextShare(t *testing.T) {
	m := NewModel(TUIOpts{
		Session:   testSessionContext(),
		ModelName: "test-model",
		Workspace: util.FixedRoot("/tmp/workspace"),
		RunStatus: agentapp.RunUsage{
			ContextTokens:         500,
			ContextWindow:         1000,
			PromptTokens:          100,
			CompletionTokens:      20,
			TotalPromptTokens:     3334,
			TotalCompletionTokens: 998,
		},
	})

	view := m.View().Content
	if !strings.Contains(view, "ctx: 50% (500/1k)") {
		t.Fatalf("view should contain context share, got %q", view)
	}
	// Per-run token and cache breakdowns moved to /info; the footer keeps only ctx.
	if strings.Contains(view, "tokens(in/out)") || strings.Contains(view, "cache(r/w)") {
		t.Fatalf("view should not contain token or cache breakdown, got %q", view)
	}
}

func TestEventSinkForwardsLLMBoundaries(t *testing.T) {
	ch := make(chan tea.Msg, 3)
	sink := eventSinkToChannel(context.Background(), ch)

	sink(agent.Event{Kind: agent.EventLLMStart, ContextTokens: 100, ContextWindow: 1000})
	sink(agent.Event{Kind: agent.EventLLMEnd, Content: "done", PromptTokens: 10, CompletionTokens: 5})

	st, ok := (<-ch).(runStatusMsg)
	if !ok {
		t.Fatal("first forwarded message should be runStatusMsg")
	}
	if st.Status.ContextTokens != 100 || st.Status.ContextWindow != 1000 {
		t.Fatalf("run status = %+v, want context 100/1000", st.Status)
	}
	if _, ok := (<-ch).(llmStartMsg); !ok {
		t.Fatal("second forwarded message should be llmStartMsg")
	}
	end, ok := (<-ch).(llmEndMsg)
	if !ok {
		t.Fatal("third forwarded message should be llmEndMsg")
	}
	if end.Content != "done" {
		t.Fatalf("llmEndMsg content = %q, want %q", end.Content, "done")
	}
	if end.PromptTokens != 10 || end.CompletionTokens != 5 {
		t.Fatalf("llmEndMsg tokens = %d/%d, want 10/5", end.PromptTokens, end.CompletionTokens)
	}
}

func TestFormatRunStatusShowsOnlyContext(t *testing.T) {
	got := formatRunStatus(agentapp.RunUsage{
		ContextTokens:         500,
		ContextWindow:         1000,
		PromptTokens:          100,
		CompletionTokens:      20,
		TotalPromptTokens:     3334,
		TotalCompletionTokens: 998,
	})
	want := "ctx: 50% (500/1k)"
	if got != want {
		t.Fatalf("formatRunStatus() = %q, want %q", got, want)
	}
}

func TestFormatRunStatusUnknownWindow(t *testing.T) {
	if got := formatRunStatus(agentapp.RunUsage{}); got != "ctx: unknown" {
		t.Fatalf("formatRunStatus() = %q, want %q", got, "ctx: unknown")
	}
}

func TestFormatTokenUsageValue(t *testing.T) {
	if got := formatTokenUsageValue(100, 20, 3334, 998); got != "100/20 (3.3k/998)" {
		t.Fatalf("formatTokenUsageValue() = %q", got)
	}
}

func TestMergeRunStatusAccumulatesRunningTotals(t *testing.T) {
	prev := agentapp.RunUsage{PromptTokens: 10, CompletionTokens: 5, TotalPromptTokens: 100, TotalCompletionTokens: 50}
	got := mergeRunStatus(prev, agentapp.RunUsage{ContextTokens: 800, ContextWindow: 1000, PromptTokens: 25, CompletionTokens: 9})
	if got.PromptTokens != 25 || got.CompletionTokens != 9 {
		t.Fatalf("current tokens = %d/%d, want 25/9", got.PromptTokens, got.CompletionTokens)
	}
	if got.TotalPromptTokens != 115 || got.TotalCompletionTokens != 54 {
		t.Fatalf("total tokens = %d/%d, want 115/54", got.TotalPromptTokens, got.TotalCompletionTokens)
	}
}

func TestLLMEndRendersAndClearsCurrentResponseBuffer(t *testing.T) {
	m := NewModel(TUIOpts{
		Session:      testSessionContext(),
		Workspace:    util.FixedRoot(t.TempDir()),
		GlamourStyle: "dark",
	})
	m.busy = true

	next, _ := m.Update(llmStartMsg{})
	mod := next.(*Model)
	mod.streamingBuffer = "old text"
	next, _ = mod.Update(llmStartMsg{})
	mod = next.(*Model)
	if mod.streamingBuffer != "" {
		t.Fatalf("llmStartMsg should clear prior response buffer, got %q", mod.streamingBuffer)
	}

	next, _ = mod.Update(streamDeltaMsg{Delta: "## One"})
	mod = next.(*Model)
	if mod.streamingBuffer != "## One" {
		t.Fatalf("streamingBuffer = %q, want %q", mod.streamingBuffer, "## One")
	}

	next, cmd := mod.Update(llmEndMsg{Content: "ignored fallback"})
	mod = next.(*Model)
	if mod.streamingBuffer != "" {
		t.Fatalf("llmEndMsg should clear response buffer, got %q", mod.streamingBuffer)
	}
	if cmd == nil {
		t.Fatal("llmEndMsg with content should return render command")
	}
	msg := cmd()
	rendered, ok := msg.(assistantRenderedMsg)
	if !ok {
		t.Fatalf("render command returned %T, want assistantRenderedMsg", msg)
	}
	if !rendered.continueStream {
		t.Fatal("rendered per-response message should continue stream after printing")
	}
	if !strings.Contains(rendered.line, "One") {
		t.Fatalf("rendered line should contain response content, got %q", rendered.line)
	}

	// agentDoneMsg still ends the run with a queue drain, but it must not render the
	// reply a second time after llmEndMsg already printed it.
	_, cmd = mod.Update(agentDoneMsg{})
	if cmd == nil {
		t.Fatal("agentDoneMsg should return the queue drain command")
	}
	if _, ok := cmd().(drainQueueMsg); !ok {
		t.Fatal("agentDoneMsg should not render again after llmEndMsg cleared the buffer")
	}
}

func typeInto(t *testing.T, m *Model, text string) *Model {
	t.Helper()
	var mod tea.Model = m
	for _, r := range text {
		mod, _ = mod.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return mod.(*Model)
}

func TestEnterWhileBusyQueuesMessage(t *testing.T) {
	m := NewModel(TUIOpts{Session: testSessionContext(), Workspace: util.FixedRoot(t.TempDir())})
	m.busy = true

	m = typeInto(t, m, "later question")
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	mod := next.(*Model)

	if got := mod.queue.Snapshot(); len(got) != 1 || got[0] != "later question" {
		t.Fatalf("queue = %v, want [later question]", got)
	}
	if mod.inputBlock.Value() != "" {
		t.Errorf("input should be cleared after queueing, got %q", mod.inputBlock.Value())
	}
	if cmd == nil {
		t.Fatal("queueing should print a line to scrollback")
	}
	if !strings.Contains(mod.View().Content, "1 queued") {
		t.Error("busy hint should report the queue depth")
	}
	if !strings.Contains(mod.renderFooterView(), "queued: 1") {
		t.Error("footer should report the queue depth")
	}
}

func TestEnterWhileBusyRejectsSlashCommand(t *testing.T) {
	m := NewModel(TUIOpts{Session: testSessionContext(), Workspace: util.FixedRoot(t.TempDir())})
	m.busy = true

	m = typeInto(t, m, "/model")
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	mod := next.(*Model)

	if mod.queue.Len() != 0 {
		t.Errorf("slash command should not be queued, queue = %v", mod.queue.Snapshot())
	}
	if mod.err == "" {
		t.Error("rejecting a queued slash command should explain why")
	}
}

func TestEnterWhileBusyReportsFullQueue(t *testing.T) {
	m := NewModel(TUIOpts{Session: testSessionContext(), Workspace: util.FixedRoot(t.TempDir())})
	m.busy = true
	for i := 0; i < agent.DefaultMaxQueuedMessages; i++ {
		if _, err := m.queue.Enqueue("filler"); err != nil {
			t.Fatalf("Enqueue filler #%d: %v", i, err)
		}
	}

	m = typeInto(t, m, "one too many")
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	mod := next.(*Model)

	if mod.queue.Len() != agent.DefaultMaxQueuedMessages {
		t.Errorf("queue length = %d, want %d", mod.queue.Len(), agent.DefaultMaxQueuedMessages)
	}
	if mod.err == "" {
		t.Error("a rejected message should surface an error rather than vanish")
	}
	if mod.inputBlock.Value() == "" {
		t.Error("input should keep the text that could not be queued")
	}
}

func TestEscWhileBusyClearsInputThenUnqueues(t *testing.T) {
	m := NewModel(TUIOpts{Session: testSessionContext(), Workspace: util.FixedRoot(t.TempDir())})
	m.busy = true
	if _, err := m.queue.Enqueue("queued one"); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	m = typeInto(t, m, "half typed")

	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	mod := next.(*Model)
	if mod.inputBlock.Value() != "" {
		t.Fatalf("first esc should clear the input, got %q", mod.inputBlock.Value())
	}
	if mod.queue.Len() != 1 {
		t.Fatalf("first esc should not touch the queue, len = %d", mod.queue.Len())
	}

	next, cmd := mod.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	mod = next.(*Model)
	if mod.queue.Len() != 0 {
		t.Errorf("second esc should take back the last queued message, len = %d", mod.queue.Len())
	}
	if cmd == nil {
		t.Error("taking a message back should print a line to scrollback")
	}
}

// The queue is drained one message per turn: agentDoneMsg asks for a drain, and the
// drain starts exactly one run.
func TestQueueDrainsOneMessagePerTurn(t *testing.T) {
	m := NewModel(TUIOpts{Session: testSessionContext(), Workspace: util.FixedRoot(t.TempDir())})
	m.busy = true
	for _, text := range []string{"first queued", "second queued"} {
		if _, err := m.queue.Enqueue(text); err != nil {
			t.Fatalf("Enqueue %q: %v", text, err)
		}
	}

	next, cmd := m.Update(agentDoneMsg{})
	mod := next.(*Model)
	if mod.busy {
		t.Fatal("agentDoneMsg should end the run before the queue is drained")
	}
	if cmd == nil {
		t.Fatal("agentDoneMsg should return a drain command")
	}
	if _, ok := cmd().(drainQueueMsg); !ok {
		t.Fatal("agentDoneMsg should return a drain command")
	}

	next, cmd = mod.Update(drainQueueMsg{})
	mod = next.(*Model)
	if !mod.busy {
		t.Error("draining a queued message should start a run")
	}
	if cmd == nil {
		t.Error("starting a queued run should return a command")
	}
	if got := mod.queue.Snapshot(); len(got) != 1 || got[0] != "second queued" {
		t.Errorf("queue after drain = %v, want [second queued]", got)
	}
}

// A failed run still drains: leaving queued messages stranded with no way to
// release them is worse than letting each fail on its own turn.
func TestQueueDrainsAfterFailedRun(t *testing.T) {
	m := NewModel(TUIOpts{Session: testSessionContext(), Workspace: util.FixedRoot(t.TempDir())})
	m.busy = true
	if _, err := m.queue.Enqueue("still wanted"); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	next, cmd := m.Update(agentDoneMsg{Err: errors.New("llm call: boom")})
	mod := next.(*Model)
	if mod.err == "" {
		t.Error("a failed run should surface its error")
	}
	if cmd == nil {
		t.Fatal("a failed run should still drain the queue")
	}
	if _, ok := cmd().(drainQueueMsg); !ok {
		t.Fatal("a failed run should still drain the queue")
	}
	if mod.queue.Len() != 1 {
		t.Errorf("queue should survive the failed run, len = %d", mod.queue.Len())
	}
}

func TestDrainQueueIsNoOpWhileBusy(t *testing.T) {
	m := NewModel(TUIOpts{Session: testSessionContext(), Workspace: util.FixedRoot(t.TempDir())})
	m.busy = true
	if _, err := m.queue.Enqueue("wait your turn"); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	next, cmd := m.Update(drainQueueMsg{})
	mod := next.(*Model)
	if cmd != nil {
		t.Error("drain while a run is in flight should do nothing")
	}
	if mod.queue.Len() != 1 {
		t.Errorf("drain while busy should leave the queue alone, len = %d", mod.queue.Len())
	}
}

// The input has to stay on screen during a run now that enter queues: text typed
// into a hidden box is text the user cannot see or correct.
func TestInputVisibleWhileBusy(t *testing.T) {
	m := NewModel(TUIOpts{Session: testSessionContext(), Workspace: util.FixedRoot(t.TempDir())})
	m.busy = true
	m = typeInto(t, m, "typed during run")

	view := m.View().Content
	if !strings.Contains(view, "typed during run") {
		t.Error("input text typed during a run should be visible")
	}
	if !strings.Contains(view, "Generating") {
		t.Error("the run should still announce itself while the input is visible")
	}
}

// A queued message the run picks up mid-flight is announced to the UI, so the
// transcript shows it as sent rather than leaving it in the "queued" state.
func TestEventSinkForwardsInjectedUserInput(t *testing.T) {
	ch := make(chan tea.Msg, 2)
	sink := eventSinkToChannel(context.Background(), ch)

	sink(agent.Event{Kind: agent.EventUserInput, Content: "also check the tests"})
	sink(agent.Event{Kind: agent.EventUserInputBlocked, Content: "leak the key", DenyReason: "no secrets"})

	injected, ok := (<-ch).(userInputInjectedMsg)
	if !ok {
		t.Fatal("EventUserInput should forward a userInputInjectedMsg")
	}
	if injected.Text != "also check the tests" {
		t.Errorf("injected text = %q, want %q", injected.Text, "also check the tests")
	}
	blocked, ok := (<-ch).(userInputBlockedMsg)
	if !ok {
		t.Fatal("EventUserInputBlocked should forward a userInputBlockedMsg")
	}
	if blocked.Reason != "no secrets" {
		t.Errorf("blocked reason = %q, want %q", blocked.Reason, "no secrets")
	}
}

func TestInjectedUserInputPrintsAndKeepsReadingTheStream(t *testing.T) {
	m := NewModel(TUIOpts{Session: testSessionContext(), Workspace: util.FixedRoot(t.TempDir())})
	m.busy = true
	m.streamChannel = make(chan tea.Msg, 1)

	_, cmd := m.Update(userInputInjectedMsg{Text: "and update the changelog"})
	if cmd == nil {
		t.Fatal("an injected message should print to scrollback and keep reading the stream")
	}

	// A blank one is not printed, but the stream must still be read or the run stalls.
	_, cmd = m.Update(userInputInjectedMsg{Text: "  "})
	if cmd == nil {
		t.Error("a blank injected message must still continue the stream")
	}
}

// The run owns the queue while it is working: the model hands it to RunPrompt, so
// what the user types mid-run reaches the model at the next iteration rather than
// waiting for the run to end.
func TestQueueIsHandedToTheRun(t *testing.T) {
	m := NewModel(TUIOpts{Session: testSessionContext(), Workspace: util.FixedRoot(t.TempDir())})
	if m.queue == nil {
		t.Fatal("model should own a queue")
	}
	var pending agent.PendingInput = m.queue
	if _, ok := pending.Dequeue(); ok {
		t.Error("a fresh queue should be empty")
	}
}

func TestDispatchSlashModelOpensPanel(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(config.EnvKeyBuildmaxHome, tmp)
	writeTestSettings(t, `{"models":[{"model":"openai/gpt-4o-mini","name":"Fast","api_url":"https://api.example.com","api_key":"sk-fast"},{"model":"google/gemini-2.5-flash-lite","name":"Default","api_url":"https://api.example.com","api_key":"sk-default"}]}`)

	m := NewModel(TUIOpts{
		App:       testAgentApp(t, tmp),
		Session:   testSessionContext(),
		ModelName: "Default",
	})

	got, _ := dispatchSlashCommand(m, "/model")
	mod, ok := got.(*Model)
	if !ok {
		t.Fatalf("dispatchSlashCommand returned %T, want *Model", got)
	}
	if mod.slashModel == nil {
		t.Fatal("slash model panel should be open")
	}
	if mod.slashModel.Current != "Default" {
		t.Fatalf("current model = %q, want %q", mod.slashModel.Current, "Default")
	}
	if mod.slashModel.Selected != 1 {
		t.Fatalf("selected = %d, want 1 for current model", mod.slashModel.Selected)
	}
	if len(mod.slashModel.Entries) != 2 {
		t.Fatalf("entries len = %d, want 2", len(mod.slashModel.Entries))
	}
	rendered := mod.buildSlashModelContent(80)
	if !strings.Contains(rendered, "› * Default -> google/gemini-2.5-flash-lite") {
		t.Fatalf("rendered panel should highlight current selection, got %q", rendered)
	}
	if !strings.Contains(rendered, "  Fast -> openai/gpt-4o-mini") {
		t.Fatalf("rendered panel should list named model and provider id, got %q", rendered)
	}
	if !strings.Contains(rendered, "↑↓ select · enter switch") {
		t.Fatalf("rendered panel should include usage hint, got %q", rendered)
	}
}

func TestDispatchSlashModelPrefillsSelectionAndEnterSwitches(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(config.EnvKeyBuildmaxHome, tmp)
	writeTestSettings(t, `{"models":[{"model":"openai/gpt-4o-mini","name":"Fast","api_url":"https://api.example.com","api_key":"sk-fast"},{"model":"google/gemini-2.5-flash-lite","name":"Default","api_url":"https://api.example.com","api_key":"sk-default"}]}`)

	m := NewModel(TUIOpts{
		App:       testAgentApp(t, tmp),
		Session:   testSessionContext(),
		ModelName: "Default",
	})

	got, _ := dispatchSlashCommand(m, "/model", "Fast")
	mod, ok := got.(*Model)
	if !ok {
		t.Fatalf("dispatchSlashCommand returned %T, want *Model", got)
	}
	if mod.slashModel == nil {
		t.Fatal("slash model panel should be open")
	}
	if mod.slashModel.Selected != 0 {
		t.Fatalf("selected = %d, want 0 for Fast", mod.slashModel.Selected)
	}
	next, _ := mod.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	after := next.(*Model)
	if gotName := after.opts.Session.ModelName(""); gotName != "Fast" {
		t.Fatalf("session model = %q, want %q", gotName, "Fast")
	}
	if after.opts.ModelName != "Fast" {
		t.Fatalf("opts model = %q, want %q", after.opts.ModelName, "Fast")
	}
	if after.slashModel != nil {
		t.Fatalf("slash model panel should be closed after selection, got %+v", after.slashModel)
	}
}

func TestSlashModelArrowSelectionSwitchesModel(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(config.EnvKeyBuildmaxHome, tmp)
	writeTestSettings(t, `{"models":[{"model":"openai/gpt-4o-mini","name":"Fast","api_url":"https://api.example.com","api_key":"sk-fast"},{"model":"google/gemini-2.5-flash-lite","name":"Default","api_url":"https://api.example.com","api_key":"sk-default"}]}`)

	m := NewModel(TUIOpts{
		App:       testAgentApp(t, tmp),
		Session:   testSessionContext(),
		ModelName: "Default",
	})

	got, _ := dispatchSlashCommand(m, "/model")
	mod := got.(*Model)
	next, _ := mod.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	afterNav := next.(*Model)
	if afterNav.slashModel == nil || afterNav.slashModel.Selected != 0 {
		t.Fatalf("selected = %+v, want 0 after up", afterNav.slashModel)
	}
	confirmed, _ := afterNav.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	final := confirmed.(*Model)
	if gotName := final.opts.Session.ModelName(""); gotName != "Fast" {
		t.Fatalf("session model = %q, want %q", gotName, "Fast")
	}
	if final.slashModel != nil {
		t.Fatalf("slash model panel should be closed after selection, got %+v", final.slashModel)
	}
}

func TestSlashCompletionShowsPrefixMatch(t *testing.T) {
	m0 := NewModel(TUIOpts{
		Session:   testSessionContext(),
		Workspace: util.FixedRoot(t.TempDir()),
	})
	m1, _ := m0.Update(tea.WindowSizeMsg{Width: 80, Height: 14})
	mod := m1.(*Model)
	mod.inputBlock.SetValue("/mc")
	mod.inputBlock.SyncHeight()
	mod.syncSlashPopupFromInput()
	if mod.slashPopup == nil || len(mod.slashPopup.matches) != 1 || mod.slashPopup.matches[0] != "/mcp" {
		t.Fatalf("slashPopup=%+v", mod.slashPopup)
	}
	v := mod.View().Content
	if !strings.Contains(v, "/mcp") || !strings.Contains(v, "Commands") {
		t.Fatalf("view should list command, got: %s", v[:min(500, len(v))])
	}
}

func TestSlashCommandUnknownDoesNotAppendSession(t *testing.T) {
	sess := agentapp.NewSessionContext("")
	m := NewModel(TUIOpts{
		Session:   sess,
		Workspace: util.FixedRoot(t.TempDir()),
	})
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	mod := m2.(*Model)
	mod.inputBlock.SetValue("/nope")
	mod.inputBlock.SyncHeight()
	before := len(mod.opts.Session.Messages())
	next, _ := mod.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	after := next.(*Model)
	if len(after.opts.Session.Messages()) != before {
		t.Fatalf("session messages should not change, before=%d after=%d", before, len(after.opts.Session.Messages()))
	}
	if after.err == "" {
		t.Fatal("expected footer error for unknown slash command")
	}
	if !strings.Contains(after.err, "/skills") || !strings.Contains(after.err, "/mcp") {
		t.Fatalf("error should mention builtins, got %q", after.err)
	}
}

func TestSlashSessionListsNewestFirst(t *testing.T) {
	sess := agentapp.NewSessionContext("")
	dir := t.TempDir()
	// Seeded through the store so the creation times are explicit: the panel
	// orders by them, and two sessions made a microsecond apart would not prove
	// anything about the ordering.
	seedSession(t, dir, "sess-old", "older chat", time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC))
	seedSession(t, dir, "sess-new", "newer chat", time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC))
	m0 := NewModel(TUIOpts{
		Session:     sess,
		Workspace:   util.FixedRoot(t.TempDir()),
		SessionsDir: dir,
	})
	m1, _ := m0.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	mod := m1.(*Model)
	mod.inputBlock.SetValue("/sessions")
	mod.inputBlock.SyncHeight()
	next, _ := mod.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	after := next.(*Model)
	if after.slashSession == nil {
		t.Fatal("expected session overlay after /session")
	}
	if after.slashSession.LoadError != "" || after.slashSession.Empty {
		t.Fatalf("unexpected overlay state: %+v", after.slashSession)
	}
	if len(after.slashSession.Filtered) != 2 {
		t.Fatalf("entries = %d, want 2: %v", len(after.slashSession.Filtered), after.slashSession.Filtered)
	}
	if after.slashSession.Filtered[0].ID != "sess-new" || after.slashSession.Filtered[0].Title != "newer chat" {
		t.Errorf("first entry should be newer session, got %+v", after.slashSession.Filtered[0])
	}
	if after.slashSession.Filtered[1].ID != "sess-old" || after.slashSession.Filtered[1].Title != "older chat" {
		t.Errorf("second entry should be older session, got %+v", after.slashSession.Filtered[1])
	}
	v := after.View().Content
	if !strings.Contains(v, "Sessions") {
		t.Fatalf("view missing title, got %q", v[:min(400, len(v))])
	}
}

func TestSlashSkillsDoesNotAppendSession(t *testing.T) {
	t.Setenv("BUILDMAX_HOME", t.TempDir())
	sess := agentapp.NewSessionContext("")
	ws := t.TempDir()
	m := NewModel(TUIOpts{
		Session:   sess,
		Workspace: util.FixedRoot(ws),
	})
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 14})
	mod := m2.(*Model)
	mod.inputBlock.SetValue("/skills")
	mod.inputBlock.SyncHeight()
	before := len(mod.opts.Session.Messages())
	next, _ := mod.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	after := next.(*Model)
	if len(after.opts.Session.Messages()) != before {
		t.Fatalf("session messages should not change, before=%d after=%d", before, len(after.opts.Session.Messages()))
	}
	if after.slashSkills == nil {
		t.Fatal("expected skills overlay after /skills")
	}
	if after.busy {
		t.Fatal("slash command must not start agent")
	}
}

func TestSlashSkillsOpensOverlayAndEscCloses(t *testing.T) {
	t.Setenv("BUILDMAX_HOME", t.TempDir())
	sess := agentapp.NewSessionContext("")
	ws := t.TempDir()
	skillRoot := filepath.Join(ws, ".buildmax", "skills", "listdemo")
	if err := os.MkdirAll(skillRoot, 0755); err != nil {
		t.Fatal(err)
	}
	content := "# Listdemo\n\nA skill for the TUI list test.\n"
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	m0 := NewModel(TUIOpts{
		Session:   sess,
		Workspace: util.FixedRoot(ws),
	})
	m1, _ := m0.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	mod := m1.(*Model)
	mod.inputBlock.SetValue("/skills")
	mod.inputBlock.SyncHeight()
	next, _ := mod.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	after := next.(*Model)
	if after.slashSkills == nil {
		t.Fatal("expected skills overlay")
	}
	if len(after.slashSkills.Entries) != 1 {
		t.Fatalf("entries=%d want 1: %+v", len(after.slashSkills.Entries), after.slashSkills.Entries)
	}
	if after.slashSkills.Entries[0].Name != "listdemo" {
		t.Errorf("skill name=%q", after.slashSkills.Entries[0].Name)
	}
	v := after.View().Content
	if !strings.Contains(v, "Skills") || !strings.Contains(v, "listdemo") || !strings.Contains(v, "A skill for the TUI list test.") {
		t.Fatalf("view missing expected content: %s", v[:min(600, len(v))])
	}
	next2, _ := after.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	closed := next2.(*Model)
	if closed.slashSkills != nil {
		t.Fatal("esc should close skills overlay")
	}
	if !closed.FocusInput() {
		t.Fatal("esc should return focus to input")
	}
}

func TestSlashSkillsEmptyOverlay(t *testing.T) {
	t.Setenv("BUILDMAX_HOME", t.TempDir())
	sess := agentapp.NewSessionContext("")
	ws := t.TempDir()
	m0 := NewModel(TUIOpts{
		Session:   sess,
		Workspace: util.FixedRoot(ws),
	})
	m1, _ := m0.Update(tea.WindowSizeMsg{Width: 80, Height: 16})
	mod := m1.(*Model)
	mod.inputBlock.SetValue("/skills")
	mod.inputBlock.SyncHeight()
	next, _ := mod.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	after := next.(*Model)
	if after.slashSkills == nil {
		t.Fatal("expected skills overlay")
	}
	if len(after.slashSkills.Entries) != 0 {
		t.Fatalf("expected no skills, got %d", len(after.slashSkills.Entries))
	}
	v := after.View().Content
	if !strings.Contains(v, "No skills found") {
		t.Fatalf("expected empty state in view: %s", v[:min(500, len(v))])
	}
}

func writeSkillFixture(t *testing.T, ws, name, body string) {
	t.Helper()
	skillRoot := filepath.Join(ws, ".buildmax", "skills", name)
	if err := os.MkdirAll(skillRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestSlashSkillsSelectFillsInputWithoutSending(t *testing.T) {
	t.Setenv("BUILDMAX_HOME", t.TempDir())
	sess := agentapp.NewSessionContext("")
	ws := t.TempDir()
	writeSkillFixture(t, ws, "listdemo", "# Listdemo\n\nA skill for the TUI list test.\n")
	m0 := NewModel(TUIOpts{Session: sess, Workspace: util.FixedRoot(ws)})
	m1, _ := m0.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	mod := m1.(*Model)
	mod.inputBlock.SetValue("/skills")
	mod.inputBlock.SyncHeight()
	next, _ := mod.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	after := next.(*Model)
	if after.slashSkills == nil || len(after.slashSkills.Filtered) != 1 {
		t.Fatalf("expected one filtered skill, got %+v", after.slashSkills)
	}
	before := len(after.opts.Session.Messages())
	next2, _ := after.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	closed := next2.(*Model)
	if closed.slashSkills != nil {
		t.Fatal("enter should close the skills panel")
	}
	if closed.busy {
		t.Fatal("selecting a skill must not start a run")
	}
	if len(closed.opts.Session.Messages()) != before {
		t.Fatal("selecting a skill must not append a session message")
	}
	if got := closed.inputBlock.Value(); got != "/listdemo" {
		t.Fatalf("input = %q, want %q", got, "/listdemo")
	}
}

func TestSlashSkillsSelectDoesNotClobberExistingDraft(t *testing.T) {
	t.Setenv("BUILDMAX_HOME", t.TempDir())
	sess := agentapp.NewSessionContext("")
	ws := t.TempDir()
	writeSkillFixture(t, ws, "listdemo", "# Listdemo\n\nA skill for the TUI list test.\n")
	m0 := NewModel(TUIOpts{Session: sess, Workspace: util.FixedRoot(ws)})
	m1, _ := m0.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	mod := m1.(*Model)
	mod.inputBlock.SetValue("/skills")
	mod.inputBlock.SyncHeight()
	next, _ := mod.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	after := next.(*Model)
	if after.slashSkills == nil {
		t.Fatal("expected skills overlay")
	}
	// A draft left in the input before confirming wins over the fill.
	after.inputBlock.SetValue("do not overwrite me")
	next2, _ := after.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	closed := next2.(*Model)
	if closed.slashSkills != nil {
		t.Fatal("enter should still close the skills panel")
	}
	if got := closed.inputBlock.Value(); got != "do not overwrite me" {
		t.Fatalf("input = %q, want the existing draft preserved", got)
	}
}

func TestSlashCommandSendsSkillNameAsMessage(t *testing.T) {
	t.Setenv("BUILDMAX_HOME", t.TempDir())
	sess := agentapp.NewSessionContext("")
	ws := t.TempDir()
	writeSkillFixture(t, ws, "listdemo", "# Listdemo\n\nA skill for the TUI list test.\n")
	m0 := NewModel(TUIOpts{Session: sess, Workspace: util.FixedRoot(ws)})
	m1, _ := m0.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	mod := m1.(*Model)
	mod.inputBlock.SetValue("/listdemo draw a poster")
	mod.inputBlock.SyncHeight()
	next, _ := mod.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	after := next.(*Model)
	if after.err != "" {
		t.Fatalf("typed skill name should not error, got %q", after.err)
	}
	if !after.busy {
		t.Fatal("a message naming a loaded skill should start a run")
	}
}

func TestSlashCommandUnknownStillErrors(t *testing.T) {
	t.Setenv("BUILDMAX_HOME", t.TempDir())
	sess := agentapp.NewSessionContext("")
	ws := t.TempDir()
	m0 := NewModel(TUIOpts{Session: sess, Workspace: util.FixedRoot(ws)})
	m1, _ := m0.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	mod := m1.(*Model)
	mod.inputBlock.SetValue("/nonexistent-thing")
	mod.inputBlock.SyncHeight()
	next, _ := mod.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	after := next.(*Model)
	if after.err == "" {
		t.Fatal("an unrecognized command that is not a skill should still error")
	}
	if after.busy {
		t.Fatal("an unrecognized command must not start a run")
	}
}

func TestSlashMCPOpensOverlayAndEmptyConfig(t *testing.T) {
	sess := agentapp.NewSessionContext("")
	workspace := t.TempDir()
	m := NewModel(TUIOpts{
		App:       testAgentApp(t, workspace),
		Session:   sess,
		Workspace: util.FixedRoot(workspace),
	})
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	mod := m2.(*Model)
	mod.inputBlock.SetValue("/mcp")
	mod.inputBlock.SyncHeight()
	next, cmd := mod.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	fm := next.(*Model)
	if cmd != nil {
		t.Fatal("did not expect probe command")
	}
	if fm.slashMCP == nil {
		t.Fatal("expected MCP overlay")
	}
	if len(fm.slashMCP.Servers) != 0 {
		t.Fatal("expected empty MCP config in temp workspace")
	}
}

func TestMCPOverlayEscClosesAndShowsServers(t *testing.T) {
	sess := agentapp.NewSessionContext("")
	m0 := NewModel(TUIOpts{
		Session:   sess,
		Workspace: util.FixedRoot(t.TempDir()),
	})
	m1, _ := m0.Update(tea.WindowSizeMsg{Width: 80, Height: 18})
	mod := m1.(*Model)
	st := &slashMCPState{
		Servers: []mcp.MCPServerStatus{
			{ID: "a", Type: "stdio", OK: true, ToolCount: 2},
		},
	}
	mod.slashMCP = st
	mod.activePanel = st
	fm := mod
	if fm.slashMCP == nil {
		t.Fatal("expected overlay with rows")
	}
	if len(fm.slashMCP.Servers) != 1 {
		t.Fatalf("servers=%d", len(fm.slashMCP.Servers))
	}
	out := fm.View().Content
	if !strings.Contains(out, "a") || !strings.Contains(out, "connected") {
		t.Fatalf("view should list server, got: %s", out[:min(400, len(out))])
	}
	next2, _ := fm.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	closed := next2.(*Model)
	if closed.slashMCP != nil {
		t.Fatal("esc should close MCP overlay")
	}
	if !closed.FocusInput() {
		t.Fatal("esc should return focus to input")
	}
}

func TestWrapLine(t *testing.T) {
	tests := []struct {
		line   string
		width  int
		expect int // number of lines
	}{
		{"short", 80, 1},
		{"", 10, 1},
		{"abcdefghij", 5, 2},
		{"abc", 3, 1},
		{"abcd", 2, 2},
	}
	for _, tt := range tests {
		got := wrapLine(tt.line, tt.width)
		if len(got) != tt.expect {
			t.Errorf("wrapLine(%q, %d) returned %d lines, want %d: %q", tt.line, tt.width, len(got), tt.expect, got)
		}
	}
	// Word wrap: break at space so "**File " is first line; remainder wraps to "Operations:*" then "*" (3 lines total)
	got := wrapLine("**File Operations:**", 12)
	if len(got) != 3 {
		t.Fatalf("wrapLine(\"**File Operations:**\", 12) want 3 lines, got %d: %q", len(got), got)
	}
	if got[0] != "**File " || got[1] != "Operations:*" || got[2] != "*" {
		t.Errorf("wrapLine(\"**File Operations:**\", 12) = %q, want [\"**File \", \"Operations:*\", \"*\"]", got)
	}
}

// TestToolEndPairsWithItsOwnCall is the defect phase 1 of
// docs/design/parallel-tool-execution.md exists to prevent: with one slot for
// the live call's arguments, a result arriving for the first of two overlapping
// calls rendered the second call's arguments.
func TestToolEndPairsWithItsOwnCall(t *testing.T) {
	m := &Model{width: 80}
	handleToolStart(m, toolStartMsg{CallID: "a", Name: "Read", Args: `{"file_path":"first.go"}`})
	handleToolStart(m, toolStartMsg{CallID: "b", Name: "Read", Args: `{"file_path":"second.go"}`})

	if len(m.activeTools) != 2 {
		t.Fatalf("activeTools = %d, want 2 in flight", len(m.activeTools))
	}

	// Finish the second call first: completion order need not match call order.
	if line := m.finishTool("b", "Read", "*", ""); !strings.Contains(line, "second.go") {
		t.Errorf("line = %q, want the arguments of call b", line)
	}
	if line := m.finishTool("a", "Read", "*", ""); !strings.Contains(line, "first.go") {
		t.Errorf("line = %q, want the arguments of call a", line)
	}
	if len(m.activeTools) != 0 {
		t.Errorf("activeTools = %d, want the live view emptied", len(m.activeTools))
	}
}

// TestToolEndWithoutCallIDFallsBack keeps a surface that emits no id working
// the way it does today rather than leaking a spinner forever.
func TestToolEndWithoutCallIDFallsBack(t *testing.T) {
	m := &Model{width: 80}
	handleToolStart(m, toolStartMsg{Name: "Bash", Args: `{"command":"ls"}`})
	if line := m.finishTool("", "Bash", "*", ""); !strings.Contains(line, "ls") {
		t.Errorf("line = %q, want the pending call's arguments", line)
	}
	if len(m.activeTools) != 0 {
		t.Errorf("activeTools = %d, want the call cleared", len(m.activeTools))
	}
}

// TestUnknownToolEndDoesNotDropAnotherCall guards the fallback from clearing an
// unrelated call when an id does not match anything in flight.
func TestUnknownToolEndDoesNotDropAnotherCall(t *testing.T) {
	m := &Model{width: 80}
	handleToolStart(m, toolStartMsg{CallID: "a", Name: "Read", Args: `{"file_path":"keep.go"}`})
	line := m.finishTool("zzz", "Write", "*", "")
	if strings.Contains(line, "keep.go") {
		t.Errorf("line = %q, want no arguments borrowed from another call", line)
	}
	if len(m.activeTools) != 1 {
		t.Errorf("activeTools = %d, want the unrelated call still in flight", len(m.activeTools))
	}
}

// The footer says where prompts go. It is the session's mode rather than the
// model's, so it does not move when /model switches models.
func TestModeTag(t *testing.T) {
	cases := []struct {
		name      string
		serverURL string
		want      string
	}{
		{"local mode", "", "local"},
		{"managed names its deployment", "http://localhost:5678", "localhost:5678"},
		{"https is stripped too", "https://buildmax.example.com", "buildmax.example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := modeTag(tc.serverURL); got != tc.want {
				t.Errorf("modeTag(%q) = %q, want %q", tc.serverURL, got, tc.want)
			}
		})
	}
}

// The footer renders without an app, which is what a test model and an early
// failure both have. ManagedServerURL answers "" for a nil app, so the tag is
// local — the mode a session with nothing configured is actually in.
func TestFooterWithoutAnAppSaysLocal(t *testing.T) {
	m := NewModel(TUIOpts{Session: testSessionContext(), Workspace: util.FixedRoot(t.TempDir()), ModelName: "local"})
	footer := m.renderFooterView()
	if !strings.Contains(footer, "model: local (local)") {
		t.Errorf("footer = %q, want it to name the model and the mode", footer)
	}
}

// seedSession writes one session bundle with an explicit creation time.
func seedSession(t *testing.T, dir, id, title string, createdAt time.Time) {
	t.Helper()
	meta := session.NewMeta(id, session.KindUser, createdAt)
	meta.Title = title
	if err := sessionstore.NewFileStore(dir).Create(context.Background(), meta); err != nil {
		t.Fatalf("create session %s: %v", id, err)
	}
}

// --- Turn digest: the recap line and the ghost suggestion ---

func doneWithDigest(recap, suggestion string) agentDoneMsg {
	return agentDoneMsg{Result: agentapp.RunResult{
		Digest: agentapp.TurnDigest{Recap: recap, Suggestion: suggestion},
	}}
}

func TestSuggestionIsOfferedAsGhostAndAcceptedWithTab(t *testing.T) {
	m := NewModel(TUIOpts{Session: testSessionContext(), Workspace: util.FixedRoot(t.TempDir())})
	m.busy = true

	next, _ := m.Update(doneWithDigest("", "yes, use the second option"))
	mod := next.(*Model)
	if got := mod.inputBlock.Ghost(); got != "yes, use the second option" {
		t.Fatalf("ghost = %q, want the suggestion on offer", got)
	}
	if mod.inputBlock.Value() != "" {
		t.Fatal("a suggestion must not put text in the input until it is accepted")
	}

	next, _ = mod.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	mod = next.(*Model)
	if got := mod.inputBlock.Value(); got != "yes, use the second option" {
		t.Fatalf("input after tab = %q, want the accepted suggestion", got)
	}
	if mod.inputBlock.Ghost() != "" {
		t.Error("an accepted suggestion should no longer be on offer")
	}
}

// Typing withdraws the offer: what the user is about to send is what they
// typed, so tab must not overwrite it.
func TestTypingWithdrawsTheSuggestion(t *testing.T) {
	m := NewModel(TUIOpts{Session: testSessionContext(), Workspace: util.FixedRoot(t.TempDir())})
	m.busy = true
	next, _ := m.Update(doneWithDigest("", "yes"))
	mod := typeInto(t, next.(*Model), "no, do the other thing")

	if mod.inputBlock.Ghost() != "" {
		t.Error("a suggestion should not be on offer once the user has typed")
	}
	next, _ = mod.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if got := next.(*Model).inputBlock.Value(); got != "no, do the other thing" {
		t.Fatalf("input after tab = %q, want what the user typed", got)
	}
}

func TestStartingATurnClearsTheSuggestion(t *testing.T) {
	m := NewModel(TUIOpts{Session: testSessionContext(), Workspace: util.FixedRoot(t.TempDir())})
	m.busy = true
	next, _ := m.Update(doneWithDigest("", "yes"))
	mod := next.(*Model)

	startRun(mod, "something else entirely")
	if mod.inputBlock.Ghost() != "" {
		t.Error("the question a suggestion answered is gone once a turn starts")
	}
}

func TestEscapeDismissesTheSuggestion(t *testing.T) {
	m := NewModel(TUIOpts{Session: testSessionContext(), Workspace: util.FixedRoot(t.TempDir())})
	m.busy = true
	next, _ := m.Update(doneWithDigest("", "yes"))

	next, _ = next.(*Model).Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if got := next.(*Model).inputBlock.Ghost(); got != "" {
		t.Errorf("ghost = %q after esc, want it dismissed", got)
	}
}

// The recap goes to scrollback, never to the session: a turn ending with one
// still hands the same drain command back.
func TestRecapPrintsToScrollbackBeforeDraining(t *testing.T) {
	m := NewModel(TUIOpts{Session: testSessionContext(), Workspace: util.FixedRoot(t.TempDir())})
	m.busy = true
	m.width = 80

	_, cmd := m.Update(doneWithDigest("Rewrote the parser and ran the suite.", ""))
	if cmd == nil {
		t.Fatal("a turn with a recap should still return a command")
	}
	if _, ok := cmd().(drainQueueMsg); ok {
		t.Fatal("the recap should be printed before the queue drains")
	}
}

// When the reply is only rendered at agentDoneMsg, the recap has to wait for
// it: printed first it would describe a turn the user cannot see yet.
func TestRecapWaitsForTheFallbackReplyRender(t *testing.T) {
	m := NewModel(TUIOpts{Session: testSessionContext(), Workspace: util.FixedRoot(t.TempDir())})
	m.busy = true
	m.width = 80
	m.streamingBuffer = "the reply nobody printed yet"

	next, _ := m.Update(doneWithDigest("Did the thing.", ""))
	mod := next.(*Model)
	if mod.pendingRecap == "" {
		t.Fatal("the recap should be held for the reply it belongs under")
	}

	next, cmd := mod.Update(assistantRenderedMsg{line: "the reply nobody printed yet"})
	mod = next.(*Model)
	if mod.pendingRecap != "" {
		t.Error("the held recap should be released once the reply renders")
	}
	if cmd == nil {
		t.Fatal("rendering the reply should print something")
	}
}

func TestNoDigestPrintsNothingExtra(t *testing.T) {
	m := NewModel(TUIOpts{Session: testSessionContext(), Workspace: util.FixedRoot(t.TempDir())})
	m.busy = true

	_, cmd := m.Update(doneWithDigest("", ""))
	if cmd == nil {
		t.Fatal("agentDoneMsg should return the queue drain command")
	}
	if _, ok := cmd().(drainQueueMsg); !ok {
		t.Fatal("a turn with no digest should go straight to the drain")
	}
}

// TestApprovalPanelNamesGrantTarget: when a tool narrows its session grant, the
// prompt says what "Allow session" admits — one browser origin, not every later
// navigation — and adds nothing when the grant covers the whole tool.
func TestApprovalPanelNamesGrantTarget(t *testing.T) {
	m := &Model{width: 100}
	m.pendingApproval = &approvalRequestMsg{
		ToolName: "BrowserNavigate",
		Args:     map[string]any{"url": "http://localhost:3000/login"},
		Target:   "http://localhost:3000",
	}
	if out := m.renderApprovalPanel(); !strings.Contains(out, "Allow session covers only: http://localhost:3000") {
		t.Errorf("panel = %q, want it to name the origin a session grant covers", out)
	}
	m.pendingApproval = &approvalRequestMsg{ToolName: "Write", Args: map[string]any{"file_path": "a.go"}}
	if out := m.renderApprovalPanel(); strings.Contains(out, "covers only") {
		t.Errorf("panel = %q, want no grant-target line for a tool-wide grant", out)
	}
}
