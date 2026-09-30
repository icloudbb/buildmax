// Package agent owns the rules for a space's agent definitions.
//
// The handlers that used to hold these rules re-derived them per route: four of
// them separately asked the store for an agent and compared its space, and the
// one that deletes carried the workflow check inline. Both belong here, where a
// second caller -- a CLI command, another service -- gets them for free.
package agent

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/icloudbb/buildmax/internal/config"
	coreagent "github.com/icloudbb/buildmax/internal/core/agent"
	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/service/audit"
	"github.com/icloudbb/buildmax/internal/util"
)

var (
	ErrAgentsNotConfigured  = apierr.New(apierr.KindNotConfigured, "agents not configured")
	ErrAgentNotFound        = apierr.New(apierr.KindNotFound, "agent not found")
	ErrRevisionNotFound     = apierr.New(apierr.KindNotFound, "agent revision not found")
	ErrNameRequired         = apierr.New(apierr.KindInvalid, "name required")
	ErrUnknownModel         = apierr.New(apierr.KindInvalid, "unknown model")
	ErrInvalidSandboxTier   = apierr.New(apierr.KindInvalid, "unknown sandbox network or filesystem tier")
	ErrUsedByPublishedFlows = apierr.New(apierr.KindConflict, "agent is used by published workflows")
	ErrPluginsNotConfigured = apierr.New(apierr.KindNotConfigured,
		"this deployment cannot resolve plugins, so an agent cannot name one")
	ErrSecretsNotConfigured = apierr.New(apierr.KindNotConfigured,
		"this deployment has no secret store, so an agent cannot consume one")
	ErrInstructionsTooLong = apierr.New(apierr.KindInvalid, "Space and Agent instructions exceed 8192 characters")
	ErrNameTooLong         = apierr.New(apierr.KindInvalid, "name is too long")
)

// maxAgentNameRunes bounds an Agent name to its varchar(255) column, so an
// over-long name is a 400 rather than a write error surfaced as a 500.
const maxAgentNameRunes = 255

// validateAgentName trims, requires, and length-bounds an Agent name, returning
// the normalized value. Whitespace alone is no name, the same as empty.
func validateAgentName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrNameRequired
	}
	if util.ExceedsRuneLimit(name, maxAgentNameRunes) {
		return "", ErrNameTooLong
	}
	return name, nil
}

// WorkflowUsage reports which published workflows still name an agent.
//
// An interface rather than the workflow service itself: this package needs one
// question answered, and depending on the whole service would tie an agent edit
// to workflow orchestration.
type WorkflowUsage interface {
	PublishedWorkflowsUsingAgent(ctx context.Context, spaceID, agentID string) ([]coreworkflow.Workflow, error)
}

// PluginSelection turns the plugin names an agent carries into the space
// activations that back them, applying the space's curation mode.
//
// An interface rather than the plugin service itself, for the reason
// WorkflowUsage is one: this package needs one question answered, and depending
// on the whole service would tie an agent edit to publication and package
// storage.
type PluginSelection interface {
	ResolveSelection(ctx context.Context, spaceID string, names []string, actorID string) ([]coreplugin.Activation, error)
}

// ModelCatalog lists the model names this deployment offers, so an agent naming
// a model its deployment does not serve is refused while somebody is watching a
// create/update rather than failing at its first run. The gateway checks again
// at call time -- a model can be disabled after the agent was saved -- but that
// later refusal is a failed run, and this one is a correction.
//
// An interface rather than the gateway service itself, for the reason
// WorkflowUsage and PluginSelection are: this package needs one question
// answered, and depending on the whole service would tie an agent edit to
// inference routing.
type ModelCatalog interface {
	ModelNames(ctx context.Context) ([]string, error)
}

