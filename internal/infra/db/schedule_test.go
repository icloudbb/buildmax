package db

import (
	"context"
	"testing"
	"time"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	coreschedule "github.com/icloudbb/buildmax/internal/core/schedule"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

// The db Store must satisfy the schedule domain's persistence contract.
var _ coreschedule.Store = (*Store)(nil)

// newTestAgent creates a space agent and registers its removal.
func newTestAgent(t *testing.T, s *Store, spaceID, userID, name string) string {
	t.Helper()
	ctx := context.Background()
	agent, err := s.CreateAgentInSpace(ctx, agentdef.CreateInput{
		SpaceID: spaceID, UserID: userID,
		Def: agentdef.Definition{Name: name, Description: "d", Instructions: "i"},
	})
	if err != nil {
		t.Fatalf("CreateAgentInSpace: %v", err)
	}
	t.Cleanup(func() {
		_ = s.db.Delete(&agentRow{}, "public_id = ?", canonicalPublicID(agent.ID)).Error
	})
	return agent.ID
}

// scheduleFixture is a space, its owner, and an agent -- everything a schedule
// needs to reference before it can exist.
type scheduleFixture struct {
	userID  string
	spaceID string
	agentID string
}

func newScheduleFixture(t *testing.T, s *Store, label string) scheduleFixture {
	t.Helper()
	userID := newTestUser(t, s, label)
	spaceID := newTestSpace(t, s, userID)
	agentID := newTestAgent(t, s, spaceID, userID, label+"-agent")
	return scheduleFixture{userID: userID, spaceID: spaceID, agentID: agentID}
}

// newTestSchedule creates a schedule with the given next fire time and registers
// its removal.
func newTestSchedule(t *testing.T, s *Store, f scheduleFixture, nextFireAt time.Time, enabled bool) *coreschedule.Schedule {
	t.Helper()
	ctx := context.Background()
	sched, err := s.CreateSchedule(ctx, &coreschedule.CreateInput{
		SpaceID:      f.spaceID,
		ExecutorKind: coreschedule.ExecutorAgent,
		ExecutorID:   f.agentID,
		CreatedBy:    f.userID,
		Name:         "nightly",
		Input:        "summarize new issues",
		CronExpr:     "0 9 * * *",
		Timezone:     "Asia/Shanghai",
		Enabled:      enabled,
		NextFireAt:   nextFireAt,
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	t.Cleanup(func() {
		_ = s.db.Delete(&scheduleRow{}, "public_id = ?", canonicalPublicID(sched.ID)).Error
	})
	return sched
}

func TestScheduleCRUD(t *testing.T) {
	s, ctx := newTestStore(t)
	f := newScheduleFixture(t, s, "sched-crud")
	next := time.Unix(1_800_000_000, 0).UTC()

	created := newTestSchedule(t, s, f, next, true)
	if created.SpaceID != f.spaceID || created.ExecutorKind != coreschedule.ExecutorAgent || created.ExecutorID != f.agentID || created.CreatedBy != f.userID {
		t.Fatalf("created schedule references = %+v, want the fixture's handles", created)
	}
	if !created.Enabled || created.Input != "summarize new issues" || !created.NextFireAt.Equal(next) {
		t.Fatalf("created schedule = %+v, want enabled with the given input and next fire", created)
	}
	if created.ConsecutiveFailures != 0 || created.LastFireAt != nil || created.LastFireRef != nil {
		t.Errorf("a fresh schedule has fire history %+v, want none", created)
	}

	got, err := s.GetSchedule(ctx, created.ID)
	if err != nil || got == nil {
		t.Fatalf("GetSchedule: %v (got %v)", err, got)
	}
	if got.ID != created.ID {
		t.Errorf("GetSchedule id = %q, want %q", got.ID, created.ID)
	}

	list, total, err := s.ListSchedulesBySpace(ctx, f.spaceID, 10, 0)
	if err != nil {
		t.Fatalf("ListSchedulesBySpace: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].ID != created.ID {
		t.Errorf("list = %+v (total %d), want exactly the created schedule", list, total)
	}

	newInput := "summarize open PRs"
	disabled := false
	updated, err := s.UpdateSchedule(ctx, coreschedule.UpdateInput{
		ScheduleID: created.ID, Input: &newInput, Enabled: &disabled,
	})
	if err != nil {
		t.Fatalf("UpdateSchedule: %v", err)
	}
	if updated.Input != newInput || updated.Enabled {
		t.Errorf("updated schedule = %+v, want the new input and disabled", updated)
	}

	if err := s.DeleteSchedule(ctx, created.ID); err != nil {
		t.Fatalf("DeleteSchedule: %v", err)
	}
	gone, err := s.GetSchedule(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetSchedule after delete: %v", err)
	}
	if gone != nil {
		t.Errorf("schedule still present after delete: %+v", gone)
	}
}

// DueSchedules must return an enabled schedule whose time has arrived and skip a
// disabled one and a future one. It is global rather than space-scoped, so the
// assertions look for the fixture's own ids rather than an exact set.
func TestDueSchedulesReturnsOnlyEnabledAndDue(t *testing.T) {
	s, ctx := newTestStore(t)
	f := newScheduleFixture(t, s, "sched-due")
	now := time.Unix(1_800_000_000, 0).UTC()

	dueEnabled := newTestSchedule(t, s, f, now.Add(-time.Minute), true)
	dueDisabled := newTestSchedule(t, s, f, now.Add(-time.Minute), false)
	future := newTestSchedule(t, s, f, now.Add(time.Hour), true)

	due, err := s.DueSchedules(ctx, now, 100)
	if err != nil {
		t.Fatalf("DueSchedules: %v", err)
	}
	present := map[string]bool{}
	for _, sc := range due {
		present[sc.ID] = true
	}
	if !present[dueEnabled.ID] {
		t.Errorf("an enabled, arrived schedule was not returned")
	}
	if present[dueDisabled.ID] {
		t.Errorf("a disabled schedule was returned as due")
	}
	if present[future.ID] {
		t.Errorf("a future schedule was returned as due")
	}
}

// A deactivation pauses a creator's enabled schedules at once, so it needs every
// enabled schedule that account created regardless of due time, and none that are
// already paused or belong to someone else.
func TestListEnabledSchedulesByCreator(t *testing.T) {
	s, ctx := newTestStore(t)
	f := newScheduleFixture(t, s, "sched-creator")
	now := time.Unix(1_800_000_000, 0).UTC()

	enabledFuture := newTestSchedule(t, s, f, now.Add(time.Hour), true)
	paused := newTestSchedule(t, s, f, now.Add(time.Hour), false)

	// A second account's enabled schedule in the same Space must not appear.
	otherUser := newTestUser(t, s, "sched-creator-other")
	if _, err := s.AddSpaceMember(ctx, f.spaceID, otherUser, corespace.RoleMember); err != nil {
		t.Fatalf("AddSpaceMember: %v", err)
	}
	otherSched, err := s.CreateSchedule(ctx, &coreschedule.CreateInput{
		SpaceID: f.spaceID, ExecutorKind: coreschedule.ExecutorAgent, ExecutorID: f.agentID, CreatedBy: otherUser,
		Name: "theirs", Input: "x", CronExpr: "0 9 * * *", Timezone: "UTC",
		Enabled: true, NextFireAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateSchedule other: %v", err)
	}
	t.Cleanup(func() { _ = s.db.Delete(&scheduleRow{}, "public_id = ?", canonicalPublicID(otherSched.ID)).Error })

	got, err := s.ListEnabledSchedulesByCreator(ctx, f.userID)
	if err != nil {
		t.Fatalf("ListEnabledSchedulesByCreator: %v", err)
	}
	present := map[string]bool{}
	for _, sc := range got {
		present[sc.ID] = true
	}
	if !present[enabledFuture.ID] {
		t.Error("an enabled schedule the account created was not returned")
	}
	if present[paused.ID] {
		t.Error("a paused schedule was returned")
	}
	if present[otherSched.ID] {
		t.Error("another account's schedule was returned")
	}
}

// Two replicas claiming one due time is the case exactly-once firing rests on: a
// second winner means the same schedule fires twice, doubling its Tasks and
// their token spend.
func TestClaimScheduleHasOneWinnerUnderContention(t *testing.T) {
	s, ctx := newTestStore(t)
	f := newScheduleFixture(t, s, "sched-claim")
	due := time.Unix(1_800_000_000, 0).UTC()
	next := due.Add(24 * time.Hour)
	sched := newTestSchedule(t, s, f, due, true)

	winners := race(t, func(int) (bool, error) {
		return s.ClaimSchedule(ctx, coreschedule.ClaimInput{
			ScheduleID:         sched.ID,
			ExpectedNextFireAt: due,
			NewNextFireAt:      next,
		})
	})
	if winners != 1 {
		t.Errorf("%d of %d concurrent claims succeeded, want exactly 1", winners, raceCount)
	}

	stored, err := s.GetSchedule(ctx, sched.ID)
	if err != nil {
		t.Fatalf("GetSchedule: %v", err)
	}
	if !stored.NextFireAt.Equal(next) {
		t.Errorf("next_fire_at = %v, want it advanced to %v", stored.NextFireAt, next)
	}
}

// A disabled schedule cannot be claimed: the dispatcher must not fire a schedule
// paused between the due query and the claim.
func TestClaimScheduleRefusesDisabled(t *testing.T) {
	s, ctx := newTestStore(t)
	f := newScheduleFixture(t, s, "sched-claim-disabled")
	due := time.Unix(1_800_000_000, 0).UTC()
	sched := newTestSchedule(t, s, f, due, false)

	claimed, err := s.ClaimSchedule(ctx, coreschedule.ClaimInput{
		ScheduleID: sched.ID, ExpectedNextFireAt: due, NewNextFireAt: due.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("ClaimSchedule: %v", err)
	}
	if claimed {
		t.Error("a disabled schedule was claimed")
	}
}

// RecordFire counts consecutive admission failures and resets the count on a
// success, and a successful fire records the Task it created.
func TestRecordFireCountsFailuresAndRecordsTask(t *testing.T) {
	s, ctx := newTestStore(t)
	f := newScheduleFixture(t, s, "sched-fire")
	sched := newTestSchedule(t, s, f, time.Unix(1_800_000_000, 0).UTC(), true)

	fireAt := time.Unix(1_800_000_060, 0).UTC()
	for want := 1; want <= 2; want++ {
		if err := s.RecordFire(ctx, coreschedule.RecordFireInput{
			ScheduleID: sched.ID, FiredAt: fireAt, Failed: true,
		}); err != nil {
			t.Fatalf("RecordFire failure %d: %v", want, err)
		}
		got, err := s.GetSchedule(ctx, sched.ID)
		if err != nil {
			t.Fatalf("GetSchedule: %v", err)
		}
		if got.ConsecutiveFailures != want {
			t.Errorf("consecutive_failures = %d after %d failures, want %d", got.ConsecutiveFailures, want, want)
		}
		if got.LastFireAt == nil || !got.LastFireAt.Equal(fireAt) {
			t.Errorf("last_fire_at = %v, want %v", got.LastFireAt, fireAt)
		}
	}

	// A successful fire records what it produced and resets the failure count.
	task := newScheduledTaskForTest(t, s, ctx, f, sched.ID)
	if err := s.RecordFire(ctx, coreschedule.RecordFireInput{
		ScheduleID: sched.ID, FiredAt: fireAt, FireRef: &task.ID, Failed: false,
	}); err != nil {
		t.Fatalf("RecordFire success: %v", err)
	}
	got, err := s.GetSchedule(ctx, sched.ID)
	if err != nil {
		t.Fatalf("GetSchedule: %v", err)
	}
	if got.ConsecutiveFailures != 0 {
		t.Errorf("consecutive_failures = %d after a success, want 0", got.ConsecutiveFailures)
	}
	if got.LastFireRef == nil || *got.LastFireRef != task.ID {
		t.Errorf("last_fire_ref = %v, want %q", got.LastFireRef, task.ID)
	}
}

// A Task created by a schedule carries the schedule as an origin relation, and
// that relation round-trips through a read.
func TestCreateTaskCarriesScheduleOrigin(t *testing.T) {
	s, ctx := newTestStore(t)
	f := newScheduleFixture(t, s, "sched-task-origin")
	sched := newTestSchedule(t, s, f, time.Unix(1_800_000_000, 0).UTC(), true)

	task := newScheduledTaskForTest(t, s, ctx, f, sched.ID)
	if task.ScheduleID == nil || *task.ScheduleID != sched.ID {
		t.Fatalf("created task schedule_id = %v, want %q", task.ScheduleID, sched.ID)
	}

	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.ScheduleID == nil || *got.ScheduleID != sched.ID {
		t.Errorf("read task schedule_id = %v, want %q", got.ScheduleID, sched.ID)
	}
}

// ListTasksBySchedule returns only the tasks a schedule created, scoped to its
// space.
func TestListTasksBySchedule(t *testing.T) {
	s, ctx := newTestStore(t)
	f := newScheduleFixture(t, s, "sched-tasklist")
	sched := newTestSchedule(t, s, f, time.Unix(1_800_000_000, 0).UTC(), true)
	// A task from this schedule, and an unrelated direct task in the same space.
	fired := newScheduledTaskForTest(t, s, ctx, f, sched.ID)
	other, err := s.CreateTask(ctx, &coretask.CreateInput{SpaceID: f.spaceID, AgentID: &f.agentID, Input: "direct", CreatedBy: f.userID})
	if err != nil {
		t.Fatalf("CreateTask (unrelated): %v", err)
	}
	t.Cleanup(func() {
		if other.LastRunID != nil {
			_ = s.db.Delete(&taskRunRow{}, "public_id = ?", canonicalPublicID(*other.LastRunID)).Error
		}
		_ = s.db.Delete(&taskRow{}, "public_id = ?", canonicalPublicID(other.ID)).Error
	})

	list, total, err := s.ListTasksBySchedule(ctx, f.spaceID, sched.ID, 10, 0)
	if err != nil {
		t.Fatalf("ListTasksBySchedule: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].ID != fired.ID {
		t.Errorf("ListTasksBySchedule = %+v (total %d), want only the fired task %q", list, total, fired.ID)
	}
}

// newScheduledTaskForTest creates a Task attributed to a schedule and registers
// the removal of the task and its first run.
func newScheduledTaskForTest(t *testing.T, s *Store, ctx context.Context, f scheduleFixture, scheduleID string) *coretask.Task {
	t.Helper()
	task, err := s.CreateTask(ctx, &coretask.CreateInput{
		SpaceID:                 f.spaceID,
		AgentID:                 &f.agentID,
		Input:                   "summarize new issues",
		CreatedBy:               f.userID,
		ScheduleID:              &scheduleID,
		InitialRunTriggerSource: coretask.RunTriggerSourceSchedule,
		InitialRunCreatedByType: coretask.RunCreatedByTypeSystem,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() {
		if task.LastRunID != nil {
			_ = s.db.Delete(&taskRunRow{}, "public_id = ?", canonicalPublicID(*task.LastRunID)).Error
		}
		_ = s.db.Delete(&taskRow{}, "public_id = ?", canonicalPublicID(task.ID)).Error
	})
	return task
}
