// Package account serves what the acting subject — the authenticated account —
// owns across spaces, rather than what any single space owns.
//
// Webhook keys are the current member: user_webhook_key keys every row by
// user_id, so the routes are top-level (/api/webhook-keys), not space-scoped.
// The distinction is the ownership rule in the route conventions in
// docs/contribute/architecture/server.md; keeping these handlers out of the
// space package makes it something the compiler knows about rather than
// something a reviewer has to remember.
package account

import (
	"net/http"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	"github.com/icloudbb/buildmax/internal/server/access"
	"github.com/icloudbb/buildmax/internal/service/audit"
)

type Config struct {
	// JWTSecret authenticates the caller. Empty means no one can be
	// authenticated, and the guard says so rather than trusting an unsigned token.
	JWTSecret string
	// Users backs the guard's active-account check.
	Users coreidentity.UserStore
	// WebhookKeys is the account-owned key store. Nil leaves the routes
	// reporting the feature is unconfigured.
	WebhookKeys coreidentity.UserWebhookKeyStore
	// Audit records key creation and revocation. Nil discards them.
	Audit *audit.Recorder
}

type Handler struct{ cfg Config }

func New(cfg Config) *Handler { return &Handler{cfg: cfg} }

// guard authenticates the account. It holds no space store: these routes are
// account-scoped and never make a space authorization decision.
func (h *Handler) guard() *access.Guard {
	return &access.Guard{JWTSecret: h.cfg.JWTSecret, Users: h.cfg.Users, Audit: h.cfg.Audit}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/webhook-keys", h.createWebhookKeyHandler)
	mux.HandleFunc("GET /api/webhook-keys", h.listWebhookKeysHandler)
	mux.HandleFunc("DELETE /api/webhook-keys/{key_id}", h.revokeWebhookKeyHandler)
}
