package auth

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	"github.com/icloudbb/buildmax/internal/server/httputil"
	identitysvc "github.com/icloudbb/buildmax/internal/service/identity"
)

// The Portal keeps its access token in memory and its renewable credential in a
// cookie it cannot read. These three routes are credential-delivery adapters
// over the same identity service the JSON routes use: they never put a refresh
// token in a response body, so a script that runs in the Portal cannot read one.
const (
	portalRefreshCookie = "buildmax_portal_refresh"
	// Scoped to the session routes: the browser attaches the renewable credential
	// only to the endpoints that rotate it, not to every API call.
	portalRefreshCookiePath = "/api/auth/portal"
)

// PortalSessionResponse is what the Portal routes return: the short-lived access
// token and who it belongs to, never the refresh token.
type PortalSessionResponse struct {
	AccessToken string    `json:"access_token"`
	ExpiresIn   int64     `json:"expires_in"`
	User        LoginUser `json:"user"`
}

// portalLoginHandler serves POST /api/auth/portal/login: verify a credential,
// set the refresh cookie, and return the access token in the body.
func (h *Handler) portalLoginHandler(w http.ResponseWriter, r *http.Request) {
	if !h.requirePortalOrigin(w, r) {
		return
	}
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	result, err := h.identityService().Login(r.Context(), identitysvc.LoginCmd{
		Email:    req.Email,
		Password: req.Password,
		Otp:      req.Otp,
		Platform: "portal",
	})
	if err != nil {
		h.writeLoginError(w, r, req, err)
		return
	}
	if result.LoginMetaErr != nil {
		// Not fatal: the session is real, so this is recorded rather than answered.
		slog.Error("update login meta failed", "err", result.LoginMetaErr, "handler", "portal_login", "user_id", result.User.ID)
	}
	h.cfg.Audit.Record(r.Context(), coreaudit.Event{
		ActorType:  coreaudit.ActorUser,
		ActorID:    result.User.ID,
		Action:     coreaudit.UserLogin,
		TargetType: "platform",
		TargetID:   result.Platform,
		Detail:     result.Method,
	})
	h.setPortalRefreshCookie(w, r, result.RefreshToken)
	httputil.WriteJSON(w, http.StatusOK, PortalSessionResponse{
		AccessToken: result.AccessToken,
		ExpiresIn:   result.ExpiresIn,
		User:        LoginUser{ID: result.User.ID, Email: result.User.Email, Name: result.User.Name},
	})
}

// portalSessionHandler serves POST /api/auth/portal/session: exchange the
// refresh cookie for a fresh access token, rotating the cookie. The Portal calls
// it on load, on reload, and whenever a request meets a 401. It is how the Portal
// gets an access token without ever holding a refresh token in script.
func (h *Handler) portalSessionHandler(w http.ResponseWriter, r *http.Request) {
	if !h.requirePortalOrigin(w, r) {
		return
	}
	cookie, err := r.Cookie(portalRefreshCookie)
	if err != nil || cookie.Value == "" {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "no session")
		return
	}
	result, err := h.identityService().Refresh(r.Context(), cookie.Value)
	if err != nil {
		// A dead cookie is cleared so the browser stops presenting it, then the
		// same one-status refusal the JSON refresh gives.
		h.clearPortalRefreshCookie(w, r)
		h.writeRefreshError(w, r, err)
		return
	}
	h.setPortalRefreshCookie(w, r, result.RefreshToken)
	httputil.WriteJSON(w, http.StatusOK, PortalSessionResponse{
		AccessToken: result.AccessToken,
		ExpiresIn:   result.ExpiresIn,
		User:        LoginUser{ID: result.User.ID, Email: result.User.Email, Name: result.User.Name},
	})
}

// portalLogoutHandler serves POST /api/auth/portal/logout: revoke the session the
// cookie names and clear the cookie. An absent cookie is still a success — the
// caller's goal, being signed out, already holds.
func (h *Handler) portalLogoutHandler(w http.ResponseWriter, r *http.Request) {
	if !h.requirePortalOrigin(w, r) {
		return
	}
	if cookie, err := r.Cookie(portalRefreshCookie); err == nil && cookie.Value != "" {
		if _, err := h.identityService().LogoutByRefreshToken(r.Context(), cookie.Value); err != nil {
			if !httputil.WriteServiceError(w, err) {
				httputil.WriteInternalError(w, err, "auth handler error", "handler", "portal_logout")
			}
			return
		}
	}
	h.clearPortalRefreshCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setPortalRefreshCookie(w http.ResponseWriter, r *http.Request, token string) {
	if token == "" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     portalRefreshCookie,
		Value:    token,
		Path:     portalRefreshCookiePath,
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(h.refreshTokenTTL() / time.Second),
	})
}

func (h *Handler) clearPortalRefreshCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     portalRefreshCookie,
		Value:    "",
		Path:     portalRefreshCookiePath,
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

// requirePortalOrigin refuses a cross-origin request to a session-establishing
// route. The renewable credential is a SameSite=Strict cookie, so the browser
// already withholds it cross-site; this is the belt to that suspenders, and it
// is why these routes get no permissive CORS. A browser always sends Origin on a
// cross-origin POST and on a same-origin one, so an absent Origin is refused too.
func (h *Handler) requirePortalOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin != "" && origin == requestOrigin(r) {
		return true
	}
	httputil.WriteJSONError(w, http.StatusForbidden, "forbidden")
	return false
}

// requestOrigin is the scheme-and-host the request arrived on, to compare an
// Origin header against. Behind a TLS-terminating proxy the scheme comes from
// X-Forwarded-Proto; the proxy is expected to preserve Host.
func requestOrigin(r *http.Request) string {
	scheme := "http"
	if requestIsHTTPS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func requestIsHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
