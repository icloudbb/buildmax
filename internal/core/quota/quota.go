package quota

import (
	"context"
	"time"
)

// Tier defines limits for a tier (e.g. free_trial, pro).
//
// Two kinds of limit live here and they are not measured the same way. Runs and
// tokens are rates, spent over PeriodDays and forgotten as the window moves.
// Storage is a stock: bytes held now, released only when an artifact is deleted
// or expires. A tier that leaves a limit at zero does not impose it.
type Tier struct {
	TierName           string `json:"tier_name"`
	MaxRunsPerPeriod   int    `json:"max_runs_per_period"`
	MaxTokensPerPeriod int    `json:"max_tokens_per_period"`
	// MaxStorageBytes caps what the space's live artifacts may hold at once.
	// PeriodDays does not apply to it.
	MaxStorageBytes int64 `json:"max_storage_bytes"`
	PeriodDays      int   `json:"period_days"`
}

// TierStore provides quota tier limits by tier name.
//
// Tier definitions are seeded, not edited: a System Administrator assigns a
// Space to an existing tier, and nothing here writes one.
type TierStore interface {
	// GetQuotaTier returns the tier limits by tier name, or (nil, nil) when not found.
	GetQuotaTier(ctx context.Context, tierName string) (*Tier, error)
	// ListQuotaTiers returns every defined tier, ordered by name.
	ListQuotaTiers(ctx context.Context) ([]Tier, error)
}

// UsageInWindowReader provides usage aggregation for a space in a time window.
type UsageInWindowReader interface {
	// SpaceUsageInWindow returns run count and total tokens for the space in [sinceUnix, untilUnix].
	SpaceUsageInWindow(ctx context.Context, spaceID string, since, until time.Time) (runCount, totalTokens int, err error)
}

// StorageReader reports what a space currently holds.
//
// Separate from UsageInWindowReader because it takes no window: asking "how
// many bytes in the last 30 days" would answer a question nobody has, and a
// tier that limited it that way would let a space hold unbounded storage by
// waiting.
type StorageReader interface {
	// SpaceArtifactBytes returns the bytes the space's live artifacts hold.
	SpaceArtifactBytes(ctx context.Context, spaceID string) (int64, error)
}

// Limit names one of a tier's limits. The strings are persisted in audit event
// details and in a refused run's failure cause, so they are permanent.
type Limit string

const (
	LimitRuns   Limit = "runs"
	LimitTokens Limit = "tokens"
	// LimitStorage is a stock rather than a rate: what the space holds now,
	// not what it spent in a window.
	LimitStorage Limit = "storage"
)

// Refusal is the rate limit an admission would exceed, as the Space's quota
// tier sets it. A run the gateway refused records it, so the person reading
// the failure is told which allowance ran out rather than only that one did.
type Refusal struct {
	Limit Limit
	// Max is the tier's limit for Limit.
	Max int64
	// PeriodDays is the rolling window the limit counts over.
	PeriodDays int
}

// Reason is the sentence a refused caller reads.
func (r Refusal) Reason() string {
	if r.Limit == LimitRuns {
		return "quota exceeded: run limit"
	}
	return "quota exceeded: token limit"
}
