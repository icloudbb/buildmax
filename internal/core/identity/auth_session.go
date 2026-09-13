package identity

import (
	"context"
	"errors"
	"time"
)

// SessionAbsoluteTTLDefault bounds how long one login may live regardless of
// how often its refresh token rotates. Rotation keeps a session usable
// indefinitely otherwise, so the absolute cap is what makes "a session cannot
// renew forever" true. It is the local/password/login-code default; a stricter
// bound (an OIDC session_max_age, say) is a separate, shorter cap layered on
// top.
const SessionAbsoluteTTLDefault = 90 * 24 * time.Hour

// ErrSessionInactive means the named session does not exist, was revoked, or has
// passed its absolute expiry. The three are deliberately indistinguishable to
// the caller: each means the same thing to a request presenting a token under
// that session — you are not signed in.
var ErrSessionInactive = errors.New("session inactive")

// AuthSession is the durable authority for one login.
//
// The access token is a signed JWT the server never stores, so before this row
// existed there was no way to stop honouring an issued one early: logout and
// revocation could only retire the refresh token, and the bearer kept working
// until it expired. The guard now resolves this row on every request, so
// revoking it, or its absolute expiry passing, stops an already-issued access
// token on the next call. The refresh token is subordinate — it belongs to a
// session and holds only rotation state.
type AuthSession struct {
	// SID is the public session identifier. It is not a secret: it is already a
	// claim (`sid`) in every access token issued under it, and it is the handle a
	// revoke names.
	SID string
	// UserID is the account whose authority the session exercises.
	UserID string
	// Platform is the surface that logged in ("portal", "cli", "desktop"), a
	// label for the operator reading the session list rather than something the
	// server enforces.
	Platform string
	// AuthMethod is the proof that opened the session ("password", "login_code").
	AuthMethod string
	// CreatedAt is when the login happened.
	CreatedAt time.Time
	// LastSeenAt is the most recent time a request or refresh used the session.
	// Zero until the first touch.
	LastSeenAt time.Time
	// AbsoluteExpiresAt is the hard ceiling: past it the session is inactive no
	// matter how recently it was used.
	AbsoluteExpiresAt time.Time
}

// NewAuthSession describes a session to open.
type NewAuthSession struct {
	UserID            string
	Platform          string
	AuthMethod        string
	AbsoluteExpiresAt time.Time
}

// AuthSessionStore owns the lifecycle of the durable session record.
//
// Revocation cascades: revoking a session also retires the refresh tokens that
// belong to it, so the whole login dies at once rather than leaving a chain that
// still rotates.
type AuthSessionStore interface {
	// CreateSession opens a session and returns its public id, which becomes the
	// `sid` claim of the access token and the session_id of the first refresh
	// token.
	CreateSession(ctx context.Context, in NewAuthSession) (sid string, err error)

	// ActiveSession returns the session if it exists, is not revoked, and has not
	// passed its absolute expiry; otherwise ErrSessionInactive. This is the
	// per-request authority check the guard makes.
	ActiveSession(ctx context.Context, sid string, now time.Time) (AuthSession, error)

	// TouchSession records that the session was used, at most once per the store's
	// own throttle so an active session does not mean a write per request. A
	// missing or inactive session is not an error: touching something already
	// gone is a no-op.
	TouchSession(ctx context.Context, sid string, now time.Time) error

	// RevokeSession revokes one session and its refresh tokens, returning how many
	// sessions it retired (0 or 1). An unknown or already-revoked session is not
	// an error.
	RevokeSession(ctx context.Context, sid string, now time.Time) (int64, error)

	// RevokeUserSessions revokes every live session the user has, and their
	// refresh tokens, returning how many sessions it retired. This is what "sign
	// them out everywhere" and disabling an account do to the credential.
	RevokeUserSessions(ctx context.Context, userID string, now time.Time) (int64, error)

	// CountUserSessions counts the user's live sessions.
	CountUserSessions(ctx context.Context, userID string, now time.Time) (int, error)

	// ListUserSessions returns the user's live sessions as safe metadata, newest
	// first, so an administrator can revoke one device rather than all of them.
	ListUserSessions(ctx context.Context, userID string, now time.Time) ([]AuthSession, error)
}
