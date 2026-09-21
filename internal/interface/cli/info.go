package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/icloudbb/buildmax/internal/agentapp"
	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/core/agent"
	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/icloudbb/buildmax/internal/core/session"
	"github.com/icloudbb/buildmax/internal/infra/localprojectstore"
	"github.com/icloudbb/buildmax/internal/util"

	"github.com/spf13/cobra"
)

// maxStatsTools bounds the per-tool table. The list is sorted by weight, so the
// tail is the part nobody reads; a session with sixty tools should still print
// something a person can take in.
const maxStatsTools = 12

func newInfoCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "info [session-id]",
		Short: "Show what a session spent and did, and what its project remembers",
		Long: `Show one session's statistics and the memories of the project it belongs to.

With no argument, the most recent session by creation time is used.

Tokens and cost come from the session file, which accumulated them turn by turn
at the rates in force for each. Timings, per-tool detail, and the delegated
breakdown come from the session's run traces; where no trace was written, those
lines say so rather than reporting zero.

The memories are the project's, not the session's: every session of that project
sees them. This lists each one's name and description, which is what a run
carries on every model call; read a body by opening its file at the path printed
below, or in the TUI with ` + "`/info`" + `.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var id string
			if len(args) == 1 {
				id = args[0]
			}
			asJSON, _ := cmd.Flags().GetBool("json")
			return runInfo(cmd.Context(), os.Stdout, id, asJSON)
		},
	}
	c.Flags().Bool("json", false, "emit the report as JSON instead of a table")
	return c
}

// sessionInfo is what `buildmax info` reports: one session's statistics and
// fork tree, plus the memories of the project that session belongs to.
//
// These are separate fields rather than a merged object because they have
// different lifetimes and owners. Statistics belong to this session, the fork
// tree is a projection of user-session provenance, and memories outlive the
// session and are shared by every session of the project.
type sessionInfo struct {
	Stats         agentapp.SessionStats  `json:"stats"`
	ForkTree      *agentapp.ForkTreeNode `json:"fork_tree,omitempty"`
	ForkTreeError string                 `json:"fork_tree_error,omitempty"`
	Memory        *memoryInfoReport      `json:"project_memory,omitempty"`
}

// memoryInfoReport is the JSON shape of the memory half. Bodies are deliberately
// absent: this is a listing, and a listing that inlined twenty bodies would be
// unreadable in a terminal and a surprise in a pipe. The path is printed so a
// reader can open one.
type memoryInfoReport struct {
	ProjectID   string              `json:"project_id"`
	ProjectName string              `json:"project_name"`
	Directory   string              `json:"directory"`
	IndexChars  int                 `json:"index_chars"`
	IndexBudget int                 `json:"index_budget"`
	Unavailable string              `json:"unavailable,omitempty"`
	Memories    []memoryInfoEntry   `json:"memories"`
	Skipped     []memoryInfoSkipped `json:"skipped,omitempty"`
}

type memoryInfoEntry struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	UpdatedAt   string `json:"updated_at,omitempty"`
	VerifiedAt  string `json:"verified_at,omitempty"`
	Chars       int    `json:"chars"`
}

type memoryInfoSkipped struct {
	File   string `json:"file"`
	Reason string `json:"reason"`
}

func runInfo(ctx context.Context, w io.Writer, id string, asJSON bool) error {
	sessionsDir := config.SessionsDir()
	if id == "" {
		// Scoped like --continue, and for the same reason: "the last session"
		// asked from inside a repository means the last one here, not whichever
		// repository was touched most recently on this machine.
		project, err := currentProject(ctx, "")
		if err != nil {
			return fmt.Errorf("resolve project: %w", err)
		}
		list, err := agentapp.NewSessionManager(sessionsDir).List()
		if err != nil {
			return fmt.Errorf("load session list: %w", err)
		}
		last := latestSessionItem(filterByProject(list, project.ID))
		if last == nil {
			return fmt.Errorf("no sessions yet in %s; run one with -p PROMPT or start the TUI", project.Name)
		}
		id = last.ID
	}

	stats, err := agentapp.LoadSessionStats(sessionsDir, id)
	if err != nil {
		return err
	}
	report := sessionInfo{Stats: stats, Memory: memoryReportFor(ctx, sessionsDir, id)}
	report.ForkTree, err = agentapp.NewSessionManager(sessionsDir).ForkTree(id)
	if err != nil {
		report.ForkTreeError = err.Error()
	}
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	writeStats(w, stats)
	writeForkTree(w, report.ForkTree, report.ForkTreeError)
	writeMemoryReport(w, report.Memory)
	return nil
}

// memoryReportFor loads the memories of the project the named session belongs
// to, or nil when it belongs to none -- a session written before Projects
// existed, or by a worker. Nothing is inferred from the session's directory:
// that is the path coincidence the Project id replaced.
func memoryReportFor(ctx context.Context, sessionsDir, sessionID string) *memoryInfoReport {
	loaded, err := agentapp.NewSessionManager(sessionsDir).Load(sessionID, session.LoadMetaOnly)
	if err != nil || loaded.Meta.ProjectID == "" {
		return nil
	}
	manager := agentapp.NewProjectManager(config.ProjectsDir())
	project, err := manager.Store().Get(ctx, loaded.Meta.ProjectID)
	if err != nil {
		return &memoryInfoReport{
			ProjectID:   loaded.Meta.ProjectID,
			Unavailable: err.Error(),
			IndexBudget: agent.MaxMemoryIndexChars,
			Memories:    []memoryInfoEntry{},
		}
	}
	overview := manager.MemoryOverviewFor(ctx, project)

	report := &memoryInfoReport{
		ProjectID:   project.ID,
		ProjectName: project.Name,
		Directory: filepath.Join(config.ProjectsDir(), project.ID,
			localprojectstore.MemoryDir),
		IndexChars:  overview.IndexChars,
		IndexBudget: overview.IndexBudget,
		Unavailable: overview.Unavailable,
		Memories:    make([]memoryInfoEntry, 0, len(overview.Memories)),
	}
	for _, m := range overview.Memories {
		entry := memoryInfoEntry{
			Name:        m.Name,
			Type:        string(m.Type),
			Description: m.Description,
			Chars:       utf8.RuneCountInString(m.Body),
		}
		if !m.UpdatedAt.IsZero() {
			entry.UpdatedAt = m.UpdatedAt.Format(time.RFC3339)
		}
		if m.VerifiedAt != nil {
			entry.VerifiedAt = m.VerifiedAt.Format("2006-01-02")
		}
		report.Memories = append(report.Memories, entry)
	}
	for _, s := range overview.Skipped {
		report.Skipped = append(report.Skipped, memoryInfoSkipped{File: s.File, Reason: s.Reason})
	}
	return report
}

// writeMemoryReport prints the memory half. A session with no Project prints
// nothing rather than an empty heading: it has no memories to be missing.
func writeMemoryReport(w io.Writer, r *memoryInfoReport) {
	if r == nil {
		return
	}
	fmt.Fprintf(w, "\nProject memory — %s\n", r.ProjectName)
	if r.Unavailable != "" {
		fmt.Fprintf(w, "  cannot be read: %s\n", r.Unavailable)
		return
	}
	fmt.Fprintf(w, "  %s, index %d/%d characters sent on every call\n",
		countLabelPlural(len(r.Memories), "memory", "memories"), r.IndexChars, r.IndexBudget)
	if r.Directory != "" {
		fmt.Fprintf(w, "  %s\n", r.Directory)
	}
	if len(r.Memories) > 0 {
		fmt.Fprintln(w)
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, m := range r.Memories {
			verified := ""
			if m.VerifiedAt != "" {
				verified = " · verified " + m.VerifiedAt
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s%s\n", m.Name, m.Type, m.Description, verified)
		}
		_ = tw.Flush()
	}
	for _, s := range r.Skipped {
		fmt.Fprintf(w, "  ! %s is skipped and never loaded: %s\n", s.File, s.Reason)
	}
}

func writeStats(w io.Writer, s agentapp.SessionStats) {
	title := s.Title
	if title == "" {
		title = "(untitled)"
	}
	fmt.Fprintf(w, "%s\n", title)
	fmt.Fprintf(w, "Session:   %s\n", s.ID)
	if s.Workspace != "" {
		fmt.Fprintf(w, "Workspace: %s\n", s.Workspace)
	}
	fmt.Fprintf(w, "Started:   %s\n", s.CreatedAt.Local().Format(time.RFC3339))
	if len(s.Runs.Models) > 0 {
		fmt.Fprintf(w, "Models:    %s\n", strings.Join(s.Runs.Models, ", "))
	}

	writeStatsSpend(w, s)
	writeStatsContext(w, s)
	writeStatsWork(w, s)
	writeStatsTools(w, s)
	writeStatsCaveats(w, s)
}

func writeStatsSpend(w io.Writer, s agentapp.SessionStats) {
	fmt.Fprintf(w, "\nSpend\n")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  Tokens (in/out)\t%s / %s\n",
		formatCount(s.Usage.PromptTokens), formatCount(s.Usage.CompletionTokens))
	// Only where a provider reported cached tokens: "0 / 0" on a provider that
	// reports nothing would claim a miss nobody measured.
	if s.Usage.CacheReadTokens > 0 || s.Usage.CacheWriteTokens > 0 {
		fmt.Fprintf(tw, "  Cache (read/write)\t%s / %s\n",
			formatCount(s.Usage.CacheReadTokens), formatCount(s.Usage.CacheWriteTokens))
	}
	if s.Cost == nil {
		fmt.Fprintf(tw, "  Cost\tnot priced — no model in this session had rates configured\n")
	} else {
		fmt.Fprintf(tw, "  Cost\t%s %s\n", cllm.FormatAmount(s.Cost.Total), s.Cost.Currency)
		fmt.Fprintf(tw, "    input / cache read / cache write / output\t%s / %s / %s / %s\n",
			cllm.FormatAmount(s.Cost.Uncached), cllm.FormatAmount(s.Cost.CacheRead),
			cllm.FormatAmount(s.Cost.CacheWrite), cllm.FormatAmount(s.Cost.Output))
		if saved, ok := s.CacheSaved(); ok {
			fmt.Fprintf(tw, "    saved by caching\t%s of %s uncached\n",
				cllm.FormatAmount(saved), cllm.FormatAmount(s.Cost.Baseline))
		} else if s.Cost.Baseline > 0 {
			fmt.Fprintf(tw, "    saved by caching\tnothing — this session paid more than it would have uncached\n")
		}
	}
	if d := s.Runs.Delegated; d != nil && d.Runs > 0 {
		line := fmt.Sprintf("  Of which delegated\t%s, %s in / %s out",
			countLabel(d.Runs, "run"), formatCount(d.PromptTokens), formatCount(d.CompletionTokens))
		if d.Cost != nil {
			line += fmt.Sprintf(", %s %s", cllm.FormatAmount(d.Cost.Total), d.Cost.Currency)
		}
		fmt.Fprintln(tw, line)
	}
	_ = tw.Flush()
}

func writeStatsContext(w io.Writer, s agentapp.SessionStats) {
	fmt.Fprintf(w, "\nContext\n")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if share, ok := s.ContextPeakShare(); ok {
		fmt.Fprintf(tw, "  Peak window use\t%s of %s (%.0f%%)\n",
			formatCount(s.Runs.PeakContextTokens), formatCount(s.Runs.ContextWindow), share*100)
	} else {
		fmt.Fprintf(tw, "  Peak window use\tnot recorded\n")
	}
	fmt.Fprintf(tw, "  Compactions\t%d", s.Runs.Compactions)
	if s.Conversation.CompactedMessages > 0 {
		fmt.Fprintf(tw, " (%s summarized away)", countLabel(s.Conversation.CompactedMessages, "message"))
	}
	fmt.Fprintln(tw)
	// The share is only meaningful where a provider reported cache usage at
	// all; a provider that reports nothing has not reported a miss.
	if s.Usage.PromptTokens > 0 && (s.Usage.CacheReadTokens > 0 || s.Usage.CacheWriteTokens > 0) {
		fmt.Fprintf(tw, "  Prompt served from cache\t%.0f%%\n",
			float64(s.Usage.CacheReadTokens)/float64(s.Usage.PromptTokens)*100)
	}
	c := s.Conversation
	fmt.Fprintf(tw, "  History bytes (text / tool output)\t%s / %s\n",
		formatBytes(c.TextBytes), formatBytes(c.ToolResultBytes))
	_ = tw.Flush()
}

func writeStatsWork(w io.Writer, s agentapp.SessionStats) {
	c := s.Conversation
	fmt.Fprintf(w, "\nWork\n")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  Your messages\t%d", c.UserMessages)
	if c.BackgroundMessages > 0 {
		fmt.Fprintf(tw, " (plus %s)", countLabel(c.BackgroundMessages, "background event"))
	}
	fmt.Fprintln(tw)
	fmt.Fprintf(tw, "  Assistant turns\t%d\n", c.AssistantTurns)
	fmt.Fprintf(tw, "  Tool calls\t%d\n", c.ToolCalls)
	if s.Runs.ToolFailures > 0 {
		fmt.Fprintf(tw, "  Calls that could not complete\t%d (a command exiting non-zero is not one)\n",
			s.Runs.ToolFailures)
	}
	if s.Runs.ToolDenials > 0 {
		fmt.Fprintf(tw, "  Calls denied\t%d\n", s.Runs.ToolDenials)
	}
	if c.Notes > 0 || c.Todos > 0 {
		fmt.Fprintf(tw, "  Notes / todos\t%d / %d\n", c.Notes, c.Todos)
	}

	if s.Runs.Runs == 0 {
		fmt.Fprintf(tw, "  Runs\tno trace recorded, so timings are unavailable\n")
		_ = tw.Flush()
		return
	}
	fmt.Fprintf(tw, "  Runs\t%d", s.Runs.Runs)
	if s.Runs.Subagents > 0 {
		fmt.Fprintf(tw, " (plus %s)", countLabel(s.Runs.Subagents, "subagent run"))
	}
	fmt.Fprintln(tw)
	fmt.Fprintf(tw, "  Time spent waiting\t%s\n", util.FormatDuration(s.Runs.Wall))
	if model, ok := s.ModelTime(); ok {
		fmt.Fprintf(tw, "    model / tools\t%s / %s\n",
			util.FormatDuration(model), util.FormatDuration(s.Runs.ToolWall))
	} else if s.Runs.ToolWall > 0 {
		// Parallel tool execution can make summed tool time exceed the wall
		// clock. Reporting a negative model time would be worse than saying
		// the split does not divide.
		fmt.Fprintf(tw, "    tools\t%s (overlapping, so it does not subtract from the wall clock)\n",
			util.FormatDuration(s.Runs.ToolWall))
	}
	_ = tw.Flush()
}

func writeStatsTools(w io.Writer, s agentapp.SessionStats) {
	tools := mergeToolStats(s)
	if len(tools) == 0 {
		return
	}
	shown := tools
	if len(shown) > maxStatsTools {
		shown = shown[:maxStatsTools]
	}
	// The note column is dropped when nothing has one, so a clean session does
	// not print a header for an empty column.
	notes := false
	for _, t := range shown {
		if t.Note != "" {
			notes = true
			break
		}
	}

	fmt.Fprintf(w, "\nTools, heaviest first\n")
	var table strings.Builder
	tw := tabwriter.NewWriter(&table, 0, 0, 2, ' ', 0)
	header := "  TOOL\tCALLS\tOUTPUT\tTIME"
	if notes {
		header += "\tNOTE"
	}
	fmt.Fprintln(tw, header)
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
		row := fmt.Sprintf("  %s\t%d\t%s\t%s", name, t.Calls, output, spent)
		if notes {
			row += "\t" + t.Note
		}
		fmt.Fprintln(tw, row)
	}
	_ = tw.Flush()
	fmt.Fprint(w, trimRowPadding(table.String()))
	if len(tools) > len(shown) {
		fmt.Fprintf(w, "  … and %d more; --json lists them all\n", len(tools)-len(shown))
	}
}

// statsTool is one tool's row, joining what the history knows (how many bytes
// it put back into the context) with what the trace knows (how long it took,
// and how it failed).
type statsTool struct {
	Name        string
	Calls       int
	ResultBytes int
	Wall        time.Duration
	Note        string
}

func mergeToolStats(s agentapp.SessionStats) []statsTool {
	rows := make(map[string]*statsTool)
	row := func(name string) *statsTool {
		r, ok := rows[name]
		if !ok {
			r = &statsTool{Name: name}
			rows[name] = r
		}
		return r
	}
	for _, t := range s.Conversation.Tools {
		r := row(t.Name)
		r.Calls = t.Calls
		r.ResultBytes = t.ResultBytes
	}
	for _, t := range s.Runs.Tools {
		r := row(t.Name)
		// The traces see subagent calls the parent's history never recorded,
		// so a trace count above the history's is the truth about what ran.
		if t.Calls > r.Calls {
			r.Calls = t.Calls
		}
		r.Wall = t.Wall
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
	out := make([]statsTool, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.ResultBytes != b.ResultBytes {
			return a.ResultBytes > b.ResultBytes
		}
		if a.Wall != b.Wall {
			return a.Wall > b.Wall
		}
		return a.Name < b.Name
	})
	return out
}

// statsCaveats names what these numbers do not cover. A total that silently
// dropped a killed run is worse than one that says it did.
//
// Shared by the command and the TUI panel. The two lay their reports out
// differently, but what they are allowed to claim is one answer, and a warning
// that appeared on only one surface would be a warning nobody trusted.
func statsCaveats(s agentapp.SessionStats) []string {
	var lines []string
	if s.CostIncomplete {
		lines = append(lines, "Part of this session ran against an unpriced model or a different currency, so the cost understates it.")
	}
	if s.Runs.Incomplete > 0 {
		lines = append(lines, fmt.Sprintf("%s ended without writing a trace end record — killed or crashed — so their timings are missing here.", countLabel(s.Runs.Incomplete, "run")))
	}
	if s.Runs.Failed > 0 {
		lines = append(lines, fmt.Sprintf("%s ended with an error.", countLabel(s.Runs.Failed, "run")))
	}
	if s.Conversation.ToolCalls > 0 && s.Runs.Runs == 0 {
		lines = append(lines, "No run trace was found for this session, so every timing above is unavailable rather than zero.")
	}
	return lines
}

func writeStatsCaveats(w io.Writer, s agentapp.SessionStats) {
	lines := statsCaveats(s)
	if len(lines) == 0 {
		return
	}
	fmt.Fprintln(w)
	for _, l := range lines {
		fmt.Fprintf(w, "! %s\n", l)
	}
}

// trimRowPadding strips the padding tabwriter leaves after a row whose last
// cell is empty. The column has to be there for the rows that do use it; the
// spaces it leaves behind on the rows that do not are noise in a terminal and
// in anything that diffs the output.
func trimRowPadding(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return strings.Join(lines, "\n")
}

// formatCount groups a token count so six- and seven-figure numbers stay
// readable at a glance.
func formatCount(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	s := fmt.Sprintf("%d", n)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

func formatBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
