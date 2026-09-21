package handlers

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	coreremote "github.com/icloudbb/buildmax/internal/core/remotesession"
	"github.com/icloudbb/buildmax/internal/server/httputil"
	wsconn "github.com/icloudbb/buildmax/internal/server/websocket"
)

// Remote Control routes. A local session dials out to the agent WebSocket to
// register and relay itself; another device of the same account lists its
// sessions and watches one over SSE. All are account-scoped, not Space-scoped —
// see docs/design/remote-control.md.

// agentWSUpgradeHandler authenticates the outbound agent socket and hands it off.
// The credential is a user access token in the query string, the same one the
// browser socket uses, because a WebSocket upgrade cannot set a header.
func (h *Handler) agentWSUpgradeHandler(w http.ResponseWriter, r *http.Request) {
	if h.draining() {
		http.Error(w, "this server is shutting down", http.StatusServiceUnavailable)
		return
	}
	if h.cfg.RemoteSessionStore == nil {
		http.Error(w, "remote control not configured", http.StatusServiceUnavailable)
		return
	}
	tokenStr := r.URL.Query().Get("token")
	if tokenStr == "" {
		http.Error(w, "token required", http.StatusUnauthorized)
		return
	}
	userID, ok := h.guard().TokenSubjectActive(r.Context(), tokenStr)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	wsconn.ServeAgent(w, r, userID, wsconn.AgentConnDeps{
		Sessions:   h.cfg.RemoteSessionStore,
		Hub:        h.hub,
		Registry:   h.sessionRegistry,
		CORSOrigin: h.cfg.CORSOrigin,
	})
}

// remoteCommand is the cross-replica envelope for a Remote Control command: a
// prompt bound for the replica that holds the session's socket.
type remoteCommand struct {
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
}

type remotePromptRequest struct {
	Content string `json:"content"`
}

// promptRemoteSessionHandler delivers a follow-up prompt to a live session, after
// checking the caller owns it and it is online. Delivery is best-effort and
// fire-and-forget, like a cancel: the socket may be on another replica, so the
// command is also forwarded over the bus when one is configured.
func (h *Handler) promptRemoteSessionHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.guard().ActiveUser(w, r)
	if !ok {
		return
	}
	if h.cfg.RemoteSessionStore == nil {
		httputil.WriteJSONError(w, http.StatusServiceUnavailable, "remote control not configured")
		return
	}
	sessionID, ok := httputil.PathValue(w, r, "session_id")
	if !ok {
		return
	}
	var req remotePromptRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "content required")
		return
	}
	sess, err := h.cfg.RemoteSessionStore.GetRemoteSession(r.Context(), sessionID)
	if err != nil || sess.UserID != userID {
		httputil.WriteJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	if sess.Status != coreremote.StatusOnline {
		httputil.WriteJSONError(w, http.StatusConflict, "session is offline")
		return
	}
	h.deliverRemotePrompt(sessionID, req.Content)
	httputil.WriteJSON(w, http.StatusAccepted, map[string]bool{"accepted": true})
}

// deliverRemotePrompt sends the prompt to the session's socket if it is on this
// replica, and otherwise forwards it over the bus for the replica that holds it.
func (h *Handler) deliverRemotePrompt(sessionID, content string) {
	if h.sessionRegistry.DeliverPrompt(sessionID, content) {
		return
	}
	if h.cfg.CommandBus == nil {
		return
	}
	if payload, err := json.Marshal(remoteCommand{SessionID: sessionID, Content: content}); err == nil {
		h.cfg.CommandBus.PublishCommand(payload)
	}
}

// consumeRemoteCommands delivers bus-forwarded commands to a session this replica
// holds, ignoring the ones bound for a socket elsewhere.
func (h *Handler) consumeRemoteCommands(incoming <-chan []byte) {
	for payload := range incoming {
		var cmd remoteCommand
		if json.Unmarshal(payload, &cmd) != nil {
			continue
		}
		h.sessionRegistry.DeliverPrompt(cmd.SessionID, cmd.Content)
	}
}

