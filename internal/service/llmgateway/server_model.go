package llmgateway

import (
	"context"
	"errors"
	"fmt"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
)

// ServerModel is the model the server answers Tier 1 conversations with.
//
// It hands out clients rather than being one, because every call has to be
// attributed before it is made: a turn's calls cost the same money as a
// worker's, and a server-side client with no identity is how they once reached
// the provider without a ledger row or a quota check. Each client runs its
// calls through Service in process, so they share the worker's metering,
// ledger, and soft quota enforcement without an HTTP self-call.
type ServerModel struct {
	Service *Service
	// TargetID is the catalog target conversation.model_target (or the
	// derived conversation.model) resolved to at startup.
	TargetID string
}

// ForConversation returns a client whose every call is recorded against the
// conversation, the person taking the turn, and the conversation's space.
// model is a catalog model name a Space Assistant chose; empty uses the
// deployment's conversation model.
//
// The target is resolved now, so a turn on a disabled or deleted model fails
// before it writes anything, and so the client can report the target's context
// window to the loop that compacts against it.
func (m *ServerModel) ForConversation(ctx context.Context, userID, spaceID, conversationID, model string) (cllm.LLMClient, error) {
	if m == nil || m.Service == nil || m.Service.Router == nil || m.Service.Router.Resolver == nil {
		return nil, ErrCatalogNotConfigured
	}
	resolver := m.Service.Router.Resolver
	var target Target
	if model == "" {
		t, err := resolver.ResolveTargetByID(ctx, m.TargetID, BaselineCapabilities())
		if err != nil {
			return nil, fmt.Errorf("conversation model %q: %w", m.TargetID, err)
		}
		target = t
	} else {
		res, err := resolver.Resolve(ctx, ResolveRequest{Name: model, Requires: BaselineCapabilities()})
		if err != nil {
			return nil, fmt.Errorf("conversation model %q: %w", model, err)
		}
		target = res.Target
	}
	return &serverModelClient{
		service:       m.Service,
		contextWindow: target.ContextWindow,
		request: CompleteRequest{
			SpaceID:        spaceID,
			UserID:         &userID,
			ConversationID: &conversationID,
			Surface:        coregw.CallSurfaceConversation,
			TargetID:       target.ID,
		},
	}, nil
}

// serverModelClient is a cllm.LLMClient bound to one conversation's identity.
type serverModelClient struct {
	service       *Service
	contextWindow int
	request       CompleteRequest
}

func (c *serverModelClient) ChatCompletionBlocking(ctx context.Context, req cllm.Request) (cllm.Completion, error) {
	result, err := c.service.Complete(ctx, c.bind(req))
	if err != nil {
		return cllm.Completion{}, serverModelError(err)
	}
	return completionOf(result), nil
}

func (c *serverModelClient) ChatCompletionStreaming(ctx context.Context, req cllm.Request, onDelta func(string)) (cllm.Completion, error) {
	result, err := c.service.Stream(ctx, c.bind(req), onDelta)
	if err != nil {
		return cllm.Completion{}, serverModelError(err)
	}
	return completionOf(result), nil
}

func (c *serverModelClient) ContextWindow() int { return c.contextWindow }

// bind copies the turn's identity onto one request. The request's CacheScope
// is not carried: the gateway scopes the provider cache by the space itself.
func (c *serverModelClient) bind(req cllm.Request) CompleteRequest {
	out := c.request
	out.Messages = req.Messages
	out.Tools = req.Tools
	out.CallProfile = req.Profile
	out.Output = req.Output
	return out
}

func completionOf(result CompleteResult) cllm.Completion {
	return cllm.Completion{
		Content:       result.Content,
		ToolCalls:     result.ToolCalls,
		Usage:         result.Usage,
		ProviderState: result.ProviderState,
		Structured:    result.Structured,
	}
}

// serverModelError turns a quota refusal into the error every conversation
// transport already answers: a 429 over HTTP, the reason itself in a chat
// app. Anything else keeps its chain, so the loop still recognizes a provider
// failure it knows how to handle.
func serverModelError(err error) error {
	var quota *QuotaError
	if errors.As(err, &quota) {
		return apierr.New(apierr.KindQuotaExceeded, quota.Reason)
	}
	return err
}
