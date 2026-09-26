package db

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/config"
	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
)

// The store is the only implementation the server wires in, so a signature
// drift here should stop the build rather than surface as a nil interface at
// startup.
var _ coreidentity.SystemGrantStore = (*Store)(nil)

func openGrantStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn, Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, ctx
}

// TestSystemGrantLifecycle is the lockout exercise from
// docs/design/system-administration.md section 11, at the store level: an
// authority can be granted, seen, revoked, and granted again, and the count
// that guards the last grant tracks it throughout.
func TestSystemGrantLifecycle(t *testing.T) {
	s, ctx := openGrantStore(t)
	userID := newTestUser(t, s, "grant")

	before, err := s.CountActiveSystemGrants(ctx, coreidentity.SystemRoleAdmin)
	if err != nil {
		t.Fatalf("CountActiveSystemGrants: %v", err)
	}

	grant, err := s.GrantSystemRole(ctx, userID, coreidentity.SystemRoleAdmin, coreaudit.ActorOperator, time.Unix(100, 0).UTC())
	if err != nil {
		t.Fatalf("GrantSystemRole: %v", err)
	}
	if grant.ID == "" || grant.RevokedAt != nil || !grant.Active() {
		t.Fatalf("new grant should be active with an id: %+v", grant)
	}

	roles, err := s.ActiveSystemRoles(ctx, userID)
	if err != nil {
		t.Fatalf("ActiveSystemRoles: %v", err)
	}
	if len(roles) != 1 || roles[0] != coreidentity.SystemRoleAdmin {
		t.Fatalf("ActiveSystemRoles = %v, want [%s]", roles, coreidentity.SystemRoleAdmin)
	}

	if n, err := s.CountActiveSystemGrants(ctx, coreidentity.SystemRoleAdmin); err != nil || n != before+1 {
		t.Fatalf("CountActiveSystemGrants = %d, %v; want %d", n, err, before+1)
	}

	// A second grant of a role already held is refused rather than silently
	// creating a second row, so a caller can say "already an admin".
	if _, err := s.GrantSystemRole(ctx, userID, coreidentity.SystemRoleAdmin, "u_other", time.Unix(101, 0).UTC()); !errors.Is(err, coreidentity.ErrSystemGrantExists) {
		t.Fatalf("second grant err = %v, want ErrSystemGrantExists", err)
	}

	found, err := s.RevokeSystemRole(ctx, userID, coreidentity.SystemRoleAdmin, time.Unix(200, 0).UTC(), false)
	if err != nil || !found {
		t.Fatalf("RevokeSystemRole = %v, %v; want true, nil", found, err)
	}
	roles, err = s.ActiveSystemRoles(ctx, userID)
	if err != nil || len(roles) != 0 {
		t.Fatalf("after revoke ActiveSystemRoles = %v, %v; want empty", roles, err)
	}
	if n, err := s.CountActiveSystemGrants(ctx, coreidentity.SystemRoleAdmin); err != nil || n != before {
		t.Fatalf("after revoke count = %d, %v; want %d", n, err, before)
	}

	// Revoking again is not an error: the end state is what was asked for.
	if found, err := s.RevokeSystemRole(ctx, userID, coreidentity.SystemRoleAdmin, time.Unix(201, 0).UTC(), false); err != nil || found {
		t.Fatalf("second revoke = %v, %v; want false, nil", found, err)
	}

	// The revoked row stays, and a re-grant does not collide with it. This is
	// what the (user_id, role, live_marker) unique index has to allow: the
	// retired row's marker is NULL, so it never collides with the new live one.
	regrant, err := s.GrantSystemRole(ctx, userID, coreidentity.SystemRoleAdmin, "u_admin", time.Unix(300, 0).UTC())
	if err != nil {
		t.Fatalf("re-grant after revoke: %v", err)
	}
	if regrant.ID == grant.ID {
		t.Errorf("re-grant reused the retired row's id %q", regrant.ID)
	}

	all, err := s.ListSystemGrants(ctx, true)
	if err != nil {
		t.Fatalf("ListSystemGrants: %v", err)
	}
	live, retired := 0, 0
	for _, g := range all {
		if g.UserID != userID {
			continue
		}
		if g.Active() {
			live++
		} else {
			retired++
		}
	}
	if live != 1 || retired != 1 {
		t.Errorf("history for %s: %d live, %d retired; want 1 and 1", userID, live, retired)
	}

	active, err := s.ListSystemGrants(ctx, false)
	if err != nil {
		t.Fatalf("ListSystemGrants(false): %v", err)
	}
	for _, g := range active {
		if !g.Active() {
			t.Errorf("ListSystemGrants(false) returned a revoked grant: %+v", g)
		}
	}
}

