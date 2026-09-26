package cli

import (
	"context"
	"encoding/json"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/icloudbb/buildmax/internal/core/agent"
	"github.com/icloudbb/buildmax/internal/util"
)

// approvalRequestMsg is sent by TUIApprovalHandler to the Tea program when a tool
// call needs interactive approval. The agent goroutine blocks on response until
// the request is resolved — by the local user or, through Remote Control, by
// another device. id correlates the two.
type approvalRequestMsg struct {
	id       string
	ToolName string
	Args     map[string]any
	// Target is what "Allow session" covers within the tool (an MCP
	// server/tool, a browser origin); empty when it covers every call.
	Target   string
	response chan agent.ApprovalDecision
}

// approvalResolvedMsg dismisses the local approval prompt when the request was
// answered elsewhere (a remote device, or a cancel).
type approvalResolvedMsg struct{ id string }

// TUIApprovalHandler implements agent.ApprovalHandler for the Bubble Tea TUI.
// Create it before the program, wire the program in after tea.NewProgram. When
// Remote Control is on, it also forwards the prompt outward and accepts a remote
// decision; whichever of {local, remote} answers first wins.
type TUIApprovalHandler struct {
	program *tea.Program

	mu      sync.Mutex
	pending map[string]chan agent.ApprovalDecision

	// forwardRequest and forwardResolved bridge to the Remote Control relay. Nil
	// when Remote Control is off.
	forwardRequest  func(id, tool, summary string)
	forwardResolved func(id string)
}

func NewTUIApprovalHandler() *TUIApprovalHandler {
	return &TUIApprovalHandler{pending: make(map[string]chan agent.ApprovalDecision)}
}

// parseApprovalDecision maps the wire decision a remote device sends to the
// runtime decision. An unknown value is the safe conservative one: allow once.
func parseApprovalDecision(s string) agent.ApprovalDecision {
	switch s {
	case "session":
		return agent.ApprovalAllowSession
	case "deny":
		return agent.ApprovalDeny
	default:
		return agent.ApprovalAllowOnce
	}
}

func (h *TUIApprovalHandler) SetProgram(p *tea.Program) { h.program = p }

// SetForwarders wires the relay bridges once the AgentApp exists. Both may be
// nil (Remote Control off).
func (h *TUIApprovalHandler) SetForwarders(request func(id, tool, summary string), resolved func(id string)) {
	h.forwardRequest = request
	h.forwardResolved = resolved
}

// RequestApproval shows the prompt locally, forwards it to connected devices, and
// blocks until it is answered from either side or the run is cancelled.
func (h *TUIApprovalHandler) RequestApproval(ctx context.Context, name string, args map[string]any, target string) agent.ApprovalDecision {
	if h.program == nil {
		return agent.ApprovalDeny
	}
	id, _ := util.NewPublicID()
	respCh := make(chan agent.ApprovalDecision, 1)
	h.mu.Lock()
	h.pending[id] = respCh
	h.mu.Unlock()

	h.program.Send(approvalRequestMsg{id: id, ToolName: name, Args: args, Target: target, response: respCh})
	if h.forwardRequest != nil {
		h.forwardRequest(id, name, summarizeArgs(args))
	}

	select {
	case d := <-respCh:
		return d
	case <-ctx.Done():
		// The run was cancelled while the prompt was up. Dismiss it everywhere and
		// deny.
		h.Resolve(id, agent.ApprovalDeny)
		return agent.ApprovalDeny
	}
}

// Resolve applies a decision that came from outside the local prompt (a remote
// device, or a cancel) and dismisses the local prompt. It is a no-op if the
// request was already answered.
func (h *TUIApprovalHandler) Resolve(id string, d agent.ApprovalDecision) bool {
	if !h.deliver(id, d) {
		return false
	}
	if h.program != nil {
		h.program.Send(approvalResolvedMsg{id: id})
	}
	return true
}

// deliver hands the decision to the waiting RequestApproval and retires the
// request. The send is non-blocking on a buffered channel, so whichever of the
// local answer and a remote decision reaches deliver first wins and the other is
// a no-op once the entry is gone.
func (h *TUIApprovalHandler) deliver(id string, d agent.ApprovalDecision) bool {
	h.mu.Lock()
	ch, ok := h.pending[id]
	if ok {
		delete(h.pending, id)
	}
	h.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- d:
	default:
	}
	if h.forwardResolved != nil {
		h.forwardResolved(id)
	}
	return true
}

// summarizeArgs renders a tool call's arguments as a short one-line string for a
// remote device to read, bounded so a large argument does not bloat the wire.
func summarizeArgs(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	b, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	const max = 240
	if len(b) > max {
		return string(b[:max]) + "…"
	}
	return string(b)
}
