// Package identity owns what proves who a caller is.
//
// It knows nothing about HTTP. Every refusal below is a value the transport
// maps, and the mapping matters more here than anywhere else in this codebase:
// the three ways a password login fails answer one status and one message on
// purpose, so that neither the response nor the time it took says whether an
// address is registered.
//
// The token issuer, the clock, and the session-id source are injected. Minting
// an access token needs the signing secret, which lives with the server, and a
// service that reached for it would be a service that depends on a transport.
package identity

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	"github.com/icloudbb/buildmax/internal/service/audit"
	"github.com/icloudbb/buildmax/internal/util"
)

// Refusals a login can produce. The two 401s carry no Kind: apierr has no
// unauthenticated Kind, and adding one would change what every other service's
// errors map to. The transport answers these two itself.
var (
	// ErrNotConfigured means this deployment cannot log anyone in.
	ErrNotConfigured = apierr.New(apierr.KindNotConfigured, "login not configured")
	// ErrNoVerifier means it has no way to check a credential.
	ErrNoVerifier = apierr.New(apierr.KindNotConfigured, "login not configured: no way to verify a credential")
	// ErrPasswordLoginNotConfigured means passwords are not stored here.
	ErrPasswordLoginNotConfigured = apierr.New(apierr.KindNotConfigured, "password login is not configured")
	// ErrLoginCodesNotConfigured means single-use codes are not stored here.
	ErrLoginCodesNotConfigured = apierr.New(apierr.KindNotConfigured, "login codes are not configured")
	// ErrEmailRequired and ErrCredentialRequired are malformed requests.
	ErrEmailRequired      = apierr.New(apierr.KindInvalid, "email required")
	ErrCredentialRequired = apierr.New(apierr.KindInvalid, "password or otp required")
	// ErrDisabled means the credential was right and the account is switched
	// off. Checked after the credential verifies, never before: refusing
	// earlier tells an unauthenticated caller the address is registered.
	ErrDisabled = apierr.New(apierr.KindForbidden, "account disabled")
)

// InvalidCredential is what every failed password login answers, whatever
// actually went wrong.
//
// One value for "no such address", "no password set", and "wrong password" is
// the point: three refusals that a caller can tell apart are three answers to
// "is this address registered". The message is the transport's to choose; this
// only guarantees the three cases arrive here as one thing.
type InvalidCredential struct {
	Method string
	// Reason is for the operator's log and never for the response. Debugging a
	// 401 from this side of the server used to mean guessing between an unknown
	// address, a spent code, and a code pasted into the wrong browser, because
	// all three were one line.
	Reason string
	// UserID is set when the attempt reached a known account, so a log line can
	// name it. Empty when naming it would say the address exists.
	UserID string
}

func (e *InvalidCredential) Error() string { return "invalid " + e.Method }

// Login methods, recorded so the trail can say which credential let someone in.
const (
	MethodPassword  = "password"
	MethodLoginCode = "login_code"
)

// TokenIssuer mints the access half of a session.
type TokenIssuer interface {
	// Mint returns a signed access token for the session and how long it lasts.
	Mint(userID, sessionID string, now time.Time) (token string, ttl time.Duration, err error)
}

