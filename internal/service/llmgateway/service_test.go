package llmgateway_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
	"github.com/icloudbb/buildmax/internal/service/llmgateway"
)

// scriptedClient returns a fixed reply, recording what it was asked.
type scriptedClient struct {
	content   string
	deltas    []string
	toolCalls []cllm.ToolCall
	usage     cllm.Usage
	err       error
	// providerState is what an upstream that carries reasoning state returns.
	providerState *cllm.ProviderState

	gotMessages []cllm.Message
	gotTools    []cllm.ToolDef
	gotOrigin   cllm.CallOrigin
}

func (c *scriptedClient) ChatCompletionBlocking(ctx context.Context, req cllm.Request) (cllm.Completion, error) {
	c.gotMessages = req.Messages
	c.gotTools = req.Tools
	c.gotOrigin, _ = cllm.CallOriginFromContext(ctx)
	if c.err != nil {
		return cllm.Completion{}, c.err
	}
	return cllm.Completion{
		Content:       c.content,
		ToolCalls:     c.toolCalls,
		Usage:         c.usage,
		ProviderState: c.providerState,
	}, nil
}

func (c *scriptedClient) ChatCompletionStreaming(ctx context.Context, req cllm.Request, onDelta func(string)) (cllm.Completion, error) {
	messages := req.Messages
	tools := req.Tools
	c.gotMessages = messages
	c.gotTools = tools
	c.gotOrigin, _ = cllm.CallOriginFromContext(ctx)
	for _, delta := range c.deltas {
		onDelta(delta)
	}
	if c.err != nil {
		return cllm.Completion{}, c.err
	}
	return cllm.Completion{
		Content:       c.content,
		ToolCalls:     c.toolCalls,
		Usage:         c.usage,
		ProviderState: c.providerState,
	}, nil
}

func (c *scriptedClient) ContextWindow() int { return 0 }

// fakeLedger records calls in memory.
type fakeLedger struct {
	mu       sync.Mutex
	opened   []coregw.Call
	outcomes map[string]coregw.CallOutcome
	existing *coregw.Call
	openErr  error
	closeErr error
	nextID   int
}

func newFakeLedger() *fakeLedger {
	return &fakeLedger{outcomes: map[string]coregw.CallOutcome{}}
}

func (l *fakeLedger) OpenLLMCall(_ context.Context, call *coregw.Call) (*coregw.Call, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.openErr != nil {
		return nil, l.openErr
	}
	l.nextID++
	stored := *call
	stored.ID = "lc_test" + string(rune('0'+l.nextID))
	l.opened = append(l.opened, stored)
	return &stored, nil
}

func (l *fakeLedger) CompleteLLMCall(_ context.Context, llmCallID string, outcome coregw.CallOutcome) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closeErr != nil {
		return l.closeErr
	}
	l.outcomes[llmCallID] = outcome
	return nil
}

func (l *fakeLedger) GetLLMCall(context.Context, string) (*coregw.Call, error) { return nil, nil }

func (l *fakeLedger) GetLLMCallByClientID(context.Context, string, string) (*coregw.Call, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.existing, nil
}

func (l *fakeLedger) ListLLMCallsByTaskRun(context.Context, string) ([]coregw.Call, error) {
	return nil, nil
}

func (l *fakeLedger) SearchLLMCalls(context.Context, coregw.CallFilter, int, int) ([]coregw.Call, int, error) {
	return nil, 0, nil
}

func (l *fakeLedger) only(t *testing.T) (coregw.Call, coregw.CallOutcome) {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.opened) != 1 {
		t.Fatalf("ledger opened %d calls, want 1", len(l.opened))
	}
	call := l.opened[0]
	return call, l.outcomes[call.ID]
}

// denyQuota refuses every space.
type denyQuota struct{ reason string }

func (q denyQuota) Check(context.Context, string, int, int) (bool, string, error) {
	return false, q.reason, nil
}

// allowQuota accepts every space and records that it was asked.
type allowQuota struct{ calls int }

func (q *allowQuota) Check(context.Context, string, int, int) (bool, string, error) {
	q.calls++
	return true, "", nil
}

// unreadableQuota cannot answer: the limit exists but the store is unreachable.
type unreadableQuota struct{ err error }

func (q unreadableQuota) Check(context.Context, string, int, int) (bool, string, error) {
	return false, "", q.err
}

