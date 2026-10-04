package db

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreassistant "github.com/icloudbb/buildmax/internal/core/assistant"
	corechannel "github.com/icloudbb/buildmax/internal/core/channel"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
)

// assistantRow is a Space Assistant at its current revision. The definition is
// one JSON document, written and read whole: nothing queries inside it, and a
// revision is a copy of exactly it.
type assistantRow struct {
	ID         uint64 `gorm:"primaryKey;autoIncrement"`
	PublicID   string `gorm:"column:public_id;type:char(20) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_assistant_public_id;not null"`
	SpaceID    uint64 `gorm:"column:space_id;not null;index"`
	Definition string `gorm:"column:definition;type:mediumtext;not null"`
	Revision   int    `gorm:"column:revision;not null;default:1"`
	State      string `gorm:"column:state;type:varchar(16);not null"`
	// SponsorUserID is outside the definition: who is accountable is not part
	// of what produced an answer.
	SponsorUserID uint64     `gorm:"column:sponsor_user_id;not null"`
	CreatedBy     uint64     `gorm:"column:created_by;not null"`
	DeletedAt     *time.Time `gorm:"column:deleted_at;index"`
	CreatedAt     time.Time  `gorm:"autoCreateTime"`
	UpdatedAt     time.Time  `gorm:"autoUpdateTime"`
}

func (assistantRow) TableName() string { return "assistant" }

// assistantRevisionRow is one recorded definition. Append-only.
type assistantRevisionRow struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement"`
	AssistantID uint64    `gorm:"column:assistant_id;not null;uniqueIndex:uq_assistant_revision,priority:1"`
	Revision    int       `gorm:"column:revision;not null;uniqueIndex:uq_assistant_revision,priority:2"`
	Definition  string    `gorm:"column:definition;type:mediumtext;not null"`
	CreatedBy   uint64    `gorm:"column:created_by;not null"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`
}

func (assistantRevisionRow) TableName() string { return "assistant_revision" }

// assistantBindingRow is an Assistant's bot. One per Assistant, and one per bot:
// a platform delivers each message to one receiver.
type assistantBindingRow struct {
	ID          uint64 `gorm:"primaryKey;autoIncrement"`
	PublicID    string `gorm:"column:public_id;type:char(20) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_assistant_binding_public_id;not null"`
	AssistantID uint64 `gorm:"column:assistant_id;not null;uniqueIndex:uq_assistant_binding_assistant"`
	Platform    string `gorm:"column:platform;type:varchar(32) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_assistant_binding_bot,priority:1"`
	BotID       string `gorm:"column:bot_id;type:varchar(128) CHARACTER SET ascii COLLATE ascii_bin;not null;uniqueIndex:uq_assistant_binding_bot,priority:2"`
	BotHandle   string `gorm:"column:bot_handle;type:varchar(255)"`
	// TokenSealed is the bot token under the deployment KEK. It is never
	// selected by a general read; BindingToken is the one way out.
	TokenSealed []byte    `gorm:"column:token_sealed;type:blob;not null"`
	CreatedBy   uint64    `gorm:"column:created_by;not null"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`
}

func (assistantBindingRow) TableName() string { return "assistant_binding" }

// botTokenAAD domain-separates a bot token from everything else sealed under
// the deployment KEK, so a blob authenticated as one cannot be opened as a
// model credential or the reverse.
var botTokenAAD = []byte("bmax-assistant-bot-token\x00")

type assistantReadRow struct {
	Row           assistantRow `gorm:"embedded"`
	SpacePublicID string       `gorm:"column:space_public_id"`
	SponsorPublic string       `gorm:"column:sponsor_public_id"`
	CreatorPublic string       `gorm:"column:creator_public_id"`
}

func (s *Store) assistantSelect(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Model(&assistantRow{}).
		Select("assistant.*, t.public_id AS space_public_id, sp.public_id AS sponsor_public_id, cb.public_id AS creator_public_id").
		Joins("INNER JOIN space t ON t.id = assistant.space_id").
		Joins("INNER JOIN `user` sp ON sp.id = assistant.sponsor_user_id").
		Joins("INNER JOIN `user` cb ON cb.id = assistant.created_by").
		Where("assistant.deleted_at IS NULL")
}

