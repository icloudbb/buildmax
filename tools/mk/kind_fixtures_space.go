package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
)

// Lists with a total must be exhausted: a long-lived development cluster may
// have hundreds of resources ahead of the fixtures.
func fixturePage[T any](ctx context.Context, client *http.Client, base, token, key string) ([]T, error) {
	var all []T
	for offset := 0; ; {
		var raw map[string]json.RawMessage
		endpoint := fmt.Sprintf("%s?limit=100&offset=%d", base, offset)
		if err := requestJSON(ctx, client, http.MethodGet, endpoint, token, nil, &raw, http.StatusOK); err != nil {
			return nil, err
		}
		var page []T
		var total int
		if err := json.Unmarshal(raw[key], &page); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw["total"], &total); err != nil {
			return nil, err
		}
		all = append(all, page...)
		offset += len(page)
		if offset >= total {
			return all, nil
		}
		if len(page) == 0 {
			return nil, fmt.Errorf("%s pagination stopped at %d of %d", key, offset, total)
		}
	}
}

type fxSpace struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	PersonalForUserID string `json:"personal_for_user_id"`
}

// Never assume list order identifies the personal space after team membership
// has been added. The login helper's first space is intentionally ignored.
func fixtureSignIn(ctx context.Context, client *http.Client, target smokeTarget, email string) (string, string, error) {
	token, _, err := smokeSignIn(ctx, client, target, email)
	if err != nil {
		return "", "", err
	}
	var spaces []fxSpace
	if err := requestJSON(ctx, client, http.MethodGet, target.apiBase+"/api/spaces", token, nil, &spaces, http.StatusOK); err != nil {
		return "", "", err
	}
	for _, s := range spaces {
		if s.PersonalForUserID != "" {
			return token, s.ID, nil
		}
	}
	return "", "", fmt.Errorf("%s has no personal space", email)
}

