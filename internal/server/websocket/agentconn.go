package websocket

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	coreremote "github.com/icloudbb/buildmax/internal/core/remotesession"

	gws "github.com/gorilla/websocket"
)

// ApprovalStreamKey is the stream-hub key carrying a session's pending
// tool-approval prompts, distinct from the content stream keyed by the session
// id alone. Reusing the hub means approvals are cross-replica-correct for free.
func ApprovalStreamKey(sessionID string) string { return sessionID + ":approval" }

// approvalFrame is one line on the approval stream: a pending prompt (Resolved
// false) or its dismissal (Resolved true).
type approvalFrame struct {
	ID       string `json:"id"`
	Tool     string `json:"tool,omitempty"`
	Summary  string `json:"summary,omitempty"`
	Resolved bool   `json:"resolved,omitempty"`
}

// AgentConnDeps is everything a Remote Control agent socket needs. It is
// deliberately narrow: this socket registers a live session, heartbeats it, and
// relays its output into the stream hub — nothing else.
type AgentConnDeps struct {
	Sessions coreremote.Store
	Hub      StreamHub
	// Registry maps this session to its socket so an inbound command (a remote
	// prompt) can reach it. Nil disables inbound delivery on this replica.
	Registry *SessionRegistry
	// CORSOrigin is checked on the upgrade. Empty or "*" accepts any origin.
	CORSOrigin string
}

// agentConn manages one outbound-dialed agent connection for one authenticated
// user. Unlike the per-space browser Conn, it carries no conversation turns; it
// only registers a session and relays events for read-only observation.
type agentConn struct {
	conn      *gws.Conn
	deps      AgentConnDeps
	userID    string
	sessionID string // assigned on register; empty until then

	writeCh chan []byte
	closed  chan struct{}
	cancel  context.CancelFunc
}

// ServeAgent upgrades an authenticated agent request and runs the connection
// until it closes. The caller has already resolved the owning user; the session
// id is assigned when the client registers.
func ServeAgent(w http.ResponseWriter, r *http.Request, userID string, deps AgentConnDeps) {
	upgrader := gws.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			if deps.CORSOrigin == "" || deps.CORSOrigin == "*" {
				return true
			}
			origin := r.Header.Get("Origin")
			return origin == "" || origin == deps.CORSOrigin
		},
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		componentLog().Warn("agent upgrade failed", "err", err, "user_id", userID)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	ac := &agentConn{
		conn:    conn,
		deps:    deps,
		userID:  userID,
		writeCh: make(chan []byte, wsWriteChSize),
		closed:  make(chan struct{}),
		cancel:  cancel,
	}
	componentLog().Info("agent connected", "user_id", userID, "remote", r.RemoteAddr)
	go ac.writeLoop(ctx)
	ac.readLoop(ctx)
}

func (ac *agentConn) readLoop(ctx context.Context) {
	defer func() {
		componentLog().Info("agent disconnected", "user_id", ac.userID, "session_id", ac.sessionID)
		ac.cleanup()
		_ = ac.conn.Close()
	}()

	_ = ac.conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
	ac.conn.SetPongHandler(func(string) error {
		return ac.conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
	})

	for {
		_, data, err := ac.conn.ReadMessage()
		if err != nil {
			if gws.IsUnexpectedCloseError(err, gws.CloseGoingAway, gws.CloseNormalClosure) {
				componentLog().Info("agent read error", "err", err, "user_id", ac.userID)
			}
			return
		}
		_ = ac.conn.SetReadDeadline(time.Now().Add(wsReadDeadline))

		env, err := Decode(data)
		if err != nil {
			ac.sendEvent(TypeSystemError, SystemError{Error: "invalid message format"})
			continue
		}
		ac.handleClientEvent(ctx, env)
	}
}

func (ac *agentConn) handleClientEvent(ctx context.Context, env Envelope) {
	switch env.Type {
	case TypeAgentRegister:
		p, err := DecodePayload[AgentRegister](env)
		if err != nil {
			ac.sendEvent(TypeSystemError, SystemError{Error: "invalid payload"})
			return
		}
		ac.handleRegister(ctx, p)
	case TypeAgentHeartbeat:
		if ac.sessionID == "" {
			return
		}
		if err := ac.deps.Sessions.TouchRemoteSession(ctx, ac.sessionID, time.Now().UTC()); err != nil {
			componentLog().Warn("agent heartbeat", "err", err, "session_id", ac.sessionID)
		}
	case TypeAgentEvent:
		if ac.sessionID == "" {
			return
		}
		p, err := DecodePayload[AgentEvent](env)
		if err != nil {
			return
		}
		ac.deps.Hub.Append(ac.sessionID, p.Delta)
	case TypeAgentApproval:
		if ac.sessionID == "" {
			return
		}
		p, err := DecodePayload[AgentApproval](env)
		if err != nil {
			return
		}
		ac.appendApprovalFrame(approvalFrame{ID: p.ID, Tool: p.Tool, Summary: p.Summary})
	case TypeAgentApprovalResolved:
		if ac.sessionID == "" {
			return
		}
		p, err := DecodePayload[AgentApprovalResolved](env)
		if err != nil {
			return
		}
		ac.appendApprovalFrame(approvalFrame{ID: p.ID, Resolved: true})
	default:
		ac.sendEvent(TypeSystemError, SystemError{Error: "unknown event type: " + env.Type})
	}
}

