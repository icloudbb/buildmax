// Package llm implements the core/llm.LLMClient contract over the LLM wire
// protocols BuildMax speaks: OpenAI Chat Completions, OpenAI Responses,
// Anthropic Messages, and Ollama's native local API.
//
// One package, one entry point. Config.Provider selects an adapter; everything
// a real network call needs to survive — the per-call timeout, the retry loop,
// and error classification — is shared, so the three protocols cannot drift
// apart on the parts a caller depends on.
//
// Mirrors the design in docs/design/llm-provider-adapters.md.
package llm

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/icloudbb/buildmax/internal/config"
	cllm "github.com/icloudbb/buildmax/internal/core/llm"
)

// Config holds the settings for one LLM client.
type Config struct {
	// Provider is the wire protocol to speak. Empty means
	// cllm.ProviderOpenAICompatible.
	Provider      string
	APIKey        string
	BaseURL       string
	Model         string
	ContextWindow int           // 0 = look up, then fall back to config.DefaultContextWindow
	MaxTokens     int           // 0 = the adapter's own default
	CallTimeout   time.Duration // 0 = use DefaultCallTimeoutSecs
	// Reasoning is the effort level (config.Reasoning*). Any level other than
	// off also replays reasoning state on later turns. It does nothing on a
	// protocol that has none.
	Reasoning string
	// CacheControl is the resolved prompt-cache policy. Whether a given call
	// acts on it also depends on that call's core/llm.CallProfile: a mode is
	// what to ask for, and a profile is whether asking is worth its price.
	CacheControl config.CacheControl
	// Integration names a qualified OpenAI-compatible gateway whose cache
	// behaviour is known. Empty is the normal case: the protocol family makes
	// no promise about cache fields, so nothing is sent unless a named profile
	// says this endpoint was tested.
	Integration string
	// Vision says the model accepts image input. When false, an adapter drops
	// image parts and sends only the text describing them.
	Vision bool
	// Surface identifies where the request originated, such as cli, desktop,
	// server, or worker. It appears in the User-Agent and is not user input.
	Surface string
	// KeepAlive is how long a local runtime keeps the model loaded after a
	// call. Only the Ollama adapter has a model to keep loaded; the hosted
	// protocols ignore it.
	KeepAlive string
	// HTTPClient overrides the transport. Tests use it; production leaves it nil.
	HTTPClient *http.Client
}

// adapter speaks one wire protocol. It performs a single attempt and reports
// failures as *apiError where it can classify them, so the shared retry and
// classification logic never has to know which library produced the error.
type adapter interface {
	// name is the provider value this adapter implements.
	name() string
	blocking(ctx context.Context, req cllm.Request) (cllm.Completion, error)
	streaming(ctx context.Context, req cllm.Request, onDelta func(string)) (cllm.Completion, error)
}

// Client calls one model through one wire protocol and holds the
// configuration for those calls.
type Client struct {
	adapter       adapter
	contextWindow int
	callTimeout   time.Duration
}

// ContextWindow returns the configured context window size (0 = no windowing).
func (c *Client) ContextWindow() int { return c.contextWindow }

// Provider returns the wire protocol this client speaks. Diagnostics and the
// trace use it to say which protocol served a call.
func (c *Client) Provider() string { return c.adapter.name() }

// NewClient builds an LLM client for the configured wire protocol.
//
// An unknown provider is an error rather than a fallback: a model that cannot
// be reached the way it was configured must fail at selection, not send its
// prompt somewhere the operator did not name.
func NewClient(cfg Config) (*Client, error) {
	cw := cfg.ContextWindow
	if cw == 0 {
		cw = resolveContextWindow(cfg)
	}
	callTimeout := cfg.CallTimeout
	if callTimeout == 0 {
		callTimeout = time.Duration(config.DefaultCallTimeoutSecs) * time.Second
	}

	var (
		impl adapter
		err  error
	)
	switch cfg.Provider {
	case "", cllm.ProviderOpenAICompatible:
		impl = newOpenAIChatAdapter(cfg)
	case cllm.ProviderOpenAI:
		impl = newOpenAIResponsesAdapter(cfg)
	case cllm.ProviderAnthropic:
		impl, err = newAnthropicAdapter(cfg)
	case cllm.ProviderOllama:
		// The window is passed in rather than read from cfg: this protocol
		// sends it on every call, and it must be the same number the caller
		// trims history against.
		impl, err = newOllamaAdapter(cfg, cw)
	default:
		return nil, fmt.Errorf("unknown llm provider %q: use one of %s",
			cfg.Provider, strings.Join(cllm.Providers(), ", "))
	}
	if !config.KnownReasoningEffort(cfg.Reasoning) {
		return nil, fmt.Errorf("unknown reasoning effort %q: use one of %s",
			cfg.Reasoning, strings.Join(config.ReasoningEfforts(), ", "))
	}
	// Checked here rather than per call: a policy the target cannot honour is a
	// configuration mistake, and finding it at construction names the model
	// entry instead of failing the first turn of a run.
	provider := cfg.Provider
	if provider == "" {
		provider = cllm.ProviderOpenAICompatible
	}
	capability, cacheErr := cacheCapabilityForIntegration(provider, cfg.Integration)
	if cacheErr != nil {
		return nil, cacheErr
	}
	if cacheErr := validateCachePolicy(cfg.CacheControl, capability, provider); cacheErr != nil {
		return nil, cacheErr
	}
	if err != nil {
		return nil, err
	}

	return &Client{
		adapter:       impl,
		contextWindow: cw,
		callTimeout:   callTimeout,
	}, nil
}

