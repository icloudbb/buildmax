package channel

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	corechannel "github.com/icloudbb/buildmax/internal/core/channel"
	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"
	"github.com/icloudbb/buildmax/internal/core/eligibility"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
)

const genericFailure = "Something went wrong on the BuildMax side. Please try again in a moment."

// handle answers one message. Every refusal happens before a model runs: an
// unlinked sender, a group chat, or a sender who may not use the Space gets a
// fixed reply that carries no Space data.
func (g *Gateway) handle(ctx context.Context, c corechannel.Connector, in corechannel.Inbound) {
	// Group chats are not served: every reply there is visible to people who
	// may not belong to the Space, and binding a group is a disclosure decision
	// this version does not offer.
	if in.ChatType != corechannel.ChatPrivate {
		return
	}
	ident, err := g.identities.IdentityByExternal(ctx, c.Platform(), in.Tenant, in.SenderID)
	if err != nil {
		g.log.Error("chat identity lookup failed", "platform", c.Platform(), "err", err)
		g.reply(ctx, c, in.ChatID, genericFailure)
		return
	}
	if ident == nil {
		g.offerPairing(ctx, c, in)
		return
	}
	if in.Unsupported {
		g.reply(ctx, c, in.ChatID, "I can only read text messages for now.")
		return
	}
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return
	}
	if cmd, arg, ok := parseCommand(text); ok {
		g.command(ctx, c, in, ident, cmd, arg)
		return
	}
	g.converse(ctx, c, in, ident, text)
}

// parseCommand splits "/space@SomeBot 2" into ("space", "2"). Telegram appends
// the bot's name to a command picked from its menu in some clients.
func parseCommand(text string) (string, string, bool) {
	if !strings.HasPrefix(text, "/") {
		return "", "", false
	}
	head, arg, _ := strings.Cut(text[1:], " ")
	head, _, _ = strings.Cut(head, "@")
	return strings.ToLower(head), strings.TrimSpace(arg), true
}

func (g *Gateway) converse(ctx context.Context, c corechannel.Connector, in corechannel.Inbound, ident *corechannel.Identity, text string) {
	conv, refusal := g.currentConversation(ctx, c.Platform(), in.ChatID, ident.UserID)
	if refusal != "" {
		g.reply(ctx, c, in.ChatID, refusal)
		return
	}
	turns := g.turnRunner()
	if turns == nil {
		g.reply(ctx, c, in.ChatID, "The BuildMax assistant is not available on this server.")
		return
	}
	stop := g.typing(ctx, c, in.ChatID)
	reply, err := turns.RunChannelTurn(ctx, conv.ID, ident.UserID, c.Platform(), text)
	stop()
	if err != nil {
		g.reply(ctx, c, in.ChatID, g.turnFailure(conv, err))
		return
	}
	g.reply(ctx, c, in.ChatID, reply)
}

// currentConversation returns the chat's newest conversation, or starts one in
// the sender's personal Space, having checked the sender may use its Space. A
// non-empty string is the refusal to send instead.
func (g *Gateway) currentConversation(ctx context.Context, platform, chatID, userID string) (*coreconv.Conversation, string) {
	conv, err := g.conversations.LatestChatConversation(ctx, userID, platform, chatID)
	if err != nil {
		g.log.Error("chat conversation lookup failed", "platform", platform, "err", err)
		return nil, genericFailure
	}
	if conv != nil {
		if refusal := g.checkEligible(ctx, userID, conv.SpaceID, ""); refusal != "" {
			return nil, refusal
		}
		return conv, ""
	}
	space, err := g.personalSpace(ctx, userID)
	if err != nil || space == nil {
		if err != nil {
			g.log.Error("personal space lookup failed", "err", err)
		}
		return nil, "You have no Space to talk in yet. Use /space to choose one."
	}
	return g.startConversation(ctx, platform, chatID, userID, space)
}