func toAssistant(r *assistantReadRow) (*coreassistant.Assistant, error) {
	var def coreassistant.Definition
	if err := json.Unmarshal([]byte(r.Row.Definition), &def); err != nil {
		return nil, err
	}
	return &coreassistant.Assistant{
		ID:            r.Row.PublicID,
		SpaceID:       r.SpacePublicID,
		Def:           def,
		Revision:      r.Row.Revision,
		State:         r.Row.State,
		SponsorUserID: r.SponsorPublic,
		CreatedBy:     r.CreatorPublic,
		CreatedAt:     r.Row.CreatedAt,
		UpdatedAt:     r.Row.UpdatedAt,
	}, nil
}

// CreateAssistant implements coreassistant.Store. It starts paused at
// revision 1: going live is a separate, confirmed step.
func (s *Store) CreateAssistant(ctx context.Context, spaceID, createdBy, sponsorUserID string, def coreassistant.Definition) (*coreassistant.Assistant, error) {
	body, err := json.Marshal(def)
	if err != nil {
		return nil, err
	}
	var publicID string
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		spaceKey, err := lookupKey(ctx, tx, "space", spaceID)
		if err != nil {
			return err
		}
		creator, err := lookupKey(ctx, tx, "user", createdBy)
		if err != nil {
			return err
		}
		sponsor, err := lookupKey(ctx, tx, "user", sponsorUserID)
		if err != nil {
			return err
		}
		row := &assistantRow{
			SpaceID: spaceKey, Definition: string(body), Revision: 1,
			State: coreassistant.StatePaused, SponsorUserID: sponsor, CreatedBy: creator,
		}
		if err := createWithPublicID(ctx, tx, "uq_assistant_public_id", func(id string) { row.PublicID = id }, row); err != nil {
			return err
		}
		publicID = row.PublicID
		return tx.Create(&assistantRevisionRow{AssistantID: row.ID, Revision: 1, Definition: string(body), CreatedBy: creator}).Error
	})
	if err != nil {
		return nil, err
	}
	return s.GetAssistant(ctx, publicID)
}

