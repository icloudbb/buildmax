package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Fixture names the execution fixtures look up again, so a rename here cannot
// leave --runs dispatching against a resource the data fixtures never made.
const (
	fixtureDAGWorkflow  = "QA Release Readiness"
	fixtureBlockedAgent = "QA Blocked Agent"
	fixtureDAGInput     = `{"milestone":"v0.2 fixture milestone","focus":"install"}`
)

// seedFeatureFixtures fills the surfaces the curated QA space would otherwise
// show only in their simplest shape: a branching Workflow with a typed input
// and structured output, Workflows with human requests and retry/timeout
// policy, Workflow-executor schedules, an Agent with revision history and every
// definition field set, non-default Space settings, and artifact share links. It follows the Secret and plugin fixtures because the
// configured Agent names both.
func seedFeatureFixtures(ctx context.Context, client *http.Client, target smokeTarget, spaceID, token, writer, reviewer, reviewWorkflow string) error {
	base := target.apiBase + "/api/spaces/" + url.PathEscape(spaceID)
	dagID, err := ensureFixtureGraphWorkflows(ctx, client, base, token, writer, reviewer)
	if err != nil {
		return err
	}
	if err := ensureFixtureWorkflowSchedules(ctx, client, base, token, dagID, reviewWorkflow); err != nil {
		return err
	}
	if err := ensureFixtureSpaceSettings(ctx, client, base, token); err != nil {
		return err
	}
	if err := ensureFixtureConfiguredAgents(ctx, client, target, spaceID, base, token); err != nil {
		return err
	}
	blocked, err := fixtureAgentByName(ctx, client, base, token, fixtureBlockedAgent)
	if err != nil {
		return err
	}
	if err := ensureFixturePolicyWorkflows(ctx, client, base, token, writer, reviewer, blocked); err != nil {
		return err
	}
	return ensureFixtureArtifactShares(ctx, client, target, base, token)
}

// ensureNamedWorkflow creates a Workflow by name when it is missing and moves it
// to status. An existing definition is left as it is, like every other fixture.
func ensureNamedWorkflow(ctx context.Context, client *http.Client, base, token, name, description string, definition any, status string) (string, error) {
	var list fxWorkflowList
	if err := requestJSON(ctx, client, http.MethodGet, base+"/workflows", token, nil, &list, http.StatusOK); err != nil {
		return "", err
	}
	id := ""
	for _, w := range list.Workflows {
		if w.Name == name {
			id = w.ID
		}
	}
	if id == "" {
		raw, err := json.Marshal(definition)
		if err != nil {
			return "", err
		}
		var created fxWorkflow
		if err := requestJSON(ctx, client, http.MethodPost, base+"/workflows", token, map[string]string{"name": name, "description": description, "definition": string(raw)}, &created, http.StatusCreated); err != nil {
			return "", fmt.Errorf("create workflow %q: %w", name, err)
		}
		id = created.ID
		fmt.Printf("  [BuildMax QA] workflow %q: created\n", name)
	}
	var current struct {
		Status string `json:"status"`
	}
	if err := requestJSON(ctx, client, http.MethodGet, base+"/workflows/"+id, token, nil, &current, http.StatusOK); err != nil {
		return "", err
	}
	if current.Status != status {
		if err := requestJSON(ctx, client, http.MethodPatch, base+"/workflows/"+id, token, map[string]string{"status": status}, nil, http.StatusOK); err != nil {
			return "", fmt.Errorf("move workflow %q to %s: %w", name, status, err)
		}
	}
	return id, nil
}

type fxNode = map[string]any

func fixtureNode(id, agentID, instruction string, needs []string, bindings []map[string]string, issueAccess string) fxNode {
	node := fxNode{"id": id, "type": "agent_task", "agent": map[string]string{"id": agentID}, "input": map[string]any{"instruction": instruction}}
	if len(needs) > 0 {
		node["needs"] = needs
	}
	if len(bindings) > 0 {
		node["input"].(map[string]any)["bindings"] = bindings
	}
	if issueAccess != "" {
		node["issue_access"] = issueAccess
	}
	return node
}

func fixtureBinding(name, source, pointer string) map[string]string {
	return map[string]string{"name": name, "source": source, "pointer": pointer}
}

