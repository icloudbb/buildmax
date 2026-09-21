package desktop

import (
	"fmt"
	"sort"
	"strings"

	"github.com/icloudbb/buildmax/internal/agentapp"
	"github.com/icloudbb/buildmax/internal/config"
	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/icloudbb/buildmax/internal/core/session"
	"github.com/icloudbb/buildmax/internal/util"
)

// The /info session-statistics and fork-tree surfaces. The memory tab is served
// by ProjectMemory, which the frontend already reads for the Memory drawer.
//
// It is a purpose-built payload rather than agentapp.SessionStats itself: that
// type carries nested records with no wire tags and helper methods the frontend
// cannot call, so this flattens it the way the CLI command and the TUI panel
// each render it. What may be claimed of the numbers is the shared part — the
// caveats and the "meaningful only" gates below match info.go and chat_info.go.

// SlashInfoTool is one tool's row on the session tab, heaviest first.
type SlashInfoTool struct {
	Name        string `json:"name"`
	Calls       int    `json:"calls"`
	ResultBytes int    `json:"result_bytes"`
	// WallText is a formatted duration, empty when no trace timed the tool.
	WallText string `json:"wall_text,omitempty"`
	// Note is the failure/denial summary, e.g. "2 timeout, 1 denied".
	Note string `json:"note,omitempty"`
}

// SlashInfoResult is the session tab of /info. Money is pre-formatted here
// because the currency arithmetic lives in llm.FormatAmount; token, byte, and
// count values are raw for the frontend to format.
type SlashInfoResult struct {
	// LoadError is set when there is no session or the stats could not be fully
	// read; the panel shows it and still renders whatever else is present.
	LoadError string `json:"load_error,omitempty"`

	Title string `json:"title,omitempty"`

	// Spend.
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	Priced           bool   `json:"priced"`
	CostText         string `json:"cost_text,omitempty"`
	Currency         string `json:"currency,omitempty"`

	// Cache — all zero when the provider reported none.
	CacheReadTokens  int    `json:"cache_read_tokens"`
	CacheWriteTokens int    `json:"cache_write_tokens"`
	CacheSavedText   string `json:"cache_saved_text,omitempty"`
	// CacheCostMore is set when caching cost more than it saved this session.
	CacheCostMore bool `json:"cache_cost_more,omitempty"`

	// Delegated — DelegatedRuns == 0 means no subagent ran.
	DelegatedRuns             int    `json:"delegated_runs"`
	DelegatedPromptTokens     int    `json:"delegated_prompt_tokens"`
	DelegatedCompletionTokens int    `json:"delegated_completion_tokens"`
	DelegatedCostText         string `json:"delegated_cost_text,omitempty"`

	// Context.
	PeakRecorded      bool    `json:"peak_recorded"`
	PeakContextTokens int     `json:"peak_context_tokens"`
	ContextWindow     int     `json:"context_window"`
	ContextShare      float64 `json:"context_share"`
	Compactions       int     `json:"compactions"`

	// History bytes.
	TextBytes       int `json:"text_bytes"`
	ToolResultBytes int `json:"tool_result_bytes"`

	// Work.
	UserMessages   int `json:"user_messages"`
	AssistantTurns int `json:"assistant_turns"`
	ToolCalls      int `json:"tool_calls"`
	ToolFailures   int `json:"tool_failures"`
	ToolDenials    int `json:"tool_denials"`

	// Time — HasTrace false means no trace, so timings are unavailable rather
	// than zero.
	HasTrace  bool   `json:"has_trace"`
	WallText  string `json:"wall_text,omitempty"`
	ModelText string `json:"model_text,omitempty"`
	ToolsText string `json:"tools_text,omitempty"`
	// ToolsOverlap says summed tool time exceeded the wall clock (parallel
	// execution), so the model/tools split is not shown.
	ToolsOverlap bool `json:"tools_overlap,omitempty"`
	Subagents    int  `json:"subagents"`

	Tools   []SlashInfoTool `json:"tools"`
	Caveats []string        `json:"caveats,omitempty"`
}

// SessionForkTreeResult is the tree tab of /info. It stays separate from the
// statistics payload so opening the tree reads only index.json, not history or
// traces for every node (or even for the current one).
type SessionForkTreeResult struct {
	Tree      *agentapp.ForkTreeNode `json:"tree,omitempty"`
	LoadError string                 `json:"load_error,omitempty"`
}

