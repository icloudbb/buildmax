package architecture_test

// The backlog frontmatter and the Roadmap `Status:` lines are the machine-readable
// status signal `./make board` renders. These tests keep that signal honest: a
// live task missing a field, or a priority whose one-word status is absent or
// misspelled, is caught here rather than surfacing as a silently wrong report.
//
// The allowed values are declared here rather than shared with tools/mk because a
// package under internal/ that tools/mk imported would pull the task runner into
// the application's dependency graph — the same reason changelog_test.go mirrors
// its categories.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var backlogRoadmaps = map[string]bool{
	"R0": true, "R1": true, "R2": true, "R3": true, "R4": true, "R5": true, "R6": true, "none": true,
}

var roadmapStatuses = map[string]bool{
	"open": true, "in-progress": true, "candidate-proof-remains": true, "done": true,
}

// backlogRequiredKeys are the frontmatter fields every live task carries. pr is
// optional — it appears only once a pull request is open — so it is validated
// for shape when present but not required here.
var backlogRequiredKeys = []string{"id", "title", "roadmap", "source", "depends_on", "verification", "claim"}

var (
	claimRe       = regexp.MustCompile(`^\S+ \d{4}-\d{2}-\d{2}$`)
	prRe          = regexp.MustCompile(`^#?\d+$`)
	taskFileRe    = regexp.MustCompile(`^\d+-[a-z0-9-]+\.md$`)
	roadmapHeadRe = regexp.MustCompile(`^### R\d+\.`)
	statusLineRe  = regexp.MustCompile(`^\*\*Status:\*\* (\S+)\s*$`)
)

func TestBacklogFrontmatterIsValid(t *testing.T) {
	root := moduleRoot(t)
	dir := filepath.Join(root, "docs", "backlog")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read docs/backlog: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !taskFileRe.MatchString(e.Name()) {
			// README.md and TEMPLATE.md document the queue; only NN-slug.md files
			// are live tasks the board reports on.
			continue
		}
		checkBacklogTask(t, filepath.Join(dir, e.Name()), e.Name())
	}
}

func checkBacklogTask(t *testing.T, path, name string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	fields, ok := parseFrontmatter(string(body))
	if !ok {
		t.Errorf("docs/backlog/%s has no `---` frontmatter block", name)
		return
	}
	for _, key := range backlogRequiredKeys {
		if _, present := fields[key]; !present {
			t.Errorf("docs/backlog/%s frontmatter is missing %q", name, key)
		}
	}
	if v := fields["roadmap"]; v != "" && !backlogRoadmaps[v] {
		t.Errorf("docs/backlog/%s roadmap %q is not one of R0-R6 or none", name, v)
	}
	if v := fields["claim"]; v != "" && !claimRe.MatchString(v) {
		t.Errorf("docs/backlog/%s claim %q is not \"<handle> <YYYY-MM-DD>\"", name, v)
	}
	if v := fields["pr"]; v != "" && !prRe.MatchString(v) {
		t.Errorf("docs/backlog/%s pr %q is not a pull request number", name, v)
	}
	for _, dep := range fields.list("depends_on") {
		// Shape only: a dependency that has merged is deleted, so requiring the
		// file to still exist would fail the moment a dependency lands.
		if !taskFileRe.MatchString(dep) {
			t.Errorf("docs/backlog/%s depends_on entry %q is not an NN-slug.md task filename", name, dep)
		}
	}
}

func TestRoadmapPrioritiesCarryStatus(t *testing.T) {
	root := moduleRoot(t)
	for _, rel := range []string{
		filepath.Join("docs", "ROADMAP.md"),
		filepath.Join("docs", "zh-CN", "ROADMAP.md"),
	} {
		checkRoadmapStatuses(t, filepath.Join(root, rel), rel)
	}
}

func checkRoadmapStatuses(t *testing.T, path, rel string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	lines := strings.Split(string(body), "\n")
	for i, line := range lines {
		if !roadmapHeadRe.MatchString(line) {
			continue
		}
		if status, ok := statusForSection(lines[i+1:]); !ok {
			t.Errorf("%s: %q has no `**Status:**` line before the next section", rel, strings.TrimSpace(line))
		} else if !roadmapStatuses[status] {
			t.Errorf("%s: %q Status %q is not open, in-progress, candidate-proof-remains, or done", rel, strings.TrimSpace(line), status)
		}
	}
}

// statusForSection returns the Status value from the lines that follow a priority
// header, stopping at the next header so one section cannot borrow another's.
func statusForSection(after []string) (string, bool) {
	for _, line := range after {
		if strings.HasPrefix(line, "### ") || strings.HasPrefix(line, "## ") {
			return "", false
		}
		if m := statusLineRe.FindStringSubmatch(line); m != nil {
			return m[1], true
		}
	}
	return "", false
}

// frontmatter holds one task's top-level frontmatter values. A scalar keeps its
// trimmed text; a key with no value (an unclaimed claim, an inline `[]`) maps to
// the empty string but is still present.
type frontmatter map[string]string

// list returns the entries of a key written either inline (`[a, b]`) or as an
// indented block list beneath the key. block-list items are collected by
// parseFrontmatter into the value joined by commas.
func (f frontmatter) list(key string) []string {
	raw := strings.TrimSpace(f[key])
	raw = strings.TrimPrefix(raw, "[")
	raw = strings.TrimSuffix(raw, "]")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for part := range strings.SplitSeq(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseFrontmatter reads the leading `---` block. It handles the flat scalars and
// the two list spellings the template uses (inline `[...]` and an indented block
// list); it is not a general YAML parser and does not need to be.
func parseFrontmatter(doc string) (frontmatter, bool) {
	lines := strings.Split(doc, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, false
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return nil, false
	}
	fields := frontmatter{}
	var lastKey string
	for _, line := range lines[1:end] {
		if strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t") {
			// A block-list item under the previous key. Fold it into the value so
			// list() sees the same comma-joined shape as an inline list.
			item := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
			if lastKey != "" && item != "" {
				if fields[lastKey] == "" {
					fields[lastKey] = item
				} else {
					fields[lastKey] += ", " + item
				}
			}
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		// Strip a trailing "# comment" the template carries on its scalars.
		value = strings.TrimSpace(value)
		if i := strings.Index(value, " #"); i >= 0 {
			value = strings.TrimSpace(value[:i])
		}
		fields[key] = value
		lastKey = key
	}
	return fields, true
}
