package db

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
)

// eidIssuer returns an issuer unique to this test run. external_identity rows
// are not cleaned up when a test user is deleted (there is no FK cascade), so a
// fixed issuer would make (issuer, subject) collide with a previous run against
// the same database. A per-run issuer keeps each run's identities disjoint.
func eidIssuer(t *testing.T) string {
	t.Helper()
	return "https://" + testPublicID(t) + ".okta.example"
}

func auditCount(t *testing.T, s *Store, ctx context.Context, action, targetID string) int64 {
	t.Helper()
	var n int64
	if err := s.db.WithContext(ctx).Model(&auditEventRow{}).
		Where("action = ? AND target_id = ?", action, targetID).Count(&n).Error; err != nil {
		t.Fatalf("count audit %q: %v", action, err)
	}
	return n
}

// TestExternalIdentityLinkAndLookup covers the round trip and the two lookups
// the association algorithm branches on.
func TestExternalIdentityLinkAndLookup(t *testing.T) {
	s, ctx := newTestStore(t)
	issuer := eidIssuer(t)
	userID := newTestUser(t, s, "eidlink")

	link, err := s.LinkExisting(ctx, coreidentity.LinkIdentity{
		UserID: userID, Issuer: issuer, Subject: "okta|abc",
		Seen: coreidentity.SeenClaims{Email: "person@example.com", Name: "Person"},
	})
	if err != nil {
		t.Fatalf("LinkExisting: %v", err)
	}
	if link.ID == "" || link.UserID != userID {
		t.Errorf("link = %+v", link)
	}

	got, err := s.IdentityBySubject(ctx, issuer, "okta|abc")
	if err != nil || got == nil {
		t.Fatalf("IdentityBySubject = %v, %v", got, err)
	}
	if got.UserID != userID || got.LastSeenEmail != "person@example.com" {
		t.Errorf("resolved = %+v", got)
	}

	byIssuer, err := s.IdentityByUserAndIssuer(ctx, userID, issuer)
	if err != nil || byIssuer == nil || byIssuer.Subject != "okta|abc" {
		t.Fatalf("IdentityByUserAndIssuer = %+v, %v", byIssuer, err)
	}

	// An unknown subject is nil, not an error: a first login relies on this.
	if got, err := s.IdentityBySubject(ctx, issuer, "okta|nobody"); err != nil || got != nil {
		t.Errorf("unknown subject = %v, %v; want nil, nil", got, err)
	}

	// The link records an audit row in the same transaction.
	if n := auditCount(t, s, ctx, coreaudit.UserExternalIdentityLinked, userID); n != 1 {
		t.Errorf("linked audit rows = %d, want 1", n)
	}
}

// TestExternalIdentityUniqueness pins the two invariants: one subject to one
// account, and one identity per issuer per account.
func TestExternalIdentityUniqueness(t *testing.T) {
	s, ctx := newTestStore(t)
	issuer := eidIssuer(t)
	userA := newTestUser(t, s, "eiduniqa")
	userB := newTestUser(t, s, "eiduniqb")

	if _, err := s.LinkExisting(ctx, coreidentity.LinkIdentity{
		UserID: userA, Issuer: issuer, Subject: "okta|shared",
	}); err != nil {
		t.Fatalf("seed link: %v", err)
	}

	// Same (issuer, subject) to a different account: refused.
	_, err := s.LinkExisting(ctx, coreidentity.LinkIdentity{
		UserID: userB, Issuer: issuer, Subject: "okta|shared",
	})
	if !errors.Is(err, coreidentity.ErrIdentityConflict) {
		t.Errorf("second subject link err = %v, want ErrIdentityConflict", err)
	}

	// Same account, same issuer, different subject: also refused.
	_, err = s.LinkExisting(ctx, coreidentity.LinkIdentity{
		UserID: userA, Issuer: issuer, Subject: "okta|other",
	})
	if !errors.Is(err, coreidentity.ErrIdentityConflict) {
		t.Errorf("second issuer link err = %v, want ErrIdentityConflict", err)
	}
}

