package space

import (
	"encoding/json"
	"net/http"
	"testing"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	coreschedule "github.com/icloudbb/buildmax/internal/core/schedule"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/testsupport"
)

// A schedule that delivers through an Assistant decides what the Space's bot
// says to someone, so a member may read it and pause or resume it, but setting
// a target or editing such a schedule needs the right to manage Assistants.
func TestScheduleDeliveryNeedsAssistantManagement(t *testing.T) {
	spaceID := "tm_1"
	schedules := &mock.MockScheduleStore{
		Schedules: []coreschedule.Schedule{{
			ID: "sch_1", SpaceID: spaceID, ExecutorKind: coreschedule.ExecutorAgent, ExecutorID: "ag_1",
			CreatedBy: "u1", Input: "report", CronExpr: "0 9 * * *", Timezone: "UTC", Enabled: true,
			Delivery: &coreschedule.Delivery{AssistantID: "as_1", RequesterID: "u3"},
		}},
		Deliveries: []coreschedule.FireDelivery{{ID: "d1", ScheduleID: "sch_1", FireRef: "t1", Status: coreschedule.DeliverySkipped, Reason: coreschedule.SkipNoBot}},
	}
	h := New(Config{
		JWTSecret: scheduleTestSecret,
		Spaces: &mock.MockSpaceStore{
			Spaces: []corespace.Space{{ID: spaceID, Name: "Space", CreatedBy: "u1"}},
			Members: []corespace.Member{
				{SpaceID: spaceID, UserID: "u1", Role: corespace.RoleOwner},
				{SpaceID: spaceID, UserID: "u2", Role: corespace.RoleMember},
			},
		},
		Agents:    &mock.MockAgentStore{Agents: []agentdef.Agent{{ID: "ag_1", SpaceID: spaceID}}},
		Schedules: schedules,
	})
	mux := http.NewServeMux()
	h.Register(mux)
	owner := "Bearer " + testsupport.SignJWT("u1", scheduleTestSecret)
	member := "Bearer " + testsupport.SignJWT("u2", scheduleTestSecret)
	base := "/api/spaces/tm_1/schedules"
	create := `{"executor_kind":"agent","executor_id":"ag_1","input":"report","cron_expr":"0 9 * * *","timezone":"UTC","delivery":{"assistant_id":"as_1","requester_id":"u3"}}`

	for _, tc := range []struct {
		name, token, method, path, body string
		want                            int
	}{
		{"member sets a target", member, http.MethodPost, base, create, http.StatusForbidden},
		{"member edits a delivering schedule", member, http.MethodPatch, base + "/sch_1", `{"input":"say something else"}`, http.StatusForbidden},
		{"member removes the target", member, http.MethodPatch, base + "/sch_1", `{"delivery":{"assistant_id":""}}`, http.StatusForbidden},
		{"member pauses it", member, http.MethodPatch, base + "/sch_1", `{"enabled":false}`, http.StatusOK},
		// Past authorization, a deployment without Assistants refuses the target.
		{"owner sets a target", owner, http.MethodPost, base, create, http.StatusServiceUnavailable},
		{"owner removes the target", owner, http.MethodPatch, base + "/sch_1", `{"delivery":{"assistant_id":""}}`, http.StatusOK},
	} {
		if rec := doJSON(t, mux, tc.method, tc.path, tc.token, tc.body); rec.Code != tc.want {
			t.Errorf("%s: status = %d, want %d: %s", tc.name, rec.Code, tc.want, rec.Body.String())
		}
	}
	if schedules.Schedules[0].Delivery != nil {
		t.Errorf("delivery = %+v after the owner removed it", schedules.Schedules[0].Delivery)
	}

	rec := doJSON(t, mux, http.MethodGet, base+"/sch_1/deliveries", member, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list deliveries status = %d: %s", rec.Code, rec.Body.String())
	}
	var got scheduleDeliveryListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Total != 1 || got.Deliveries[0].Reason != coreschedule.SkipNoBot {
		t.Errorf("deliveries = %+v", got)
	}
	if rec := doJSON(t, mux, http.MethodGet, "/api/spaces/tm_1/schedules/sch_missing/deliveries", member, ""); rec.Code != http.StatusNotFound {
		t.Errorf("another schedule's deliveries status = %d, want 404", rec.Code)
	}
}
