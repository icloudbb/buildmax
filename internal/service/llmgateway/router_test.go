package llmgateway_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/icloudbb/buildmax/internal/service/llmgateway"
)

// fakeLLMClient is a distinguishable core client. Routing never calls it; the
// tests only check which instance came back.
type fakeLLMClient struct{ builtFor string }

func (c *fakeLLMClient) ChatCompletionBlocking(context.Context, cllm.Request) (cllm.Completion, error) {
	return cllm.Completion{}, nil
}

func (c *fakeLLMClient) ChatCompletionStreaming(context.Context, cllm.Request, func(string)) (cllm.Completion, error) {
	return cllm.Completion{}, nil
}

func (c *fakeLLMClient) ContextWindow() int { return 0 }

// countingFactory records how often it built a client.
type countingFactory struct {
	mu    sync.Mutex
	calls int
	err   error
	nilOK bool
}

func (f *countingFactory) build(_ context.Context, target llmgateway.Target) (cllm.LLMClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if f.nilOK {
		return nil, nil
	}
	return &fakeLLMClient{builtFor: target.ID}, nil
}

func (f *countingFactory) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// mutableCatalog serves one target that a test can change, standing in for a
// catalog an operator edits at runtime.
type mutableCatalog struct {
	mu     sync.Mutex
	target llmgateway.Target
}

func (c *mutableCatalog) Target(_ context.Context, id string) (llmgateway.Target, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if id != c.target.ID {
		return llmgateway.Target{}, llmgateway.ErrTargetNotFound
	}
	return c.target, nil
}

func (c *mutableCatalog) TargetByName(_ context.Context, name string) (llmgateway.Target, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if name != c.target.Name {
		return llmgateway.Target{}, llmgateway.ErrTargetNotFound
	}
	return c.target, nil
}

func (c *mutableCatalog) List(_ context.Context) ([]llmgateway.Target, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return []llmgateway.Target{c.target}, nil
}

func (c *mutableCatalog) set(target llmgateway.Target) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.target = target
}

func testRouter(t *testing.T, factory *countingFactory) *llmgateway.Router {
	t.Helper()
	return &llmgateway.Router{Resolver: testResolver(t), Factory: factory.build}
}

func TestClientForReturnsResolutionAndClient(t *testing.T) {
	factory := &countingFactory{}
	router := testRouter(t, factory)

	routed, err := router.ClientFor(context.Background(), llmgateway.ResolveRequest{Name: "Deep"})
	if err != nil {
		t.Fatalf("ClientFor: %v", err)
	}
	if routed.Resolution.Name != "Deep" {
		t.Errorf("Name = %q, want %q", routed.Resolution.Name, "Deep")
	}
	if routed.Resolution.Target.ID != "mt_deep" {
		t.Errorf("Target.ID = %q, want %q", routed.Resolution.Target.ID, "mt_deep")
	}
	client, ok := routed.Client.(*fakeLLMClient)
	if !ok {
		t.Fatalf("Client is %T, want *fakeLLMClient", routed.Client)
	}
	if client.builtFor != "mt_deep" {
		t.Errorf("client built for %q, want %q", client.builtFor, "mt_deep")
	}
}

func TestClientForCachesPerTarget(t *testing.T) {
	factory := &countingFactory{}
	router := testRouter(t, factory)
	ctx := context.Background()

	first, err := router.ClientFor(ctx, llmgateway.ResolveRequest{Name: "Fast"})
	if err != nil {
		t.Fatalf("ClientFor: %v", err)
	}
	second, err := router.ClientFor(ctx, llmgateway.ResolveRequest{})
	if err != nil {
		t.Fatalf("ClientFor: %v", err)
	}
	if first.Client != second.Client {
		t.Error("the same target produced two clients")
	}
	if factory.count() != 1 {
		t.Errorf("factory called %d times, want 1", factory.count())
	}

	other, err := router.ClientFor(ctx, llmgateway.ResolveRequest{Name: "Deep"})
	if err != nil {
		t.Fatalf("ClientFor: %v", err)
	}
	if other.Client == first.Client {
		t.Error("different targets shared a client")
	}
	if factory.count() != 2 {
		t.Errorf("factory called %d times, want 2", factory.count())
	}
}

