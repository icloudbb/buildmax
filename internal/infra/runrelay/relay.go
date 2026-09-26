// Package runrelay is the local half of Remote Control: the outbound WebSocket a
// device-resident session dials to register itself with the server and relay its
// output, so another device can observe it. Execution and the filesystem stay
// local; this only carries events out.
//
// It is fail-open by construction: a relay that cannot connect, or a send that
// would block, never disturbs the local run. See docs/design/remote-control.md.
package runrelay

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	gws "github.com/gorilla/websocket"
)

const (
	agentWSPath       = "/api/remote-control/agent-ws"
	heartbeatInterval = 30 * time.Second
	handshakeTimeout  = 10 * time.Second
	outBufferSize     = 512
	// Reconnect timing: a dropped connection is retried with backoff so a network
	// blip does not take Remote Control off for the rest of the session. Buffered
	// outbound frames flush once the connection is back.
	minReconnectBackoff = 1 * time.Second
	maxReconnectBackoff = 30 * time.Second
)

// The agent-socket wire contract. It mirrors the envelope and payloads the
// server defines in internal/server/websocket; the client keeps its own copy
// rather than importing the server package, which infra may not reach.
type envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

const (
	typeAgentRegister         = "agent.register"
	typeAgentHeartbeat        = "agent.heartbeat"
	typeAgentEvent            = "agent.event"
	typeAgentApproval         = "agent.approval"
	typeAgentApprovalResolved = "agent.approval_resolved"
	typeAgentRegistered       = "agent.registered"
	typeAgentPrompt           = "agent.prompt"
	typeAgentApprovalResponse = "agent.approval_response"
	typeAgentCancel           = "agent.cancel"
)

