// Package llmcatalog owns what a deployment's model catalog will accept and
// what changing it records.
//
// Two edges reach it and they are not the same authority: `buildmax-server
// model` runs at a shell with the database credentials, and /api/admin/llm is
// a signed-in System Administrator. Which one acted belongs in the trail; what
// the catalog will take does not depend on it.
//
// Adding a model is reachable from both edges. A create carries a provider
// credential, which is why it was once shell-only; it is safe over HTTP now that
// the store encrypts the credential at rest and refuses a credentialed model
// when no encryption key is configured (see internal/infra/secret and
// internal/infra/db). The key travels in the request body alone, never a query
// or path, and no read returns it.
package llmcatalog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	"github.com/icloudbb/buildmax/internal/core/llm"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
	"github.com/icloudbb/buildmax/internal/service/audit"
	"github.com/icloudbb/buildmax/internal/service/llmgateway"
)

// Service is the catalog's administration workflows.
type Service struct {
	Models coregw.ModelStore
	Audit  *audit.Recorder
}

const auditTarget = "llm_model"

// ErrNameTaken is returned when a create names a model the catalog already has.
var ErrNameTaken = apierr.New(apierr.KindConflict, "a model with this name already exists")

// ErrEncryptionUnavailable is returned when a create carries a provider
// credential but the deployment configured no encryption key to protect it at
// rest. The credential is refused rather than stored in the clear; configure a
// KEK (see docs/design/space-secrets.md §9.1) and retry.
var ErrEncryptionUnavailable = apierr.New(apierr.KindNotConfigured, "no deployment encryption key is configured, so a model credential cannot be stored")

// Validate rejects a row that could never serve a call, so a caller hears about
// it here rather than at somebody's first prompt.
func Validate(in coregw.CreateModelInput) error {
	switch {
	case in.Name == "":
		return invalidf("name", "is required")
	case in.APIURL == "":
		return invalidf("api_url", "is required")
	// A local runtime has no credential, and requiring a placeholder for it
	// would put a meaningless secret in the catalog and in the audit trail.
	case in.APIKey == "" && llm.ProviderNeedsCredential(in.ProviderType):
		return invalidf("api_key", "is required")
	case in.Model == "":
		return invalidf("model", "is required")
	case in.ContextWindow < 0:
		return invalidf("context_window", "cannot be negative")
	case in.CallTimeout < 0:
		return invalidf("call_timeout", "cannot be negative")
	case in.MaxTokens < 0:
		return invalidf("max_tokens", "cannot be negative")
	case !config.KnownReasoningEffort(in.Reasoning):
		return invalidf("reasoning", "%q is not a level; use one of %s",
			in.Reasoning, strings.Join(config.ReasoningEfforts(), ", "))
	case !llm.KnownProvider(in.ProviderType):
		return invalidf("provider_type", "%q is not implemented; use one of %s",
			in.ProviderType, strings.Join(llm.Providers(), ", "))
	case !config.KnownCacheMode(in.CacheMode):
		return invalidf("cache_mode", "%q is not a mode; use one of %s",
			in.CacheMode, strings.Join(config.CacheModes(), ", "))
	case !config.KnownCacheTTL(in.CacheTTL):
		return invalidf("cache_ttl", "%q is not a retention; use one of %s",
			in.CacheTTL, strings.Join(config.CacheTTLs(), ", "))
	}
	for _, c := range in.Capabilities {
		if !knownCapability(c) {
			return invalidf("", "unknown capability %q", c)
		}
	}
	return nil
}

func knownCapability(name string) bool {
	return llmgateway.NewCapabilitySet(llmgateway.BaselineCapabilities()...).
		Has(llmgateway.Capability(name))
}

// Pricing is a catalog row's resolved per-million-token rates, in nano-units of
// its currency. It is what ResolvePricing turns an edge's string prices into,
// so a caller that cannot import internal/config still reaches the one price
// parser.
type Pricing struct {
	Currency          string
	InputPerMTok      int64
	CacheReadPerMTok  int64
	CacheWritePerMTok int64
	OutputPerMTok     int64
}

// ResolvePricing parses the string prices an edge collects into stored rates,
// through the same config parser the shell command uses. It lives here so the
// admin handler, which may not import internal/config, still resolves prices the
// one way. Empty strings mean an unpriced model.
func ResolvePricing(currency, input, cacheRead, cacheWrite, output string) (Pricing, error) {
	p, err := config.ResolvePricing(&config.ModelPricing{
		Currency:          strings.TrimSpace(currency),
		InputPerMTok:      strings.TrimSpace(input),
		CacheReadPerMTok:  strings.TrimSpace(cacheRead),
		CacheWritePerMTok: strings.TrimSpace(cacheWrite),
		OutputPerMTok:     strings.TrimSpace(output),
	})
	if err != nil {
		return Pricing{}, err
	}
	return Pricing{
		Currency:          p.Currency,
		InputPerMTok:      p.InputPerMTok,
		CacheReadPerMTok:  p.CacheReadPerMTok,
		CacheWritePerMTok: p.CacheWritePerMTok,
		OutputPerMTok:     p.OutputPerMTok,
	}, nil
}

