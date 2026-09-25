package agentapp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/icloudbb/buildmax/internal/infra/llm"
	"github.com/icloudbb/buildmax/internal/infra/llmremote"
)

const testServerURL = "https://buildmax.example.com"

func directEntry() config.ModelEntry {
	return config.ModelEntry{
		Model:  "openai/gpt-4o-mini",
		Name:   "Direct",
		APIURL: "https://openrouter.ai/api/v1",
		APIKey: "provider-key",
	}
}

// managedEntry is what a deployment's model looks like once fetched: a name and
// a context window, and no endpoint or credential — the server holds both.
func managedEntry() config.ModelEntry {
	return config.ModelEntry{
		Model:         "Fast",
		Name:          "Fast",
		ContextWindow: 128_000,
	}
}

// localCacheFor is a cache in local mode: no server, every model called from
// this machine with its own credential.
func localCacheFor(entries []config.ModelEntry) *LLMClientCache {
	return &LLMClientCache{
		settings: config.Settings{Models: entries},
		surface:  "cli",
		clients:  make(map[string]cllm.LLMClient),
	}
}

// managedCacheFor is a cache in managed mode: one deployment serves every model
// in the list.
func managedCacheFor(entries []config.ModelEntry, token ManagedTokenFunc) *LLMClientCache {
	return &LLMClientCache{
		settings:         config.Settings{Models: entries},
		managedServerURL: testServerURL,
		managedToken:     token,
		surface:          "cli",
		clients:          make(map[string]cllm.LLMClient),
	}
}

func TestBuildsADirectClient(t *testing.T) {
	cache := localCacheFor([]config.ModelEntry{directEntry()})

	client, err := cache.Get("Direct")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if _, ok := client.(*llm.Client); !ok {
		t.Errorf("direct entry produced %T", client)
	}
}

func TestBuildsAManagedClient(t *testing.T) {
	var askedFor string
	cache := managedCacheFor([]config.ModelEntry{managedEntry()}, func(serverURL string) (string, error) {
		askedFor = serverURL
		return "token", nil
	})

	client, err := cache.Get("Fast")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if _, ok := client.(*llmremote.Client); !ok {
		t.Fatalf("managed mode produced %T", client)
	}
	if askedFor != testServerURL {
		t.Errorf("the token was requested for %q", askedFor)
	}
}

// TestTheModeDecidesTheTransport is what replaced a per-entry transport: the
// same entry builds a direct client in one mode and a remote one in the other,
// because where a prompt goes is a property of the app, not of the model.
func TestTheModeDecidesTheTransport(t *testing.T) {
	entry := managedEntry()
	entry.APIURL = "https://openrouter.ai/api/v1"
	entry.APIKey = "provider-key"

	local, err := localCacheFor([]config.ModelEntry{entry}).Get("Fast")
	if err != nil {
		t.Fatalf("local Get: %v", err)
	}
	if _, ok := local.(*llm.Client); !ok {
		t.Errorf("local mode produced %T", local)
	}

	managed, err := managedCacheFor([]config.ModelEntry{entry},
		func(string) (string, error) { return "token", nil }).Get("Fast")
	if err != nil {
		t.Fatalf("managed Get: %v", err)
	}
	if _, ok := managed.(*llmremote.Client); !ok {
		t.Errorf("managed mode produced %T", managed)
	}
}

// TestManagedModeNeverFallsBackToDirect is the invariant behind having two
// modes: a managed call that cannot be authorized must fail, not quietly reach
// a provider by some other route.
func TestManagedModeNeverFallsBackToDirect(t *testing.T) {
	// An entry carrying a usable provider endpoint and key, so a fallback would
	// succeed if one existed.
	entry := managedEntry()
	entry.APIURL = "https://openrouter.ai/api/v1"
	entry.APIKey = "provider-key"

	tests := []struct {
		name   string
		token  ManagedTokenFunc
		wantIn string
	}{
		{
			name:   "surface cannot authenticate at all",
			token:  nil,
			wantIn: "cannot authenticate",
		},
		{
			name:   "login belongs elsewhere",
			token:  func(string) (string, error) { return "", errors.New("logged in to another server") },
			wantIn: "another server",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cache := managedCacheFor([]config.ModelEntry{entry}, tc.token)
			client, err := cache.Get(entry.Name)
			if err == nil {
				t.Fatalf("a managed mode that cannot authenticate produced %T", client)
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Errorf("error %q does not mention %q", err, tc.wantIn)
			}
		})
	}
}

// TestManagedEntryCarriesNoProviderCredential records what a fetched model is:
// a name and a window. The credential arrives from the login state.
func TestManagedEntryCarriesNoProviderCredential(t *testing.T) {
	cfg := toModelConfig(managedEntry())
	if cfg.APIKey != "" {
		t.Errorf("a managed entry carries an api_key: %q", cfg.APIKey)
	}
	if cfg.BaseURL != "" {
		t.Errorf("a managed entry carries an endpoint: %q", cfg.BaseURL)
	}
	if cfg.ProviderModel != "Fast" {
		t.Errorf("ProviderModel = %q, want the catalog name", cfg.ProviderModel)
	}
}

func TestDirectEntryStillRequiresAnAPIKey(t *testing.T) {
	entry := directEntry()
	entry.APIKey = ""
	cache := localCacheFor([]config.ModelEntry{entry})

	if _, err := cache.Get(entry.Name); err == nil {
		t.Error("a direct entry with no api_key built a client")
	}
}

