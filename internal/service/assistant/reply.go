package assistant

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreassistant "github.com/icloudbb/buildmax/internal/core/assistant"
	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	issuesvc "github.com/icloudbb/buildmax/internal/service/issue"
)

// maxReplyRunes keeps a reply to one chat message; Telegram's limit is 4096.
const maxReplyRunes = 4000

var (
	ErrNotEscalated       = apierr.New(apierr.KindInvalid, "this issue was not escalated by an assistant, so it has no requester to reply to")
	ErrReplyRequired      = apierr.New(apierr.KindInvalid, "reply text is required")
	ErrReplyTooLong       = apierr.New(apierr.KindInvalid, "reply is too long for one chat message (4000 characters)")
	ErrAssistantNotAnswer = apierr.New(apierr.KindConflict, "the assistant is paused or cannot answer; reply once it is published again")
	ErrRequesterGone      = apierr.New(apierr.KindConflict, "the requester can no longer be answered by this assistant")
	ErrNoBot              = apierr.New(apierr.KindConflict, "the assistant has no bot to reply through")
)

// Issues reads an escalated Issue and records the reply on it.
type Issues interface {
	GetIssue(ctx context.Context, spaceID, issueID string) (*coreissue.Issue, error)
	CreateComment(ctx context.Context, cmd issuesvc.CreateCommentCmd) (*coreissue.Comment, error)
}

// ReplyCmd is a Space member answering an escalated Issue's requester.
type ReplyCmd struct {
	SpaceID string
	ActorID string
	IssueID string
	Text    string
}

// ReplyToRequester sends a member's text to the requester of an escalated
// Issue, through the Assistant's bot into the chat the request came from, and
// records it on the Issue. It is refused while the Assistant would not answer
// that requester itself. See docs/design/space-assistants.md §11.
func (f *FrontDoor) ReplyToRequester(ctx context.Context, cmd ReplyCmd) (*coreissue.Comment, error) {
	if f == nil || f.Service.ready() != nil || f.Issues == nil || f.Conversations == nil || f.Service.Bots == nil {
		return nil, ErrNotConfigured
	}
	text := strings.TrimSpace(cmd.Text)
	if text == "" {
		return nil, ErrReplyRequired
	}
	if utf8.RuneCountInString(text) > maxReplyRunes {
		return nil, ErrReplyTooLong
	}
	issue, err := f.Issues.GetIssue(ctx, cmd.SpaceID, cmd.IssueID)
	if err != nil {
		return nil, err
	}
	if issue.ConversationID == "" {
		return nil, ErrNotEscalated
	}
	conv, err := f.Conversations.GetConversation(ctx, issue.ConversationID)
	if err != nil {
		return nil, err
	}
	if conv == nil || conv.AssistantID == "" || conv.SpaceID != cmd.SpaceID {
		return nil, ErrNotEscalated
	}
	s := f.Service
	a, err := s.Store.GetAssistant(ctx, conv.AssistantID)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, ErrAssistantNotAnswer
	}
	if avail, err := s.Availability(ctx, a); err != nil {
		return nil, err
	} else if avail != coreassistant.Available {
		return nil, ErrAssistantNotAnswer
	}
	if f.checkRequester(ctx, a, conv.UserID) != "" {
		return nil, ErrRequesterGone
	}
	b, err := s.Store.GetBindingByAssistant(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	if b == nil || b.Platform != conv.Channel {
		return nil, ErrNoBot
	}
	if err := s.Bots.Gateway.Send(ctx, b.Platform, b.ID, conv.ChannelRef, text); err != nil {
		f.log().Warn("reply to requester not delivered", "issue_id", issue.ID, "assistant_id", a.ID, "err", err)
		return nil, apierr.New(apierr.KindUnavailable, "the reply could not be delivered; try again")
	}
	comment, err := f.Issues.CreateComment(ctx, issuesvc.CreateCommentCmd{
		IssueID: issue.ID, AuthorKind: coreissue.CommentAuthorUser, AuthorID: cmd.ActorID,
		Body: "Replied to the requester through " + a.Def.Name + ":\n\n" + text,
	})
	if err != nil {
		return nil, err
	}
	s.Audit.UserAction(ctx, cmd.ActorID, cmd.SpaceID, coreaudit.AssistantRequesterReplied, "issue", issue.ID, a.Def.Name)
	return comment, nil
}