// remoteSessionResponse is the wire shape of one live session.
type remoteSessionResponse struct {
	ID          string     `json:"id"`
	DisplayName string     `json:"display_name,omitempty"`
	Platform    string     `json:"platform,omitempty"`
	Host        string     `json:"host,omitempty"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	LastSeenAt  *time.Time `json:"last_seen_at,omitempty"`
}

type remoteSessionsResponse struct {
	Sessions []remoteSessionResponse `json:"sessions"`
}

// listRemoteSessionsHandler returns the caller's live sessions with presence.
func (h *Handler) listRemoteSessionsHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.guard().ActiveUser(w, r)
	if !ok {
		return
	}
	if h.cfg.RemoteSessionStore == nil {
		httputil.WriteJSONError(w, http.StatusServiceUnavailable, "remote control not configured")
		return
	}
	sessions, err := h.cfg.RemoteSessionStore.ListRemoteSessionsByUser(r.Context(), userID)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "list_remote_sessions")
		return
	}
	out := make([]remoteSessionResponse, 0, len(sessions))
	for _, s := range sessions {
		item := remoteSessionResponse{
			ID:          s.ID,
			DisplayName: s.DisplayName,
			Platform:    s.Platform,
			Host:        s.Host,
			Status:      string(s.Status),
			CreatedAt:   s.CreatedAt,
		}
		if !s.LastSeenAt.IsZero() {
			t := s.LastSeenAt
			item.LastSeenAt = &t
		}
		out = append(out, item)
	}
	httputil.WriteJSON(w, http.StatusOK, remoteSessionsResponse{Sessions: out})
}

// remoteSessionStreamHandler streams a session's relayed output over SSE, after
// checking the caller owns it. It mirrors the Task output stream: buffer replay
// then live deltas until the session ends.
func (h *Handler) remoteSessionStreamHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.guard().ActiveUser(w, r)
	if !ok {
		return
	}
	if h.cfg.RemoteSessionStore == nil {
		httputil.WriteJSONError(w, http.StatusServiceUnavailable, "remote control not configured")
		return
	}
	sessionID, ok := httputil.PathValue(w, r, "session_id")
	if !ok {
		return
	}
	sess, err := h.cfg.RemoteSessionStore.GetRemoteSession(r.Context(), sessionID)
	if err != nil || sess.UserID != userID {
		// A session owned by someone else is reported the same as a missing one, so
		// ownership is not probeable.
		httputil.WriteJSONError(w, http.StatusNotFound, "session not found")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}

	events, unsub := h.hub.Subscribe(sessionID)
	defer unsub()

	if buf := h.hub.Buffer(sessionID); buf != "" {
		writeRemoteSSE(w, buf)
		if flusher != nil {
			flusher.Flush()
		}
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case <-h.cfg.Drain:
			// This server is going away; the session lives on the laptop and keeps
			// relaying into whichever replica the client reconnects to.
			writeRemoteSSEEvent(w, "draining", "")
			if flusher != nil {
				flusher.Flush()
			}
			return
		case msg, ok := <-events:
			if !ok {
				return
			}
			if msg == wsconn.StreamEventDone {
				writeRemoteSSE(w, "done")
				if flusher != nil {
					flusher.Flush()
				}
				return
			}
			writeRemoteSSE(w, msg)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

func writeRemoteSSEEvent(w http.ResponseWriter, event, payload string) {
	_, _ = w.Write([]byte("event: " + event + "\n"))
	writeRemoteSSE(w, payload)
}

func writeRemoteSSE(w http.ResponseWriter, payload string) {
	if payload == "" {
		_, _ = w.Write([]byte("data: \n\n"))
		return
	}
	scanner := bufio.NewScanner(strings.NewReader(payload))
	for scanner.Scan() {
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(scanner.Bytes())
		_, _ = w.Write([]byte("\n"))
	}
	_, _ = w.Write([]byte("\n"))
}
