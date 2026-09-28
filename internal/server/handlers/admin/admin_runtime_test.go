package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/audit"
	"github.com/icloudbb/buildmax/internal/util"
)

// leakMarker stands for everything a Space's members wrote or an Agent
// produced. None of it may reach an administration response.
const leakMarker = "LEAKMARK-NIGHTJAR"

func runtimeMux(t *testing.T, runs coretask.RunStore) *http.ServeMux {
	t.Helper()
	users := &mock.MockUserStore{}
	seedUser(t, users, adminUser, "admin@example.com")
	seedUser(t, users, "u_alice", "alice@example.com")
	seedUser(t, users, "u_bob", "bob@example.com")
	grants := &mock.MockSystemGrantStore{}
	grants.GrantForTest(adminUser, coreidentity.SystemRoleAdmin)
	spaces := &mock.MockSpaceStore{
		Spaces: []corespace.Space{
			{ID: "sp_team", Name: "Payments"},
			{ID: "sp_personal", Name: "My Space", PersonalForUserID: util.Ptr("u_bob")},
		},
		Members: []corespace.Member{
			{SpaceID: "sp_team", UserID: "u_alice", Role: corespace.RoleOwner},
			{SpaceID: "sp_team", UserID: "u_bob", Role: corespace.RoleMember},
			{SpaceID: "sp_personal", UserID: "u_bob", Role: corespace.RoleOwner},
		},
	}
	audits := &mock.MockAuditStore{}
	h := New(Config{
		JWTSecret: testSecret, Grants: grants, Users: users, Spaces: spaces,
		TaskRuns: runs, Audits: audits, Audit: audit.NewRecorder(audits),
	})
	mux := http.NewServeMux()
	h.Register(mux)
	return mux
}

func drillRuns() *mock.MockTaskRunStore {
	now := time.Now().UTC()
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	ended := func(d time.Duration) *time.Time { t := ago(d); return &t }
	secret := leakMarker + " https://buildmax-worker-api.internal:5679/api/worker/task-runs/x"
	return &mock.MockTaskRunStore{
		TaskList: []coretask.Task{
			{ID: "t_team", SpaceID: "sp_team", Input: leakMarker, Title: leakMarker},
			{ID: "t_personal", SpaceID: "sp_personal", Input: leakMarker},
		},
		Runs: []coretask.Run{
			{ID: "r_waiting", TaskID: "t_team", Status: "SCHEDULED", Input: leakMarker, CreatedAt: ago(30 * time.Minute)},
			{ID: "r_pending", TaskID: "t_team", Status: "PENDING", Input: leakMarker, CreatedAt: ago(5 * time.Minute)},
			{ID: "r_storage", TaskID: "t_personal", Status: "FAILED", Input: leakMarker, ErrorMessage: &secret,
				FailureClass: string(coretask.FailureInfrastructure), CreatedAt: ago(2 * time.Hour), EndedAt: ended(time.Hour)},
			{ID: "r_old", TaskID: "t_team", Status: "FAILED", ErrorMessage: &secret,
				FailureClass: string(coretask.FailureModel), CreatedAt: ago(72 * time.Hour), EndedAt: ended(71 * time.Hour)},
			{ID: "r_done", TaskID: "t_team", Status: "SUCCEEDED", Output: util.Ptr(leakMarker), CreatedAt: ago(time.Hour)},
		},
	}
}

func TestRuntimeSpacesListsAffectedSpacesWithOwnersAndNoContent(t *testing.T) {
	mux := runtimeMux(t, drillRuns())
	code, body := getAs(t, mux, "/api/admin/runtime/spaces", adminUser)
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", code, body)
	}
	if strings.Contains(body, leakMarker) || strings.Contains(body, "worker-api.internal") {
		t.Fatalf("response carries Space content: %s", body)
	}
	var resp AdminSpacesAttentionResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Total != 2 || len(resp.Spaces) != 2 {
		t.Fatalf("spaces = %+v, want the team Space and the personal one", resp.Spaces)
	}
	team, personal := resp.Spaces[0], resp.Spaces[1]
	if team.SpaceID != "sp_team" || team.Active["SCHEDULED"] != 1 || team.Active["PENDING"] != 1 {
		t.Errorf("team = %+v, want its SCHEDULED and PENDING runs", team)
	}
	if len(team.Owners) != 1 || team.Owners[0].Email != "alice@example.com" {
		t.Errorf("team owners = %+v, want alice only", team.Owners)
	}
	// A failure outside the window is history, not something needing attention.
	if len(team.Failures) != 0 {
		t.Errorf("team failures = %v, want none inside the window", team.Failures)
	}
	if !personal.Personal || personal.Failures[string(coretask.FailureInfrastructure)] != 1 {
		t.Errorf("personal = %+v, want the personal Space with its infrastructure failure", personal)
	}
	if len(personal.Owners) != 1 || personal.Owners[0].Email != "bob@example.com" {
		t.Errorf("personal owners = %+v, want bob", personal.Owners)
	}
}

func TestSystemStatusReportsRuntimeWithoutContent(t *testing.T) {
	mux := runtimeMux(t, drillRuns())
	code, body := getAs(t, mux, "/api/admin/system", adminUser)
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", code, body)
	}
	if strings.Contains(body, leakMarker) {
		t.Fatalf("response carries Space content: %s", body)
	}
	var resp AdminSystemResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	rt := resp.Runtime
	if rt == nil {
		t.Fatal("runtime missing")
	}
	if rt.OldestPendingAt == nil || rt.OldestUnstartedAt == nil {
		t.Errorf("runtime = %+v, want both stall timestamps", rt)
	}
	if rt.Failures[string(coretask.FailureInfrastructure)] != 1 || rt.Failures[string(coretask.FailureModel)] != 0 {
		t.Errorf("failures = %v, want only the in-window infrastructure failure", rt.Failures)
	}
	if rt.StaleAfterSeconds != int(coretask.WorkerLivenessGrace/time.Second) || rt.FailureWindowHours != 24 {
		t.Errorf("runtime bounds = %+v", rt)
	}
}

type unavailableRuntime struct{ *mock.MockTaskRunStore }

func (unavailableRuntime) RuntimeSummary(context.Context, time.Time, time.Time) (coretask.RuntimeSummary, error) {
	return coretask.RuntimeSummary{}, errors.New("database unavailable")
}

// Unavailable is not the same answer as "nothing stalled": the field is left
// out so Portal can say so.
func TestSystemStatusOmitsRuntimeItCannotRead(t *testing.T) {
	mux := runtimeMux(t, unavailableRuntime{drillRuns()})
	code, body := getAs(t, mux, "/api/admin/system", adminUser)
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", code, body)
	}
	if strings.Contains(body, `"runtime"`) {
		t.Errorf("body = %s, want no runtime when it could not be read", body)
	}
}
