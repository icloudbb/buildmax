// Package account serves what the acting subject — the authenticated account —
// owns across spaces, rather than what any single space owns.
//
// Webhook keys and chat-account links are the current members: both key every
// row by user_id, so the routes are top-level (/api/webhook-keys,
// /api/channel-links), not space-scoped.
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
	// Sessions backs the guard's active-session check.
	Sessions coreidentity.AuthSessionStore
	// WebhookKeys is the account-owned key store. Nil leaves the routes
	// reporting the feature is unconfigured.
	WebhookKeys coreidentity.UserWebhookKeyStore
	// ChannelLinks manages the account's chat-platform links. Nil means no chat
	// platform is configured: the list answers empty and the rest 503.
	ChannelLinks ChannelLinks
	// Audit records key and link creation and removal. Nil discards them.
	Audit *audit.Recorder
}

type Handler struct{ cfg Config }

func New(cfg Config) *Handler { return &Handler{cfg: cfg} }

// guard authenticates the account. It holds no space store: these routes are
// account-scoped and never make a space authorization decision.
func (h *Handler) guard() *access.Guard {
	return &access.Guard{JWTSecret: h.cfg.JWTSecret, Users: h.cfg.Users, Sessions: h.cfg.Sessions, Audit: h.cfg.Audit}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/webhook-keys", h.createWebhookKeyHandler)
	mux.HandleFunc("GET /api/webhook-keys", h.listWebhookKeysHandler)
	mux.HandleFunc("DELETE /api/webhook-keys/{key_id}", h.revokeWebhookKeyHandler)
	mux.HandleFunc("GET /api/channel-links", h.listChannelLinksHandler)
	mux.HandleFunc("POST /api/channel-links", h.createChannelLinkHandler)
	mux.HandleFunc("DELETE /api/channel-links/{link_id}", h.deleteChannelLinkHandler)
	mux.HandleFunc("GET /api/channel-link-pairings", h.getChannelPairingHandler)
}