// ensureFixtureGraphWorkflows seeds two published graphs beside the linear QA
// Workflows. "QA Release Readiness" fans out to three parallel checks under a
// concurrency limit of two and joins them again; it takes a typed input and is
// executable on the mock, so --runs runs and cancels it. "QA Triage Classifier"
// carries an output_schema and a required Issue node: it is for the editor and
// admission rules only, since the scripted mock never answers with JSON.
func ensureFixtureGraphWorkflows(ctx context.Context, client *http.Client, base, token, writer, reviewer string) (string, error) {
	const input = "workflow.input"
	readiness := map[string]any{
		"schema_version": 1,
		"input_schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"milestone": map[string]string{"type": "string", "description": "The release being checked."},
				"focus":     map[string]any{"type": "string", "enum": []string{"docs", "install", "upgrade"}, "description": "The area to check most closely."},
			},
			"required":             []string{"milestone"},
			"additionalProperties": false,
		},
		"policy": map[string]int{"max_parallel_nodes": 2},
		"nodes": []fxNode{
			fixtureNode("plan", writer, "Plan the readiness checks for this milestone.", nil,
				[]map[string]string{fixtureBinding("milestone", input, "/milestone"), fixtureBinding("focus", input, "/focus")}, "if_bound"),
			fixtureNode("docs", writer, "Check the user documentation against the plan.", []string{"plan"},
				[]map[string]string{fixtureBinding("plan", "node.plan.output", "/text")}, ""),
			fixtureNode("install", reviewer, "Check a clean installation against the plan.", []string{"plan"},
				[]map[string]string{fixtureBinding("plan", "node.plan.output", "/text")}, ""),
			fixtureNode("upgrade", reviewer, "Check an upgrade from the previous release against the plan.", []string{"plan"},
				[]map[string]string{fixtureBinding("plan", "node.plan.output", "/text")}, ""),
			fixtureNode("summary", writer, "Summarize the three checks into one readiness verdict.", []string{"docs", "install", "upgrade"},
				[]map[string]string{
					fixtureBinding("docs", "node.docs.output", "/text"),
					fixtureBinding("install", "node.install.output", "/text"),
					fixtureBinding("upgrade", "node.upgrade.output", "/text"),
				}, "if_bound"),
		},
		"result": map[string]string{"source": "node.summary.output", "pointer": "/text"},
	}
	dagID, err := ensureNamedWorkflow(ctx, client, base, token, fixtureDAGWorkflow, "Synthetic fan-out/fan-in readiness check with a typed input.", readiness, "published")
	if err != nil {
		return "", err
	}

	classify := fixtureNode("classify", reviewer, "Classify the Issue by severity and summarize it in one sentence.", nil, nil, "required")
	classify["output_schema"] = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"severity": map[string]any{"type": "string", "enum": []string{"low", "medium", "high"}},
			"summary":  map[string]string{"type": "string"},
		},
		"required":             []string{"severity", "summary"},
		"additionalProperties": false,
	}
	triage := map[string]any{
		"schema_version": 1,
		"nodes": []fxNode{
			classify,
			fixtureNode("route", writer, "Draft the next step for an Issue of this severity.", []string{"classify"},
				[]map[string]string{fixtureBinding("severity", "node.classify.output", "/structured/severity")}, "none"),
		},
		"result": map[string]string{"source": "node.classify.output", "pointer": "/structured"},
	}
	if _, err := ensureNamedWorkflow(ctx, client, base, token, "QA Triage Classifier", "Synthetic structured-output classifier that requires an Issue.", triage, "published"); err != nil {
		return "", err
	}
	return dagID, nil
}

// ensureFixtureWorkflowSchedules adds Workflow-executor schedules to the QA
// space: one enabled weekly with typed input, one paused with none. The agent
// schedules in volume stay in the pagination space.
func ensureFixtureWorkflowSchedules(ctx context.Context, client *http.Client, base, token, dagID, reviewID string) error {
	existing, err := fixturePage[fxSchedule](ctx, client, base+"/schedules", token, "schedules")
	if err != nil {
		return err
	}
	byName := map[string]fxSchedule{}
	for _, s := range existing {
		byName[s.Name] = s
	}
	for _, spec := range []struct {
		name, workflowID, input, cron, zone string
		enabled                             bool
	}{
		{"QA Weekly Readiness", dagID, fixtureDAGInput, "0 9 * * 1", "Asia/Shanghai", true},
		{"QA Nightly Review", reviewID, "", "0 2 * * *", "UTC", false},
	} {
		found, ok := byName[spec.name]
		if !ok {
			body := map[string]string{"executor_kind": "workflow", "executor_id": spec.workflowID, "name": spec.name, "input": spec.input, "cron_expr": spec.cron, "timezone": spec.zone}
			if err := requestJSON(ctx, client, http.MethodPost, base+"/schedules", token, body, &found, http.StatusCreated); err != nil {
				return fmt.Errorf("create schedule %q: %w", spec.name, err)
			}
		}
		if found.Enabled != spec.enabled {
			if err := requestJSON(ctx, client, http.MethodPatch, base+"/schedules/"+url.PathEscape(found.ID), token, map[string]bool{"enabled": spec.enabled}, nil, http.StatusOK); err != nil {
				return err
			}
		}
	}
	return nil
}

