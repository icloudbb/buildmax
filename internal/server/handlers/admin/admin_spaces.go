package admin

import (
	"net/http"
	"strings"
	"time"

	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	"github.com/icloudbb/buildmax/internal/server/httputil"
	"github.com/icloudbb/buildmax/internal/service/spacerecovery"
)

// AdminSpace is one space as an administrator sees it: metadata only.
//
// There is deliberately no field here that a space member wrote or an agent
// produced. The rule the design states — if a member wrote it or an agent
// produced it, it is content — is what keeps this struct from growing an
// "issue count" that turns into an issue list that turns into issue titles.
type AdminSpace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Personal marks a user's own space rather than a collaborative space.
	Personal    bool      `json:"personal"`
	QuotaTier   string    `json:"quota_tier,omitempty"`
	MemberCount int       `json:"member_count"`
	CreatedBy   string    `json:"created_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// AdminSpacesResponse is a page of spaces.
type AdminSpacesResponse struct {
	Spaces []AdminSpace `json:"spaces"`
	Total  int          `json:"total"`
}

// AdminSpaceDetail adds the membership and the capacity the space is using.
type AdminSpaceDetail struct {
	AdminSpace
	Members []AdminSpaceMember `json:"members"`
	// Usage is nil when the deployment reports no quota, so a reader can tell
	// "no limits configured" from "using nothing".
	Usage *spaceUsage `json:"usage,omitempty"`
}

// AdminSpaceMember names one member and their role.
type AdminSpaceMember struct {
	UserID string `json:"user_id"`
	Email  string `json:"email,omitempty"`
	Role   string `json:"role"`
}

// listAdminSpacesHandler serves GET /api/admin/spaces.
// spaceUsage is this surface's view of a space's consumption. The space's own
// /api/spaces/{id}/usage answers a different question to a different caller, so
// each endpoint owns its shape rather than sharing one that must serve both.
type spaceUsage struct {
	RunCount           int    `json:"run_count"`
	TotalTokens        int    `json:"total_tokens"`
	TierName           string `json:"tier"`
	PeriodDays         int    `json:"period_days"`
	MaxRunsPerPeriod   *int   `json:"max_runs_per_period,omitempty"`
	MaxTokensPerPeriod *int   `json:"max_tokens_per_period,omitempty"`
}

func (h *Handler) listAdminSpacesHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.guard().SystemAdmin(w, r); !ok {
		return
	}
	if !httputil.RequireStore(w, h.cfg.Spaces, "spaces not configured") {
		return
	}
	limit, offset := httputil.LimitOffset(r.URL.Query(), "limit", "offset", httputil.BulkPageDefault, httputil.BulkPageMax)
	spaces, total, err := h.cfg.Spaces.ListAllSpaces(r.Context(), strings.TrimSpace(r.URL.Query().Get("q")), limit, offset)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_list_spaces")
		return
	}
	ids := make([]string, 0, len(spaces))
	for _, space := range spaces {
		ids = append(ids, space.ID)
	}
	counts, err := h.cfg.Spaces.CountSpaceMembers(r.Context(), ids)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_list_spaces", "counts")
		return
	}

	out := make([]AdminSpace, 0, len(spaces))
	for _, space := range spaces {
		out = append(out, AdminSpace{
			ID:          space.ID,
			Name:        space.Name,
			Personal:    space.PersonalForUserID != nil,
			QuotaTier:   space.QuotaTier,
			MemberCount: counts[space.ID],
			CreatedBy:   space.CreatedBy,
			CreatedAt:   space.CreatedAt,
		})
	}
	httputil.WriteJSON(w, http.StatusOK, AdminSpacesResponse{Spaces: out, Total: total})
}

// recoverSpaceOwnerRequest is the body of PUT /api/admin/spaces/{space_id}/owner.
type recoverSpaceOwnerRequest struct {
	SuccessorID string `json:"successor_id"`
}

// recoverSpaceOwnershipHandler serves PUT /api/admin/spaces/{space_id}/owner:
// the disabled-owner-only recovery that promotes an enabled member to owner. It
// is metadata-only — the operator names a successor and never reads the Space's
// contents.
func (h *Handler) recoverSpaceOwnershipHandler(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.guard().SystemAdmin(w, r)
	if !ok {
		return
	}
	if h.cfg.SpaceRecovery == nil {
		httputil.WriteJSONError(w, http.StatusServiceUnavailable, "space recovery not configured")
		return
	}
	spaceID, ok := httputil.PathValue(w, r, "space_id")
	if !ok {
		return
	}
	var req recoverSpaceOwnerRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	successor := strings.TrimSpace(req.SuccessorID)
	demotedOwner, err := h.cfg.SpaceRecovery.RecoverOwnership(r.Context(), spacerecovery.RecoverCmd{
		SpaceID:     spaceID,
		SuccessorID: successor,
	})
	if err != nil {
		if httputil.WriteServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_recover_space_ownership", "space_id", spaceID)
		return
	}
	if h.cfg.Audit != nil {
		// Actor is the administrator, target the successor, detail the disabled
		// owner that was demoted — the trail names all three.
		h.cfg.Audit.UserAction(r.Context(), actorID, spaceID, coreaudit.SpaceOwnershipRecovered, "user", successor, demotedOwner)
	}
	w.WriteHeader(http.StatusNoContent)
}

// getAdminSpaceHandler serves GET /api/admin/spaces/{space_id}.
func (h *Handler) getAdminSpaceHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.guard().SystemAdmin(w, r); !ok {
		return
	}
	if !httputil.RequireStore(w, h.cfg.Spaces, "spaces not configured") {
		return
	}
	spaceID, ok := httputil.PathValue(w, r, "space_id")
	if !ok {
		return
	}
	space, err := h.cfg.Spaces.GetSpace(r.Context(), spaceID)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_get_space", "space_id", spaceID)
		return
	}
	if space == nil {
		httputil.WriteJSONError(w, http.StatusNotFound, "space not found")
		return
	}
	members, err := h.cfg.Spaces.ListSpaceMembers(r.Context(), spaceID)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_get_space", "members")
		return
	}

	detail := AdminSpaceDetail{
		AdminSpace: AdminSpace{
			ID:          space.ID,
			Name:        space.Name,
			Personal:    space.PersonalForUserID != nil,
			QuotaTier:   space.QuotaTier,
			MemberCount: len(members),
			CreatedBy:   space.CreatedBy,
			CreatedAt:   space.CreatedAt,
		},
		Members: make([]AdminSpaceMember, 0, len(members)),
	}
	for _, member := range members {
		row := AdminSpaceMember{UserID: member.UserID, Role: member.Role}
		if h.cfg.Users != nil {
			// A membership naming an account that is gone is not expected.
			// Showing the user id beats refusing to describe the space.
			if user, err := h.cfg.Users.GetUser(r.Context(), member.UserID); err == nil && user != nil {
				row.Email = user.Email
			}
		}
		detail.Members = append(detail.Members, row)
	}

	// Usage is what makes this page answer an operator's question rather than
	// just listing rows. It is a count of runs and tokens — capacity, not
	// content.
	if h.cfg.Quota != nil {
		if info, err := h.cfg.Quota.GetUsage(r.Context(), spaceID); err == nil {
			detail.Usage = &spaceUsage{
				RunCount:           info.RunCount,
				TotalTokens:        info.TotalTokens,
				TierName:           info.TierName,
				PeriodDays:         info.PeriodDays,
				MaxRunsPerPeriod:   info.MaxRunsPerPeriod,
				MaxTokensPerPeriod: info.MaxTokensPerPeriod,
			}
		}
	}
	httputil.WriteJSON(w, http.StatusOK, detail)
}
