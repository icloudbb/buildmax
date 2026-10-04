package handlers

import (
	"context"
	"errors"

	"github.com/icloudbb/buildmax/internal/server/turnqueue"
	assistantsvc "github.com/icloudbb/buildmax/internal/service/assistant"
	chansvc "github.com/icloudbb/buildmax/internal/service/channel"
	"github.com/icloudbb/buildmax/internal/service/conversation"
)

var (
	_ chansvc.TurnRunner = (*Handler)(nil)
	_ assistantsvc.Turns = (*Handler)(nil)
)

// RunChannelTurn runs one chat-platform turn through the same turn queue and
// Tier 1 Conversation service a Portal message uses, so a chat turn and a
// Portal turn on one conversation are serialized against each other and behave
// the same. The refusals a chat can act on come back as the gateway's own
// errors.
func (h *Handler) RunChannelTurn(ctx context.Context, conversationID, userID, channel, message string) (string, error) {
	return h.runChatTurn(ctx, conversation.HandleTurnCmd{
		UserID: userID, Channel: channel, Message: message, ConversationID: conversationID,
	})
}

// RunAssistantTurn runs one turn of a Space Assistant's conversation the same
// way, with the Assistant's profile; requesterID is the person asking.
func (h *Handler) RunAssistantTurn(ctx context.Context, conversationID, requesterID, channel, message string, a conversation.AssistantTurn) (string, error) {
	return h.runChatTurn(ctx, conversation.HandleTurnCmd{
		UserID: requesterID, Channel: channel, Message: message, ConversationID: conversationID, Assistant: &a,
	})
}

func (h *Handler) runChatTurn(ctx context.Context, cmd conversation.HandleTurnCmd) (string, error) {
	var (
		result  conversation.ConversationResult
		turnErr error
	)
	err := h.turns.RunSync(ctx, cmd.ConversationID, func(fence int64) {
		cmd.Fence = fence
		result, turnErr = h.conversations.HandleTurn(ctx, cmd)
	})
	switch {
	case errors.Is(err, turnqueue.ErrQueueFull):
		return "", chansvc.ErrBusy
	case errors.Is(err, turnqueue.ErrDraining):
		return "", chansvc.ErrRestarting
	case errors.Is(err, turnqueue.ErrCoordinationUnavailable):
		return "", chansvc.ErrUnavailable
	case err != nil:
		return "", err
	}
	return result.Reply, turnErr
}
