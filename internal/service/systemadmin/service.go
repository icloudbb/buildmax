// Package systemadmin owns who holds a deployment-scoped role.
//
// A system grant is the only authority in this system that is not scoped to a
// space, so both edges that change one are worth recording and neither may
// invent its own rules about it. What differs between them is authority, not
// procedure: the shell reached the machine and its database credentials, the
// route carries a session and an existing grant.
package systemadmin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	"github.com/icloudbb/buildmax/internal/service/audit"
)

// Refusals a caller can act on. Everything else is a failure, not an answer.
var (
	// ErrAccountNotFound means no account has that id.
	ErrAccountNotFound = apierr.New(apierr.KindNotFound, "account not found")
	// ErrAlreadyHeld means the account already holds the role.
	ErrAlreadyHeld = apierr.New(apierr.KindConflict, "the account already holds this role")
	// ErrNotHeld means the account does not hold the role, so there is nothing
	// to revoke. Revoking is otherwise idempotent.
	ErrNotHeld = apierr.New(apierr.KindNotFound, "the account does not hold this role")
	// ErrUnknownRole means the role is not one this deployment implements.
	ErrUnknownRole = apierr.New(apierr.KindInvalid, "unknown system role")
	// ErrAccountDisabled means the target account is disabled. A grant on it
	// could not authorize a request, so it is refused with an actionable
	// conflict rather than stored as dormant authority an operator cannot see.
	ErrAccountDisabled = apierr.New(apierr.KindConflict, "the account is disabled; enable it before granting a role")
	// ErrServiceAccount means the target is a service account. A system role
	// is reached through a signed-in session, which a service account never has.
	ErrServiceAccount = apierr.New(apierr.KindInvalid, "a service account cannot hold a system role")
	// ErrLastHolder means revoking would leave the deployment with nobody in
	// the role. Only the shell may do that, because only the shell can undo it.
	ErrLastHolder = apierr.New(apierr.KindConflict, "this is the deployment's last holder of the role")
)

const auditTarget = "user"

// Service grants and revokes deployment-scoped roles.
type Service struct {
	Grants coreidentity.SystemGrantStore
	Users  coreidentity.UserStore
	Audit  *audit.Recorder
}

// Grant gives an account a system role.
func (s *Service) Grant(ctx context.Context, userID, role string, actor coreaudit.Actor) (*coreidentity.SystemGrant, error) {
	if !coreidentity.ValidSystemRole(role) {
		return nil, ErrUnknownRole
	}
	user, err := s.Users.GetUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("read account: %w", err)
	}
	if user == nil {
		return nil, ErrAccountNotFound
	}
	if user.IsService() {
		return nil, ErrServiceAccount
	}
	if user.Disabled() {
		return nil, ErrAccountDisabled
	}
	grant, err := s.Grants.GrantSystemRole(ctx, userID, role, actor.ID, time.Now().UTC())
	if err != nil {
		switch {
		case errors.Is(err, coreidentity.ErrSystemGrantExists):
			return nil, ErrAlreadyHeld
		case errors.Is(err, coreidentity.ErrSystemRoleUnknown):
			return nil, ErrUnknownRole
		}
		return nil, fmt.Errorf("grant %s: %w", role, err)
	}
	s.record(ctx, actor, coreaudit.SystemAdminGranted, userID, role)
	return grant, nil
}

// Revoke takes a system role away.
//
// Leaving the deployment with nobody in the role is refused for a signed-in
// caller and permitted from the shell. That is not a convenience: the HTTP
// route is reached through the very grant it would be removing, so a caller who
// took the last one would lock the deployment out of its own admin area with no
// way back through the same door. The shell is the way back, which is why it
// may do it -- and why the caller decides nothing here. The authority does.
func (s *Service) Revoke(ctx context.Context, userID, role string, actor coreaudit.Actor) error {
	if !coreidentity.ValidSystemRole(role) {
		return ErrUnknownRole
	}
	// The shell (a system actor) may empty the role because it is the way back
	// from an empty role; a signed-in caller may not. The store makes that
	// decision atomically with the revoke, so two concurrent callers cannot both
	// read one holder each and both proceed — the old count-then-revoke here
	// could.
	keepLastHolder := actor.Type != coreaudit.ActorSystem
	revoked, err := s.Grants.RevokeSystemRole(ctx, userID, role, time.Now().UTC(), keepLastHolder)
	if err != nil {
		if errors.Is(err, coreidentity.ErrSystemGrantLastHolder) {
			return ErrLastHolder
		}
		return fmt.Errorf("revoke %s: %w", role, err)
	}
	if !revoked {
		return ErrNotHeld
	}
	s.record(ctx, actor, coreaudit.SystemAdminRevoked, userID, role)
	return nil
}

// RemainingHolders reports how many accounts still hold the role. The shell
// uses it to say what it just did; nothing decides anything with it.
func (s *Service) RemainingHolders(ctx context.Context, role string) (int, error) {
	return s.Grants.CountActiveSystemGrants(ctx, role)
}

func (s *Service) record(ctx context.Context, actor coreaudit.Actor, action, userID, role string) {
	if s.Audit == nil {
		return
	}
	s.Audit.Record(ctx, coreaudit.Event{
		ActorType:  actor.Type,
		ActorID:    actor.ID,
		Action:     action,
		TargetType: auditTarget,
		TargetID:   userID,
		Detail:     role,
	})
}
