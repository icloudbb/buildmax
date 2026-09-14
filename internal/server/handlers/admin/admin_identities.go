package admin

import (
	"errors"
	"net/http"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	"github.com/icloudbb/buildmax/internal/server/httputil"
)

// AdminExternalIdentity is one of an account's SSO links as an administrator sees
// it. The subject and issuer are shown because reconciling a mismatch needs
// them; there is no secret here — the identity key is protocol metadata, not a
// credential.
type AdminExternalIdentity struct {
	ID            string     `json:"id"`
	Issuer        string     `json:"issuer"`
	Subject       string     `json:"subject"`
	LastSeenEmail string     `json:"last_seen_email,omitempty"`
	LastSeenName  string     `json:"last_seen_name,omitempty"`
	LastLoginAt   *time.Time `json:"last_login_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// AdminExternalIdentitiesResponse is an account's SSO links.
type AdminExternalIdentitiesResponse struct {
	Identities []AdminExternalIdentity `json:"identities"`
}

// listAdminUserIdentitiesHandler serves GET /api/admin/users/{user_id}/identities.
func (h *Handler) listAdminUserIdentitiesHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.guard().SystemAdmin(w, r); !ok {
		return
	}
	if !httputil.RequireStore(w, h.cfg.ExternalIdentities, "sso not configured") {
		return
	}
	user, ok := h.adminTargetUser(w, r)
	if !ok {
		return
	}
	links, err := h.cfg.ExternalIdentities.ListUserIdentities(r.Context(), user.ID)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_list_identities", "user_id", user.ID)
		return
	}
	out := AdminExternalIdentitiesResponse{Identities: make([]AdminExternalIdentity, 0, len(links))}
	for _, l := range links {
		item := AdminExternalIdentity{
			ID: l.ID, Issuer: l.Issuer, Subject: l.Subject,
			LastSeenEmail: l.LastSeenEmail, LastSeenName: l.LastSeenName, CreatedAt: l.CreatedAt,
		}
		if !l.LastLoginAt.IsZero() {
			last := l.LastLoginAt
			item.LastLoginAt = &last
		}
		out.Identities = append(out.Identities, item)
	}
	httputil.WriteJSON(w, http.StatusOK, out)
}

// unlinkAdminUserIdentityHandler serves
// DELETE /api/admin/users/{user_id}/identities/{identity_id}.
//
// The store enforces the "only while disabled" precondition and records the
// unlink in the same transaction as the deletion, with this administrator as the
// actor. The handler does not also best-effort record it: one atomic row is the
// authoritative account of who removed the binding.
func (h *Handler) unlinkAdminUserIdentityHandler(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.guard().SystemAdmin(w, r)
	if !ok {
		return
	}
	if !httputil.RequireStore(w, h.cfg.ExternalIdentities, "sso not configured") {
		return
	}
	user, ok := h.adminTargetUser(w, r)
	if !ok {
		return
	}
	identityID, ok := httputil.PathValue(w, r, "identity_id")
	if !ok {
		return
	}
	err := h.cfg.ExternalIdentities.UnlinkIdentity(r.Context(), coreidentity.UnlinkIdentity{
		UserID: user.ID, IdentityID: identityID, ActorID: actorID,
	})
	switch {
	case errors.Is(err, coreidentity.ErrIdentityNotFound):
		httputil.WriteJSONError(w, http.StatusNotFound, "no such identity for this account")
	case errors.Is(err, coreidentity.ErrUnlinkRequiresDisabled):
		httputil.WriteJSONError(w, http.StatusConflict, "disable the account before unlinking its identity")
	case err != nil:
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_unlink_identity", "user_id", user.ID)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
