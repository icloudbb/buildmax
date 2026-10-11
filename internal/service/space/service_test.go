package space_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	"github.com/icloudbb/buildmax/internal/core/apierr"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/space"
)

func TestSpaceInstructionsShareTheAgentPromptBudget(t *testing.T) {
	s, spaceID, ownerID, _ := newSpace(t)
	s.Agents = &mock.MockAgentStore{Agents: []agentdef.Agent{{
		ID: "ag_1", SpaceID: spaceID, Name: "writer", Instructions: strings.Repeat("a", 4097),
	}}}
	err := s.SetAgentInstructions(context.Background(), space.SetAgentInstructionsCmd{
		SpaceID: spaceID, ActorID: ownerID, Instructions: strings.Repeat("s", 4096),
	})
	if !errors.Is(err, space.ErrAgentInstructionsTooLong) {
		t.Fatalf("err = %v, want ErrAgentInstructionsTooLong", err)
	}
}

// owner plus one member, which is the shape every rule here is about.
func newSpace(t *testing.T) (*space.Service, string, string, string) {
	t.Helper()
	spaces := &mock.MockSpaceStore{}
	users := &mock.MockUserStore{}
	ctx := context.Background()

	owner, err := users.CreateUser(ctx, "owner@example.com", "free")
	if err != nil {
		t.Fatalf("CreateUser owner: %v", err)
	}
	member, err := users.CreateUser(ctx, "member@example.com", "free")
	if err != nil {
		t.Fatalf("CreateUser member: %v", err)
	}
	tm, err := spaces.CreateSpace(ctx, "acme", owner.ID, "free")
	if err != nil {
		t.Fatalf("CreateSpace: %v", err)
	}
	if _, err := spaces.AddSpaceMember(ctx, tm.ID, member.ID, corespace.RoleMember); err != nil {
		t.Fatalf("AddSpaceMember: %v", err)
	}
	return &space.Service{Spaces: spaces, Users: users}, tm.ID, owner.ID, member.ID
}

// addMemberWithRole seats an account in the space at a role the service itself
// will not grant through InviteMember, which is the only way to test what
// that role may do.
func addMemberWithRole(t *testing.T, s *space.Service, spaceID, email, role string) string {
	t.Helper()
	ctx := context.Background()
	users, ok := s.Users.(*mock.MockUserStore)
	if !ok {
		t.Fatalf("fixture user store is %T", s.Users)
	}
	spaces, ok := s.Spaces.(*mock.MockSpaceStore)
	if !ok {
		t.Fatalf("fixture space store is %T", s.Spaces)
	}
	user, err := users.CreateUser(ctx, email, "free")
	if err != nil {
		t.Fatalf("CreateUser %s: %v", email, err)
	}
	if _, err := spaces.AddSpaceMember(ctx, spaceID, user.ID, role); err != nil {
		t.Fatalf("AddSpaceMember %s: %v", role, err)
	}
	return user.ID
}

// createAccount registers an account with no space, which is InviteMember's
// precondition: the address must already exist.
func createAccount(t *testing.T, s *space.Service, email string) string {
	t.Helper()
	users, ok := s.Users.(*mock.MockUserStore)
	if !ok {
		t.Fatalf("fixture user store is %T", s.Users)
	}
	user, err := users.CreateUser(context.Background(), email, "free")
	if err != nil {
		t.Fatalf("CreateUser %s: %v", email, err)
	}
	return user.ID
}

// The owner check existed twice, once per mutating handler. These two cases are
// what those copies were each meant to enforce.
func TestOnlyOwnersMayInviteOrRemove(t *testing.T) {
	s, spaceID, _, memberID := newSpace(t)
	ctx := context.Background()
	createAccount(t, s, "new@example.com")

	_, _, err := s.InviteMember(ctx, space.InviteMemberCmd{SpaceID: spaceID, ActorID: memberID, Email: "new@example.com"})
	if !errors.Is(err, space.ErrOnlyOwnerOrAdminCanInvite) {
		t.Errorf("invite by a member: %v, want ErrOnlyOwnerOrAdminCanInvite", err)
	}
	if kind, _ := apierr.KindOf(err); kind != apierr.KindForbidden {
		t.Errorf("kind = %q, want forbidden", kind)
	}

	err = s.RemoveMember(ctx, space.RemoveMemberCmd{SpaceID: spaceID, ActorID: memberID, TargetUserID: memberID})
	if !errors.Is(err, space.ErrOnlyOwnerCanRemove) {
		t.Errorf("remove by a member: %v, want ErrOnlyOwnerCanRemove", err)
	}
}

