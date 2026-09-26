// Package secret owns the Space Secret domain: a Space-owned group of named
// items, its lifecycle, and the store contract. It holds no cryptography and
// no persistence -- a value crosses this package only as opaque sealed bytes
// (Sealed) or, on the materialization path, as a decrypted item map the
// caller obtained from internal/infra/secret. See
// docs/design/space-secrets.md.
package secret

import (
	"context"
	"fmt"
	"regexp"
	"time"
)

// itemNamePattern is the identifier an item name must be, so a whole group can
// be injected as environment variables without an item that cannot become a
// variable. See docs/design/space-secrets.md §5.1.
var itemNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// IsItemName reports whether name is a valid Secret item name.
func IsItemName(name string) bool { return itemNamePattern.MatchString(name) }

// State is a Secret's lifecycle. Disabling refuses new run grants and
// materializations; destruction erases recoverable material once nothing
// references it and is terminal.
type State string

const (
	StateActive    State = "active"
	StateDisabled  State = "disabled"
	StateDestroyed State = "destroyed"
)

// Provider names where a Secret's items live. Only the embedded encrypted
// store exists today; external references are a later phase.
type Provider string

const (
	ProviderEmbedded Provider = "embedded"
)

// Secret is the metadata a caller may read. It never carries an item value:
// ItemNames lists the keys present so a listing and consumption validation
// work without decrypting anything.
type Secret struct {
	ID          string
	SpaceID     string
	Name        string
	Description string
	Provider    Provider
	State       State
	ItemNames   []string
	CreatedBy   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Items is a Secret's decrypted key/value set. It leaves the crypto boundary
// only toward a materialization path, never toward a list or get API. An item
// name is an identifier so a whole group can become environment variables.
type Items map[string]string

// Sealed is the stored form of a Secret's items: ciphertext plus the envelope
// metadata needed to open it, and no plaintext. internal/infra/db persists it
// and the secret service seals and opens it; neither this package nor db
// performs cryptography.
type Sealed struct {
	Ciphertext []byte
	Nonce      []byte
	WrappedDEK []byte
	KeyID      string
}

// Sealer is the cryptography the secret service depends on: seal an item map,
// open a sealed blob. It is an interface so the embedded, Vault, and cloud
// implementations do not leak into the service, handlers, or runtime. The
// implementation lives in internal/infra/secret.
type Sealer interface {
	Seal(items Items, aad []byte) (Sealed, error)
	Open(s Sealed, aad []byte) (Items, error)
}

// AAD builds the associated data that binds a sealed blob to its Space. A
// ciphertext moved to another Space's row then fails to open, which is the
// cross-Space isolation the threat model defends.
//
// It binds nothing else. Per-deployment isolation is already cryptographic --
// another deployment has a different KEK, so unwrapping the DEK fails before
// GCM is even reached -- and binding a deployment id here would instead break
// a disaster-recovery replica that deliberately shares the KEK to read the same
// rows. It binds no Secret public id either: that id is minted when the row is
// inserted, after the value is sealed, and an intra-Space ciphertext swap needs
// database write access, which is the deployment operator the model trusts.
// Kept here so seal and open cannot disagree on it.
func AAD(spacePublicID string) []byte {
	return fmt.Appendf(nil, "bmax-secret\x00%s", spacePublicID)
}

// GrantRecord is the non-secret audit of one materialized env grant: which
// item of which Secret a run received, under which variable name, authorized by
// which Agent revision. It carries no value. See docs/design/space-secrets.md
// §5.2 and §11.
type GrantRecord struct {
	TaskRunID     string
	SecretID      string
	ItemName      string
	AgentID       string
	AgentRevision int
	EnvName       string
}

// CreateInput carries one new Secret. ItemNames is the plaintext key set the
// caller sealed, stored in the clear; Sealed is the encrypted map.
type CreateInput struct {
	SpaceID     string
	Name        string
	Description string
	Provider    Provider
	CreatedBy   string
	ItemNames   []string
	Sealed      Sealed
}

// UpdateItemsInput replaces a Secret's items and item names. The row is
// rewritten whole, which is what makes rotation atomic.
type UpdateItemsInput struct {
	ID        string
	ItemNames []string
	Sealed    Sealed
}

// Store persists Secret metadata and sealed items. GORM stays below it; above
// it, an absent Secret is apierr.ErrNotFound.
//
// A metadata read never carries ciphertext: GetSealed is separate from
// GetSecret so a listing or detail view cannot accidentally ship the sealed
// bytes.
type Store interface {
	CreateSecret(ctx context.Context, in CreateInput) (*Secret, error)
	GetSecret(ctx context.Context, id string) (*Secret, error)
	ListSecretsBySpace(ctx context.Context, spaceID string) ([]Secret, error)
	// GetSealed returns metadata and the sealed items for materialization. It
	// refuses a destroyed Secret.
	GetSealed(ctx context.Context, id string) (*Secret, *Sealed, error)
	UpdateItems(ctx context.Context, in UpdateItemsInput) (*Secret, error)
	SetState(ctx context.Context, id string, state State) (*Secret, error)
	// RecordEnvGrant records the non-secret audit of one materialized grant,
	// idempotent on (run, secret, item). It carries no value.
	RecordEnvGrant(ctx context.Context, in GrantRecord) error
}
