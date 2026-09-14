package admin

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/audit"
)

// Every route under /api/admin is deployment-scoped, and a system grant is the
// only thing that opens one. These tests drive real requests for each kind of
// caller, for the same reason the space matrix does: the check lives in a
// handler helper rather than in one middleware, so a route that forgets to call
// it is not a compile error.
//
// The case that matters most is not any single route — it is
// TestSystemGrantIsNotASpaceKey below. See docs/design/system-administration.md
// sections 4 and 11.

// errStoreUnavailable stands in for a database that is not answering.
var errStoreUnavailable = errors.New("grant store unavailable")

const (
	adminUser       = "u_sysadmin"
	adminRevoked    = "u_was_admin"
	adminSpaceOwner = "u_space_owner"
	adminOrdinary   = "u_ordinary"
)

// adminCase is one deployment-scoped route.
type adminCase struct {
	method string
	path   string
}

// adminRoutes is the authorization matrix. Every /api/admin route this package registers
// must appear here — TestAdminMatrixCoversEveryAdminRoute fails otherwise, so a
// new one cannot ship without someone deciding it is admin-only.
var adminRoutes = []adminCase{
	{"GET", "/api/admin/me"},
	{"GET", "/api/admin/grants"},
	{"POST", "/api/admin/grants"},
	{"DELETE", "/api/admin/grants/{user_id}"},
	{"GET", "/api/admin/users"},
	{"POST", "/api/admin/users"},
	{"GET", "/api/admin/users/{user_id}"},
	{"POST", "/api/admin/users/{user_id}/login-code"},
	{"PUT", "/api/admin/users/{user_id}/state"},
	{"GET", "/api/admin/users/{user_id}/identities"},
	{"DELETE", "/api/admin/users/{user_id}/identities/{identity_id}"},
	{"GET", "/api/admin/users/{user_id}/sessions"},
	{"DELETE", "/api/admin/users/{user_id}/sessions"},
	{"DELETE", "/api/admin/users/{user_id}/sessions/{session_id}"},
	{"GET", "/api/admin/system"},
	{"GET", "/api/admin/config"},
	{"GET", "/api/admin/audit-events"},
	{"GET", "/api/admin/audit-events/export"},
	{"GET", "/api/admin/spaces"},
	{"GET", "/api/admin/spaces/{space_id}"},
	{"GET", "/api/admin/llm/models"},
	{"POST", "/api/admin/llm/models"},
	{"PUT", "/api/admin/llm/models/{model_id}/state"},
	// Publishing changes what every member of the deployment can install, so
	// reaching any of these without a grant has to be refused before the
	// handler looks at a body.
	{"GET", "/api/admin/plugins"},
	{"POST", "/api/admin/plugins"},
	{"GET", "/api/admin/plugins/{plugin_name}/releases"},
	{"POST", "/api/admin/plugins/{plugin_name}/releases"},
	{"PUT", "/api/admin/plugins/{plugin_name}/releases/{version}/state"},
	{"PUT", "/api/admin/plugins/{plugin_name}/state"},
}

// adminMux builds a handler whose grant store has one active admin and one
// whose grant has been revoked.
func adminMux(t *testing.T) (*http.ServeMux, *mock.MockAuditStore) {
	t.Helper()
	grants := &mock.MockSystemGrantStore{}
	grants.GrantForTest(adminUser, coreidentity.SystemRoleAdmin)
	grants.GrantForTest(adminRevoked, coreidentity.SystemRoleAdmin)
	revokedAt := time.Unix(500, 0).UTC()
	grants.Grants[1].RevokedAt = &revokedAt

	audits := &mock.MockAuditStore{}
	spaces := &mock.MockSpaceStore{
		Spaces: []corespace.Space{{ID: matrixSpace, Name: "Matrix", CreatedBy: adminSpaceOwner}},
		Members: []corespace.Member{
			{SpaceID: matrixSpace, UserID: adminSpaceOwner, Role: corespace.RoleOwner},
		},
	}
	users := &mock.MockUserStore{}
	// The admin himself must exist, or requireActiveUser cannot tell an
	// enabled account from an absent one.
	seedUser(t, users, adminUser, "admin@example.com")
	seedUser(t, users, adminSpaceOwner, "owner@example.com")
	h := New(Config{
		JWTSecret:     testSecret,
		Grants:        grants,
		Spaces:        spaces,
		Users:         users,
		LoginCodes:    &mock.MockLoginCodeStore{},
		RefreshTokens: &mock.MockRefreshTokenStore{},
		Audits:        audits,
		// The recorder, not just the store: Config.Audit is what the handlers
		// write through, and a nil one discards silently — which would make
		// TestAdminDenialIsRecorded pass for the wrong reason if it were
		// asserting the other way round.
		Audit: audit.NewRecorder(audits),
	})
	mux := http.NewServeMux()
	h.Register(mux)
	return mux, audits
}

