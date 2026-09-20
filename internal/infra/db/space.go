package db

import (
	"context"
	"errors"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	corespace "github.com/icloudbb/buildmax/internal/core/space"

	"github.com/icloudbb/buildmax/internal/util"
	"gorm.io/gorm"
)

type spaceRow struct {
	ID                uint64  `gorm:"primaryKey;autoIncrement"`
	PublicID          string  `gorm:"column:public_id;type:char(20) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_space_public_id;not null"`
	Name              string  `gorm:"type:varchar(255);not null"`
	PersonalForUserID *uint64 `gorm:"column:personal_for_user_id;uniqueIndex"`
	QuotaTier         string  `gorm:"column:quota_tier;type:varchar(64)"`
	// PluginCuration is who fills the space's plugin activation list. It
	// defaults to open: the gate that crosses spaces is operator eligibility,
	// not a space's housekeeping. See docs/design/plugin-space-distribution.md.
	PluginCuration            string `gorm:"column:plugin_curation;type:varchar(16);not null;default:'open'"`
	AgentInstructions         string `gorm:"column:agent_instructions;type:text"`
	AgentInstructionsRevision int    `gorm:"column:agent_instructions_revision;not null;default:0"`
	// DefaultSandboxNetworkTier and DefaultSandboxFilesystemTier mirror
	// agentRow's columns of the same names: empty means this space sets no
	// default and an agent that declares neither tier falls through to the
	// surface baseline.
	DefaultSandboxNetworkTier    string    `gorm:"column:default_sandbox_network_tier;type:varchar(64)"`
	DefaultSandboxFilesystemTier string    `gorm:"column:default_sandbox_filesystem_tier;type:varchar(64)"`
	CreatedBy                    uint64    `gorm:"column:created_by;not null"`
	CreatedAt                    time.Time `gorm:"autoCreateTime"`
	UpdatedAt                    time.Time `gorm:"autoUpdateTime"`
}

func (spaceRow) TableName() string { return "space" }

// spaceReadRow is spaceRow plus the handles its user references resolve to.
//
// Every read that returns a space joins for them, so a caller never sees a row
// key and no listing turns into one query per row. spaceSelect is the one place
// the join set is written down.
//
// The row is a named field, not an anonymous one. GORM reads an anonymous
// embedded struct that has its own TableName as an association and scans none
// of its columns, which produces a zero-valued row rather than an error. Every
// read struct in this package follows this shape for that reason.
//
// A pointer field is one a LEFT JOIN may leave NULL.
type spaceReadRow struct {
	Row                     spaceRow `gorm:"embedded"`
	PersonalForUserPublicID *string  `gorm:"column:personal_for_user_public_id"`
	CreatedByPublicID       *string  `gorm:"column:created_by_public_id"`
}

