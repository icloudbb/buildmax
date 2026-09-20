package desktop

import (
	"errors"
	"testing"
	"time"

	schedstore "github.com/icloudbb/buildmax/internal/infra/localschedulestore"
)

func TestApplyFireSuccessAdvancesAndResets(t *testing.T) {
	now := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	rec := &schedstore.Record{
		CronExpr:            "0 9 * * *", // daily 09:00
		Timezone:            "UTC",
		Enabled:             true,
		ConsecutiveFailures: 3,
	}
	applyFire(rec, now, nil)
	if !rec.LastFireAt.Equal(now) {
		t.Fatalf("LastFireAt = %v, want %v", rec.LastFireAt, now)
	}
	if rec.ConsecutiveFailures != 0 {
		t.Fatalf("ConsecutiveFailures = %d, want 0 after success", rec.ConsecutiveFailures)
	}
	want := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	if !rec.NextFireAt.Equal(want) {
		t.Fatalf("NextFireAt = %v, want %v", rec.NextFireAt, want)
	}
	if !rec.Enabled {
		t.Fatal("task should stay enabled after a good fire")
	}
}

func TestApplyFirePausesAfterMaxFailures(t *testing.T) {
	now := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	rec := &schedstore.Record{
		CronExpr:            "0 9 * * *",
		Timezone:            "UTC",
		Enabled:             true,
		ConsecutiveFailures: maxScheduleFailures - 1,
	}
	applyFire(rec, now, errors.New("no model"))
	if rec.ConsecutiveFailures != maxScheduleFailures {
		t.Fatalf("ConsecutiveFailures = %d, want %d", rec.ConsecutiveFailures, maxScheduleFailures)
	}
	if rec.Enabled {
		t.Fatal("task should pause once it reaches the failure ceiling")
	}
	// Even a paused task keeps a sensible next fire for the UI to show.
	if rec.NextFireAt.IsZero() {
		t.Fatal("NextFireAt should still advance on a failed fire")
	}
}

func TestApplyFireCountsUpBeforeCeiling(t *testing.T) {
	now := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	rec := &schedstore.Record{CronExpr: "0 9 * * *", Timezone: "UTC", Enabled: true}
	applyFire(rec, now, errors.New("boom"))
	if rec.ConsecutiveFailures != 1 || !rec.Enabled {
		t.Fatalf("after one failure: failures=%d enabled=%v", rec.ConsecutiveFailures, rec.Enabled)
	}
}

func TestRecomputeNextPausesOnBadCron(t *testing.T) {
	rec := &schedstore.Record{CronExpr: "not a cron", Timezone: "UTC", Enabled: true}
	recomputeNext(rec, time.Now())
	if rec.Enabled {
		t.Fatal("an unparseable stored cron should pause the task")
	}
}

func TestCreateScheduledTaskValidates(t *testing.T) {
	a := &App{}
	cases := []struct {
		name                              string
		projectID, prompt, cron, timezone string
	}{
		{"no project", "", "do it", "0 9 * * *", "UTC"},
		{"no prompt", "p", "", "0 9 * * *", "UTC"},
		{"no cron", "p", "do it", "", "UTC"},
		{"bad cron", "p", "do it", "not a cron", "UTC"},
		{"bad timezone", "p", "do it", "0 9 * * *", "Mars/Phobos"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := a.CreateScheduledTask(tc.projectID, "", tc.prompt, tc.cron, tc.timezone); err == nil {
				t.Fatal("want validation error, got nil")
			}
		})
	}
}

func TestScheduledTaskPayloadFormatsTimes(t *testing.T) {
	now := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	p := scheduledTaskPayload(schedstore.Record{
		ID:         "t1",
		ProjectID:  "p1",
		CronExpr:   "0 9 * * *",
		Timezone:   "UTC",
		Enabled:    true,
		NextFireAt: now,
		CreatedAt:  now,
		UpdatedAt:  now,
		// LastFireAt left zero.
	})
	if p.NextFireAt != "2026-01-01T09:00:00Z" {
		t.Fatalf("NextFireAt = %q", p.NextFireAt)
	}
	if p.LastFireAt != "" {
		t.Fatalf("LastFireAt = %q, want empty for an unfired task", p.LastFireAt)
	}
}
