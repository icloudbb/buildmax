package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The board is a derived, read-only status view. It holds no state of its own:
// the backlog frontmatter, the Roadmap `Status:` lines, the design index progress
// column, the open proposal files, the unreleased changelog, and the git history
// are the single sources of truth, so the view cannot drift from them. The format contract that keeps those docs parseable is enforced by
// TestBacklogFrontmatterIsValid and TestRoadmapPrioritiesCarryStatus, not here —
// this command renders what it finds and degrades gracefully when a field or the
// git binary is missing, rather than gating.
//
// The frontmatter parser below is a near-duplicate of the one in
// internal/architecture/backlog_test.go. They are declared twice rather than
// shared because a package under internal/ that tools/mk imported would pull the
// task runner into the application's dependency graph — the same reason
// changelog_test.go mirrors its categories.

var (
	boardTaskFileRe    = regexp.MustCompile(`^\d+-[a-z0-9-]+\.md$`)
	boardRoadmapHeadRe = regexp.MustCompile(`^### (R\d+)\. (.+)$`)
	boardStatusLineRe  = regexp.MustCompile(`^\*\*Status:\*\* (\S+)`)
	boardMergeRe       = regexp.MustCompile(`^Merge pull request #(\d+) from \S+/(\S+)$`)
	// A design-record row in docs/design/README.md: a title linking to a `.md`
	// record, its lifecycle, then its progress. The `.md` link in the first cell
	// is what tells these rows apart from the domain-browse table, whose first
	// cell links to a `#anchor` instead.
	boardDesignRowRe = regexp.MustCompile(`^\|\s*\[([^\]]+)\]\(([^)]+\.md)\)\s*\|([^|]*)\|([^|]*)\|`)
)

// boardDesignDone marks the progress values that need no further implementation,
// so the board omits them. Everything else — Partial, Not started, Conditional,
// and Decision only — counts as unfinished. The full progress vocabulary lives
// in docs/design/README.md.
var boardDesignDone = map[string]bool{"Complete": true, "Superseded in part": true}

type boardTask struct {
	file      string // NN-slug.md
	prefix    string // NN
	title     string
	roadmap   string
	claim     string
	pr        string
	dependsOn []string
}

func cmdBoard(args []string) error {
	asMarkdown := false
	for _, a := range args {
		switch a {
		case "--md", "--markdown":
			asMarkdown = true
		default:
			return usageErrorf("board", "board does not take %q", a)
		}
	}

	tasks, err := loadBoardTasks()
	if err != nil {
		return err
	}
	roadmap := loadRoadmapStatuses()
	designs := loadUnfinishedDesigns()
	proposals := loadOpenProposals()
	done := loadRecentlyDone()

	if asMarkdown {
		printBoardMarkdown(tasks, roadmap, designs, proposals, done)
	} else {
		printBoardText(tasks, roadmap, designs, proposals, done)
	}
	return nil
}

// loadBoardTasks reads every live task in docs/backlog in queue order (the
// directory sorted by NN prefix, which os.ReadDir already returns).
func loadBoardTasks() ([]boardTask, error) {
	dir := filepath.Join("docs", "backlog")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var tasks []boardTask
	for _, e := range entries {
		if e.IsDir() || !boardTaskFileRe.MatchString(e.Name()) {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		fields := parseBoardFrontmatter(string(body))
		tasks = append(tasks, boardTask{
			file:      e.Name(),
			prefix:    strings.SplitN(e.Name(), "-", 2)[0],
			title:     boardTitle(fields, e.Name()),
			roadmap:   orNone(fields["roadmap"]),
			claim:     fields["claim"],
			pr:        strings.TrimPrefix(fields["pr"], "#"),
			dependsOn: boardList(fields["depends_on"]),
		})
	}
	return tasks, nil
}

func boardTitle(fields map[string]string, file string) string {
	if t := strings.TrimSpace(fields["title"]); t != "" {
		return t
	}
	return strings.TrimSuffix(file, ".md")
}

func orNone(v string) string {
	if v == "" {
		return "none"
	}
	return v
}

// blockers returns the depends_on entries whose task file still exists, meaning
// the dependency has not merged yet. An empty result means the task is ready.
func (t boardTask) blockers(live map[string]bool) []string {
	var out []string
	for _, dep := range t.dependsOn {
		if live[dep] {
			out = append(out, dep)
		}
	}
	return out
}

type roadmapPriority struct {
	id     string // R0..R5
	status string
	title  string
}

func loadRoadmapStatuses() []roadmapPriority {
	body, err := os.ReadFile(filepath.Join("docs", "ROADMAP.md"))
	if err != nil {
		return nil
	}
	lines := strings.Split(string(body), "\n")
	var out []roadmapPriority
	for i, line := range lines {
		m := boardRoadmapHeadRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		out = append(out, roadmapPriority{id: m[1], title: m[2], status: statusAfter(lines[i+1:])})
	}
	return out
}

func statusAfter(after []string) string {
	for _, line := range after {
		if strings.HasPrefix(line, "### ") || strings.HasPrefix(line, "## ") {
			return "?"
		}
		if m := boardStatusLineRe.FindStringSubmatch(line); m != nil {
			return m[1]
		}
	}
	return "?"
}

// designRecord is one unfinished design, read from the docs/design/README.md
// progress snapshot the maintainer keeps aligned with the code and roadmap.
type designRecord struct {
	title    string
	progress string
}

// loadUnfinishedDesigns parses the design index tables and keeps the records
// whose progress is not yet complete. The README progress column is the
// authoritative snapshot; the free-text Status line in each record is prose and
// does not classify into that vocabulary.
func loadUnfinishedDesigns() (out []designRecord) {
	body, err := os.ReadFile(filepath.Join("docs", "design", "README.md"))
	if err != nil {
		return nil
	}
	for line := range strings.SplitSeq(string(body), "\n") {
		m := boardDesignRowRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		progress := strings.TrimSpace(m[4])
		if progress == "" || boardDesignDone[progress] {
			continue
		}
		out = append(out, designRecord{title: strings.TrimSpace(m[1]), progress: progress})
	}
	return out
}

// loadOpenProposals lists every live proposal. A proposal file exists only while
// its direction is open — it is deleted when accepted, rejected, or superseded —
// so the directory itself is the source of truth, not a hand-kept index. The
// title comes from each file's first heading.
func loadOpenProposals() (out []string) {
	dir := filepath.Join("docs", "proposals")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.IsDir() || e.Name() == "README.md" || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, firstHeading(string(body), e.Name()))
	}
	return out
}

