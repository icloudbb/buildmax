package llm

import "testing"

// Rates are written the way providers publish them, so a configured value can
// be checked against a price page without arithmetic. They are held as integers
// because a run of a few hundred calls accumulates float error into a figure
// someone will compare against an invoice.
func TestParseRate(t *testing.T) {
	tests := map[string]int64{
		"":            0,
		"0":           0,
		"3":           3_000_000_000,
		"3.00":        3_000_000_000,
		"0.30":        300_000_000,
		"3.75":        3_750_000_000,
		"15":          15_000_000_000,
		"0.000000001": 1,
		" 1.5 ":       1_500_000_000,
	}
	for in, want := range tests {
		got, err := ParseRate(in)
		if err != nil {
			t.Errorf("ParseRate(%q) = %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseRate(%q) = %d, want %d", in, got, want)
		}
	}
	// Exponent form parses: it is still a decimal number, just an unusual way
	// to write a price.
	if got, err := ParseRate("1e-3"); err != nil || got != 1_000_000 {
		t.Errorf(`ParseRate("1e-3") = %d, %v`, got, err)
	}
	for _, bad := range []string{"free", "-1", "1.2.3", "0.0000000001"} {
		if _, err := ParseRate(bad); err == nil {
			t.Errorf("ParseRate(%q) = nil error, want a refusal", bad)
		}
	}
}

// Claude Sonnet's published rates, as an operator would write them.
func sonnetPricing(t *testing.T) Pricing {
	t.Helper()
	rate := func(s string) int64 {
		v, err := ParseRate(s)
		if err != nil {
			t.Fatalf("ParseRate(%q): %v", s, err)
		}
		return v
	}
	return Pricing{
		Currency:          "USD",
		InputPerMTok:      rate("3.00"),
		CacheReadPerMTok:  rate("0.30"),
		CacheWritePerMTok: rate("3.75"),
		OutputPerMTok:     rate("15.00"),
	}
}

func TestEstimateCostPricesEachTokenClassSeparately(t *testing.T) {
	// A cached turn: 100k prompt, of which 90k was read back from the cache.
	usage := Usage{
		PromptTokens: 100_000, CompletionTokens: 1_000, TotalTokens: 101_000,
		CacheReadTokens: 90_000,
	}
	cost, ok := EstimateCost(usage, sonnetPricing(t))
	if !ok {
		t.Fatal("a priced model with reported usage should produce a cost")
	}
	// 10k fresh at $3/M = $0.03; 90k read at $0.30/M = $0.027; 1k out at
	// $15/M = $0.015.
	if got, want := FormatAmount(cost.Uncached), "0.030000"; got != want {
		t.Errorf("uncached = %s, want %s", got, want)
	}
	if got, want := FormatAmount(cost.CacheRead), "0.027000"; got != want {
		t.Errorf("cache read = %s, want %s", got, want)
	}
	if got, want := FormatAmount(cost.Output), "0.015000"; got != want {
		t.Errorf("output = %s, want %s", got, want)
	}
	if got, want := FormatAmount(cost.Total), "0.072000"; got != want {
		t.Errorf("total = %s, want %s", got, want)
	}
	// Without caching the same 100k prompt bills at $3/M = $0.30, plus output.
	if got, want := FormatAmount(cost.Baseline), "0.315000"; got != want {
		t.Errorf("baseline = %s, want %s", got, want)
	}
	if got, want := FormatAmount(cost.Saved()), "0.243000"; got != want {
		t.Errorf("saved = %s, want %s", got, want)
	}
}

