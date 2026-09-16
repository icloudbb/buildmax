package accountlifecycle_test

import (
	"context"
	"testing"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	coreschedule "github.com/icloudbb/buildmax/internal/core/schedule"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/accountlifecycle"
)

func newService(t *testing.T) (*accountlifecycle.Service, *mock.MockUserStore, *mock.MockScheduleStore, *mock.MockTaskRunStore) {
	t.Helper()
	users := &mock.MockUserStore{ByID: map[string]*coreidentity.User{
		"u1": {ID: "u1", Email: "u1@example.com"},
	}}
	schedules := &mock.MockScheduleStore{Schedules: []coreschedule.Schedule{
		{ID: "s1", SpaceID: "sp1", CreatedBy: "u1", Enabled: true},
		{ID: "s2", SpaceID: "sp1", CreatedBy: "u1", Enabled: true},
	}}
	runs := &mock.MockTaskRunStore{
		TaskList: []coretask.Task{{ID: "t1", SpaceID: "sp1"}},
		Runs: []coretask.Run{
			{ID: "r1", TaskID: "t1", CreatedBy: "u1", Status: string(coretask.RunStatusRunning)},
		},
	}
	svc := &accountlifecycle.Service{
		Users:     users,
		Sessions:  &mock.MockAuthSessionStore{},
		Webhooks:  &mock.MockUserWebhookKeyStore{Metas: map[string][]coreidentity.WebhookKeyMeta{"u1": {{KeyID: "k1"}, {KeyID: "k2"}}}},
		Schedules: schedules,
		Runs:      runs,
		Spaces: &mock.MockSpaceStore{
			Spaces:  []corespace.Space{{ID: "sp1"}},
			Members: []corespace.Member{{SpaceID: "sp1", UserID: "u1", Role: corespace.RoleOwner}},
		},
		Now: func() time.Time { return time.Unix(1000, 0).UTC() },
	}
	return svc, users, schedules, runs
}

func TestDisableRetiresKeysPausesSchedulesAndCancelsRuns(t *testing.T) {
	svc, users, schedules, runs := newService(t)

	res, err := svc.Disable(context.Background(), "u1", accountlifecycle.DisableOptions{RetireWebhookKeys: true})
	if err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if users.ByID["u1"].DisabledAt == nil {
		t.Error("account gate not set")
	}
	if res.WebhookKeysRetired != 2 {
		t.Errorf("webhook_keys_retired = %d, want 2", res.WebhookKeysRetired)
	}
	if res.SchedulesPaused != 2 {
		t.Errorf("schedules_paused = %d, want 2", res.SchedulesPaused)
	}
	for _, s := range schedules.Schedules {
		if s.Enabled || s.PauseReason != coreschedule.PauseReasonCreatorDisabled {
			t.Errorf("schedule %s: enabled=%v reason=%q, want paused with creator_disabled", s.ID, s.Enabled, s.PauseReason)
		}
	}
	if res.RunsCanceled != 1 {
		t.Errorf("runs_canceled = %d, want 1", res.RunsCanceled)
	}
	if runs.Runs[0].CancelReason != coretask.CancelReasonCreatorDisabled {
		t.Errorf("run cancel_reason = %q, want creator_disabled", runs.Runs[0].CancelReason)
	}
}

func TestDisableSuspensionKeepsWebhookKeys(t *testing.T) {
	svc, _, _, _ := newService(t)
	res, err := svc.Disable(context.Background(), "u1", accountlifecycle.DisableOptions{RetireWebhookKeys: false})
	if err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if res.WebhookKeysRetired != 0 {
		t.Errorf("a suspension retired %d webhook keys, want 0", res.WebhookKeysRetired)
	}
}

func TestEnableReopensGateWithoutResurrecting(t *testing.T) {
	svc, users, schedules, _ := newService(t)
	if _, err := svc.Disable(context.Background(), "u1", accountlifecycle.DisableOptions{}); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if err := svc.Enable(context.Background(), "u1"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if users.ByID["u1"].DisabledAt != nil {
		t.Error("gate not reopened")
	}
	// The paused schedules stay paused: enabling the account does not resurrect
	// derived automation.
	for _, s := range schedules.Schedules {
		if s.Enabled {
			t.Errorf("schedule %s was resurrected by re-enable", s.ID)
		}
	}
}

func TestImpactCountsAndSoleOwnership(t *testing.T) {
	svc, _, _, _ := newService(t)
	impact, err := svc.Impact(context.Background(), "u1")
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if impact.WebhookKeys != 2 {
		t.Errorf("webhook_keys = %d, want 2", impact.WebhookKeys)
	}
	if impact.EnabledSchedules != 2 {
		t.Errorf("enabled_schedules = %d, want 2", impact.EnabledSchedules)
	}
	if impact.ActiveRunsByStatus[string(coretask.RunStatusRunning)] != 1 {
		t.Errorf("active RUNNING = %d, want 1", impact.ActiveRunsByStatus[string(coretask.RunStatusRunning)])
	}
	if len(impact.SoleOwnedSpaceIDs) != 1 || impact.SoleOwnedSpaceIDs[0] != "sp1" {
		t.Errorf("sole_owned = %v, want [sp1]", impact.SoleOwnedSpaceIDs)
	}
	if len(impact.Memberships) != 1 || impact.Memberships[0].Role != corespace.RoleOwner {
		t.Errorf("memberships = %+v, want one owner membership", impact.Memberships)
	}
	if impact.CancellationBound == "" {
		t.Error("cancellation_bound not reported")
	}
}
