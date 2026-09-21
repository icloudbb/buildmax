package websocket

import "sync"

// SessionRegistry maps a live Remote Control session to the agent socket serving
// it on this replica. It is what lets an inbound command (a remote prompt) reach
// the one connection holding the session, and it is per-replica: a command that
// arrives on a replica without the socket is forwarded over the coordination bus
// so the replica that does hold it can deliver. See docs/design/remote-control.md.
type SessionRegistry struct {
	mu    sync.RWMutex
	conns map[string]*agentConn
}

// NewSessionRegistry returns an empty registry.
func NewSessionRegistry() *SessionRegistry {
	return &SessionRegistry{conns: make(map[string]*agentConn)}
}

// Register records the socket serving a session. A reconnect replaces the prior
// entry; there is at most one live socket per session.
func (r *SessionRegistry) Register(sessionID string, c *agentConn) {
	if r == nil || sessionID == "" || c == nil {
		return
	}
	r.mu.Lock()
	r.conns[sessionID] = c
	r.mu.Unlock()
}

// Unregister drops the entry only if it still points at this connection, so a
// stale socket closing after a reconnect does not evict the newer one.
func (r *SessionRegistry) Unregister(sessionID string, c *agentConn) {
	if r == nil || sessionID == "" {
		return
	}
	r.mu.Lock()
	if r.conns[sessionID] == c {
		delete(r.conns, sessionID)
	}
	r.mu.Unlock()
}

// DeliverPrompt sends a remote prompt to the session's socket if it is on this
// replica, returning whether it was delivered here. A false result means the
// socket is on another replica (or gone), and the caller forwards over the bus.
func (r *SessionRegistry) DeliverPrompt(sessionID, content string) bool {
	if r == nil || sessionID == "" {
		return false
	}
	r.mu.RLock()
	c := r.conns[sessionID]
	r.mu.RUnlock()
	if c == nil {
		return false
	}
	c.sendEvent(TypeAgentPrompt, AgentPrompt{Content: content})
	return true
}

// DeliverApprovalResponse sends a remote decision on a pending approval to the
// session's socket if it is on this replica, returning whether it was delivered
// here. A false result means the caller forwards over the bus.
func (r *SessionRegistry) DeliverApprovalResponse(sessionID, id, decision string) bool {
	if r == nil || sessionID == "" {
		return false
	}
	r.mu.RLock()
	c := r.conns[sessionID]
	r.mu.RUnlock()
	if c == nil {
		return false
	}
	c.sendEvent(TypeAgentApprovalResponse, AgentApprovalResponse{ID: id, Decision: decision})
	return true
}

// DeliverCancel asks the session to stop its current run, if its socket is on
// this replica, returning whether it was delivered here.
func (r *SessionRegistry) DeliverCancel(sessionID string) bool {
	if r == nil || sessionID == "" {
		return false
	}
	r.mu.RLock()
	c := r.conns[sessionID]
	r.mu.RUnlock()
	if c == nil {
		return false
	}
	c.sendEvent(TypeAgentCancel, struct{}{})
	return true
}
