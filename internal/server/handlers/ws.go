package handlers

import (
	"net/http"

	"github.com/icloudbb/buildmax/internal/server/httputil"
	wsconn "github.com/icloudbb/buildmax/internal/server/websocket"
)

// wsUpgradeHandler authenticates the upgrade and hands the socket over.
//
// The credential arrives as a query parameter rather than a header because a
// browser cannot set one on a WebSocket upgrade. Everything after "who is this
// and which space" belongs to internal/server/websocket.
func (h *Handler) wsUpgradeHandler(w http.ResponseWriter, r *http.Request) {
	// A socket opened now would be hijacked past the shutdown that is already
	// running, and its first turn refused anyway. The Portal reconnects with
	// backoff, so refusing sends it to an instance that will still be here.
	if h.draining() {
		http.Error(w, "this server is shutting down", http.StatusServiceUnavailable)
		return
	}
	tokenStr := r.URL.Query().Get("token")
	if tokenStr == "" {
		http.Error(w, "token required", http.StatusUnauthorized)
		return
	}
	// The same active-account and active-session checks the HTTP guard makes, so a
	// disabled account or a revoked session cannot open a socket on a token that
	// has not yet expired.
	userID, ok := h.guard().TokenSubjectActive(r.Context(), tokenStr)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if h.cfg.SpaceStore == nil {
		http.Error(w, "spaces not configured", http.StatusServiceUnavailable)
		return
	}
	spaceID, ok := httputil.PathValue(w, r, "space_id")
	if !ok {
		return
	}
	if _, spaceID, ok = h.guard().ExplicitSpace(w, r, userID, spaceID); !ok {
		return
	}
	wsconn.Serve(w, r, userID, spaceID, h.connDeps())
}

func (h *Handler) connDeps() wsconn.ConnDeps {
	return wsconn.ConnDeps{
		Conversations: h.cfg.ConversationStore,
		Turns:         h.turns,
		Turner:        h.conversations,
		Registry:      h.connRegistry,
		CORSOrigin:    h.cfg.CORSOrigin,
	}
}
