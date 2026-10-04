package conversation

import (
	"context"
	"errors"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"
	"github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/icloudbb/buildmax/internal/mock"
	convchannel "github.com/icloudbb/buildmax/internal/service/conversation/channel"
)

// turnBinding is one identity a turn asked the model to bind.
type turnBinding struct {
	userID, spaceID, conversationID, model string
}

// fixedModel hands every turn the same client and records who it was bound to.
type fixedModel struct {
	client llm.LLMClient
	err    error
	bound  []turnBinding
}

func (m *fixedModel) ForConversation(_ context.Context, userID, spaceID, conversationID, model string) (llm.LLMClient, error) {
	m.bound = append(m.bound, turnBinding{userID, spaceID, conversationID, model})
	if m.err != nil {
		return nil, m.err
	}
	return m.client, nil
}

// countingClient answers every call with fixed text and counts the calls.
type countingClient struct {
	reply string
	calls int
}

func (c *countingClient) ChatCompletionBlocking(context.Context, llm.Request) (llm.Completion, error) {
	c.calls++
	return llm.Completion{Content: c.reply}, nil
}

func (c *countingClient) ChatCompletionStreaming(ctx context.Context, req llm.Request, _ func(string)) (llm.Completion, error) {
	return c.ChatCompletionBlocking(ctx, req)
}

func (c *countingClient) ContextWindow() int { return 0 }

// A turn's calls are metered to whoever the model was bound to, so the binding
// has to name the person taking the turn and the conversation's own Space —
// the Space is read from the conversation, never from the caller. The title of
// a new conversation is one of the turn's calls and goes through the same
// client, which is what puts its tokens in the ledger with the reply's.
func TestHandleTurnBindsTheModelToTheTurnAndTitlesThroughIt(t *testing.T) {
	const conversationID = "conv-1"
	client := &countingClient{reply: "Flaky test triage"}
	model := &fixedModel{client: client}
	conversations := &mock.MockConversationStore{Conversations: []coreconv.Conversation{{ID: conversationID, SpaceID: "tm_team", Channel: convchannel.ChannelPortal}}}
	svc := &Service{
		ConversationStore: conversations,
		MessageStore:      &mock.MockConversationMessageStore{},
		Model:             model,
	}

	if _, err := svc.HandleTurn(context.Background(), HandleTurnCmd{
		UserID:         "u_member",
		Channel:        convchannel.ChannelPortal,
		Message:        "why is the test flaky?",
		ConversationID: conversationID,
	}); err != nil {
		t.Fatalf("HandleTurn: %v", err)
	}

	want := []turnBinding{{"u_member", "tm_team", conversationID, ""}}
	if len(model.bound) != 1 || model.bound[0] != want[0] {
		t.Fatalf("bound = %+v, want %+v", model.bound, want)
	}
	if client.calls != 2 {
		t.Errorf("calls through the turn's client = %d, want 2 (the reply and the title)", client.calls)
	}
	if got := conversations.Conversations[0].Title; got != "Flaky test triage" {
		t.Errorf("title = %q, want the one generated through the turn's client", got)
	}
}

// A model that cannot serve the turn — a disabled target, a missing catalog —
// fails the turn before the person's message is written, so a retry does not
// leave the same message twice in the history.
func TestHandleTurnFailsBeforeWritingWhenTheModelCannotBind(t *testing.T) {
	const conversationID = "conv-1"
	messages := &mock.MockConversationMessageStore{}
	refused := apierr.New(apierr.KindNotConfigured, "conversation model disabled")
	svc := &Service{
		ConversationStore: &mock.MockConversationStore{Conversations: []coreconv.Conversation{{ID: conversationID, SpaceID: "tm_team", Channel: convchannel.ChannelPortal}}},
		MessageStore:      messages,
		Model:             &fixedModel{err: refused},
	}

	_, err := svc.HandleTurn(context.Background(), HandleTurnCmd{
		UserID: "u_member", Channel: convchannel.ChannelPortal, Message: "hello", ConversationID: conversationID,
	})
	if !errors.Is(err, refused) {
		t.Fatalf("err = %v, want the model's refusal", err)
	}
	stored, _ := messages.ListMessages(context.Background(), conversationID)
	if len(stored) != 0 {
		t.Errorf("stored %d messages, want none", len(stored))
	}
}
