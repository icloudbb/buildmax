package conversation

import (
	"context"
	"fmt"
	"strings"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	"github.com/icloudbb/buildmax/internal/core/llm"
	issuesvc "github.com/icloudbb/buildmax/internal/service/issue"
	"github.com/icloudbb/buildmax/internal/util"
)

// Issues creates the Issue an Assistant escalates to.
type Issues interface {
	CreateIssue(ctx context.Context, cmd issuesvc.CreateIssueCmd) (*coreissue.Issue, error)
}

const (
	toolNameEscalate    = "Escalate"
	maxEscalationTitle  = 120
	maxEscalationDetail = 8000
)

// escalateTool hands a request the Assistant cannot answer to a person in its
// Space: one Issue per call, created by the service account and linked to this
// conversation, from which a member replies to the requester. See
// docs/design/space-assistants.md §11.
type escalateTool struct {
	issues         Issues
	spaceID        string
	conversationID string
	assistant      *AssistantTurn
}

func (t *escalateTool) Name() string { return toolNameEscalate }

func (t *escalateTool) Description() string {
	return "Hand the person's request to a person in the Space when you cannot answer it or do what they need with your other tools. " +
		"Write a summary a person can act on: what they asked, and anything they told you that matters. Call it once per request. " +
		"Then tell the person that someone will follow up in this chat."
}

func (t *escalateTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"summary": map[string]any{"type": "string", "description": "What the person needs, for the person who picks it up."},
		},
		"required": []any{"summary"},
	}
}

func (t *escalateTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	summary, _ := args["summary"].(string)
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return "summary is required: say what the person needs.", nil
	}
	summary = util.TruncateRunes(summary, maxEscalationDetail)
	first, _, _ := strings.Cut(summary, "\n")
	issue, err := t.issues.CreateIssue(ctx, issuesvc.CreateIssueCmd{
		UserID:  t.assistant.ActingUserID,
		SpaceID: t.spaceID,
		Title:   util.TruncateRunes("Request via "+t.assistant.Name+": "+strings.TrimSpace(first), maxEscalationTitle),
		Description: fmt.Sprintf("%s\n\nEscalated by the assistant %q from a chat. The person asking cannot see this issue: answer them with Reply to requester.",
			summary, t.assistant.Name),
		ConversationID: t.conversationID,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Escalated as issue %s. Tell the person that someone in the Space will follow up in this chat. Do not share the issue id.", issue.ID), nil
}

func newEscalateTool(issues Issues, in turnRunInput) []llm.Tool {
	if issues == nil || in.SpaceID == "" {
		return nil
	}
	return []llm.Tool{&escalateTool{issues: issues, spaceID: in.SpaceID, conversationID: in.ConversationID, assistant: in.Assistant}}
}
