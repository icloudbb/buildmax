package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/testsupport"
)

// Every route under /api/spaces/{space_id} is a space-scoped resource, and Space is
// the authorization boundary for all of them. These tests drive real requests
// through the mux for each role and for two kinds of stranger, because the
// checks live in handler helpers rather than in one middleware — so a route
// that forgets to call one is not a compile error, and would otherwise be
// caught by nobody.
//
// Driving real requests rather than unit-testing core/space.Allows is
// deliberate, and stays deliberate now that Allows is the only implementation
// of the rules. What this proves is not what a role may do -- core/space's own
// table proves that -- but that each route asks. A rule with one owner that
// nobody consults on a route is still an open route.

const (
	matrixSecret    = "matrix-secret"
	matrixSpace     = "tm_matrix"
	matrixOther     = "tm_other"
	matrixOwner     = "u_owner"
	matrixAdmin     = "u_admin"
	matrixMember    = "u_member"
	matrixUnsetRole = "u_unset_role"
	matrixOutside   = "u_outsider"
)

// authzCase is one route and the least privileged role that may reach its
// handler.
type authzCase struct {
	method string
	path   string
	// minRole is SpaceRoleMember, SpaceRoleAdmin, or SpaceRoleOwner.
	minRole string
	// tokenInQuery marks a route that reads its credential from ?token=
	// instead of the Authorization header. A browser cannot set headers on a
	// WebSocket handshake, so the upgrade route has no other option.
	tokenInQuery bool
}

