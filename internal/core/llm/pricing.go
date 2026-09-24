package llm

import (
	"fmt"
	"math/big"
	"strings"
)

// NanoUnitsPerUnit is the fixed-point scale rates and costs are held at: one
// currency unit is 1e9 of them.
//
// Money is not held in a float here. A rate like $0.30 per million tokens has
// no exact binary representation, and a run of a few hundred calls accumulates
// the error into a figure someone will compare against an invoice. Integer
// nano-units multiply and add exactly, and nine decimal places is finer than
// any provider prices to.
const NanoUnitsPerUnit = 1_000_000_000

// tokensPerRateUnit is the token count a rate is quoted against. Providers
// publish per-million-token prices, so storing the rate in the same unit means
// a configured value can be checked against a price page without arithmetic.
const tokensPerRateUnit = 1_000_000

// Pricing is what one model charges, as nano-currency-units per million tokens.
//
// The four rates are separate because prompt caching prices them differently: a
// cache read is cheaper than fresh input and a cache write is dearer, which is
// the whole reason caching is a decision rather than a free win. Collapsing
// them into one input rate would make every cached call look mispriced.
//
// A zero rate is a real price — some models genuinely do not charge for cache
// reads — so "not configured" is the zero Pricing as a whole, reported by
// Configured, rather than a zero in any single field.
type Pricing struct {
	// Currency is the ISO 4217 code the rates are quoted in. Rates in different
	// currencies are never added: BuildMax does not convert.
	Currency string
	// InputPerMTok is fresh prompt input, excluding anything cached.
	InputPerMTok int64
	// CacheReadPerMTok is prompt served from the provider's cache.
	CacheReadPerMTok int64
	// CacheWritePerMTok is prompt written into it.
	CacheWritePerMTok int64
	// OutputPerMTok is generated tokens.
	OutputPerMTok int64
}

// Configured reports whether these rates can price a call. A currency alone is
// not enough, and neither is a rate without one: a cost shown from half a
// price list is a guess wearing a number.
func (p Pricing) Configured() bool {
	return p.Currency != "" &&
		(p.InputPerMTok != 0 || p.CacheReadPerMTok != 0 || p.CacheWritePerMTok != 0 || p.OutputPerMTok != 0)
}

// Cost is what one call, or a run of them, is estimated to have cost.
//
// Every field is nano-units of Currency. The breakdown is kept rather than
// summed away because the parts answer different questions: whether caching
// paid for itself is Uncached + CacheRead + CacheWrite against Baseline, and
// what the call cost is Total.
type Cost struct {
	Currency string `json:"currency"`
	// Uncached is fresh prompt input — the prompt minus what was cached.
	Uncached int64 `json:"uncached"`
	// CacheRead and CacheWrite are the cached parts of the prompt.
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
	// Output is the generated tokens.
	Output int64 `json:"output"`
	// Total is the sum of the four above.
	Total int64 `json:"total"`
	// Baseline is what the same call would have cost with no caching at all:
	// every prompt token billed at the fresh input rate. It is the only honest
	// way to say whether caching helped, because the alternative — comparing
	// against zero — would report a saving on a call that only ever wrote.
	Baseline int64 `json:"baseline"`
}

// Saved is Baseline minus Total, and zero when caching cost more than it saved.
//
// A negative saving is not reported as one. A run that wrote a cache entry
// nothing went on to read genuinely paid more than it would have without
// caching, and dressing that up as a small saving is the kind of claim this
// package exists not to make. Callers that want the loss compare the two
// fields themselves.
func (c Cost) Saved() int64 {
	if c.Baseline <= c.Total {
		return 0
	}
	return c.Baseline - c.Total
}

