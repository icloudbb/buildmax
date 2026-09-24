package space

import (
	"net/http"
	"time"

	"github.com/icloudbb/buildmax/internal/server/handlers/llmcallview"
	"github.com/icloudbb/buildmax/internal/server/httputil"
)

type usageResponse struct {
	RunCount           int    `json:"run_count"`
	TotalTokens        int    `json:"total_tokens"`
	TierName           string `json:"tier"`
	PeriodDays         int    `json:"period_days"`
	MaxRunsPerPeriod   *int   `json:"max_runs_per_period,omitempty"`
	MaxTokensPerPeriod *int   `json:"max_tokens_per_period,omitempty"`
	// StorageBytes is what the space holds now, not a windowed total, and is
	// absent on a deployment with no artifact storage to read.
	StorageBytes    *int64 `json:"storage_bytes,omitempty"`
	MaxStorageBytes *int64 `json:"max_storage_bytes,omitempty"`
	// ManagedCalls is the caller's own managed calls from signed-in CLI and
	// Desktop sessions over the same period. They belong to no space, so the
	// space figures above cannot include them; without this a signed-in user
	// has nowhere to see what those sessions cost. Present only on the
	// personal route, and only on a deployment with a call ledger.
	ManagedCalls *llmcallview.Totals `json:"managed_calls,omitempty"`
}

// usageHandler serves GET /api/usage, a live convenience alias that reports
// usage for the caller's personal space. Portal calls it for the common
// single-space view so it need not resolve a space ID first; the space-scoped
// GET /api/spaces/{space_id}/usage answers for any space.
func (h *Handler) usageHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.guard().UserAndStore(w, r, h.cfg.Spaces, "spaces not configured")
	if !ok {
		return
	}
	if h.cfg.Spaces == nil {
		httputil.WriteJSONError(w, http.StatusServiceUnavailable, "spaces not configured")
		return
	}
	space, err := h.cfg.Spaces.GetPersonalSpaceByUser(r.Context(), userID)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "usage_get_personal_space", "user_id", userID)
		return
	}
	if space == nil {
		httputil.WriteJSONError(w, http.StatusNotFound, "personal space not found")
		return
	}
	resp, ok := h.usageForSpace(w, r, space.ID)
	if !ok {
		return
	}
	if h.cfg.LLMCalls != nil {
		since := time.Now().UTC().AddDate(0, 0, -resp.PeriodDays)
		groups, err := h.cfg.LLMCalls.SummarizeForegroundLLMCalls(r.Context(), userID, since)
		if err != nil {
			httputil.WriteInternalError(w, err, "handler error", "handler", "usage_managed_calls", "user_id", userID)
			return
		}
		totals := llmcallview.PriceTotals(groups)
		resp.ManagedCalls = &totals
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) spaceUsageHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.guard().UserAndStore(w, r, h.cfg.Spaces, "spaces not configured")
	if !ok {
		return
	}
	spaceID, ok := httputil.PathValue(w, r, "space_id")
	if !ok {
		return
	}
	if _, resolvedSpaceID, ok := h.guard().ExplicitSpace(w, r, userID, spaceID); !ok || resolvedSpaceID == "" {
		return
	}
	if resp, ok := h.usageForSpace(w, r, spaceID); ok {
		httputil.WriteJSON(w, http.StatusOK, resp)
	}
}

func (h *Handler) usageForSpace(w http.ResponseWriter, r *http.Request, spaceID string) (usageResponse, bool) {
	if h.cfg.Quota == nil {
		httputil.WriteJSONError(w, http.StatusServiceUnavailable, "usage not available")
		return usageResponse{}, false
	}
	info, err := h.cfg.Quota.GetUsage(r.Context(), spaceID)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "usage", "space_id", spaceID)
		return usageResponse{}, false
	}
	return usageResponse{
		RunCount:           info.RunCount,
		TotalTokens:        info.TotalTokens,
		TierName:           info.TierName,
		PeriodDays:         info.PeriodDays,
		MaxRunsPerPeriod:   info.MaxRunsPerPeriod,
		MaxTokensPerPeriod: info.MaxTokensPerPeriod,
		StorageBytes:       info.StorageBytes,
		MaxStorageBytes:    info.MaxStorageBytes,
	}, true
}
