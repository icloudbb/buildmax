package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
)

// nativeSignInRow is one CLI or Desktop sign-in request waiting for, or
// carrying, a Portal decision. It has no public id: nothing addresses a request
// except its user code or its poll secret, and only their SHA-256 hashes are
// stored, each under a unique index, so a database read recovers neither. The
// row holds no token, email, or address. Redemption deletes it; the credential
// cleaner deletes the rest after expiry.
type nativeSignInRow struct {
	ID             uint64 `gorm:"primaryKey;autoIncrement"`
	UserCodeHash   string `gorm:"column:user_code_hash;type:char(64) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_native_sign_in_user_code_hash;not null"`
	PollSecretHash string `gorm:"column:poll_secret_hash;type:char(64) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_native_sign_in_poll_secret_hash;not null"`
	Platform       string `gorm:"column:platform;type:varchar(32) CHARACTER SET ascii COLLATE ascii_bin;not null"`
	ClientLabel    string `gorm:"column:client_label;type:varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;not null;default:''"`
	// State leads the index the start cap counts with.
	State           string     `gorm:"column:state;type:varchar(16) CHARACTER SET ascii COLLATE ascii_bin;not null;index:idx_native_sign_in_state_expires,priority:1"`
	IntervalSeconds int        `gorm:"column:interval_seconds;not null"`
	LastPolledAt    *time.Time `gorm:"column:last_polled_at"`
	// DecidedByUserID and DecidedSessionID are the confirming account and its
	// Portal session, whose method and absolute expiry the native session
	// inherits.
	DecidedByUserID  *uint64    `gorm:"column:decided_by_user_id"`
	DecidedSessionID *uint64    `gorm:"column:decided_session_id"`
	DecidedAt        *time.Time `gorm:"column:decided_at"`
	ExpiresAt        time.Time  `gorm:"column:expires_at;not null;index;index:idx_native_sign_in_state_expires,priority:2"`
	CreatedAt        time.Time  `gorm:"column:created_at;not null"`
}

func (nativeSignInRow) TableName() string { return "native_sign_in" }

const uqNativeSignInUserCode = "uq_native_sign_in_user_code_hash"

// hashNativeSignInValue is the only form of a user code or poll secret that
// reaches the database. Lookups compare these digests through the unique
// index; no plaintext is ever compared.
func hashNativeSignInValue(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}

func (r nativeSignInRow) toNativeSignIn() coreidentity.NativeSignIn {
	out := coreidentity.NativeSignIn{
		Platform:    r.Platform,
		ClientLabel: r.ClientLabel,
		State:       coreidentity.NativeSignInState(r.State),
		Interval:    time.Duration(r.IntervalSeconds) * time.Second,
		ExpiresAt:   r.ExpiresAt,
		CreatedAt:   r.CreatedAt,
	}
	if r.LastPolledAt != nil {
		out.LastPolledAt = *r.LastPolledAt
	}
	if r.DecidedAt != nil {
		out.DecidedAt = *r.DecidedAt
	}
	return out
}

// CountPendingNativeSignIns implements coreidentity.NativeSignInStore.
func (s *Store) CountPendingNativeSignIns(ctx context.Context, now time.Time) (int, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&nativeSignInRow{}).
		Where("state = ? AND expires_at > ?", string(coreidentity.NativeSignInPending), now.UTC()).
		Count(&n).Error
	return int(n), err
}

// CreateNativeSignIn implements coreidentity.NativeSignInStore.
func (s *Store) CreateNativeSignIn(ctx context.Context, in coreidentity.NewNativeSignIn) error {
	if in.UserCode == "" || in.PollSecret == "" {
		return errors.New("native sign-in: user code and poll secret required")
	}
	row := nativeSignInRow{
		UserCodeHash:    hashNativeSignInValue(in.UserCode),
		PollSecretHash:  hashNativeSignInValue(in.PollSecret),
		Platform:        in.Platform,
		ClientLabel:     in.ClientLabel,
		State:           string(coreidentity.NativeSignInPending),
		IntervalSeconds: int(coreidentity.NativeSignInInterval / time.Second),
		ExpiresAt:       in.ExpiresAt.UTC(),
		CreatedAt:       in.CreatedAt.UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if isDuplicateOnIndex(err, uqNativeSignInUserCode) {
			return coreidentity.ErrUserCodeTaken
		}
		return err
	}
	return nil
}

