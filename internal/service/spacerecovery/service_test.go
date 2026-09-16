package spacerecovery_test

import (
	"context"
	"errors"
	"testing"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/spacerecovery"
)

const space = "sp_1"

func disabled() *time.Time { d := time.Unix(1, 0).UTC(); return &d }

// scenario builds a shared space whose sole owner is disabled and a successor
// who is an enabled member, then lets a test bend one fact.
func scenario() (*mock.MockUserStore, *mock.MockSpaceStore) {
	users := &mock.MockUserStore{ByID: map[string]*coreidentity.User{
		"owner":     {ID: "owner", DisabledAt: disabled()},
		"successor": {ID: "successor"},
	}}
	spaces := &mock.MockSpaceStore{
		Spaces: []corespace.Space{{ID: space}},
		Members: []corespace.Member{
			{SpaceID: space, UserID: "owner", Role: corespace.RoleOwner},
			{SpaceID: space, UserID: "successor", Role: corespace.RoleMember},
		},
	}
	return users, spaces
}

func TestRecoverPromotesSuccessorWhenOwnerDisabled(t *testing.T) {
	users, spaces := scenario()
	svc := &spacerecovery.Service{Spaces: spaces, Users: users}

	demoted, err := svc.RecoverOwnership(context.Background(), spacerecovery.RecoverCmd{SpaceID: space, SuccessorID: "successor"})
	if err != nil {
		t.Fatalf("RecoverOwnership: %v", err)
	}
	if demoted != "owner" {
		t.Errorf("demoted = %q, want owner", demoted)
	}
	if got := corespace.EffectiveRoleOf(spaces.Members, "successor"); got != corespace.RoleOwner {
		t.Errorf("successor role = %q, want owner", got)
	}
	if got := corespace.EffectiveRoleOf(spaces.Members, "owner"); got != corespace.RoleAdmin {
		t.Errorf("demoted owner role = %q, want admin", got)
	}
}

func TestRecoverRefusals(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*mock.MockUserStore, *mock.MockSpaceStore)
		cmd     spacerecovery.RecoverCmd
		wantErr error
	}{
		{
			name:    "space not found",
			mutate:  func(_ *mock.MockUserStore, s *mock.MockSpaceStore) { s.Spaces = nil },
			cmd:     spacerecovery.RecoverCmd{SpaceID: space, SuccessorID: "successor"},
			wantErr: spacerecovery.ErrSpaceNotFound,
		},
		{
			name: "personal space",
			mutate: func(_ *mock.MockUserStore, s *mock.MockSpaceStore) {
				u := "owner"
				s.Spaces[0].PersonalForUserID = &u
			},
			cmd:     spacerecovery.RecoverCmd{SpaceID: space, SuccessorID: "successor"},
			wantErr: spacerecovery.ErrPersonalSpace,
		},
		{
			name:    "an owner can still sign in",
			mutate:  func(u *mock.MockUserStore, _ *mock.MockSpaceStore) { u.ByID["owner"].DisabledAt = nil },
			cmd:     spacerecovery.RecoverCmd{SpaceID: space, SuccessorID: "successor"},
			wantErr: spacerecovery.ErrOwnerStillActive,
		},
		{
			name:    "successor is not a member",
			cmd:     spacerecovery.RecoverCmd{SpaceID: space, SuccessorID: "stranger"},
			wantErr: spacerecovery.ErrSuccessorNotMember,
		},
		{
			name: "successor is disabled",
			mutate: func(u *mock.MockUserStore, _ *mock.MockSpaceStore) {
				u.ByID["successor"].DisabledAt = disabled()
			},
			cmd:     spacerecovery.RecoverCmd{SpaceID: space, SuccessorID: "successor"},
			wantErr: spacerecovery.ErrSuccessorDisabled,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			users, spaces := scenario()
			if tc.mutate != nil {
				tc.mutate(users, spaces)
			}
			svc := &spacerecovery.Service{Spaces: spaces, Users: users}
			_, err := svc.RecoverOwnership(context.Background(), tc.cmd)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
