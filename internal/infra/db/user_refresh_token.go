package db

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type userRefreshTokenRow struct {
	ID        uint64 `gorm:"primaryKey;autoIncrement"`
	TokenHash string `gorm:"type:varchar(128);uniqueIndex;not null"`
	UserID    uint64 `gorm:"column:user_id;not null;index"`
	// SessionID is the login chain this token belongs to. It keeps its own
	// format -- an "as_" prefixed ID minted with the token, not a row handle --
	// because it names a chain, not a row, and it is a claim in every access
	// token already issued under it.
	SessionID string    `gorm:"type:varchar(64);not null;index"`
	Platform  string    `gorm:"type:varchar(32)"`
	ExpiresAt time.Time `gorm:"not null;index"`
	UsedAt    *time.Time
	RevokedAt *time.Time
	// ReplacedBy holds the hash of the token most recently issued in exchange
	// for this one. It is never read by the exchange itself — it exists so that
	// an operator investigating a reuse report can walk the chain back to the
	// login.
	ReplacedBy string    `gorm:"type:varchar(128)"`
	CreatedAt  time.Time `gorm:"autoCreateTime"`
}

func (userRefreshTokenRow) TableName() string { return "user_refresh_token" }

// refreshTokenReadRow is the row plus the handle its owner resolves to. The
// rotation result names a user to the caller, and the row holds a key.
type refreshTokenReadRow struct {
	Row          userRefreshTokenRow `gorm:"embedded"`
	UserPublicID string              `gorm:"column:user_public_id"`
}

func refreshTokenSelect(tx *gorm.DB) *gorm.DB {
	return tx.Model(&userRefreshTokenRow{}).
		Select("user_refresh_token.*, u.public_id AS user_public_id").
		Joins("INNER JOIN `user` u ON u.id = user_refresh_token.user_id")
}

const (
	// Same reasoning as loginCodePrefix: a leaked credential should be
	// recognizable on sight, and to a secret scanner.
	refreshTokenPrefix = "bmxrefresh_"
	refreshTokenBytes  = 32
)

// hashRefreshToken is the only representation that reaches the database.
func hashRefreshToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

func newRefreshTokenPlaintext() (string, error) {
	b := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return refreshTokenPrefix + hex.EncodeToString(b), nil
}

// CreateRefreshToken implements coreidentity.RefreshTokenStore.
func (s *Store) CreateRefreshToken(ctx context.Context, in coreidentity.NewRefreshToken) (string, time.Time, error) {
	if in.UserID == "" {
		return "", time.Time{}, errors.New("refresh token: user id required")
	}
	if in.SessionID == "" {
		return "", time.Time{}, errors.New("refresh token: session id required")
	}
	ttl := in.TTL
	if ttl <= 0 {
		ttl = coreidentity.RefreshTokenTTLDefault
	}
	plaintext, err := newRefreshTokenPlaintext()
	if err != nil {
		return "", time.Time{}, err
	}
	userKey, err := lookupKey(ctx, s.db, "user", in.UserID)
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt := time.Now().UTC().Add(ttl)
	row := userRefreshTokenRow{
		TokenHash: hashRefreshToken(plaintext),
		UserID:    userKey,
		SessionID: in.SessionID,
		Platform:  in.Platform,
		ExpiresAt: expiresAt,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return "", time.Time{}, err
	}
	return plaintext, expiresAt, nil
}

