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

// Native sign-in: a CLI or Desktop starts a request anonymously, a person
// confirms it from a signed-in Portal session, and the client's next poll
// redeems it for a session of its own. See
// docs/design/enterprise-identity-and-access.md §14.

// LoginDetailPortalConfirmation is the user.login detail a redeemed native
// sign-in records. The session's own auth_method keeps the proof behind the
// confirming sign-in, so an operator sees both.
const LoginDetailPortalConfirmation = "portal_confirmation"

// Refusals the native sign-in routes produce.
var (
	ErrNativeSignInNotConfigured = apierr.New(apierr.KindNotConfigured, "native sign-in not configured")
	ErrNativeSignInPlatform      = apierr.New(apierr.KindInvalid, "platform must be cli or desktop")
	// ErrTooManyPendingSignIns is the start cap. A flood can refuse native
	// sign-in to everyone until requests expire; it cannot create a session.
	ErrTooManyPendingSignIns = apierr.New(apierr.KindUnavailable, "too many pending sign-in requests")
	ErrPollSecretRequired    = apierr.New(apierr.KindInvalid, "poll_secret required")
	// ErrNativeSignInCodeInvalid is one answer for a wrong, expired, or
	// already-decided code, so a lookup does not say which.
	ErrNativeSignInCodeInvalid = apierr.New(apierr.KindNotFound, "sign-in code is not valid")
	// ErrNativeSignInPortalOnly refuses a decision from anything but a Portal
	// session. A stolen CLI token must not mint more native sessions, each of
	// which would outlive the revocation of the token that minted it.
	ErrNativeSignInPortalOnly = apierr.New(apierr.KindForbidden, "native sign-in is confirmed from a Portal session")
	// ErrNativeSignInReauthenticate refuses a confirmation from a session whose
	// way of signing in this deployment no longer admits, so an old session
	// cannot launder itself into a fresh native one.
	ErrNativeSignInReauthenticate = apierr.New(apierr.KindForbidden, "sign in to the Portal again to confirm this sign-in")
)

// The poll answers, named as RFC 8628 §3.5 names them. They are protocol
// outcomes rather than apierr refusals: the transport answers each with 400
// and the name as the error code.
var (
	ErrAuthorizationPending = errors.New("authorization_pending")
	ErrSlowDown             = errors.New("slow_down")
	ErrAccessDenied         = errors.New("access_denied")
	ErrExpiredToken         = errors.New("expired_token")
)

// StartNativeSignInCmd is what a client says about itself.
type StartNativeSignInCmd struct {
	Platform    string
	ClientLabel string
}

// NativeSignInStarted is what the client is told: the code a person compares,
// in XXXX-XXXX form, and the secret only the client holds.
type NativeSignInStarted struct {
	UserCode   string
	PollSecret string
	ExpiresIn  time.Duration
	Interval   time.Duration
}

// userCodeAttempts bounds redrawing a colliding code. With at most a few
// hundred live codes among about 10^12, a second draw is already rare.
const userCodeAttempts = 5

// StartNativeSignIn stores a new request. It is anonymous, so nothing here is
// trusted: the platform is validated and the label only bounded.
func (s *Service) StartNativeSignIn(ctx context.Context, cmd StartNativeSignInCmd) (*NativeSignInStarted, error) {
	if s.NativeSignIns == nil || s.Sessions == nil || s.Users == nil || s.Tokens == nil {
		return nil, ErrNativeSignInNotConfigured
	}
	if !coreidentity.ValidNativePlatform(cmd.Platform) {
		return nil, ErrNativeSignInPlatform
	}
	label, err := coreidentity.NormalizeClientLabel(cmd.ClientLabel)
	if err != nil {
		return nil, ErrClientLabelInvalid
	}
	now := s.now().UTC()
	// The count and the insert are not one atomic step, so concurrent starts
	// can overshoot the cap by their number. The cap bounds a flood's storage,
	// which a handful of extra rows does not change.
	pending, err := s.NativeSignIns.CountPendingNativeSignIns(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("count pending sign-ins: %w", err)
	}
	if pending >= coreidentity.NativeSignInPendingCap {
		slog.WarnContext(ctx, "native sign-in refused", "reason", "pending cap reached")
		return nil, ErrTooManyPendingSignIns
	}
	secret, err := coreidentity.NewNativeSignInPollSecret()
	if err != nil {
		return nil, fmt.Errorf("draw poll secret: %w", err)
	}
	for range userCodeAttempts {
		code, err := coreidentity.NewUserCode()
		if err != nil {
			return nil, fmt.Errorf("draw user code: %w", err)
		}
		err = s.NativeSignIns.CreateNativeSignIn(ctx, coreidentity.NewNativeSignIn{
			UserCode:    code,
			PollSecret:  secret,
			Platform:    cmd.Platform,
			ClientLabel: label,
			CreatedAt:   now,
			ExpiresAt:   now.Add(coreidentity.NativeSignInTTL),
		})
		if errors.Is(err, coreidentity.ErrUserCodeTaken) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("store sign-in request: %w", err)
		}
		slog.InfoContext(ctx, "native sign-in started", "platform", cmd.Platform)
		return &NativeSignInStarted{
			UserCode:   coreidentity.DisplayUserCode(code),
			PollSecret: secret,
			ExpiresIn:  coreidentity.NativeSignInTTL,
			Interval:   coreidentity.NativeSignInInterval,
		}, nil
	}
	return nil, errors.New("store sign-in request: no free user code")
}

