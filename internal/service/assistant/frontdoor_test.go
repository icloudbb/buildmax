package assistant

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreassistant "github.com/icloudbb/buildmax/internal/core/assistant"
	corechannel "github.com/icloudbb/buildmax/internal/core/channel"
	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"
	"github.com/icloudbb/buildmax/internal/core/eligibility"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	"github.com/icloudbb/buildmax/internal/service/conversation"
)

const outsider = "user_outsider"

// fakeConversations keys an Assistant's conversations the way the store does:
// Assistant, requester, platform, and chat, newest first.
type fakeConversations struct {
	list []coreconv.Conversation
}

func (c *fakeConversations) LatestAssistantConversation(_ context.Context, assistantID, userID, channel, chatID string) (*coreconv.Conversation, error) {
	for i := len(c.list) - 1; i >= 0; i-- {
		v := c.list[i]
		if v.AssistantID == assistantID && v.UserID == userID && v.Channel == channel && v.ChannelRef == chatID {
			return &v, nil
		}
	}
	return nil, nil
}

func (c *fakeConversations) CreateAssistantConversation(_ context.Context, assistantID, spaceID, userID, channel, connector, chatID string) (*coreconv.Conversation, error) {
	v := coreconv.Conversation{
		ID: fmt.Sprintf("conv%d", len(c.list)+1), AssistantID: assistantID, SpaceID: spaceID, UserID: userID,
		Channel: channel, ChannelConnector: connector, ChannelRef: chatID,
	}
	c.list = append(c.list, v)
	return &v, nil
}

type turnCall struct {
	conversationID, requesterID, message string
	profile                              conversation.AssistantTurn
}

// fakeTurns stands where the model runs: a refusal must never reach it.
type fakeTurns struct {
	calls []turnCall
	err   error
}

func (t *fakeTurns) RunAssistantTurn(_ context.Context, conversationID, requesterID, _, message string, a conversation.AssistantTurn) (string, error) {
	t.calls = append(t.calls, turnCall{conversationID, requesterID, message, a})
	if t.err != nil {
		return "", t.err
	}
	return "answered", nil
}

type frontDoorFixture struct {
	*fixture
	door  *FrontDoor
	turns *fakeTurns
	convs *fakeConversations
	view  *View
}

// newFrontDoorFixture binds and publishes the HR assistant with the given
// audience. outsider is an active account in no Space.
func newFrontDoorFixture(t *testing.T, audience string) *frontDoorFixture {
	t.Helper()
	ctx := context.Background()
	f := newFixture(t)
	f.users.ByID[outsider] = &coreidentity.User{ID: outsider, Name: outsider, Kind: coreidentity.KindHuman}
	f.connector["good"] = &fakeConnector{id: "200"}
	def := hrDefinition()
	def.Audience = audience
	def.Description = "Answers leave questions."
	v, err := f.svc.Create(ctx, CreateCmd{SpaceID: team, ActorID: owner, Def: def})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Bind(ctx, BindCmd{SpaceID: team, ActorID: owner, AssistantID: v.Assistant.ID, Platform: "telegram", Token: "good"}); err != nil {
		t.Fatal(err)
	}
	if v, err = f.svc.SetState(ctx, SetStateCmd{SpaceID: team, ActorID: owner, AssistantID: v.Assistant.ID, State: coreassistant.StateActive, ConfirmStatement: v.Statement.Digest}); err != nil {
		t.Fatal(err)
	}
	ff := &frontDoorFixture{fixture: f, turns: &fakeTurns{}, convs: &fakeConversations{}, view: v}
	ff.door = &FrontDoor{Service: f.svc, Conversations: ff.convs, Eligibility: eligibility.New(f.users, f.spaces), Turns: ff.turns}
	return ff
}

func (f *frontDoorFixture) ask(t *testing.T, from, chat, text string) string {
	t.Helper()
	return f.door.Answer(context.Background(), f.view.Binding.ID, corechannel.Inbound{ChatID: chat, ChatType: corechannel.ChatPrivate, Text: text}, from)
}

