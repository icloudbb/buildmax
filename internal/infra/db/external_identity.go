package db

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/util"
)

// externalIdentityRow links one BuildMax account to one verified identity at an
// external IdP. The two composite unique indexes are the invariants: one subject
// maps to at most one account (uq_external_identity_issuer_subject), and one
// account has at most one identity per issuer (uq_external_identity_issuer_user).
// issuer and subject use a binary collation because OIDC iss/sub are
// case-sensitive protocol values; the default case-insensitive collation would
// treat two distinct subjects as one.
type externalIdentityRow struct {
	ID       uint64 `gorm:"primaryKey;autoIncrement"`
	PublicID string `gorm:"column:public_id;type:char(20) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_external_identity_public_id;not null"`
	UserID   uint64 `gorm:"column:user_id;not null;uniqueIndex:uq_external_identity_issuer_user,priority:2"`
	Issuer   string `gorm:"column:issuer;type:varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;not null;uniqueIndex:uq_external_identity_issuer_subject,priority:1;uniqueIndex:uq_external_identity_issuer_user,priority:1"`
	Subject  string `gorm:"column:subject;type:varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;not null;uniqueIndex:uq_external_identity_issuer_subject,priority:2"`
	// LastSeenEmail and LastSeenName snapshot the most recent sign-in's
	// attributes, for drift reconciliation. They are never identity.
	LastSeenEmail string     `gorm:"column:last_seen_email;type:varchar(320)"`
	LastSeenName  string     `gorm:"column:last_seen_name;type:varchar(255)"`
	LastLoginAt   *time.Time `gorm:"column:last_login_at"`
	CreatedAt     time.Time  `gorm:"autoCreateTime"`
}

func (externalIdentityRow) TableName() string { return "external_identity" }

// externalIdentityReadRow is the row plus the handle its owner resolves to, so a
// returned link names its user by public id rather than by row key.
type externalIdentityReadRow struct {
	Row          externalIdentityRow `gorm:"embedded"`
	UserPublicID string              `gorm:"column:user_public_id"`
}

func externalIdentitySelect(tx *gorm.DB) *gorm.DB {
	return tx.Model(&externalIdentityRow{}).
		Select("external_identity.*, u.public_id AS user_public_id").
		Joins("INNER JOIN `user` u ON u.id = external_identity.user_id")
}

func (r externalIdentityReadRow) toIdentity() coreidentity.ExternalIdentity {
	out := coreidentity.ExternalIdentity{
		ID:            r.Row.PublicID,
		UserID:        r.UserPublicID,
		Issuer:        r.Row.Issuer,
		Subject:       r.Row.Subject,
		LastSeenEmail: r.Row.LastSeenEmail,
		LastSeenName:  r.Row.LastSeenName,
		CreatedAt:     r.Row.CreatedAt,
	}
	if r.Row.LastLoginAt != nil {
		out.LastLoginAt = *r.Row.LastLoginAt
	}
	return out
}