// PollNativeSignIn answers one poll, and redeems a confirmed request for a
// session. Exactly one poll redeems a request; any later one, like an unknown
// secret or an expired request, answers ErrExpiredToken.
func (s *Service) PollNativeSignIn(ctx context.Context, pollSecret string) (*LoginResult, error) {
	if s.NativeSignIns == nil || s.Sessions == nil || s.Users == nil || s.Tokens == nil {
		return nil, ErrNativeSignInNotConfigured
	}
	if pollSecret == "" {
		return nil, ErrPollSecretRequired
	}
	now := s.now().UTC()
	req, slowDown, err := s.NativeSignIns.PollNativeSignIn(ctx, pollSecret, now)
	if errors.Is(err, coreidentity.ErrNativeSignInNotFound) {
		return nil, ErrExpiredToken
	}
	if err != nil {
		return nil, fmt.Errorf("poll sign-in request: %w", err)
	}
	if slowDown {
		return nil, ErrSlowDown
	}
	switch req.State {
	case coreidentity.NativeSignInPending:
		return nil, ErrAuthorizationPending
	case coreidentity.NativeSignInDenied:
		return nil, ErrAccessDenied
	case coreidentity.NativeSignInConfirmed:
		return s.redeemNativeSignIn(ctx, pollSecret, now)
	default:
		return nil, fmt.Errorf("sign-in request in unknown state %q", req.State)
	}
}

func (s *Service) redeemNativeSignIn(ctx context.Context, pollSecret string, now time.Time) (*LoginResult, error) {
	redeemed, err := s.NativeSignIns.RedeemNativeSignIn(ctx, pollSecret, now, s.RefreshTTL)
	switch {
	case errors.Is(err, coreidentity.ErrNativeSignInNotFound):
		// Another poll redeemed it first, or it expired between the two steps.
		return nil, ErrExpiredToken
	case errors.Is(err, coreidentity.ErrNativeSignInRefused):
		slog.InfoContext(ctx, "native sign-in refused",
			"reason", "confirming account disabled or confirming session ended")
		return nil, ErrAccessDenied
	case err != nil:
		return nil, fmt.Errorf("redeem sign-in request: %w", err)
	}
	user, err := s.Users.GetUser(ctx, redeemed.UserID)
	if err != nil {
		return nil, fmt.Errorf("read account: %w", err)
	}
	if user == nil {
		return nil, fmt.Errorf("redeemed sign-in names no account %s", redeemed.UserID)
	}
	result, err := s.issueAccess(ctx, user, redeemed.AuthMethod, redeemed.Platform, redeemed.SessionID, redeemed.RefreshToken, now)
	if err != nil {
		return nil, err
	}
	s.record(ctx, coreaudit.Event{
		ActorType:  coreaudit.ActorUser,
		ActorID:    user.ID,
		Action:     coreaudit.UserLogin,
		TargetType: "platform",
		TargetID:   redeemed.Platform,
		Detail:     LoginDetailPortalConfirmation,
	})
	slog.InfoContext(ctx, "native sign-in redeemed",
		"user_id", user.ID, "session_id", redeemed.SessionID, "platform", redeemed.Platform)
	return result, nil
}

// NativeSignInView is what the Portal shows before a person decides.
type NativeSignInView struct {
	Request coreidentity.NativeSignIn
	// SessionExpiresAt is when the resulting session would end: the confirming
	// session's own absolute expiry.
	SessionExpiresAt time.Time
}

// LookUpNativeSignIn returns the pending request a code names, for the Portal
// session sid of userID.
func (s *Service) LookUpNativeSignIn(ctx context.Context, userID, sid, code string) (*NativeSignInView, error) {
	confirming, err := s.portalSession(ctx, userID, sid)
	if err != nil {
		return nil, err
	}
	req, err := s.pendingNativeSignIn(ctx, code)
	if err != nil {
		return nil, err
	}
	return &NativeSignInView{Request: req, SessionExpiresAt: confirming.AbsoluteExpiresAt}, nil
}

// ConfirmNativeSignIn records the Portal session's confirmation. The first
// decision on a code wins; a second one finds the code not valid.
func (s *Service) ConfirmNativeSignIn(ctx context.Context, userID, sid, code string) error {
	confirming, err := s.portalSession(ctx, userID, sid)
	if err != nil {
		return err
	}
	if err := s.admitConfirmation(ctx, userID, confirming.AuthMethod); err != nil {
		return err
	}
	return s.decideNativeSignIn(ctx, userID, confirming.SID, code, coreidentity.NativeSignInConfirmed)
}