// TestGrantSystemRoleRejectsUnknownRole pins the decision that the role column
// exists for a future role but accepts only roles this build authorizes. A
// store that took any string would make the column a way to invent authority.
func TestGrantSystemRoleRejectsUnknownRole(t *testing.T) {
	s, ctx := openGrantStore(t)
	userID := newTestUser(t, s, "grant")

	if _, err := s.GrantSystemRole(ctx, userID, "system_observer", coreaudit.ActorOperator, time.Unix(100, 0).UTC()); !errors.Is(err, coreidentity.ErrSystemRoleUnknown) {
		t.Fatalf("GrantSystemRole(system_observer) err = %v, want ErrSystemRoleUnknown", err)
	}
	roles, err := s.ActiveSystemRoles(ctx, userID)
	if err != nil || len(roles) != 0 {
		t.Fatalf("ActiveSystemRoles = %v, %v; want empty", roles, err)
	}
}

// TestActiveSystemRolesIsEmptyForOrdinaryUsers is the case that runs on every
// authenticated request: almost nobody holds a grant, and the answer must be
// empty rather than an error.
func TestActiveSystemRolesIsEmptyForOrdinaryUsers(t *testing.T) {
	s, ctx := openGrantStore(t)
	roles, err := s.ActiveSystemRoles(ctx, testPublicID(t))
	if err != nil {
		t.Fatalf("ActiveSystemRoles: %v", err)
	}
	if len(roles) != 0 {
		t.Errorf("ActiveSystemRoles for an ungranted user = %v, want empty", roles)
	}
	if roles, err := s.ActiveSystemRoles(ctx, ""); err != nil || len(roles) != 0 {
		t.Errorf("ActiveSystemRoles(\"\") = %v, %v; want empty, nil", roles, err)
	}
}

// resetAdmins revokes every live system_admin grant so a last-holder test can
// reason about a known count. It uses the shell path (keepLastHolder=false)
// because that is the only one allowed to empty the role.
func resetAdmins(t *testing.T, s *Store, ctx context.Context) {
	t.Helper()
	grants, err := s.ListSystemGrants(ctx, false)
	if err != nil {
		t.Fatalf("ListSystemGrants: %v", err)
	}
	for _, g := range grants {
		if g.Role != coreidentity.SystemRoleAdmin {
			continue
		}
		if _, err := s.RevokeSystemRole(ctx, g.UserID, g.Role, time.Now().UTC(), false); err != nil {
			t.Fatalf("resetAdmins revoke %s: %v", g.UserID, err)
		}
	}
}

// TestGrantSystemRoleConcurrentOneLiveRow is the live-grant uniqueness rule in
// docs/design/system-administration.md §5.1: many grants of one role to one
// account race, and exactly one live row may result. The unique
// index on (user_id, role, live_marker) is what enforces it — the old index
// over revoked_at let NULLs be distinct and permitted several live rows.
func TestGrantSystemRoleConcurrentOneLiveRow(t *testing.T) {
	s, ctx := openGrantStore(t)
	userID := newTestUser(t, s, "grant")

	before, err := s.CountActiveSystemGrants(ctx, coreidentity.SystemRoleAdmin)
	if err != nil {
		t.Fatalf("CountActiveSystemGrants: %v", err)
	}

	const attempts = 20
	var wg sync.WaitGroup
	results := make([]error, attempts)
	for i := range attempts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, results[i] = s.GrantSystemRole(ctx, userID, coreidentity.SystemRoleAdmin, "u_admin", time.Unix(int64(400+i), 0).UTC())
		}(i)
	}
	wg.Wait()

	success, exists := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, coreidentity.ErrSystemGrantExists):
			exists++
		default:
			t.Fatalf("unexpected grant error: %v", err)
		}
	}
	if success != 1 {
		t.Errorf("concurrent grants: %d succeeded, want exactly 1 (%d reported already-held)", success, exists)
	}
	if n, err := s.CountActiveSystemGrants(ctx, coreidentity.SystemRoleAdmin); err != nil || n != before+1 {
		t.Errorf("effective holders after race = %d, %v; want %d", n, err, before+1)
	}
}

