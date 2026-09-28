package llm

// Error classification reads the neutral apiError every adapter produces, so
// these tests feed provider errors through the adapter conversion rather than
// asserting against one library's error type directly.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"
)

func TestClassifyLLMError_Nil(t *testing.T) {
	if got := classifyLLMError(nil); got != "" {
		t.Errorf("classifyLLMError(nil) = %q, want empty", got)
	}
}

func TestClassifyLLMError_ContextDeadline(t *testing.T) {
	got := classifyLLMError(context.DeadlineExceeded)
	if !strings.Contains(got, "timed out") {
		t.Errorf("classifyLLMError(DeadlineExceeded) = %q, want 'timed out'", got)
	}
}

func TestClassifyLLMError_ContextCanceled(t *testing.T) {
	got := classifyLLMError(context.Canceled)
	if !strings.Contains(got, "cancelled") {
		t.Errorf("classifyLLMError(Canceled) = %q, want 'cancelled'", got)
	}
}

func TestClassifyLLMError_AuthError(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		got := classifyLLMError(openAIAPIError(&openai.APIError{HTTPStatusCode: code}))
		if !strings.Contains(got, "authentication failed") {
			t.Errorf("HTTP %d: classifyLLMError = %q, want 'authentication failed'", code, got)
		}
		if !strings.Contains(got, "api_key") {
			t.Errorf("HTTP %d: classifyLLMError = %q, should mention api_key", code, got)
		}
	}
}

func TestClassifyLLMError_RateLimit(t *testing.T) {
	got := classifyLLMError(openAIAPIError(&openai.APIError{HTTPStatusCode: 429}))
	if !strings.Contains(got, "rate limited") {
		t.Errorf("classifyLLMError(429) = %q, want 'rate limited'", got)
	}
}

func TestClassifyLLMError_ServerErrors(t *testing.T) {
	for _, code := range []int{500, 502, 503, 504} {
		got := classifyLLMError(openAIAPIError(&openai.APIError{HTTPStatusCode: code}))
		if got == "" {
			t.Errorf("HTTP %d: classifyLLMError returned empty string", code)
		}
	}
}

func TestClassifyLLMError_UnknownAPIError_WithMessage(t *testing.T) {
	got := classifyLLMError(openAIAPIError(&openai.APIError{HTTPStatusCode: 422, Message: "invalid model"}))
	if !strings.Contains(got, "422") || !strings.Contains(got, "invalid model") {
		t.Errorf("classifyLLMError(422, msg) = %q, want status and message", got)
	}
}

func TestClassifyLLMError_PlainError(t *testing.T) {
	err := errors.New("connection refused")
	got := classifyLLMError(err)
	if got != "connection refused" {
		t.Errorf("classifyLLMError(plain) = %q, want original message", got)
	}
}

func TestWrapLLMError_PreservesUnwrap(t *testing.T) {
	original := openAIAPIError(&openai.APIError{HTTPStatusCode: 429})
	wrapped := wrapLLMError(original)
	var apiErr *openai.APIError
	if !errors.As(wrapped, &apiErr) {
		t.Error("wrapLLMError: errors.As should unwrap to *openai.APIError")
	}
	if apiErr.HTTPStatusCode != 429 {
		t.Errorf("unwrapped status = %d, want 429", apiErr.HTTPStatusCode)
	}
}

func TestWrapLLMError_Nil(t *testing.T) {
	if wrapLLMError(nil) != nil {
		t.Error("wrapLLMError(nil) should return nil")
	}
}

func TestIsProviderError(t *testing.T) {
	provider := &apiError{status: 503}
	if !IsProviderError(provider) {
		t.Error("an apiError is a provider error")
	}
	if !IsProviderError(fmt.Errorf("llm call: %w", wrapLLMError(provider))) {
		t.Error("a wrapped apiError is still a provider error")
	}
	// A timeout or dropped connection carries no HTTP status but is still the
	// model call failing, which is what the drill's stalled model produced.
	timedOut := fmt.Errorf("agent: llm call: %w", wrapLLMError(context.DeadlineExceeded))
	if !IsProviderError(timedOut) {
		t.Error("a timed-out model call is a provider error")
	}
	if IsProviderError(&requestError{err: errors.New("bad history")}) {
		t.Error("a request that never reached a provider is not a provider error")
	}
	if IsProviderError(wrapLLMError(&requestError{err: errors.New("bad history")})) {
		t.Error("a wrapped request error is still not a provider error")
	}
	if IsProviderError(errors.New("tool crashed")) {
		t.Error("an arbitrary error is not a provider error")
	}
}
