// Package auth serves the routes that establish a session.
//
// Everything here runs before a caller has one, or changes the credential that
// produces one: request an account, log in, refresh, log out, set a password.
// Once a session exists, deciding what it may reach is internal/server/access's
// job, not this package's -- which is why this Config holds no space store.
package auth

import (
	"net/http"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	"github.com/icloudbb/buildmax/internal/server/access"
	"github.com/icloudbb/buildmax/internal/service/audit"
)

type Config struct {
	// JWTSecret signs access tokens. Empty means this deployment cannot log
	// anyone in, and the routes say so rather than minting something unsigned.
	JWTSecret string
	// AllowSignup opens POST /api/auth/otp to self-registration. False --
	// the zero value -- means accounts are created by an operator.
	AllowSignup      bool
	DefaultQuotaTier string

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

	Users         coreidentity.UserStore
	LoginCodes    coreidentity.LoginCodeStore
	Passwords     coreidentity.PasswordStore
	RefreshTokens coreidentity.RefreshTokenStore

	// Audit records logins and credential changes. Nil discards them.
	Audit *audit.Recorder
}

type Handler struct{ cfg Config }

func New(cfg Config) *Handler { return &Handler{cfg: cfg} }

func (h *Handler) guard() *access.Guard {
	return &access.Guard{JWTSecret: h.cfg.JWTSecret, Users: h.cfg.Users, Audit: h.cfg.Audit}
}

func (h *Handler) Register(mux *http.ServeMux) {
	// Session and credential routes for the acting subject share the /api/auth/
	// prefix. See the route conventions in docs/contribute/architecture/server.md
	// Unauthenticated.
	mux.HandleFunc("POST /api/auth/otp", h.otpRequestHandler)
	mux.HandleFunc("POST /api/auth/login", h.loginHandler)
	mux.HandleFunc("POST /api/auth/token/refresh", h.refreshHandler)
	mux.HandleFunc("POST /api/auth/logout", h.logoutHandler)
	// Authenticated; sets or changes the caller's own password.
	mux.HandleFunc("POST /api/auth/password", h.setPasswordHandler)
}
