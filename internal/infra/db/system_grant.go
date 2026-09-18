package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	"github.com/icloudbb/buildmax/internal/util"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// lastHolderTx runs the last-holder guard at READ COMMITTED. The guard
// serializes a revoke and a concurrent disable behind a FOR UPDATE lock on the
// role's live grants, then decides on a count of the effective holders. Under
// MySQL's default REPEATABLE READ that count is a consistent read from the
// snapshot taken at the transaction's first read — established before the lock
// was granted — so the loser of the race still sees the winner's holder as
// effective and both proceed, emptying the role. READ COMMITTED gives each read
// the latest committed data, so the count taken after the lock reflects the
// change the lock was waited on.
var lastHolderTx = &sql.TxOptions{Isolation: sql.LevelReadCommitted}

// systemGrantRow is one deployment-scoped authority held by one user.
//
// Revocation sets revoked_at; nothing here deletes. The table answers "who
// could operate this deployment, and when", and a deleted row cannot answer it.
type systemGrantRow struct {
	ID       uint64 `gorm:"primaryKey;autoIncrement"`
	PublicID string `gorm:"column:public_id;type:char(20) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_system_grant_public_id;not null"`

	// The composite unique index is what keeps one user from holding two active
	// grants for the same role. LiveMarker is its third column: it holds a fixed
	// non-NULL value while the grant is active and NULL once it is revoked, and
	// MySQL treats NULLs as distinct in a unique index. So any number of revoked
	// rows may share a (user, role) while at most one live row can. RevokedAt
	// cannot play this part — two revocations at the same instant would carry the
	// same value and collide — which is why the marker, not the timestamp, is
	// what the index constrains.
	UserID     uint64 `gorm:"column:user_id;not null;uniqueIndex:idx_system_grant_live,priority:1;index:idx_system_grant_user"`
	Role       string `gorm:"type:varchar(32);not null;uniqueIndex:idx_system_grant_live,priority:2"`
	RevokedAt  *time.Time
	LiveMarker *uint8 `gorm:"column:live_marker;uniqueIndex:idx_system_grant_live,priority:3"`

	// GrantedBy stays an opaque handle. The operator who bootstraps the first
	// grant is a command line, not a user row, so this column cannot be a
	// reference to one.
	GrantedBy string    `gorm:"type:varchar(64);not null"`
	GrantedAt time.Time `gorm:"not null;index"`
}

func (systemGrantRow) TableName() string { return "system_grant" }

// liveMarker is the fixed value LiveMarker carries while a grant is active. Its
// only property that matters is being the same non-NULL value for every live
// row, so the unique index rejects a second one. A fresh pointer per row keeps
// GORM from sharing one address across inserts.
func liveMarker() *uint8 {
	v := uint8(1)
	return &v
}

// systemGrantReadRow is the row plus the handle its holder resolves to.
type systemGrantReadRow struct {
	Row          systemGrantRow `gorm:"embedded"`
	UserPublicID string         `gorm:"column:user_public_id"`
}

func (s *Store) systemGrantSelect(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Model(&systemGrantRow{}).
		Select("system_grant.*, u.public_id AS user_public_id").
		Joins("INNER JOIN `user` u ON u.id = system_grant.user_id")
}

func toSystemGrant(row *systemGrantReadRow) *coreidentity.SystemGrant {
	if row == nil {
		return nil
	}
	return &coreidentity.SystemGrant{
		ID:        row.Row.PublicID,
		UserID:    row.UserPublicID,
		Role:      row.Row.Role,
		GrantedBy: row.Row.GrantedBy,
		GrantedAt: row.Row.GrantedAt,
		RevokedAt: row.Row.RevokedAt,
	}
}

