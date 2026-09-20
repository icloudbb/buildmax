package quota

import (
	"context"
	"errors"
	"testing"
	"time"

	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	corequota "github.com/icloudbb/buildmax/internal/core/quota"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
)

type mockSpaceStore struct {
	space *corespace.Space
	err   error
}

func (m *mockSpaceStore) GetSpace(_ context.Context, _ string) (*corespace.Space, error) {
	return m.space, m.err
}

func (m *mockSpaceStore) GetPersonalSpaceByUser(_ context.Context, _ string) (*corespace.Space, error) {
	return nil, nil
}

func (m *mockSpaceStore) ListSpacesByUser(_ context.Context, _ string) ([]corespace.Space, error) {
	return nil, nil
}

func (m *mockSpaceStore) CreateSpace(_ context.Context, _, _, _ string) (*corespace.Space, error) {
	return nil, nil
}

func (m *mockSpaceStore) AddSpaceMember(_ context.Context, _, _, _ string) (*corespace.Member, error) {
	return nil, nil
}

func (m *mockSpaceStore) RemoveSpaceMember(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockSpaceStore) ListTeamSpaces(_ context.Context, _ string, _, _ int) ([]corespace.Space, int, error) {
	return nil, 0, nil
}

func (m *mockSpaceStore) CountSpaceMembers(_ context.Context, _ []string) (map[string]int, error) {
	return nil, nil
}

func (m *mockSpaceStore) ListSpaceMembers(_ context.Context, _ string) ([]corespace.Member, error) {
	return nil, nil
}

type mockUsageReader struct {
	runCount, totalTokens int
	err                   error
}

func (m *mockUsageReader) SpaceUsageInWindow(_ context.Context, _ string, _, _ time.Time) (runCount, totalTokens int, err error) {
	return m.runCount, m.totalTokens, m.err
}

type mockTierStore struct {
	tier *corequota.Tier
	err  error
}

func (m *mockTierStore) GetQuotaTier(_ context.Context, _ string) (*corequota.Tier, error) {
	return m.tier, m.err
}

func TestCheck_NoSpace_Allows(t *testing.T) {
	c := &Service{
		SpaceStore:  &mockSpaceStore{space: nil},
		UsageReader: &mockUsageReader{},
		TierStore:   &mockTierStore{},
		DefaultTier: "free_trial",
	}
	allowed, _, _ := c.Check(context.Background(), "tm_any", 1, 0)
	if !allowed {
		t.Error("Check: expected allow when space is nil")
	}
}

func TestCheck_UnknownTier_Allows(t *testing.T) {
	c := &Service{
		SpaceStore:  &mockSpaceStore{space: &corespace.Space{ID: "tm_1", QuotaTier: "unknown"}},
		UsageReader: &mockUsageReader{runCount: 0, totalTokens: 0},
		TierStore:   &mockTierStore{tier: nil},
		DefaultTier: "free_trial",
	}
	allowed, _, _ := c.Check(context.Background(), "tm_1", 1, 0)
	if !allowed {
		t.Error("Check: expected allow when tier not found")
	}
}

func TestCheck_RunLimitExceeded_Denies(t *testing.T) {
	c := &Service{
		SpaceStore:  &mockSpaceStore{space: &corespace.Space{ID: "tm_1", QuotaTier: "free_trial"}},
		UsageReader: &mockUsageReader{runCount: 10, totalTokens: 0},
		TierStore:   &mockTierStore{tier: &corequota.Tier{TierName: "free_trial", MaxRunsPerPeriod: 10, MaxTokensPerPeriod: 100000, PeriodDays: 30}},
		DefaultTier: "free_trial",
	}
	allowed, reason, _ := c.Check(context.Background(), "tm_1", 1, 0)
	if allowed {
		t.Error("Check: expected deny when run count would exceed limit")
	}
	if reason != "quota exceeded: run limit" {
		t.Errorf("Check: reason = %q, want quota exceeded: run limit", reason)
	}
}

func TestCheck_TokenLimitExceeded_Denies(t *testing.T) {
	c := &Service{
		SpaceStore:  &mockSpaceStore{space: &corespace.Space{ID: "tm_1", QuotaTier: "free_trial"}},
		UsageReader: &mockUsageReader{runCount: 0, totalTokens: 100000},
		TierStore:   &mockTierStore{tier: &corequota.Tier{TierName: "free_trial", MaxRunsPerPeriod: 10, MaxTokensPerPeriod: 100000, PeriodDays: 30}},
		DefaultTier: "free_trial",
	}
	allowed, reason, _ := c.Check(context.Background(), "tm_1", 0, 1)
	if allowed {
		t.Error("Check: expected deny when token count would exceed limit")
	}
	if reason != "quota exceeded: token limit" {
		t.Errorf("Check: reason = %q, want quota exceeded: token limit", reason)
	}
}

