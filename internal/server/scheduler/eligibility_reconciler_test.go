package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/core/eligibility"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/mock"
)

// TestEligibilityReconcilerCancelsRunsWhoseInitiatorLostAuthority is the durable
// backstop's whole job: a run already under way whose initiator was disabled or
// removed from the Space is asked to stop, with the reason recorded, while an
// eligible initiator's run is left running.
func TestEligibilityReconcilerCancelsRunsWhoseInitiatorLostAuthority(t *testing.T) {
	const space = "sp_1"
	disabledAt := time.Unix(1, 0).UTC()

	runs := &mock.MockTaskRunStore{
		TaskList: []coretask.Task{{ID: "t_ok", SpaceID: space}, {ID: "t_disabled", SpaceID: space}, {ID: "t_removed", SpaceID: space}},
		Runs: []coretask.Run{
			{ID: "r_ok", TaskID: "t_ok", Status: string(coretask.RunStatusRunning), CreatedBy: "u_ok"},
			{ID: "r_disabled", TaskID: "t_disabled", Status: string(coretask.RunStatusRunning), CreatedBy: "u_disabled"},
			{ID: "r_removed", TaskID: "t_removed", Status: string(coretask.RunStatusScheduled), CreatedBy: "u_removed"},
		},
	}
	users := &mock.MockUserStore{ByID: map[string]*coreidentity.User{
		"u_ok":       {ID: "u_ok"},
		"u_disabled": {ID: "u_disabled", DisabledAt: &disabledAt},
		"u_removed":  {ID: "u_removed"},
	}}
	spaces := &mock.MockSpaceStore{Members: []corespace.Member{
		{SpaceID: space, UserID: "u_ok", Role: corespace.RoleMember},
		{SpaceID: space, UserID: "u_disabled", Role: corespace.RoleMember},
		// u_removed has no membership row.
	}}

	c := NewEligibilityReconciler(runs, eligibility.New(users, spaces), time.Minute)
	c.Sweep(context.Background(), time.Now().UTC())

	byID := map[string]coretask.Run{}
	for _, r := range runs.Runs {
		byID[r.ID] = r
	}
	if byID["r_ok"].CancelRequestedAt != nil {
		t.Error("an eligible initiator's run was asked to stop")
	}
	if got := byID["r_disabled"]; got.CancelRequestedAt == nil || got.CancelReason != coretask.CancelReasonCreatorDisabled {
		t.Errorf("disabled initiator's run: cancel_requested=%v reason=%q, want requested with creator_disabled", got.CancelRequestedAt != nil, got.CancelReason)
	}
	if got := byID["r_removed"]; got.CancelRequestedAt == nil || got.CancelReason != coretask.CancelReasonCreatorNotMember {
		t.Errorf("removed initiator's run: cancel_requested=%v reason=%q, want requested with creator_not_member", got.CancelRequestedAt != nil, got.CancelReason)
	}
}

// TestEligibilityReconcilerNilWithoutDependencies keeps a deployment that wires
// no authority stores from starting a reconciler that would cancel everything.
func TestEligibilityReconcilerNilWithoutDependencies(t *testing.T) {
	if NewEligibilityReconciler(nil, nil, 0) != nil {
		t.Error("a reconciler with no store or checker should be nil")
	}
	if NewEligibilityReconciler(&mock.MockTaskRunStore{}, nil, 0) != nil {
		t.Error("a reconciler with no checker should be nil")
	}
}
