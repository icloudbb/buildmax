package db

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
)

// newTestSignIn stores a pending request with fresh plaintexts and removes it
// afterwards, whatever became of it.
func newTestSignIn(t *testing.T, s *Store, ctx context.Context, now time.Time, ttl time.Duration) (code, secret string) {
	t.Helper()
	code = strings.ToUpper(testPublicID(t)[:8])
	secret = "bmxsignin_" + testPublicID(t) + testPublicID(t)
	t.Cleanup(func() {
		_ = s.db.Where("poll_secret_hash = ?", hashNativeSignInValue(secret)).Delete(&nativeSignInRow{}).Error
	})
	if err := s.CreateNativeSignIn(ctx, coreidentity.NewNativeSignIn{
		UserCode: code, PollSecret: secret, Platform: coreidentity.PlatformCLI,
		ClientLabel: "alice-mbp · macOS", CreatedAt: now, ExpiresAt: now.Add(ttl),
	}); err != nil {
		t.Fatalf("CreateNativeSignIn: %v", err)
	}
	return code, secret
}

func newPortalSession(t *testing.T, s *Store, ctx context.Context, userID, method string, expiresAt time.Time) string {
	t.Helper()
	sid, err := s.CreateSession(ctx, coreidentity.NewAuthSession{
		UserID: userID, Platform: coreidentity.PlatformPortal, AuthMethod: method, AbsoluteExpiresAt: expiresAt,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return sid
}

func confirmTestSignIn(t *testing.T, s *Store, ctx context.Context, code, userID, sid string, at time.Time) {
	t.Helper()
	if err := s.DecideNativeSignIn(ctx, code, coreidentity.NativeSignInDecision{
		State: coreidentity.NativeSignInConfirmed, UserID: userID, SessionID: sid, At: at,
	}); err != nil {
		t.Fatalf("DecideNativeSignIn: %v", err)
	}
}

// TestNativeSignInStoresOnlyHashesUnderUniqueIndexes: a database read recovers
// neither the code nor the secret, and a colliding code is reported so the
// caller draws another.
func TestNativeSignInStoresOnlyHashesUnderUniqueIndexes(t *testing.T) {
	s, ctx := newTestStore(t)
	now := time.Now().UTC()
	code, secret := newTestSignIn(t, s, ctx, now, coreidentity.NativeSignInTTL)

	var row nativeSignInRow
	if err := s.db.Where("poll_secret_hash = ?", hashNativeSignInValue(secret)).Take(&row).Error; err != nil {
		t.Fatalf("read row: %v", err)
	}
	if row.UserCodeHash != hashNativeSignInValue(code) || strings.Contains(row.UserCodeHash, code) ||
		strings.Contains(row.PollSecretHash, secret) || len(row.UserCodeHash) != 64 {
		t.Errorf("row = %+v; want only SHA-256 hashes", row)
	}
	if row.State != "pending" || row.IntervalSeconds != 5 || row.ClientLabel != "alice-mbp · macOS" {
		t.Errorf("row = %+v", row)
	}

	other := "bmxsignin_" + testPublicID(t)
	t.Cleanup(func() {
		_ = s.db.Where("poll_secret_hash = ?", hashNativeSignInValue(other)).Delete(&nativeSignInRow{}).Error
	})
	err := s.CreateNativeSignIn(ctx, coreidentity.NewNativeSignIn{
		UserCode: code, PollSecret: other, Platform: coreidentity.PlatformCLI, CreatedAt: now, ExpiresAt: now.Add(time.Minute),
	})
	if !errors.Is(err, coreidentity.ErrUserCodeTaken) {
		t.Errorf("a colliding code = %v, want ErrUserCodeTaken", err)
	}
	err = s.CreateNativeSignIn(ctx, coreidentity.NewNativeSignIn{
		UserCode: strings.ToUpper(testPublicID(t)[:8]), PollSecret: secret, Platform: coreidentity.PlatformCLI,
		CreatedAt: now, ExpiresAt: now.Add(time.Minute),
	})
	if err == nil || errors.Is(err, coreidentity.ErrUserCodeTaken) {
		t.Errorf("a colliding poll secret = %v, want the unique index to refuse it", err)
	}

	n, err := s.CountPendingNativeSignIns(ctx, now)
	if err != nil || n < 1 {
		t.Errorf("CountPendingNativeSignIns = %d, %v", n, err)
	}
}

// TestNativeSignInDecisionAndPacing: the first decision wins, and polls are
// paced in the row.
func TestNativeSignInDecisionAndPacing(t *testing.T) {
	s, ctx := newTestStore(t)
	userID := newTestUser(t, s, "nsidecide")
	now := time.Now().UTC()
	sid := newPortalSession(t, s, ctx, userID, "password", now.Add(time.Hour))
	code, secret := newTestSignIn(t, s, ctx, now, coreidentity.NativeSignInTTL)

	req, slow, err := s.PollNativeSignIn(ctx, secret, now.Add(time.Second))
	if err != nil || !slow || req.Interval != 10*time.Second {
		t.Fatalf("early poll = %+v, %v, %v; want slow_down raising the interval to 10s", req, slow, err)
	}
	req, slow, err = s.PollNativeSignIn(ctx, secret, now.Add(12*time.Second))
	if err != nil || slow || req.State != coreidentity.NativeSignInPending || req.Interval != 10*time.Second {
		t.Fatalf("on-time poll = %+v, %v, %v", req, slow, err)
	}

	if got, err := s.PendingNativeSignIn(ctx, code, now); err != nil || got.Platform != "cli" || got.ClientLabel != "alice-mbp · macOS" {
		t.Fatalf("PendingNativeSignIn = %+v, %v", got, err)
	}
	confirmTestSignIn(t, s, ctx, code, userID, sid, now)
	for _, state := range []coreidentity.NativeSignInState{coreidentity.NativeSignInDenied, coreidentity.NativeSignInConfirmed} {
		err := s.DecideNativeSignIn(ctx, code, coreidentity.NativeSignInDecision{State: state, UserID: userID, SessionID: sid, At: now})
		if !errors.Is(err, coreidentity.ErrNativeSignInNotFound) {
			t.Errorf("second decision (%s) = %v, want ErrNativeSignInNotFound", state, err)
		}
	}
	if _, err := s.PendingNativeSignIn(ctx, code, now); !errors.Is(err, coreidentity.ErrNativeSignInNotFound) {
		t.Errorf("a decided request still looks pending: %v", err)
	}

	expiredCode, _ := newTestSignIn(t, s, ctx, now, time.Minute)
	err = s.DecideNativeSignIn(ctx, expiredCode, coreidentity.NativeSignInDecision{
		State: coreidentity.NativeSignInConfirmed, UserID: userID, SessionID: sid, At: now.Add(2 * time.Minute),
	})
	if !errors.Is(err, coreidentity.ErrNativeSignInNotFound) {
		t.Errorf("deciding an expired request = %v, want ErrNativeSignInNotFound", err)
	}
}

// TestNativeSignInRedemptionInheritsAndDeletes: the session carries the
// request's platform and label and the confirming session's method and exact
// absolute expiry; the refresh token works; the request is gone.
func TestNativeSignInRedemptionInheritsAndDeletes(t *testing.T) {
	s, ctx := newTestStore(t)
	userID := newTestUser(t, s, "nsiredeem")
	now := time.Now().UTC()
	portalExpiry := now.Add(11*time.Hour + 123456*time.Microsecond).Truncate(time.Microsecond)
	sid := newPortalSession(t, s, ctx, userID, "oidc", portalExpiry)
	code, secret := newTestSignIn(t, s, ctx, now, coreidentity.NativeSignInTTL)
	confirmTestSignIn(t, s, ctx, code, userID, sid, now)

	got, err := s.RedeemNativeSignIn(ctx, secret, now.Add(time.Minute), time.Hour)
	if err != nil {
		t.Fatalf("RedeemNativeSignIn: %v", err)
	}
	if got.UserID != userID || got.Platform != "cli" || got.AuthMethod != "oidc" || got.SessionID == "" || got.RefreshToken == "" {
		t.Fatalf("redemption = %+v", got)
	}
	sess, err := s.ActiveSession(ctx, got.SessionID, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("ActiveSession: %v", err)
	}
	if sess.Platform != "cli" || sess.AuthMethod != "oidc" || sess.ClientLabel != "alice-mbp · macOS" ||
		!sess.AbsoluteExpiresAt.Equal(portalExpiry) {
		t.Errorf("native session = %+v; want cli, oidc, the label, expiry %v exactly", sess, portalExpiry)
	}
	listed, err := s.ListUserSessions(ctx, userID, now)
	if err != nil || len(listed) != 2 {
		t.Fatalf("ListUserSessions = %+v, %v", listed, err)
	}
	if _, err := s.RotateRefreshToken(ctx, got.RefreshToken, now.Add(2*time.Minute), time.Hour, time.Second); err != nil {
		t.Errorf("the redeemed refresh token does not rotate: %v", err)
	}
	if _, _, err := s.PollNativeSignIn(ctx, secret, now.Add(2*time.Minute)); !errors.Is(err, coreidentity.ErrNativeSignInNotFound) {
		t.Errorf("poll after redemption = %v, want ErrNativeSignInNotFound", err)
	}
	if _, err := s.RedeemNativeSignIn(ctx, secret, now.Add(2*time.Minute), time.Hour); !errors.Is(err, coreidentity.ErrNativeSignInNotFound) {
		t.Errorf("second redemption = %v, want ErrNativeSignInNotFound", err)
	}
}

// TestNativeSignInConcurrentRedemptionCreatesOneSession is the single-use
// property under the row lock: of many redemptions racing on one confirmed
// request, exactly one creates a session.
func TestNativeSignInConcurrentRedemptionCreatesOneSession(t *testing.T) {
	s, ctx := newTestStore(t)
	userID := newTestUser(t, s, "nsirace")
	now := time.Now().UTC()
	sid := newPortalSession(t, s, ctx, userID, "password", now.Add(time.Hour))
	code, secret := newTestSignIn(t, s, ctx, now, coreidentity.NativeSignInTTL)
	confirmTestSignIn(t, s, ctx, code, userID, sid, now)

	const racers = 8
	var wg sync.WaitGroup
	errs := make(chan error, racers)
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.RedeemNativeSignIn(ctx, secret, now.Add(time.Minute), time.Hour)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	wins := 0
	for err := range errs {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, coreidentity.ErrNativeSignInNotFound):
		default:
			t.Errorf("redemption = %v", err)
		}
	}
	native := 0
	listed, err := s.ListUserSessions(ctx, userID, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, sess := range listed {
		if sess.Platform == "cli" {
			native++
		}
	}
	if wins != 1 || native != 1 {
		t.Errorf("%d redemptions succeeded and %d native sessions exist; want exactly one of each", wins, native)
	}
}

// TestNativeSignInRedemptionRefusals: an account disabled, or a confirming
// session revoked, after confirmation yields no session, and the request is
// marked denied so it answers the same way until it expires.
func TestNativeSignInRedemptionRefusals(t *testing.T) {
	cases := map[string]func(t *testing.T, s *Store, ctx context.Context, userID, sid string, now time.Time){
		"account disabled": func(t *testing.T, s *Store, ctx context.Context, userID, _ string, now time.Time) {
			if err := s.SetUserDisabled(ctx, userID, &now); err != nil {
				t.Fatal(err)
			}
		},
		"confirming session revoked": func(t *testing.T, s *Store, ctx context.Context, _, sid string, now time.Time) {
			if _, err := s.RevokeSession(ctx, sid, now); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			s, ctx := newTestStore(t)
			userID := newTestUser(t, s, "nsirefuse")
			now := time.Now().UTC()
			sid := newPortalSession(t, s, ctx, userID, "password", now.Add(time.Hour))
			code, secret := newTestSignIn(t, s, ctx, now, coreidentity.NativeSignInTTL)
			confirmTestSignIn(t, s, ctx, code, userID, sid, now)
			change(t, s, ctx, userID, sid, now)

			if _, err := s.RedeemNativeSignIn(ctx, secret, now.Add(time.Minute), time.Hour); !errors.Is(err, coreidentity.ErrNativeSignInRefused) {
				t.Fatalf("redeem = %v, want ErrNativeSignInRefused", err)
			}
			req, _, err := s.PollNativeSignIn(ctx, secret, now.Add(2*time.Minute))
			if err != nil || req.State != coreidentity.NativeSignInDenied {
				t.Errorf("after a refusal the request is %+v, %v; want denied", req, err)
			}
			var native int64
			key, _ := lookupKey(ctx, s.db, "user", userID)
			if err := s.db.Model(&authSessionRow{}).Where("user_id = ? AND platform = ?", key, "cli").Count(&native).Error; err != nil || native != 0 {
				t.Errorf("%d native sessions after a refusal (%v)", native, err)
			}
		})
	}
}

