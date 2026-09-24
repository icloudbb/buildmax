package db

import (
	"context"
	"errors"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"

	"github.com/icloudbb/buildmax/internal/util"
	"gorm.io/gorm"
)

type conversationRow struct {
	ID       uint64 `gorm:"primaryKey;autoIncrement"`
	PublicID string `gorm:"column:public_id;type:char(20) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_conversation_public_id;not null"`
	UserID   uint64 `gorm:"column:user_id;not null;index:idx_conversation_user_created,priority:1"`
	SpaceID  uint64 `gorm:"column:space_id;index:idx_conversation_space_created,priority:1"`
	Channel  string `gorm:"type:varchar(32);not null"`
	// ChannelRef addresses the chat a platform-carried conversation belongs to,
	// so the next message from that chat continues it. Empty for Portal and
	// webhook conversations. Several rows share one ref: starting a new
	// conversation from a chat leaves the old ones in place, and the newest is
	// the chat's current one.
	ChannelRef string `gorm:"column:channel_ref;type:varchar(191) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;not null;default:'';index:idx_conversation_channel_ref,priority:1"`
	Title      string `gorm:"type:varchar(256)"`
	CreatedBy  uint64 `gorm:"column:created_by;not null"`
	// TurnFence is the highest turn lease fencing token this conversation has
	// accepted a message-history write under. AppendMessage advances it and
	// refuses a lower token, so a stale replica cannot write behind the holder
	// that superseded it. Zero until the first fenced write. See
	// docs/design/server-coordination.md §7.
	TurnFence int64 `gorm:"column:turn_fence;not null;default:0"`
	// The two composite indexes carry created_at because every listing of a
	// conversation is ordered by it. The single-column indexes the string model
	// left behind could not serve the sort.
	CreatedAt time.Time `gorm:"autoCreateTime;index:idx_conversation_user_created,priority:2;index:idx_conversation_space_created,priority:2;index:idx_conversation_channel_ref,priority:2"`
}

func (conversationRow) TableName() string { return "conversation" }

// conversationReadRow is the row plus the handles its references resolve to.
// The row is a named field: see spaceReadRow for why anonymous embedding does
// not work here. A pointer field is one a LEFT JOIN may leave NULL.
type conversationReadRow struct {
	Row               conversationRow `gorm:"embedded"`
	UserPublicID      string          `gorm:"column:user_public_id"`
	SpacePublicID     *string         `gorm:"column:space_public_id"`
	CreatedByPublicID string          `gorm:"column:created_by_public_id"`
}

func (s *Store) conversationSelect(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Model(&conversationRow{}).
		Select("conversation.*, u.public_id AS user_public_id, t.public_id AS space_public_id, cb.public_id AS created_by_public_id").
		Joins("INNER JOIN `user` u ON u.id = conversation.user_id").
		Joins("LEFT JOIN space t ON t.id = conversation.space_id").
		Joins("INNER JOIN `user` cb ON cb.id = conversation.created_by")
}

func toConversation(row *conversationReadRow) *coreconv.Conversation {
	if row == nil {
		return nil
	}
	return &coreconv.Conversation{
		ID:         row.Row.PublicID,
		UserID:     row.UserPublicID,
		SpaceID:    derefPublicID(row.SpacePublicID),
		Channel:    row.Row.Channel,
		ChannelRef: row.Row.ChannelRef,
		Title:      row.Row.Title,
		CreatedBy:  row.CreatedByPublicID,
		CreatedAt:  row.Row.CreatedAt,
	}
}

func toConversations(rows []conversationReadRow) []coreconv.Conversation {
	out := make([]coreconv.Conversation, len(rows))
	for i := range rows {
		out[i] = *toConversation(&rows[i])
	}
	return out
}

// CreateConversation creates a new Tier 1 conversation. Returns the conversation with conversation_id set.
func (s *Store) CreateConversation(ctx context.Context, userID, channel, createdBy string) (*coreconv.Conversation, error) {
	spaceID, err := s.personalSpaceIDForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return s.CreateConversationInSpace(ctx, spaceID, userID, channel, createdBy)
}

// CreateConversationInSpace creates a new space-scoped Tier 1 conversation.
func (s *Store) CreateConversationInSpace(ctx context.Context, spaceID, userID, channel, createdBy string) (*coreconv.Conversation, error) {
	return s.createConversation(ctx, spaceID, userID, channel, createdBy, "")
}

