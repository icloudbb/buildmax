package desktop

import (
	"context"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/agentapp"
	"github.com/icloudbb/buildmax/internal/core/agent"
)

// askingHost is a RunHost whose turn makes one gated tool call and reports the
// decision it got, so a test can hold a run parked on an approval without a model.
type askingHost struct {
	decided chan agent.ApprovalDecision
}

func newAskingHost() *askingHost {
	return &askingHost{decided: make(chan agent.ApprovalDecision, 1)}
}

func (h *askingHost) OpenSession(string) (*agentapp.SessionContext, error) {
	return agentapp.NewSessionContext(""), nil
}
func (h *askingHost) CloseSession(*agentapp.SessionContext) {}

func (h *askingHost) RunPrompt(ctx context.Context, _ *agentapp.SessionContext, prompt string, o agentapp.RunPromptOpts) (agentapp.RunResult, error) {
	h.decided <- o.Approval.RequestApproval(ctx, "Write", map[string]any{"file_path": prompt})
	return agentapp.RunResult{}, nil
}

func (h *askingHost) RunBackgroundEvent(ctx context.Context, s *agentapp.SessionContext, _ agentapp.BackgroundEvent, o agentapp.RunPromptOpts) (agentapp.RunResult, error) {
	return h.RunPrompt(ctx, s, "", o)
}

func (h *askingHost) decision(t *testing.T) agent.ApprovalDecision {
	t.Helper()
	select {
	case d := <-h.decided:
		return d
	case <-time.After(10 * time.Second):
		t.Fatal("the run never got an approval decision")
		return agent.ApprovalDeny
	}
}

func (h *askingHost) stillWaiting(t *testing.T) {
	t.Helper()
	select {
	case d := <-h.decided:
		t.Fatalf("run was answered with %v by another session's response", d)
	case <-time.After(50 * time.Millisecond):
	}
}

// startAskingRun submits a prompt for one session of project "p", bound to an
// approval handler the way hostForProject binds it.
func startAskingRun(t *testing.T, app *App, sessionID string) (*desktopRun, *askingHost) {
	t.Helper()
	host := newAskingHost()
	lc := &desktopRun{app: app, ctx: context.Background(), projectID: "p", sessionID: sessionID, key: runKey("p", sessionID)}
	lc.handler = &runApprover{app: app, run: lc}
	hostFn := func() (agentapp.RunHost, error) { return host, nil }
	if _, err := app.scheduler.Submit(context.Background(), lc.key, sessionID, sessionID+".txt", hostFn, lc); err != nil {
		t.Fatalf("submit %s: %v", sessionID, err)
	}
	return lc, host
}

func approvalApp() (*App, *uiEvents) {
	events := &uiEvents{}
	app := NewApp()
	app.emit = events.emit
	app.ctx = context.Background()
	return app, events
}

// approvalsFor waits for two approval requests and returns them by the run that
// asked. Each run tags its request with the session id it adopted on start.
func approvalsFor(t *testing.T, events *uiEvents, a, b *desktopRun) (*ApprovalRequestPayload, *ApprovalRequestPayload) {
	t.Helper()
	events.waitForNth(t, eventApprovalRequest, 2)
	byRun := map[string]*ApprovalRequestPayload{}
	events.mu.Lock()
	for _, e := range events.events {
		if p, ok := e.data.(*ApprovalRequestPayload); ok {
			byRun[p.SessionID] = p
		}
	}
	events.mu.Unlock()
	reqA, reqB := byRun[a.sessionID], byRun[b.sessionID]
	if reqA == nil || reqB == nil {
		t.Fatalf("approval requests %+v are not one per session (%s, %s)", byRun, a.sessionID, b.sessionID)
	}
	if reqA.ApprovalID == reqB.ApprovalID {
		t.Fatalf("both sessions got approval id %q", reqA.ApprovalID)
	}
	if reqA.ProjectID != "p" || reqB.ProjectID != "p" {
		t.Fatalf("project ids = %q, %q, want p", reqA.ProjectID, reqB.ProjectID)
	}
	return reqA, reqB
}

// Two sessions of one project ask at once: each prompt stays pending on its own
// and is answered only by a response carrying its own id.
func TestConcurrentSessionApprovalsResolveIndependently(t *testing.T) {
	app, events := approvalApp()
	runA, hostA := startAskingRun(t, app, "sA")
	runB, hostB := startAskingRun(t, app, "sB")
	reqA, reqB := approvalsFor(t, events, runA, runB)

	if err := app.RespondApproval(reqB.ApprovalID, "deny"); err != nil {
		t.Fatalf("answer B: %v", err)
	}
	if d := hostB.decision(t); d != agent.ApprovalDeny {
		t.Fatalf("B decision = %v, want deny", d)
	}
	hostA.stillWaiting(t)

	if err := app.RespondApproval(reqA.ApprovalID, "session"); err != nil {
		t.Fatalf("answer A: %v", err)
	}
	if d := hostA.decision(t); d != agent.ApprovalAllowSession {
		t.Fatalf("A decision = %v, want allow-session", d)
	}

	// An id answers once; a repeat or an unknown id reaches no run.
	if err := app.RespondApproval(reqA.ApprovalID, "once"); err == nil {
		t.Fatal("a second answer to an answered approval succeeded")
	}
	if err := app.RespondApproval("no-such-approval", "once"); err == nil {
		t.Fatal("an answer to an unknown approval succeeded")
	}
}

// Cancelling one session's run withdraws only its own pending approval.
func TestCancellingOneSessionLeavesTheOtherApprovalPending(t *testing.T) {
	app, events := approvalApp()
	runA, hostA := startAskingRun(t, app, "sA")
	runB, hostB := startAskingRun(t, app, "sB")
	reqA, reqB := approvalsFor(t, events, runA, runB)

	if err := app.CancelRun("p", "sA"); err != nil {
		t.Fatalf("cancel A: %v", err)
	}
	if d := hostA.decision(t); d != agent.ApprovalDeny {
		t.Fatalf("cancelled A decision = %v, want deny", d)
	}
	hostB.stillWaiting(t)

	// The withdrawn prompt is stale: answering it late must not leak into B.
	if err := app.RespondApproval(reqA.ApprovalID, "once"); err == nil {
		t.Fatal("an answer to a cancelled run's approval succeeded")
	}
	hostB.stillWaiting(t)

	if err := app.RespondApproval(reqB.ApprovalID, "once"); err != nil {
		t.Fatalf("answer B: %v", err)
	}
	if d := hostB.decision(t); d != agent.ApprovalAllowOnce {
		t.Fatalf("B decision = %v, want allow-once", d)
	}
}

// An unrecognised decision string fails closed.
func TestUnknownApprovalDecisionDenies(t *testing.T) {
	app, _ := approvalApp()
	id, answer := app.approvals.open()
	if err := app.RespondApproval(id, "yes please"); err != nil {
		t.Fatalf("respond: %v", err)
	}
	if d := <-answer; d != agent.ApprovalDeny {
		t.Fatalf("decision = %v, want deny", d)
	}
}
