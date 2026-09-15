package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Fixture entities are modeled by their wire JSON, not the server's internal
// types: tools/mk cannot import internal/server, and only the shape crossing the
// API matters here.
type fxIssue struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Status        string `json:"status"`
	Version       uint64 `json:"version"`
	ParentIssueID string `json:"parent_issue_id"`
	OwnerID       string `json:"owner_id"`
	ExecutorKind  string `json:"executor_kind"`
	ExecutorID    string `json:"executor_id"`
}

type fxAgent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type fxWorkflow struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type fxWorkflowList struct {
	Workflows []fxWorkflow `json:"workflows"`
}

type fixtureIssue struct {
	title       string
	description string
	status      string
	comments    []string
	parentTitle string
	// ownerID is the accountable person, independent of executorKind/executorID
	// -- both can be set on the same fixture at once.
	ownerID      string
	executorKind string
	executorID   string
}

// kindFixtures fills the running deployment through public APIs. Stable fixture
// names let interrupted runs resume without replacing unrelated test data.
func kindFixtures(withRuns bool) error {
	if err := requireCommands("kubectl"); err != nil {
		return err
	}
	cluster := kindClusterName()
	exists, err := kindClusterExists(cluster)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("kind cluster %q does not exist; run %s kind up", cluster, mk())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if withRuns {
		if err := requireFixtureMock(); err != nil {
			return err
		}
	}
	target := kindSmokeTarget()
	client := &http.Client{Timeout: 30 * time.Second}
	if err := waitForHTTP(ctx, client, target.apiBase+"/healthz", 60*time.Second); err != nil {
		return err
	}

	fmt.Printf("Seeding fixtures into %s (%s)\n", cluster, target.apiBase)

	// Accounts come first, through the operator CLI, because a personal space is
	// created with the user and every other fixture hangs off it. "already has
	// an account" is the idempotent success here, not a failure.
	for _, email := range []string{"alice@buildmax.local", "bob@buildmax.local", "carol@buildmax.local", "dave@buildmax.local"} {
		out, err := target.admin("user", "create", email)
		switch {
		case err == nil:
			fmt.Printf("  user %s: created\n", email)
		case strings.Contains(out, "already has an account"):
			fmt.Printf("  user %s: exists\n", email)
		default:
			return fmt.Errorf("create user %s: %w\n%s", email, err, out)
		}
	}

	// Alice carries the rich fixtures: an agent, a workflow that targets it, and
	// a spread of issues. Bob exists so a second account with its own personal
	// space and its own issues is present for boundary and list testing.
	if err := seedAliceFixtures(ctx, client, target); err != nil {
		return fmt.Errorf("seed alice@buildmax.local: %w", err)
	}
	if err := seedIssues(ctx, client, target, "bob@buildmax.local", []fixtureIssue{
		{title: "Triage inbound bug reports", description: "Weekly pass over new reports.", status: "in_progress"},
		{title: "Draft Q3 roadmap", description: "Collect themes from the space.", status: "todo"},
	}); err != nil {
		return fmt.Errorf("seed bob@buildmax.local: %w", err)
	}

	if err := seedTeamFixtures(ctx, client, target, withRuns); err != nil {
		return fmt.Errorf("seed team fixtures: %w", err)
	}

	fmt.Printf("\nFixtures ready. Sign in with %s kind login <email> — try alice@buildmax.local.\n", mk())
	return nil
}

func seedAliceFixtures(ctx context.Context, client *http.Client, target smokeTarget) error {
	const email = "alice@buildmax.local"
	token, spaceID, err := fixtureSignIn(ctx, client, target, email)
	if err != nil {
		return err
	}

	agentID, err := ensureAgent(ctx, client, target, spaceID, token, email,
		"Docs Writer",
		"Turns merged changes into release notes.",
		"You write concise, user-facing release notes from a list of changes.")
	if err != nil {
		return err
	}
	if err := ensureWorkflow(ctx, client, target, spaceID, token, email,
		"Release Notes", "Draft release notes for a milestone.", agentID); err != nil {
		return err
	}

	return ensureIssues(ctx, client, target, spaceID, token, email, []fixtureIssue{
		{title: "Set up CI pipeline", description: "Build, test, and lint on every PR.", status: "done"},
		{title: "Fix flaky login test", description: "Times out under load; suspect a race.", status: "in_progress",
			comments: []string{"Reproduced locally about one run in five.", "Narrowed it to the token refresh path."}},
		{title: "Write onboarding docs", description: "A new contributor should reach a green build in under an hour.", status: "todo"},
		{title: "Investigate memory leak", description: "Worker RSS climbs across long sessions.", status: "todo"},
	})
}

func seedIssues(ctx context.Context, client *http.Client, target smokeTarget, email string, specs []fixtureIssue) error {
	token, spaceID, err := fixtureSignIn(ctx, client, target, email)
	if err != nil {
		return err
	}
	return ensureIssues(ctx, client, target, spaceID, token, email, specs)
}

