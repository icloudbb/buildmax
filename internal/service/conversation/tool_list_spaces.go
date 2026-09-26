package conversation

import (
	"context"
	"fmt"
	"strings"

	"github.com/icloudbb/buildmax/internal/core/llm"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
)

// spaceLister is the slice of the space store ListSpaces reads.
type spaceLister interface {
	ListSpacesByUser(ctx context.Context, userID string) ([]corespace.Space, error)
}

// listSpacesTool shows the user's Spaces so the assistant can say where work
// would run and where else it could. It cannot move the conversation: a
// conversation belongs to one Space for its whole life, so switching stays the
// user's action in Portal or through a chat app's /space command.
type listSpacesTool struct {
	spaces         spaceLister
	userID         string
	currentSpaceID string
}

const toolNameListSpaces = "ListSpaces"

// Access implements llm.AccessDeclarer. Listing memberships changes nothing.
func (t *listSpacesTool) Access(_ map[string]any) llm.Access { return llm.AccessReadOnly }

func (t *listSpacesTool) Name() string { return toolNameListSpaces }

func (t *listSpacesTool) Description() string {
	return "List the Spaces the user belongs to, marking the one this conversation belongs to and the user's personal Space. The numbering matches the chat-app /space command. Use this when the user asks which Spaces they have or where work would run. You cannot switch Space; tell the user how to."
}

func (t *listSpacesTool) Parameters() any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
		"required":   []any{},
	}
}

func (t *listSpacesTool) Execute(ctx context.Context, _ map[string]any) (string, error) {
	if t.spaces == nil {
		return "", fmt.Errorf("%s not configured", toolNameListSpaces)
	}
	spaces, err := t.spaces.ListSpacesByUser(ctx, t.userID)
	if err != nil {
		return "", err
	}
	if len(spaces) == 0 {
		return "The user belongs to no Space.", nil
	}
	var b strings.Builder
	for i, s := range spaces {
		fmt.Fprintf(&b, "%d. %s", i+1, s.Name)
		if p := s.PersonalForUserID; p != nil && *p == t.userID {
			b.WriteString(" (personal)")
		}
		if s.ID == t.currentSpaceID {
			b.WriteString(" ← current")
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// newListSpacesTool returns a llm.Tool that lists the user's Spaces. If spaces
// is nil, Execute returns "not configured".
func newListSpacesTool(spaces spaceLister, userID, currentSpaceID string) llm.Tool {
	return &listSpacesTool{spaces: spaces, userID: userID, currentSpaceID: currentSpaceID}
}
