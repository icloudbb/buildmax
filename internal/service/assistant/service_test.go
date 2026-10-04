package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/core/agentdef"
	coreartifact "github.com/icloudbb/buildmax/internal/core/artifact"
	coreassistant "github.com/icloudbb/buildmax/internal/core/assistant"
	corechannel "github.com/icloudbb/buildmax/internal/core/channel"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	coresecret "github.com/icloudbb/buildmax/internal/core/secret"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/mock"
	spacesvc "github.com/icloudbb/buildmax/internal/service/space"
	"github.com/icloudbb/buildmax/internal/util"
)

const (
	team     = "space_team"
	other    = "space_other"
	personal = "space_personal"
	owner    = "user_owner"
	admin    = "user_admin"
	member   = "user_member"
)

var resultSchema = json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string"},"raw":{"type":"string"}},"required":["answer","raw"],"additionalProperties":false}`)

type fakeSecrets []coresecret.Secret

func (f fakeSecrets) ListSecretsBySpace(_ context.Context, spaceID string) ([]coresecret.Secret, error) {
	var out []coresecret.Secret
	for _, s := range f {
		if s.SpaceID == spaceID {
			out = append(out, s)
		}
	}
	return out, nil
}

type fixture struct {
	svc       *Service
	store     *mock.MockAssistantStore
	users     *mock.MockUserStore
	spaces    *mock.MockSpaceStore
	gateway   *fakeGateway
	connector map[string]*fakeConnector
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	users := &mock.MockUserStore{ByID: map[string]*coreidentity.User{}}
	for _, id := range []string{owner, admin, member} {
		users.ByID[id] = &coreidentity.User{ID: id, Name: id, Kind: coreidentity.KindHuman}
	}
	spaces := &mock.MockSpaceStore{
		Spaces: []corespace.Space{
			{ID: team, Name: "HR"},
			{ID: other, Name: "Other"},
			{ID: personal, Name: "Mine", PersonalForUserID: util.Ptr(owner)},
		},
		Members: []corespace.Member{
			{SpaceID: team, UserID: owner, Role: corespace.RoleOwner},
			{SpaceID: team, UserID: admin, Role: corespace.RoleAdmin},
			{SpaceID: team, UserID: member, Role: corespace.RoleMember},
			{SpaceID: personal, UserID: owner, Role: corespace.RoleOwner},
		},
	}
	agents := &mock.MockAgentStore{Agents: []agentdef.Agent{
		{ID: "agent_hr", SpaceID: team, Name: "HR Agent", SecretConsumption: agentdef.SecretConsumption{
			Env: []agentdef.SecretEnvGrant{{Secret: "sec_hris"}},
		}},
		{ID: "agent_elsewhere", SpaceID: other, Name: "Elsewhere"},
	}}
	wfDef := `{"schema_version":2,"nodes":[{"id":"lookup","type":"agent_task","agent":{"id":"agent_hr"},"input":{"instruction":"x"},"output_schema":` + string(resultSchema) + `}],"result":{"source":"node.lookup.output","pointer":""}}`
	workflows := &mock.MockWorkflowStore{Workflows: []coreworkflow.Workflow{
		{ID: "wf_leave", SpaceID: team, Name: "Leave lookup", Status: coreworkflow.StatusPublished, Definition: wfDef},
		{ID: "wf_draft", SpaceID: team, Name: "Draft", Status: "draft", Definition: wfDef},
		{ID: "wf_noresult", SpaceID: team, Name: "No result", Status: coreworkflow.StatusPublished, Definition: `{"schema_version":2,"nodes":[]}`},
	}}
	artifacts := &mock.MockArtifactStore{}
	if _, err := artifacts.CreateArtifact(ctx, coreartifact.CreateInput{ArtifactID: "file_policy", SpaceID: team, Filename: "leave-policy.md"}); err != nil {
		t.Fatal(err)
	}
	if _, err := artifacts.CreateArtifact(ctx, coreartifact.CreateInput{ArtifactID: "file_other", SpaceID: other, Filename: "x.md"}); err != nil {
		t.Fatal(err)
	}
	store := &mock.MockAssistantStore{}
	f := &fixture{store: store, users: users, spaces: spaces, gateway: &fakeGateway{registered: map[string]corechannel.Connector{}}, connector: map[string]*fakeConnector{}}
	f.svc = &Service{
		Store: store, Spaces: spaces, Users: users,
		ServiceAccounts: &spacesvc.Service{Spaces: spaces, Users: users, ServiceAccounts: &mock.MockServiceAccountStore{Users: users, Spaces: spaces}},
		Agents:          agents, Workflows: workflows, Artifacts: artifacts,
		Secrets: fakeSecrets{{ID: "sec_hris", SpaceID: team, Name: "HRIS"}},
		Bots: &Reconciler{Store: store, Gateway: f.gateway, Connect: func(_, token string) (corechannel.Connector, error) {
			c := f.connector[token]
			if c == nil {
				c = &fakeConnector{id: "", err: errors.New("unauthorized")}
			}
			return c, nil
		}},
	}
	return f
}

