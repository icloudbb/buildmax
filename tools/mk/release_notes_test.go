package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func writeChangelog(t *testing.T, body string) {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := os.WriteFile(changelogFile, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", changelogFile, err)
	}
}

func TestChangelogSectionSplitsSummaryFromDetails(t *testing.T) {
	writeChangelog(t, `# Changelog

## [Unreleased]

Nothing yet.

## [0.3.0] - 2026-09-01

### Highlights

- The headline change.

### Upgrade notes

- Recreate the database.

### Added

- A new flag.

### Fixed

- An old bug.

## [0.2.0] - 2026-08-24

### Fixed

- Something else.

[0.3.0]: https://example.invalid/compare/v0.2.0...v0.3.0
`)

	summary, details, err := changelogSection("0.3.0")
	if err != nil {
		t.Fatalf("changelogSection: %v", err)
	}
	// Headings rise a level: the release title already names the version, so
	// nothing in the body sits under a `##` version heading.
	if !strings.HasPrefix(summary, "## Highlights") || !strings.Contains(summary, "## Upgrade notes") {
		t.Errorf("summary = %q; want the highlights and upgrade notes at heading level two", summary)
	}
	if strings.Contains(summary, "Added") || strings.Contains(summary, "A new flag") {
		t.Errorf("summary carries the categorized lists: %q", summary)
	}
	if !strings.Contains(details, "## Added") || !strings.Contains(details, "## Fixed") {
		t.Errorf("details = %q; want the categorized lists", details)
	}
	// The next version's section, and the link definitions that belong to no
	// section at all, both stay out.
	for _, unwanted := range []string{"Something else", "0.2.0", "https://example.invalid"} {
		if strings.Contains(summary+details, unwanted) {
			t.Errorf("section leaked %q", unwanted)
		}
	}
}

func TestChangelogSectionWithoutHighlights(t *testing.T) {
	writeChangelog(t, `# Changelog

## [0.3.1] - 2026-09-02

### Fixed

- A patch.
`)

	summary, details, err := changelogSection("v0.3.1")
	if err != nil {
		t.Fatalf("changelogSection: %v", err)
	}
	if summary != "" {
		t.Errorf("summary = %q; want empty for a section that is only categorized lists", summary)
	}
	if !strings.Contains(details, "- A patch.") {
		t.Errorf("details = %q; want the fixed entry", details)
	}
}

// A tag whose section was never folded in must stop the release rather than
// publish a body that says nothing about the version.
func TestChangelogSectionRequiresTheVersion(t *testing.T) {
	writeChangelog(t, "# Changelog\n\n## [Unreleased]\n\nNothing yet.\n")

	if _, _, err := changelogSection("0.4.0"); err == nil {
		t.Fatal("a missing section was accepted")
	} else if !strings.Contains(err.Error(), "changelog release 0.4.0") {
		t.Errorf("error %q does not say how to fold the entries in", err)
	}
}

// The template and the changelog are edited independently, so render the real
// pair: a placeholder renamed on one side would otherwise only fail in CI, on
// a pushed tag, with the release half-built.
func TestReleaseNotesRenderTheReleasedVersion(t *testing.T) {
	t.Chdir("../..")

	notes, err := releaseNotes(latestReleasedVersion(t))
	if err != nil {
		t.Fatalf("releaseNotes: %v", err)
	}
	for _, want := range []string{"## Install", "go install github.com/icloudbb/buildmax/cmd/buildmax@v", "This is an alpha", "SECURITY.md"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes do not contain %q", want)
		}
	}
	if strings.Contains(notes, "{{") {
		t.Error("notes contain an unrendered template action")
	}
	// GitHub rejects a release body over 125,000 characters.
	if len(notes) > 125_000 {
		t.Errorf("notes are %d characters; GitHub accepts at most 125,000", len(notes))
	}
}

func TestReleaseNotesWriteToAFile(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	version := func() string {
		t.Chdir(root)
		return latestReleasedVersion(t)
	}()

	out := filepath.Join(t.TempDir(), "notes", "release-notes.md")
	if err := cmdReleaseNotes([]string{version, "-o", out}); err != nil {
		t.Fatalf("cmdReleaseNotes: %v", err)
	}
	if body, err := os.ReadFile(out); err != nil {
		t.Fatalf("read %s: %v", out, err)
	} else if len(body) == 0 {
		t.Error("wrote an empty release body")
	}

	if err := cmdReleaseNotes(nil); err == nil {
		t.Error("a missing version was accepted")
	}
	if err := cmdReleaseNotes([]string{version, "extra"}); err == nil {
		t.Error("a second version was accepted")
	}
}

// latestReleasedVersion reads the newest version heading from CHANGELOG.md, so
// the tests follow the file instead of pinning a version that ages out.
func latestReleasedVersion(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(changelogFile)
	if err != nil {
		t.Fatalf("read %s: %v", changelogFile, err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		rest, ok := strings.CutPrefix(line, "## [")
		if !ok || strings.HasPrefix(rest, "Unreleased]") {
			continue
		}
		version, _, _ := strings.Cut(rest, "]")
		return version
	}
	t.Fatalf("%s has no released version heading", changelogFile)
	return ""
}

// The composed body reaches GitHub through GoReleaser's changelog pipe:
// --release-notes is a file that pipe loads, not a flag the release step reads
// on its own. Disabling the pipe drops the file with no warning and publishes
// an empty body, which is what happened to v0.2.0-alpha.2, so pin the pair
// rather than either half.
func TestReleaseNotesFileReachesTheChangelogPipe(t *testing.T) {
	workflow, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	if !strings.Contains(string(workflow), "--release-notes=") {
		t.Error("release.yml does not pass --release-notes; the body would be GoReleaser's generated changelog")
	}

	raw, err := os.ReadFile("../../.goreleaser.yaml")
	if err != nil {
		t.Fatalf("read .goreleaser.yaml: %v", err)
	}
	var config struct {
		Changelog struct {
			Disable string `yaml:"disable"`
		} `yaml:"changelog"`
	}
	if err := yaml.Unmarshal(raw, &config); err != nil {
		t.Fatalf("parse .goreleaser.yaml: %v", err)
	}
	if config.Changelog.Disable == "true" {
		t.Error(".goreleaser.yaml disables the changelog pipe, which silently discards --release-notes")
	}
}

// Tags pushed with GITHUB_TOKEN do not recursively start tag workflows. Keep
// the explicit dispatch path and the manual recovery triggers paired, or an
// approved release would stop after creating an inert tag.
func TestPreparedReleaseCanDispatchBothPublishers(t *testing.T) {
	promote, err := os.ReadFile("../../.github/workflows/release-promote.yml")
	if err != nil {
		t.Fatalf("read release promotion workflow: %v", err)
	}
	for _, workflow := range []string{"release.yml", "portal-image.yml"} {
		if !strings.Contains(string(promote), "gh workflow run "+workflow) {
			t.Errorf("release promotion does not dispatch %s", workflow)
		}
		raw, readErr := os.ReadFile(filepath.Join("../../.github/workflows", workflow))
		if readErr != nil {
			t.Fatalf("read %s: %v", workflow, readErr)
		}
		if !strings.Contains(string(raw), "workflow_dispatch:") {
			t.Errorf("%s cannot be dispatched after the automated tag push", workflow)
		}
	}
}
