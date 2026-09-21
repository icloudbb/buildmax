package cli

import (
	"github.com/icloudbb/buildmax/internal/util"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/agentapp"
	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/icloudbb/buildmax/internal/core/localproject"
	"github.com/icloudbb/buildmax/internal/core/session"
	"github.com/icloudbb/buildmax/internal/infra/trace"

	tea "charm.land/bubbletea/v2"
)

// statsPanelModel is a sized model with a session open, which is what the
// panel's width- and height-driven trimming reads.
func infoPanelModel(t *testing.T) *Model {
	t.Helper()
	m := NewModel(TUIOpts{Session: testSessionContext(), Workspace: util.FixedRoot(t.TempDir())})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	return next.(*Model)
}

func renderInfoPanel(t *testing.T, s agentapp.SessionStats) string {
	t.Helper()
	m := infoPanelModel(t)
	p := &slashInfoPanel{Stats: s, Opened: -1}
	return p.Render(m, m.panelContentWidth())
}

func TestSlashInfo_IsRegisteredForCompletionAndDispatch(t *testing.T) {
	if !slices.Contains(builtinSlashCommands, "/info") {
		t.Fatal("/info is missing from the completion list")
	}
	if !slices.IsSorted(builtinSlashCommands) {
		t.Errorf("builtinSlashCommands is not sorted: %v", builtinSlashCommands)
	}
	m := infoPanelModel(t)
	opened, _ := dispatchSlashCommand(m, "/info")
	mod := opened.(*Model)
	if mod.activePanel == nil {
		t.Fatal("/info opened no panel")
	}
	if mod.err != "" {
		t.Errorf("dispatching /info set err = %q, want none", mod.err)
	}
}

// The panel folds the live session, so a run with no session open must say so
// rather than render an empty report that reads as a session that did nothing.
func TestSlashInfo_NoSessionSaysSo(t *testing.T) {
	m := NewModel(TUIOpts{Workspace: util.FixedRoot(t.TempDir())})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	mod := next.(*Model)
	opened, _ := openSlashInfo(mod)
	mod = opened.(*Model)
	if mod.activePanel == nil {
		t.Fatal("openSlashStats opened no panel")
	}
	out := mod.activePanel.Render(mod, mod.panelContentWidth())
	if !strings.Contains(out, "no session is open") {
		t.Errorf("panel does not report the missing session:\n%s", out)
	}
}

// Same rule as the command: a provider reporting no cache usage has not
// reported a miss, so no cache line is printed at all.
func TestSlashInfo_UnreportedCacheIsNotPrintedAsZero(t *testing.T) {
	out := renderInfoPanel(t, agentapp.SessionStats{
		ID:    "s1",
		Usage: cllm.Usage{PromptTokens: 1000, CompletionTokens: 50},
	})
	if strings.Contains(out, "Cache") {
		t.Errorf("panel claims a cache result nobody measured:\n%s", out)
	}
	if !strings.Contains(out, "not priced") {
		t.Errorf("panel does not say the session was never priced:\n%s", out)
	}
}

func TestSlashInfo_MissingTraceSaysUnavailableNotZero(t *testing.T) {
	out := renderInfoPanel(t, agentapp.SessionStats{
		ID:           "s1",
		Conversation: session.ConversationStats{UserMessages: 1, ToolCalls: 3},
	})
	if !strings.Contains(out, "timings are unavailable") {
		t.Errorf("panel does not say the timings are missing:\n%s", out)
	}
	if strings.Contains(out, "waiting") {
		t.Errorf("panel reports a wall clock it never measured:\n%s", out)
	}
}

// The warnings are shared with the command deliberately: one that appeared on
// only one surface would be a warning nobody trusted.
func TestSlashInfo_CarriesTheSameCaveatsAsTheCommand(t *testing.T) {
	s := agentapp.SessionStats{
		ID:             "s1",
		CostIncomplete: true,
		Runs:           trace.SessionSummary{Runs: 2, Incomplete: 1},
	}
	panel := renderInfoPanel(t, s)
	notes := statsCaveats(s)
	if len(notes) == 0 {
		t.Fatal("the fixture produced no caveats, so the test asserts nothing")
	}
	for _, want := range notes {
		// Truncated to the panel width, so match on a distinctive head.
		head := string([]rune(want)[:40])
		if !strings.Contains(panel, head) {
			t.Errorf("panel is missing the caveat %q:\n%s", want, panel)
		}
	}
}

