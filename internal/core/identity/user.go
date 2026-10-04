package identity

import (
	"context"
	"time"
)

// User is the user model. JSON uses snake_case per project convention.
// Internal numeric ID is retained for compatibility but is not part of the public API.
type User struct {
	ID                string     `json:"id"`
	Email             string     `json:"email"`
	Name              string     `json:"name"`
	LastLoginAt       *time.Time `json:"last_login_at,omitempty"`
	LastLoginPlatform *string    `json:"last_login_platform,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	// HasPassword reports whether this account can sign in with a password. The
	// hash itself never travels on this struct — see PasswordStore — so that no
	// handler can serialize it into a response by accident.
	HasPassword bool `json:"has_password"`
	// DisabledAt is nil for an ordinary account. Non-nil means every credential
	// this account holds is refused: password, login code, refresh token, the
	// access token it already has, and its webhook keys. Disabling is not
	// deletion — nothing is removed, and enabling reverses the state and
	// nothing else. See docs/design/system-administration.md section 8.
	DisabledAt *time.Time `json:"disabled_at,omitempty"`
	// Kind is KindHuman or KindService. A service account is a Space-owned
	// principal work runs as: it has no email, password, login code, external
	// identity, or session, and every sign-in path refuses it. See
	// docs/design/space-assistants.md §6.
	Kind string `json:"kind"`
	// SponsorUserID names the human owner or admin accountable for a service
	// account. Nil for a person.
	SponsorUserID *string `json:"sponsor_user_id,omitempty"`
}

// Account kinds.
const (
	KindHuman   = "human"
	KindService = "service"
)

// Disabled reports whether the account is currently refused.
func (u User) Disabled() bool { return u.DisabledAt != nil }

// IsService reports whether the account is a service account rather than a
// person.
func (u User) IsService() bool { return u.Kind == KindService }

// UserStore looks up users by email and creates new users.
// UserFilter narrows a ListUsers result. A zero value matches every account;
// each set field is an AND.
type UserFilter struct {
	// Query matches the email as a substring.
	Query string
	// Disabled, when set, keeps only disabled (true) or only enabled (false)
	// accounts.
	Disabled *bool
	// HasPassword, when set, keeps only accounts that have set a password (true)
	// or have not (false) — the latter is who still needs a login code.
	HasPassword *bool
	// SystemRole, when non-empty, keeps only accounts holding that role as an
	// active grant.
	SystemRole string
	// Platform, when non-empty, keeps only accounts whose last login was on it.
	// An account that never logged in is excluded.
	Platform string
	// LastLoginAfter and LastLoginBefore, when set, bound the last-login time to
	// [after, before). An account that never logged in is excluded by either
	// bound, since it has no time to compare.
	LastLoginAfter  *time.Time
	LastLoginBefore *time.Time
}

type UserStore interface {
	// UserByEmail matches the address without regard to case, and returns
	// (nil, nil) when nobody has it. Login resolves the account this way and
	// compares nothing afterwards, so a case-sensitive implementation would
	// refuse people whose address is stored in another case.
	UserByEmail(ctx context.Context, email string) (*User, error)
	// GetUser returns the user by user_id, or (nil, nil) when not found.
	GetUser(ctx context.Context, userID string) (*User, error)
	// CreateUser creates a user with the given email. defaultQuotaTier is applied when non-empty. Returns ErrEmailExists if the email is already registered.
	CreateUser(ctx context.Context, email string, defaultQuotaTier string) (*User, error)
	// UpdateLoginMeta records the last login timestamp and platform for the user.
	UpdateLoginMeta(ctx context.Context, userID string, loginAt time.Time, platform string) error
	// ListUsers returns accounts newest first with the total count of accounts
	// the filter matched (not the page size), so a caller can page through them.
	ListUsers(ctx context.Context, filter UserFilter, limit, offset int) ([]User, int, error)
	// SetUserDisabled disables the account at the given time, or enables it
	// when disabledAt is nil. Returns ErrUserNotFound when there is no such
	// account.
	//
	// Disabling refuses with ErrSystemGrantLastHolder when the account is the
	// last effective holder of a system role: a disabled account cannot
	// authorize a request, so disabling the last one would leave the deployment
	// with nobody able to operate it. The check and the disable are one atomic
	// step so a concurrent grant revoke cannot slip between them.
	SetUserDisabled(ctx context.Context, userID string, disabledAt *time.Time) error
}

// PasswordStore reads and writes the one credential a person chose themselves.
//
// It is deliberately separate from UserStore. A password hash is the only value
// in the system whose exposure would reach beyond BuildMax — people reuse
// passwords — so it is fetched only by the code that verifies a login, and
// never rides along on a User that some handler might serialize.
type PasswordStore interface {
	// PasswordHash returns the stored hash for userID, or "" when the account
	// has no password and can only sign in with a login code.
	PasswordHash(ctx context.Context, userID string) (string, error)
	// SetPassword stores an already-hashed password. Hashing belongs to the
	// caller — this interface must not be a place where a plaintext password
	// can be passed by mistake.
	SetPassword(ctx context.Context, userID, encodedHash string, setAt time.Time) error
}
