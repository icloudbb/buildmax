package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"syscall"
)

// apiError is a provider failure reduced to what every caller of this package
// reacts to. Each adapter converts its own library's error into one, so retry
// policy and human-readable classification are written once and cannot mean
// different things for different protocols.
//
// The original error is kept and unwrapped, so a caller that does know a
// specific library's error type can still reach it.
type apiError struct {
	// status is the HTTP status the provider returned, or 0 when the failure
	// happened below HTTP (DNS, connection reset, TLS).
	status int
	// message is the provider's own description, when it gave a safe one, or
	// this package's own when it knows the next step better than the provider
	// stated it.
	message string
	// permanent marks a failure that trying again cannot fix even though its
	// status would normally allow a retry — a local daemon that is not running,
	// or a model that is not pulled. Both are fixed by a command, and three
	// backed-off attempts only delay saying so.
	permanent bool
	err       error
}

func (e *apiError) Error() string {
	switch {
	case e.message != "" && e.status != 0:
		return fmt.Sprintf("provider error (HTTP %d): %s", e.status, e.message)
	case e.status != 0:
		return fmt.Sprintf("provider error (HTTP %d)", e.status)
	case e.message != "":
		return e.message
	case e.err != nil:
		return e.err.Error()
	default:
		return "provider error"
	}
}

func (e *apiError) Unwrap() error { return e.err }

// requestError is a failure to build a request from the history the caller
// supplied. It never reached a provider, so it is deterministic: retrying it
// would repeat the same failure three times and report it three attempts late.
type requestError struct{ err error }

func (e *requestError) Error() string { return e.err.Error() }

func (e *requestError) Unwrap() error { return e.err }

// classifyLLMError returns a concise, human-readable description of an LLM call error.
// It is used to wrap raw provider errors before they propagate to callers.
func classifyLLMError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "LLM call timed out"
	}
	if errors.Is(err, context.Canceled) {
		return "LLM call cancelled"
	}
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		switch apiErr.status {
		case 401, 403:
			return fmt.Sprintf("authentication failed (HTTP %d): check api_key in settings.yaml", apiErr.status)
		case 429:
			return "rate limited by provider: too many requests"
		case 500:
			return "provider internal server error (HTTP 500)"
		case 502:
			return "provider bad gateway (HTTP 502): upstream is unavailable"
		case 503:
			return "provider service unavailable (HTTP 503)"
		case 504:
			return "provider gateway timeout (HTTP 504)"
		case 0:
			// Below HTTP: report what actually failed rather than a status.
			return apiErr.Error()
		default:
			return apiErr.Error()
		}
	}
	return err.Error()
}

// callError is a model call that failed, with its classified description. It
// marks the failure as the call's — a provider refusal, a timeout, a dropped
// connection — so a caller can tell it from a failure of the run around it.
type callError struct {
	msg string
	err error
}

func (e *callError) Error() string { return e.msg + ": " + e.err.Error() }

func (e *callError) Unwrap() error { return e.err }

// wrapLLMError wraps err with a classified human-readable prefix.
// The original error is preserved so errors.As / errors.Is still work.
func wrapLLMError(err error) error {
	if err == nil {
		return nil
	}
	return &callError{msg: classifyLLMError(err), err: err}
}

// isConnectionRefused reports whether a dial failed because nothing is
// listening. errors.Is covers the Unix errno; Windows reports its own, so the
// message is matched there rather than importing a platform-specific code.
func isConnectionRefused(err error) bool {
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	text := err.Error()
	return strings.Contains(text, "connection refused") ||
		strings.Contains(text, "actively refused")
}

// IsProviderError reports whether err is a model call that failed — refused,
// timed out, or cut off — rather than a failure of the run around it. A request
// this package could not build never reached a provider, so it is not one.
func IsProviderError(err error) bool {
	var reqErr *requestError
	if errors.As(err, &reqErr) {
		return false
	}
	var call *callError
	var apiErr *apiError
	return errors.As(err, &call) || errors.As(err, &apiErr)
}
