// Package assistant is the Space Assistant: a service front door a Space
// publishes on its own chat bot. It answers people outside the Space's work,
// dispatches a bounded roster of the Space's Agents and Workflows as Tasks
// under the Assistant's service account, and releases only contracted result
// fields. See docs/design/space-assistants.md.
package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	"github.com/icloudbb/buildmax/internal/core/jsonschema"
)

// Audiences: who may ask. A policy over BuildMax users, never over chat
// accounts, because every requester is a linked user (design §7.2).
const (
	AudienceSpaceMembers = "space_members"
	AudienceAllUsers     = "all_users"
)

// States an owner sets. Whether an active Assistant can actually answer also
// depends on its service account (Availability).
const (
	StateActive = "active"
	StatePaused = "paused"
)

// Roster entry kinds.
const (
	KindAgent    = "agent"
	KindWorkflow = "workflow"
)

// Bounds on a definition. They keep the publish statement readable and the
// front-door prompt small; none is a product limit worth configuring.
const (
	MaxNameRunes         = 255
	MaxDescriptionRunes  = 2000
	MaxInstructionsRunes = 32000
	MaxRosterEntries     = 20
	MaxReadableFiles     = 50
)

var (
	ErrNotFound            = apierr.New(apierr.KindNotFound, "assistant not found")
	ErrNameRequired        = apierr.New(apierr.KindInvalid, "name is required")
	ErrTooLong             = apierr.New(apierr.KindInvalid, "field is too long")
	ErrInvalidAudience     = apierr.New(apierr.KindInvalid, "audience must be space_members or all_users")
	ErrInvalidState        = apierr.New(apierr.KindInvalid, "state must be active or paused")
	ErrInvalidRoster       = apierr.New(apierr.KindInvalid, "invalid roster entry")
	ErrInvalidReadableFile = apierr.New(apierr.KindInvalid, "invalid readable file")
	// ErrStatementNotConfirmed is the answer to activating, or widening an
	// active Assistant, without confirming the statement of what it discloses.
	ErrStatementNotConfirmed = apierr.New(apierr.KindConflict, "confirm what this assistant discloses before publishing it")
	ErrAlreadyBound          = apierr.New(apierr.KindConflict, "this assistant already has a bot; unbind it first")
	ErrNotBound              = apierr.New(apierr.KindNotFound, "this assistant has no bot")
	ErrInvalidBotToken       = apierr.New(apierr.KindInvalid, "the chat platform rejected this bot token")
	ErrUnsupportedPlatform   = apierr.New(apierr.KindInvalid, "unsupported chat platform")
)

// RosterEntry is one Agent or published Workflow the Assistant may dispatch,
// with its release contract: the result shape and which of its top-level fields
// may reach a requester (design §8).
type RosterEntry struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	// OutputSchema is the result shape an Agent entry's Tasks must satisfy.
	// Empty for a Workflow, whose result node declares its own.
	OutputSchema json.RawMessage `json:"output_schema,omitempty"`
	// Releasable lists top-level properties of the result a requester may see.
	Releasable []string `json:"releasable"`
}

// Definition is everything a revision records: what answered and how.
// Sponsor and state are not part of it, because changing who is accountable
// or pausing does not change what an answer was produced by.
type Definition struct {
	Name          string        `json:"name"`
	Description   string        `json:"description"`
	Instructions  string        `json:"instructions"`
	Model         string        `json:"model,omitempty"`
	Roster        []RosterEntry `json:"roster"`
	ReadableFiles []string      `json:"readable_files"`
	Audience      string        `json:"audience"`
	// ServiceAccountID is the user the Assistant's work runs as (design §6).
	ServiceAccountID string `json:"service_account_id"`
}

