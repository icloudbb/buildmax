package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestBoardBucketsClassifyByFrontmatter(t *testing.T) {
	tasks := []boardTask{
		{file: "10-done-dep.md"},                                      // stands in as a merged dep: absent from the queue below
		{file: "20-writing.md", claim: "alice 2026-09-13"},            // claimed, no PR -> in progress
		{file: "30-review.md", claim: "bob 2026-09-13", pr: "123"},    // PR set -> in review, even while claimed
		{file: "40-ready.md", dependsOn: []string{"99-merged.md"}},    // dep already merged (absent) -> ready
		{file: "50-blocked.md", dependsOn: []string{"20-writing.md"}}, // dep still in queue -> blocked
	}

	inProgress, inReview, ready, blocked := boardBuckets(tasks)

	assertFiles(t, "in progress", inProgress, []string{"20-writing.md"})
	assertFiles(t, "in review", inReview, []string{"30-review.md"})
	assertFiles(t, "ready", ready, []string{"10-done-dep.md", "40-ready.md"})
	assertFiles(t, "blocked", blocked, []string{"50-blocked.md"})
}

func assertFiles(t *testing.T, bucket string, got []boardTask, want []string) {
	t.Helper()
	var names []string
	for _, task := range got {
		names = append(names, task.file)
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("%s bucket = %v, want %v", bucket, names, want)
	}
}

func TestBoardParseFrontmatter(t *testing.T) {
	doc := `---
id: sample-task
title: Do the thing
roadmap: R1
depends_on: [20-other.md]
verification:
  - ./make test
  - ./make check docs
claim: alice 2026-09-13
pr: #123
---

## Outcome
Body text, not frontmatter.
`
	fields := parseBoardFrontmatter(doc)

	if got := fields["title"]; got != "Do the thing" {
		t.Errorf("title = %q", got)
	}
	if got := fields["roadmap"]; got != "R1" {
		t.Errorf("roadmap = %q", got)
	}
	if got := fields["claim"]; got != "alice 2026-09-13" {
		t.Errorf("claim = %q", got)
	}
	// A block list folds to the same comma-joined shape as an inline list.
	if got := boardList(fields["verification"]); !reflect.DeepEqual(got, []string{"./make test", "./make check docs"}) {
		t.Errorf("verification = %v", got)
	}
	if got := boardList(fields["depends_on"]); !reflect.DeepEqual(got, []string{"20-other.md"}) {
		t.Errorf("depends_on = %v", got)
	}
}

func TestBoardFirstListItemJoinsWrappedLines(t *testing.T) {
	body := "- A change that wraps across\n  two lines in the source.\n"
	if got := firstListItem(body); got != "A change that wraps across two lines in the source." {
		t.Errorf("firstListItem = %q", got)
	}
}

func TestBoardDesignRowKeepsUnfinishedOnly(t *testing.T) {
	// The design-row regex must skip the domain-browse table (a `#anchor` link),
	// the header and separator rows, and every record whose progress is done.
	rows := []struct {
		line     string
		progress string // "" means the row should not match at all
	}{
		{"| [Product and Execution Model](#product-and-execution-model) | [Product vision](product-vision.md) | Covers |", ""},
		{"| Document | Lifecycle | Progress | Covers |", ""},
		{"|---|---|---|---|", ""},
		{"| [Product vision](product-vision.md) | Direction | Decision only | Long-range model |", "Decision only"},
		{"| [Managed LLM gateway](llm-gateway.md) | Active plan | Partial | Managed inference |", "Partial"},
		{"| [Structured output](structured-output.md) | Active plan | Not started | Schema results |", "Not started"},
		{"| [gVisor worker runtime](gvisor-worker-runtime.md) | Direction | Conditional | Pod isolation |", "Conditional"},
		{"| [Scheduled Agent execution](scheduled-agent-execution.md) | Specification | Complete | Recurring runs |", ""},
		{"| [Portal execution model](portal-execution-model.md) | Specification | Superseded in part | Outcome |", ""},
	}
	var got []string
	for _, r := range rows {
		m := boardDesignRowRe.FindStringSubmatch(r.line)
		matched := m != nil && strings.TrimSpace(m[4]) != ""
		wantMatch := r.progress != ""
		if matched && boardDesignDone[strings.TrimSpace(m[4])] {
			matched = false // done rows are dropped by loadUnfinishedDesigns
		}
		if matched != wantMatch {
			t.Errorf("line %q matched=%v, want %v", r.line, matched, wantMatch)
		}
		if matched {
			if p := strings.TrimSpace(m[4]); p != r.progress {
				t.Errorf("line %q progress = %q, want %q", r.line, p, r.progress)
			}
			got = append(got, strings.TrimSpace(m[1]))
		}
	}
	want := []string{"Product vision", "Managed LLM gateway", "Structured output", "gVisor worker runtime"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unfinished titles = %v, want %v", got, want)
	}
}

func TestBoardFirstHeadingFallsBackToFilename(t *testing.T) {
	if got := firstHeading("Intro line\n\n# The Real Title\n\nbody\n", "x.md"); got != "The Real Title" {
		t.Errorf("firstHeading = %q", got)
	}
	if got := firstHeading("no heading here\n", "durable-agent-sessions.md"); got != "durable-agent-sessions" {
		t.Errorf("firstHeading fallback = %q", got)
	}
}