// spaceRoutes is the authorization matrix. Every space-scoped route the server registers
// must appear here — TestAuthzMatrixCoversEverySpaceRoute fails otherwise, so a
// new route cannot ship without someone deciding who may call it.
var spaceRoutes = []authzCase{
	{"GET", "/api/spaces/{space_id}/agents", corespace.RoleMember, false},
	{"POST", "/api/spaces/{space_id}/agents", corespace.RoleAdmin, false},
	{"GET", "/api/spaces/{space_id}/agents/{agent_id}", corespace.RoleMember, false},
	{"PATCH", "/api/spaces/{space_id}/agents/{agent_id}", corespace.RoleAdmin, false},
	{"DELETE", "/api/spaces/{space_id}/agents/{agent_id}", corespace.RoleAdmin, false},
	{"GET", "/api/spaces/{space_id}/agents/{agent_id}/revisions", corespace.RoleMember, false},
	{"POST", "/api/spaces/{space_id}/agents/{agent_id}/revisions/{revision}/restore", corespace.RoleAdmin, false},
	// Every member may inspect the context their background runs inherit;
	// changing that shared behavior uses the same authority as managing agents.
	{"GET", "/api/spaces/{space_id}/agent-instructions", corespace.RoleMember, false},
	{"PUT", "/api/spaces/{space_id}/agent-instructions", corespace.RoleAdmin, false},

	// Schedules: any member may manage recurring triggers, the same tier as
	// running work, because a schedule is a member arranging a run they could
	// start by hand. See docs/design/scheduled-agent-execution.md §9.
	{"GET", "/api/spaces/{space_id}/schedules", corespace.RoleMember, false},
	{"POST", "/api/spaces/{space_id}/schedules", corespace.RoleMember, false},
	{"GET", "/api/spaces/{space_id}/schedules/{schedule_id}", corespace.RoleMember, false},
	{"PATCH", "/api/spaces/{space_id}/schedules/{schedule_id}", corespace.RoleMember, false},
	{"DELETE", "/api/spaces/{space_id}/schedules/{schedule_id}", corespace.RoleMember, false},

	// Reading what a space activated answers "why did this run have this
	// plugin", which is any member's question. Changing an activation is the
	// same authority the space's other shared automation needs.
	{"GET", "/api/spaces/{space_id}/plugin-activations", corespace.RoleMember, false},
	{"POST", "/api/spaces/{space_id}/plugin-activations", corespace.RoleAdmin, false},
	{"PATCH", "/api/spaces/{space_id}/plugin-activations/{plugin_name}", corespace.RoleAdmin, false},
	{"PUT", "/api/spaces/{space_id}/plugin-curation", corespace.RoleAdmin, false},

	// Reading the space's default sandbox tiers answers "what would an
	// undeclared agent run under here", which is any member's question.
	// Changing it is the same authority as managing agents.
	{"GET", "/api/spaces/{space_id}/sandbox-defaults", corespace.RoleMember, false},
	{"PUT", "/api/spaces/{space_id}/sandbox-defaults", corespace.RoleAdmin, false},

	// Space Secrets: reading metadata is owner-or-admin, because an admin editing
	// an agent must see which secrets exist to configure its consumption;
	// managing values -- create, edit, disable, destroy -- is owner-only. No
	// role reads a value. See docs/design/space-secrets.md §10.
	{"GET", "/api/spaces/{space_id}/secrets", corespace.RoleAdmin, false},
	{"POST", "/api/spaces/{space_id}/secrets", corespace.RoleOwner, false},
	{"GET", "/api/spaces/{space_id}/secrets/{secret_id}", corespace.RoleAdmin, false},
	{"PATCH", "/api/spaces/{space_id}/secrets/{secret_id}", corespace.RoleOwner, false},
	{"PUT", "/api/spaces/{space_id}/secrets/{secret_id}/state", corespace.RoleOwner, false},

	{"GET", "/api/spaces/{space_id}/members", corespace.RoleMember, false},
	{"DELETE", "/api/spaces/{space_id}/members/{user_id}", corespace.RoleOwner, false},
	// Role change (including the ownership transfer that results from
	// setting a target's role to owner) and space-scoped access recovery are
	// both owner-only -- see docs/design/space-membership-lifecycle.md §5.2,
	// §5.3, §5.4, and §7.
	{"PATCH", "/api/spaces/{space_id}/members/{user_id}", corespace.RoleOwner, false},
	{"POST", "/api/spaces/{space_id}/members/{user_id}/login-code", corespace.RoleOwner, false},

	// Invitation is the one membership action admin holds, at member role
	// only -- see docs/design/space-membership-lifecycle.md §5.1 and §7. That
	// role-content restriction is enforced by the service, not the route, so
	// this matrix -- which drives one representative request per case -- still
	// reads minRole as admin: an admin's request with no role in the body
	// defaults to member and succeeds.
	{"POST", "/api/spaces/{space_id}/invitations", corespace.RoleAdmin, false},
	{"GET", "/api/spaces/{space_id}/invitations", corespace.RoleAdmin, false},
	{"DELETE", "/api/spaces/{space_id}/invitations/{invitation_id}", corespace.RoleAdmin, false},

	{"GET", "/api/spaces/{space_id}/conversations", corespace.RoleMember, false},
	{"POST", "/api/spaces/{space_id}/conversations", corespace.RoleMember, false},
	{"GET", "/api/spaces/{space_id}/conversations/{conversation_id}/messages", corespace.RoleMember, false},
	{"POST", "/api/spaces/{space_id}/conversations/{conversation_id}/messages", corespace.RoleMember, false},
	{"GET", "/api/spaces/{space_id}/conversations/{conversation_id}/tasks", corespace.RoleMember, false},
	{"POST", "/api/spaces/{space_id}/conversations/{conversation_id}/tasks", corespace.RoleMember, false},
	{"POST", "/api/spaces/{space_id}/tasks", corespace.RoleMember, false},
	{"GET", "/api/spaces/{space_id}/agents/{agent_id}/tasks", corespace.RoleMember, false},
	{"POST", "/api/spaces/{space_id}/agents/{agent_id}/tasks", corespace.RoleMember, false},
	{"GET", "/api/spaces/{space_id}/schedules/{schedule_id}/tasks", corespace.RoleMember, false},
	{"GET", "/api/spaces/{space_id}/schedules/{schedule_id}/runs", corespace.RoleMember, false},

	{"GET", "/api/spaces/{space_id}/issues", corespace.RoleMember, false},
	{"POST", "/api/spaces/{space_id}/issues", corespace.RoleMember, false},
	{"GET", "/api/spaces/{space_id}/issues/{issue_id}", corespace.RoleMember, false},
	{"PATCH", "/api/spaces/{space_id}/issues/{issue_id}", corespace.RoleMember, false},
	{"GET", "/api/spaces/{space_id}/issues/{issue_id}/flow", corespace.RoleMember, false},
	// Commenting is collaboration, so every member may do it. Moderation —
	// deleting a comment you did not write — is owner-only, but that is decided
	// inside the handler from the comment's author, not by the route: a member
	// deleting their own comment reaches the same endpoint.
	{"GET", "/api/spaces/{space_id}/issues/{issue_id}/comments", corespace.RoleMember, false},
	{"POST", "/api/spaces/{space_id}/issues/{issue_id}/comments", corespace.RoleMember, false},
	{"PATCH", "/api/spaces/{space_id}/issues/{issue_id}/comments/{comment_id}", corespace.RoleMember, false},
	{"DELETE", "/api/spaces/{space_id}/issues/{issue_id}/comments/{comment_id}", corespace.RoleMember, false},
	{"POST", "/api/spaces/{space_id}/issues/{issue_id}/agent-runs", corespace.RoleMember, false},
	{"POST", "/api/spaces/{space_id}/issues/{issue_id}/workflow-runs", corespace.RoleMember, false},

	{"GET", "/api/spaces/{space_id}/workflows", corespace.RoleMember, false},
	{"POST", "/api/spaces/{space_id}/workflows", corespace.RoleAdmin, false},
	{"GET", "/api/spaces/{space_id}/workflows/{workflow_id}", corespace.RoleMember, false},
	{"PATCH", "/api/spaces/{space_id}/workflows/{workflow_id}", corespace.RoleAdmin, false},
	{"GET", "/api/spaces/{space_id}/workflows/{workflow_id}/revisions", corespace.RoleMember, false},
	{"POST", "/api/spaces/{space_id}/workflows/{workflow_id}/revisions/{revision}/restore", corespace.RoleAdmin, false},
	{"GET", "/api/spaces/{space_id}/workflows/{workflow_id}/runs", corespace.RoleMember, false},
	{"POST", "/api/spaces/{space_id}/workflows/{workflow_id}/runs", corespace.RoleMember, false},
	{"GET", "/api/spaces/{space_id}/workflow-runs/{workflow_run_id}", corespace.RoleMember, false},

	{"GET", "/api/spaces/{space_id}/tasks/{task_id}", corespace.RoleMember, false},
	{"GET", "/api/spaces/{space_id}/tasks/{task_id}/runs", corespace.RoleMember, false},
	{"POST", "/api/spaces/{space_id}/tasks/{task_id}/runs", corespace.RoleMember, false},
	// Starting a run and stopping one are the same level of act on the same
	// resource. A member who may spend the space's budget may also stop
	// spending it, including on a run somebody else started.
	{"POST", "/api/spaces/{space_id}/tasks/{task_id}/cancel", corespace.RoleMember, false},
	// Retry starts a run, so it sits at the same level as starting one.
	{"POST", "/api/spaces/{space_id}/tasks/{task_id}/retry", corespace.RoleMember, false},
	// Unified artifacts. Any member may keep a file for the space and see what
	// the space holds; removing one is decided per artifact rather than per
	// role, so it is not on a space-scoped route -- see the artifact package.
	{"GET", "/api/spaces/{space_id}/artifacts", corespace.RoleMember, false},
	{"POST", "/api/spaces/{space_id}/artifacts", corespace.RoleMember, false},
	{"GET", "/api/spaces/{space_id}/tasks/{task_id}/conversation", corespace.RoleMember, false},
	{"GET", "/api/spaces/{space_id}/tasks/{task_id}/stream", corespace.RoleMember, false},

	{"GET", "/api/spaces/{space_id}/task-runs/{task_run_id}", corespace.RoleMember, false},
	{"GET", "/api/spaces/{space_id}/task-runs/{task_run_id}/trace", corespace.RoleMember, false},
	// A member may read what their space's run spent, the same as its trace and
	// artifacts. The ledger carries no prompts, and hiding a space's own usage
	// from the people producing it would make quota unexplainable.
	{"GET", "/api/spaces/{space_id}/task-runs/{task_run_id}/llm-calls", corespace.RoleMember, false},

	{"GET", "/api/spaces/{space_id}/files", corespace.RoleMember, false},
	{"GET", "/api/spaces/{space_id}/files/{path...}", corespace.RoleMember, false},
	{"POST", "/api/spaces/{space_id}/upload", corespace.RoleMember, false},

	{"GET", "/api/spaces/{space_id}/usage", corespace.RoleMember, false},
	// Owner only: the trail names who did what, including who was refused,
	// which is administrative rather than collaborative information.
	{"GET", "/api/spaces/{space_id}/audit-events", corespace.RoleOwner, false},
	// The export is the same read in a file, so it is the same reader.
	{"GET", "/api/spaces/{space_id}/audit-events/export", corespace.RoleOwner, false},
	// The managed gateway is deliberately absent: it is not space-scoped. Every
	// catalog model is available to every signed-in user, so its routes carry no
	// space and are covered by the gateway's own tests.

	{"GET", "/api/spaces/{space_id}/ws", corespace.RoleMember, true},
}

