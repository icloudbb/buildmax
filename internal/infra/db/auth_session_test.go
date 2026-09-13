package db

import (
	"context"
	"errors"
	"testing"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
)

func newTestSession(t *testing.T, s *Store, ctx context.Context, userID string, absoluteTTL time.Duration) string {
	t.Helper()
	sid, err := s.CreateSession(ctx, coreidentity.NewAuthSession{
		UserID: userID, Platform: "portal", AuthMethod: "login_code",
		AbsoluteExpiresAt: time.Now().UTC().Add(absoluteTTL),
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return sid
}

// TestAuthSessionActiveThenRevoked is the authority the guard reads: a session
// is active until it is revoked, and a revoked one is indistinguishable from a
// missing one.
func TestAuthSessionActiveThenRevoked(t *testing.T) {
	s, ctx := newTestStore(t)
	userID := newTestUser(t, s, "sessionactive")
	now := time.Now().UTC()
	sid := newTestSession(t, s, ctx, userID, time.Hour)

	sess, err := s.ActiveSession(ctx, sid, now)
	if err != nil {
		t.Fatalf("ActiveSession: %v", err)
	}
	if sess.SID != sid || sess.UserID != userID {
		t.Errorf("session = %+v, want sid %q user %q", sess, sid, userID)
	}
	if sess.AuthMethod != "login_code" || sess.Platform != "portal" {
		t.Errorf("metadata = %+v, want portal/login_code", sess)
	}

	if _, err := s.RevokeSession(ctx, sid, now); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	if _, err := s.ActiveSession(ctx, sid, now); !errors.Is(err, coreidentity.ErrSessionInactive) {
		t.Errorf("a revoked session is still active: err = %v", err)
	}
	// An unknown sid is the same inactive answer, never a different error.
	if _, err := s.ActiveSession(ctx, "as_never", now); !errors.Is(err, coreidentity.ErrSessionInactive) {
		t.Errorf("unknown sid: err = %v, want ErrSessionInactive", err)
	}
}

// TestAuthSessionAbsoluteExpiry proves the ceiling: past absolute_expires_at the
// session is inactive no matter that it was never revoked.
func TestAuthSessionAbsoluteExpiry(t *testing.T) {
	s, ctx := newTestStore(t)
	userID := newTestUser(t, s, "sessionexpiry")
	sid := newTestSession(t, s, ctx, userID, time.Hour)

	past := time.Now().UTC().Add(2 * time.Hour)
	if _, err := s.ActiveSession(ctx, sid, past); !errors.Is(err, coreidentity.ErrSessionInactive) {
		t.Errorf("a session past its absolute expiry is still active: err = %v", err)
	}
}

// TestAuthSessionTouchThrottled proves TouchSession updates last_seen, but not on
// every call: within the throttle it leaves the row alone.
func TestAuthSessionTouchThrottled(t *testing.T) {
	s, ctx := newTestStore(t)
	userID := newTestUser(t, s, "sessiontouch")
	sid := newTestSession(t, s, ctx, userID, time.Hour)
	base := time.Now().UTC()

	if err := s.TouchSession(ctx, sid, base); err != nil {
		t.Fatalf("TouchSession: %v", err)
	}
	first, err := s.ActiveSession(ctx, sid, base)
	if err != nil {
		t.Fatalf("ActiveSession: %v", err)
	}
	if first.LastSeenAt.IsZero() {
		t.Fatal("last_seen_at was not set by the first touch")
	}

	// Within the throttle: no write, last_seen unchanged.
	if err := s.TouchSession(ctx, sid, base.Add(sessionTouchThrottle/2)); err != nil {
		t.Fatalf("TouchSession within throttle: %v", err)
	}
	within, _ := s.ActiveSession(ctx, sid, base.Add(sessionTouchThrottle/2))
	if !within.LastSeenAt.Equal(first.LastSeenAt) {
		t.Errorf("last_seen moved inside the throttle: %v -> %v", first.LastSeenAt, within.LastSeenAt)
	}

	// Past the throttle: the write lands.
	later := base.Add(2 * sessionTouchThrottle)
	if err := s.TouchSession(ctx, sid, later); err != nil {
		t.Fatalf("TouchSession past throttle: %v", err)
	}
	after, _ := s.ActiveSession(ctx, sid, later)
	if !after.LastSeenAt.After(first.LastSeenAt) {
		t.Errorf("last_seen did not advance past the throttle: %v -> %v", first.LastSeenAt, after.LastSeenAt)
	}
}

// TestAuthSessionRevokeCascadesToRefreshTokens proves revoking a session retires
// its refresh tokens too, so a chain cannot keep rotating after its login ends.
func TestAuthSessionRevokeCascadesToRefreshTokens(t *testing.T) {
	s, ctx := newTestStore(t)
	userID := newTestUser(t, s, "sessioncascade")
	now := time.Now().UTC()
	sid := newTestSession(t, s, ctx, userID, time.Hour)

	plaintext, _, err := s.CreateRefreshToken(ctx, coreidentity.NewRefreshToken{
		UserID: userID, SessionID: sid, Platform: "portal", TTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("CreateRefreshToken: %v", err)
	}
	if _, err := s.RevokeSession(ctx, sid, now); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	if _, err := s.RotateRefreshToken(ctx, plaintext, now, time.Hour, 30*time.Second); !errors.Is(err, coreidentity.ErrRefreshTokenInvalid) {
		t.Errorf("refresh token survived its session's revocation: err = %v", err)
	}
}

// TestListAndCountUserSessions is the projection the admin session list is built
// on: one row per session, newest first, and none that has been revoked.
func TestListAndCountUserSessions(t *testing.T) {
	s, ctx := newTestStore(t)
	userID := newTestUser(t, s, "sessionlist")
	now := time.Now().UTC()

	laptop := newTestSession(t, s, ctx, userID, time.Hour)
	phone := newTestSession(t, s, ctx, userID, time.Hour)

	if n, err := s.CountUserSessions(ctx, userID, now); err != nil || n != 2 {
		t.Fatalf("CountUserSessions = %d, %v; want 2", n, err)
	}
	sessions, err := s.ListUserSessions(ctx, userID, now)
	if err != nil {
		t.Fatalf("ListUserSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d, want 2: %+v", len(sessions), sessions)
	}

	if _, err := s.RevokeSession(ctx, laptop, now); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	after, err := s.ListUserSessions(ctx, userID, now)
	if err != nil {
		t.Fatalf("ListUserSessions: %v", err)
	}
	if len(after) != 1 || after[0].SID != phone {
		t.Fatalf("after revoke = %+v, want only %q", after, phone)
	}
}
