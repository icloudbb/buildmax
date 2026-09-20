package desktop

import (
	"errors"
	"os"
	"path/filepath"
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
	// Every case must fail before CreateScheduledTask reaches the store, so the
	// test needs no BUILDMAX_HOME: validation runs before resolveWorkingDir, which
	// runs before the store write.
	cases := []struct {
		name                               string
		workingDir, prompt, cron, timezone string
	}{
		{"no prompt", "", "", "0 9 * * *", "UTC"},
		{"no cron", "", "do it", "", "UTC"},
		{"bad cron", "", "do it", "not a cron", "UTC"},
		{"bad timezone", "", "do it", "0 9 * * *", "Mars/Phobos"},
		{"missing dir", "/no/such/dir/buildmax-schedule-test", "do it", "0 9 * * *", "UTC"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := a.CreateScheduledTask(tc.workingDir, "", tc.prompt, tc.cron, tc.timezone, ""); err == nil {
				t.Fatal("want validation error, got nil")
			}
		})
	}
}

func TestPreviewScheduledTaskListsUpcomingFires(t *testing.T) {
	a := &App{}
	out, err := a.PreviewScheduledTask("0 9 * * *", "UTC")
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(out) != schedulePreviewCount {
		t.Fatalf("want %d previews, got %d", schedulePreviewCount, len(out))
	}
	// RFC3339 UTC strings sort in time order, so each must exceed the prior.
	for i := 1; i < len(out); i++ {
		if out[i] <= out[i-1] {
			t.Fatalf("previews not strictly increasing: %v", out)
		}
	}
	if _, err := a.PreviewScheduledTask("not a cron", "UTC"); err == nil {
		t.Fatal("want error for an invalid cron expression")
	}
}

func TestResolveWorkingDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	if got, err := resolveWorkingDir("  "); err != nil || got != home {
		t.Fatalf("blank dir = (%q, %v), want home %q", got, err, home)
	}
	dir := t.TempDir()
	if got, err := resolveWorkingDir(dir); err != nil || got != dir {
		t.Fatalf("existing dir = (%q, %v), want %q", got, err, dir)
	}
	if _, err := resolveWorkingDir(filepath.Join(dir, "nope")); err == nil {
		t.Fatal("want error for a missing directory")
	}
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveWorkingDir(file); err == nil {
		t.Fatal("want error for a path that is a file, not a directory")
	}
}

func TestScheduledTaskPayloadFormatsTimes(t *testing.T) {
	now := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	p := scheduledTaskPayload(schedstore.Record{
		ID:         "t1",
		WorkingDir: "/tmp/p1",
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