// resolveContextWindow finds the window for an entry that did not state one.
//
// A local model is asked rather than looked up: the fallback table is a
// snapshot of a hosted catalog keyed by its identifiers, and a daemon can
// answer the same question about the model actually installed.
func resolveContextWindow(cfg Config) int {
	if cfg.Provider == cllm.ProviderOllama {
		return ollamaContextWindow(cfg)
	}
	if cw := lookupContextWindow(cfg.Model); cw > 0 {
		return cw
	}
	return config.DefaultContextWindow
}

// ChatCompletionBlocking sends messages and tool definitions, returns assistant content, any tool calls, and usage.
// Retries on rate-limit and transient server errors up to maxRetryAttempts times.
// Errors are wrapped with a human-readable classification before being returned.
func (c *Client) ChatCompletionBlocking(ctx context.Context, req cllm.Request) (completion cllm.Completion, err error) {
	for attempt := range maxRetryAttempts {
		if attempt > 0 {
			logRetry(attempt, err)
			if sleepErr := sleepWithContext(ctx, retryBackoff[attempt-1]); sleepErr != nil {
				return cllm.Completion{}, wrapLLMError(sleepErr)
			}
		}
		callCtx, cancel := c.withCallTimeout(ctx)
		completion, err = c.adapter.blocking(callCtx, req)
		cancel()
		if err == nil || !isRetryableError(err) {
			break
		}
	}
	if err != nil {
		err = wrapLLMError(err)
		return
	}
	finalizeStructured(req, &completion)
	return
}

// ChatCompletionStreaming sends messages and tool definitions, streams content deltas via onDelta,
// and returns full content, any tool calls, and usage (when the provider reports it).
// Retries on transient errors only when no delta has been emitted yet, to avoid duplicate output.
// Errors are wrapped with a human-readable classification before being returned.
// If onDelta is nil, it is not called.
func (c *Client) ChatCompletionStreaming(ctx context.Context, req cllm.Request, onDelta func(delta string)) (completion cllm.Completion, err error) {
	for attempt := range maxRetryAttempts {
		if attempt > 0 {
			logRetry(attempt, err)
			if sleepErr := sleepWithContext(ctx, retryBackoff[attempt-1]); sleepErr != nil {
				return cllm.Completion{}, wrapLLMError(sleepErr)
			}
		}
		var deltaEmitted bool
		guardedDelta := func(delta string) {
			deltaEmitted = true
			if onDelta != nil {
				onDelta(delta)
			}
		}
		callCtx, cancel := c.withCallTimeout(ctx)
		completion, err = c.adapter.streaming(callCtx, req, guardedDelta)
		cancel()
		if err == nil || !isRetryableError(err) || deltaEmitted {
			break
		}
	}
	if err != nil {
		err = wrapLLMError(err)
		return
	}
	finalizeStructured(req, &completion)
	return
}

// withCallTimeout bounds one attempt. The returned cancel is always safe to
// call, so the caller does not branch on whether a timeout is configured.
func (c *Client) withCallTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.callTimeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, c.callTimeout)
}

// maxTokensOrDefault returns the configured output cap, substituting the
// built-in default when a protocol requires the field and the operator set none.
func maxTokensOrDefault(configured int) int {
	if configured > 0 {
		return configured
	}
	return config.DefaultMaxTokens
}

// Ensure *Client implements cllm.LLMClient.
var _ cllm.LLMClient = (*Client)(nil)