func (ac *agentConn) handleRegister(ctx context.Context, p AgentRegister) {
	if ac.sessionID != "" {
		// Already registered on this socket; ignore a duplicate.
		ac.sendEvent(TypeAgentRegistered, AgentRegistered{SessionID: ac.sessionID})
		return
	}
	// Reconnect: reattach to the same session if the caller still owns it, so its
	// id and the URL another device is watching stay stable across a network blip.
	if p.SessionID != "" {
		if sess, err := ac.deps.Sessions.GetRemoteSession(ctx, p.SessionID); err == nil && sess.UserID == ac.userID {
			ac.sessionID = p.SessionID
			if err := ac.deps.Sessions.TouchRemoteSession(ctx, p.SessionID, time.Now().UTC()); err != nil {
				componentLog().Warn("agent reattach touch", "err", err, "session_id", p.SessionID)
			}
			ac.deps.Registry.Register(p.SessionID, ac)
			componentLog().Info("agent reattached", "user_id", ac.userID, "session_id", p.SessionID)
			ac.sendEvent(TypeAgentRegistered, AgentRegistered{SessionID: p.SessionID})
			return
		}
		// A stale or foreign id falls through to a fresh registration.
	}
	id, err := ac.deps.Sessions.RegisterRemoteSession(ctx, coreremote.NewRemoteSession{
		UserID:      ac.userID,
		DisplayName: p.DisplayName,
		Platform:    p.Platform,
		Host:        p.Host,
	})
	if err != nil {
		componentLog().Error("agent register", "err", err, "user_id", ac.userID)
		ac.sendEvent(TypeSystemError, SystemError{Error: "failed to register session"})
		return
	}
	ac.sessionID = id
	ac.deps.Registry.Register(id, ac)
	componentLog().Info("agent registered", "user_id", ac.userID, "session_id", id)
	ac.sendEvent(TypeAgentRegistered, AgentRegistered{SessionID: id})
}

// appendApprovalFrame publishes one approval frame onto the session's approval
// stream. Each frame is newline-terminated so a reconnecting reader can split a
// replayed buffer of several frames back apart.
func (ac *agentConn) appendApprovalFrame(f approvalFrame) {
	js, err := json.Marshal(f)
	if err != nil {
		return
	}
	ac.deps.Hub.Append(ApprovalStreamKey(ac.sessionID), string(js)+"\n")
}

func (ac *agentConn) writeLoop(ctx context.Context) {
	ticker := time.NewTicker(wsPingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = ac.conn.WriteMessage(gws.CloseMessage, gws.FormatCloseMessage(gws.CloseNormalClosure, ""))
			return
		case msg, ok := <-ac.writeCh:
			if !ok {
				_ = ac.conn.WriteMessage(gws.CloseMessage, gws.FormatCloseMessage(gws.CloseNormalClosure, ""))
				return
			}
			_ = ac.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := ac.conn.WriteMessage(gws.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = ac.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := ac.conn.WriteMessage(gws.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (ac *agentConn) sendEvent(eventType string, payload any) {
	data, err := Encode(eventType, payload)
	if err != nil {
		return
	}
	select {
	case <-ac.closed:
		return
	default:
	}
	select {
	case ac.writeCh <- data:
	case <-ac.closed:
	default:
	}
}

func (ac *agentConn) cleanup() {
	if ac.sessionID != "" {
		ac.deps.Registry.Unregister(ac.sessionID, ac)
		// Best-effort: mark offline now; the reaper is the backstop for an unclean
		// drop that never reaches here.
		if err := ac.deps.Sessions.MarkRemoteSessionOffline(context.Background(), ac.sessionID, time.Now().UTC()); err != nil {
			componentLog().Warn("agent mark offline", "err", err, "session_id", ac.sessionID)
		}
		// The stream is deliberately not ended here: the session may reconnect and
		// reattach, and a device watching it should keep its stream open across the
		// blip rather than see it finish. Presence (offline) marks the gap instead.
	}
	ac.cancel()
	select {
	case <-ac.closed:
	default:
		close(ac.closed)
	}
	close(ac.writeCh)
}
