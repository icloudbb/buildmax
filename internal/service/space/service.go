// Package space owns membership rules: who is in a space, who may change that,
// and what a member may be.
//
// The handlers held these before, and wrote the owner check out twice --
// fetching the roster, scanning it for the caller, comparing the role -- once
// to add a member and once to remove one. A rule about who may change a space is
// exactly the thing that must not exist in two copies.
package space

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/icloudbb/buildmax/internal/config"
	coreagent "github.com/icloudbb/buildmax/internal/core/agent"
	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/service/accountlifecycle"
)

var (
	ErrSpacesNotConfigured = apierr.New(apierr.KindNotConfigured, "spaces not configured")
	ErrUsersNotConfigured  = apierr.New(apierr.KindNotConfigured, "users not configured")
	ErrOnlyOwnerCanRemove  = apierr.New(apierr.KindForbidden, "only space owners can remove members")
	ErrEmailRequired       = apierr.New(apierr.KindInvalid, "email is required")
	ErrUnsupportedRole     = apierr.New(apierr.KindInvalid, "role must be member or admin")
	ErrCannotRemoveSelf    = apierr.New(apierr.KindInvalid, "owners cannot remove themselves")
	ErrMemberNotFound      = apierr.New(apierr.KindNotFound, "space member not found")

	// ErrInviteeAccountRequired refuses an invitation to an email with no
	// account. Creating one is system_admin's job, never a space-scoped call's
	// -- see docs/design/space-membership-lifecycle.md §1.
	ErrInviteeAccountRequired = apierr.New(apierr.KindInvalid,
		"no account exists for this email; ask a system administrator to create one "+
			"(POST /api/admin/users or buildmax-server user create), then invite it")
	// ErrOnlyOwnerOrAdminCanInvite covers creating, listing, and revoking
	// invitations -- the one action ActionInviteSpaceMember gates.
	ErrOnlyOwnerOrAdminCanInvite = apierr.New(apierr.KindForbidden,
		"only a space owner or admin may invite, list, or revoke invitations")
	// ErrOnlyOwnerCanInviteAdmin is the role-content restriction Allows cannot
	// express on its own: admin may invite, but only at the member role.
	ErrOnlyOwnerCanInviteAdmin  = apierr.New(apierr.KindForbidden, "only a space owner may invite someone as admin")
	ErrAlreadyMember            = apierr.New(apierr.KindConflict, "already a member of this space")
	ErrInvitationAlreadyPending = apierr.New(apierr.KindConflict, "an invitation is already pending for this account")
	// ErrInvitationNotFound also covers an invitation that exists but belongs
	// to someone else: the same refusal for both, so a valid-looking id is not
	// an existence oracle for another account's invitations.
	ErrInvitationNotFound   = apierr.New(apierr.KindNotFound, "invitation not found")
	ErrInvitationNotPending = apierr.New(apierr.KindConflict, "invitation is no longer pending")
	ErrInvitationExpired    = apierr.New(apierr.KindConflict, "invitation has expired")

	// ErrOnlyOwnerCanChangeRole gates SetMemberRole -- promotion, demotion,
	// and the ownership transfer that results from setting a target to owner.
	ErrOnlyOwnerCanChangeRole = apierr.New(apierr.KindForbidden, "only a space owner may change a member's role")
	// ErrCannotTransferToSelf refuses the one request SetMemberRole cannot
	// give a meaning to: "promote someone to owner while I stay owner too" is
	// not a state this package defines.
	ErrCannotTransferToSelf = apierr.New(apierr.KindInvalid, "cannot transfer ownership to yourself")
	// ErrCannotDemoteLastOwner is the space-scoped version of the rule
	// system-administration.md §6 applies to the last system_admin grant: the
	// API refuses to leave a space with none. Transfer first.
	ErrCannotDemoteLastOwner = apierr.New(apierr.KindConflict, "the last owner cannot be demoted; transfer ownership first")
	// ErrUnsupportedMemberRole is ErrUnsupportedRole's counterpart for
	// SetMemberRole, which -- unlike an invitation -- does accept owner.
	ErrUnsupportedMemberRole = apierr.New(apierr.KindInvalid, "role must be owner, admin, or member")

	// ErrOnlyOwnerOrAdminCanSetSandboxDefaults gates SetSandboxDefaults --
	// the same authority as managing agents, since a space's default sandbox
	// tier shapes what every agent that declares nothing may do. See
	// docs/design/agent-sandbox-policy.md §9 M3.
	ErrOnlyOwnerOrAdminCanSetSandboxDefaults = apierr.New(apierr.KindForbidden,
		"only a space owner or admin may set the space's default sandbox tiers")
	// ErrInvalidSandboxTier rejects a network or filesystem tier this binary
	// does not recognize.
	ErrInvalidSandboxTier                      = apierr.New(apierr.KindInvalid, "unknown sandbox network or filesystem tier")
	ErrOnlyOwnerOrAdminCanSetAgentInstructions = apierr.New(apierr.KindForbidden,
		"only a space owner or admin may set the space's agent instructions")
	ErrAgentInstructionsTooLong = apierr.New(apierr.KindInvalid, "space agent instructions exceed 8192 characters")

	// ErrOnlyOwnerCanIssueMemberLoginCode gates the one place in this package
	// a login code is issued: a locked-out member of the caller's own space.
	ErrOnlyOwnerCanIssueMemberLoginCode = apierr.New(apierr.KindForbidden, "only a space owner may issue a login code for a member")
	ErrLoginCodesNotConfigured          = apierr.New(apierr.KindNotConfigured, "login codes not configured")
	// ErrTargetAccountDisabled mirrors the admin route's refusal: a code for
	// an account that cannot use it would be a way in that opens nothing, and
	// an owner would reasonably read success as "they can sign in now".
	ErrTargetAccountDisabled = apierr.New(apierr.KindConflict, "the account is disabled; a system administrator must enable it first")
)

