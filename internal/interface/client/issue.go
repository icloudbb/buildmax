package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/infra/httpclient"
)

// OwnedIssue is one item of the caller's inbox, with the space it belongs to
// kept alongside it.
//
// The space travels with the issue because a local surface has no current space:
// a login names a server and a person, and that person's work is spread across
// every space they are in. Anything the caller does next with this issue needs
// the space back.
type OwnedIssue struct {
	SpaceID   string
	SpaceName string
	Issue     coreissue.Issue
}

type spacesResponse []corespace.Space

type issueListResponse struct {
	Issues []coreissue.Issue `json:"issues"`
	Total  int               `json:"total"`
}

// ListSpaces returns the spaces the caller belongs to.
func (c *Client) ListSpaces(ctx context.Context, token string) ([]corespace.Space, error) {
	var out spacesResponse
	if err := c.getJSON(ctx, token, "/api/spaces", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListOwnedIssues returns what the caller owns, across every space they
// belong to.
//
// One request per space, because the server has no cross-space listing and
// inventing one would put a route that reads every space a person is in behind a
// question only this inbox asks. A person's spaces are few; if that stops being
// true, the fix is a route, not a wider fan-out here.
//
// A space that fails is skipped rather than failing the inbox: an inbox missing
// one space's work is more useful than no inbox, and the caller is told which
// space could not be read.
func (c *Client) ListOwnedIssues(ctx context.Context, token, status string, limit int) ([]OwnedIssue, []error) {
	spaces, err := c.ListSpaces(ctx, token)
	if err != nil {
		return nil, []error{fmt.Errorf("list spaces: %w", err)}
	}
	var out []OwnedIssue
	var problems []error
	for _, space := range spaces {
		issues, err := c.listOwnedInSpace(ctx, token, space.ID, status, limit)
		if err != nil {
			problems = append(problems, fmt.Errorf("space %s: %w", space.Name, err))
			continue
		}
		for _, issue := range issues {
			out = append(out, OwnedIssue{SpaceID: space.ID, SpaceName: space.Name, Issue: issue})
		}
	}
	return out, problems
}

func (c *Client) listOwnedInSpace(ctx context.Context, token, spaceID, status string, limit int) ([]coreissue.Issue, error) {
	query := url.Values{}
	query.Set("owner", "me")
	if status != "" {
		query.Set("status", status)
	}
	if limit > 0 {
		query.Set("limit", fmt.Sprint(limit))
	}
	path := "/api/spaces/" + url.PathEscape(spaceID) + "/issues?" + query.Encode()
	var out issueListResponse
	if err := c.getJSON(ctx, token, path, &out); err != nil {
		return nil, err
	}
	return out.Issues, nil
}

// issueCommentWindow is how much of a thread a local session reads, matching
// the worker plane's bound. The reason is the same: an agent that spends its
// context on a space's discussion has less of it left for the work.
const issueCommentWindow = 20

type createCommentPayload struct {
	Body       string `json:"body"`
	AuthorKind string `json:"author_kind,omitempty"`
}

// CommentOnIssue posts one comment on an issue, claimed as local_agent.
//
// This is the CLI's report path, reachable by any executor that can run a
// command. It is local_agent, not agent: a machine this deployment did not
// schedule, so the thread says so. The server bounds the body and, for a run,
// the count; this client adds no limit of its own.
func (c *Client) CommentOnIssue(ctx context.Context, token, spaceID, issueID, body string) error {
	return c.postIssueComment(ctx, token, spaceID, issueID, body, coreissue.CommentAuthorLocalAgent)
}

// CommentAsPerson posts one comment written by the signed-in person, so the
// thread attributes it to them rather than to a local agent. Desktop's comment
// box uses it: a person typed it, whatever a session helped them draft.
func (c *Client) CommentAsPerson(ctx context.Context, token, spaceID, issueID, body string) error {
	return c.postIssueComment(ctx, token, spaceID, issueID, body, "")
}

func (c *Client) postIssueComment(ctx context.Context, token, spaceID, issueID, body, authorKind string) error {
	payload, err := json.Marshal(createCommentPayload{Body: body, AuthorKind: authorKind})
	if err != nil {
		return err
	}
	path := "/api/spaces/" + url.PathEscape(spaceID) + "/issues/" + url.PathEscape(issueID) + "/comments"
	resp, err := c.do(ctx, http.MethodPost, token, path, "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		return httpclient.DecodeError(resp, "POST "+path)
	}
	return nil
}

// FindIssue reports which of the caller's spaces holds an issue, and the issue.
//
// One request per space until one answers, because the server addresses an issue
// through its space and there is no route that resolves a bare issue id. Adding
// one would let anyone probe whether an id exists in a space they cannot see,
// which is a worse trade than a handful of requests a person makes once when
// starting work.
//
// The issue comes back with the space because every caller needs it next: to
// print it, to say what a session is working on, or to read the version an
// update has to carry.
func (c *Client) FindIssue(ctx context.Context, token, issueID string) (corespace.Space, coreissue.Issue, error) {
	spaces, err := c.ListSpaces(ctx, token)
	if err != nil {
		return corespace.Space{}, coreissue.Issue{}, fmt.Errorf("list spaces: %w", err)
	}
	for _, space := range spaces {
		var issue coreissue.Issue
		path := "/api/spaces/" + url.PathEscape(space.ID) + "/issues/" + url.PathEscape(issueID)
		if err := c.getJSON(ctx, token, path, &issue); err == nil && issue.ID != "" {
			return space, issue, nil
		}
	}
	return corespace.Space{}, coreissue.Issue{}, fmt.Errorf("no space you belong to has issue %s", issueID)
}

// GetIssue reads one issue in a space the caller already knows.
func (c *Client) GetIssue(ctx context.Context, token, spaceID, issueID string) (coreissue.Issue, error) {
	var issue coreissue.Issue
	path := "/api/spaces/" + url.PathEscape(spaceID) + "/issues/" + url.PathEscape(issueID)
	if err := c.getJSON(ctx, token, path, &issue); err != nil {
		return coreissue.Issue{}, err
	}
	return issue, nil
}

// SetIssueStatus moves an issue, carrying the version it was read at.
//
// A person's action, never a tool's: status is what the space reads to plan
// around, and `done` means a person accepted the work. See
// docs/design/issue-agent-access.md section 2.
//
// The version is a parameter rather than something this re-reads, so the status
// a caller confirmed is the status of the issue they looked at. Re-reading here
// would turn a refused stale write into a silent one.
func (c *Client) SetIssueStatus(ctx context.Context, token, spaceID, issueID, status string, version uint64) (coreissue.Issue, error) {
	payload, err := json.Marshal(map[string]any{"version": version, "status": status})
	if err != nil {
		return coreissue.Issue{}, err
	}
	path := "/api/spaces/" + url.PathEscape(spaceID) + "/issues/" + url.PathEscape(issueID)
	resp, err := c.do(ctx, http.MethodPatch, token, path, "application/json", bytes.NewReader(payload))
	if err != nil {
		return coreissue.Issue{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return coreissue.Issue{}, httpclient.DecodeError(resp, "PATCH "+path)
	}
	var out coreissue.Issue
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return coreissue.Issue{}, err
	}
	return out, nil
}

// IssueThread reads an issue's children and recent comments for a person to
// look at. Same window as the agent gets, for the same reason.
func (c *Client) IssueThread(ctx context.Context, token, spaceID, issueID string) ([]coreissue.Issue, []coreissue.Comment, int, error) {
	var children issueListResponse
	childPath := "/api/spaces/" + url.PathEscape(spaceID) + "/issues?parent_id=" + url.QueryEscape(issueID)
	if err := c.getJSON(ctx, token, childPath, &children); err != nil {
		return nil, nil, 0, err
	}
	var page struct {
		Comments []coreissue.Comment `json:"comments"`
		Total    int                 `json:"total"`
	}
	base := "/api/spaces/" + url.PathEscape(spaceID) + "/issues/" + url.PathEscape(issueID) + "/comments"
	if err := c.getJSON(ctx, token, base+"?limit="+strconv.Itoa(issueCommentWindow), &page); err != nil {
		return children.Issues, nil, 0, err
	}
	omitted := 0
	if page.Total > issueCommentWindow {
		omitted = page.Total - issueCommentWindow
		var tail struct {
			Comments []coreissue.Comment `json:"comments"`
			Total    int                 `json:"total"`
		}
		if err := c.getJSON(ctx, token, base+"?limit="+strconv.Itoa(issueCommentWindow)+"&offset="+strconv.Itoa(omitted), &tail); err == nil {
			page.Comments = tail.Comments
		} else {
			omitted = 0
		}
	}
	return children.Issues, page.Comments, omitted, nil
}