// matrixMux builds a handler with every store wired.
//
// A nil store answers 503 before any authorization check runs, which would turn
// a missing denial into a passing test. Wiring them all is what makes a 403
// assertion mean what it says.
func matrixMux(t *testing.T) *http.ServeMux {
	t.Helper()
	return matrixMuxWithGrants(t, nil)
}

// matrixMuxWithGrants is matrixMux with a deployment-scoped grant store, so
// the space matrix can also be driven by a system administrator. That caller
// must be refused by every route here — see TestSystemGrantIsNotASpaceKey.
func matrixMuxWithGrants(t *testing.T, grants coreidentity.SystemGrantStore) *http.ServeMux {
	t.Helper()
	spaces := &mock.MockSpaceStore{
		Spaces: []corespace.Space{
			{ID: matrixSpace, Name: "Matrix", CreatedBy: matrixOwner},
			{ID: matrixOther, Name: "Other", CreatedBy: matrixOutside},
		},
		Members: []corespace.Member{
			{SpaceID: matrixSpace, UserID: matrixOwner, Role: corespace.RoleOwner},
			{SpaceID: matrixSpace, UserID: matrixAdmin, Role: corespace.RoleAdmin},
			{SpaceID: matrixSpace, UserID: matrixMember, Role: corespace.RoleMember},
			// A membership row that never got a role. Nothing writes one now --
			// the space service defaults an unset role before storing it -- so
			// this stands in for a legacy row, and pins what such a row may do.
			{SpaceID: matrixSpace, UserID: matrixUnsetRole, Role: ""},
			// The stranger owns a different space, which is the case that
			// separates "is a member of something" from "is a member of this".
			{SpaceID: matrixOther, UserID: matrixOutside, Role: corespace.RoleOwner},
		},
	}
	conversations := &mock.MockConversationStore{}
	h := NewHandler(Config{
		JWTSecret:                matrixSecret,
		LLMGateway:               llmTestService(t, &llmStubClient{content: "ok"}, nil),
		SpaceStore:               spaces,
		UserStore:                &mock.MockUserStore{},
		AgentStore:               &mock.MockAgentStore{},
		IssueStore:               &mock.MockIssueStore{},
		IssueCommentStore:        &mock.MockIssueCommentStore{},
		WorkflowStore:            &mock.MockWorkflowStore{},
		TaskStore:                &mock.MockTaskStore{},
		TaskRunStore:             &mock.MockTaskRunStore{},
		ScheduleStore:            &mock.MockScheduleStore{},
		ConversationStore:        conversations,
		ConversationMessageStore: &mock.MockConversationMessageStore{},
		AuditStore:               &mock.MockAuditStore{},
		SystemGrantStore:         grants,
		LoginCodeStore:           &mock.MockLoginCodeStore{},
		PersistStorage:           mock.NewMockPersistStorage(),
		ArtifactStore:            &mock.MockArtifactStore{},
		ArtifactStorage:          mock.NewMockArtifactStorage(),
		WorkspacesDir:            t.TempDir(),
	})
	mux := http.NewServeMux()
	h.Register(mux)
	return mux
}

