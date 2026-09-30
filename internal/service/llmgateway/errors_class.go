package llmgateway

import (
	"context"
	"errors"
	"net"

	cllm "github.com/icloudbb/buildmax/internal/core/llm"
)

// Stable error classifications.
//
// These strings are the contract: they reach clients as error codes and land in
// the call ledger. They describe what BuildMax decided, never what an upstream
// provider said, so a provider's error body cannot leak account identifiers,
// endpoints, or request fragments through them.
const (
	ErrorClassTargetNotFound = "target_not_found"
	ErrorClassTargetDisabled = "target_disabled"
	ErrorClassCapability     = "capability_unsupported"
	ErrorClassQuotaExceeded  = "quota_exceeded"
	ErrorClassDuplicateCall  = "duplicate_call"
	ErrorClassInvalidRequest = "invalid_request"
	ErrorClassNotConfigured  = "not_configured"
	// ErrorClassCanceled is the caller going away. It is never a timeout of
	// the gateway's own: that is ErrorClassUpstreamTimeout.
	ErrorClassCanceled = "canceled"
	// The upstream classes split a provider failure only where the operator's
	// next action differs, and only by what BuildMax observed — a status or a
	// deadline — never by the provider's text.
	ErrorClassUpstream            = "upstream_error"
	ErrorClassUpstreamTimeout     = "upstream_timeout"
	ErrorClassUpstreamAuth        = "upstream_auth_failed"
	ErrorClassUpstreamRateLimited = "upstream_rate_limited"
	ErrorClassInternal            = "internal_error"
)

// UpstreamError is a provider call that failed, carrying the class the
// gateway decided for it when it closed the ledger row. Carrying the decision,
// rather than re-deriving it from the error, is what keeps the ledger and the
// caller's error code from disagreeing. It satisfies errors.Is(err,
// ErrUpstream) and still unwraps to the provider client's error.
type UpstreamError struct {
	Class string
	Err   error
}

func (e *UpstreamError) Error() string { return ErrUpstream.Error() + ": " + e.Err.Error() }

func (e *UpstreamError) Unwrap() []error { return []error{ErrUpstream, e.Err} }

// upstreamClass decides what a failed provider call was. callerErr is the
// caller's context error when the call returned.
//
// Only a caller that has gone away makes a call canceled. A deadline with the
// caller still present is the provider client's own per-call timeout (the
// catalog model's call_timeout), so it is a provider failure the operator acts
// on, not a cancellation nobody asked for.
func upstreamClass(callerErr, callErr error) string {
	contextEnded := errors.Is(callErr, context.Canceled) || errors.Is(callErr, context.DeadlineExceeded)
	var netErr net.Error
	switch {
	case callerErr != nil && contextEnded:
		return ErrorClassCanceled
	case errors.Is(callErr, context.DeadlineExceeded),
		errors.As(callErr, &netErr) && netErr.Timeout():
		return ErrorClassUpstreamTimeout
	case errors.Is(callErr, cllm.ErrProviderAuth):
		return ErrorClassUpstreamAuth
	case errors.Is(callErr, cllm.ErrProviderRateLimited):
		return ErrorClassUpstreamRateLimited
	default:
		return ErrorClassUpstream
	}
}

// ErrorClassFor maps an error to its stable classification. Anything
// unrecognized is internal rather than upstream, so a new failure mode is
// reported as our problem until someone classifies it.
func ErrorClassFor(err error) string {
	var upstream *UpstreamError
	switch {
	case err == nil:
		return ""
	case errors.As(err, &upstream):
		return upstream.Class
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return ErrorClassCanceled
	case errors.Is(err, ErrTargetNotFound), errors.Is(err, ErrCatalogEmpty):
		return ErrorClassTargetNotFound
	case errors.Is(err, ErrTargetDisabled):
		return ErrorClassTargetDisabled
	case errors.Is(err, ErrCapabilityUnsupported):
		return ErrorClassCapability
	case errors.Is(err, ErrQuotaExceeded):
		return ErrorClassQuotaExceeded
	case errors.Is(err, ErrDuplicateCall):
		return ErrorClassDuplicateCall
	case errors.Is(err, ErrMessagesRequired):
		return ErrorClassInvalidRequest
	case errors.Is(err, ErrCatalogNotConfigured),
		errors.Is(err, ErrFactoryNotConfigured),
		errors.Is(err, ErrLedgerNotConfigured):
		return ErrorClassNotConfigured
	case errors.Is(err, ErrUpstream):
		return ErrorClassUpstream
	default:
		return ErrorClassInternal
	}
}

// RetryableClass reports whether trying the same call again could plausibly
// succeed. It describes the failure; it never authorizes a replay after the
// caller has already seen output. A refused credential is the one provider
// failure a retry cannot fix.
func RetryableClass(class string) bool {
	switch class {
	case ErrorClassUpstream, ErrorClassUpstreamTimeout, ErrorClassUpstreamRateLimited:
		return true
	default:
		return false
	}
}