// Parallel tool execution can push summed tool time past the wall clock.
func TestSlashInfo_OverlappingToolTimeDoesNotSubtract(t *testing.T) {
	out := renderInfoPanel(t, agentapp.SessionStats{
		ID:   "s1",
		Runs: trace.SessionSummary{Runs: 1, Wall: 10_000_000_000, ToolWall: 25_000_000_000},
	})
	if !strings.Contains(out, "overlapping") {
		t.Errorf("panel does not explain why the split does not divide:\n%s", out)
	}
	if strings.Contains(out, "model /") {
		t.Errorf("panel split a wall clock that tool time exceeds:\n%s", out)
	}
}

// A trimmed tool list must say it was trimmed: a short list is otherwise
// indistinguishable from a session that used four tools.
func TestSlashInfo_TrimmedToolListSaysSo(t *testing.T) {
	var tools []session.ToolStats
	for i := range 40 {
		tools = append(tools, session.ToolStats{
			Name: strings.Repeat("t", i%9+3), Calls: 1, ResultBytes: 100 - i,
		})
	}
	out := renderInfoPanel(t, agentapp.SessionStats{
		ID:           "s1",
		Conversation: session.ConversationStats{UserMessages: 3, ToolCalls: 40, Tools: tools},
		Runs:         trace.SessionSummary{Runs: 1, Wall: 5_000_000_000},
	})
	if !strings.Contains(out, "more") {
		t.Errorf("panel trimmed the tool list without saying so:\n%s", out)
	}
}

func memoryOverview(memories ...localproject.Memory) agentapp.MemoryOverview {
	return agentapp.MemoryOverview{
		Project:     localproject.Project{ID: "hyzc3kqxa2vw7m4t9pbn", Name: "buildmax", Kind: localproject.KindGit},
		Memories:    memories,
		IndexChars:  420,
		IndexBudget: 3200,
	}
}

func sampleMemories() []localproject.Memory {
	return []localproject.Memory{
		{
			Name: "merge-commit", Type: localproject.MemoryTypeFeedback,
			Description: "merge commits, not squash",
			Body:        "Use merge commits.\n\n**Why:** per-commit revert.",
			UpdatedAt:   time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC),
		},
		{
			Name: "fixture-layout", Type: localproject.MemoryTypeProject,
			Description: "generated fixtures sit outside testdata/",
			Body:        "Fixtures live in gen/.",
		},
	}
}

func openInfoOn(t *testing.T, tab infoTab, o agentapp.MemoryOverview) (*Model, *slashInfoPanel) {
	t.Helper()
	m := infoPanelModel(t)
	p := &slashInfoPanel{Tab: tab, Memory: o, Opened: -1}
	opened, _ := m.openPanel(p)
	return opened.(*Model), p
}

// The three views answer the related session, lineage, and project questions,
// and the tab bar names each: a tab a person cannot see is one they will not
// press.
func TestSlashInfo_TabBarNamesAllViews(t *testing.T) {
	m, p := openInfoOn(t, infoTabSession, memoryOverview())
	out := p.Render(m, m.panelContentWidth())
	for _, want := range []string{"session", "tree", "memory"} {
		if !strings.Contains(out, want) {
			t.Errorf("tab bar does not name %q:\n%s", want, out)
		}
	}
}

func TestSlashInfo_TreeTabShowsTheCurrentForkAmongItsSiblings(t *testing.T) {
	m, p := openInfoOn(t, infoTabTree, memoryOverview())
	p.ForkTree = &agentapp.ForkTreeNode{
		ID: "root", Title: "Original",
		Children: []agentapp.ForkTreeNode{
			{ID: "first", Title: "First approach"},
			{ID: "second", Title: "Second approach", Current: true},
		},
	}
	out := p.Render(m, m.panelContentWidth())
	for _, want := range []string{"Original", "First approach", "Second approach", "current"} {
		if !strings.Contains(out, want) {
			t.Errorf("tree tab does not show %q:\n%s", want, out)
		}
	}
}

