package localschedulehistorystore

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *FileStore {
	t.Helper()
	return NewFileStore(filepath.Join(t.TempDir(), "schedule-runs.json"))
}

func TestAddListNewestFirst(t *testing.T) {
	s := newTestStore(t)
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	must(t, s.Add(Run{ID: "r1", ScheduleID: "s1", SessionID: "sess1", FiredAt: base, Status: StatusRunning}))
	must(t, s.Add(Run{ID: "r2", ScheduleID: "s1", SessionID: "sess2", FiredAt: base.Add(time.Hour), Status: StatusOK}))
	rows, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 runs, got %d", len(rows))
	}
	if rows[0].ID != "r2" {
		t.Fatalf("want newest run first, got %q", rows[0].ID)
	}
}

func TestUpdateStampsOutcome(t *testing.T) {
	s := newTestStore(t)
	must(t, s.Add(Run{ID: "r1", ScheduleID: "s1", SessionID: "sess1", FiredAt: time.Now().UTC(), Status: StatusRunning}))
	if _, err := s.Update("r1", func(r *Run) { r.Status = StatusFailed; r.Error = "boom" }); err != nil {
		t.Fatalf("Update: %v", err)
	}
	rows, _ := s.List()
	if rows[0].Status != StatusFailed || rows[0].Error != "boom" {
		t.Fatalf("outcome not stamped: %+v", rows[0])
	}
}

func TestDeleteByScheduleReturnsSessions(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC()
	must(t, s.Add(Run{ID: "r1", ScheduleID: "s1", SessionID: "sess1", FiredAt: now, Status: StatusOK}))
	must(t, s.Add(Run{ID: "r2", ScheduleID: "s1", SessionID: "", FiredAt: now, Status: StatusFailed}))
	must(t, s.Add(Run{ID: "r3", ScheduleID: "s2", SessionID: "sess3", FiredAt: now, Status: StatusOK}))
	sessions, err := s.DeleteBySchedule("s1")
	if err != nil {
		t.Fatalf("DeleteBySchedule: %v", err)
	}
	if len(sessions) != 1 || sessions[0] != "sess1" {
		t.Fatalf("want [sess1] (empty session skipped), got %v", sessions)
	}
	rows, _ := s.List()
	if len(rows) != 1 || rows[0].ScheduleID != "s2" {
		t.Fatalf("only the other task's runs should remain: %+v", rows)
	}
}

func TestAddPrunesToCap(t *testing.T) {
	s := newTestStore(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	total := maxRunsPerSchedule + 10
	for i := range total {
		must(t, s.Add(Run{
			ID:         "r" + strconv.Itoa(i),
			ScheduleID: "s1",
			FiredAt:    base.Add(time.Duration(i) * time.Minute),
			Status:     StatusOK,
		}))
	}
	rows, _ := s.List()
	if len(rows) != maxRunsPerSchedule {
		t.Fatalf("want cap %d rows, got %d", maxRunsPerSchedule, len(rows))
	}
	// The newest run must survive; the oldest must have been pruned.
	if rows[0].ID != "r"+strconv.Itoa(total-1) {
		t.Fatalf("newest run pruned: front is %q", rows[0].ID)
	}
	for _, r := range rows {
		if r.ID == "r0" {
			t.Fatal("oldest run should have been pruned")
		}
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