func hrDefinition() coreassistant.Definition {
	return coreassistant.Definition{
		Name: "HR Assistant", Instructions: "Answer HR questions.", Audience: coreassistant.AudienceSpaceMembers,
		Roster: []coreassistant.RosterEntry{
			{Kind: coreassistant.KindAgent, ID: "agent_hr", OutputSchema: resultSchema, Releasable: []string{"answer"}},
			{Kind: coreassistant.KindWorkflow, ID: "wf_leave", Releasable: []string{"answer"}},
		},
		ReadableFiles: []string{"file_policy"},
	}
}

func TestCreateMakesAServiceAccountAndStartsPaused(t *testing.T) {
	f := newFixture(t)
	v, err := f.svc.Create(context.Background(), CreateCmd{SpaceID: team, ActorID: admin, Def: hrDefinition()})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	a := v.Assistant
	if a.State != coreassistant.StatePaused || v.Availability != coreassistant.PausedByOwner || a.Revision != 1 || a.SponsorUserID != admin {
		t.Errorf("assistant = %+v, availability %s", a, v.Availability)
	}
	sa := f.users.ByID[a.Def.ServiceAccountID]
	if sa == nil || !sa.IsService() || sa.Name != "HR Assistant" {
		t.Fatalf("service account = %+v", sa)
	}
	if role := corespace.EffectiveRoleOf(f.spaces.Members, sa.ID); role != corespace.RoleMember {
		t.Errorf("service account role = %q, want member", role)
	}
}

func TestCreateRefusesWhatTheSpaceDoesNotOwn(t *testing.T) {
	cases := map[string]func(*coreassistant.Definition){
		"agent of another space": func(d *coreassistant.Definition) { d.Roster[0].ID = "agent_elsewhere" },
		"unpublished workflow":   func(d *coreassistant.Definition) { d.Roster[1].ID = "wf_draft" },
		"workflow without a result schema": func(d *coreassistant.Definition) {
			d.Roster[1].ID = "wf_noresult"
		},
		"releasable outside the schema": func(d *coreassistant.Definition) { d.Roster[0].Releasable = []string{"salary"} },
		"agent without output_schema":   func(d *coreassistant.Definition) { d.Roster[0].OutputSchema = nil },
		"file of another space":         func(d *coreassistant.Definition) { d.ReadableFiles = []string{"file_other"} },
		"unknown audience":              func(d *coreassistant.Definition) { d.Audience = "everyone" },
		"someone else as the account":   func(d *coreassistant.Definition) { d.ServiceAccountID = member },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			def := hrDefinition()
			edit(&def)
			if _, err := f.svc.Create(context.Background(), CreateCmd{SpaceID: team, ActorID: owner, Def: def}); err == nil {
				t.Fatal("Create accepted it")
			}
			if len(f.users.ByID) != 3 {
				t.Error("a refused definition left a service account behind")
			}
		})
	}
	f := newFixture(t)
	if _, err := f.svc.Create(context.Background(), CreateCmd{SpaceID: personal, ActorID: owner, Def: hrDefinition()}); !errors.Is(err, ErrPersonalSpace) {
		t.Errorf("personal space: %v", err)
	}
	if _, err := f.svc.Create(context.Background(), CreateCmd{SpaceID: team, ActorID: member, Def: hrDefinition()}); !errors.Is(err, ErrInvalidSponsor) {
		t.Errorf("member as sponsor: %v", err)
	}
}