func serviceWith(t *testing.T, client cllm.LLMClient, ledger coregw.CallStore, quota llmgateway.QuotaChecker) *llmgateway.Service {
	t.Helper()
	router := &llmgateway.Router{
		Resolver: testResolver(t),
		Factory: func(context.Context, llmgateway.Target) (cllm.LLMClient, error) {
			return client, nil
		},
	}
	fixed := time.Unix(1_700_000_000, 0)
	return &llmgateway.Service{
		Router: router,
		Ledger: ledger,
		Quota:  quota,
		Now:    func() time.Time { return fixed },
	}
}

func userRequest() llmgateway.CompleteRequest {
	userID := "u_one"
	return llmgateway.CompleteRequest{
		SpaceID:  "tm_one",
		UserID:   &userID,
		Surface:  coregw.CallSurfaceCLI,
		Messages: []cllm.Message{{Role: "user", Content: "hello"}},
	}
}

func TestCompleteRecordsASuccessfulCall(t *testing.T) {
	client := &scriptedClient{
		content: "hi there",
		usage:   cllm.Usage{PromptTokens: 10, CompletionTokens: 4, TotalTokens: 14},
	}
	ledger := newFakeLedger()
	svc := serviceWith(t, client, ledger, &allowQuota{})

	result, err := svc.Complete(context.Background(), userRequest())
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if result.Content != "hi there" {
		t.Errorf("Content = %q, want %q", result.Content, "hi there")
	}
	if result.Model != "Fast" {
		t.Errorf("Model = %q, want the deployment default", result.Model)
	}
	if !result.UsageReported || result.Usage.TotalTokens != 14 {
		t.Errorf("usage = %+v, reported=%v", result.Usage, result.UsageReported)
	}

	call, outcome := ledger.only(t)
	if call.UserID == nil || *call.UserID != "u_one" {
		t.Errorf("ledger identity = user %v, want u_one", call.UserID)
	}
	if call.TargetID != "mt_fast" || call.UpstreamModel != "vendor/fast-1" {
		t.Errorf("ledger model = %q / %q", call.TargetID, call.UpstreamModel)
	}
	if call.Status != coregw.CallStatusAccepted {
		t.Errorf("opened status = %q, want %q", call.Status, coregw.CallStatusAccepted)
	}
	if outcome.Status != coregw.CallStatusSucceeded {
		t.Errorf("outcome status = %q, want %q", outcome.Status, coregw.CallStatusSucceeded)
	}
	if outcome.Usage == nil || outcome.Usage.TotalTokens != 14 {
		t.Fatalf("outcome usage = %+v", outcome.Usage)
	}
	if outcome.Usage.Source != coregw.UsageSourceReported {
		t.Errorf("usage source = %q, want %q", outcome.Usage.Source, coregw.UsageSourceReported)
	}
	if outcome.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", outcome.Attempts)
	}
}

func TestCompletePreservesClientSurfaceForTheUpstream(t *testing.T) {
	client := &scriptedClient{content: "ok"}
	svc := serviceWith(t, client, newFakeLedger(), &allowQuota{})
	if _, err := svc.Complete(context.Background(), userRequest()); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if want := (cllm.CallOrigin{Surface: coregw.CallSurfaceCLI, ViaGateway: true}); client.gotOrigin != want {
		t.Errorf("upstream origin = %+v, want %+v", client.gotOrigin, want)
	}
}

// TestCompleteLeavesUsageUnavailable covers a provider that reports no counts.
// Zero tokens and unknown tokens are different facts.
func TestCompleteLeavesUsageUnavailable(t *testing.T) {
	ledger := newFakeLedger()
	svc := serviceWith(t, &scriptedClient{content: "hi"}, ledger, nil)

	result, err := svc.Complete(context.Background(), userRequest())
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if result.UsageReported {
		t.Error("usage reported for a provider that sent none")
	}
	_, outcome := ledger.only(t)
	if outcome.Usage != nil {
		t.Errorf("outcome usage = %+v, want nil", outcome.Usage)
	}
}

func TestCompleteRecordsAFailedCall(t *testing.T) {
	boom := errors.New("provider said no: account acct_12345 over limit")
	ledger := newFakeLedger()
	svc := serviceWith(t, &scriptedClient{err: boom}, ledger, nil)

	_, err := svc.Complete(context.Background(), userRequest())
	if !errors.Is(err, llmgateway.ErrUpstream) {
		t.Fatalf("want ErrUpstream, got %v", err)
	}

	_, outcome := ledger.only(t)
	if outcome.Status != coregw.CallStatusFailed {
		t.Errorf("outcome status = %q, want %q", outcome.Status, coregw.CallStatusFailed)
	}
	if outcome.ErrorClass == nil || *outcome.ErrorClass != llmgateway.ErrorClassUpstream {
		t.Errorf("error class = %v, want %q", outcome.ErrorClass, llmgateway.ErrorClassUpstream)
	}
	// The ledger keeps a classification, never the provider's own words.
	if outcome.ErrorClass != nil && *outcome.ErrorClass == boom.Error() {
		t.Error("the ledger stored the provider error body")
	}
}

