// Package auth serves the routes that establish a session.
//
// Everything here runs before a caller has one, or changes the credential that
// produces one: request an account, log in, refresh, log out, set a password.
// Once a session exists, deciding what it may reach is internal/server/access's
// job, not this package's -- which is why this Config holds no space store.
package auth

import (
	"context"
	"net/http"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	infraoidc "github.com/icloudbb/buildmax/internal/infra/oidc"
	"github.com/icloudbb/buildmax/internal/server/access"
	"github.com/icloudbb/buildmax/internal/service/audit"
)

// OIDCFlow is the slice of the OIDC provider the browser flow uses. It is an
// interface so a handler test can drive an adversarial fake IdP without a
// network; *internal/infra/oidc.Provider satisfies it.
type OIDCFlow interface {
	BeginAuth(ctx context.Context, redirectURL string, scopes []string) (string, infraoidc.Transaction, error)
	Complete(ctx context.Context, code string, txn infraoidc.Transaction, redirectURL string, scopes []string) (*infraoidc.Claims, error)
}

type Config struct {
	// JWTSecret signs access tokens. Empty means this deployment cannot log
	// anyone in, and the routes say so rather than minting something unsigned.
	JWTSecret string
	// AllowSignup opens POST /api/auth/otp to self-registration. False --
	// the zero value -- means accounts are created by an operator.
	AllowSignup      bool
	DefaultQuotaTier string

	// LocalLogin gates the native password and login-code paths: "all"
	// (the default when empty), "system_admins", or "off". It is advertised at
	// GET /api/auth/methods so the Portal knows whether to show local inputs.
	LocalLogin string
	// OIDCEnabled and OIDCDisplayName advertise SSO at GET /api/auth/methods.
	// They carry no secret: the issuer, client, and policy stay server-side.
	OIDCEnabled     bool
	OIDCDisplayName string
	// OIDC drives the browser sign-in flow. Nil when SSO is not configured, which
	// makes the /api/auth/oidc/* routes answer 404.
	OIDC OIDCFlow
	// OIDCSessionMaxAge caps a session opened through SSO. Zero uses the core
	// default; it is deliberately shorter than a native login's absolute TTL.
	OIDCSessionMaxAge time.Duration
	// Provisioning and AllowedEmailDomains parameterize just-in-time account
	// creation on a first SSO sign-in.
	Provisioning        string
	AllowedEmailDomains []string
	// PublicBaseURL is the externally reachable origin. The OIDC redirect URI and
	// the post-login Portal redirect are built from it, never from a request Host.
	PublicBaseURL string

	// Token lifetimes. Zero means the model package's default. The access token
	// is signed and unstored, so its lifetime is the window in which a stolen
	// one still works; the refresh token is a row and can be revoked before it
	// expires.
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	// RefreshRotationGrace is how long a just-rotated refresh token may be
	// exchanged again before that counts as reuse. It exists because the CLI
	// and Desktop share one credentials file between processes.
	RefreshRotationGrace time.Duration
	// SessionAbsoluteTTL caps a session's life from its creation, regardless of
	// refresh activity. Zero means the core package default.
	SessionAbsoluteTTL time.Duration

	Users         coreidentity.UserStore
	LoginCodes    coreidentity.LoginCodeStore
	Passwords     coreidentity.PasswordStore
	RefreshTokens coreidentity.RefreshTokenStore
	// Sessions is the durable session authority: a login opens one, the guard
	// checks it on every request, and logout/revocation retire it.
	Sessions coreidentity.AuthSessionStore
	// ExternalIdentities links accounts to verified IdP identities, for the SSO
	// association. Nil when SSO is not configured.
	ExternalIdentities coreidentity.ExternalIdentityStore
	// Grants resolves an account's system roles, for the system_admins
	// local-login mode. Nil makes that mode refuse everyone.
	Grants coreidentity.SystemGrantStore

	// Audit records logins and credential changes. Nil discards them.
	Audit *audit.Recorder
}

type Handler struct{ cfg Config }

func New(cfg Config) *Handler { return &Handler{cfg: cfg} }

func (h *Handler) guard() *access.Guard {
	return &access.Guard{JWTSecret: h.cfg.JWTSecret, Users: h.cfg.Users, Sessions: h.cfg.Sessions, Audit: h.cfg.Audit}
}

func (h *Handler) Register(mux *http.ServeMux) {
	// Session and credential routes for the acting subject share the /api/auth/
	// prefix. See the route conventions in docs/contribute/architecture/server.md
	// Unauthenticated.
	mux.HandleFunc("GET /api/auth/methods", h.methodsHandler)
	// SSO browser flow. Unauthenticated by nature: they establish who the caller
	// is. They answer 404 when OIDC is not configured.
	mux.HandleFunc("GET /api/auth/oidc/start", h.oidcStartHandler)
	mux.HandleFunc("GET /api/auth/oidc/callback", h.oidcCallbackHandler)
	mux.HandleFunc("POST /api/auth/otp", h.otpRequestHandler)
	mux.HandleFunc("POST /api/auth/login", h.loginHandler)
	mux.HandleFunc("POST /api/auth/token/refresh", h.refreshHandler)
	mux.HandleFunc("POST /api/auth/logout", h.logoutHandler)
	// Portal credential-delivery adapters: same identity service as the routes
	// above, but the renewable credential rides in a Secure, HttpOnly,
	// SameSite=Strict cookie instead of a JSON body. See portal.go.
	mux.HandleFunc("POST /api/auth/portal/login", h.portalLoginHandler)
	mux.HandleFunc("POST /api/auth/portal/session", h.portalSessionHandler)
	mux.HandleFunc("POST /api/auth/portal/logout", h.portalLogoutHandler)
	// Authenticated; sets or changes the caller's own password.
	mux.HandleFunc("POST /api/auth/password", h.setPasswordHandler)
}
