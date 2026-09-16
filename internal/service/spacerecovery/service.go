// Package spacerecovery is the one narrow System Administrator action for a
// shared Space whose recorded owners can no longer sign in: promote an enabled
// member to owner so the Space is manageable again. It is deliberately not a
// general power to transfer a healthy Space — it applies only when every owner
// is disabled, never touches a personal Space, creates no membership, and grants
// the administrator no access to the Space's contents. See
// docs/proposals/personnel-deactivation-lifecycle.md §9.
package spacerecovery

import (
	"context"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
)

var (
	ErrNotConfigured      = apierr.New(apierr.KindNotConfigured, "space recovery not configured")
	ErrSpaceNotFound      = apierr.New(apierr.KindNotFound, "space not found")
	ErrPersonalSpace      = apierr.New(apierr.KindInvalid, "a personal space cannot have its ownership recovered")
	ErrOwnerStillActive   = apierr.New(apierr.KindConflict, "an owner can still sign in; they can transfer ownership themselves")
	ErrSuccessorNotMember = apierr.New(apierr.KindInvalid, "the successor must already be a member of the space")
	ErrSuccessorDisabled  = apierr.New(apierr.KindConflict, "the successor account is disabled")
)

// Service performs disabled-owner-only ownership recovery. It does the mechanics
// and returns the demoted owner; the caller records the audit, so the actor is
// attributed correctly whether the call came from an administrator over the
// Admin API or the operator running the break-glass command.
type Service struct {
	Spaces corespace.Store
	Users  coreidentity.UserStore
	// Now is the clock, injectable for tests.
	Now func() time.Time
}

// RecoverCmd names the Space and the enabled member to promote.
type RecoverCmd struct {
	SpaceID     string
	SuccessorID string
}

// RecoverOwnership promotes SuccessorID to owner of SpaceID when every recorded
// owner is disabled, and returns the disabled owner it demoted so the caller can
// name all three in the audit trail. It reuses the ordinary ownership transfer,
// so the demoted owner becomes admin in the same move; other disabled owners, if
// any, keep their (disabled, powerless) owner rows.
func (s *Service) RecoverOwnership(ctx context.Context, cmd RecoverCmd) (demotedOwner string, err error) {
	if s.Spaces == nil || s.Users == nil {
		return "", ErrNotConfigured
	}
	space, err := s.Spaces.GetSpace(ctx, cmd.SpaceID)
	if err != nil {
		return "", err
	}
	if space == nil {
		return "", ErrSpaceNotFound
	}
	// A personal Space is bound to one account and cannot be handed to anyone
	// else; recovery there would be an account transfer, which is a non-goal.
	if space.PersonalForUserID != nil {
		return "", ErrPersonalSpace
	}
	members, err := s.Spaces.ListSpaceMembers(ctx, cmd.SpaceID)
	if err != nil {
		return "", err
	}

	// Every recorded owner must be disabled. A single owner who can still sign in
	// is expected to transfer ownership themselves; recovery is the break-glass
	// for when none can.
	sawOwner := false
	for i := range members {
		if corespace.EffectiveRole(members[i].Role) != corespace.RoleOwner {
			continue
		}
		sawOwner = true
		u, err := s.Users.GetUser(ctx, members[i].UserID)
		if err != nil {
			return "", err
		}
		if u == nil || !u.Disabled() {
			return "", ErrOwnerStillActive
		}
		if demotedOwner == "" {
			demotedOwner = members[i].UserID
		}
	}
	if !sawOwner {
		// No owner row at all is not a recovery this action defines; treat the
		// Space as if it were not found rather than inventing an owner.
		return "", ErrSpaceNotFound
	}

	// The successor must already be an enabled member. Recovery never creates a
	// membership, and promoting a disabled account would just reproduce the
	// problem.
	if corespace.EffectiveRoleOf(members, cmd.SuccessorID) == "" {
		return "", ErrSuccessorNotMember
	}
	successor, err := s.Users.GetUser(ctx, cmd.SuccessorID)
	if err != nil {
		return "", err
	}
	if successor == nil || successor.Disabled() {
		return "", ErrSuccessorDisabled
	}

	if err := s.Spaces.TransferOwnership(ctx, cmd.SpaceID, demotedOwner, cmd.SuccessorID); err != nil {
		return "", err
	}
	return demotedOwner, nil
}
