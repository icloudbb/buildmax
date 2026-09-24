package bootstrap

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
	httpserver "github.com/icloudbb/buildmax/internal/server"
	"github.com/icloudbb/buildmax/internal/service/llmgateway"
)

func conversationModel() config.ServerModelEntry {
	return config.ServerModelEntry{
		Model:         "openai/gpt-4o",
		Name:          "GPT-4o",
		APIURL:        "https://openrouter.ai/api/v1",
		APIKey:        "conversation-key",
		ContextWindow: 128000,
		CallTimeout:   300,
	}
}

// fakeModels is an in-memory llm_model table.
type fakeModels struct {
	rows        map[string]coregw.Model
	credentials map[string]string
	err         error
}

func newFakeModels(rows ...coregw.Model) *fakeModels {
	m := &fakeModels{rows: map[string]coregw.Model{}, credentials: map[string]string{}}
	for _, r := range rows {
		m.rows[r.ID] = r
		m.credentials[r.ID] = "key-for-" + r.ID
	}
	return m
}

func (m *fakeModels) CreateLLMModel(context.Context, coregw.CreateModelInput) (*coregw.Model, error) {
	return nil, errors.New("not used")
}

func (m *fakeModels) GetLLMModel(_ context.Context, id string) (*coregw.Model, error) {
	if m.err != nil {
		return nil, m.err
	}
	row, ok := m.rows[id]
	if !ok {
		return nil, nil
	}
	return &row, nil
}

func (m *fakeModels) GetLLMModelByName(_ context.Context, name string) (*coregw.Model, error) {
	if m.err != nil {
		return nil, m.err
	}
	for _, row := range m.rows {
		if row.Name == name {
			return &row, nil
		}
	}
	return nil, nil
}