// Service authenticates.
type Service struct {
	Users         coreidentity.UserStore
	Passwords     coreidentity.PasswordStore
	LoginCodes    coreidentity.LoginCodeStore
	RefreshTokens coreidentity.RefreshTokenStore
	// Sessions is the durable session authority. A login opens a session here;
	// the guard checks it on every request so logout and revocation stop an
	// already-issued access token, and the absolute expiry caps how long a login
	// may live regardless of refresh activity.
	Sessions coreidentity.AuthSessionStore

	Tokens     TokenIssuer
	RefreshTTL time.Duration
	// SessionAbsoluteTTL caps a session's life from its creation. Zero means the
	// core package default.
	SessionAbsoluteTTL time.Duration
	// RotationGrace is how long a just-rotated refresh token may be exchanged
	// again before that counts as reuse. It exists because the CLI and Desktop
	// share one credentials file between processes.
	RotationGrace time.Duration
	// Audit records what a deployment should be able to look back at. Nil
	// records nothing.
	Audit *audit.Recorder

	// Now is the clock. Nil means time.Now.
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// LoginCmd is one attempt.
type LoginCmd struct {
	Email    string
	Password string
	Otp      string
	Platform string
}

// LoginResult is a session that now exists.
type LoginResult struct {
	User         coreidentity.User
	Method       string
	Platform     string
	SessionID    string
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64
	// LoginMetaErr is a failure to record when and where the account last
	// signed in. The session is real either way, so this is reported rather
	// than returned: refusing a good login because a bookkeeping write failed
	// would be worse than the missing row.
	LoginMetaErr error
}

// Login verifies a credential and opens a session.
func (s *Service) Login(ctx context.Context, cmd LoginCmd) (*LoginResult, error) {
	if s.Users == nil || s.Tokens == nil {
		return nil, ErrNotConfigured
	}
	if s.LoginCodes == nil && s.Passwords == nil {
		return nil, ErrNoVerifier
	}
	// Both of these arrive by paste, and both used to fail on whitespace the
	// person could not see: a login code copied out of a terminal carries the
	// indentation of the line it was printed on, and an autofilled address can
	// carry a trailing space. The password is left alone -- whitespace in one
	// may be deliberate.
	email := strings.TrimSpace(cmd.Email)
	otp := strings.TrimSpace(cmd.Otp)
	if email == "" {
		return nil, ErrEmailRequired
	}
	if cmd.Password == "" && otp == "" {
		return nil, ErrCredentialRequired
	}

	var (
		user   *coreidentity.User
		method string
		err    error
	)
	if cmd.Password != "" {
		method = MethodPassword
		user, err = s.verifyPassword(ctx, email, cmd.Password)
	} else {
		method = MethodLoginCode
		user, err = s.verifyLoginCode(ctx, email, otp)
	}
	if err != nil {
		return nil, err
	}
	// Checked after the credential verifies, never before. Refusing a disabled
	// account earlier would answer an unauthenticated caller "that address is
	// registered but switched off", which is more than a wrong password is
	// told. Someone who just proved the account is theirs, on the other hand,
	// should hear the real reason rather than "wrong password".
	if user.Disabled() {
		return nil, ErrDisabled
	}

	platform := cmd.Platform
	if platform == "" {
		platform = "unknown"
	}
	now := s.now()
	// Every login opens its own session. Signing in from a second machine
	// therefore does not disturb the first, and revoking one leaves the other
	// alone -- which is the whole point of tracking sessions rather than users.
	// With a session store the session is a durable record the guard checks and
	// an absolute expiry caps; without one (a deployment with no database) the id
	// is minted inline so the access token still carries a sid, and the guard,
	// which has no session store either, simply does not check it.
	var sessionID string
	if s.Sessions != nil {
		absoluteTTL := s.SessionAbsoluteTTL
		if absoluteTTL <= 0 {
			absoluteTTL = coreidentity.SessionAbsoluteTTLDefault
		}
		sessionID, err = s.Sessions.CreateSession(ctx, coreidentity.NewAuthSession{
			UserID:            user.ID,
			Platform:          platform,
			AuthMethod:        method,
			AbsoluteExpiresAt: now.Add(absoluteTTL),
		})
		if err != nil {
			return nil, fmt.Errorf("create session: %w", err)
		}
	} else {
		sessionID, err = util.NewPublicID()
		if err != nil {
			return nil, fmt.Errorf("mint session id: %w", err)
		}
	}
	accessToken, ttl, err := s.Tokens.Mint(user.ID, sessionID, now)
	if err != nil {
		return nil, fmt.Errorf("mint access token: %w", err)
	}
	var refreshToken string
	if s.RefreshTokens != nil {
		refreshToken, _, err = s.RefreshTokens.CreateRefreshToken(ctx, coreidentity.NewRefreshToken{
			UserID:    user.ID,
			SessionID: sessionID,
			Platform:  platform,
			TTL:       s.RefreshTTL,
		})
		if err != nil {
			return nil, fmt.Errorf("create refresh token: %w", err)
		}
	}
	result := &LoginResult{
		User: *user, Method: method, Platform: platform, SessionID: sessionID,
		AccessToken: accessToken, RefreshToken: refreshToken,
		ExpiresIn: int64(ttl.Seconds()),
	}
	result.LoginMetaErr = s.Users.UpdateLoginMeta(ctx, user.ID, now, platform)
	return result, nil
}

func (s *Service) verifyPassword(ctx context.Context, email, password string) (*coreidentity.User, error) {
	if s.Passwords == nil {
		return nil, ErrPasswordLoginNotConfigured
	}
	user, err := s.Users.UserByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("read account: %w", err)
	}
	var hash string
	if user != nil {
		hash, err = s.Passwords.PasswordHash(ctx, user.ID)
		if err != nil {
			return nil, fmt.Errorf("read password hash: %w", err)
		}
	}
	if hash == "" {
		// No account, or one that has never set a password. Hash anyway: the
		// work is what makes the two cases take the same time, and time is the
		// other channel that would answer "is this address registered".
		coreidentity.DummyVerifyPassword(password)
		return nil, &InvalidCredential{Method: MethodPassword, Reason: "no account, or no password set"}
	}
	if !coreidentity.VerifyPassword(hash, password) {
		return nil, &InvalidCredential{Method: MethodPassword, Reason: "wrong password", UserID: user.ID}
	}
	return user, nil
}

func (s *Service) verifyLoginCode(ctx context.Context, email, otp string) (*coreidentity.User, error) {
	if s.LoginCodes == nil {
		return nil, ErrLoginCodesNotConfigured
	}
	user, err := s.Users.UserByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("read account: %w", err)
	}
	if user == nil {
		return nil, &InvalidCredential{Method: MethodLoginCode, Reason: "no account for that email"}
	}
	// The account is resolved from the submitted address first, and the code is
	// redeemed only if it was issued to that account. The order matters:
	// redeeming first and comparing the address afterwards spent the code on a
	// typo, so the operator had to issue another one and the person retrying
	// with the right address was refused for a reason neither could see.
	redeemed, err := s.LoginCodes.ConsumeLoginCode(ctx, otp, user.ID, s.now().UTC())
	if err != nil {
		return nil, fmt.Errorf("consume login code: %w", err)
	}
	if !redeemed {
		return nil, &InvalidCredential{
			Method: MethodLoginCode,
			Reason: "unknown, spent, expired, or issued to another account",
			UserID: user.ID,
		}
	}
	return user, nil
}
