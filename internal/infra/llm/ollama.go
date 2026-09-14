package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/icloudbb/buildmax/internal/config"
	cllm "github.com/icloudbb/buildmax/internal/core/llm"
)

// ollamaAdapter speaks Ollama's native /api/chat.
//
// The compatibility endpoint the same daemon serves would answer
// openai_compatible, and this adapter exists because that path accepts no
// options: the runtime then applies its own default context window and
// truncates a longer prompt instead of rejecting it. What gets dropped is the
// front of the request — the system prompt and the tool definitions — so the
// symptom is a model that stops calling tools. num_ctx is reachable only here,
// and this adapter sends it on every call.
//
// Two protocol properties shape the rest: tool calls carry no identifier, and
// their arguments are a JSON object rather than a string. Both are repaired
// here so core/llm sees what every other protocol produces.
//
// Mirrors the design in docs/design/local-ollama-provider.md.
type ollamaAdapter struct {
	http          *http.Client
	baseURL       string
	model         string
	maxTokens     int
	contextWindow int
	keepAlive     string
	reasoning     string
	vision        bool
}

func newOllamaAdapter(cfg Config, contextWindow int) (*ollamaAdapter, error) {
	if cfg.Model == "" {
		return nil, errors.New("ollama provider needs a model, for example qwen3:8b")
	}
	return &ollamaAdapter{
		// The per-call deadline comes from the context, and a cold model can
		// spend most of one loading, so this transport adds no client timeout.
		http:          withBuildMaxUserAgent(cfg.HTTPClient, cfg.Surface),
		baseURL:       ollamaBaseURL(cfg.BaseURL),
		model:         cfg.Model,
		maxTokens:     cfg.MaxTokens,
		contextWindow: contextWindow,
		keepAlive:     cfg.KeepAlive,
		reasoning:     cfg.Reasoning,
		vision:        cfg.Vision,
	}, nil
}

func (a *ollamaAdapter) name() string { return cllm.ProviderOllama }

// ollamaBaseURL normalizes what an operator wrote to the daemon root.
//
// A "/v1" suffix is dropped rather than honored: it is the compatibility
// endpoint, it is what every other BuildMax example shows, and leaving it on
// would send this protocol's requests to a path that answers a different one.
func ollamaBaseURL(raw string) string {
	url := strings.TrimRight(strings.TrimSpace(raw), "/")
	if url == "" {
		return config.DefaultOllamaBaseURL
	}
	return strings.TrimSuffix(url, "/v1")
}

// --- Wire types -------------------------------------------------------------

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Tools    []ollamaTool    `json:"tools,omitempty"`
	// Format is this protocol's native structured-output mechanism: a JSON Schema
	// the daemon constrains output to. Empty is unconstrained free text.
	Format    json.RawMessage `json:"format,omitempty"`
	Stream    bool            `json:"stream"`
	Think     bool            `json:"think,omitempty"`
	KeepAlive string          `json:"keep_alive,omitempty"`
	Options   ollamaOptions   `json:"options"`
}

type ollamaOptions struct {
	// NumCtx is never omitted: an absent value is the silent truncation this
	// adapter exists to prevent.
	NumCtx     int `json:"num_ctx"`
	NumPredict int `json:"num_predict,omitempty"`
}

