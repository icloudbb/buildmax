package admin

import (
	"encoding/json"
	"net/http"
	"testing"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
)

// TestAdminServiceAccounts: the user list marks a service account, a login
// code is refused for it, and an administrator can disable it like anyone.
func TestAdminServiceAccounts(t *testing.T) {
	f := newDisableFixture(t)
	sponsor := adminUser
	f.target.Kind = coreidentity.KindService
	f.target.Email = ""
	f.target.SponsorUserID = &sponsor

	rec := f.do(t, "GET", "/api/admin/users", adminUser, "")
	var page AdminUsersResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	kinds := map[string]string{}
	for _, u := range page.Users {
		kinds[u.ID] = u.Kind
	}
	if kinds[f.target.ID] != coreidentity.KindService || kinds[adminUser] != coreidentity.KindHuman {
		t.Fatalf("kinds = %v", kinds)
	}

	rec = f.do(t, "POST", "/api/admin/users/"+f.target.ID+"/login-code", adminUser, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("login code for a service account = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if len(f.codes.Codes) != 0 {
		t.Fatal("a login code was minted for a service account")
	}

	rec = f.do(t, "PUT", "/api/admin/users/"+f.target.ID+"/state", adminUser, `{"disabled":true}`)
	if rec.Code != http.StatusOK || !f.users.ByID[f.target.ID].Disabled() {
		t.Fatalf("admin disable = %d: %s", rec.Code, rec.Body.String())
	}
}
