package channel

import (
	"context"
	"fmt"
	"strings"
	"time"

	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

const (
	// reportOutputLimit keeps an outcome report readable on a phone; the full
	// result is one link away.
	reportOutputLimit = 1500
	reportErrorLimit  = 500
)

// ReportRunTerminal tells the chat a Task came from how its run ended, through
// the bot that chat talks to. It runs on whichever replica saw the run finish:
// sending needs only the bot credential, not the receive lease.
//
// It reports only while the conversation's owner can still see the result:
// their account may still work in the Space, they still have a chat account
// linked on that platform, and they signed in recently enough for it to act.
// A Space Assistant's conversation is reported by its front door, which
// decides who may still hear and what of the result they may learn.
func (g *Gateway) ReportRunTerminal(ctx context.Context, info coretask.RunTerminalInfo) {
	if g == nil || info.ConversationID == "" {
		return
	}
	conv, err := g.conversations.GetConversation(ctx, info.ConversationID)
	if err != nil || conv == nil || conv.ChannelRef == "" {
		return
	}
	if !g.mayReport(ctx, conv) {
		return
	}
	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if conv.AssistantID != "" {
		f := g.currentFrontDoor()
		if f == nil {
			return
		}
		key, text, ok := f.Outcome(ctx, conv, info)
		if c := g.connector(conv.Channel, key); ok && c != nil {
			g.reply(sendCtx, *c, conv.ChannelRef, text)
		}
		return
	}
	c := g.connector(conv.Channel, conv.ChannelConnector)
	if c == nil {
		return
	}
	if g.eligible != nil && g.eligible.Check(ctx, conv.UserID, conv.SpaceID) != nil {
		return
	}
	title := ""
	if g.tasks != nil {
		if t, err := g.tasks.GetTask(ctx, info.TaskID); err == nil && t != nil {
			title = t.Title
		}
	}
	g.reply(sendCtx, *c, conv.ChannelRef, g.formatReport(info, title))
}

// mayReport is whether the conversation's owner still has a chat account
// linked on its platform and signed in recently enough for it to act.
func (g *Gateway) mayReport(ctx context.Context, conv *coreconv.Conversation) bool {
	links, err := g.identities.ListIdentitiesByUser(ctx, conv.UserID)
	if err != nil {
		g.log.Warn("outcome report skipped: link lookup failed", "err", err)
		return false
	}
	linked := false
	for _, l := range links {
		linked = linked || l.Platform == conv.Channel
	}
	if !linked {
		return false
	}
	active, err := g.linkActive(ctx, conv.UserID)
	return err == nil && active
}

func (g *Gateway) formatReport(info coretask.RunTerminalInfo, title string) string {
	name := "A task"
	if title != "" {
		name = fmt.Sprintf("Task “%s”", title)
	}
	var b strings.Builder
	switch coretask.RunStatus(info.Status) {
	case coretask.RunStatusSucceeded:
		if info.AwaitingAnswer {
			// The questions close the output, so a long reply keeps its end: the
			// part the user has to act on.
			fmt.Fprintf(&b, "%s is waiting for your answer.", name)
			if info.Output != nil && strings.TrimSpace(*info.Output) != "" {
				b.WriteString("\n\n" + truncateHead(strings.TrimSpace(*info.Output), reportOutputLimit))
			}
			break
		}
		fmt.Fprintf(&b, "%s finished.", name)
		if info.Output != nil && strings.TrimSpace(*info.Output) != "" {
			b.WriteString("\n\n" + truncate(strings.TrimSpace(*info.Output), reportOutputLimit))
		}
	case coretask.RunStatusCanceled:
		fmt.Fprintf(&b, "%s was canceled.", name)
	default:
		fmt.Fprintf(&b, "%s failed.", name)
		if info.ErrorMessage != nil && *info.ErrorMessage != "" {
			b.WriteString("\n\n" + truncate(*info.ErrorMessage, reportErrorLimit))
		}
	}
	if link := g.link("/#/spaces/" + info.SpaceID + "/tasks/" + info.TaskID); link != "" {
		b.WriteString("\n\n" + link)
	}
	return b.String()
}

// truncateHead keeps the end of s, where a run waiting on the user put its
// questions.
func truncateHead(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return "…" + string(r[len(r)-limit:])
}

func truncate(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "…"
}