// GetSlashInfo returns the session statistics for the /info panel. The memory
// tab is served by ProjectMemory and the tree tab by GetSessionForkTree.
func (a *App) GetSlashInfo(projectID, sessionID string) (SlashInfoResult, error) {
	if sessionID == "" {
		return SlashInfoResult{LoadError: "no session is open"}, nil
	}
	// Read-only load: showing statistics must not take the session's writer
	// lock, or opening /info during a run would lock out the run.
	loaded, err := sessionManager().Load(sessionID, session.LoadFull)
	if err != nil {
		return SlashInfoResult{}, fmt.Errorf("load session: %w", err)
	}
	stats, statsErr := agentapp.NewSessionStats(loaded, config.SessionsDir())

	out := SlashInfoResult{
		Title:            stats.Title,
		PromptTokens:     stats.Usage.PromptTokens,
		CompletionTokens: stats.Usage.CompletionTokens,
		CacheReadTokens:  stats.Usage.CacheReadTokens,
		CacheWriteTokens: stats.Usage.CacheWriteTokens,
		Compactions:      stats.Runs.Compactions,
		TextBytes:        stats.Conversation.TextBytes,
		ToolResultBytes:  stats.Conversation.ToolResultBytes,
		UserMessages:     stats.Conversation.UserMessages,
		AssistantTurns:   stats.Conversation.AssistantTurns,
		ToolCalls:        stats.Conversation.ToolCalls,
		ToolFailures:     stats.Runs.ToolFailures,
		ToolDenials:      stats.Runs.ToolDenials,
		Subagents:        stats.Runs.Subagents,
	}
	if statsErr != nil {
		out.LoadError = statsErr.Error()
	}

	if stats.Cost != nil {
		out.Priced = true
		out.CostText = cllm.FormatAmount(stats.Cost.Total)
		out.Currency = stats.Cost.Currency
	}

	// Cache saving only where it is meaningful: a session that only ever wrote
	// cache entries paid more than uncached, and calling that a win would lie.
	if saved, ok := stats.CacheSaved(); ok {
		out.CacheSavedText = cllm.FormatAmount(saved)
	} else if stats.Cost != nil && stats.Cost.Baseline > 0 {
		out.CacheCostMore = true
	}

	if d := stats.Runs.Delegated; d != nil && d.Runs > 0 {
		out.DelegatedRuns = d.Runs
		out.DelegatedPromptTokens = d.PromptTokens
		out.DelegatedCompletionTokens = d.CompletionTokens
		if d.Cost != nil {
			out.DelegatedCostText = cllm.FormatAmount(d.Cost.Total) + " " + d.Cost.Currency
		}
	}

	if share, ok := stats.ContextPeakShare(); ok {
		out.PeakRecorded = true
		out.PeakContextTokens = stats.Runs.PeakContextTokens
		out.ContextWindow = stats.Runs.ContextWindow
		out.ContextShare = share
	}

	if stats.Runs.Runs > 0 {
		out.HasTrace = true
		out.WallText = util.FormatDuration(stats.Runs.Wall)
		if model, ok := stats.ModelTime(); ok {
			out.ModelText = util.FormatDuration(model)
			out.ToolsText = util.FormatDuration(stats.Runs.ToolWall)
		} else if stats.Runs.ToolWall > 0 {
			out.ToolsText = util.FormatDuration(stats.Runs.ToolWall)
			out.ToolsOverlap = true
		}
	}

	out.Tools = slashInfoToolRows(stats)
	out.Caveats = slashInfoCaveats(stats)
	return out, nil
}

// GetSessionForkTree returns the connected user-session fork tree containing
// sessionID from the picker projection.
func (a *App) GetSessionForkTree(projectID, sessionID string) SessionForkTreeResult {
	if sessionID == "" {
		return SessionForkTreeResult{LoadError: "no session is open"}
	}
	tree, err := sessionManager().ForkTree(sessionID)
	if err != nil {
		return SessionForkTreeResult{LoadError: err.Error()}
	}
	return SessionForkTreeResult{Tree: tree}
}

// slashInfoToolRows joins what the history knows (result bytes) with what the
// traces know (duration, failures), heaviest by bytes first — the same merge
// info.go's mergeToolStats does, kept here because that one is CLI-private.
func slashInfoToolRows(s agentapp.SessionStats) []SlashInfoTool {
	rows := map[string]*SlashInfoTool{}
	get := func(name string) *SlashInfoTool {
		r, ok := rows[name]
		if !ok {
			r = &SlashInfoTool{Name: name}
			rows[name] = r
		}
		return r
	}
	for _, t := range s.Conversation.Tools {
		r := get(t.Name)
		r.Calls = t.Calls
		r.ResultBytes = t.ResultBytes
	}
	for _, t := range s.Runs.Tools {
		r := get(t.Name)
		// The traces see subagent calls the parent's history never recorded.
		if t.Calls > r.Calls {
			r.Calls = t.Calls
		}
		if t.Wall > 0 {
			r.WallText = util.FormatDuration(t.Wall)
		}
		var notes []string
		for kind, n := range t.Failures {
			notes = append(notes, fmt.Sprintf("%d %s", n, kind))
		}
		sort.Strings(notes)
		if t.Denials > 0 {
			notes = append(notes, fmt.Sprintf("%d denied", t.Denials))
		}
		r.Note = strings.Join(notes, ", ")
	}
	out := make([]SlashInfoTool, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ResultBytes != out[j].ResultBytes {
			return out[i].ResultBytes > out[j].ResultBytes
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// slashInfoCaveats names what the numbers do not cover. It mirrors info.go's
// statsCaveats: a total that silently dropped a killed run is worse than one
// that says it did.
func slashInfoCaveats(s agentapp.SessionStats) []string {
	var lines []string
	if s.CostIncomplete {
		lines = append(lines, "Part of this session ran against an unpriced model or a different currency, so the cost understates it.")
	}
	if s.Runs.Incomplete > 0 {
		lines = append(lines, fmt.Sprintf("%d run(s) ended without writing a trace end record — killed or crashed — so their timings are missing here.", s.Runs.Incomplete))
	}
	if s.Runs.Failed > 0 {
		lines = append(lines, fmt.Sprintf("%d run(s) ended with an error.", s.Runs.Failed))
	}
	if s.Conversation.ToolCalls > 0 && s.Runs.Runs == 0 {
		lines = append(lines, "No run trace was found for this session, so every timing above is unavailable rather than zero.")
	}
	return lines
}
