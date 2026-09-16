package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/core/eligibility"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	coreschedule "github.com/icloudbb/buildmax/internal/core/schedule"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/mock"
	tasksvc "github.com/icloudbb/buildmax/internal/service/task"
)

// fakeScheduleStore is an in-memory coreschedule.Store. Only the methods the
// dispatcher uses carry behavior; the rest satisfy the interface.
type fakeScheduleStore struct {
	mu        sync.Mutex
	schedules map[string]*coreschedule.Schedule
}

func newFakeScheduleStore(seed ...*coreschedule.Schedule) *fakeScheduleStore {
	s := &fakeScheduleStore{schedules: map[string]*coreschedule.Schedule{}}
	for _, sc := range seed {
		cp := *sc
		s.schedules[sc.ID] = &cp
	}
	return s
}

func (f *fakeScheduleStore) get(id string) *coreschedule.Schedule {
	f.mu.Lock()
	defer f.mu.Unlock()
	sc := f.schedules[id]
	if sc == nil {
		return nil
	}
	cp := *sc
	return &cp
}

func (f *fakeScheduleStore) DueSchedules(_ context.Context, now time.Time, _ int) ([]coreschedule.Schedule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []coreschedule.Schedule
	for _, sc := range f.schedules {
		if sc.Enabled && !sc.NextFireAt.After(now) {
			out = append(out, *sc)
		}
	}
	return out, nil
}

func (f *fakeScheduleStore) ClaimSchedule(_ context.Context, in coreschedule.ClaimInput) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sc := f.schedules[in.ScheduleID]
	if sc == nil || !sc.Enabled || !sc.NextFireAt.Equal(in.ExpectedNextFireAt) {
		return false, nil
	}
	sc.NextFireAt = in.NewNextFireAt
	return true, nil
}

func (f *fakeScheduleStore) RecordFire(_ context.Context, in coreschedule.RecordFireInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sc := f.schedules[in.ScheduleID]
	if sc == nil {
		return fmt.Errorf("no such schedule")
	}
	sc.LastFireAt = &in.FiredAt
	if in.TaskID != nil {
		sc.LastTaskID = in.TaskID
	}
	if in.Failed {
		sc.ConsecutiveFailures++
	} else {
		sc.ConsecutiveFailures = 0
	}
	return nil
}

func (f *fakeScheduleStore) UpdateSchedule(_ context.Context, in coreschedule.UpdateInput) (*coreschedule.Schedule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sc := f.schedules[in.ScheduleID]
	if sc == nil {
		return nil, fmt.Errorf("no such schedule")
	}
	if in.Enabled != nil {
		sc.Enabled = *in.Enabled
		if *in.Enabled {
			sc.PauseReason = ""
		} else if in.PauseReason != nil {
			sc.PauseReason = *in.PauseReason
		}
	}
	cp := *sc
	return &cp, nil
}

func (f *fakeScheduleStore) CreateSchedule(context.Context, *coreschedule.CreateInput) (*coreschedule.Schedule, error) {
	return nil, nil
}
func (f *fakeScheduleStore) GetSchedule(_ context.Context, id string) (*coreschedule.Schedule, error) {
	return f.get(id), nil
}
func (f *fakeScheduleStore) ListSchedulesBySpace(context.Context, string, int, int) ([]coreschedule.Schedule, int, error) {
	return nil, 0, nil
}
func (f *fakeScheduleStore) DeleteSchedule(context.Context, string) error { return nil }

// fakeAdmitter records the CreateTask commands it receives and can be told to
// fail a number of times.
type fakeAdmitter struct {
	mu       sync.Mutex
	cmds     []tasksvc.CreateTaskCmd
	err      error
	failUpTo int // fail the first N calls when err is set
}

func (a *fakeAdmitter) CreateTask(_ context.Context, cmd tasksvc.CreateTaskCmd) (*coretask.Task, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cmds = append(a.cmds, cmd)
	if a.err != nil && (a.failUpTo == 0 || len(a.cmds) <= a.failUpTo) {
		return nil, a.err
	}
	return &coretask.Task{ID: fmt.Sprintf("task%d", len(a.cmds))}, nil
}

func (a *fakeAdmitter) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.cmds)
}