// ensureFixtureSpaceSettings moves the QA space off both defaults: a registries
// network tier for its agents and curated plugin activation. Settings someone
// already changed are left alone.
func ensureFixtureSpaceSettings(ctx context.Context, client *http.Client, base, token string) error {
	var sandbox struct {
		Network    string `json:"sandbox_network_tier"`
		Filesystem string `json:"sandbox_filesystem_tier"`
	}
	if err := requestJSON(ctx, client, http.MethodGet, base+"/sandbox-defaults", token, nil, &sandbox, http.StatusOK); err != nil {
		return err
	}
	if sandbox.Network == "" && sandbox.Filesystem == "" {
		body := map[string]string{"sandbox_network_tier": "registries", "sandbox_filesystem_tier": "workspace"}
		if err := requestJSON(ctx, client, http.MethodPut, base+"/sandbox-defaults", token, body, nil, http.StatusOK); err != nil {
			return fmt.Errorf("set sandbox defaults: %w", err)
		}
	}
	var activations struct {
		Curation string `json:"curation"`
	}
	if err := requestJSON(ctx, client, http.MethodGet, base+"/plugin-activations", token, nil, &activations, http.StatusOK); err != nil {
		return err
	}
	if activations.Curation == "" || activations.Curation == "open" {
		if err := requestJSON(ctx, client, http.MethodPut, base+"/plugin-curation", token, map[string]string{"curation": "curated"}, nil, http.StatusOK); err != nil {
			return fmt.Errorf("set plugin curation: %w", err)
		}
	}
	return nil
}

type fxAgentDetail struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
}

// ensureFixtureConfiguredAgents seeds the two Agents whose definitions use
// every field the fixture writers leave empty. "QA Release Engineer" is walked
// to its third revision so revision history and restore have something to
// show. "QA Blocked Agent" requires an item of the disabled Secret, so its runs
// fail before the Agent starts; --runs relies on that for a FAILED Task.
func ensureFixtureConfiguredAgents(ctx context.Context, client *http.Client, target smokeTarget, spaceID, base, token string) error {
	secrets, err := fixtureSecretIDs(ctx, client, base, token)
	if err != nil {
		return err
	}
	demo, disabled := secrets["fixture-demo"], secrets["fixture-disabled"]
	if demo == "" || disabled == "" {
		return fmt.Errorf("configured agents need the fixture-demo and fixture-disabled Secrets")
	}

	const name = "QA Release Engineer"
	description := "Checks release readiness with the code-review plugin."
	id, err := ensureAgent(ctx, client, target, spaceID, token, "BuildMax QA", name, description,
		"Check the release against fixtures/docs/brief.md.")
	if err != nil {
		return err
	}
	// Each revision past the first is a whole definition, as the API requires;
	// the list is the history a rerun completes from wherever it stopped.
	revisions := []map[string]any{
		{"name": name, "description": description,
			"instructions": "Check the release against fixtures/docs/brief.md. List blocking findings first."},
		{"name": name, "description": description,
			"instructions":            "Check the release against fixtures/docs/brief.md. List blocking findings first, then known limitations.",
			"plugins":                 []string{fixtureActivatedPlugin},
			"sandbox_network_tier":    "open",
			"sandbox_filesystem_tier": "workspace",
			"secret_consumption": map[string]any{"env": []map[string]any{
				{"secret": demo, "item": "token", "env_name": "QA_FIXTURE_TOKEN"},
				{"secret": demo, "item": "endpoint", "env_name": "QA_FIXTURE_ENDPOINT", "optional": true},
			}}},
	}
	var agent fxAgentDetail
	if err := requestJSON(ctx, client, http.MethodGet, base+"/agents/"+url.PathEscape(id), token, nil, &agent, http.StatusOK); err != nil {
		return err
	}
	for agent.Revision >= 1 && agent.Revision <= len(revisions) {
		if err := requestJSON(ctx, client, http.MethodPatch, base+"/agents/"+url.PathEscape(id), token, revisions[agent.Revision-1], &agent, http.StatusOK); err != nil {
			return fmt.Errorf("revise agent %q: %w", name, err)
		}
		fmt.Printf("    agent %q: revision %d\n", name, agent.Revision)
	}

	var list []fxAgent
	if err := requestJSON(ctx, client, http.MethodGet, base+"/agents", token, nil, &list, http.StatusOK); err != nil {
		return err
	}
	for _, a := range list {
		if a.Name == fixtureBlockedAgent {
			return nil
		}
	}
	body := map[string]any{
		"name":         fixtureBlockedAgent,
		"description":  "Requires a disabled Secret, so every run fails before it starts.",
		"instructions": "Report the configured endpoint.",
		"secret_consumption": map[string]any{"env": []map[string]any{
			{"secret": disabled, "item": "token", "env_name": "QA_DISABLED_TOKEN"},
		}},
	}
	if err := requestJSON(ctx, client, http.MethodPost, base+"/agents", token, body, nil, http.StatusCreated); err != nil {
		return fmt.Errorf("create agent %q: %w", fixtureBlockedAgent, err)
	}
	fmt.Printf("  [BuildMax QA] agent %q: created\n", fixtureBlockedAgent)
	return nil
}

