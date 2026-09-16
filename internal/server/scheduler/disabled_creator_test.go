package scheduler

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/core/eligibility"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/mock"
)

// runnerCalls reads how many workers the runner was asked to spawn.
func runnerCalls(r *recordingRunner) int {
	calls, _ := r.observed()
	return calls
}

// enabledMember builds authority stores where userID is an enabled account and a
// member of the spy's task space, so it is eligible.
func enabledMember(userID string) eligibility.Checker {
	users := &mock.MockUserStore{ByID: map[string]*coreidentity.User{
		userID: {ID: userID, Email: userID + "@example.com"},
	}}
	spaces := &mock.MockSpaceStore{Members: []corespace.Member{
		{SpaceID: "tm_test", UserID: userID, Role: corespace.RoleMember},
	}}
	return eligibility.New(users, spaces)
}

// TestSchedulerDoesNotDispatchForAnIneligibleInitiator.
//
// Withdrawing authority — disabling the account, or removing it from the run's
// Space — has to stop work it queued, or the change means "stops signing in"
// rather than "stops acting". The run fails at dispatch rather than being left
// with no worker coming for it: a run nobody will ever pick up, sitting in a
// queue with no explanation, is worse than a terminal one that says why.
func TestSchedulerDoesNotDispatchForAnIneligibleInitiator(t *testing.T) {
	disabledAt := time.Unix(1, 0).UTC()

	tests := []struct {
		name    string
		elig    eligibility.Checker
		wantMsg string
	}{
		{
			name: "disabled account",
			elig: eligibility.New(
				&mock.MockUserStore{ByID: map[string]*coreidentity.User{
					"u_gone": {ID: "u_gone", DisabledAt: &disabledAt},
				}},
				&mock.MockSpaceStore{Members: []corespace.Member{
					{SpaceID: "tm_test", UserID: "u_gone", Role: corespace.RoleMember},
				}},
			),
			wantMsg: "disabled",
		},
		{
			name: "removed from the space",
			elig: eligibility.New(
				&mock.MockUserStore{ByID: map[string]*coreidentity.User{
					"u_gone": {ID: "u_gone", Email: "here@example.com"},
				}},
				&mock.MockSpaceStore{}, // enabled, but no membership row
			),
			wantMsg: "not a member",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spy := newSpyTaskRunStore("r_disabled12345678901234")
			spy.pendingRun.CreatedBy = "u_gone"
			runner := &recordingRunner{}

			s, err := NewSchedulerWithPollInterval(spy, runner, nil, 10*time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			s.WithEligibility(tc.elig).Start()
			time.Sleep(25 * time.Millisecond)
			s.Stop(context.Background())

			if runnerCalls(runner) != 0 {
				t.Errorf("a worker was spawned for an ineligible initiator: %d", runnerCalls(runner))
			}

			spy.mu.Lock()
			defer spy.mu.Unlock()
			if spy.lastUpdateStatus == nil {
				t.Fatal("the run was left with no explanation")
			}
			if spy.lastUpdateStatus.status != "FAILED" {
				t.Errorf("status = %q, want FAILED", spy.lastUpdateStatus.status)
			}
			if spy.lastUpdateStatus.errorMessage == nil || !strings.Contains(*spy.lastUpdateStatus.errorMessage, tc.wantMsg) {
				t.Errorf("the run should say why it did not start, got %v", spy.lastUpdateStatus.errorMessage)
			}
		})
	}
}

// TestSchedulerDispatchesForAnEligibleInitiator is the other half: the guard must
// not stop ordinary work, including when the deployment wires no eligibility
// check at all.
func TestSchedulerDispatchesForAnEligibleInitiator(t *testing.T) {
	for _, tc := range []struct {
		name string
		elig eligibility.Checker
	}{
		{"enabled member", enabledMember("u_active")},
		{"no eligibility check", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spy := newSpyTaskRunStore("r_active123456789012345")
			spy.pendingRun.CreatedBy = "u_active"
			runner := &recordingRunner{}

			s, err := NewSchedulerWithPollInterval(spy, runner, nil, 10*time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			s.WithEligibility(tc.elig).Start()
			time.Sleep(25 * time.Millisecond)
			s.Stop(context.Background())

			if runnerCalls(runner) == 0 {
				t.Error("the run was not dispatched")
			}
		})
	}
}
