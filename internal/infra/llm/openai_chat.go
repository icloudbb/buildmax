package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	openai "github.com/sashabaranov/go-openai"
)

// openAIChatAdapter speaks OpenAI Chat Completions. It is the protocol served
// by OpenRouter, LiteLLM, vLLM, and local inference servers, and the default
// for a model entry that names no provider.
type openAIChatAdapter struct {
	client    *openai.Client
	model     string
	maxTokens int
	vision    bool
}

func newOpenAIChatAdapter(cfg Config) *openAIChatAdapter {
	clientConfig := openai.DefaultConfig(cfg.APIKey)
	if cfg.BaseURL != "" {
		clientConfig.BaseURL = cfg.BaseURL
	}
	base := withBuildMaxUserAgent(cfg.HTTPClient, cfg.Surface)
	// This protocol's library does not surface usage from stream chunks, so the
	// transport reads it off the raw SSE. The workaround stays here rather than
	// in the shared layer: the other two protocols report usage in their own
	// event streams and need nothing like it.
	clientConfig.HTTPClient = &usageCaptureHTTPClient{base: base}
	return &openAIChatAdapter{
		client:    openai.NewClientWithConfig(clientConfig),
		model:     cfg.Model,
		maxTokens: cfg.MaxTokens,
		vision:    cfg.Vision,
	}
}

func (a *openAIChatAdapter) name() string { return cllm.ProviderOpenAICompatible }

func (a *openAIChatAdapter) buildRequest(req cllm.Request) openai.ChatCompletionRequest {
	messages, tools := req.Messages, req.Tools
	openaiMsgs := make([]openai.ChatCompletionMessage, 0, len(messages))
	for _, m := range messages {
		openaiMsgs = append(openaiMsgs, toOpenAIMessage(m))
		// This protocol takes images only in a user message, so an image a tool
		// returned follows the result as its own turn. The tool result itself
		// stays text, which is what the protocol requires.
		if a.vision {
			if follow, ok := imageFollowUpMessage(m); ok {
				openaiMsgs = append(openaiMsgs, follow)
			}
		}
	}
	openaiTools := make([]openai.Tool, 0, len(tools))
	for _, t := range tools {
		openaiTools = append(openaiTools, toOpenAITool(t))
	}
	built := openai.ChatCompletionRequest{
		Model:     a.model,
		Messages:  openaiMsgs,
		Tools:     openaiTools,
		MaxTokens: a.maxTokens,
	}
	if req.Output != nil {
		// json_schema with strict is this protocol's native structured-output
		// mechanism. The Client re-validates the result regardless.
		built.ResponseFormat = &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONSchema,
			JSONSchema: &openai.ChatCompletionResponseFormatJSONSchema{
				Name:   req.Output.Name,
				Schema: json.RawMessage(req.Output.Schema),
				Strict: true,
			},
		}
	}
	return built
}

func toOpenAIMessage(m cllm.Message) openai.ChatCompletionMessage {
	msg := openai.ChatCompletionMessage{
		Role:       m.Role,
		Content:    m.Content,
		ToolCallID: m.ToolCallID,
	}
	// The library refuses a message that sets both content forms, so a user
	// turn with images moves entirely into parts.
	if m.Role == "user" {
		if images := m.Images(); len(images) > 0 {
			parts := make([]openai.ChatMessagePart, 0, len(images)+1)
			if m.Content != "" {
				parts = append(parts, openai.ChatMessagePart{
					Type: openai.ChatMessagePartTypeText,
					Text: m.Content,
				})
			}
			for _, image := range images {
				parts = append(parts, openai.ChatMessagePart{
					Type:     openai.ChatMessagePartTypeImageURL,
					ImageURL: &openai.ChatMessageImageURL{URL: dataURL(image)},
				})
			}
			msg.Content = ""
			msg.MultiContent = parts
		}
	}
	if len(m.ToolCalls) > 0 {
		msg.ToolCalls = make([]openai.ToolCall, 0, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, openai.ToolCall{
				ID:   tc.ID,
				Type: openai.ToolTypeFunction,
				Function: openai.FunctionCall{
					Name:      tc.Name,
					Arguments: tc.Arguments,
				},
			})
		}
	}
	return msg
}

