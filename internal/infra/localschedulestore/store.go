// Package localschedulestore persists the Desktop app's local scheduled tasks
// as a single JSON file under BUILDMAX_HOME.
//
// A local scheduled task is deliberately leaner than the server's Schedule
// (internal/core/schedule): the Desktop process is the only writer and the only
// thing that fires it, so there is no space, agent definition, quota, creator
// eligibility, or multi-replica compare-and-swap to carry. Timezone-aware cron
// parsing and next-fire computation are reused from internal/service/schedule by
// the caller; this package only stores the records.
package localschedulestore

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

// ErrNotFound reports that no task holds the given id.
var ErrNotFound = errors.New("localschedulestore: task not found")

// Record is one local scheduled task. Times are stored in UTC.
type Record struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	// WorkingDir is the directory the fired agent runs in. Empty means the
	// user's home directory (resolved at fire time), so a task needs no project.
	WorkingDir string `json:"working_dir"`
	Prompt     string `json:"prompt"`
	// Model is the model each fire runs under. Empty means the app's default.
	Model    string `json:"model,omitempty"`
	CronExpr string `json:"cron_expr"`
	Timezone string `json:"timezone"`
	Enabled  bool   `json:"enabled"`
	// NextFireAt is the next UTC instant the task is due. The tick loop fires a
	// task when this is not after now.
	NextFireAt time.Time `json:"next_fire_at"`
	// LastFireAt is the last UTC instant the task fired, zero before the first.
	LastFireAt time.Time `json:"last_fire_at"`
	// ConsecutiveFailures counts fires that could not start a run in a row; the
	// caller pauses a task once it crosses its runaway threshold.
	ConsecutiveFailures int       `json:"consecutive_failures"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// FileStore keeps every task in one JSON file. All mutations read-modify-write
// under an in-process lock and replace the file atomically, so a reader never
// sees a partial write and concurrent tick and UI mutations do not lose each
// other's fields.
type FileStore struct {
	path string
	mu   sync.Mutex
}

// NewFileStore returns a store over the tasks file at path.
func NewFileStore(path string) *FileStore { return &FileStore{path: path} }

// load reads the file. A missing file is an empty list, not an error: a machine
// that has never made a task has no file yet.
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
		return nil, fmt.Errorf("localschedulestore: read %s: %w", s.path, err)
	}
	return rows, nil
}

// write replaces the file atomically. Rows are sorted by creation time so the
// file diffs cleanly.
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

// List returns every task, oldest first.
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

// Get returns one task by id.
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

// Add appends a new task. The record must carry a unique id.
func (s *FileStore) Add(r Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.load()
	if err != nil {
		return err
	}
	for _, existing := range rows {
		if existing.ID == r.ID {
			return fmt.Errorf("localschedulestore: task %s already exists", r.ID)
		}
	}
	return s.write(append(rows, r))
}

// Update applies mut to the task with the given id and writes it back. The
// mutation is field-scoped so a concurrent Update touching other fields is not
// lost.
func (s *FileStore) Update(id string, mut func(*Record)) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.load()
	if err != nil {
		return Record{}, err
	}
	for i := range rows {
		if rows[i].ID == id {
			mut(&rows[i])
			if err := s.write(rows); err != nil {
				return Record{}, err
			}
			return rows[i], nil
		}
	}
	return Record{}, fmt.Errorf("%w: %s", ErrNotFound, id)
}

// Delete removes one task. A missing task is not an error: the end state the
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
