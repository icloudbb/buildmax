package workerclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/icloudbb/buildmax/internal/infra/httpclient"
	"github.com/icloudbb/buildmax/internal/tool"
)

type issueClient struct {
	Cfg       WorkerAPIClientConfig
	TaskRunID string
}

// NewIssueClient scopes a run's agent to the one Issue its task names.
//
// The Issue is not a parameter here either: the server derives it from the run
// token, so this client cannot be pointed at another Issue even by the code
// holding it. See docs/design/agent-bridge-cli.md section 3.
func NewIssueClient(cfg WorkerAPIClientConfig, taskRunID string) tool.IssueClient {
	return &issueClient{Cfg: cfg, TaskRunID: taskRunID}
}

type issueChildPayload struct {
	Title  string `json:"title"`
	Status string `json:"status"`
}

type issueCommentPayload struct {
	AuthorKind string    `json:"author_kind"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
}

type issuePayload struct {
	Title           string                `json:"title"`
	Description     string                `json:"description"`
	Status          string                `json:"status"`
	ExecutorKind    string                `json:"executor_kind"`
	Children        []issueChildPayload   `json:"children"`
	Comments        []issueCommentPayload `json:"comments"`
	OmittedComments int                   `json:"omitted_comments"`
}

type issueCommentRequest struct {
	Body        string   `json:"body"`
	ArtifactIDs []string `json:"artifact_ids,omitempty"`
}

// Issue implements tool.IssueClient.
func (c *issueClient) Issue(ctx context.Context) (tool.IssueSnapshot, error) {
	path := "/api/worker/task-runs/" + url.PathEscape(c.TaskRunID) + "/issue"
	resp, err := workerDo(ctx, c.Cfg, http.MethodGet, path, nil)
	if err != nil {
		return tool.IssueSnapshot{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return tool.IssueSnapshot{}, httpclient.DecodeError(resp, "worker API GET "+path)
	}
	var payload issuePayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return tool.IssueSnapshot{}, err
	}
	out := tool.IssueSnapshot{
		Title:           payload.Title,
		Description:     payload.Description,
		Status:          payload.Status,
		ExecutorKind:    payload.ExecutorKind,
		OmittedComments: payload.OmittedComments,
	}
	for _, child := range payload.Children {
		out.Children = append(out.Children, tool.IssueChild{Title: child.Title, Status: child.Status})
	}
	for _, comment := range payload.Comments {
		out.Comments = append(out.Comments, tool.IssueComment{
			AuthorKind: comment.AuthorKind,
			Body:       comment.Body,
			CreatedAt:  comment.CreatedAt,
		})
	}
	return out, nil
}

// Report implements tool.IssueClient.
func (c *issueClient) Report(ctx context.Context, in tool.IssueReport) error {
	body, err := json.Marshal(issueCommentRequest{Body: in.Body, ArtifactIDs: in.ArtifactIDs})
	if err != nil {
		return err
	}
	path := "/api/worker/task-runs/" + url.PathEscape(c.TaskRunID) + "/issue/comments"
	resp, err := workerDo(ctx, c.Cfg, http.MethodPost, path, body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		return httpclient.DecodeError(resp, "worker API POST "+path)
	}
	return nil
}