func TestClientForRebuildsWhenTargetChanges(t *testing.T) {
	target := validTarget()
	catalog := &mutableCatalog{}
	catalog.set(target)

	factory := &countingFactory{}
	// No configured default, so every request resolves to the catalog's only
	// row whatever it is currently named — this test is about the client cache,
	// not about name resolution, and it renames the target part-way through.
	router := &llmgateway.Router{
		Resolver: &llmgateway.Resolver{Catalog: catalog},
		Factory:  factory.build,
	}
	ctx := context.Background()
	req := llmgateway.ResolveRequest{}

	first, err := router.ClientFor(ctx, req)
	if err != nil {
		t.Fatalf("ClientFor: %v", err)
	}

	// A rotated credential must not keep serving calls through the old client.
	rotated := target
	rotated.CredentialRef = "rotated_key"
	catalog.set(rotated)

	second, err := router.ClientFor(ctx, req)
	if err != nil {
		t.Fatalf("ClientFor after rotation: %v", err)
	}
	if second.Client == first.Client {
		t.Error("a rotated credential reused the cached client")
	}
	if factory.count() != 2 {
		t.Errorf("factory called %d times, want 2", factory.count())
	}

	// In the catalog a replaced key keeps the same CredentialRef (the row's
	// ID); only the row's revision moves. That alone must retire the client.
	sameRef := rotated
	sameRef.Revision = rotated.Revision.Add(time.Second)
	catalog.set(sameRef)
	replaced, err := router.ClientFor(ctx, req)
	if err != nil {
		t.Fatalf("ClientFor after a replaced key: %v", err)
	}
	if replaced.Client == second.Client {
		t.Error("a key replaced in place reused the client built with the old one")
	}
	second = replaced
	rotated = sameRef
	// The later steps count from the two builds before this one.
	factory.mu.Lock()
	factory.calls = 2
	factory.mu.Unlock()

	// An output cap is a construction detail: a client built with the old one
	// would keep capping responses after the operator changed it.
	capped := rotated
	capped.MaxTokens = 4096
	catalog.set(capped)

	third, err := router.ClientFor(ctx, req)
	if err != nil {
		t.Fatalf("ClientFor after a max-tokens change: %v", err)
	}
	if third.Client == second.Client {
		t.Error("a changed output cap reused the cached client")
	}
	if factory.count() != 3 {
		t.Errorf("factory called %d times, want 3", factory.count())
	}

	// The cache policy changes what the client puts in every request, so an
	// operator who turns caching off must not keep a client that still asks
	// for it — and pays for it.
	cached := capped
	cached.CacheMode = "off"
	catalog.set(cached)

	fourth, err := router.ClientFor(ctx, req)
	if err != nil {
		t.Fatalf("ClientFor after a cache-mode change: %v", err)
	}
	if fourth.Client == third.Client {
		t.Error("a changed cache mode reused the cached client")
	}
	if factory.count() != 4 {
		t.Errorf("factory called %d times, want 4", factory.count())
	}

	retained := cached
	retained.CacheMode = "auto"
	retained.CacheTTL = "1h"
	catalog.set(retained)

	fifth, err := router.ClientFor(ctx, req)
	if err != nil {
		t.Fatalf("ClientFor after a cache-ttl change: %v", err)
	}
	if fifth.Client == fourth.Client {
		t.Error("a changed cache retention reused the cached client")
	}
	if factory.count() != 5 {
		t.Errorf("factory called %d times, want 5", factory.count())
	}

	// A change that does not affect construction keeps the cached client.
	renamed := retained
	renamed.Name = "Renamed"
	catalog.set(renamed)

	sixth, err := router.ClientFor(ctx, req)
	if err != nil {
		t.Fatalf("ClientFor after rename: %v", err)
	}
	if sixth.Client != fifth.Client {
		t.Error("a display-name change rebuilt the client")
	}
	if factory.count() != 5 {
		t.Errorf("factory called %d times, want 5", factory.count())
	}
}

func TestClientForPropagatesResolutionErrors(t *testing.T) {
	factory := &countingFactory{}
	router := testRouter(t, factory)

	tests := []struct {
		name string
		req  llmgateway.ResolveRequest
		want error
	}{
		{
			name: "unknown model",
			req:  llmgateway.ResolveRequest{Name: "Reasoning"},
			want: llmgateway.ErrTargetNotFound,
		},
		{
			name: "disabled target",
			req:  llmgateway.ResolveRequest{Name: "Retired"},
			want: llmgateway.ErrTargetDisabled,
		},
		{
			name: "unsupported capability",
			req: llmgateway.ResolveRequest{
				Name:     "Deep",
				Requires: []llmgateway.Capability{llmgateway.CapabilityToolCalls},
			},
			want: llmgateway.ErrCapabilityUnsupported,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := router.ClientFor(context.Background(), tc.req); !errors.Is(err, tc.want) {
				t.Fatalf("ClientFor: want %v, got %v", tc.want, err)
			}
		})
	}

	if factory.count() != 0 {
		t.Errorf("factory ran %d times for rejected requests, want 0", factory.count())
	}
}

func TestClientForFactoryFailures(t *testing.T) {
	boom := errors.New("no credential for upstream_key")
	failing := testRouter(t, &countingFactory{err: boom})
	if _, err := failing.ClientFor(context.Background(), llmgateway.ResolveRequest{}); !errors.Is(err, boom) {
		t.Errorf("want the factory error, got %v", err)
	}

	// A factory that returns nothing must not hand a nil client to the caller.
	empty := testRouter(t, &countingFactory{nilOK: true})
	if _, err := empty.ClientFor(context.Background(), llmgateway.ResolveRequest{}); !errors.Is(err, llmgateway.ErrFactoryNotConfigured) {
		t.Errorf("want ErrFactoryNotConfigured, got %v", err)
	}
}

