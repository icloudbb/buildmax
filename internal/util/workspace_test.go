package util

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRealPathConfinesToRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "link-file")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link-dir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "src"), filepath.Join(root, "inner")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "missing"), filepath.Join(root, "dangling")); err != nil {
		t.Fatal(err)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, path, want string
	}{
		{"relative file", "src/main.go", filepath.Join(realRoot, "src", "main.go")},
		{"absolute file inside", filepath.Join(root, "src", "main.go"), filepath.Join(realRoot, "src", "main.go")},
		{"file not created yet", "src/new.go", filepath.Join(realRoot, "src", "new.go")},
		{"new directory and file", "docs/a/b.md", filepath.Join(realRoot, "docs", "a", "b.md")},
		{"link that stays inside", "inner/main.go", filepath.Join(realRoot, "src", "main.go")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveRealPath(root, tc.path)
			if err != nil {
				t.Fatalf("ResolveRealPath(%q) error = %v", tc.path, err)
			}
			if got != tc.want {
				t.Fatalf("ResolveRealPath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}

	for _, tc := range []struct{ name, path string }{
		{"dot-dot escape", "../" + filepath.Base(outside) + "/secret"},
		{"nested dot-dot escape", "src/../../x"},
		{"absolute path outside", filepath.Join(outside, "secret")},
		{"link to a file outside", "link-file"},
		{"new file under a link to a directory outside", "link-dir/new.txt"},
		{"existing file under a link to a directory outside", "link-dir/secret"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveRealPath(root, tc.path)
			if !errors.Is(err, ErrPathOutsideRoot) {
				t.Fatalf("ResolveRealPath(%q) = %q, %v; want ErrPathOutsideRoot", tc.path, got, err)
			}
		})
	}

	// A dangling link is not a file that does not exist yet: writing through it
	// creates its target, so it must not resolve as a new path.
	if got, err := ResolveRealPath(root, "dangling"); err == nil || os.IsNotExist(err) {
		t.Fatalf("ResolveRealPath(dangling) = %q, %v; want a non-NotExist error", got, err)
	}
}
