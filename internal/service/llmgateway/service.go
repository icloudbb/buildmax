package llmgateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
)

// Service-level failures, in addition to the resolution errors in resolve.go.
var (
	ErrLedgerNotConfigured = errors.New("call ledger is not configured")
	ErrMessagesRequired    = errors.New("messages are required")
	ErrQuotaExceeded       = errors.New("space quota exceeded")
	ErrDuplicateCall       = errors.New("client call id has already been used")
	// ErrUpstream wraps a provider failure. The wrapped error stays inside the
	// server: callers receive the classification, not the provider's body.
	ErrUpstream = errors.New("upstream model call failed")
)

// DuplicateCallError reports a client call ID the space has already used.
//
// The first version does not attach to a running call or replay a completed
// one: it names the original call so the caller can stop guessing whether its
// request landed. It satisfies errors.Is(err, ErrDuplicateCall).
type DuplicateCallError struct {
	LLMCallID string
	Status    string
}

func (e *DuplicateCallError) Error() string {
	if e.LLMCallID == "" {
		return "this call id has already been used"
	}
	return fmt.Sprintf("call id already used by %s (%s)", e.LLMCallID, e.Status)
}

// Is reports whether the error matches the ErrDuplicateCall sentinel.
func (e *DuplicateCallError) Is(target error) bool { return target == ErrDuplicateCall }

// QuotaError reports why a space was refused. It satisfies
// errors.Is(err, ErrQuotaExceeded).
type QuotaError struct {
	Reason string
}

func (e *QuotaError) Error() string { return e.Reason }

// Is reports whether the error matches the ErrQuotaExceeded sentinel.
func (e *QuotaError) Is(target error) bool { return target == ErrQuotaExceeded }

// QuotaChecker is the narrow quota surface the gateway needs.
type QuotaChecker interface {
	Check(ctx context.Context, spaceID string, addRuns, addTokens int) (allowed bool, reason string, err error)
}

