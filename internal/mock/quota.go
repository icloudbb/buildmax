package mock

import (
	"context"
	"time"

	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	corequota "github.com/icloudbb/buildmax/internal/core/quota"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
)

// MockUsageReader returns fixed run count and token total for SpaceUsageInWindow.
type MockUsageReader struct {
	RunCount    int
	TotalTokens int
	Err         error
}

func (m *MockUsageReader) SpaceUsageInWindow(_ context.Context, _ string, _, _ time.Time) (int, int, error) {
	if m.Err != nil {
		return 0, 0, m.Err
	}
	return m.RunCount, m.TotalTokens, nil
}

// MockTierStore returns a fixed tier for GetQuotaTier.
type MockTierStore struct {
	Tier *corequota.Tier
	Err  error
}

func (m *MockTierStore) GetQuotaTier(_ context.Context, _ string) (*corequota.Tier, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Tier, nil
}

// DenyQuotaSpaceStore is used by quota 429 tests to supply a space with tier.
type DenyQuotaSpaceStore struct {
	Space *corespace.Space
}

func (d *DenyQuotaSpaceStore) GetSpace(_ context.Context, _ string) (*corespace.Space, error) {
	return d.Space, nil
}

func (d *DenyQuotaSpaceStore) GetPersonalSpaceByUser(_ context.Context, _ string) (*corespace.Space, error) {
	return d.Space, nil
}

func (d *DenyQuotaSpaceStore) ListSpacesByUser(_ context.Context, _ string) ([]corespace.Space, error) {
	if d.Space == nil {
		return nil, nil
	}
	return []corespace.Space{*d.Space}, nil
}

func (d *DenyQuotaSpaceStore) CreateSpace(_ context.Context, _, _, _ string) (*corespace.Space, error) {
	return nil, nil
}

func (d *DenyQuotaSpaceStore) AddSpaceMember(_ context.Context, _, _, _ string) (*corespace.Member, error) {
	return nil, nil
}

func (d *DenyQuotaSpaceStore) RemoveSpaceMember(_ context.Context, _, _ string) error {
	return nil
}

func (d *DenyQuotaSpaceStore) ListSpaceMembers(_ context.Context, _ string) ([]corespace.Member, error) {
	return nil, nil
}

// DenyQuotaUsageReader is used by quota 429 tests.
type DenyQuotaUsageReader struct {
	RunCount    int
	TotalTokens int
}

func (d *DenyQuotaUsageReader) SpaceUsageInWindow(_ context.Context, _ string, _, _ time.Time) (int, int, error) {
	return d.RunCount, d.TotalTokens, nil
}

// DenyQuotaTierStore is used by quota 429 tests.
type DenyQuotaTierStore struct {
	Tier *corequota.Tier
}

func (d *DenyQuotaTierStore) GetQuotaTier(_ context.Context, _ string) (*corequota.Tier, error) {
	return d.Tier, nil
}

func (d *DenyQuotaSpaceStore) ListTeamSpaces(_ context.Context, _ string, _, _ int) ([]corespace.Space, int, error) {
	if d.Space == nil {
		return nil, 0, nil
	}
	return []corespace.Space{*d.Space}, 1, nil
}

func (d *DenyQuotaSpaceStore) CountSpaceMembers(_ context.Context, _ []string) (map[string]int, error) {
	return nil, nil
}

func (d *DenyQuotaSpaceStore) SetSpacePluginCuration(_ context.Context, _ string, _ coreplugin.Curation) error {
	return nil
}

func (d *DenyQuotaSpaceStore) SetSpaceSandboxDefaults(_ context.Context, _, _, _ string) error {
	return nil
}

func (d *DenyQuotaSpaceStore) SetSpaceAgentInstructions(_ context.Context, _, _ string) error {
	return nil
}

func (d *DenyQuotaSpaceStore) CreateInvitation(_ context.Context, _, _, _, _ string, _ time.Time) (*corespace.Invitation, error) {
	return nil, nil
}

func (d *DenyQuotaSpaceStore) GetInvitation(_ context.Context, _ string) (*corespace.Invitation, error) {
	return nil, nil
}

func (d *DenyQuotaSpaceStore) ListPendingInvitationsBySpace(_ context.Context, _ string, _ time.Time) ([]corespace.Invitation, error) {
	return nil, nil
}

func (d *DenyQuotaSpaceStore) ListPendingInvitationsByUser(_ context.Context, _ string, _ time.Time) ([]corespace.Invitation, error) {
	return nil, nil
}

func (d *DenyQuotaSpaceStore) AcceptInvitation(_ context.Context, _ string, _ time.Time) (*corespace.Invitation, error) {
	return nil, nil
}

func (d *DenyQuotaSpaceStore) RevokeInvitation(_ context.Context, _ string, _ time.Time) error {
	return nil
}

func (d *DenyQuotaSpaceStore) TransferOwnership(_ context.Context, _, _, _ string) error {
	return nil
}