func seedTeamFixtures(ctx context.Context, client *http.Client, target smokeTarget, withRuns bool) error {
	token, _, err := fixtureSignIn(ctx, client, target, "alice@buildmax.local")
	if err != nil {
		return err
	}
	var spaces []fxSpace
	if err := requestJSON(ctx, client, http.MethodGet, target.apiBase+"/api/spaces", token, nil, &spaces, http.StatusOK); err != nil {
		return err
	}
	var team fxSpace
	for _, s := range spaces {
		if s.Name == "BuildMax QA" && s.PersonalForUserID == "" {
			team = s
			break
		}
	}
	if team.ID == "" {
		if err := requestJSON(ctx, client, http.MethodPost, target.apiBase+"/api/spaces", token, map[string]string{"name": "BuildMax QA"}, &team, http.StatusCreated); err != nil {
			return err
		}
	}
	base := target.apiBase + "/api/spaces/" + url.PathEscape(team.ID)
	people := map[string]string{}
	for _, spec := range []struct {
		email, role string
		accept      bool
	}{
		{"bob@buildmax.local", "admin", true},
		{"carol@buildmax.local", "member", true},
		{"dave@buildmax.local", "member", false},
	} {
		id, err := ensureFixtureMember(ctx, client, target, base, token, spec.email, spec.role, spec.accept)
		if err != nil {
			return err
		}
		people[spec.email] = id
	}
	writer, err := ensureAgent(ctx, client, target, team.ID, token, "BuildMax QA", "QA Writer", "Drafts test plans and release notes.", "Write concise Markdown test plans. Use only the provided fixture files.")
	if err != nil {
		return err
	}
	reviewer, err := ensureAgent(ctx, client, target, team.ID, token, "BuildMax QA", "QA Reviewer", "Reviews acceptance criteria.", "Review the supplied plan for missing acceptance criteria.")
	if err != nil {
		return err
	}
	workflowID, err := ensureFixtureWorkflows(ctx, client, base, token, writer, reviewer)
	if err != nil {
		return err
	}
	specs := []fixtureIssue{
		{title: "QA release checklist", description: "## Acceptance criteria\n\n- [ ] Review documentation\n- [x] Verify installation\n\nParent issue with mixed child progress.", status: "in_progress"},
		{title: "Verify clean installation", description: "Check a fresh workspace with no existing settings.", status: "done", parentTitle: "QA release checklist", ownerID: people["bob@buildmax.local"]},
		{title: "Review Chinese onboarding 中文入门", description: "检查中文、Markdown 和长文本展示。\n\n```sh\n./make kind fixtures\n```", status: "todo", parentTitle: "QA release checklist", ownerID: people["carol@buildmax.local"]},
		// Owner and executor set together: the case one combined assignee field
		// could never express. Bob is accountable; the agent does the work.
		{title: "Draft QA release notes", description: "Prepare a user-facing summary from fixtures/docs/brief.md.", status: "todo", ownerID: people["bob@buildmax.local"], executorKind: "agent", executorID: writer, comments: []string{"## Review notes\n\nPlease include the known limitations.", "The fixture data is synthetic and contains no customer information."}},
		{title: "Run QA review workflow", description: "Draft and review a test plan in two sequential steps.", status: "todo", ownerID: people["carol@buildmax.local"], executorKind: "workflow", executorID: workflowID},
		{title: "Unassigned backlog item", description: "", status: "todo"},
		{title: "Completed QA retrospective", description: "All acceptance criteria verified.", status: "done", comments: []string{"Verified in the local reference deployment."}},
	}
	if err := ensureIssues(ctx, client, target, team.ID, token, "BuildMax QA", specs); err != nil {
		return err
	}
	if err := ensureFixtureFiles(ctx, client, base, token); err != nil {
		return err
	}
	if err := ensureFixtureArtifacts(ctx, client, base, token); err != nil {
		return err
	}
	if err := ensureFixturePagination(ctx, client, target, token); err != nil {
		return err
	}
	if err := ensureFixtureSecrets(ctx, client, base, token); err != nil {
		return err
	}
	if err := seedPluginFixtures(ctx, client, target, team.ID, token); err != nil {
		return err
	}
	var instructions struct {
		Instructions string `json:"instructions"`
	}
	if err := requestJSON(ctx, client, http.MethodGet, base+"/agent-instructions", token, nil, &instructions, http.StatusOK); err != nil {
		return err
	}
	if instructions.Instructions == "" {
		if err := requestJSON(ctx, client, http.MethodPut, base+"/agent-instructions", token, map[string]string{"instructions": "This is a synthetic QA space. Read fixtures/docs/brief.md for context. State assumptions and known limitations in results."}, nil, http.StatusOK); err != nil {
			return err
		}
	}
	if withRuns {
		if err := seedFixtureRuns(ctx, client, base, token); err != nil {
			return err
		}
	}
	fmt.Printf("  team BuildMax QA: %s (Alice owner, Bob admin, Carol member; Dave invited)\n", team.ID)
	return nil
}

func ensureFixtureMember(ctx context.Context, client *http.Client, target smokeTarget, base, ownerToken, email, role string, accept bool) (string, error) {
	var members []struct {
		UserID string `json:"user_id"`
		Email  string `json:"user_email"`
	}
	if err := requestJSON(ctx, client, http.MethodGet, base+"/members", ownerToken, nil, &members, http.StatusOK); err != nil {
		return "", err
	}
	for _, m := range members {
		if m.Email == email {
			return m.UserID, nil
		}
	}
	memberToken, personalID, err := fixtureSignIn(ctx, client, target, email)
	if err != nil {
		return "", err
	}
	var personalMembers []struct {
		UserID string `json:"user_id"`
	}
	if err := requestJSON(ctx, client, http.MethodGet, target.apiBase+"/api/spaces/"+personalID+"/members", memberToken, nil, &personalMembers, http.StatusOK); err != nil {
		return "", err
	}
	if len(personalMembers) != 1 {
		return "", fmt.Errorf("expected one personal-space member for %s", email)
	}
	userID := personalMembers[0].UserID
	type invitation struct {
		ID     string `json:"id"`
		UserID string `json:"user_id"`
	}
	var invitations []invitation
	if err := requestJSON(ctx, client, http.MethodGet, base+"/invitations", ownerToken, nil, &invitations, http.StatusOK); err != nil {
		return "", err
	}
	var inv invitation
	for _, candidate := range invitations {
		if candidate.UserID == userID {
			inv = candidate
			break
		}
	}
	if inv.ID == "" {
		if err := requestJSON(ctx, client, http.MethodPost, base+"/invitations", ownerToken, map[string]string{"email": email, "role": role}, &inv, http.StatusCreated); err != nil {
			return "", err
		}
	}
	if accept {
		if err := requestJSON(ctx, client, http.MethodPost, target.apiBase+"/api/invitations/"+inv.ID+"/accept", memberToken, nil, nil, http.StatusOK); err != nil {
			return "", err
		}
	}
	return userID, nil
}