func TestCompleteRefusesOverQuota(t *testing.T) {
	ledger := newFakeLedger()
	svc := serviceWith(t, &scriptedClient{content: "hi"}, ledger, denyQuota{reason: "quota exceeded: token limit"})

	_, err := svc.Complete(context.Background(), userRequest())
	if !errors.Is(err, llmgateway.ErrQuotaExceeded) {
		t.Fatalf("want ErrQuotaExceeded, got %v", err)
	}
	var quotaErr *llmgateway.QuotaError
	if !errors.As(err, &quotaErr) || quotaErr.Reason == "" {
		t.Errorf("want a QuotaError with a reason, got %v", err)
	}
	// A refused call is not a call: nothing is opened in the ledger.
	if len(ledger.opened) != 0 {
		t.Errorf("ledger opened %d calls for a refused request", len(ledger.opened))
	}
}

// A store that cannot answer must not be read as "no limit": serving the call
// would spend a space's allowance on a deployment that cannot see it.
func TestCompleteRefusesWhenQuotaCannotBeRead(t *testing.T) {
	ledger := newFakeLedger()
	boom := errors.New("quota store unreachable")
	svc := serviceWith(t, &scriptedClient{content: "hi"}, ledger, unreadableQuota{err: boom})

	_, err := svc.Complete(context.Background(), userRequest())
	if !errors.Is(err, boom) {
		t.Fatalf("want the store error, got %v", err)
	}
	if errors.Is(err, llmgateway.ErrQuotaExceeded) {
		t.Error("an unreadable quota was reported as an exceeded one")
	}
	if len(ledger.opened) != 0 {
		t.Errorf("ledger opened %d calls for a request that was never admitted", len(ledger.opened))
	}
}

func TestCompleteChecksQuotaAfterResolution(t *testing.T) {
	quota := &allowQuota{}
	ledger := newFakeLedger()
	svc := serviceWith(t, &scriptedClient{content: "hi"}, ledger, quota)

	req := userRequest()
	req.Model = "Reasoning"
	if _, err := svc.Complete(context.Background(), req); !errors.Is(err, llmgateway.ErrTargetNotFound) {
		t.Fatalf("want ErrTargetNotFound, got %v", err)
	}
	// An unusable model is rejected before the quota query runs.
	if quota.calls != 0 {
		t.Errorf("quota consulted %d times for an unknown model, want 0", quota.calls)
	}
}

func TestCompleteRequiresToolCapability(t *testing.T) {
	ledger := newFakeLedger()
	svc := serviceWith(t, &scriptedClient{content: "hi"}, ledger, nil)

	req := userRequest()
	req.Model = "Deep" // declares text_chat only
	req.Tools = []cllm.ToolDef{{Name: "read_file"}}

	_, err := svc.Complete(context.Background(), req)
	if !errors.Is(err, llmgateway.ErrCapabilityUnsupported) {
		t.Fatalf("want ErrCapabilityUnsupported, got %v", err)
	}
	if len(ledger.opened) != 0 {
		t.Error("a capability rejection opened a ledger record")
	}
}

