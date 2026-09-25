package worker

// Fixtures copied from the root package rather than imported: a test helper
// shared across a package boundary makes the test boundary softer than the
// code's, which is what splitting these packages was for.

import (
	"context"
	"testing"
	"time"

	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
	"github.com/icloudbb/buildmax/internal/server/authtoken"
	"github.com/icloudbb/buildmax/internal/service/llmgateway"
)

const (
	// One secret signs every credential this package presents, because one
	// credential now opens every worker route.
	workerTestSecret = "test-worker-secret"
	llmTestUser      = "u_llm"
	llmTestSpace     = "tm_llm"
)

// runTokenFor mints what the scheduler would have handed the worker executing
// this run. Every worker route is scoped to the run named in its path, so a
// test that calls one needs that run's own token and no other.
func runTokenFor(t *testing.T, taskRunID, taskID string) string {
	t.Helper()
	token, err := authtoken.MintRun(workerTestSecret, authtoken.RunClaims{
		UserID:    llmTestUser,
		SpaceID:   llmTestSpace,
		TaskRunID: taskRunID,
		TaskID:    taskID,
	}, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("MintRun: %v", err)
	}
	return token
}

// llmStubClient answers every call the same way.
func llmTestService(t *testing.T, client cllm.LLMClient, quota llmgateway.QuotaChecker) *llmgateway.Service {
	t.Helper()

	fast := llmgateway.Target{
		ID:            "mt_fast",
		Name:          "Fast",
		ProviderType:  cllm.ProviderOpenAICompatible,
		Endpoint:      "https://SECRET-ENDPOINT.internal/v1",
		CredentialRef: "SECRET-CREDENTIAL",
		UpstreamModel: "SECRET-UPSTREAM-MODEL",
		Capabilities:  llmgateway.NewCapabilitySet(llmgateway.BaselineCapabilities()...),
		Enabled:       true,
	}
	catalog, err := llmgateway.NewStaticCatalog([]llmgateway.Target{fast})
	if err != nil {
		t.Fatalf("NewStaticCatalog: %v", err)
	}
	return &llmgateway.Service{
		Router: &llmgateway.Router{
			Resolver: &llmgateway.Resolver{Catalog: catalog, DefaultModel: "Fast"},
			Factory: func(context.Context, llmgateway.Target) (cllm.LLMClient, error) {
				return client, nil
			},
		},
		Ledger: &llmStubLedger{},
		Quota:  quota,
	}
}

// llmStubClient answers every call the same way.
type llmStubClient struct {
	content string
	deltas  []string
	usage   cllm.Usage
	err     error
	// gotProfile is what the gateway passed the provider client, so a test can
	// check that a worker's stated intent survived the route.
	gotProfile cllm.CallProfile
}

func (c *llmStubClient) ChatCompletionBlocking(_ context.Context, req cllm.Request) (cllm.Completion, error) {
	c.gotProfile = req.Profile
	if c.err != nil {
		return cllm.Completion{}, c.err
	}
	return cllm.Completion{Content: c.content, Usage: c.usage}, nil
}
func (c *llmStubClient) ChatCompletionStreaming(_ context.Context, req cllm.Request, onDelta func(string)) (cllm.Completion, error) {
	c.gotProfile = req.Profile
	for _, delta := range c.deltas {
		onDelta(delta)
	}
	if c.err != nil {
		return cllm.Completion{}, c.err
	}
	return cllm.Completion{Content: c.content, Usage: c.usage}, nil
}
func (c *llmStubClient) ContextWindow() int { return 0 }

// llmStubLedger accepts every write and keeps the last one so a test can check
// what a call was attributed to.
type llmStubLedger struct {
	opened  int
	last    coregw.Call
	calls   []coregw.Call
	listErr error
}

func (l *llmStubLedger) OpenLLMCall(_ context.Context, call *coregw.Call) (*coregw.Call, error) {
	l.opened++
	stored := *call
	stored.ID = "lc_stub"
	l.last = stored
	return &stored, nil
}
func (l *llmStubLedger) CompleteLLMCall(context.Context, string, coregw.CallOutcome) error {
	return nil
}
func (l *llmStubLedger) GetLLMCall(context.Context, string) (*coregw.Call, error) { return nil, nil }

func (l *llmStubLedger) GetLLMCallByClientID(context.Context, string, string) (*coregw.Call, error) {
	return nil, nil
}
func (l *llmStubLedger) ListLLMCallsByTaskRun(_ context.Context, taskRunID string) ([]coregw.Call, error) {
	if l.listErr != nil {
		return nil, l.listErr
	}
	var out []coregw.Call
	for _, call := range l.calls {
		if call.TaskRunID != nil && *call.TaskRunID == taskRunID {
			out = append(out, call)
		}
	}
	return out, nil
}
func (l *llmStubLedger) SearchLLMCalls(context.Context, coregw.CallFilter, int, int) ([]coregw.Call, int, error) {
	return nil, 0, nil
}

func (l *llmStubLedger) SummarizeForegroundLLMCalls(context.Context, string, time.Time) ([]coregw.CallTotals, error) {
	return nil, nil
}
