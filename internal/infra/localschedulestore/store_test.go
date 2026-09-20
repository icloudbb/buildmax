package localschedulestore

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *FileStore {
	t.Helper()
	return NewFileStore(filepath.Join(t.TempDir(), "scheduled-tasks.json"))
}

func TestListEmptyWhenNoFile(t *testing.T) {
	s := newTestStore(t)
	rows, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("want empty, got %d", len(rows))
	}
}

func TestAddGetListRoundTrip(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	r := Record{
		ID:         "task1",
		Name:       "Daily",
		ProjectID:  "proj1",
		Prompt:     "summarize",
		CronExpr:   "0 9 * * *",
		Timezone:   "UTC",
		Enabled:    true,
		NextFireAt: now.Add(time.Hour),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.Add(r); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, err := s.Get("task1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Daily" || got.Prompt != "summarize" || !got.NextFireAt.Equal(r.NextFireAt) {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	rows, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 task, got %d", len(rows))
	}
}

func TestAddRejectsDuplicateID(t *testing.T) {
	s := newTestStore(t)
	r := Record{ID: "dup", CreatedAt: time.Now()}
	if err := s.Add(r); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Add(r); err == nil {
		t.Fatal("want duplicate error, got nil")
	}
}

func TestUpdateIsFieldScoped(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC()
	if err := s.Add(Record{ID: "t", ProjectID: "p", Enabled: true, CreatedAt: now}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// Two updates touching different fields must both survive.
	if _, err := s.Update("t", func(r *Record) { r.LastSessionID = "sess-1" }); err != nil {
		t.Fatalf("Update session: %v", err)
	}
	if _, err := s.Update("t", func(r *Record) { r.Enabled = false }); err != nil {
		t.Fatalf("Update enabled: %v", err)
	}
	got, err := s.Get("t")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.LastSessionID != "sess-1" || got.Enabled {
		t.Fatalf("field-scoped update lost data: %+v", got)
	}
}

func TestUpdateUnknownIDIsNotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Update("missing", func(*Record) {}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestDeleteRemovesAndIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	if err := s.Add(Record{ID: "t", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Delete("t"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get("t"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound after delete, got %v", err)
	}
	// Deleting again is a no-op, not an error.
	if err := s.Delete("t"); err != nil {
		t.Fatalf("Delete idempotent: %v", err)
	}
}