// seedUser puts an account in the store under a chosen id, which CreateUser
// does not allow.
func seedUser(t *testing.T, users *mock.MockUserStore, userID, email string) *coreidentity.User {
	t.Helper()
	if users.ByID == nil {
		users.ByID = make(map[string]*coreidentity.User)
	}
	if users.ByEmail == nil {
		users.ByEmail = make(map[string]*coreidentity.User)
	}
	u := &coreidentity.User{ID: userID, Email: email, CreatedAt: time.Unix(1, 0).UTC()}
	users.ByID[userID] = u
	users.ByEmail[email] = u
	return u
}

// TestSystemAuthzMatrix drives every admin route as a system administrator, a
// space owner with no grant, an ordinary user, a user whose grant was revoked,
// and an anonymous caller.
func TestSystemAuthzMatrix(t *testing.T) {
	mux, _ := adminMux(t)

	for _, c := range adminRoutes {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			if got := adminRequestAs(t, mux, c, adminUser).Code; got == http.StatusForbidden || got == http.StatusUnauthorized {
				t.Errorf("a system administrator got %d, want the handler to run", got)
			}
			// A space owner is the caller this boundary exists to refuse.
			// Owning a space says nothing about the deployment.
			if got := adminRequestAs(t, mux, c, adminSpaceOwner).Code; got != http.StatusForbidden {
				t.Errorf("a space owner got %d, want 403", got)
			}
			if got := adminRequestAs(t, mux, c, adminOrdinary).Code; got != http.StatusForbidden {
				t.Errorf("an ordinary user got %d, want 403", got)
			}
			// Revocation is a live check, not something read once at startup.
			if got := adminRequestAs(t, mux, c, adminRevoked).Code; got != http.StatusForbidden {
				t.Errorf("a revoked administrator got %d, want 403", got)
			}
			// 401 rather than 403: no credential is a different answer from a
			// credential that does not carry the authority.
			if got := adminRequestAs(t, mux, c, "").Code; got != http.StatusUnauthorized {
				t.Errorf("an anonymous caller got %d, want 401", got)
			}
		})
	}
}

// TestSystemGrantIsNotASpaceKey is the assertion that makes the principals table
// in the design true.
//
// A system administrator with no membership drives the space-scoped routes and
// is refused by every one of them. If anybody ever proposes consulting the
// grant inside authorizeSpaceAction, this is the test that fails, and the reason
// it exists is that the failure would otherwise be silent: an administrator
// would quietly acquire read access to every space's prompts, artifacts, and
// traces, and no route would look different.
func TestAdminDenialIsRecorded(t *testing.T) {
	mux, audits := adminMux(t)

	if got := adminRequestAs(t, mux, adminRoutes[0], adminSpaceOwner).Code; got != http.StatusForbidden {
		t.Fatalf("setup: space owner got %d, want 403", got)
	}
	if len(audits.Events) != 1 {
		t.Fatalf("got %d events, want 1: %+v", len(audits.Events), audits.Events)
	}
	e := audits.Events[0]
	if e.Action != coreaudit.AccessDenied || e.ActorID != adminSpaceOwner || e.SpaceID != "" || e.TargetType != "route" {
		t.Errorf("denial event wrong: %+v", e)
	}
	if !strings.Contains(e.TargetID, "/api/admin/me") {
		t.Errorf("denial should name the route that was tried, got %q", e.TargetID)
	}

	// An anonymous caller writes nothing: there is no actor to record, and an
	// event keyed by an unauthenticated request would let anyone write rows.
	audits.Events = nil
	if got := adminRequestAs(t, mux, adminRoutes[0], "").Code; got != http.StatusUnauthorized {
		t.Fatalf("setup: anonymous got %d, want 401", got)
	}
	if len(audits.Events) != 0 {
		t.Errorf("an unauthenticated refusal must not be recorded: %+v", audits.Events)
	}
}

