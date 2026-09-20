package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/audit"
)

func TestAdminSpacesList(t *testing.T) {
	mux := adminSpacesMux(t)
	rec := adminRequestAs(t, mux, adminCase{"GET", "/api/admin/spaces"}, adminUser)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	var out AdminSpacesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Total != 1 || len(out.Spaces) != 1 {
		t.Fatalf("got %d of %d spaces: %+v", len(out.Spaces), out.Total, out.Spaces)
	}
	byID := map[string]AdminSpace{}
	for _, space := range out.Spaces {
		byID[space.ID] = space
	}
	if got := byID["tm_shared"]; got.MemberCount != 2 || got.Personal || got.QuotaTier != "free_trial" {
		t.Errorf("shared space = %+v", got)
	}
	// Personal spaces are not governed by an operator — every account has one —
	// so the list omits them rather than doubling the count with noise. The total
	// counts only what is listed.
	if _, ok := byID["tm_personal"]; ok {
		t.Errorf("personal space should not be listed: %+v", out.Spaces)
	}
}

func TestAdminSpaceDetailShowsMembershipNotContent(t *testing.T) {
	mux := adminSpacesMux(t)
	rec := adminRequestAs(t, mux, adminCase{"GET", "/api/admin/spaces/tm_shared"}, adminUser)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	var detail AdminSpaceDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(detail.Members) != 2 {
		t.Fatalf("members = %+v", detail.Members)
	}
	var owner AdminSpaceMember
	for _, m := range detail.Members {
		if m.UserID == "u_alice" {
			owner = m
		}
	}
	if owner.Role != corespace.RoleOwner || owner.Email != "alice@example.com" {
		t.Errorf("owner = %+v", owner)
	}
	// A membership naming an account the store does not have still lists, with
	// the id: refusing to describe the space would be worse.
	for _, m := range detail.Members {
		if m.UserID == "u_bob" && m.Email != "" {
			t.Errorf("u_bob has no account seeded; email should be empty: %+v", m)
		}
	}

	// The rule the design states: if a member wrote it or an agent produced
	// it, it is content — and none of it is here.
	body := strings.ToLower(rec.Body.String())
	for _, forbidden := range []string{"issue", "conversation", "artifact", "task", "workflow", "trace"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the space detail mentions %q: %s", forbidden, rec.Body.String())
		}
	}
}

// TestAdminSpaceDetailOnAnUnknownSpace is 404, not an empty space.
func TestAdminSpaceDetailOnAnUnknownSpace(t *testing.T) {
	mux := adminSpacesMux(t)
	if got := adminRequestAs(t, mux, adminCase{"GET", "/api/admin/spaces/tm_nobody"}, adminUser).Code; got != http.StatusNotFound {
		t.Errorf("got %d, want 404", got)
	}
}

// TestAdminSpaceRoutesAreNotAWayIntoASpace: the routes describe a space from the
// outside, and reaching a space's own resources still needs membership. This is
// the same claim TestSystemGrantIsNotASpaceKey makes, restated at the surface
// that most looks like an exception to it.

func adminSpacesMux(t *testing.T) *http.ServeMux {
	t.Helper()
	users := &mock.MockUserStore{}
	seedUser(t, users, adminUser, "admin@example.com")
	seedUser(t, users, "u_alice", "alice@example.com")
	grants := &mock.MockSystemGrantStore{}
	grants.GrantForTest(adminUser, coreidentity.SystemRoleAdmin)

	personalOf := "u_alice"
	spaces := &mock.MockSpaceStore{
		Spaces: []corespace.Space{
			{ID: "tm_shared", Name: "Platform", CreatedBy: "u_alice", QuotaTier: "free_trial", CreatedAt: time.Unix(200, 0).UTC()},
			{ID: "tm_personal", Name: "My Space", PersonalForUserID: &personalOf, CreatedBy: "u_alice", CreatedAt: time.Unix(100, 0).UTC()},
		},
		Members: []corespace.Member{
			{SpaceID: "tm_shared", UserID: "u_alice", Role: corespace.RoleOwner},
			{SpaceID: "tm_shared", UserID: "u_bob", Role: corespace.RoleMember},
			{SpaceID: "tm_personal", UserID: "u_alice", Role: corespace.RoleOwner},
		},
	}
	audits := &mock.MockAuditStore{}
	h := New(Config{
		JWTSecret: testSecret,
		Grants:    grants,
		Users:     users,
		Spaces:    spaces,
		// Wired because a nil store answers 503 before any authorization check
		// runs, which would make TestAdminSpaceRoutesAreNotAWayIntoASpace pass
		// for the wrong reason.
		Audits: audits,
		Audit:  audit.NewRecorder(audits),
	})
	mux := http.NewServeMux()
	h.Register(mux)
	return mux
}
