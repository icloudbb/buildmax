// Package localterminalsnapshotstore persists the Desktop app's terminal buffer
// snapshots as a single JSON file under BUILDMAX_HOME.
//
// A snapshot is the serialized visible contents of one terminal tab, saved so a
// restart can restore what the terminal showed (a fresh shell then runs beneath
// it). Snapshots are keyed by project id and a stable per-terminal restore key
// the frontend assigns; the shell process itself is never persisted. The Desktop
// process is the only reader and writer.
package localterminalsnapshotstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/icloudbb/buildmax/internal/util"
)

// maxSnapshotBytes caps one snapshot. A terminal's serialized buffer is already
// bounded by its scrollback, but a pathological line can still be long; keeping
// the tail bounds the file and preserves the most recent (most useful) output.
const maxSnapshotBytes = 256 * 1024

// FileStore keeps every snapshot in one JSON file: project id -> restore key ->
// serialized contents. All mutations read-modify-write under an in-process lock
// and replace the file atomically.
type FileStore struct {
	path string
	mu   sync.Mutex
}

// NewFileStore returns a store over the snapshot file at path.
func NewFileStore(path string) *FileStore { return &FileStore{path: path} }

// load reads the file. A missing file is an empty map, not an error.
func (s *FileStore) load() (map[string]map[string]string, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]map[string]string{}, nil
		}
		return nil, err
	}
	var m map[string]map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("localterminalsnapshotstore: read %s: %w", s.path, err)
	}
	if m == nil {
		m = map[string]map[string]string{}
	}
	return m, nil
}

func (s *FileStore) write(m map[string]map[string]string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	return util.WriteFileAtomic(s.path, data, 0o600)
}

// Save stores content for one terminal, replacing any previous snapshot. Content
// longer than the cap keeps its tail.
func (s *FileStore) Save(projectID, key, content string) error {
	if projectID == "" || key == "" {
		return fmt.Errorf("localterminalsnapshotstore: project id and key are required")
	}
	if len(content) > maxSnapshotBytes {
		content = content[len(content)-maxSnapshotBytes:]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return err
	}
	if m[projectID] == nil {
		m[projectID] = map[string]string{}
	}
	m[projectID][key] = content
	return s.write(m)
}

// Load returns the snapshot for one terminal, or "" when none is stored.
func (s *FileStore) Load(projectID, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return "", err
	}
	return m[projectID][key], nil
}

// Prune drops every snapshot for projectID whose key is not in keep, so closed
// terminals do not leave their contents behind. An empty project map is removed.
func (s *FileStore) Prune(projectID string, keep []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return err
	}
	proj := m[projectID]
	if proj == nil {
		return nil
	}
	keepSet := make(map[string]struct{}, len(keep))
	for _, k := range keep {
		keepSet[k] = struct{}{}
	}
	for k := range proj {
		if _, ok := keepSet[k]; !ok {
			delete(proj, k)
		}
	}
	if len(proj) == 0 {
		delete(m, projectID)
	}
	return s.write(m)
}
