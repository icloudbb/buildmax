package desktop

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/icloudbb/buildmax/internal/testsupport/mockllm"
)

// Rewind and fork through the bridge, against a session a real run left behind.
//
// The fixture matters more than the assertions: a history whose only tool call
// sits in the middle is the one that tells the two halves of the feature apart.
// Points after it abandon nothing that touched the workspace; points before it
// abandon a Write whose file is still on disk either way.
//
// The TestBridge prefix is what puts a test in the Desktop suite: `./make e2e
// desktop` selects by that name, so a bridge test named anything else runs only
// in the package's own `./make test` and is silently absent from the suite.

// historyScenario is three turns: one that only talks, one that writes a file,
// and one that only talks again.
//
// The opening turn is there because rewind cannot take back the first prompt of
// a session — nothing precedes it to land on — so a fixture that started with
// the turn that wrote the file could never rewind across the Write.
func historyScenario() mockllm.Scenario {
	return mockllm.Scenario{Steps: []mockllm.Step{
		{Text: "hello to you too"},
		{
			Text:      "writing it now",
			ToolCalls: []mockllm.ToolCall{{Name: "Write", Args: map[string]any{"file_path": "notes.txt", "content": "scripted content\n"}}},
		},
		{Text: "wrote notes.txt"},
		// That turn ran a tool, so it also asks for a recap — a model call of
		// its own, scripted here because these runs use the shipped default
		// configuration. The turns that ran no tool and answered briefly ask
		// for nothing and need no step.
		{Text: `{"recap": "Wrote notes.txt."}`},
		{Text: "you are welcome"},
	}}
}

// historySession runs all three turns and returns the session they left.
func historySession(t *testing.T) (*App, string, string) {
	t.Helper()
	app, events, _, projectID := bridge(t, historyScenario(), map[string]string{"Write": "allow"})

	if _, err := app.SendMessageStream(projectID, "", "say hello"); err != nil {
		t.Fatalf("first send: %v", err)
	}
	first, ok := events.waitFor(t, eventStreamDone).(*ReplyPayload)
	if !ok {
		t.Fatalf("first turn did not finish:\n%s", events.summary())
	}
	for i, prompt := range []string{"write notes.txt", "thanks"} {
		if _, err := app.SendMessageStream(projectID, first.SessionID, prompt); err != nil {
			t.Fatalf("send %q: %v", prompt, err)
		}
		// That turn's own stream-done, not an earlier one. It is also the point
		// at which the session is free: a run releases it before announcing that
		// it finished, so a move issued from here does not race the close.
		events.waitForNth(t, eventStreamDone, i+2)
	}
	return app, projectID, first.SessionID
}

func TestBridgeHistoryPointsListEachOperationNewestFirst(t *testing.T) {
	app, _, sessionID := historySession(t)

	got, err := app.GetHistoryPoints(sessionID)
	if err != nil {
		t.Fatalf("GetHistoryPoints: %v", err)
	}
	// Forking branches from a point, so every message a turn ended on is one —
	// the newest included, which is "branch off from here".
	if len(got.Fork) != 6 {
		t.Fatalf("fork points = %d, want the whole conversation: %+v", len(got.Fork), got.Fork)
	}
	if got.Fork[0].Content != "you are welcome" {
		t.Errorf("first fork row = %q, want the newest message", got.Fork[0].Content)
	}
	if last := got.Fork[len(got.Fork)-1]; last.Content != "say hello" {
		t.Errorf("last fork row = %q, want the opening message", last.Content)
	}

	// Rewinding hands a prompt back, so only prompts are offered, and not the
	// first one: nothing precedes it to land on.
	if len(got.Rewind) != 2 {
		t.Fatalf("rewind points = %d, want the two prompts after the first: %+v", len(got.Rewind), got.Rewind)
	}
	if got.Rewind[0].Content != "thanks" || got.Rewind[1].Content != "write notes.txt" {
		t.Errorf("rewind rows = %+v, want the prompts newest first", got.Rewind)
	}
	for _, p := range got.Rewind {
		if p.Role != "user" {
			t.Errorf("rewind row %+v is not a prompt", p)
		}
	}
}

