package identity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
)

// Refusals a refresh can produce.
var (
	// ErrRefreshNotConfigured means this deployment issues no refresh tokens.
	ErrRefreshNotConfigured = apierr.New(apierr.KindNotConfigured, "refresh not configured")
	// ErrRefreshTokenRequired means the request carried none.
	ErrRefreshTokenRequired = apierr.New(apierr.KindInvalid, "refresh_token required")
)

// InvalidRefresh is what every failed refresh answers.
//
// One value for a token that never existed, one that was already spent, and one
// whose account is gone: a caller who can tell them apart can learn whether a
// stolen token was ever real. The reason is for the log.
type InvalidRefresh struct {
	Reason string
	// Reused marks the case worth alarming on: a token presented after it was
	// rotated means a copy exists somewhere.
	Reused    bool
	UserID    string
	SessionID string
}

func (e *InvalidRefresh) Error() string { return "invalid refresh token" }

// RefreshResult is the next pair in a session that is still good.
type RefreshResult struct {
	User         coreidentity.User
	SessionID    string
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64
}

// Refresh exchanges a refresh token for the next pair.
//
// Three of its refusals revoke the session on the way out, which is why they
// are decided here rather than by a transport: a reused token means a copy
// exists, and an account that is gone or switched off outranks any row that
// still rotates. Answering without revoking would leave the copy working.
func (s *Service) Refresh(ctx context.Context, token string) (*RefreshResult, error) {
	if s.Users == nil || s.Tokens == nil {
		return nil, ErrNotConfigured
	}
	if s.RefreshTokens == nil {
		return nil, ErrRefreshNotConfigured
	}
	if token == "" {
		return nil, ErrRefreshTokenRequired
	}
	now := s.now()
	rotated, err := s.RefreshTokens.RotateRefreshToken(ctx, token, now, s.RefreshTTL, s.RotationGrace)
	switch {
	case errors.Is(err, coreidentity.ErrRefreshTokenReused):
		// The store has already revoked this session's refresh tokens; revoke the
		// session record too so a still-valid access token under it stops working.
		// Record it: this is the one signal a deployment gets that a credential was
		// copied, and it arrives without anyone reporting anything.
		s.revokeSession(ctx, rotated.SessionID, now, "refresh token reused")
		s.recordReuse(ctx, rotated.UserID, rotated.SessionID)
		return nil, &InvalidRefresh{
			Reason: "presented after rotation", Reused: true,
			UserID: rotated.UserID, SessionID: rotated.SessionID,
		}
	case errors.Is(err, coreidentity.ErrRefreshTokenInvalid):
		return nil, &InvalidRefresh{Reason: "unknown or expired"}
	case err != nil:
		return nil, fmt.Errorf("rotate refresh token: %w", err)
	}

	// The refresh token outlives many access tokens, so the account behind it
	// is re-checked here rather than trusted from the login it descends from.
	user, err := s.Users.GetUser(ctx, rotated.UserID)
	if err != nil {
		return nil, fmt.Errorf("read account: %w", err)
	}
	if user == nil {
		s.revokeSession(ctx, rotated.SessionID, now, "missing user")
		return nil, &InvalidRefresh{
			Reason: "the account is gone", UserID: rotated.UserID, SessionID: rotated.SessionID,
		}
	}
	// A disabled account's sessions are revoked when it is disabled, so a
	// refresh token that still rotates was issued after that or escaped the
	// sweep. Either way the account is the authority, not the row: revoke the
	// session this one belongs to and say why.
	if user.Disabled() {
		s.revokeSession(ctx, rotated.SessionID, now, "disabled user")
		return nil, ErrDisabled
	}
	// The refresh token still rotated, but the session's absolute expiry can pass
	// before the token's does. The session is the authority, so a login past its
	// ceiling ends here rather than renewing.
	if s.Sessions != nil {
		if _, err := s.Sessions.ActiveSession(ctx, rotated.SessionID, now); err != nil {
			if errors.Is(err, coreidentity.ErrSessionInactive) {
				s.revokeSession(ctx, rotated.SessionID, now, "session expired or revoked")
				return nil, &InvalidRefresh{
					Reason: "session is no longer active", UserID: rotated.UserID, SessionID: rotated.SessionID,
				}
			}
			return nil, fmt.Errorf("read session: %w", err)
		}
		if err := s.Sessions.TouchSession(ctx, rotated.SessionID, now); err != nil {
			slog.Warn("touch session failed", "err", err, "session_id", rotated.SessionID)
		}
	}

	accessToken, ttl, err := s.Tokens.Mint(user.ID, rotated.SessionID, now)
	if err != nil {
		return nil, fmt.Errorf("mint access token: %w", err)
	}
	return &RefreshResult{
		User: *user, SessionID: rotated.SessionID,
		AccessToken: accessToken, RefreshToken: rotated.Plaintext,
		ExpiresIn: int64(ttl.Seconds()),
	}, nil
}

func (s *Service) recordReuse(ctx context.Context, userID, sessionID string) {
	if s.Audit == nil {
		return
	}
	s.Audit.Record(ctx, coreaudit.Event{
		ActorType:  coreaudit.ActorSystem,
		ActorID:    userID,
		Action:     coreaudit.RefreshReuse,
		TargetType: "auth_session",
		TargetID:   sessionID,
	})
}

// revokeSession ends a session a refusal has just decided against. A failure
// here is logged rather than returned: the caller is being refused either way,
// and turning a cleanup failure into a different answer would tell them
// something about the account.
func (s *Service) revokeSession(ctx context.Context, sessionID string, now time.Time, why string) {
	if s.Sessions == nil {
		return
	}
	if _, err := s.Sessions.RevokeSession(ctx, sessionID, now); err != nil {
		slog.Error("revoke session failed", "err", err, "reason", why, "session_id", sessionID)
	}
}