// TestAdminMayInviteAtMemberRoleOnly is the half the role matrix in core/space
// cannot show on its own: Allows answers about the caller's own role, not
// about the role they are trying to grant. An admin holds ActionInviteSpaceMember
// but the service still refuses the one escalation it would otherwise permit --
// staffing the space with a peer admin.
func TestAdminMayInviteAtMemberRoleOnly(t *testing.T) {
	s, spaceID, _, _ := newSpace(t)
	ctx := context.Background()
	adminID := addMemberWithRole(t, s, spaceID, "admin@example.com", corespace.RoleAdmin)
	createAccount(t, s, "member2@example.com")
	createAccount(t, s, "admin2@example.com")

	inv, _, err := s.InviteMember(ctx, space.InviteMemberCmd{SpaceID: spaceID, ActorID: adminID, Email: "member2@example.com"})
	if err != nil {
		t.Fatalf("admin inviting at member role: %v", err)
	}
	if inv.Role != corespace.RoleMember {
		t.Errorf("role = %q, want member", inv.Role)
	}

	_, _, err = s.InviteMember(ctx, space.InviteMemberCmd{
		SpaceID: spaceID, ActorID: adminID, Email: "admin2@example.com", Role: corespace.RoleAdmin,
	})
	if !errors.Is(err, space.ErrOnlyOwnerCanInviteAdmin) {
		t.Errorf("admin inviting at admin role: %v, want ErrOnlyOwnerCanInviteAdmin", err)
	}
}

// An owner may invite at either grantable role.
func TestOwnerMayInviteAtEitherRole(t *testing.T) {
	s, spaceID, ownerID, _ := newSpace(t)
	ctx := context.Background()
	createAccount(t, s, "future-admin@example.com")

	inv, _, err := s.InviteMember(ctx, space.InviteMemberCmd{
		SpaceID: spaceID, ActorID: ownerID, Email: "future-admin@example.com", Role: corespace.RoleAdmin,
	})
	if err != nil {
		t.Fatalf("owner inviting at admin role: %v", err)
	}
	if inv.Role != corespace.RoleAdmin {
		t.Errorf("role = %q, want admin", inv.Role)
	}
}

// A stranger is refused for the same reason a member is: not an owner or
// admin here.
func TestNonMemberMayNotInvite(t *testing.T) {
	s, spaceID, _, _ := newSpace(t)
	createAccount(t, s, "new@example.com")

	_, _, err := s.InviteMember(context.Background(), space.InviteMemberCmd{
		SpaceID: spaceID, ActorID: "u_stranger", Email: "new@example.com",
	})

	if !errors.Is(err, space.ErrOnlyOwnerOrAdminCanInvite) {
		t.Errorf("err = %v, want ErrOnlyOwnerOrAdminCanInvite", err)
	}
}

func TestOwnerCannotRemoveThemselves(t *testing.T) {
	s, spaceID, ownerID, _ := newSpace(t)

	err := s.RemoveMember(context.Background(), space.RemoveMemberCmd{
		SpaceID: spaceID, ActorID: ownerID, TargetUserID: ownerID,
	})

	if !errors.Is(err, space.ErrCannotRemoveSelf) {
		t.Fatalf("err = %v, want ErrCannotRemoveSelf", err)
	}
}

