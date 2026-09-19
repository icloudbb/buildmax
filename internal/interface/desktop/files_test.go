package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListWorkspaceDir(t *testing.T) {
	root := t.TempDir()
	// A "secret" beside the root proves traversal cannot climb above it.
	if err := os.WriteFile(filepath.Join(filepath.Dir(root), "outside.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, filepath.Join(root, ".git"))
	mustMkdir(t, filepath.Join(root, "src"))
	mustMkdir(t, filepath.Join(root, "src", "inner"))
	mustWrite(t, filepath.Join(root, "src", "main.go"))
	mustWrite(t, filepath.Join(root, "README.md"))
	mustWrite(t, filepath.Join(root, "Makefile"))

	t.Run("root lists dirs first, skips .git, sorts case-insensitively", func(t *testing.T) {
		got, err := listWorkspaceDir(root, "")
		if err != nil {
			t.Fatal(err)
		}
		if got.Error != "" {
			t.Fatalf("unexpected error: %s", got.Error)
		}
		want := []WorkspaceEntry{
			{Name: "src", Path: "src", IsDir: true},
			{Name: "Makefile", Path: "Makefile", IsDir: false},
			{Name: "README.md", Path: "README.md", IsDir: false},
		}
		assertEntries(t, got.Entries, want)
	})

	t.Run("subdirectory listing uses root-relative child paths", func(t *testing.T) {
		got, err := listWorkspaceDir(root, "src")
		if err != nil {
			t.Fatal(err)
		}
		want := []WorkspaceEntry{
			{Name: "inner", Path: "src/inner", IsDir: true},
			{Name: "main.go", Path: "src/main.go", IsDir: false},
		}
		assertEntries(t, got.Entries, want)
	})

	t.Run("traversal collapses to the root, never above it", func(t *testing.T) {
		for _, rel := range []string{"..", "../..", "src/../..", "/.."} {
			got, err := listWorkspaceDir(root, rel)
			if err != nil {
				t.Fatalf("%q: %v", rel, err)
			}
			if got.Dir != "" {
				t.Fatalf("%q: escaped to %q", rel, got.Dir)
			}
			for _, e := range got.Entries {
				if e.Name == "outside.txt" {
					t.Fatalf("%q: leaked a sibling of the root", rel)
				}
			}
		}
	})

	t.Run("unreadable directory reports an error in place", func(t *testing.T) {
		got, err := listWorkspaceDir(root, "does-not-exist")
		if err != nil {
			t.Fatal(err)
		}
		if got.Error == "" {
			t.Fatal("expected a per-directory error")
		}
	})
}

func TestReadWorkspaceFile(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "src"))
	if err := os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "logo.png"), []byte{0x89, 0x50, 0x00, 0x01}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(root), "outside.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("text file returns its content", func(t *testing.T) {
		got, err := readWorkspaceFile(root, "src/main.go")
		if err != nil {
			t.Fatal(err)
		}
		if got.Binary || got.Error != "" {
			t.Fatalf("unexpected: %+v", got)
		}
		if got.Content != "package main\n" {
			t.Fatalf("content = %q", got.Content)
		}
	})

	t.Run("binary file is reported, not returned", func(t *testing.T) {
		got, err := readWorkspaceFile(root, "logo.png")
		if err != nil {
			t.Fatal(err)
		}
		if !got.Binary || got.Content != "" {
			t.Fatalf("expected binary with no content, got %+v", got)
		}
	})

	t.Run("directory is not a file", func(t *testing.T) {
		got, err := readWorkspaceFile(root, "src")
		if err != nil {
			t.Fatal(err)
		}
		if got.Error == "" {
			t.Fatal("expected an error for a directory")
		}
	})

	t.Run("traversal cannot read outside the root", func(t *testing.T) {
		got, err := readWorkspaceFile(root, "../outside.txt")
		if err != nil {
			t.Fatal(err)
		}
		// "../outside.txt" collapses to "outside.txt" under the root, which does
		// not exist — so it errors rather than leaking the sibling file.
		if got.Content == "secret" {
			t.Fatal("leaked a file outside the workspace")
		}
		if got.Error == "" {
			t.Fatal("expected a not-found error")
		}
	})
}

func TestWriteWorkspaceFile(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "src"))
	if err := os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(root), "outside.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("overwrites a file and returns the fresh content", func(t *testing.T) {
		got, err := writeWorkspaceFile(root, "src/main.go", "package main\n\nfunc main() {}\n")
		if err != nil {
			t.Fatal(err)
		}
		if got.Error != "" {
			t.Fatalf("unexpected error: %s", got.Error)
		}
		if got.Content != "package main\n\nfunc main() {}\n" {
			t.Fatalf("content = %q", got.Content)
		}
		on, err := os.ReadFile(filepath.Join(root, "src", "main.go"))
		if err != nil {
			t.Fatal(err)
		}
		if string(on) != "package main\n\nfunc main() {}\n" {
			t.Fatalf("on disk = %q", on)
		}
	})

	t.Run("creates a new file under the root", func(t *testing.T) {
		got, err := writeWorkspaceFile(root, "src/new.txt", "hello")
		if err != nil {
			t.Fatal(err)
		}
		if got.Error != "" || got.Content != "hello" {
			t.Fatalf("unexpected: %+v", got)
		}
	})

	t.Run("refuses to write a directory", func(t *testing.T) {
		got, err := writeWorkspaceFile(root, "src", "x")
		if err != nil {
			t.Fatal(err)
		}
		if got.Error == "" {
			t.Fatal("expected an error writing a directory")
		}
	})

	t.Run("traversal cannot write outside the root", func(t *testing.T) {
		if _, err := writeWorkspaceFile(root, "../outside.txt", "clobbered"); err != nil {
			t.Fatal(err)
		}
		// "../outside.txt" collapses to "outside.txt" under the root, so the real
		// sibling is untouched.
		on, err := os.ReadFile(filepath.Join(filepath.Dir(root), "outside.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if string(on) != "secret" {
			t.Fatal("wrote outside the workspace root")
		}
	})
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, p string) {
	t.Helper()
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertEntries(t *testing.T, got, want []WorkspaceEntry) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}
