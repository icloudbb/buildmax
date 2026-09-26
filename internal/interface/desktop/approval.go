package desktop

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/icloudbb/buildmax/internal/core/agent"
)

const eventApprovalRequest = "desktop/approval-request"

// ApprovalRequestPayload is emitted to the frontend when a tool call needs approval.
// SessionID routes it to the asking run's chat tab; ApprovalID is what that tab
// answers with, so concurrent sessions of one project never share a prompt.
type ApprovalRequestPayload struct {
	ApprovalID string         `json:"approval_id"`
	ProjectID  string         `json:"project_id"`
	SessionID  string         `json:"session_id"`
	ToolName   string         `json:"tool_name"`
	Args       map[string]any `json:"args"`
	// Target is what "Allow session" covers within the tool (an MCP
	// server/tool, a browser origin); empty when it covers every call.
	Target string `json:"target,omitempty"`
}

// pendingApprovals holds the tool approvals awaiting an answer, keyed by a
// per-request id rather than by project or session: an id is answered at most
// once, so a late answer to a request its run already withdrew cannot resolve
// the next request that run makes.
type pendingApprovals struct {
	mu      sync.Mutex
	next    uint64
	waiting map[string]chan agent.ApprovalDecision
}

// open registers a new pending request and returns its id and answer channel.
func (p *pendingApprovals) open() (string, chan agent.ApprovalDecision) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.next++
	id := strconv.FormatUint(p.next, 10)
	ch := make(chan agent.ApprovalDecision, 1)
	if p.waiting == nil {
		p.waiting = make(map[string]chan agent.ApprovalDecision)
	}
	p.waiting[id] = ch
	return id, ch
}

// withdraw drops a request its run stopped waiting for.
func (p *pendingApprovals) withdraw(id string) {
	p.mu.Lock()
	delete(p.waiting, id)
	p.mu.Unlock()
}

// resolve answers one pending request. It fails for an id that is unknown,
// already answered, or withdrawn, so a stale answer reaches no run.
func (p *pendingApprovals) resolve(id string, decision agent.ApprovalDecision) error {
	p.mu.Lock()
	ch, ok := p.waiting[id]
	delete(p.waiting, id)
	p.mu.Unlock()
	if !ok {
		return fmt.Errorf("no pending approval %q: it was already answered or its run ended", id)
	}
	ch <- decision // buffered, and only one resolve can find it
	return nil
}

// runApprover is one run's agent.ApprovalHandler. It is bound to the run rather
// than the project so its prompt carries the run's own session id.
type runApprover struct {
	app *App
	run *desktopRun
}

// RequestApproval emits an approval-request event to the frontend and blocks until
// the user answers it via RespondApproval. Denies if the app context is not ready.
func (h *runApprover) RequestApproval(ctx context.Context, name string, args map[string]any, target string) agent.ApprovalDecision {
	h.app.mu.Lock()
	uiCtx := h.app.ctx // Wails context for emitting, distinct from the run's ctx
	h.app.mu.Unlock()
	if uiCtx == nil {
		return agent.ApprovalDeny
	}

	id, answer := h.app.approvals.open()
	// A new chat's run has adopted its real id in OnStart, before any tool call,
	// so the prompt is routed to that chat tab and never to another new chat.
	h.app.emit(uiCtx, eventApprovalRequest, &ApprovalRequestPayload{
		ApprovalID: id,
		ProjectID:  h.run.projectID,
		SessionID:  h.run.sessionID,
		ToolName:   name,
		Args:       args,
		Target:     target,
	})

	select {
	case d := <-answer:
		return d
	case <-ctx.Done():
		// Cancelled with the prompt still up. Without this the run goroutine
		// waits forever on an answer nobody will give, its deferred cleanup
		// never runs, and the session stays permanently "already in progress".
		h.app.approvals.withdraw(id)
		return agent.ApprovalDeny
	}
}
