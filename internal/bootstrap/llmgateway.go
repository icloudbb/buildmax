package bootstrap

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/icloudbb/buildmax/internal/config"
	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
	llm "github.com/icloudbb/buildmax/internal/infra/llm"
	"github.com/icloudbb/buildmax/internal/service/llmgateway"
)

// conversationTargetID is the catalog ID of the target derived from
// conversation.model.
//
// The stored catalog can be empty — a fresh deployment has no model rows — so
// the server still needs one model of its own to answer Tier 1 conversations.
// That is what this entry is: a bootstrap path, not a catalog feature.
const conversationTargetID = "conversation"

// conversationCredentialRef names where the derived target's credential came
// from. Stored models use their own ID as the reference, so the two namespaces
// cannot collide.
const conversationCredentialRef = "conversation.model.api_key"

// conversationKeyHint is where an operator replaces the conversation model's
// key, for the server's derived target and for a direct-transport worker that
// calls the same model: the setting and its environment override, never the key.
const conversationKeyHint = "check " + conversationCredentialRef + " in server.yaml or its override " + config.EnvKeyBuildmaxConversationAPIKey

// llmRouting is the gateway wiring for one server process.
type llmRouting struct {
	// Router resolves models and supplies clients.
	Router *llmgateway.Router
	// Tier1TargetID is the catalog target Server-owned conversation inference
	// uses. Empty means no conversation model is configured.
	Tier1TargetID string
}

// bootstrapCatalog serves the stored model catalog plus the one model derived
// from server.yaml.
//
// The derived entry is checked first and is not in the database, so a
// deployment can answer conversations before an operator has added a single
// model row.
type bootstrapCatalog struct {
	derived *llmgateway.Target
	stored  *llmgateway.StoreCatalog
}

func (c *bootstrapCatalog) Target(ctx context.Context, id string) (llmgateway.Target, error) {
	if c.derived != nil && id == conversationTargetID {
		return *c.derived, nil
	}
	if c.stored == nil {
		return llmgateway.Target{}, llmgateway.ErrTargetNotFound
	}
	return c.stored.Target(ctx, id)
}

// TargetByName checks the derived model before the store, so a deployment whose
// only model is conversation.model can still be addressed by name.
func (c *bootstrapCatalog) TargetByName(ctx context.Context, name string) (llmgateway.Target, error) {
	if c.derived != nil && c.derived.Name == name {
		return *c.derived, nil
	}
	if c.stored == nil {
		return llmgateway.Target{}, llmgateway.ErrTargetNotFound
	}
	return c.stored.TargetByName(ctx, name)
}

// List puts the derived model first, which makes it the fallback default for a
// deployment that has configured no llm.default_model and added no rows.
//
// A stored row that has taken the derived model's name is dropped from the
// listing rather than shown twice: TargetByName would never reach it, and a
// listing that offers a name resolving to something else is worse than a
// listing missing one entry. The operator sees it in `buildmax-server model
// list`, where the catalog is edited.
func (c *bootstrapCatalog) List(ctx context.Context) ([]llmgateway.Target, error) {
	var out []llmgateway.Target
	if c.derived != nil {
		out = append(out, *c.derived)
	}
	if c.stored == nil {
		return out, nil
	}
	stored, err := c.stored.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, target := range stored {
		if c.derived != nil && target.Name == c.derived.Name {
			continue
		}
		out = append(out, target)
	}
	return out, nil
}

// buildLLMRouting assembles the catalog, space policy, and router.
//
// It returns nil when the deployment has neither a database nor a conversation
// model, which leaves Tier 1 unwired exactly as before the gateway existed.
func buildLLMRouting(sc config.ServerConfig, models coregw.ModelStore) (*llmRouting, error) {
	catalog := &bootstrapCatalog{}
	if entry := sc.Conversation.Model; entry.APIKey != "" {
		derived := derivedConversationTarget(entry)
		catalog.derived = &derived
	}
	if models != nil {
		catalog.stored = &llmgateway.StoreCatalog{Models: models}
	}
	if catalog.derived == nil && catalog.stored == nil {
		return nil, nil
	}

	tier1 := sc.Conversation.ModelTarget
	if tier1 == "" && catalog.derived != nil {
		tier1 = conversationTargetID
	}

	return &llmRouting{
		Router: &llmgateway.Router{
			Resolver: &llmgateway.Resolver{
				Catalog:      catalog,
				DefaultModel: sc.LLM.DefaultModel,
			},
			Factory: newClientFactory(sc.Conversation.Model.APIKey, models),
		},
		Tier1TargetID: tier1,
	}, nil
}

