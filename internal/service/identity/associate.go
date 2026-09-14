package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
)

// Provisioning modes. They mirror the config values; the service takes the
// string so it stays free of the config package.
const (
	ProvisioningJIT          = "jit"
	ProvisioningExistingOnly = "existing_only"
)

// Refusals association can produce, on top of ErrDisabled (reused from login):
var (
	// ErrSSONotConfigured means association was attempted with no identity store.
	ErrSSONotConfigured = apierr.New(apierr.KindNotConfigured, "sso not configured")
	// ErrNotAuthorizedForDeployment is the single generic refusal for a subject
	// this deployment will not admit: existing_only with no account, or jit with a
	// disallowed email domain. One message on purpose — it must not reveal whether
	// the email is already an account.
	ErrNotAuthorizedForDeployment = apierr.New(apierr.KindForbidden, "not authorized for this deployment")
	// ErrIdentityNeedsOperator means the verified email belongs to an account
	// already linked to a different subject at this issuer. It is never resolved
	// automatically: replacing either link silently would be an account takeover
	// primitive. An operator investigates. See §5.2 rule 4.
	ErrIdentityNeedsOperator = apierr.New(apierr.KindConflict, "external identity requires operator reconciliation")
)

// AssociationInput is one verified sign-in, after the callback has checked the
// ID Token (signature, issuer, audience, azp, expiry, nonce, transaction). For a
// first association the caller has already required a syntactically valid email
// with email_verified=true; VerifiedEmail carries it.
type AssociationInput struct {
	Issuer        string
	Subject       string
	VerifiedEmail string
	Name          string
}

// AssociationResult is the BuildMax account a verified sign-in resolves to.
type AssociationResult struct {
	User coreidentity.User
	// Created is true when this call provisioned the account just in time.
	Created bool
}

// Associate applies the §5.2 algorithm and returns the account a verified
// sign-in belongs to, creating one under jit when the rules allow. It does not
// open a session: the callback does that only after this commits, so a failed
// association never leaves a usable login.
func (s *Service) Associate(ctx context.Context, in AssociationInput) (*AssociationResult, error) {
	if s.Users == nil || s.ExternalIdentities == nil {
		return nil, ErrSSONotConfigured
	}
	if in.Issuer == "" || in.Subject == "" {
		return nil, fmt.Errorf("associate: %w", ErrCredentialRequired)
	}
	seen := coreidentity.SeenClaims{Email: in.VerifiedEmail, Name: in.Name}

	// Rule 1: an existing link decides the account. A changed email cannot move
	// the identity, so this is checked before any email matching.
	link, err := s.ExternalIdentities.IdentityBySubject(ctx, in.Issuer, in.Subject)
	if err != nil {
		return nil, fmt.Errorf("resolve identity: %w", err)
	}
	if link != nil {
		user, err := s.Users.GetUser(ctx, link.UserID)
		if err != nil {
			return nil, fmt.Errorf("read account: %w", err)
		}
		// A link whose account is gone is a broken state, not a login. Refuse
		// generically rather than provisioning a replacement under the same
		// subject.
		if user == nil {
			return nil, ErrNotAuthorizedForDeployment
		}
		// Rule 2: a disabled account is refused after the identity is known, so the
		// person whose account it is hears why. Re-enabling is an operator decision.
		if user.Disabled() {
			return nil, ErrDisabled
		}
		_ = s.ExternalIdentities.UpdateLastSeen(ctx, in.Issuer, in.Subject, seen, s.now())
		return &AssociationResult{User: *user}, nil
	}

	// No link yet: a first association needs a verified email.
	if in.VerifiedEmail == "" {
		return nil, fmt.Errorf("associate: %w", ErrEmailRequired)
	}

	existing, err := s.Users.UserByEmail(ctx, in.VerifiedEmail)
	if err != nil {
		return nil, fmt.Errorf("read account by email: %w", err)
	}
	if existing != nil {
		// Rule 2 before any linking.
		if existing.Disabled() {
			return nil, ErrDisabled
		}
		// Rules 3 and 4: link only when this account has no identity for this
		// issuer. If it already has one, its subject differs (rule 1 found none for
		// ours), so this is the takeover case an operator must resolve.
		byIssuer, err := s.ExternalIdentities.IdentityByUserAndIssuer(ctx, existing.ID, in.Issuer)
		if err != nil {
			return nil, fmt.Errorf("resolve account identity: %w", err)
		}
		if byIssuer != nil {
			return nil, ErrIdentityNeedsOperator
		}
		if _, err := s.ExternalIdentities.LinkExisting(ctx, coreidentity.LinkIdentity{
			UserID: existing.ID, Issuer: in.Issuer, Subject: in.Subject, Seen: seen,
		}); err != nil {
			// A concurrent first login linked one of the two pairs first.
			if errors.Is(err, coreidentity.ErrIdentityConflict) {
				return s.resolveRace(ctx, in)
			}
			return nil, fmt.Errorf("link identity: %w", err)
		}
		return &AssociationResult{User: *existing}, nil
	}

	// Rule 5: no account exists.
	switch s.provisioning() {
	case ProvisioningExistingOnly:
		return nil, ErrNotAuthorizedForDeployment
	case ProvisioningJIT:
		if !s.emailDomainAllowed(in.VerifiedEmail) {
			return nil, ErrNotAuthorizedForDeployment
		}
		user, _, err := s.ExternalIdentities.CreateUserWithIdentity(ctx, coreidentity.ProvisionUser{
			Email: in.VerifiedEmail, Name: in.Name, QuotaTier: s.DefaultQuotaTier,
			Issuer: in.Issuer, Subject: in.Subject,
		})
		if err != nil {
			// A concurrent first login created the account or the link first.
			if errors.Is(err, coreidentity.ErrEmailExists) || errors.Is(err, coreidentity.ErrIdentityConflict) {
				return s.resolveRace(ctx, in)
			}
			return nil, fmt.Errorf("provision account: %w", err)
		}
		return &AssociationResult{User: *user, Created: true}, nil
	default:
		return nil, fmt.Errorf("associate: unknown provisioning mode %q", s.Provisioning)
	}
}

