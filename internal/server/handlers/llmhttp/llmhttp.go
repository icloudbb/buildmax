// Package llmhttp presents the managed gateway over HTTP.
//
// Shared because two routes do it: a space calls the gateway for itself, and a
// worker calls it on behalf of a run. The SSE framing, the status a refusal
// maps to, and the rule that a provider's own error body never reaches a client
// must be one answer for both, not two that happen to agree.
package llmhttp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/icloudbb/buildmax/internal/infra/llmwire"
	"github.com/icloudbb/buildmax/internal/server/httputil"
	"github.com/icloudbb/buildmax/internal/service/llmgateway"
)

// sseWriter frames a managed completion as server-sent events.
//
// It holds no buffer of its own: a delta reaches the network as soon as it
// arrives, which is what makes a managed stream feel like a direct one.
type sseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	started bool
}

func (s *sseWriter) send(event string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if !s.started {
		s.started = true
		header := s.w.Header()
		header.Set("Content-Type", "text/event-stream")
		header.Set("Cache-Control", "no-cache")
		header.Set("Connection", "keep-alive")
		// Reverse proxies in supported deployments must not buffer this
		// response; nginx honours the header, others need configuration.
		header.Set("X-Accel-Buffering", "no")
		s.w.WriteHeader(http.StatusOK)
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, body); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// Response headers are written on the first event, not up front. A call refused
// before any output — an unknown model, quota, a duplicate call ID — is still a
// plain HTTP error with a status the client can act on; only a failure after
// the stream has started becomes an error event.
// Stream runs one managed call and frames it as server-sent events.
func Stream(w http.ResponseWriter, r *http.Request, gateway *llmgateway.Service, cmd llmgateway.CompleteRequest, spaceID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		httputil.WriteJSONError(w, http.StatusInternalServerError, "streaming is not supported by this server")
		return
	}

	stream := &sseWriter{w: w, flusher: flusher}
	result, err := gateway.Stream(r.Context(), cmd, func(delta string) {
		if delta == "" {
			return
		}
		_ = stream.send(llmwire.EventDelta, llmwire.DeltaEvent{Content: delta})
	})

	if err != nil {
		if !stream.started {
			WriteError(w, err, "llm_completions_stream", spaceID)
			return
		}
		class := llmgateway.ErrorClassFor(err)
		_, message := statusFor(class, err)
		_ = stream.send(llmwire.EventError, llmwire.ErrorEvent{
			Code:      class,
			Error:     message,
			Retryable: llmgateway.RetryableClass(class),
		})
		return
	}

	final := llmwire.CompletionResponse{
		LLMCallID:     result.LLMCallID,
		Model:         result.Model,
		Content:       result.Content,
		ToolCalls:     WireToolCalls(result.ToolCalls),
		ProviderState: WireProviderState(result.ProviderState),
	}
	if result.UsageReported {
		final.Usage = &llmwire.Usage{
			PromptTokens:     result.Usage.PromptTokens,
			CompletionTokens: result.Usage.CompletionTokens,
			TotalTokens:      result.Usage.TotalTokens,
			CacheReadTokens:  result.Usage.CacheReadTokens,
			CacheWriteTokens: result.Usage.CacheWriteTokens,
		}
	}
	_ = stream.send(llmwire.EventResult, final)
}

// requireLLMGateway reports whether managed inference is configured.
// RequireGateway reports whether managed inference is configured.
func RequireGateway(w http.ResponseWriter, gateway *llmgateway.Service) bool {
	if gateway == nil {
		httputil.WriteJSONError(w, http.StatusServiceUnavailable, "llm gateway not configured")
		return false
	}
	return true
}

// writeLLMGatewayError maps a stable class onto an HTTP status. Provider error
// bodies never reach this path — only BuildMax classifications do.
// WriteError maps a stable class onto a status. Provider error bodies never
// reach this path -- only BuildMax classifications do.
func WriteError(w http.ResponseWriter, err error, handlerName, spaceID string) {
	class := llmgateway.ErrorClassFor(err)
	status, message := statusFor(class, err)
	if status == http.StatusInternalServerError {
		httputil.WriteInternalError(w, err, "handler error", "handler", handlerName, "space_id", spaceID, "code", class)
		return
	}
	httputil.WriteJSON(w, status, llmwire.ErrorResponse{Error: message, Code: class})
}
func statusFor(class string, err error) (int, string) {
	switch class {
	case llmgateway.ErrorClassInvalidRequest:
		return http.StatusBadRequest, err.Error()
	case llmgateway.ErrorClassTargetNotFound:
		return http.StatusBadRequest, "model is not in this deployment's catalog"
	case llmgateway.ErrorClassTargetDisabled:
		return http.StatusBadRequest, "model is disabled"
	case llmgateway.ErrorClassCapability:
		return http.StatusBadRequest, err.Error()
	case llmgateway.ErrorClassQuotaExceeded:
		return http.StatusTooManyRequests, err.Error()
	case llmgateway.ErrorClassDuplicateCall:
		// The caller does not know whether its first attempt landed. Saying
		// "already used" answers that; it is not an invitation to retry.
		return http.StatusConflict, err.Error()
	case llmgateway.ErrorClassNotConfigured:
		return http.StatusServiceUnavailable, "llm gateway not configured"
	case llmgateway.ErrorClassCanceled:
		return http.StatusRequestTimeout, "call canceled"
	case llmgateway.ErrorClassUpstream:
		// The provider's own message stays server-side — the gateway service
		// logs it with the ledger row's ID — because it can carry account
		// identifiers, endpoints, and request fragments.
		return http.StatusBadGateway, "model provider unavailable"
	default:
		return http.StatusInternalServerError, "internal error"
	}
}

