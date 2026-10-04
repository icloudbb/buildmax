package identity

import "context"

// NewServiceAccount creates a service account as a member of one team Space.
type NewServiceAccount struct {
	SpaceID       string
	Name          string
	SponsorUserID string
}

// ServiceAccountUpdate changes a service account's name, sponsor, or both. A nil
// field is left unchanged.
type ServiceAccountUpdate struct {
	UserID        string
	Name          *string
	SponsorUserID *string
}

// ServiceAccountStore persists Space-owned service accounts. Who may create or
// change one, and which Space and sponsor are valid, is decided above it, in
// internal/service/space.
type ServiceAccountStore interface {
	// CreateServiceAccount writes the account and its member-role membership
	// in the Space in one transaction: a service account outside its Space is
	// not a state any caller can be handed. No personal Space is created.
	CreateServiceAccount(ctx context.Context, in NewServiceAccount) (*User, error)
	// ListServiceAccountsBySpace returns the Space's service accounts, oldest
	// first.
	ListServiceAccountsBySpace(ctx context.Context, spaceID string) ([]User, error)
	// UpdateServiceAccount applies the update, or returns ErrUserNotFound when
	// no service account has that id. A person is never changed through it.
	UpdateServiceAccount(ctx context.Context, in ServiceAccountUpdate) error
}
