package identity

import (
	"context"
	"errors"
	"time"
)

// ExternalIdentity links a BuildMax account to one verified identity at an
// external IdP. The identity key is the exact OIDC (issuer, subject) pair, both
// case-sensitive protocol values; email and name are attributes captured for
// display and reconciliation, never identity. A changed email can never move a
// link to another account. See docs/design/enterprise-identity-and-access.md §5.
type ExternalIdentity struct {
	// ID is the public identifier of the link, the handle an admin unlink names.
	ID     string
	UserID string
	Issuer string
	// Subject is the IdP's stable `sub` for this account.
	Subject string
	// LastSeenEmail and LastSeenName are the attributes from the most recent
	// successful sign-in, kept so Portal administration can surface drift from
	// the local account without rewriting it.
	LastSeenEmail string
	LastSeenName  string
	LastLoginAt   time.Time
	CreatedAt     time.Time
}

// SeenClaims are the display attributes captured from a verified sign-in. They
// are snapshotted, not authoritative: the account handle stays user.email.
type SeenClaims struct {
	Email string
	Name  string
}

// LinkIdentity describes linking an existing account to a subject on a first
// verified sign-in whose email matched that account (the migration path for
// operator-created accounts, §5.2 rule 3).
type LinkIdentity struct {
	UserID  string
	Issuer  string
	Subject string
	Seen    SeenClaims
}

// ProvisionUser describes a just-in-time account to create atomically with its
// identity link (§5.2 rule 5). Email is the verified address the domain
// allow-list already admitted; the caller owns that check.
type ProvisionUser struct {
	Email     string
	Name      string
	QuotaTier string
	Issuer    string
	Subject   string
}

// UnlinkIdentity describes an administrator removing one link. The actor is the
// admin, recorded atomically with the deletion. Unlinking is permitted only
// while the account is disabled; the store enforces that inside the transaction
// so the check cannot race the delete.
type UnlinkIdentity struct {
	UserID     string
	IdentityID string
	// ActorID is the administrator performing the unlink, for the atomic audit
	// row. Empty records the action against the system actor.
	ActorID string
}

// Refusals the external-identity store and association can produce.
var (
	// ErrIdentityConflict means the (issuer, subject) or (issuer, user_id) pair
	// is already taken — a concurrent first login, or an account already linked
	// to this issuer under a different subject. The association service turns it
	// into an operator-investigation refusal rather than replacing either link.
	ErrIdentityConflict = errors.New("external identity already linked")
	// ErrUnlinkRequiresDisabled means an unlink was attempted while the account
	// was still enabled. Unlinking is an operator recovery step; enabling the
	// account again is the deliberate re-authorization.
	ErrUnlinkRequiresDisabled = errors.New("external identity can be unlinked only while the account is disabled")
	// ErrIdentityNotFound means the named link does not exist for that user.
	ErrIdentityNotFound = errors.New("external identity not found")
)

// ExternalIdentityStore owns the link table and the atomic transactions that
// create, link, and remove a link along with the account it creates and the
// audit row that records it.
type ExternalIdentityStore interface {
	// IdentityBySubject returns the link for one (issuer, subject), or nil when
	// none exists. This is §5.2 rule 1.
	IdentityBySubject(ctx context.Context, issuer, subject string) (*ExternalIdentity, error)

	// IdentityByUserAndIssuer returns the account's link for one issuer, or nil.
	// It is what tells rule 3 (link an unlinked account) from rule 4 (that
	// account already has a different subject at this issuer — refuse).
	IdentityByUserAndIssuer(ctx context.Context, userID, issuer string) (*ExternalIdentity, error)

	// ListUserIdentities returns every link an account has, newest first.
	ListUserIdentities(ctx context.Context, userID string) ([]ExternalIdentity, error)

	// LinkExisting links an existing account to a subject and records the link in
	// one transaction. It returns ErrIdentityConflict if either uniqueness
	// constraint rejects the insert, so a concurrent first login cannot produce a
	// second link.
	LinkExisting(ctx context.Context, in LinkIdentity) (*ExternalIdentity, error)

	// CreateUserWithIdentity creates the account, its personal Space, the owner
	// membership, and the identity link atomically, recording the creation and
	// the link in the same transaction. It returns ErrIdentityConflict or
	// ErrEmailExists when a concurrent first login won the race.
	CreateUserWithIdentity(ctx context.Context, in ProvisionUser) (*User, *ExternalIdentity, error)

	// UnlinkIdentity deletes one link and records the deletion in one
	// transaction, only while the account is disabled. It returns
	// ErrUnlinkRequiresDisabled when the account is enabled and
	// ErrIdentityNotFound when the link is not the user's.
	UnlinkIdentity(ctx context.Context, in UnlinkIdentity) error

	// UpdateLastSeen refreshes the display attributes and last-login time after a
	// successful sign-in. It is best-effort: a missing link is not an error.
	UpdateLastSeen(ctx context.Context, issuer, subject string, seen SeenClaims, now time.Time) error
}