// GetAssistant implements coreassistant.Store.
func (s *Store) GetAssistant(ctx context.Context, assistantID string) (*coreassistant.Assistant, error) {
	var r assistantReadRow
	err := s.assistantSelect(ctx).Where("assistant.public_id = ?", canonicalPublicID(assistantID)).Take(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toAssistant(&r)
}

// ListAssistantsBySpace implements coreassistant.Store. A Space has a handful,
// so the list is not paged.
func (s *Store) ListAssistantsBySpace(ctx context.Context, spaceID string) ([]coreassistant.Assistant, error) {
	var rows []assistantReadRow
	if err := s.assistantSelect(ctx).Where("t.public_id = ?", canonicalPublicID(spaceID)).
		Order("assistant.created_at, assistant.id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]coreassistant.Assistant, 0, len(rows))
	for i := range rows {
		a, err := toAssistant(&rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, nil
}

// UpdateAssistantDefinition implements coreassistant.Store. The revision
// advances with a compare on the one read, so two concurrent saves cannot both
// claim the same next revision: the unique (assistant_id, revision) index
// refuses the second.
func (s *Store) UpdateAssistantDefinition(ctx context.Context, assistantID, updatedBy string, def coreassistant.Definition) (*coreassistant.Assistant, error) {
	current, err := s.GetAssistant(ctx, assistantID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, coreassistant.ErrNotFound
	}
	before, err := json.Marshal(current.Def)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(def)
	if err != nil {
		return nil, err
	}
	if string(before) == string(body) {
		return current, nil
	}
	next := nextRevision(current.Revision)
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		key, err := lookupKey(ctx, tx, "assistant", assistantID)
		if err != nil {
			return err
		}
		author, err := lookupKey(ctx, tx, "user", updatedBy)
		if err != nil {
			return err
		}
		upd := tx.Model(&assistantRow{}).Where("id = ? AND revision = ? AND deleted_at IS NULL", key, current.Revision).
			Updates(map[string]any{"definition": string(body), "revision": next})
		if upd.Error != nil {
			return upd.Error
		}
		if upd.RowsAffected == 0 {
			return apierr.New(apierr.KindConflict, "the assistant changed while saving; reload and try again")
		}
		return tx.Create(&assistantRevisionRow{AssistantID: key, Revision: next, Definition: string(body), CreatedBy: author}).Error
	})
	if err != nil {
		return nil, err
	}
	return s.GetAssistant(ctx, assistantID)
}

// SetAssistantState implements coreassistant.Store.
func (s *Store) SetAssistantState(ctx context.Context, assistantID, state string) error {
	return s.updateLiveAssistant(ctx, assistantID, map[string]any{"state": state})
}

// SetAssistantSponsor implements coreassistant.Store.
func (s *Store) SetAssistantSponsor(ctx context.Context, assistantID, sponsorUserID string) error {
	sponsor, err := lookupKey(ctx, s.db, "user", sponsorUserID)
	if err != nil {
		return err
	}
	return s.updateLiveAssistant(ctx, assistantID, map[string]any{"sponsor_user_id": sponsor})
}

func (s *Store) updateLiveAssistant(ctx context.Context, assistantID string, fields map[string]any) error {
	upd := s.db.WithContext(ctx).Model(&assistantRow{}).
		Where("public_id = ? AND deleted_at IS NULL", canonicalPublicID(assistantID)).Updates(fields)
	if upd.Error != nil {
		return upd.Error
	}
	if upd.RowsAffected == 0 {
		// Updates reports 0 for a no-op as well, so tell a missing row apart.
		if a, err := s.GetAssistant(ctx, assistantID); err != nil || a == nil {
			if err != nil {
				return err
			}
			return coreassistant.ErrNotFound
		}
	}
	return nil
}

// DeleteAssistant implements coreassistant.Store. The row stays, marked, so
// conversations and Tasks that name it keep a readable reference; its bot is
// released at once.
func (s *Store) DeleteAssistant(ctx context.Context, assistantID string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row assistantRow
		err := tx.Select("id").Where("public_id = ? AND deleted_at IS NULL", canonicalPublicID(assistantID)).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return coreassistant.ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := tx.Where("assistant_id = ?", row.ID).Delete(&assistantBindingRow{}).Error; err != nil {
			return err
		}
		return tx.Model(&assistantRow{}).Where("id = ?", row.ID).Update("deleted_at", time.Now().UTC()).Error
	})
}

type bindingReadRow struct {
	Row               assistantBindingRow `gorm:"embedded"`
	AssistantPublicID string              `gorm:"column:assistant_public_id"`
	SpacePublicID     string              `gorm:"column:space_public_id"`
	CreatorPublicID   string              `gorm:"column:creator_public_id"`
}

// bindingSelect never selects the sealed token.
func (s *Store) bindingSelect(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Model(&assistantBindingRow{}).
		Select("assistant_binding.id, assistant_binding.public_id, assistant_binding.assistant_id, assistant_binding.platform, " +
			"assistant_binding.bot_id, assistant_binding.bot_handle, assistant_binding.created_by, assistant_binding.created_at, " +
			"a.public_id AS assistant_public_id, t.public_id AS space_public_id, cb.public_id AS creator_public_id").
		Joins("INNER JOIN assistant a ON a.id = assistant_binding.assistant_id AND a.deleted_at IS NULL").
		Joins("INNER JOIN space t ON t.id = a.space_id").
		Joins("INNER JOIN `user` cb ON cb.id = assistant_binding.created_by")
}

func toBinding(r *bindingReadRow) *coreassistant.Binding {
	return &coreassistant.Binding{
		ID:          r.Row.PublicID,
		AssistantID: r.AssistantPublicID,
		SpaceID:     r.SpacePublicID,
		Platform:    r.Row.Platform,
		BotID:       r.Row.BotID,
		BotHandle:   r.Row.BotHandle,
		CreatedBy:   r.CreatorPublicID,
		CreatedAt:   r.Row.CreatedAt,
	}
}

