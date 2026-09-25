package llmgateway

import (
	"context"
	"errors"
	"time"
)

// ErrModelNameTaken is returned when an operator reuses a model name.
var ErrModelNameTaken = errors.New("a model with this name already exists")

// ErrCredentialEncryptionUnavailable is returned when a model carries a
// provider credential but the deployment has no encryption key configured to
// protect it at rest. The credential is refused rather than stored in the
// clear: the store never writes a plaintext key.
var ErrCredentialEncryptionUnavailable = errors.New("no deployment encryption key is configured; a model credential cannot be stored")

// Model is one operator-approved upstream the managed gateway may call.
//
// The record deliberately has no credential field. The key lives in the same
// table but is read only by the component that opens a provider connection, so
// listing, resolving, and diagnosing models can never carry it by accident.
// See docs/design/llm-gateway.md.
type Model struct {
	ID string `json:"id"`
	// Name is the operator-facing name, unique within a deployment.
	Name string `json:"name"`
	// ProviderType selects the client implementation.
	ProviderType string `json:"provider_type"`
	// APIURL is the upstream base URL.
	APIURL string `json:"api_url"`
	// Model is the provider's own model identifier.
	Model string `json:"model"`
	// ContextWindow is the usable context size; 0 uses the client default.
	ContextWindow int `json:"context_window,omitempty"`
	// CallTimeout bounds one upstream call in seconds; 0 uses the client default.
	CallTimeout int `json:"call_timeout,omitempty"`
	// MaxTokens caps one response; 0 uses the client default. The Anthropic
	// protocol requires the field, so a target speaking it always sends one.
	MaxTokens int `json:"max_tokens,omitempty"`
	// Reasoning is the effort level the upstream is asked for; empty means off.
	Reasoning string `json:"reasoning,omitempty"`
	// CacheMode and CacheTTL are the prompt-cache policy: which calls ask the
	// upstream to cache the stable prefix of a request, and for how long. Empty
	// means unset, which takes the default policy.
	CacheMode string `json:"cache_mode,omitempty"`
	CacheTTL  string `json:"cache_ttl,omitempty"`
	// Pricing is what this upstream charges, in nano-currency-units per
	// million tokens. The four rates are separate because caching prices them
	// differently; an empty currency means the model is unpriced and its calls
	// report cost as unavailable rather than as zero.
	//
	// These are the *current* rates. What a past call cost is not recomputed
	// from them — the ledger row keeps the rates that applied when it ran, so
	// a price change does not rewrite history.
	Currency          string `json:"currency,omitempty"`
	InputPerMTok      int64  `json:"input_per_mtok,omitempty"`
	CacheReadPerMTok  int64  `json:"cache_read_per_mtok,omitempty"`
	CacheWritePerMTok int64  `json:"cache_write_per_mtok,omitempty"`
	OutputPerMTok     int64  `json:"output_per_mtok,omitempty"`
	// Vision says the upstream accepts image input.
	Vision bool `json:"vision,omitempty"`
	// Capabilities is what this model supports, e.g. "text_chat".
	Capabilities []string `json:"capabilities,omitempty"`
	// Enabled lets an operator retire a model without deleting it.
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CreateModelInput is a new catalog row, including the credential that the
// record itself never carries afterwards.
type CreateModelInput struct {
	Name              string
	ProviderType      string
	APIURL            string
	APIKey            string
	Model             string
	ContextWindow     int
	CallTimeout       int
	MaxTokens         int
	Reasoning         string
	CacheMode         string
	CacheTTL          string
	Vision            bool
	Currency          string
	InputPerMTok      int64
	CacheReadPerMTok  int64
	CacheWritePerMTok int64
	OutputPerMTok     int64
	Capabilities      []string
}

// ModelStore persists the managed model catalog.
//
// Reading a model and reading its credential are separate operations on
// purpose: everything that lists, resolves, or reports a model uses the first,
// and only the client factory uses the second.
type ModelStore interface {
	// CreateLLMModel stores a new model and returns it without its credential.
	CreateLLMModel(ctx context.Context, in CreateModelInput) (*Model, error)
	// GetLLMModel returns one model by ID, or (nil, nil) when not found.
	GetLLMModel(ctx context.Context, llmModelID string) (*Model, error)
	// GetLLMModelByName returns one model by its operator-facing name, or
	// (nil, nil) when not found. Name is unique across the deployment and is
	// what a client names a model by, so this is the lookup on the call path.
	GetLLMModelByName(ctx context.Context, name string) (*Model, error)
	// ListLLMModels returns every model, enabled or not, oldest first.
	ListLLMModels(ctx context.Context) ([]Model, error)
	// SetLLMModelEnabled retires or restores a model.
	SetLLMModelEnabled(ctx context.Context, llmModelID string, enabled bool) error
	// SetLLMModelCredential replaces a model's upstream key in place, so a
	// leaked or expired key is rotated without renaming the model every client
	// selects it by. An empty key clears it, for a provider that needs none.
	SetLLMModelCredential(ctx context.Context, llmModelID, apiKey string) error
	// LLMModelCredential returns the upstream key for a model. It is the only
	// way a credential leaves the store.
	LLMModelCredential(ctx context.Context, llmModelID string) (string, error)
}