func (g *Gateway) startConversation(ctx context.Context, platform, chatID, userID string, space *corespace.Space) (*coreconv.Conversation, string) {
	if refusal := g.checkEligible(ctx, userID, space.ID, space.Name); refusal != "" {
		return nil, refusal
	}
	conv, err := g.conversations.CreateChatConversation(ctx, space.ID, userID, platform, chatID)
	if err != nil {
		g.log.Error("chat conversation not created", "platform", platform, "err", err)
		return nil, genericFailure
	}
	return conv, ""
}

// checkEligible asks the one authority on whether a user may work in a Space
// right now. It runs on every message, so a disabled account or a removed
// member stops being served at once, with the link still in place.
func (g *Gateway) checkEligible(ctx context.Context, userID, spaceID, spaceName string) string {
	if g.eligible == nil {
		return ""
	}
	err := g.eligible.Check(ctx, userID, spaceID)
	switch {
	case err == nil:
		return ""
	case errors.Is(err, eligibility.ErrAccountDisabled):
		return "Your BuildMax account is disabled."
	case errors.Is(err, eligibility.ErrNotSpaceMember):
		if spaceName == "" {
			spaceName = "the Space this chat was using"
		}
		return fmt.Sprintf("You are no longer a member of %s. Use /space to choose another.", spaceName)
	default:
		g.log.Warn("eligibility check failed", "err", err)
		return genericFailure
	}
}

func (g *Gateway) turnFailure(conv *coreconv.Conversation, err error) string {
	switch {
	case errors.Is(err, ErrBusy):
		return "I'm still working through your earlier messages. Send this again once I've answered them."
	case errors.Is(err, ErrRestarting):
		return "BuildMax is restarting. Please send that again in a moment."
	}
	// An apierr message is written for the caller; anything else may carry
	// internals and stays in the log.
	var public *apierr.Error
	if errors.As(err, &public) {
		if public.Kind() == apierr.KindNotConfigured {
			return "The BuildMax assistant is not configured on this server."
		}
		return public.Error()
	}
	g.log.Error("chat turn failed", "conversation_id", conv.ID, "err", err)
	if link := g.link("/#/spaces/" + conv.SpaceID + "/chat/" + conv.ID); link != "" {
		return genericFailure + " You can also continue in BuildMax: " + link
	}
	return genericFailure
}

func (g *Gateway) command(ctx context.Context, c corechannel.Connector, in corechannel.Inbound, ident *corechannel.Identity, cmd, arg string) {
	switch cmd {
	case "start", "help":
		g.reply(ctx, c, in.ChatID, g.helpText(ctx, c.Platform(), in.ChatID, ident.UserID))
	case "new":
		space, refusal := g.currentSpace(ctx, c.Platform(), in.ChatID, ident.UserID)
		if refusal != "" {
			g.reply(ctx, c, in.ChatID, refusal)
			return
		}
		if _, refusal := g.startConversation(ctx, c.Platform(), in.ChatID, ident.UserID, space); refusal != "" {
			g.reply(ctx, c, in.ChatID, refusal)
			return
		}
		g.reply(ctx, c, in.ChatID, fmt.Sprintf("Started a new conversation in %s.", space.Name))
	case "space":
		g.spaceCommand(ctx, c, in, ident, arg)
	default:
		g.reply(ctx, c, in.ChatID, "Unknown command.\n\n"+commandList)
	}
}

const commandList = `/new — start a new conversation
/space — list your Spaces; /space <number> switches
/help — show this help`

func (g *Gateway) helpText(ctx context.Context, platform, chatID, userID string) string {
	var b strings.Builder
	b.WriteString("Talk to your BuildMax assistant here: ask questions, or ask it to start work in a Space. You'll hear back here when work it started finishes.")
	if space, refusal := g.currentSpace(ctx, platform, chatID, userID); refusal == "" {
		fmt.Fprintf(&b, "\n\nCurrent Space: %s", space.Name)
	}
	b.WriteString("\n\n" + commandList)
	if link := g.link("/#/account/chat"); link != "" {
		b.WriteString("\n\nManage or remove this link in BuildMax: " + link)
	}
	return b.String()
}