// requestAs issues one request for the matrix. An empty user means no token.
func requestAs(t *testing.T, mux *http.ServeMux, c authzCase, spaceID, userID string) int {
	t.Helper()
	path := strings.ReplaceAll(c.path, "{space_id}", spaceID)
	// Remaining path parameters are filled with ids that do not exist. The
	// resource is irrelevant: authorization is decided before it is looked up,
	// and a 404 for a missing object still proves the caller got past the gate.
	path = regexp.MustCompile(`\{[^}]+\}`).ReplaceAllString(path, "nonexistent")
	if c.tokenInQuery && userID != "" {
		path += "?token=" + testsupport.SignJWT(userID, matrixSecret)
	}
	req := httptest.NewRequest(c.method, path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	if userID != "" && !c.tokenInQuery {
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT(userID, matrixSecret))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Code
}

func allowedFor(minRole, role string) bool {
	switch minRole {
	case corespace.RoleOwner:
		return role == corespace.RoleOwner
	case corespace.RoleAdmin:
		return role == corespace.RoleOwner || role == corespace.RoleAdmin
	default:
		return true
	}
}

// TestSpaceAuthzMatrix drives every space-scoped route as an owner, an admin, a
// member, a member of a different space, and an anonymous caller.
func TestSpaceAuthzMatrix(t *testing.T) {
	mux := matrixMux(t)

	roles := map[string]string{
		matrixOwner:  corespace.RoleOwner,
		matrixAdmin:  corespace.RoleAdmin,
		matrixMember: corespace.RoleMember,
		// A row with no role is a member, so it is driven through every route
		// against the same expectations. It used to be refused everything.
		matrixUnsetRole: corespace.RoleMember,
	}

	for _, c := range spaceRoutes {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			// Nobody outside the space gets in, whatever the route does.
			if got := requestAs(t, mux, c, matrixSpace, matrixOutside); got != http.StatusForbidden {
				t.Errorf("a member of another space got %d, want 403", got)
			}
			if got := requestAs(t, mux, c, matrixSpace, ""); got != http.StatusUnauthorized {
				t.Errorf("an anonymous caller got %d, want 401", got)
			}
			// Naming someone else's space must not work either, even for a user
			// who is a legitimate member of their own.
			if got := requestAs(t, mux, c, matrixOther, matrixMember); got != http.StatusForbidden {
				t.Errorf("a member reaching into another space got %d, want 403", got)
			}

			for userID, role := range roles {
				got := requestAs(t, mux, c, matrixSpace, userID)
				if allowedFor(c.minRole, role) {
					if got == http.StatusForbidden || got == http.StatusUnauthorized {
						t.Errorf("%s may call this route but got %d", role, got)
					}
					continue
				}
				if got != http.StatusForbidden {
					t.Errorf("%s must not call this route (needs %s) but got %d", role, c.minRole, got)
				}
			}
		})
	}
}