// A cache write costs more than fresh input. On the call that pays for one and
// nothing reads it back, caching lost money, and reporting that as a small
// saving would be exactly the false claim this package exists to avoid.
func TestACacheWriteThatIsNeverReadReportsNoSaving(t *testing.T) {
	usage := Usage{PromptTokens: 100_000, CompletionTokens: 1_000, CacheWriteTokens: 90_000}
	cost, ok := EstimateCost(usage, sonnetPricing(t))
	if !ok {
		t.Fatal("expected a cost")
	}
	if cost.Total <= cost.Baseline {
		t.Errorf("a write-only call should cost more than the same call uncached: total %s baseline %s",
			FormatAmount(cost.Total), FormatAmount(cost.Baseline))
	}
	if cost.Saved() != 0 {
		t.Errorf("saved = %s, want 0 on a call that cost more", FormatAmount(cost.Saved()))
	}
}

// Two absences that must not become zeroes: no rates, and no reported usage. A
// zero cost for either turns an unknown into a claim that the call was free.
func TestEstimateCostRefusesRatherThanGuess(t *testing.T) {
	priced := sonnetPricing(t)
	usage := Usage{PromptTokens: 10, CompletionTokens: 1}

	if _, ok := EstimateCost(usage, Pricing{}); ok {
		t.Error("an unpriced model produced a cost")
	}
	if _, ok := EstimateCost(usage, Pricing{Currency: "USD"}); ok {
		t.Error("a currency with no rates produced a cost")
	}
	if _, ok := EstimateCost(usage, Pricing{InputPerMTok: 1}); ok {
		t.Error("rates with no currency produced a cost")
	}
	if _, ok := EstimateCost(Usage{}, priced); ok {
		t.Error("a call the provider never reported produced a cost")
	}
}

// A provider that reports more cached tokens than prompt tokens must not drive
// fresh input negative and bill the call less than nothing.
func TestOverReportedCacheDoesNotProduceANegativeCost(t *testing.T) {
	usage := Usage{PromptTokens: 100, CompletionTokens: 10, CacheReadTokens: 500}
	cost, ok := EstimateCost(usage, sonnetPricing(t))
	if !ok {
		t.Fatal("expected a cost")
	}
	if cost.Uncached < 0 || cost.Total < 0 {
		t.Errorf("negative cost: %+v", cost)
	}
}

// BuildMax holds no exchange rate, so two currencies are never summed into a
// figure that is wrong in both.
func TestAddRefusesToMixCurrencies(t *testing.T) {
	usd, _ := EstimateCost(Usage{PromptTokens: 1000, CompletionTokens: 100}, sonnetPricing(t))
	eur := usd
	eur.Currency = "EUR"

	if _, ok := usd.Add(eur); ok {
		t.Error("two currencies were added")
	}
	summed, ok := usd.Add(usd)
	if !ok {
		t.Fatal("the same currency should add")
	}
	if summed.Total != usd.Total*2 {
		t.Errorf("total = %d, want %d", summed.Total, usd.Total*2)
	}
	// An unpriced call contributes nothing rather than breaking the total: it
	// is already reported as unmeasured elsewhere.
	if _, ok := usd.Add(Cost{}); !ok {
		t.Error("an unpriced call should not poison a total")
	}
}

func TestFormatAmount(t *testing.T) {
	tests := map[int64]string{
		0:              "0.000000",
		1_000:          "0.000001",
		300_000_000:    "0.300000",
		3_000_000_000:  "3.000000",
		-3_000_000_000: "-3.000000",
		// Below a millionth of a unit: shown as zero rather than rounded up to
		// something the caller was not charged.
		999: "0.000000",
	}
	for in, want := range tests {
		if got := FormatAmount(in); got != want {
			t.Errorf("FormatAmount(%d) = %q, want %q", in, got, want)
		}
	}
}

// A rate formatted for the wire parses back to the same nano-units, so a
// client prices a managed call exactly as the deployment's ledger does.
func TestFormatRateRoundTrips(t *testing.T) {
	for _, in := range []string{"0", "3", "0.2", "0.02", "3.75", "15", "0.000000001", "1234.5"} {
		nano, err := ParseRate(in)
		if err != nil {
			t.Fatalf("ParseRate(%q): %v", in, err)
		}
		if got := FormatRate(nano); got != in {
			t.Errorf("FormatRate(ParseRate(%q)) = %q", in, got)
		}
	}
}