func fixtureSecretIDs(ctx context.Context, client *http.Client, base, token string) (map[string]string, error) {
	var list struct {
		Secrets []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"secrets"`
	}
	if err := requestJSON(ctx, client, http.MethodGet, base+"/secrets", token, nil, &list, http.StatusOK); err != nil {
		return nil, err
	}
	ids := make(map[string]string, len(list.Secrets))
	for _, s := range list.Secrets {
		ids[s.Name] = s.ID
	}
	return ids, nil
}

type fxShare struct {
	ShareID   string     `json:"share_id"`
	ExpiresAt *time.Time `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at"`
}

func (s fxShare) live(now time.Time) bool {
	return s.RevokedAt == nil && (s.ExpiresAt == nil || s.ExpiresAt.After(now))
}

// ensureFixtureArtifactShares gives the HTML artifact a live public link and
// the report a revoked one, so both the share list states and the public
// /shared page have data. Links expire, so a rerun replaces a lapsed one.
func ensureFixtureArtifactShares(ctx context.Context, client *http.Client, target smokeTarget, base, token string) error {
	artifacts, err := fixturePage[struct {
		ID       string `json:"id"`
		Filename string `json:"filename"`
	}](ctx, client, base+"/artifacts", token, "items")
	if err != nil {
		return err
	}
	ids := map[string]string{}
	for _, a := range artifacts {
		ids[a.Filename] = a.ID
	}
	now := time.Now()
	for _, spec := range []struct {
		filename string
		revoked  bool
	}{{"fixture-dashboard.html", false}, {"fixture-report.txt", true}} {
		id := ids[spec.filename]
		if id == "" {
			return fmt.Errorf("artifact %s is missing", spec.filename)
		}
		sharesURL := target.apiBase + "/api/artifacts/" + url.PathEscape(id) + "/shares"
		var shares struct {
			Items []fxShare `json:"items"`
		}
		if err := requestJSON(ctx, client, http.MethodGet, sharesURL, token, nil, &shares, http.StatusOK); err != nil {
			return err
		}
		done := false
		for _, s := range shares.Items {
			if spec.revoked && s.RevokedAt != nil || !spec.revoked && s.live(now) {
				done = true
			}
		}
		if done {
			continue
		}
		var created struct {
			ShareID string `json:"share_id"`
			URL     string `json:"url"`
		}
		if err := requestJSON(ctx, client, http.MethodPost, sharesURL, token, nil, &created, http.StatusCreated); err != nil {
			return fmt.Errorf("share %s: %w", spec.filename, err)
		}
		if spec.revoked {
			response, err := request(ctx, client, http.MethodDelete, sharesURL+"/"+url.PathEscape(created.ShareID), token, "", nil, http.StatusNoContent)
			if err != nil {
				return fmt.Errorf("revoke the %s share: %w", spec.filename, err)
			}
			if err := response.Close(); err != nil {
				return err
			}
			continue
		}
		// The link token is shown only on creation; print it so the public page
		// can be opened without minting another.
		fmt.Printf("    artifact %s shared at %s\n", spec.filename, created.URL)
	}
	return nil
}
