// Package secret is the Space Secret lifecycle service: it validates a Secret's
// items, seals them through a Sealer, and stores metadata plus sealed bytes. It
// never returns an item value -- there is no reveal path -- and it owns the
// rule that an item name is an identifier. Authorization (owner-only) is the
// handler's job; space ownership is checked here. See docs/design/space-secrets.md.
package secret

import (
	"context"
	"errors"
	"maps"
	"sort"
	"strings"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coresecret "github.com/icloudbb/buildmax/internal/core/secret"
	"github.com/icloudbb/buildmax/internal/util"
)

var (
	ErrNameRequired       = apierr.New(apierr.KindInvalid, "secret name required")
	ErrNameTooLong        = apierr.New(apierr.KindInvalid, "secret name is too long")
	ErrDescriptionTooLong = apierr.New(apierr.KindInvalid, "secret description is too long")
	ErrNameTaken          = apierr.New(apierr.KindConflict, "a secret with this name already exists in the space")
	ErrNoItems            = apierr.New(apierr.KindInvalid, "a secret needs at least one item")
	ErrInvalidItem        = apierr.New(apierr.KindInvalid, "an item name must be an identifier")
	ErrUnknownState       = apierr.New(apierr.KindInvalid, "unknown secret state")
	ErrStateTransition    = apierr.New(apierr.KindConflict, "a destroyed secret is permanent and cannot be reactivated")
	ErrNotFound           = apierr.New(apierr.KindNotFound, "secret not found")
	ErrDestroyed          = apierr.New(apierr.KindConflict, "secret is destroyed")
	ErrItemNotFound       = apierr.New(apierr.KindInvalid, "no such item to remove")
	ErrDisabled           = apierr.New(apierr.KindConflict, "secret is disabled")
)

// maxSecretNameRunes and maxSecretDescriptionRunes bound the name and
// description to their varchar(128)/varchar(1024) columns.
const (
	maxSecretNameRunes        = 128
	maxSecretDescriptionRunes = 1024
)

// Service owns Secret lifecycle. Store persists metadata and sealed bytes and
// Sealer does the cryptography. The associated data binds each ciphertext to
// its space; see coresecret.AAD.
type Service struct {
	Store  coresecret.Store
	Sealer coresecret.Sealer
}

// CreateCmd creates a Secret with its first items.
type CreateCmd struct {
	SpaceID     string
	CreatedBy   string
	Name        string
	Description string
	Items       map[string]string
}

// Create validates and seals the items, then stores the Secret.
func (s *Service) Create(ctx context.Context, cmd CreateCmd) (*coresecret.Secret, error) {
	name := strings.TrimSpace(cmd.Name)
	if name == "" {
		return nil, ErrNameRequired
	}
	if util.ExceedsRuneLimit(name, maxSecretNameRunes) {
		return nil, ErrNameTooLong
	}
	cmd.Name = name
	if util.ExceedsRuneLimit(cmd.Description, maxSecretDescriptionRunes) {
		return nil, ErrDescriptionTooLong
	}
	names, err := validateItems(cmd.Items)
	if err != nil {
		return nil, err
	}
	sealed, err := s.Sealer.Seal(coresecret.Items(cmd.Items), coresecret.AAD(cmd.SpaceID))
	if err != nil {
		return nil, err
	}
	created, err := s.Store.CreateSecret(ctx, coresecret.CreateInput{
		SpaceID:     cmd.SpaceID,
		Name:        cmd.Name,
		Description: cmd.Description,
		Provider:    coresecret.ProviderEmbedded,
		CreatedBy:   cmd.CreatedBy,
		ItemNames:   names,
		Sealed:      sealed,
	})
	if errors.Is(err, coresecret.ErrNameTaken) {
		return nil, ErrNameTaken
	}
	return created, err
}

// List returns a space's Secrets, metadata only.
func (s *Service) List(ctx context.Context, spaceID string) ([]coresecret.Secret, error) {
	return s.Store.ListSecretsBySpace(ctx, spaceID)
}

// Get returns one Secret the space owns. A Secret in another space reads as
// not-found, so the answer does not confirm one exists elsewhere.
func (s *Service) Get(ctx context.Context, spaceID, id string) (*coresecret.Secret, error) {
	sec, err := s.scoped(ctx, spaceID, id)
	if err != nil {
		return nil, err
	}
	return sec, nil
}

// ReplaceItems sets the whole item map, the shape a raw-JSON editor sends.
func (s *Service) ReplaceItems(ctx context.Context, spaceID, id string, items map[string]string) (*coresecret.Secret, error) {
	sec, err := s.scoped(ctx, spaceID, id)
	if err != nil {
		return nil, err
	}
	if sec.State == coresecret.StateDestroyed {
		return nil, ErrDestroyed
	}
	names, err := validateItems(items)
	if err != nil {
		return nil, err
	}
	return s.seal(ctx, spaceID, id, items, names)
}

