package channel

import (
	"context"
	"fmt"
	"strings"
	"time"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

const (
	// reportOutputLimit keeps an outcome report readable on a phone; the full
	// result is one link away.
	reportOutputLimit = 1500
	reportErrorLimit  = 500
)

// ReportRunTerminal tells the chat a Task came from how its run ended. It runs
// on whichever replica saw the run finish: sending needs only the bot
// credential, not the receive lease.
//
// It reports only while the conversation's owner can still see the result:
// their account may still work in the Space, and they still have a chat
// account linked on that platform.
func (g *Gateway) ReportRunTerminal(ctx context.Context, info coretask.RunTerminalInfo) {
	if g == nil || info.ConversationID == "" {
		return
	}
	conv, err := g.conversations.GetConversation(ctx, info.ConversationID)
	if err != nil || conv == nil || conv.ChannelRef == "" {
		return
	}
	c := g.connectors[conv.Channel]
	if c == nil {
		return
	}
	if g.eligible != nil && g.eligible.Check(ctx, conv.UserID, conv.SpaceID) != nil {
		return
	}
	links, err := g.identities.ListIdentitiesByUser(ctx, conv.UserID)
	if err != nil {
		g.log.Warn("outcome report skipped: link lookup failed", "err", err)
		return
	}
	linked := false
	for _, l := range links {
		linked = linked || l.Platform == conv.Channel
	}
	if !linked {
		return
	}
	title := ""
	if g.tasks != nil {
		if t, err := g.tasks.GetTask(ctx, info.TaskID); err == nil && t != nil {
			title = t.Title
		}
	}
	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	g.reply(sendCtx, c, conv.ChannelRef, g.formatReport(info, title))
}

func (g *Gateway) formatReport(info coretask.RunTerminalInfo, title string) string {
	name := "A task"
	if title != "" {
		name = fmt.Sprintf("Task “%s”", title)
	}
	var b strings.Builder
	switch coretask.RunStatus(info.Status) {
	case coretask.RunStatusSucceeded:
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

func truncate(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "…"
}