// imageFollowUpMessage builds the user turn that carries a tool result's images.
//
// It exists because neither OpenAI protocol accepts image content on a tool
// message, and dropping the image silently would leave the model answering
// about something it was never shown.
func imageFollowUpMessage(m cllm.Message) (openai.ChatCompletionMessage, bool) {
	images := m.Images()
	if len(images) == 0 {
		return openai.ChatCompletionMessage{}, false
	}
	parts := make([]openai.ChatMessagePart, 0, len(images)+1)
	parts = append(parts, openai.ChatMessagePart{
		Type: openai.ChatMessagePartTypeText,
		Text: imageFollowUpPreamble,
	})
	for _, image := range images {
		parts = append(parts, openai.ChatMessagePart{
			Type:     openai.ChatMessagePartTypeImageURL,
			ImageURL: &openai.ChatMessageImageURL{URL: dataURL(image)},
		})
	}
	return openai.ChatCompletionMessage{Role: "user", MultiContent: parts}, true
}

func toOpenAITool(t cllm.ToolDef) openai.Tool {
	return openai.Tool{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		},
	}
}

// chatUsage maps reported tokens. This protocol caches automatically, and
// reports the cached part as a breakdown of the prompt rather than in addition
// to it. It has no cache-write count of its own.
func chatUsage(usage openai.Usage) cllm.Usage {
	out := cllm.Usage{
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
	}
	if usage.PromptTokensDetails != nil {
		out.CacheReadTokens = usage.PromptTokensDetails.CachedTokens
	}
	return out
}

func toToolCalls(toolCalls []openai.ToolCall) []cllm.ToolCall {
	if len(toolCalls) == 0 {
		return nil
	}
	out := make([]cllm.ToolCall, 0, len(toolCalls))
	for _, tc := range toolCalls {
		out = append(out, cllm.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}
	return out
}

// openAIAPIError converts a go-openai failure into the neutral error the shared
// retry and classification logic reads. Both OpenAI protocols use it, because
// both are served by the same library.
func openAIAPIError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var apiErr *openai.APIError
	if errors.As(err, &apiErr) {
		return &apiError{status: apiErr.HTTPStatusCode, message: apiErr.Message, err: err}
	}
	var reqErr *openai.RequestError
	if errors.As(err, &reqErr) {
		return &apiError{status: reqErr.HTTPStatusCode, err: err}
	}
	return &apiError{err: err}
}

func (a *openAIChatAdapter) blocking(ctx context.Context, req cllm.Request) (cllm.Completion, error) {
	resp, err := a.client.CreateChatCompletion(ctx, a.buildRequest(req))
	if err != nil {
		return cllm.Completion{}, fmt.Errorf("chat completion: %w", openAIAPIError(err))
	}
	if len(resp.Choices) == 0 {
		return cllm.Completion{}, fmt.Errorf("no choices in response")
	}
	msg := resp.Choices[0].Message
	// This protocol carries no reasoning state, so a completion from it never
	// sets ProviderState.
	return cllm.Completion{
		Content:    msg.Content,
		ToolCalls:  toToolCalls(msg.ToolCalls),
		Usage:      chatUsage(resp.Usage),
		Structured: nativeCandidate(req, msg.Content),
	}, nil
}

func (a *openAIChatAdapter) streaming(ctx context.Context, req cllm.Request, onDelta func(string)) (cllm.Completion, error) {
	var streamUsage cllm.Usage
	ctx = context.WithValue(ctx, streamUsageKey, &streamUsage)
	stream, err := a.client.CreateChatCompletionStream(ctx, a.buildRequest(req))
	if err != nil {
		return cllm.Completion{}, fmt.Errorf("chat completion stream: %w", openAIAPIError(err))
	}
	defer func() { _ = stream.Close() }()

	var fullContent strings.Builder
	accum := newToolCallAccumulator()
	for {
		resp, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return cllm.Completion{Content: fullContent.String()}, fmt.Errorf("stream recv: %w", openAIAPIError(err))
		}
		if len(resp.Choices) == 0 {
			continue
		}
		// Usage is captured from raw SSE by usageCaptureTransport when the provider sends it.
		delta := resp.Choices[0].Delta
		if delta.Content != "" {
			fullContent.WriteString(delta.Content)
			if onDelta != nil {
				onDelta(delta.Content)
			}
		}
		for _, tc := range delta.ToolCalls {
			idx := 0
			if tc.Index != nil {
				idx = *tc.Index
			}
			accum.add(idx, tc.ID, tc.Function.Name, tc.Function.Arguments)
		}
	}
	return cllm.Completion{
		Content:    fullContent.String(),
		ToolCalls:  accum.toolCalls(),
		Usage:      streamUsage,
		Structured: nativeCandidate(req, fullContent.String()),
	}, nil
}