type spaceMemberRow struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement"`
	SpaceID   uint64    `gorm:"column:space_id;not null;uniqueIndex:uq_space_member_space_user"`
	UserID    uint64    `gorm:"column:user_id;not null;uniqueIndex:uq_space_member_space_user"`
	Role      string    `gorm:"type:varchar(32);not null"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
}

func (spaceMemberRow) TableName() string { return "space_member" }

// spaceMemberReadRow is spaceMemberRow plus the handles its two references
// resolve to. A membership has no handle of its own.
type spaceMemberReadRow struct {
	Row           spaceMemberRow `gorm:"embedded"`
	SpacePublicID string         `gorm:"column:space_public_id"`
	UserPublicID  string         `gorm:"column:user_public_id"`
}

// spaceSelect is the read shape for a space: the row plus the public handles of
// the users it names.
func (s *Store) spaceSelect(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Model(&spaceRow{}).
		Select("space.*, pu.public_id AS personal_for_user_public_id, cb.public_id AS created_by_public_id").
		Joins("LEFT JOIN `user` pu ON pu.id = space.personal_for_user_id").
		Joins("LEFT JOIN `user` cb ON cb.id = space.created_by")
}

// spaceMemberSelect is the read shape for a membership.
func (s *Store) spaceMemberSelect(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Model(&spaceMemberRow{}).
		Select("space_member.*, t.public_id AS space_public_id, u.public_id AS user_public_id").
		Joins("INNER JOIN space t ON t.id = space_member.space_id").
		Joins("INNER JOIN `user` u ON u.id = space_member.user_id")
}

func toSpace(row *spaceReadRow) *corespace.Space {
	if row == nil {
		return nil
	}
	out := &corespace.Space{
		ID:                           row.Row.PublicID,
		Name:                         row.Row.Name,
		QuotaTier:                    row.Row.QuotaTier,
		PluginCuration:               coreplugin.NormalizeCuration(row.Row.PluginCuration),
		AgentInstructions:            row.Row.AgentInstructions,
		AgentInstructionsRevision:    row.Row.AgentInstructionsRevision,
		DefaultSandboxNetworkTier:    row.Row.DefaultSandboxNetworkTier,
		DefaultSandboxFilesystemTier: row.Row.DefaultSandboxFilesystemTier,
		CreatedBy:                    derefPublicID(row.CreatedByPublicID),
		CreatedAt:                    row.Row.CreatedAt,
		UpdatedAt:                    row.Row.UpdatedAt,
	}
	if row.Row.PersonalForUserID != nil {
		personal := derefPublicID(row.PersonalForUserPublicID)
		out.PersonalForUserID = &personal
	}
	return out
}

func toSpaces(rows []spaceReadRow) []corespace.Space {
	out := make([]corespace.Space, len(rows))
	for i := range rows {
		out[i] = *toSpace(&rows[i])
	}
	return out
}

func toSpaceMember(row *spaceMemberReadRow) *corespace.Member {
	if row == nil {
		return nil
	}
	return &corespace.Member{
		SpaceID:   row.SpacePublicID,
		UserID:    row.UserPublicID,
		Role:      row.Row.Role,
		CreatedAt: row.Row.CreatedAt,
	}
}

func toSpaceMembers(rows []spaceMemberReadRow) []corespace.Member {
	out := make([]corespace.Member, len(rows))
	for i := range rows {
		out[i] = *toSpaceMember(&rows[i])
	}
	return out
}

// GetSpace returns the space by space_id, or (nil, nil) when not found.
func (s *Store) GetSpace(ctx context.Context, spaceID string) (*corespace.Space, error) {
	id, ok := util.CanonicalPublicID(spaceID)
	if !ok {
		return nil, nil
	}
	var space spaceReadRow
	err := s.spaceSelect(ctx).Where("space.public_id = ?", id).Take(&space).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toSpace(&space), nil
}

// GetPersonalSpaceByUser returns the default personal space for the user, or (nil, nil) when not found.
func (s *Store) GetPersonalSpaceByUser(ctx context.Context, userID string) (*corespace.Space, error) {
	id, ok := util.CanonicalPublicID(userID)
	if !ok {
		return nil, nil
	}
	var space spaceReadRow
	err := s.spaceSelect(ctx).Where("pu.public_id = ?", id).Take(&space).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toSpace(&space), nil
}

// ListSpacesByUser returns all spaces the user belongs to, ordered by created_at ASC.
func (s *Store) ListSpacesByUser(ctx context.Context, userID string) ([]corespace.Space, error) {
	id, ok := util.CanonicalPublicID(userID)
	if !ok {
		return nil, nil
	}
	var list []spaceReadRow
	err := s.spaceSelect(ctx).
		Joins("INNER JOIN space_member ON space_member.space_id = space.id").
		Joins("INNER JOIN `user` mu ON mu.id = space_member.user_id").
		Where("mu.public_id = ?", id).
		Order("space.created_at ASC").
		Find(&list).Error
	return toSpaces(list), err
}

// CreateSpace creates a new space and owner membership.
func (s *Store) CreateSpace(ctx context.Context, name, createdBy, quotaTier string) (*corespace.Space, error) {
	now := time.Now().UTC()
	spaceDB := &spaceRow{Name: name, QuotaTier: quotaTier, CreatedAt: now, UpdatedAt: now}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		creator, err := lookupKey(ctx, tx, "user", createdBy)
		if err != nil {
			return err
		}
		spaceDB.CreatedBy = creator
		if err := createWithPublicID(ctx, tx, "uq_space_public_id",
			func(id string) { spaceDB.PublicID = id }, spaceDB); err != nil {
			return err
		}
		return tx.Create(&spaceMemberRow{
			SpaceID:   spaceDB.ID,
			UserID:    creator,
			Role:      corespace.RoleOwner,
			CreatedAt: now,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return &corespace.Space{
		ID:        spaceDB.PublicID,
		Name:      name,
		QuotaTier: quotaTier,
		// Normalized through the same call toSpace uses. The column's own
		// 'open' default lands in the row but not in this struct, so reading
		// the field back off a create used to answer "" where reading the same
		// row through GetSpace answered "open".
		PluginCuration: coreplugin.NormalizeCuration(spaceDB.PluginCuration),
		CreatedBy:      createdBy,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// AddSpaceMember adds or updates a space membership.
func (s *Store) AddSpaceMember(ctx context.Context, spaceID, userID, role string) (*corespace.Member, error) {
	member := &corespace.Member{
		SpaceID:   spaceID,
		UserID:    userID,
		Role:      role,
		CreatedAt: time.Now().UTC(),
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		spaceKey, err := lookupKey(ctx, tx, "space", spaceID)
		if err != nil {
			return err
		}
		userKey, err := lookupKey(ctx, tx, "user", userID)
		if err != nil {
			return err
		}
		var existing spaceMemberRow
		findErr := tx.Where("space_id = ? AND user_id = ?", spaceKey, userKey).First(&existing).Error
		switch {
		case errors.Is(findErr, gorm.ErrRecordNotFound):
			return tx.Create(&spaceMemberRow{
				SpaceID:   spaceKey,
				UserID:    userKey,
				Role:      role,
				CreatedAt: member.CreatedAt,
			}).Error
		case findErr != nil:
			return findErr
		default:
			existing.Role = role
			member.CreatedAt = existing.CreatedAt
			return tx.Save(&existing).Error
		}
	})
	if err != nil {
		return nil, err
	}
	return member, nil
}

// TransferOwnership implements corespace.Store. It moves the owner role in
// one transaction: a space must never be read with two owners or none because
// a caller observed the change half-applied.
func (s *Store) TransferOwnership(ctx context.Context, spaceID, fromUserID, toUserID string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		spaceKey, err := lookupKey(ctx, tx, "space", spaceID)
		if err != nil {
			return err
		}
		fromKey, err := lookupKey(ctx, tx, "user", fromUserID)
		if err != nil {
			return err
		}
		toKey, err := lookupKey(ctx, tx, "user", toUserID)
		if err != nil {
			return err
		}
		if err := tx.Model(&spaceMemberRow{}).
			Where("space_id = ? AND user_id = ?", spaceKey, toKey).
			Update("role", corespace.RoleOwner).Error; err != nil {
			return err
		}
		return tx.Model(&spaceMemberRow{}).
			Where("space_id = ? AND user_id = ?", spaceKey, fromKey).
			Update("role", corespace.RoleAdmin).Error
	})
}

// RemoveSpaceMember removes a space membership when present.
func (s *Store) RemoveSpaceMember(ctx context.Context, spaceID, userID string) error {
	spaceKey, err := lookupKey(ctx, s.db, "space", spaceID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	userKey, err := lookupKey(ctx, s.db, "user", userID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).
		Where("space_id = ? AND user_id = ?", spaceKey, userKey).
		Delete(&spaceMemberRow{}).Error
}

// ListSpaceMembers returns members of the space ordered by created_at ASC.
func (s *Store) ListSpaceMembers(ctx context.Context, spaceID string) ([]corespace.Member, error) {
	id, ok := util.CanonicalPublicID(spaceID)
	if !ok {
		return nil, nil
	}
	var list []spaceMemberReadRow
	err := s.spaceMemberSelect(ctx).
		Where("t.public_id = ?", id).
		Order("space_member.created_at ASC").
		Find(&list).Error
	return toSpaceMembers(list), err
}

func (s *Store) personalSpaceIDForUser(ctx context.Context, userID string) (string, error) {
	space, err := s.GetPersonalSpaceByUser(ctx, userID)
	if err != nil {
		return "", err
	}
	if space == nil {
		return "", nil
	}
	return space.ID, nil
}

// ListTeamSpaces implements corespace.Store.
func (s *Store) ListTeamSpaces(ctx context.Context, query string, limit, offset int) ([]corespace.Space, int, error) {
	limit, offset = clampPage(limit, offset)
	var total int64
	countQ := s.db.WithContext(ctx).Model(&spaceRow{}).Where("personal_for_user_id IS NULL")
	if query != "" {
		countQ = countQ.Where("name LIKE ?", "%"+query+"%")
	}
	if err := countQ.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	q := s.spaceSelect(ctx).Where("space.personal_for_user_id IS NULL")
	if query != "" {
		q = q.Where("space.name LIKE ?", "%"+query+"%")
	}
	var rows []spaceReadRow
	if err := q.Order("space.created_at DESC, space.id DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]corespace.Space, 0, len(rows))
	for i := range rows {
		out = append(out, *toSpace(&rows[i]))
	}
	return out, int(total), nil
}

// CountSpaceMembers implements corespace.Store.
func (s *Store) CountSpaceMembers(ctx context.Context, spaceIDs []string) (map[string]int, error) {
	out := make(map[string]int, len(spaceIDs))
	if len(spaceIDs) == 0 {
		return out, nil
	}
	// Counting groups by the row key, so the handles the caller asked about are
	// carried through the join rather than resolved one at a time.
	ids := make([]string, 0, len(spaceIDs))
	for _, spaceID := range spaceIDs {
		if id, ok := util.CanonicalPublicID(spaceID); ok {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		PublicID string
		N        int
	}
	if err := s.db.WithContext(ctx).
		Model(&spaceMemberRow{}).
		Select("t.public_id AS public_id, count(*) AS n").
		Joins("INNER JOIN space t ON t.id = space_member.space_id").
		Where("t.public_id IN ?", ids).
		Group("t.public_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.PublicID] = row.N
	}
	return out, nil
}

// SetSpacePluginCuration records who fills the space's plugin activation list.
//
// The value is validated above this layer, which is why an unrecognized one
// reaches the column rather than being rejected here: this package translates,
// it does not decide what a mode means.
func (s *Store) SetSpacePluginCuration(ctx context.Context, spaceID string, mode coreplugin.Curation) error {
	key, err := lookupKey(ctx, s.db, "space", spaceID)
	if err != nil {
		return err
	}
	// Zero rows affected is the mode already having that value: the space
	// resolved a moment ago, so it is not a missing row.
	return s.db.WithContext(ctx).Model(&spaceRow{}).Where("id = ?", key).
		Update("plugin_curation", string(mode)).Error
}

// SetSpaceSandboxDefaults records the tiers an agent that declares neither
// inherits.
//
// The values are validated above this layer, which is why an unrecognized one
// reaches the columns rather than being rejected here: this package
// translates, it does not decide what a tier means.
func (s *Store) SetSpaceSandboxDefaults(ctx context.Context, spaceID, networkTier, filesystemTier string) error {
	key, err := lookupKey(ctx, s.db, "space", spaceID)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(&spaceRow{}).Where("id = ?", key).
		Updates(map[string]any{
			"default_sandbox_network_tier":    networkTier,
			"default_sandbox_filesystem_tier": filesystemTier,
		}).Error
}

// SetSpaceAgentInstructions replaces a space's shared agent guidance and bumps
// its revision only when the text actually changes.
func (s *Store) SetSpaceAgentInstructions(ctx context.Context, spaceID, instructions string) error {
	key, err := lookupKey(ctx, s.db, "space", spaceID)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(&spaceRow{}).
		Where("id = ? AND (agent_instructions IS NULL OR agent_instructions <> ?)", key, instructions).
		Updates(map[string]any{
			"agent_instructions":          instructions,
			"agent_instructions_revision": gorm.Expr("agent_instructions_revision + 1"),
		}).Error
}