// PendingNativeSignIn implements coreidentity.NativeSignInStore.
func (s *Store) PendingNativeSignIn(ctx context.Context, userCode string, now time.Time) (coreidentity.NativeSignIn, error) {
	if userCode == "" {
		return coreidentity.NativeSignIn{}, coreidentity.ErrNativeSignInNotFound
	}
	var row nativeSignInRow
	err := s.db.WithContext(ctx).
		Where("user_code_hash = ? AND state = ? AND expires_at > ?",
			hashNativeSignInValue(userCode), string(coreidentity.NativeSignInPending), now.UTC()).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return coreidentity.NativeSignIn{}, coreidentity.ErrNativeSignInNotFound
	}
	if err != nil {
		return coreidentity.NativeSignIn{}, err
	}
	return row.toNativeSignIn(), nil
}

// DecideNativeSignIn implements coreidentity.NativeSignInStore.
//
// The decision is one conditional UPDATE on a pending, unexpired row, so of two
// decisions racing on one code exactly one matches and the other finds nothing.
func (s *Store) DecideNativeSignIn(ctx context.Context, userCode string, d coreidentity.NativeSignInDecision) error {
	if userCode == "" {
		return coreidentity.ErrNativeSignInNotFound
	}
	if d.State != coreidentity.NativeSignInConfirmed && d.State != coreidentity.NativeSignInDenied {
		return errors.New("native sign-in: a decision confirms or denies")
	}
	db := s.db.WithContext(ctx)
	userKey, err := lookupKey(ctx, db, "user", d.UserID)
	if err != nil {
		return err
	}
	sessionKey, err := lookupKey(ctx, db, "auth_session", d.SessionID)
	if err != nil {
		return err
	}
	at := d.At.UTC()
	res := db.Model(&nativeSignInRow{}).
		Where("user_code_hash = ? AND state = ? AND expires_at > ?",
			hashNativeSignInValue(userCode), string(coreidentity.NativeSignInPending), at).
		Updates(map[string]any{
			"state":              string(d.State),
			"decided_by_user_id": userKey,
			"decided_session_id": sessionKey,
			"decided_at":         at,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return coreidentity.ErrNativeSignInNotFound
	}
	return nil
}

// lockLiveNativeSignIn reads the unexpired request a poll secret names under a
// row lock, so polls and redemptions of one request run one at a time.
func lockLiveNativeSignIn(tx *gorm.DB, pollSecret string, now time.Time) (nativeSignInRow, error) {
	var row nativeSignInRow
	if pollSecret == "" {
		return row, coreidentity.ErrNativeSignInNotFound
	}
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("poll_secret_hash = ? AND expires_at > ?", hashNativeSignInValue(pollSecret), now.UTC()).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return row, coreidentity.ErrNativeSignInNotFound
	}
	return row, err
}

// PollNativeSignIn implements coreidentity.NativeSignInStore.
func (s *Store) PollNativeSignIn(ctx context.Context, pollSecret string, now time.Time) (coreidentity.NativeSignIn, bool, error) {
	var (
		out      coreidentity.NativeSignIn
		slowDown bool
	)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := lockLiveNativeSignIn(tx, pollSecret, now)
		if err != nil {
			return err
		}
		out = row.toNativeSignIn()
		slowDown = out.Poll(now.UTC())
		return tx.Model(&nativeSignInRow{}).Where("id = ?", row.ID).Updates(map[string]any{
			"interval_seconds": int(out.Interval / time.Second),
			"last_polled_at":   out.LastPolledAt,
		}).Error
	})
	return out, slowDown, err
}