type registerPayload struct {
	// SessionID, when set on a reconnect, asks the server to reattach to the same
	// session rather than create a new one.
	SessionID   string `json:"session_id,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Platform    string `json:"platform,omitempty"`
	Host        string `json:"host,omitempty"`
}

type eventPayload struct {
	Delta string `json:"delta,omitempty"`
}

type registeredPayload struct {
	SessionID string `json:"session_id"`
}

type promptPayload struct {
	Content string `json:"content"`
}

type approvalPayload struct {
	ID      string `json:"id"`
	Tool    string `json:"tool"`
	Summary string `json:"summary,omitempty"`
}

type approvalResolvedPayload struct {
	ID string `json:"id"`
}

type approvalResponsePayload struct {
	ID       string `json:"id"`
	Decision string `json:"decision"`
}

// TokenFunc yields a fresh user access token per call, because a client outlives
// any one token.
type TokenFunc func() (string, error)

// RenewFunc is told the token the server refused at the upgrade and returns
// one to retry with.
type RenewFunc func(rejected string) (string, error)

// Config wires a relay to the server as the signed-in user.
type Config struct {
	ServerURL string
	TokenFunc TokenFunc
	// RenewToken, when set, is asked for a new token after the server answers
	// the upgrade with 401, and the dial is retried once with it. A token can
	// look unexpired and still be refused — a rotated JWT secret does that — and
	// redialing with TokenFunc alone would repeat the refusal until the token's
	// own expiry. Nil leaves a refused dial to the ordinary backoff.
	RenewToken  RenewFunc
	HTTPClient  *http.Client
	DisplayName string
	Platform    string
	Host        string
	// OnRegistered is called once the server assigns a session id, so a surface
	// can tell the user where to watch. Optional.
	OnRegistered func(sessionID string)
	// OnRemotePrompt is called when another device sends a follow-up prompt, to be
	// delivered into the local session as if the user typed it. Called from the
	// relay's read goroutine. Optional — nil ignores inbound prompts.
	OnRemotePrompt func(content string)
	// OnRemoteApproval is called when another device answers a tool-approval
	// prompt, with the request id and the decision ("once"/"session"/"deny").
	// Called from the relay's read goroutine. Optional.
	OnRemoteApproval func(id, decision string)
	// OnRemoteCancel is called when another device asks the session to stop its
	// current run. Called from the relay's read goroutine. Optional.
	OnRemoteCancel func()
}

// Relay is one outbound control channel. The zero value is inert; use New.
type Relay struct {
	cfg  Config
	out  chan []byte
	stop chan struct{}
	done chan struct{}

	// sid remembers the session id the server assigned, so a reconnect reattaches
	// to the same session (and the same URL another device is watching) instead of
	// creating a new one.
	sidMu sync.Mutex
	sid   string
}

// New returns a relay, or nil when there is nothing to connect to (no managed
// server), so a caller need not check before wiring it.
func New(cfg Config) *Relay {
	if cfg.ServerURL == "" || cfg.TokenFunc == nil {
		return nil
	}
	return &Relay{
		cfg:  cfg,
		out:  make(chan []byte, outBufferSize),
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
}

// Start dials the server and runs the channel in the background. It never blocks
// the caller and never fails the run: a dial error leaves the relay inert.
// Calling Start on a nil relay is a no-op.
func (r *Relay) Start(ctx context.Context) {
	if r == nil {
		return
	}
	go r.run(ctx)
}

// run keeps the channel connected: it dials, serves until the connection drops,
// then reconnects with backoff, reattaching to the same session, until stopped.
func (r *Relay) run(ctx context.Context) {
	defer close(r.done)

	backoff := minReconnectBackoff
	first := true
	for {
		conn, err := r.dial(ctx)
		if err != nil {
			if first {
				slog.Warn("remote control: could not connect; retrying in the background", "err", err)
			}
			if !r.wait(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, maxReconnectBackoff)
			continue
		}
		first = false
		stop := r.serveConn(ctx, conn)
		_ = conn.Close()
		if stop {
			return
		}
		// The connection dropped, not us: reconnect promptly, resetting backoff.
		backoff = minReconnectBackoff
		if !r.wait(ctx, backoff) {
			return
		}
	}
}

// serveConn registers on a fresh connection and pumps it until the connection
// breaks (returns false, meaning reconnect) or the relay is told to stop (returns
// true). Buffered outbound frames flush here, so events raised during a brief
// outage survive.
func (r *Relay) serveConn(ctx context.Context, conn *gws.Conn) (stop bool) {
	if err := writeJSON(conn, typeAgentRegister, registerPayload{
		SessionID: r.currentSID(), DisplayName: r.cfg.DisplayName, Platform: r.cfg.Platform, Host: r.cfg.Host,
	}); err != nil {
		return false
	}

	connDone := make(chan struct{})
	go func() { r.readLoop(conn); close(connDone) }()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return true
		case <-r.stop:
			return true
		case <-connDone:
			return false
		case frame := <-r.out:
			if err := writeFrame(conn, frame); err != nil {
				return false
			}
		case <-ticker.C:
			if err := writeJSON(conn, typeAgentHeartbeat, struct{}{}); err != nil {
				return false
			}
		}
	}
}

// wait sleeps for d, or returns false early if the relay is stopping.
func (r *Relay) wait(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	case <-r.stop:
		return false
	}
}

func (r *Relay) currentSID() string {
	r.sidMu.Lock()
	defer r.sidMu.Unlock()
	return r.sid
}

// rememberSID records the assigned session id and reports whether this is the
// first assignment, so the surface is told "reachable" once, not on every
// reconnect.
func (r *Relay) rememberSID(id string) (first bool) {
	r.sidMu.Lock()
	defer r.sidMu.Unlock()
	first = r.sid == ""
	r.sid = id
	return first
}

func (r *Relay) readLoop(conn *gws.Conn) {
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var env envelope
		if json.Unmarshal(data, &env) != nil {
			continue
		}
		switch env.Type {
		case typeAgentRegistered:
			var p registeredPayload
			if json.Unmarshal(env.Payload, &p) == nil && p.SessionID != "" {
				// Tell the surface once, on the first registration — not on every
				// reconnect, which reattaches to the same id.
				if r.rememberSID(p.SessionID) && r.cfg.OnRegistered != nil {
					r.cfg.OnRegistered(p.SessionID)
				}
			}
		case typeAgentPrompt:
			var p promptPayload
			if json.Unmarshal(env.Payload, &p) == nil && p.Content != "" && r.cfg.OnRemotePrompt != nil {
				r.cfg.OnRemotePrompt(p.Content)
			}
		case typeAgentApprovalResponse:
			var p approvalResponsePayload
			if json.Unmarshal(env.Payload, &p) == nil && p.ID != "" && r.cfg.OnRemoteApproval != nil {
				r.cfg.OnRemoteApproval(p.ID, p.Decision)
			}
		case typeAgentCancel:
			if r.cfg.OnRemoteCancel != nil {
				r.cfg.OnRemoteCancel()
			}
		}
	}
}

func (r *Relay) dial(ctx context.Context) (*gws.Conn, error) {
	token, err := r.cfg.TokenFunc()
	if err != nil {
		return nil, err
	}
	conn, status, err := r.dialWith(ctx, token)
	if err == nil || status != http.StatusUnauthorized || r.cfg.RenewToken == nil {
		return conn, err
	}
	renewed, renewErr := r.cfg.RenewToken(token)
	if renewErr != nil {
		return nil, fmt.Errorf("%w; renewing the credential failed: %w", err, renewErr)
	}
	conn, _, err = r.dialWith(ctx, renewed)
	return conn, err
}

// dialWith opens the agent socket with token, reporting the HTTP status of a
// refused upgrade so the caller can tell a rejected credential from an outage.
func (r *Relay) dialWith(ctx context.Context, token string) (*gws.Conn, int, error) {
	wsURL, err := agentWSURL(r.cfg.ServerURL, token)
	if err != nil {
		return nil, 0, err
	}
	dialer := &gws.Dialer{HandshakeTimeout: handshakeTimeout}
	if t, ok := transportOf(r.cfg.HTTPClient); ok && t.TLSClientConfig != nil {
		dialer.TLSClientConfig = t.TLSClientConfig.Clone()
	}
	conn, resp, err := dialer.DialContext(ctx, wsURL, nil)
	status := 0
	if resp != nil {
		status = resp.StatusCode
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}
	return conn, status, err
}

// OnDelta implements llm.StreamSink: it enqueues a content delta for relay,
// dropping it rather than blocking the run if the buffer is full.
func (r *Relay) OnDelta(delta string) {
	if r == nil || delta == "" {
		return
	}
	r.enqueue(typeAgentEvent, eventPayload{Delta: delta})
}

// SendApprovalRequest relays a pending tool-approval prompt so another device can
// answer it. Dropped if the buffer is full (the local prompt still stands).
func (r *Relay) SendApprovalRequest(id, tool, summary string) {
	if r == nil || id == "" {
		return
	}
	r.enqueue(typeAgentApproval, approvalPayload{ID: id, Tool: tool, Summary: summary})
}

// SendApprovalResolved tells the server an approval was answered (locally or
// remotely), so connected devices dismiss their copy of it.
func (r *Relay) SendApprovalResolved(id string) {
	if r == nil || id == "" {
		return
	}
	r.enqueue(typeAgentApprovalResolved, approvalResolvedPayload{ID: id})
}

// enqueue marshals a frame and queues it for the write loop, dropping it rather
// than blocking the run if the buffer is full.
func (r *Relay) enqueue(typ string, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	frame, err := json.Marshal(envelope{Type: typ, Payload: raw})
	if err != nil {
		return
	}
	select {
	case r.out <- frame:
	default:
	}
}

// Close stops the relay and waits for its goroutine to finish.
func (r *Relay) Close() error {
	if r == nil {
		return nil
	}
	select {
	case <-r.stop:
	default:
		close(r.stop)
	}
	<-r.done
	return nil
}

func writeJSON(conn *gws.Conn, typ string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	data, err := json.Marshal(envelope{Type: typ, Payload: raw})
	if err != nil {
		return err
	}
	_ = conn.SetWriteDeadline(time.Now().Add(handshakeTimeout))
	return conn.WriteMessage(gws.TextMessage, data)
}

// writeFrame writes an already-encoded envelope from the outbound queue.
func writeFrame(conn *gws.Conn, frame []byte) error {
	_ = conn.SetWriteDeadline(time.Now().Add(handshakeTimeout))
	return conn.WriteMessage(gws.TextMessage, frame)
}

// agentWSURL turns the managed server URL into the ws/wss agent-socket URL with
// the token as a query parameter, the credential form a WebSocket handshake
// allows.
func agentWSURL(serverURL, token string) (string, error) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + agentWSPath
	q := u.Query()
	q.Set("token", token)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func transportOf(client *http.Client) (*http.Transport, bool) {
	if client == nil {
		return nil, false
	}
	t, ok := client.Transport.(*http.Transport)
	return t, ok
}
