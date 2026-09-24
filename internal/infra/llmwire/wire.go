// Package llmwire is the versioned wire contract for BuildMax managed
// inference.
//
// It is the single definition of the protocol: the server handler and the
// remote client both marshal these types, so a field can never mean one thing
// on one side and something else on the other.
//
// The contract is deliberately narrower than any provider API. It carries
// messages, tool definitions, tool calls, and usage — the semantic content of
// internal/core/llm — and nothing about where a call goes. Upstream URLs,
// provider credentials, provider model identifiers, and free-form generation
// parameters are not part of it in either direction.
//
// Mirrors the design in docs/design/llm-gateway.md section 8.
package llmwire

import "encoding/json"

// Version is the protocol version these types describe. A breaking change to
// any shape here needs a new version, not a silent edit.
const Version = "1"

// Paths, relative to the server base URL.
const (
	// ModelsPath lists the models this deployment offers. Every model in the
	// catalog is available to every signed-in user, so the path names no space.
	ModelsPath = "/api/llm/models"
	// CompletionsPath runs one managed call.
	CompletionsPath = "/api/llm/completions"
	// WorkerCompletionsPath runs one managed call on behalf of a task run.
	// The space comes from the run rather than the path, so a worker cannot
	// name a space it was not scheduled for.
	WorkerCompletionsPath = "/api/worker/task-runs/%s/llm/completions"
)

// Message is one chat message.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	// ProviderState is opaque reasoning state, present only for role
	// "assistant" and only when the upstream protocol produced some.
	ProviderState *ProviderState `json:"provider_state,omitempty"`
	// Parts is non-text content this message carries. Content stays the text
	// describing it, so a deployment whose target cannot take images still
	// serves the call.
	Parts []ContentPart `json:"parts,omitempty"`
}

// ContentPart is one piece of a message's content: text, or an image as base64
// with its media type.
type ContentPart struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
}

// ProviderState is reasoning state the upstream protocol requires unchanged on
// later turns: Anthropic thinking blocks, OpenAI Responses reasoning items.
//
// It is the one field here that is upstream-shaped, and it is carried without
// being read. That is a deliberate narrowing of the rule that this protocol
// says nothing about where a call goes: without it, a deployment that enables
// reasoning would break every managed tool-calling run, because the protocols
// that produce this state reject a turn that drops it. Protocol names the
// producer so a run continued against a differently-configured target discards
// what it cannot use rather than sending it on.
//
// The field is additive. A client or server that predates it omits it and is
// understood by one that does not, so Version does not move.
type ProviderState struct {
	Protocol string          `json:"protocol"`
	Data     json.RawMessage `json:"data"`
}

// ToolCall is a tool invocation.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
}

// Tool is a tool the model may call.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// Metadata is correlation context. It is never authorization input: space and
// user identity come from authentication, not from here.
type Metadata struct {
	Surface   string  `json:"surface,omitempty"`
	SessionID *string `json:"session_id,omitempty"`
}

// CompletionRequest is one managed completion request.
type CompletionRequest struct {
	// CallID is the caller's idempotency key. It identifies one logical
	// invocation; it is not permission to replay a generation.
	CallID   *string   `json:"call_id,omitempty"`
	Model    string    `json:"model,omitempty"`
	Messages []Message `json:"messages"`
	Tools    []Tool    `json:"tools,omitempty"`
	Stream   bool      `json:"stream,omitempty"`
	Metadata *Metadata `json:"metadata,omitempty"`
	// CallProfile is what the caller says the call is for — a core/llm
	// CallProfile value such as "agent_turn" or "title".
	//
	// It is operational input, not authorization input. The server combines it
	// with the approved target's own cache policy, so naming a profile cannot
	// select a mode, a retention, or a cache key the operator did not approve;
	// the strongest thing a client can do with it is describe its own call
	// wrongly and get the caching that description deserves.
	//
	// The field is additive. A client that predates it omits it, and the server
	// then has no evidence that anything will read the prefix back, which is
	// not a reason to buy a cache write.
	CallProfile string `json:"call_profile,omitempty"`
}

// Usage is the token usage for one call.
//
// The cache counts break the prompt down rather than adding to it, so a caller
// that sums all four is double-counting.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CacheReadTokens  int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
}

// CompletionResponse is a finished managed call.
type CompletionResponse struct {
	LLMCallID string     `json:"llm_call_id"`
	Model     string     `json:"model"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// ProviderState is the reasoning state this turn produced, to be sent back
	// on the next request. Absent when the upstream protocol produced none.
	ProviderState *ProviderState `json:"provider_state,omitempty"`
	// Usage is absent when the provider reported none. An absent usage is not
	// the same fact as zero tokens.
	Usage *Usage `json:"usage,omitempty"`
}

// Server-sent event names for a streaming call.
//
// The stream carries BuildMax events, not upstream chunks: a delta is text to
// hand a local stream sink, and tool calls arrive assembled in the result. That
// keeps the protocol matched to core/llm.LLMClient, which exposes content
// deltas during a call and returns assembled tool calls after it.
const (
	// EventDelta carries a DeltaEvent: text to deliver to the caller now.
	EventDelta = "delta"
	// EventResult carries a CompletionResponse and completes the call.
	EventResult = "result"
	// EventError carries an ErrorEvent and terminates the call.
	EventError = "error"
)

// DeltaEvent is one increment of generated text.
type DeltaEvent struct {
	Content string `json:"content"`
}

// ErrorEvent terminates a streaming call.
//
// Retryable says whether trying again could plausibly succeed. It is advice
// about the failure, not permission to replay: once a delta has reached the
// caller, no layer retries, because the caller has already seen output.
type ErrorEvent struct {
	Code      string `json:"code"`
	Error     string `json:"error"`
	Retryable bool   `json:"retryable"`
}

// Model is one model a client may call. Name is what a completion request puts
// in its model field. It carries no endpoint, credential, provider type, or
// upstream model identifier.
//
// ContextWindow and Vision are here because the client acts on them before any
// call: it compacts a session against the window, and sends an image only to a
// model that can read one. Everything else about how the call is made —
// reasoning effort, output cap, cache policy, price — is the operator's target
// policy and stays on the server, which is also what records the cost.
type Model struct {
	Name string `json:"name"`
	// ContextWindow is the usable context size; 0 means the client's default.
	ContextWindow int      `json:"context_window,omitempty"`
	Vision        bool     `json:"vision,omitempty"`
	Capabilities  []string `json:"capabilities"`
	Default       bool     `json:"default"`
	// Pricing is the model's current rates, absent when the operator priced
	// none. A client prices its own session from them, which gives the same
	// figure the deployment's ledger records for the same usage.
	Pricing *ModelPricing `json:"pricing,omitempty"`
}

// ModelPricing is a price list in the form settings.yaml writes one: decimal
// strings per million tokens, so a rate crosses the wire without rounding.
type ModelPricing struct {
	Currency          string `json:"currency"`
	InputPerMTok      string `json:"input_per_mtok"`
	CacheReadPerMTok  string `json:"cache_read_per_mtok"`
	CacheWritePerMTok string `json:"cache_write_per_mtok"`
	OutputPerMTok     string `json:"output_per_mtok"`
}

// ModelsResponse lists the models this deployment offers.
type ModelsResponse struct {
	Models []Model `json:"models"`
}

// ErrorResponse is a failed managed call. Code is the stable classification;
// Error is a safe message. Neither carries a provider's own error body.
type ErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}
