package auth

import (
	"net/http"
	"time"

	"github.com/icloudbb/buildmax/internal/server/httputil"
	identitysvc "github.com/icloudbb/buildmax/internal/service/identity"
)

// Self-service sessions (design §14.5). Native sign-in lets a person create
// sessions for other machines, so recovering from a mistaken or phished
// confirmation must not need an operator. Both routes act only on the caller's
// own account: the account comes from the token, never from the request.

// Session is one of the caller's live sessions. It carries metadata to recognise
// a device by, and never a token or its hash.
type Session struct {
	SessionID  string `json:"session_id"`
	Platform   string `json:"platform,omitempty"`
	AuthMethod string `json:"auth_method,omitempty"`
	// ClientLabel is what a native client said it is when it signed in. It is
	// the client's claim, not verified.
	ClientLabel string    `json:"client_label,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	// LastSeenAt is zero until the session is first used after sign-in.
	LastSeenAt time.Time `json:"last_seen_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	// Current marks the session that made this request.
	Current bool `json:"current"`
}

// SessionsResponse is the caller's live sessions, newest first.
type SessionsResponse struct {
	Sessions []Session `json:"sessions"`
}

// listSessionsHandler serves GET /api/auth/sessions.
func (h *Handler) listSessionsHandler(w http.ResponseWriter, r *http.Request) {
	userID, sid, ok := h.guard().ActiveUserSession(w, r)
	if !ok {
		return
	}
	sessions, err := h.identityService().ListUserSessions(r.Context(), userID)
	if err != nil {
		if !httputil.WriteServiceError(w, err) {
			httputil.WriteInternalError(w, err, "auth handler error", "handler", "list_sessions", "user_id", userID)
		}
		return
	}
	out := make([]Session, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, Session{
			SessionID:   s.SID,
			Platform:    s.Platform,
			AuthMethod:  s.AuthMethod,
			ClientLabel: s.ClientLabel,
			CreatedAt:   s.CreatedAt,
			LastSeenAt:  s.LastSeenAt,
			ExpiresAt:   s.AbsoluteExpiresAt,
			Current:     s.SID == sid,
		})
	}
	httputil.WriteJSON(w, http.StatusOK, SessionsResponse{Sessions: out})
}

// revokeSessionHandler serves DELETE /api/auth/sessions/{session_id}. Naming
// the caller's own session signs it out; the Portal does not, because its
// ordinary sign-out ends that session and clears the cookie in one action.
func (h *Handler) revokeSessionHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.guard().ActiveUser(w, r)
	if !ok {
		return
	}
	sessionID, ok := httputil.PathValue(w, r, "session_id")
	if !ok {
		return
	}
	_, err := h.identityService().RevokeUserSession(r.Context(), identitysvc.RevokeUserSessionCmd{
		ActorID: userID, UserID: userID, SessionID: sessionID,
	})
	if err != nil {
		if !httputil.WriteServiceError(w, err) {
			httputil.WriteInternalError(w, err, "auth handler error", "handler", "revoke_session", "user_id", userID)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
