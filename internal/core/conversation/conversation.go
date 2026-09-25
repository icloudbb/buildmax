package conversation

import (
	"context"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
)

// ErrStaleTurnWrite is returned by AppendMessage when the write carries a
// fencing token below the highest the conversation has already accepted: the
// turn's cross-replica lease expired and a newer holder took over, so this
// writer is stale and must not append behind it. See
// docs/design/server-coordination.md §7.
var ErrStaleTurnWrite = apierr.New(apierr.KindConflict, "conversation turn superseded by a newer holder")

// Conversation is the Tier 1 conversation container.
type Conversation struct {
	ID      string `json:"id"`
	UserID  string `json:"user_id"`
	SpaceID string `json:"space_id,omitempty"`
	Channel string `json:"channel"`
	// ChannelRef addresses the chat a platform-carried conversation belongs to.
	// Empty for Portal and webhook conversations.
	ChannelRef string    `json:"channel_ref,omitempty"`
	Title      string    `json:"title,omitempty"`
	CreatedBy  string    `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
}

// Message is one message in a Tier 1 conversation.
type Message struct {
	ID             string  `json:"id"`
	ConversationID string  `json:"conversation_id"`
	Role           string  `json:"role"`
	Content        string  `json:"content"`
	Channel        *string `json:"channel,omitempty"`
	ToolCallID     *string `json:"tool_call_id,omitempty"`
	ToolCallsJSON  *string `json:"tool_calls,omitempty"`
	// ProviderStateJSON is opaque reasoning state for an assistant message,
	// stored and replayed but never read here. See core/llm.ProviderState.
	ProviderStateJSON *string `json:"provider_state,omitempty"`
	// PartsJSON is non-text content on the message, stored as the canonical
	// part list. Content remains the text describing it.
	PartsJSON *string   `json:"parts,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// AppendInput is one message to store.
//
// It is a struct because the column set grows as the LLM contract does, and a
// positional list this long stops saying which nil means what.
type AppendInput struct {
	ConversationID string
	Role           string
	Content        string
	Channel        *string
	ToolCallID     *string
	ToolCallsJSON  *string
	// ProviderStateJSON is set only for an assistant message from a protocol
	// that produced reasoning state.
	ProviderStateJSON *string
	// PartsJSON is set when the message carries non-text content.
	PartsJSON *string
	// Fence is the turn's cross-replica lease token. When above zero the store
	// rejects the write with ErrStaleTurnWrite if the conversation has already
	// accepted a higher token, so a holder that resumed after its lease expired
	// cannot append behind the replica that took over. Zero disables the check,
	// which is the single-instance (local coordination) path where the process's
	// own turn queue is the only serialization.
	Fence int64
}

// Store provides Tier 1 conversation persistence. Conversations are user-scoped.
type Store interface {
	CreateConversation(ctx context.Context, userID, channel, createdBy string) (*Conversation, error)
	CreateConversationInSpace(ctx context.Context, spaceID, userID, channel, createdBy string) (*Conversation, error)
	GetConversation(ctx context.Context, conversationID string) (*Conversation, error)
	ListConversationsByUser(ctx context.Context, userID string, limit, offset int) ([]Conversation, int, error)
	ListConversationsBySpace(ctx context.Context, spaceID string, limit, offset int) ([]Conversation, int, error)
	UpdateConversationTitle(ctx context.Context, conversationID, title string) error
}

// MessageStore provides Tier 1 conversation message persistence.
// For role=assistant with tool calls, toolCallsJSON should be the JSON-encoded array of tool calls (id, name, arguments).
type MessageStore interface {
	AppendMessage(ctx context.Context, in AppendInput) (*Message, error)
	ListMessages(ctx context.Context, conversationID string) ([]Message, error)
	// GetMessage returns one message by handle, or (nil, nil) when there is no
	// such row. It exists because a run names the message that asked for it,
	// and reading a whole transcript to resolve one handle is the wrong shape
	// for a question about one run.
	GetMessage(ctx context.Context, messageID string) (*Message, error)
}
