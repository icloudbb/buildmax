package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/icloudbb/buildmax/internal/agentapp"
	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/icloudbb/buildmax/internal/core/localproject"
	"github.com/icloudbb/buildmax/internal/util"

	tea "charm.land/bubbletea/v2"
)

// slashStatsToolRows caps the tool table on the session tab. `buildmax info` prints
// a longer one and --json prints all of them; an overlay is a glance.
const slashStatsToolRows = 5

// slashStatsChromeLines are the session tab's own lines: box border, title and
// tab bar, the summary block above the table, its header, and the key hint.
const slashStatsChromeLines = 18

// infoTab is which view of the panel is on screen.
//
// The tabs answer three related questions: what this session has done, where it
// sits in its fork tree, and what this project knows.
type infoTab int

const (
	infoTabSession infoTab = iota
	infoTabTree
	infoTabMemory
	infoTabCount
)

// slashInfoPanel implements slashPanel for the /info overlay.
//
// All views are read once, when the panel opens, rather than on every render: a
// render runs on each keystroke and each frame of the spinner, and the
// statistics fold reads every trace file the session has.
type slashInfoPanel struct {
	Tab        infoTab
	Stats      agentapp.SessionStats
	LoadError  string
	ForkTree   *agentapp.ForkTreeNode
	TreeError  string
	TreeOffset int

	Memory   agentapp.MemoryOverview
	Selected int
	Offset   int
	// Opened is the memory whose body is on screen, or -1 for the list. A body
	// is shown on request because a list of twenty full bodies is not a list.
	Opened int
}

func openSlashInfo(m *Model) (tea.Model, tea.Cmd) {
	p := &slashInfoPanel{Opened: -1}
	if m.opts.App != nil {
		p.Memory = m.opts.App.MemoryOverview()
	}
	if m.opts.Session == nil {
		p.LoadError = "no session is open"
		return m.openPanel(p)
	}
	// The live session, not the file: the conversation commits as the turn
	// runs but its metadata lands at the end, so reading the bundle back here
	// would answer about the turn before the one on screen.
	stats, err := agentapp.NewSessionStats(m.opts.Session.Snapshot(), m.opts.SessionsDir)
	if err != nil {
		p.LoadError = err.Error()
	}
	p.Stats = stats
	if m.opts.Session.Persisted() && m.opts.SessionsDir != "" {
		p.ForkTree, err = agentapp.NewSessionManager(m.opts.SessionsDir).ForkTree(m.opts.Session.ID())
		if err != nil {
			p.TreeError = err.Error()
		} else {
			p.TreeOffset = max(0, forkTreeCurrentIndex(p.ForkTree)-2)
		}
	}
	return m.openPanel(p)
}

func (p *slashInfoPanel) HandleKey(m *Model, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.Code {
	case tea.KeyEscape:
		// Esc backs out of a body before it closes the panel: the reader who
		// opened one is one keystroke from the list, not from nothing.
		if p.Opened >= 0 {
			p.Opened = -1
			return true, nil
		}
		_, cmd := m.closeActivePanel()
		return true, cmd
	case tea.KeyTab, tea.KeyRight:
		p.switchTab((p.Tab + 1) % infoTabCount)
		return true, nil
	case tea.KeyLeft:
		p.switchTab((p.Tab + infoTabCount - 1) % infoTabCount)
		return true, nil
	case tea.KeyUp:
		if p.Tab == infoTabTree && p.TreeOffset > 0 {
			p.TreeOffset--
		}
		if p.Tab == infoTabMemory && p.Opened < 0 && p.Selected > 0 {
			p.Selected--
			p.scrollIntoView(m)
		}
		return true, nil
	case tea.KeyDown:
		if p.Tab == infoTabTree && p.TreeOffset < len(forkTreeLines(p.ForkTree))-1 {
			p.TreeOffset++
		}
		if p.Tab == infoTabMemory && p.Opened < 0 && p.Selected < len(p.Memory.Memories)-1 {
			p.Selected++
			p.scrollIntoView(m)
		}
		return true, nil
	case tea.KeyEnter:
		if p.Tab == infoTabMemory && p.Opened < 0 && len(p.Memory.Memories) > 0 {
			p.Opened = p.Selected
		}
		return true, nil
	}
	return false, nil
}

