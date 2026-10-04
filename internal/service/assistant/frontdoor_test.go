package assistant

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreassistant "github.com/icloudbb/buildmax/internal/core/assistant"
	corechannel "github.com/icloudbb/buildmax/internal/core/channel"
	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"
	"github.com/icloudbb/buildmax/internal/core/eligibility"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/service/conversation"
	issuesvc "github.com/icloudbb/buildmax/internal/service/issue"
)

const outsider = "user_outsider"

// fakeConversations keys an Assistant's conversations the way the store does:
// Assistant, requester, platform, and chat, newest first.
type fakeConversations struct {
	list []coreconv.Conversation
}

func (c *fakeConversations) GetConversation(_ context.Context, id string) (*coreconv.Conversation, error) {
	for i := range c.list {
		if c.list[i].ID == id {
			v := c.list[i]
			return &v, nil
		}
	}
	return nil, nil
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
		p.Instructions != "Answer HR questions." || len(p.Roster) != 2 || p.Roster[0].ID != "agent_hr" || p.Roster[1].ID != "wf_leave" || p.Roster[0].Releasable[0] != "answer" ||
		len(p.ReadableFiles) != 1 || p.ReadableFiles[0] != "file_policy" {
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

type fakeTasks map[string]*coretask.Task

func (f fakeTasks) GetTask(_ context.Context, id string) (*coretask.Task, error) { return f[id], nil }

// An outcome report carries only the releasable fields, through the
// Assistant's current bot; a failure is a fixed sentence; nothing is sent
// while the Assistant would not answer the requester.
func TestFrontDoorOutcomeReleasesOnlyContractedFields(t *testing.T) {
	f := newFrontDoorFixture(t, coreassistant.AudienceSpaceMembers)
	agent := "agent_hr"
	f.door.Tasks = fakeTasks{"tk": {ID: "tk", Title: "Leave balance", AgentID: &agent}}
	ctx := context.Background()
	f.ask(t, member, "c1", "how many days?")
	conv := &f.convs.list[0]
	structured := `{"answer":"15 days","raw":"SECRET_RAW"}`
	output, errText := "SECRET_OUTPUT", "SECRET_ERROR"
	info := coretask.RunTerminalInfo{TaskID: "tk", ConversationID: conv.ID, Status: string(coretask.RunStatusSucceeded), Structured: &structured, Output: &output}

	key, text, ok := f.door.Outcome(ctx, conv, info)
	if !ok || key != f.view.Binding.ID || text != "“Leave balance” is done.\n\nanswer: 15 days" {
		t.Errorf("success = %q, %q, %v", key, text, ok)
	}
	info.Status, info.ErrorMessage = string(coretask.RunStatusFailed), &errText
	if _, text, _ := f.door.Outcome(ctx, conv, info); text != "“Leave balance” could not be completed." {
		t.Errorf("failure = %q", text)
	}
	info.Status = string(coretask.RunStatusCanceled)
	if _, text, _ := f.door.Outcome(ctx, conv, info); strings.Contains(text, "SECRET") || !strings.Contains(text, "stopped") {
		t.Errorf("cancel = %q", text)
	}

	// The requester left the Space: no report.
	f.spaces.Members = slices.DeleteFunc(f.spaces.Members, func(m corespace.Member) bool { return m.UserID == member })
	if _, _, ok := f.door.Outcome(ctx, conv, info); ok {
		t.Error("reported to a requester outside the audience")
	}
}

// fakeIssues serves escalated Issues and keeps the comments recorded on them.
type fakeIssues struct {
	issues   map[string]*coreissue.Issue
	comments []issuesvc.CreateCommentCmd
}

func (f *fakeIssues) GetIssue(_ context.Context, spaceID, id string) (*coreissue.Issue, error) {
	if i := f.issues[id]; i != nil && i.SpaceID == spaceID {
		return i, nil
	}
	return nil, issuesvc.ErrIssueNotFound
}

func (f *fakeIssues) CreateComment(_ context.Context, cmd issuesvc.CreateCommentCmd) (*coreissue.Comment, error) {
	f.comments = append(f.comments, cmd)
	return &coreissue.Comment{ID: fmt.Sprintf("c%d", len(f.comments)), IssueID: cmd.IssueID, AuthorKind: cmd.AuthorKind, AuthorID: cmd.AuthorID, Body: cmd.Body}, nil
}

// A member's reply leaves through the Assistant's bot into the requester's
// chat and is recorded on the Issue as their comment; nothing is sent or
// recorded while the Assistant would not answer that requester itself.
func TestReplyToRequesterSendsThroughTheBotAndRecordsIt(t *testing.T) {
	f := newFrontDoorFixture(t, coreassistant.AudienceSpaceMembers)
	ctx := context.Background()
	f.ask(t, member, "chat-77", "can someone check my payslip?")
	conv := f.convs.list[0]
	issues := &fakeIssues{issues: map[string]*coreissue.Issue{
		"iss_esc":   {ID: "iss_esc", SpaceID: team, ConversationID: conv.ID},
		"iss_plain": {ID: "iss_plain", SpaceID: team},
	}}
	f.door.Issues = issues
	reply := func(issue, text string) (*coreissue.Comment, error) {
		return f.door.ReplyToRequester(ctx, ReplyCmd{SpaceID: team, ActorID: admin, IssueID: issue, Text: text})
	}

	c, err := reply("iss_esc", "  Your payslip is fixed.  ")
	if err != nil {
		t.Fatalf("ReplyToRequester: %v", err)
	}
	want := "telegram|" + f.view.Binding.ID + "|chat-77|Your payslip is fixed."
	if len(f.gateway.sent) != 1 || f.gateway.sent[0] != want {
		t.Errorf("sent = %v, want %q", f.gateway.sent, want)
	}
	if c.AuthorID != admin || !strings.Contains(c.Body, "HR Assistant") || !strings.HasSuffix(c.Body, "Your payslip is fixed.") {
		t.Errorf("comment = %+v", c)
	}

	// In order: each case's setup stays in place for the ones after it.
	for _, tc := range []struct {
		name, issue, text string
		setup             func()
		want              error
	}{
		{name: "not escalated", issue: "iss_plain", text: "hi", want: ErrNotEscalated},
		{name: "empty", issue: "iss_esc", text: "  ", want: ErrReplyRequired},
		{name: "too long", issue: "iss_esc", text: strings.Repeat("x", maxReplyRunes+1), want: ErrReplyTooLong},
		{name: "other space", issue: "iss_missing", text: "hi", want: issuesvc.ErrIssueNotFound},
		{name: "send fails", issue: "iss_esc", text: "hi", setup: func() { f.gateway.sendErr = fmt.Errorf("telegram down") }},
		{name: "requester left", issue: "iss_esc", text: "hi", want: ErrRequesterGone, setup: func() {
			f.gateway.sendErr = nil
			f.spaces.Members = slices.DeleteFunc(f.spaces.Members, func(m corespace.Member) bool { return m.UserID == member })
		}},
		{name: "paused", issue: "iss_esc", text: "hi", want: ErrAssistantNotAnswer, setup: func() {
			if _, err := f.svc.SetState(ctx, SetStateCmd{SpaceID: team, ActorID: owner, AssistantID: f.view.Assistant.ID, State: coreassistant.StatePaused}); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		if tc.setup != nil {
			tc.setup()
		}
		before := len(issues.comments)
		_, err := reply(tc.issue, tc.text)
		if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
		if len(issues.comments) != before {
			t.Errorf("%s: recorded a comment for a reply that was not sent", tc.name)
		}
	}
	if len(f.gateway.sent) != 1 {
		t.Errorf("sent %d messages, want only the first", len(f.gateway.sent))
	}
}