// firstHeading returns the text of the first Markdown H1, falling back to the
// filename so a proposal missing its heading still appears.
func firstHeading(body, file string) string {
	for line := range strings.SplitSeq(body, "\n") {
		if h, ok := strings.CutPrefix(line, "# "); ok {
			return strings.TrimSpace(h)
		}
	}
	return strings.TrimSuffix(file, ".md")
}

type doneEntry struct {
	tag  string // changelog category, or "#NNN"
	text string
}

// loadRecentlyDone gathers both signals the board reports as done: the unreleased
// changelog entries (user-visible work, one file each) and the recent first-parent
// git history (every merge, including internal changes a changelog omits).
func loadRecentlyDone() (out []doneEntry) {
	for _, category := range changelogCategories {
		matches, _ := filepath.Glob(filepath.Join(changelogDir, category, "*.md"))
		for _, path := range matches {
			if body, err := os.ReadFile(path); err == nil {
				out = append(out, doneEntry{tag: category, text: firstListItem(string(body))})
			}
		}
	}
	if have("git") {
		if log, err := capture("git", "log", "--first-parent", "-n", "8", "--pretty=format:%s"); err == nil && log != "" {
			for line := range strings.SplitSeq(log, "\n") {
				tag, text := "", strings.TrimSpace(line)
				if m := boardMergeRe.FindStringSubmatch(text); m != nil {
					tag, text = "#"+m[1], strings.ReplaceAll(m[2], "-", " ")
				}
				out = append(out, doneEntry{tag: tag, text: text})
			}
		}
	}
	return out
}

// firstListItem joins the first Markdown list item — its `- ` line plus any
// two-space continuation lines a changelog entry wraps onto — into one line,
// then trims it to a width the board can align.
func firstListItem(body string) string {
	var item []string
	for line := range strings.SplitSeq(strings.TrimSpace(body), "\n") {
		switch {
		case len(item) == 0 && strings.HasPrefix(line, "- "):
			item = append(item, strings.TrimPrefix(line, "- "))
		case len(item) > 0 && strings.HasPrefix(line, "  "):
			item = append(item, strings.TrimSpace(line))
		case len(item) > 0:
			// A blank line or a second item ends the first one.
			return truncate(strings.Join(item, " "), 72)
		}
	}
	return truncate(strings.Join(item, " "), 72)
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max-1])) + "…"
}

// parseBoardFrontmatter reads the leading `---` block into a flat map, folding an
// indented block list into the same comma-joined shape as an inline `[...]` list.
func parseBoardFrontmatter(doc string) map[string]string {
	fields := map[string]string{}
	lines := strings.Split(doc, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return fields
	}
	var lastKey string
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			break
		}
		if strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t") {
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
		value = strings.TrimSpace(value)
		if i := strings.Index(value, " #"); i >= 0 {
			value = strings.TrimSpace(value[:i])
		}
		fields[key] = value
		lastKey = key
	}
	return fields
}