// Sorted by ID, so a listing and the default it implies do not move between
// calls the way ranging over a map would.
func (m *fakeModels) ListLLMModels(context.Context) ([]coregw.Model, error) {
	if m.err != nil {
		return nil, m.err
	}
	ids := make([]string, 0, len(m.rows))
	for id := range m.rows {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	out := make([]coregw.Model, 0, len(ids))
	for _, id := range ids {
		out = append(out, m.rows[id])
	}
	return out, nil
}

func (m *fakeModels) SetLLMModelEnabled(context.Context, string, bool) error { return nil }

func (m *fakeModels) SetLLMModelCredential(_ context.Context, id, key string) error {
	m.credentials[id] = key
	return nil
}

func (m *fakeModels) LLMModelCredential(_ context.Context, id string) (string, error) {
	key, ok := m.credentials[id]
	if !ok {
		return "", errors.New("model not found")
	}
	return key, nil
}

func catalogRow(id string) coregw.Model {
	return coregw.Model{
		ID:            id,
		Name:          strings.ToUpper(id),
		ProviderType:  cllm.ProviderOpenAICompatible,
		APIURL:        "https://upstream.example.com/v1",
		Model:         "vendor/" + id,
		ContextWindow: 64000,
		CallTimeout:   120,
		Capabilities:  []string{"text_chat", "tool_calls", "streaming_text", "usage_reporting"},
		Enabled:       true,
	}
}

// TestBuildLLMRoutingWithoutConfiguration is the regression guard for a
// deployment that has neither a database nor a conversation model.
func TestBuildLLMRoutingWithoutConfiguration(t *testing.T) {
	routing, err := buildLLMRouting(config.ServerConfig{}, nil)
	if err != nil {
		t.Fatalf("buildLLMRouting: %v", err)
	}
	if routing != nil {
		t.Fatalf("routing = %+v, want nil", routing)
	}
}

// TestBuildLLMRoutingDerivesConversationTarget covers the deployment that only
// configures conversation.model, which is every server built before this
// feature and every fresh one whose catalog is still empty.
func TestBuildLLMRoutingDerivesConversationTarget(t *testing.T) {
	sc := config.ServerConfig{Conversation: config.ServerConvConfig{Model: conversationModel()}}

	routing, err := buildLLMRouting(sc, newFakeModels())
	if err != nil {
		t.Fatalf("buildLLMRouting: %v", err)
	}
	if routing.Tier1TargetID != conversationTargetID {
		t.Errorf("Tier1TargetID = %q, want %q", routing.Tier1TargetID, conversationTargetID)
	}

	routed, err := routing.Router.ClientForTarget(context.Background(), routing.Tier1TargetID, llmgateway.BaselineCapabilities())
	if err != nil {
		t.Fatalf("ClientForTarget: %v", err)
	}
	if routed.Client == nil {
		t.Fatal("no client for the conversation target")
	}
	if got := routed.Resolution.Target.UpstreamModel; got != "openai/gpt-4o" {
		t.Errorf("UpstreamModel = %q, want %q", got, "openai/gpt-4o")
	}
	if routed.Resolution.Name != "" {
		t.Errorf("Name = %q, want empty for a deployment-owned target", routed.Resolution.Name)
	}
}

// TestBuildLLMRoutingServesStoredModels is the point of this change: the
// catalog comes from the database, not from server.yaml.
func TestBuildLLMRoutingServesStoredModels(t *testing.T) {
	sc := config.ServerConfig{
		Conversation: config.ServerConvConfig{Model: conversationModel()},
		LLM:          config.ServerLLMConfig{DefaultModel: "LM_FAST"},
	}

	routing, err := buildLLMRouting(sc, newFakeModels(catalogRow("lm_fast")))
	if err != nil {
		t.Fatalf("buildLLMRouting: %v", err)
	}

	routed, err := routing.Router.ClientFor(context.Background(), llmgateway.ResolveRequest{})
	if err != nil {
		t.Fatalf("ClientFor: %v", err)
	}
	if routed.Resolution.Target.ID != "lm_fast" {
		t.Errorf("Target.ID = %q, want %q", routed.Resolution.Target.ID, "lm_fast")
	}
	if routed.Resolution.Target.UpstreamModel != "vendor/lm_fast" {
		t.Errorf("UpstreamModel = %q", routed.Resolution.Target.UpstreamModel)
	}
	// The credential reference is the model ID; the key itself stays in the
	// store until the factory asks for it.
	if routed.Resolution.Target.CredentialRef != "lm_fast" {
		t.Errorf("CredentialRef = %q, want the model ID", routed.Resolution.Target.CredentialRef)
	}
}

// TestBuildLLMRoutingSurvivesAnEmptyCatalog records the state a fresh
// deployment is in before an operator runs `buildmax-server model add`: the
// model derived from conversation.model is the whole catalog, and it answers.
func TestBuildLLMRoutingSurvivesAnEmptyCatalog(t *testing.T) {
	sc := config.ServerConfig{Conversation: config.ServerConvConfig{Model: conversationModel()}}

	routing, err := buildLLMRouting(sc, newFakeModels())
	if err != nil {
		t.Fatalf("an empty catalog failed startup: %v", err)
	}
	// Tier 1 still works.
	if _, err := routing.Router.ClientForTarget(context.Background(), conversationTargetID, nil); err != nil {
		t.Errorf("ClientForTarget: %v", err)
	}
	routed, err := routing.Router.ClientFor(context.Background(), llmgateway.ResolveRequest{})
	if err != nil {
		t.Fatalf("ClientFor: %v", err)
	}
	if routed.Resolution.Name != "GPT-4o" {
		t.Errorf("Name = %q, want the derived conversation model", routed.Resolution.Name)
	}
	models, err := routing.Router.Available(context.Background())
	if err != nil {
		t.Fatalf("Available: %v", err)
	}
	if len(models) != 1 || models[0].Name != "GPT-4o" || !models[0].Default {
		t.Errorf("Available = %+v, want the derived model marked default", models)
	}
}

// TestBuildLLMRoutingConfiguredDefaultMustExist is the startup guard: a name in
// server.yaml that resolves to nothing is a configuration mistake.
func TestBuildLLMRoutingConfiguredDefaultMustExist(t *testing.T) {
	sc := config.ServerConfig{
		Conversation: config.ServerConvConfig{Model: conversationModel()},
		LLM:          config.ServerLLMConfig{DefaultModel: "Gone"},
	}

	routing, err := buildLLMRouting(sc, newFakeModels(catalogRow("lm_fast")))
	if err != nil {
		t.Fatalf("buildLLMRouting: %v", err)
	}
	// buildLLMRouting itself does not read the catalog; the wiring step does.
	if err := validateConfiguredModels(context.Background(), routing, sc); err == nil {
		t.Fatal("a default_model naming no row was accepted")
	} else if !strings.Contains(err.Error(), "llm.default_model") {
		t.Errorf("the error does not name the field: %v", err)
	}

	sc.LLM.DefaultModel = "LM_FAST"
	if err := validateConfiguredModels(context.Background(), routing, sc); err != nil {
		t.Errorf("a default_model naming a real row was rejected: %v", err)
	}
}

// TestResolveTier1TargetIDByName is the point of conversation.model_target
// accepting a name: an operator points Tier 1 at a seeded catalog row by the
// name it was added with, without discovering the runtime ID first.
func TestResolveTier1TargetIDByName(t *testing.T) {
	sc := config.ServerConfig{Conversation: config.ServerConvConfig{Model: conversationModel()}}
	routing, err := buildLLMRouting(sc, newFakeModels(catalogRow("lm_fast")))
	if err != nil {
		t.Fatalf("buildLLMRouting: %v", err)
	}
	catalog := routing.Router.Resolver.Catalog

	// A name resolves to the row's runtime ID.
	if id, err := resolveTier1TargetID(context.Background(), catalog, "LM_FAST"); err != nil {
		t.Fatalf("resolveTier1TargetID by name: %v", err)
	} else if id != "lm_fast" {
		t.Errorf("resolved ID = %q, want %q", id, "lm_fast")
	}

	// An ID passes through unchanged, so a deployment that named one keeps working.
	if id, err := resolveTier1TargetID(context.Background(), catalog, "lm_fast"); err != nil {
		t.Fatalf("resolveTier1TargetID by ID: %v", err)
	} else if id != "lm_fast" {
		t.Errorf("resolved ID = %q, want the ID unchanged", id)
	}

	// The derived conversation target resolves by its own ID.
	if _, err := resolveTier1TargetID(context.Background(), catalog, conversationTargetID); err != nil {
		t.Errorf("the derived target did not resolve: %v", err)
	}

	// A value that is neither an ID nor a name stops startup rather than serving
	// conversations from a model that does not exist.
	if _, err := resolveTier1TargetID(context.Background(), catalog, "nope"); err == nil {
		t.Error("an unresolvable model_target was accepted")
	}
}

// TestBuildLLMRoutingServesEveryCatalogModel records that the catalog is the
// grant: every model in it is callable, with no per-space policy in between.
func TestBuildLLMRoutingServesEveryCatalogModel(t *testing.T) {
	sc := config.ServerConfig{Conversation: config.ServerConvConfig{Model: conversationModel()}}

	routing, err := buildLLMRouting(sc, newFakeModels(catalogRow("lm_fast")))
	if err != nil {
		t.Fatalf("buildLLMRouting: %v", err)
	}
	routed, err := routing.Router.ClientFor(context.Background(), llmgateway.ResolveRequest{Name: "LM_FAST"})
	if err != nil {
		t.Fatalf("ClientFor: %v", err)
	}
	if routed.Resolution.Target.ID != "lm_fast" {
		t.Errorf("Target.ID = %q, want %q", routed.Resolution.Target.ID, "lm_fast")
	}
	if _, err := routing.Router.ClientForTarget(context.Background(), conversationTargetID, nil); err != nil {
		t.Errorf("the deployment's own model stopped resolving: %v", err)
	}
}

func TestBuildLLMRoutingTier1UsesAStoredModel(t *testing.T) {
	sc := config.ServerConfig{
		Conversation: config.ServerConvConfig{Model: conversationModel(), ModelTarget: "lm_fast"},
	}

	routing, err := buildLLMRouting(sc, newFakeModels(catalogRow("lm_fast")))
	if err != nil {
		t.Fatalf("buildLLMRouting: %v", err)
	}
	if routing.Tier1TargetID != "lm_fast" {
		t.Errorf("Tier1TargetID = %q", routing.Tier1TargetID)
	}
	routed, err := routing.Router.ClientForTarget(context.Background(), "lm_fast", llmgateway.BaselineCapabilities())
	if err != nil {
		t.Fatalf("ClientForTarget: %v", err)
	}
	if routed.Resolution.Target.UpstreamModel != "vendor/lm_fast" {
		t.Errorf("UpstreamModel = %q", routed.Resolution.Target.UpstreamModel)
	}
}

// TestStoredModelWithoutACredentialFails keeps a half-written catalog row from
// producing a client pointed at a provider with no key.
func TestStoredModelWithoutACredentialFails(t *testing.T) {
	models := newFakeModels(catalogRow("lm_fast"))
	models.credentials["lm_fast"] = ""

	routing, err := buildLLMRouting(config.ServerConfig{}, models)
	if err != nil {
		t.Fatalf("buildLLMRouting: %v", err)
	}
	_, err = routing.Router.ClientFor(context.Background(), llmgateway.ResolveRequest{Name: "LM_FAST"})
	if err == nil || !strings.Contains(err.Error(), "no credential stored") {
		t.Fatalf("want a missing-credential error, got %v", err)
	}
}

// TestStoredModelRowIsValidatedOnRead covers an incomplete row: it must fail its
// own calls rather than build a client pointed at nothing.
func TestStoredModelRowIsValidatedOnRead(t *testing.T) {
	broken := catalogRow("lm_broken")
	broken.APIURL = ""

	routing, err := buildLLMRouting(config.ServerConfig{}, newFakeModels(broken))
	if err != nil {
		t.Fatalf("buildLLMRouting: %v", err)
	}
	if _, err := routing.Router.ClientFor(context.Background(), llmgateway.ResolveRequest{Name: "LM_BROKEN"}); !errors.Is(err, llmgateway.ErrInvalidCatalog) {
		t.Fatalf("want ErrInvalidCatalog, got %v", err)
	}
}

func TestClientFactoryRejectsUnsupportedProvider(t *testing.T) {
	factory := newClientFactory("conversation-key", nil)

	_, err := factory(context.Background(), llmgateway.Target{
		Name:          "Native",
		ProviderType:  "anthropic_native",
		CredentialRef: conversationCredentialRef,
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported provider") {
		t.Errorf("want an unsupported-provider error, got %v", err)
	}
}

// TestClientFactoryBuildsALocalTargetWithoutACredential is the exemption the
// managed path needs for a local runtime: there is no secret to hold, and
// demanding one would make the provider unusable through the gateway.
func TestClientFactoryBuildsALocalTargetWithoutACredential(t *testing.T) {
	factory := newClientFactory("", nil)

	client, err := factory(context.Background(), llmgateway.Target{
		Name:          "Local",
		ProviderType:  cllm.ProviderOllama,
		Endpoint:      "http://ollama.test:11434",
		UpstreamModel: "qwen3:8b",
		// Set so building the client asks the daemon nothing.
		ContextWindow: 32_000,
		CredentialRef: conversationCredentialRef,
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if client == nil {
		t.Fatal("factory returned no client")
	}
}

// TestClientFactoryStillDemandsACredentialForHostedTargets keeps that exemption
// narrow. A hosted target with no key must fail at selection rather than send
// an unauthenticated request upstream.
func TestClientFactoryStillDemandsACredentialForHostedTargets(t *testing.T) {
	factory := newClientFactory("", nil)

	_, err := factory(context.Background(), llmgateway.Target{
		Name:          "Hosted",
		ProviderType:  cllm.ProviderOpenAICompatible,
		Endpoint:      "https://api.example.test/v1",
		UpstreamModel: "gpt-4o-mini",
		CredentialRef: conversationCredentialRef,
	})
	if err == nil || !strings.Contains(err.Error(), "api_key is not set") {
		t.Errorf("want a missing-credential error, got %v", err)
	}
}

// TestLLMRoutingErrorsCarryNoCredential keeps call diagnostics safe to log.
func TestLLMRoutingErrorsCarryNoCredential(t *testing.T) {
	const secret = "SUPER-SECRET-KEY"

	models := newFakeModels(catalogRow("lm_fast"))
	models.credentials["lm_fast"] = secret
	models.err = errors.New("catalog unavailable")

	routing, err := buildLLMRouting(config.ServerConfig{}, models)
	if err != nil {
		t.Fatalf("buildLLMRouting: %v", err)
	}
	if _, err := routing.Router.ClientFor(context.Background(), llmgateway.ResolveRequest{Name: "LM_FAST"}); err == nil {
		t.Fatal("a failing catalog produced a client")
	} else if strings.Contains(err.Error(), secret) {
		t.Errorf("the error leaked the credential: %v", err)
	}
}

// TestWireLLMPreservesExistingDeployments checks what the handler layer
// actually receives.
func TestWireLLMPreservesExistingDeployments(t *testing.T) {
	var cfg httpserver.Config
	sc := config.ServerConfig{Conversation: config.ServerConvConfig{Model: conversationModel()}}

	if err := wireLLM(&cfg, sc, nil, nil); err != nil {
		t.Fatalf("wireLLM: %v", err)
	}
	if cfg.Conv.ConversationLLMClient == nil {
		t.Error("the Tier 1 client was not wired")
	}
	if cfg.Conv.TitleGenerator == nil {
		t.Error("the title generator was not wired")
	}
	// Without a store there is nowhere to record managed calls, so the gateway
	// stays off rather than serving unmetered inference.
	if cfg.Conv.LLMGateway != nil {
		t.Error("the gateway was wired with no store")
	}

	var unconfigured httpserver.Config
	if err := wireLLM(&unconfigured, config.ServerConfig{}, nil, nil); err != nil {
		t.Fatalf("wireLLM without a model: %v", err)
	}
	if unconfigured.Conv.ConversationLLMClient != nil || unconfigured.Conv.TitleGenerator != nil {
		t.Error("Tier 1 was wired with no conversation model configured")
	}
}

// A default_model that names nothing must stop the server, not surface later as
// a model outage on every session's first call.
func TestWireLLMFailsStartupOnAnUnknownDefaultModel(t *testing.T) {
	var cfg httpserver.Config
	sc := config.ServerConfig{
		Conversation: config.ServerConvConfig{Model: conversationModel()},
		LLM:          config.ServerLLMConfig{DefaultModel: "Gone"},
	}

	if err := wireLLM(&cfg, sc, nil, nil); err == nil {
		t.Fatal("a default_model naming nothing did not fail startup")
	}
	if cfg.Conv.ConversationLLMClient != nil {
		t.Error("a failed wiring left a client behind")
	}
}

// TestStoredModelOutputCapReachesTheUpstream closes the loop an operator cares
// about: a cap set on a catalog row has to arrive on the wire, not stop at the
// resolved target. It runs against the Anthropic protocol because that one
// requires the field, so the cap is always visible in the request.
func TestStoredModelOutputCapReachesTheUpstream(t *testing.T) {
	var body []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"m",` +
			`"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer upstream.Close()

	row := catalogRow("lm_capped")
	row.ProviderType = cllm.ProviderAnthropic
	row.APIURL = upstream.URL
	row.MaxTokens = 1234

	sc := config.ServerConfig{
		Conversation: config.ServerConvConfig{Model: conversationModel()},
		LLM:          config.ServerLLMConfig{DefaultModel: "LM_CAPPED"},
	}
	routing, err := buildLLMRouting(sc, newFakeModels(row))
	if err != nil {
		t.Fatalf("buildLLMRouting: %v", err)
	}
	routed, err := routing.Router.ClientFor(context.Background(), llmgateway.ResolveRequest{})
	if err != nil {
		t.Fatalf("ClientFor: %v", err)
	}
	if _, err := routed.Client.ChatCompletionBlocking(context.Background(),
		cllm.Request{Messages: []cllm.Message{{Role: "user", Content: "hi"}}}); err != nil {
		t.Fatalf("ChatCompletionBlocking: %v", err)
	}
	if !strings.Contains(string(body), `"max_tokens":1234`) {
		t.Errorf("request %s does not carry the catalog's output cap", body)
	}
}

// Tier 1 is the third managed path, and the design asks that it reach the same
// decision as the other two by calling the same code rather than by issuing an
// HTTP request back to the server. It does: the conversation client is built
// from the same catalog target through the same factory, so a policy set on the
// row is a policy Tier 1 honours.
func TestConversationModelCachePolicyReachesTheUpstream(t *testing.T) {
	tests := []struct {
		name      string
		cacheMode string
		profile   cllm.CallProfile
		wantCache bool
	}{
		{name: "an agent turn caches under the default policy", profile: cllm.ProfileAgentTurn, wantCache: true},
		{name: "a title does not", profile: cllm.ProfileTitle},
		{name: "an operator opt-out is honoured", cacheMode: "off", profile: cllm.ProfileAgentTurn},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var body []byte
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ = io.ReadAll(r.Body)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"m",` +
					`"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
			}))
			defer upstream.Close()

			row := catalogRow("lm_tier1")
			row.ProviderType = cllm.ProviderAnthropic
			row.APIURL = upstream.URL
			row.CacheMode = tc.cacheMode

			sc := config.ServerConfig{
				Conversation: config.ServerConvConfig{Model: conversationModel(), ModelTarget: "lm_tier1"},
				LLM:          config.ServerLLMConfig{DefaultModel: "LM_TIER1"},
			}
			routing, err := buildLLMRouting(sc, newFakeModels(row))
			if err != nil {
				t.Fatalf("buildLLMRouting: %v", err)
			}
			if routing.Tier1TargetID != "lm_tier1" {
				t.Fatalf("Tier1TargetID = %q, want the catalog row", routing.Tier1TargetID)
			}
			routed, err := routing.Router.ClientForTarget(context.Background(),
				routing.Tier1TargetID, llmgateway.BaselineCapabilities())
			if err != nil {
				t.Fatalf("ClientForTarget: %v", err)
			}
			if _, err := routed.Client.ChatCompletionBlocking(context.Background(), cllm.Request{
				Messages: []cllm.Message{{Role: "system", Content: "be brief"}, {Role: "user", Content: "hi"}},
				Profile:  tc.profile,
			}); err != nil {
				t.Fatalf("ChatCompletionBlocking: %v", err)
			}
			cached := strings.Contains(string(body), "cache_control")
			if cached != tc.wantCache {
				t.Errorf("cache_control present = %v, want %v: %s", cached, tc.wantCache, body)
			}
		})
	}
}
