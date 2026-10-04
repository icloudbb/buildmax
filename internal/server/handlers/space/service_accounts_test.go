package space

import (
	"encoding/json"
	"net/http"
	"testing"

	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/accountlifecycle"
	"github.com/icloudbb/buildmax/internal/service/audit"
	"github.com/icloudbb/buildmax/internal/testsupport"
)

func serviceAccountFixture(t *testing.T) (*http.ServeMux, *mock.MockAuditStore) {
	t.Helper()
	users := &mock.MockUserStore{ByID: map[string]*coreidentity.User{
		matrixOwner: {ID: matrixOwner, Email: "owner@example.com"},
		matrixAdmin: {ID: matrixAdmin, Email: "admin@example.com"},
	}}
	spaces := &mock.MockSpaceStore{
		Spaces: []corespace.Space{{ID: matrixSpace, Name: "Team", CreatedBy: matrixOwner}},
		Members: []corespace.Member{
			{SpaceID: matrixSpace, UserID: matrixOwner, Role: corespace.RoleOwner},
			{SpaceID: matrixSpace, UserID: matrixAdmin, Role: corespace.RoleAdmin},
		},
	}
	store := &mock.MockAuditStore{}
	h := New(Config{
		JWTSecret:       matrixSecret,
		Spaces:          spaces,
		Users:           users,
		ServiceAccounts: &mock.MockServiceAccountStore{Users: users, Spaces: spaces},
		Lifecycle:       &accountlifecycle.Service{Users: users},
		Audits:          store,
		Audit:           audit.NewRecorder(store),
	})
	mux := http.NewServeMux()
	h.Register(mux)
	return mux, store
}

// TestServiceAccountRoutesAreAudited drives create, rename, re-sponsor,
// disable, and re-enable over HTTP and checks each left its own event.
func TestServiceAccountRoutesAreAudited(t *testing.T) {
	mux, store := serviceAccountFixture(t)
	admin := "Bearer " + testsupport.SignJWT(matrixAdmin, matrixSecret)
	base := "/api/spaces/" + matrixSpace + "/service-accounts"

	rec := doJSON(t, mux, http.MethodPost, base, admin, `{"name":"HR operations"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", rec.Code, rec.Body.String())
	}
	var created serviceAccountResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.SponsorUserID == nil || *created.SponsorUserID != matrixAdmin || created.NeedsSponsor {
		t.Fatalf("created = %+v", created)
	}
	if ev := firstEvent(t, store, coreaudit.ServiceAccountCreated); ev.TargetID != created.ID || ev.ActorID != matrixAdmin || ev.SpaceID != matrixSpace {
		t.Fatalf("created event = %+v", ev)
	}

	one := base + "/" + created.ID
	if rec := doJSON(t, mux, http.MethodPatch, one, admin, `{"name":"People ops","sponsor_user_id":"`+matrixOwner+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("patch = %d: %s", rec.Code, rec.Body.String())
	}
	firstEvent(t, store, coreaudit.ServiceAccountRenamed)
	if ev := firstEvent(t, store, coreaudit.ServiceAccountSponsorChanged); ev.Detail != matrixOwner {
		t.Fatalf("sponsor event detail = %q, want the new sponsor", ev.Detail)
	}

	rec = doJSON(t, mux, http.MethodPut, one+"/state", admin, `{"disabled":true}`)
	var state serviceAccountResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &state)
	if rec.Code != http.StatusOK || state.DisabledAt == nil {
		t.Fatalf("disable = %d %+v", rec.Code, state)
	}
	firstEvent(t, store, coreaudit.ServiceAccountDisabled)
	if rec := doJSON(t, mux, http.MethodPut, one+"/state", admin, `{"disabled":false}`); rec.Code != http.StatusOK {
		t.Fatalf("enable = %d", rec.Code)
	}
	firstEvent(t, store, coreaudit.ServiceAccountEnabled)

	// The roster marks it, so a client can leave it out of people pickers.
	rec = doJSON(t, mux, http.MethodGet, "/api/spaces/"+matrixSpace+"/members", admin, "")
	var members []spaceMemberResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &members); err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{}
	for _, m := range members {
		kinds[m.UserID] = m.UserKind
	}
	if kinds[created.ID] != coreidentity.KindService || kinds[matrixOwner] != coreidentity.KindHuman {
		t.Fatalf("member kinds = %v", kinds)
	}
}