// ActiveSystemRoles returns the roles the user currently holds.
//
// This runs on every authenticated request to an admin route, so it selects
// one column against the user index and nothing else.
func (s *Store) ActiveSystemRoles(ctx context.Context, userID string) ([]string, error) {
	if userID == "" {
		return nil, nil
	}
	// One join rather than a resolve-then-query: this runs on every admin
	// request, and the extra round trip would be the cost of the boundary
	// rather than of the question.
	id, ok := util.CanonicalPublicID(userID)
	if !ok {
		return nil, nil
	}
	var roles []string
	if err := s.db.WithContext(ctx).
		Model(&systemGrantRow{}).
		Joins("INNER JOIN `user` u ON u.id = system_grant.user_id").
		Where("u.public_id = ? AND system_grant.revoked_at IS NULL", id).
		Pluck("role", &roles).Error; err != nil {
		return nil, err
	}
	return roles, nil
}

// ListSystemGrants returns grants newest first.
func (s *Store) ListSystemGrants(ctx context.Context, includeRevoked bool) ([]coreidentity.SystemGrant, error) {
	q := s.systemGrantSelect(ctx)
	if !includeRevoked {
		q = q.Where("system_grant.revoked_at IS NULL")
	}
	var rows []systemGrantReadRow
	if err := q.Order("system_grant.granted_at DESC, system_grant.id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]coreidentity.SystemGrant, 0, len(rows))
	for i := range rows {
		out = append(out, *toSystemGrant(&rows[i]))
	}
	return out, nil
}

// GrantSystemRole grants role to userID.
//
// The existence check and the insert are not in one transaction, and do not
// need to be: the unique index on (user_id, role, live_marker) is what actually
// enforces one active grant, so a lost race ends as a duplicate-key error on
// that index rather than as a second row. The check exists to turn the common
// case into a clear message instead of relying on the driver error, and the
// error translation below turns the race into the same message.
func (s *Store) GrantSystemRole(ctx context.Context, userID, role, grantedBy string, now time.Time) (*coreidentity.SystemGrant, error) {
	if !coreidentity.ValidSystemRole(role) {
		return nil, coreidentity.ErrSystemRoleUnknown
	}
	userKey, err := lookupKey(ctx, s.db, "user", userID)
	if err != nil {
		return nil, err
	}
	var existing systemGrantRow
	err = s.db.WithContext(ctx).
		Where("user_id = ? AND role = ? AND revoked_at IS NULL", userKey, role).
		First(&existing).Error
	if err == nil {
		return nil, coreidentity.ErrSystemGrantExists
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	row := systemGrantRow{
		UserID:     userKey,
		Role:       role,
		GrantedBy:  grantedBy,
		GrantedAt:  now,
		LiveMarker: liveMarker(),
	}
	err = createWithPublicID(ctx, s.db, "uq_system_grant_public_id",
		func(id string) { row.PublicID = id }, &row)
	if isDuplicateOnIndex(err, "idx_system_grant_live") {
		return nil, coreidentity.ErrSystemGrantExists
	}
	if err != nil {
		return nil, err
	}
	return toSystemGrant(&systemGrantReadRow{Row: row, UserPublicID: canonicalPublicID(userID)}), nil
}

// RevokeSystemRole revokes the active grant, reporting whether one was found.
//
// When keepLastHolder is set, the check and the revoke run in one transaction
// that first locks every live grant for the role FOR UPDATE. That lock is what
// makes the last-holder rule safe under concurrency: two revokes of different
// holders would otherwise each read the other's grant as still live and both
// proceed, leaving the role empty. Serialized behind the lock, the second one
// sees the first's revocation and refuses. The operator shell passes false: it
// is the way back from an empty role, so it must be able to empty it.
func (s *Store) RevokeSystemRole(ctx context.Context, userID, role string, now time.Time, keepLastHolder bool) (bool, error) {
	userKey, err := lookupKey(ctx, s.db, "user", userID)
	if errors.Is(err, apierr.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var revoked bool
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if keepLastHolder {
			// Lock the role's live rows so a concurrent revoke serializes behind
			// this one. Only grant rows are locked, never the joined user rows,
			// so contention stays on the authority being changed.
			var lockedIDs []uint64
			if err := tx.Model(&systemGrantRow{}).
				Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("role = ? AND revoked_at IS NULL", role).
				Pluck("id", &lockedIDs).Error; err != nil {
				return err
			}
		}
		res := tx.Model(&systemGrantRow{}).
			Where("user_id = ? AND role = ? AND revoked_at IS NULL", userKey, role).
			Updates(map[string]any{"revoked_at": now, "live_marker": nil})
		if res.Error != nil {
			return res.Error
		}
		revoked = res.RowsAffected > 0
		if !keepLastHolder || !revoked {
			return nil
		}
		remaining, err := countEffectiveSystemGrants(ctx, tx, role)
		if err != nil {
			return err
		}
		if remaining == 0 {
			// Roll back the revoke: removing this grant would leave the role
			// with no effective holder.
			return coreidentity.ErrSystemGrantLastHolder
		}
		return nil
	}, lastHolderTx)
	if errors.Is(err, coreidentity.ErrSystemGrantLastHolder) {
		return false, err
	}
	if err != nil {
		return false, err
	}
	return revoked, nil
}

// CountActiveSystemGrants counts the effective holders of role.
func (s *Store) CountActiveSystemGrants(ctx context.Context, role string) (int, error) {
	return countEffectiveSystemGrants(ctx, s.db.WithContext(ctx), role)
}

// ensureNotLastSystemHolder refuses, with ErrSystemGrantLastHolder, to remove
// the account from the effective holders of a system role it holds when that
// would leave the role with none. It is called inside the disable transaction,
// before the account is disabled.
//
// For each role the account holds it locks that role's live grants FOR UPDATE,
// the same rows RevokeSystemRole locks, so a disable and a concurrent revoke
// serialize on one lock and cannot each read the other's holder as still
// effective and both proceed.
func ensureNotLastSystemHolder(ctx context.Context, tx *gorm.DB, userPublicID string) error {
	userKey, err := lookupKey(ctx, tx, "user", userPublicID)
	if errors.Is(err, apierr.ErrNotFound) {
		// No such account; the caller's update reports it.
		return nil
	}
	if err != nil {
		return err
	}
	var roles []string
	if err := tx.WithContext(ctx).Model(&systemGrantRow{}).
		Where("user_id = ? AND revoked_at IS NULL", userKey).
		Pluck("role", &roles).Error; err != nil {
		return err
	}
	for _, role := range roles {
		var lockedIDs []uint64
		if err := tx.WithContext(ctx).Model(&systemGrantRow{}).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("role = ? AND revoked_at IS NULL", role).
			Pluck("id", &lockedIDs).Error; err != nil {
			return err
		}
		var others int64
		if err := tx.WithContext(ctx).Model(&systemGrantRow{}).
			Joins("INNER JOIN `user` u ON u.id = system_grant.user_id").
			Where("system_grant.role = ? AND system_grant.revoked_at IS NULL AND u.disabled_at IS NULL AND system_grant.user_id <> ?", role, userKey).
			Count(&others).Error; err != nil {
			return err
		}
		if others == 0 {
			return coreidentity.ErrSystemGrantLastHolder
		}
	}
	return nil
}

// countEffectiveSystemGrants counts live grants for role held by accounts that
// are not disabled. A disabled account is refused before its grant is consulted,
// so it cannot be the holder that keeps the deployment reachable — counting it
// would let the last usable administrator be revoked.
func countEffectiveSystemGrants(ctx context.Context, tx *gorm.DB, role string) (int, error) {
	var n int64
	if err := tx.WithContext(ctx).
		Model(&systemGrantRow{}).
		Joins("INNER JOIN `user` u ON u.id = system_grant.user_id").
		Where("system_grant.role = ? AND system_grant.revoked_at IS NULL AND u.disabled_at IS NULL", role).
		Count(&n).Error; err != nil {
		return 0, err
	}
	return int(n), nil
}