func TestCompleteValidatesRequest(t *testing.T) {
	svc := serviceWith(t, &scriptedClient{}, newFakeLedger(), nil)

	tests := []struct {
		name string
		req  llmgateway.CompleteRequest
		want error
	}{
		{name: "no messages", req: llmgateway.CompleteRequest{SpaceID: "tm_one"}, want: llmgateway.ErrMessagesRequired},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Complete(context.Background(), tc.req); !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}

func TestCompleteNotConfigured(t *testing.T) {
	var nilService *llmgateway.Service
	if _, err := nilService.Complete(context.Background(), userRequest()); !errors.Is(err, llmgateway.ErrCatalogNotConfigured) {
		t.Errorf("nil service: want ErrCatalogNotConfigured, got %v", err)
	}

	noLedger := serviceWith(t, &scriptedClient{}, nil, nil)
	if _, err := noLedger.Complete(context.Background(), userRequest()); !errors.Is(err, llmgateway.ErrLedgerNotConfigured) {
		t.Errorf("want ErrLedgerNotConfigured, got %v", err)
	}
}

// TestCompleteSurvivesLedgerCloseFailure records the deliberate asymmetry: a
// call that cannot be opened is refused, but one that cannot be closed still
// returns its result, leaving an ACCEPTED row as the reconciliation signal.
func TestCompleteSurvivesLedgerCloseFailure(t *testing.T) {
	ledger := newFakeLedger()
	ledger.closeErr = errors.New("ledger unavailable")
	svc := serviceWith(t, &scriptedClient{content: "hi"}, ledger, nil)

	result, err := svc.Complete(context.Background(), userRequest())
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if result.Content != "hi" {
		t.Errorf("Content = %q, want %q", result.Content, "hi")
	}

	openFails := newFakeLedger()
	openFails.openErr = errors.New("ledger unavailable")
	refusing := serviceWith(t, &scriptedClient{content: "hi"}, openFails, nil)
	if _, err := refusing.Complete(context.Background(), userRequest()); err == nil {
		t.Error("a call ran without an open ledger record")
	}
}

func TestCompletePassesMessagesAndToolsThrough(t *testing.T) {
	client := &scriptedClient{content: "ok"}
	svc := serviceWith(t, client, newFakeLedger(), nil)

	req := userRequest()
	req.Messages = append(req.Messages, cllm.Message{Role: "assistant", Content: "prior"})
	req.Tools = []cllm.ToolDef{{Name: "read_file", Description: "reads"}}

	if _, err := svc.Complete(context.Background(), req); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(client.gotMessages) != 2 {
		t.Errorf("client got %d messages, want 2", len(client.gotMessages))
	}
	if len(client.gotTools) != 1 || client.gotTools[0].Name != "read_file" {
		t.Errorf("client got tools %+v", client.gotTools)
	}
}

func TestModelsDelegatesToTheRouter(t *testing.T) {
	svc := serviceWith(t, &scriptedClient{}, newFakeLedger(), nil)

	models, err := svc.Models(context.Background())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("Models returned %d, want 2", len(models))
	}
}

func TestStreamRecordsTheFirstDelta(t *testing.T) {
	client := &scriptedClient{
		content: "Hello",
		deltas:  []string{"Hel", "lo"},
		usage:   cllm.Usage{TotalTokens: 5},
	}
	ledger := newFakeLedger()
	svc := serviceWith(t, client, ledger, nil)

	var got []string
	result, err := svc.Stream(context.Background(), userRequest(), func(d string) { got = append(got, d) })
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if strings.Join(got, "") != "Hello" || result.Content != "Hello" {
		t.Errorf("deltas = %v, content = %q", got, result.Content)
	}

	call, outcome := ledger.only(t)
	if !call.Streaming {
		t.Error("the ledger did not record the call as streaming")
	}
	if outcome.FirstDeltaAt == nil {
		t.Fatal("the ledger recorded no first-delta time")
	}
	if outcome.Status != coregw.CallStatusSucceeded {
		t.Errorf("status = %q", outcome.Status)
	}
}

// TestStreamWithoutDeltasRecordsNoFirstDelta keeps the timing honest when a
// provider returns everything at once.
func TestStreamWithoutDeltasRecordsNoFirstDelta(t *testing.T) {
	ledger := newFakeLedger()
	svc := serviceWith(t, &scriptedClient{content: "all at once"}, ledger, nil)

	if _, err := svc.Stream(context.Background(), userRequest(), nil); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	_, outcome := ledger.only(t)
	if outcome.FirstDeltaAt != nil {
		t.Errorf("FirstDeltaAt = %v, want nil", outcome.FirstDeltaAt)
	}
}

func TestStreamRequiresTheStreamingCapability(t *testing.T) {
	ledger := newFakeLedger()
	svc := serviceWith(t, &scriptedClient{content: "hi"}, ledger, nil)

	req := userRequest()
	req.Model = "Deep" // declares text_chat only

	_, err := svc.Stream(context.Background(), req, nil)
	if !errors.Is(err, llmgateway.ErrCapabilityUnsupported) {
		t.Fatalf("want ErrCapabilityUnsupported, got %v", err)
	}
	if len(ledger.opened) != 0 {
		t.Error("a capability rejection opened a ledger record")
	}
}