func ensureIssues(ctx context.Context, client *http.Client, target smokeTarget, spaceID, token, who string, specs []fixtureIssue) error {
	base := target.apiBase + "/api/spaces/" + url.PathEscape(spaceID) + "/issues"
	existing, err := fixturePage[fxIssue](ctx, client, base, token, "issues")
	if err != nil {
		return err
	}
	byTitle := make(map[string]fxIssue, len(existing))
	for _, is := range existing {
		byTitle[is.Title] = is
	}

	for _, spec := range specs {
		issue, ok := byTitle[spec.title]
		if ok {
			fmt.Printf("  [%s] issue %q: exists\n", who, spec.title)
		} else {
			body := map[string]any{"title": spec.title, "description": spec.description}
			if spec.parentTitle != "" {
				parent, found := byTitle[spec.parentTitle]
				if !found {
					return fmt.Errorf("fixture parent %q must precede %q", spec.parentTitle, spec.title)
				}
				body["parent_issue_id"] = parent.ID
			}
			if err := requestJSON(ctx, client, http.MethodPost, base, token, body, &issue, http.StatusCreated); err != nil {
				return err
			}
			fmt.Printf("  [%s] issue %q: created\n", who, spec.title)
		}

		// A create always lands in "todo", so the status move is what puts an
		// issue in the other columns. It is a read-modify-write: the PATCH must
		// echo the version the issue currently carries.
		patch := map[string]any{"version": issue.Version}
		if !ok && spec.status != "" && spec.status != issue.Status {
			patch["status"] = spec.status
		}
		if spec.ownerID != "" && issue.OwnerID != spec.ownerID {
			patch["owner_id"] = spec.ownerID
		}
		if spec.executorKind != "" && (issue.ExecutorKind != spec.executorKind || issue.ExecutorID != spec.executorID) {
			patch["executor_kind"], patch["executor_id"] = spec.executorKind, spec.executorID
		}
		if len(patch) > 1 {
			if err := requestJSON(ctx, client, http.MethodPatch, base+"/"+url.PathEscape(issue.ID), token, patch, &issue, http.StatusOK); err != nil {
				return err
			}
			fmt.Printf("    status %s; owner %s; executor %s\n", issue.Status, issue.OwnerID, issue.ExecutorKind)
		}

		byTitle[spec.title] = issue
		if len(spec.comments) > 0 {
			if err := ensureComments(ctx, client, target, spaceID, token, issue.ID, spec.comments); err != nil {
				return err
			}
		}
	}
	return nil
}

// Match each body independently so partial threads and unrelated comments do
// not prevent missing fixture comments from being added.
func ensureComments(ctx context.Context, client *http.Client, target smokeTarget, spaceID, token, issueID string, bodies []string) error {
	base := target.apiBase + "/api/spaces/" + url.PathEscape(spaceID) + "/issues/" + url.PathEscape(issueID) + "/comments"
	existing, err := fixturePage[struct {
		Body string `json:"body"`
	}](ctx, client, base, token, "comments")
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, c := range existing {
		seen[c.Body] = true
	}
	for _, body := range bodies {
		if seen[body] {
			continue
		}
		if err := requestJSON(ctx, client, http.MethodPost, base, token, map[string]any{"body": body}, nil, http.StatusCreated); err != nil {
			return err
		}
		seen[body] = true
	}
	return nil
}

func ensureAgent(ctx context.Context, client *http.Client, target smokeTarget, spaceID, token, who, name, description, instructions string) (string, error) {
	base := target.apiBase + "/api/spaces/" + url.PathEscape(spaceID) + "/agents"
	var existing []fxAgent // the agent list is a bare JSON array
	if err := requestJSON(ctx, client, http.MethodGet, base, token, nil, &existing, http.StatusOK); err != nil {
		return "", err
	}
	for _, a := range existing {
		if a.Name == name {
			fmt.Printf("  [%s] agent %q: exists\n", who, name)
			return a.ID, nil
		}
	}
	var created fxAgent
	body := map[string]any{"name": name, "description": description, "instructions": instructions}
	if err := requestJSON(ctx, client, http.MethodPost, base, token, body, &created, http.StatusCreated); err != nil {
		return "", err
	}
	fmt.Printf("  [%s] agent %q: created\n", who, name)
	return created.ID, nil
}

func ensureWorkflow(ctx context.Context, client *http.Client, target smokeTarget, spaceID, token, who, name, description, agentID string) error {
	base := target.apiBase + "/api/spaces/" + url.PathEscape(spaceID) + "/workflows"
	var existing fxWorkflowList
	if err := requestJSON(ctx, client, http.MethodGet, base, token, nil, &existing, http.StatusOK); err != nil {
		return err
	}
	for _, w := range existing.Workflows {
		if w.Name == name {
			fmt.Printf("  [%s] workflow %q: exists\n", who, name)
			return nil
		}
	}
	// One agent_task node is the minimum a definition will validate with, and it
	// must target a real agent in this space — hence the agent is seeded first.
	def := fmt.Sprintf(`{"schema_version":1,"nodes":[{"id":"draft","type":"agent_task","target_agent_id":%q,"prompt":"Draft the release notes from the merged changes."}]}`, agentID)
	body := map[string]any{"name": name, "description": description, "definition": def}
	var created fxWorkflow
	if err := requestJSON(ctx, client, http.MethodPost, base, token, body, &created, http.StatusCreated); err != nil {
		return err
	}
	fmt.Printf("  [%s] workflow %q: created\n", who, name)
	return nil
}
