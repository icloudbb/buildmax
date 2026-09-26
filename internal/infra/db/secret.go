package db

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coresecret "github.com/icloudbb/buildmax/internal/core/secret"
)

// secretRow is one Space-owned Secret: a group of named items stored as one
// encrypted blob. The items are a single AEAD ciphertext (Ciphertext under
// Nonce, its DEK sealed as WrappedDEK by the KEK named in KeyID), rewritten
// whole on every edit -- which is what makes a multi-item rotation atomic.
// ItemNames is the plaintext key set so a listing and consumption validation
// work without decrypting anything. No item value is stored in the clear, and
// there is no reveal path. See docs/design/space-secrets.md §5.1.
type secretRow struct {
	ID       uint64 `gorm:"primaryKey;autoIncrement"`
	PublicID string `gorm:"column:public_id;type:char(20) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_secret_public_id;not null"`

	SpaceID uint64 `gorm:"column:space_id;not null;uniqueIndex:ux_secret_space_name,priority:1"`
	Name    string `gorm:"type:varchar(128);not null;uniqueIndex:ux_secret_space_name,priority:2"`

	Description string `gorm:"type:varchar(1024);not null;default:''"`
	Provider    string `gorm:"type:varchar(32);not null;default:'embedded'"`
	State       string `gorm:"type:varchar(16);not null;default:'active'"`

	// ItemNames is a JSON array of the item keys present, in the clear.
	ItemNames string `gorm:"column:item_names;type:text;not null"`

	// The sealed items and their envelope metadata. A destroyed Secret has
	// these cleared.
	Ciphertext []byte `gorm:"column:ciphertext;type:blob"`
	Nonce      []byte `gorm:"column:nonce;type:varbinary(64)"`
	WrappedDEK []byte `gorm:"column:wrapped_dek;type:varbinary(256)"`
	KeyID      string `gorm:"column:key_id;type:varchar(128);not null;default:''"`

	CreatedBy uint64    `gorm:"column:created_by;not null"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

func (secretRow) TableName() string { return "secret" }

// secretReadRow is secretRow plus the handles its references resolve to.
type secretReadRow struct {
	Row             secretRow `gorm:"embedded"`
	SpacePublicID   string    `gorm:"column:space_public_id"`
	CreatedByPublic string    `gorm:"column:created_by_public_id"`
}

func (s *Store) secretSelect(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Model(&secretRow{}).
		Select("secret.*, t.public_id AS space_public_id, cb.public_id AS created_by_public_id").
		Joins("INNER JOIN space t ON t.id = secret.space_id").
		Joins("INNER JOIN `user` cb ON cb.id = secret.created_by")
}

func toSecret(r *secretReadRow) *coresecret.Secret {
	var names []string
	if r.Row.ItemNames != "" {
		_ = json.Unmarshal([]byte(r.Row.ItemNames), &names)
	}
	return &coresecret.Secret{
		ID:          r.Row.PublicID,
		SpaceID:     r.SpacePublicID,
		Name:        r.Row.Name,
		Description: r.Row.Description,
		Provider:    coresecret.Provider(r.Row.Provider),
		State:       coresecret.State(r.Row.State),
		ItemNames:   names,
		CreatedBy:   r.CreatedByPublic,
		CreatedAt:   r.Row.CreatedAt.UTC(),
		UpdatedAt:   r.Row.UpdatedAt.UTC(),
	}
}

func encodeItemNames(names []string) string {
	if len(names) == 0 {
		return "[]"
	}
	b, err := json.Marshal(names)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// CreateSecret inserts a new Secret with its first sealed items.
func (s *Store) CreateSecret(ctx context.Context, in coresecret.CreateInput) (*coresecret.Secret, error) {
	provider := in.Provider
	if provider == "" {
		provider = coresecret.ProviderEmbedded
	}
	row := &secretRow{
		Description: in.Description,
		Name:        in.Name,
		Provider:    string(provider),
		State:       string(coresecret.StateActive),
		ItemNames:   encodeItemNames(in.ItemNames),
		Ciphertext:  in.Sealed.Ciphertext,
		Nonce:       in.Sealed.Nonce,
		WrappedDEK:  in.Sealed.WrappedDEK,
		KeyID:       in.Sealed.KeyID,
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		spaceKey, err := lookupKey(ctx, tx, "space", in.SpaceID)
		if err != nil {
			return err
		}
		creatorKey, err := lookupKey(ctx, tx, "user", in.CreatedBy)
		if err != nil {
			return err
		}
		row.SpaceID = spaceKey
		row.CreatedBy = creatorKey
		return createWithPublicID(ctx, tx, "uq_secret_public_id",
			func(id string) { row.PublicID = id }, row)
	})
	if err != nil {
		return nil, err
	}
	return s.GetSecret(ctx, row.PublicID)
}

// GetSecret returns one Secret's metadata, or nil when no such Secret exists.
// It never carries the sealed bytes.
func (s *Store) GetSecret(ctx context.Context, id string) (*coresecret.Secret, error) {
	var r secretReadRow
	err := s.secretSelect(ctx).Where("secret.public_id = ?", id).Take(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toSecret(&r), nil
}

// ListSecretsBySpace returns a space's Secrets, newest first, metadata only.
func (s *Store) ListSecretsBySpace(ctx context.Context, spaceID string) ([]coresecret.Secret, error) {
	spaceKey, err := lookupKey(ctx, s.db, "space", spaceID)
	if err != nil {
		return nil, err
	}
	var rows []secretReadRow
	if err := s.secretSelect(ctx).Where("secret.space_id = ?", spaceKey).
		Order("secret.id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]coresecret.Secret, 0, len(rows))
	for i := range rows {
		out = append(out, *toSecret(&rows[i]))
	}
	return out, nil
}

// GetSealed returns a Secret's metadata and its sealed items, for
// materialization. A missing Secret yields nil; a destroyed one
// is refused with ErrNotFound -- the row still exists for audit, but its
// material is cryptographically gone, so this is a deliberate state refusal
// rather than a missing row.
func (s *Store) GetSealed(ctx context.Context, id string) (*coresecret.Secret, *coresecret.Sealed, error) {
	var r secretReadRow
	err := s.secretSelect(ctx).Where("secret.public_id = ?", id).Take(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if r.Row.State == string(coresecret.StateDestroyed) {
		return nil, nil, apierr.ErrNotFound
	}
	sealed := &coresecret.Sealed{
		Ciphertext: r.Row.Ciphertext,
		Nonce:      r.Row.Nonce,
		WrappedDEK: r.Row.WrappedDEK,
		KeyID:      r.Row.KeyID,
	}
	return toSecret(&r), sealed, nil
}

// UpdateItems rewrites a Secret's sealed items and item names as one row. A
// destroyed Secret is refused. A KEK rotation does not come through here: it
// re-wraps the DEK alone (RewrapSealedKeys).
func (s *Store) UpdateItems(ctx context.Context, in coresecret.UpdateItemsInput) (*coresecret.Secret, error) {
	err := s.db.WithContext(ctx).Model(&secretRow{}).
		Where("public_id = ? AND state <> ?", in.ID, string(coresecret.StateDestroyed)).
		Updates(map[string]any{
			"item_names":  encodeItemNames(in.ItemNames),
			"ciphertext":  in.Sealed.Ciphertext,
			"nonce":       in.Sealed.Nonce,
			"wrapped_dek": in.Sealed.WrappedDEK,
			"key_id":      in.Sealed.KeyID,
		}).Error
	if err != nil {
		return nil, err
	}
	return s.GetSecret(ctx, in.ID)
}

// SetState moves a Secret to active, disabled, or destroyed. Destroying also
// clears the sealed material: the row stays for audit and references, but the
// value is cryptographically gone.
func (s *Store) SetState(ctx context.Context, id string, state coresecret.State) (*coresecret.Secret, error) {
	updates := map[string]any{"state": string(state)}
	if state == coresecret.StateDestroyed {
		updates["ciphertext"] = nil
		updates["nonce"] = nil
		updates["wrapped_dek"] = nil
		updates["key_id"] = ""
	}
	res := s.db.WithContext(ctx).Model(&secretRow{}).
		Where("public_id = ?", id).Updates(updates)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, apierr.ErrNotFound
	}
	return s.GetSecret(ctx, id)
}