// resolveRace re-resolves after a uniqueness conflict, so two simultaneous first
// logins for one subject settle on the one account that won rather than one of
// them failing. If the winning link is not ours, it is the operator case.
func (s *Service) resolveRace(ctx context.Context, in AssociationInput) (*AssociationResult, error) {
	link, err := s.ExternalIdentities.IdentityBySubject(ctx, in.Issuer, in.Subject)
	if err != nil {
		return nil, fmt.Errorf("resolve identity after race: %w", err)
	}
	if link == nil {
		// The conflict was on (issuer, user): the email's account gained a
		// different subject concurrently. That is the operator case.
		return nil, ErrIdentityNeedsOperator
	}
	user, err := s.Users.GetUser(ctx, link.UserID)
	if err != nil {
		return nil, fmt.Errorf("read account after race: %w", err)
	}
	if user == nil {
		return nil, ErrNotAuthorizedForDeployment
	}
	if user.Disabled() {
		return nil, ErrDisabled
	}
	return &AssociationResult{User: *user}, nil
}

func (s *Service) provisioning() string {
	if s.Provisioning == "" {
		return ProvisioningJIT
	}
	return s.Provisioning
}

// emailDomainAllowed reports whether the email's domain is in the allow-list,
// compared canonically (lowercased, exact). An empty list admits nothing: an
// unbounded JIT would provision for anyone the IdP authenticates.
func (s *Service) emailDomainAllowed(email string) bool {
	domain := emailDomain(email)
	if domain == "" {
		return false
	}
	for _, allowed := range s.AllowedEmailDomains {
		if domain == canonicalDomain(allowed) {
			return true
		}
	}
	return false
}

// emailDomain returns the canonical domain of an email, or "" if it has no
// single unambiguous one.
func emailDomain(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	local := email[:at]
	if local == "" || strings.Contains(email[at+1:], "@") {
		return ""
	}
	return canonicalDomain(email[at+1:])
}

func canonicalDomain(d string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(d), "."))
}
