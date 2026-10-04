package assistant

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreassistant "github.com/icloudbb/buildmax/internal/core/assistant"
	corechannel "github.com/icloudbb/buildmax/internal/core/channel"
	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"
	"github.com/icloudbb/buildmax/internal/core/eligibility"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	chansvc "github.com/icloudbb/buildmax/internal/service/channel"
	"github.com/icloudbb/buildmax/internal/service/conversation"
)

const (
	genericFailure = "Something went wrong on the BuildMax side. Please try again in a moment."
	notAvailable   = "This assistant is not available."
)

// Conversations finds and starts an Assistant's conversations: one requester
// in one chat, newest first.
type Conversations interface {
	GetConversation(ctx context.Context, conversationID string) (*coreconv.Conversation, error)
	LatestAssistantConversation(ctx context.Context, assistantID, userID, channel, channelRef string) (*coreconv.Conversation, error)
	CreateAssistantConversation(ctx context.Context, assistantID, spaceID, userID, channel, connector, channelRef string) (*coreconv.Conversation, error)
}

// Turns runs one Assistant turn through the same turn queue as every other
// conversation turn.
type Turns interface {
	RunAssistantTurn(ctx context.Context, conversationID, requesterID, channel, message string, a conversation.AssistantTurn) (string, error)
}

// Tasks reads the Task a finished run belongs to, for its title and Agent.
type Tasks interface {
	GetTask(ctx context.Context, taskID string) (*coretask.Task, error)
}

// FrontDoor answers linked people who message an Assistant's bot. It is the
// chat Gateway's channel.FrontDoor. See docs/design/space-assistants.md §10.
type FrontDoor struct {
	Service       *Service
	Conversations Conversations
	Tasks         Tasks
	// Issues backs a member's reply to an escalated Issue's requester.
	Issues Issues
	// Eligibility decides whether a requester may use a Space for an
	// Assistant whose audience is its members.
	Eligibility eligibility.Checker
	Turns       Turns
	Log         *slog.Logger
}

func (f *FrontDoor) log() *slog.Logger {
	if f.Log != nil {
		return f.Log
	}
	return slog.Default()
}

// Answer implements channel.FrontDoor. The Gateway has taken private chats
// only, identified the sender, and checked their sign-in. Everything else that
// decides whether this Assistant answers this person happens here, before any
// model runs, and each refusal is a fixed reply carrying no Space data.
func (f *FrontDoor) Answer(ctx context.Context, connectorKey string, in corechannel.Inbound, requesterID string) string {
	if f == nil || f.Service.ready() != nil || f.Conversations == nil {
		return notAvailable
	}
	s := f.Service
	b, err := s.Store.GetBinding(ctx, connectorKey)
	if err != nil {
		f.log().Error("assistant binding lookup failed", "binding_id", connectorKey, "err", err)
		return genericFailure
	}
	if b == nil {
		return notAvailable
	}
	a, err := s.Store.GetAssistant(ctx, b.AssistantID)
	if err != nil {
		f.log().Error("assistant lookup failed", "assistant_id", b.AssistantID, "err", err)
		return genericFailure
	}
	if a == nil {
		return notAvailable
	}
	avail, err := s.Availability(ctx, a)
	if err != nil {
		f.log().Error("assistant availability check failed", "assistant_id", a.ID, "err", err)
		return genericFailure
	}
	if avail != coreassistant.Available {
		return "This assistant is paused. Please try again later."
	}
	if refusal := f.checkRequester(ctx, a, requesterID); refusal != "" {
		return refusal
	}
	if cmd, ok := parseCommand(in.Text); ok {
		return f.command(ctx, a, b, in, requesterID, cmd)
	}
	conv, err := f.Conversations.LatestAssistantConversation(ctx, a.ID, requesterID, b.Platform, in.ChatID)
	if err == nil && conv == nil {
		conv, err = f.Conversations.CreateAssistantConversation(ctx, a.ID, a.SpaceID, requesterID, b.Platform, b.ID, in.ChatID)
	}
	if err != nil {
		f.log().Error("assistant conversation not found or created", "assistant_id", a.ID, "err", err)
		return genericFailure
	}
	if f.Turns == nil {
		return notAvailable
	}
	reply, err := f.Turns.RunAssistantTurn(ctx, conv.ID, requesterID, b.Platform, in.Text, turnProfile(a))
	if err != nil {
		return f.turnFailure(a, conv, err)
	}
	return reply
}

// checkRequester refuses a disabled account, and a requester outside the
// Space when only its members may ask.
func (f *FrontDoor) checkRequester(ctx context.Context, a *coreassistant.Assistant, requesterID string) string {
	if a.Def.Audience == coreassistant.AudienceAllUsers {
		u, err := f.Service.Users.GetUser(ctx, requesterID)
		if err != nil {
			f.log().Error("requester lookup failed", "err", err)
			return genericFailure
		}
		if u == nil || u.Disabled() {
			return "Your BuildMax account is disabled."
		}
		return ""
	}
	if f.Eligibility == nil {
		return notAvailable
	}
	err := f.Eligibility.Check(ctx, requesterID, a.SpaceID)
	switch {
	case err == nil:
		return ""
	case errors.Is(err, eligibility.ErrAccountDisabled):
		return "Your BuildMax account is disabled."
	case errors.Is(err, eligibility.ErrNotSpaceMember):
		return "This assistant only answers members of the Space that runs it."
	default:
		f.log().Warn("requester eligibility check failed", "err", err)
		return genericFailure
	}
}

