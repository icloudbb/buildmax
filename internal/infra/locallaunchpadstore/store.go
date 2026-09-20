// Package locallaunchpadstore persists the Desktop app's launchpad — the user's
// custom quick-launch application entries — as a single JSON file under
// BUILDMAX_HOME.
//
// An entry is deliberately minimal: a display name and the target to hand the
// operating system (an application bundle, executable, document, or URL), plus
// optional arguments. The Desktop process is the only reader and writer, and the
// entries are global rather than project-scoped, so there is no ownership,
// authorization, or scheduling state to carry.
package locallaunchpadstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/icloudbb/buildmax/internal/util"
)

// ErrNotFound reports that no entry holds the given id.
var ErrNotFound = errors.New("locallaunchpadstore: entry not found")

// Record is one launchpad entry.
type Record struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Target is what the OS launcher acts on: an application bundle or executable
	// path, a document, or a URL.
	Target string `json:"target"`
	// Args are extra arguments passed to the launched application, if any.
	Args      []string  `json:"args,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// FileStore keeps every entry in one JSON file. All mutations read-modify-write
// under an in-process lock and replace the file atomically, so a reader never
// sees a partial write.
type FileStore struct {
	path string
	mu   sync.Mutex
}

// NewFileStore returns a store over the launchpad file at path.
func NewFileStore(path string) *FileStore { return &FileStore{path: path} }

// load reads the file. A missing file is an empty list, not an error: a machine
// that has never added an entry has no file yet.
func (s *FileStore) load() ([]Record, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var rows []Record
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("locallaunchpadstore: read %s: %w", s.path, err)
	}
	return rows, nil
}

// write replaces the file atomically. Rows are sorted by creation time so the
// file diffs cleanly and the UI shows entries in the order they were added.
func (s *FileStore) write(rows []Record) error {
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].CreatedAt.Before(rows[j].CreatedAt) })
	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	return util.WriteFileAtomic(s.path, data, 0o600)
}

// List returns every entry, oldest first.
func (s *FileStore) List() ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.load()
	if err != nil {
		return nil, err
	}
	if rows == nil {
		return []Record{}, nil
	}
	return rows, nil
}

// Get returns one entry by id.
func (s *FileStore) Get(id string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.load()
	if err != nil {
		return Record{}, err
	}
	for _, r := range rows {
		if r.ID == id {
			return r, nil
		}
	}
	return Record{}, fmt.Errorf("%w: %s", ErrNotFound, id)
}

// Add appends a new entry. The record must carry a unique id.
func (s *FileStore) Add(r Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.load()
	if err != nil {
		return err
	}
	for _, existing := range rows {
		if existing.ID == r.ID {
			return fmt.Errorf("locallaunchpadstore: entry %s already exists", r.ID)
		}
	}
	return s.write(append(rows, r))
}

// Delete removes one entry. A missing entry is not an error: the end state the
// caller asked for already holds.
func (s *FileStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.load()
	if err != nil {
		return err
	}
	kept := rows[:0]
	for _, r := range rows {
		if r.ID != id {
			kept = append(kept, r)
		}
	}
	return s.write(kept)
}
