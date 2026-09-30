package db

import (
	"context"
	"errors"

	corequota "github.com/icloudbb/buildmax/internal/core/quota"

	"gorm.io/gorm"
)

type quotaTierRow struct {
	TierName           string `gorm:"column:tier_name;primaryKey;type:varchar(64)"`
	MaxRunsPerPeriod   int    `gorm:"column:max_runs_per_period;not null"`
	MaxTokensPerPeriod int    `gorm:"column:max_tokens_per_period;not null"`
	// MaxStorageBytes caps what a space's live artifacts may hold. Zero is no
	// limit, which is what an existing deployment gets when the column appears
	// underneath it -- a storage policy nobody chose must not arrive with a
	// schema change. PeriodDays does not apply: this is a stock, not a rate.
	MaxStorageBytes int64 `gorm:"column:max_storage_bytes;not null;default:0"`
	PeriodDays      int   `gorm:"column:period_days;not null"`
}

func (quotaTierRow) TableName() string { return "quota_tier" }

func toQuotaTier(row *quotaTierRow) *corequota.Tier {
	if row == nil {
		return nil
	}
	return &corequota.Tier{
		TierName:           row.TierName,
		MaxRunsPerPeriod:   row.MaxRunsPerPeriod,
		MaxTokensPerPeriod: row.MaxTokensPerPeriod,
		MaxStorageBytes:    row.MaxStorageBytes,
		PeriodDays:         row.PeriodDays,
	}
}

// GetQuotaTier returns the tier limits by tier name, or (nil, nil) when not found.
func (s *Store) GetQuotaTier(ctx context.Context, tierName string) (*corequota.Tier, error) {
	var t quotaTierRow
	err := s.db.WithContext(ctx).Where("tier_name = ?", tierName).First(&t).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toQuotaTier(&t), nil
}

// ListQuotaTiers returns every defined tier, ordered by name.
func (s *Store) ListQuotaTiers(ctx context.Context) ([]corequota.Tier, error) {
	var rows []quotaTierRow
	if err := s.db.WithContext(ctx).Order("tier_name ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]corequota.Tier, 0, len(rows))
	for i := range rows {
		out = append(out, *toQuotaTier(&rows[i]))
	}
	return out, nil
}

// SeedDefaultQuotaTiers inserts free_trial and pro tiers if the quota_tier table is empty.
func (s *Store) SeedDefaultQuotaTiers(ctx context.Context) error {
	var count int64
	if err := s.db.WithContext(ctx).Model(&quotaTierRow{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	// The seeds deliberately set no storage limit. Runs and tokens cost money
	// per period and a trial has to bound them; bytes held are cheap until they
	// are not, and picking a number here would impose it on every deployment
	// that seeded these tiers before storage was measured at all.
	defaults := []quotaTierRow{
		{TierName: "free_trial", MaxRunsPerPeriod: 10, MaxTokensPerPeriod: 100_000, PeriodDays: 30},
		{TierName: "pro", MaxRunsPerPeriod: 1000, MaxTokensPerPeriod: 10_000_000, PeriodDays: 30},
	}
	for _, t := range defaults {
		if err := s.db.WithContext(ctx).Create(&t).Error; err != nil {
			return err
		}
	}
	return nil
}
