package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Governance fixtures leave the account and membership states the Beta
// identity journeys start from: a member removed after creating a schedule, a
// shared Space whose only owner is disabled, a schedule paused by failures,
// and an active member whose deactivation impact is not empty.
const (
	fixtureDepartedEmail   = "frank@buildmax.local"
	fixtureDisabledEmail   = "erin@buildmax.local"
	fixtureOrphanSpace     = "BuildMax QA Archive"
	fixtureDepartedSched   = "QA Departed Member Check"
	fixtureRetiredWorkflow = "QA Retired Check"
	fixtureRetiredSched    = "QA Retired Workflow Check"
	fixtureCarolSched      = "QA Carol Weekly Check"
	fixtureStalledInput    = "[kind fixture] Long installation check that stays RUNNING for about ten minutes."
	// everyMinute makes the two schedules that must pause do so within minutes.
	// Neither reaches a model: one pauses before its first claim, the other
	// fails to start because its Workflow is archived.
	everyMinute = "* * * * *"
)

func seedGovernanceFixtures(ctx context.Context, client *http.Client, target smokeTarget, qaID, token, writer string) error {
	base := target.apiBase + "/api/spaces/" + url.PathEscape(qaID)
	schedules, err := fixturePage[fxSchedule](ctx, client, base+"/schedules", token, "schedules")
	if err != nil {
		return err
	}
	byName := map[string]fxSchedule{}
	for _, s := range schedules {
		byName[s.Name] = s
	}
	if err := ensureFixtureCarolSchedule(ctx, client, target, base, writer, byName); err != nil {
		return err
	}
	if err := ensureFixtureDepartedMember(ctx, client, target, base, token, byName); err != nil {
		return err
	}
	if err := ensureFixtureRetiredSchedule(ctx, client, base, token, byName); err != nil {
		return err
	}
	return ensureFixtureDisabledOwner(ctx, client, target, token)
}

func postFixtureSchedule(ctx context.Context, client *http.Client, base, token string, body map[string]string) error {
	if err := requestJSON(ctx, client, http.MethodPost, base+"/schedules", token, body, nil, http.StatusCreated); err != nil {
		return fmt.Errorf("create schedule %q: %w", body["name"], err)
	}
	return nil
}

// ensureFixtureCarolSchedule gives Carol, an ordinary member, an enabled
// schedule, so her deactivation impact lists work that would stop.
func ensureFixtureCarolSchedule(ctx context.Context, client *http.Client, target smokeTarget, base, writer string, existing map[string]fxSchedule) error {
	if _, ok := existing[fixtureCarolSched]; ok {
		return nil
	}
	carol, _, err := fixtureSignIn(ctx, client, target, "carol@buildmax.local")
	if err != nil {
		return err
	}
	return postFixtureSchedule(ctx, client, base, carol, map[string]string{
		"executor_kind": "agent", "executor_id": writer, "name": fixtureCarolSched,
		"input": "[kind fixture] Weekly check of the QA release brief.", "cron_expr": "0 10 * * 5", "timezone": "Europe/London",
	})
}

