package git

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// CommitPageSize is the default and CommitPageMax the largest page ListCommits
// returns; a caller pages further back with skip.
const (
	CommitPageSize = 50
	CommitPageMax  = 200
)

type Commit struct {
	SHA        string    `json:"sha"`
	Parents    []string  `json:"parents"`
	Author     string    `json:"author"`
	AuthoredAt time.Time `json:"authored_at"`
	Subject    string    `json:"subject"`
}

// CommitLog is one page of HEAD's history. Error carries the same
// "not a git repository" answer WorkspaceDiff does, so a plain-directory
// Project renders a hint rather than failing.
type CommitLog struct {
	Workspace string   `json:"workspace"`
	Branch    string   `json:"branch,omitempty"`
	Commits   []Commit `json:"commits"`
	HasMore   bool     `json:"has_more"`
	Error     string   `json:"error,omitempty"`
}

// CommitDetail lists what one commit changed, without patches: a commit can
// touch hundreds of files, and a patch is read per file when one is opened.
type CommitDetail struct {
	Commit Commit        `json:"commit"`
	Files  []ChangedFile `json:"files"`
}

// A revision argument must be a hex object name. Anything else could be read
// by git as an option or a revision expression.
var commitSHAPattern = regexp.MustCompile(`^[0-9a-f]{4,64}$`)

const (
	logFieldSep  = "\x1f"
	logFormatArg = "--format=%H%x1f%P%x1f%an%x1f%aI%x1f%s"
)

// ListCommits returns up to limit commits reachable from HEAD, newest first,
// after skipping skip of them. A repository with no commits yet has an empty
// history, not an error.
func ListCommits(ctx context.Context, workspace string, skip, limit int) (CommitLog, error) {
	root, err := repoRoot(workspace)
	if err != nil {
		return CommitLog{}, err
	}
	if _, err := runGit(ctx, root, "rev-parse", "--is-inside-work-tree"); err != nil {
		return CommitLog{Workspace: root, Error: "not a git repository"}, nil
	}
	log := CommitLog{Workspace: root, Branch: CurrentBranch(root), Commits: []Commit{}}
	if _, err := runGit(ctx, root, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
		return log, nil
	}
	if skip < 0 {
		skip = 0
	}
	if limit <= 0 {
		limit = CommitPageSize
	}
	limit = min(limit, CommitPageMax)

	// One extra row answers whether an older page exists.
	out, err := runGit(ctx, root, "log", "-z", "--no-show-signature", logFormatArg,
		fmt.Sprintf("--skip=%d", skip), fmt.Sprintf("--max-count=%d", limit+1), "HEAD", "--")
	if err != nil {
		return CommitLog{}, fmt.Errorf("git log: %w: %s", err, strings.TrimSpace(out))
	}
	commits, err := parseCommits(out)
	if err != nil {
		return CommitLog{}, err
	}
	if len(commits) > limit {
		commits = commits[:limit]
		log.HasMore = true
	}
	log.Commits = commits
	return log, nil
}

// ReadCommit returns sha's metadata and the files it changed, compared with its
// first parent. First-parent is what a merge commit means on a merge-commit
// history: the change the merged branch brought in.
func ReadCommit(ctx context.Context, workspace, sha string) (CommitDetail, error) {
	root, commit, err := resolveCommit(ctx, workspace, sha)
	if err != nil {
		return CommitDetail{}, err
	}
	rng := commitRange(commit)
	statusOut, err := runGit(ctx, root, append([]string{"diff-tree", "-r", "-M", "-z", "--no-commit-id", "--name-status"}, rng...)...)
	if err != nil {
		return CommitDetail{}, fmt.Errorf("git diff-tree: %w: %s", err, strings.TrimSpace(statusOut))
	}
	files := parseNameStatusZ(statusOut)
	counts := parseNumstatZ(mustRunGit(ctx, root, append([]string{"diff-tree", "-r", "-M", "-z", "--no-commit-id", "--numstat"}, rng...)...))
	for i := range files {
		if c, ok := counts[files[i].Path]; ok {
			files[i].Additions = c.additions
			files[i].Deletions = c.deletions
		}
	}
	return CommitDetail{Commit: commit, Files: files}, nil
}