func derivedConversationTarget(entry config.ServerModelEntry) llmgateway.Target {
	name := entry.Name
	if name == "" {
		name = entry.Model
	}
	apiURL := entry.APIURL
	if apiURL == "" {
		apiURL = config.DefaultOpenRouterBaseURL
	}
	modelName := entry.Model
	if modelName == "" {
		modelName = config.DefaultModel
	}
	providerType := entry.Provider
	if providerType == "" {
		providerType = cllm.ProviderOpenAICompatible
	}
	conversationCache := config.ResolveCacheControl(entry.CacheControl)
	return llmgateway.Target{
		ID:            conversationTargetID,
		Name:          name,
		ProviderType:  providerType,
		Endpoint:      apiURL,
		CredentialRef: conversationCredentialRef,
		UpstreamModel: modelName,
		ContextWindow: entry.ContextWindow,
		CallTimeout:   time.Duration(entry.CallTimeout) * time.Second,
		MaxTokens:     entry.MaxTokens,
		Reasoning:     entry.Reasoning,
		CacheMode:     conversationCache.Mode,
		CacheTTL:      conversationCache.TTL,
		Vision:        entry.Vision,
		Capabilities:  llmgateway.NewCapabilitySet(llmgateway.BaselineCapabilities()...),
		Enabled:       true,
	}
}

// newClientFactory builds provider clients for approved targets. It is the only
// place a credential reference becomes a real credential.
func newClientFactory(conversationKey string, models coregw.ModelStore) llmgateway.ClientFactory {
	return func(ctx context.Context, target llmgateway.Target) (cllm.LLMClient, error) {
		if !cllm.KnownProvider(target.ProviderType) {
			return nil, fmt.Errorf("model %q uses unsupported provider %q; use one of %s",
				target.Name, target.ProviderType, strings.Join(cllm.Providers(), ", "))
		}
		apiKey, err := resolveCredential(ctx, target, conversationKey, models)
		if err != nil {
			return nil, fmt.Errorf("model %q: %w", target.Name, err)
		}
		// The provider type is the catalog's word for a wire protocol, and the
		// client package uses the same values, so the target selects an adapter
		// without a translation table to keep in step.
		client, err := llm.NewClient(llm.Config{
			Provider:      target.ProviderType,
			APIKey:        apiKey,
			BaseURL:       target.Endpoint,
			Model:         target.UpstreamModel,
			ContextWindow: target.ContextWindow,
			MaxTokens:     target.MaxTokens,
			Reasoning:     target.Reasoning,
			CacheControl:  config.CacheControl{Mode: target.CacheMode, TTL: target.CacheTTL},
			Vision:        target.Vision,
			Surface:       coregw.CallSurfaceServer,
			CallTimeout:   target.CallTimeout,
			// The key is the server's, so a refusal must not send the operator
			// to a settings.yaml that holds no key.
			CredentialHint: credentialHint(target),
		})
		if err != nil {
			return nil, fmt.Errorf("model %q: %w", target.Name, err)
		}
		return client, nil
	}
}

// credentialHint names where the operator replaces a target's key: the command
// and the model ID, never the key.
func credentialHint(target llmgateway.Target) string {
	if target.CredentialRef == conversationCredentialRef {
		return conversationKeyHint
	}
	return fmt.Sprintf("replace the key of catalog model %s with `buildmax admin model set-key %s` or `buildmax-server model set-key --id %s`",
		target.ID, target.ID, target.ID)
}

// resolveCredential turns a target's reference into the secret behind it.
//
// A missing credential is an error for every protocol that authenticates, so a
// half-configured target fails at selection rather than sending an
// unauthenticated request upstream. A local runtime has none by definition, and
// for it an empty key is the configured state — the target is authorized by the
// deployment being able to reach the daemon, not by a secret.
func resolveCredential(ctx context.Context, target llmgateway.Target, conversationKey string, models coregw.ModelStore) (string, error) {
	required := cllm.ProviderNeedsCredential(target.ProviderType)
	if target.CredentialRef == conversationCredentialRef {
		if conversationKey == "" && required {
			return "", fmt.Errorf("conversation.model.api_key is not set")
		}
		return conversationKey, nil
	}
	if models == nil {
		return "", fmt.Errorf("no model store to read a credential from")
	}
	key, err := models.LLMModelCredential(ctx, target.CredentialRef)
	if err != nil {
		return "", err
	}
	if key == "" && required {
		return "", fmt.Errorf("no credential stored")
	}
	return key, nil
}

// routedTitleGenerator generates task titles through the Tier 1 target,
// resolving it on every call. A client built once at startup kept the key it
// started with, so after `model set-key` every task title still went to the
// provider with the old key, and failed once that key was revoked.
type routedTitleGenerator struct {
	router   *llmgateway.Router
	targetID string
}

func (g routedTitleGenerator) GenerateTitle(ctx context.Context, input string) (string, int, int, error) {
	routed, err := g.router.ClientForTarget(ctx, g.targetID, llmgateway.BaselineCapabilities())
	if err != nil {
		return "", 0, 0, err
	}
	return cllm.NewTitleGenerator(routed.Client).GenerateTitle(ctx, input)
}
