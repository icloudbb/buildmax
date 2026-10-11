package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// A native sign-in request lets a CLI or Desktop obtain a session that a
// signed-in Portal session confirms, shaped after OAuth 2.0 Device
// Authorization (RFC 8628). The client starts it anonymously and polls with a
// secret; a person confirms or denies it in the Portal by its user code; the
// first poll after a confirmation redeems it for an ordinary session. See
// docs/design/enterprise-identity-and-access.md §14.

// Session platforms. The label a session carries for the operator reading the
// session list; a native sign-in request accepts only the two native ones, and
// only a Portal session may confirm one.
const (
	PlatformPortal  = "portal"
	PlatformCLI     = "cli"
	PlatformDesktop = "desktop"
)

// The bounds of one request. They are constants rather than configuration
// until a deployment shows a need (design §14.6).
const (
	// NativeSignInTTL is fixed: long enough to sign in through an IdP, short
	// enough to bound phishing and guessing. Confirmation must land within it.
	NativeSignInTTL = 10 * time.Minute
	// NativeSignInInterval is the poll interval a request starts with;
	// NativeSignInSlowDownStep is what each too-early poll adds to it, up to
	// NativeSignInMaxInterval (RFC 8628 §3.5).
	NativeSignInInterval     = 5 * time.Second
	NativeSignInSlowDownStep = 5 * time.Second
	NativeSignInMaxInterval  = 30 * time.Second
	// NativeSignInPendingCap bounds the anonymous start: at most this many
	// unexpired pending requests deployment-wide.
	NativeSignInPendingCap = 500
	// ClientLabelMaxLength is in characters, not bytes.
	ClientLabelMaxLength = 64
	// nativeSignInPollSecretPrefix makes a leaked poll secret recognizable to
	// a secret scanner, as bmxlogin_ and bmxrefresh_ are.
	nativeSignInPollSecretPrefix = "bmxsignin_"
	nativeSignInPollSecretBytes  = 32
)

// NativeSignInState is where a request is in its life. A redeemed request is
// not a state: redemption deletes the row.
type NativeSignInState string

const (
	NativeSignInPending   NativeSignInState = "pending"
	NativeSignInConfirmed NativeSignInState = "confirmed"
	NativeSignInDenied    NativeSignInState = "denied"
)

var (
	// ErrNativeSignInNotFound means no live request answers to the code or
	// secret: unknown, expired, already decided (for a decision), or already
	// redeemed. The cases are indistinguishable on purpose.
	ErrNativeSignInNotFound = errors.New("native sign-in request not found")
	// ErrUserCodeTaken means a new request's user code collided with a live
	// one. The caller draws another code.
	ErrUserCodeTaken = errors.New("user code already in use")
	// ErrNativeSignInRefused means a confirmed request can no longer be
	// redeemed: the confirming account was disabled or its Portal session
	// ended first.
	ErrNativeSignInRefused = errors.New("native sign-in refused")
	// ErrClientLabelInvalid refuses a label that is too long or carries a
	// character a person cannot see, such as a control or bidi override.
	ErrClientLabelInvalid = errors.New("client_label must be at most 64 printable characters")
)

// NativeSignIn is one request as the store holds it. It carries no secret: the
// store keeps only hashes of the user code and poll secret.
type NativeSignIn struct {
	// Platform is the native client kind, PlatformCLI or PlatformDesktop.
	Platform string
	// ClientLabel is what the client says it is, such as "alice-mbp · macOS".
	// It is shown to the person confirming and never trusted.
	ClientLabel string
	State       NativeSignInState
	// Interval is the current poll interval; LastPolledAt is zero until the
	// first poll.
	Interval     time.Duration
	LastPolledAt time.Time
	// DecidedByUserID and DecidedSessionID name the account and Portal session
	// that confirmed or denied the request. The native session inherits that
	// session's method and absolute expiry.
	DecidedByUserID  string
	DecidedSessionID string
	DecidedAt        time.Time
	ExpiresAt        time.Time
	CreatedAt        time.Time
}

// Poll applies RFC 8628 pacing to one poll at now and reports whether it came
// earlier than the stored interval. A too-early poll raises the interval by
// NativeSignInSlowDownStep, up to NativeSignInMaxInterval. Every poll, early or
// not, becomes the one the next is measured from; the first is measured from
// the request's creation.
func (r *NativeSignIn) Poll(now time.Time) (slowDown bool) {
	interval := r.Interval
	if interval <= 0 {
		interval = NativeSignInInterval
	}
	last := r.LastPolledAt
	if last.IsZero() {
		last = r.CreatedAt
	}
	r.LastPolledAt = now
	if now.Sub(last) < interval {
		r.Interval = min(interval+NativeSignInSlowDownStep, NativeSignInMaxInterval)
		return true
	}
	r.Interval = interval
	return false
}