type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	Thinking  string           `json:"thinking,omitempty"`
	ToolName  string           `json:"tool_name,omitempty"`
	Images    []string         `json:"images,omitempty"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
}

type ollamaToolCall struct {
	Function ollamaToolCallFunction `json:"function"`
}

type ollamaToolCallFunction struct {
	Name string `json:"name"`
	// Arguments is an object on this protocol, where the canonical format has
	// a string. RawMessage carries either direction without a second decode.
	Arguments json.RawMessage `json:"arguments"`
}

type ollamaTool struct {
	Type     string             `json:"type"`
	Function ollamaToolFunction `json:"function"`
}

type ollamaToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type ollamaChatResponse struct {
	Message         ollamaMessage `json:"message"`
	Done            bool          `json:"done"`
	DoneReason      string        `json:"done_reason"`
	PromptEvalCount int           `json:"prompt_eval_count"`
	EvalCount       int           `json:"eval_count"`
	Error           string        `json:"error"`
}

// --- Request construction ---------------------------------------------------

func (a *ollamaAdapter) buildRequest(req cllm.Request, stream bool) ollamaChatRequest {
	var format json.RawMessage
	if req.Output != nil {
		format = json.RawMessage(req.Output.Schema)
	}
	return ollamaChatRequest{
		Model:     a.model,
		Messages:  a.ollamaMessages(req.Messages),
		Tools:     ollamaTools(req.Tools),
		Format:    format,
		Stream:    stream,
		Think:     config.ReasoningEnabled(a.reasoning),
		KeepAlive: a.keepAlive,
		Options: ollamaOptions{
			// The window covers prompt and response together, and the agent
			// loop already trims history with response headroom reserved, so
			// the two numbers are the same one by construction.
			NumCtx:     a.contextWindow,
			NumPredict: a.maxTokens,
		},
	}
}

// ollamaMessages converts canonical history into this protocol's messages.
//
// The one repair it makes is identifier-shaped. This protocol answers a tool
// call by name, not by id, so a tool result is resolved against the tool calls
// of the assistant message before it. A result whose call is gone — trimmed, or
// replaced by a compaction summary — is dropped rather than sent with an empty
// name, which is what the other adapters do with an unpaired half.
func (a *ollamaAdapter) ollamaMessages(messages []cllm.Message) []ollamaMessage {
	out := make([]ollamaMessage, 0, len(messages))
	callNames := map[string]string{}
	for _, m := range messages {
		switch m.Role {
		case "assistant":
			clear(callNames)
			converted := ollamaMessage{Role: m.Role, Content: m.Content}
			for _, tc := range m.ToolCalls {
				callNames[tc.ID] = tc.Name
				converted.ToolCalls = append(converted.ToolCalls, ollamaToolCall{
					Function: ollamaToolCallFunction{
						Name:      tc.Name,
						Arguments: ollamaArguments(tc.Arguments),
					},
				})
			}
			out = append(out, converted)
		case "tool":
			name, ok := callNames[m.ToolCallID]
			if !ok {
				continue
			}
			out = append(out, ollamaMessage{Role: m.Role, Content: m.Content, ToolName: name})
			// A tool result cannot carry an image on this protocol, so images
			// follow as their own user turn with a preamble — the same repair
			// the OpenAI adapters make, for the same reason.
			if a.vision {
				if images := m.Images(); len(images) > 0 {
					out = append(out, ollamaMessage{
						Role:    "user",
						Content: imageFollowUpPreamble,
						Images:  ollamaImages(images),
					})
				}
			}
		default:
			converted := ollamaMessage{Role: m.Role, Content: m.Content}
			if a.vision && m.Role == "user" {
				converted.Images = ollamaImages(m.Images())
			}
			out = append(out, converted)
		}
	}
	return out
}

// ollamaArguments renders canonical argument text as the JSON object this
// protocol takes. A model that produced something unparsable earlier in the
// session is sent an empty object rather than a body the daemon rejects: the
// turn is already recorded, and failing the whole request would lose it.
func ollamaArguments(arguments string) json.RawMessage {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" || !json.Valid([]byte(trimmed)) {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(trimmed)
}

// ollamaImages carries raw base64. This protocol takes the payload alone, not
// the data URL the OpenAI protocols expect.
func ollamaImages(images []cllm.ContentPart) []string {
	if len(images) == 0 {
		return nil
	}
	out := make([]string, 0, len(images))
	for _, image := range images {
		out = append(out, image.Data)
	}
	return out
}

func ollamaTools(tools []cllm.ToolDef) []ollamaTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]ollamaTool, 0, len(tools))
	for _, t := range tools {
		out = append(out, ollamaTool{
			Type: "function",
			Function: ollamaToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}
	return out
}

// --- Reply conversion -------------------------------------------------------

// toolCallsFrom names the calls in a reply.
//
// The protocol sends none, and the canonical format needs them, so they are
// numbered by position in the conversation: `offset` is how many calls the
// request already contained. Numbering per turn instead would repeat an
// identifier in a long session, and a protocol that pairs calls to results by
// id — Anthropic — would then read the same session ambiguously.
func toolCallsFrom(calls []ollamaToolCall, offset int) []cllm.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]cllm.ToolCall, 0, len(calls))
	for i, call := range calls {
		arguments := string(call.Function.Arguments)
		if arguments == "" {
			arguments = "{}"
		}
		out = append(out, cllm.ToolCall{
			ID:        fmt.Sprintf("call_%d", offset+i+1),
			Name:      call.Function.Name,
			Arguments: arguments,
		})
	}
	return out
}

// priorToolCalls reports the number the next minted identifier must exceed.
//
// It is the count of calls in the history and the highest `call_<n>` already in
// it, whichever is larger. The count alone would be enough for a history that
// only grows, but trimming shortens one: a session that dropped an early turn
// and is later replayed with a larger window would otherwise meet the same
// identifier twice.
func priorToolCalls(messages []cllm.Message) int {
	total, highest := 0, 0
	for _, m := range messages {
		total += len(m.ToolCalls)
		for _, tc := range m.ToolCalls {
			if n, ok := mintedToolCallNumber(tc.ID); ok && n > highest {
				highest = n
			}
		}
	}
	return max(total, highest)
}

// mintedToolCallNumber reads back an identifier this adapter wrote. An
// identifier another protocol minted has no number to read, and the count
// covers it.
func mintedToolCallNumber(id string) (int, bool) {
	suffix, ok := strings.CutPrefix(id, "call_")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(suffix)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func ollamaUsage(resp ollamaChatResponse) cllm.Usage {
	return cllm.Usage{
		PromptTokens:     resp.PromptEvalCount,
		CompletionTokens: resp.EvalCount,
		TotalTokens:      resp.PromptEvalCount + resp.EvalCount,
	}
}

// --- Calls ------------------------------------------------------------------

func (a *ollamaAdapter) blocking(ctx context.Context, req cllm.Request) (cllm.Completion, error) {
	body, err := a.post(ctx, "/api/chat", a.buildRequest(req, false))
	if err != nil {
		return cllm.Completion{}, fmt.Errorf("chat: %w", err)
	}
	defer func() { _ = body.Close() }()

	var resp ollamaChatResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return cllm.Completion{}, fmt.Errorf("chat: decode response: %w", &apiError{err: err})
	}
	if resp.Error != "" {
		return cllm.Completion{}, fmt.Errorf("chat: %w", &apiError{message: resp.Error, err: errors.New(resp.Error)})
	}
	// Thinking is requested but not carried: this protocol signs nothing and
	// replays nothing, so state no one sends back would be state to migrate for
	// nothing. The transcript keeps the answer, as it does on Anthropic.
	return cllm.Completion{
		Content:    resp.Message.Content,
		ToolCalls:  toolCallsFrom(resp.Message.ToolCalls, priorToolCalls(req.Messages)),
		Usage:      ollamaUsage(resp),
		Structured: nativeCandidate(req, resp.Message.Content),
	}, nil
}

func (a *ollamaAdapter) streaming(ctx context.Context, req cllm.Request, onDelta func(string)) (cllm.Completion, error) {
	body, err := a.post(ctx, "/api/chat", a.buildRequest(req, true))
	if err != nil {
		return cllm.Completion{}, fmt.Errorf("chat stream: %w", err)
	}
	defer func() { _ = body.Close() }()

	var content strings.Builder
	var calls []ollamaToolCall
	var usage cllm.Usage
	// The stream is newline-delimited JSON objects rather than SSE, and a
	// decoder over the body reads them without a line-length limit.
	decoder := json.NewDecoder(body)
	for {
		var chunk ollamaChatResponse
		if err := decoder.Decode(&chunk); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return cllm.Completion{Content: content.String()},
				fmt.Errorf("chat stream: decode chunk: %w", &apiError{err: err})
		}
		if chunk.Error != "" {
			return cllm.Completion{Content: content.String()},
				fmt.Errorf("chat stream: %w", &apiError{message: chunk.Error, err: errors.New(chunk.Error)})
		}
		if text := chunk.Message.Content; text != "" {
			content.WriteString(text)
			if onDelta != nil {
				onDelta(text)
			}
		}
		// Tool calls arrive whole rather than as argument deltas, so they are
		// collected in order instead of accumulated by index.
		calls = append(calls, chunk.Message.ToolCalls...)
		if chunk.Done {
			usage = ollamaUsage(chunk)
		}
	}
	return cllm.Completion{
		Content:    content.String(),
		ToolCalls:  toolCallsFrom(calls, priorToolCalls(req.Messages)),
		Usage:      usage,
		Structured: nativeCandidate(req, content.String()),
	}, nil
}

// post sends one JSON request and returns the response body for the caller to
// decode. A non-2xx status becomes the neutral error the shared retry and
// classification logic reads.
func (a *ollamaAdapter) post(ctx context.Context, path string, payload any) (io.ReadCloser, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, &requestError{err: fmt.Errorf("encode request: %w", err)}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return nil, &requestError{err: fmt.Errorf("build request: %w", err)}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.http.Do(req)
	if err != nil {
		return nil, ollamaTransportError(a.baseURL, err)
	}
	if resp.StatusCode >= 300 {
		defer func() { _ = resp.Body.Close() }()
		return nil, ollamaStatusError(a.model, resp)
	}
	return resp.Body, nil
}

// ollamaStatusError reads the daemon's own message and attaches the command
// that fixes it. A local runtime's failures are all things the user can act on,
// so a message that stops at the status code wastes the one advantage it has.
func ollamaStatusError(model string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	message := strings.TrimSpace(string(body))
	var envelope struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil && envelope.Error != "" {
		message = envelope.Error
	}
	if resp.StatusCode == http.StatusNotFound {
		return &apiError{
			status:    resp.StatusCode,
			message:   fmt.Sprintf("model %q is not pulled: run `ollama pull %s`", model, model),
			permanent: true,
			err:       errors.New(message),
		}
	}
	return &apiError{status: resp.StatusCode, message: message, err: errors.New(message)}
}

// ollamaTransportError names the daemon rather than the network. Retrying a
// refused connection to localhost only delays the one sentence that helps.
func ollamaTransportError(baseURL string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if isConnectionRefused(err) {
		return &apiError{
			message:   fmt.Sprintf("no Ollama daemon at %s: start it with `ollama serve`", baseURL),
			permanent: true,
			err:       err,
		}
	}
	return &apiError{err: err}
}
