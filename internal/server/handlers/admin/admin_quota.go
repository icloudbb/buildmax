package admin

import (
	"net/http"

	corequota "github.com/icloudbb/buildmax/internal/core/quota"
	"github.com/icloudbb/buildmax/internal/server/httputil"
)

// AdminQuotaTiersResponse is every tier a space can be assigned to.
type AdminQuotaTiersResponse struct {
	Tiers []corequota.Tier `json:"tiers"`
}

// setQuotaTierRequest is the body of PUT /api/admin/spaces/{space_id}/quota-tier.
type setQuotaTierRequest struct {
	Tier string `json:"tier"`
}

// listAdminQuotaTiersHandler serves GET /api/admin/quota-tiers.
func (h *Handler) listAdminQuotaTiersHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.guard().SystemAdmin(w, r); !ok {
		return
	}
	if h.cfg.Quota == nil {
		httputil.WriteJSONError(w, http.StatusServiceUnavailable, "quota not configured")
		return
	}
	tiers, err := h.cfg.Quota.ListTiers(r.Context())
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_list_quota_tiers")
		return
	}
	if tiers == nil {
		tiers = []corequota.Tier{}
	}
	httputil.WriteJSON(w, http.StatusOK, AdminQuotaTiersResponse{Tiers: tiers})
}

// setAdminSpaceQuotaTierHandler serves PUT /api/admin/spaces/{space_id}/quota-tier.
//
// Personal Spaces are included: each is quota-checked like any other Space,
// and raising one person's capacity is exactly a personal Space's tier.
func (h *Handler) setAdminSpaceQuotaTierHandler(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.guard().SystemAdmin(w, r)
	if !ok {
		return
	}
	if h.cfg.Quota == nil {
		httputil.WriteJSONError(w, http.StatusServiceUnavailable, "quota not configured")
		return
	}
	spaceID, ok := httputil.PathValue(w, r, "space_id")
	if !ok {
		return
	}
	var req setQuotaTierRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	if _, err := h.cfg.Quota.AssignTier(r.Context(), actorID, spaceID, req.Tier); err != nil {
		if httputil.WriteServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_set_space_quota_tier", "space_id", spaceID)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
