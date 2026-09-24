// Package llmremote implements the core LLM contract against a BuildMax
// managed gateway instead of a provider.
//
// It is the client half of internal/infra/llmwire. The agent loop cannot tell
// the difference between this and internal/infra/llm: both satisfy
// core/llm.LLMClient, and the caller never learns which upstream served a call.
//
// Mirrors the design in docs/design/llm-gateway.md.
package llmremote

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/icloudbb/buildmax/internal/config"
	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/icloudbb/buildmax/internal/infra/llmwire"
)

const (
	// maxErrorBodyBytes bounds how much of a failure response is read. A
	// gateway should answer with a small JSON error; anything larger is not
	// worth holding.
	maxErrorBodyBytes = 64 << 10
	// maxSSEEventBytes bounds one stream event. A delta is text and a result is
	// one completion, so a larger frame means a broken peer, not a big answer.
	maxSSEEventBytes = 8 << 20
)

// Config holds managed client settings.
type Config struct {
	// ServerURL is the BuildMax server base URL.
	ServerURL string
	// Token authenticates the caller. It is a BuildMax credential, never a
	// provider key. Prefer TokenFunc; this is for callers with a fixed token.
	Token string
	// TokenFunc supplies the credential for each request, and takes precedence
	// over Token.
	//
	// A client outlives its token. This one is built once and cached for the
	// life of the process, while an access token expires on its own schedule —
	// so a long-running TUI or Desktop session that baked the token in at
	// construction would authenticate for a week and then fail every call
	// afterwards, with no way to recover short of a restart. Asking per request
	// lets the credential layer renew underneath.
	TokenFunc func() (string, error)
	// TaskRunID selects the worker route, where the credential is a run token
	// rather than a user login and the server derives user, space, task, and run
	// from it. Empty means the signed-in user's own route.
	TaskRunID string
	// Model is the catalog model name to call. Empty uses the deployment
	// default.
	Model string
	// ContextWindow is the usable context size for this model; 0 disables
	// windowing. The protocol does not report it per call, so it comes from
	// model discovery or local configuration.
	ContextWindow int
	// Surface labels where the call came from, for correlation only.
	Surface string
	// CallTimeout bounds one request; 0 means no client-side deadline, leaving
	// the server's own per-target timeout in charge.
	CallTimeout time.Duration
	// HTTPClient is optional; http.DefaultClient is used when nil.
	HTTPClient *http.Client
}

// Client calls a BuildMax managed gateway. It satisfies core/llm.LLMClient.
type Client struct {
	cfg        Config
	httpClient *http.Client
}

// NewClient builds a managed client.
func NewClient(cfg Config) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	cfg.ServerURL = strings.TrimRight(cfg.ServerURL, "/")
	return &Client{cfg: cfg, httpClient: httpClient}
}

