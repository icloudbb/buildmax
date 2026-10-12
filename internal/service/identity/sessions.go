package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
)

// ErrSessionNotFound refuses revoking a session that is not a live session of
// the named account. Another account's session, an unknown id, and one already
// revoked or expired all get it, so a caller learns nothing about sessions it
// does not own.
var ErrSessionNotFound = apierr.New(apierr.KindNotFound, "no live session with that id for this account")

// ErrSessionsNotConfigured means this deployment has no session store.
var ErrSessionsNotConfigured = apierr.New(apierr.KindNotConfigured, "sessions not configured")

// ListUserSessions returns the account's live sessions, newest first.
func (s *Service) ListUserSessions(ctx context.Context, userID string) ([]coreidentity.AuthSession, error) {
	if s.Sessions == nil {
		return nil, ErrSessionsNotConfigured
	}
	sessions, err := s.Sessions.ListUserSessions(ctx, userID, s.now().UTC())
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return sessions, nil
}

// RevokeUserSessionCmd names one session of one account, and who is ending it:
// the account itself, or an administrator acting on it.
type RevokeUserSessionCmd struct {
	ActorID   string
	UserID    string
	SessionID string
}

// RevokeUserSession ends one live session of the account, leaving its other
// sessions live, and reports how many it retired (0 only when a concurrent
// revoke won).
//
// A session id alone names its chain, but the caller claims an account and the
// audit event is recorded against it, so the session must be one of that
// account's live sessions. Anything else is ErrSessionNotFound rather than a
// revoke recorded against the wrong person.
func (s *Service) RevokeUserSession(ctx context.Context, cmd RevokeUserSessionCmd) (int64, error) {
	if s.Sessions == nil {
		return 0, ErrSessionsNotConfigured
	}
	now := s.now().UTC()
	sess, err := s.Sessions.ActiveSession(ctx, cmd.SessionID, now)
	if errors.Is(err, coreidentity.ErrSessionInactive) {
		return 0, ErrSessionNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("read session: %w", err)
	}
	if sess.UserID != cmd.UserID {
		return 0, ErrSessionNotFound
	}
	n, err := s.Sessions.RevokeSession(ctx, cmd.SessionID, now)
	if err != nil {
		return 0, fmt.Errorf("revoke session: %w", err)
	}
	s.record(ctx, coreaudit.Event{
		ActorType: coreaudit.ActorUser, ActorID: cmd.ActorID,
		Action: coreaudit.SessionRevoked, TargetType: "user", TargetID: cmd.UserID,
		Detail: cmd.SessionID,
	})
	return n, nil
}
