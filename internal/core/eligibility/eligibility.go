// Package eligibility answers one question every path that admits or dispatches
// unattended Agent work must ask: may this principal run work in this Space right
// now? It is the single authority contract behind personnel deactivation — see
// docs/design/system-administration.md §8.2. The two facts it reads,
// an account that is not disabled and a live space_member row, are the same gates
// the per-request HTTP guard already enforces; this package makes them reachable
// from the durable Schedule, run-dispatch, and worker-fetch paths that never pass
// through an HTTP handler.
package eligibility

import (
	"context"
	"errors"
	"fmt"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
)

// ErrAccountDisabled means the initiating account is disabled or no longer
// exists. Both refuse identically: neither can carry authority into a run, and a
// caller recording why it stopped work treats them the same.
var ErrAccountDisabled = errors.New("initiating account is disabled")

// ErrNotSpaceMember means the account is enabled but holds no membership in the
// target Space, so it may not drive work there even from a durable Schedule or
// Workflow that it created while it was still a member.
var ErrNotSpaceMember = errors.New("account is not a member of the space")

// ErrUnavailable means eligibility could not be determined because an authority
// store failed. It is not a refusal: each caller decides whether to defer or
// proceed and let a later gate re-check. See
// docs/design/system-administration.md §8.2 for each gate's choice.
var ErrUnavailable = errors.New("eligibility could not be determined")

// Checker reports whether userID may run work in spaceID. It answers only the
// admission question; it does not decide role-specific management rights, because
// ordinary membership is enough to run work under current Space policy.
type Checker interface {
	Check(ctx context.Context, userID, spaceID string) error
}

type storeChecker struct {
	users  coreidentity.UserStore
	spaces corespace.Store
}

// New builds a Checker over the account and Space authority stores.
func New(users coreidentity.UserStore, spaces corespace.Store) Checker {
	return storeChecker{users: users, spaces: spaces}
}

func (c storeChecker) Check(ctx context.Context, userID, spaceID string) error {
	user, err := c.users.GetUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("%w: load account: %v", ErrUnavailable, err)
	}
	// A missing account is refused like a disabled one: a deleted or never-valid
	// initiator has no authority to lend a run.
	if user == nil || user.Disabled() {
		return ErrAccountDisabled
	}
	members, err := c.spaces.ListSpaceMembers(ctx, spaceID)
	if err != nil {
		return fmt.Errorf("%w: load membership: %v", ErrUnavailable, err)
	}
	if corespace.EffectiveRoleOf(members, userID) == "" {
		return ErrNotSpaceMember
	}
	return nil
}