// RotateRefreshToken implements coreidentity.RefreshTokenStore.
//
// The exchange opens with a conditional UPDATE, the same shape ConsumeLoginCode
// uses and for the same reason: concurrent callers holding one token must not
// both be treated as the first. Exactly one wins that UPDATE. Everyone else
// falls through to the slower path below, which decides whether they are a
// racing sibling process or a replay.
func (s *Store) RotateRefreshToken(ctx context.Context, plaintext string, now time.Time, ttl, grace time.Duration) (coreidentity.RotatedRefreshToken, error) {
	if plaintext == "" {
		return coreidentity.RotatedRefreshToken{}, coreidentity.ErrRefreshTokenInvalid
	}
	if ttl <= 0 {
		ttl = coreidentity.RefreshTokenTTLDefault
	}
	if grace < 0 {
		grace = 0
	}
	hash := hashRefreshToken(plaintext)

	var out coreidentity.RotatedRefreshToken
	// Reuse is reported after the transaction rather than from inside it.
	// Returning an error from the closure rolls the transaction back, which
	// would undo the very revocation that makes the report meaningful.
	var reused bool
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&userRefreshTokenRow{}).
			Where("token_hash = ? AND used_at IS NULL AND revoked_at IS NULL AND expires_at > ?", hash, now).
			Update("used_at", now)
		if res.Error != nil {
			return res.Error
		}

		// A locking read, not a plain one. Under REPEATABLE READ a consistent
		// read would answer from this transaction's snapshot, and the caller
		// that just lost the UPDATE race would see the row as still unspent —
		// which this function reads as "impossible" and refuses. Reading the
		// current row instead is what lets the grace window below recognize a
		// racing sibling for what it is.
		// The lock is taken on this row alone. Joining the account into the
		// locking read would lock the user row for the length of the
		// transaction, so two sessions of one account would serialize on each
		// other and, under load, wait past the lock timeout. The owner's handle
		// is read separately below.
		var row userRefreshTokenRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("token_hash = ?", hash).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return coreidentity.ErrRefreshTokenInvalid
			}
			return err
		}
		ownerPublicID, err := publicIDForKey(ctx, tx, "user", row.UserID)
		if err != nil {
			return err
		}

		if res.RowsAffected == 0 {
			// Not the winner. Work out why.
			if row.RevokedAt != nil || !row.ExpiresAt.After(now) {
				return coreidentity.ErrRefreshTokenInvalid
			}
			if row.UsedAt == nil {
				// Live, unexpired, unrevoked, unspent — yet the UPDATE matched
				// nothing. Nothing should produce this; refuse rather than
				// guess.
				return coreidentity.ErrRefreshTokenInvalid
			}
			if now.Sub(*row.UsedAt) > grace {
				// Spent long enough ago that a second presentation is not a
				// racing sibling. Retire the whole chain before reporting it:
				// if there are two holders, neither keeps access.
				if err := revokeSessionTx(tx, row.SessionID, now); err != nil {
					return err
				}
				// No new token, but the caller still needs to know whose
				// session was just revoked in order to record it. Commit, and
				// let the wrapper turn this into ErrRefreshTokenReused.
				out = coreidentity.RotatedRefreshToken{UserID: ownerPublicID, SessionID: row.SessionID}
				reused = true
				return nil
			}
			// Inside the grace window. Issue a second live token in the same
			// session rather than failing, and leave used_at at the first
			// exchange so the window does not slide forward on every retry.
		}

		next, err := newRefreshTokenPlaintext()
		if err != nil {
			return err
		}
		nextHash := hashRefreshToken(next)
		nextRow := userRefreshTokenRow{
			TokenHash: nextHash,
			UserID:    row.UserID,
			SessionID: row.SessionID,
			Platform:  row.Platform,
			ExpiresAt: now.Add(ttl),
		}
		if err := tx.Create(&nextRow).Error; err != nil {
			return err
		}
		if err := tx.Model(&userRefreshTokenRow{}).
			Where("token_hash = ?", hash).
			Update("replaced_by", nextHash).Error; err != nil {
			return err
		}
		out = coreidentity.RotatedRefreshToken{
			UserID:    ownerPublicID,
			SessionID: row.SessionID,
			Plaintext: next,
			ExpiresAt: nextRow.ExpiresAt,
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	if reused {
		return out, coreidentity.ErrRefreshTokenReused
	}
	return out, nil
}

// RevokeRefreshTokenSession implements coreidentity.RefreshTokenStore.
func (s *Store) RevokeRefreshTokenSession(ctx context.Context, plaintext string, now time.Time) (string, string, error) {
	if plaintext == "" {
		return "", "", nil
	}
	hash := hashRefreshToken(plaintext)
	var row refreshTokenReadRow
	err := refreshTokenSelect(s.db.WithContext(ctx)).Where("token_hash = ?", hash).Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", "", nil
		}
		return "", "", err
	}
	if err := revokeSessionTx(s.db.WithContext(ctx), row.Row.SessionID, now); err != nil {
		return "", "", err
	}
	return row.UserPublicID, row.Row.SessionID, nil
}

// revokeSessionTx retires every live refresh token in one session. Session-level
// revocation is owned by AuthSessionStore, which calls this to cascade to the
// refresh tokens; the reuse path in RotateRefreshToken uses it too.
func revokeSessionTx(tx *gorm.DB, sessionID string, now time.Time) error {
	if sessionID == "" {
		return nil
	}
	return tx.Model(&userRefreshTokenRow{}).
		Where("session_id = ? AND revoked_at IS NULL", sessionID).
		Update("revoked_at", now).Error
}

// DeleteExpiredRefreshTokens implements coreidentity.RefreshTokenStore.
//
// Revoked rows are kept until they expire rather than deleted on revocation:
// a reuse report is worth investigating, and the chain it points at should
// still be there when someone looks.
func (s *Store) DeleteExpiredRefreshTokens(ctx context.Context, before time.Time) (int64, error) {
	res := s.db.WithContext(ctx).
		Where("expires_at <= ?", before).
		Delete(&userRefreshTokenRow{})
	return res.RowsAffected, res.Error
}
