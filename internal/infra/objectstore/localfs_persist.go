package objectstore

import (
	"context"
	"github.com/icloudbb/buildmax/internal/core/apierr"
	"io"
	"os"
	"path/filepath"
)

// LocalFSPersistStorage implements PersistStorage using the local filesystem.
type LocalFSPersistStorage struct {
	persistRoot func(spaceID string) string
	// runGlobalDir resolves the on-disk directory a worker wrote a run's global
	// files to, so the store can read or remove one. Home files live under persistRoot;
	// run-global files live in a separate subtree the caller owns the layout of,
	// which is why this is injected rather than derived from persistRoot.
	runGlobalDir func(spaceID, taskID, taskRunID string) string
}

// NewLocalFSPersistStorage returns a PersistStorage that uses the given per-space
// persist root and the given resolver for a run's global directory.
func NewLocalFSPersistStorage(persistRoot func(spaceID string) string, runGlobalDir func(spaceID, taskID, taskRunID string) string) *LocalFSPersistStorage {
	return &LocalFSPersistStorage{persistRoot: persistRoot, runGlobalDir: runGlobalDir}
}

// Put writes one file at relPath under the space's persist root.
func (s *LocalFSPersistStorage) Put(ctx context.Context, spaceID string, relPath string, r io.Reader) error {
	clean, err := CleanRelPath(relPath)
	if err != nil {
		return err
	}
	root := s.persistRoot(spaceID)
	fullPath := filepath.Join(root, filepath.FromSlash(clean))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return err
	}
	f, err := os.Create(fullPath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

// Get reads one file. Returns os.ErrNotExist if the file does not exist.
func (s *LocalFSPersistStorage) Get(ctx context.Context, spaceID string, relPath string) ([]byte, error) {
	clean, err := CleanRelPath(relPath)
	if err != nil {
		return nil, err
	}
	root := s.persistRoot(spaceID)
	fullPath := filepath.Join(root, filepath.FromSlash(clean))
	return os.ReadFile(fullPath)
}

// ListFiles returns all file relative paths under the space persist root (files only).
func (s *LocalFSPersistStorage) ListFiles(ctx context.Context, spaceID string) ([]string, error) {
	root := s.persistRoot(spaceID)
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		out = append(out, rel)
		return nil
	})
	return out, err
}

// PutRunGlobal is a no-op for local FS (task run global files already live on worker disk).
func (s *LocalFSPersistStorage) PutRunGlobal(ctx context.Context, ref RunObjectRef, r io.Reader) error {
	return nil
}

// GetRunGlobal reads one run-global file from the worker disk it was written
// to: on local_fs that disk is the storage, since PutRunGlobal stores nothing
// elsewhere. Reporting every file as missing here broke a Continue run's session
// restore, which reads the previous run's bundle through this method and has no
// disk path of its own to fall back to. Without a resolver the store does not
// know the layout and reports the file as not found.
func (s *LocalFSPersistStorage) GetRunGlobal(ctx context.Context, ref RunObjectRef) ([]byte, error) {
	if s.runGlobalDir == nil {
		return nil, apierr.ErrNotFound
	}
	clean, err := CleanRelPath(ref.RelPath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(s.runGlobalDir(ref.SpaceID, ref.TaskID, ref.TaskRunID), filepath.FromSlash(clean)))
	if os.IsNotExist(err) {
		return nil, apierr.ErrNotFound
	}
	return data, err
}

// DeleteRunGlobal removes one run-global file from the worker disk it was
// written to. A file that is not there is not an error. Without a resolver the
// store does not know the layout and reports nothing to remove.
func (s *LocalFSPersistStorage) DeleteRunGlobal(ctx context.Context, ref RunObjectRef) error {
	if s.runGlobalDir == nil {
		return nil
	}
	clean, err := CleanRelPath(ref.RelPath)
	if err != nil {
		return err
	}
	fullPath := filepath.Join(s.runGlobalDir(ref.SpaceID, ref.TaskID, ref.TaskRunID), filepath.FromSlash(clean))
	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// MaterializeToDir copies all persistent files from the space into dstDir.
// If the persist root does not exist or is empty, no error (empty dst).
func (s *LocalFSPersistStorage) MaterializeToDir(ctx context.Context, spaceID string, dstDir string) error {
	root := s.persistRoot(spaceID)
	return copyDirContents(root, dstDir)
}

// copyDirContents copies files and directories from src to dst recursively.
// If src is missing or not a directory, returns nil (no-op).
func copyDirContents(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		destPath := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode().Perm())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return copyFile(path, destPath)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	// Close reports write errors that Copy could not see yet, so on the write
	// side it belongs in the return path rather than in a defer.
	return out.Close()
}
