package mock

import (
	"context"
	"fmt"
	"sync"
	"time"

	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"
)

// MockConversationStore is an in-memory ConversationStore for tests.
type MockConversationStore struct {
	Conversations []coreconv.Conversation
}

func (m *MockConversationStore) CreateConversation(_ context.Context, userID, channel, createdBy string) (*coreconv.Conversation, error) {
	return m.CreateConversationInSpace(context.Background(), "tm_personal", userID, channel, createdBy)
}

func (m *MockConversationStore) CreateConversationInSpace(_ context.Context, spaceID, userID, channel, createdBy string) (*coreconv.Conversation, error) {
	conv := coreconv.Conversation{
		ID:        fmt.Sprintf("v_%d", len(m.Conversations)+1),
		UserID:    userID,
		SpaceID:   spaceID,
		Channel:   channel,
		CreatedBy: createdBy,
		CreatedAt: time.Now().UTC(),
	}
	m.Conversations = append(m.Conversations, conv)
	return &m.Conversations[len(m.Conversations)-1], nil
}

func (m *MockConversationStore) GetConversation(_ context.Context, conversationID string) (*coreconv.Conversation, error) {
	for i := range m.Conversations {
		if m.Conversations[i].ID == conversationID {
			return &m.Conversations[i], nil
		}
	}
	return nil, nil
}

func (m *MockConversationStore) ListConversationsByUser(_ context.Context, userID string, limit, offset int) ([]coreconv.Conversation, int, error) {
	var out []coreconv.Conversation
	for _, conv := range m.Conversations {
		if conv.UserID == userID {
			out = append(out, conv)
		}
	}
	total := len(out)
	if offset > total {
		return []coreconv.Conversation{}, total, nil
	}
	if limit <= 0 || offset+limit > total {
		limit = total - offset
	}
	return out[offset : offset+limit], total, nil
}

func (m *MockConversationStore) ListConversationsBySpace(_ context.Context, spaceID string, limit, offset int) ([]coreconv.Conversation, int, error) {
	var out []coreconv.Conversation
	for _, conv := range m.Conversations {
		if conv.SpaceID == spaceID {
			out = append(out, conv)
		}
	}
	total := len(out)
	if offset > total {
		return []coreconv.Conversation{}, total, nil
	}
	if limit <= 0 || offset+limit > total {
		limit = total - offset
	}
	return out[offset : offset+limit], total, nil
}

func (m *MockConversationStore) UpdateConversationTitle(_ context.Context, conversationID, title string) error {
	for i := range m.Conversations {
		if m.Conversations[i].ID == conversationID {
			m.Conversations[i].Title = title
			return nil
		}
	}
	return nil
}

// MockConversationMessageStore is an in-memory ConversationMessageStore.
//
// Every appended message gets its own handle. That matters more than it looks:
// a run records the message that asked for it, and a store handing out one
// shared ID would let that assertion pass on any message at all.
type MockConversationMessageStore struct {
	mu       sync.Mutex
	Messages []coreconv.Message
}

func (m *MockConversationMessageStore) AppendMessage(_ context.Context, in coreconv.AppendInput) (*coreconv.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	msg := coreconv.Message{
		ID:                fmt.Sprintf("cm_mock_%d", len(m.Messages)+1),
		ConversationID:    in.ConversationID,
		Role:              in.Role,
		Content:           in.Content,
		Channel:           in.Channel,
		ToolCallID:        in.ToolCallID,
		ToolCallsJSON:     in.ToolCallsJSON,
		ProviderStateJSON: in.ProviderStateJSON,
		PartsJSON:         in.PartsJSON,
		AssistantRevision: in.AssistantRevision,
		CreatedAt:         seqTime(len(m.Messages) + 1),
	}
	m.Messages = append(m.Messages, msg)
	return &msg, nil
}

func (m *MockConversationMessageStore) ListMessages(_ context.Context, conversationID string) ([]coreconv.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]coreconv.Message, 0, len(m.Messages))
	for _, msg := range m.Messages {
		if msg.ConversationID == conversationID {
			out = append(out, msg)
		}
	}
	return out, nil
}

func (m *MockConversationMessageStore) GetMessage(_ context.Context, messageID string) (*coreconv.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.Messages {
		if m.Messages[i].ID == messageID {
			msg := m.Messages[i]
			return &msg, nil
		}
	}
	return nil, nil
}