// CreateBinding implements coreassistant.Store. A token is refused rather than
// stored in the clear when the deployment has no encryption key.
func (s *Store) CreateBinding(ctx context.Context, in coreassistant.NewBinding) (*coreassistant.Binding, error) {
	if s.credentialCipher == nil {
		return nil, coregw.ErrCredentialEncryptionUnavailable
	}
	sealed, err := s.credentialCipher.SealValue(in.Token, botTokenAAD)
	if err != nil {
		return nil, err
	}
	var publicID string
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var a assistantRow
		err := tx.Select("id").Where("public_id = ? AND deleted_at IS NULL", canonicalPublicID(in.AssistantID)).Take(&a).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return coreassistant.ErrNotFound
		}
		if err != nil {
			return err
		}
		creator, err := lookupKey(ctx, tx, "user", in.CreatedBy)
		if err != nil {
			return err
		}
		row := &assistantBindingRow{
			AssistantID: a.ID, Platform: in.Platform, BotID: in.BotID, BotHandle: in.BotHandle,
			TokenSealed: sealed, CreatedBy: creator,
		}
		err = createWithPublicID(ctx, tx, "uq_assistant_binding_public_id", func(id string) { row.PublicID = id }, row)
		switch {
		case isDuplicateOnIndex(err, "uq_assistant_binding_assistant"):
			return coreassistant.ErrAlreadyBound
		case isDuplicateOnIndex(err, "uq_assistant_binding_bot"):
			return corechannel.ErrBotInUse
		case err != nil:
			return err
		}
		publicID = row.PublicID
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetBinding(ctx, publicID)
}

// GetBinding implements coreassistant.Store.
func (s *Store) GetBinding(ctx context.Context, bindingID string) (*coreassistant.Binding, error) {
	var r bindingReadRow
	err := s.bindingSelect(ctx).Where("assistant_binding.public_id = ?", canonicalPublicID(bindingID)).Take(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toBinding(&r), nil
}

// GetBindingByAssistant implements coreassistant.Store.
func (s *Store) GetBindingByAssistant(ctx context.Context, assistantID string) (*coreassistant.Binding, error) {
	var r bindingReadRow
	err := s.bindingSelect(ctx).Where("a.public_id = ?", canonicalPublicID(assistantID)).Take(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toBinding(&r), nil
}

// DeleteBindingByAssistant implements coreassistant.Store.
func (s *Store) DeleteBindingByAssistant(ctx context.Context, assistantID string) error {
	key, err := lookupKey(ctx, s.db, "assistant", assistantID)
	if errors.Is(err, apierr.ErrNotFound) {
		return coreassistant.ErrNotBound
	}
	if err != nil {
		return err
	}
	del := s.db.WithContext(ctx).Where("assistant_id = ?", key).Delete(&assistantBindingRow{})
	if del.Error != nil {
		return del.Error
	}
	if del.RowsAffected == 0 {
		return coreassistant.ErrNotBound
	}
	return nil
}

// ListBindings implements coreassistant.Store. Bindings are few, one per
// published Assistant, so the list is read whole.
func (s *Store) ListBindings(ctx context.Context) ([]coreassistant.Binding, error) {
	var rows []bindingReadRow
	if err := s.bindingSelect(ctx).Order("assistant_binding.id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]coreassistant.Binding, 0, len(rows))
	for i := range rows {
		out = append(out, *toBinding(&rows[i]))
	}
	return out, nil
}

// BindingToken implements coreassistant.Store.
func (s *Store) BindingToken(ctx context.Context, bindingID string) (string, error) {
	var row assistantBindingRow
	err := s.db.WithContext(ctx).Select("token_sealed").
		Where("public_id = ?", canonicalPublicID(bindingID)).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", coreassistant.ErrNotBound
	}
	if err != nil {
		return "", err
	}
	if s.credentialCipher == nil {
		return "", coregw.ErrCredentialEncryptionUnavailable
	}
	return s.credentialCipher.OpenValue(row.TokenSealed, botTokenAAD)
}

// eachSealedBotToken visits every binding's sealed token, in bounded batches.
func (s *Store) eachSealedBotToken(ctx context.Context, fn func(row *assistantBindingRow) error) error {
	var cursor uint64
	for {
		var rows []assistantBindingRow
		if err := s.db.WithContext(ctx).Select("id", "public_id", "token_sealed").
			Where("id > ?", cursor).Order("id").Limit(rewrapBatch).Find(&rows).Error; err != nil {
			return err
		}
		for i := range rows {
			if err := fn(&rows[i]); err != nil {
				return err
			}
		}
		if len(rows) < rewrapBatch {
			return nil
		}
		cursor = rows[len(rows)-1].ID
	}
}
