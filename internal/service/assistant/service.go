// Package assistant manages Space Assistants: their definitions and
// revisions, the statement of what publishing one discloses, their bots, and
// keeping those bots connected to the chat Gateway. See
// docs/design/space-assistants.md.
package assistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/icloudbb/buildmax/internal/core/agentdef"
	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreartifact "github.com/icloudbb/buildmax/internal/core/artifact"
	coreassistant "github.com/icloudbb/buildmax/internal/core/assistant"
	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	coresecret "github.com/icloudbb/buildmax/internal/core/secret"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/service/audit"
	spacesvc "github.com/icloudbb/buildmax/internal/service/space"
)

var (
	ErrNotConfigured   = apierr.New(apierr.KindNotConfigured, "assistants not configured")
	ErrPersonalSpace   = apierr.New(apierr.KindInvalid, "a personal space cannot publish assistants")
	ErrUnknownModel    = apierr.New(apierr.KindInvalid, "unknown model")
	ErrInvalidSponsor  = apierr.New(apierr.KindInvalid, "the sponsor must be an enabled owner or admin of this space")
	ErrServiceAccount  = apierr.New(apierr.KindInvalid, "the service account must be a service account of this space")
	ErrAgentNotFound   = apierr.New(apierr.KindInvalid, "roster agent not found in this space")
	ErrWorkflowInvalid = apierr.New(apierr.KindInvalid, "roster workflow cannot be used")
	ErrFileNotFound    = apierr.New(apierr.KindInvalid, "readable file not found in this space")
)

// Spaces reads the Space an Assistant belongs to.
type Spaces interface {
	GetSpace(ctx context.Context, spaceID string) (*corespace.Space, error)
}

// Users reads accounts: the service account and sponsors.
type Users interface {
	GetUser(ctx context.Context, userID string) (*coreidentity.User, error)
}

// ServiceAccounts is the Space service's service-account surface.
type ServiceAccounts interface {
	CreateServiceAccount(ctx context.Context, cmd spacesvc.CreateServiceAccountCmd) (*spacesvc.ServiceAccount, error)
	ListServiceAccounts(ctx context.Context, spaceID string) ([]spacesvc.ServiceAccount, error)
	ValidSponsor(ctx context.Context, spaceID, sponsorID string) (bool, error)
}

// Agents, Workflows, Artifacts, and Secrets resolve what a definition names.
type Agents interface {
	GetAgent(ctx context.Context, agentID string) (*agentdef.Agent, error)
}

type Workflows interface {
	GetWorkflow(ctx context.Context, workflowID string) (*coreworkflow.Workflow, error)
}

type Artifacts interface {
	GetArtifact(ctx context.Context, artifactID string) (*coreartifact.Artifact, error)
}

type Secrets interface {
	ListSecretsBySpace(ctx context.Context, spaceID string) ([]coresecret.Secret, error)
}

// Models lists the catalog's model names. Nil means no catalog to check
// against, and a named model is stored as given, as for Agents.
type Models interface {
	ModelNames(ctx context.Context) ([]string, error)
}

// Service manages Space Assistants. Authorization is the transport's: every
// mutating call is made by an owner or admin of the Space
// (corespace.ActionManageAssistants).
type Service struct {
	Store           coreassistant.Store
	Spaces          Spaces
	Users           Users
	ServiceAccounts ServiceAccounts
	Agents          Agents
	Workflows       Workflows
	Artifacts       Artifacts
	Secrets         Secrets
	// SpaceFiles lists the Space's Files, which every roster Agent and Workflow
	// step can read, so the publish statement can name them. Nil leaves them
	// unnamed but still stated.
	SpaceFiles SpaceFiles
	Models     Models
	Audit      *audit.Recorder
	// Bots connects bindings to the chat Gateway. Nil leaves bindings stored
	// but unserved, as on a server without chat support.
	Bots *Reconciler
}