type Service struct {
	Spaces corespace.Store
	Agents interface {
		ListAgentsBySpace(context.Context, string) ([]agentdef.Agent, error)
	}
	Users coreidentity.UserStore
	// LoginCodes backs IssueMemberLoginCode only -- the one place in this
	// package a credential is issued, and only for a member of the caller's
	// own space. Nil leaves that route unavailable, which is what a deployment
	// with no login-code store has.
	LoginCodes coreidentity.LoginCodeStore
	// ServiceAccounts and Lifecycle back the Space's service accounts: the
	// first writes them, the second is the account gate that disables and
	// re-enables one. Nil leaves those routes unavailable.
	ServiceAccounts coreidentity.ServiceAccountStore
	Lifecycle       *accountlifecycle.Service
	// Now is the clock. Nil means time.Now. Tests set it to pin an invitation's
	// expiry rather than waiting on InvitationTTLDefault.
	Now func() time.Time
}

type SetAgentInstructionsCmd struct {
	SpaceID      string
	ActorID      string
	Instructions string
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

// Member pairs a membership with the account behind it. User is nil when the
// deployment has no user store to resolve it against.
type Member struct {
	Membership corespace.Member
	User       *coreidentity.User
}

type InviteMemberCmd struct {
	SpaceID string
	// ActorID is the caller, who must hold ActionInviteSpaceMember.
	ActorID string
	Email   string
	// Role defaults to member. Only member and admin are accepted; owner
	// moves through SetMemberRole instead -- see
	// docs/design/space-membership-lifecycle.md §5.2.
	Role string
}

type RevokeInvitationCmd struct {
	// SpaceID is the path's space, checked first: a caller's permission is
	// decided from the space they named, never from a space the invitation
	// happens to resolve to. See RevokeInvitation.
	SpaceID      string
	InvitationID string
	ActorID      string
}

type AcceptInvitationCmd struct {
	InvitationID string
	// ActorID is the signed-in caller. Accepting requires no code: the
	// caller already reached a session on their own, so authorization is "this
	// is my own pending row," not proving anything a second time.
	ActorID string
}

type RemoveMemberCmd struct {
	SpaceID      string
	ActorID      string
	TargetUserID string
}

type SetMemberRoleCmd struct {
	SpaceID      string
	ActorID      string
	TargetUserID string
	// Role is owner, admin, or member. Setting owner is ownership transfer:
	// the caller is demoted to admin in the same call -- see SetMemberRole.
	Role string
}

type IssueMemberLoginCodeCmd struct {
	SpaceID      string
	ActorID      string
	TargetUserID string
}

// SetSandboxDefaultsCmd sets the tiers an agent that declares neither
// inherits. Either field may be empty, meaning this space sets no default on
// that axis and the surface baseline applies instead.
type SetSandboxDefaultsCmd struct {
	SpaceID        string
	ActorID        string
	NetworkTier    string
	FilesystemTier string
}

// ListMembers returns the roster with each member's account resolved.
func (s *Service) ListMembers(ctx context.Context, spaceID string) ([]Member, error) {
	if s.Spaces == nil {
		return nil, ErrSpacesNotConfigured
	}
	memberships, err := s.Spaces.ListSpaceMembers(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	out := make([]Member, len(memberships))
	for i := range memberships {
		out[i] = Member{Membership: memberships[i]}
		if s.Users == nil {
			continue
		}
		user, err := s.Users.GetUser(ctx, memberships[i].UserID)
		if err != nil {
			return nil, err
		}
		out[i].User = user
	}
	return out, nil
}

// InviteMember creates a pending invitation for an email that already has an
// account. It never creates one -- see
// docs/design/space-membership-lifecycle.md §1 for why account creation and
// space membership are kept as two different authorities.
func (s *Service) InviteMember(ctx context.Context, cmd InviteMemberCmd) (*corespace.Invitation, *coreidentity.User, error) {
	if s.Spaces == nil {
		return nil, nil, ErrSpacesNotConfigured
	}
	if s.Users == nil {
		return nil, nil, ErrUsersNotConfigured
	}

	// Permission is checked before any input is validated, matching
	// AddMember's original order: a caller who may not invite at all should
	// not learn anything about why their request would otherwise have been
	// rejected.
	members, err := s.Spaces.ListSpaceMembers(ctx, cmd.SpaceID)
	if err != nil {
		return nil, nil, err
	}
	callerRole := roleOf(members, cmd.ActorID)
	if callerRole == "" || !corespace.Allows(corespace.EffectiveRole(callerRole), corespace.ActionInviteSpaceMember) {
		return nil, nil, ErrOnlyOwnerOrAdminCanInvite
	}

	email := strings.TrimSpace(strings.ToLower(cmd.Email))
	if email == "" {
		return nil, nil, ErrEmailRequired
	}
	role := strings.TrimSpace(cmd.Role)
	if role == "" {
		role = corespace.RoleMember
	}
	if role != corespace.RoleMember && role != corespace.RoleAdmin {
		return nil, nil, ErrUnsupportedRole
	}
	// Admin may invite, but only at member -- inviting a peer admin is the one
	// escalation this action must not permit. See core/space/policy.go.
	if role == corespace.RoleAdmin && corespace.EffectiveRole(callerRole) != corespace.RoleOwner {
		return nil, nil, ErrOnlyOwnerCanInviteAdmin
	}

	user, err := s.Users.UserByEmail(ctx, email)
	if err != nil {
		return nil, nil, err
	}
	if user == nil {
		return nil, nil, ErrInviteeAccountRequired
	}
	if user.IsService() {
		return nil, nil, ErrServiceAccountCannotJoin
	}
	if isMember(members, user.ID) {
		return nil, nil, ErrAlreadyMember
	}
	now := s.now()
	pending, err := s.Spaces.ListPendingInvitationsBySpace(ctx, cmd.SpaceID, now)
	if err != nil {
		return nil, nil, err
	}
	for i := range pending {
		if pending[i].UserID == user.ID {
			return nil, nil, ErrInvitationAlreadyPending
		}
	}

	inv, err := s.Spaces.CreateInvitation(ctx, cmd.SpaceID, user.ID, role, cmd.ActorID, now.Add(corespace.InvitationTTLDefault))
	if err != nil {
		return nil, nil, fmt.Errorf("create invitation: %w", err)
	}
	return inv, user, nil
}

// ListSpaceInvitations returns a space's pending invitations. Reading who has
// been invited is the same authority as sending or revoking one.
func (s *Service) ListSpaceInvitations(ctx context.Context, spaceID, actorID string) ([]corespace.Invitation, error) {
	if s.Spaces == nil {
		return nil, ErrSpacesNotConfigured
	}
	members, err := s.Spaces.ListSpaceMembers(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	if !allows(members, actorID, corespace.ActionInviteSpaceMember) {
		return nil, ErrOnlyOwnerOrAdminCanInvite
	}
	return s.Spaces.ListPendingInvitationsBySpace(ctx, spaceID, s.now())
}

// ListMyInvitations answers GET /api/invitations: what is pending for the
// signed-in caller, across every space.
func (s *Service) ListMyInvitations(ctx context.Context, userID string) ([]corespace.Invitation, error) {
	if s.Spaces == nil {
		return nil, ErrSpacesNotConfigured
	}
	return s.Spaces.ListPendingInvitationsByUser(ctx, userID, s.now())
}

// RevokeInvitation withdraws a pending invitation before it is accepted.
//
// Permission is decided from cmd.SpaceID -- the path the caller actually
// named -- before the invitation is even looked up. Deciding it from the
// invitation's own space instead would let an unresolvable id (a typo, or one
// from another space) skip the permission check entirely and answer "not
// found" to a caller who was never authorized to ask.
func (s *Service) RevokeInvitation(ctx context.Context, cmd RevokeInvitationCmd) error {
	if s.Spaces == nil {
		return ErrSpacesNotConfigured
	}
	members, err := s.Spaces.ListSpaceMembers(ctx, cmd.SpaceID)
	if err != nil {
		return err
	}
	if !allows(members, cmd.ActorID, corespace.ActionInviteSpaceMember) {
		return ErrOnlyOwnerOrAdminCanInvite
	}
	inv, err := s.Spaces.GetInvitation(ctx, cmd.InvitationID)
	if err != nil {
		return err
	}
	// Not found also covers an invitation that resolves but belongs to a
	// different space than the one named in the path -- the same refusal
	// either way, for the reason ErrInvitationNotFound already states.
	if inv == nil || inv.SpaceID != cmd.SpaceID {
		return ErrInvitationNotFound
	}
	now := s.now()
	if !inv.Pending(now) {
		return ErrInvitationNotPending
	}
	return s.Spaces.RevokeInvitation(ctx, cmd.InvitationID, now)
}

// AcceptInvitation activates a pending invitation for the caller it names.
// It takes no code: the caller already reached a session on their own, so
// this is authorized by "this is my own pending row," not by proving
// anything a second time. See docs/design/space-membership-lifecycle.md §5.1.
func (s *Service) AcceptInvitation(ctx context.Context, cmd AcceptInvitationCmd) (*corespace.Invitation, error) {
	if s.Spaces == nil {
		return nil, ErrSpacesNotConfigured
	}
	inv, err := s.Spaces.GetInvitation(ctx, cmd.InvitationID)
	if err != nil {
		return nil, err
	}
	// The same refusal whether the invitation does not exist or belongs to
	// someone else -- see ErrInvitationNotFound.
	if inv == nil || inv.UserID != cmd.ActorID {
		return nil, ErrInvitationNotFound
	}
	now := s.now()
	if inv.AcceptedAt != nil || inv.RevokedAt != nil {
		return nil, ErrInvitationNotPending
	}
	if !now.Before(inv.ExpiresAt) {
		return nil, ErrInvitationExpired
	}
	accepted, err := s.Spaces.AcceptInvitation(ctx, cmd.InvitationID, now)
	if err != nil {
		return nil, fmt.Errorf("accept invitation: %w", err)
	}
	if accepted == nil {
		return nil, ErrInvitationNotPending
	}
	return accepted, nil
}

func (s *Service) RemoveMember(ctx context.Context, cmd RemoveMemberCmd) error {
	if s.Spaces == nil {
		return ErrSpacesNotConfigured
	}
	members, err := s.Spaces.ListSpaceMembers(ctx, cmd.SpaceID)
	if err != nil {
		return err
	}
	if !allows(members, cmd.ActorID, corespace.ActionManageSpaceMembers) {
		return ErrOnlyOwnerCanRemove
	}
	// An owner removing themselves could leave a space nobody can administer.
	if cmd.TargetUserID == cmd.ActorID {
		return ErrCannotRemoveSelf
	}
	if !isMember(members, cmd.TargetUserID) {
		return ErrMemberNotFound
	}
	if err := s.refuseServiceAccountMember(ctx, cmd.TargetUserID); err != nil {
		return err
	}
	return s.Spaces.RemoveSpaceMember(ctx, cmd.SpaceID, cmd.TargetUserID)
}

// SetMemberRole promotes or demotes a member. Setting a target's role to
// owner is ownership transfer -- exposed as this one endpoint rather than a
// separate one, because "promote someone to owner while I stay owner too" is
// not a state this package defines a meaning for, so the caller is demoted to
// admin in the same call. Transfer is unilateral and immediate, not subject
// to the target's acceptance -- see
// docs/design/space-membership-lifecycle.md §5.2-§5.3.
func (s *Service) SetMemberRole(ctx context.Context, cmd SetMemberRoleCmd) error {
	if s.Spaces == nil {
		return ErrSpacesNotConfigured
	}
	members, err := s.Spaces.ListSpaceMembers(ctx, cmd.SpaceID)
	if err != nil {
		return err
	}
	if !allows(members, cmd.ActorID, corespace.ActionChangeMemberRole) {
		return ErrOnlyOwnerCanChangeRole
	}
	role := strings.TrimSpace(cmd.Role)
	if role != corespace.RoleOwner && role != corespace.RoleAdmin && role != corespace.RoleMember {
		return ErrUnsupportedMemberRole
	}
	if !isMember(members, cmd.TargetUserID) {
		return ErrMemberNotFound
	}
	if err := s.refuseServiceAccountMember(ctx, cmd.TargetUserID); err != nil {
		return err
	}

	if role == corespace.RoleOwner {
		if cmd.TargetUserID == cmd.ActorID {
			return ErrCannotTransferToSelf
		}
		return s.Spaces.TransferOwnership(ctx, cmd.SpaceID, cmd.ActorID, cmd.TargetUserID)
	}

	// Demoting the space's only owner -- including the owner demoting
	// themselves -- must go through transfer first, or the space is left with
	// nobody who can administer it.
	if corespace.EffectiveRole(roleOf(members, cmd.TargetUserID)) == corespace.RoleOwner && countOwners(members) <= 1 {
		return ErrCannotDemoteLastOwner
	}
	_, err = s.Spaces.AddSpaceMember(ctx, cmd.SpaceID, cmd.TargetUserID, role)
	return err
}

// IssueMemberLoginCode issues a login code for a locked-out member of the
// caller's own space. It removes the dependency on a system_admin existing at
// all for the common case of one member locked out of an otherwise healthy
// space -- see docs/design/space-membership-lifecycle.md §5.4. It does not
// replace system-administration.md's deployment-scoped route, which is what
// recovers an owner who has no co-owner and no admin left in their own space.
func (s *Service) IssueMemberLoginCode(ctx context.Context, cmd IssueMemberLoginCodeCmd) (string, time.Time, error) {
	if s.Spaces == nil {
		return "", time.Time{}, ErrSpacesNotConfigured
	}
	if s.LoginCodes == nil {
		return "", time.Time{}, ErrLoginCodesNotConfigured
	}
	members, err := s.Spaces.ListSpaceMembers(ctx, cmd.SpaceID)
	if err != nil {
		return "", time.Time{}, err
	}
	// ActionManageSpaceMembers, not a new action: helping a locked-out member
	// back in is a membership-management act like adding or removing one.
	if !allows(members, cmd.ActorID, corespace.ActionManageSpaceMembers) {
		return "", time.Time{}, ErrOnlyOwnerCanIssueMemberLoginCode
	}
	if !isMember(members, cmd.TargetUserID) {
		return "", time.Time{}, ErrMemberNotFound
	}
	if s.Users != nil {
		user, err := s.Users.GetUser(ctx, cmd.TargetUserID)
		if err != nil {
			return "", time.Time{}, err
		}
		if user != nil && user.IsService() {
			return "", time.Time{}, ErrServiceAccountNoSignIn
		}
		if user != nil && user.Disabled() {
			return "", time.Time{}, ErrTargetAccountDisabled
		}
	}
	return s.LoginCodes.CreateLoginCode(ctx, cmd.TargetUserID, coreidentity.LoginCodeTTLDefault)
}

// SetSandboxDefaults sets the tiers an agent that declares neither inherits.
// ActionManageAgents, not a new action: a space's default sandbox tier shapes
// agent behavior the same way the agents themselves do. See
// docs/design/agent-sandbox-policy.md §9 M3.
func (s *Service) SetSandboxDefaults(ctx context.Context, cmd SetSandboxDefaultsCmd) error {
	if s.Spaces == nil {
		return ErrSpacesNotConfigured
	}
	members, err := s.Spaces.ListSpaceMembers(ctx, cmd.SpaceID)
	if err != nil {
		return err
	}
	if !allows(members, cmd.ActorID, corespace.ActionManageAgents) {
		return ErrOnlyOwnerOrAdminCanSetSandboxDefaults
	}
	if !config.ValidSandboxNetworkTier(cmd.NetworkTier) || !config.ValidSandboxFilesystemTier(cmd.FilesystemTier) {
		return ErrInvalidSandboxTier
	}
	return s.Spaces.SetSpaceSandboxDefaults(ctx, cmd.SpaceID, cmd.NetworkTier, cmd.FilesystemTier)
}

// SetAgentInstructions replaces the guidance inherited by every future
// background agent run in the space.
func (s *Service) SetAgentInstructions(ctx context.Context, cmd SetAgentInstructionsCmd) error {
	if s.Spaces == nil {
		return ErrSpacesNotConfigured
	}
	members, err := s.Spaces.ListSpaceMembers(ctx, cmd.SpaceID)
	if err != nil {
		return err
	}
	if !allows(members, cmd.ActorID, corespace.ActionManageAgents) {
		return ErrOnlyOwnerOrAdminCanSetAgentInstructions
	}
	instructions := strings.TrimSpace(cmd.Instructions)
	if s.Agents == nil {
		if err := coreagent.ValidateInstructionLayers(instructions, ""); err != nil {
			return apierr.Detail(ErrAgentInstructionsTooLong, "%v", err)
		}
	} else {
		agents, err := s.Agents.ListAgentsBySpace(ctx, cmd.SpaceID)
		if err != nil {
			return err
		}
		for i := range agents {
			if err := coreagent.ValidateInstructionLayers(instructions, agents[i].Instructions); err != nil {
				return apierr.Detail(ErrAgentInstructionsTooLong, "agent %q: %v", agents[i].Name, err)
			}
		}
	}
	return s.Spaces.SetSpaceAgentInstructions(ctx, cmd.SpaceID, instructions)
}

func countOwners(members []corespace.Member) int {
	n := 0
	for i := range members {
		if corespace.EffectiveRole(members[i].Role) == corespace.RoleOwner {
			n++
		}
	}
	return n
}

func allows(members []corespace.Member, userID string, action corespace.Action) bool {
	role := corespace.EffectiveRoleOf(members, userID)
	return role != "" && corespace.Allows(role, action)
}

// roleOf returns the stored role for userID, or "" when the roster has no
// membership for them. It exists alongside allows for InviteMember, which
// needs the caller's own role a second time -- to decide whether the
// requested target role is one they may grant -- not only whether the
// invite action itself is allowed.
func roleOf(members []corespace.Member, userID string) string {
	for i := range members {
		if members[i].UserID == userID {
			return members[i].Role
		}
	}
	return ""
}

func isMember(members []corespace.Member, userID string) bool {
	for i := range members {
		if members[i].UserID == userID {
			return true
		}
	}
	return false
}
