package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/core/eligibility"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	coreschedule "github.com/icloudbb/buildmax/internal/core/schedule"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/mock"
)

// A service account is a user row, so the unattended gates treat work created
// by one exactly as work created by a person: an active one passes, a disabled
// one is refused. Nothing here may special-case the kind. See
// docs/design/space-assistants.md §6.2.

func serviceAccountChecker(userID, spaceID string, disabledAt *time.Time) eligibility.Checker {
	return eligibility.New(
		&mock.MockUserStore{ByID: map[string]*coreidentity.User{
			userID: {ID: userID, Kind: coreidentity.KindService, DisabledAt: disabledAt},
		}},
		&mock.MockSpaceStore{Members: []corespace.Member{
			{SpaceID: spaceID, UserID: userID, Role: corespace.RoleMember},
		}},
	)
}

// Admission: the schedule fire gate.
func TestServiceAccountAtAdmission(t *testing.T) {
	t0 := time.Date(2000, 1, 1, 9, 0, 0, 0, time.UTC)
	disabledAt := t0.Add(-time.Hour)
	for _, tc := range []struct {
		name     string
		disabled *time.Time
		admitted int
	}{
		{"active", nil, 1},
		{"disabled", &disabledAt, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sched := hourlySchedule(t0)
			store := newFakeScheduleStore(sched)
			admitter := &fakeAdmitter{}
			d := newTestDispatcher(t, store, admitter, t0)
			d.WithEligibility(serviceAccountChecker(sched.CreatedBy, sched.SpaceID, tc.disabled))

			d.fireOne(context.Background(), *sched, t0)

			if admitter.count() != tc.admitted {
				t.Fatalf("admitted %d, want %d", admitter.count(), tc.admitted)
			}
			if tc.disabled != nil && store.get(sched.ID).PauseReason != coreschedule.PauseReasonCreatorDisabled {
				t.Errorf("pause_reason = %q, want creator_disabled", store.get(sched.ID).PauseReason)
			}
		})
	}
}

// Dispatch: the scheduler's claim of a PENDING run.
func TestServiceAccountAtDispatch(t *testing.T) {
	disabledAt := time.Unix(1, 0).UTC()
	for _, tc := range []struct {
		name       string
		disabled   *time.Time
		dispatched bool
	}{
		{"active", nil, true},
		{"disabled", &disabledAt, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spy := newSpyTaskRunStore("r_service123456789012345")
			spy.pendingRun.CreatedBy = "u_service"
			runner := &recordingRunner{}
			s, err := NewSchedulerWithPollInterval(spy, runner, nil, 10*time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			s.WithEligibility(serviceAccountChecker("u_service", "tm_test", tc.disabled)).Start()
			time.Sleep(25 * time.Millisecond)
			s.Stop(context.Background())

			if got := runnerCalls(runner) > 0; got != tc.dispatched {
				t.Fatalf("dispatched = %v, want %v", got, tc.dispatched)
			}
			if !tc.dispatched {
				spy.mu.Lock()
				defer spy.mu.Unlock()
				if spy.lastUpdateStatus == nil || spy.lastUpdateStatus.cancelReason != coretask.CancelReasonCreatorDisabled {
					t.Fatalf("run not canceled as creator_disabled: %+v", spy.lastUpdateStatus)
				}
			}
		})
	}
}

// Reconcile: the sweep over active runs.
func TestServiceAccountAtReconcile(t *testing.T) {
	const space = "sp_1"
	disabledAt := time.Unix(1, 0).UTC()
	runs := &mock.MockTaskRunStore{
		TaskList: []coretask.Task{{ID: "t_on", SpaceID: space}, {ID: "t_off", SpaceID: space}},
		Runs: []coretask.Run{
			{ID: "r_on", TaskID: "t_on", Status: string(coretask.RunStatusRunning), CreatedBy: "u_svc_on"},
			{ID: "r_off", TaskID: "t_off", Status: string(coretask.RunStatusRunning), CreatedBy: "u_svc_off"},
		},
	}
	users := &mock.MockUserStore{ByID: map[string]*coreidentity.User{
		"u_svc_on":  {ID: "u_svc_on", Kind: coreidentity.KindService},
		"u_svc_off": {ID: "u_svc_off", Kind: coreidentity.KindService, DisabledAt: &disabledAt},
	}}
	spaces := &mock.MockSpaceStore{Members: []corespace.Member{
		{SpaceID: space, UserID: "u_svc_on", Role: corespace.RoleMember},
		{SpaceID: space, UserID: "u_svc_off", Role: corespace.RoleMember},
	}}

	NewEligibilityReconciler(runs, eligibility.New(users, spaces), time.Minute).Sweep(context.Background(), time.Now().UTC())

	byID := map[string]coretask.Run{}
	for _, r := range runs.Runs {
		byID[r.ID] = r
	}
	if byID["r_on"].CancelRequestedAt != nil {
		t.Error("an active service account's run was asked to stop")
	}
	if got := byID["r_off"]; got.CancelRequestedAt == nil || got.CancelReason != coretask.CancelReasonCreatorDisabled {
		t.Errorf("disabled service account's run: requested=%v reason=%q", got.CancelRequestedAt != nil, got.CancelReason)
	}
}
