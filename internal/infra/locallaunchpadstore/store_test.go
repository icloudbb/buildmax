package locallaunchpadstore

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestListMissingFileIsEmpty(t *testing.T) {
	s := NewFileStore(filepath.Join(t.TempDir(), "launchpad.json"))
	rows, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("want empty, got %d", len(rows))
	}
}

func TestAddGetDeleteRoundTrip(t *testing.T) {
	s := NewFileStore(filepath.Join(t.TempDir(), "launchpad.json"))
	base := time.Now().UTC()
	first := Record{ID: "a", Name: "Code", Target: "/Applications/Code.app", CreatedAt: base}
	second := Record{ID: "b", Name: "Term", Target: "/usr/bin/term", Args: []string{"-e"}, CreatedAt: base.Add(time.Second)}
	if err := s.Add(second); err != nil {
		t.Fatalf("Add second: %v", err)
	}
	if err := s.Add(first); err != nil {
		t.Fatalf("Add first: %v", err)
	}

	rows, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// write sorts by creation time, so the earlier record comes first regardless
	// of add order.
	if len(rows) != 2 || rows[0].ID != "a" || rows[1].ID != "b" {
		t.Fatalf("unexpected order/content: %+v", rows)
	}

	got, err := s.Get("b")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Target != "/usr/bin/term" || len(got.Args) != 1 || got.Args[0] != "-e" {
		t.Fatalf("Get returned %+v", got)
	}

	if err := s.Delete("a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	rows, _ = s.List()
	if len(rows) != 1 || rows[0].ID != "b" {
		t.Fatalf("after delete: %+v", rows)
	}
}

func TestAddDuplicateIDFails(t *testing.T) {
	s := NewFileStore(filepath.Join(t.TempDir(), "launchpad.json"))
	r := Record{ID: "x", Name: "X", Target: "/x", CreatedAt: time.Now().UTC()}
	if err := s.Add(r); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Add(r); err == nil {
		t.Fatal("want error adding a duplicate id")
	}
}

func TestGetMissingReturnsErrNotFound(t *testing.T) {
	s := NewFileStore(filepath.Join(t.TempDir(), "launchpad.json"))
	if _, err := s.Get("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestDeleteMissingIsNoError(t *testing.T) {
	s := NewFileStore(filepath.Join(t.TempDir(), "launchpad.json"))
	if err := s.Delete("nope"); err != nil {
		t.Fatalf("Delete missing: %v", err)
	}
}