// runScopedCacheFor builds the cache a task run gets: managed calls carry a run
// token and name a run rather than a user.
func runScopedCacheFor(entries []config.ModelEntry, taskRunID string) *LLMClientCache {
	return &LLMClientCache{
		settings:         config.Settings{Models: entries},
		managedServerURL: testServerURL,
		managedToken:     func(string) (string, error) { return "run-token", nil },
		managedTaskRunID: taskRunID,
		surface:          "worker",
		clients:          make(map[string]cllm.LLMClient),
	}
}

// TestRunScopedManagedEntryCallsAsItsRun is the difference between a worker and
// every other surface: a run authenticates with its run token on the worker
// route, so it cannot list models and the server derives user, space, and task
// from the credential rather than from configuration.
func TestRunScopedManagedEntryCallsAsItsRun(t *testing.T) {
	client, err := runScopedCacheFor([]config.ModelEntry{managedEntry()}, "r_1").Get("Fast")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	remote, ok := client.(*llmremote.Client)
	if !ok {
		t.Fatalf("a run-scoped managed entry produced %T", client)
	}
	if _, err := remote.Models(context.Background()); err == nil {
		t.Error("a run-scoped client offered model discovery")
	}
}

// TestRunScopedDirectEntryIsUnaffected keeps the direct path intact: a worker
// with a provider key behaves exactly as before, run scope or not.
func TestRunScopedDirectEntryIsUnaffected(t *testing.T) {
	client, err := localCacheFor([]config.ModelEntry{directEntry()}).Get("Direct")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if _, ok := client.(*llm.Client); !ok {
		t.Errorf("direct entry produced %T", client)
	}
}

// TestBuildsALocalClientWithoutACredential is the other half of the credential
// exemption: a local entry must build, and a hosted one with the same gap must
// still fail here rather than at the first prompt.
func TestBuildsALocalClientWithoutACredential(t *testing.T) {
	local := config.ModelEntry{
		Model:    "qwen3:8b",
		Name:     "Local",
		Provider: cllm.ProviderOllama,
		APIURL:   "http://127.0.0.1:11434",
		// Set so building the client asks the daemon nothing.
		ContextWindow: 32_000,
	}
	cache := localCacheFor([]config.ModelEntry{local})
	client, err := cache.Get("Local")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := client.(*llm.Client).Provider(); got != cllm.ProviderOllama {
		t.Errorf("provider = %q, want %q", got, cllm.ProviderOllama)
	}

	hosted := config.ModelEntry{Model: "openai/gpt-4o-mini", Name: "Hosted", APIURL: "https://example.test/v1"}
	if _, err := localCacheFor([]config.ModelEntry{hosted}).Get("Hosted"); err == nil {
		t.Error("a hosted entry with no api_key should fail at selection")
	}
}

// TestDefaultModelSelectsByName is the local half of section 7: a new session
// starts on the model default_model names, not on the first entry.
func TestDefaultModelSelectsByName(t *testing.T) {
	fast := directEntry()
	deep := directEntry()
	deep.Name = "Deep"
	deep.Model = "openai/gpt-5"

	settings := config.Settings{Models: []config.ModelEntry{fast, deep}, DefaultModel: "Deep"}
	if got := DefaultModelName(settings); got != "Deep" {
		t.Errorf("DefaultModelName = %q, want %q", got, "Deep")
	}

	// By provider model id as well, which is what --model already accepts.
	settings.DefaultModel = "openai/gpt-5"
	if got := DefaultModelName(settings); got != "Deep" {
		t.Errorf("DefaultModelName by model id = %q, want %q", got, "Deep")
	}

	// Naming nothing falls through to the first entry rather than to no model:
	// a picker that returns nothing is worse than one that returns the wrong
	// first choice, and `buildmax doctor` reports the mismatch.
	settings.DefaultModel = "Gone"
	if got := DefaultModelName(settings); got != "Direct" {
		t.Errorf("DefaultModelName with an unknown default = %q, want the first entry", got)
	}

	settings.DefaultModel = ""
	if got := DefaultModelName(settings); got != "Direct" {
		t.Errorf("DefaultModelName with no default = %q, want the first entry", got)
	}
}

// A managed session prices itself from the rates the deployment sent with its
// model list. Returning nothing here left every signed-in surface reporting
// "not priced" for calls the ledger had priced.
func TestManagedSessionIsPricedFromTheDeploymentsRates(t *testing.T) {
	entry := managedEntry()
	entry.Pricing = &config.ModelPricing{Currency: "USD", InputPerMTok: "0.2", OutputPerMTok: "1.2"}
	mgr := NewSessionManager(t.TempDir())
	app := &AgentApp{
		sessionManager: mgr,
		settings:       config.Settings{Models: []config.ModelEntry{entry}},
		llmClients:     &LLMClientCache{managedServerURL: "https://buildmax.example.com"},
	}
	sess, err := mgr.Create("Fast")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })

	got := app.pricingFor(sess)
	if got.Currency != "USD" || got.InputPerMTok != 200_000_000 || got.OutputPerMTok != 1_200_000_000 {
		t.Errorf("pricingFor = %+v, want the deployment's rates", got)
	}
}