// Every refusal happens before the turn, where the model runs, and carries no
// Space data.
func TestFrontDoorRefusesBeforeAnyModelCall(t *testing.T) {
	disable := func(f *frontDoorFixture, id string) {
		now := time.Now()
		f.users.ByID[id].DisabledAt = &now
	}
	tests := []struct {
		name     string
		audience string
		from     string
		setup    func(*frontDoorFixture)
		key      string
		want     string
	}{
		{name: "unknown bot", audience: coreassistant.AudienceSpaceMembers, from: member, key: "unknown", want: "not available"},
		{name: "paused", audience: coreassistant.AudienceSpaceMembers, from: member, want: "paused", setup: func(f *frontDoorFixture) {
			if _, err := f.svc.SetState(context.Background(), SetStateCmd{SpaceID: team, ActorID: owner, AssistantID: f.view.Assistant.ID, State: coreassistant.StatePaused}); err != nil {
				panic(err)
			}
		}},
		{name: "service account disabled", audience: coreassistant.AudienceSpaceMembers, from: member, want: "paused", setup: func(f *frontDoorFixture) {
			disable(f, f.view.Assistant.Def.ServiceAccountID)
		}},
		{name: "requester disabled", audience: coreassistant.AudienceSpaceMembers, from: member, want: "disabled", setup: func(f *frontDoorFixture) {
			disable(f, member)
		}},
		{name: "requester outside a members-only audience", audience: coreassistant.AudienceSpaceMembers, from: outsider, want: "only answers members"},
		{name: "requester disabled, every user may ask", audience: coreassistant.AudienceAllUsers, from: outsider, want: "disabled", setup: func(f *frontDoorFixture) {
			disable(f, outsider)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFrontDoorFixture(t, tc.audience)
			if tc.setup != nil {
				tc.setup(f)
			}
			key := f.view.Binding.ID
			if tc.key != "" {
				key = tc.key
			}
			got := f.door.Answer(context.Background(), key, corechannel.Inbound{ChatID: "c1", Text: "how many leave days do I have?"}, tc.from)
			if !strings.Contains(got, tc.want) {
				t.Errorf("answer = %q, want it to contain %q", got, tc.want)
			}
			if strings.Contains(got, "HR") {
				t.Errorf("refusal %q names the Space", got)
			}
			if len(f.turns.calls) != 0 || len(f.convs.list) != 0 {
				t.Errorf("refusal reached a turn (%d) or stored a conversation (%d)", len(f.turns.calls), len(f.convs.list))
			}
		})
	}
}

// A person outside the Space may ask an Assistant every user may ask; the turn
// runs in the Assistant's Space as its service account, with the requester as
// the turn's user and the roster as its reach.
func TestFrontDoorRunsTheTurnAsTheAssistant(t *testing.T) {
	f := newFrontDoorFixture(t, coreassistant.AudienceAllUsers)
	if got := f.ask(t, outsider, "c1", "how many leave days do I have?"); got != "answered" {
		t.Fatalf("answer = %q", got)
	}
	if len(f.turns.calls) != 1 || len(f.convs.list) != 1 {
		t.Fatalf("turns = %d, conversations = %d", len(f.turns.calls), len(f.convs.list))
	}
	conv, call := f.convs.list[0], f.turns.calls[0]
	a := f.view.Assistant
	if conv.SpaceID != team || conv.UserID != outsider || conv.AssistantID != a.ID || conv.ChannelConnector != f.view.Binding.ID {
		t.Errorf("conversation = %+v", conv)
	}
	p := call.profile
	if call.requesterID != outsider || p.ActingUserID != a.Def.ServiceAccountID || p.ID != a.ID || p.Revision != a.Revision ||
		p.Instructions != "Answer HR questions." || len(p.Agents) != 1 || p.Agents[0] != "agent_hr" || len(p.Workflows) != 1 || p.Workflows[0] != "wf_leave" {
		t.Errorf("turn = %+v", call)
	}

	// The same chat continues the conversation; another chat, or /new, starts one.
	f.ask(t, outsider, "c1", "and next year?")
	f.ask(t, outsider, "c2", "hello")
	if f.turns.calls[1].conversationID != conv.ID || f.turns.calls[2].conversationID == conv.ID {
		t.Errorf("conversations used = %s, %s", f.turns.calls[1].conversationID, f.turns.calls[2].conversationID)
	}
	if got := f.ask(t, outsider, "c1", "/new"); !strings.Contains(got, "new conversation") {
		t.Errorf("/new = %q", got)
	}
	f.ask(t, outsider, "c1", "start over")
	if last := f.turns.calls[len(f.turns.calls)-1]; last.conversationID == conv.ID {
		t.Error("/new did not start a new conversation")
	}
}

// Only /new and /help exist; /help names the Assistant and its Space without
// a turn, and /space is not a command here.
func TestFrontDoorCommands(t *testing.T) {
	f := newFrontDoorFixture(t, coreassistant.AudienceSpaceMembers)
	help := f.ask(t, member, "c1", "/help@hr_bot")
	for _, want := range []string{"HR Assistant", "Answers leave questions.", `"HR"`, "review", "/new"} {
		if !strings.Contains(help, want) {
			t.Errorf("/help = %q, want %q in it", help, want)
		}
	}
	if got := f.ask(t, member, "c1", "/space 2"); !strings.HasPrefix(got, "Unknown command") || strings.Contains(got, "/space") {
		t.Errorf("/space = %q", got)
	}
	if len(f.turns.calls) != 0 {
		t.Errorf("a command ran %d turns", len(f.turns.calls))
	}
}

// A Space out of quota is a fixed reply; any other failure stays in the log.
func TestFrontDoorTurnFailures(t *testing.T) {
	f := newFrontDoorFixture(t, coreassistant.AudienceSpaceMembers)
	f.turns.err = apierr.New(apierr.KindQuotaExceeded, "space HR token quota exhausted")
	if got := f.ask(t, member, "c1", "hi"); !strings.Contains(got, "usage limit") || strings.Contains(got, "HR") {
		t.Errorf("quota answer = %q", got)
	}
	f.turns.err = fmt.Errorf("upstream said: secret internal detail")
	if got := f.ask(t, member, "c1", "hi"); strings.Contains(got, "secret") || strings.Contains(got, "http") {
		t.Errorf("failure answer = %q", got)
	}
}