// setAuthorization puts the current credential on req.
//
// A TokenFunc that fails stops the call rather than sending it unauthenticated.
// An anonymous request to a managed gateway can only be refused, and reporting
// "not logged in" is more use than reporting the 401 that would follow.
func (c *Client) setAuthorization(req *http.Request) error {
	if c.cfg.TokenFunc != nil {
		token, err := c.cfg.TokenFunc()
		if err != nil {
			return fmt.Errorf("managed credential: %w", err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		return nil
	}
	if c.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	}
	return nil
}

// completionsURL picks the route this client's identity entitles it to.
//
// A misconfigured client fails here rather than at the server, so the message
// names what is missing instead of reporting a 401 or a 404 the caller would
// have to interpret.
func (c *Client) completionsURL() (string, error) {
	if c.cfg.ServerURL == "" {
		return "", errors.New("managed llm client needs a server URL")
	}
	if c.cfg.TaskRunID != "" {
		return c.cfg.ServerURL + fmt.Sprintf(llmwire.WorkerCompletionsPath, c.cfg.TaskRunID), nil
	}
	return c.cfg.ServerURL + llmwire.CompletionsPath, nil
}

// ContextWindow returns the configured context window (0 = no windowing).
func (c *Client) ContextWindow() int {
	if c == nil {
		return 0
	}
	return c.cfg.ContextWindow
}

// ChatCompletionBlocking runs one managed call.
func (c *Client) ChatCompletionBlocking(ctx context.Context, req cllm.Request) (cllm.Completion, error) {
	if c == nil {
		return cllm.Completion{}, errors.New("managed llm client is not configured")
	}

	resp, err := c.post(ctx, false, req)
	if err != nil {
		return cllm.Completion{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return cllm.Completion{}, gatewayError(resp)
	}

	var out llmwire.CompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return cllm.Completion{}, fmt.Errorf("decode managed response: %w", err)
	}

	// An absent usage object means the provider reported none. The zero value
	// here says "unknown", the same thing the local client reports when a
	// provider omits usage.
	var usage cllm.Usage
	if out.Usage != nil {
		usage = cllm.Usage{
			PromptTokens:     out.Usage.PromptTokens,
			CompletionTokens: out.Usage.CompletionTokens,
			TotalTokens:      out.Usage.TotalTokens,
			CacheReadTokens:  out.Usage.CacheReadTokens,
			CacheWriteTokens: out.Usage.CacheWriteTokens,
		}
	}
	return cllm.Completion{
		Content:       out.Content,
		ToolCalls:     fromWireToolCalls(out.ToolCalls),
		Usage:         usage,
		ProviderState: fromWireProviderState(out.ProviderState),
	}, nil
}

// ChatCompletionStreaming runs one managed call, delivering content deltas to
// onDelta as they arrive.
//
// It never retries. The server-side provider client owns retry policy and stops
// once a delta has been emitted; adding a retry here would replay output the
// caller has already seen.
func (c *Client) ChatCompletionStreaming(ctx context.Context, req cllm.Request, onDelta func(string)) (cllm.Completion, error) {
	if c == nil {
		return cllm.Completion{}, errors.New("managed llm client is not configured")
	}
	resp, err := c.post(ctx, true, req)
	if err != nil {
		return cllm.Completion{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return cllm.Completion{}, gatewayError(resp)
	}
	return consumeStream(resp.Body, onDelta)
}

// consumeStream reads typed events until the call completes.
//
// A stream that ends without a result or an error event is a failure, not an
// empty answer: silently returning "" would hide a dropped connection as a
// model that had nothing to say.
func consumeStream(body io.Reader, onDelta func(string)) (cllm.Completion, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64<<10), maxSSEEventBytes)

	var state streamState
	var event string
	var data []byte

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:"))...)
		case line == "":
			if event == "" || len(data) == 0 {
				event, data = "", nil
				continue
			}
			done, err := state.apply(event, data, onDelta)
			event, data = "", nil
			if err != nil {
				return cllm.Completion{}, err
			}
			if done {
				return state.finish()
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return cllm.Completion{}, fmt.Errorf("managed stream interrupted: %w", err)
	}
	return cllm.Completion{}, errors.New("managed stream ended without a result")
}

// streamState accumulates one call's events.
type streamState struct {
	content strings.Builder
	result  llmwire.CompletionResponse
}

func (s *streamState) apply(event string, data []byte, onDelta func(string)) (bool, error) {
	switch event {
	case llmwire.EventDelta:
		var delta llmwire.DeltaEvent
		if err := json.Unmarshal(data, &delta); err != nil {
			return false, fmt.Errorf("decode managed delta: %w", err)
		}
		s.content.WriteString(delta.Content)
		if onDelta != nil && delta.Content != "" {
			onDelta(delta.Content)
		}
		return false, nil

	case llmwire.EventResult:
		if err := json.Unmarshal(data, &s.result); err != nil {
			return false, fmt.Errorf("decode managed result: %w", err)
		}
		return true, nil

	case llmwire.EventError:
		var failure llmwire.ErrorEvent
		if err := json.Unmarshal(data, &failure); err != nil {
			return false, fmt.Errorf("decode managed error: %w", err)
		}
		return false, &GatewayError{
			StatusCode: http.StatusOK,
			Code:       failure.Code,
			Message:    failure.Error,
		}

	default:
		// An unknown event is ignored rather than fatal, so the server can add
		// one without breaking older clients.
		return false, nil
	}
}

