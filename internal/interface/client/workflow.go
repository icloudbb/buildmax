package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/icloudbb/buildmax/internal/infra/httpclient"
)

// Workflow is the identity a caller needs to run one. Only a workflow whose
// Status is "published" can be run; "draft" and "archived" are refused by the
// server. Workflows are addressed by id, so a name is resolved to one here.
type Workflow struct {
	ID          string `json:"id"`
	SpaceID     string `json:"space_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Revision    int    `json:"revision"`
}

// WorkflowRun is the run a start creates, and the shape a status read returns.
// Status is one of pending, running, succeeded, failed, canceled (lowercase,
// unlike a task's).
type WorkflowRun struct {
	ID           string  `json:"id"`
	WorkflowID   string  `json:"workflow_id"`
	Status       string  `json:"status"`
	ErrorMessage *string `json:"error_message"`
}

// ListWorkflows returns the workflows defined in one space.
func (c *Client) ListWorkflows(ctx context.Context, token, spaceID string) ([]Workflow, error) {
	var out struct {
		Workflows []Workflow `json:"workflows"`
	}
	path := "/api/spaces/" + url.PathEscape(spaceID) + "/workflows"
	if err := c.getJSON(ctx, token, path, &out); err != nil {
		return nil, err
	}
	return out.Workflows, nil
}

// ListOwnedWorkflows returns every workflow across the caller's spaces, each
// carrying the space it belongs to, so a local surface with no current space can
// still show what is runnable.
func (c *Client) ListOwnedWorkflows(ctx context.Context, token string) ([]Workflow, []error) {
	spaces, err := c.ListSpaces(ctx, token)
	if err != nil {
		return nil, []error{fmt.Errorf("list spaces: %w", err)}
	}
	var out []Workflow
	var problems []error
	for _, s := range spaces {
		wfs, err := c.ListWorkflows(ctx, token, s.ID)
		if err != nil {
			problems = append(problems, fmt.Errorf("space %s: %w", s.Name, err))
			continue
		}
		for _, wf := range wfs {
			wf.SpaceID = s.ID
			out = append(out, wf)
		}
	}
	return out, problems
}

// FindWorkflow resolves a workflow by id or name to the space that holds it,
// fanning out across the caller's spaces when spaceID is empty. A name matched
// in more than one space is refused rather than guessed, the same as FindAgent.
func (c *Client) FindWorkflow(ctx context.Context, token, spaceID, ref string) (Workflow, error) {
	var spaceIDs []string
	if spaceID != "" {
		spaceIDs = []string{spaceID}
	} else {
		spaces, err := c.ListSpaces(ctx, token)
		if err != nil {
			return Workflow{}, fmt.Errorf("list spaces: %w", err)
		}
		for _, s := range spaces {
			spaceIDs = append(spaceIDs, s.ID)
		}
	}
	var matches []Workflow
	for _, id := range spaceIDs {
		wfs, err := c.ListWorkflows(ctx, token, id)
		if err != nil {
			continue
		}
		for _, wf := range wfs {
			if wf.ID == ref || wf.Name == ref {
				wf.SpaceID = id
				matches = append(matches, wf)
			}
		}
	}
	switch len(matches) {
	case 0:
		return Workflow{}, fmt.Errorf("no workflow %q in %s", ref, agentScopeLabel(spaceID))
	case 1:
		return matches[0], nil
	default:
		return Workflow{}, fmt.Errorf("workflow %q is ambiguous across %d spaces; pass --space to choose one", ref, len(matches))
	}
}

// RunWorkflow starts a run of a published workflow and returns it.
//
// input, when given, must be JSON satisfying the workflow's input_schema, and is
// only accepted when the workflow declares one; it is validated as JSON here so
// a malformed value fails before the request rather than as an opaque 400.
func (c *Client) RunWorkflow(ctx context.Context, token, spaceID, workflowID, input, issueID string) (WorkflowRun, error) {
	var body []byte
	if input != "" || issueID != "" {
		payload := struct {
			IssueID *string         `json:"issue_id,omitempty"`
			Input   json.RawMessage `json:"input,omitempty"`
		}{}
		if issueID != "" {
			payload.IssueID = &issueID
		}
		if input != "" {
			if !json.Valid([]byte(input)) {
				return WorkflowRun{}, fmt.Errorf("--input must be JSON satisfying the workflow's input schema")
			}
			payload.Input = json.RawMessage(input)
		}
		marshaled, err := json.Marshal(payload)
		if err != nil {
			return WorkflowRun{}, err
		}
		body = marshaled
	}
	path := "/api/spaces/" + url.PathEscape(spaceID) + "/workflows/" + url.PathEscape(workflowID) + "/runs"
	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	} else {
		reader = bytes.NewReader(nil)
	}
	resp, err := c.do(ctx, http.MethodPost, token, path, "application/json", reader)
	if err != nil {
		return WorkflowRun{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		return WorkflowRun{}, httpclient.DecodeError(resp, "POST "+path)
	}
	var wrapper struct {
		Run WorkflowRun `json:"run"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return WorkflowRun{}, err
	}
	return wrapper.Run, nil
}

// GetWorkflowRun reads one workflow run's current status.
func (c *Client) GetWorkflowRun(ctx context.Context, token, spaceID, runID string) (WorkflowRun, error) {
	var wrapper struct {
		Run WorkflowRun `json:"run"`
	}
	path := "/api/spaces/" + url.PathEscape(spaceID) + "/workflow-runs/" + url.PathEscape(runID)
	if err := c.getJSON(ctx, token, path, &wrapper); err != nil {
		return WorkflowRun{}, err
	}
	return wrapper.Run, nil
}

// FindWorkflowRun resolves a run id to the space that holds it, fanning out
// across the caller's spaces when spaceID is empty.
func (c *Client) FindWorkflowRun(ctx context.Context, token, spaceID, runID string) (WorkflowRun, error) {
	if spaceID != "" {
		return c.GetWorkflowRun(ctx, token, spaceID, runID)
	}
	spaces, err := c.ListSpaces(ctx, token)
	if err != nil {
		return WorkflowRun{}, fmt.Errorf("list spaces: %w", err)
	}
	for _, s := range spaces {
		if run, err := c.GetWorkflowRun(ctx, token, s.ID, runID); err == nil && run.ID != "" {
			return run, nil
		}
	}
	return WorkflowRun{}, fmt.Errorf("no space you belong to has workflow run %s", runID)
}