// IdentityBySubject implements coreidentity.ExternalIdentityStore.
func (s *Store) IdentityBySubject(ctx context.Context, issuer, subject string) (*coreidentity.ExternalIdentity, error) {
	if issuer == "" || subject == "" {
		return nil, nil
	}
	var row externalIdentityReadRow
	err := externalIdentitySelect(s.db.WithContext(ctx)).
		Where("external_identity.issuer = ? AND external_identity.subject = ?", issuer, subject).
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

// IdentityByUserAndIssuer implements coreidentity.ExternalIdentityStore.
func (s *Store) IdentityByUserAndIssuer(ctx context.Context, userID, issuer string) (*coreidentity.ExternalIdentity, error) {
	if issuer == "" {
		return nil, nil
	}
	userKey, err := lookupKey(ctx, s.db, "user", userID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var row externalIdentityReadRow
	err = externalIdentitySelect(s.db.WithContext(ctx)).
		Where("external_identity.user_id = ? AND external_identity.issuer = ?", userKey, issuer).
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

// ListUserIdentities implements coreidentity.ExternalIdentityStore.
func (s *Store) ListUserIdentities(ctx context.Context, userID string) ([]coreidentity.ExternalIdentity, error) {
	userKey, err := lookupKey(ctx, s.db, "user", userID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rows []externalIdentityReadRow
	if err := externalIdentitySelect(s.db.WithContext(ctx)).
		Where("external_identity.user_id = ?", userKey).
		Order("external_identity.created_at DESC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]coreidentity.ExternalIdentity, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toIdentity())
	}
	return out, nil
}

// LinkExisting implements coreidentity.ExternalIdentityStore.
func (s *Store) LinkExisting(ctx context.Context, in coreidentity.LinkIdentity) (*coreidentity.ExternalIdentity, error) {
	userKey, err := lookupKey(ctx, s.db, "user", in.UserID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, coreidentity.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	row := externalIdentityRow{
		UserID:        userKey,
		Issuer:        in.Issuer,
		Subject:       in.Subject,
		LastSeenEmail: in.Seen.Email,
		LastSeenName:  in.Seen.Name,
		LastLoginAt:   &now,
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := createWithPublicID(ctx, tx, "uq_external_identity_public_id",
			func(id string) { row.PublicID = id }, &row); err != nil {
			// A duplicate on either composite index is a concurrent first login or
			// an account already linked to this issuer, not a public-id collision.
			if isDuplicateKey(err) {
				return coreidentity.ErrIdentityConflict
			}
			return err
		}
		return writeAuditEvent(ctx, tx, coreaudit.Event{
			ActorType:  coreaudit.ActorSystem,
			ActorID:    in.UserID,
			Action:     coreaudit.UserExternalIdentityLinked,
			TargetType: "user",
			TargetID:   in.UserID,
			Detail:     in.Issuer,
		})
	})
	if err != nil {
		return nil, err
	}
	out := coreidentity.ExternalIdentity{
		ID: row.PublicID, UserID: in.UserID, Issuer: in.Issuer, Subject: in.Subject,
		LastSeenEmail: in.Seen.Email, LastSeenName: in.Seen.Name, LastLoginAt: now, CreatedAt: row.CreatedAt,
	}
	return &out, nil
}

// CreateUserWithIdentity implements coreidentity.ExternalIdentityStore.
//
// It is CreateUser plus a fourth insert for the link and the two audit rows,
// all in one transaction: a just-in-time account, its personal Space, the owner
// membership, its identity link, and the record of both come into existence
// together or not at all.
func (s *Store) CreateUserWithIdentity(ctx context.Context, in coreidentity.ProvisionUser) (*coreidentity.User, *coreidentity.ExternalIdentity, error) {
	existing, err := s.UserByEmail(ctx, in.Email)
	if err != nil {
		return nil, nil, err
	}
	if existing != nil {
		return nil, nil, coreidentity.ErrEmailExists
	}
	now := time.Now().UTC()
	u := coreidentity.User{Email: in.Email, Name: in.Name, CreatedAt: now}
	if in.QuotaTier != "" {
		u.QuotaTier = in.QuotaTier
	}
	userDB := toUserRow(&u)
	personalSpaceDB := &spaceRow{
		Name:      corespace.DefaultPersonalName,
		QuotaTier: in.QuotaTier,
		CreatedAt: now,
		UpdatedAt: now,
	}
	identityDB := externalIdentityRow{
		Issuer: in.Issuer, Subject: in.Subject,
		LastSeenEmail: in.Email, LastSeenName: in.Name, LastLoginAt: &now,
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := createWithPublicID(ctx, tx, "uq_user_public_id",
			func(id string) { userDB.PublicID = id }, userDB); err != nil {
			if isDuplicateKey(err) {
				return coreidentity.ErrEmailExists
			}
			return err
		}
		personalSpaceDB.PersonalForUserID = &userDB.ID
		personalSpaceDB.CreatedBy = userDB.ID
		if err := createWithPublicID(ctx, tx, "uq_space_public_id",
			func(id string) { personalSpaceDB.PublicID = id }, personalSpaceDB); err != nil {
			return err
		}
		if err := tx.Create(&spaceMemberRow{
			SpaceID: personalSpaceDB.ID, UserID: userDB.ID, Role: corespace.RoleOwner, CreatedAt: now,
		}).Error; err != nil {
			return err
		}
		identityDB.UserID = userDB.ID
		if err := createWithPublicID(ctx, tx, "uq_external_identity_public_id",
			func(id string) { identityDB.PublicID = id }, &identityDB); err != nil {
			if isDuplicateKey(err) {
				return coreidentity.ErrIdentityConflict
			}
			return err
		}
		if err := writeAuditEvent(ctx, tx, coreaudit.Event{
			ActorType: coreaudit.ActorSystem, ActorID: userDB.PublicID,
			Action: coreaudit.UserCreated, TargetType: "user", TargetID: userDB.PublicID,
			Detail: "oidc jit",
		}); err != nil {
			return err
		}
		return writeAuditEvent(ctx, tx, coreaudit.Event{
			ActorType: coreaudit.ActorSystem, ActorID: userDB.PublicID,
			Action: coreaudit.UserExternalIdentityLinked, TargetType: "user", TargetID: userDB.PublicID,
			Detail: in.Issuer,
		})
	}); err != nil {
		return nil, nil, err
	}
	u.ID = userDB.PublicID
	identity := coreidentity.ExternalIdentity{
		ID: identityDB.PublicID, UserID: u.ID, Issuer: in.Issuer, Subject: in.Subject,
		LastSeenEmail: in.Email, LastSeenName: in.Name, LastLoginAt: now, CreatedAt: identityDB.CreatedAt,
	}
	return &u, &identity, nil
}

// UnlinkIdentity implements coreidentity.ExternalIdentityStore.
func (s *Store) UnlinkIdentity(ctx context.Context, in coreidentity.UnlinkIdentity) error {
	userKey, err := lookupKey(ctx, s.db, "user", in.UserID)
	if errors.Is(err, apierr.ErrNotFound) {
		return coreidentity.ErrIdentityNotFound
	}
	if err != nil {
		return err
	}
	idCanon, ok := util.CanonicalPublicID(in.IdentityID)
	if !ok {
		return coreidentity.ErrIdentityNotFound
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// The disabled check and the delete share one transaction so an account
		// cannot be re-enabled between the two: unlinking a live account's identity
		// is exactly the race the precondition exists to prevent.
		var u userRow
		if err := tx.Select("id", "disabled_at").Where("id = ?", userKey).Take(&u).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return coreidentity.ErrIdentityNotFound
			}
			return err
		}
		if u.DisabledAt == nil {
			return coreidentity.ErrUnlinkRequiresDisabled
		}
		res := tx.Where("public_id = ? AND user_id = ?", idCanon, userKey).Delete(&externalIdentityRow{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return coreidentity.ErrIdentityNotFound
		}
		actorType, actorID := coreaudit.ActorUser, in.ActorID
		if actorID == "" {
			actorType, actorID = coreaudit.ActorSystem, coreaudit.ActorOperator
		}
		return writeAuditEvent(ctx, tx, coreaudit.Event{
			ActorType: actorType, ActorID: actorID,
			Action: coreaudit.UserExternalIdentityUnlinked, TargetType: "user", TargetID: in.UserID,
			Detail: in.IdentityID,
		})
	})
}

// UpdateLastSeen implements coreidentity.ExternalIdentityStore. It is
// best-effort: a link that is gone by the time a login settles is not an error.
func (s *Store) UpdateLastSeen(ctx context.Context, issuer, subject string, seen coreidentity.SeenClaims, now time.Time) error {
	if issuer == "" || subject == "" {
		return nil
	}
	return s.db.WithContext(ctx).Model(&externalIdentityRow{}).
		Where("issuer = ? AND subject = ?", issuer, subject).
		Updates(map[string]any{
			"last_seen_email": seen.Email,
			"last_seen_name":  seen.Name,
			"last_login_at":   now,
		}).Error
}