// View is an Assistant with what the Space's owners need to judge it.
type View struct {
	Assistant    coreassistant.Assistant
	Binding      *coreassistant.Binding
	Availability coreassistant.Availability
	Statement    Statement
}

func (s *Service) ready() error {
	if s == nil || s.Store == nil || s.Spaces == nil || s.Users == nil || s.ServiceAccounts == nil {
		return ErrNotConfigured
	}
	return nil
}

// List returns the Space's Assistants.
func (s *Service) List(ctx context.Context, spaceID string) ([]View, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	list, err := s.Store.ListAssistantsBySpace(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	out := make([]View, 0, len(list))
	for i := range list {
		v, err := s.view(ctx, &list[i])
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, nil
}

// Get returns one of the Space's Assistants.
func (s *Service) Get(ctx context.Context, spaceID, assistantID string) (*View, error) {
	a, err := s.load(ctx, spaceID, assistantID)
	if err != nil {
		return nil, err
	}
	return s.view(ctx, a)
}

func (s *Service) load(ctx context.Context, spaceID, assistantID string) (*coreassistant.Assistant, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	a, err := s.Store.GetAssistant(ctx, assistantID)
	if err != nil {
		return nil, err
	}
	// Another Space's Assistant is reported as missing, so an id is not an
	// existence oracle across Spaces.
	if a == nil || a.SpaceID != spaceID {
		return nil, coreassistant.ErrNotFound
	}
	return a, nil
}

func (s *Service) view(ctx context.Context, a *coreassistant.Assistant) (*View, error) {
	b, err := s.Store.GetBindingByAssistant(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	avail, err := s.Availability(ctx, a)
	if err != nil {
		return nil, err
	}
	st, err := s.Statement(ctx, a.SpaceID, a.Def)
	if err != nil {
		return nil, err
	}
	return &View{Assistant: *a, Binding: b, Availability: avail, Statement: st}, nil
}

// CreateCmd creates an Assistant. An empty Def.ServiceAccountID creates a
// service account with the Assistant's name, the default (design §6.2).
type CreateCmd struct {
	SpaceID string
	ActorID string
	Def     coreassistant.Definition
}

// Create defines a new Assistant. It starts paused, with the actor as sponsor:
// publishing is a separate, confirmed step.
func (s *Service) Create(ctx context.Context, cmd CreateCmd) (*View, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	space, err := s.Spaces.GetSpace(ctx, cmd.SpaceID)
	if err != nil {
		return nil, err
	}
	if space == nil {
		return nil, apierr.ErrNotFound
	}
	if space.PersonalForUserID != nil {
		return nil, ErrPersonalSpace
	}
	def, err := coreassistant.Normalize(cmd.Def)
	if err != nil {
		return nil, err
	}
	if ok, err := s.ServiceAccounts.ValidSponsor(ctx, cmd.SpaceID, cmd.ActorID); err != nil || !ok {
		if err != nil {
			return nil, err
		}
		return nil, ErrInvalidSponsor
	}
	// Everything else is checked before a service account is made for it, so
	// a refused definition leaves nothing behind.
	if err := s.checkReferences(ctx, cmd.SpaceID, &def, def.ServiceAccountID != ""); err != nil {
		return nil, err
	}
	if def.ServiceAccountID == "" {
		sa, err := s.ServiceAccounts.CreateServiceAccount(ctx, spacesvc.CreateServiceAccountCmd{
			SpaceID: cmd.SpaceID, ActorID: cmd.ActorID, Name: def.Name,
		})
		if err != nil {
			return nil, fmt.Errorf("create service account: %w", err)
		}
		def.ServiceAccountID = sa.User.ID
		s.Audit.UserAction(ctx, cmd.ActorID, cmd.SpaceID, coreaudit.ServiceAccountCreated, "user", sa.User.ID, sa.User.Name)
	}
	a, err := s.Store.CreateAssistant(ctx, cmd.SpaceID, cmd.ActorID, cmd.ActorID, def)
	if err != nil {
		return nil, err
	}
	s.Audit.UserAction(ctx, cmd.ActorID, cmd.SpaceID, coreaudit.AssistantCreated, "assistant", a.ID, def.Name)
	return s.view(ctx, a)
}

// UpdateCmd changes an Assistant's definition, its sponsor, or both. Def is the
// whole proposed definition. ConfirmStatement is the digest of the statement
// the owner was shown, required when an active Assistant's statement changes.
type UpdateCmd struct {
	SpaceID          string
	ActorID          string
	AssistantID      string
	Def              *coreassistant.Definition
	SponsorUserID    *string
	ConfirmStatement string
}

// StatementError refuses a change that needs the owner to confirm what it
// discloses, carrying the statement to show them.
type StatementError struct {
	Statement Statement
}

func (e *StatementError) Error() string { return coreassistant.ErrStatementNotConfirmed.Error() }

func (e *StatementError) Unwrap() error { return coreassistant.ErrStatementNotConfirmed }

// Update applies cmd. Changing what an active Assistant discloses is publishing
// it again, so it needs the new statement confirmed.
func (s *Service) Update(ctx context.Context, cmd UpdateCmd) (*View, error) {
	a, err := s.load(ctx, cmd.SpaceID, cmd.AssistantID)
	if err != nil {
		return nil, err
	}
	if cmd.SponsorUserID != nil {
		sponsor := strings.TrimSpace(*cmd.SponsorUserID)
		if ok, err := s.ServiceAccounts.ValidSponsor(ctx, cmd.SpaceID, sponsor); err != nil || !ok {
			if err != nil {
				return nil, err
			}
			return nil, ErrInvalidSponsor
		}
		cmd.SponsorUserID = &sponsor
	}
	if cmd.Def != nil {
		def, err := coreassistant.Normalize(*cmd.Def)
		if err != nil {
			return nil, err
		}
		if def.ServiceAccountID == "" {
			def.ServiceAccountID = a.Def.ServiceAccountID
		}
		if err := s.checkReferences(ctx, cmd.SpaceID, &def, true); err != nil {
			return nil, err
		}
		if a.State == coreassistant.StateActive {
			before, err := s.Statement(ctx, cmd.SpaceID, a.Def)
			if err != nil {
				return nil, err
			}
			after, err := s.Statement(ctx, cmd.SpaceID, def)
			if err != nil {
				return nil, err
			}
			if after.Digest != before.Digest && cmd.ConfirmStatement != after.Digest {
				return nil, &StatementError{Statement: after}
			}
		}
		prev := a.Revision
		if a, err = s.Store.UpdateAssistantDefinition(ctx, a.ID, cmd.ActorID, def); err != nil {
			return nil, err
		}
		if a.Revision != prev {
			s.Audit.UserAction(ctx, cmd.ActorID, cmd.SpaceID, coreaudit.AssistantUpdated, "assistant", a.ID, fmt.Sprintf("revision %d", a.Revision))
		}
	}
	if cmd.SponsorUserID != nil && *cmd.SponsorUserID != a.SponsorUserID {
		if err := s.Store.SetAssistantSponsor(ctx, a.ID, *cmd.SponsorUserID); err != nil {
			return nil, err
		}
		s.Audit.UserAction(ctx, cmd.ActorID, cmd.SpaceID, coreaudit.AssistantSponsorChanged, "assistant", a.ID, *cmd.SponsorUserID)
	}
	return s.Get(ctx, cmd.SpaceID, a.ID)
}

// SetStateCmd publishes or pauses an Assistant. Activating needs the digest of
// the current statement.
type SetStateCmd struct {
	SpaceID          string
	ActorID          string
	AssistantID      string
	State            string
	ConfirmStatement string
}

// SetState publishes or pauses an Assistant.
func (s *Service) SetState(ctx context.Context, cmd SetStateCmd) (*View, error) {
	a, err := s.load(ctx, cmd.SpaceID, cmd.AssistantID)
	if err != nil {
		return nil, err
	}
	switch cmd.State {
	case coreassistant.StatePaused:
	case coreassistant.StateActive:
		st, err := s.Statement(ctx, cmd.SpaceID, a.Def)
		if err != nil {
			return nil, err
		}
		if cmd.ConfirmStatement != st.Digest {
			return nil, &StatementError{Statement: st}
		}
	default:
		return nil, coreassistant.ErrInvalidState
	}
	if a.State != cmd.State {
		if err := s.Store.SetAssistantState(ctx, a.ID, cmd.State); err != nil {
			return nil, err
		}
		action := coreaudit.AssistantPaused
		if cmd.State == coreassistant.StateActive {
			action = coreaudit.AssistantActivated
		}
		s.Audit.UserAction(ctx, cmd.ActorID, cmd.SpaceID, action, "assistant", a.ID, a.Def.Name)
	}
	return s.Get(ctx, cmd.SpaceID, a.ID)
}

// Delete removes an Assistant and releases its bot. Its service account stays:
// other automation may run as it, and disabling it is a separate decision.
func (s *Service) Delete(ctx context.Context, spaceID, actorID, assistantID string) error {
	a, err := s.load(ctx, spaceID, assistantID)
	if err != nil {
		return err
	}
	if err := s.Store.DeleteAssistant(ctx, a.ID); err != nil {
		return err
	}
	s.Audit.UserAction(ctx, actorID, spaceID, coreaudit.AssistantDeleted, "assistant", a.ID, a.Def.Name)
	s.Bots.Trigger()
	return nil
}

// Availability is whether a can answer now. An owner's pause comes first;
// otherwise an unusable service account or a sponsor no longer accountable
// pauses it without anyone's action (design §6.3).
func (s *Service) Availability(ctx context.Context, a *coreassistant.Assistant) (coreassistant.Availability, error) {
	if a.State != coreassistant.StateActive {
		return coreassistant.PausedByOwner, nil
	}
	sa, err := s.Users.GetUser(ctx, a.Def.ServiceAccountID)
	if err != nil {
		return "", err
	}
	if sa == nil || !sa.IsService() || sa.Disabled() {
		return coreassistant.ServiceAccountDisabled, nil
	}
	accounts, err := s.ServiceAccounts.ListServiceAccounts(ctx, a.SpaceID)
	if err != nil {
		return "", err
	}
	i := slices.IndexFunc(accounts, func(x spacesvc.ServiceAccount) bool { return x.User.ID == sa.ID })
	if i < 0 {
		return coreassistant.ServiceAccountDisabled, nil
	}
	if accounts[i].NeedsSponsor {
		return coreassistant.NeedsSponsor, nil
	}
	ok, err := s.ServiceAccounts.ValidSponsor(ctx, a.SpaceID, a.SponsorUserID)
	if err != nil {
		return "", err
	}
	if !ok {
		return coreassistant.NeedsSponsor, nil
	}
	return coreassistant.Available, nil
}

// checkReferences verifies everything def names belongs to the Space and is
// usable: the model, the roster's Agents and published Workflows with their
// release contracts, the readable files, and (when checkAccount) the service
// account.
func (s *Service) checkReferences(ctx context.Context, spaceID string, def *coreassistant.Definition, checkAccount bool) error {
	if def.Model != "" && s.Models != nil {
		names, err := s.Models.ModelNames(ctx)
		if err != nil {
			return err
		}
		if !slices.Contains(names, def.Model) {
			return apierr.Detail(ErrUnknownModel, "%s", def.Model)
		}
	}
	for _, e := range def.Roster {
		switch e.Kind {
		case coreassistant.KindAgent:
			if s.Agents == nil {
				return ErrNotConfigured
			}
			ag, err := s.Agents.GetAgent(ctx, e.ID)
			if err != nil && !errors.Is(err, apierr.ErrNotFound) {
				return err
			}
			if ag == nil || ag.SpaceID != spaceID {
				return apierr.Detail(ErrAgentNotFound, "%s", e.ID)
			}
		case coreassistant.KindWorkflow:
			schema, err := s.workflowResultSchema(ctx, spaceID, e.ID)
			if err != nil {
				return err
			}
			if err := coreassistant.CheckReleasable(e.Releasable, schema); err != nil {
				return apierr.Detail(ErrWorkflowInvalid, "%s: %v", e.ID, err)
			}
		}
	}
	for _, id := range def.ReadableFiles {
		if s.Artifacts == nil {
			return ErrNotConfigured
		}
		f, err := s.Artifacts.GetArtifact(ctx, id)
		if err != nil && !errors.Is(err, apierr.ErrNotFound) {
			return err
		}
		if f == nil || f.Deleted() || f.SpaceID != spaceID {
			return apierr.Detail(ErrFileNotFound, "%s", id)
		}
	}
	if !checkAccount {
		return nil
	}
	accounts, err := s.ServiceAccounts.ListServiceAccounts(ctx, spaceID)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(accounts, func(x spacesvc.ServiceAccount) bool { return x.User.ID == def.ServiceAccountID }) {
		return ErrServiceAccount
	}
	return nil
}

// workflowResultSchema returns the output_schema of the node a published
// Workflow's result is selected from. A Workflow without one has no result to
// release, so it cannot be on a roster. The result must be that node's
// structured output, the value the schema describes: the node's whole output
// is an envelope around it, whose top level holds none of the fields an
// entry releases.
func (s *Service) workflowResultSchema(ctx context.Context, spaceID, workflowID string) (json.RawMessage, error) {
	if s.Workflows == nil {
		return nil, ErrNotConfigured
	}
	wf, err := s.Workflows.GetWorkflow(ctx, workflowID)
	if err != nil && !errors.Is(err, apierr.ErrNotFound) {
		return nil, err
	}
	if wf == nil || wf.SpaceID != spaceID {
		return nil, apierr.Detail(ErrWorkflowInvalid, "%s is not a workflow of this space", workflowID)
	}
	if wf.Status != coreworkflow.StatusPublished {
		return nil, apierr.Detail(ErrWorkflowInvalid, "%s is not published", workflowID)
	}
	def, err := parseWorkflow(wf)
	if err != nil {
		return nil, err
	}
	if def.Result == nil || def.Result.Pointer != coreworkflow.StructuredOutputPointer {
		return nil, apierr.Detail(ErrWorkflowInvalid, "%s must select its result as one node's structured output (pointer %q)", workflowID, coreworkflow.StructuredOutputPointer)
	}
	node, ok := coreworkflow.ParseNodeOutputSource(def.Result.Source)
	if ok {
		for i := range def.Nodes {
			if def.Nodes[i].ID == node && len(def.Nodes[i].OutputSchema) > 0 {
				return def.Nodes[i].OutputSchema, nil
			}
		}
	}
	return nil, apierr.Detail(ErrWorkflowInvalid, "%s's result node declares no output_schema", workflowID)
}

func parseWorkflow(wf *coreworkflow.Workflow) (*coreworkflow.Definition, error) {
	var def coreworkflow.Definition
	if err := json.Unmarshal([]byte(wf.Definition), &def); err != nil {
		return nil, apierr.Detail(ErrWorkflowInvalid, "%s: %v", wf.ID, err)
	}
	return &def, nil
}

// maxStatementFiles bounds how many of the Space's Files the statement names.
const maxStatementFiles = 20

// SpaceFiles lists a Space's Files by path.
type SpaceFiles interface {
	ListFiles(ctx context.Context, spaceID string) ([]string, error)
}

// Statement is what publishing an Assistant discloses, in the terms a Space
// owner can judge (design §8): who can ask, what it can read, what it can run,
// and which Secrets that work can use.
type Statement struct {
	Audience      string           `json:"audience"`
	ReadableFiles []NamedRef       `json:"readable_files"`
	Agents        []StatementAgent `json:"agents"`
	Workflows     []StatementFlow  `json:"workflows"`
	// SpaceFiles are the Space's Files the roster's work can read, named
	// because the readable-files list alone does not show them (validation
	// run, design §18). Empty when the roster runs nothing.
	SpaceFiles      []string `json:"space_files"`
	SpaceFilesTotal int      `json:"space_files_total"`
	Text            string   `json:"text"`
	// Digest identifies this statement. Confirming it is how an owner says
	// they saw exactly this before it went live.
	Digest string `json:"digest"`
}

type NamedRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type StatementAgent struct {
	NamedRef
	Secrets []string `json:"secrets"`
}

type StatementFlow struct {
	NamedRef
	// Agents are the Agents the Workflow's nodes run as, with their Secrets: a
	// Workflow reaches whatever its steps reach.
	Agents []StatementAgent `json:"agents"`
}

// Statement builds the publish statement for def in its Space.
func (s *Service) Statement(ctx context.Context, spaceID string, def coreassistant.Definition) (Statement, error) {
	st := Statement{Audience: def.Audience, ReadableFiles: []NamedRef{}, Agents: []StatementAgent{}, Workflows: []StatementFlow{}}
	secretNames := map[string]string{}
	if s.Secrets != nil {
		list, err := s.Secrets.ListSecretsBySpace(ctx, spaceID)
		if err != nil {
			return st, err
		}
		for _, sec := range list {
			secretNames[sec.ID] = sec.Name
		}
	}
	describeAgent := func(id string) (StatementAgent, error) {
		out := StatementAgent{NamedRef: NamedRef{ID: id, Name: id}, Secrets: []string{}}
		if s.Agents == nil {
			return out, nil
		}
		ag, err := s.Agents.GetAgent(ctx, id)
		if err != nil && !errors.Is(err, apierr.ErrNotFound) {
			return out, err
		}
		if ag == nil {
			return out, nil
		}
		out.Name = ag.Name
		for _, g := range ag.SecretConsumption.Env {
			name := secretNames[g.Secret]
			if name == "" {
				name = g.Secret
			}
			if !slices.Contains(out.Secrets, name) {
				out.Secrets = append(out.Secrets, name)
			}
		}
		slices.Sort(out.Secrets)
		return out, nil
	}
	for _, e := range def.Roster {
		switch e.Kind {
		case coreassistant.KindAgent:
			ag, err := describeAgent(e.ID)
			if err != nil {
				return st, err
			}
			st.Agents = append(st.Agents, ag)
		case coreassistant.KindWorkflow:
			flow := StatementFlow{NamedRef: NamedRef{ID: e.ID, Name: e.ID}, Agents: []StatementAgent{}}
			if s.Workflows != nil {
				wf, err := s.Workflows.GetWorkflow(ctx, e.ID)
				if err != nil && !errors.Is(err, apierr.ErrNotFound) {
					return st, err
				}
				if wf != nil {
					flow.Name = wf.Name
					if wdef, err := parseWorkflow(wf); err == nil {
						var seen []string
						for _, n := range wdef.Nodes {
							if n.Agent.ID == "" || slices.Contains(seen, n.Agent.ID) {
								continue
							}
							seen = append(seen, n.Agent.ID)
							ag, err := describeAgent(n.Agent.ID)
							if err != nil {
								return st, err
							}
							flow.Agents = append(flow.Agents, ag)
						}
					}
				}
			}
			st.Workflows = append(st.Workflows, flow)
		}
	}
	for _, id := range def.ReadableFiles {
		ref := NamedRef{ID: id, Name: id}
		if s.Artifacts != nil {
			if f, err := s.Artifacts.GetArtifact(ctx, id); err == nil && f != nil {
				ref.Name = f.Filename
			}
		}
		st.ReadableFiles = append(st.ReadableFiles, ref)
	}
	st.SpaceFiles = []string{}
	if (len(st.Agents) > 0 || len(st.Workflows) > 0) && s.SpaceFiles != nil {
		files, err := s.SpaceFiles.ListFiles(ctx, spaceID)
		if err != nil {
			return st, err
		}
		slices.Sort(files)
		st.SpaceFilesTotal = len(files)
		st.SpaceFiles = files[:min(len(files), maxStatementFiles)]
	}
	st.Text = statementText(st)
	body, err := json.Marshal(struct {
		Audience      string
		ReadableFiles []NamedRef
		Agents        []StatementAgent
		Workflows     []StatementFlow
		SpaceFiles    []string
		SpaceFilesN   int
	}{st.Audience, st.ReadableFiles, st.Agents, st.Workflows, st.SpaceFiles, st.SpaceFilesTotal})
	if err != nil {
		return st, err
	}
	sum := sha256.Sum256(body)
	st.Digest = hex.EncodeToString(sum[:])
	return st, nil
}

func statementText(st Statement) string {
	var b strings.Builder
	if st.Audience == coreassistant.AudienceAllUsers {
		b.WriteString("Any active user of this deployment can ask this Assistant.")
	} else {
		b.WriteString("Any member of this Space can ask this Assistant.")
	}
	if len(st.ReadableFiles) > 0 {
		b.WriteString(" It can read " + joinNames(refNames(st.ReadableFiles)) + ".")
	} else {
		b.WriteString(" It can read no files.")
	}
	for _, ag := range st.Agents {
		fmt.Fprintf(&b, " It can run the Agent %q%s.", ag.Name, secretsClause(ag.Secrets))
	}
	for _, f := range st.Workflows {
		fmt.Fprintf(&b, " It can run the Workflow %q", f.Name)
		if len(f.Agents) > 0 {
			var parts []string
			for _, ag := range f.Agents {
				parts = append(parts, fmt.Sprintf("%q%s", ag.Name, secretsClause(ag.Secrets)))
			}
			b.WriteString(", whose steps run as " + joinNames(parts))
		}
		b.WriteString(".")
	}
	if len(st.Agents) == 0 && len(st.Workflows) == 0 {
		b.WriteString(" It can run no Agents or Workflows.")
	} else {
		// What its work reads is part of what it discloses, and is not on the
		// readable-files list.
		b.WriteString(" Those Agents and Workflow steps can read every file in this Space's Files")
		if st.SpaceFilesTotal == 0 {
			b.WriteString(", which is empty now.")
		} else {
			quoted := make([]string, len(st.SpaceFiles))
			for i, n := range st.SpaceFiles {
				quoted[i] = fmt.Sprintf("%q", n)
			}
			b.WriteString(", now " + joinNames(quoted))
			if more := st.SpaceFilesTotal - len(st.SpaceFiles); more > 0 {
				fmt.Fprintf(&b, " and %d more", more)
			}
			b.WriteString(".")
		}
		// The requester's identity goes the other way: to the Space's work.
		b.WriteString(" Each of them is told the name and email of the person who asked.")
	}
	b.WriteString(" Treat everything it can read or run as disclosed to everyone who can ask.")
	return b.String()
}

func secretsClause(secrets []string) string {
	if len(secrets) == 0 {
		return ""
	}
	quoted := make([]string, len(secrets))
	for i, n := range secrets {
		quoted[i] = fmt.Sprintf("%q", n)
	}
	return ", which holds the Secrets " + joinNames(quoted)
}

func refNames(refs []NamedRef) []string {
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = fmt.Sprintf("%q", r.Name)
	}
	return out
}

func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