// currentSpace is the Space of the chat's newest conversation, or the user's
// personal Space before there is one.
func (g *Gateway) currentSpace(ctx context.Context, platform, chatID, userID string) (*corespace.Space, string) {
	spaces, err := g.spaces.ListSpacesByUser(ctx, userID)
	if err != nil {
		g.log.Error("space listing failed", "err", err)
		return nil, genericFailure
	}
	conv, err := g.conversations.LatestChatConversation(ctx, userID, platform, chatID)
	if err != nil {
		g.log.Error("chat conversation lookup failed", "err", err)
		return nil, genericFailure
	}
	for i := range spaces {
		if conv != nil && spaces[i].ID == conv.SpaceID {
			return &spaces[i], ""
		}
	}
	if conv != nil {
		return nil, "You are no longer a member of the Space this chat was using. Use /space to choose another."
	}
	if p := personalOf(spaces, userID); p != nil {
		return p, ""
	}
	return nil, "You have no Space to talk in yet. Use /space to choose one."
}

func (g *Gateway) personalSpace(ctx context.Context, userID string) (*corespace.Space, error) {
	spaces, err := g.spaces.ListSpacesByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return personalOf(spaces, userID), nil
}

func personalOf(spaces []corespace.Space, userID string) *corespace.Space {
	for i := range spaces {
		if p := spaces[i].PersonalForUserID; p != nil && *p == userID {
			return &spaces[i]
		}
	}
	return nil
}

// spaceCommand lists the user's Spaces, or switches the chat to one by number
// or exact name. Switching starts a new conversation there: a conversation
// belongs to one Space for its whole life.
func (g *Gateway) spaceCommand(ctx context.Context, c corechannel.Connector, in corechannel.Inbound, ident *corechannel.Identity, arg string) {
	spaces, err := g.spaces.ListSpacesByUser(ctx, ident.UserID)
	if err != nil {
		g.log.Error("space listing failed", "err", err)
		g.reply(ctx, c, in.ChatID, genericFailure)
		return
	}
	if len(spaces) == 0 {
		g.reply(ctx, c, in.ChatID, "You are not a member of any Space.")
		return
	}
	if arg == "" {
		current, _ := g.currentSpace(ctx, c.Platform(), in.ChatID, ident.UserID)
		var b strings.Builder
		b.WriteString("Your Spaces:\n")
		for i, s := range spaces {
			marker := ""
			if current != nil && current.ID == s.ID {
				marker = "  ← current"
			}
			fmt.Fprintf(&b, "%d. %s%s\n", i+1, s.Name, marker)
		}
		b.WriteString("\nSend /space <number> to switch.")
		g.reply(ctx, c, in.ChatID, b.String())
		return
	}
	var target *corespace.Space
	if n, err := strconv.Atoi(arg); err == nil && n >= 1 && n <= len(spaces) {
		target = &spaces[n-1]
	} else {
		for i := range spaces {
			if strings.EqualFold(spaces[i].Name, arg) {
				target = &spaces[i]
				break
			}
		}
	}
	if target == nil {
		g.reply(ctx, c, in.ChatID, "No Space matches that. Send /space to see the list.")
		return
	}
	if _, refusal := g.startConversation(ctx, c.Platform(), in.ChatID, ident.UserID, target); refusal != "" {
		g.reply(ctx, c, in.ChatID, refusal)
		return
	}
	g.reply(ctx, c, in.ChatID, fmt.Sprintf("Switched to %s and started a new conversation there.", target.Name))
}

// link renders a Portal path against the public origin, or "" without one.
func (g *Gateway) link(path string) string {
	if g.portalURL == "" {
		return ""
	}
	return strings.TrimRight(g.portalURL, "/") + path
}