// TestAuthzMatrixCoversEverySpaceRoute reads every route registration under
// this package and fails when a space-scoped route has no entry above.
//
// Without this the matrix silently stops being a matrix: a new route ships,
// nobody notices it was never assigned a minimum role, and the gap looks
// exactly like coverage.
func TestAuthzMatrixCoversEverySpaceRoute(t *testing.T) {
	// Space-scoped routes are registered by each context package's own Register
	// method, so the matrix reads every one of them rather than the file that
	// happened to hold them all before the split.
	var body []byte
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		part, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		body = append(body, part...)
		return nil
	})
	if err != nil {
		t.Fatalf("read the route tables: %v", err)
	}
	found := regexp.MustCompile(`"(GET|POST|PATCH|PUT|DELETE) (/api/spaces/\{space_id\}[^"]*)"`).
		FindAllStringSubmatch(string(body), -1)
	if len(found) == 0 {
		t.Fatal("no space-scoped routes found; the pattern this test relies on has changed")
	}

	covered := make(map[string]bool, len(spaceRoutes))
	for _, c := range spaceRoutes {
		covered[c.method+" "+c.path] = true
	}
	var missing []string
	registered := make(map[string]bool, len(found))
	for _, m := range found {
		key := m[1] + " " + m[2]
		registered[key] = true
		if !covered[key] {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	for _, key := range missing {
		t.Errorf("%s is registered but has no authorization case; decide who may call it", key)
	}

	// The reverse: an entry for a route that no longer exists is dead weight
	// that reads as coverage.
	for _, c := range spaceRoutes {
		key := c.method + " " + c.path
		if !registered[key] {
			t.Errorf("%s has an authorization case but is registered nowhere", key)
		}
	}
}
