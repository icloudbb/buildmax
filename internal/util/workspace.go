package util

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveWorkspaceRoot resolves and absolutizes a workspace root directory.
// If dir is empty, the current working directory is used.
func ResolveWorkspaceRoot(dir string) (string, error) {
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

// Workspace reports the directory a caller resolves paths against.
//
// It is consulted per call rather than captured at construction: a session's
// root moves when it enters a worktree, and a tool or sandbox profile still
// holding the launch directory would keep working on the tree the user left,
// with nothing to signal it. See docs/design/workspace-root-and-worktrees.md.
type Workspace interface {
	Root() string
}

// FixedRoot is a Workspace that never moves: a surface with no way to switch
// roots, and any caller that only needs one directory.
type FixedRoot string

// Root implements Workspace.
func (f FixedRoot) Root() string { return string(f) }

// ErrPathOutsideRoot reports a path that resolves outside the root a caller
// confines it to.
var ErrPathOutsideRoot = errors.New("path outside allowed root")

// ResolvePath resolves a user-supplied path relative to root, ensuring the result
// stays under root. Returns the absolute, cleaned path.
// Includes a Windows-safe prefix check (filepath.Rel can return an absolute
// path when roots differ on different drives).
// Does NOT stat the path — callers handle existence and type checks.
func ResolvePath(root, userPath string) (string, error) {
	var resolved string
	if filepath.IsAbs(userPath) {
		r, err := filepath.Abs(filepath.Clean(userPath))
		if err != nil {
			return "", err
		}
		resolved = r
	} else {
		joined := filepath.Join(root, userPath)
		r, err := filepath.Abs(filepath.Clean(joined))
		if err != nil {
			return "", err
		}
		resolved = r
	}

	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", ErrPathOutsideRoot
	}
	if rel == ".." || strings.HasPrefix(rel, "..") {
		return "", ErrPathOutsideRoot
	}

	cleanRoot := filepath.Clean(root)
	resolvedClean := filepath.Clean(resolved)
	if resolvedClean != cleanRoot && !strings.HasPrefix(resolvedClean, cleanRoot+string(filepath.Separator)) {
		return "", ErrPathOutsideRoot
	}

	return resolved, nil
}

// ResolveRealPath is ResolvePath with symlinks followed: it returns the
// physical path userPath names and requires that path to lie under root's
// physical path too. ResolvePath decides containment lexically, so a link
// inside root can still name a file outside it; a caller that sends a file's
// bytes somewhere needs this check as well.
//
// A path that does not exist yet is judged by its nearest existing ancestor,
// so a new file under a linked directory that leads outside root is refused
// rather than passing as a file inside it. The root itself is resolved: on
// macOS a temporary or home directory is reached through a link, and
// comparing a resolved path against an unresolved root would refuse every
// legitimate one.
func ResolveRealPath(root, userPath string) (string, error) {
	resolved, err := ResolvePath(root, userPath)
	if err != nil {
		return "", err
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		realRoot = root
	}
	existing, rest := resolved, ""
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", ErrPathOutsideRoot
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = parent
	}
	real, err := filepath.EvalSymlinks(existing)
	if err != nil {
		// The name exists but does not resolve: a dangling link. Writing
		// through it would create its target, wherever that is, so this must
		// not read as "does not exist yet".
		return "", fmt.Errorf("%s is a link whose target cannot be resolved", userPath)
	}
	if rest != "" {
		real = filepath.Join(real, rest)
	}
	if _, err := ResolvePath(realRoot, real); err != nil {
		return "", err
	}
	return real, nil
}