func TestRemovingSomeoneNotInTheSpaceIsNotFound(t *testing.T) {
	s, spaceID, ownerID, _ := newSpace(t)

	err := s.RemoveMember(context.Background(), space.RemoveMemberCmd{
		SpaceID: spaceID, ActorID: ownerID, TargetUserID: "u_absent",
	})

	if !errors.Is(err, space.ErrMemberNotFound) {
		t.Fatalf("err = %v, want ErrMemberNotFound", err)
	}
	if kind, _ := apierr.KindOf(err); kind != apierr.KindNotFound {
		t.Errorf("kind = %q, want not_found", kind)
	}
}

func TestOwnerRemovesAMember(t *testing.T) {
	s, spaceID, ownerID, memberID := newSpace(t)
	ctx := context.Background()

	if err := s.RemoveMember(ctx, space.RemoveMemberCmd{SpaceID: spaceID, ActorID: ownerID, TargetUserID: memberID}); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}

	members, err := s.ListMembers(ctx, spaceID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	for _, m := range members {
		if m.Membership.UserID == memberID {
			t.Fatal("the member is still on the roster")
		}
	}
}

// Only member and admin are grantable: owner moves through SetMemberRole
// instead of an invitation.
func TestOwnerRoleIsNotGrantableThroughInvitation(t *testing.T) {
	s, spaceID, ownerID, _ := newSpace(t)
	createAccount(t, s, "member@example2.com")

	_, _, err := s.InviteMember(context.Background(), space.InviteMemberCmd{
		SpaceID: spaceID, ActorID: ownerID, Email: "member@example2.com", Role: corespace.RoleOwner,
	})
	if !errors.Is(err, space.ErrUnsupportedRole) {
		t.Errorf("role owner: %v, want ErrUnsupportedRole", err)
	}
}

func TestInviteRequiresAnExistingAccount(t *testing.T) {
	s, spaceID, ownerID, _ := newSpace(t)
	ctx := context.Background()

	_, _, err := s.InviteMember(ctx, space.InviteMemberCmd{SpaceID: spaceID, ActorID: ownerID, Email: ""})
	if !errors.Is(err, space.ErrEmailRequired) {
		t.Errorf("empty email: %v, want ErrEmailRequired", err)
	}

	_, _, err = s.InviteMember(ctx, space.InviteMemberCmd{SpaceID: spaceID, ActorID: ownerID, Email: "nobody@example.com"})
	if !errors.Is(err, space.ErrInviteeAccountRequired) {
		t.Errorf("unknown email: %v, want ErrInviteeAccountRequired", err)
	}
	if kind, _ := apierr.KindOf(err); kind != apierr.KindInvalid {
		t.Errorf("kind = %q, want invalid", kind)
	}
}

func TestCannotInviteSomeoneAlreadyOnTheSpace(t *testing.T) {
	s, spaceID, ownerID, memberID := newSpace(t)
	user, err := s.Users.(*mock.MockUserStore).GetUser(context.Background(), memberID)
	if err != nil || user == nil {
		t.Fatalf("resolve member: %v", err)
	}

	_, _, err = s.InviteMember(context.Background(), space.InviteMemberCmd{
		SpaceID: spaceID, ActorID: ownerID, Email: user.Email,
	})
	if !errors.Is(err, space.ErrAlreadyMember) {
		t.Errorf("err = %v, want ErrAlreadyMember", err)
	}
}

func TestInvitingTwiceIsAConflict(t *testing.T) {
	s, spaceID, ownerID, _ := newSpace(t)
	createAccount(t, s, "twice@example.com")
	ctx := context.Background()

	if _, _, err := s.InviteMember(ctx, space.InviteMemberCmd{SpaceID: spaceID, ActorID: ownerID, Email: "twice@example.com"}); err != nil {
		t.Fatalf("first invite: %v", err)
	}
	_, _, err := s.InviteMember(ctx, space.InviteMemberCmd{SpaceID: spaceID, ActorID: ownerID, Email: "twice@example.com"})
	if !errors.Is(err, space.ErrInvitationAlreadyPending) {
		t.Errorf("err = %v, want ErrInvitationAlreadyPending", err)
	}
}