// DenyNativeSignIn records the Portal session's denial. Refusing someone else's
// request is always allowed, so it needs no admission check.
func (s *Service) DenyNativeSignIn(ctx context.Context, userID, sid, code string) error {
	confirming, err := s.portalSession(ctx, userID, sid)
	if err != nil {
		return err
	}
	return s.decideNativeSignIn(ctx, userID, confirming.SID, code, coreidentity.NativeSignInDenied)
}

func (s *Service) decideNativeSignIn(ctx context.Context, userID, sid, code string, state coreidentity.NativeSignInState) error {
	// Read first only for the audit detail; the decision itself is the
	// conditional write below, which is what makes the first decision win.
	req, err := s.pendingNativeSignIn(ctx, code)
	if err != nil {
		return err
	}
	err = s.NativeSignIns.DecideNativeSignIn(ctx, coreidentity.NormalizeUserCode(code), coreidentity.NativeSignInDecision{
		State: state, UserID: userID, SessionID: sid, At: s.now().UTC(),
	})
	if errors.Is(err, coreidentity.ErrNativeSignInNotFound) {
		return ErrNativeSignInCodeInvalid
	}
	if err != nil {
		return fmt.Errorf("decide sign-in request: %w", err)
	}
	action := coreaudit.NativeSignInConfirmed
	if state == coreidentity.NativeSignInDenied {
		action = coreaudit.NativeSignInDenied
	}
	s.record(ctx, coreaudit.Event{
		ActorType:  coreaudit.ActorUser,
		ActorID:    userID,
		Action:     action,
		TargetType: "auth_session",
		TargetID:   sid,
		Detail:     nativeSignInAuditDetail(req),
	})
	return nil
}

// nativeSignInAuditDetail names the client kind and its label, marked as the
// client's claim. The label is already bounded to 64 printable characters.
func nativeSignInAuditDetail(req coreidentity.NativeSignIn) string {
	if req.ClientLabel == "" {
		return req.Platform
	}
	return fmt.Sprintf("%s; claimed label %q", req.Platform, req.ClientLabel)
}

func (s *Service) pendingNativeSignIn(ctx context.Context, code string) (coreidentity.NativeSignIn, error) {
	normalized := coreidentity.NormalizeUserCode(code)
	if len(normalized) != coreidentity.UserCodeLength {
		return coreidentity.NativeSignIn{}, ErrNativeSignInCodeInvalid
	}
	req, err := s.NativeSignIns.PendingNativeSignIn(ctx, normalized, s.now().UTC())
	if errors.Is(err, coreidentity.ErrNativeSignInNotFound) {
		return coreidentity.NativeSignIn{}, ErrNativeSignInCodeInvalid
	}
	if err != nil {
		return coreidentity.NativeSignIn{}, fmt.Errorf("read sign-in request: %w", err)
	}
	return req, nil
}

// portalSession resolves the caller's session and refuses anything that is not
// a live Portal session of that account.
func (s *Service) portalSession(ctx context.Context, userID, sid string) (coreidentity.AuthSession, error) {
	if s.NativeSignIns == nil || s.Sessions == nil || s.Users == nil {
		return coreidentity.AuthSession{}, ErrNativeSignInNotConfigured
	}
	sess, err := s.Sessions.ActiveSession(ctx, sid, s.now())
	if errors.Is(err, coreidentity.ErrSessionInactive) {
		return coreidentity.AuthSession{}, ErrNativeSignInPortalOnly
	}
	if err != nil {
		return coreidentity.AuthSession{}, fmt.Errorf("read session: %w", err)
	}
	if sess.UserID != userID || sess.Platform != coreidentity.PlatformPortal {
		return coreidentity.AuthSession{}, ErrNativeSignInPortalOnly
	}
	return sess, nil
}

// admitConfirmation re-applies today's admission rule for the way the
// confirming session signed in: local login for a password or login-code
// session, SSO being enabled for an OIDC one. The account must still be an
// active person.
func (s *Service) admitConfirmation(ctx context.Context, userID, method string) error {
	user, err := s.Users.GetUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("read account: %w", err)
	}
	if user == nil || user.IsService() {
		return ErrNativeSignInReauthenticate
	}
	if user.Disabled() {
		return ErrDisabled
	}
	switch method {
	case MethodPassword, MethodLoginCode:
		if err := s.enforceLocalLogin(ctx, user); err != nil {
			if errors.Is(err, ErrLocalLoginNotAllowed) {
				return ErrNativeSignInReauthenticate
			}
			return err
		}
		return nil
	case MethodOIDC:
		if !s.OIDCEnabled {
			return ErrNativeSignInReauthenticate
		}
		return nil
	default:
		return ErrNativeSignInReauthenticate
	}
}
