package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// approvalRoot is a project folder with one ordinary file, one binary file,
// and links that lead out of it, beside a directory holding a secret.
func approvalRoot(t *testing.T) (root, outside string) {
	t.Helper()
	root = t.TempDir()
	outside = t.TempDir()
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "src", "main.go"), "package main\n\nfunc main() {\n}\n")
	write(filepath.Join(root, "logo.png"), "\x89PNG\x00\x01")
	write(filepath.Join(outside, "secret"), "do not show\n")
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "link-file")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link-dir")); err != nil {
		t.Fatal(err)
	}
	return root, outside
}

func TestApprovalFileReadsTheFileAnEditWouldChange(t *testing.T) {
	root, _ := approvalRoot(t)

	got := approvalFile(root, "Edit", map[string]any{"file_path": "src/main.go", "old_string": "x", "new_string": "y"})
	if got == nil || !got.Exists || got.Unavailable != "" || got.Path != "src/main.go" {
		t.Fatalf("approvalFile = %+v, want the existing file's content", got)
	}
	if got.Content != "package main\n\nfunc main() {\n}\n" {
		t.Fatalf("content = %q, want the file byte for byte", got.Content)
	}

	// An absolute path inside the root reads the same file and is shown relative.
	abs := approvalFile(root, "Write", map[string]any{"file_path": filepath.Join(root, "src", "main.go"), "content": ""})
	if abs == nil || !abs.Exists || abs.Path != "src/main.go" || abs.Content != got.Content {
		t.Fatalf("absolute approvalFile = %+v, want the same file", abs)
	}
}

func TestApprovalFileReportsAWriteThatCreatesAFile(t *testing.T) {
	root, _ := approvalRoot(t)
	got := approvalFile(root, "Write", map[string]any{"file_path": "docs/new.md", "content": "hello\n"})
	if got == nil || got.Exists || got.Unavailable != "" || got.Content != "" {
		t.Fatalf("approvalFile = %+v, want a file that does not exist yet", got)
	}
}

func TestApprovalFileStaysInsideTheToolRoot(t *testing.T) {
	root, outside := approvalRoot(t)
	for _, tc := range []struct{ name, path string }{
		{"dot-dot escape", "../" + filepath.Base(outside) + "/secret"},
		{"absolute path outside", filepath.Join(outside, "secret")},
		{"symlink to a file outside", "link-file"},
		{"symlink to a directory outside", "link-dir/secret"},
		{"new file under a symlink to a directory outside", "link-dir/new.txt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := approvalFile(root, "Write", map[string]any{"file_path": tc.path, "content": ""})
			if got == nil || got.Unavailable != "outside_root" {
				t.Fatalf("approvalFile(%q) = %+v, want outside_root", tc.path, got)
			}
			if got.Content != "" || strings.Contains(got.Content, "do not show") {
				t.Fatalf("approvalFile(%q) returned content from outside the root", tc.path)
			}
		})
	}
}

func TestApprovalFileDoesNotPreviewBinaryOrHugeFiles(t *testing.T) {
	root, _ := approvalRoot(t)
	if got := approvalFile(root, "Edit", map[string]any{"file_path": "logo.png"}); got == nil || got.Unavailable != "binary" || got.Content != "" {
		t.Fatalf("binary approvalFile = %+v, want binary with no content", got)
	}

	huge := filepath.Join(root, "huge.log")
	if err := os.WriteFile(huge, []byte(strings.Repeat("a", maxFilePreviewBytes+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := approvalFile(root, "Write", map[string]any{"file_path": "huge.log"}); got == nil || got.Unavailable != "too_large" || got.Content != "" {
		t.Fatalf("huge approvalFile = %+v, want too_large with no content", got)
	}

	if got := approvalFile(root, "Write", map[string]any{"file_path": "src"}); got == nil || got.Unavailable != "not_a_file" {
		t.Fatalf("directory approvalFile = %+v, want not_a_file", got)
	}
}

func TestApprovalFileIsOnlyForEditAndWrite(t *testing.T) {
	root, _ := approvalRoot(t)
	for _, name := range []string{"Read", "Bash", "BrowserNavigate"} {
		if got := approvalFile(root, name, map[string]any{"file_path": "src/main.go"}); got != nil {
			t.Fatalf("approvalFile(%s) = %+v, want nil", name, got)
		}
	}
	if got := approvalFile(root, "Write", map[string]any{"content": "x"}); got != nil {
		t.Fatalf("approvalFile without a path = %+v, want nil", got)
	}
}
