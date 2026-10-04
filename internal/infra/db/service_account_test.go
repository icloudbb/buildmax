package db

import (
	"context"
	"errors"
	"testing"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
)

var _ coreidentity.ServiceAccountStore = (*Store)(nil)

// TestServiceAccountStore covers the row shape a service account depends on:
// no email (several may coexist under the unique index), membership in exactly
// the Space it was created in, no personal Space, and updates that never reach
// a person.
func TestServiceAccountStore(t *testing.T) {
	s, ctx := openUserAdminStore(t)
	owner := newTestUser(t, s, "sa-owner")
	other := newTestUser(t, s, "sa-other")
	spaceID := newTestSpace(t, s, owner)
	otherSpace := newTestSpace(t, s, other)

	create := func(name string) *coreidentity.User {
		t.Helper()
		u, err := s.CreateServiceAccount(ctx, coreidentity.NewServiceAccount{
			SpaceID: spaceID, Name: name, SponsorUserID: owner,
		})
		if err != nil {
			t.Fatalf("CreateServiceAccount(%s): %v", name, err)
		}
		t.Cleanup(func() { deleteTestUser(t, s, u.ID) })
		return u
	}
	first := create("HR operations")
	second := create("Audit bot")

	got, err := s.GetUser(ctx, first.ID)
	if err != nil || got == nil {
		t.Fatalf("GetUser: %+v, %v", got, err)
	}
	if !got.IsService() || got.Email != "" || got.HasPassword || got.Name != "HR operations" {
		t.Fatalf("service account read back as %+v", got)
	}
	if got.SponsorUserID == nil || *got.SponsorUserID != owner {
		t.Fatalf("sponsor = %v, want %s", got.SponsorUserID, owner)
	}
	if personal, err := s.GetPersonalSpaceByUser(ctx, first.ID); err != nil || personal != nil {
		t.Fatalf("service account has a personal space: %+v, %v", personal, err)
	}
	spaces, err := s.ListSpacesByUser(ctx, first.ID)
	if err != nil || len(spaces) != 1 || spaces[0].ID != spaceID {
		t.Fatalf("service account spaces = %+v, %v; want only %s", spaces, err, spaceID)
	}
	members, err := s.ListSpaceMembers(ctx, spaceID)
	if err != nil {
		t.Fatal(err)
	}
	if role := corespace.EffectiveRoleOf(members, first.ID); role != corespace.RoleMember {
		t.Fatalf("service account role = %q, want member", role)
	}

	list, err := s.ListServiceAccountsBySpace(ctx, spaceID)
	if err != nil || len(list) != 2 || list[0].ID != first.ID || list[1].ID != second.ID {
		t.Fatalf("ListServiceAccountsBySpace = %+v, %v", list, err)
	}
	if list, err := s.ListServiceAccountsBySpace(ctx, otherSpace); err != nil || len(list) != 0 {
		t.Fatalf("another space lists %+v, %v", list, err)
	}

	name := "People operations"
	if err := s.UpdateServiceAccount(ctx, coreidentity.ServiceAccountUpdate{
		UserID: first.ID, Name: &name, SponsorUserID: &other,
	}); err != nil {
		t.Fatalf("UpdateServiceAccount: %v", err)
	}
	got, _ = s.GetUser(ctx, first.ID)
	if got.Name != name || got.SponsorUserID == nil || *got.SponsorUserID != other {
		t.Fatalf("after update: %+v", got)
	}

	// A person is never changed through the service-account path.
	if err := s.UpdateServiceAccount(ctx, coreidentity.ServiceAccountUpdate{
		UserID: owner, Name: &name,
	}); !errors.Is(err, coreidentity.ErrUserNotFound) {
		t.Fatalf("updating a person: %v, want ErrUserNotFound", err)
	}

	// The admin list carries the kind, and a person reads back as human.
	page, _, err := s.ListUsers(ctx, coreidentity.UserFilter{}, 200, 0)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{}
	for _, u := range page {
		kinds[u.ID] = u.Kind
	}
	if kinds[second.ID] != coreidentity.KindService || kinds[owner] != coreidentity.KindHuman {
		t.Fatalf("kinds = service:%q owner:%q", kinds[second.ID], kinds[owner])
	}
	if u, err := s.UserByEmail(context.Background(), ""); err != nil || u != nil {
		t.Fatalf("an empty email resolved to %+v, %v", u, err)
	}
}
