package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// Bulk fixture volumes. These fill the list surfaces Portal paginates or renders
// unbounded so a developer can see paging, "Load more", scrolling, and filters
// against realistic counts. They are tuned so each server-paginated list spans
// more than one page: Issues page at 10 (the 105 pagination issues), Artifacts
// and the admin Accounts page at 50.
const (
	fixtureAgentCount    = 12 // agents in the pagination Space
	fixtureWorkflowCount = 9  // workflows in the pagination Space, mixed status
	fixtureScheduleCount = 8  // agent schedules, some paused
	fixtureBulkSecrets   = 8  // extra secrets beyond the two curated ones
	fixtureArtifactCount = 60 // artifacts, to cross the 50-item "Load more"
	fixtureAccountCount  = 60 // admin accounts, to cross the 50-per-page listing
	fixtureWebhookCount  = 5  // account webhook keys
)

// fxAccount is the subset of an admin account listing the fixtures reconcile.
type fxAccount struct {
	ID         string  `json:"id"`
	Email      string  `json:"email"`
	DisabledAt *string `json:"disabled_at"`
}

// fxSchedule is the subset of a schedule listing the fixtures reconcile.
type fxSchedule struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// seedPaginationBulk fills the pagination Space's list surfaces with volume:
// agents, workflows, artifacts, secrets, and schedules. Schedules and workflows
// target agents in this same Space, so the agents are seeded first.
func seedPaginationBulk(ctx context.Context, client *http.Client, target smokeTarget, spaceID, token string) error {
	base := target.apiBase + "/api/spaces/" + url.PathEscape(spaceID)
	agentIDs, err := ensureFixtureAgents(ctx, client, target, spaceID, token)
	if err != nil {
		return err
	}
	if err := ensureFixtureBulkWorkflows(ctx, client, base, token, agentIDs); err != nil {
		return err
	}
	if err := ensureBulkArtifacts(ctx, client, base, token); err != nil {
		return err
	}
	if err := ensureBulkSecrets(ctx, client, base, token); err != nil {
		return err
	}
	return ensureFixtureSchedules(ctx, client, base, token, agentIDs)
}

