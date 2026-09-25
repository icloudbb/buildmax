package handlers

import (
	"context"
	"errors"

	"github.com/icloudbb/buildmax/internal/server/turnqueue"
	chansvc "github.com/icloudbb/buildmax/internal/service/channel"
	"github.com/icloudbb/buildmax/internal/service/conversation"
)

var _ chansvc.TurnRunner = (*Handler)(nil)

// RunChannelTurn runs one chat-platform turn through the same turn queue and
// Tier 1 Conversation service a Portal message uses, so a chat turn and a
// Portal turn on one conversation are serialized against each other and behave
// the same. The refusals a chat can act on come back as the gateway's own
// errors.
func (h *Handler) RunChannelTurn(ctx context.Context, conversationID, userID, channel, message string) (string, error) {
	var (
		result  conversation.ConversationResult
		turnErr error
	)
	err := h.turns.RunSync(ctx, conversationID, func(fence int64) {
		result, turnErr = h.conversations.HandleTurn(ctx, conversation.HandleTurnCmd{
			UserID:         userID,
			Channel:        channel,
			Message:        message,
			ConversationID: conversationID,
			Fence:          fence,
		})
	})
	switch {
	case errors.Is(err, turnqueue.ErrQueueFull):
		return "", chansvc.ErrBusy
	case errors.Is(err, turnqueue.ErrDraining):
		return "", chansvc.ErrRestarting
	case err != nil:
		return "", err
	}
	return result.Reply, turnErr
}
