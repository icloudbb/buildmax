package space

import (
	"net/http"
	"time"

	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	"github.com/icloudbb/buildmax/internal/server/httputil"
	spacesvc "github.com/icloudbb/buildmax/internal/service/space"
)

// serviceAccountResponse is a Space-owned service account. It carries no email
// and no credential, because it has neither. See
// docs/design/space-assistants.md §6.
type serviceAccountResponse struct {
	ID            string     `json:"id"`
	SpaceID       string     `json:"space_id"`
	Name          string     `json:"name"`
	SponsorUserID *string    `json:"sponsor_user_id,omitempty"`
	NeedsSponsor  bool       `json:"needs_sponsor"`
	DisabledAt    *time.Time `json:"disabled_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

type createServiceAccountRequest struct {
	Name          string `json:"name"`
	SponsorUserID string `json:"sponsor_user_id,omitempty"`
}

type updateServiceAccountRequest struct {
	Name          *string `json:"name,omitempty"`
	SponsorUserID *string `json:"sponsor_user_id,omitempty"`
}

type setServiceAccountStateRequest struct {
	Disabled bool `json:"disabled"`
}

func serviceAccountToResponse(a spacesvc.ServiceAccount) serviceAccountResponse {
	return serviceAccountResponse{
		ID:            a.User.ID,
		SpaceID:       a.SpaceID,
		Name:          a.User.Name,
		SponsorUserID: a.User.SponsorUserID,
		NeedsSponsor:  a.NeedsSponsor,
		DisabledAt:    a.User.DisabledAt,
		CreatedAt:     a.User.CreatedAt,
	}
}

// serviceAccountPath authenticates the caller, resolves the path's Space, and
// refuses a caller who is not a member of it.
func (h *Handler) serviceAccountPath(w http.ResponseWriter, r *http.Request) (userID, spaceID string, ok bool) {
	userID, ok = h.guard().UserAndStore(w, r, h.cfg.ServiceAccounts, "service accounts not configured")
	if !ok {
		return "", "", false
	}
	spaceID, ok = httputil.PathValue(w, r, "space_id")
	if !ok {
		return "", "", false
	}
	if _, resolved, ok := h.guard().ExplicitSpace(w, r, userID, spaceID); !ok || resolved == "" {
		return "", "", false
	}
	return userID, spaceID, true
}

func (h *Handler) writeServiceAccountError(w http.ResponseWriter, err error, handler, userID, spaceID string) {
	if httputil.WriteServiceError(w, err) {
		return
	}
	httputil.WriteInternalError(w, err, "handler error", "handler", handler, "user_id", userID, "space_id", spaceID)
}

// listServiceAccountsHandler serves GET /api/spaces/{space_id}/service-accounts.
func (h *Handler) listServiceAccountsHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.serviceAccountPath(w, r)
	if !ok {
		return
	}
	accounts, err := h.spaceService().ListServiceAccounts(r.Context(), spaceID)
	if err != nil {
		h.writeServiceAccountError(w, err, "list_service_accounts", userID, spaceID)
		return
	}
	out := make([]serviceAccountResponse, len(accounts))
	for i := range accounts {
		out[i] = serviceAccountToResponse(accounts[i])
	}
	httputil.WriteJSON(w, http.StatusOK, out)
}

// createServiceAccountHandler serves POST /api/spaces/{space_id}/service-accounts.
func (h *Handler) createServiceAccountHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.serviceAccountPath(w, r)
	if !ok {
		return
	}
	var req createServiceAccountRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	account, err := h.spaceService().CreateServiceAccount(r.Context(), spacesvc.CreateServiceAccountCmd{
		SpaceID: spaceID, ActorID: userID, Name: req.Name, SponsorUserID: req.SponsorUserID,
	})
	if err != nil {
		h.writeServiceAccountError(w, err, "create_service_account", userID, spaceID)
		return
	}
	h.cfg.Audit.UserAction(r.Context(), userID, spaceID, coreaudit.ServiceAccountCreated, "user", account.User.ID, account.User.Name)
	httputil.WriteJSON(w, http.StatusCreated, serviceAccountToResponse(*account))
}

// updateServiceAccountHandler serves
// PATCH /api/spaces/{space_id}/service-accounts/{user_id}: rename, re-sponsor,
// or both, each audited as its own action.
func (h *Handler) updateServiceAccountHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.serviceAccountPath(w, r)
	if !ok {
		return
	}
	targetID, ok := httputil.PathValue(w, r, "user_id")
	if !ok {
		return
	}
	var req updateServiceAccountRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	account, err := h.spaceService().UpdateServiceAccount(r.Context(), spacesvc.UpdateServiceAccountCmd{
		SpaceID: spaceID, ActorID: userID, UserID: targetID, Name: req.Name, SponsorUserID: req.SponsorUserID,
	})
	if err != nil {
		h.writeServiceAccountError(w, err, "update_service_account", userID, spaceID)
		return
	}
	if req.Name != nil {
		h.cfg.Audit.UserAction(r.Context(), userID, spaceID, coreaudit.ServiceAccountRenamed, "user", targetID, account.User.Name)
	}
	if req.SponsorUserID != nil {
		// The detail is the new sponsor's id, so the trail answers who was
		// accountable when.
		sponsor := ""
		if account.User.SponsorUserID != nil {
			sponsor = *account.User.SponsorUserID
		}
		h.cfg.Audit.UserAction(r.Context(), userID, spaceID, coreaudit.ServiceAccountSponsorChanged, "user", targetID, sponsor)
	}
	httputil.WriteJSON(w, http.StatusOK, serviceAccountToResponse(*account))
}

// setServiceAccountStateHandler serves
// PUT /api/spaces/{space_id}/service-accounts/{user_id}/state.
func (h *Handler) setServiceAccountStateHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.serviceAccountPath(w, r)
	if !ok {
		return
	}
	targetID, ok := httputil.PathValue(w, r, "user_id")
	if !ok {
		return
	}
	var req setServiceAccountStateRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	account, err := h.spaceService().SetServiceAccountState(r.Context(), spacesvc.SetServiceAccountStateCmd{
		SpaceID: spaceID, ActorID: userID, UserID: targetID, Disabled: req.Disabled,
	})
	if err != nil {
		h.writeServiceAccountError(w, err, "set_service_account_state", userID, spaceID)
		return
	}
	action := coreaudit.ServiceAccountEnabled
	if req.Disabled {
		action = coreaudit.ServiceAccountDisabled
	}
	h.cfg.Audit.UserAction(r.Context(), userID, spaceID, action, "user", targetID, "")
	httputil.WriteJSON(w, http.StatusOK, serviceAccountToResponse(*account))
}
