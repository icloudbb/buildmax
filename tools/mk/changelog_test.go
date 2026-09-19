package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// changelogWithLinks is the shape the fold rewrites: an unreleased section, one
// released section, and the reference definitions at the foot.
const changelogWithLinks = `# Changelog

## [Unreleased]

Entries live under docs/changelog/.

## [0.2.0] - 2026-08-24

### Fixed

- Something else.

[Unreleased]: https://example.invalid/o/r/compare/v0.2.0...HEAD
[0.2.0]: https://example.invalid/o/r/compare/v0.1.0...v0.2.0
`

func writeEntry(t *testing.T, category, slug, body string) {
	t.Helper()
	dir := filepath.Join(changelogDir, category)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, slug+".md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", slug, err)
	}
}

func TestReleaseChangelogMovesTheCompareLinks(t *testing.T) {
	writeChangelog(t, changelogWithLinks)
	writeEntry(t, "added", "a-flag", "- A new flag.\n")

	if err := releaseChangelog("v0.3.0"); err != nil {
		t.Fatalf("releaseChangelog: %v", err)
	}

	raw, err := os.ReadFile(changelogFile)
	if err != nil {
		t.Fatalf("read %s: %v", changelogFile, err)
	}
	body := string(raw)
	for _, want := range []string{
		"[Unreleased]: https://example.invalid/o/r/compare/v0.3.0...HEAD\n",
		"[0.3.0]: https://example.invalid/o/r/compare/v0.2.0...v0.3.0\n",
		"[0.2.0]: https://example.invalid/o/r/compare/v0.1.0...v0.2.0\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("CHANGELOG.md is missing %q:\n%s", want, body)
		}
	}
	// The new link belongs directly under [Unreleased], above the older ones.
	if strings.Index(body, "[0.3.0]:") > strings.Index(body, "[0.2.0]:") {
		t.Errorf("the version links are out of order:\n%s", body)
	}
}

// writeZhEntry writes a simplified-Chinese mirror entry, the shape the fold
// must also empty.
func writeZhEntry(t *testing.T, category, slug, body string) {
	t.Helper()
	dir := filepath.Join(zhChangelogDir, category)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, slug+".md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", slug, err)
	}
}

// A release folds the English entry without its mirror link, and empties the
// simplified-Chinese mirror directory alongside the English one -- so the
// released CHANGELOG.md carries no link into a directory the release just
// emptied, and no mirror entry is left pointing at a deleted English original.
func TestReleaseChangelogDropsTheMirrorAndItsLink(t *testing.T) {
	writeChangelog(t, changelogWithLinks)
	writeEntry(t, "added", "a-flag",
		"- A new flag.\n\n  > **简体中文：** [阅读中文镜像](../../zh-CN/changelog/added/a-flag.md)\n")
	writeZhEntry(t, "added", "a-flag",
		"> **翻译说明：** 本文是[英文原文](../../../changelog/added/a-flag.md)的简体中文派生翻译。\n\n- 一个新开关。\n")

	if err := releaseChangelog("v0.3.0"); err != nil {
		t.Fatalf("releaseChangelog: %v", err)
	}

	raw, err := os.ReadFile(changelogFile)
	if err != nil {
		t.Fatalf("read %s: %v", changelogFile, err)
	}
	body := string(raw)
	if !strings.Contains(body, "- A new flag.") {
		t.Errorf("the entry text is missing:\n%s", body)
	}
	if strings.Contains(body, "zh-CN/changelog") {
		t.Errorf("CHANGELOG.md kept a link into the emptied mirror directory:\n%s", body)
	}

	if _, err := os.Stat(filepath.Join(zhChangelogDir, "added", "a-flag.md")); !os.IsNotExist(err) {
		t.Errorf("the mirror entry should be removed by the fold; stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(changelogDir, "added", "a-flag.md")); !os.IsNotExist(err) {
		t.Errorf("the English entry should be removed by the fold; stat err = %v", err)
	}
}

func TestReleaseChangelogRefusesAVersionItAlreadyLinks(t *testing.T) {
	writeChangelog(t, changelogWithLinks)
	writeEntry(t, "added", "a-flag", "- A new flag.\n")

	err := releaseChangelog("0.2.0")
	if err == nil || !strings.Contains(err.Error(), "released already") {
		t.Fatalf("expected a released-already error, got %v", err)
	}
	// A refused fold leaves the entries where they were.
	if _, err := os.Stat(filepath.Join(changelogDir, "added", "a-flag.md")); err != nil {
		t.Errorf("the entry should survive a refused fold: %v", err)
	}
}

func TestReleaseChangelogRefusesAFileWithNoUnreleasedLink(t *testing.T) {
	writeChangelog(t, strings.ReplaceAll(changelogWithLinks,
		"[Unreleased]: https://example.invalid/o/r/compare/v0.2.0...HEAD\n", ""))
	writeEntry(t, "added", "a-flag", "- A new flag.\n")

	err := releaseChangelog("0.3.0")
	if err == nil || !strings.Contains(err.Error(), "[Unreleased]") {
		t.Fatalf("expected a missing-link error, got %v", err)
	}
}