// Assistant is a Space Assistant at its current revision.
type Assistant struct {
	ID       string
	SpaceID  string
	Def      Definition
	Revision int
	State    string
	// SponsorUserID is the owner or admin accountable for the Assistant.
	SponsorUserID string
	CreatedBy     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Requester is a person who has a conversation with an Assistant: someone the
// Assistant's bot may message, such as a Schedule's delivery target.
type Requester struct {
	UserID             string    `json:"user_id"`
	Name               string    `json:"name,omitempty"`
	LastConversationAt time.Time `json:"last_conversation_at"`
}

// Binding is an Assistant's bot. The token never leaves the store except to
// build the connector that speaks as the bot.
type Binding struct {
	ID          string
	AssistantID string
	SpaceID     string
	Platform    string
	// BotID is the platform's id for the bot account; one binding per bot.
	BotID     string
	BotHandle string
	CreatedBy string
	CreatedAt time.Time
}

// NewBinding is a binding to store with its token.
type NewBinding struct {
	AssistantID string
	Platform    string
	BotID       string
	BotHandle   string
	Token       string
	CreatedBy   string
}

// Store persists Assistants, their append-only revisions, and their bindings.
type Store interface {
	CreateAssistant(ctx context.Context, spaceID, createdBy, sponsorUserID string, def Definition) (*Assistant, error)
	// GetAssistant returns a live Assistant, or (nil, nil).
	GetAssistant(ctx context.Context, assistantID string) (*Assistant, error)
	ListAssistantsBySpace(ctx context.Context, spaceID string) ([]Assistant, error)
	// UpdateAssistantDefinition appends a revision when def differs from the
	// current one, and returns the result either way.
	UpdateAssistantDefinition(ctx context.Context, assistantID, updatedBy string, def Definition) (*Assistant, error)
	SetAssistantState(ctx context.Context, assistantID, state string) error
	SetAssistantSponsor(ctx context.Context, assistantID, sponsorUserID string) error
	// DeleteAssistant marks it deleted and removes its binding.
	DeleteAssistant(ctx context.Context, assistantID string) error

	// CreateBinding stores a binding with its sealed token. ErrAlreadyBound
	// when the Assistant has one; corechannel.ErrBotInUse when another binding
	// holds the bot.
	CreateBinding(ctx context.Context, in NewBinding) (*Binding, error)
	// GetBindingByAssistant returns the Assistant's binding, or (nil, nil).
	GetBindingByAssistant(ctx context.Context, assistantID string) (*Binding, error)
	// GetBinding returns a binding by id, or (nil, nil).
	GetBinding(ctx context.Context, bindingID string) (*Binding, error)
	DeleteBindingByAssistant(ctx context.Context, assistantID string) error
	// ListBindings returns every binding of a live Assistant, for the
	// reconciler that keeps their bots connected.
	ListBindings(ctx context.Context) ([]Binding, error)
	// BindingToken opens a binding's sealed token.
	BindingToken(ctx context.Context, bindingID string) (string, error)
}

// Normalize trims a definition and checks what can be checked without the
// Space: lengths, audience, roster shape, and release contracts against the
// Agent entries' own schemas. Membership of the referenced Agents, Workflows,
// files, and service account is the service's to check.
func Normalize(def Definition) (Definition, error) {
	def.Name = strings.TrimSpace(def.Name)
	def.Description = strings.TrimSpace(def.Description)
	def.Instructions = strings.TrimSpace(def.Instructions)
	def.Model = strings.TrimSpace(def.Model)
	def.ServiceAccountID = strings.TrimSpace(def.ServiceAccountID)
	if def.Name == "" {
		return def, ErrNameRequired
	}
	for _, f := range []struct {
		name  string
		value string
		limit int
	}{
		{"name", def.Name, MaxNameRunes},
		{"description", def.Description, MaxDescriptionRunes},
		{"instructions", def.Instructions, MaxInstructionsRunes},
	} {
		if utf8.RuneCountInString(f.value) > f.limit {
			return def, apierr.Detail(ErrTooLong, "%s exceeds %d characters", f.name, f.limit)
		}
	}
	if def.Audience != AudienceSpaceMembers && def.Audience != AudienceAllUsers {
		return def, ErrInvalidAudience
	}
	if len(def.Roster) > MaxRosterEntries {
		return def, apierr.Detail(ErrInvalidRoster, "at most %d entries", MaxRosterEntries)
	}
	seen := map[string]bool{}
	for i := range def.Roster {
		e := &def.Roster[i]
		e.ID = strings.TrimSpace(e.ID)
		if e.ID == "" || (e.Kind != KindAgent && e.Kind != KindWorkflow) {
			return def, apierr.Detail(ErrInvalidRoster, "entry %d needs kind agent or workflow and an id", i+1)
		}
		if seen[e.Kind+":"+e.ID] {
			return def, apierr.Detail(ErrInvalidRoster, "%s %s is listed twice", e.Kind, e.ID)
		}
		seen[e.Kind+":"+e.ID] = true
		if e.Releasable == nil {
			e.Releasable = []string{}
		}
		switch e.Kind {
		case KindAgent:
			if len(e.OutputSchema) == 0 {
				return def, apierr.Detail(ErrInvalidRoster, "agent %s needs an output_schema", e.ID)
			}
			props, err := ObjectProperties(e.OutputSchema)
			if err != nil {
				return def, apierr.Detail(ErrInvalidRoster, "agent %s output_schema: %v", e.ID, err)
			}
			if err := checkReleasable(e.Releasable, props); err != nil {
				return def, apierr.Detail(ErrInvalidRoster, "agent %s: %v", e.ID, err)
			}
		case KindWorkflow:
			if len(e.OutputSchema) != 0 {
				return def, apierr.Detail(ErrInvalidRoster, "workflow %s takes its output_schema from its result node", e.ID)
			}
		}
	}
	if len(def.ReadableFiles) > MaxReadableFiles {
		return def, apierr.Detail(ErrInvalidReadableFile, "at most %d files", MaxReadableFiles)
	}
	files := make([]string, 0, len(def.ReadableFiles))
	for _, f := range def.ReadableFiles {
		f = strings.TrimSpace(f)
		if f == "" || slices.Contains(files, f) {
			return def, apierr.Detail(ErrInvalidReadableFile, "file ids must be non-empty and listed once")
		}
		files = append(files, f)
	}
	def.ReadableFiles = files
	if def.Roster == nil {
		def.Roster = []RosterEntry{}
	}
	return def, nil
}

// CheckReleasable verifies releasable names top-level properties of a
// Workflow's result schema, which only the service can find.
func CheckReleasable(releasable []string, schema json.RawMessage) error {
	props, err := ObjectProperties(schema)
	if err != nil {
		return err
	}
	return checkReleasable(releasable, props)
}

func checkReleasable(releasable, props []string) error {
	for i, name := range releasable {
		if !slices.Contains(props, name) {
			return fmt.Errorf("releasable field %q is not a top-level property of the result", name)
		}
		if slices.Contains(releasable[:i], name) {
			return fmt.Errorf("releasable field %q is listed twice", name)
		}
	}
	return nil
}

// ObjectProperties compiles schema, which must be an object schema in the
// shared subset, and returns its top-level property names: the units a
// release contract can release.
func ObjectProperties(schema json.RawMessage) ([]string, error) {
	if _, err := jsonschema.Compile(schema); err != nil {
		return nil, err
	}
	var top struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(schema, &top); err != nil {
		return nil, err
	}
	if top.Type != jsonschema.TypeObject {
		return nil, fmt.Errorf("the result must be an object schema")
	}
	names := make([]string, 0, len(top.Properties))
	for name := range top.Properties {
		names = append(names, name)
	}
	slices.Sort(names)
	return names, nil
}

// Availability is whether an Assistant answers now, and why not.
type Availability string

const (
	Available Availability = "available"
	// PausedByOwner: the Space paused it.
	PausedByOwner Availability = "paused"
	// ServiceAccountDisabled and NeedsSponsor pause it automatically, so an
	// Assistant serving a wide audience never runs unowned (design §6.3).
	ServiceAccountDisabled Availability = "service_account_disabled"
	NeedsSponsor           Availability = "needs_sponsor"
)
