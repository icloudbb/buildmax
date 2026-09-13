package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
)

// Refusals the account workflows produce.
var (
	// ErrCurrentPasswordIncorrect refuses a change by somebody holding the
	// session but not the password it replaces.
	ErrCurrentPasswordIncorrect = apierr.New(apierr.KindForbidden, "current password is incorrect")
	// ErrOtpNotConfigured means this deployment has no account store.
	ErrOtpNotConfigured = apierr.New(apierr.KindNotConfigured, "otp not configured")
	// ErrIntentUnknown means the request asked for neither signup nor login.
	ErrIntentUnknown = apierr.New(apierr.KindInvalid, "intent must be signup or login")
	// ErrSignupClosed is the default. Nothing here verifies that whoever typed
	// an address controls it, so open registration on a reachable server is how
	// somebody claims a colleague's address.
	ErrSignupClosed = apierr.New(apierr.KindForbidden,
		"signup is disabled on this server; ask an administrator for an account")
	// ErrEmailRegistered refuses a signup for an address already in use.
	ErrEmailRegistered = apierr.New(apierr.KindConflict, "email already registered")
	// ErrAccountNotFound refuses a login-intent request for an unknown address.
	ErrAccountNotFound = apierr.New(apierr.KindNotFound, "user not found")
)

// PasswordRejected wraps what the password rules refused, so a transport can
// print the rule's own words -- they name the limit the person has to meet.
type PasswordRejected struct{ err error }

func (e *PasswordRejected) Error() string { return e.err.Error() }
func (e *PasswordRejected) Unwrap() error { return e.err }

// SetPasswordCmd changes or first sets an account's password.
type SetPasswordCmd struct {
	UserID  string
	Current string
	New     string
}

// SetPassword writes a new password for an account that proved it holds the
// current one, or that has never had one.
//
// The rules are checked before the current password, so somebody typing a new
// password that could never be accepted hears which rule they missed rather
// than being told their current one is wrong.
func (s *Service) SetPassword(ctx context.Context, cmd SetPasswordCmd) error {
	if s.Passwords == nil || s.Users == nil {
		return ErrPasswordLoginNotConfigured
	}
	if err := coreidentity.ValidatePassword(cmd.New); err != nil {
		return &PasswordRejected{err: err}
	}
	existing, err := s.Passwords.PasswordHash(ctx, cmd.UserID)
	if err != nil {
		return fmt.Errorf("read password hash: %w", err)
	}
	// An account that has never had one needs only the session. Requiring a
	// current password there would lock out everyone an operator let in with a
	// login code.
	if existing != "" && !coreidentity.VerifyPassword(existing, cmd.Current) {
		return ErrCurrentPasswordIncorrect
	}
	hash, err := coreidentity.HashPassword(cmd.New)
	if err != nil {
		return &PasswordRejected{err: err}
	}
	if err := s.Passwords.SetPassword(ctx, cmd.UserID, hash, s.now().UTC()); err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	s.record(ctx, coreaudit.Event{
		ActorType: coreaudit.ActorUser, ActorID: cmd.UserID,
		Action: coreaudit.PasswordSet, TargetType: "user", TargetID: cmd.UserID,
	})
	return nil
}

// LogoutResult reports what a logout ended, so a transport knows whether there
// was a session to record.
type LogoutResult struct {
	UserID    string
	SessionID string
}

// LogoutByRefreshToken ends the session a refresh token belongs to.
//
// An unknown token revokes nothing and is not an error: a client should be able
// to log out of a session the server has already forgotten.
func (s *Service) LogoutByRefreshToken(ctx context.Context, token string) (*LogoutResult, error) {
	if s.RefreshTokens == nil {
		return nil, ErrRefreshNotConfigured
	}
	now := s.now().UTC()
	userID, sessionID, err := s.RefreshTokens.RevokeRefreshTokenSession(ctx, token, now)
	if err != nil {
		return nil, fmt.Errorf("revoke by token: %w", err)
	}
	// Revoke the session record too, so an access token still within its window
	// stops on its next request rather than after logout only killed the refresh
	// chain.
	if s.Sessions != nil && sessionID != "" {
		if _, err := s.Sessions.RevokeSession(ctx, sessionID, now); err != nil {
			return nil, fmt.Errorf("revoke session: %w", err)
		}
	}
	s.recordLogout(ctx, userID, sessionID)
	return &LogoutResult{UserID: userID, SessionID: sessionID}, nil
}

// LogoutSession ends a session named by a live access token's claims.
func (s *Service) LogoutSession(ctx context.Context, userID, sessionID string) (*LogoutResult, error) {
	if s.Sessions == nil {
		return nil, ErrRefreshNotConfigured
	}
	if _, err := s.Sessions.RevokeSession(ctx, sessionID, s.now().UTC()); err != nil {
		return nil, fmt.Errorf("revoke session: %w", err)
	}
	s.recordLogout(ctx, userID, sessionID)
	return &LogoutResult{UserID: userID, SessionID: sessionID}, nil
}

func (s *Service) recordLogout(ctx context.Context, userID, sessionID string) {
	if sessionID == "" {
		return
	}
	s.record(ctx, coreaudit.Event{
		ActorType: coreaudit.ActorUser, ActorID: userID,
		Action: coreaudit.UserLogout, TargetType: "auth_session", TargetID: sessionID,
	})
}

// Account request intents.
const (
	IntentSignup = "signup"
	IntentLogin  = "login"
)

// AccountRequestOutcome is what an otp request did.
type AccountRequestOutcome string

const (
	// OutcomeAccountExists means the address is already registered. Nothing is
	// sent, because there is nothing to send it with: getting in means a
	// password, or a code an operator issues by hand.
	OutcomeAccountExists AccountRequestOutcome = "account_exists"
	// OutcomeAccountCreated means an account now exists and still cannot be
	// used -- it has no password and no code has been issued for it.
	OutcomeAccountCreated AccountRequestOutcome = "account_created"
)

// RequestAccount answers a sign-up or sign-in request for an address.
func (s *Service) RequestAccount(ctx context.Context, email, intent string, allowSignup bool, defaultQuotaTier string) (AccountRequestOutcome, error) {
	if s.Users == nil {
		return "", ErrOtpNotConfigured
	}
	if strings.TrimSpace(email) == "" {
		return "", ErrEmailRequired
	}
	if intent == "" {
		intent = IntentSignup
	}
	if intent != IntentSignup && intent != IntentLogin {
		return "", ErrIntentUnknown
	}
	user, err := s.Users.UserByEmail(ctx, email)
	if err != nil {
		return "", fmt.Errorf("read account: %w", err)
	}
	if intent == IntentLogin {
		if user == nil {
			return "", ErrAccountNotFound
		}
		return OutcomeAccountExists, nil
	}
	if !allowSignup {
		return "", ErrSignupClosed
	}
	if user != nil {
		return "", ErrEmailRegistered
	}
	if _, err := s.Users.CreateUser(ctx, email, defaultQuotaTier); err != nil {
		if errors.Is(err, coreidentity.ErrEmailExists) {
			return "", ErrEmailRegistered
		}
		return "", fmt.Errorf("create account: %w", err)
	}
	return OutcomeAccountCreated, nil
}

func (s *Service) record(ctx context.Context, event coreaudit.Event) {
	if s.Audit == nil {
		return
	}
	s.Audit.Record(ctx, event)
}