// fakeUserStore answers GetUser with one account; the rest satisfy the interface.
type fakeUserStore struct{ user *coreidentity.User }

func (f fakeUserStore) GetUser(context.Context, string) (*coreidentity.User, error) {
	return f.user, nil
}
func (fakeUserStore) UserByEmail(context.Context, string) (*coreidentity.User, error) {
	return nil, nil
}
func (fakeUserStore) CreateUser(context.Context, string, string) (*coreidentity.User, error) {
	return nil, nil
}
func (fakeUserStore) UpdateLoginMeta(context.Context, string, time.Time, string) error { return nil }
func (fakeUserStore) ListUsers(context.Context, coreidentity.UserFilter, int, int) ([]coreidentity.User, int, error) {
	return nil, 0, nil
}
func (fakeUserStore) SetUserDisabled(context.Context, string, *time.Time) error { return nil }

func hourlySchedule(nextFireAt time.Time) *coreschedule.Schedule {
	return &coreschedule.Schedule{
		ID:         "sched1",
		SpaceID:    "space1",
		AgentID:    "agent1",
		CreatedBy:  "user1",
		Input:      "summarize new issues",
		CronExpr:   "0 * * * *", // minute 0 of every hour
		Timezone:   "UTC",
		Enabled:    true,
		NextFireAt: nextFireAt,
	}
}

func newTestDispatcher(t *testing.T, store coreschedule.Store, admitter ScheduleAdmitter, now time.Time) *ScheduleDispatcher {
	t.Helper()
	d, err := NewScheduleDispatcher(store, admitter, time.Minute)
	if err != nil {
		t.Fatalf("NewScheduleDispatcher: %v", err)
	}
	d.now = func() time.Time { return now }
	return d
}

// A due schedule fires exactly once: the first sweep admits its Task and
// advances the next fire past now, so a second sweep at the same instant does
// nothing.
func TestDispatcherFiresDueScheduleOnce(t *testing.T) {
	t0 := time.Date(2000, 1, 1, 9, 0, 0, 0, time.UTC)
	store := newFakeScheduleStore(hourlySchedule(t0))
	admitter := &fakeAdmitter{}
	d := newTestDispatcher(t, store, admitter, t0)

	d.sweep(context.Background())

	if admitter.count() != 1 {
		t.Fatalf("admitter received %d tasks, want 1", admitter.count())
	}
	cmd := admitter.cmds[0]
	if cmd.SpaceID != "space1" || cmd.UserID != "user1" || cmd.Input != "summarize new issues" {
		t.Errorf("admitted cmd = %+v, want the schedule's space, creator, and input", cmd)
	}
	if cmd.AgentID == nil || *cmd.AgentID != "agent1" {
		t.Errorf("admitted agent = %v, want agent1", cmd.AgentID)
	}
	if cmd.ScheduleID == nil || *cmd.ScheduleID != "sched1" {
		t.Errorf("admitted schedule_id = %v, want sched1", cmd.ScheduleID)
	}
	if cmd.TriggerSource != coretask.RunTriggerSourceSchedule || cmd.CreatedByType != coretask.RunCreatedByTypeSystem {
		t.Errorf("admitted provenance = (%q,%q), want (schedule, system)", cmd.TriggerSource, cmd.CreatedByType)
	}

	stored := store.get("sched1")
	want := time.Date(2000, 1, 1, 10, 0, 0, 0, time.UTC)
	if !stored.NextFireAt.Equal(want) {
		t.Errorf("next_fire_at = %v, want it advanced to %v", stored.NextFireAt, want)
	}
	if stored.LastTaskID == nil {
		t.Error("a successful fire recorded no task")
	}

	d.sweep(context.Background())
	if admitter.count() != 1 {
		t.Errorf("a second sweep at the same time fired again: %d tasks, want 1", admitter.count())
	}
}