// TestAdminRouteFailsClosedOnStoreError pins the one behaviour in
// requireSystemAdmin worth being paranoid about: a database error denies rather
// than admits.
func TestAdminRouteFailsClosedOnStoreError(t *testing.T) {
	grants := &mock.MockSystemGrantStore{Err: errStoreUnavailable}
	grants.GrantForTest(adminUser, coreidentity.SystemRoleAdmin)
	h := New(Config{
		JWTSecret: testSecret,
		Grants:    grants,
		Spaces:    &mock.MockSpaceStore{},
		Audits:    &mock.MockAuditStore{},
	})
	mux := http.NewServeMux()
	h.Register(mux)

	rec := adminRequestAs(t, mux, adminRoutes[0], adminUser)
	if rec.Code == http.StatusOK {
		t.Errorf("a grant store failure must not authorize: got %d", rec.Code)
	}
}

// TestAdminRouteWithoutAStoreDoesNotLeakToAnonymousCallers covers the ordering
// in requireSystemAdmin: a deployment with no grant store answers an anonymous
// caller 401, not "system administration not configured".
func TestAdminRouteWithoutAStoreDoesNotLeakToAnonymousCallers(t *testing.T) {
	h := New(Config{
		JWTSecret: testSecret,
		Spaces:    &mock.MockSpaceStore{},
		Audits:    &mock.MockAuditStore{},
	})
	mux := http.NewServeMux()
	h.Register(mux)

	if got := adminRequestAs(t, mux, adminRoutes[0], "").Code; got != http.StatusUnauthorized {
		t.Errorf("anonymous caller got %d, want 401 before any store is consulted", got)
	}
	if got := adminRequestAs(t, mux, adminRoutes[0], adminUser).Code; got != http.StatusServiceUnavailable {
		t.Errorf("authenticated caller got %d, want 503 when the store is missing", got)
	}
}

// TestAdminMeReportsTheGrant checks the response Portal reads to decide whether
// an administration area exists for this person.
func TestAdminMeReportsTheGrant(t *testing.T) {
	mux, _ := adminMux(t)
	rec := adminRequestAs(t, mux, adminCase{"GET", "/api/admin/me"}, adminUser)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{adminUser, coreidentity.SystemRoleAdmin, "granted_at"} {
		if !strings.Contains(body, want) {
			t.Errorf("response should contain %q, got %s", want, body)
		}
	}
	// Only the caller's own grants. Who else holds one is a different route.
	if strings.Contains(body, adminRevoked) {
		t.Errorf("/me leaked another account's grant: %s", body)
	}
}

// TestAdminMatrixCoversEveryAdminRoute reads this package's own registrations
// and fails when an /api/admin route has no entry above.
//
// It reads every file in the package rather than handler.go alone: which file
// holds a registration is a readability choice, and a matrix that stops seeing
// half of them looks exactly like coverage.
func TestAdminMatrixCoversEveryAdminRoute(t *testing.T) {
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
		t.Fatalf("read the route registrations: %v", err)
	}
	found := regexp.MustCompile(`"(GET|POST|PATCH|PUT|DELETE) (/api/admin[^"]*)"`).
		FindAllStringSubmatch(string(body), -1)
	if len(found) == 0 {
		t.Fatal("no admin routes found in this package; the pattern this test relies on has changed")
	}

	covered := make(map[string]bool, len(adminRoutes))
	for _, c := range adminRoutes {
		covered[c.method+" "+c.path] = true
	}
	registered := make(map[string]bool, len(found))
	var missing []string
	for _, m := range found {
		key := m[1] + " " + m[2]
		registered[key] = true
		if !covered[key] {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	for _, key := range missing {
		t.Errorf("%s is registered but has no admin authorization case", key)
	}
	for _, c := range adminRoutes {
		if key := c.method + " " + c.path; !registered[key] {
			t.Errorf("%s has an admin authorization case but is not registered", key)
		}
	}
}