// TestRevokeSystemRoleConcurrentKeepsLastHolder is the atomic last-holder rule
// in docs/design/system-administration.md §5.1: two administrators revoke
// different holders at the same time, and the deployment must not end with
// nobody. One revoke wins and
// the other is refused with ErrSystemGrantLastHolder.
func TestRevokeSystemRoleConcurrentKeepsLastHolder(t *testing.T) {
	s, ctx := openGrantStore(t)
	resetAdmins(t, s, ctx)
	u1 := newTestUser(t, s, "grant")
	u2 := newTestUser(t, s, "grant")
	for _, u := range []string{u1, u2} {
		if _, err := s.GrantSystemRole(ctx, u, coreidentity.SystemRoleAdmin, "u_admin", time.Now().UTC()); err != nil {
			t.Fatalf("GrantSystemRole(%s): %v", u, err)
		}
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, u := range []string{u1, u2} {
		wg.Add(1)
		go func(i int, u string) {
			defer wg.Done()
			_, errs[i] = s.RevokeSystemRole(ctx, u, coreidentity.SystemRoleAdmin, time.Now().UTC(), true)
		}(i, u)
	}
	wg.Wait()

	ok, lastHolder := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, coreidentity.ErrSystemGrantLastHolder):
			lastHolder++
		default:
			t.Fatalf("unexpected revoke error: %v", err)
		}
	}
	if ok != 1 || lastHolder != 1 {
		t.Errorf("concurrent last-holder revoke: %d succeeded, %d refused; want 1 and 1", ok, lastHolder)
	}
	if n, err := s.CountActiveSystemGrants(ctx, coreidentity.SystemRoleAdmin); err != nil || n != 1 {
		t.Errorf("effective holders after concurrent revoke = %d, %v; want 1", n, err)
	}
}

// TestSystemGrantEffectiveExcludesDisabled is the effective-holder rule in
// docs/design/system-administration.md §5.1: a grant on a disabled account is
// not an effective holder, because the account is refused before its grant is
// consulted. So it does not count toward the last-holder
// rule, and revoking the one enabled holder is refused even though a disabled
// grant still exists.
func TestSystemGrantEffectiveExcludesDisabled(t *testing.T) {
	s, ctx := openGrantStore(t)
	resetAdmins(t, s, ctx)
	enabled := newTestUser(t, s, "grant")
	disabled := newTestUser(t, s, "grant")
	for _, u := range []string{enabled, disabled} {
		if _, err := s.GrantSystemRole(ctx, u, coreidentity.SystemRoleAdmin, "u_admin", time.Now().UTC()); err != nil {
			t.Fatalf("GrantSystemRole(%s): %v", u, err)
		}
	}
	disabledAt := time.Now().UTC()
	if err := s.SetUserDisabled(ctx, disabled, &disabledAt); err != nil {
		t.Fatalf("SetUserDisabled: %v", err)
	}

	// Two grants exist, but only one is effective.
	if n, err := s.CountActiveSystemGrants(ctx, coreidentity.SystemRoleAdmin); err != nil || n != 1 {
		t.Fatalf("effective holders with one disabled = %d, %v; want 1", n, err)
	}

	// Revoking the only enabled holder is refused: the disabled grant cannot
	// keep the deployment reachable.
	if _, err := s.RevokeSystemRole(ctx, enabled, coreidentity.SystemRoleAdmin, time.Now().UTC(), true); !errors.Is(err, coreidentity.ErrSystemGrantLastHolder) {
		t.Fatalf("revoke last effective holder err = %v, want ErrSystemGrantLastHolder", err)
	}

	// The shell may still empty the role.
	if found, err := s.RevokeSystemRole(ctx, enabled, coreidentity.SystemRoleAdmin, time.Now().UTC(), false); err != nil || !found {
		t.Fatalf("shell revoke = %v, %v; want true, nil", found, err)
	}
}