// TestExternalIdentitySubjectIsCaseSensitive covers the binary collation: two
// subjects differing only in case are distinct identities, not one.
func TestExternalIdentitySubjectIsCaseSensitive(t *testing.T) {
	s, ctx := newTestStore(t)
	issuer := eidIssuer(t)
	userLower := newTestUser(t, s, "eidlower")
	userUpper := newTestUser(t, s, "eidupper")

	if _, err := s.LinkExisting(ctx, coreidentity.LinkIdentity{
		UserID: userLower, Issuer: issuer, Subject: "casesub",
	}); err != nil {
		t.Fatalf("link lower: %v", err)
	}
	// Differs only in case: must be admitted as a distinct subject.
	if _, err := s.LinkExisting(ctx, coreidentity.LinkIdentity{
		UserID: userUpper, Issuer: issuer, Subject: "CASESUB",
	}); err != nil {
		t.Fatalf("link upper should be a distinct subject: %v", err)
	}
	lower, _ := s.IdentityBySubject(ctx, issuer, "casesub")
	upper, _ := s.IdentityBySubject(ctx, issuer, "CASESUB")
	if lower == nil || upper == nil || lower.UserID == upper.UserID {
		t.Errorf("case variants collapsed: lower=%+v upper=%+v", lower, upper)
	}
}

// TestCreateUserWithIdentity covers JIT: the account, its personal Space, the
// owner membership, the link, and the two audit rows all commit together.
func TestCreateUserWithIdentity(t *testing.T) {
	s, ctx := newTestStore(t)
	issuer := eidIssuer(t)
	email := "jit-" + testPublicID(t) + "@example.com"

	user, link, err := s.CreateUserWithIdentity(ctx, coreidentity.ProvisionUser{
		Email: email, Name: "JIT", QuotaTier: "free_trial",
		Issuer: issuer, Subject: "okta|jit",
	})
	if err != nil {
		t.Fatalf("CreateUserWithIdentity: %v", err)
	}
	t.Cleanup(func() { deleteTestUser(t, s, user.ID) })

	if user.Email != email || user.ID == "" {
		t.Errorf("user = %+v", user)
	}
	if link.UserID != user.ID || link.Subject != "okta|jit" {
		t.Errorf("link = %+v", link)
	}

	// The account is real and reachable by its subject.
	if got, _ := s.IdentityBySubject(ctx, issuer, "okta|jit"); got == nil || got.UserID != user.ID {
		t.Errorf("subject does not resolve to the provisioned account: %+v", got)
	}
	// It has a personal Space it owns.
	personal, err := s.GetPersonalSpaceByUser(ctx, user.ID)
	if err != nil || personal == nil {
		t.Errorf("provisioned account has no personal space: %v, %v", personal, err)
	}
	// Both the creation and the link are recorded.
	if n := auditCount(t, s, ctx, coreaudit.UserCreated, user.ID); n != 1 {
		t.Errorf("created audit rows = %d, want 1", n)
	}
	if n := auditCount(t, s, ctx, coreaudit.UserExternalIdentityLinked, user.ID); n != 1 {
		t.Errorf("linked audit rows = %d, want 1", n)
	}
}

// TestCreateUserWithIdentityRollsBackOnConflict is the atomicity claim: if the
// identity link cannot be created, the account is not either.
func TestCreateUserWithIdentityRollsBackOnConflict(t *testing.T) {
	s, ctx := newTestStore(t)
	issuer := eidIssuer(t)
	existing := newTestUser(t, s, "eidtaken")
	if _, err := s.LinkExisting(ctx, coreidentity.LinkIdentity{
		UserID: existing, Issuer: issuer, Subject: "okta|taken",
	}); err != nil {
		t.Fatalf("seed link: %v", err)
	}

	email := "rollback-" + testPublicID(t) + "@example.com"
	_, _, err := s.CreateUserWithIdentity(ctx, coreidentity.ProvisionUser{
		Email: email, Issuer: issuer, Subject: "okta|taken",
	})
	if !errors.Is(err, coreidentity.ErrIdentityConflict) {
		t.Fatalf("err = %v, want ErrIdentityConflict", err)
	}
	// The account must not have been created behind the failed link.
	u, err := s.UserByEmail(ctx, email)
	if err != nil {
		t.Fatalf("UserByEmail: %v", err)
	}
	if u != nil {
		deleteTestUser(t, s, u.ID)
		t.Fatalf("a user was created despite the identity conflict: %+v", u)
	}
}

