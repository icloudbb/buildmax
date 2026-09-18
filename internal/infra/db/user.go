package db

import (
	"context"
	"errors"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	corespace "github.com/icloudbb/buildmax/internal/core/space"

	"github.com/icloudbb/buildmax/internal/util"
	"gorm.io/gorm"
)

type userRow struct {
	ID       uint64 `gorm:"primaryKey;autoIncrement"`
	PublicID string `gorm:"column:public_id;type:char(20) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_user_public_id;not null"`
	Email    string `gorm:"type:varchar(255);uniqueIndex;not null"`
	Name     string `gorm:"type:varchar(255)"`
	// PasswordHash is NULL for an account that has never set one: created by an
	// operator and not yet claimed, or signing in with login codes only. It
	// stays nullable so that an account authenticated somewhere else — an
	// identity provider, when there is one — needs no local password to exist.
	PasswordHash      *string    `gorm:"type:varchar(255)"`
	PasswordSetAt     *time.Time `gorm:""`
	QuotaTier         string     `gorm:"type:varchar(64)"`
	LastLoginAt       *time.Time `gorm:""`
	LastLoginPlatform *string    `gorm:"type:varchar(32)"`
	// DisabledAt is NULL for an ordinary account. Non-NULL is checked on every
	// authenticated request, which is why it lives on this row rather than in a
	// side table: the check has to be one primary-key read.
	DisabledAt *time.Time `gorm:""`
	CreatedAt  time.Time  `gorm:"autoCreateTime"`
}

func (userRow) TableName() string { return "user" }

func toUser(row *userRow) *coreidentity.User {
	if row == nil {
		return nil
	}
	return &coreidentity.User{
		ID:                row.PublicID,
		Email:             row.Email,
		Name:              row.Name,
		QuotaTier:         row.QuotaTier,
		LastLoginAt:       row.LastLoginAt,
		LastLoginPlatform: row.LastLoginPlatform,
		DisabledAt:        row.DisabledAt,
		CreatedAt:         row.CreatedAt,
		// Whether a password exists, never what it is.
		HasPassword: row.PasswordHash != nil && *row.PasswordHash != "",
	}
}

func toUserRow(m *coreidentity.User) *userRow {
	if m == nil {
		return nil
	}
	return &userRow{
		Email:             m.Email,
		Name:              m.Name,
		QuotaTier:         m.QuotaTier,
		LastLoginAt:       m.LastLoginAt,
		LastLoginPlatform: m.LastLoginPlatform,
		DisabledAt:        m.DisabledAt,
		CreatedAt:         m.CreatedAt,
	}
}

// ListUsers returns accounts newest first with the total count.
//
// The query filters on email as a substring. It is not an index scan and is not
// meant to be: an operator searching for a colleague on a deployment with a few
// thousand accounts is not a hot path, and a prefix-only match would fail the
// common case of searching by the part before the @.
func (s *Store) ListUsers(ctx context.Context, filter coreidentity.UserFilter, limit, offset int) ([]coreidentity.User, int, error) {
	limit, offset = clampPage(limit, offset)
	q := s.db.WithContext(ctx).Model(&userRow{})
	if filter.Query != "" {
		q = q.Where("email LIKE ?", "%"+filter.Query+"%")
	}
	if filter.Disabled != nil {
		if *filter.Disabled {
			q = q.Where("disabled_at IS NOT NULL")
		} else {
			q = q.Where("disabled_at IS NULL")
		}
	}
	if filter.HasPassword != nil {
		if *filter.HasPassword {
			q = q.Where("password_hash IS NOT NULL")
		} else {
			q = q.Where("password_hash IS NULL")
		}
	}
	if filter.Platform != "" {
		q = q.Where("last_login_platform = ?", filter.Platform)
	}
	if filter.LastLoginAfter != nil {
		// A NULL last_login_at (never signed in) fails the comparison, so a
		// time bound also excludes accounts that never logged in.
		q = q.Where("last_login_at >= ?", *filter.LastLoginAfter)
	}
	if filter.LastLoginBefore != nil {
		q = q.Where("last_login_at < ?", *filter.LastLoginBefore)
	}
	if filter.SystemRole != "" {
		// A subquery, not a join: a join would return one user row per grant and
		// double-count anyone re-granted. The set of grant holders is small, so
		// the IN list stays cheap.
		holders := s.db.WithContext(ctx).Model(&systemGrantRow{}).
			Select("user_id").
			Where("role = ? AND revoked_at IS NULL", filter.SystemRole)
		q = q.Where("id IN (?)", holders)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []userRow
	if err := q.Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]coreidentity.User, 0, len(rows))
	for i := range rows {
		out = append(out, *toUser(&rows[i]))
	}
	return out, int(total), nil
}

