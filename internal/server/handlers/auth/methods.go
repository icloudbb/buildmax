package auth

import (
	"net/http"

	"github.com/icloudbb/buildmax/internal/server/httputil"
)

// localLoginAll is the default effective mode when Config.LocalLogin is unset.
// It is a literal rather than a config import because a server handler may not
// depend on the config package; the wiring already passes the effective mode, so
// this only covers a zero-value Config in a test.
const localLoginAll = "all"

// MethodsResponse tells an unauthenticated client which ways in this deployment
// offers, so a Portal knows whether to render local inputs, an SSO button, or
// both. It carries no issuer, client id, or policy: those are the IdP's and the
// operator's business, not something to publish to anyone who can reach /login.
type MethodsResponse struct {
	// LocalLogin is the effective mode for native password and login-code sign-in:
	// "all", "system_admins", or "off".
	LocalLogin string `json:"local_login"`
	// OIDC describes corporate sign-in. Enabled false means it is not offered.
	OIDC MethodsOIDC `json:"oidc"`
}

// MethodsOIDC is the public description of SSO — only what a button needs.
type MethodsOIDC struct {
	Enabled bool `json:"enabled"`
	// DisplayName labels the sign-in button. Empty when SSO is off.
	DisplayName string `json:"display_name,omitempty"`
}

// GET /api/auth/methods reports the enabled sign-in methods. It is
// unauthenticated by design: a client has to know how to sign in before it can.
func (h *Handler) methodsHandler(w http.ResponseWriter, _ *http.Request) {
	local := h.cfg.LocalLogin
	if local == "" {
		local = localLoginAll
	}
	resp := MethodsResponse{
		LocalLogin: local,
		OIDC:       MethodsOIDC{Enabled: h.cfg.OIDCEnabled},
	}
	if h.cfg.OIDCEnabled {
		resp.OIDC.DisplayName = h.cfg.OIDCDisplayName
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}