// EstimateCost prices one usage report, or (Cost{}, false) when it cannot.
//
// It refuses rather than guesses in two cases. Unconfigured rates give no basis
// for a number at all. Usage a provider never reported gives nothing to
// multiply: a call with no counts is an unmeasured call, not a free one, and
// returning a zero cost for it would turn an unknown into a claim.
func EstimateCost(usage Usage, pricing Pricing) (Cost, bool) {
	if !pricing.Configured() {
		return Cost{}, false
	}
	if usage.PromptTokens == 0 && usage.CompletionTokens == 0 {
		return Cost{}, false
	}
	// The cache counts break the prompt down rather than add to it, so fresh
	// input is what is left after them. A provider that over-reports would
	// otherwise drive this negative and bill a call less than nothing.
	uncached := max(usage.PromptTokens-usage.CacheReadTokens-usage.CacheWriteTokens, 0)

	cost := Cost{
		Currency:   pricing.Currency,
		Uncached:   rate(uncached, pricing.InputPerMTok),
		CacheRead:  rate(usage.CacheReadTokens, pricing.CacheReadPerMTok),
		CacheWrite: rate(usage.CacheWriteTokens, pricing.CacheWritePerMTok),
		Output:     rate(usage.CompletionTokens, pricing.OutputPerMTok),
		Baseline: rate(usage.PromptTokens, pricing.InputPerMTok) +
			rate(usage.CompletionTokens, pricing.OutputPerMTok),
	}
	cost.Total = cost.Uncached + cost.CacheRead + cost.CacheWrite + cost.Output
	return cost, true
}

// Add accumulates another call's cost.
//
// Two costs in different currencies are not added, because BuildMax holds no
// exchange rate and inventing one would produce a total that is wrong in both.
// The mismatch is reported so a caller can say "unavailable" rather than show a
// figure that silently dropped half the run.
func (c Cost) Add(other Cost) (Cost, bool) {
	if other.Currency == "" {
		return c, true
	}
	if c.Currency == "" {
		return other, true
	}
	if c.Currency != other.Currency {
		return c, false
	}
	return Cost{
		Currency:   c.Currency,
		Uncached:   c.Uncached + other.Uncached,
		CacheRead:  c.CacheRead + other.CacheRead,
		CacheWrite: c.CacheWrite + other.CacheWrite,
		Output:     c.Output + other.Output,
		Total:      c.Total + other.Total,
		Baseline:   c.Baseline + other.Baseline,
	}, true
}

// rate prices one token count. The division is last so the intermediate keeps
// full precision, and it truncates: an estimate that rounds up on every line
// would drift above what a provider actually charges.
func rate(tokens int, perMTok int64) int64 {
	if tokens <= 0 || perMTok == 0 {
		return 0
	}
	return int64(tokens) * perMTok / tokensPerRateUnit
}

// ParseRate reads a decimal price string — "3", "0.30", "3.75" — into
// nano-units.
//
// Rates are written as decimal strings rather than numbers because that is how
// every provider publishes them, so a configured value can be compared against
// a price page without arithmetic, and because a JSON or YAML float would round
// the value before this package ever saw it.
func ParseRate(s string) (int64, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0, nil
	}
	value, ok := new(big.Rat).SetString(trimmed)
	if !ok {
		return 0, fmt.Errorf("price %q is not a decimal number", s)
	}
	if value.Sign() < 0 {
		return 0, fmt.Errorf("price %q is negative", s)
	}
	scaled := new(big.Rat).Mul(value, new(big.Rat).SetInt64(NanoUnitsPerUnit))
	if !scaled.IsInt() {
		return 0, fmt.Errorf("price %q is finer than nine decimal places", s)
	}
	if !scaled.Num().IsInt64() {
		return 0, fmt.Errorf("price %q is too large", s)
	}
	return scaled.Num().Int64(), nil
}

// FormatRate renders a nano-unit rate as the shortest decimal string ParseRate
// reads back to the same value. It is how a rate crosses the wire: exact, and
// in the form a settings file or price page writes it.
func FormatRate(nano int64) string {
	sign := ""
	if nano < 0 {
		sign, nano = "-", -nano
	}
	whole, frac := nano/NanoUnitsPerUnit, nano%NanoUnitsPerUnit
	if frac == 0 {
		return fmt.Sprintf("%s%d", sign, whole)
	}
	return fmt.Sprintf("%s%d.%s", sign, whole, strings.TrimRight(fmt.Sprintf("%09d", frac), "0"))
}

// FormatAmount renders nano-units as a decimal string with six places, which is
// enough to show a single cheap call without reading as zero.
func FormatAmount(nano int64) string {
	negative := nano < 0
	if negative {
		nano = -nano
	}
	// Six places out of nine: divide away the last three, then split.
	micro := nano / 1_000
	whole, frac := micro/1_000_000, micro%1_000_000
	out := fmt.Sprintf("%d.%06d", whole, frac)
	if negative {
		return "-" + out
	}
	return out
}