// ensureFixtureAgents seeds a spread of agents in one Space and returns their
// IDs so workflows and schedules can target them.
func ensureFixtureAgents(ctx context.Context, client *http.Client, target smokeTarget, spaceID, token string) ([]string, error) {
	roles := []string{"triage", "docs", "review", "planning"}
	ids := make([]string, 0, fixtureAgentCount)
	for i := 1; i <= fixtureAgentCount; i++ {
		role := roles[(i-1)%len(roles)]
		id, err := ensureAgent(ctx, client, target, spaceID, token, "QA Pagination",
			fmt.Sprintf("QA Bulk Agent %02d", i),
			fmt.Sprintf("Synthetic %s agent for list-volume testing.", role),
			fmt.Sprintf("You are a synthetic %s agent. Use only the provided fixture files.", role))
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// ensureFixtureBulkWorkflows seeds numbered workflows across the three lifecycle
// states so the Workflows list has draft, published, and archived rows in volume.
func ensureFixtureBulkWorkflows(ctx context.Context, client *http.Client, base, token string, agentIDs []string) error {
	if len(agentIDs) < 2 {
		return fmt.Errorf("bulk workflows need at least two agents to target")
	}
	var list fxWorkflowList
	if err := requestJSON(ctx, client, http.MethodGet, base+"/workflows", token, nil, &list, http.StatusOK); err != nil {
		return err
	}
	byName := map[string]string{}
	for _, w := range list.Workflows {
		byName[w.Name] = w.ID
	}
	definition := fmt.Sprintf(`{"schema_version":1,"nodes":[{"id":"draft","type":"agent_task","agent":{"id":%q},"input":{"instruction":"Draft a concise QA plan."}},{"id":"review","type":"agent_task","needs":["draft"],"agent":{"id":%q},"input":{"instruction":"Review the previous result for missing criteria."}}]}`, agentIDs[0], agentIDs[1])
	statuses := []string{"draft", "published", "archived"}
	for i := 1; i <= fixtureWorkflowCount; i++ {
		name := fmt.Sprintf("QA Bulk Workflow %02d", i)
		id := byName[name]
		if id == "" {
			var created fxWorkflow
			if err := requestJSON(ctx, client, http.MethodPost, base+"/workflows", token, map[string]string{"name": name, "description": "Synthetic workflow for list-volume testing.", "definition": definition}, &created, http.StatusCreated); err != nil {
				return err
			}
			id = created.ID
		}
		// A workflow is born draft; move it to its target lifecycle state.
		want := statuses[(i-1)%len(statuses)]
		var current struct {
			Status string `json:"status"`
		}
		if err := requestJSON(ctx, client, http.MethodGet, base+"/workflows/"+id, token, nil, &current, http.StatusOK); err != nil {
			return err
		}
		if current.Status != want {
			if err := requestJSON(ctx, client, http.MethodPatch, base+"/workflows/"+id, token, map[string]string{"status": want}, nil, http.StatusOK); err != nil {
				return err
			}
		}
	}
	return nil
}

// ensureBulkArtifacts uploads numbered artifacts past the 50-item threshold so
// the Artifacts list's "Load more" control has a second page to fetch.
func ensureBulkArtifacts(ctx context.Context, client *http.Client, base, token string) error {
	existing, err := fixturePage[struct {
		Filename string `json:"filename"`
	}](ctx, client, base+"/artifacts", token, "items")
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, artifact := range existing {
		seen[artifact.Filename] = true
	}
	for i := 1; i <= fixtureArtifactCount; i++ {
		name := fmt.Sprintf("fixture-bulk-%03d.txt", i)
		if seen[name] {
			continue
		}
		if err := uploadFixtureArtifact(ctx, client, base, token, name, fmt.Sprintf("Synthetic bulk artifact %03d for the Artifacts load-more control.\n", i)); err != nil {
			return err
		}
	}
	return nil
}

// ensureBulkSecrets adds numbered secrets, one in three disabled, so the Secrets
// list has both states in volume.
func ensureBulkSecrets(ctx context.Context, client *http.Client, base, token string) error {
	specs := make([]fixtureSecret, 0, fixtureBulkSecrets)
	for i := 1; i <= fixtureBulkSecrets; i++ {
		state := "active"
		if i%3 == 0 {
			state = "disabled"
		}
		specs = append(specs, fixtureSecret{name: fmt.Sprintf("fixture-bulk-secret-%02d", i), state: state})
	}
	return ensureSecrets(ctx, client, base, token, specs)
}

// ensureFixtureSchedules seeds recurring schedules across the Space's agents with
// varied cron expressions and timezones. Every third schedule is paused so the
// list shows both enabled and paused rows. Schedules only register a firing
// time; they do not execute here.
func ensureFixtureSchedules(ctx context.Context, client *http.Client, base, token string, agentIDs []string) error {
	if len(agentIDs) == 0 {
		return nil
	}
	existing, err := fixturePage[fxSchedule](ctx, client, base+"/schedules", token, "schedules")
	if err != nil {
		return err
	}
	byName := map[string]fxSchedule{}
	for _, s := range existing {
		byName[s.Name] = s
	}
	crons := []string{"*/15 * * * *", "0 9 * * 1-5", "30 6 * * *", "0 0 1 * *", "0 */6 * * *"}
	zones := []string{"UTC", "America/New_York", "Asia/Shanghai", "Europe/London"}
	for i := 1; i <= fixtureScheduleCount; i++ {
		name := fmt.Sprintf("QA Schedule %02d", i)
		found, ok := byName[name]
		if !ok {
			body := map[string]string{
				"agent_id":  agentIDs[(i-1)%len(agentIDs)],
				"name":      name,
				"input":     fmt.Sprintf("[kind fixture] Scheduled run %02d over the synthetic QA workspace.", i),
				"cron_expr": crons[(i-1)%len(crons)],
				"timezone":  zones[(i-1)%len(zones)],
			}
			if err := requestJSON(ctx, client, http.MethodPost, base+"/schedules", token, body, &found, http.StatusCreated); err != nil {
				return err
			}
		}
		wantEnabled := i%3 != 0
		if found.Enabled != wantEnabled {
			if err := requestJSON(ctx, client, http.MethodPatch, base+"/schedules/"+url.PathEscape(found.ID), token, map[string]bool{"enabled": wantEnabled}, nil, http.StatusOK); err != nil {
				return err
			}
		}
	}
	return nil
}

// seedFixtureAccounts creates a cohort of accounts through the admin API so the
// Accounts page spans more than one page and its status filter has a disabled
// cohort. Each account also gets a personal Space, which populates the admin
// Spaces list too. It needs the caller to hold System Administrator authority.
func seedFixtureAccounts(ctx context.Context, client *http.Client, target smokeTarget, token string) error {
	base := target.apiBase + "/api/admin/users"
	existing, err := fixturePage[fxAccount](ctx, client, base, token, "users")
	if err != nil {
		return err
	}
	byEmail := map[string]fxAccount{}
	for _, a := range existing {
		byEmail[a.Email] = a
	}
	for i := 1; i <= fixtureAccountCount; i++ {
		email := fmt.Sprintf("qa-account-%03d@buildmax.local", i)
		acct, ok := byEmail[email]
		if !ok {
			if err := requestJSON(ctx, client, http.MethodPost, base, token, map[string]string{"email": email}, &acct, http.StatusCreated); err != nil {
				return err
			}
		}
		// Disable every eighth account so the status filter has a cohort to show.
		wantDisabled := i%8 == 0
		if (acct.DisabledAt != nil) != wantDisabled {
			if err := requestJSON(ctx, client, http.MethodPut, base+"/"+url.PathEscape(acct.ID)+"/state", token, map[string]bool{"disabled": wantDisabled}, nil, http.StatusOK); err != nil {
				return err
			}
		}
	}
	fmt.Printf("  admin accounts: %d synthetic accounts (every eighth disabled)\n", fixtureAccountCount)
	return nil
}

// ensureFixtureWebhookKeys seeds account-scoped webhook keys so the account
// settings list has rows. The plaintext key is shown once and discarded.
func ensureFixtureWebhookKeys(ctx context.Context, client *http.Client, target smokeTarget, token string) error {
	base := target.apiBase + "/api/webhook-keys"
	var existing struct {
		Keys []struct {
			Name string `json:"name"`
		} `json:"keys"`
	}
	if err := requestJSON(ctx, client, http.MethodGet, base, token, nil, &existing, http.StatusOK); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, k := range existing.Keys {
		seen[k.Name] = true
	}
	for i := 1; i <= fixtureWebhookCount; i++ {
		name := fmt.Sprintf("QA Webhook Key %02d", i)
		if seen[name] {
			continue
		}
		if err := requestJSON(ctx, client, http.MethodPost, base, token, map[string]string{"name": name}, nil, http.StatusCreated); err != nil {
			return err
		}
	}
	return nil
}