// TestConcurrentFirstLoginProvisionsOnce covers two simultaneous first logins
// for one subject: exactly one account is created, the other is refused with a
// conflict the association service re-resolves.
func TestConcurrentFirstLoginProvisionsOnce(t *testing.T) {
	s, _ := newTestStore(t)
	issuer := eidIssuer(t)
	email := "race-" + testPublicID(t) + "@example.com"

	var wg sync.WaitGroup
	results := make([]error, 2)
	users := make([]*coreidentity.User, 2)
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			u, _, err := s.CreateUserWithIdentity(context.Background(), coreidentity.ProvisionUser{
				Email: email, Issuer: issuer, Subject: "okta|race",
			})
			results[i], users[i] = err, u
		}(i)
	}
	wg.Wait()

	oks := 0
	for i, err := range results {
		switch {
		case err == nil:
			oks++
			if users[i] != nil {
				t.Cleanup(func() { deleteTestUser(t, s, users[i].ID) })
			}
		case errors.Is(err, coreidentity.ErrEmailExists), errors.Is(err, coreidentity.ErrIdentityConflict):
			// The loser's expected refusal.
		default:
			t.Errorf("unexpected error from concurrent provision: %v", err)
		}
	}
	if oks != 1 {
		t.Errorf("%d concurrent first logins succeeded, want exactly 1", oks)
	}
}

// TestUnlinkOnlyWhenDisabled covers the operator recovery precondition and its
// atomic audit.
func TestUnlinkOnlyWhenDisabled(t *testing.T) {
	s, ctx := newTestStore(t)
	issuer := eidIssuer(t)
	userID := newTestUser(t, s, "eidunlink")
	link, err := s.LinkExisting(ctx, coreidentity.LinkIdentity{
		UserID: userID, Issuer: issuer, Subject: "okta|unlink",
	})
	if err != nil {
		t.Fatalf("seed link: %v", err)
	}

	// Enabled: refused.
	err = s.UnlinkIdentity(ctx, coreidentity.UnlinkIdentity{UserID: userID, IdentityID: link.ID, ActorID: "u_admin"})
	if !errors.Is(err, coreidentity.ErrUnlinkRequiresDisabled) {
		t.Fatalf("enabled unlink err = %v, want ErrUnlinkRequiresDisabled", err)
	}

	// Disable, then unlink succeeds and records the deletion.
	disabledAt := time.Now().UTC()
	if err := s.SetUserDisabled(ctx, userID, &disabledAt); err != nil {
		t.Fatalf("SetUserDisabled: %v", err)
	}
	if err := s.UnlinkIdentity(ctx, coreidentity.UnlinkIdentity{UserID: userID, IdentityID: link.ID, ActorID: "u_admin"}); err != nil {
		t.Fatalf("disabled unlink: %v", err)
	}
	if got, _ := s.IdentityBySubject(ctx, issuer, "okta|unlink"); got != nil {
		t.Errorf("link survived the unlink: %+v", got)
	}
	if n := auditCount(t, s, ctx, coreaudit.UserExternalIdentityUnlinked, userID); n != 1 {
		t.Errorf("unlinked audit rows = %d, want 1", n)
	}

	// A second unlink of the now-gone link is a not-found, not a success.
	err = s.UnlinkIdentity(ctx, coreidentity.UnlinkIdentity{UserID: userID, IdentityID: link.ID, ActorID: "u_admin"})
	if !errors.Is(err, coreidentity.ErrIdentityNotFound) {
		t.Errorf("second unlink err = %v, want ErrIdentityNotFound", err)
	}
}

// TestUpdateLastSeen covers the drift snapshot after a repeat sign-in.
func TestUpdateLastSeen(t *testing.T) {
	s, ctx := newTestStore(t)
	issuer := eidIssuer(t)
	userID := newTestUser(t, s, "eidseen")
	if _, err := s.LinkExisting(ctx, coreidentity.LinkIdentity{
		UserID: userID, Issuer: issuer, Subject: "okta|seen",
		Seen: coreidentity.SeenClaims{Email: "old@example.com", Name: "Old"},
	}); err != nil {
		t.Fatalf("seed link: %v", err)
	}
	later := time.Now().UTC().Add(time.Hour)
	if err := s.UpdateLastSeen(ctx, issuer, "okta|seen",
		coreidentity.SeenClaims{Email: "new@example.com", Name: "New"}, later); err != nil {
		t.Fatalf("UpdateLastSeen: %v", err)
	}
	got, _ := s.IdentityBySubject(ctx, issuer, "okta|seen")
	if got == nil || got.LastSeenEmail != "new@example.com" || got.LastSeenName != "New" {
		t.Errorf("after update = %+v, want the new attributes", got)
	}
}