// WireToolCalls converts core tool calls to the wire contract.
func WireToolCalls(in []cllm.ToolCall) []llmwire.ToolCall {
	if len(in) == 0 {
		return nil
	}
	out := make([]llmwire.ToolCall, 0, len(in))
	for _, tc := range in {
		out = append(out, llmwire.ToolCall{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments})
	}
	return out
}

// WireProviderState converts core provider state to the wire contract.
func WireProviderState(in *cllm.ProviderState) *llmwire.ProviderState {
	if in == nil {
		return nil
	}
	return &llmwire.ProviderState{Protocol: in.Protocol, Data: in.Data}
}

// CoreMessages converts wire messages to the core contract.
// CallProfile validates the profile a managed caller named.
//
// Shared by the space route, the worker route, and any other endpoint that
// serves a managed call, because a profile that means one thing on one route
// and another on the next is exactly the drift the gateway exists to prevent.
//
// An empty profile is accepted: the field is additive, and a client that
// predates it is not a client making a bad request. It stays empty rather than
// being promoted to a default, so the policy resolver sees "no evidence of
// reuse" instead of a claim the caller never made.
//
// A non-empty value this build does not know is rejected. The alternative —
// quietly treating it as something else — would let a newer client believe it
// had asked for one thing while being charged for another.
func CallProfile(named string) (cllm.CallProfile, error) {
	if named == "" {
		return "", nil
	}
	profile := cllm.CallProfile(named)
	if !profile.Valid() {
		return "", fmt.Errorf("unknown call_profile %q", named)
	}
	return profile, nil
}

func CoreMessages(in []llmwire.Message) ([]cllm.Message, error) {
	if len(in) == 0 {
		return nil, errors.New("messages required")
	}
	out := make([]cllm.Message, 0, len(in))
	for _, m := range in {
		if m.Role == "" {
			return nil, errors.New("every message needs a role")
		}
		out = append(out, cllm.Message{
			Role:          m.Role,
			Content:       m.Content,
			ToolCallID:    m.ToolCallID,
			ToolCalls:     toCoreToolCalls(m.ToolCalls),
			ProviderState: toCoreProviderState(m.ProviderState),
			Parts:         toCoreParts(m.Parts),
		})
	}
	return out, nil
}

// CoreTools converts wire tool definitions to the core contract.
func CoreTools(in []llmwire.Tool) []cllm.ToolDef {
	if len(in) == 0 {
		return nil
	}
	out := make([]cllm.ToolDef, 0, len(in))
	for _, t := range in {
		def := cllm.ToolDef{Name: t.Name, Description: t.Description}
		if len(t.Parameters) > 0 {
			var params any
			if err := json.Unmarshal(t.Parameters, &params); err == nil {
				def.Parameters = params
			}
		}
		out = append(out, def)
	}
	return out
}

func toCoreToolCalls(in []llmwire.ToolCall) []cllm.ToolCall {
	if len(in) == 0 {
		return nil
	}
	out := make([]cllm.ToolCall, 0, len(in))
	for _, tc := range in {
		out = append(out, cllm.ToolCall{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments})
	}
	return out
}

// toCoreProviderState and fromCoreProviderState carry reasoning state across
// the gateway boundary. The server relays it without reading it: the content is
// the upstream's, and only the adapter that produced it may interpret it.
func toCoreProviderState(in *llmwire.ProviderState) *cllm.ProviderState {
	if in == nil {
		return nil
	}
	return &cllm.ProviderState{Protocol: in.Protocol, Data: in.Data}
}
func toCoreParts(in []llmwire.ContentPart) []cllm.ContentPart {
	if len(in) == 0 {
		return nil
	}
	out := make([]cllm.ContentPart, 0, len(in))
	for _, part := range in {
		out = append(out, cllm.ContentPart{
			Type: part.Type, Text: part.Text, MediaType: part.MediaType, Data: part.Data,
		})
	}
	return out
}
