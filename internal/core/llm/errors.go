package llm

import "errors"

// Provider refusals a caller can act on without reading the provider's text.
// An LLMClient implementation makes its failures match these with errors.Is, so
// the managed gateway can classify a refusal while the provider's body, which
// may carry account identifiers, stays inside the client that received it.
var (
	// ErrProviderAuth is the provider refusing the configured credential
	// (HTTP 401 or 403). Retrying cannot help; someone must replace the key.
	ErrProviderAuth = errors.New("model provider refused the credential")
	// ErrProviderRateLimited is the provider throttling calls (HTTP 429).
	ErrProviderRateLimited = errors.New("model provider rate limited the call")
)