func (s *streamState) finish() (cllm.Completion, error) {
	// The result carries the assembled content; fall back to the deltas we
	// accumulated if a server ever omits it.
	content := s.result.Content
	if content == "" {
		content = s.content.String()
	}
	var usage cllm.Usage
	if s.result.Usage != nil {
		usage = cllm.Usage{
			PromptTokens:     s.result.Usage.PromptTokens,
			CompletionTokens: s.result.Usage.CompletionTokens,
			TotalTokens:      s.result.Usage.TotalTokens,
			CacheReadTokens:  s.result.Usage.CacheReadTokens,
			CacheWriteTokens: s.result.Usage.CacheWriteTokens,
		}
	}
	return cllm.Completion{
		Content:       content,
		ToolCalls:     fromWireToolCalls(s.result.ToolCalls),
		Usage:         usage,
		ProviderState: fromWireProviderState(s.result.ProviderState),
	}, nil
}

// post sends one completion request. The caller closes the response body.
//
// The client-side deadline, when set, covers the whole exchange including a
// stream, so a hung connection cannot hold a caller forever.
func (c *Client) post(ctx context.Context, stream bool, call cllm.Request) (*http.Response, error) {
	url, err := c.completionsURL()
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(llmwire.CompletionRequest{
		Model:    c.cfg.Model,
		Messages: toWireMessages(call.Messages),
		Tools:    toWireTools(call.Tools),
		Stream:   stream,
		Metadata: c.metadata(),
		// What the call is for, and nothing about what to do with it. The
		// server owns the cache policy for a managed call; sending a mode or a
		// retention from here would let a client spend the operator's money on
		// retention the operator did not choose.
		CallProfile: string(call.Profile),
	})
	if err != nil {
		return nil, fmt.Errorf("encode managed request: %w", err)
	}
	if c.cfg.CallTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.cfg.CallTimeout)
		// The deadline must outlive this function for a streamed body, so the
		// cancel travels with the request rather than firing on return.
		context.AfterFunc(ctx, cancel)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build managed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	origin, ok := cllm.CallOriginFromContext(ctx)
	if !ok || origin.Surface == "" {
		origin.Surface = c.cfg.Surface
	}
	req.Header.Set("User-Agent", config.UserAgent(origin.Surface, origin.ViaGateway))
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	if err := c.setAuthorization(req); err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("managed gateway unreachable: %w", err)
	}
	return resp, nil
}

func (c *Client) metadata() *llmwire.Metadata {
	if c.cfg.Surface == "" {
		return nil
	}
	return &llmwire.Metadata{Surface: c.cfg.Surface}
}

// GatewayError is a refusal from the managed gateway. Code is the server's
// stable classification, so callers branch on it instead of matching prose.
type GatewayError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *GatewayError) Error() string {
	switch {
	case e.Code == "" && e.StatusCode >= http.StatusInternalServerError:
		// No BuildMax body: something in front of the server answered, which
		// means the server itself is down or restarting. Calling that a
		// refusal sends the user looking for a policy that does not exist.
		return fmt.Sprintf("the BuildMax server did not answer (HTTP %d): it may be down or restarting; try again shortly", e.StatusCode)
	case e.Code == "upstream_error":
		return fmt.Sprintf("the deployment's model provider failed this call (%s): %s", e.Code, e.Message)
	case e.Message != "" && e.Code != "":
		return fmt.Sprintf("managed gateway refused the call (%s): %s", e.Code, e.Message)
	case e.Message != "":
		return fmt.Sprintf("managed gateway refused the call (HTTP %d): %s", e.StatusCode, e.Message)
	default:
		return fmt.Sprintf("managed gateway refused the call (HTTP %d)", e.StatusCode)
	}
}