// switchTab moves to t, leaving any open body behind: a body belongs to the
// memory tab, and returning to find it still open would be a state the tab bar
// does not show.
func (p *slashInfoPanel) switchTab(t infoTab) {
	if p.Tab != t {
		p.Tab = t
		p.Opened = -1
	}
}

func (p *slashInfoPanel) scrollIntoView(m *Model) {
	rows := m.panelListBudget(slashInfoMemoryRows, slashInfoChromeLines)
	if p.Selected < p.Offset {
		p.Offset = p.Selected
	} else if rows > 0 && p.Selected >= p.Offset+rows {
		p.Offset = p.Selected - rows + 1
	}
}

func (p *slashInfoPanel) FooterHint() string { return "esc: close info panel" }

func (p *slashInfoPanel) OnClose(_ *Model) {}

func (p *slashInfoPanel) Render(m *Model, maxLineWidth int) string {
	var b strings.Builder
	b.WriteString(slashPanelTitleStyle.Render("Info"))
	b.WriteString("   ")
	b.WriteString(renderInfoTabs(p.Tab))
	b.WriteString("\n\n")

	if p.Tab == infoTabMemory {
		b.WriteString(p.renderMemory(m, maxLineWidth))
		return strings.TrimRight(b.String(), "\n") + "\n\n" + p.memoryHints()
	}
	if p.Tab == infoTabTree {
		rows := m.panelListBudget(14, 7)
		b.WriteString(renderForkTreePanel(p.ForkTree, p.TreeError, maxLineWidth, rows, p.TreeOffset))
		return strings.TrimRight(b.String(), "\n") +
			"\n\n↑↓: scroll · tab/←/→: switch · esc: close"
	}

	if p.LoadError != "" {
		b.WriteString(truncateRunes(p.LoadError, maxLineWidth))
		return strings.TrimRight(b.String(), "\n") + "\n\ntab/←/→: switch · esc: close"
	}
	b.WriteString(renderStatsSummary(p.Stats, maxLineWidth))
	budget := m.panelListBudget(slashStatsToolRows, slashStatsChromeLines)
	if table := renderStatsToolTable(p.Stats, budget, maxLineWidth); table != "" {
		b.WriteString("\n")
		b.WriteString(table)
	}
	for _, note := range statsCaveats(p.Stats) {
		b.WriteString("\n! " + truncateRunes(note, maxLineWidth-2))
	}
	return strings.TrimRight(b.String(), "\n") +
		"\n\ntab/←/→: switch · esc: close · buildmax info for the full record"
}

// renderInfoTabs draws the tab bar. The inactive one is named rather than
// hidden, because a tab a person cannot see is one they will not press.
func renderInfoTabs(active infoTab) string {
	labels := []string{"session", "tree", "memory"}
	var out []string
	for i, label := range labels {
		if infoTab(i) == active {
			out = append(out, slashPanelTitleStyle.Render("["+label+"]"))
		} else {
			out = append(out, label)
		}
	}
	return strings.Join(out, "  ")
}

