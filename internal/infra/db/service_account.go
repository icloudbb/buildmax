package db

import (
	"context"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/util"
	"gorm.io/gorm"
)

// CreateServiceAccount implements coreidentity.ServiceAccountStore.
func (s *Store) CreateServiceAccount(ctx context.Context, in coreidentity.NewServiceAccount) (*coreidentity.User, error) {
	now := time.Now().UTC()
	row := &userRow{Name: in.Name, Kind: coreidentity.KindService, CreatedAt: now}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		spaceKey, err := lookupKey(ctx, tx, "space", in.SpaceID)
		if err != nil {
			return err
		}
		sponsorKey, err := lookupKey(ctx, tx, "user", in.SponsorUserID)
		if err != nil {
			return err
		}
		row.SponsorUserID = &sponsorKey
		if err := createWithPublicID(ctx, tx, "uq_user_public_id",
			func(id string) { row.PublicID = id }, row); err != nil {
			return err
		}
		return tx.Create(&spaceMemberRow{
			SpaceID:   spaceKey,
			UserID:    row.ID,
			Role:      corespace.RoleMember,
			CreatedAt: now,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	sponsor := in.SponsorUserID
	return &coreidentity.User{
		ID:            row.PublicID,
		Name:          row.Name,
		Kind:          coreidentity.KindService,
		SponsorUserID: &sponsor,
		CreatedAt:     now,
	}, nil
}

// ListServiceAccountsBySpace implements coreidentity.ServiceAccountStore.
func (s *Store) ListServiceAccountsBySpace(ctx context.Context, spaceID string) ([]coreidentity.User, error) {
	id, ok := util.CanonicalPublicID(spaceID)
	if !ok {
		return nil, nil
	}
	var rows []userReadRow
	err := s.userSelect(ctx).
		Joins("INNER JOIN space_member sm ON sm.user_id = `user`.id").
		Joins("INNER JOIN space ON space.id = sm.space_id").
		Where("space.public_id = ? AND `user`.kind = ?", id, coreidentity.KindService).
		Order("`user`.created_at ASC, `user`.id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]coreidentity.User, 0, len(rows))
	for i := range rows {
		out = append(out, *toUser(&rows[i]))
	}
	return out, nil
}

// UpdateServiceAccount implements coreidentity.ServiceAccountStore.
func (s *Store) UpdateServiceAccount(ctx context.Context, in coreidentity.ServiceAccountUpdate) error {
	id, ok := util.CanonicalPublicID(in.UserID)
	if !ok {
		return coreidentity.ErrUserNotFound
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&userRow{}).
			Where("public_id = ? AND kind = ?", id, coreidentity.KindService).
			Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return coreidentity.ErrUserNotFound
		}
		updates := map[string]any{}
		if in.Name != nil {
			updates["name"] = *in.Name
		}
		if in.SponsorUserID != nil {
			sponsorKey, err := lookupKey(ctx, tx, "user", *in.SponsorUserID)
			if err != nil {
				return err
			}
			updates["sponsor_user_id"] = sponsorKey
		}
		if len(updates) == 0 {
			return nil
		}
		return tx.Model(&userRow{}).Where("public_id = ?", id).Updates(updates).Error
	})
}
