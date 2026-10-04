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
	// Delivery sends each firing's releasable result to one person through a
	// Space Assistant's bot.
	Delivery  *coreschedule.Delivery `json:"delivery,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
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
	// Delivery is optional; setting one needs the right to manage Assistants.
	Delivery *coreschedule.Delivery `json:"delivery,omitempty"`
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
	// Delivery sets the delivery target; {"assistant_id": ""} removes it.
	Delivery *coreschedule.Delivery `json:"delivery,omitempty"`
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
		Delivery:            s.Delivery,
		CreatedAt:           s.CreatedAt,
		UpdatedAt:           s.UpdatedAt,
	}
}

func newScheduleService(cfg Config) *schedulesvc.Service {
	if cfg.Schedules == nil {
		return nil
	}
	svc := &schedulesvc.Service{Schedules: cfg.Schedules, Agents: cfg.Agents, Workflows: cfg.Workflows}
	if cfg.AssistantFrontDoor != nil {
		svc.Deliveries = cfg.AssistantFrontDoor
	}
	return svc
}

// requireAssistantsForDelivery guards a schedule that sends to a person through
// a Space Assistant: choosing the target, and editing such a schedule's input,
// shape what the Space's bot says to someone, which is managing Assistants.
func (h *Handler) requireAssistantsForDelivery(w http.ResponseWriter, r *http.Request, userID, spaceID string) bool {
	_, ok := h.guard().SpaceAction(w, r, userID, spaceID, corespace.ActionManageAssistants)
	return ok
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
	if req.Delivery != nil && !h.requireAssistantsForDelivery(w, r, userID, spaceID) {
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
		Delivery:     req.Delivery,
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
	existing, err := h.scheduleService().Get(r.Context(), spaceID, scheduleID)
	if err != nil {
		if h.writeScheduleSvcError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "patch_schedule", "schedule_id", scheduleID)
		return
	}
	// Pausing or resuming changes nothing the bot says, so a member may still
	// do it on a delivering schedule.
	editsDelivery := req.Delivery != nil || (existing.Delivery != nil &&
		(req.Name != nil || req.Input != nil || req.CronExpr != nil || req.Timezone != nil))
	if editsDelivery && !h.requireAssistantsForDelivery(w, r, userID, spaceID) {
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
		Delivery:   req.Delivery,
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

type scheduleDeliveryListResponse struct {
	Deliveries []coreschedule.FireDelivery `json:"deliveries"`
	Total      int                         `json:"total"`
}

// listScheduleDeliveriesHandler serves GET
// /api/spaces/{space_id}/schedules/{schedule_id}/deliveries: how each firing's
// result was delivered, or why it was not.
func (h *Handler) listScheduleDeliveriesHandler(w http.ResponseWriter, r *http.Request) {
	_, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Schedules, "schedules not configured")
	if !ok {
		return
	}
	scheduleID, ok := httputil.PathValue(w, r, "schedule_id")
	if !ok {
		return
	}
	limit, offset := httputil.LimitOffset(r.URL.Query(), "limit", "offset", httputil.BrowsePageDefault, httputil.BrowsePageMax)
	list, total, err := h.scheduleService().ListDeliveries(r.Context(), spaceID, scheduleID, limit, offset)
	if err != nil {
		if h.writeScheduleSvcError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "list_schedule_deliveries", "schedule_id", scheduleID)
		return
	}
	if list == nil {
		list = []coreschedule.FireDelivery{}
	}
	httputil.WriteJSON(w, http.StatusOK, scheduleDeliveryListResponse{Deliveries: list, Total: total})
}
