package db

import (
	"context"
	"errors"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreremote "github.com/icloudbb/buildmax/internal/core/remotesession"

	"gorm.io/gorm"
)

// remoteSessionRow is one live, device-resident Agent session made reachable
// through the server. It is account-scoped: UserID is the only owner, and there
// is no Space key by design (see docs/design/remote-control.md).
type remoteSessionRow struct {
	ID          uint64 `gorm:"primaryKey;autoIncrement"`
	PublicID    string `gorm:"column:public_id;type:char(20) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_remote_session_public_id;not null"`
	UserID      uint64 `gorm:"column:user_id;not null;index"`
	DisplayName string `gorm:"column:display_name;type:varchar(200)"`
	Platform    string `gorm:"type:varchar(32)"`
	Host        string `gorm:"type:varchar(200)"`
	Status      string `gorm:"type:varchar(16);not null;index"`
	// LastSeenAt is indexed because the reaper scans online sessions by it.
	LastSeenAt *time.Time `gorm:"column:last_seen_at;index"`
	CreatedAt  time.Time  `gorm:"autoCreateTime"`
}

func (remoteSessionRow) TableName() string { return "remote_session" }

// remoteSessionReadRow is the row plus the handle its owner resolves to, so a
// returned session names its user by public id rather than by row key.
type remoteSessionReadRow struct {
	Row          remoteSessionRow `gorm:"embedded"`
	UserPublicID string           `gorm:"column:user_public_id"`
}

func remoteSessionSelect(tx *gorm.DB) *gorm.DB {
	return tx.Model(&remoteSessionRow{}).
		Select("remote_session.*, u.public_id AS user_public_id").
		Joins("INNER JOIN `user` u ON u.id = remote_session.user_id")
}

func (r remoteSessionReadRow) toRemoteSession() coreremote.RemoteSession {
	s := coreremote.RemoteSession{
		ID:          r.Row.PublicID,
		UserID:      r.UserPublicID,
		DisplayName: r.Row.DisplayName,
		Platform:    r.Row.Platform,
		Host:        r.Row.Host,
		Status:      coreremote.Status(r.Row.Status),
		CreatedAt:   r.Row.CreatedAt,
	}
	if r.Row.LastSeenAt != nil {
		s.LastSeenAt = *r.Row.LastSeenAt
	}
	return s
}

// RegisterRemoteSession implements coreremote.Store.
func (s *Store) RegisterRemoteSession(ctx context.Context, in coreremote.NewRemoteSession) (string, error) {
	if in.UserID == "" {
		return "", errors.New("remote session: user id required")
	}
	userKey, err := lookupKey(ctx, s.db, "user", in.UserID)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	row := remoteSessionRow{
		UserID:      userKey,
		DisplayName: in.DisplayName,
		Platform:    in.Platform,
		Host:        in.Host,
		Status:      string(coreremote.StatusOnline),
		LastSeenAt:  &now,
	}
	if err := createWithPublicID(ctx, s.db.WithContext(ctx), "uq_remote_session_public_id",
		func(id string) { row.PublicID = id }, &row); err != nil {
		return "", err
	}
	return row.PublicID, nil
}

// TouchRemoteSession implements coreremote.Store. It stamps last-seen and keeps
// the session online; a missing session is a no-op. Heartbeats arrive at a
// controlled interval (not per request), so the write is unconditional.
func (s *Store) TouchRemoteSession(ctx context.Context, id string, now time.Time) error {
	if id == "" {
		return nil
	}
	return s.db.WithContext(ctx).Model(&remoteSessionRow{}).
		Where("public_id = ?", id).
		Updates(map[string]any{"last_seen_at": now, "status": string(coreremote.StatusOnline)}).Error
}

// MarkRemoteSessionOffline implements coreremote.Store.
func (s *Store) MarkRemoteSessionOffline(ctx context.Context, id string, now time.Time) error {
	if id == "" {
		return nil
	}
	return s.db.WithContext(ctx).Model(&remoteSessionRow{}).
		Where("public_id = ? AND status = ?", id, string(coreremote.StatusOnline)).
		Updates(map[string]any{"status": string(coreremote.StatusOffline), "last_seen_at": now}).Error
}

// MarkStaleRemoteSessionsOffline implements coreremote.Store. It retires online
// sessions whose last-seen lapsed past the cutoff — the backstop for an unclean
// drop.
func (s *Store) MarkStaleRemoteSessionsOffline(ctx context.Context, cutoff time.Time) (int64, error) {
	res := s.db.WithContext(ctx).Model(&remoteSessionRow{}).
		Where("status = ? AND last_seen_at IS NOT NULL AND last_seen_at <= ?", string(coreremote.StatusOnline), cutoff).
		Update("status", string(coreremote.StatusOffline))
	return res.RowsAffected, res.Error
}

// GetRemoteSession implements coreremote.Store.
func (s *Store) GetRemoteSession(ctx context.Context, id string) (coreremote.RemoteSession, error) {
	if id == "" {
		return coreremote.RemoteSession{}, coreremote.ErrNotFound
	}
	var row remoteSessionReadRow
	err := remoteSessionSelect(s.db.WithContext(ctx)).
		Where("remote_session.public_id = ?", id).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return coreremote.RemoteSession{}, coreremote.ErrNotFound
		}
		return coreremote.RemoteSession{}, err
	}
	return row.toRemoteSession(), nil
}

// ListRemoteSessionsByUser implements coreremote.Store.
func (s *Store) ListRemoteSessionsByUser(ctx context.Context, userID string) ([]coreremote.RemoteSession, error) {
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
	var rows []remoteSessionReadRow
	if err := remoteSessionSelect(s.db.WithContext(ctx)).
		Where("remote_session.user_id = ?", userKey).
		Order("remote_session.created_at DESC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]coreremote.RemoteSession, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toRemoteSession())
	}
	return out, nil
}