// A schedule missed while the server was down fires once and resumes: the next
// fire is computed from now, not backfilled for every slot in the gap.
func TestDispatcherCoalescesMissedFires(t *testing.T) {
	due := time.Date(2000, 1, 1, 9, 0, 0, 0, time.UTC)
	now := time.Date(2000, 1, 1, 11, 30, 0, 0, time.UTC) // two hourly slots missed
	store := newFakeScheduleStore(hourlySchedule(due))
	admitter := &fakeAdmitter{}
	d := newTestDispatcher(t, store, admitter, now)

	d.sweep(context.Background())

	if admitter.count() != 1 {
		t.Fatalf("missed fires were not coalesced: %d tasks, want exactly 1", admitter.count())
	}
	stored := store.get("sched1")
	want := time.Date(2000, 1, 1, 12, 0, 0, 0, time.UTC)
	if !stored.NextFireAt.Equal(want) {
		t.Errorf("next_fire_at = %v, want the next slot after now (%v)", stored.NextFireAt, want)
	}
}

// A schedule whose time has not arrived is not fired.
func TestDispatcherSkipsNotDue(t *testing.T) {
	now := time.Date(2000, 1, 1, 9, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	store := newFakeScheduleStore(hourlySchedule(future))
	admitter := &fakeAdmitter{}
	d := newTestDispatcher(t, store, admitter, now)

	d.sweep(context.Background())
	if admitter.count() != 0 {
		t.Errorf("a future schedule fired: %d tasks, want 0", admitter.count())
	}
}

// A schedule that fails to admit its Task enough times in a row is paused, so an
// unattended trigger cannot fail forever.
func TestDispatcherPausesAfterConsecutiveFailures(t *testing.T) {
	t0 := time.Date(2000, 1, 1, 9, 0, 0, 0, time.UTC)
	sched := hourlySchedule(t0)
	sched.ConsecutiveFailures = maxConsecutiveScheduleFailures - 1 // one more failure trips the bound
	store := newFakeScheduleStore(sched)
	admitter := &fakeAdmitter{err: errors.New("quota exceeded")}
	d := newTestDispatcher(t, store, admitter, t0)

	d.fireOne(context.Background(), *sched, t0)

	stored := store.get("sched1")
	if stored.Enabled {
		t.Errorf("schedule still enabled after reaching the failure bound")
	}
	if stored.ConsecutiveFailures != maxConsecutiveScheduleFailures {
		t.Errorf("consecutive_failures = %d, want %d", stored.ConsecutiveFailures, maxConsecutiveScheduleFailures)
	}
	if admitter.count() != 1 {
		t.Errorf("admitter calls = %d, want 1 failed attempt", admitter.count())
	}
}

// A schedule whose creator can no longer run work in its Space — disabled, or
// removed from the Space — pauses rather than admitting a Task that would only
// fail at dispatch.
func TestDispatcherPausesIneligibleCreator(t *testing.T) {
	t0 := time.Date(2000, 1, 1, 9, 0, 0, 0, time.UTC)
	disabledAt := t0.Add(-time.Hour)

	tests := []struct {
		name       string
		elig       eligibility.Checker
		wantReason string
	}{
		{
			name: "disabled creator",
			elig: eligibility.New(
				fakeUserStore{user: &coreidentity.User{ID: "user1", DisabledAt: &disabledAt}},
				&mock.MockSpaceStore{Members: []corespace.Member{
					{SpaceID: "space1", UserID: "user1", Role: corespace.RoleMember},
				}},
			),
			wantReason: coreschedule.PauseReasonCreatorDisabled,
		},
		{
			name: "creator removed from the space",
			elig: eligibility.New(
				fakeUserStore{user: &coreidentity.User{ID: "user1"}},
				&mock.MockSpaceStore{}, // enabled, but no membership row
			),
			wantReason: coreschedule.PauseReasonCreatorNotMember,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sched := hourlySchedule(t0)
			store := newFakeScheduleStore(sched)
			admitter := &fakeAdmitter{}
			d := newTestDispatcher(t, store, admitter, t0)
			d.WithEligibility(tc.elig)

			d.fireOne(context.Background(), *sched, t0)

			if admitter.count() != 0 {
				t.Errorf("a Task was admitted for an ineligible creator: %d, want 0", admitter.count())
			}
			stored := store.get("sched1")
			if stored.Enabled {
				t.Error("schedule still enabled after its creator was found ineligible")
			}
			if stored.PauseReason != tc.wantReason {
				t.Errorf("pause_reason = %q, want %q", stored.PauseReason, tc.wantReason)
			}
		})
	}
}
