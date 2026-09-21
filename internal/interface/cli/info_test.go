package cli

import (
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/agentapp"
	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/icloudbb/buildmax/internal/core/session"
	"github.com/icloudbb/buildmax/internal/infra/trace"
)

func renderStats(s agentapp.SessionStats) string {
	var b strings.Builder
	writeStats(&b, s)
	return b.String()
}

// A session whose trace is gone has unavailable timings, not zero ones. The
// distinction is the whole reason the view reads two records instead of one.
func TestWriteStats_MissingTraceSaysUnavailableNotZero(t *testing.T) {
	out := renderStats(agentapp.SessionStats{
		ID:           "s1",
		Usage:        cllm.Usage{PromptTokens: 500, CompletionTokens: 20},
		Conversation: session.ConversationStats{UserMessages: 1, ToolCalls: 3},
	})
	if !strings.Contains(out, "no trace recorded") {
		t.Errorf("output does not say the timings are missing:\n%s", out)
	}
	if strings.Contains(out, "Time spent waiting") {
		t.Errorf("output reports a wall clock it never measured:\n%s", out)
	}
	if !strings.Contains(out, "No run trace was found") {
		t.Errorf("output does not warn that every timing is unavailable:\n%s", out)
	}
}

// A provider that reports no cache usage has not reported a miss, so no share
// is printed at all.
func TestWriteStats_UnreportedCacheIsNotPrintedAsZero(t *testing.T) {
	out := renderStats(agentapp.SessionStats{
		ID:    "s1",
		Usage: cllm.Usage{PromptTokens: 1000, CompletionTokens: 50},
	})
	if strings.Contains(out, "Cache (read/write)") {
		t.Errorf("output claims a cache result nobody measured:\n%s", out)
	}
	if strings.Contains(out, "served from cache") {
		t.Errorf("output prints a cache share with nothing to base it on:\n%s", out)
	}
}

// A run that only ever wrote cache entries paid more than it would have
// uncached. Calling that a small win is the false claim the cost path exists
// to avoid.
func TestWriteStats_NoSavingWhenCachingCostMore(t *testing.T) {
	out := renderStats(agentapp.SessionStats{
		ID: "s1",
		Cost: &cllm.Cost{
			Currency: "USD", Total: 900, Baseline: 700, CacheWrite: 400,
		},
	})
	if !strings.Contains(out, "paid more than it would have uncached") {
		t.Errorf("output does not say caching cost more here:\n%s", out)
	}
}

func TestWriteStats_UnpricedSessionSaysSoRatherThanShowingZero(t *testing.T) {
	out := renderStats(agentapp.SessionStats{ID: "s1"})
	if !strings.Contains(out, "not priced") {
		t.Errorf("output does not say the session was never priced:\n%s", out)
	}
	if strings.Contains(out, "0.000000") {
		t.Errorf("output shows a zero cost, which is a claim rather than a silence:\n%s", out)
	}
}

// An interrupted run is missing from the totals, and a reader must be told
// rather than shown a short session.
func TestWriteStats_IncompleteRunsAreCalledOut(t *testing.T) {
	out := renderStats(agentapp.SessionStats{
		ID:             "s1",
		CostIncomplete: true,
		Runs:           trace.SessionSummary{Runs: 2, Incomplete: 1, Failed: 1},
	})
	for _, want := range []string{"understates it", "killed or crashed", "ended with an error"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing the caveat %q:\n%s", want, out)
		}
	}
}

// Parallel tool execution can make summed tool time exceed the wall clock.
// Subtracting anyway would print a negative model time.
func TestWriteStats_OverlappingToolTimeDoesNotSubtract(t *testing.T) {
	out := renderStats(agentapp.SessionStats{
		ID: "s1",
		Runs: trace.SessionSummary{
			Runs: 1, Wall: 10_000_000_000, ToolWall: 25_000_000_000,
		},
	})
	if !strings.Contains(out, "overlapping") {
		t.Errorf("output does not explain why the split does not divide:\n%s", out)
	}
	if strings.Contains(out, "model / tools") {
		t.Errorf("output split a wall clock that tool time exceeds:\n%s", out)
	}
}

func TestMergeToolStats_TracePrevailsWhenItSawMoreCalls(t *testing.T) {
	rows := mergeToolStats(agentapp.SessionStats{
		Conversation: session.ConversationStats{
			Tools: []session.ToolStats{{Name: "Grep", Calls: 1, ResultBytes: 900}},
		},
		Runs: trace.SessionSummary{
			// Three calls ran: the parent's one plus two a subagent made,
			// which the parent's history never recorded.
			Tools: []trace.SessionToolStats{{Name: "Grep", Calls: 3, Wall: 1_000_000_000}},
		},
	})
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].Calls != 3 {
		t.Errorf("Calls = %d, want 3 — the traces saw calls the history did not", rows[0].Calls)
	}
	if rows[0].ResultBytes != 900 {
		t.Errorf("ResultBytes = %d, want 900", rows[0].ResultBytes)
	}
}

func TestFormatCount(t *testing.T) {
	for _, tc := range []struct {
		in   int
		want string
	}{{0, "0"}, {999, "999"}, {1000, "1,000"}, {184320, "184,320"}, {1234567, "1,234,567"}} {
		if got := formatCount(tc.in); got != tc.want {
			t.Errorf("formatCount(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWriteForkTreeShowsLineageAndDeletedSources(t *testing.T) {
	tree := &agentapp.ForkTreeNode{
		ID: "deleted-parent", Missing: true,
		Children: []agentapp.ForkTreeNode{
			{ID: "child", Title: "Alternative", Current: true},
		},
	}
	var out strings.Builder
	writeForkTree(&out, tree, "")
	for _, want := range []string{"Session tree", "source session deleted", "Alternative", "current"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("tree report does not show %q:\n%s", want, out.String())
		}
	}
}
