package identity

import (
	"context"
	"errors"
	"time"
)

// Token lifetimes used when a deployment does not choose. The access token is
// a signed JWT the server never stores, so the only way to retire one early is
// to wait for it to expire; the refresh token is a stored row and can be
// revoked at any time. That asymmetry is why the long-lived half is the stored
// one.
const (
	// AccessTokenTTLDefault is short: the access token is a signed JWT the server
	// never stores, so this is the window a stolen one still works before the
	// guard's session check gets another chance to refuse it. The refresh token
	// carries the day-to-day usability, so shortening this does not sign anyone
	// out — it just means a client refreshes more often.
	AccessTokenTTLDefault       = 15 * time.Minute
	RefreshTokenTTLDefault      = 30 * 24 * time.Hour
	RefreshRotationGraceDefault = 30 * time.Second
)

// ErrRefreshTokenInvalid means the token is unknown, expired, or belongs to a
// session that has been revoked. The three are deliberately indistinguishable
// to the caller.
var ErrRefreshTokenInvalid = errors.New("refresh token invalid")

// ErrRefreshTokenReused means a token that had already been rotated was
// presented again after the grace window. Either the client is replaying a
// credential it should have discarded, or someone else has a copy — and there
// is no way to tell which. The session is revoked before this is returned.
var ErrRefreshTokenReused = errors.New("refresh token reused")

// NewRefreshToken describes a token to issue.
type NewRefreshToken struct {
	UserID string
	// SessionID names one login chain. Every rotation keeps it, so revoking a
	// session retires the whole chain rather than one link of it.
	SessionID string
	// Platform records which surface logged in ("portal", "cli", "desktop").
	// It is a label for the operator reading the session list, not something
	// the server enforces.
	Platform string
	TTL      time.Duration
}

// RotatedRefreshToken is the result of exchanging one refresh token for the
// next. Plaintext is returned once and never recoverable afterwards.
type RotatedRefreshToken struct {
	UserID    string
	SessionID string
	Plaintext string
	ExpiresAt time.Time
}

// RefreshTokenStore issues, rotates, and revokes the stored half of a login.
//
// Rotation is what makes a stolen refresh token detectable: each exchange
// spends the presented token and hands back a new one, so the same token
// appearing twice means two holders. See RotateRefreshToken for what the store
// does about that, and why a short grace window has to exist.
type RefreshTokenStore interface {
	// CreateRefreshToken issues a token and returns the plaintext, which is
	// never stored — the row holds a hash, so a database backup yields no
	// usable credentials.
	CreateRefreshToken(ctx context.Context, in NewRefreshToken) (plaintext string, expiresAt time.Time, err error)

	// RotateRefreshToken exchanges plaintext for a fresh token in the same
	// session, spending the presented one.
	//
	// Within grace of having been spent, a token may be exchanged again. That
	// window is not a concession to sloppy clients: BuildMax's CLI and Desktop
	// share one credentials file across independent processes, and two of them
	// refreshing at the same moment is normal rather than suspicious. Both
	// receive a usable token; both stay in the same session.
	//
	// Past the grace window a spent token means ErrRefreshTokenReused, and the
	// whole session is revoked first — logging out the legitimate holder is the
	// correct response when a credential may be in two hands. That error comes
	// back with UserID and SessionID populated and Plaintext empty, so the
	// caller can record what was revoked.
	RotateRefreshToken(ctx context.Context, plaintext string, now time.Time, ttl, grace time.Duration) (RotatedRefreshToken, error)

	// RevokeRefreshTokenSession revokes the refresh tokens the token belongs to
	// and reports whose session it was, so the caller can revoke the session
	// record too. An unknown token is not an error: logging out something already
	// gone is a success.
	RevokeRefreshTokenSession(ctx context.Context, plaintext string, now time.Time) (userID, sessionID string, err error)

	// DeleteExpiredRefreshTokens removes rows that can no longer be exchanged.
	DeleteExpiredRefreshTokens(ctx context.Context, before time.Time) (int64, error)
}

// Session listing, counting, and revocation moved to AuthSessionStore: the
// durable auth_session row is the authority for a login, and revoking it
// cascades to these refresh tokens.