// renderStatsSummary is the panel's condensed report. It is a separate
// rendering from the command's, not a narrowed copy of it: a boxed overlay a
// few dozen columns wide and a full terminal want different layouts, and the
// shared thing worth sharing is the data and the rules about what may be said
// of it.
func renderStatsSummary(s agentapp.SessionStats, maxLineWidth int) string {
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)

	row := func(label, value string) {
		fmt.Fprintf(tw, "%s\t%s\n", label, truncateRunes(value, maxLineWidth-12))
	}

	spend := fmt.Sprintf("%s in / %s out",
		formatCount(s.Usage.PromptTokens), formatCount(s.Usage.CompletionTokens))
	if s.Cost != nil {
		spend += " · " + cllm.FormatAmount(s.Cost.Total) + " " + s.Cost.Currency
	} else {
		spend += " · not priced"
	}
	row("Spend", spend)

	// Only where a provider reported cache usage: a "0 / 0" line on a provider
	// that reports nothing would claim a miss nobody measured.
	if s.Usage.CacheReadTokens > 0 || s.Usage.CacheWriteTokens > 0 {
		cache := fmt.Sprintf("%s read / %s write",
			formatCount(s.Usage.CacheReadTokens), formatCount(s.Usage.CacheWriteTokens))
		if s.Usage.PromptTokens > 0 {
			cache += fmt.Sprintf(" · %.0f%% of prompt",
				float64(s.Usage.CacheReadTokens)/float64(s.Usage.PromptTokens)*100)
		}
		if saved, ok := s.CacheSaved(); ok {
			cache += " · saved " + cllm.FormatAmount(saved)
		} else if s.Cost != nil && s.Cost.Baseline > 0 {
			// A run that only ever wrote cache entries paid more than it would
			// have uncached, and calling that a small win would be a lie.
			cache += " · cost more than uncached"
		}
		row("Cache", cache)
	}

	if d := s.Runs.Delegated; d != nil && d.Runs > 0 {
		line := fmt.Sprintf("%s · %s in / %s out",
			countLabel(d.Runs, "run"), formatCount(d.PromptTokens), formatCount(d.CompletionTokens))
		if d.Cost != nil {
			line += " · " + cllm.FormatAmount(d.Cost.Total) + " " + d.Cost.Currency
		}
		row("Delegated", line)
	}

	ctx := "peak not recorded"
	if share, ok := s.ContextPeakShare(); ok {
		ctx = fmt.Sprintf("peak %s of %s (%.0f%%)",
			formatCount(s.Runs.PeakContextTokens), formatCount(s.Runs.ContextWindow), share*100)
	}
	ctx += " · " + countLabel(s.Runs.Compactions, "compaction")
	row("Context", ctx)

	c := s.Conversation
	row("History", fmt.Sprintf("%s text / %s tool output",
		formatBytes(c.TextBytes), formatBytes(c.ToolResultBytes)))

	work := fmt.Sprintf("%s · %s · %s",
		countLabel(c.UserMessages, "message"), countLabel(c.AssistantTurns, "turn"),
		countLabel(c.ToolCalls, "tool call"))
	if s.Runs.ToolFailures > 0 {
		work += fmt.Sprintf(" · %d could not complete", s.Runs.ToolFailures)
	}
	if s.Runs.ToolDenials > 0 {
		work += fmt.Sprintf(" · %d denied", s.Runs.ToolDenials)
	}
	row("Work", work)

	if s.Runs.Runs == 0 {
		row("Time", "no trace recorded, so timings are unavailable")
	} else {
		time := util.FormatDuration(s.Runs.Wall) + " waiting"
		if model, ok := s.ModelTime(); ok {
			time += fmt.Sprintf(" · %s model / %s tools",
				util.FormatDuration(model), util.FormatDuration(s.Runs.ToolWall))
		} else if s.Runs.ToolWall > 0 {
			// Parallel tool execution can push summed tool time past the wall
			// clock; subtracting anyway would print a negative model time.
			time += fmt.Sprintf(" · %s in tools, overlapping", util.FormatDuration(s.Runs.ToolWall))
		}
		if s.Runs.Subagents > 0 {
			time += " · " + countLabel(s.Runs.Subagents, "subagent run")
		}
		row("Time", time)
	}
	_ = tw.Flush()
	return trimRowPadding(b.String())
}

func renderStatsToolTable(s agentapp.SessionStats, budget, maxLineWidth int) string {
	tools := mergeToolStats(s)
	if len(tools) == 0 || budget <= 0 {
		return ""
	}
	shown := tools
	if len(shown) > budget {
		shown = shown[:budget]
	}
	var b strings.Builder
	b.WriteString("Tools, heaviest first\n")
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, t := range shown {
		name := t.Name
		if name == "" {
			name = "(unattributed)"
		}
		output := "-"
		if t.ResultBytes > 0 {
			output = formatBytes(t.ResultBytes)
		}
		spent := "-"
		if t.Wall > 0 {
			spent = util.FormatDuration(t.Wall)
		}
		fmt.Fprintf(tw, "  %s\t%d\t%s\t%s\t%s\n",
			truncateRunes(name, maxLineWidth/3), t.Calls, output, spent, t.Note)
	}
	_ = tw.Flush()
	out := trimRowPadding(b.String())
	if len(tools) > len(shown) {
		out += fmt.Sprintf("  … %d more\n", len(tools)-len(shown))
	}
	return out
}