type Service struct {
	Agents agentdef.Store
	Spaces interface {
		GetSpace(context.Context, string) (*corespace.Space, error)
	}
	// Plugins is optional, and nil means the deployment has no Marketplace.
	// An agent that names a plugin is then refused rather than saved: storing
	// a selection nothing can resolve would be a definition that silently does
	// less than it says.
	Plugins PluginSelection
	// Workflows is optional. Nil means the deployment cannot answer which
	// workflows use an agent, so a delete is not blocked on that check -- the
	// same behaviour as before, when the handler skipped it on a nil store.
	Workflows WorkflowUsage
	// Secrets is optional, and nil means the deployment has no secret store.
	// An agent that consumes a Secret is then refused rather than saved, the
	// same way a plugin selection is refused with no Marketplace.
	Secrets SecretLookup
	// Models is optional, and nil means this deployment cannot enumerate its
	// models -- a direct-transport deployment reads its one model from
	// server.yaml and has no catalog to check against. A model name is then
	// stored unchecked and takes effect only if the run's transport can reach
	// it; unlike a plugin, an unresolvable model has a defined fallback (the
	// deployment default) at run time, so it is accepted rather than refused.
	Models ModelCatalog
	// Audit is optional; nil discards the events. An agent definition is
	// instructions plus a tool and model selection that later runs execute, so a
	// change to one is a governed act worth the trail.
	Audit *audit.Recorder
}

func (s *Service) validateInstructions(ctx context.Context, spaceID, instructions string) error {
	spaceInstructions := ""
	if s.Spaces != nil {
		space, err := s.Spaces.GetSpace(ctx, spaceID)
		if err != nil {
			return err
		}
		if space != nil {
			spaceInstructions = space.AgentInstructions
		}
	}
	if err := coreagent.ValidateInstructionLayers(spaceInstructions, instructions); err != nil {
		return apierr.Detail(ErrInstructionsTooLong, "%v", err)
	}
	return nil
}

type CreateCmd struct {
	SpaceID      string
	UserID       string
	Name         string
	Description  string
	Instructions string
	// Model is the catalog model name this agent's runs call. Empty means the
	// deployment default. See agentdef.Agent.Model.
	Model string
	// Plugins names catalog plugins this agent loads. Nothing is inherited
	// from the space's activations, so an empty list means no plugins.
	Plugins []string
	// SandboxNetworkTier and SandboxFilesystemTier declare this agent's
	// worker sandbox needs. Empty means the strictest tier on that axis. See
	// docs/design/agent-sandbox-policy.md §4.2.
	SandboxNetworkTier    string
	SandboxFilesystemTier string
	// SecretConsumption declares which Space Secrets this agent consumes.
	SecretConsumption agentdef.SecretConsumption
}

type UpdateCmd struct {
	SpaceID               string
	UserID                string
	AgentID               string
	Name                  string
	Description           string
	Instructions          string
	Model                 string
	Plugins               []string
	SandboxNetworkTier    string
	SandboxFilesystemTier string
	SecretConsumption     agentdef.SecretConsumption
}

// validateSandboxTiers checks a definition's declared tiers before it is
// stored, the same way resolvePlugins checks a plugin selection: refused
// while somebody is watching a create/update, rather than silently ignored
// by whichever binary later resolves it.
func validateSandboxTiers(networkTier, filesystemTier string) error {
	if !config.ValidSandboxNetworkTier(networkTier) || !config.ValidSandboxFilesystemTier(filesystemTier) {
		return ErrInvalidSandboxTier
	}
	return nil
}

// validateModel checks a chosen model against the deployment's catalog and
// returns the name to store, trimmed.
//
// An empty model is the deployment default and always valid. With no catalog to
// check against (s.Models nil), the name is stored as given; see Service.Models.
func (s *Service) validateModel(ctx context.Context, model string) (string, error) {
	model = strings.TrimSpace(model)
	if model == "" || s.Models == nil {
		return model, nil
	}
	names, err := s.Models.ModelNames(ctx)
	if err != nil {
		return "", err
	}
	if slices.Contains(names, model) {
		return model, nil
	}
	return "", apierr.Detail(ErrUnknownModel, "%s", model)
}

type RestoreRevisionCmd struct {
	SpaceID  string
	UserID   string
	AgentID  string
	Revision int
}

func (s *Service) ListAgents(ctx context.Context, spaceID string) ([]agentdef.Agent, error) {
	if s.Agents == nil {
		return nil, ErrAgentsNotConfigured
	}
	return s.Agents.ListAgentsBySpace(ctx, spaceID)
}