// PatchItems sets some item keys and removes others, the shape a row editor
// sends. It decrypts the current items, applies the delta, and re-seals the
// whole map -- one atomic row rewrite, so a rotation of several items is
// consistent.
func (s *Service) PatchItems(ctx context.Context, spaceID, id string, set map[string]string, remove []string) (*coresecret.Secret, error) {
	sec, sealed, err := s.scopedSealed(ctx, spaceID, id)
	if err != nil {
		return nil, err
	}
	if sec.State == coresecret.StateDestroyed {
		return nil, ErrDestroyed
	}
	items, err := s.Sealer.Open(*sealed, coresecret.AAD(sec.SpaceID))
	if err != nil {
		return nil, err
	}
	merged := map[string]string(items)
	if merged == nil {
		merged = map[string]string{}
	}
	for _, k := range remove {
		if _, ok := merged[k]; !ok {
			return nil, ErrItemNotFound
		}
		delete(merged, k)
	}
	maps.Copy(merged, set)
	names, err := validateItems(merged)
	if err != nil {
		return nil, err
	}
	return s.seal(ctx, spaceID, id, merged, names)
}

// Materialize returns a Secret's decrypted items for an authorized runtime
// consumer. It refuses a disabled or destroyed Secret: a run must not receive a
// value the owner has withdrawn. The caller is responsible for the run
// authorization; this only enforces space ownership and Secret state.
func (s *Service) Materialize(ctx context.Context, spaceID, id string) (coresecret.Items, error) {
	sec, sealed, err := s.scopedSealed(ctx, spaceID, id)
	if err != nil {
		return nil, err
	}
	if sec.State != coresecret.StateActive {
		return nil, ErrDisabled
	}
	return s.Sealer.Open(*sealed, coresecret.AAD(spaceID))
}

// SetState disables, re-enables, or destroys a Secret.
func (s *Service) SetState(ctx context.Context, spaceID, id string, state coresecret.State) (*coresecret.Secret, error) {
	switch state {
	case coresecret.StateActive, coresecret.StateDisabled, coresecret.StateDestroyed:
	default:
		return nil, ErrUnknownState
	}
	sec, err := s.scoped(ctx, spaceID, id)
	if err != nil {
		return nil, err
	}
	// Destruction is terminal: a destroyed Secret has no sealed material left, so
	// reviving it to active/disabled would leave one that reads usable but can
	// never materialize. Refuse the transition rather than store an empty Secret.
	if !coresecret.ValidStateTransition(sec.State, state) {
		return nil, ErrStateTransition
	}
	return s.Store.SetState(ctx, id, state)
}

// seal re-seals items and stores them. Item names are pre-validated.
func (s *Service) seal(ctx context.Context, spaceID, id string, items map[string]string, names []string) (*coresecret.Secret, error) {
	sealed, err := s.Sealer.Seal(coresecret.Items(items), coresecret.AAD(spaceID))
	if err != nil {
		return nil, err
	}
	return s.Store.UpdateItems(ctx, coresecret.UpdateItemsInput{ID: id, ItemNames: names, Sealed: sealed})
}

// scoped fetches a Secret and refuses one that is not the space's.
func (s *Service) scoped(ctx context.Context, spaceID, id string) (*coresecret.Secret, error) {
	sec, err := s.Store.GetSecret(ctx, id)
	if err != nil {
		return nil, err
	}
	if sec == nil || sec.SpaceID != spaceID {
		return nil, ErrNotFound
	}
	return sec, nil
}

func (s *Service) scopedSealed(ctx context.Context, spaceID, id string) (*coresecret.Secret, *coresecret.Sealed, error) {
	sec, sealed, err := s.Store.GetSealed(ctx, id)
	if err != nil {
		// GetSealed refuses a destroyed Secret with ErrNotFound; the row may or
		// may not exist, but either way the caller cannot have its material.
		if errors.Is(err, apierr.ErrNotFound) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	if sec == nil || sec.SpaceID != spaceID {
		return nil, nil, ErrNotFound
	}
	return sec, sealed, nil
}

// validateItems checks the map is non-empty and every name is an identifier,
// returning the sorted name set to store. Item names must be identifiers so a
// whole group can be injected as environment variables (§5.1, §6.2).
func validateItems(items map[string]string) ([]string, error) {
	if len(items) == 0 {
		return nil, ErrNoItems
	}
	names := make([]string, 0, len(items))
	for name := range items {
		if !coresecret.IsItemName(name) {
			return nil, ErrInvalidItem
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