// slashInfoMemoryRows caps the memory list. Twenty is the store's own bound, so
// the list rarely scrolls; the cap is what keeps the panel from growing past
// the terminal when it does.
const slashInfoMemoryRows = 8

// slashInfoChromeLines are the memory tab's own lines: box border, title and
// tab bar, the scope block above the list, and the key hints.
const slashInfoChromeLines = 12

// renderMemory draws the memory tab: the scope and what it costs, then the
// list, or one body when the reader opened it.
func (p *slashInfoPanel) renderMemory(m *Model, maxLineWidth int) string {
	o := p.Memory
	if o.Project.ID == "" {
		return "This run belongs to no project, so it carries no memory.\n" +
			"A project is one Git repository, including its worktrees, or one folder."
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s)\n", o.Project.Name, o.Project.Kind)
	switch {
	case o.Unavailable != "":
		// Said rather than shown as an empty list: a store that cannot be read
		// is not a store with nothing in it.
		b.WriteString(truncateRunes("cannot be read: "+o.Unavailable, maxLineWidth))
		return b.String()
	case o.Disabled:
		b.WriteString("memory is off for this run (--no-project-memory); nothing below was loaded\n")
	}
	fmt.Fprintf(&b, "%s · index %d/%d characters sent on every call\n",
		countLabelPlural(len(o.Memories), "memory", "memories"), o.IndexChars, o.IndexBudget)

	if p.Opened >= 0 && p.Opened < len(o.Memories) {
		b.WriteString("\n")
		b.WriteString(renderMemoryBody(o.Memories[p.Opened], maxLineWidth))
		return b.String()
	}

	if len(o.Memories) == 0 && len(o.Skipped) == 0 {
		b.WriteString("\nNothing is remembered for this project yet.")
		return b.String()
	}

	b.WriteString("\n")
	end := p.Offset + max(1, m.panelListBudget(slashInfoMemoryRows, slashInfoChromeLines))
	end = min(end, len(o.Memories))
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for i := p.Offset; i < end; i++ {
		cursor := "  "
		if i == p.Selected {
			cursor = "› "
		}
		mem := o.Memories[i]
		fmt.Fprintf(tw, "%s%s\t%s\t%s\n", cursor, mem.Name, mem.Type,
			truncateRunes(mem.Description, maxLineWidth/2))
	}
	_ = tw.Flush()
	if remaining := len(o.Memories) - end; remaining > 0 {
		fmt.Fprintf(&b, "  … %d more\n", remaining)
	}
	for _, s := range o.Skipped {
		b.WriteString("! " + truncateRunes(s.File+" is skipped and never loaded: "+s.Reason, maxLineWidth-2) + "\n")
	}
	return trimRowPadding(b.String())
}

// renderMemoryBody shows one memory as its file holds it. The body is where the
// reason lives, which is the half a description cannot carry.
func renderMemoryBody(mem localproject.Memory, maxLineWidth int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s)\n%s\n", mem.Name, mem.Type,
		truncateRunes(mem.Description, maxLineWidth))
	if !mem.UpdatedAt.IsZero() {
		fmt.Fprintf(&b, "written %s", mem.UpdatedAt.Local().Format("2006-01-02 15:04"))
		if mem.VerifiedAt != nil {
			fmt.Fprintf(&b, " · verified %s", mem.VerifiedAt.Format("2006-01-02"))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	for _, line := range strings.Split(strings.TrimSpace(mem.Body), "\n") {
		b.WriteString(truncateRunes(line, maxLineWidth) + "\n")
	}
	return b.String()
}

func (p *slashInfoPanel) memoryHints() string {
	if p.Opened >= 0 {
		return "esc: back to the list · tab/←/→: switch"
	}
	if len(p.Memory.Memories) > 0 {
		return "↑↓ select · enter: read it · tab/←/→: switch · esc: close"
	}
	return "tab/←/→: switch · esc: close"
}