// ReadCommitFile returns one file's change in sha, patch included. path is the
// file's path after the commit; for a rename the patch covers both names.
func ReadCommitFile(ctx context.Context, workspace, sha, path string) (ChangedFile, error) {
	detail, err := ReadCommit(ctx, workspace, sha)
	if err != nil {
		return ChangedFile{}, err
	}
	var file *ChangedFile
	for i := range detail.Files {
		if detail.Files[i].Path == path {
			file = &detail.Files[i]
			break
		}
	}
	if file == nil {
		return ChangedFile{}, fmt.Errorf("commit %s does not change %s", shortSHA(sha), path)
	}
	root, _ := repoRoot(workspace)
	args := append([]string{"diff-tree", "-r", "-M", "-p", "--no-commit-id", "--no-ext-diff", "--no-color"}, commitRange(detail.Commit)...)
	args = append(args, "--", file.Path)
	if file.OldPath != "" {
		args = append(args, file.OldPath)
	}
	out := mustRunGit(ctx, root, args...)
	file.Binary = strings.Contains(out, "Binary files ") || strings.Contains(out, "GIT binary patch")
	file.Patch, file.Truncated = truncateRunes(out, maxPatchRunes)
	return *file, nil
}

func repoRoot(workspace string) (string, error) {
	if strings.TrimSpace(workspace) == "" {
		return "", errors.New("workspace required")
	}
	return filepath.Abs(workspace)
}

func resolveCommit(ctx context.Context, workspace, sha string) (string, Commit, error) {
	root, err := repoRoot(workspace)
	if err != nil {
		return "", Commit{}, err
	}
	if !commitSHAPattern.MatchString(sha) {
		return "", Commit{}, fmt.Errorf("invalid commit %q: expected a hex object name", sha)
	}
	out, err := runGit(ctx, root, "log", "-z", "--no-show-signature", logFormatArg, "--max-count=1", sha, "--")
	if err != nil {
		return "", Commit{}, fmt.Errorf("commit %s not found: %s", shortSHA(sha), strings.TrimSpace(out))
	}
	commits, err := parseCommits(out)
	if err != nil {
		return "", Commit{}, err
	}
	if len(commits) != 1 {
		return "", Commit{}, fmt.Errorf("commit %s not found", shortSHA(sha))
	}
	return root, commits[0], nil
}

// commitRange is the diff-tree arguments comparing a commit with its first
// parent, or with the empty tree for a root commit.
func commitRange(c Commit) []string {
	if len(c.Parents) == 0 {
		return []string{"--root", c.SHA}
	}
	return []string{c.Parents[0], c.SHA}
}

func parseCommits(out string) ([]Commit, error) {
	var commits []Commit
	for rec := range strings.SplitSeq(out, "\x00") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		fields := strings.SplitN(rec, logFieldSep, 5)
		if len(fields) != 5 {
			return nil, fmt.Errorf("unexpected git log record %q", rec)
		}
		at, err := time.Parse(time.RFC3339, fields[3])
		if err != nil {
			return nil, fmt.Errorf("parse commit date %q: %w", fields[3], err)
		}
		commits = append(commits, Commit{
			SHA:        fields[0],
			Parents:    strings.Fields(fields[1]),
			Author:     fields[2],
			AuthoredAt: at,
			Subject:    fields[4],
		})
	}
	return commits, nil
}

// parseNameStatusZ reads `--name-status -z`: a status token, then one path, or
// two (old, new) for a rename.
func parseNameStatusZ(out string) []ChangedFile {
	tokens := strings.Split(strings.TrimRight(out, "\x00"), "\x00")
	var files []ChangedFile
	for i := 0; i < len(tokens); i++ {
		code := tokens[i]
		if code == "" {
			continue
		}
		switch code[0] {
		case 'R':
			if i+2 >= len(tokens) {
				return files
			}
			files = append(files, ChangedFile{OldPath: tokens[i+1], Path: tokens[i+2], Status: StatusRenamed})
			i += 2
		default:
			if i+1 >= len(tokens) {
				return files
			}
			files = append(files, ChangedFile{Path: tokens[i+1], Status: statusFromCode(code[:1])})
			i++
		}
	}
	return files
}

// parseNumstatZ reads `--numstat -z`: "add\tdel\tpath" per file, or
// "add\tdel\t" followed by old and new paths as separate tokens for a rename.
// Binary files report "-" counts and are left out.
func parseNumstatZ(out string) map[string]lineCounts {
	counts := map[string]lineCounts{}
	tokens := strings.Split(strings.TrimRight(out, "\x00"), "\x00")
	for i := 0; i < len(tokens); i++ {
		fields := strings.SplitN(tokens[i], "\t", 3)
		if len(fields) != 3 {
			continue
		}
		path := fields[2]
		if path == "" {
			if i+2 >= len(tokens) {
				break
			}
			path = tokens[i+2]
			i += 2
		}
		add, addOK := parseCount(fields[0])
		del, delOK := parseCount(fields[1])
		if addOK && delOK {
			counts[path] = lineCounts{additions: add, deletions: del}
		}
	}
	return counts
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