func TestDuplicateClientCallIDIsRefused(t *testing.T) {
	ledger := newFakeLedger()
	ledger.existing = &coregw.Call{ID: "lc_original", Status: coregw.CallStatusAccepted}
	svc := serviceWith(t, &scriptedClient{content: "hi"}, ledger, nil)

	req := userRequest()
	key := "client-key-1"
	req.ClientCallID = &key

	_, err := svc.Complete(context.Background(), req)
	if !errors.Is(err, llmgateway.ErrDuplicateCall) {
		t.Fatalf("want ErrDuplicateCall, got %v", err)
	}
	var dup *llmgateway.DuplicateCallError
	if !errors.As(err, &dup) || dup.LLMCallID != "lc_original" {
		t.Errorf("want the original call named, got %v", err)
	}
	// A refused duplicate runs nothing and opens nothing.
	if len(ledger.opened) != 0 {
		t.Errorf("a duplicate opened %d ledger records", len(ledger.opened))
	}
}

// TestDuplicateDetectedByTheIndexIsRefused covers the race the lookup cannot
// close: two concurrent requests carrying one key, where the unique index is
// what decides.
func TestDuplicateDetectedByTheIndexIsRefused(t *testing.T) {
	ledger := newFakeLedger()
	ledger.openErr = coregw.ErrDuplicateCall
	svc := serviceWith(t, &scriptedClient{content: "hi"}, ledger, nil)

	req := userRequest()
	key := "client-key-1"
	req.ClientCallID = &key

	if _, err := svc.Complete(context.Background(), req); !errors.Is(err, llmgateway.ErrDuplicateCall) {
		t.Fatalf("want ErrDuplicateCall, got %v", err)
	}
}

func TestRetryableClass(t *testing.T) {
	if !llmgateway.RetryableClass(llmgateway.ErrorClassUpstream) {
		t.Error("an upstream failure should be reported as retryable")
	}
	for _, class := range []string{
		llmgateway.ErrorClassQuotaExceeded,
		llmgateway.ErrorClassTargetNotFound,
		llmgateway.ErrorClassDuplicateCall,
		llmgateway.ErrorClassCapability,
		llmgateway.ErrorClassInternal,
	} {
		if llmgateway.RetryableClass(class) {
			t.Errorf("%s should not be reported as retryable", class)
		}
	}
}