func TestSlashInfo_MemoryTabListsWhatTheProjectKnows(t *testing.T) {
	m, p := openInfoOn(t, infoTabMemory, memoryOverview(sampleMemories()...))
	out := p.Render(m, m.panelContentWidth())

	for _, want := range []string{"buildmax", "merge-commit", "fixture-layout", "2 memories"} {
		if !strings.Contains(out, want) {
			t.Errorf("memory tab does not show %q:\n%s", want, out)
		}
	}
	// What the index costs on every call is the number a person prunes
	// against, not the count.
	if !strings.Contains(out, "420/3200") {
		t.Errorf("memory tab does not report what the index costs:\n%s", out)
	}
	// A list of twenty full bodies is not a list.
	if strings.Contains(out, "per-commit revert") {
		t.Errorf("the list inlined a body:\n%s", out)
	}
}

// The body is where the reason lives, which is the half a description cannot
// carry — so it is one keystroke away, and one keystroke back.
func TestSlashInfo_EnterOpensABodyAndEscGoesBack(t *testing.T) {
	m, p := openInfoOn(t, infoTabMemory, memoryOverview(sampleMemories()...))

	if _, _ = p.HandleKey(m, tea.KeyPressMsg{Code: tea.KeyEnter}); p.Opened != 0 {
		t.Fatalf("Opened = %d, want the selected memory", p.Opened)
	}
	out := p.Render(m, m.panelContentWidth())
	if !strings.Contains(out, "per-commit revert") {
		t.Errorf("the opened body is not shown:\n%s", out)
	}

	handled, cmd := p.HandleKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !handled || cmd != nil {
		t.Error("esc on an open body should return to the list, not close the panel")
	}
	if p.Opened != -1 {
		t.Errorf("Opened = %d, want the list", p.Opened)
	}
	if m.activePanel == nil {
		t.Error("esc on an open body closed the panel")
	}
}

// A body belongs to the memory tab, so leaving the tab leaves the body: coming
// back to find it still open would be a state the tab bar does not show.
func TestSlashInfo_SwitchingTabsClosesAnOpenBody(t *testing.T) {
	m, p := openInfoOn(t, infoTabMemory, memoryOverview(sampleMemories()...))
	p.Opened = 1

	p.HandleKey(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	if p.Tab != infoTabTree || p.Opened != -1 {
		t.Errorf("tab = %v, opened = %d; want the tree tab with no body open", p.Tab, p.Opened)
	}
	p.HandleKey(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	if p.Tab != infoTabSession {
		t.Errorf("tab = %v, want the session tab", p.Tab)
	}
	p.HandleKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if p.Tab != infoTabTree {
		t.Errorf("tab = %v, want the tree tab", p.Tab)
	}
}

func TestSlashInfo_MemoryTabWithoutAProject(t *testing.T) {
	m, p := openInfoOn(t, infoTabMemory, agentapp.MemoryOverview{IndexBudget: 3200})
	out := p.Render(m, m.panelContentWidth())
	if !strings.Contains(out, "no project") {
		t.Errorf("panel does not explain why there is no memory:\n%s", out)
	}
}

// A store that cannot be read is not a store with nothing in it, and an empty
// one is not a run that turned memory off. Each says which it is.
func TestSlashInfo_MemoryTabDistinguishesEmptyFromUnavailableAndOff(t *testing.T) {
	tests := map[string]struct {
		overview agentapp.MemoryOverview
		want     string
	}{
		"empty":       {memoryOverview(), "Nothing is remembered"},
		"unavailable": {agentapp.MemoryOverview{Project: memoryOverview().Project, Unavailable: "permission denied"}, "cannot be read"},
		"turned off":  {agentapp.MemoryOverview{Project: memoryOverview().Project, Disabled: true}, "off for this run"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			m, p := openInfoOn(t, infoTabMemory, tt.overview)
			out := p.Render(m, m.panelContentWidth())
			if !strings.Contains(out, tt.want) {
				t.Errorf("memory tab does not say %q:\n%s", tt.want, out)
			}
		})
	}
}

// A file that never loads is silently absent from every run, so the panel a
// person opens to look at their memories is where it has to appear.
func TestSlashInfo_MemoryTabNamesSkippedFiles(t *testing.T) {
	o := memoryOverview(sampleMemories()...)
	o.Skipped = []localproject.SkippedMemory{{File: "broken.md", Reason: "no opening --- frontmatter delimiter"}}
	m, p := openInfoOn(t, infoTabMemory, o)

	out := p.Render(m, m.panelContentWidth())
	if !strings.Contains(out, "broken.md") {
		t.Errorf("memory tab does not name the skipped file:\n%s", out)
	}
}