// Service runs one managed call: resolve, authorize, meter, dispatch, record.
type Service struct {
	// Router resolves models and supplies clients.
	Router *Router
	// Ledger records every managed call.
	Ledger coregw.CallStore
	// Quota is optional. Without it, calls are recorded but never refused.
	Quota QuotaChecker
	// Now is the clock, injectable so tests can pin ledger timestamps.
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s == nil || s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

// CompleteRequest is one managed inference request.
//
// Identity fields are derived from authentication by the caller of this
// service; nothing here may be taken from a client request body.
type CompleteRequest struct {
	// SpaceID is what the call is metered against, and is set only on a worker
	// call, where the run names the space it was scheduled for. A foreground call
	// belongs to no space and is not counted against one — see
	// docs/design/client-modes.md section 9.
	SpaceID string
	// UserID is who the call is for, and what the ledger attributes it to. A
	// user-authenticated call takes it from the login; a worker call takes it
	// from the run token, which names the task's owner — a run is somebody's
	// work even though no person is at the keyboard.
	UserID *string
	// TaskRunID and TaskID are set on worker calls, from the run token.
	TaskRunID *string
	TaskID    *string

	// ClientCallID is the caller's idempotency key. Optional.
	ClientCallID *string
	// Surface and SessionID are correlation context only.
	Surface   string
	SessionID *string

	// Model is the catalog model name. Empty selects the deployment default.
	Model    string
	Messages []cllm.Message
	Tools    []cllm.ToolDef
	// CallProfile is what the caller says the call is for. It is operational
	// input, never authorization input: the gateway combines it with the
	// operator's own target policy, and a client cannot use it to select a
	// stronger cache request than the deployment allows.
	CallProfile cllm.CallProfile
}

// CompleteResult is a finished managed call.
type CompleteResult struct {
	LLMCallID string
	Model     string
	Content   string
	ToolCalls []cllm.ToolCall
	Usage     cllm.Usage
	// UsageReported is false when the provider returned no token counts. The
	// zero Usage then means "unknown", not "free".
	UsageReported bool
	// ProviderState is reasoning state the upstream needs back on the next
	// request. The gateway carries it without reading it.
	ProviderState *cllm.ProviderState
}

// Models lists the models this deployment offers.
func (s *Service) Models(ctx context.Context) ([]AvailableModel, error) {
	if s == nil || s.Router == nil {
		return nil, ErrCatalogNotConfigured
	}
	return s.Router.Available(ctx)
}

// Complete runs one blocking managed call.
//
// The ledger record opens before the upstream request and closes after it, so a
// call that never returns still leaves an ACCEPTED row behind as evidence that
// tokens may have been spent.
func (s *Service) Complete(ctx context.Context, req CompleteRequest) (CompleteResult, error) {
	return s.run(ctx, req, nil)
}

// Stream runs one managed call, delivering content deltas to onDelta as they
// arrive and returning the assembled result.
//
// Retry policy belongs to the component that talks to the provider, which is
// the server-side client underneath this call. It stops retrying once a delta
// has been emitted, because the caller has already seen output. No layer above
// this one may add a retry of its own.
func (s *Service) Stream(ctx context.Context, req CompleteRequest, onDelta func(string)) (CompleteResult, error) {
	if onDelta == nil {
		onDelta = func(string) {}
	}
	return s.run(ctx, req, onDelta)
}

func (s *Service) run(ctx context.Context, req CompleteRequest, onDelta func(string)) (CompleteResult, error) {
	if s == nil || s.Router == nil {
		return CompleteResult{}, ErrCatalogNotConfigured
	}
	if s.Ledger == nil {
		return CompleteResult{}, ErrLedgerNotConfigured
	}
	if len(req.Messages) == 0 {
		return CompleteResult{}, ErrMessagesRequired
	}

	streaming := onDelta != nil

	// A repeated client call ID means the caller does not know whether we
	// accepted the first attempt. Say so instead of running the call twice.
	if err := s.rejectDuplicate(ctx, req); err != nil {
		return CompleteResult{}, err
	}

	routed, err := s.Router.ClientFor(ctx, ResolveRequest{
		Name:     req.Model,
		Requires: requiredCapabilities(req, streaming),
	})
	if err != nil {
		return CompleteResult{}, err
	}

	// Soft enforcement: a space already over its limit is refused. Concurrent
	// calls can still overshoot, because the size of a completion is unknown
	// before it exists. See docs/design/llm-gateway.md section 10.
	//
	// Only a call that belongs to a space is metered against one; a foreground
	// call names no space and passes.
	if s.Quota != nil && req.SpaceID != "" {
		allowed, reason, err := s.Quota.Check(ctx, req.SpaceID, 0, 0)
		if err != nil {
			// Refusing here costs one call; admitting serves unmetered
			// inference on a deployment that cannot see its own limits.
			return CompleteResult{}, fmt.Errorf("check quota for space %s: %w", req.SpaceID, err)
		}
		if !allowed {
			return CompleteResult{}, &QuotaError{Reason: reason}
		}
	}

	acceptedAt := s.now().UTC()
	ledgerEntry := &coregw.Call{
		ClientCallID:  req.ClientCallID,
		UserID:        req.UserID,
		TaskRunID:     req.TaskRunID,
		TaskID:        req.TaskID,
		Surface:       req.Surface,
		SessionID:     req.SessionID,
		Model:         routed.Resolution.Name,
		TargetID:      routed.Resolution.Target.ID,
		ProviderType:  routed.Resolution.Target.ProviderType,
		UpstreamModel: routed.Resolution.Target.UpstreamModel,
		Streaming:     streaming,
		AcceptedAt:    acceptedAt,
		Status:        coregw.CallStatusAccepted,
	}
	// The rates are copied onto the row at acceptance, not looked up when
	// someone reads it back. A catalog price changes; what a space spent last
	// month does not, and a spend report recomputed from today's rates would
	// quietly restate an invoice that has already been paid.
	applyRateSnapshot(ledgerEntry, routed.Resolution.Target)
	call, err := s.Ledger.OpenLLMCall(ctx, ledgerEntry)
	if err != nil {
		if errors.Is(err, coregw.ErrDuplicateCall) {
			return CompleteResult{}, &DuplicateCallError{}
		}
		return CompleteResult{}, fmt.Errorf("open call ledger: %w", err)
	}

	upstreamStartedAt := s.now().UTC()
	var firstDeltaAt *time.Time

	var completion cllm.Completion
	var callErr error
	upstreamCtx := cllm.WithCallOrigin(ctx, upstreamCallOrigin(req.Surface))
	upstreamCall := cllm.Request{
		Messages: req.Messages,
		Tools:    req.Tools,
		Profile:  req.CallProfile,
		// Spaces sharing one approved model share one provider credential, and
		// therefore one provider cache bucket unless something separates them.
		// The space is that separator, and it comes from authentication rather
		// than from the request: a caller that could name its own scope could
		// aim at another space's bucket.
		CacheScope: req.SpaceID,
	}
	if streaming {
		observed := func(delta string) {
			if firstDeltaAt == nil {
				at := s.now().UTC()
				firstDeltaAt = &at
			}
			onDelta(delta)
		}
		completion, callErr = routed.Client.ChatCompletionStreaming(upstreamCtx, upstreamCall, observed)
	} else {
		completion, callErr = routed.Client.ChatCompletionBlocking(upstreamCtx, upstreamCall)
	}

	outcome := coregw.CallOutcome{
		Attempts:          1,
		UpstreamStartedAt: &upstreamStartedAt,
		FirstDeltaAt:      firstDeltaAt,
		CompletedAt:       s.now().UTC(),
	}
	if callErr != nil {
		class := ErrorClassFor(callErr)
		if class == ErrorClassInternal {
			class = ErrorClassUpstream
		}
		outcome.Status = coregw.CallStatusFailed
		if errors.Is(callErr, context.Canceled) || errors.Is(callErr, context.DeadlineExceeded) {
			outcome.Status = coregw.CallStatusCanceled
		}
		outcome.ErrorClass = &class
		s.closeLedger(ctx, call.ID, outcome)
		// The caller is told only the class; the provider's own message can
		// carry account identifiers and request fragments. It is still the
		// operator's one clue to whether a key, a model id, or the provider is
		// at fault, so it is kept here, server-side, next to the ledger row.
		if outcome.Status == coregw.CallStatusFailed {
			slog.Warn("managed llm call failed upstream",
				"llm_call_id", call.ID,
				"model", ledgerEntry.Model,
				"target_id", ledgerEntry.TargetID,
				"provider_type", ledgerEntry.ProviderType,
				"upstream_model", ledgerEntry.UpstreamModel,
				"surface", ledgerEntry.Surface,
				"error_class", class,
				"err", callErr)
		}
		return CompleteResult{LLMCallID: call.ID}, fmt.Errorf("%w: %w", ErrUpstream, callErr)
	}

	usage := completion.Usage
	reported := usage.TotalTokens > 0 || usage.PromptTokens > 0 || usage.CompletionTokens > 0
	outcome.Status = coregw.CallStatusSucceeded
	if reported {
		outcome.Usage = &coregw.CallUsage{
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			TotalTokens:      usage.TotalTokens,
			CacheReadTokens:  usage.CacheReadTokens,
			CacheWriteTokens: usage.CacheWriteTokens,
			Source:           coregw.UsageSourceReported,
		}
	}
	s.closeLedger(ctx, call.ID, outcome)

	return CompleteResult{
		LLMCallID:     call.ID,
		Model:         routed.Resolution.Name,
		Content:       completion.Content,
		ToolCalls:     completion.ToolCalls,
		Usage:         usage,
		UsageReported: reported,
		ProviderState: completion.ProviderState,
	}, nil
}

// upstreamCallOrigin preserves only runtime-owned surfaces. CompleteRequest is
// assembled by handlers, but its metadata may have crossed the network and
// must not become an arbitrary HTTP header value at an upstream provider.
func upstreamCallOrigin(surface string) cllm.CallOrigin {
	switch surface {
	case coregw.CallSurfaceCLI, coregw.CallSurfaceDesktop, coregw.CallSurfaceWorker:
		return cllm.CallOrigin{Surface: surface, ViaGateway: true}
	case coregw.CallSurfaceServer:
		return cllm.CallOrigin{Surface: surface}
	default:
		return cllm.CallOrigin{Surface: coregw.CallSurfaceServer}
	}
}

// closeLedger writes the terminal record. A write failure does not discard work
// the provider already charged for: the row stays ACCEPTED, which is the signal
// that reconciliation is needed.
func (s *Service) closeLedger(ctx context.Context, llmCallID string, outcome coregw.CallOutcome) {
	// The call may have ended because the caller went away; use a context that
	// is still live so the terminal record is written anyway.
	writeCtx := context.WithoutCancel(ctx)
	if err := s.Ledger.CompleteLLMCall(writeCtx, llmCallID, outcome); err != nil {
		gatewayLog().Error("call ledger not closed; row stays ACCEPTED",
			"err", err, "llm_call_id", llmCallID, "status", outcome.Status)
	}
}

// requiredCapabilities derives what this request needs from its shape, so a
// target that cannot serve it fails before an upstream call.
func requiredCapabilities(req CompleteRequest, streaming bool) []Capability {
	capabilities := []Capability{CapabilityTextChat}
	if len(req.Tools) > 0 {
		capabilities = append(capabilities, CapabilityToolCalls)
	}
	if streaming {
		capabilities = append(capabilities, CapabilityStreamingText)
	}
	return capabilities
}

// rejectDuplicate reports a client call ID this caller has already used.
//
// The unique index is what actually decides duplication; this lookup exists to
// answer with the original call ID instead of a bare constraint violation. A
// lookup failure is not fatal: the index still closes the race.
func (s *Service) rejectDuplicate(ctx context.Context, req CompleteRequest) error {
	if req.ClientCallID == nil || *req.ClientCallID == "" {
		return nil
	}
	// The key is scoped to the person who sent it, so a caller with no user
	// identity has no key namespace to be duplicated within.
	if req.UserID == nil || *req.UserID == "" {
		return nil
	}
	existing, err := s.Ledger.GetLLMCallByClientID(ctx, *req.UserID, *req.ClientCallID)
	if err != nil || existing == nil {
		return nil
	}
	return &DuplicateCallError{LLMCallID: existing.ID, Status: existing.Status}
}

// Identity belongs in an attr, not in every message string.
func gatewayLog() *slog.Logger { return slog.With("component", "llm_gateway") }

// applyRateSnapshot copies the target's current prices onto a ledger row.
//
// An unpriced target leaves every field nil, which reads back as "cost
// unavailable" rather than as a call that cost nothing. A zero rate on a priced
// target is a real price and is stored as one.
func applyRateSnapshot(call *coregw.Call, target Target) {
	if target.Currency == "" {
		return
	}
	input, cacheRead, cacheWrite, output :=
		target.InputPerMTok, target.CacheReadPerMTok, target.CacheWritePerMTok, target.OutputPerMTok
	call.Currency = target.Currency
	call.RateInputPerMTok = &input
	call.RateCacheReadPerMTok = &cacheRead
	call.RateCacheWritePerMTok = &cacheWrite
	call.RateOutputPerMTok = &output
}
