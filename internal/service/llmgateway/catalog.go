// Package llmgateway resolves a model name to an operator-approved upstream
// target. It owns the model catalog and the capability contract.
//
// Models are global to a deployment: the alias layer and per-space model policy
// this package once carried are withdrawn, so a name resolves the same way for
// every caller. See docs/design/client-modes.md section 6.
//
// The package deliberately does not open provider connections, read
// configuration files, or speak HTTP: process wiring supplies an already-built
// catalog, and higher layers turn a resolved target into a client.
//
// Mirrors the design in docs/design/llm-gateway.md, as revised by
// docs/design/client-modes.md.
package llmgateway

import (
	"context"
	"fmt"
	"slices"
	"time"
)

// Target is one operator-approved upstream in the model catalog.
//
// A Target never carries a credential. CredentialRef names a secret that
// process wiring resolves separately, so a resolved target can be compared,
// listed, and mentioned in diagnostics without leaking provider access.
type Target struct {
	// ID is the opaque catalog identifier referenced by space policy.
	ID string
	// Name is the operator-facing display name.
	Name string
	// ProviderType selects the client implementation, e.g. ProviderOpenAICompatible.
	ProviderType string
	// Endpoint is the upstream base URL. Only an operator may set it; it is
	// never accepted from a client request.
	Endpoint string
	// CredentialRef names the secret used for this target, not the secret.
	CredentialRef string
	// Revision changes whenever the catalog row does, including when its
	// credential is replaced. The secret behind CredentialRef is not visible
	// here, so this is what tells a cached client it is stale.
	Revision time.Time
	// UpstreamModel is the provider's own model identifier.
	UpstreamModel string
	// ContextWindow is the usable context size; 0 means the client default.
	ContextWindow int
	// CallTimeout bounds one upstream call; 0 means the client default.
	CallTimeout time.Duration
	// MaxTokens caps one response; 0 means the client default.
	MaxTokens int
	// Reasoning is the effort level the upstream is asked for; off means none.
	// The gateway carries the resulting state without reading it.
	Reasoning string
	// CacheMode and CacheTTL are the operator's prompt-cache policy: which
	// calls ask the upstream to cache the stable prefix of a request, and for
	// how long. They are carried as plain strings for the same reason the
	// provider constants above are redefined here — this package resolves what
	// a space may call without depending on how a process reads configuration.
	// A managed caller never supplies either: the operator's target does.
	CacheMode string
	CacheTTL  string
	// Pricing is what this upstream charges, in nano-currency-units per
	// million tokens, carried as plain fields for the same reason the cache
	// policy is. An empty Currency means unpriced, and a call against it
	// reports cost as unavailable rather than as zero.
	Currency          string
	InputPerMTok      int64
	CacheReadPerMTok  int64
	CacheWritePerMTok int64
	OutputPerMTok     int64
	// Vision says the upstream accepts image input.
	Vision bool
	// Capabilities is what this target declares it can do.
	Capabilities CapabilitySet
	// Enabled allows an operator to retire a target without deleting it.
	Enabled bool
}

// Catalog provides operator-approved targets. Implementations return
// ErrTargetNotFound for an unknown ID or name.
//
// Name is the addressing a client uses; ID stays for deployment-owned
// selection, where the server names a target it configured itself.
type Catalog interface {
	Target(ctx context.Context, id string) (Target, error)
	TargetByName(ctx context.Context, name string) (Target, error)
	// List returns every target in a stable order, for discovery and for
	// resolving the default when no model is configured as such.
	List(ctx context.Context) ([]Target, error)
}

// StaticCatalog is an immutable in-memory catalog, used for deployment-wide
// configuration before a database-backed catalog exists.
type StaticCatalog struct {
	targets map[string]Target
	byName  map[string]Target
	ids     []string
	ordered []Target
}

// NewStaticCatalog validates the targets and builds a catalog. Validation
// happens once at startup so an operator misconfiguration surfaces there
// rather than on a user's first call.
//
// Names must be unique as well as IDs: a name is how a client addresses a
// model, so two targets sharing one would make the second unreachable.
func NewStaticCatalog(targets []Target) (*StaticCatalog, error) {
	byID := make(map[string]Target, len(targets))
	byName := make(map[string]Target, len(targets))
	ids := make([]string, 0, len(targets))
	for i, target := range targets {
		if err := validateTarget(i, target); err != nil {
			return nil, err
		}
		if _, duplicate := byID[target.ID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate target id %q", ErrInvalidCatalog, target.ID)
		}
		if _, duplicate := byName[target.Name]; duplicate {
			return nil, fmt.Errorf("%w: duplicate target name %q", ErrInvalidCatalog, target.Name)
		}
		byID[target.ID] = target
		byName[target.Name] = target
		ids = append(ids, target.ID)
	}
	slices.Sort(ids)
	ordered := make([]Target, 0, len(ids))
	for _, id := range ids {
		ordered = append(ordered, byID[id])
	}
	return &StaticCatalog{targets: byID, byName: byName, ids: ids, ordered: ordered}, nil
}

func validateTarget(index int, target Target) error {
	switch {
	case target.ID == "":
		return fmt.Errorf("%w: target %d has no id", ErrInvalidCatalog, index)
	case target.ProviderType == "":
		return fmt.Errorf("%w: target %q has no provider type", ErrInvalidCatalog, target.ID)
	case target.Endpoint == "":
		return fmt.Errorf("%w: target %q has no endpoint", ErrInvalidCatalog, target.ID)
	case target.UpstreamModel == "":
		return fmt.Errorf("%w: target %q has no upstream model", ErrInvalidCatalog, target.ID)
	case len(target.Capabilities) == 0:
		return fmt.Errorf("%w: target %q declares no capabilities", ErrInvalidCatalog, target.ID)
	case target.ContextWindow < 0:
		return fmt.Errorf("%w: target %q has a negative context window", ErrInvalidCatalog, target.ID)
	case target.CallTimeout < 0:
		return fmt.Errorf("%w: target %q has a negative call timeout", ErrInvalidCatalog, target.ID)
	case target.MaxTokens < 0:
		return fmt.Errorf("%w: target %q has a negative max tokens", ErrInvalidCatalog, target.ID)
	}
	return nil
}

// Target returns the target for the catalog ID.
func (c *StaticCatalog) Target(_ context.Context, id string) (Target, error) {
	if c == nil {
		return Target{}, ErrCatalogNotConfigured
	}
	target, ok := c.targets[id]
	if !ok {
		return Target{}, ErrTargetNotFound
	}
	return target, nil
}

// TargetByName returns the target an operator gave this name.
func (c *StaticCatalog) TargetByName(_ context.Context, name string) (Target, error) {
	if c == nil {
		return Target{}, ErrCatalogNotConfigured
	}
	target, ok := c.byName[name]
	if !ok {
		return Target{}, ErrTargetNotFound
	}
	return target, nil
}

// List returns every target, ordered by ID so the default and the listing are
// stable across calls.
func (c *StaticCatalog) List(_ context.Context) ([]Target, error) {
	if c == nil {
		return nil, ErrCatalogNotConfigured
	}
	out := make([]Target, len(c.ordered))
	copy(out, c.ordered)
	return out, nil
}

// IDs returns every catalog ID in a stable order.
func (c *StaticCatalog) IDs() []string {
	if c == nil || len(c.ids) == 0 {
		return nil
	}
	out := make([]string, len(c.ids))
	copy(out, c.ids)
	return out
}