// SetUserDisabled disables or enables an account.
//
// Enabling reverses the state and nothing else: sessions revoked by the disable
// stay revoked, and runs it stopped stay stopped. Undo is not a goal.
//
// Disabling refuses when the account is the last effective holder of a system
// role. The check and the update run in one transaction that locks the role's
// live grants, so it serializes with a concurrent revoke — the two together
// cannot leave the role with nobody.
func (s *Store) SetUserDisabled(ctx context.Context, userID string, disabledAt *time.Time) error {
	id, ok := util.CanonicalPublicID(userID)
	if !ok {
		return coreidentity.ErrUserNotFound
	}
	// lastHolderTx (READ COMMITTED) so ensureNotLastSystemHolder counts the
	// committed effective holders after its FOR UPDATE lock is granted, not the
	// snapshot from before a racing revoke committed.
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if disabledAt != nil {
			if err := ensureNotLastSystemHolder(ctx, tx, id); err != nil {
				return err
			}
		}
		res := tx.Model(&userRow{}).
			Where("public_id = ?", id).
			Update("disabled_at", disabledAt)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			// Distinguishing "no such account" from "already in that state" needs
			// a read, because an update to the value a row already holds affects
			// no rows under MySQL's default client flags.
			var count int64
			if err := tx.Model(&userRow{}).Where("public_id = ?", id).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				return coreidentity.ErrUserNotFound
			}
		}
		return nil
	}, lastHolderTx)
}

// UserByEmail returns the user with the given email, or (nil, nil) when not found.
func (s *Store) UserByEmail(ctx context.Context, email string) (*coreidentity.User, error) {
	var u userRow
	err := s.db.WithContext(ctx).Where("email = ?", email).First(&u).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toUser(&u), nil
}

// GetUser returns the user by user_id, or (nil, nil) when not found.
func (s *Store) GetUser(ctx context.Context, userID string) (*coreidentity.User, error) {
	id, ok := util.CanonicalPublicID(userID)
	if !ok {
		return nil, nil
	}
	var u userRow
	err := s.db.WithContext(ctx).Where("public_id = ?", id).First(&u).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toUser(&u), nil
}

// UpdateLoginMeta records the last login timestamp and platform for the user.
func (s *Store) UpdateLoginMeta(ctx context.Context, userID string, loginAt time.Time, platform string) error {
	id, ok := util.CanonicalPublicID(userID)
	if !ok {
		return coreidentity.ErrUserNotFound
	}
	return s.db.WithContext(ctx).
		Model(&userRow{}).
		Where("public_id = ?", id).
		Updates(map[string]interface{}{
			"last_login_at":       loginAt,
			"last_login_platform": platform,
		}).Error
}

// CreateUser creates a user with the given email. Name is set to empty.
// When defaultQuotaTier is non-empty, User.QuotaTier is set to it.
// Returns ErrEmailExists if the email is already registered.
func (s *Store) CreateUser(ctx context.Context, email string, defaultQuotaTier string) (*coreidentity.User, error) {
	existing, err := s.UserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, coreidentity.ErrEmailExists
	}
	u := coreidentity.User{
		Email:     email,
		Name:      "",
		CreatedAt: time.Now().UTC(),
	}
	if defaultQuotaTier != "" {
		u.QuotaTier = defaultQuotaTier
	}
	userDB := toUserRow(&u)
	personalSpaceDB := &spaceRow{
		Name:      corespace.DefaultPersonalName,
		QuotaTier: defaultQuotaTier,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.CreatedAt,
	}
	// The personal space and its membership are written inside the same
	// transaction because they reference the user by the key the insert
	// assigns: an account with no space of its own is not a state any caller
	// can be handed.
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := createWithPublicID(ctx, tx, "uq_user_public_id",
			func(id string) { userDB.PublicID = id }, userDB); err != nil {
			return err
		}
		personalSpaceDB.PersonalForUserID = &userDB.ID
		personalSpaceDB.CreatedBy = userDB.ID
		if err := createWithPublicID(ctx, tx, "uq_space_public_id",
			func(id string) { personalSpaceDB.PublicID = id }, personalSpaceDB); err != nil {
			return err
		}
		return tx.Create(&spaceMemberRow{
			SpaceID:   personalSpaceDB.ID,
			UserID:    userDB.ID,
			Role:      corespace.RoleOwner,
			CreatedAt: u.CreatedAt,
		}).Error
	}); err != nil {
		return nil, err
	}
	u.ID = userDB.PublicID
	return &u, nil
}

// PasswordHash implements coreidentity.PasswordStore.
//
// A missing account and an account with no password both return "", because
// the caller does the same thing with either: refuse, having spent the same
// work. Distinguishing them here would only invite a handler to distinguish
// them in a response.
func (s *Store) PasswordHash(ctx context.Context, userID string) (string, error) {
	if userID == "" {
		return "", nil
	}
	id, ok := util.CanonicalPublicID(userID)
	if !ok {
		return "", nil
	}
	var row userRow
	err := s.db.WithContext(ctx).Select("password_hash").Where("public_id = ?", id).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	if row.PasswordHash == nil {
		return "", nil
	}
	return *row.PasswordHash, nil
}

// SetPassword implements coreidentity.PasswordStore.
func (s *Store) SetPassword(ctx context.Context, userID, encodedHash string, setAt time.Time) error {
	if userID == "" {
		return errors.New("set password: user id required")
	}
	if encodedHash == "" {
		return errors.New("set password: hash required")
	}
	id, ok := util.CanonicalPublicID(userID)
	if !ok {
		return coreidentity.ErrUserNotFound
	}
	res := s.db.WithContext(ctx).Model(&userRow{}).
		Where("public_id = ?", id).
		Updates(map[string]any{"password_hash": encodedHash, "password_set_at": setAt})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return coreidentity.ErrUserNotFound
	}
	return nil
}