// withDefaults fills the catalog's defaults for fields a caller may omit, so a
// model added through any edge -- the shell or the admin API -- lands as the
// same row. The protocol defaults to OpenAI-compatible, and capabilities to the
// baseline set an OpenAI-compatible client already guarantees. Both defaults
// live here rather than at each edge so the two cannot drift.
func withDefaults(in coregw.CreateModelInput) coregw.CreateModelInput {
	if in.ProviderType == "" {
		in.ProviderType = llm.ProviderOpenAICompatible
	}
	if len(in.Capabilities) == 0 {
		caps := llmgateway.BaselineCapabilities()
		in.Capabilities = make([]string, 0, len(caps))
		for _, c := range caps {
			in.Capabilities = append(in.Capabilities, string(c))
		}
	}
	return in
}

// Create validates and adds a catalog entry.
func (s *Service) Create(ctx context.Context, in coregw.CreateModelInput, actor coreaudit.Actor) (*coregw.Model, error) {
	in = withDefaults(in)
	if err := Validate(in); err != nil {
		return nil, err
	}
	created, err := s.Models.CreateLLMModel(ctx, in)
	if err != nil {
		if errors.Is(err, coregw.ErrModelNameTaken) {
			return nil, ErrNameTaken
		}
		if errors.Is(err, coregw.ErrCredentialEncryptionUnavailable) {
			return nil, ErrEncryptionUnavailable
		}
		return nil, fmt.Errorf("create model: %w", err)
	}
	s.record(ctx, actor, coreaudit.ModelCreated, created.ID, created.Name)
	return created, nil
}

// SetEnabled turns a catalog entry on or off and returns it as it now stands.
func (s *Service) SetEnabled(ctx context.Context, modelID string, enabled bool, actor coreaudit.Actor) (*coregw.Model, error) {
	existing, err := s.Models.GetLLMModel(ctx, modelID)
	if err != nil {
		return nil, fmt.Errorf("read model: %w", err)
	}
	if existing == nil {
		return nil, apierr.New(apierr.KindNotFound, "model not found")
	}
	if err := s.Models.SetLLMModelEnabled(ctx, modelID, enabled); err != nil {
		return nil, fmt.Errorf("set model enabled: %w", err)
	}
	action := coreaudit.ModelDisabled
	if enabled {
		action = coreaudit.ModelEnabled
	}
	// The name, from both edges. The trail should distinguish a catalog change
	// by who made it, not by where -- and a reader looking at a revoked model
	// months later has the id and wants the name.
	s.record(ctx, actor, action, modelID, existing.Name)

	updated, err := s.Models.GetLLMModel(ctx, modelID)
	if err != nil || updated == nil {
		return nil, fmt.Errorf("reload model: %w", err)
	}
	return updated, nil
}

// ReplaceCredential rotates a catalog entry's upstream key in place and
// returns the entry as it now stands. The model keeps its ID and name, so no
// client has to learn a new one; the gateway picks up the new key on its next
// call.
func (s *Service) ReplaceCredential(ctx context.Context, modelID, apiKey string, actor coreaudit.Actor) (*coregw.Model, error) {
	existing, err := s.Models.GetLLMModel(ctx, modelID)
	if err != nil {
		return nil, fmt.Errorf("read model: %w", err)
	}
	if existing == nil {
		return nil, apierr.New(apierr.KindNotFound, "model not found")
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" && llm.ProviderNeedsCredential(existing.ProviderType) {
		return nil, invalidf("api_key", "is required")
	}
	if err := s.Models.SetLLMModelCredential(ctx, modelID, apiKey); err != nil {
		if errors.Is(err, coregw.ErrCredentialEncryptionUnavailable) {
			return nil, ErrEncryptionUnavailable
		}
		return nil, fmt.Errorf("replace model credential: %w", err)
	}
	s.record(ctx, actor, coreaudit.ModelCredentialReplaced, modelID, existing.Name)
	updated, err := s.Models.GetLLMModel(ctx, modelID)
	if err != nil || updated == nil {
		return nil, fmt.Errorf("reload model: %w", err)
	}
	return updated, nil
}

func (s *Service) record(ctx context.Context, actor coreaudit.Actor, action, modelID, detail string) {
	if s.Audit == nil {
		return
	}
	s.Audit.Record(ctx, coreaudit.Event{
		ActorType:  actor.Type,
		ActorID:    actor.ID,
		Action:     action,
		TargetType: auditTarget,
		TargetID:   modelID,
		Detail:     detail,
	})
}