func (s *Service) CreateAgent(ctx context.Context, cmd CreateCmd) (*agentdef.Agent, error) {
	if s.Agents == nil {
		return nil, ErrAgentsNotConfigured
	}
	name, err := validateAgentName(cmd.Name)
	if err != nil {
		return nil, err
	}
	cmd.Name = name
	if err := s.validateInstructions(ctx, cmd.SpaceID, cmd.Instructions); err != nil {
		return nil, err
	}
	if err := validateSandboxTiers(cmd.SandboxNetworkTier, cmd.SandboxFilesystemTier); err != nil {
		return nil, err
	}
	model, err := s.validateModel(ctx, cmd.Model)
	if err != nil {
		return nil, err
	}
	if err := s.validateConsumption(ctx, cmd.SpaceID, cmd.SecretConsumption); err != nil {
		return nil, err
	}
	plugins, err := s.resolvePlugins(ctx, cmd.SpaceID, cmd.Plugins, cmd.UserID)
	if err != nil {
		return nil, err
	}
	created, err := s.Agents.CreateAgentInSpace(ctx, agentdef.CreateInput{
		SpaceID: cmd.SpaceID,
		UserID:  cmd.UserID,
		Def: agentdef.Definition{
			Name:                  cmd.Name,
			Description:           cmd.Description,
			Instructions:          cmd.Instructions,
			Model:                 model,
			Plugins:               plugins,
			SandboxNetworkTier:    cmd.SandboxNetworkTier,
			SandboxFilesystemTier: cmd.SandboxFilesystemTier,
			SecretConsumption:     cmd.SecretConsumption.Canonical(),
		},
	})
	if err != nil {
		return nil, err
	}
	s.Audit.UserAction(ctx, cmd.UserID, cmd.SpaceID, coreaudit.AgentCreated, "agent", created.ID, created.Name)
	return created, nil
}

// resolvePlugins checks a selection before it is stored and returns the names
// to store, normalized.
//
// The check happens here rather than at the run because an agent naming a
// plugin its space cannot use should be refused while somebody is watching. The
// run checks again — an activation can be suspended after the agent was saved,
// and a revision is append-only — but that later refusal is a failure, and this
// one is a correction.
func (s *Service) resolvePlugins(ctx context.Context, spaceID string, names []string, actorID string) ([]string, error) {
	normalized := normalizePluginNames(names)
	if len(normalized) == 0 {
		return nil, nil
	}
	if s.Plugins == nil {
		return nil, ErrPluginsNotConfigured
	}
	if _, err := s.Plugins.ResolveSelection(ctx, spaceID, normalized, actorID); err != nil {
		return nil, err
	}
	return normalized, nil
}

// normalizePluginNames trims, drops blanks, removes duplicates, and sorts.
//
// Sorted so that reordering the same set is not an edit: an agent revision is
// appended whenever the definition differs, and a list whose order carried no
// meaning would append one for a reshuffle.
func normalizePluginNames(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if _, dup := seen[trimmed]; dup {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	slices.Sort(out)
	return out
}

// GetAgent resolves an agent the space owns.
//
// An agent belonging to another space reads as not found rather than forbidden,
// so the answer does not confirm that an id exists somewhere else.
func (s *Service) GetAgent(ctx context.Context, spaceID, agentID string) (*agentdef.Agent, error) {
	if s.Agents == nil {
		return nil, ErrAgentsNotConfigured
	}
	found, err := s.Agents.GetAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if found == nil || found.SpaceID != spaceID {
		return nil, ErrAgentNotFound
	}
	return found, nil
}

func (s *Service) UpdateAgent(ctx context.Context, cmd UpdateCmd) (*agentdef.Agent, error) {
	if s.Agents == nil {
		return nil, ErrAgentsNotConfigured
	}
	name, err := validateAgentName(cmd.Name)
	if err != nil {
		return nil, err
	}
	cmd.Name = name
	if err := s.validateInstructions(ctx, cmd.SpaceID, cmd.Instructions); err != nil {
		return nil, err
	}
	if err := validateSandboxTiers(cmd.SandboxNetworkTier, cmd.SandboxFilesystemTier); err != nil {
		return nil, err
	}
	model, err := s.validateModel(ctx, cmd.Model)
	if err != nil {
		return nil, err
	}
	if err := s.validateConsumption(ctx, cmd.SpaceID, cmd.SecretConsumption); err != nil {
		return nil, err
	}
	plugins, err := s.resolvePlugins(ctx, cmd.SpaceID, cmd.Plugins, cmd.UserID)
	if err != nil {
		return nil, err
	}
	updated, err := s.Agents.UpdateAgentInSpace(ctx, agentdef.UpdateInput{
		AgentID:   cmd.AgentID,
		SpaceID:   cmd.SpaceID,
		UpdatedBy: cmd.UserID,
		Def: agentdef.Definition{
			Name:                  cmd.Name,
			Description:           cmd.Description,
			Instructions:          cmd.Instructions,
			Model:                 model,
			Plugins:               plugins,
			SandboxNetworkTier:    cmd.SandboxNetworkTier,
			SandboxFilesystemTier: cmd.SandboxFilesystemTier,
			SecretConsumption:     cmd.SecretConsumption.Canonical(),
		},
	})
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, ErrAgentNotFound
	}
	s.Audit.UserAction(ctx, cmd.UserID, cmd.SpaceID, coreaudit.AgentUpdated, "agent", updated.ID, updated.Name)
	return updated, nil
}

