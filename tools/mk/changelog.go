package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

// changelogDir holds one unreleased entry per file. See its README for why the
// entries are files rather than lines in one list.
const changelogDir = "docs/changelog"

// changelogFile is the released history the fragments fold into, and the source
// a release body quotes.
const changelogFile = "CHANGELOG.md"

// zhChangelogDir mirrors changelogDir in simplified Chinese: one translated
// entry per file, each cross-linked to its English original. A release empties
// it alongside changelogDir (see the directory READMEs). CHANGELOG.md is the
// English released history; the translations are not folded into a released
// file, so the fold discards the mirror rather than keeping dangling links to
// an emptied directory.
const zhChangelogDir = "docs/zh-CN/changelog"

// changelogCategories are the headings a release section uses, in the order
// they appear in it. A directory outside this set is a typo rather than a new
// category, and is reported as one.
var changelogCategories = []string{"added", "changed", "fixed", "security"}

func cmdChangelog(args []string) error {
	if len(args) > 0 && args[0] == "release" {
		if len(args) != 2 {
			return usageErrorf("changelog", "changelog release needs a version")
		}
		return releaseChangelog(args[1])
	}
	if len(args) > 0 && args[0] == "new" {
		if len(args) != 3 {
			return usageErrorf("changelog", "changelog new needs a category and a slug")
		}
		return newChangelogEntry(args[1], args[2])
	}
	if len(args) > 0 {
		return usageErrorf("changelog", "unknown changelog action: %s", args[0])
	}
	section, count, err := unreleasedSection()
	if err != nil {
		return err
	}
	if count == 0 {
		fmt.Println("No unreleased entries.")
		return nil
	}
	fmt.Print(section)
	fmt.Fprintf(os.Stderr, "\n%d entries in %s\n", count, changelogDir)
	return nil
}

// newChangelogEntry writes an empty entry at the right path with the shape a
// release expects.
//
// Forgetting the entry is one of the two most common first-contribution review
// comments, and the command could only ever preview entries that already
// existed: the category directories, the "one Markdown list item" rule, and the
// wrapping were all things to look up first. A test rejects a malformed entry,
// but only after it is written.
func newChangelogEntry(category, slug string) error {
	if !slices.Contains(changelogCategories, category) {
		return fmt.Errorf("%q is not a changelog category; expected one of %s", category, strings.Join(changelogCategories, ", "))
	}
	if slug == "" || strings.ContainsAny(slug, `/\ `) || strings.HasPrefix(slug, ".") {
		return fmt.Errorf("%q is not a usable slug; use a short kebab-case name for the change, like request-id-header", slug)
	}
	slug = strings.TrimSuffix(slug, ".md")

	dir := filepath.Join(changelogDir, category)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	path := filepath.Join(dir, slug+".md")
	// O_EXCL: two branches choosing the same filename are describing the same
	// change, and silently overwriting the other one's text would lose it.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%s already exists; edit it, or choose a slug for a different change", path)
		}
		return err
	}
	const template = "- TODO: one sentence a user or operator would recognise, wrapped at 80\n" +
		"  columns with continuation lines indented two spaces.\n"
	if _, err := file.WriteString(template); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	fmt.Printf("Created %s. Replace the TODO line with the entry.\n", path)
	return nil
}

// unreleasedSection renders the fragments as they will appear under a version
// heading, and reports how many there were.
func unreleasedSection() (string, int, error) {
	known := map[string]bool{}
	for _, c := range changelogCategories {
		known[c] = true
	}
	entries, err := os.ReadDir(changelogDir)
	if err != nil {
		return "", 0, fmt.Errorf("read %s: %w", changelogDir, err)
	}
	for _, e := range entries {
		if e.IsDir() && !known[e.Name()] {
			return "", 0, fmt.Errorf("%s/%s is not a changelog category; expected one of %s",
				changelogDir, e.Name(), strings.Join(changelogCategories, ", "))
		}
	}

	var b strings.Builder
	total := 0
	for _, category := range changelogCategories {
		files, err := filepath.Glob(filepath.Join(changelogDir, category, "*.md"))
		if err != nil {
			return "", 0, err
		}
		sort.Strings(files)
		if len(files) == 0 {
			continue
		}
		fmt.Fprintf(&b, "### %s\n\n", strings.ToUpper(category[:1])+category[1:])
		for _, f := range files {
			body, err := os.ReadFile(f)
			if err != nil {
				return "", 0, fmt.Errorf("read %s: %w", f, err)
			}
			text := strings.TrimRight(string(body), "\n")
			if !strings.HasPrefix(text, "- ") {
				return "", 0, fmt.Errorf("%s does not start with \"- \"; an entry is one Markdown list item", f)
			}
			b.WriteString(stripMirrorLink(text))
			b.WriteString("\n\n")
			total++
		}
	}
	return b.String(), total, nil
}