// RedeemNativeSignIn implements coreidentity.NativeSignInStore.
//
// One transaction: the request row is locked first, so a concurrent redemption
// waits here and then finds the row deleted. The confirming session is read
// under a lock too, so a revocation racing this either lands before (and the
// request is refused) or after (and the native session, which is independent of
// the session that confirmed it, stands). A refusal is committed as a denial
// rather than rolled back, so the request answers the same way until it expires.
func (s *Store) RedeemNativeSignIn(ctx context.Context, pollSecret string, now time.Time, refreshTTL time.Duration) (coreidentity.NativeSignInRedemption, error) {
	now = now.UTC()
	var (
		out     coreidentity.NativeSignInRedemption
		refused bool
	)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := lockLiveNativeSignIn(tx, pollSecret, now)
		if err != nil {
			return err
		}
		req := row.toNativeSignIn()
		if req.State != coreidentity.NativeSignInConfirmed || row.DecidedByUserID == nil || row.DecidedSessionID == nil {
			return coreidentity.ErrNativeSignInNotFound
		}
		userKey := *row.DecidedByUserID
		user, err := nativeSignInDecider(tx, userKey)
		if err != nil {
			return err
		}
		confirming, err := nativeSignInConfirmingSession(tx, *row.DecidedSessionID, userKey, user, now)
		if err != nil {
			return err
		}
		if user != nil {
			req.DecidedByUserID = user.ID
		}
		if confirming != nil {
			req.DecidedSessionID = confirming.SID
		}
		if !req.RedeemableBy(user, confirming, now) {
			refused = true
			return tx.Model(&nativeSignInRow{}).Where("id = ?", row.ID).
				Update("state", string(coreidentity.NativeSignInDenied)).Error
		}
		next := req.SessionFor(confirming)
		sid, err := createSessionTx(ctx, tx, userKey, next)
		if err != nil {
			return err
		}
		refresh, _, err := createRefreshTokenTx(tx, userKey, sid, next.Platform, refreshTTL, now)
		if err != nil {
			return err
		}
		if err := tx.Delete(&nativeSignInRow{}, row.ID).Error; err != nil {
			return err
		}
		out = coreidentity.NativeSignInRedemption{
			UserID:       user.ID,
			SessionID:    sid,
			Platform:     next.Platform,
			AuthMethod:   next.AuthMethod,
			RefreshToken: refresh,
		}
		return nil
	})
	if err != nil {
		return coreidentity.NativeSignInRedemption{}, err
	}
	if refused {
		return coreidentity.NativeSignInRedemption{}, coreidentity.ErrNativeSignInRefused
	}
	return out, nil
}

// nativeSignInDecider reads the confirming account as it stands now: only what
// RedeemableBy weighs.
func nativeSignInDecider(tx *gorm.DB, userKey uint64) (*coreidentity.User, error) {
	var row userRow
	err := tx.Select("id", "public_id", "kind", "disabled_at").Where("id = ?", userKey).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &coreidentity.User{ID: row.PublicID, Kind: row.Kind, DisabledAt: row.DisabledAt}, nil
}

// nativeSignInConfirmingSession reads the confirming session under a lock, or
// nil when it is no longer active. The read does not join the account, so the
// lock stays on this one row; the owner is named only when the row belongs to
// the deciding account, which RedeemableBy then requires.
func nativeSignInConfirmingSession(tx *gorm.DB, sessionKey, deciderKey uint64, decider *coreidentity.User, now time.Time) (*coreidentity.AuthSession, error) {
	var row authSessionRow
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND revoked_at IS NULL AND absolute_expires_at > ?", sessionKey, now).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sess := &coreidentity.AuthSession{
		SID:               row.PublicID,
		Platform:          row.Platform,
		AuthMethod:        row.AuthMethod,
		AbsoluteExpiresAt: row.AbsoluteExpiresAt,
	}
	if decider != nil && row.UserID == deciderKey {
		sess.UserID = decider.ID
	}
	return sess, nil
}

// DeleteExpiredNativeSignIns implements coreidentity.NativeSignInStore.
func (s *Store) DeleteExpiredNativeSignIns(ctx context.Context, before time.Time) (int64, error) {
	res := s.db.WithContext(ctx).
		Where("expires_at <= ?", before.UTC()).
		Delete(&nativeSignInRow{})
	return res.RowsAffected, res.Error
}