func TestStatementNamesWhatTheAudienceCanReach(t *testing.T) {
	f := newFixture(t)
	v, err := f.svc.Create(context.Background(), CreateCmd{SpaceID: team, ActorID: owner, Def: hrDefinition()})
	if err != nil {
		t.Fatal(err)
	}
	st := v.Statement
	for _, want := range []string{"Any member of this Space", `"leave-policy.md"`, `Agent "HR Agent", which holds the Secrets "HRIS"`, `Workflow "Leave lookup", whose steps run as "HR Agent"`} {
		if !strings.Contains(st.Text, want) {
			t.Errorf("statement lacks %q: %s", want, st.Text)
		}
	}
	if st.Digest == "" {
		t.Error("no digest")
	}
}

// Publishing is a disclosure decision: it needs the statement the owner saw,
// and so does widening what an active Assistant discloses.
func TestPublishingNeedsTheStatementConfirmed(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	v, _ := f.svc.Create(ctx, CreateCmd{SpaceID: team, ActorID: owner, Def: hrDefinition()})
	id := v.Assistant.ID

	_, err := f.svc.SetState(ctx, SetStateCmd{SpaceID: team, ActorID: owner, AssistantID: id, State: coreassistant.StateActive})
	var stmt *StatementError
	if !errors.As(err, &stmt) || stmt.Statement.Digest != v.Statement.Digest {
		t.Fatalf("activate without confirming = %v", err)
	}
	v, err = f.svc.SetState(ctx, SetStateCmd{SpaceID: team, ActorID: owner, AssistantID: id, State: coreassistant.StateActive, ConfirmStatement: stmt.Statement.Digest})
	if err != nil || v.Availability != coreassistant.Available {
		t.Fatalf("activate = %+v, %v", v, err)
	}

	quiet := hrDefinition()
	quiet.Instructions = "Be brief."
	quiet.ServiceAccountID = v.Assistant.Def.ServiceAccountID
	if v, err = f.svc.Update(ctx, UpdateCmd{SpaceID: team, ActorID: owner, AssistantID: id, Def: &quiet}); err != nil || v.Assistant.Revision != 2 {
		t.Fatalf("a change that discloses nothing new = %+v, %v", v, err)
	}

	wider := quiet
	wider.Audience = coreassistant.AudienceAllUsers
	_, err = f.svc.Update(ctx, UpdateCmd{SpaceID: team, ActorID: owner, AssistantID: id, Def: &wider})
	if !errors.As(err, &stmt) || !strings.Contains(stmt.Statement.Text, "Any active user") {
		t.Fatalf("widening without confirming = %v", err)
	}
	if v, err = f.svc.Update(ctx, UpdateCmd{SpaceID: team, ActorID: owner, AssistantID: id, Def: &wider, ConfirmStatement: stmt.Statement.Digest}); err != nil || v.Assistant.Def.Audience != coreassistant.AudienceAllUsers || v.Assistant.Revision != 3 {
		t.Fatalf("widening confirmed = %+v, %v", v, err)
	}
}