func TestBridgeHistoryPointsNameTheToolAPointWouldMovePast(t *testing.T) {
	app, _, sessionID := historySession(t)

	got, err := app.GetHistoryPoints(sessionID)
	if err != nil {
		t.Fatalf("GetHistoryPoints: %v", err)
	}
	// The head describes no span at all: nothing came after it.
	if head := got.Fork[0]; head.Messages != 0 || len(head.Tools) != 0 {
		t.Errorf("head point = %+v, want an empty span", head)
	}
	// Every point before the Write must name it. Its file stays on disk through
	// both operations, and a surface that did not say so would be claiming a
	// rewind undoes the run.
	var named int
	for _, p := range append(append([]HistoryPoint{}, got.Fork...), got.Rewind...) {
		for _, tool := range p.Tools {
			if tool.Name == "Write" {
				named++
			}
			if tool.Interrupted {
				t.Errorf("%q reports Write as interrupted, but the run finished it", p.Content)
			}
		}
	}
	if named == 0 {
		t.Fatalf("no point named the Write that ran:\n%+v", got)
	}
	// The prompt of the turn that wrote the file removes the Write with it,
	// and the prompt after it removes conversation alone. Getting either wrong
	// means the span is computed from the wrong end.
	if wrote := got.Rewind[1]; len(wrote.Tools) == 0 {
		t.Errorf("rewinding %q reports no tools, but its own turn wrote the file", wrote.Content)
	}
	if thanks := got.Rewind[0]; thanks.Messages == 0 || len(thanks.Tools) != 0 {
		t.Errorf("rewinding %q = %+v, want conversation alone", thanks.Content, thanks)
	}
}