// ensureFixtureDepartedMember has Frank join the QA space, schedule a
// Workflow, and be removed. The dispatcher re-checks the creator at the next
// tick and pauses the schedule as creator_not_member rather than firing it.
func ensureFixtureDepartedMember(ctx context.Context, client *http.Client, target smokeTarget, base, token string, existing map[string]fxSchedule) error {
	var members []struct {
		UserID string `json:"user_id"`
		Email  string `json:"user_email"`
	}
	if err := requestJSON(ctx, client, http.MethodGet, base+"/members", token, nil, &members, http.StatusOK); err != nil {
		return err
	}
	frankID := ""
	for _, m := range members {
		if m.Email == fixtureDepartedEmail {
			frankID = m.UserID
		}
	}
	if _, ok := existing[fixtureDepartedSched]; !ok {
		ids, err := fixtureWorkflowIDs(ctx, client, base, token)
		if err != nil {
			return err
		}
		if ids[fixtureExpiringWorkflow] == "" {
			return fmt.Errorf("the departed-member schedule needs the %q Workflow", fixtureExpiringWorkflow)
		}
		if frankID, err = ensureFixtureMember(ctx, client, target, base, token, fixtureDepartedEmail, "member", true); err != nil {
			return err
		}
		frank, _, err := fixtureSignIn(ctx, client, target, fixtureDepartedEmail)
		if err != nil {
			return err
		}
		if err := postFixtureSchedule(ctx, client, base, frank, map[string]string{
			"executor_kind": "workflow", "executor_id": ids[fixtureExpiringWorkflow], "name": fixtureDepartedSched,
			"cron_expr": everyMinute, "timezone": "UTC",
		}); err != nil {
			return err
		}
	}
	if frankID == "" {
		return nil
	}
	response, err := request(ctx, client, http.MethodDelete, base+"/members/"+url.PathEscape(frankID), token, "", nil, http.StatusNoContent)
	if err != nil {
		return fmt.Errorf("remove %s from the QA space: %w", fixtureDepartedEmail, err)
	}
	fmt.Printf("  [BuildMax QA] %s removed; %q pauses as creator_not_member at the next tick\n", fixtureDepartedEmail, fixtureDepartedSched)
	return response.Close()
}

// ensureFixtureRetiredSchedule schedules a Workflow and then archives it, so
// each fire fails to start until the dispatcher pauses the schedule for
// consecutive failures, a few minutes later.
func ensureFixtureRetiredSchedule(ctx context.Context, client *http.Client, base, token string, existing map[string]fxSchedule) error {
	definition := map[string]any{
		"schema_version": 1,
		"nodes":          []fxNode{fixtureHumanNode("confirm", "Confirm the retired check still applies.", nil, nil, nil, fixtureShortTimeout)},
	}
	const description = "Synthetic Workflow archived after it was scheduled."
	if _, ok := existing[fixtureRetiredSched]; !ok {
		id, err := ensureNamedWorkflow(ctx, client, base, token, fixtureRetiredWorkflow, description, definition, "published")
		if err != nil {
			return err
		}
		if err := postFixtureSchedule(ctx, client, base, token, map[string]string{
			"executor_kind": "workflow", "executor_id": id, "name": fixtureRetiredSched,
			"cron_expr": everyMinute, "timezone": "UTC",
		}); err != nil {
			return err
		}
	}
	_, err := ensureNamedWorkflow(ctx, client, base, token, fixtureRetiredWorkflow, description, definition, "archived")
	return err
}

// ensureFixtureDisabledOwner has Erin create a shared Space she alone owns,
// with Bob as a member and an enabled schedule, and then disables her. Her
// sessions are revoked and the schedule pauses as creator_disabled, and the
// Space is left for an administrator to recover.
func ensureFixtureDisabledOwner(ctx context.Context, client *http.Client, target smokeTarget, adminToken string) error {
	accounts, err := fixturePage[fxAccount](ctx, client, target.apiBase+"/api/admin/users", adminToken, "users")
	if err != nil {
		return err
	}
	var erin fxAccount
	for _, a := range accounts {
		if a.Email == fixtureDisabledEmail {
			erin = a
		}
	}
	if erin.ID == "" {
		return fmt.Errorf("%s has no account", fixtureDisabledEmail)
	}
	if erin.DisabledAt != nil {
		return nil
	}
	token, _, err := fixtureSignIn(ctx, client, target, fixtureDisabledEmail)
	if err != nil {
		return err
	}
	var spaces []fxSpace
	if err := requestJSON(ctx, client, http.MethodGet, target.apiBase+"/api/spaces", token, nil, &spaces, http.StatusOK); err != nil {
		return err
	}
	var archive fxSpace
	for _, s := range spaces {
		if s.Name == fixtureOrphanSpace && s.PersonalForUserID == "" {
			archive = s
		}
	}
	if archive.ID == "" {
		if err := requestJSON(ctx, client, http.MethodPost, target.apiBase+"/api/spaces", token, map[string]string{"name": fixtureOrphanSpace}, &archive, http.StatusCreated); err != nil {
			return err
		}
	}
	base := target.apiBase + "/api/spaces/" + url.PathEscape(archive.ID)
	if _, err := ensureFixtureMember(ctx, client, target, base, token, "bob@buildmax.local", "member", true); err != nil {
		return err
	}
	agent, err := ensureAgent(ctx, client, target, archive.ID, token, fixtureOrphanSpace, "Archive Keeper", "Summarizes archived releases.", "Summarize the archived release notes.")
	if err != nil {
		return err
	}
	schedules, err := fixturePage[fxSchedule](ctx, client, base+"/schedules", token, "schedules")
	if err != nil {
		return err
	}
	if len(schedules) == 0 {
		if err := postFixtureSchedule(ctx, client, base, token, map[string]string{
			"executor_kind": "agent", "executor_id": agent, "name": "Archive Weekly Digest",
			"input": "[kind fixture] Summarize last week's archived releases.", "cron_expr": "0 8 * * 1", "timezone": "Asia/Shanghai",
		}); err != nil {
			return err
		}
	}
	if err := requestJSON(ctx, client, http.MethodPut, target.apiBase+"/api/admin/users/"+url.PathEscape(erin.ID)+"/state", adminToken, map[string]bool{"disabled": true}, nil, http.StatusOK); err != nil {
		return fmt.Errorf("disable %s: %w", fixtureDisabledEmail, err)
	}
	fmt.Printf("  %s disabled: sole owner of %s, which awaits owner recovery\n", fixtureDisabledEmail, fixtureOrphanSpace)
	return nil
}

