// Package llmcallview prices a managed call ledger row for a reader.
//
// Shared because two routes present the same spend: a space reads one run's
// calls and an administrator reads the deployment's. What a call cost, and when
// that cost is unknown rather than zero, must be one answer, not two that happen
// to agree.
package llmcallview

import (
	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
)

// Cost is one call's estimated spend, broken down the way caching charges it.
//
// The breakdown is kept rather than summed away because the parts answer
// different questions. Total is what the call cost; Baseline is what the same
// tokens would have cost with no caching at all, which is the only honest way to
// say whether caching helped — comparing against zero would report a saving on a
// call that only ever wrote.
//
// Amounts are nano-units of Currency — one currency unit is 1e9 of them — so a
// client sums them exactly instead of accumulating float error across a run.
type Cost struct {
	Currency   string `json:"currency"`
	Uncached   int64  `json:"uncached"`
	CacheRead  int64  `json:"cache_read"`
	CacheWrite int64  `json:"cache_write"`
	Output     int64  `json:"output"`
	Total      int64  `json:"total"`
	Baseline   int64  `json:"baseline"`
}

// Price prices a ledger row from its own rate snapshot.
//
// The rates come from the row rather than the catalog on purpose: a model's
// price changes, and recomputing an old call from the new rates would restate
// what a space already spent. A row written before the snapshot existed has no
// rates and reports no cost, which is the truthful answer — nobody recorded what
// it was charged.
func Price(call coregw.Call) (Cost, bool) {
	if call.Currency == "" || call.RateInputPerMTok == nil {
		return Cost{}, false
	}
	usage := cllm.Usage{
		PromptTokens:     derefInt(call.PromptTokens),
		CompletionTokens: derefInt(call.CompletionTokens),
		TotalTokens:      derefInt(call.TotalTokens),
		CacheReadTokens:  derefInt(call.CacheReadTokens),
		CacheWriteTokens: derefInt(call.CacheWriteTokens),
	}
	cost, ok := cllm.EstimateCost(usage, cllm.Pricing{
		Currency:          call.Currency,
		InputPerMTok:      derefInt64(call.RateInputPerMTok),
		CacheReadPerMTok:  derefInt64(call.RateCacheReadPerMTok),
		CacheWritePerMTok: derefInt64(call.RateCacheWritePerMTok),
		OutputPerMTok:     derefInt64(call.RateOutputPerMTok),
	})
	if !ok {
		return Cost{}, false
	}
	return Cost{
		Currency:   cost.Currency,
		Uncached:   cost.Uncached,
		CacheRead:  cost.CacheRead,
		CacheWrite: cost.CacheWrite,
		Output:     cost.Output,
		Total:      cost.Total,
		Baseline:   cost.Baseline,
	}, true
}

func derefInt(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

func derefInt64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}
