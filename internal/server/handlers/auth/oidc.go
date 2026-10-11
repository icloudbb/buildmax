package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	infraoidc "github.com/icloudbb/buildmax/internal/infra/oidc"
	identitysvc "github.com/icloudbb/buildmax/internal/service/identity"
)

// The OIDC browser flow. Two GETs: start redirects to the IdP, callback brings
// the browser back with an authorization code. BuildMax proves authentication
// through the IdP and then owns the session exactly as a password login does —
// the callback opens a session and sets the same Portal refresh cookie the
// Portal already hydrates from, so no new client path is needed.
const (
	oidcCallbackPath = "/api/auth/oidc/callback"
	// The transaction cookie binds a callback to the browser that began it. It is
	// SameSite=Lax, not Strict: the callback is a top-level navigation coming from
	// the IdP's origin, and a Strict cookie would not be sent on it. It is scoped
	// to the OIDC routes and lives only as long as a login reasonably takes.
	oidcTxnCookie = "buildmax_oidc_txn"
	oidcTxnPath   = "/api/auth/oidc"
	oidcTxnMaxAge = 10 * time.Minute
	// oidcReturnParam names the Portal hash route the person returns to after
	// signing in, such as `#/sign-in/WDJB-MJHT` (design §8).
	oidcReturnParam = "return_to"
	// oidcReturnMaxLen bounds the return route. A Portal route is far shorter;
	// the bound only keeps the cookie small.
	oidcReturnMaxLen = 256
)

// SSO failure classes surfaced to the Portal as a coarse, non-sensitive
// ?sso_error= code. The raw provider error stays in the server log; this only
// tells the person which of the four documented outcomes happened so the Portal
// can say something true. See docs/design/enterprise-identity-and-access.md §5.
const (
	ssoErrUnavailable   = "unavailable"    // IdP unreachable, or the code exchange failed
	ssoErrExpired       = "expired"        // the login transaction was missing, altered, or stale
	ssoErrNotAuthorized = "not_authorized" // authenticated, but this deployment will not admit the account
	ssoErrDisabled      = "disabled"       // the account exists here and is switched off
)

// oidcStartHandler serves GET /api/auth/oidc/start: mint a transaction, set its
// cookie, and redirect the browser to the IdP's authorize endpoint.
func (h *Handler) oidcStartHandler(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.OIDCEnabled || h.cfg.OIDC == nil {
		http.NotFound(w, r)
		return
	}
	returnTo := portalReturnRoute(r.URL.Query().Get(oidcReturnParam))
	redirectURL := h.oidcRedirectURL()
	authURL, txn, err := h.cfg.OIDC.BeginAuth(r.Context(), redirectURL, nil)
	if err != nil {
		// The IdP is unreachable or its metadata will not load. Send the person
		// back to the sign-in page with a coarse reason rather than a raw error.
		slog.Warn("oidc start failed", "err", err, "handler", "oidc_start")
		h.redirectSSOError(w, r, ssoErrUnavailable, returnTo)
		return
	}
	h.setOIDCTxnCookie(w, r, txn, returnTo)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// portalReturnRoute returns raw when it is a Portal hash route this server may
// send a browser to, and "" otherwise, which lands on the Portal root. The
// callback puts it after the Portal's own base URL, so a fragment could not
// leave the origin anyway; the rules keep it a plain route all the same: `#/`
// then unreserved characters, `/`, and a query, with no empty or dot segment
// and nothing percent-encoded that a later decode could turn into another URL.
func portalReturnRoute(raw string) string {
	if len(raw) > oidcReturnMaxLen || !strings.HasPrefix(raw, "#/") {
		return ""
	}
	for _, c := range raw[1:] {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.ContainsRune("-._~/?=&", c):
		default:
			return ""
		}
	}
	path, _, _ := strings.Cut(raw[1:], "?")
	if strings.Contains(path, "//") {
		return ""
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return ""
		}
	}
	return raw
}

