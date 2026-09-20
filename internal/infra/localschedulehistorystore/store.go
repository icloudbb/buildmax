// Package localschedulehistorystore persists the run history of the Desktop
// app's local scheduled tasks as a single JSON file under BUILDMAX_HOME.
//
// A scheduled fire creates a projectless chat session (see the Desktop
// scheduler); this store is the only link from a task to the sessions it
// produced, because those sessions carry no project and so never appear in the
// project session list. One Run row records when a task fired, which session it
// created, and how the run ended, so the Schedules view can show recent history
// and open any run's transcript.
package localschedulehistorystore

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

// ErrNotFound reports that no run holds the given id.
var ErrNotFound = errors.New("localschedulehistorystore: run not found")

// Run status values. A run starts Running and ends Ok or Failed; a run left
// Running is one the app did not see finish (it was closed mid-run).
const (
	StatusRunning = "running"
	StatusOK      = "ok"
	StatusFailed  = "failed"
)

// maxRunsPerSchedule bounds how many recent runs a task keeps, so the history
// file cannot grow without limit. Older runs are pruned at insert time.
const maxRunsPerSchedule = 50

// Run is one fire of a scheduled task. Times are stored in UTC.
type Run struct {
	ID         string `json:"id"`
	ScheduleID string `json:"schedule_id"`
	// SessionID is the chat session this fire created, for the UI to open. Empty
	// when the run failed to start before a session existed.
	SessionID string    `json:"session_id,omitempty"`
	FiredAt   time.Time `json:"fired_at"`
	Status    string    `json:"status"`
	// Error is the failure message when Status is Failed.
	Error string `json:"error,omitempty"`
}

// FileStore keeps every run in one JSON file, read-modify-written under an
// in-process lock and replaced atomically, matching localschedulestore.
type FileStore struct {
	path string
	mu   sync.Mutex
}

// NewFileStore returns a store over the history file at path.
func NewFileStore(path string) *FileStore { return &FileStore{path: path} }

// load reads the file; a missing file is an empty list.
func (s *FileStore) load() ([]Run, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var rows []Run
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("localschedulehistorystore: read %s: %w", s.path, err)
	}
	return rows, nil
}

// write replaces the file atomically, newest run first so the file reads like
// the history the UI shows.
func (s *FileStore) write(rows []Run) error {
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].FiredAt.After(rows[j].FiredAt) })
	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	return util.WriteFileAtomic(s.path, data, 0o600)
}

// List returns every run, newest first.
func (s *FileStore) List() ([]Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.load()
	if err != nil {
		return nil, err
	}
	if rows == nil {
		return []Run{}, nil
	}
	return rows, nil
}

// Add records a new run and prunes the owning task's history to the most recent
// maxRunsPerSchedule rows.
func (s *FileStore) Add(r Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.load()
	if err != nil {
		return err
	}
	rows = append(rows, r)
	rows = pruneSchedule(rows, r.ScheduleID)
	return s.write(rows)
}

// Update applies mut to the run with the given id and writes it back, so a
// fire's completion status can be stamped onto the row its start created.
func (s *FileStore) Update(id string, mut func(*Run)) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.load()
	if err != nil {
		return Run{}, err
	}
	for i := range rows {
		if rows[i].ID == id {
			mut(&rows[i])
			if err := s.write(rows); err != nil {
				return Run{}, err
			}
			return rows[i], nil
		}
	}
	return Run{}, fmt.Errorf("%w: %s", ErrNotFound, id)
}

// DeleteBySchedule removes every run of a task and returns the session ids those
// runs created, so the caller can delete the now-orphaned sessions too.
func (s *FileStore) DeleteBySchedule(scheduleID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.load()
	if err != nil {
		return nil, err
	}
	var sessions []string
	kept := rows[:0]
	for _, r := range rows {
		if r.ScheduleID == scheduleID {
			if r.SessionID != "" {
				sessions = append(sessions, r.SessionID)
			}
			continue
		}
		kept = append(kept, r)
	}
	if err := s.write(kept); err != nil {
		return nil, err
	}
	return sessions, nil
}

// pruneSchedule keeps only the most recent maxRunsPerSchedule runs of one task,
// leaving other tasks' rows untouched.
func pruneSchedule(rows []Run, scheduleID string) []Run {
	var owned []int
	for i := range rows {
		if rows[i].ScheduleID == scheduleID {
			owned = append(owned, i)
		}
	}
	if len(owned) <= maxRunsPerSchedule {
		return rows
	}
	// Newest first, drop the oldest beyond the cap.
	sort.SliceStable(owned, func(a, b int) bool { return rows[owned[a]].FiredAt.After(rows[owned[b]].FiredAt) })
	drop := make(map[int]bool, len(owned)-maxRunsPerSchedule)
	for _, idx := range owned[maxRunsPerSchedule:] {
		drop[idx] = true
	}
	kept := make([]Run, 0, len(rows)-len(drop))
	for i := range rows {
		if !drop[i] {
			kept = append(kept, rows[i])
		}
	}
	return kept
}