func TestErrorClassFor(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{err: nil, want: ""},
		{err: llmgateway.ErrMessagesRequired, want: llmgateway.ErrorClassInvalidRequest},
		{err: llmgateway.ErrCatalogEmpty, want: llmgateway.ErrorClassTargetNotFound},
		{err: llmgateway.ErrTargetNotFound, want: llmgateway.ErrorClassTargetNotFound},
		{err: llmgateway.ErrTargetDisabled, want: llmgateway.ErrorClassTargetDisabled},
		{err: &llmgateway.CapabilityError{Model: "Deep"}, want: llmgateway.ErrorClassCapability},
		{err: &llmgateway.QuotaError{Reason: "over"}, want: llmgateway.ErrorClassQuotaExceeded},
		{err: llmgateway.ErrMessagesRequired, want: llmgateway.ErrorClassInvalidRequest},
		{err: llmgateway.ErrCatalogNotConfigured, want: llmgateway.ErrorClassNotConfigured},
		{err: llmgateway.ErrFactoryNotConfigured, want: llmgateway.ErrorClassNotConfigured},
		{err: llmgateway.ErrLedgerNotConfigured, want: llmgateway.ErrorClassNotConfigured},
		{err: context.Canceled, want: llmgateway.ErrorClassCanceled},
		{err: context.DeadlineExceeded, want: llmgateway.ErrorClassCanceled},
		{err: llmgateway.ErrUpstream, want: llmgateway.ErrorClassUpstream},
		// Anything unrecognized is our problem until someone classifies it.
		{err: errors.New("something new"), want: llmgateway.ErrorClassInternal},
	}
	for _, tc := range tests {
		if got := llmgateway.ErrorClassFor(tc.err); got != tc.want {
			t.Errorf("ErrorClassFor(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

// The rates are copied onto the ledger row when the call is accepted, not
// looked up when someone reads it back. A catalog price changes; what a space
// spent last month does not, and a spend report recomputed from today's rates
// would restate an invoice that has already been paid.
func TestCompleteSnapshotsTheTargetRates(t *testing.T) {
	priced := validTarget()
	priced.Currency = "USD"
	priced.InputPerMTok = 3_000_000_000
	priced.CacheReadPerMTok = 300_000_000
	priced.CacheWritePerMTok = 3_750_000_000
	priced.OutputPerMTok = 15_000_000_000

	catalog, err := llmgateway.NewStaticCatalog([]llmgateway.Target{priced})
	if err != nil {
		t.Fatalf("NewStaticCatalog: %v", err)
	}
	ledger := newFakeLedger()
	svc := &llmgateway.Service{
		Router: &llmgateway.Router{
			Resolver: &llmgateway.Resolver{
				Catalog:      catalog,
				DefaultModel: "Fast",
			},
			Factory: func(context.Context, llmgateway.Target) (cllm.LLMClient, error) {
				return &scriptedClient{content: "hi"}, nil
			},
		},
		Ledger: ledger,
	}
	if _, err := svc.Complete(context.Background(), userRequest()); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	call, _ := ledger.only(t)
	if call.Currency != "USD" {
		t.Fatalf("currency = %q, want USD", call.Currency)
	}
	rates := map[string]struct {
		got  *int64
		want int64
	}{
		"input":       {call.RateInputPerMTok, 3_000_000_000},
		"cache read":  {call.RateCacheReadPerMTok, 300_000_000},
		"cache write": {call.RateCacheWritePerMTok, 3_750_000_000},
		"output":      {call.RateOutputPerMTok, 15_000_000_000},
	}
	for name, r := range rates {
		if r.got == nil || *r.got != r.want {
			t.Errorf("%s rate = %v, want %d", name, r.got, r.want)
		}
	}
}

// An unpriced target leaves the row unpriced. A zero rate would read as a model
// that charges nothing, which is a claim about money nobody made.
func TestCompleteLeavesAnUnpricedTargetUnpriced(t *testing.T) {
	ledger := newFakeLedger()
	svc := serviceWith(t, &scriptedClient{content: "hi"}, ledger, nil)
	if _, err := svc.Complete(context.Background(), userRequest()); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	call, _ := ledger.only(t)
	if call.Currency != "" || call.RateInputPerMTok != nil {
		t.Errorf("an unpriced target recorded rates: currency %q input %v", call.Currency, call.RateInputPerMTok)
	}
}

// Two spaces granted the same approved model share one provider credential, and
// therefore one provider cache bucket unless something separates them. The
// scope is that separator, and it comes from the resolved space rather than from
// the request: a caller that could name its own scope could aim at another
// space's bucket.
func TestCompleteScopesTheCacheBucketBySpace(t *testing.T) {
	client := &profileClient{}
	catalog, err := llmgateway.NewStaticCatalog([]llmgateway.Target{validTarget()})
	if err != nil {
		t.Fatalf("NewStaticCatalog: %v", err)
	}
	// Two spaces, one catalog model, therefore one credential between them.
	svc := &llmgateway.Service{
		Router: &llmgateway.Router{
			Resolver: &llmgateway.Resolver{Catalog: catalog, DefaultModel: "Fast"},
			Factory:  func(context.Context, llmgateway.Target) (cllm.LLMClient, error) { return client, nil },
		},
		Ledger: newFakeLedger(),
	}

	first := userRequest()
	first.SpaceID = "tm_one"
	if _, err := svc.Complete(context.Background(), first); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	second := userRequest()
	second.SpaceID = "tm_two"
	if _, err := svc.Complete(context.Background(), second); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if len(client.scopes) != 2 {
		t.Fatalf("saw %d calls, want 2", len(client.scopes))
	}
	if client.scopes[0] == client.scopes[1] {
		t.Errorf("two spaces shared a cache scope: %q", client.scopes[0])
	}
	if client.scopes[0] != "tm_one" || client.scopes[1] != "tm_two" {
		t.Errorf("scopes = %v, want the resolved spaces", client.scopes)
	}
}

// profileClient records the scope and profile each call arrived with.
type profileClient struct {
	scopes   []string
	profiles []cllm.CallProfile
}

func (c *profileClient) ChatCompletionBlocking(_ context.Context, req cllm.Request) (cllm.Completion, error) {
	c.scopes = append(c.scopes, req.CacheScope)
	c.profiles = append(c.profiles, req.Profile)
	return cllm.Completion{Content: "ok"}, nil
}

func (c *profileClient) ChatCompletionStreaming(_ context.Context, req cllm.Request, onDelta func(string)) (cllm.Completion, error) {
	return c.ChatCompletionBlocking(context.Background(), req)
}

func (c *profileClient) ContextWindow() int { return 0 }