// CreateChatConversation creates a space-scoped conversation a chat platform
// carries, addressed by channelRef. The user both owns and started it.
func (s *Store) CreateChatConversation(ctx context.Context, spaceID, userID, channel, channelRef string) (*coreconv.Conversation, error) {
	if channelRef == "" {
		return nil, errors.New("chat conversation: channel ref required")
	}
	return s.createConversation(ctx, spaceID, userID, channel, userID, channelRef)
}

// LatestChatConversation returns userID's newest conversation for one chat, or
// (nil, nil). The user is part of the condition so a chat account relinked to
// another person never continues the previous person's conversation.
func (s *Store) LatestChatConversation(ctx context.Context, userID, channel, channelRef string) (*coreconv.Conversation, error) {
	if channelRef == "" {
		return nil, nil
	}
	userKey, err := lookupKey(ctx, s.db, "user", userID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c conversationReadRow
	err = s.conversationSelect(ctx).
		Where("conversation.channel_ref = ? AND conversation.channel = ? AND conversation.user_id = ?", channelRef, channel, userKey).
		Order("conversation.created_at DESC").Order("conversation.id DESC").
		Take(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toConversation(&c), nil
}

func (s *Store) createConversation(ctx context.Context, spaceID, userID, channel, createdBy, channelRef string) (*coreconv.Conversation, error) {
	now := time.Now().UTC()
	row := &conversationRow{Channel: channel, ChannelRef: channelRef, CreatedAt: now}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		userKey, err := lookupKey(ctx, tx, "user", userID)
		if err != nil {
			return err
		}
		row.UserID = userKey
		if spaceID != "" {
			spaceKey, err := lookupKey(ctx, tx, "space", spaceID)
			if err != nil {
				return err
			}
			row.SpaceID = spaceKey
		}
		creator, err := lookupKey(ctx, tx, "user", createdBy)
		if err != nil {
			return err
		}
		row.CreatedBy = creator
		return createWithPublicID(ctx, tx, "uq_conversation_public_id",
			func(id string) { row.PublicID = id }, row)
	}); err != nil {
		return nil, err
	}
	return &coreconv.Conversation{
		ID:         row.PublicID,
		UserID:     userID,
		SpaceID:    spaceID,
		Channel:    channel,
		ChannelRef: channelRef,
		CreatedBy:  createdBy,
		CreatedAt:  now,
	}, nil
}

// GetConversation returns the conversation by conversation_id, or (nil, nil) if not found.
func (s *Store) GetConversation(ctx context.Context, conversationID string) (*coreconv.Conversation, error) {
	id, ok := util.CanonicalPublicID(conversationID)
	if !ok {
		return nil, nil
	}
	var c conversationReadRow
	err := s.conversationSelect(ctx).Where("conversation.public_id = ?", id).Take(&c).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toConversation(&c), nil
}

// ListConversationsByUser returns conversations for the user ordered by created_at DESC.
// total is the total count of matching conversations (ignoring limit/offset).
func (s *Store) ListConversationsByUser(ctx context.Context, userID string, limit, offset int) ([]coreconv.Conversation, int, error) {
	limit, offset = capPage(limit, offset)
	userKey, err := lookupKey(ctx, s.db, "user", userID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := s.db.WithContext(ctx).Model(&conversationRow{}).Where("user_id = ?", userKey).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []conversationReadRow
	err = s.conversationSelect(ctx).Where("conversation.user_id = ?", userKey).
		Order("conversation.created_at DESC").
		Limit(limit).Offset(offset).
		Find(&list).Error
	return toConversations(list), int(total), err
}

// ListConversationsBySpace returns the space's conversations, newest first.
func (s *Store) ListConversationsBySpace(ctx context.Context, spaceID string, limit, offset int) ([]coreconv.Conversation, int, error) {
	limit, offset = capPage(limit, offset)
	spaceKey, err := lookupKey(ctx, s.db, "space", spaceID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := s.db.WithContext(ctx).Model(&conversationRow{}).
		Where("space_id = ?", spaceKey).
		Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []conversationReadRow
	err = s.conversationSelect(ctx).
		Where("conversation.space_id = ?", spaceKey).
		Order("conversation.created_at DESC").
		Limit(limit).Offset(offset).
		Find(&list).Error
	return toConversations(list), int(total), err
}

// UpdateConversationTitle sets the title for the conversation (e.g. after first-round LLM generation).
func (s *Store) UpdateConversationTitle(ctx context.Context, conversationID, title string) error {
	id, ok := util.CanonicalPublicID(conversationID)
	if !ok {
		return apierr.ErrNotFound
	}
	return s.db.WithContext(ctx).Model(&conversationRow{}).
		Where("public_id = ?", id).
		Update("title", title).Error
}
