package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	corequota "github.com/icloudbb/buildmax/internal/core/quota"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/audit"
	"github.com/icloudbb/buildmax/internal/service/quota"
)

func adminQuotaMux(t *testing.T) (*http.ServeMux, *mock.MockSpaceStore, *mock.MockAuditStore) {
	t.Helper()
	users := &mock.MockUserStore{}
	seedUser(t, users, adminUser, "admin@example.com")
	seedUser(t, users, adminSpaceOwner, "owner@example.com")
	grants := &mock.MockSystemGrantStore{}
	grants.GrantForTest(adminUser, coreidentity.SystemRoleAdmin)
	personalOf := adminSpaceOwner
	spaces := &mock.MockSpaceStore{
		Spaces: []corespace.Space{
			{ID: "tm_shared", Name: "Platform", QuotaTier: "free_trial"},
			{ID: "tm_personal", Name: "My Space", PersonalForUserID: &personalOf, QuotaTier: "free_trial"},
		},
		Members: []corespace.Member{
			{SpaceID: "tm_shared", UserID: adminSpaceOwner, Role: corespace.RoleOwner},
			{SpaceID: "tm_personal", UserID: adminSpaceOwner, Role: corespace.RoleOwner},
		},
	}
	audits := &mock.MockAuditStore{}
	h := New(Config{
		JWTSecret: testSecret,
		Grants:    grants,
		Users:     users,
		Spaces:    spaces,
		Audits:    audits,
		Audit:     audit.NewRecorder(audits),
		Quota: &quota.Service{
			SpaceStore:  spaces,
			UsageReader: &mock.MockUsageReader{},
			TierStore: &mock.MockTierCatalog{Tiers: []corequota.Tier{
				{TierName: "free_trial", MaxRunsPerPeriod: 10, MaxTokensPerPeriod: 100_000, PeriodDays: 30},
				{TierName: "pro", MaxRunsPerPeriod: 1000, MaxTokensPerPeriod: 10_000_000, PeriodDays: 30},
			}},
			DefaultTier: "free_trial",
			Audit:       audits,
		},
	})
	mux := http.NewServeMux()
	h.Register(mux)
	return mux, spaces, audits
}

func TestAdminQuotaTiersList(t *testing.T) {
	mux, _, _ := adminQuotaMux(t)
	rec := adminRequestAs(t, mux, adminCase{"GET", "/api/admin/quota-tiers"}, adminUser)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	var out AdminQuotaTiersResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Tiers) != 2 || out.Tiers[1].TierName != "pro" || out.Tiers[1].MaxRunsPerPeriod != 1000 {
		t.Errorf("tiers = %+v", out.Tiers)
	}
}

func TestAdminSetSpaceQuotaTier(t *testing.T) {
	for _, spaceID := range []string{"tm_shared", "tm_personal"} {
		t.Run(spaceID, func(t *testing.T) {
			mux, spaces, audits := adminQuotaMux(t)
			rec := adminRequestJSON(t, mux, http.MethodPut, "/api/admin/spaces/"+spaceID+"/quota-tier", adminUser, `{"tier":"pro"}`)
			if rec.Code != http.StatusNoContent {
				t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
			}
			space, _ := spaces.GetSpace(t.Context(), spaceID)
			if space.QuotaTier != "pro" {
				t.Errorf("tier = %q, want pro", space.QuotaTier)
			}
			var changed []coreaudit.Event
			for _, e := range audits.Events {
				if e.Action == coreaudit.SpaceQuotaTierChanged {
					changed = append(changed, e)
				}
			}
			if len(changed) != 1 || changed[0].ActorID != adminUser || changed[0].SpaceID != spaceID || changed[0].Detail != "free_trial -> pro" {
				t.Errorf("tier change events = %+v", changed)
			}
		})
	}
}

func TestAdminSetSpaceQuotaTierRefusals(t *testing.T) {
	tests := []struct {
		name     string
		caller   string
		path     string
		body     string
		wantCode int
		wantText string
	}{
		{"a space owner is not an administrator", adminSpaceOwner, "/api/admin/spaces/tm_shared/quota-tier", `{"tier":"pro"}`, http.StatusForbidden, ""},
		{"an unknown space", adminUser, "/api/admin/spaces/tm_nobody/quota-tier", `{"tier":"pro"}`, http.StatusNotFound, "space not found"},
		{"an unknown tier names the valid ones", adminUser, "/api/admin/spaces/tm_shared/quota-tier", `{"tier":"gold"}`, http.StatusBadRequest, "free_trial, pro"},
		{"no tier at all", adminUser, "/api/admin/spaces/tm_shared/quota-tier", `{}`, http.StatusBadRequest, "valid tiers"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mux, spaces, _ := adminQuotaMux(t)
			rec := adminRequestJSON(t, mux, http.MethodPut, tc.path, tc.caller, tc.body)
			if rec.Code != tc.wantCode {
				t.Fatalf("got %d, want %d: %s", rec.Code, tc.wantCode, rec.Body.String())
			}
			if tc.wantText != "" && !strings.Contains(rec.Body.String(), tc.wantText) {
				t.Errorf("body %s should mention %q", rec.Body.String(), tc.wantText)
			}
			if space, _ := spaces.GetSpace(t.Context(), "tm_shared"); space.QuotaTier != "free_trial" {
				t.Errorf("a refusal changed the tier to %q", space.QuotaTier)
			}
		})
	}
}