// seedFixtureStalledTask leaves one of Carol's Tasks RUNNING, so the admin
// runtime view and her deactivation impact both show active work. The mock
// answers the worker's first turn with a ten-minute Bash call; after that the
// Task finishes on its own, and a rerun starts another once none is active.
func seedFixtureStalledTask(ctx context.Context, client *http.Client, target smokeTarget, base, writer string) error {
	if target.llmControlRequestsURL == "" {
		return fmt.Errorf("this stack does not publish its mock model's request log")
	}
	carol, _, err := fixtureSignIn(ctx, client, target, "carol@buildmax.local")
	if err != nil {
		return err
	}
	agentTasks := base + "/agents/" + url.PathEscape(writer) + "/tasks"
	var existing struct {
		Tasks []fxTask `json:"tasks"`
	}
	if err := requestJSON(ctx, client, http.MethodGet, agentTasks+"?limit=100", carol, nil, &existing, http.StatusOK); err != nil {
		return err
	}
	for _, t := range existing.Tasks {
		if t.Input == fixtureStalledInput && !isTerminalSmokeStatus(t.Status) {
			return nil
		}
	}
	var task fxTask
	if err := requestJSON(ctx, client, http.MethodPost, agentTasks, carol, map[string]string{"input": fixtureStalledInput}, &task, http.StatusCreated); err != nil {
		return err
	}
	var before []struct{ Body []byte }
	if err := requestJSON(ctx, client, http.MethodGet, target.llmControlRequestsURL, "", nil, &before, http.StatusOK); err != nil {
		return err
	}
	release, err := armFixtureToolCall(ctx, client, target, "Bash", map[string]any{"command": "sleep 590", "timeout": 600000}, 1)
	if err != nil {
		return err
	}
	defer release()
	// Clearing before the worker's turn would drop the arm, so wait for that
	// call to reach the mock; nothing else is calling it at this point.
	deadline := time.Now().Add(3 * time.Minute)
	for {
		var after []struct{ Body []byte }
		if err := requestJSON(ctx, client, http.MethodGet, target.llmControlRequestsURL, "", nil, &after, http.StatusOK); err != nil {
			return err
		}
		if len(after) > len(before) {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the stalled fixture Task's worker made no model call within 3m")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	if err := waitForTaskStatus(ctx, client, base+"/tasks/"+url.PathEscape(task.ID), carol, "RUNNING", time.Minute); err != nil {
		return err
	}
	fmt.Printf("    Carol's Task %s stays RUNNING for about ten minutes\n", task.ID)
	return nil
}