// An address is matched however it was typed.
func TestEmailIsNormalisedBeforeLookup(t *testing.T) {
	s, spaceID, ownerID, _ := newSpace(t)
	createAccount(t, s, "casey@example.com")

	_, user, err := s.InviteMember(context.Background(), space.InviteMemberCmd{
		SpaceID: spaceID, ActorID: ownerID, Email: "  Casey@Example.COM  ",
	})

	if err != nil {
		t.Fatalf("InviteMember: %v", err)
	}
	if user.Email != "casey@example.com" {
		t.Errorf("Email = %q", user.Email)
	}
}

func TestListMembersResolvesAccounts(t *testing.T) {
	s, spaceID, _, _ := newSpace(t)

	members, err := s.ListMembers(context.Background(), spaceID)

	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("members = %d, want 2", len(members))
	}
	for _, m := range members {
		if m.User == nil || m.User.ID != m.Membership.UserID {
			t.Errorf("membership %s has no resolved account", m.Membership.UserID)
		}
	}
}

// The whole invitation lifecycle: invite, accept, and the membership it
// produces.
func TestInviteThenAcceptCreatesMembership(t *testing.T) {
	s, spaceID, ownerID, _ := newSpace(t)
	inviteeID := createAccount(t, s, "invitee@example.com")
	ctx := context.Background()

	inv, _, err := s.InviteMember(ctx, space.InviteMemberCmd{SpaceID: spaceID, ActorID: ownerID, Email: "invitee@example.com"})
	if err != nil {
		t.Fatalf("InviteMember: %v", err)
	}

	accepted, err := s.AcceptInvitation(ctx, space.AcceptInvitationCmd{InvitationID: inv.ID, ActorID: inviteeID})
	if err != nil {
		t.Fatalf("AcceptInvitation: %v", err)
	}
	if accepted.Invitation.AcceptedAt == nil {
		t.Fatal("accepted invitation has no AcceptedAt")
	}

	members, err := s.ListMembers(ctx, spaceID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	found := false
	for _, m := range members {
		if m.Membership.UserID == inviteeID {
			found = true
		}
	}
	if !found {
		t.Fatal("invitee is not on the roster after accepting")
	}

	pending, err := s.Spaces.ListPendingInvitationsByUser(ctx, inviteeID, time.Now().UTC())
	if err != nil {
		t.Fatalf("ListPendingInvitationsByUser: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending invitations after accepting = %d, want 0", len(pending))
	}
}

// Only the invited account may accept -- a valid-looking id belonging to
// somebody else is refused the same way a nonexistent one is.
func TestOnlyTheInviteeMayAccept(t *testing.T) {
	s, spaceID, ownerID, memberID := newSpace(t)
	createAccount(t, s, "invitee@example.com")
	ctx := context.Background()

	inv, _, err := s.InviteMember(ctx, space.InviteMemberCmd{SpaceID: spaceID, ActorID: ownerID, Email: "invitee@example.com"})
	if err != nil {
		t.Fatalf("InviteMember: %v", err)
	}

	_, err = s.AcceptInvitation(ctx, space.AcceptInvitationCmd{InvitationID: inv.ID, ActorID: memberID})
	if !errors.Is(err, space.ErrInvitationNotFound) {
		t.Errorf("err = %v, want ErrInvitationNotFound", err)
	}
}

func TestAcceptingAnExpiredInvitationIsRefused(t *testing.T) {
	spaces := &mock.MockSpaceStore{}
	users := &mock.MockUserStore{}
	ctx := context.Background()
	owner, err := users.CreateUser(ctx, "owner@example.com", "free")
	if err != nil {
		t.Fatalf("CreateUser owner: %v", err)
	}
	invitee, err := users.CreateUser(ctx, "invitee@example.com", "free")
	if err != nil {
		t.Fatalf("CreateUser invitee: %v", err)
	}
	tm, err := spaces.CreateSpace(ctx, "acme", owner.ID, "free")
	if err != nil {
		t.Fatalf("CreateSpace: %v", err)
	}

	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	s := &space.Service{Spaces: spaces, Users: users, Now: func() time.Time { return past }}
	inv, _, err := s.InviteMember(ctx, space.InviteMemberCmd{SpaceID: tm.ID, ActorID: owner.ID, Email: "invitee@example.com"})
	if err != nil {
		t.Fatalf("InviteMember: %v", err)
	}

	live := &space.Service{Spaces: spaces, Users: users, Now: func() time.Time { return past.Add(corespace.InvitationTTLDefault + time.Second) }}
	_, err = live.AcceptInvitation(ctx, space.AcceptInvitationCmd{InvitationID: inv.ID, ActorID: invitee.ID})
	if !errors.Is(err, space.ErrInvitationExpired) {
		t.Errorf("err = %v, want ErrInvitationExpired", err)
	}
}

func TestOwnerRevokesAPendingInvitation(t *testing.T) {
	s, spaceID, ownerID, _ := newSpace(t)
	inviteeID := createAccount(t, s, "invitee@example.com")
	ctx := context.Background()

	inv, _, err := s.InviteMember(ctx, space.InviteMemberCmd{SpaceID: spaceID, ActorID: ownerID, Email: "invitee@example.com"})
	if err != nil {
		t.Fatalf("InviteMember: %v", err)
	}
	if err := s.RevokeInvitation(ctx, space.RevokeInvitationCmd{SpaceID: spaceID, InvitationID: inv.ID, ActorID: ownerID}); err != nil {
		t.Fatalf("RevokeInvitation: %v", err)
	}

	_, err = s.AcceptInvitation(ctx, space.AcceptInvitationCmd{InvitationID: inv.ID, ActorID: inviteeID})
	if !errors.Is(err, space.ErrInvitationNotPending) {
		t.Errorf("accepting a revoked invitation: %v, want ErrInvitationNotPending", err)
	}
}

func TestOnlyOwnerOrAdminMayRevoke(t *testing.T) {
	s, spaceID, ownerID, memberID := newSpace(t)
	createAccount(t, s, "invitee@example.com")
	ctx := context.Background()

	inv, _, err := s.InviteMember(ctx, space.InviteMemberCmd{SpaceID: spaceID, ActorID: ownerID, Email: "invitee@example.com"})
	if err != nil {
		t.Fatalf("InviteMember: %v", err)
	}
	err = s.RevokeInvitation(ctx, space.RevokeInvitationCmd{SpaceID: spaceID, InvitationID: inv.ID, ActorID: memberID})
	if !errors.Is(err, space.ErrOnlyOwnerOrAdminCanInvite) {
		t.Errorf("err = %v, want ErrOnlyOwnerOrAdminCanInvite", err)
	}
}

// TestRevokePermissionComesFromThePathSpace pins the fix for a real ordering
// bug: checking the invitation's own space before the caller's permission
// would let an id that resolves nowhere (a typo, or an id spent trying
// another space) skip the permission check and answer "not found" instead of
// "forbidden" to a caller who was never authorized to ask in the first
// place -- see RevokeInvitation.
func TestRevokePermissionComesFromThePathSpace(t *testing.T) {
	s, spaceID, ownerID, memberID := newSpace(t)
	ctx := context.Background()
	spaces := s.Spaces.(*mock.MockSpaceStore)
	otherSpace, err := spaces.CreateSpace(ctx, "other", ownerID, "free")
	if err != nil {
		t.Fatalf("CreateSpace other: %v", err)
	}

	// A member has no invite permission at all, so even an id that resolves
	// nowhere must still read as forbidden, not "not found".
	err = s.RevokeInvitation(ctx, space.RevokeInvitationCmd{SpaceID: spaceID, InvitationID: "inv_absent", ActorID: memberID})
	if !errors.Is(err, space.ErrOnlyOwnerOrAdminCanInvite) {
		t.Errorf("member revoking a nonexistent id: %v, want ErrOnlyOwnerOrAdminCanInvite", err)
	}

	// An invitation that belongs to a different space than the one named in
	// the path reads as not found -- an owner of spaceID has no standing to
	// revoke otherSpace's invitations by guessing at otherSpace's own path.
	createAccount(t, s, "elsewhere@example.com")
	otherInv, _, err := s.InviteMember(ctx, space.InviteMemberCmd{SpaceID: otherSpace.ID, ActorID: ownerID, Email: "elsewhere@example.com"})
	if err != nil {
		t.Fatalf("InviteMember otherSpace: %v", err)
	}
	err = s.RevokeInvitation(ctx, space.RevokeInvitationCmd{SpaceID: spaceID, InvitationID: otherInv.ID, ActorID: ownerID})
	if !errors.Is(err, space.ErrInvitationNotFound) {
		t.Errorf("revoking another space's invitation through this space's path: %v, want ErrInvitationNotFound", err)
	}
}

func TestListMyInvitationsAcrossSpaces(t *testing.T) {
	s, spaceID, ownerID, _ := newSpace(t)
	inviteeID := createAccount(t, s, "busy@example.com")
	ctx := context.Background()

	spaces := s.Spaces.(*mock.MockSpaceStore)
	otherSpace, err := spaces.CreateSpace(ctx, "other", ownerID, "free")
	if err != nil {
		t.Fatalf("CreateSpace other: %v", err)
	}

	if _, _, err := s.InviteMember(ctx, space.InviteMemberCmd{SpaceID: spaceID, ActorID: ownerID, Email: "busy@example.com"}); err != nil {
		t.Fatalf("InviteMember spaceID: %v", err)
	}
	if _, _, err := s.InviteMember(ctx, space.InviteMemberCmd{SpaceID: otherSpace.ID, ActorID: ownerID, Email: "busy@example.com"}); err != nil {
		t.Fatalf("InviteMember otherSpace: %v", err)
	}

	pending, err := s.ListMyInvitations(ctx, inviteeID)
	if err != nil {
		t.Fatalf("ListMyInvitations: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("pending = %d, want 2", len(pending))
	}
}

func TestOwnerPromotesAndDemotesAMember(t *testing.T) {
	s, spaceID, ownerID, memberID := newSpace(t)
	ctx := context.Background()

	before, err := s.ListMembers(ctx, spaceID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	var createdAt time.Time
	for _, m := range before {
		if m.Membership.UserID == memberID {
			createdAt = m.Membership.CreatedAt
		}
	}

	if err := s.SetMemberRole(ctx, space.SetMemberRoleCmd{SpaceID: spaceID, ActorID: ownerID, TargetUserID: memberID, Role: corespace.RoleAdmin}); err != nil {
		t.Fatalf("promote to admin: %v", err)
	}
	if err := s.SetMemberRole(ctx, space.SetMemberRoleCmd{SpaceID: spaceID, ActorID: ownerID, TargetUserID: memberID, Role: corespace.RoleMember}); err != nil {
		t.Fatalf("demote to member: %v", err)
	}

	after, err := s.ListMembers(ctx, spaceID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("member count changed = %d, want %d (a role change is not a remove/add pair)", len(after), len(before))
	}
	for _, m := range after {
		if m.Membership.UserID == memberID {
			if m.Membership.Role != corespace.RoleMember {
				t.Errorf("final role = %q, want member", m.Membership.Role)
			}
			if !m.Membership.CreatedAt.Equal(createdAt) {
				t.Errorf("CreatedAt changed from %v to %v; a role change must not read as a fresh join", createdAt, m.Membership.CreatedAt)
			}
		}
	}
}

func TestOnlyOwnerMayChangeRole(t *testing.T) {
	s, spaceID, _, memberID := newSpace(t)
	ctx := context.Background()
	adminID := addMemberWithRole(t, s, spaceID, "admin@example.com", corespace.RoleAdmin)

	for _, actor := range []string{adminID, memberID} {
		err := s.SetMemberRole(ctx, space.SetMemberRoleCmd{SpaceID: spaceID, ActorID: actor, TargetUserID: memberID, Role: corespace.RoleAdmin})
		if !errors.Is(err, space.ErrOnlyOwnerCanChangeRole) {
			t.Errorf("actor %s: %v, want ErrOnlyOwnerCanChangeRole", actor, err)
		}
	}
}

func TestOwnerTransfersOwnership(t *testing.T) {
	s, spaceID, ownerID, memberID := newSpace(t)
	ctx := context.Background()

	if err := s.SetMemberRole(ctx, space.SetMemberRoleCmd{SpaceID: spaceID, ActorID: ownerID, TargetUserID: memberID, Role: corespace.RoleOwner}); err != nil {
		t.Fatalf("transfer: %v", err)
	}

	members, err := s.ListMembers(ctx, spaceID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	roles := map[string]string{}
	for _, m := range members {
		roles[m.Membership.UserID] = m.Membership.Role
	}
	if roles[memberID] != corespace.RoleOwner {
		t.Errorf("new owner role = %q, want owner", roles[memberID])
	}
	if roles[ownerID] != corespace.RoleAdmin {
		t.Errorf("former owner role = %q, want admin", roles[ownerID])
	}

	// Reversible: the new owner can transfer straight back.
	if err := s.SetMemberRole(ctx, space.SetMemberRoleCmd{SpaceID: spaceID, ActorID: memberID, TargetUserID: ownerID, Role: corespace.RoleOwner}); err != nil {
		t.Fatalf("transfer back: %v", err)
	}
}

func TestCannotTransferToSelf(t *testing.T) {
	s, spaceID, ownerID, _ := newSpace(t)

	err := s.SetMemberRole(context.Background(), space.SetMemberRoleCmd{
		SpaceID: spaceID, ActorID: ownerID, TargetUserID: ownerID, Role: corespace.RoleOwner,
	})
	if !errors.Is(err, space.ErrCannotTransferToSelf) {
		t.Errorf("err = %v, want ErrCannotTransferToSelf", err)
	}
}

func TestSoleOwnerCannotDemoteThemselvesWithoutTransferring(t *testing.T) {
	s, spaceID, ownerID, _ := newSpace(t)

	err := s.SetMemberRole(context.Background(), space.SetMemberRoleCmd{
		SpaceID: spaceID, ActorID: ownerID, TargetUserID: ownerID, Role: corespace.RoleAdmin,
	})
	if !errors.Is(err, space.ErrCannotDemoteLastOwner) {
		t.Errorf("err = %v, want ErrCannotDemoteLastOwner", err)
	}
}

func TestIssueMemberLoginCodeForOwnSpaceMember(t *testing.T) {
	s, spaceID, ownerID, memberID := newSpace(t)
	s.LoginCodes = &mock.MockLoginCodeStore{}

	code, expiresAt, err := s.IssueMemberLoginCode(context.Background(), space.IssueMemberLoginCodeCmd{
		SpaceID: spaceID, ActorID: ownerID, TargetUserID: memberID,
	})
	if err != nil {
		t.Fatalf("IssueMemberLoginCode: %v", err)
	}
	if code == "" {
		t.Error("code is empty")
	}
	if !expiresAt.After(time.Now().UTC()) {
		t.Error("expiresAt is not in the future")
	}
}

func TestIssueMemberLoginCodeRefusedForNonOwner(t *testing.T) {
	s, spaceID, _, memberID := newSpace(t)
	s.LoginCodes = &mock.MockLoginCodeStore{}
	adminID := addMemberWithRole(t, s, spaceID, "admin@example.com", corespace.RoleAdmin)

	_, _, err := s.IssueMemberLoginCode(context.Background(), space.IssueMemberLoginCodeCmd{
		SpaceID: spaceID, ActorID: adminID, TargetUserID: memberID,
	})
	if !errors.Is(err, space.ErrOnlyOwnerCanIssueMemberLoginCode) {
		t.Errorf("err = %v, want ErrOnlyOwnerCanIssueMemberLoginCode", err)
	}
}

func TestIssueMemberLoginCodeRefusedOutsideTheCallersSpace(t *testing.T) {
	s, spaceID, ownerID, _ := newSpace(t)
	s.LoginCodes = &mock.MockLoginCodeStore{}

	_, _, err := s.IssueMemberLoginCode(context.Background(), space.IssueMemberLoginCodeCmd{
		SpaceID: spaceID, ActorID: ownerID, TargetUserID: "u_stranger",
	})
	if !errors.Is(err, space.ErrMemberNotFound) {
		t.Errorf("err = %v, want ErrMemberNotFound", err)
	}
}

func TestIssueMemberLoginCodeRefusedForDisabledAccount(t *testing.T) {
	s, spaceID, ownerID, memberID := newSpace(t)
	s.LoginCodes = &mock.MockLoginCodeStore{}
	users := s.Users.(*mock.MockUserStore)
	disabledAt := time.Now().UTC()
	if err := users.SetUserDisabled(context.Background(), memberID, &disabledAt); err != nil {
		t.Fatalf("SetUserDisabled: %v", err)
	}

	_, _, err := s.IssueMemberLoginCode(context.Background(), space.IssueMemberLoginCodeCmd{
		SpaceID: spaceID, ActorID: ownerID, TargetUserID: memberID,
	})
	if !errors.Is(err, space.ErrTargetAccountDisabled) {
		t.Errorf("err = %v, want ErrTargetAccountDisabled", err)
	}
}

// A member may not lower or raise the bar every undeclared agent in the space
// runs under; an admin may.
func TestSetSandboxDefaults_MemberForbiddenAdminAllowed(t *testing.T) {
	s, spaceID, _, memberID := newSpace(t)
	ctx := context.Background()
	adminID := addMemberWithRole(t, s, spaceID, "admin@example.com", corespace.RoleAdmin)

	err := s.SetSandboxDefaults(ctx, space.SetSandboxDefaultsCmd{
		SpaceID: spaceID, ActorID: memberID, NetworkTier: "registries", FilesystemTier: "workspace",
	})
	if !errors.Is(err, space.ErrOnlyOwnerOrAdminCanSetSandboxDefaults) {
		t.Errorf("member setting defaults: %v, want ErrOnlyOwnerOrAdminCanSetSandboxDefaults", err)
	}

	err = s.SetSandboxDefaults(ctx, space.SetSandboxDefaultsCmd{
		SpaceID: spaceID, ActorID: adminID, NetworkTier: "registries", FilesystemTier: "workspace",
	})
	if err != nil {
		t.Fatalf("admin setting defaults: %v", err)
	}
	got, err := s.Spaces.GetSpace(ctx, spaceID)
	if err != nil {
		t.Fatalf("GetSpace: %v", err)
	}
	if got.DefaultSandboxNetworkTier != "registries" || got.DefaultSandboxFilesystemTier != "workspace" {
		t.Errorf("space defaults = %+v, want registries/workspace", got)
	}
}

// An unrecognized tier is refused even when the caller may otherwise change
// the setting.
func TestSetSandboxDefaults_InvalidTierRejected(t *testing.T) {
	s, spaceID, ownerID, _ := newSpace(t)
	err := s.SetSandboxDefaults(context.Background(), space.SetSandboxDefaultsCmd{
		SpaceID: spaceID, ActorID: ownerID, NetworkTier: "not-a-tier",
	})
	if !errors.Is(err, space.ErrInvalidSandboxTier) {
		t.Errorf("err = %v, want ErrInvalidSandboxTier", err)
	}
}

func TestMissingStoresAreReported(t *testing.T) {
	ctx := context.Background()

	if _, err := (&space.Service{}).ListMembers(ctx, "tm_1"); !errors.Is(err, space.ErrSpacesNotConfigured) {
		t.Errorf("no space store: %v", err)
	}
	// A space store with no user store cannot resolve an email to an account.
	s := &space.Service{Spaces: &mock.MockSpaceStore{}}
	_, _, err := s.InviteMember(ctx, space.InviteMemberCmd{SpaceID: "tm_1", ActorID: "u_1", Email: "a@b.c"})
	if !errors.Is(err, space.ErrUsersNotConfigured) {
		t.Errorf("no user store: %v, want ErrUsersNotConfigured", err)
	}
	if kind, _ := apierr.KindOf(err); kind != apierr.KindNotConfigured {
		t.Errorf("kind = %q, want not_configured", kind)
	}
}
