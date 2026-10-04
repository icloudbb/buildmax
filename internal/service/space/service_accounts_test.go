package space_test

import (
	"context"
	"errors"
	"testing"
	"time"

	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/accountlifecycle"
	"github.com/icloudbb/buildmax/internal/service/space"
	"github.com/icloudbb/buildmax/internal/util"
)

// newServiceAccountSpace is newSpace with service accounts wired, plus an admin.
func newServiceAccountSpace(t *testing.T) (s *space.Service, spaceID, ownerID, adminID, memberID string) {
	t.Helper()
	s, spaceID, ownerID, memberID = newSpace(t)
	users := s.Users.(*mock.MockUserStore)
	spaces := s.Spaces.(*mock.MockSpaceStore)
	s.ServiceAccounts = &mock.MockServiceAccountStore{Users: users, Spaces: spaces}
	s.Lifecycle = &accountlifecycle.Service{Users: users}
	adminID = addMemberWithRole(t, s, spaceID, "admin@example.com", corespace.RoleAdmin)
	return s, spaceID, ownerID, adminID, memberID
}

func createServiceAccount(t *testing.T, s *space.Service, spaceID, actorID string) *space.ServiceAccount {
	t.Helper()
	a, err := s.CreateServiceAccount(context.Background(), space.CreateServiceAccountCmd{
		SpaceID: spaceID, ActorID: actorID, Name: "HR operations",
	})
	if err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}
	return a
}

func TestServiceAccountLifecycle(t *testing.T) {
	ctx := context.Background()
	s, spaceID, ownerID, adminID, memberID := newServiceAccountSpace(t)

	// A member may read the inventory but not manage it.
	if _, err := s.CreateServiceAccount(ctx, space.CreateServiceAccountCmd{
		SpaceID: spaceID, ActorID: memberID, Name: "x",
	}); !errors.Is(err, space.ErrOnlyOwnerOrAdminCanManageServiceAccounts) {
		t.Fatalf("member create: %v", err)
	}

	a := createServiceAccount(t, s, spaceID, adminID)
	if !a.User.IsService() || a.User.SponsorUserID == nil || *a.User.SponsorUserID != adminID {
		t.Fatalf("created %+v; the sponsor defaults to the caller", a.User)
	}
	members, _ := s.Spaces.ListSpaceMembers(ctx, spaceID)
	if role := corespace.EffectiveRoleOf(members, a.User.ID); role != corespace.RoleMember {
		t.Fatalf("role = %q, want member", role)
	}

	name := "People operations"
	got, err := s.UpdateServiceAccount(ctx, space.UpdateServiceAccountCmd{
		SpaceID: spaceID, ActorID: ownerID, UserID: a.User.ID, Name: &name, SponsorUserID: &ownerID,
	})
	if err != nil || got.User.Name != name || *got.User.SponsorUserID != ownerID || got.NeedsSponsor {
		t.Fatalf("rename and re-sponsor = %+v, %v", got, err)
	}

	got, err = s.SetServiceAccountState(ctx, space.SetServiceAccountStateCmd{
		SpaceID: spaceID, ActorID: adminID, UserID: a.User.ID, Disabled: true,
	})
	if err != nil || !got.User.Disabled() {
		t.Fatalf("disable = %+v, %v", got, err)
	}
	got, err = s.SetServiceAccountState(ctx, space.SetServiceAccountStateCmd{
		SpaceID: spaceID, ActorID: ownerID, UserID: a.User.ID, Disabled: false,
	})
	if err != nil || got.User.Disabled() {
		t.Fatalf("re-enable = %+v, %v", got, err)
	}

	list, err := s.ListServiceAccounts(ctx, spaceID)
	if err != nil || len(list) != 1 || list[0].User.ID != a.User.ID {
		t.Fatalf("ListServiceAccounts = %+v, %v", list, err)
	}

	// A person is not reachable through these routes.
	if _, err := s.SetServiceAccountState(ctx, space.SetServiceAccountStateCmd{
		SpaceID: spaceID, ActorID: ownerID, UserID: memberID, Disabled: true,
	}); !errors.Is(err, space.ErrServiceAccountNotFound) {
		t.Fatalf("disabling a person through the service-account route: %v", err)
	}
}

