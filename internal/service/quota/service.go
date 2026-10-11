package quota

import (
	"context"
	"fmt"
	"time"

	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	corequota "github.com/icloudbb/buildmax/internal/core/quota"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
)

// Service enforces space-scoped quota (run count and token limits) using tier limits from the store.
type Service struct {
	SpaceStore  corespace.Store
	UsageReader corequota.UsageInWindowReader
	TierStore   corequota.TierStore
	// StorageReader reports the bytes a space's artifacts hold. Nil leaves the
	// storage dimension unenforced and unreported, which is what a deployment
	// without artifact storage has.
	StorageReader corequota.StorageReader
	DefaultTier   string
	// Audit records a space approaching or reaching its limits. Nil records
	// nothing, so a deployment without a database still enforces quota — the
	// enforcement is the point, and the record of it is what a space admin reads
	// afterwards. It is the full store rather than a writer because the events
	// are deduplicated against what is already there; see alert.go.
	Audit coreaudit.Store
	clock func() time.Time
}

// UsageInfo is a snapshot of a space's usage and tier limits for display.
type UsageInfo struct {
	RunCount           int
	TotalTokens        int
	TierName           string
	PeriodDays         int
	MaxRunsPerPeriod   *int // nil when tier unknown or not found
	MaxTokensPerPeriod *int
	// StorageBytes is what the space's artifacts hold now, not a windowed
	// total. Nil when this deployment has no artifact storage to read.
	StorageBytes    *int64
	MaxStorageBytes *int64
}

const defaultUsagePeriodDays = 30

func (c *Service) now() time.Time {
	if c.clock != nil {
		return c.clock()
	}
	return time.Now()
}

// GetUsage returns the current space's usage and tier info in the same rolling
// window used by Check. When space or tier is not found, returns usage for a
// default 30-day window with limits nil; a read that fails is an error, because
// a zeroed snapshot reads as "this space has used nothing".
func (c *Service) GetUsage(ctx context.Context, spaceID string) (*UsageInfo, error) {
	space, err := c.SpaceStore.GetSpace(ctx, spaceID)
	if err != nil {
		return nil, fmt.Errorf("read space %s: %w", spaceID, err)
	}
	if space == nil {
		return &UsageInfo{}, nil
	}
	tierName := space.QuotaTier
	if tierName == "" {
		tierName = c.DefaultTier
	}
	now := c.now().UTC()

	// Resolve tier limits; if not found, still return usage for default window with limits nil
	tier, err := c.TierStore.GetQuotaTier(ctx, tierName)
	if err != nil {
		return nil, fmt.Errorf("read quota tier %s: %w", tierName, err)
	}
	if tier == nil {
		periodDays := defaultUsagePeriodDays
		since := now.Add(-time.Duration(periodDays) * 24 * time.Hour)
		runCount, totalTokens, err := c.UsageReader.SpaceUsageInWindow(ctx, spaceID, since, now)
		if err != nil {
			return nil, fmt.Errorf("read space %s usage: %w", spaceID, err)
		}
		return &UsageInfo{
			RunCount:    runCount,
			TotalTokens: totalTokens,
			TierName:    tierName,
			PeriodDays:  periodDays,
		}, nil
	}

	since := now.Add(-time.Duration(tier.PeriodDays) * 24 * time.Hour)
	runCount, totalTokens, err := c.UsageReader.SpaceUsageInWindow(ctx, spaceID, since, now)
	if err != nil {
		return nil, fmt.Errorf("read space %s usage: %w", spaceID, err)
	}
	maxRuns := tier.MaxRunsPerPeriod
	maxTokens := tier.MaxTokensPerPeriod
	info := &UsageInfo{
		RunCount:           runCount,
		TotalTokens:        totalTokens,
		TierName:           tier.TierName,
		PeriodDays:         tier.PeriodDays,
		MaxRunsPerPeriod:   &maxRuns,
		MaxTokensPerPeriod: &maxTokens,
	}
	// Read only where there is something to read. A deployment with no artifact
	// storage reports no storage rather than zero, which would read as a space
	// holding nothing.
	if c.StorageReader != nil {
		held, err := c.StorageReader.SpaceArtifactBytes(ctx, spaceID)
		if err != nil {
			return nil, fmt.Errorf("read space %s storage: %w", spaceID, err)
		}
		maxStorage := tier.MaxStorageBytes
		info.StorageBytes = &held
		info.MaxStorageBytes = &maxStorage
	}
	return info, nil
}

