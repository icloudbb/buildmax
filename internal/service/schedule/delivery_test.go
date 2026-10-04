package schedule

import (
	"context"
	"errors"
	"testing"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreschedule "github.com/icloudbb/buildmax/internal/core/schedule"
	"github.com/icloudbb/buildmax/internal/mock"
)

type fakeTargets struct {
	checked []coreschedule.Delivery
	err     error
}

func (f *fakeTargets) CheckDeliveryTarget(_ context.Context, _, _, _ string, d coreschedule.Delivery) error {
	f.checked = append(f.checked, d)
	return f.err
}

// A delivery target is checked by the Assistant service on create and on
// change, stored with the schedule, and removed by an empty assistant id; one
// it refuses is not stored.
func TestDeliveryTargetIsCheckedAndStored(t *testing.T) {
	ctx := context.Background()
	targets := &fakeTargets{}
	store := &mock.MockScheduleStore{}
	svc := &Service{
		Schedules:  store,
		Agents:     &mock.MockAgentStore{Agents: []agentdef.Agent{{ID: "a1", SpaceID: "sp1"}}},
		Deliveries: targets,
	}
	target := &coreschedule.Delivery{AssistantID: "as1", RequesterID: "u2"}
	base := CreateCmd{SpaceID: "sp1", UserID: "u1", ExecutorKind: coreschedule.ExecutorAgent, ExecutorID: "a1",
		Input: "tick", CronExpr: "0 9 * * *", Timezone: "UTC"}

	cmd := base
	cmd.Delivery = target
	s, err := svc.Create(ctx, cmd)
	if err != nil || s.Delivery == nil || *s.Delivery != *target || len(targets.checked) != 1 {
		t.Fatalf("create = %+v, %v; checked %v", s, err, targets.checked)
	}

	incomplete := base
	incomplete.Delivery = &coreschedule.Delivery{AssistantID: "as1"}
	if _, err := svc.Create(ctx, incomplete); !errors.Is(err, ErrDeliveryIncomplete) {
		t.Errorf("incomplete target: err = %v", err)
	}

	targets.err = apierr.New(apierr.KindInvalid, "not on roster")
	other := &coreschedule.Delivery{AssistantID: "as2", RequesterID: "u3"}
	if _, err := svc.Update(ctx, UpdateCmd{SpaceID: "sp1", ScheduleID: s.ID, Delivery: other}); err == nil {
		t.Error("a refused target was accepted")
	}
	if got, _ := svc.Get(ctx, "sp1", s.ID); *got.Delivery != *target {
		t.Errorf("a refused target replaced the stored one: %+v", got.Delivery)
	}

	cleared, err := svc.Update(ctx, UpdateCmd{SpaceID: "sp1", ScheduleID: s.ID, Delivery: &coreschedule.Delivery{}})
	if err != nil || cleared.Delivery != nil {
		t.Errorf("clear = %+v, %v; want no delivery", cleared, err)
	}

	noChecker := &Service{Schedules: store, Agents: svc.Agents}
	if _, err := noChecker.Create(ctx, cmd); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("without a checker: err = %v, want not configured", err)
	}
}
