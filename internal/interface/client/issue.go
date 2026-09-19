package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/infra/httpclient"
	"github.com/icloudbb/buildmax/internal/tool"
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

type issueCommentPayload struct {
	AuthorKind string    `json:"author_kind"`
	AuthorID   string    `json:"author_id"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
}

type issueCommentListPayload struct {
	Comments []issueCommentPayload `json:"comments"`
	Total    int                   `json:"total"`
}

type createCommentPayload struct {
	Body       string `json:"body"`
	AuthorKind string `json:"author_kind,omitempty"`
}

type localIssueClient struct {
	base    string
	spaceID string
	issueID string
	token   TokenFunc
	client  *Client
}

// NewIssueClient scopes a local session to one Issue on one server.
//
// The Issue and the space are fixed here, not passed per call, for the reason
// the worker client has no issue parameter either: a model that can name an
// Issue turns every instruction hidden in a comment thread into a working verb.
// See docs/design/issue-agent-access.md section 5.3.
//
// Returns nil when anything needed is missing, so the caller registers no tools
// rather than tools that fail on every call.
func NewIssueClient(serverURL, spaceID, issueID string, token TokenFunc) tool.IssueClient {
	if serverURL == "" || spaceID == "" || issueID == "" || token == nil {
		return nil
	}
	return &localIssueClient{
		base:    strings.TrimRight(serverURL, "/"),
		spaceID: spaceID,
		issueID: issueID,
		token:   token,
		client:  NewClient(serverURL),
	}
}

func (c *localIssueClient) issuePath(suffix string) string {
	return "/api/spaces/" + url.PathEscape(c.spaceID) + "/issues/" + url.PathEscape(c.issueID) + suffix
}

// Issue implements tool.IssueClient.
func (c *localIssueClient) Issue(ctx context.Context) (tool.IssueSnapshot, error) {
	token, err := c.token(c.base)
	if err != nil {
		return tool.IssueSnapshot{}, fmt.Errorf("authenticate to %s: %w", c.base, err)
	}
	var issue coreissue.Issue
	if err := c.client.getJSON(ctx, token, c.issuePath(""), &issue); err != nil {
		return tool.IssueSnapshot{}, err
	}
	out := tool.IssueSnapshot{
		Title:       issue.Title,
		Description: issue.Description,
		Status:      issue.Status,
	}
	if issue.ExecutorKind != nil {
		out.ExecutorKind = *issue.ExecutorKind
	}
	// Children and the thread are context, not the answer. Failing to read
	// either leaves the Issue readable rather than failing the whole call.
	var children issueListResponse
	childPath := "/api/spaces/" + url.PathEscape(c.spaceID) + "/issues?parent_id=" + url.QueryEscape(c.issueID)
	if err := c.client.getJSON(ctx, token, childPath, &children); err == nil {
		for _, child := range children.Issues {
			out.Children = append(out.Children, tool.IssueChild{Title: child.Title, Status: child.Status})
		}
	}
	comments, omitted := c.recentComments(ctx, token)
	out.Comments, out.OmittedComments = comments, omitted
	return out, nil
}

func (c *localIssueClient) recentComments(ctx context.Context, token string) ([]tool.IssueComment, int) {
	read := func(offset int) (issueCommentListPayload, error) {
		var page issueCommentListPayload
		path := c.issuePath("/comments") + "?limit=" + strconv.Itoa(issueCommentWindow) + "&offset=" + strconv.Itoa(offset)
		return page, c.client.getJSON(ctx, token, path, &page)
	}
	page, err := read(0)
	if err != nil {
		return nil, 0
	}
	omitted := 0
	if page.Total > issueCommentWindow {
		omitted = page.Total - issueCommentWindow
		if tail, err := read(omitted); err == nil {
			page = tail
		} else {
			omitted = 0
		}
	}
	out := make([]tool.IssueComment, 0, len(page.Comments))
	for _, comment := range page.Comments {
		out = append(out, tool.IssueComment{
			AuthorKind: comment.AuthorKind,
			Body:       comment.Body,
			CreatedAt:  comment.CreatedAt,
		})
	}
	return out, omitted
}

// Report implements tool.IssueClient.
//
// The comment is claimed as local_agent, not agent. It is a report from a
// machine this deployment did not schedule, admitted no quota for, and recorded
// no trace of, and the thread says so rather than letting a Portal reader
// believe otherwise.
func (c *localIssueClient) Report(ctx context.Context, in tool.IssueReport) error {
	token, err := c.token(c.base)
	if err != nil {
		return fmt.Errorf("authenticate to %s: %w", c.base, err)
	}
	body := in.Body
	if len(in.ArtifactIDs) > 0 {
		body = strings.TrimSpace(body + "\n\nArtifacts: " + strings.Join(in.ArtifactIDs, ", "))
	}
	payload, err := json.Marshal(createCommentPayload{Body: body, AuthorKind: coreissue.CommentAuthorLocalAgent})
	if err != nil {
		return err
	}
	// Not postNoContent: this route answers 201 with the comment it created,
	// and that helper's contract is 200 or 204.
	resp, err := c.client.do(ctx, http.MethodPost, token, c.issuePath("/comments"), "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		return httpclient.DecodeError(resp, "POST "+c.issuePath("/comments"))
	}
	return nil
}

// CommentOnIssue posts one comment on an issue, claimed as local_agent.
//
// This is the CLI's report path — the same statement the in-process
// ReportToIssue tool makes, reachable by any executor that can run a command.
// It is local_agent, not agent, for the reason Report is: a machine this
// deployment did not schedule, so the thread says so. The server bounds the
// body and, for a run, the count; this client adds no limit of its own.
func (c *Client) CommentOnIssue(ctx context.Context, token, spaceID, issueID, body string) error {
	payload, err := json.Marshal(createCommentPayload{Body: body, AuthorKind: coreissue.CommentAuthorLocalAgent})
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

// SetIssueStatus moves an issue, carrying the version it was read at.
//
// A person's action, never a tool's: status is what the space reads to plan
// around, and `done` means a person accepted the work. See
// docs/design/issue-agent-access.md section 6.
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