// CheckStorage reports whether the space may hold addBytes more.
//
// Separate from Check because storage is a stock, not a rate: there is no
// window, PeriodDays does not apply, and the answer changes when an artifact is
// deleted rather than when time passes. The admission rules are otherwise the
// same — an unreadable limit is an error rather than an allowance, and an
// absent one is no limit.
func (c *Service) CheckStorage(ctx context.Context, spaceID string, addBytes int64) (allowed bool, reason string, err error) {
	if c.StorageReader == nil {
		return true, "", nil
	}
	space, err := c.SpaceStore.GetSpace(ctx, spaceID)
	if err != nil {
		return false, "", fmt.Errorf("read space %s: %w", spaceID, err)
	}
	if space == nil {
		return true, "", nil
	}
	tierName := space.QuotaTier
	if tierName == "" {
		tierName = c.DefaultTier
	}
	if tierName == "" {
		return true, "", nil
	}
	tier, err := c.TierStore.GetQuotaTier(ctx, tierName)
	if err != nil {
		return false, "", fmt.Errorf("read quota tier %s: %w", tierName, err)
	}
	// A tier that sets no storage limit imposes none. This is what keeps a
	// deployment that never chose a storage policy from acquiring one when the
	// column appeared underneath it.
	if tier == nil || tier.MaxStorageBytes <= 0 {
		return true, "", nil
	}
	held, err := c.StorageReader.SpaceArtifactBytes(ctx, spaceID)
	if err != nil {
		return false, "", fmt.Errorf("read space %s storage: %w", spaceID, err)
	}
	now := c.now().UTC()
	since := now.Add(-time.Duration(tier.PeriodDays) * 24 * time.Hour)
	if held+addBytes > tier.MaxStorageBytes {
		c.noteUsage(ctx, spaceID, corequota.LimitStorage, held, tier.MaxStorageBytes, since, true)
		return false, fmt.Sprintf("quota exceeded: storage limit, %d of %d bytes used", held, tier.MaxStorageBytes), nil
	}
	c.warnIfNear(ctx, spaceID, corequota.LimitStorage, held+addBytes, tier.MaxStorageBytes, since)
	return true, "", nil
}

// Check returns the limit adding addRuns and addTokens would exceed, or nil
// when the space may add them. A refusal's Reason is the 429 message.
//
// A configured limit that cannot be read is an error, not an allowance. The two
// look the same to a caller that only asks "allowed?", and answering yes means a
// deployment whose database is unreachable serves unmetered work and records
// nothing about having done so. Absence of a limit still means no limit: a space
// with no record, no tier, or a tier that names nothing is admitted.
func (c *Service) Check(ctx context.Context, spaceID string, addRuns, addTokens int) (*corequota.Refusal, error) {
	space, err := c.SpaceStore.GetSpace(ctx, spaceID)
	if err != nil {
		return nil, fmt.Errorf("read space %s: %w", spaceID, err)
	}
	if space == nil {
		return nil, nil // no space => no tier => no limit
	}
	tierName := space.QuotaTier
	if tierName == "" {
		tierName = c.DefaultTier
	}
	if tierName == "" {
		return nil, nil // no tier => allow
	}
	tier, err := c.TierStore.GetQuotaTier(ctx, tierName)
	if err != nil {
		return nil, fmt.Errorf("read quota tier %s: %w", tierName, err)
	}
	if tier == nil {
		return nil, nil // the tier names nothing => no limit
	}
	now := c.now().UTC()
	since := now.Add(-time.Duration(tier.PeriodDays) * 24 * time.Hour)
	runCount, totalTokens, err := c.UsageReader.SpaceUsageInWindow(ctx, spaceID, since, now)
	if err != nil {
		return nil, fmt.Errorf("read space %s usage: %w", spaceID, err)
	}
	if runCount+addRuns > tier.MaxRunsPerPeriod {
		c.noteUsage(ctx, spaceID, corequota.LimitRuns, int64(runCount), int64(tier.MaxRunsPerPeriod), since, true)
		return &corequota.Refusal{Limit: corequota.LimitRuns, Max: int64(tier.MaxRunsPerPeriod), PeriodDays: tier.PeriodDays}, nil
	}
	if totalTokens+addTokens > tier.MaxTokensPerPeriod {
		c.noteUsage(ctx, spaceID, corequota.LimitTokens, int64(totalTokens), int64(tier.MaxTokensPerPeriod), since, true)
		return &corequota.Refusal{Limit: corequota.LimitTokens, Max: int64(tier.MaxTokensPerPeriod), PeriodDays: tier.PeriodDays}, nil
	}

	// Warn on the way past the threshold, on an admission that was allowed.
	// This is the only moment the crossing is visible: usage is a rolling
	// window with no period boundary for a sweep to notice it at.
	c.warnIfNear(ctx, spaceID, corequota.LimitRuns, int64(runCount+addRuns), int64(tier.MaxRunsPerPeriod), since)
	c.warnIfNear(ctx, spaceID, corequota.LimitTokens, int64(totalTokens+addTokens), int64(tier.MaxTokensPerPeriod), since)
	return nil, nil
}

// warnIfNear records a threshold crossing, and does nothing below it.
func (c *Service) warnIfNear(ctx context.Context, spaceID string, limit corequota.Limit, used, max int64, windowStart time.Time) {
	if c.Audit == nil || max <= 0 {
		return
	}
	if float64(used) < quotaWarnThreshold*float64(max) {
		return
	}
	c.noteUsage(ctx, spaceID, limit, used, max, windowStart, false)
}