func ensureFixtureWorkflows(ctx context.Context, client *http.Client, base, token, writer, reviewer string) (string, error) {
	var list fxWorkflowList
	if err := requestJSON(ctx, client, http.MethodGet, base+"/workflows", token, nil, &list, http.StatusOK); err != nil {
		return "", err
	}
	byName := map[string]string{}
	for _, w := range list.Workflows {
		byName[w.Name] = w.ID
	}
	var published string
	for _, spec := range []struct{ name, status string }{{"QA Draft Plan", "draft"}, {"QA Release Review", "published"}, {"QA Archived Plan", "archived"}} {
		id := byName[spec.name]
		if id == "" {
			definition := fmt.Sprintf(`{"schema_version":1,"nodes":[{"id":"draft","type":"agent_task","target_agent_id":%q,"prompt":"Draft a concise QA plan."},{"id":"review","type":"agent_task","needs":["draft"],"target_agent_id":%q,"prompt":"Review the previous result for missing criteria."}]}`, writer, reviewer)
			var created fxWorkflow
			if err := requestJSON(ctx, client, http.MethodPost, base+"/workflows", token, map[string]string{"name": spec.name, "description": "Synthetic two-step QA workflow.", "definition": definition}, &created, http.StatusCreated); err != nil {
				return "", err
			}
			id = created.ID
		}
		var current struct {
			Status string `json:"status"`
		}
		if err := requestJSON(ctx, client, http.MethodGet, base+"/workflows/"+id, token, nil, &current, http.StatusOK); err != nil {
			return "", err
		}
		if current.Status != spec.status {
			if err := requestJSON(ctx, client, http.MethodPatch, base+"/workflows/"+id, token, map[string]string{"status": spec.status}, nil, http.StatusOK); err != nil {
				return "", err
			}
		}
		if spec.status == "published" {
			published = id
		}
	}
	return published, nil
}