// gatewayError reads a failure response into a classified error. The body is a
// BuildMax error shape by contract; anything else is reported by status alone
// rather than echoed back to the caller.
func gatewayError(resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	out := &GatewayError{StatusCode: resp.StatusCode}
	if err != nil || len(body) == 0 {
		return out
	}
	var parsed llmwire.ErrorResponse
	if json.Unmarshal(body, &parsed) == nil {
		out.Code = parsed.Code
		out.Message = parsed.Error
	}
	return out
}

func toWireMessages(in []cllm.Message) []llmwire.Message {
	if len(in) == 0 {
		return nil
	}
	out := make([]llmwire.Message, 0, len(in))
	for _, m := range in {
		out = append(out, llmwire.Message{
			Role:          m.Role,
			Content:       m.Content,
			ToolCallID:    m.ToolCallID,
			ToolCalls:     toWireToolCalls(m.ToolCalls),
			ProviderState: toWireProviderState(m.ProviderState),
			Parts:         toWireParts(m.Parts),
		})
	}
	return out
}

func toWireToolCalls(in []cllm.ToolCall) []llmwire.ToolCall {
	if len(in) == 0 {
		return nil
	}
	out := make([]llmwire.ToolCall, 0, len(in))
	for _, tc := range in {
		out = append(out, llmwire.ToolCall{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments})
	}
	return out
}

func toWireProviderState(in *cllm.ProviderState) *llmwire.ProviderState {
	if in == nil {
		return nil
	}
	return &llmwire.ProviderState{Protocol: in.Protocol, Data: in.Data}
}

func fromWireProviderState(in *llmwire.ProviderState) *cllm.ProviderState {
	if in == nil {
		return nil
	}
	return &cllm.ProviderState{Protocol: in.Protocol, Data: in.Data}
}

func toWireParts(in []cllm.ContentPart) []llmwire.ContentPart {
	if len(in) == 0 {
		return nil
	}
	out := make([]llmwire.ContentPart, 0, len(in))
	for _, part := range in {
		out = append(out, llmwire.ContentPart{
			Type: part.Type, Text: part.Text, MediaType: part.MediaType, Data: part.Data,
		})
	}
	return out
}

func fromWireToolCalls(in []llmwire.ToolCall) []cllm.ToolCall {
	if len(in) == 0 {
		return nil
	}
	out := make([]cllm.ToolCall, 0, len(in))
	for _, tc := range in {
		out = append(out, cllm.ToolCall{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments})
	}
	return out
}

func toWireTools(in []cllm.ToolDef) []llmwire.Tool {
	if len(in) == 0 {
		return nil
	}
	out := make([]llmwire.Tool, 0, len(in))
	for _, t := range in {
		tool := llmwire.Tool{Name: t.Name, Description: t.Description}
		if t.Parameters != nil {
			if raw, err := json.Marshal(t.Parameters); err == nil {
				tool.Parameters = raw
			}
		}
		out = append(out, tool)
	}
	return out
}

// Models lists the models this client's deployment offers.
//
// Discovery is a space capability, so a worker-mode client refuses it. A task run
// is told which model to use at dispatch; letting it browse the space's catalog
// would be a choice it has no business making.
func (c *Client) Models(ctx context.Context) ([]llmwire.Model, error) {
	if c == nil || c.cfg.ServerURL == "" {
		return nil, errors.New("managed llm client needs a server URL")
	}
	// A run is told which model to call and never chooses one, so listing is not
	// a question it may ask.
	if c.cfg.TaskRunID != "" {
		return nil, errors.New("a task run cannot list models")
	}
	url := c.cfg.ServerURL + llmwire.ModelsPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build models request: %w", err)
	}
	if err := c.setAuthorization(req); err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("managed gateway unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, gatewayError(resp)
	}
	var out llmwire.ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}
	return out.Models, nil
}

// Compile-time proof that the managed client is interchangeable with the
// provider client everywhere the agent loop expects one.
var _ cllm.LLMClient = (*Client)(nil)