func boardList(raw string) []string {
	raw = strings.TrimSpace(raw)
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

// boardBuckets splits tasks into the four status columns. pr wins over claim so a
// task in review is not double-counted as still being written; an unclaimed task
// is ready only when none of its dependencies remain in the queue.
func boardBuckets(tasks []boardTask) (inProgress, inReview, ready, blocked []boardTask) {
	live := map[string]bool{}
	for _, t := range tasks {
		live[t.file] = true
	}
	for _, t := range tasks {
		switch {
		case t.pr != "":
			inReview = append(inReview, t)
		case t.claim != "":
			inProgress = append(inProgress, t)
		case len(t.blockers(live)) > 0:
			blocked = append(blocked, t)
		default:
			ready = append(ready, t)
		}
	}
	return
}

func (t boardTask) note(live map[string]bool) string {
	switch {
	case t.pr != "":
		return "PR #" + t.pr
	case t.claim != "":
		return "@ " + t.claim
	default:
		if b := t.blockers(live); len(b) > 0 {
			return "blocked by " + strings.Join(b, ", ")
		}
	}
	return ""
}

func printBoardText(tasks []boardTask, roadmap []roadmapPriority, designs []designRecord, proposals []string, done []doneEntry) {
	live := map[string]bool{}
	for _, t := range tasks {
		live[t.file] = true
	}
	inProgress, inReview, ready, blocked := boardBuckets(tasks)

	fmt.Println("BuildMax board — derived from docs/backlog, docs/ROADMAP.md, docs/design, docs/proposals, and git")
	fmt.Println()
	fmt.Println("Backlog")
	printTaskColumn(live, "In progress", inProgress)
	printTaskColumn(live, "In review", inReview)
	printTaskColumn(live, "Ready", ready)
	printTaskColumn(live, "Blocked", blocked)

	fmt.Println()
	fmt.Println("Roadmap")
	if len(roadmap) == 0 {
		fmt.Println("  (docs/ROADMAP.md not found)")
	}
	for _, p := range roadmap {
		fmt.Printf("  %-3s %-24s %s\n", p.id, p.status, p.title)
	}

	fmt.Println()
	fmt.Printf("Designs (unfinished) (%d)\n", len(designs))
	for _, d := range designs {
		fmt.Printf("  %-14s %s\n", d.progress, d.title)
	}

	fmt.Println()
	fmt.Printf("Proposals (open) (%d)\n", len(proposals))
	for _, p := range proposals {
		fmt.Printf("  %s\n", p)
	}

	fmt.Println()
	fmt.Println("Recently done")
	if len(done) == 0 {
		fmt.Println("  (nothing recorded)")
	}
	for _, d := range done {
		fmt.Printf("  %-12s %s\n", d.tag, d.text)
	}
}

func printTaskColumn(live map[string]bool, heading string, tasks []boardTask) {
	fmt.Printf("  %s (%d)\n", heading, len(tasks))
	for _, t := range tasks {
		line := fmt.Sprintf("    %-3s %-5s %s", t.prefix, t.roadmap, t.title)
		if note := t.note(live); note != "" {
			line += "  — " + note
		}
		fmt.Println(line)
	}
}

func printBoardMarkdown(tasks []boardTask, roadmap []roadmapPriority, designs []designRecord, proposals []string, done []doneEntry) {
	live := map[string]bool{}
	for _, t := range tasks {
		live[t.file] = true
	}
	inProgress, inReview, ready, blocked := boardBuckets(tasks)

	fmt.Println("# BuildMax Board")
	fmt.Println()
	fmt.Println("Derived from `docs/backlog/`, `docs/ROADMAP.md`, `docs/design/`, `docs/proposals/`, and git. Do not edit; run `./make board --md`.")
	fmt.Println()
	fmt.Println("## Backlog")
	printTaskColumnMarkdown(live, "In progress", inProgress)
	printTaskColumnMarkdown(live, "In review", inReview)
	printTaskColumnMarkdown(live, "Ready", ready)
	printTaskColumnMarkdown(live, "Blocked", blocked)

	fmt.Println()
	fmt.Println("## Roadmap")
	fmt.Println()
	for _, p := range roadmap {
		fmt.Printf("- **%s** `%s` — %s\n", p.id, p.status, p.title)
	}

	fmt.Println()
	fmt.Printf("## Designs (unfinished) (%d)\n", len(designs))
	fmt.Println()
	for _, d := range designs {
		fmt.Printf("- `%s` %s\n", d.progress, d.title)
	}

	fmt.Println()
	fmt.Printf("## Proposals (open) (%d)\n", len(proposals))
	fmt.Println()
	for _, p := range proposals {
		fmt.Printf("- %s\n", p)
	}

	fmt.Println()
	fmt.Println("## Recently done")
	fmt.Println()
	for _, d := range done {
		if d.tag != "" {
			fmt.Printf("- `%s` %s\n", d.tag, d.text)
		} else {
			fmt.Printf("- %s\n", d.text)
		}
	}
}

func printTaskColumnMarkdown(live map[string]bool, heading string, tasks []boardTask) {
	fmt.Println()
	fmt.Printf("### %s (%d)\n", heading, len(tasks))
	fmt.Println()
	for _, t := range tasks {
		line := fmt.Sprintf("- `%s` %s **%s**", t.prefix, t.roadmap, t.title)
		if note := t.note(live); note != "" {
			line += " — " + note
		}
		fmt.Println(line)
	}
}
