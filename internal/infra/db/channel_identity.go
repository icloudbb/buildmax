package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	corechannel "github.com/icloudbb/buildmax/internal/core/channel"
	"github.com/icloudbb/buildmax/internal/util"
)

// channelIdentityRow links one chat-platform account to one user. The unique
// index is the invariant: a chat account speaks for at most one user. A user
// may link several chat accounts. The platform ids use a binary collation
// because they are opaque protocol values.
type channelIdentityRow struct {
	ID             uint64    `gorm:"primaryKey;autoIncrement"`
	PublicID       string    `gorm:"column:public_id;type:char(20) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_channel_identity_public_id;not null"`
	UserID         uint64    `gorm:"column:user_id;not null;index"`
	Platform       string    `gorm:"column:platform;type:varchar(32) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_channel_identity_external,priority:1"`
	Tenant         string    `gorm:"column:tenant;type:varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;not null;default:'';uniqueIndex:uq_channel_identity_external,priority:2"`
	ExternalUserID string    `gorm:"column:external_user_id;type:varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;not null;uniqueIndex:uq_channel_identity_external,priority:3"`
	Handle         string    `gorm:"column:handle;type:varchar(255)"`
	CreatedAt      time.Time `gorm:"autoCreateTime"`
}

func (channelIdentityRow) TableName() string { return "channel_identity" }

// channelPairingRow is a link request a chat account started. Only the code's
// hash is stored, so a database backup yields nothing redeemable. One pending
// row per chat account: a new request replaces the old one.
type channelPairingRow struct {
	ID             uint64    `gorm:"primaryKey;autoIncrement"`
	CodeHash       string    `gorm:"column:code_hash;type:char(64) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex;not null"`
	Platform       string    `gorm:"column:platform;type:varchar(32) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_channel_pairing_external,priority:1"`
	Tenant         string    `gorm:"column:tenant;type:varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;not null;default:'';uniqueIndex:uq_channel_pairing_external,priority:2"`
	ExternalUserID string    `gorm:"column:external_user_id;type:varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;not null;uniqueIndex:uq_channel_pairing_external,priority:3"`
	ChatID         string    `gorm:"column:chat_id;type:varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;not null"`
	Handle         string    `gorm:"column:handle;type:varchar(255)"`
	ExpiresAt      time.Time `gorm:"column:expires_at;not null;index"`
	CreatedAt      time.Time `gorm:"autoCreateTime"`
}

func (channelPairingRow) TableName() string { return "channel_pairing" }

func hashPairingCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

type channelIdentityReadRow struct {
	Row          channelIdentityRow `gorm:"embedded"`
	UserPublicID string             `gorm:"column:user_public_id"`
}

func channelIdentitySelect(tx *gorm.DB) *gorm.DB {
	return tx.Model(&channelIdentityRow{}).
		Select("channel_identity.*, u.public_id AS user_public_id").
		Joins("INNER JOIN `user` u ON u.id = channel_identity.user_id")
}

func (r channelIdentityReadRow) toIdentity() corechannel.Identity {
	return corechannel.Identity{
		ID:             r.Row.PublicID,
		UserID:         r.UserPublicID,
		Platform:       r.Row.Platform,
		Tenant:         r.Row.Tenant,
		ExternalUserID: r.Row.ExternalUserID,
		Handle:         r.Row.Handle,
		CreatedAt:      r.Row.CreatedAt,
	}
}

func (p channelPairingRow) toPairing() corechannel.Pairing {
	return corechannel.Pairing{
		Platform:       p.Platform,
		Tenant:         p.Tenant,
		ExternalUserID: p.ExternalUserID,
		ChatID:         p.ChatID,
		Handle:         p.Handle,
		ExpiresAt:      p.ExpiresAt,
	}
}

// IdentityByExternal implements corechannel.IdentityStore.
func (s *Store) IdentityByExternal(ctx context.Context, platform, tenant, externalUserID string) (*corechannel.Identity, error) {
	if platform == "" || externalUserID == "" {
		return nil, nil
	}
	var row channelIdentityReadRow
	err := channelIdentitySelect(s.db.WithContext(ctx)).
		Where("channel_identity.platform = ? AND channel_identity.tenant = ? AND channel_identity.external_user_id = ?",
			platform, tenant, externalUserID).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	id := row.toIdentity()
	return &id, nil
}