// An Assistant serving a wide audience never runs unowned: a disabled service
// account or a sponsor who is no longer accountable pauses it on its own.
func TestAvailabilityFollowsTheServiceAccountAndSponsor(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	v, _ := f.svc.Create(ctx, CreateCmd{SpaceID: team, ActorID: admin, Def: hrDefinition()})
	v, _ = f.svc.SetState(ctx, SetStateCmd{SpaceID: team, ActorID: admin, AssistantID: v.Assistant.ID, State: coreassistant.StateActive, ConfirmStatement: v.Statement.Digest})
	if v.Availability != coreassistant.Available {
		t.Fatalf("availability = %s", v.Availability)
	}

	for i := range f.spaces.Members {
		if f.spaces.Members[i].UserID == admin {
			f.spaces.Members[i].Role = corespace.RoleMember
		}
	}
	if v, _ = f.svc.Get(ctx, team, v.Assistant.ID); v.Availability != coreassistant.NeedsSponsor {
		t.Errorf("sponsor demoted: availability = %s", v.Availability)
	}
	if _, err := f.svc.Update(ctx, UpdateCmd{SpaceID: team, ActorID: owner, AssistantID: v.Assistant.ID, SponsorUserID: util.Ptr(member)}); !errors.Is(err, ErrInvalidSponsor) {
		t.Errorf("a member as sponsor: %v", err)
	}
	// The service account's sponsor was the admin too; both move to the owner.
	sa := v.Assistant.Def.ServiceAccountID
	f.users.ByID[sa].SponsorUserID = util.Ptr(owner)
	if v, _ = f.svc.Update(ctx, UpdateCmd{SpaceID: team, ActorID: owner, AssistantID: v.Assistant.ID, SponsorUserID: util.Ptr(owner)}); v.Availability != coreassistant.Available {
		t.Errorf("re-sponsored: availability = %s", v.Availability)
	}

	now := time.Now()
	f.users.ByID[sa].DisabledAt = &now
	if v, _ = f.svc.Get(ctx, team, v.Assistant.ID); v.Availability != coreassistant.ServiceAccountDisabled {
		t.Errorf("service account disabled: availability = %s", v.Availability)
	}
}

func TestAnotherSpacesAssistantIsNotFound(t *testing.T) {
	f := newFixture(t)
	v, _ := f.svc.Create(context.Background(), CreateCmd{SpaceID: team, ActorID: owner, Def: hrDefinition()})
	if _, err := f.svc.Get(context.Background(), other, v.Assistant.ID); !errors.Is(err, coreassistant.ErrNotFound) {
		t.Errorf("Get from another space = %v", err)
	}
}

type fakeConnector struct {
	id  string
	err error
}

func (c *fakeConnector) Platform() string { return corechannel.PlatformTelegram }
func (c *fakeConnector) Receive(ctx context.Context, _ func(corechannel.Inbound)) error {
	<-ctx.Done()
	return nil
}
func (c *fakeConnector) Send(context.Context, corechannel.Outbound) error { return nil }
func (c *fakeConnector) Typing(context.Context, string) error            { return nil }
func (c *fakeConnector) Info(context.Context) corechannel.Info {
	return corechannel.Info{Platform: corechannel.PlatformTelegram, BotHandle: "@hr_bot"}
}
func (c *fakeConnector) BotID(context.Context) (string, error) { return c.id, c.err }

type fakeGateway struct {
	mu         sync.Mutex
	registered map[string]corechannel.Connector
	system     string
}

func (g *fakeGateway) Register(ctx context.Context, key string, c corechannel.Connector) error {
	id, err := c.BotID(ctx)
	if err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.registered[key] = c
	_ = id
	return nil
}

func (g *fakeGateway) Unregister(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.registered, key)
}

func (g *fakeGateway) Registered() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []string
	for k := range g.registered {
		out = append(out, k)
	}
	return out
}

