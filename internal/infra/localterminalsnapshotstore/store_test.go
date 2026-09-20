package localterminalsnapshotstore

import (
	"path/filepath"
	"strings"
	"testing"
)

func newStore(t *testing.T) *FileStore {
	t.Helper()
	return NewFileStore(filepath.Join(t.TempDir(), "terminal-snapshots.json"))
}

func mustSave(t *testing.T, s *FileStore, projectID, key, content string) {
	t.Helper()
	if err := s.Save(projectID, key, content); err != nil {
		t.Fatalf("Save(%s/%s): %v", projectID, key, err)
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	s := newStore(t)
	got, err := s.Load("p1", "k1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s := newStore(t)
	if err := s.Save("p1", "k1", "hello"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.Save("p1", "k2", "world"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.Save("p2", "k1", "other"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got, _ := s.Load("p1", "k1"); got != "hello" {
		t.Fatalf("p1/k1 = %q", got)
	}
	if got, _ := s.Load("p2", "k1"); got != "other" {
		t.Fatalf("p2/k1 = %q", got)
	}
	// Re-saving replaces.
	if err := s.Save("p1", "k1", "again"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got, _ := s.Load("p1", "k1"); got != "again" {
		t.Fatalf("p1/k1 after resave = %q", got)
	}
}

func TestPruneKeepsOnlyListed(t *testing.T) {
	s := newStore(t)
	mustSave(t, s, "p1", "keep", "a")
	mustSave(t, s, "p1", "drop", "b")
	mustSave(t, s, "p2", "x", "c")
	if err := s.Prune("p1", []string{"keep"}); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if got, _ := s.Load("p1", "keep"); got != "a" {
		t.Fatalf("kept snapshot lost: %q", got)
	}
	if got, _ := s.Load("p1", "drop"); got != "" {
		t.Fatalf("dropped snapshot survived: %q", got)
	}
	if got, _ := s.Load("p2", "x"); got != "c" {
		t.Fatalf("other project pruned: %q", got)
	}
}

func TestSaveTruncatesToTail(t *testing.T) {
	s := newStore(t)
	long := strings.Repeat("a", maxSnapshotBytes) + "TAIL"
	if err := s.Save("p", "k", long); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, _ := s.Load("p", "k")
	if len(got) != maxSnapshotBytes {
		t.Fatalf("want capped length %d, got %d", maxSnapshotBytes, len(got))
	}
	if !strings.HasSuffix(got, "TAIL") {
		t.Fatalf("truncation dropped the tail: %q...", got[:16])
	}
}

func TestSaveRequiresKeys(t *testing.T) {
	s := newStore(t)
	if err := s.Save("", "k", "x"); err == nil {
		t.Fatal("want error for empty project id")
	}
	if err := s.Save("p", "", "x"); err == nil {
		t.Fatal("want error for empty key")
	}
}
