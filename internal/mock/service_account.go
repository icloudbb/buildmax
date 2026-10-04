package mock

import (
	"context"
	"fmt"
	"sort"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
)

// MockServiceAccountStore is an in-memory ServiceAccountStore over the user and
// space doubles, so an account it creates is visible to GetUser and
// ListSpaceMembers the way the real store's one transaction makes it.
type MockServiceAccountStore struct {
	Users  *MockUserStore
	Spaces *MockSpaceStore
}

func (m *MockServiceAccountStore) CreateServiceAccount(ctx context.Context, in coreidentity.NewServiceAccount) (*coreidentity.User, error) {
	if m.Users.ByID == nil {
		m.Users.ByID = make(map[string]*coreidentity.User)
	}
	m.Users.NextUserID++
	sponsor := in.SponsorUserID
	u := &coreidentity.User{
		ID:            fmt.Sprintf("mock-sa-%d", m.Users.NextUserID),
		Name:          in.Name,
		Kind:          coreidentity.KindService,
		SponsorUserID: &sponsor,
		CreatedAt:     time.Now().UTC(),
	}
	m.Users.ByID[u.ID] = u
	if _, err := m.Spaces.AddSpaceMember(ctx, in.SpaceID, u.ID, corespace.RoleMember); err != nil {
		return nil, err
	}
	out := *u
	return &out, nil
}

func (m *MockServiceAccountStore) ListServiceAccountsBySpace(ctx context.Context, spaceID string) ([]coreidentity.User, error) {
	members, err := m.Spaces.ListSpaceMembers(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	var out []coreidentity.User
	for _, member := range members {
		if u := m.Users.ByID[member.UserID]; u != nil && u.IsService() {
			out = append(out, *u)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (m *MockServiceAccountStore) UpdateServiceAccount(_ context.Context, in coreidentity.ServiceAccountUpdate) error {
	u := m.Users.ByID[in.UserID]
	if u == nil || !u.IsService() {
		return coreidentity.ErrUserNotFound
	}
	if in.Name != nil {
		u.Name = *in.Name
	}
	if in.SponsorUserID != nil {
		sponsor := *in.SponsorUserID
		u.SponsorUserID = &sponsor
	}
	return nil
}