// oidcCallbackHandler serves GET /api/auth/oidc/callback: verify the
// transaction, exchange the code, validate the ID Token, associate the account,
// open a session, and hand the browser the Portal refresh cookie.
func (h *Handler) oidcCallbackHandler(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.OIDCEnabled || h.cfg.OIDC == nil {
		http.NotFound(w, r)
		return
	}
	// The transaction cookie is one-time: read it, then clear it whatever happens,
	// so a replayed callback cannot reuse it.
	txn, returnTo, ok := h.readOIDCTxnCookie(r)
	h.clearOIDCTxnCookie(w, r)
	// The route was checked at start and is signed; checking it again keeps the
	// callback safe on its own, whatever issued the cookie.
	returnTo = portalReturnRoute(returnTo)

	// An IdP that returns an error param never carried a code. It is the account
	// being refused upstream (consent declined, not assigned the app), not a
	// transport fault.
	if e := r.URL.Query().Get("error"); e != "" {
		slog.Warn("oidc callback returned an error", "idp_error", e, "handler", "oidc_callback")
		h.redirectSSOError(w, r, ssoErrNotAuthorized, returnTo)
		return
	}
	if !ok || txn.State == "" || r.URL.Query().Get("state") != txn.State {
		// Missing, altered, stale, or mismatched: the login did not begin in this
		// browser, or it took too long. Nothing here is proof of anything.
		h.redirectSSOError(w, r, ssoErrExpired, returnTo)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		h.redirectSSOError(w, r, ssoErrExpired, returnTo)
		return
	}

	claims, err := h.cfg.OIDC.Complete(r.Context(), code, txn, h.oidcRedirectURL(), nil)
	if err != nil {
		// Exchange or validation failed. The detail is a provider or protocol
		// fault, logged with a correlation the Portal never sees; the person gets
		// the coarse "provider unavailable".
		slog.Warn("oidc code exchange failed", "err", err, "handler", "oidc_callback")
		h.redirectSSOError(w, r, ssoErrUnavailable, returnTo)
		return
	}

	// A first association requires a verified email; an unverified one is passed
	// as none, so the association refuses to create or link on it.
	verifiedEmail := ""
	if claims.EmailVerified {
		verifiedEmail = claims.Email
	} else if claims.Email != "" {
		slog.Info("oidc email is not verified; it cannot link or provision an account", "handler", "oidc_callback")
	}
	svc := h.identityService()
	assoc, err := svc.Associate(r.Context(), identitysvc.AssociationInput{
		Issuer: claims.Issuer, Subject: claims.Subject, VerifiedEmail: verifiedEmail, Name: claims.Name,
	})
	if err != nil {
		h.redirectSSOError(w, r, classifyAssociationError(err), returnTo)
		return
	}

	result, err := svc.StartSSOSession(r.Context(), assoc.User, coreidentity.PlatformPortal, h.cfg.OIDCSessionMaxAge)
	if err != nil {
		if errors.Is(err, identitysvc.ErrDisabled) {
			h.redirectSSOError(w, r, ssoErrDisabled, returnTo)
			return
		}
		slog.Error("oidc session open failed", "err", err, "handler", "oidc_callback", "user_id", assoc.User.ID)
		h.redirectSSOError(w, r, ssoErrUnavailable, returnTo)
		return
	}
	if result.LoginMetaErr != nil {
		slog.Error("update login meta failed", "err", result.LoginMetaErr, "handler", "oidc_callback", "user_id", assoc.User.ID)
	}
	h.cfg.Audit.Record(r.Context(), coreaudit.Event{
		ActorType:  coreaudit.ActorUser,
		ActorID:    assoc.User.ID,
		Action:     coreaudit.UserLogin,
		TargetType: "platform",
		TargetID:   result.Platform,
		Detail:     result.Method,
	})
	// The same cookie the Portal hydrates from. The provider's own tokens are not
	// kept: BuildMax owns the session now, so an IdP refresh token would be a
	// credential held for nothing.
	h.setPortalRefreshCookie(w, r, result.RefreshToken)
	http.Redirect(w, r, h.portalBaseURL()+"/"+returnTo, http.StatusFound)
}

// classifyAssociationError maps an association refusal to a coarse SSO error
// class. A disabled account is its own class; every "we will not admit this
// account" reason collapses to not_authorized, which is also the one generic
// answer the association service gives so the callback reveals no more. The
// specific reason goes to the server log, where an operator needs it.
func classifyAssociationError(err error) string {
	switch {
	case errors.Is(err, identitysvc.ErrNotAuthorizedForDeployment),
		errors.Is(err, identitysvc.ErrIdentityNeedsOperator),
		errors.Is(err, identitysvc.ErrEmailRequired):
		slog.Warn("oidc sign-in refused", "reason", err.Error(), "handler", "oidc_callback")
	}
	switch {
	case errors.Is(err, identitysvc.ErrDisabled):
		return ssoErrDisabled
	case errors.Is(err, identitysvc.ErrNotAuthorizedForDeployment),
		errors.Is(err, identitysvc.ErrIdentityNeedsOperator),
		errors.Is(err, identitysvc.ErrEmailRequired):
		return ssoErrNotAuthorized
	case errors.Is(err, identitysvc.ErrSSONotConfigured):
		return ssoErrUnavailable
	default:
		// An unexpected store or read error. Log-worthy, but to the person it is
		// the provider path not working.
		slog.Warn("oidc association error", "err", err, "handler", "oidc_callback")
		return ssoErrUnavailable
	}
}

