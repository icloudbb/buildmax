package git

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func headSHA(t *testing.T, root string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestListCommitsPagesNewestFirst(t *testing.T) {
	root := t.TempDir()
	initRepo(t, root)
	for _, name := range []string{"second", "third", "fourth"} {
		writeFile(t, root, name+".txt", name+"\n")
		git(t, root, "add", ".")
		git(t, root, "commit", "-m", "Add "+name)
	}

	first, err := ListCommits(context.Background(), root, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := subjects(first.Commits); strings.Join(got, ",") != "Add fourth,Add third" {
		t.Fatalf("first page = %v", got)
	}
	if !first.HasMore {
		t.Fatal("first page should report an older page")
	}
	if first.Branch == "" {
		t.Fatal("branch should be reported on a checked-out branch")
	}
	c := first.Commits[0]
	if len(c.SHA) < 40 || len(c.Parents) != 1 || c.Author != "Test User" || c.AuthoredAt.IsZero() {
		t.Fatalf("commit fields not filled: %+v", c)
	}

	last, err := ListCommits(context.Background(), root, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := subjects(last.Commits); strings.Join(got, ",") != "Add second,initial" {
		t.Fatalf("last page = %v", got)
	}
	if last.HasMore {
		t.Fatal("last page should not report an older page")
	}
}

func TestListCommitsMarksMergeParents(t *testing.T) {
	root := t.TempDir()
	initRepo(t, root)
	git(t, root, "checkout", "-q", "-b", "topic")
	writeFile(t, root, "topic.txt", "topic\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "Topic work")
	git(t, root, "checkout", "-q", "-")
	git(t, root, "merge", "--no-ff", "-m", "Merge topic", "topic")

	log, err := ListCommits(context.Background(), root, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if log.Commits[0].Subject != "Merge topic" || len(log.Commits[0].Parents) != 2 {
		t.Fatalf("merge commit = %+v", log.Commits[0])
	}

	// A merge's change is what the merged branch brought in: its first-parent diff.
	detail, err := ReadCommit(context.Background(), root, log.Commits[0].SHA)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Files) != 1 || detail.Files[0].Path != "topic.txt" || detail.Files[0].Status != StatusAdded {
		t.Fatalf("merge files = %+v", detail.Files)
	}
}

func TestListCommitsOutsideARepositoryAndBeforeTheFirstCommit(t *testing.T) {
	plain, err := ListCommits(context.Background(), t.TempDir(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if plain.Error != "not a git repository" {
		t.Fatalf("plain directory error = %q", plain.Error)
	}

	root := t.TempDir()
	git(t, root, "init")
	empty, err := ListCommits(context.Background(), root, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Error != "" || len(empty.Commits) != 0 || empty.HasMore {
		t.Fatalf("unborn history = %+v", empty)
	}
}

func TestReadCommitListsItsChanges(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init")
	git(t, root, "config", "user.email", "test@example.com")
	git(t, root, "config", "user.name", "Test User")
	writeFile(t, root, "modified.txt", "one\n")
	writeFile(t, root, "deleted.txt", "gone\n")
	writeFile(t, root, "old name.txt", "a stable body long enough to be detected as a rename\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "initial")

	// The root commit compares with the empty tree.
	rootCommit, err := ReadCommit(context.Background(), root, headSHA(t, root))
	if err != nil {
		t.Fatal(err)
	}
	if len(rootCommit.Files) != 3 {
		t.Fatalf("root commit files = %+v", rootCommit.Files)
	}

	writeFile(t, root, "modified.txt", "one\ntwo\n")
	git(t, root, "rm", "-q", "deleted.txt")
	git(t, root, "mv", "old name.txt", "new name.txt")
	writeFile(t, root, "added.txt", "new\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "Change things")
	sha := headSHA(t, root)

	detail, err := ReadCommit(context.Background(), root, sha)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Commit.Subject != "Change things" {
		t.Fatalf("commit = %+v", detail.Commit)
	}
	got := map[string]ChangedFile{}
	for _, f := range detail.Files {
		got[f.Path] = f
		if f.Patch != "" {
			t.Fatalf("ReadCommit should not carry patches: %+v", f)
		}
	}
	if f := got["modified.txt"]; f.Status != StatusModified || f.Additions != 1 || f.Deletions != 0 {
		t.Fatalf("modified = %+v", f)
	}
	if f := got["new name.txt"]; f.Status != StatusRenamed || f.OldPath != "old name.txt" {
		t.Fatalf("renamed = %+v", f)
	}
	if got["deleted.txt"].Status != StatusDeleted || got["added.txt"].Status != StatusAdded {
		t.Fatalf("all = %+v", got)
	}

	file, err := ReadCommitFile(context.Background(), root, sha, "modified.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(file.Patch, "+two") {
		t.Fatalf("patch = %q", file.Patch)
	}
	if _, err := ReadCommitFile(context.Background(), root, sha, "untouched.txt"); err == nil {
		t.Fatal("a path the commit did not change should be an error")
	}
}

func TestReadCommitRefusesANonHexRevision(t *testing.T) {
	root := t.TempDir()
	initRepo(t, root)
	for _, rev := range []string{"HEAD", "--output=/tmp/x", "main~1", ""} {
		if _, err := ReadCommit(context.Background(), root, rev); err == nil {
			t.Fatalf("revision %q should be refused", rev)
		}
	}
}

func subjects(commits []Commit) []string {
	out := make([]string, len(commits))
	for i, c := range commits {
		out[i] = c.Subject
	}
	return out
}
