package admin

import (
	"net/http"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	"github.com/icloudbb/buildmax/internal/server/httputil"
)

// AdminMeResponse describes the caller's deployment-scoped authority, for an
// administrator's own view of it inside the administration area.
//
// Whether that area exists for a person is not asked here: Portal reads it from
// the system roles its session carries, so the many accounts without a grant
// never meet this route's 403.
type AdminMeResponse struct {
	UserID string   `json:"user_id"`
	Roles  []string `json:"roles"`
	// Grants are the caller's own active grants, so the area can show when the
	// authority was given and by whom. Other people's grants are not here;
	// that is GET /api/admin/grants.
	Grants []coreidentity.SystemGrant `json:"grants"`
}

// adminMeHandler serves GET /api/admin/me.
func (h *Handler) adminMeHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.guard().SystemAdmin(w, r)
	if !ok {
		return
	}
	roles, err := h.cfg.Grants.ActiveSystemRoles(r.Context(), userID)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_me", "user_id", userID)
		return
	}
	// Filtering the full active list in memory is fine here and is expected to
	// stay fine: the set of people who can operate a deployment is small by
	// construction, and a per-user query would be a store method with one
	// caller.
	all, err := h.cfg.Grants.ListSystemGrants(r.Context(), false)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_me", "user_id", userID)
		return
	}
	mine := make([]coreidentity.SystemGrant, 0, 1)
	for _, g := range all {
		if g.UserID == userID {
			mine = append(mine, g)
		}
	}
	httputil.WriteJSON(w, http.StatusOK, AdminMeResponse{UserID: userID, Roles: roles, Grants: mine})
}
