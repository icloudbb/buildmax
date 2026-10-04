package conversation

import (
	"context"
	"strings"
	"testing"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	issuesvc "github.com/icloudbb/buildmax/internal/service/issue"
)

type fakeEscalations struct{ created []issuesvc.CreateIssueCmd }

func (f *fakeEscalations) CreateIssue(_ context.Context, cmd issuesvc.CreateIssueCmd) (*coreissue.Issue, error) {
	f.created = append(f.created, cmd)
	return &coreissue.Issue{ID: "iss_1", SpaceID: cmd.SpaceID, ConversationID: cmd.ConversationID}, nil
}

// Escalate opens one Issue per call in the Assistant's Space, created by its
// service account and linked to this conversation, and tells the model what
// to say without the Issue's id reaching the requester.
func TestAssistantEscalateOpensOneIssuePerCall(t *testing.T) {
	escalations := &fakeEscalations{}
	f := newAssistantFixture(
		toolCall(t, "Escalate", map[string]string{"summary": "Wants their payslip for March corrected.\nSays overtime is missing."}),
	)
	f.svc.Issues = escalations
	reply := f.turn(t, "my payslip is wrong")

	if len(escalations.created) != 1 {
		t.Fatalf("issues created = %d, want 1", len(escalations.created))
	}
	c := escalations.created[0]
	if c.SpaceID != asstSpace || c.UserID != serviceAcct || c.ConversationID != asstConv ||
		c.Title != "Request via HR Assistant: Wants their payslip for March corrected." ||
		!strings.Contains(c.Description, "Says overtime is missing.") || !strings.Contains(c.Description, "Reply to requester") {
		t.Errorf("issue = %+v", c)
	}
	stored, _ := f.messages.ListMessages(context.Background(), asstConv)
	var tool string
	for _, m := range stored {
		if m.Role == "tool" {
			tool = m.Content
		}
	}
	if !strings.Contains(tool, "someone in the Space will follow up") {
		t.Errorf("tool result = %q", tool)
	}
	if strings.Contains(reply, "iss_1") {
		t.Errorf("reply = %q leaks the issue id", reply)
	}
}

// An empty summary is refused without an Issue, and the tool is absent
// without an issue store.
func TestAssistantEscalateRefusesAnEmptySummary(t *testing.T) {
	escalations := &fakeEscalations{}
	tools := newEscalateTool(escalations, turnRunInput{SpaceID: asstSpace, ConversationID: asstConv, Assistant: &newAssistantFixture().profile})
	out, err := tools[0].Execute(context.Background(), map[string]any{"summary": "  "})
	if err != nil || !strings.Contains(out, "summary is required") || len(escalations.created) != 0 {
		t.Errorf("empty summary = %q, %v, created %d", out, err, len(escalations.created))
	}
	if got := newEscalateTool(nil, turnRunInput{SpaceID: asstSpace}); got != nil {
		t.Errorf("tools without issues = %v", got)
	}
}