const assistantCommands = `/new — start a new conversation
/help — show this help`

func (f *FrontDoor) command(ctx context.Context, a *coreassistant.Assistant, b *coreassistant.Binding, in corechannel.Inbound, requesterID, cmd string) string {
	switch cmd {
	case "start", "help":
		return f.helpText(ctx, a)
	case "new":
		if _, err := f.Conversations.CreateAssistantConversation(ctx, a.ID, a.SpaceID, requesterID, b.Platform, b.ID, in.ChatID); err != nil {
			f.log().Error("assistant conversation not created", "assistant_id", a.ID, "err", err)
			return genericFailure
		}
		return "Started a new conversation."
	default:
		return "Unknown command.\n\n" + assistantCommands
	}
}

func (f *FrontDoor) helpText(ctx context.Context, a *coreassistant.Assistant) string {
	var b strings.Builder
	b.WriteString(a.Def.Name)
	if d := strings.TrimSpace(a.Def.Description); d != "" {
		b.WriteString("\n\n" + d)
	}
	if sp, err := f.Service.Spaces.GetSpace(ctx, a.SpaceID); err == nil && sp != nil {
		fmt.Fprintf(&b, "\n\nOperated by the Space %q, whose people can review your conversations with this assistant.", sp.Name)
	}
	b.WriteString("\n\n" + assistantCommands)
	return b.String()
}

func (f *FrontDoor) turnFailure(a *coreassistant.Assistant, conv *coreconv.Conversation, err error) string {
	switch {
	case errors.Is(err, chansvc.ErrBusy):
		return "I'm still working through your earlier messages. Send this again once I've answered them."
	case errors.Is(err, chansvc.ErrRestarting):
		return "BuildMax is restarting. Please send that again in a moment."
	case errors.Is(err, chansvc.ErrUnavailable):
		return "BuildMax cannot take messages right now. Please send that again in a moment."
	}
	var public *apierr.Error
	if errors.As(err, &public) && public.Kind() == apierr.KindQuotaExceeded {
		return "This assistant has reached its usage limit for now. Please try again later."
	}
	// Anything else may carry internals: it stays in the log, and the requester,
	// who may not belong to the Space, gets no link into it.
	f.log().Error("assistant turn failed", "assistant_id", a.ID, "conversation_id", conv.ID, "err", err)
	return genericFailure
}

// Outcome implements channel.FrontDoor: it tells a requester that work their
// conversation started has ended, through the Assistant's current bot. A
// success carries only the result's releasable fields; a failure or a
// cancellation is a fixed sentence, never error text or a link. Nothing is
// sent while the Assistant would not answer this requester.
func (f *FrontDoor) Outcome(ctx context.Context, conv *coreconv.Conversation, info coretask.RunTerminalInfo) (string, string, bool) {
	if f == nil || f.Service.ready() != nil || conv.AssistantID == "" {
		return "", "", false
	}
	s := f.Service
	a, err := s.Store.GetAssistant(ctx, conv.AssistantID)
	if err != nil || a == nil {
		return "", "", false
	}
	if avail, err := s.Availability(ctx, a); err != nil || avail != coreassistant.Available {
		return "", "", false
	}
	if f.checkRequester(ctx, a, conv.UserID) != "" {
		return "", "", false
	}
	b, err := s.Store.GetBindingByAssistant(ctx, a.ID)
	if err != nil || b == nil || b.Platform != conv.Channel {
		return "", "", false
	}
	var t *coretask.Task
	if f.Tasks != nil {
		t, _ = f.Tasks.GetTask(ctx, info.TaskID)
	}
	return b.ID, outcomeText(a, t, info), true
}

func outcomeText(a *coreassistant.Assistant, t *coretask.Task, info coretask.RunTerminalInfo) string {
	name := "The work you asked for"
	if t != nil && t.Title != "" {
		name = fmt.Sprintf("“%s”", t.Title)
	}
	switch coretask.RunStatus(info.Status) {
	case coretask.RunStatusSucceeded:
		if info.AwaitingAnswer {
			return name + " needs more information from the Space before it can finish. Someone there may follow up."
		}
		var entry *coreassistant.RosterEntry
		if t != nil && t.AgentID != nil {
			entry = a.Def.Entry(coreassistant.KindAgent, *t.AgentID)
		}
		if fields := coreassistant.Release(info.Structured, entry); len(fields) > 0 {
			return name + " is done.\n\n" + coreassistant.FormatReleased(fields)
		}
		return name + " is done. There is nothing from it this assistant may share."
	case coretask.RunStatusCanceled:
		return name + " was stopped."
	default:
		return name + " could not be completed."
	}
}

// turnProfile is the conversation service's view of the Assistant's current
// revision.
func turnProfile(a *coreassistant.Assistant) conversation.AssistantTurn {
	return conversation.AssistantTurn{
		ID: a.ID, Revision: a.Revision, Name: a.Def.Name, Instructions: a.Def.Instructions,
		Model: a.Def.Model, ActingUserID: a.Def.ServiceAccountID, Roster: a.Def.Roster,
		ReadableFiles: a.Def.ReadableFiles,
	}
}

// parseCommand reads "/new@SomeBot" as "new". Telegram appends the bot's name
// to a command picked from its menu in some clients.
func parseCommand(text string) (string, bool) {
	if !strings.HasPrefix(text, "/") {
		return "", false
	}
	head, _, _ := strings.Cut(text[1:], " ")
	head, _, _ = strings.Cut(head, "@")
	return strings.ToLower(head), true
}