func TestRouterNotConfigured(t *testing.T) {
	var nilRouter *llmgateway.Router
	if _, err := nilRouter.ClientFor(context.Background(), llmgateway.ResolveRequest{}); !errors.Is(err, llmgateway.ErrCatalogNotConfigured) {
		t.Errorf("nil router: want ErrCatalogNotConfigured, got %v", err)
	}
	if _, err := nilRouter.Available(context.Background()); !errors.Is(err, llmgateway.ErrCatalogNotConfigured) {
		t.Errorf("nil router Available: want ErrCatalogNotConfigured, got %v", err)
	}

	noFactory := &llmgateway.Router{Resolver: testResolver(t)}
	if _, err := noFactory.ClientFor(context.Background(), llmgateway.ResolveRequest{}); !errors.Is(err, llmgateway.ErrFactoryNotConfigured) {
		t.Errorf("want ErrFactoryNotConfigured, got %v", err)
	}

	noResolver := &llmgateway.Router{Factory: (&countingFactory{}).build}
	if _, err := noResolver.ClientFor(context.Background(), llmgateway.ResolveRequest{}); !errors.Is(err, llmgateway.ErrCatalogNotConfigured) {
		t.Errorf("want ErrCatalogNotConfigured, got %v", err)
	}
}

func TestClientForTargetSharesTheClientCache(t *testing.T) {
	factory := &countingFactory{}
	router := testRouter(t, factory)
	ctx := context.Background()

	byAlias, err := router.ClientFor(ctx, llmgateway.ResolveRequest{Name: "Deep"})
	if err != nil {
		t.Fatalf("ClientFor: %v", err)
	}
	byTarget, err := router.ClientForTarget(ctx, "mt_deep", nil)
	if err != nil {
		t.Fatalf("ClientForTarget: %v", err)
	}

	// Server-owned inference and a space call on the same target must not open
	// two clients, or the two paths can drift apart.
	if byAlias.Client != byTarget.Client {
		t.Error("alias and target resolution produced different clients")
	}
	if factory.count() != 1 {
		t.Errorf("factory called %d times, want 1", factory.count())
	}
	if byTarget.Resolution.Name != "" {
		t.Errorf("Alias = %q, want empty for a deployment-owned target", byTarget.Resolution.Name)
	}
}

func TestClientForTargetErrors(t *testing.T) {
	factory := &countingFactory{}
	router := testRouter(t, factory)

	if _, err := router.ClientForTarget(context.Background(), "mt_retired", nil); !errors.Is(err, llmgateway.ErrTargetDisabled) {
		t.Errorf("want ErrTargetDisabled, got %v", err)
	}
	if _, err := router.ClientForTarget(context.Background(), "mt_gone", nil); !errors.Is(err, llmgateway.ErrTargetNotFound) {
		t.Errorf("want ErrTargetNotFound, got %v", err)
	}
	if factory.count() != 0 {
		t.Errorf("factory ran %d times for rejected targets, want 0", factory.count())
	}

	var nilRouter *llmgateway.Router
	if _, err := nilRouter.ClientForTarget(context.Background(), "mt_fast", nil); !errors.Is(err, llmgateway.ErrCatalogNotConfigured) {
		t.Errorf("nil router: want ErrCatalogNotConfigured, got %v", err)
	}
	noFactory := &llmgateway.Router{Resolver: testResolver(t)}
	if _, err := noFactory.ClientForTarget(context.Background(), "mt_fast", nil); !errors.Is(err, llmgateway.ErrFactoryNotConfigured) {
		t.Errorf("want ErrFactoryNotConfigured, got %v", err)
	}
}

func TestRouterAvailableDelegates(t *testing.T) {
	router := testRouter(t, &countingFactory{})

	models, err := router.Available(context.Background())
	if err != nil {
		t.Fatalf("Available: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("Available returned %d models, want 2", len(models))
	}
}

func TestClientForIsSafeUnderConcurrency(t *testing.T) {
	factory := &countingFactory{}
	router := testRouter(t, factory)

	var wg sync.WaitGroup
	clients := make([]cllm.LLMClient, 16)
	for i := range clients {
		wg.Go(func() {
			routed, err := router.ClientFor(context.Background(), llmgateway.ResolveRequest{})
			if err != nil {
				t.Errorf("ClientFor: %v", err)
				return
			}
			clients[i] = routed.Client
		})
	}
	wg.Wait()

	if factory.count() != 1 {
		t.Errorf("factory called %d times, want 1", factory.count())
	}
	for i, client := range clients {
		if client != clients[0] {
			t.Fatalf("goroutine %d got a different client", i)
		}
	}
}
