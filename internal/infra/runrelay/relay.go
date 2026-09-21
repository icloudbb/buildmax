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
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	gws "github.com/gorilla/websocket"
)

const (
	agentWSPath       = "/api/remote-control/agent-ws"
	heartbeatInterval = 30 * time.Second
	handshakeTimeout  = 10 * time.Second
	deltaBufferSize   = 512
)

// The agent-socket wire contract. It mirrors the envelope and payloads the
// server defines in internal/server/websocket; the client keeps its own copy
// rather than importing the server package, which infra may not reach.
type envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

const (
	typeAgentRegister   = "agent.register"
	typeAgentHeartbeat  = "agent.heartbeat"
	typeAgentEvent      = "agent.event"
	typeAgentRegistered = "agent.registered"
	typeAgentPrompt     = "agent.prompt"
)

type registerPayload struct {
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

// TokenFunc yields a fresh user access token per call, because a client outlives
// any one token.
type TokenFunc func() (string, error)

// Config wires a relay to the server as the signed-in user.
type Config struct {
	ServerURL   string
	TokenFunc   TokenFunc
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
}

// Relay is one outbound control channel. The zero value is inert; use New.
type Relay struct {
	cfg    Config
	deltas chan string
	stop   chan struct{}
	done   chan struct{}
}

// New returns a relay, or nil when there is nothing to connect to (no managed
// server), so a caller need not check before wiring it.
func New(cfg Config) *Relay {
	if cfg.ServerURL == "" || cfg.TokenFunc == nil {
		return nil
	}
	return &Relay{
		cfg:    cfg,
		deltas: make(chan string, deltaBufferSize),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
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

func (r *Relay) run(ctx context.Context) {
	defer close(r.done)

	conn, err := r.dial(ctx)
	if err != nil {
		slog.Warn("remote control: could not connect; the session will not be reachable", "err", err)
		return
	}
	defer func() { _ = conn.Close() }()

	if err := writeJSON(conn, typeAgentRegister, registerPayload{
		DisplayName: r.cfg.DisplayName, Platform: r.cfg.Platform, Host: r.cfg.Host,
	}); err != nil {
		slog.Warn("remote control: register failed", "err", err)
		return
	}

	// A reader goroutine handles the registration reply, pongs, and close.
	go r.readLoop(conn)

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stop:
			return
		case delta := <-r.deltas:
			if err := writeJSON(conn, typeAgentEvent, eventPayload{Delta: delta}); err != nil {
				return
			}
		case <-ticker.C:
			if err := writeJSON(conn, typeAgentHeartbeat, struct{}{}); err != nil {
				return
			}
		}
	}
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
			if json.Unmarshal(env.Payload, &p) == nil && p.SessionID != "" && r.cfg.OnRegistered != nil {
				r.cfg.OnRegistered(p.SessionID)
			}
		case typeAgentPrompt:
			var p promptPayload
			if json.Unmarshal(env.Payload, &p) == nil && p.Content != "" && r.cfg.OnRemotePrompt != nil {
				r.cfg.OnRemotePrompt(p.Content)
			}
		}
	}
}

func (r *Relay) dial(ctx context.Context) (*gws.Conn, error) {
	token, err := r.cfg.TokenFunc()
	if err != nil {
		return nil, err
	}
	wsURL, err := agentWSURL(r.cfg.ServerURL, token)
	if err != nil {
		return nil, err
	}
	dialer := &gws.Dialer{HandshakeTimeout: handshakeTimeout}
	if t, ok := transportOf(r.cfg.HTTPClient); ok && t.TLSClientConfig != nil {
		dialer.TLSClientConfig = t.TLSClientConfig.Clone()
	}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	return conn, err
}

// OnDelta implements llm.StreamSink: it enqueues a content delta for relay,
// dropping it rather than blocking the run if the buffer is full.
func (r *Relay) OnDelta(delta string) {
	if r == nil || delta == "" {
		return
	}
	select {
	case r.deltas <- delta:
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
