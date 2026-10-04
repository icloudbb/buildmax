package db

import (
	"testing"
	"time"

	coreschedule "github.com/icloudbb/buildmax/internal/core/schedule"
)

// A firing that started its executor on a delivering schedule records one
// pending delivery with the fire; a failed firing or a schedule without a
// target records none. Settling is a claim: it succeeds once, from the status
// the caller names. Deleting the schedule takes its deliveries.
func TestScheduleDeliveryLifecycle(t *testing.T) {
	s, ctx := newTestStore(t)
	f := newScheduleFixture(t, s, "sched-deliver")
	next := time.Unix(1_800_000_000, 0).UTC()
	plain := newTestSchedule(t, s, f, next, true)
	delivering := newTestSchedule(t, s, f, next, true)
	a, err := s.CreateAssistant(ctx, f.spaceID, f.userID, f.userID, testAssistantDef("Deliverer"))
	if err != nil {
		t.Fatal(err)
	}
	target := coreschedule.Delivery{AssistantID: a.ID, RequesterID: newTestUser(t, s, "sched-requester")}
	if _, err := s.UpdateSchedule(ctx, coreschedule.UpdateInput{ScheduleID: delivering.ID, Delivery: &coreschedule.Delivery{AssistantID: a.ID, RequesterID: "missing"}}); err == nil {
		t.Error("a target naming no user was stored")
	}
	updated, err := s.UpdateSchedule(ctx, coreschedule.UpdateInput{ScheduleID: delivering.ID, Delivery: &target})
	if err != nil || updated.Delivery == nil || *updated.Delivery != target {
		t.Fatalf("set delivery = %+v, %v", updated, err)
	}

	ref := "task_one"
	fired := time.Unix(1_800_000_100, 0).UTC()
	for _, in := range []coreschedule.RecordFireInput{
		{ScheduleID: plain.ID, FiredAt: fired, FireRef: &ref},
		{ScheduleID: delivering.ID, FiredAt: fired, Failed: true},
		{ScheduleID: delivering.ID, FiredAt: fired, FireRef: &ref},
	} {
		if err := s.RecordFire(ctx, in); err != nil {
			t.Fatalf("RecordFire %+v: %v", in, err)
		}
	}
	list, total, err := s.ListDeliveriesBySchedule(ctx, delivering.ID, 10, 0)
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("deliveries = %+v (%d), %v; want one", list, total, err)
	}
	d := list[0]
	if d.ScheduleID != delivering.ID || d.FireRef != ref || d.Status != coreschedule.DeliveryPending || d.SettledAt != nil {
		t.Errorf("delivery = %+v", d)
	}
	if _, n, _ := s.ListDeliveriesBySchedule(ctx, plain.ID, 10, 0); n != 0 {
		t.Errorf("a schedule without a target recorded %d deliveries", n)
	}
	pending, err := s.ListPendingDeliveries(ctx, 1000)
	if err != nil || !containsDelivery(pending, d.ID) {
		t.Errorf("pending = %+v, %v; want it listed", pending, err)
	}

	settled := time.Unix(1_800_000_200, 0).UTC()
	won, err := s.SettleDelivery(ctx, coreschedule.SettleDeliveryInput{DeliveryID: d.ID, Status: coreschedule.DeliveryDelivered, SettledAt: settled})
	if err != nil || !won {
		t.Fatalf("first settle = %v, %v", won, err)
	}
	if again, _ := s.SettleDelivery(ctx, coreschedule.SettleDeliveryInput{DeliveryID: d.ID, Status: coreschedule.DeliverySkipped, Reason: coreschedule.SkipNoBot, SettledAt: settled}); again {
		t.Error("a settled delivery settled again")
	}
	if failed, _ := s.SettleDelivery(ctx, coreschedule.SettleDeliveryInput{DeliveryID: d.ID, From: coreschedule.DeliveryDelivered, Status: coreschedule.DeliveryFailed, SettledAt: settled}); !failed {
		t.Error("a delivered delivery could not be marked failed")
	}
	list, _, _ = s.ListDeliveriesBySchedule(ctx, delivering.ID, 10, 0)
	if got := list[0]; got.Status != coreschedule.DeliveryFailed || got.SettledAt == nil || !got.SettledAt.Equal(settled) {
		t.Errorf("settled delivery = %+v", got)
	}
	if pending, _ := s.ListPendingDeliveries(ctx, 1000); containsDelivery(pending, d.ID) {
		t.Error("a settled delivery is still pending")
	}

	cleared, err := s.UpdateSchedule(ctx, coreschedule.UpdateInput{ScheduleID: delivering.ID, Delivery: &coreschedule.Delivery{}})
	if err != nil || cleared.Delivery != nil {
		t.Errorf("clear delivery = %+v, %v", cleared, err)
	}
	if err := s.DeleteSchedule(ctx, delivering.ID); err != nil {
		t.Fatal(err)
	}
	var left int64
	if err := s.db.Model(&scheduleDeliveryRow{}).Where("fire_ref = ? AND public_id = ?", ref, canonicalPublicID(d.ID)).Count(&left).Error; err != nil || left != 0 {
		t.Errorf("deleting the schedule left %d deliveries, %v", left, err)
	}
}

func containsDelivery(list []coreschedule.FireDelivery, id string) bool {
	for _, d := range list {
		if d.ID == id {
			return true
		}
	}
	return false
}
