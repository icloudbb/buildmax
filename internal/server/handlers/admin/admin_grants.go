package admin

import (
	"errors"
	"net/http"

	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	"github.com/icloudbb/buildmax/internal/server/httputil"
	"github.com/icloudbb/buildmax/internal/service/systemadmin"
)

// AdminGrantsResponse lists who can operate the deployment.
type AdminGrantsResponse struct {
	Grants []AdminGrant `json:"grants"`
}

// AdminGrant is one grant with the account it names resolved, so a list is
// readable without a second call per row.
type AdminGrant struct {
	coreidentity.SystemGrant
	Email string `json:"email,omitempty"`
}

// AdminGrantRequest is the body for POST /api/admin/grants.
type AdminGrantRequest struct {
	UserID string `json:"user_id"`
	// Role is optional and defaults to system_admin, which is the only role
	// this build accepts. It is in the body so that adding a second role later
	// is not a new route.
	Role string `json:"role,omitempty"`
}

// listAdminGrantsHandler serves GET /api/admin/grants.
func (h *Handler) listAdminGrantsHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.guard().SystemAdmin(w, r); !ok {
		return
	}
	includeRevoked := r.URL.Query().Get("include_revoked") == "true"
	grants, err := h.cfg.Grants.ListSystemGrants(r.Context(), includeRevoked)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_list_grants")
		return
	}
	out := make([]AdminGrant, 0, len(grants))
	for _, g := range grants {
		row := AdminGrant{SystemGrant: g}
		if h.cfg.Users != nil {
			// A grant outliving the account it names is not expected. Showing
			// the row without an email beats refusing to list authority.
			if user, err := h.cfg.Users.GetUser(r.Context(), g.UserID); err == nil && user != nil {
				row.Email = user.Email
			}
		}
		out = append(out, row)
	}
	httputil.WriteJSON(w, http.StatusOK, AdminGrantsResponse{Grants: out})
}

// createAdminGrantHandler serves POST /api/admin/grants.
func (h *Handler) createAdminGrantHandler(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.guard().SystemAdmin(w, r)
	if !ok {
		return
	}
	if !httputil.RequireStore(w, h.cfg.Users, "accounts not configured") {
		return
	}
	var req AdminGrantRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	if req.UserID == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "user_id required")
		return
	}
	role := req.Role
	if role == "" {
		role = coreidentity.SystemRoleAdmin
	}
	// Granting does not create an account, for the same reason the operator
	// command does not: creating an account and minting deployment authority
	// are two decisions. The service refuses an id nobody holds.
	svc := &systemadmin.Service{Grants: h.cfg.Grants, Users: h.cfg.Users, Audit: h.cfg.Audit}
	grant, err := svc.Grant(r.Context(), req.UserID, role, coreaudit.UserActor(actorID))
	if err != nil {
		if httputil.WriteServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_create_grant", "user_id", req.UserID)
		return
	}
	// Read for the response, not for the rule: the service already refused an
	// id nobody holds, and this route answers with the address so a caller does
	// not have to look it up to show what it just did.
	email := ""
	if user, err := h.cfg.Users.GetUser(r.Context(), req.UserID); err == nil && user != nil {
		email = user.Email
	}
	httputil.WriteJSON(w, http.StatusCreated, AdminGrant{SystemGrant: *grant, Email: email})
}

// deleteAdminGrantHandler serves DELETE /api/admin/grants/{user_id}.
//
// It refuses to revoke the last active grant. The operator command allows it,
// because that command is the recovery path and its caller already holds the
// database credentials — see docs/design/system-administration.md section 6.
// Nobody should be able to leave a deployment unadministerable by clicking.
func (h *Handler) deleteAdminGrantHandler(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.guard().SystemAdmin(w, r)
	if !ok {
		return
	}
	userID, ok := httputil.PathValue(w, r, "user_id")
	if !ok {
		return
	}
	role := r.URL.Query().Get("role")
	if role == "" {
		role = coreidentity.SystemRoleAdmin
	}

	svc := &systemadmin.Service{Grants: h.cfg.Grants, Users: h.cfg.Users, Audit: h.cfg.Audit}
	if err := svc.Revoke(r.Context(), userID, role, coreaudit.UserActor(actorID)); err != nil {
		if errors.Is(err, systemadmin.ErrLastHolder) {
			httputil.WriteJSONError(w, http.StatusConflict,
				"this is the deployment's last "+role+"; revoke it with `buildmax-server admin revoke <email>` if that is the intent")
			return
		}
		if httputil.WriteServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_delete_grant", "user_id", userID)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