func TestDeleteExpiredNativeSignIns(t *testing.T) {
	s, ctx := newTestStore(t)
	now := time.Now().UTC()
	_, live := newTestSignIn(t, s, ctx, now, time.Hour)
	_, expired := newTestSignIn(t, s, ctx, now.Add(-time.Hour), time.Minute)

	n, err := s.DeleteExpiredNativeSignIns(ctx, now)
	if err != nil || n < 1 {
		t.Fatalf("DeleteExpiredNativeSignIns = %d, %v", n, err)
	}
	var left int64
	s.db.Model(&nativeSignInRow{}).Where("poll_secret_hash = ?", hashNativeSignInValue(expired)).Count(&left)
	if left != 0 {
		t.Error("an expired request survived the sweep")
	}
	s.db.Model(&nativeSignInRow{}).Where("poll_secret_hash = ?", hashNativeSignInValue(live)).Count(&left)
	if left != 1 {
		t.Error("the sweep removed a live request")
	}
}

// TestAuthSessionClientLabel: the label is stored as given and absent as NULL.
func TestAuthSessionClientLabel(t *testing.T) {
	s, ctx := newTestStore(t)
	userID := newTestUser(t, s, "sessionlabel")
	now := time.Now().UTC()
	labelled, err := s.CreateSession(ctx, coreidentity.NewAuthSession{
		UserID: userID, Platform: "desktop", AuthMethod: "password", ClientLabel: "alice-mbp · macOS",
		AbsoluteExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	plain := newPortalSession(t, s, ctx, userID, "password", now.Add(time.Hour))
	got, err := s.ActiveSession(ctx, labelled, now)
	if err != nil || got.ClientLabel != "alice-mbp · macOS" {
		t.Errorf("labelled session = %+v, %v", got, err)
	}
	var row authSessionRow
	if err := s.db.Where("public_id = ?", plain).Take(&row).Error; err != nil || row.ClientLabel != nil {
		t.Errorf("an unlabelled session stored %v (%v), want NULL", row.ClientLabel, err)
	}
}