// stripMirrorLink removes an entry's trailing link to its simplified-Chinese
// mirror. An English fragment may end with a blockquote pointing at its mirror
// under docs/zh-CN/changelog; that link is valid from the fragment but not from
// CHANGELOG.md, and the release empties the directory it points at, so the fold
// drops it. An entry without the trailer is returned unchanged.
func stripMirrorLink(text string) string {
	lines := strings.Split(text, "\n")
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	if end > 0 && isMirrorLinkLine(lines[end-1]) {
		end--
		for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
			end--
		}
	}
	return strings.Join(lines[:end], "\n")
}

func isMirrorLinkLine(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, ">") && strings.Contains(t, "zh-CN/changelog/")
}

// releaseChangelog folds the fragments into CHANGELOG.md under version and
// today's date, then removes the files it folded in -- both the English
// entries under changelogDir and their simplified-Chinese mirrors under
// zhChangelogDir, so neither directory outlives the release with entries the
// history has already absorbed.
//
// The files are deleted only after the rewrite succeeds, so a failure leaves
// the entries where they were rather than half-moved.
func releaseChangelog(version string) error {
	version = strings.TrimPrefix(version, "v")
	section, count, err := unreleasedSection()
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("no entries in %s; nothing to release", changelogDir)
	}

	raw, err := os.ReadFile(changelogFile)
	if err != nil {
		return fmt.Errorf("read %s: %w", changelogFile, err)
	}
	body := string(raw)
	marker := "## [Unreleased]"
	at := strings.Index(body, marker)
	if at < 0 {
		return fmt.Errorf("%s has no %q heading", changelogFile, marker)
	}
	next := strings.Index(body[at+len(marker):], "\n## ")
	if next < 0 {
		return fmt.Errorf("%s has no released section after %q", changelogFile, marker)
	}
	cut := at + len(marker) + next + 1

	heading := fmt.Sprintf("## [%s] - %s\n\n", version, time.Now().Format("2006-01-02"))
	updated := body[:cut] + heading + section + body[cut:]
	updated, err = withVersionLink(updated, version)
	if err != nil {
		return err
	}
	if err := os.WriteFile(changelogFile, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", changelogFile, err)
	}

	for _, dir := range []string{changelogDir, zhChangelogDir} {
		for _, category := range changelogCategories {
			files, _ := filepath.Glob(filepath.Join(dir, category, "*.md"))
			for _, f := range files {
				if err := os.Remove(f); err != nil {
					return fmt.Errorf("remove %s: %w", f, err)
				}
			}
		}
	}
	fmt.Printf("Folded %d entries into %s as %s, and moved the compare links onto it.\n", count, changelogFile, version)
	return nil
}

// withVersionLink moves the reference definitions at the foot of the file onto
// the new version: `[Unreleased]` compares from it to HEAD, and it compares
// from whatever `[Unreleased]` named before.
//
// The fold used to leave this to whoever cut the release, so every version's
// links were added by hand afterwards -- and 0.2.0-alpha.5, prepared by the
// scheduled workflow with nobody editing the file, reached its release pull
// request with a dangling `[0.2.0-alpha.5]` reference. The compare range is the
// one thing here that is derivable rather than judged, so it is not a
// maintainer's job.
//
// The previous version is read out of the existing link rather than out of git:
// the fold's subject is the file, and a tag that does not exist yet cannot say
// what the last released version was.
func withVersionLink(body, version string) (string, error) {
	const unreleased = "[Unreleased]: "
	const head = "...HEAD"
	const compare = "/compare/"

	if strings.Contains(body, "\n["+version+"]: ") {
		return "", fmt.Errorf("%s already links %s; it has been released already", changelogFile, version)
	}
	start := strings.Index(body, "\n"+unreleased)
	if start < 0 {
		return "", fmt.Errorf("%s has no %q link definition to move onto %s", changelogFile, "[Unreleased]", version)
	}
	start++
	end := len(body)
	if nl := strings.IndexByte(body[start:], '\n'); nl >= 0 {
		end = start + nl
	}

	url := body[start+len(unreleased) : end]
	at := strings.LastIndex(url, compare)
	if at < 0 || !strings.HasSuffix(url, head) {
		return "", fmt.Errorf("%s links [Unreleased] to %q, which is not a %s<previous>%s comparison", changelogFile, url, compare, head)
	}
	base, previous := url[:at], url[at+len(compare):len(url)-len(head)]

	moved := fmt.Sprintf("%s%s%sv%s%s\n[%s]: %s%s%s...v%s",
		unreleased, base, compare, version, head,
		version, base, compare, previous, version)
	return body[:start] + moved + body[end:], nil
}
