package space

import (
	"net/http"
	"time"

	coreschedule "github.com/icloudbb/buildmax/internal/core/schedule"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/server/httputil"
	schedulesvc "github.com/icloudbb/buildmax/internal/service/schedule"
)

// ScheduleResponse is the wire shape of a recurring schedule.
type ScheduleResponse struct {
	ID           string `json:"id"`
	SpaceID      string `json:"space_id"`
	ExecutorKind string `json:"executor_kind"`
	ExecutorID   string `json:"executor_id"`
	CreatedBy    string `json:"created_by"`
	Name         string `json:"name,omitempty"`
	Input        string `json:"input"`
	CronExpr     string `json:"cron_expr"`
	Timezone     string `json:"timezone"`
	Enabled      bool   `json:"enabled"`
	// PauseReason tells a paused schedule apart -- a person paused it, or the
	// system did after failures, a lost creator, or a broken cron. Empty when enabled.
	PauseReason         string     `json:"pause_reason,omitempty"`
	NextFireAt          time.Time  `json:"next_fire_at"`
	LastFireAt          *time.Time `json:"last_fire_at,omitempty"`
	LastFireRef         *string    `json:"last_fire_ref,omitempty"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type scheduleListResponse struct {
	Schedules []ScheduleResponse `json:"schedules"`
	Total     int                `json:"total"`
}

type createScheduleRequest struct {
	// ExecutorKind and ExecutorID name what the schedule fires: an Agent or a
	// published Workflow in this Space. The executor is fixed at creation.
	ExecutorKind string `json:"executor_kind"`
	ExecutorID   string `json:"executor_id"`
	Name         string `json:"name,omitempty"`
	Input        string `json:"input"`
	CronExpr     string `json:"cron_expr"`
	Timezone     string `json:"timezone"`
}

// patchScheduleRequest is a partial update: a nil field is left unchanged. This
// differs from the agent patch, which replaces the whole definition, because a
// schedule's fields are independent — pausing one should not require restating
// its cron expression.
type patchScheduleRequest struct {
	Name     *string `json:"name,omitempty"`
	Input    *string `json:"input,omitempty"`
	CronExpr *string `json:"cron_expr,omitempty"`
	Timezone *string `json:"timezone,omitempty"`
	Enabled  *bool   `json:"enabled,omitempty"`
}

func scheduleToResponse(s coreschedule.Schedule) ScheduleResponse {
	return ScheduleResponse{
		ID:                  s.ID,
		SpaceID:             s.SpaceID,
		ExecutorKind:        s.ExecutorKind,
		ExecutorID:          s.ExecutorID,
		CreatedBy:           s.CreatedBy,
		Name:                s.Name,
		Input:               s.Input,
		CronExpr:            s.CronExpr,
		Timezone:            s.Timezone,
		Enabled:             s.Enabled,
		PauseReason:         s.PauseReason,
		NextFireAt:          s.NextFireAt,
		LastFireAt:          s.LastFireAt,
		LastFireRef:         s.LastFireRef,
		ConsecutiveFailures: s.ConsecutiveFailures,
		CreatedAt:           s.CreatedAt,
		UpdatedAt:           s.UpdatedAt,
	}
}

func newScheduleService(cfg Config) *schedulesvc.Service {
	if cfg.Schedules == nil {
		return nil
	}
	return &schedulesvc.Service{Schedules: cfg.Schedules, Agents: cfg.Agents, Workflows: cfg.Workflows}
}

func (h *Handler) scheduleService() *schedulesvc.Service { return h.schedules }

// writeScheduleSvcError maps a schedule write's refusals (invalid cron, unknown
// agent, not found) to their status; other errors fall through to a 500.
func (h *Handler) writeScheduleSvcError(w http.ResponseWriter, err error) bool {
	return httputil.WriteServiceError(w, err)
}

func (h *Handler) listSchedulesHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Schedules, "schedules not configured")
	if !ok {
		return
	}
	limit, offset := httputil.LimitOffset(r.URL.Query(), "limit", "offset", httputil.BrowsePageDefault, httputil.BrowsePageMax)
	list, total, err := h.scheduleService().List(r.Context(), spaceID, limit, offset)
	if err != nil {
		if h.writeScheduleSvcError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "list_schedules", "user_id", userID, "space_id", spaceID)
		return
	}
	out := make([]ScheduleResponse, len(list))
	for i := range list {
		out[i] = scheduleToResponse(list[i])
	}
	httputil.WriteJSON(w, http.StatusOK, scheduleListResponse{Schedules: out, Total: total})
}

func (h *Handler) createScheduleHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Schedules, "schedules not configured")
	if !ok {
		return
	}
	if _, ok := h.guard().SpaceAction(w, r, userID, spaceID, corespace.ActionManageSchedules); !ok {
		return
	}
	var req createScheduleRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	created, err := h.scheduleService().Create(r.Context(), schedulesvc.CreateCmd{
		SpaceID:      spaceID,
		UserID:       userID,
		ExecutorKind: req.ExecutorKind,
		ExecutorID:   req.ExecutorID,
		Name:         req.Name,
		Input:        req.Input,
		CronExpr:     req.CronExpr,
		Timezone:     req.Timezone,
	})
	if err != nil {
		if h.writeScheduleSvcError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "create_schedule", "user_id", userID, "space_id", spaceID)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, scheduleToResponse(*created))
}

func (h *Handler) getScheduleHandler(w http.ResponseWriter, r *http.Request) {
	_, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Schedules, "schedules not configured")
	if !ok {
		return
	}
	scheduleID, ok := httputil.PathValue(w, r, "schedule_id")
	if !ok {
		return
	}
	found, err := h.scheduleService().Get(r.Context(), spaceID, scheduleID)
	if err != nil {
		if h.writeScheduleSvcError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "get_schedule", "schedule_id", scheduleID)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, scheduleToResponse(*found))
}

func (h *Handler) patchScheduleHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Schedules, "schedules not configured")
	if !ok {
		return
	}
	if _, ok := h.guard().SpaceAction(w, r, userID, spaceID, corespace.ActionManageSchedules); !ok {
		return
	}
	scheduleID, ok := httputil.PathValue(w, r, "schedule_id")
	if !ok {
		return
	}
	var req patchScheduleRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	updated, err := h.scheduleService().Update(r.Context(), schedulesvc.UpdateCmd{
		ScheduleID: scheduleID,
		SpaceID:    spaceID,
		Name:       req.Name,
		Input:      req.Input,
		CronExpr:   req.CronExpr,
		Timezone:   req.Timezone,
		Enabled:    req.Enabled,
	})
	if err != nil {
		if h.writeScheduleSvcError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "patch_schedule", "schedule_id", scheduleID)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, scheduleToResponse(*updated))
}

func (h *Handler) deleteScheduleHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Schedules, "schedules not configured")
	if !ok {
		return
	}
	if _, ok := h.guard().SpaceAction(w, r, userID, spaceID, corespace.ActionManageSchedules); !ok {
		return
	}
	scheduleID, ok := httputil.PathValue(w, r, "schedule_id")
	if !ok {
		return
	}
	if err := h.scheduleService().Delete(r.Context(), spaceID, scheduleID); err != nil {
		if h.writeScheduleSvcError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "delete_schedule", "schedule_id", scheduleID)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
