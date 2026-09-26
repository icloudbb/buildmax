package scheduler

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/core/eligibility"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
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
// rather than "stops acting". The run reaches CANCELED, not FAILED: nothing went
// wrong, the account may no longer act, and the reason records which withdrawal
// it was.
func TestSchedulerDoesNotDispatchForAnIneligibleInitiator(t *testing.T) {
	disabledAt := time.Unix(1, 0).UTC()

	tests := []struct {
		name       string
		elig       eligibility.Checker
		wantReason string
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
			wantReason: coretask.CancelReasonCreatorDisabled,
		},
		{
			name: "removed from the space",
			elig: eligibility.New(
				&mock.MockUserStore{ByID: map[string]*coreidentity.User{
					"u_gone": {ID: "u_gone", Email: "here@example.com"},
				}},
				&mock.MockSpaceStore{}, // enabled, but no membership row
			),
			wantReason: coretask.CancelReasonCreatorNotMember,
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
			if spy.lastUpdateStatus.status != string(coretask.RunStatusCanceled) {
				t.Errorf("status = %q, want CANCELED", spy.lastUpdateStatus.status)
			}
			if spy.lastUpdateStatus.cancelReason != tc.wantReason {
				t.Errorf("cancel_reason = %q, want %q", spy.lastUpdateStatus.cancelReason, tc.wantReason)
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

// switchableChecker answers every Check with err, which a test can change
// between calls to model an authority store that recovers.
type switchableChecker struct {
	mu  sync.Mutex
	err error
}

func (c *switchableChecker) Check(context.Context, string, string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *switchableChecker) set(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.err = err
}

// unavailable is what the Checker returns when an authority store failed.
func unavailable() error {
	return fmt.Errorf("%w: load account: connection refused", eligibility.ErrUnavailable)
}

// TestSchedulerLeavesRunPendingWhenEligibilityIsUnavailable.
//
// When the scheduler cannot tell whether the initiator may still run work, it
// must not dispatch — that would spend authority on a guess — and it must not
// end the run either, because nothing is known to be wrong with it. The run stays
// PENDING and is dispatched once the store answers.
func TestSchedulerLeavesRunPendingWhenEligibilityIsUnavailable(t *testing.T) {
	spy := newSpyTaskRunStore("r_unknown12345678901234")
	runner := &recordingRunner{}
	elig := &switchableChecker{err: unavailable()}

	s, err := NewSchedulerWithPollInterval(spy, runner, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	s.WithEligibility(elig)

	s.pollOnce()
	s.inflight.Wait()
	if runnerCalls(runner) != 0 {
		t.Fatalf("a worker was spawned while eligibility was unknown: %d", runnerCalls(runner))
	}
	spy.mu.Lock()
	status := spy.pendingRun.Status
	ended := spy.lastUpdateStatus
	spy.pendingCalls = 0 // hand the same run out again on the next poll
	spy.mu.Unlock()
	if status != string(coretask.RunStatusPending) {
		t.Fatalf("status = %q, want PENDING", status)
	}
	if ended != nil {
		t.Fatalf("the run was ended (%s) over an unavailable store", ended.status)
	}

	elig.set(nil)
	s.pollOnce()
	s.inflight.Wait()
	if runnerCalls(runner) != 1 {
		t.Errorf("the run was not dispatched once eligibility could be confirmed: %d", runnerCalls(runner))
	}
}