func TestCheck_UnderLimit_Allows(t *testing.T) {
	c := &Service{
		SpaceStore:  &mockSpaceStore{space: &corespace.Space{ID: "tm_1", QuotaTier: "free_trial"}},
		UsageReader: &mockUsageReader{runCount: 5, totalTokens: 50000},
		TierStore:   &mockTierStore{tier: &corequota.Tier{TierName: "free_trial", MaxRunsPerPeriod: 10, MaxTokensPerPeriod: 100000, PeriodDays: 30}},
		DefaultTier: "free_trial",
	}
	allowed, reason, _ := c.Check(context.Background(), "tm_1", 1, 10000)
	if !allowed {
		t.Errorf("Check: expected allow; reason = %q", reason)
	}
	if reason != "" {
		t.Errorf("Check: reason = %q, want empty", reason)
	}
}

func TestCheck_EmptySpaceTier_UsesDefault(t *testing.T) {
	c := &Service{
		SpaceStore:  &mockSpaceStore{space: &corespace.Space{ID: "tm_1", QuotaTier: ""}},
		UsageReader: &mockUsageReader{runCount: 10, totalTokens: 0},
		TierStore:   &mockTierStore{tier: &corequota.Tier{TierName: "free_trial", MaxRunsPerPeriod: 10, MaxTokensPerPeriod: 100000, PeriodDays: 30}},
		DefaultTier: "free_trial",
	}
	allowed, _, _ := c.Check(context.Background(), "tm_1", 1, 0)
	if allowed {
		t.Error("Check: expected deny when default tier limit reached")
	}
}

func (m *mockSpaceStore) SetSpacePluginCuration(_ context.Context, _ string, _ coreplugin.Curation) error {
	return nil
}

func (m *mockSpaceStore) SetSpaceSandboxDefaults(_ context.Context, _, _, _ string) error {
	return nil
}

func (m *mockSpaceStore) SetSpaceAgentInstructions(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockSpaceStore) CreateInvitation(_ context.Context, _, _, _, _ string, _ time.Time) (*corespace.Invitation, error) {
	return nil, nil
}

func (m *mockSpaceStore) GetInvitation(_ context.Context, _ string) (*corespace.Invitation, error) {
	return nil, nil
}

func (m *mockSpaceStore) ListPendingInvitationsBySpace(_ context.Context, _ string, _ time.Time) ([]corespace.Invitation, error) {
	return nil, nil
}

func (m *mockSpaceStore) ListPendingInvitationsByUser(_ context.Context, _ string, _ time.Time) ([]corespace.Invitation, error) {
	return nil, nil
}

func (m *mockSpaceStore) AcceptInvitation(_ context.Context, _ string, _ time.Time) (*corespace.Invitation, error) {
	return nil, nil
}

func (m *mockSpaceStore) RevokeInvitation(_ context.Context, _ string, _ time.Time) error {
	return nil
}

func (m *mockSpaceStore) TransferOwnership(_ context.Context, _, _, _ string) error {
	return nil
}

// A store that cannot answer is not a space without a limit. Every one of these
// used to return "allowed", so a deployment whose database was unreachable
// served unmetered work and recorded nothing about having done so.
func TestCheckReportsAReadItCouldNotMake(t *testing.T) {
	boom := errors.New("database unreachable")
	tier := &corequota.Tier{TierName: "free_trial", MaxRunsPerPeriod: 10, MaxTokensPerPeriod: 100000, PeriodDays: 30}
	cases := []struct {
		name string
		svc  *Service
	}{
		{"space read fails", &Service{
			SpaceStore:  &mockSpaceStore{err: boom},
			UsageReader: &mockUsageReader{},
			TierStore:   &mockTierStore{tier: tier},
			DefaultTier: "free_trial",
		}},
		{"tier read fails", &Service{
			SpaceStore:  &mockSpaceStore{space: &corespace.Space{ID: "tm_1", QuotaTier: "free_trial"}},
			UsageReader: &mockUsageReader{},
			TierStore:   &mockTierStore{err: boom},
			DefaultTier: "free_trial",
		}},
		{"usage aggregation fails", &Service{
			SpaceStore:  &mockSpaceStore{space: &corespace.Space{ID: "tm_1", QuotaTier: "free_trial"}},
			UsageReader: &mockUsageReader{err: boom},
			TierStore:   &mockTierStore{tier: tier},
			DefaultTier: "free_trial",
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			allowed, _, err := c.svc.Check(context.Background(), "tm_1", 1, 0)
			if !errors.Is(err, boom) {
				t.Fatalf("Check err = %v, want %v", err, boom)
			}
			if allowed {
				t.Error("Check admitted a run whose limit it could not read")
			}
		})
	}
}

// GetUsage has the same failure: a zeroed snapshot reads as "used nothing".
func TestGetUsageReportsAReadItCouldNotMake(t *testing.T) {
	boom := errors.New("database unreachable")
	c := &Service{
		SpaceStore:  &mockSpaceStore{err: boom},
		UsageReader: &mockUsageReader{},
		TierStore:   &mockTierStore{},
		DefaultTier: "free_trial",
	}
	info, err := c.GetUsage(context.Background(), "tm_1")
	if !errors.Is(err, boom) {
		t.Fatalf("GetUsage err = %v, want %v", err, boom)
	}
	if info != nil {
		t.Errorf("GetUsage returned a snapshot alongside an error: %+v", info)
	}
}