func ensureFixtureFiles(ctx context.Context, client *http.Client, base, token string) error {
	type node struct {
		ID       string            `json:"id"`
		Children []json.RawMessage `json:"children"`
	}
	var tree json.RawMessage
	if err := requestJSON(ctx, client, http.MethodGet, base+"/files", token, nil, &tree, http.StatusOK); err != nil {
		return err
	}
	seen := map[string]bool{}
	var walk func(json.RawMessage) error
	walk = func(raw json.RawMessage) error {
		var n node
		if err := json.Unmarshal(raw, &n); err != nil {
			return err
		}
		seen[n.ID] = true
		for _, child := range n.Children {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(tree); err != nil {
		return err
	}
	for _, file := range []struct{ path, content string }{
		{"fixtures/docs/brief.md", "# QA release brief\n\nSynthetic data for testing.\n\n## Scope\n\nIssues, workflows, membership, and workspace files.\n"},
		{"fixtures/data/checks.csv", "feature,status\nissues,passed\nworkflow,pending\n"},
		{"fixtures/data/config.json", "{\"environment\":\"fixture\",\"enabled\":true}\n"},
		{"fixtures/中文说明.txt", "这是测试文件，用于验证 Unicode 路径与文本预览。\n"},
		{"fixtures/empty.txt", ""},
	} {
		if seen[file.path] {
			continue
		}
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("files", "fixture.txt")
		if err != nil {
			return err
		}
		if _, err := io.WriteString(part, file.content); err != nil {
			return err
		}
		if err := writer.WriteField("paths", file.path); err != nil {
			return err
		}
		if err := writer.Close(); err != nil {
			return err
		}
		response, err := request(ctx, client, http.MethodPost, base+"/upload", token, writer.FormDataContentType(), &body, http.StatusOK)
		if err != nil {
			return err
		}
		if err := response.Close(); err != nil {
			return err
		}
	}
	return nil
}

func ensureFixtureSecrets(ctx context.Context, client *http.Client, base, token string) error {
	type secret struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		State string `json:"state"`
	}
	var existing struct {
		Secrets []secret `json:"secrets"`
	}
	if err := requestJSON(ctx, client, http.MethodGet, base+"/secrets", token, nil, &existing, http.StatusOK); err != nil {
		return err
	}
	for _, spec := range []struct{ name, state string }{{"fixture-demo", "active"}, {"fixture-disabled", "disabled"}} {
		var found secret
		for _, s := range existing.Secrets {
			if s.Name == spec.name {
				found = s
				break
			}
		}
		if found.ID == "" {
			if err := requestJSON(ctx, client, http.MethodPost, base+"/secrets", token, map[string]any{"name": spec.name, "description": "Synthetic placeholder; not a working credential.", "items": map[string]string{"token": "fixture-only-not-a-real-token", "endpoint": "https://example.invalid"}}, &found, http.StatusCreated); err != nil {
				return err
			}
		}
		if found.State != spec.state {
			if err := requestJSON(ctx, client, http.MethodPut, base+"/secrets/"+found.ID+"/state", token, map[string]string{"state": spec.state}, nil, http.StatusOK); err != nil {
				return err
			}
		}
	}
	return nil
}

func ensureFixturePagination(ctx context.Context, client *http.Client, target smokeTarget, token string) error {
	var spaces []fxSpace
	if err := requestJSON(ctx, client, http.MethodGet, target.apiBase+"/api/spaces", token, nil, &spaces, http.StatusOK); err != nil {
		return err
	}
	var space fxSpace
	for _, candidate := range spaces {
		if candidate.Name == "BuildMax QA Pagination" && candidate.PersonalForUserID == "" {
			space = candidate
			break
		}
	}
	if space.ID == "" {
		if err := requestJSON(ctx, client, http.MethodPost, target.apiBase+"/api/spaces", token, map[string]string{"name": "BuildMax QA Pagination"}, &space, http.StatusCreated); err != nil {
			return err
		}
	}
	specs := make([]fixtureIssue, 105)
	for i := range specs {
		specs[i] = fixtureIssue{title: fmt.Sprintf("Pagination sample %03d", i+1), description: "Synthetic issue for list pagination and status filtering.", status: []string{"todo", "in_progress", "done"}[i%3]}
	}
	for i := 0; i < 25; i++ {
		specs[0].comments = append(specs[0].comments, fmt.Sprintf("Pagination fixture comment %02d: verify the complete discussion is reachable.", i+1))
	}
	return ensureIssues(ctx, client, target, space.ID, token, "QA Pagination", specs)
}

func ensureFixtureArtifacts(ctx context.Context, client *http.Client, base, token string) error {
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
	for _, file := range []struct{ name, content string }{
		{"fixture-report.txt", "QA report\n\nSynthetic fixture: all basic acceptance checks passed.\n"},
		{"fixture-dashboard.html", "<!doctype html><html lang=\"en\"><meta charset=\"utf-8\"><title>QA fixture</title><h1>QA results</h1><p>Synthetic HTML artifact for sandbox preview.</p></html>"},
		{"fixture-download.bin", "\x00\x01\x02\x03"},
	} {
		if seen[file.name] {
			continue
		}
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", file.name)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(part, file.content); err != nil {
			return err
		}
		if err := writer.Close(); err != nil {
			return err
		}
		response, err := request(ctx, client, http.MethodPost, base+"/artifacts", token, writer.FormDataContentType(), &body, http.StatusCreated)
		if err != nil {
			return err
		}
		if err := response.Close(); err != nil {
			return err
		}
	}
	return nil
}