// TestDisablingTheLastEffectiveHolderIsRefused is the other half of the
// effective-holder rule: disabling an account revokes every credential, so disabling the
// last effective administrator would lock the deployment out just as revoking
// the grant would. The store refuses and the account stays enabled.
func TestDisablingTheLastEffectiveHolderIsRefused(t *testing.T) {
	s, ctx := openGrantStore(t)
	resetAdmins(t, s, ctx)
	only := newTestUser(t, s, "grant")
	if _, err := s.GrantSystemRole(ctx, only, coreidentity.SystemRoleAdmin, "u_admin", time.Now().UTC()); err != nil {
		t.Fatalf("GrantSystemRole: %v", err)
	}

	disabledAt := time.Now().UTC()
	if err := s.SetUserDisabled(ctx, only, &disabledAt); !errors.Is(err, coreidentity.ErrSystemGrantLastHolder) {
		t.Fatalf("SetUserDisabled err = %v, want ErrSystemGrantLastHolder", err)
	}
	user, err := s.GetUser(ctx, only)
	if err != nil || user == nil {
		t.Fatalf("GetUser: %v", err)
	}
	if user.Disabled() {
		t.Errorf("the account was disabled despite the refusal")
	}
}

// TestDisablingWhenAnotherHolderRemainsSucceeds pins that the refusal is about
// the last one, not about disabling an administrator at all.
func TestDisablingWhenAnotherHolderRemainsSucceeds(t *testing.T) {
	s, ctx := openGrantStore(t)
	resetAdmins(t, s, ctx)
	u1 := newTestUser(t, s, "grant")
	u2 := newTestUser(t, s, "grant")
	for _, u := range []string{u1, u2} {
		if _, err := s.GrantSystemRole(ctx, u, coreidentity.SystemRoleAdmin, "u_admin", time.Now().UTC()); err != nil {
			t.Fatalf("GrantSystemRole(%s): %v", u, err)
		}
	}

	disabledAt := time.Now().UTC()
	if err := s.SetUserDisabled(ctx, u1, &disabledAt); err != nil {
		t.Fatalf("SetUserDisabled: %v", err)
	}
	if n, err := s.CountActiveSystemGrants(ctx, coreidentity.SystemRoleAdmin); err != nil || n != 1 {
		t.Errorf("effective holders after disabling one of two = %d, %v; want 1", n, err)
	}
}

// TestConcurrentRevokeAndDisableKeepOneHolder is the effective-holder rule under
// contention: a revoke of one holder and a disable of the other race, and the
// deployment must not end with nobody effective. Both take the same lock on the
// role's live grants, so one wins and the other is refused.
func TestConcurrentRevokeAndDisableKeepOneHolder(t *testing.T) {
	s, ctx := openGrantStore(t)
	resetAdmins(t, s, ctx)
	revokeTarget := newTestUser(t, s, "grant")
	disableTarget := newTestUser(t, s, "grant")
	for _, u := range []string{revokeTarget, disableTarget} {
		if _, err := s.GrantSystemRole(ctx, u, coreidentity.SystemRoleAdmin, "u_admin", time.Now().UTC()); err != nil {
			t.Fatalf("GrantSystemRole(%s): %v", u, err)
		}
	}

	var wg sync.WaitGroup
	var revokeErr, disableErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, revokeErr = s.RevokeSystemRole(ctx, revokeTarget, coreidentity.SystemRoleAdmin, time.Now().UTC(), true)
	}()
	go func() {
		defer wg.Done()
		at := time.Now().UTC()
		disableErr = s.SetUserDisabled(ctx, disableTarget, &at)
	}()
	wg.Wait()

	refused := 0
	if errors.Is(revokeErr, coreidentity.ErrSystemGrantLastHolder) {
		refused++
	} else if revokeErr != nil {
		t.Fatalf("unexpected revoke error: %v", revokeErr)
	}
	if errors.Is(disableErr, coreidentity.ErrSystemGrantLastHolder) {
		refused++
	} else if disableErr != nil {
		t.Fatalf("unexpected disable error: %v", disableErr)
	}
	if refused != 1 {
		t.Errorf("revokeErr=%v disableErr=%v; want exactly one last-holder refusal", revokeErr, disableErr)
	}
	if n, err := s.CountActiveSystemGrants(ctx, coreidentity.SystemRoleAdmin); err != nil || n != 1 {
		t.Errorf("effective holders after the race = %d, %v; want 1", n, err)
	}
}
