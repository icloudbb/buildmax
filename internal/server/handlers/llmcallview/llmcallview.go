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

// Totals is what a set of calls cost, per currency, and how many of them no
// one priced. Currencies are never added together.
type Totals struct {
	Calls         int    `json:"call_count"`
	TotalTokens   int    `json:"total_tokens"`
	Costs         []Cost `json:"costs"`
	UnpricedCalls int    `json:"unpriced_calls"`
}

// PriceTotals prices summed ledger groups, each from its own rate snapshot,
// with the same estimate a single call gets.
func PriceTotals(groups []coregw.CallTotals) Totals {
	out := Totals{Costs: []Cost{}}
	byCurrency := map[string]int{}
	for _, g := range groups {
		out.Calls += g.Calls
		out.TotalTokens += g.TotalTokens
		cost, ok := cllm.EstimateCost(cllm.Usage{
			PromptTokens:     g.PromptTokens,
			CompletionTokens: g.CompletionTokens,
			TotalTokens:      g.TotalTokens,
			CacheReadTokens:  g.CacheReadTokens,
			CacheWriteTokens: g.CacheWriteTokens,
		}, cllm.Pricing{
			Currency:          g.Currency,
			InputPerMTok:      g.RateInputPerMTok,
			CacheReadPerMTok:  g.RateCacheReadPerMTok,
			CacheWritePerMTok: g.RateCacheWritePerMTok,
			OutputPerMTok:     g.RateOutputPerMTok,
		})
		if !ok {
			if g.Currency == "" {
				out.UnpricedCalls += g.Calls
			}
			continue
		}
		i, seen := byCurrency[cost.Currency]
		if !seen {
			byCurrency[cost.Currency] = len(out.Costs)
			out.Costs = append(out.Costs, Cost{Currency: cost.Currency})
			i = len(out.Costs) - 1
		}
		c := &out.Costs[i]
		c.Uncached += cost.Uncached
		c.CacheRead += cost.CacheRead
		c.CacheWrite += cost.CacheWrite
		c.Output += cost.Output
		c.Total += cost.Total
		c.Baseline += cost.Baseline
	}
	return out
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
