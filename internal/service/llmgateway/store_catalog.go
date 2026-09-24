package llmgateway

import (
	"context"
	"fmt"
	"time"

	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
)

// StoreCatalog serves the model catalog from persistent storage.
//
// Unlike StaticCatalog it can change while the server runs, which is why the
// router compares a target's connection details before reusing a cached client
// and why a name that no longer resolves is a call-time failure rather than a
// startup one.
type StoreCatalog struct {
	Models coregw.ModelStore
}

// Target returns one approved upstream by catalog ID.
//
// CredentialRef is the model ID: the credential itself stays in the store and
// is fetched only by the component that opens a provider connection.
func (c *StoreCatalog) Target(ctx context.Context, id string) (Target, error) {
	if c == nil || c.Models == nil {
		return Target{}, ErrCatalogNotConfigured
	}
	stored, err := c.Models.GetLLMModel(ctx, id)
	if err != nil {
		return Target{}, err
	}
	if stored == nil {
		return Target{}, ErrTargetNotFound
	}
	return targetFromModel(*stored)
}

// TargetByName returns one approved upstream by its operator-facing name.
//
// The name column is unique, so this addresses exactly one row — which is what
// lets a client name a model without learning its catalog ID.
func (c *StoreCatalog) TargetByName(ctx context.Context, name string) (Target, error) {
	if c == nil || c.Models == nil {
		return Target{}, ErrCatalogNotConfigured
	}
	stored, err := c.Models.GetLLMModelByName(ctx, name)
	if err != nil {
		return Target{}, err
	}
	if stored == nil {
		return Target{}, ErrTargetNotFound
	}
	return targetFromModel(*stored)
}

// List returns every stored target, oldest first.
//
// A row that cannot be converted is skipped rather than failing the listing: a
// half-written row must not hide every other model from a client, and it still
// fails its own calls in TargetByName.
func (c *StoreCatalog) List(ctx context.Context) ([]Target, error) {
	if c == nil || c.Models == nil {
		return nil, ErrCatalogNotConfigured
	}
	stored, err := c.Models.ListLLMModels(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Target, 0, len(stored))
	for _, m := range stored {
		target, err := targetFromModel(m)
		if err != nil {
			continue
		}
		out = append(out, target)
	}
	return out, nil
}

// IDs returns every catalog ID, for callers that need to check a reference.
func (c *StoreCatalog) IDs(ctx context.Context) ([]string, error) {
	if c == nil || c.Models == nil {
		return nil, ErrCatalogNotConfigured
	}
	stored, err := c.Models.ListLLMModels(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(stored))
	for _, m := range stored {
		out = append(out, m.ID)
	}
	return out, nil
}

// targetFromModel converts a stored row, rejecting one that cannot be called.
//
// A row is validated on the way out rather than trusted: the catalog is edited
// through an API, and a half-written row must fail its own calls instead of
// producing a client pointed at nothing.
func targetFromModel(m coregw.Model) (Target, error) {
	capabilities := make([]Capability, 0, len(m.Capabilities))
	for _, c := range m.Capabilities {
		capabilities = append(capabilities, Capability(c))
	}
	target := Target{
		ID:                m.ID,
		Name:              m.Name,
		ProviderType:      m.ProviderType,
		Endpoint:          m.APIURL,
		CredentialRef:     m.ID,
		Revision:          m.UpdatedAt,
		UpstreamModel:     m.Model,
		ContextWindow:     m.ContextWindow,
		CallTimeout:       time.Duration(m.CallTimeout) * time.Second,
		MaxTokens:         m.MaxTokens,
		Reasoning:         m.Reasoning,
		CacheMode:         m.CacheMode,
		CacheTTL:          m.CacheTTL,
		Currency:          m.Currency,
		InputPerMTok:      m.InputPerMTok,
		CacheReadPerMTok:  m.CacheReadPerMTok,
		CacheWritePerMTok: m.CacheWritePerMTok,
		OutputPerMTok:     m.OutputPerMTok,
		Vision:            m.Vision,
		Capabilities:      NewCapabilitySet(capabilities...),
		Enabled:           m.Enabled,
	}
	if err := validateTarget(0, target); err != nil {
		return Target{}, fmt.Errorf("model %q: %w", m.Name, err)
	}
	return target, nil
}