// oidcRedirectURL is the fixed callback URL, built from public_base_url and never
// from a request Host: the redirect URI is registered with the IdP and a
// Host-derived one would be both rejected and an open-redirect surface.
func (h *Handler) oidcRedirectURL() string {
	return h.portalBaseURL() + oidcCallbackPath
}

func (h *Handler) portalBaseURL() string {
	return strings.TrimRight(h.cfg.PublicBaseURL, "/")
}

// redirectSSOError sends the browser back to the Portal sign-in page with a
// coarse, safe reason. No token, email, or raw provider detail is ever in the URL.
// returnTo, a route portalReturnRoute accepted or "", keeps the page the person
// was signing in for, so signing in another way still lands on it.
func (h *Handler) redirectSSOError(w http.ResponseWriter, r *http.Request, class, returnTo string) {
	http.Redirect(w, r, h.portalBaseURL()+"/login?sso_error="+class+returnTo, http.StatusFound)
}

// txnCookiePayload is what the transaction cookie carries: the transaction, the
// Portal route to return to, and when it expires, integrity-protected so the
// callback trusts only a value this server issued. None of it is a bearer
// secret, but all of it must be unaltered. The return route travels only here,
// so the callback never redirects to anything a request handed it unsigned.
type txnCookiePayload struct {
	Txn    infraoidc.Transaction `json:"txn"`
	Return string                `json:"return,omitempty"`
	Exp    int64                 `json:"exp"`
}

func (h *Handler) setOIDCTxnCookie(w http.ResponseWriter, r *http.Request, txn infraoidc.Transaction, returnTo string) {
	payload := txnCookiePayload{Txn: txn, Return: returnTo, Exp: time.Now().Add(oidcTxnMaxAge).Unix()}
	raw, _ := json.Marshal(payload)
	body := base64.RawURLEncoding.EncodeToString(raw)
	value := body + "." + base64.RawURLEncoding.EncodeToString(h.oidcTxnMAC(body))
	http.SetCookie(w, &http.Cookie{
		Name:     oidcTxnCookie,
		Value:    value,
		Path:     oidcTxnPath,
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(oidcTxnMaxAge / time.Second),
	})
}

func (h *Handler) readOIDCTxnCookie(r *http.Request) (txn infraoidc.Transaction, returnTo string, ok bool) {
	cookie, err := r.Cookie(oidcTxnCookie)
	if err != nil || cookie.Value == "" {
		return infraoidc.Transaction{}, "", false
	}
	body, mac, found := strings.Cut(cookie.Value, ".")
	if !found {
		return infraoidc.Transaction{}, "", false
	}
	gotMAC, err := base64.RawURLEncoding.DecodeString(mac)
	if err != nil || subtle.ConstantTimeCompare(gotMAC, h.oidcTxnMAC(body)) != 1 {
		return infraoidc.Transaction{}, "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return infraoidc.Transaction{}, "", false
	}
	var payload txnCookiePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return infraoidc.Transaction{}, "", false
	}
	if time.Now().Unix() > payload.Exp {
		return infraoidc.Transaction{}, "", false
	}
	return payload.Txn, payload.Return, true
}

func (h *Handler) clearOIDCTxnCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     oidcTxnCookie,
		Value:    "",
		Path:     oidcTxnPath,
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// oidcTxnMAC keys an HMAC to the deployment's JWT secret, domain-separated so it
// can never collide with an access token's signature. Deriving from the shared
// secret is deliberate: any replica can then verify a callback it did not start.
func (h *Handler) oidcTxnMAC(body string) []byte {
	key := sha256.Sum256([]byte("buildmax-oidc-txn\x00" + h.cfg.JWTSecret))
	mac := hmac.New(sha256.New, key[:])
	mac.Write([]byte(body))
	return mac.Sum(nil)
}
