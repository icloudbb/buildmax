package db

import (
	"context"
	"errors"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"

	"gorm.io/gorm"
)

// authSessionRow is the durable authority for one login. The refresh tokens in
// user_refresh_token reference it by SessionID == this row's PublicID; revoking
// this row cascades to them.
type authSessionRow struct {
	ID       uint64 `gorm:"primaryKey;autoIncrement"`
	PublicID string `gorm:"column:public_id;type:char(20) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_auth_session_public_id;not null"`
	UserID   uint64 `gorm:"column:user_id;not null;index"`
	// Platform and AuthMethod are the surface and proof that opened the session.
	Platform   string `gorm:"type:varchar(32)"`
	AuthMethod string `gorm:"column:auth_method;type:varchar(32)"`
	// AbsoluteExpiresAt is the hard ceiling; LastSeenAt is best-effort activity.
	AbsoluteExpiresAt time.Time  `gorm:"column:absolute_expires_at;not null;index"`
	LastSeenAt        *time.Time `gorm:"column:last_seen_at"`
	RevokedAt         *time.Time `gorm:"column:revoked_at"`
	CreatedAt         time.Time  `gorm:"autoCreateTime"`
}

func (authSessionRow) TableName() string { return "auth_session" }

// sessionTouchThrottle is how stale LastSeenAt may be before a request updates
// it. Without a throttle an active session would mean a write on every request;
// with one, "last seen" is accurate to about this granularity, which is all an
// operator reading a session list needs.
const sessionTouchThrottle = time.Minute

// authSessionReadRow is the row plus the handle its owner resolves to, so the
// returned session names a user by public id rather than by row key.
type authSessionReadRow struct {
	Row          authSessionRow `gorm:"embedded"`
	UserPublicID string         `gorm:"column:user_public_id"`
}

func authSessionSelect(tx *gorm.DB) *gorm.DB {
	return tx.Model(&authSessionRow{}).
		Select("auth_session.*, u.public_id AS user_public_id").
		Joins("INNER JOIN `user` u ON u.id = auth_session.user_id")
}

func (r authSessionReadRow) toSession() coreidentity.AuthSession {
	s := coreidentity.AuthSession{
		SID:               r.Row.PublicID,
		UserID:            r.UserPublicID,
		Platform:          r.Row.Platform,
		AuthMethod:        r.Row.AuthMethod,
		CreatedAt:         r.Row.CreatedAt,
		AbsoluteExpiresAt: r.Row.AbsoluteExpiresAt,
	}
	if r.Row.LastSeenAt != nil {
		s.LastSeenAt = *r.Row.LastSeenAt
	}
	return s
}

// CreateSession implements coreidentity.AuthSessionStore.
func (s *Store) CreateSession(ctx context.Context, in coreidentity.NewAuthSession) (string, error) {
	if in.UserID == "" {
		return "", errors.New("auth session: user id required")
	}
	userKey, err := lookupKey(ctx, s.db, "user", in.UserID)
	if err != nil {
		return "", err
	}
	row := authSessionRow{
		UserID:            userKey,
		Platform:          in.Platform,
		AuthMethod:        in.AuthMethod,
		AbsoluteExpiresAt: in.AbsoluteExpiresAt,
	}
	if err := createWithPublicID(ctx, s.db.WithContext(ctx), "uq_auth_session_public_id",
		func(id string) { row.PublicID = id }, &row); err != nil {
		return "", err
	}
	return row.PublicID, nil
}

// ActiveSession implements coreidentity.AuthSessionStore.
func (s *Store) ActiveSession(ctx context.Context, sid string, now time.Time) (coreidentity.AuthSession, error) {
	if sid == "" {
		return coreidentity.AuthSession{}, coreidentity.ErrSessionInactive
	}
	var row authSessionReadRow
	err := authSessionSelect(s.db.WithContext(ctx)).
		Where("auth_session.public_id = ? AND auth_session.revoked_at IS NULL AND auth_session.absolute_expires_at > ?", sid, now).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return coreidentity.AuthSession{}, coreidentity.ErrSessionInactive
		}
		return coreidentity.AuthSession{}, err
	}
	return row.toSession(), nil
}

// TouchSession implements coreidentity.AuthSessionStore.
//
// The write is conditional so an active session does not mean a write per
// request: it updates only when last_seen_at is unset or older than the
// throttle. A row that no longer matches (missing, revoked, expired) is a no-op.
func (s *Store) TouchSession(ctx context.Context, sid string, now time.Time) error {
	if sid == "" {
		return nil
	}
	cutoff := now.Add(-sessionTouchThrottle)
	return s.db.WithContext(ctx).Model(&authSessionRow{}).
		Where("public_id = ? AND revoked_at IS NULL AND absolute_expires_at > ?", sid, now).
		Where("last_seen_at IS NULL OR last_seen_at < ?", cutoff).
		Update("last_seen_at", now).Error
}

// RevokeSession implements coreidentity.AuthSessionStore.
func (s *Store) RevokeSession(ctx context.Context, sid string, now time.Time) (int64, error) {
	if sid == "" {
		return 0, nil
	}
	var revoked int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&authSessionRow{}).
			Where("public_id = ? AND revoked_at IS NULL", sid).
			Update("revoked_at", now)
		if res.Error != nil {
			return res.Error
		}
		revoked = res.RowsAffected
		return revokeSessionTx(tx, sid, now)
	})
	return revoked, err
}

// RevokeUserSessions implements coreidentity.AuthSessionStore.
func (s *Store) RevokeUserSessions(ctx context.Context, userID string, now time.Time) (int64, error) {
	if userID == "" {
		return 0, nil
	}
	userKey, err := lookupKey(ctx, s.db, "user", userID)
	if errors.Is(err, apierr.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var revoked int64
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&authSessionRow{}).
			Where("user_id = ? AND revoked_at IS NULL", userKey).
			Update("revoked_at", now)
		if res.Error != nil {
			return res.Error
		}
		revoked = res.RowsAffected
		// Cascade to the refresh tokens by the same user key so a chain cannot
		// keep rotating after its session is gone.
		return tx.Model(&userRefreshTokenRow{}).
			Where("user_id = ? AND revoked_at IS NULL", userKey).
			Update("revoked_at", now).Error
	})
	return revoked, err
}

// CountUserSessions implements coreidentity.AuthSessionStore.
func (s *Store) CountUserSessions(ctx context.Context, userID string, now time.Time) (int, error) {
	if userID == "" {
		return 0, nil
	}
	userKey, err := lookupKey(ctx, s.db, "user", userID)
	if errors.Is(err, apierr.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var n int64
	if err := s.db.WithContext(ctx).Model(&authSessionRow{}).
		Where("user_id = ? AND revoked_at IS NULL AND absolute_expires_at > ?", userKey, now).
		Count(&n).Error; err != nil {
		return 0, err
	}
	return int(n), nil
}

// ListUserSessions implements coreidentity.AuthSessionStore.
func (s *Store) ListUserSessions(ctx context.Context, userID string, now time.Time) ([]coreidentity.AuthSession, error) {
	if userID == "" {
		return nil, nil
	}
	userKey, err := lookupKey(ctx, s.db, "user", userID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rows []authSessionReadRow
	if err := authSessionSelect(s.db.WithContext(ctx)).
		Where("auth_session.user_id = ? AND auth_session.revoked_at IS NULL AND auth_session.absolute_expires_at > ?", userKey, now).
		Order("auth_session.created_at DESC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]coreidentity.AuthSession, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toSession())
	}
	return out, nil
}
