package eligibility_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/core/eligibility"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/mock"
)

// erroringUsers turns GetUser into a failure so the fail-closed path is testable;
// the embedded store supplies the rest of the interface.
type erroringUsers struct {
	*mock.MockUserStore
	err error
}

func (e erroringUsers) GetUser(context.Context, string) (*coreidentity.User, error) {
	return nil, e.err
}

// erroringSpaces turns ListSpaceMembers into a failure for the same reason.
type erroringSpaces struct {
	*mock.MockSpaceStore
	err error
}

func (e erroringSpaces) ListSpaceMembers(context.Context, string) ([]corespace.Member, error) {
	return nil, e.err
}

func TestCheck(t *testing.T) {
	const (
		userID  = "u-1"
		spaceID = "s-1"
	)
	disabledAt := time.Unix(1000, 0)

	enabledUsers := func() *mock.MockUserStore {
		return &mock.MockUserStore{ByID: map[string]*coreidentity.User{
			userID: {ID: userID, Email: "a@b.c"},
		}}
	}
	memberSpaces := func() *mock.MockSpaceStore {
		return &mock.MockSpaceStore{Members: []corespace.Member{
			{SpaceID: spaceID, UserID: userID, Role: corespace.RoleMember},
		}}
	}

	tests := []struct {
		name    string
		users   coreidentity.UserStore
		spaces  corespace.Store
		wantErr error
	}{
		{
			name:    "enabled member is eligible",
			users:   enabledUsers(),
			spaces:  memberSpaces(),
			wantErr: nil,
		},
		{
			name: "disabled account is refused",
			users: &mock.MockUserStore{ByID: map[string]*coreidentity.User{
				userID: {ID: userID, DisabledAt: &disabledAt},
			}},
			spaces:  memberSpaces(),
			wantErr: eligibility.ErrAccountDisabled,
		},
		{
			name:    "missing account is refused like disabled",
			users:   &mock.MockUserStore{ByID: map[string]*coreidentity.User{}},
			spaces:  memberSpaces(),
			wantErr: eligibility.ErrAccountDisabled,
		},
		{
			name:    "enabled non-member is refused",
			users:   enabledUsers(),
			spaces:  &mock.MockSpaceStore{},
			wantErr: eligibility.ErrNotSpaceMember,
		},
		{
			name:    "user store failure fails closed",
			users:   erroringUsers{MockUserStore: enabledUsers(), err: errors.New("db down")},
			spaces:  memberSpaces(),
			wantErr: eligibility.ErrUnavailable,
		},
		{
			name:    "space store failure fails closed",
			users:   enabledUsers(),
			spaces:  erroringSpaces{MockSpaceStore: memberSpaces(), err: errors.New("db down")},
			wantErr: eligibility.ErrUnavailable,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := eligibility.New(tc.users, tc.spaces).Check(context.Background(), userID, spaceID)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("Check() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Check() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
