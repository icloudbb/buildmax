package work

import (
	"net/http"
	"time"

	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
	"github.com/icloudbb/buildmax/internal/server/handlers/llmcallview"
	"github.com/icloudbb/buildmax/internal/server/httputil"
)

// LLMCallSummary is one managed call as a reader sees it.
//
// It is the ledger row minus nothing, because the ledger already holds no
// prompts, tool arguments, or generated content — it was built as an accounting
// record. What it does hold and this deliberately drops is `target_id`, the
// catalog entry the name resolved to: the operator's routing behind a model
// name is not the caller's business.
type LLMCallSummary struct {
	ID string `json:"id"`
	// UserID is the run's owner. A task run is somebody's work even though no
	// person was at the keyboard when it called a model.
	UserID    *string `json:"user_id,omitempty"`
	TaskID    *string `json:"task_id,omitempty"`
	Surface   string  `json:"surface,omitempty"`
	SessionID *string `json:"session_id,omitempty"`

	Model     string `json:"model,omitempty"`
	Streaming bool   `json:"streaming"`

	AcceptedAt   time.Time  `json:"accepted_at"`
	FirstDeltaAt *time.Time `json:"first_delta_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`

	Status     string  `json:"status"`
	ErrorClass *string `json:"error_class,omitempty"`
	Attempts   int     `json:"attempts,omitempty"`

	PromptTokens     *int `json:"prompt_tokens,omitempty"`
	CompletionTokens *int `json:"completion_tokens,omitempty"`
	TotalTokens      *int `json:"total_tokens,omitempty"`
	// Cache counts are the cached parts of PromptTokens, not tokens on top of
	// it. The ledger already recorded them; leaving them out of this view was
	// what made a cache-heavy run indistinguishable from an uncached one.
	CacheReadTokens  *int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *int `json:"cache_write_tokens,omitempty"`
	// UsageSource separates a provider that reported nothing from one that
	// reported zero. Without it an absent count reads as a free call.
	UsageSource string `json:"usage_source,omitempty"`

	// Cost is what this call is estimated to have cost, priced at the rates
	// recorded when it ran rather than at whatever the catalog says now. Absent
	// when the model was unpriced or the provider reported no usage: an
	// unpriced call is an unknown, and a zero would read as a free one.
	//
	// Amounts are nano-units of Currency — one currency unit is 1e9 of them —
	// so a client sums them exactly instead of accumulating float error across
	// a run.
	Cost *llmcallview.Cost `json:"cost,omitempty"`
}

// listTaskRunLLMCallsHandler serves
// GET /api/spaces/{space_id}/task-runs/{task_run_id}/llm-calls.
//
// It answers what a run spent and on which approved model, which until now was
// recorded and unreachable: the ledger had no route, so the only way to read it
// was a database query. Diagnosing a run should not require the database
// password.
func (h *Handler) listTaskRunLLMCallsHandler(w http.ResponseWriter, r *http.Request) {
	// Space membership is checked before the ledger, so an unauthenticated caller
	// learns nothing about whether this deployment records managed calls. Every
	// other space-scoped route authenticates first, and an authorization matrix is
	// only meaningful if they all agree.
	_, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Spaces, "spaces not configured")
	if !ok {
		return
	}
	if !httputil.RequireStore(w, h.cfg.LLMCalls, "managed model calls not configured") {
		return
	}
	taskRunID, ok := httputil.PathValue(w, r, "task_run_id")
	if !ok {
		return
	}
	// The run has to belong to this space's conversations before its ledger is
	// read, so a member of one space cannot enumerate another's spending by
	// guessing run ids. This check is the whole authorization: ledger rows carry
	// no space of their own, and a run belongs to exactly one.
	if _, _, ok = h.runAndTaskForSpace(w, r, spaceID, taskRunID); !ok {
		return
	}

	calls, err := h.cfg.LLMCalls.ListLLMCallsByTaskRun(r.Context(), taskRunID)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "task_run_llm_calls", "task_run_id", taskRunID)
		return
	}
	out := make([]LLMCallSummary, 0, len(calls))
	for _, call := range calls {
		out = append(out, toLLMCallSummary(call))
	}
	httputil.WriteJSON(w, http.StatusOK, out)
}

func toLLMCallSummary(call coregw.Call) LLMCallSummary {
	out := LLMCallSummary{
		ID:               call.ID,
		UserID:           call.UserID,
		TaskID:           call.TaskID,
		Surface:          call.Surface,
		SessionID:        call.SessionID,
		Model:            call.Model,
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
