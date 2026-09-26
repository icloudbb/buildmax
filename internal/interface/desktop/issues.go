package desktop

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	"github.com/icloudbb/buildmax/internal/infra/httpclient"
	"github.com/icloudbb/buildmax/internal/interface/auth"
	"github.com/icloudbb/buildmax/internal/interface/client"
)

// The Issues view is the Desktop half of the local Issue bridge: a person sees
// the Space work they own, reads one Issue, moves its status, comments on it,
// and starts a local chat from it. Every call is a person's action with their
// own login; nothing here is reachable by an Agent, and nothing links a chat
// to an Issue — the chat starts from a visible, editable prompt instead. See
// docs/design/agent-bridge-cli.md.

// issueInboxLimit bounds each space's share of the inbox per status. An inbox
// longer than this is a planning question Portal's Board answers better.
const issueInboxLimit = 50

// IssueItemPayload is one Issue as the Issues view lists it.
type IssueItemPayload struct {
	ID        string `json:"id"`
	SpaceID   string `json:"space_id"`
	SpaceName string `json:"space_name"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Version   uint64 `json:"version"`
	UpdatedAt string `json:"updated_at"`
}

// IssueInboxPayload is the signed-in person's open work. Warnings name the
// spaces that could not be read, so a partial inbox never passes for a whole one.
type IssueInboxPayload struct {
	Issues   []IssueItemPayload `json:"issues"`
	Warnings []string           `json:"warnings"`
}

// IssueCommentPayload is one comment on an Issue's thread.
type IssueCommentPayload struct {
	AuthorKind string `json:"author_kind"`
	// Mine is true for a comment this person wrote, so the view can say "You"
	// without the server returning member names on every comment.
	Mine      bool   `json:"mine"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

// IssueDetailPayload is one Issue with the context a person needs to act on it:
// the same bounded window of children and discussion `buildmax issue show` reads.
type IssueDetailPayload struct {
	Issue           IssueItemPayload      `json:"issue"`
	Description     string                `json:"description"`
	Children        []IssueItemPayload    `json:"children"`
	Comments        []IssueCommentPayload `json:"comments"`
	OmittedComments int                   `json:"omitted_comments"`
}

// IssueStatusResult reports a status change. Conflict is set, with a nil error,
// when the Issue changed after it was read: that is an expected outcome the view
// answers by reloading, not a failure.
type IssueStatusResult struct {
	Issue    *IssueItemPayload `json:"issue,omitempty"`
	Conflict bool              `json:"conflict"`
}

func issueItemPayload(spaceID, spaceName string, issue coreissue.Issue) IssueItemPayload {
	return IssueItemPayload{
		ID:        issue.ID,
		SpaceID:   spaceID,
		SpaceName: spaceName,
		Title:     issue.Title,
		Status:    issue.Status,
		Version:   issue.Version,
		UpdatedAt: issue.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// issueSession is the signed-in server, a token for it, and who is signed in.
// The Issues view exists only in server mode, so every failure here means the
// login is gone or unusable, and says so.
func issueSession() (*client.Client, string, string, error) {
	info, err := auth.Info()
	if err != nil {
		return nil, "", "", fmt.Errorf("read credentials: %w", err)
	}
	if !info.LoggedIn || info.ServerURL == "" {
		return nil, "", "", fmt.Errorf("sign in to a BuildMax server to see Space issues")
	}
	token, err := auth.TokenForServer(info.ServerURL)
	if err != nil {
		return nil, "", "", fmt.Errorf("authenticate to %s: %w", info.ServerURL, err)
	}
	return auth.ServerClient(info.ServerURL), token, info.UserID, nil
}

// ListMyIssues returns the open Issues the signed-in person owns, across every
// space they are in. Done work is left to Portal: this is an inbox, not a history.
func (a *App) ListMyIssues() (*IssueInboxPayload, error) {
	c, token, _, err := issueSession()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.requestContext()
	defer cancel()
	out := &IssueInboxPayload{Issues: []IssueItemPayload{}, Warnings: []string{}}
	seen := map[string]bool{}
	for _, status := range []string{coreissue.StatusInProgress, coreissue.StatusTodo} {
		owned, problems := c.ListOwnedIssues(ctx, token, status, issueInboxLimit)
		for _, p := range problems {
			if msg := p.Error(); !seen[msg] {
				seen[msg] = true
				out.Warnings = append(out.Warnings, msg)
			}
		}
		for _, item := range owned {
			out.Issues = append(out.Issues, issueItemPayload(item.SpaceID, item.SpaceName, item.Issue))
		}
	}
	// A listing that failed for every space is a failure, not an empty inbox.
	if len(out.Issues) == 0 && len(out.Warnings) > 0 {
		return nil, errors.New(strings.Join(out.Warnings, "; "))
	}
	sort.SliceStable(out.Issues, func(i, j int) bool { return out.Issues[i].UpdatedAt > out.Issues[j].UpdatedAt })
	return out, nil
}

// GetIssueDetail reads one Issue with its sub-issues and recent discussion.
func (a *App) GetIssueDetail(spaceID, spaceName, issueID string) (*IssueDetailPayload, error) {
	c, token, userID, err := issueSession()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.requestContext()
	defer cancel()
	issue, err := c.GetIssue(ctx, token, spaceID, issueID)
	if err != nil {
		return nil, fmt.Errorf("read issue: %w", err)
	}
	children, comments, omitted, err := c.IssueThread(ctx, token, spaceID, issueID)
	if err != nil {
		return nil, fmt.Errorf("read issue thread: %w", err)
	}
	out := &IssueDetailPayload{
		Issue:           issueItemPayload(spaceID, spaceName, issue),
		Description:     issue.Description,
		Children:        make([]IssueItemPayload, 0, len(children)),
		Comments:        make([]IssueCommentPayload, 0, len(comments)),
		OmittedComments: omitted,
	}
	for _, child := range children {
		out.Children = append(out.Children, issueItemPayload(spaceID, spaceName, child))
	}
	for _, comment := range comments {
		out.Comments = append(out.Comments, IssueCommentPayload{
			AuthorKind: comment.AuthorKind,
			Mine:       comment.AuthorKind == coreissue.CommentAuthorUser && comment.AuthorID == userID,
			Body:       comment.Body,
			CreatedAt:  comment.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}

// SetIssueStatus moves an Issue, carrying the version the view read. A newer
// edit is reported as a conflict rather than overwritten.
func (a *App) SetIssueStatus(spaceID, spaceName, issueID, status string, version uint64) (*IssueStatusResult, error) {
	c, token, _, err := issueSession()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.requestContext()
	defer cancel()
	updated, err := c.SetIssueStatus(ctx, token, spaceID, issueID, status, version)
	var httpErr *httpclient.Error
	if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusConflict {
		return &IssueStatusResult{Conflict: true}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("update status: %w", err)
	}
	item := issueItemPayload(spaceID, spaceName, updated)
	return &IssueStatusResult{Issue: &item}, nil
}

// CommentOnIssue posts a comment the signed-in person wrote.
func (a *App) CommentOnIssue(spaceID, issueID, body string) error {
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("a comment needs some text")
	}
	c, token, _, err := issueSession()
	if err != nil {
		return err
	}
	ctx, cancel := a.requestContext()
	defer cancel()
	if err := c.CommentAsPerson(ctx, token, spaceID, issueID, body); err != nil {
		return fmt.Errorf("post comment: %w", err)
	}
	return nil
}

// requestContext bounds one Issue request. The app context outlives any one
// call, and a server that stops answering should fail the view, not hang it.
func (a *App) requestContext() (context.Context, context.CancelFunc) {
	base := a.ctx
	if base == nil {
		base = context.Background()
	}
	return context.WithTimeout(base, 30*time.Second)
}
