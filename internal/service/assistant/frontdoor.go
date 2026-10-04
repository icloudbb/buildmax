package assistant

import (
	"context"
	"log/slog"

	coreassistant "github.com/icloudbb/buildmax/internal/core/assistant"
	corechannel "github.com/icloudbb/buildmax/internal/core/channel"
)

const genericFailure = "Something went wrong on the BuildMax side. Please try again in a moment."

// FrontDoor answers linked people who message an Assistant's bot. It is the
// chat Gateway's channel.FrontDoor.
type FrontDoor struct {
	Service *Service
}

// Answer implements channel.FrontDoor. The Gateway has identified the sender
// and checked their sign-in; whether this Assistant answers at all is decided
// here, before any model runs.
func (f FrontDoor) Answer(ctx context.Context, connectorKey string, _ corechannel.Inbound, _ string) string {
	s := f.Service
	if s.ready() != nil {
		return "This assistant is not available."
	}
	b, err := s.Store.GetBinding(ctx, connectorKey)
	if err != nil {
		slog.Error("assistant binding lookup failed", "binding_id", connectorKey, "err", err)
		return genericFailure
	}
	if b == nil {
		return "This assistant is not available."
	}
	a, err := s.Store.GetAssistant(ctx, b.AssistantID)
	if err != nil {
		slog.Error("assistant lookup failed", "assistant_id", b.AssistantID, "err", err)
		return genericFailure
	}
	if a == nil {
		return "This assistant is not available."
	}
	avail, err := s.Availability(ctx, a)
	if err != nil {
		slog.Error("assistant availability check failed", "assistant_id", a.ID, "err", err)
		return genericFailure
	}
	if avail != coreassistant.Available {
		return "This assistant is paused. Please try again later."
	}
	// Answering requesters is the front-door turn
	// (docs/backlog/66-assistant-front-door-turn.md).
	return "This assistant is not answering questions yet."
}