// RedeemableBy reports whether a confirmed request may still become a session,
// given the confirming account and its Portal session as they stand now. The
// session is nil when it is no longer active (revoked or past its expiry). A
// confirmation is a decision about a moment; redemption happens later, and an
// account disabled or a session revoked in between must not yield a session.
func (r *NativeSignIn) RedeemableBy(user *User, confirming *AuthSession, now time.Time) bool {
	if r.State != NativeSignInConfirmed || !now.Before(r.ExpiresAt) {
		return false
	}
	if user == nil || user.Disabled() || user.IsService() || user.ID != r.DecidedByUserID {
		return false
	}
	if confirming == nil || confirming.SID != r.DecidedSessionID || confirming.UserID != user.ID {
		return false
	}
	return now.Before(confirming.AbsoluteExpiresAt)
}

// SessionFor describes the session a redeemable request becomes. It never
// outlives the sign-in that confirmed it: the method and the absolute expiry
// are the confirming session's, exactly, while the platform and the claimed
// label are the request's.
func (r *NativeSignIn) SessionFor(confirming *AuthSession) NewAuthSession {
	return NewAuthSession{
		UserID:            confirming.UserID,
		Platform:          r.Platform,
		AuthMethod:        confirming.AuthMethod,
		ClientLabel:       r.ClientLabel,
		AbsoluteExpiresAt: confirming.AbsoluteExpiresAt,
	}
}

// NativeSignInDecision is a confirmation or denial.
type NativeSignInDecision struct {
	// State is NativeSignInConfirmed or NativeSignInDenied.
	State     NativeSignInState
	UserID    string
	SessionID string
	At        time.Time
}

// NewNativeSignIn describes a request to store. UserCode and PollSecret are
// plaintext here and hashed by the store; neither is ever written as given.
type NewNativeSignIn struct {
	// UserCode is in NormalizeUserCode form.
	UserCode    string
	PollSecret  string
	Platform    string
	ClientLabel string
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// NativeSignInRedemption is the session a redemption created.
type NativeSignInRedemption struct {
	UserID       string
	SessionID    string
	Platform     string
	AuthMethod   string
	RefreshToken string
}

// NativeSignInStore owns native sign-in requests. Every lookup is by the hash
// of a normalized user code or of a poll secret, under a unique index.
type NativeSignInStore interface {
	// CountPendingNativeSignIns counts unexpired pending requests, for the
	// start cap.
	CountPendingNativeSignIns(ctx context.Context, now time.Time) (int, error)

	// CreateNativeSignIn stores a request. A user code that collides with a
	// stored one returns ErrUserCodeTaken.
	CreateNativeSignIn(ctx context.Context, in NewNativeSignIn) error

	// PendingNativeSignIn returns the unexpired pending request a user code
	// names, or ErrNativeSignInNotFound.
	PendingNativeSignIn(ctx context.Context, userCode string, now time.Time) (NativeSignIn, error)

	// DecideNativeSignIn records the first decision on an unexpired pending
	// request. Anything else — unknown, expired, or already decided — returns
	// ErrNativeSignInNotFound, so a second decision never overrides the first.
	DecideNativeSignIn(ctx context.Context, userCode string, d NativeSignInDecision) error

	// PollNativeSignIn applies NativeSignIn.Poll to the unexpired request a
	// poll secret names, under a row lock, and returns the request after the
	// poll with whether it came too early. An unknown secret, an expired
	// request, and a redeemed one return ErrNativeSignInNotFound.
	PollNativeSignIn(ctx context.Context, pollSecret string, now time.Time) (req NativeSignIn, slowDown bool, err error)

	// RedeemNativeSignIn turns a confirmed request into a session in one
	// transaction: lock the request, re-check it with RedeemableBy against the
	// current account and confirming session, create the session described by
	// SessionFor and its refresh token, and delete the request. Concurrent
	// redemptions of one request create exactly one session; the others return
	// ErrNativeSignInNotFound. A request that is no longer redeemable is marked
	// denied, creates nothing, and returns ErrNativeSignInRefused.
	RedeemNativeSignIn(ctx context.Context, pollSecret string, now time.Time, refreshTTL time.Duration) (NativeSignInRedemption, error)

	// DeleteExpiredNativeSignIns removes requests that expired before the
	// given time.
	DeleteExpiredNativeSignIns(ctx context.Context, before time.Time) (int64, error)
}

// NewNativeSignInPollSecret returns a fresh poll secret: the bearer that
// redeems the session, so it carries full entropy.
func NewNativeSignInPollSecret() (string, error) {
	b := make([]byte, nativeSignInPollSecretBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return nativeSignInPollSecretPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// ValidNativePlatform reports whether a platform may start a native sign-in.
func ValidNativePlatform(platform string) bool {
	return platform == PlatformCLI || platform == PlatformDesktop
}

// NormalizeClientLabel trims a client-reported label and refuses one longer
// than ClientLabelMaxLength characters or carrying a character that is not
// printable. The label is shown beside a security decision, so an invisible
// character that reorders or hides text is refused rather than stored.
func NormalizeClientLabel(label string) (string, error) {
	label = strings.TrimSpace(label)
	if !utf8.ValidString(label) || utf8.RuneCountInString(label) > ClientLabelMaxLength {
		return "", ErrClientLabelInvalid
	}
	for _, r := range label {
		if !unicode.IsPrint(r) {
			return "", ErrClientLabelInvalid
		}
	}
	return label, nil
}