// ListIdentitiesByUser implements corechannel.IdentityStore.
func (s *Store) ListIdentitiesByUser(ctx context.Context, userID string) ([]corechannel.Identity, error) {
	userKey, err := lookupKey(ctx, s.db, "user", userID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rows []channelIdentityReadRow
	if err := channelIdentitySelect(s.db.WithContext(ctx)).
		Where("channel_identity.user_id = ?", userKey).
		Order("channel_identity.created_at DESC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]corechannel.Identity, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toIdentity())
	}
	return out, nil
}

// DeleteIdentity implements corechannel.IdentityStore. The user is part of the
// condition, so one person cannot remove another's link by naming its id.
func (s *Store) DeleteIdentity(ctx context.Context, userID, identityID string) error {
	id, ok := util.CanonicalPublicID(identityID)
	if !ok {
		return apierr.ErrNotFound
	}
	userKey, err := lookupKey(ctx, s.db, "user", userID)
	if err != nil {
		return err
	}
	res := s.db.WithContext(ctx).
		Where("public_id = ? AND user_id = ?", id, userKey).
		Delete(&channelIdentityRow{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return apierr.ErrNotFound
	}
	return nil
}

// CreatePairing implements corechannel.IdentityStore.
//
// Expired rows from anyone are swept in the same transaction: pairing rows are
// only ever created here, so this is where their lifetime is cheapest to bound.
func (s *Store) CreatePairing(ctx context.Context, p corechannel.Pairing, code string) error {
	if code == "" || p.Platform == "" || p.ExternalUserID == "" {
		return errors.New("channel pairing: code, platform, and external user id required")
	}
	row := channelPairingRow{
		CodeHash:       hashPairingCode(code),
		Platform:       p.Platform,
		Tenant:         p.Tenant,
		ExternalUserID: p.ExternalUserID,
		ChatID:         p.ChatID,
		Handle:         p.Handle,
		ExpiresAt:      p.ExpiresAt.UTC(),
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("expires_at <= ?", time.Now().UTC()).Delete(&channelPairingRow{}).Error; err != nil {
			return err
		}
		if err := tx.Where("platform = ? AND tenant = ? AND external_user_id = ?", p.Platform, p.Tenant, p.ExternalUserID).
			Delete(&channelPairingRow{}).Error; err != nil {
			return err
		}
		return tx.Create(&row).Error
	})
}

// PairingByCode implements corechannel.IdentityStore.
func (s *Store) PairingByCode(ctx context.Context, code string, now time.Time) (*corechannel.Pairing, error) {
	if code == "" {
		return nil, nil
	}
	var row channelPairingRow
	err := s.db.WithContext(ctx).
		Where("code_hash = ? AND expires_at > ?", hashPairingCode(code), now.UTC()).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p := row.toPairing()
	return &p, nil
}

// ConsumePairing implements corechannel.IdentityStore.
//
// The pairing row is read under a lock and deleted in the transaction that
// creates the link, so two confirmations racing on one code produce one link.
func (s *Store) ConsumePairing(ctx context.Context, code, userID string, now time.Time) (*corechannel.Identity, *corechannel.Pairing, error) {
	if code == "" {
		return nil, nil, corechannel.ErrPairingNotFound
	}
	var (
		out     corechannel.Identity
		pairing corechannel.Pairing
	)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		userKey, err := lookupKey(ctx, tx, "user", userID)
		if err != nil {
			return err
		}
		var p channelPairingRow
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("code_hash = ? AND expires_at > ?", hashPairingCode(code), now.UTC()).
			Take(&p).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return corechannel.ErrPairingNotFound
		}
		if err != nil {
			return err
		}
		pairing = p.toPairing()
		if err := tx.Delete(&channelPairingRow{}, p.ID).Error; err != nil {
			return err
		}
		var existing channelIdentityReadRow
		err = channelIdentitySelect(tx).
			Where("channel_identity.platform = ? AND channel_identity.tenant = ? AND channel_identity.external_user_id = ?",
				p.Platform, p.Tenant, p.ExternalUserID).
			Take(&existing).Error
		if err == nil {
			if existing.Row.UserID != userKey {
				return corechannel.ErrAlreadyLinked
			}
			out = existing.toIdentity()
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		row := channelIdentityRow{
			UserID:         userKey,
			Platform:       p.Platform,
			Tenant:         p.Tenant,
			ExternalUserID: p.ExternalUserID,
			Handle:         p.Handle,
			CreatedAt:      now.UTC(),
		}
		if err := createWithPublicID(ctx, tx, "uq_channel_identity_public_id",
			func(id string) { row.PublicID = id }, &row); err != nil {
			// Another confirmation linked this chat account first.
			if isDuplicateOnIndex(err, "uq_channel_identity_external") {
				return corechannel.ErrAlreadyLinked
			}
			return err
		}
		out = corechannel.Identity{
			ID:             row.PublicID,
			UserID:         canonicalPublicID(userID),
			Platform:       row.Platform,
			Tenant:         row.Tenant,
			ExternalUserID: row.ExternalUserID,
			Handle:         row.Handle,
			CreatedAt:      row.CreatedAt,
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return &out, &pairing, nil
}
