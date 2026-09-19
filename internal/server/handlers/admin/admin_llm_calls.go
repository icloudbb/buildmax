package admin

import (
	"net/http"
	"time"

	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
	"github.com/icloudbb/buildmax/internal/server/handlers/llmcallview"
	"github.com/icloudbb/buildmax/internal/server/httputil"
)

// AdminLLMCallsResponse is a page of the deployment-wide managed call ledger.
type AdminLLMCallsResponse struct {
	Calls []AdminLLMCall `json:"calls"`
	Total int            `json:"total"`
}

// AdminLLMCall is one ledger row as a deployment administrator sees it.
//
// It carries more than the space-scoped view because the reader is different:
// the space view hides target_id, provider_type, and upstream_model because an
// operator's routing behind a model name is not a caller's business. The
// operator reading this ledger is who decided that routing, so it is shown here.
//
// What stays absent is what the ledger never held: no prompts, no tool
// arguments, no generated content. Reading spend across every space must not
// become a way to read across them — run detail lives in durable local traces.
type AdminLLMCall struct {
	ID        string  `json:"id"`
	UserID    *string `json:"user_id,omitempty"`
	TaskID    *string `json:"task_id,omitempty"`
	TaskRunID *string `json:"task_run_id,omitempty"`
	Surface   string  `json:"surface,omitempty"`
	SessionID *string `json:"session_id,omitempty"`

	// Model is what the caller asked for; the three that follow are how the
	// deployment served it.
	Model         string `json:"model,omitempty"`
	TargetID      string `json:"target_id,omitempty"`
	ProviderType  string `json:"provider_type,omitempty"`
	UpstreamModel string `json:"upstream_model,omitempty"`
	Streaming     bool   `json:"streaming"`

	AcceptedAt   time.Time  `json:"accepted_at"`
	FirstDeltaAt *time.Time `json:"first_delta_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`

	Status     string  `json:"status"`
	ErrorClass *string `json:"error_class,omitempty"`
	Attempts   int     `json:"attempts,omitempty"`

	PromptTokens     *int   `json:"prompt_tokens,omitempty"`
	CompletionTokens *int   `json:"completion_tokens,omitempty"`
	TotalTokens      *int   `json:"total_tokens,omitempty"`
	CacheReadTokens  *int   `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *int   `json:"cache_write_tokens,omitempty"`
	UsageSource      string `json:"usage_source,omitempty"`

	Cost *llmcallview.Cost `json:"cost,omitempty"`
}

// listAdminLLMCallsHandler serves GET /api/admin/llm/calls.
//
// It answers what a deployment spent on managed inference, across every user and
// space — the read the space-scoped route cannot do, because it can only reach
// one run whose space the caller already belongs to. Attributing spend across
// spaces is an operator question, so it lives behind the system-admin grant.
func (h *Handler) listAdminLLMCallsHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.guard().SystemAdmin(w, r); !ok {
		return
	}
	if !httputil.RequireStore(w, h.cfg.LLMCalls, "managed model calls not configured") {
		return
	}
	q := r.URL.Query()
	since, ok := parseTimeParam(w, q.Get("since"), "since")
	if !ok {
		return
	}
	until, ok := parseTimeParam(w, q.Get("until"), "until")
	if !ok {
		return
	}
	filter := coregw.CallFilter{
		UserID:  q.Get("user_id"),
		Model:   q.Get("model"),
		Status:  q.Get("status"),
		Surface: q.Get("surface"),
		Since:   derefTime(since),
		Until:   derefTime(until),
	}
	limit, offset := httputil.LimitOffset(q, "limit", "offset", httputil.BulkPageDefault, httputil.BulkPageMax)

	calls, total, err := h.cfg.LLMCalls.SearchLLMCalls(r.Context(), filter, limit, offset)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_search_llm_calls")
		return
	}
	out := make([]AdminLLMCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, toAdminLLMCall(call))
	}
	httputil.WriteJSON(w, http.StatusOK, AdminLLMCallsResponse{Calls: out, Total: total})
}

func toAdminLLMCall(call coregw.Call) AdminLLMCall {
	out := AdminLLMCall{
		ID:               call.ID,
		UserID:           call.UserID,
		TaskID:           call.TaskID,
		TaskRunID:        call.TaskRunID,
		Surface:          call.Surface,
		SessionID:        call.SessionID,
		Model:            call.Model,
		TargetID:         call.TargetID,
		ProviderType:     call.ProviderType,
		UpstreamModel:    call.UpstreamModel,
		Streaming:        call.Streaming,
		AcceptedAt:       call.AcceptedAt,
		FirstDeltaAt:     call.FirstDeltaAt,
		CompletedAt:      call.CompletedAt,
		Status:           call.Status,
		ErrorClass:       call.ErrorClass,
		Attempts:         call.Attempts,
		PromptTokens:     call.PromptTokens,
		CompletionTokens: call.CompletionTokens,
		TotalTokens:      call.TotalTokens,
		CacheReadTokens:  call.CacheReadTokens,
		CacheWriteTokens: call.CacheWriteTokens,
		UsageSource:      call.UsageSource,
	}
	if cost, ok := llmcallview.Price(call); ok {
		out.Cost = &cost
	}
	return out
}

// derefTime turns the optional bound parseTimeParam returns into the zero-means-
// unbounded time the filter takes.
func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
