package account

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	corechannel "github.com/icloudbb/buildmax/internal/core/channel"
	"github.com/icloudbb/buildmax/internal/server/httputil"
)

// ChannelLinks is the chat-account link service. The Server's channel gateway
// implements it; nil means no chat platform is configured.
type ChannelLinks interface {
	Platforms(ctx context.Context) []corechannel.Info
	PreviewPairing(ctx context.Context, code string) (*corechannel.Pairing, error)
	ConfirmPairing(ctx context.Context, userID, code string) (*corechannel.Identity, error)
	ListLinks(ctx context.Context, userID string) ([]corechannel.Identity, error)
	Unlink(ctx context.Context, userID, identityID string) error
}

type channelLinkResponse struct {
	ID        string    `json:"id"`
	Platform  string    `json:"platform"`
	Handle    string    `json:"handle,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type listChannelLinksResponse struct {
	// Platforms are the chat platforms this deployment has a bot on. Empty
	// means none, and the Portal says so rather than offering a dead end.
	Platforms []corechannel.Info    `json:"platforms"`
	Links     []channelLinkResponse `json:"links"`
}

type channelPairingResponse struct {
	Platform  string    `json:"platform"`
	Handle    string    `json:"handle,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
}

type createChannelLinkRequest struct {
	Code string `json:"code"`
}

func toChannelLinkResponse(i corechannel.Identity) channelLinkResponse {
	return channelLinkResponse{ID: i.ID, Platform: i.Platform, Handle: i.Handle, CreatedAt: i.CreatedAt}
}

func (h *Handler) listChannelLinksHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.guard().ActiveUser(w, r)
	if !ok {
		return
	}
	out := listChannelLinksResponse{Platforms: []corechannel.Info{}, Links: []channelLinkResponse{}}
	if h.cfg.ChannelLinks == nil {
		httputil.WriteJSON(w, http.StatusOK, out)
		return
	}
	links, err := h.cfg.ChannelLinks.ListLinks(r.Context(), userID)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "list_channel_links", "user_id", userID)
		return
	}
	out.Platforms = h.cfg.ChannelLinks.Platforms(r.Context())
	for _, l := range links {
		out.Links = append(out.Links, toChannelLinkResponse(l))
	}
	httputil.WriteJSON(w, http.StatusOK, out)
}

// getChannelPairingHandler shows which chat account a code would link. The code
// is a query parameter so the request log redacts it.
func (h *Handler) getChannelPairingHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.guard().UserAndStore(w, r, h.cfg.ChannelLinks, "chat channels not configured"); !ok {
		return
	}
	p, err := h.cfg.ChannelLinks.PreviewPairing(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		if !httputil.WriteServiceError(w, err) {
			httputil.WriteInternalError(w, err, "handler error", "handler", "get_channel_pairing")
		}
		return
	}
	httputil.WriteJSON(w, http.StatusOK, channelPairingResponse{Platform: p.Platform, Handle: p.Handle, ExpiresAt: p.ExpiresAt})
}

func (h *Handler) createChannelLinkHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.guard().UserAndStore(w, r, h.cfg.ChannelLinks, "chat channels not configured")
	if !ok {
		return
	}
	var req createChannelLinkRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	link, err := h.cfg.ChannelLinks.ConfirmPairing(r.Context(), userID, req.Code)
	if err != nil {
		if !httputil.WriteServiceError(w, err) {
			httputil.WriteInternalError(w, err, "handler error", "handler", "create_channel_link", "user_id", userID)
		}
		return
	}
	// A link lets a chat account act as this user, so its life is in the trail
	// like a webhook key's. The platform ids are not.
	h.cfg.Audit.UserAction(r.Context(), userID, "", coreaudit.ChannelLinkCreated, "channel_link", link.ID, link.Platform)
	httputil.WriteJSON(w, http.StatusCreated, toChannelLinkResponse(*link))
}

func (h *Handler) deleteChannelLinkHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.guard().UserAndStore(w, r, h.cfg.ChannelLinks, "chat channels not configured")
	if !ok {
		return
	}
	linkID, ok := httputil.PathValue(w, r, "link_id")
	if !ok {
		return
	}
	if err := h.cfg.ChannelLinks.Unlink(r.Context(), userID, linkID); err != nil {
		// Someone else's link and no link read the same, so an id is not an
		// existence oracle.
		if errors.Is(err, apierr.ErrNotFound) {
			httputil.WriteJSONError(w, http.StatusNotFound, "chat link not found")
			return
		}
		if !httputil.WriteServiceError(w, err) {
			httputil.WriteInternalError(w, err, "handler error", "handler", "delete_channel_link", "user_id", userID)
		}
		return
	}
	h.cfg.Audit.UserAction(r.Context(), userID, "", coreaudit.ChannelLinkRemoved, "channel_link", linkID, "")
	w.WriteHeader(http.StatusNoContent)
}
