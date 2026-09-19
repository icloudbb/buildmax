package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/icloudbb/buildmax/internal/infra/httpclient"
)

// Agent is the identity a caller needs to trigger one: its id, and the name a
// person types instead. The server addresses an agent only by id, so a name is
// resolved to an id here (FindAgent) before any run is created.
type Agent struct {
	ID          string `json:"id"`
	SpaceID     string `json:"space_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Task mirrors the server's TaskResponse for the fields a local caller reads.
// A task created for an agent starts its first run automatically; LastRunID
// points at the run whose status Status reflects.
type Task struct {
	ID        string    `json:"id"`
	SpaceID   string    `json:"space_id"`
	Status    string    `json:"status"`
	Input     string    `json:"input"`
	Title     string    `json:"title"`
	Output    *string   `json:"output"`
	AgentID   *string   `json:"agent_id"`
	IssueID   *string   `json:"issue_id"`
	LastRunID *string   `json:"last_run_id"`
	CreatedAt time.Time `json:"created_at"`
}

// ListAgents returns the agents defined in one space.
func (c *Client) ListAgents(ctx context.Context, token, spaceID string) ([]Agent, error) {
	var out []Agent
	path := "/api/spaces/" + url.PathEscape(spaceID) + "/agents"
	if err := c.getJSON(ctx, token, path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// FindAgent resolves an agent by id or name to the space that holds it.
//
// When spaceID is empty it fans out across the caller's spaces, the way
// FindIssue does, because a login names a person and their agents are spread
// across every space they are in, with no cross-space listing. A name matched
// in more than one space is ambiguous rather than guessed: triggering the wrong
// agent is worse than asking the caller to name the space.
func (c *Client) FindAgent(ctx context.Context, token, spaceID, ref string) (Agent, error) {
	var spaceIDs []string
	if spaceID != "" {
		spaceIDs = []string{spaceID}
	} else {
		spaces, err := c.ListSpaces(ctx, token)
		if err != nil {
			return Agent{}, fmt.Errorf("list spaces: %w", err)
		}
		for _, s := range spaces {
			spaceIDs = append(spaceIDs, s.ID)
		}
	}
	var matches []Agent
	for _, id := range spaceIDs {
		agents, err := c.ListAgents(ctx, token, id)
		if err != nil {
			// A space that cannot be read is skipped rather than failing the
			// lookup, the same trade the issue inbox makes.
			continue
		}
		for _, a := range agents {
			if a.ID == ref || a.Name == ref {
				a.SpaceID = id
				matches = append(matches, a)
			}
		}
	}
	switch len(matches) {
	case 0:
		return Agent{}, fmt.Errorf("no agent %q in %s", ref, agentScopeLabel(spaceID))
	case 1:
		return matches[0], nil
	default:
		return Agent{}, fmt.Errorf("agent %q is ambiguous across %d spaces; pass --space to choose one", ref, len(matches))
	}
}

func agentScopeLabel(spaceID string) string {
	if spaceID != "" {
		return "space " + spaceID
	}
	return "any space you belong to"
}

// TriggerAgent creates a task for an agent and returns it. Creating the task
// starts the agent's first run; the returned Task's LastRunID names it.
func (c *Client) TriggerAgent(ctx context.Context, token, spaceID, agentID, input string) (Task, error) {
	payload, err := json.Marshal(map[string]string{"input": input})
	if err != nil {
		return Task{}, err
	}
	path := "/api/spaces/" + url.PathEscape(spaceID) + "/agents/" + url.PathEscape(agentID) + "/tasks"
	resp, err := c.do(ctx, http.MethodPost, token, path, "application/json", bytes.NewReader(payload))
	if err != nil {
		return Task{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		return Task{}, httpclient.DecodeError(resp, "POST "+path)
	}
	var out Task
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Task{}, err
	}
	return out, nil
}

// GetTask reads one task's current status and output.
func (c *Client) GetTask(ctx context.Context, token, spaceID, taskID string) (Task, error) {
	var out Task
	path := "/api/spaces/" + url.PathEscape(spaceID) + "/tasks/" + url.PathEscape(taskID)
	if err := c.getJSON(ctx, token, path, &out); err != nil {
		return Task{}, err
	}
	return out, nil
}

// FindTask resolves a task id to the space that holds it, fanning out across the
// caller's spaces when spaceID is empty — the same reason FindAgent does.
func (c *Client) FindTask(ctx context.Context, token, spaceID, taskID string) (Task, error) {
	if spaceID != "" {
		return c.GetTask(ctx, token, spaceID, taskID)
	}
	spaces, err := c.ListSpaces(ctx, token)
	if err != nil {
		return Task{}, fmt.Errorf("list spaces: %w", err)
	}
	for _, s := range spaces {
		if task, err := c.GetTask(ctx, token, s.ID, taskID); err == nil && task.ID != "" {
			return task, nil
		}
	}
	return Task{}, fmt.Errorf("no space you belong to has task %s", taskID)
}