func TestServiceAccountRefusals(t *testing.T) {
	ctx := context.Background()
	s, spaceID, ownerID, adminID, memberID := newServiceAccountSpace(t)

	cases := []struct {
		name string
		cmd  space.CreateServiceAccountCmd
		want error
	}{
		{"blank name", space.CreateServiceAccountCmd{SpaceID: spaceID, ActorID: ownerID, Name: "  "}, space.ErrServiceAccountNameRequired},
		{"member sponsor", space.CreateServiceAccountCmd{SpaceID: spaceID, ActorID: ownerID, Name: "x", SponsorUserID: memberID}, space.ErrInvalidSponsor},
		{"outsider sponsor", space.CreateServiceAccountCmd{SpaceID: spaceID, ActorID: ownerID, Name: "x", SponsorUserID: "u_nobody"}, space.ErrInvalidSponsor},
	}
	for _, tc := range cases {
		if _, err := s.CreateServiceAccount(ctx, tc.cmd); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}

	t.Run("personal space", func(t *testing.T) {
		spaces := s.Spaces.(*mock.MockSpaceStore)
		spaces.Spaces = append(spaces.Spaces, corespace.Space{ID: "tm_personal", PersonalForUserID: util.Ptr(ownerID)})
		spaces.Members = append(spaces.Members, corespace.Member{SpaceID: "tm_personal", UserID: ownerID, Role: corespace.RoleOwner})
		if _, err := s.CreateServiceAccount(ctx, space.CreateServiceAccountCmd{
			SpaceID: "tm_personal", ActorID: ownerID, Name: "x",
		}); !errors.Is(err, space.ErrServiceAccountPersonalSpace) {
			t.Fatalf("err = %v, want ErrServiceAccountPersonalSpace", err)
		}
	})

	a := createServiceAccount(t, s, spaceID, adminID)

	t.Run("promotion and transfer", func(t *testing.T) {
		for _, role := range []string{corespace.RoleAdmin, corespace.RoleOwner, corespace.RoleMember} {
			if err := s.SetMemberRole(ctx, space.SetMemberRoleCmd{
				SpaceID: spaceID, ActorID: ownerID, TargetUserID: a.User.ID, Role: role,
			}); !errors.Is(err, space.ErrServiceAccountMembershipFixed) {
				t.Errorf("set role %s: %v", role, err)
			}
		}
	})
	t.Run("removal", func(t *testing.T) {
		if err := s.RemoveMember(ctx, space.RemoveMemberCmd{
			SpaceID: spaceID, ActorID: ownerID, TargetUserID: a.User.ID,
		}); !errors.Is(err, space.ErrServiceAccountMembershipFixed) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("login code", func(t *testing.T) {
		s.LoginCodes = &mock.MockLoginCodeStore{}
		if _, _, err := s.IssueMemberLoginCode(ctx, space.IssueMemberLoginCodeCmd{
			SpaceID: spaceID, ActorID: ownerID, TargetUserID: a.User.ID,
		}); !errors.Is(err, space.ErrServiceAccountNoSignIn) {
			t.Fatalf("err = %v", err)
		}
	})
}

// TestServiceAccountsStayInTheirSpace is the cross-Space isolation the record
// requires: another Space cannot see, manage, or invite one.
func TestServiceAccountsStayInTheirSpace(t *testing.T) {
	ctx := context.Background()
	s, spaceID, _, adminID, _ := newServiceAccountSpace(t)
	a := createServiceAccount(t, s, spaceID, adminID)

	users := s.Users.(*mock.MockUserStore)
	spaces := s.Spaces.(*mock.MockSpaceStore)
	other, err := users.CreateUser(ctx, "other@example.com", "free")
	if err != nil {
		t.Fatal(err)
	}
	otherSpace, err := spaces.CreateSpace(ctx, "elsewhere", other.ID, "free")
	if err != nil {
		t.Fatal(err)
	}

	if list, err := s.ListServiceAccounts(ctx, otherSpace.ID); err != nil || len(list) != 0 {
		t.Fatalf("another space lists %+v, %v", list, err)
	}
	name := "stolen"
	if _, err := s.UpdateServiceAccount(ctx, space.UpdateServiceAccountCmd{
		SpaceID: otherSpace.ID, ActorID: other.ID, UserID: a.User.ID, Name: &name,
	}); !errors.Is(err, space.ErrServiceAccountNotFound) {
		t.Fatalf("rename from another space: %v", err)
	}
	if _, err := s.SetServiceAccountState(ctx, space.SetServiceAccountStateCmd{
		SpaceID: otherSpace.ID, ActorID: other.ID, UserID: a.User.ID, Disabled: true,
	}); !errors.Is(err, space.ErrServiceAccountNotFound) {
		t.Fatalf("disable from another space: %v", err)
	}
	// Invitation is by email, which a real service account lacks; one given an
	// address anyway is still refused.
	u := users.ByID[a.User.ID]
	u.Email = "svc@example.com"
	users.ByEmail[u.Email] = u
	if _, _, err := s.InviteMember(ctx, space.InviteMemberCmd{
		SpaceID: otherSpace.ID, ActorID: other.ID, Email: u.Email,
	}); !errors.Is(err, space.ErrServiceAccountCannotJoin) {
		t.Fatalf("invite into another space: %v", err)
	}
	if got, _ := spaces.ListSpacesByUser(ctx, a.User.ID); len(got) != 1 || got[0].ID != spaceID {
		t.Fatalf("service account spaces = %+v", got)
	}
}

// TestSponsorNeeded: the state appears when the sponsor is demoted, removed,
// or disabled, and clears when an owner or admin takes sponsorship.
func TestSponsorNeeded(t *testing.T) {
	ctx := context.Background()
	needs := func(t *testing.T, s *space.Service, spaceID string) bool {
		t.Helper()
		list, err := s.ListServiceAccounts(ctx, spaceID)
		if err != nil || len(list) != 1 {
			t.Fatalf("ListServiceAccounts = %+v, %v", list, err)
		}
		return list[0].NeedsSponsor
	}

	for _, tc := range []struct {
		name   string
		depart func(s *space.Service, spaceID, ownerID, adminID string) error
	}{
		{"demoted", func(s *space.Service, spaceID, ownerID, adminID string) error {
			return s.SetMemberRole(ctx, space.SetMemberRoleCmd{
				SpaceID: spaceID, ActorID: ownerID, TargetUserID: adminID, Role: corespace.RoleMember,
			})
		}},
		{"removed", func(s *space.Service, spaceID, ownerID, adminID string) error {
			return s.RemoveMember(ctx, space.RemoveMemberCmd{SpaceID: spaceID, ActorID: ownerID, TargetUserID: adminID})
		}},
		{"disabled", func(s *space.Service, _, _, adminID string) error {
			s.Users.(*mock.MockUserStore).DisableForTest(adminID, time.Now())
			return nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, spaceID, ownerID, adminID, _ := newServiceAccountSpace(t)
			a := createServiceAccount(t, s, spaceID, adminID)
			if needs(t, s, spaceID) {
				t.Fatal("a fresh account with an admin sponsor needs a sponsor")
			}
			if err := tc.depart(s, spaceID, ownerID, adminID); err != nil {
				t.Fatal(err)
			}
			if !needs(t, s, spaceID) {
				t.Fatal("the sponsor left but the account does not need one")
			}
			// Nothing else stops: the account itself stays enabled.
			if u, _ := s.Users.GetUser(ctx, a.User.ID); u.Disabled() {
				t.Fatal("sponsor departure disabled the account")
			}
			got, err := s.UpdateServiceAccount(ctx, space.UpdateServiceAccountCmd{
				SpaceID: spaceID, ActorID: ownerID, UserID: a.User.ID, SponsorUserID: &ownerID,
			})
			if err != nil || got.NeedsSponsor || needs(t, s, spaceID) {
				t.Fatalf("taking sponsorship = %+v, %v; want it cleared", got, err)
			}
		})
	}
}

// TestServiceAccountCannotSponsor keeps accountability with a person.
func TestServiceAccountCannotSponsor(t *testing.T) {
	ctx := context.Background()
	s, spaceID, ownerID, adminID, _ := newServiceAccountSpace(t)
	a := createServiceAccount(t, s, spaceID, adminID)
	// Seat it as admin directly; the service would never allow it.
	_, _ = s.Spaces.AddSpaceMember(ctx, spaceID, a.User.ID, corespace.RoleAdmin)
	if _, err := s.CreateServiceAccount(ctx, space.CreateServiceAccountCmd{
		SpaceID: spaceID, ActorID: ownerID, Name: "x", SponsorUserID: a.User.ID,
	}); !errors.Is(err, space.ErrInvalidSponsor) {
		t.Fatalf("err = %v, want ErrInvalidSponsor", err)
	}
}