func (g *fakeGateway) BotInUse(ctx context.Context, _, botID, except string) (bool, error) {
	if botID == g.system {
		return true, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for k, c := range g.registered {
		if id, _ := c.BotID(ctx); k != except && id == botID {
			return true, nil
		}
	}
	return false, nil
}

func TestBindConnectsTheBotAndUnbindReleasesIt(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.gateway.system = "100"
	f.connector["good"] = &fakeConnector{id: "200"}
	f.connector["system"] = &fakeConnector{id: "100"}
	v, _ := f.svc.Create(ctx, CreateCmd{SpaceID: team, ActorID: owner, Def: hrDefinition()})
	id := v.Assistant.ID

	if _, err := f.svc.Bind(ctx, BindCmd{SpaceID: team, ActorID: owner, AssistantID: id, Platform: "telegram", Token: "revoked"}); !errors.Is(err, coreassistant.ErrInvalidBotToken) {
		t.Errorf("rejected token: %v", err)
	}
	if _, err := f.svc.Bind(ctx, BindCmd{SpaceID: team, ActorID: owner, AssistantID: id, Platform: "telegram", Token: "system"}); !errors.Is(err, corechannel.ErrBotInUse) {
		t.Errorf("the system bot's token: %v", err)
	}
	if _, err := f.svc.Bind(ctx, BindCmd{SpaceID: team, ActorID: owner, AssistantID: id, Platform: "slack", Token: "good"}); !errors.Is(err, coreassistant.ErrUnsupportedPlatform) {
		t.Errorf("unsupported platform: %v", err)
	}
	v, err := f.svc.Bind(ctx, BindCmd{SpaceID: team, ActorID: owner, AssistantID: id, Platform: "telegram", Token: "good"})
	if err != nil || v.Binding == nil || v.Binding.BotHandle != "@hr_bot" {
		t.Fatalf("Bind = %+v, %v", v, err)
	}
	if got := f.gateway.Registered(); len(got) != 1 || got[0] != v.Binding.ID {
		t.Errorf("registered = %v, want the binding", got)
	}
	if _, err := f.svc.Bind(ctx, BindCmd{SpaceID: team, ActorID: owner, AssistantID: id, Platform: "telegram", Token: "good"}); !errors.Is(err, coreassistant.ErrAlreadyBound) {
		t.Errorf("second bind: %v", err)
	}

	if _, err := f.svc.Unbind(ctx, team, owner, id); err != nil {
		t.Fatal(err)
	}
	if got := f.gateway.Registered(); len(got) != 0 {
		t.Errorf("registered after unbind = %v", got)
	}
}

// A replica that did not make the change catches up on its next pass, and a
// deleted Assistant's bot goes away.
func TestSyncFollowsTheStoredBindings(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.connector["good"] = &fakeConnector{id: "200"}
	v, _ := f.svc.Create(ctx, CreateCmd{SpaceID: team, ActorID: owner, Def: hrDefinition()})
	if _, err := f.svc.Bind(ctx, BindCmd{SpaceID: team, ActorID: owner, AssistantID: v.Assistant.ID, Platform: "telegram", Token: "good"}); err != nil {
		t.Fatal(err)
	}
	elsewhere := &fakeGateway{registered: map[string]corechannel.Connector{"stale": &fakeConnector{id: "9"}}}
	(&Reconciler{Store: f.store, Gateway: elsewhere, Connect: f.svc.Bots.Connect}).Sync(ctx)
	if got := elsewhere.Registered(); len(got) != 1 || got[0] == "stale" {
		t.Errorf("other replica registered = %v", got)
	}
	if err := f.svc.Delete(ctx, team, owner, v.Assistant.ID); err != nil {
		t.Fatal(err)
	}
	f.svc.Bots.Sync(ctx)
	if got := f.gateway.Registered(); len(got) != 0 {
		t.Errorf("registered after delete = %v", got)
	}
}

func TestFrontDoorAnswersOnlyWhileAvailable(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.connector["good"] = &fakeConnector{id: "200"}
	v, _ := f.svc.Create(ctx, CreateCmd{SpaceID: team, ActorID: owner, Def: hrDefinition()})
	v, _ = f.svc.Bind(ctx, BindCmd{SpaceID: team, ActorID: owner, AssistantID: v.Assistant.ID, Platform: "telegram", Token: "good"})
	door := FrontDoor{Service: f.svc}

	if got := door.Answer(ctx, v.Binding.ID, corechannel.Inbound{Text: "hi"}, member); !strings.Contains(got, "paused") {
		t.Errorf("paused answer = %q", got)
	}
	if _, err := f.svc.SetState(ctx, SetStateCmd{SpaceID: team, ActorID: owner, AssistantID: v.Assistant.ID, State: coreassistant.StateActive, ConfirmStatement: v.Statement.Digest}); err != nil {
		t.Fatal(err)
	}
	if got := door.Answer(ctx, v.Binding.ID, corechannel.Inbound{Text: "hi"}, member); !strings.Contains(got, "not answering questions yet") {
		t.Errorf("active answer = %q", got)
	}
	if got := door.Answer(ctx, "unknown", corechannel.Inbound{Text: "hi"}, member); got != "This assistant is not available." {
		t.Errorf("unknown bot answer = %q", got)
	}
}