func (s *Service) ListRevisions(ctx context.Context, spaceID, agentID string, limit, offset int) ([]agentdef.Revision, int, error) {
	if _, err := s.GetAgent(ctx, spaceID, agentID); err != nil {
		return nil, 0, err
	}
	return s.Agents.ListAgentRevisions(ctx, agentID, limit, offset)
}

// RestoreRevision writes an older definition back as a new revision. Restoring
// is an edit, not a rewind: the history keeps growing.
func (s *Service) RestoreRevision(ctx context.Context, cmd RestoreRevisionCmd) (*agentdef.Agent, error) {
	if _, err := s.GetAgent(ctx, cmd.SpaceID, cmd.AgentID); err != nil {
		return nil, err
	}
	rev, err := s.Agents.GetAgentRevision(ctx, cmd.AgentID, cmd.Revision)
	if err != nil {
		return nil, err
	}
	if rev == nil {
		return nil, ErrRevisionNotFound
	}
	return s.UpdateAgent(ctx, UpdateCmd{
		SpaceID:               cmd.SpaceID,
		UserID:                cmd.UserID,
		AgentID:               cmd.AgentID,
		Name:                  rev.Name,
		Description:           rev.Description,
		Instructions:          rev.Instructions,
		Model:                 rev.Model,
		Plugins:               rev.Plugins,
		SandboxNetworkTier:    rev.SandboxNetworkTier,
		SandboxFilesystemTier: rev.SandboxFilesystemTier,
		SecretConsumption:     rev.SecretConsumption,
	})
}

// DeleteAgent marks an agent deleted, refusing while a published workflow still
// names it.
//
// Deleting it anyway would leave that workflow unable to run and the operator
// would only find out at its next step. The refusal names the workflows so they
// can be fixed or archived first.
func (s *Service) DeleteAgent(ctx context.Context, spaceID, agentID, userID string) error {
	if s.Agents == nil {
		return ErrAgentsNotConfigured
	}
	if s.Workflows != nil {
		using, err := s.Workflows.PublishedWorkflowsUsingAgent(ctx, spaceID, agentID)
		if err != nil {
			return err
		}
		if len(using) > 0 {
			return apierr.Detail(ErrUsedByPublishedFlows, "%s", workflowNameList(using))
		}
	}
	if err := s.Agents.DeleteAgentInSpace(ctx, agentID, spaceID); err != nil {
		if errors.Is(err, apierr.ErrNotFound) {
			return ErrAgentNotFound
		}
		return err
	}
	s.Audit.UserAction(ctx, userID, spaceID, coreaudit.AgentDeleted, "agent", agentID, "")
	return nil
}

// workflowNameList renders the blocking workflows as "name (id)": a name alone
// is ambiguous and an id alone means nothing to a reader.
func workflowNameList(workflows []coreworkflow.Workflow) string {
	parts := make([]string, len(workflows))
	for i := range workflows {
		parts[i] = workflows[i].Name + " (" + workflows[i].ID + ")"
	}
	return strings.Join(parts, ", ")
}
