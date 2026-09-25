package llmgateway

import (
	"context"
	"errors"
	"fmt"
	"strings"

	cllm "github.com/icloudbb/buildmax/internal/core/llm"
)

// Resolution outcomes. Handlers map these to stable BuildMax error codes, so
// the wording here stays free of upstream endpoints, credentials, and provider
// model identifiers.
var (
	ErrCatalogNotConfigured  = errors.New("model catalog is not configured")
	ErrCatalogEmpty          = errors.New("model catalog has no usable model")
	ErrTargetNotFound        = errors.New("model not found in catalog")
	ErrTargetDisabled        = errors.New("model is disabled")
	ErrCapabilityUnsupported = errors.New("model does not support a required capability")
	ErrInvalidCatalog        = errors.New("invalid model catalog")
)

// CapabilityError reports which required capabilities a target does not
// declare. It satisfies errors.Is(err, ErrCapabilityUnsupported) so callers can
// classify the failure without inspecting the detail.
type CapabilityError struct {
	Model   string
	Missing []Capability
}

func (e *CapabilityError) Error() string {
	names := make([]string, 0, len(e.Missing))
	for _, capability := range e.Missing {
		names = append(names, string(capability))
	}
	return fmt.Sprintf("model %q does not support: %s", e.Model, strings.Join(names, ", "))
}

// Is reports whether the error matches the ErrCapabilityUnsupported sentinel.
func (e *CapabilityError) Is(target error) bool { return target == ErrCapabilityUnsupported }

// ResolveRequest names the model a caller wants.
//
// Every catalog model is available to every user of the deployment, so a
// request carries no space: authorization is being signed in. See
// docs/design/client-modes.md section 5.
type ResolveRequest struct {
	// Name is the operator-facing model name. Empty selects the deployment
	// default.
	Name string
	// Requires is the capability set the call needs.
	Requires []Capability
}

// Resolution is a successful model resolution.
type Resolution struct {
	// Name is the model actually used, with the default already applied.
	Name string
	// Target is the operator-approved upstream to call.
	Target Target
}

// AvailableModel is one model a client may call. It deliberately omits the
// endpoint, credential reference, provider type, and upstream model identifier:
// listing models must not disclose how the deployment reaches a provider.
type AvailableModel struct {
	Name string
	// ContextWindow and Vision are what a client needs before it calls: it
	// compacts against the window and sends an image only to a model that reads
	// one. Neither says anything about how the deployment reaches the provider.
	ContextWindow int
	Vision        bool
	Capabilities  []Capability
	Default       bool
	// Pricing is the target's current rates; an empty currency means unpriced.
	// Rates are not secret — a caller sees what its own calls cost anyway.
	Pricing cllm.Pricing
}

// Resolver maps a model name to an operator-approved target.
type Resolver struct {
	Catalog Catalog
	// DefaultModel is the name used when a request names none. Empty falls back
	// to the first enabled model in the catalog, so a deployment with one model
	// needs no configuration.
	DefaultModel string
}

// Resolve returns the target for a request, applying the deployment default.
func (r *Resolver) Resolve(ctx context.Context, req ResolveRequest) (Resolution, error) {
	if r == nil || r.Catalog == nil {
		return Resolution{}, ErrCatalogNotConfigured
	}

	name := req.Name
	if name == "" {
		resolved, err := r.defaultName(ctx)
		if err != nil {
			return Resolution{}, err
		}
		name = resolved
	}

	target, err := r.Catalog.TargetByName(ctx, name)
	if err != nil {
		return Resolution{}, err
	}
	if err := checkTarget(target, name, req.Requires); err != nil {
		return Resolution{}, err
	}
	return Resolution{Name: name, Target: target}, nil
}

// defaultName is the model a request that names none gets.
//
// A configured default that names nothing is not silently replaced: an operator
// stated which model this deployment answers with, and quietly using a
// different one would be worse than failing. Falling back to the first enabled
// model applies only when nothing was configured.
func (r *Resolver) defaultName(ctx context.Context) (string, error) {
	if r.DefaultModel != "" {
		return r.DefaultModel, nil
	}
	targets, err := r.Catalog.List(ctx)
	if err != nil {
		return "", err
	}
	for _, target := range targets {
		if target.Enabled {
			return target.Name, nil
		}
	}
	return "", ErrCatalogEmpty
}

// ResolveTargetByID returns a catalog target the deployment itself selected.
//
// Server-owned inference — Tier 1 conversation and title generation — is
// configured by an operator rather than requested by a client, so it names a
// catalog entry by ID. Nothing a client submits reaches this path.
func (r *Resolver) ResolveTargetByID(ctx context.Context, targetID string, requires []Capability) (Target, error) {
	if r == nil || r.Catalog == nil {
		return Target{}, ErrCatalogNotConfigured
	}
	if targetID == "" {
		return Target{}, ErrTargetNotFound
	}
	target, err := r.Catalog.Target(ctx, targetID)
	if err != nil {
		return Target{}, err
	}
	if err := checkTarget(target, targetID, requires); err != nil {
		return Target{}, err
	}
	return target, nil
}

// checkTarget applies the checks every caller needs. The label names the target
// the way the caller asked for it, so a client failure talks about the model
// name and a deployment-owned one talks about the ID.
func checkTarget(target Target, label string, requires []Capability) error {
	if !target.Enabled {
		return ErrTargetDisabled
	}
	if missing := target.Capabilities.Missing(requires); len(missing) > 0 {
		return &CapabilityError{Model: label, Missing: missing}
	}
	return nil
}

// Available lists the models a client may call, in a stable order.
//
// A disabled model is skipped rather than reported: a listing stays usable when
// part of the catalog is retired, while Resolve still fails loudly for a caller
// that asks for that model by name.
func (r *Resolver) Available(ctx context.Context) ([]AvailableModel, error) {
	if r == nil || r.Catalog == nil {
		return nil, ErrCatalogNotConfigured
	}

	targets, err := r.Catalog.List(ctx)
	if err != nil {
		return nil, err
	}

	// The default is resolved once against the same listing, so exactly one row
	// is marked even when the configured default is disabled or missing — in
	// which case none is, and the client falls back to the first entry itself.
	defaultName := r.DefaultModel
	if defaultName == "" {
		for _, target := range targets {
			if target.Enabled {
				defaultName = target.Name
				break
			}
		}
	}

	models := make([]AvailableModel, 0, len(targets))
	for _, target := range targets {
		if !target.Enabled {
			continue
		}
		models = append(models, AvailableModel{
			Name:          target.Name,
			ContextWindow: target.ContextWindow,
			Vision:        target.Vision,
			Capabilities:  target.Capabilities.List(),
			Default:       target.Name == defaultName,
			Pricing: cllm.Pricing{
				Currency:          target.Currency,
				InputPerMTok:      target.InputPerMTok,
				CacheReadPerMTok:  target.CacheReadPerMTok,
				CacheWritePerMTok: target.CacheWritePerMTok,
				OutputPerMTok:     target.OutputPerMTok,
			},
		})
	}
	return models, nil
}
