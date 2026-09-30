package trace

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/icloudbb/buildmax/internal/core/agent"
	"github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/icloudbb/buildmax/internal/util/secretscan"
)

func TestRecordFromEvent_Mapping(t *testing.T) {
	cases := []struct {
		name     string
		ev       agent.Event
		wantType string
		check    func(t *testing.T, r Record)
	}{
		{"iter", agent.Event{Kind: agent.EventIterStart, Iter: 2}, "iter_start", func(t *testing.T, r Record) {
			if r.Iter != 2 {
				t.Errorf("iter = %d", r.Iter)
			}
		}},
		{"llm_end", agent.Event{Kind: agent.EventLLMEnd, Iter: 1, HasToolCalls: true, PromptTokens: 10, CompletionTokens: 5, Content: "hi"}, "llm_end", func(t *testing.T, r Record) {
			if !r.HasToolCalls || r.PromptTokens != 10 || r.Content != "hi" {
				t.Errorf("bad llm_end: %+v", r)
			}
		}},
		{"tool_end", agent.Event{Kind: agent.EventToolEnd, ToolName: "bash", ToolCallID: "c1", ToolResult: "ok", ToolDuration: 1500 * time.Millisecond}, "tool_end", func(t *testing.T, r Record) {
			if r.Tool != "bash" || r.Result != "ok" || r.DurationMS != 1500 {
				t.Errorf("bad tool_end: %+v", r)
			}
		}},
		{"tool_denied", agent.Event{Kind: agent.EventToolDenied, ToolName: "bash", DenyReason: agent.DenyReasonPolicy}, "tool_denied", func(t *testing.T, r Record) {
			if r.DenyReason != "policy" {
				t.Errorf("deny = %q", r.DenyReason)
			}
		}},
		{"run_end", agent.Event{Kind: agent.EventRunEnd, Stats: agent.RunStats{ToolCalls: 3}, Err: errors.New("boom")}, "run_end", func(t *testing.T, r Record) {
			if r.ToolCalls != 3 || r.Error != "boom" {
				t.Errorf("bad run_end: %+v", r)
			}
		}},
		// A trace is where a run's cost is read back after the fact, so the
		// cached breakdown has to be in it: without the counts, a reader cannot
		// tell a cheap cached run from an expensive uncached one.
		{"llm_end_cache", agent.Event{
			Kind: agent.EventLLMEnd, Iter: 1, PromptTokens: 100, CompletionTokens: 5,
			CacheReadTokens: 80, CacheWriteTokens: 10,
		}, "llm_end", func(t *testing.T, r Record) {
			if r.CacheReadTokens != 80 || r.CacheWriteTokens != 10 {
				t.Errorf("bad llm_end cache counts: %+v", r)
			}
		}},
		// A reader asking which turn was expensive gets the call's own figures
		// rather than having to subtract consecutive records — which goes wrong
		// the moment a call in between failed and wrote none.
		{"llm_end_cost", agent.Event{
			Kind: agent.EventLLMEnd, Iter: 2, PromptTokens: 200, CacheReadTokens: 90,
			CallUsage: llm.Usage{PromptTokens: 100, CompletionTokens: 5, CacheReadTokens: 90},
			CallCost:  &llm.Cost{Currency: "USD", Uncached: 30, CacheRead: 27, Output: 75, Total: 132, Baseline: 375},
		}, "llm_end", func(t *testing.T, r Record) {
			if r.CallPromptTokens != 100 || r.CallCacheReadTokens != 90 {
				t.Errorf("call counts wrong: %+v", r)
			}
			if r.PromptTokens != 200 {
				t.Errorf("the run total should still be the run total, got %d", r.PromptTokens)
			}
			if r.Cost == nil || r.Cost.Total != 132 || r.Cost.Baseline != 375 {
				t.Errorf("bad llm_end cost: %+v", r.Cost)
			}
		}},
		// An unpriced model leaves the field out. A zero would read as a call
		// that cost nothing.
		{"llm_end_unpriced", agent.Event{
			Kind: agent.EventLLMEnd, Iter: 1,
			CallUsage: llm.Usage{PromptTokens: 100, CompletionTokens: 5},
		}, "llm_end", func(t *testing.T, r Record) {
			if r.Cost != nil {
				t.Errorf("an unpriced call recorded a cost: %+v", r.Cost)
			}
		}},
		{"run_end_cost", agent.Event{
			Kind: agent.EventRunEnd,
			Stats: agent.RunStats{
				ToolCalls: 2, PromptTokens: 200,
				Cost:           &llm.Cost{Currency: "USD", Total: 264, Baseline: 750},
				CostIncomplete: true,
			},
		}, "run_end", func(t *testing.T, r Record) {
			if r.Cost == nil || r.Cost.Total != 264 {
				t.Fatalf("bad run_end cost: %+v", r.Cost)
			}
			if !r.CostIncomplete {
				t.Error("a partial total must say so; a figure that quietly dropped a call is worse than none")
			}
		}},
		{"run_end_cache", agent.Event{
			Kind:  agent.EventRunEnd,
			Stats: agent.RunStats{ToolCalls: 1, PromptTokens: 100, CacheReadTokens: 80, CacheWriteTokens: 10},
		}, "run_end", func(t *testing.T, r Record) {
			if r.CacheReadTokens != 80 || r.CacheWriteTokens != 10 {
				t.Errorf("bad run_end cache counts: %+v", r)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, ok := recordFromEvent(c.ev, defaultMaxFieldBytes, nil)
			if !ok {
				t.Fatal("expected ok")
			}
			if r.Type != c.wantType {
				t.Fatalf("type = %q, want %q", r.Type, c.wantType)
			}
			c.check(t, r)
		})
	}
}

func TestRecordFromEvent_DeltaSkipped(t *testing.T) {
	if _, ok := recordFromEvent(agent.Event{Kind: agent.EventLLMDelta, Content: "x"}, defaultMaxFieldBytes, nil); ok {
		t.Error("EventLLMDelta should not be persisted")
	}
}

func TestRecordFromEvent_BoundsAndRedacts(t *testing.T) {
	big := strings.Repeat("a", 100) + " Bearer secrettoken123456"
	r, _ := recordFromEvent(agent.Event{Kind: agent.EventLLMEnd, Content: big}, 20, nil)
	if !strings.Contains(r.Content, "truncated") {
		t.Errorf("expected truncation marker, got %q", r.Content)
	}
	full, _ := recordFromEvent(agent.Event{Kind: agent.EventToolEnd, ToolResult: "x Bearer secrettoken123456"}, defaultMaxFieldBytes, nil)
	if strings.Contains(full.Result, "secrettoken123456") {
		t.Errorf("token not redacted: %q", full.Result)
	}
}

func TestBound(t *testing.T) {
	if got := bound("hello", 0); got != "hello" {
		t.Errorf("max<=0 should not bound: %q", got)
	}
	if got := bound("hello", 100); got != "hello" {
		t.Errorf("under limit should not bound: %q", got)
	}
	got := bound("hello world", 5)
	if !strings.HasPrefix(got, "hello") || !strings.Contains(got, "truncated 6 bytes") {
		t.Errorf("bound = %q", got)
	}
}

// TestBound_RuneBoundary asserts the cut never splits a multi-byte character:
// the kept prefix must stay valid UTF-8 so the JSON encoder does not rewrite
// the tail as U+FFFD.
func TestBound_RuneBoundary(t *testing.T) {
	// Each CJK rune is 3 bytes; a limit of 4 falls inside the second rune.
	got := bound("中文内容", 4)
	if !utf8.ValidString(got) {
		t.Fatalf("bound produced invalid UTF-8: %q", got)
	}
	prefix, _, _ := strings.Cut(got, " … [truncated")
	if prefix != "中" {
		t.Errorf("prefix = %q, want %q", prefix, "中")
	}
	if !strings.Contains(got, "truncated 9 bytes") {
		t.Errorf("bound = %q, want 9 dropped bytes", got)
	}
}

// TestRecordFromEvent_RedactsExactSecretValue proves a run's materialized
// Secret value is redacted from a tool result before it is written to the
// trace, even though it matches no secret shape.
func TestRecordFromEvent_RedactsExactSecretValue(t *testing.T) {
	red := secretscan.NewRedactor([]string{"ghs_deadbeefcafe0001"})
	r, _ := recordFromEvent(
		agent.Event{Kind: agent.EventToolEnd, ToolResult: "cloned with token ghs_deadbeefcafe0001 ok"},
		defaultMaxFieldBytes, red)
	if strings.Contains(r.Result, "ghs_deadbeefcafe0001") {
		t.Fatalf("exact secret value not redacted from trace: %q", r.Result)
	}
	if !strings.Contains(r.Result, "[redacted]") {
		t.Fatalf("expected a redaction marker: %q", r.Result)
	}
}

// A run's error and a denial's reason quote what failed -- a provider echoing
// the prompt, a hook quoting the input it blocked -- so they are free text too.
func TestRecordFromEvent_RedactsSecretValuesFromErrorsAndDenials(t *testing.T) {
	const secret = "harbor-canary-value"
	red := secretscan.NewRedactor([]string{secret})
	for _, e := range []agent.Event{
		{Kind: agent.EventRunEnd, Err: errors.New("provider error (HTTP 400): the request quoted " + secret)},
		{Kind: agent.EventToolDenied, DenyReason: "hook refused an argument holding " + secret},
		{Kind: agent.EventUserInputBlocked, DenyReason: "hook refused input holding " + secret},
	} {
		r, _ := recordFromEvent(e, defaultMaxFieldBytes, red)
		if strings.Contains(r.Error+r.DenyReason, secret) || !strings.Contains(r.Error+r.DenyReason, "[redacted]") {
			t.Errorf("%s record: error %q, deny reason %q", r.Type, r.Error, r.DenyReason)
		}
	}
}