func TestBridgeRewindTakesThePromptBack(t *testing.T) {
	app, projectID, sessionID := historySession(t)

	before, err := app.GetSession(sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	points, err := app.GetHistoryPoints(sessionID)
	if err != nil {
		t.Fatalf("GetHistoryPoints: %v", err)
	}
	// The oldest prompt that can be taken back: rewinding it crosses the Write.
	target := points.Rewind[len(points.Rewind)-1]

	got, err := app.RewindSession(projectID, sessionID, target.ItemID)
	if err != nil {
		t.Fatalf("RewindSession: %v", err)
	}
	if got.SessionID != sessionID {
		t.Errorf("result session = %q, want the same session", got.SessionID)
	}
	if got.Messages != target.Messages {
		t.Errorf("reported %d messages, but the point promised %d", got.Messages, target.Messages)
	}
	if len(got.Tools) == 0 {
		t.Error("the rewind moved past the Write without reporting it")
	}
	// The composer takes the prompt back; without it the user would have to
	// retype what they asked to edit.
	if got.Prompt != "write notes.txt" {
		t.Errorf("prompt = %q, want the rewound message", got.Prompt)
	}

	after, err := app.GetSession(sessionID)
	if err != nil {
		t.Fatalf("GetSession after the rewind: %v", err)
	}
	if len(after.Messages) >= len(before.Messages) {
		t.Errorf("messages after the rewind = %d, before = %d; want fewer",
			len(after.Messages), len(before.Messages))
	}
	// The opening exchange, and nothing of the turn whose prompt was taken.
	if len(after.Messages) != 2 {
		t.Errorf("messages after the rewind = %d, want the first exchange", len(after.Messages))
	}
	// The session is closed again, so the next run can take it.
	if _, err := app.GetHistoryPoints(sessionID); err != nil {
		t.Errorf("the session did not reopen after the rewind: %v", err)
	}
}

func TestBridgeForkLeavesTheOriginalAndReturnsANewOne(t *testing.T) {
	app, projectID, sessionID := historySession(t)

	before, err := app.GetSession(sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	points, err := app.GetHistoryPoints(sessionID)
	if err != nil {
		t.Fatalf("GetHistoryPoints: %v", err)
	}
	target := points.Fork[len(points.Fork)-1]

	got, err := app.ForkSession(projectID, sessionID, target.ItemID)
	if err != nil {
		t.Fatalf("ForkSession: %v", err)
	}
	if got.SessionID == "" || got.SessionID == sessionID {
		t.Fatalf("fork returned %q, want a new session", got.SessionID)
	}

	// The whole point of a fork: the original keeps everything.
	after, err := app.GetSession(sessionID)
	if err != nil {
		t.Fatalf("GetSession after the fork: %v", err)
	}
	if len(after.Messages) != len(before.Messages) {
		t.Errorf("the original went from %d messages to %d; a fork must not touch it",
			len(before.Messages), len(after.Messages))
	}

	child, err := app.GetSession(got.SessionID)
	if err != nil {
		t.Fatalf("GetSession on the fork: %v", err)
	}
	if len(child.Messages) != 1 {
		t.Errorf("fork messages = %d, want the history through the chosen point", len(child.Messages))
	}
	// The child is a session in its own right, so it is listed and can be
	// forked again — which is also how we know it was left closed.
	if _, err := app.GetHistoryPoints(got.SessionID); err != nil {
		t.Errorf("the fork is not readable as a session: %v", err)
	}
	listed, err := app.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	var found bool
	for _, s := range listed {
		if s.ID == got.SessionID {
			found = true
		}
	}
	if !found {
		t.Error("the fork is missing from the session list")
	}

	tree := app.GetSessionForkTree(projectID, got.SessionID)
	if tree.LoadError != "" {
		t.Fatalf("GetSessionForkTree: %s", tree.LoadError)
	}
	if tree.Tree == nil || tree.Tree.ID != sessionID || len(tree.Tree.Children) != 1 {
		t.Fatalf("fork tree = %+v, want the original with one child", tree.Tree)
	}
	if child := tree.Tree.Children[0]; child.ID != got.SessionID || !child.Current {
		t.Errorf("fork tree child = %+v, want the new session marked current", child)
	}
}

func TestBridgeHistoryMovesAreRefusedWhileTheSessionIsBusy(t *testing.T) {
	// Its own scenario, whose first turn reaches the gate: the shared fixture
	// opens with a turn that runs no tool, and nothing here needs the history
	// that fixture builds.
	scenario := mockllm.Scenario{Steps: []mockllm.Step{
		{
			Text:      "writing it now",
			ToolCalls: []mockllm.ToolCall{{Name: "Write", Args: map[string]any{"file_path": "notes.txt", "content": "scripted content\n"}}},
		},
		{Text: "wrote notes.txt"},
		{Text: `{"recap": "Wrote notes.txt."}`},
	}}
	app, events, _, projectID := bridge(t, scenario, map[string]string{"Write": "ask"})

	if _, err := app.SendMessageStream(projectID, "", "write notes.txt"); err != nil {
		t.Fatalf("send: %v", err)
	}
	// The approval gate parks the run mid-turn with the session still open,
	// which is the state a rewind must refuse rather than corrupt.
	if _, ok := events.waitFor(t, eventApprovalRequest).(*ApprovalRequestPayload); !ok {
		t.Fatalf("no approval request:\n%s", events.summary())
	}
	sessions, err := app.ListSessions()
	if err != nil || len(sessions) == 0 {
		t.Fatalf("no session to act on (err = %v)", err)
	}
	sessionID := sessions[0].ID

	// Reading is still allowed: the picker has to open while a run is going, or
	// it could only ever be used on an idle session.
	points, err := app.GetHistoryPoints(sessionID)
	if err != nil {
		t.Fatalf("GetHistoryPoints during a run: %v", err)
	}
	if len(points.Fork) == 0 {
		t.Fatal("no points while the run is parked")
	}

	_, err = app.RewindSession(projectID, sessionID, points.Fork[len(points.Fork)-1].ItemID)
	if err == nil {
		t.Fatal("RewindSession succeeded on a session a run holds")
	}
	if !strings.Contains(err.Error(), "busy") {
		t.Errorf("error = %q, want it to say the session is busy", err)
	}

	app.RespondApproval(projectID, "once")
	events.waitFor(t, eventStreamDone)
}

// probeAtTerminalEvent makes the app try to take the session's writer lock at
// the instant it announces a turn is over, and reports what happened.
//
// Waiting for the event and then trying would test the poll interval, not the
// ordering: by the time a 10ms poll notices, a close that came afterwards has
// long since run. Doing it inside the emit is the frontend's own position —
// acting on the event as it arrives — and the only place the question has a
// definite answer.
func probeAtTerminalEvent(app *App, events *uiEvents) func() error {
	var mu sync.Mutex
	var probed, failure error
	probed = errors.New("no terminal event was emitted")
	inner := events.emit
	app.emit = func(ctx context.Context, name string, data any) {
		if name == eventStreamDone || name == eventStreamError {
			mu.Lock()
			probed = nil
			sessions, err := sessionManager().List()
			if err == nil && len(sessions) > 0 {
				if held, oerr := sessionManager().Open(sessions[0].ID, "mock"); oerr != nil {
					failure = oerr
				} else {
					_ = held.Close()
				}
			}
			mu.Unlock()
		}
		inner(ctx, name, data)
	}
	return func() error {
		mu.Lock()
		defer mu.Unlock()
		if probed != nil {
			return probed
		}
		return failure
	}
}

func TestBridgeTheSessionIsFreeTheMomentARunSaysItFinished(t *testing.T) {
	app, events, _, projectID := bridge(t, mockllm.Scenario{Steps: []mockllm.Step{{Text: "hello there"}}}, nil)
	result := probeAtTerminalEvent(app, events)

	if _, err := app.SendMessageStream(projectID, "", "hi"); err != nil {
		t.Fatalf("send: %v", err)
	}
	events.waitFor(t, eventStreamDone)

	// The frontend acts on this event. If the run still held its session here,
	// a rewind issued from the event handler would be refused by the very run
	// that just reported it was done.
	if err := result(); err != nil {
		t.Fatalf("the session was not free when stream-done fired: %v", err)
	}
}

func TestBridgeTheSessionIsFreeTheMomentARunReportsAFailure(t *testing.T) {
	scenario := mockllm.Scenario{Steps: []mockllm.Step{{Status: 400, Error: "scripted provider refusal"}}}
	app, events, _, projectID := bridge(t, scenario, nil)
	result := probeAtTerminalEvent(app, events)

	if _, err := app.SendMessageStream(projectID, "", "hi"); err != nil {
		t.Fatalf("send: %v", err)
	}
	events.waitFor(t, eventStreamError)

	// Going back is a likely thing to want right after a turn goes wrong, so
	// the failing path has to release the session as the succeeding one does.
	if err := result(); err != nil {
		t.Fatalf("the session was not free when stream-error fired: %v", err)
	}
}

func TestBridgeHistoryMovesRejectMissingArguments(t *testing.T) {
	app := NewApp()
	if _, err := app.RewindSession("", "s1", "i1"); err == nil {
		t.Error("RewindSession with no project succeeded")
	}
	if _, err := app.ForkSession("p1", "", "i1"); err == nil {
		t.Error("ForkSession with no session succeeded")
	}
	if _, err := app.ForkSession("p1", "s1", ""); err == nil {
		t.Error("ForkSession with no target succeeded")
	}
	if _, err := app.GetHistoryPoints(""); err == nil {
		t.Error("GetHistoryPoints with no session succeeded")
	}
}
