package admin

import (
	"net/http"
	"time"

	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/server/httputil"
)

// AdminSpaceAttention is one Space with active runs, Workflow requests waiting
// on its members, or recent failures. It is metadata an operator needs to find
// who can act — never a run's input, output, error text, a request's prompt or
// answer, or anything else the Space's members wrote.
type AdminSpaceAttention struct {
	SpaceID  string             `json:"space_id"`
	Name     string             `json:"name"`
	Personal bool               `json:"personal"`
	Owners   []AdminSpaceMember `json:"owners"`
	Active   map[string]int     `json:"active"`
	// OldestActiveAt is when the Space's oldest PENDING, SCHEDULED, or RUNNING
	// run was created.
	OldestActiveAt *time.Time     `json:"oldest_active_at,omitempty"`
	Failures       map[string]int `json:"failures"`
	adminWorkflowRuntime
	// The ids let an operator name the run to the Space's members, who alone
	// can open it.
	OldestWaitingWorkflowRunID string `json:"oldest_waiting_workflow_run_id,omitempty"`
	LatestFailedWorkflowRunID  string `json:"latest_failed_workflow_run_id,omitempty"`
}

// AdminSpacesAttentionResponse is a page of Spaces needing attention.
type AdminSpacesAttentionResponse struct {
	Spaces             []AdminSpaceAttention `json:"spaces"`
	Total              int                   `json:"total"`
	FailureWindowHours int                   `json:"failure_window_hours"`
}

// listRuntimeSpacesHandler serves GET /api/admin/runtime/spaces: the Spaces,
// team and personal, that have active runs, pending Workflow requests, or
// failures in the window, with their owners. Any member may answer a request;
// the owners are who an operator contacts. Personal Spaces are included because a person working alone
// can hit a platform fault as easily as a team.
func (h *Handler) listRuntimeSpacesHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.guard().SystemAdmin(w, r); !ok {
		return
	}
	if !httputil.RequireStore(w, h.cfg.TaskRuns, "task runs not configured") ||
		!httputil.RequireStore(w, h.cfg.Spaces, "spaces not configured") {
		return
	}
	limit, offset := httputil.LimitOffset(r.URL.Query(), "limit", "offset", httputil.BulkPageDefault, httputil.BulkPageMax)
	since := time.Now().UTC().Add(-runtimeFailureWindow)
	activity, total, err := h.cfg.TaskRuns.ListSpaceRunActivity(r.Context(), since, limit, offset)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_runtime_spaces")
		return
	}

	out := make([]AdminSpaceAttention, 0, len(activity))
	for _, a := range activity {
		row := AdminSpaceAttention{
			SpaceID:                    a.SpaceID,
			Owners:                     []AdminSpaceMember{},
			Active:                     a.Active,
			OldestActiveAt:             a.OldestActiveAt,
			Failures:                   a.FailuresByClass,
			adminWorkflowRuntime:       toAdminWorkflowRuntime(a.WorkflowRuntime),
			OldestWaitingWorkflowRunID: a.OldestWaitingWorkflowRunID,
			LatestFailedWorkflowRunID:  a.LatestFailedWorkflowRunID,
		}
		// A Space that cannot be read is still listed by id: the operator's
		// question is that it needs attention, which the counts already answer.
		if space, err := h.cfg.Spaces.GetSpace(r.Context(), a.SpaceID); err == nil && space != nil {
			row.Name = space.Name
			row.Personal = space.PersonalForUserID != nil
		}
		if members, err := h.cfg.Spaces.ListSpaceMembers(r.Context(), a.SpaceID); err == nil {
			for _, m := range members {
				if m.Role != corespace.RoleOwner {
					continue
				}
				owner := AdminSpaceMember{UserID: m.UserID, Role: m.Role}
				if h.cfg.Users != nil {
					if user, err := h.cfg.Users.GetUser(r.Context(), m.UserID); err == nil && user != nil {
						owner.Email = user.Email
					}
				}
				row.Owners = append(row.Owners, owner)
			}
		}
		out = append(out, row)
	}
	httputil.WriteJSON(w, http.StatusOK, AdminSpacesAttentionResponse{
		Spaces:             out,
		Total:              total,
		FailureWindowHours: int(runtimeFailureWindow / time.Hour),
	})
}
