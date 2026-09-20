package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/icloudbb/buildmax/internal/config"
	corehook "github.com/icloudbb/buildmax/internal/core/hook"
	"github.com/icloudbb/buildmax/internal/core/plugin"
	"github.com/icloudbb/buildmax/internal/infra/git"
	"github.com/icloudbb/buildmax/internal/interface/appconnect"
	tools "github.com/icloudbb/buildmax/internal/tool"
)

// writePluginStatus reports what each plugin contributes and what happened to
// it. Resolution is run against the same layering a run uses, so what is
// printed is what would load rather than what the directory holds.
func writePluginStatus(ctx context.Context, w io.Writer, workspace, name string, fetch bool) error {
	if workspace == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("resolve workspace: %w", err)
		}
		workspace = wd
	}
	discovery := config.DiscoverPlugins()
	selected := discovery.Plugins
	if name != "" {
		found, ok := findPlugin(discovery, name)
		if !ok {
			return fmt.Errorf("no plugin named %q under %s", name, discovery.Dir)
		}
		selected = []config.DiscoveredPlugin{found}
	}
	if len(selected) == 0 {
		fmt.Fprintf(w, "No plugins installed. Clone one into %s to add it.\n", discovery.Dir)
		return nil
	}

	loadable := discovery.Loadable()
	skills := tools.ResolveSkills(config.SkillSources(workspace, loadable))
	agents, err := tools.ResolveAgentDefs(config.AgentDefSources(workspace, loadable))
	if err != nil {
		return fmt.Errorf("resolve subagents: %w", err)
	}
	mcpRes, err := config.ResolveMCPConfig(workspace, loadable)
	if err != nil {
		return fmt.Errorf("resolve mcp servers: %w", err)
	}
	shadowed := append(append([]plugin.Shadowed(nil), skills.Shadowed...), agents.Shadowed...)
	shadowed = append(shadowed, mcpRes.Shadowed...)
	crossFindings := append(append([]plugin.Finding(nil), skills.Findings...), agents.Findings...)
	crossFindings = append(crossFindings, mcpRes.Findings...)

	for i, p := range selected {
		if i > 0 {
			fmt.Fprintln(w)
		}
		writeOnePluginStatus(ctx, w, p, shadowed, crossFindings, fetch)
	}
	if discovery.StateErr != nil {
		fmt.Fprintf(w, "\nPlugin state could not be read, so provenance and disabled state are unknown: %v\n",
			discovery.StateErr)
	}
	return nil
}

func writeOnePluginStatus(ctx context.Context, w io.Writer, p config.DiscoveredPlugin,
	shadowed []plugin.Shadowed, crossFindings []plugin.Finding, fetch bool,
) {
	fmt.Fprintf(w, "%s (%s)\n", p.Manifest.DisplayTitle(), pluginStateLabel(p))
	if d := p.Manifest.Description; d != "" {
		fmt.Fprintf(w, "  %s\n", d)
	}
	fmt.Fprintf(w, "  directory:  %s\n", p.Path)
	writePluginSource(ctx, w, p, fetch)

	if v := p.Manifest.MinBuildmaxVersion; v != "" {
		fmt.Fprintf(w, "  requires:   BuildMax %s or newer%s\n", v, boundNote(v))
	}
	writePluginContributions(w, p)
	writePluginShadowed(w, p.Name(), shadowed)
	writePluginEnv(w, p.Manifest)

	findings := append([]plugin.Finding(nil), p.Findings...)
	findings = append(findings, findingsFor(crossFindings, p.Name())...)
	if len(findings) > 0 {
		fmt.Fprintln(w, "  problems:")
		for _, f := range findings {
			fmt.Fprintf(w, "    %s\n", f.String())
		}
	}
}

// boundNote says when this build does not satisfy a plugin's bound, and when it
// could not be compared at all.
func boundNote(bound string) string {
	min, err := plugin.ParseVersion(bound)
	if err != nil {
		return " (unreadable bound)"
	}
	client := plugin.ParseClientVersion(config.Version)
	switch {
	case !client.Known:
		return fmt.Sprintf(" (this build reports %q, so the check was skipped)", config.Version)
	case !client.Satisfies(min):
		return fmt.Sprintf(" — this build is %s", client.Version)
	default:
		return ""
	}
}

func writePluginSource(ctx context.Context, w io.Writer, p config.DiscoveredPlugin, fetch bool) {
	fmt.Fprintf(w, "  source:     %s\n", pluginSourceLabel(p))
	switch {
	case p.State.Source == config.PluginSourceMarketplace:
		if p.State.MarketplaceServer != "" {
			fmt.Fprintf(w, "  server:     %s\n", p.State.MarketplaceServer)
		}
		if p.State.Digest != "" {
			fmt.Fprintf(w, "  digest:     %s\n", p.State.Digest)
		}
	case p.Source() == config.PluginSourceRepository:
		if fetch {
			// The only path here that reaches the network, and only because it
			// was asked for by name.
			if err := git.Fetch(ctx, p.Path); err != nil {
				fmt.Fprintf(w, "  fetch:      failed: %v\n", err)
			}
		}
		if url := git.ReadRemoteURL(ctx, p.Path); url != "" {
			fmt.Fprintf(w, "  remote:     %s\n", url)
		}
		st, err := git.ReadStatus(ctx, p.Path)
		if err != nil {
			fmt.Fprintf(w, "  checkout:   could not be read: %v\n", err)
			return
		}
		fmt.Fprintf(w, "  checkout:   %s at %s%s\n", branchLabel(st), shortCommit(st.Commit), dirtySuffix(st.Dirty))
		writeDivergence(w, st, fetch)
	}
}

func branchLabel(st git.Status) string {
	if st.Detached {
		return "detached HEAD"
	}
	if st.Branch == "" {
		return "no branch"
	}
	return st.Branch
}

func shortCommit(commit string) string {
	if commit == "" {
		return "no commit yet"
	}
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}

func dirtySuffix(dirty bool) string {
	if dirty {
		return ", with uncommitted changes"
	}
	return ""
}

// writeDivergence reports how far a checkout has drifted, and how stale that
// answer is. Counts come from the tracking ref as it is known locally, so
// without --fetch they are only as current as the last fetch was.
func writeDivergence(w io.Writer, st git.Status, fetched bool) {
	if !st.HasUpstream {
		return
	}
	staleness := " (as of the last fetch; pass --fetch to update)"
	if fetched {
		staleness = ""
	}
	switch {
	case st.Behind > 0 && st.Ahead > 0:
		fmt.Fprintf(w, "  drift:      %d behind, %d ahead of its upstream%s\n", st.Behind, st.Ahead, staleness)
	case st.Behind > 0:
		fmt.Fprintf(w, "  drift:      %d behind its upstream%s\n", st.Behind, staleness)
	case st.Ahead > 0:
		fmt.Fprintf(w, "  drift:      %d ahead of its upstream%s\n", st.Ahead, staleness)
	case fetched:
		fmt.Fprintln(w, "  drift:      up to date with its upstream")
	}
}

// writePluginContributions lists what the directory ships, resolved on its own
// so the list is this plugin's rather than the merged result.
func writePluginContributions(w io.Writer, p config.DiscoveredPlugin) {
	only := []config.DiscoveredPlugin{p}
	var lines []string
	if names := ownSkillNames(p); len(names) > 0 {
		lines = append(lines, "  skills:     "+strings.Join(names, ", "))
	}
	if names := ownAgentNames(p); len(names) > 0 {
		lines = append(lines, "  subagents:  "+strings.Join(names, ", "))
	}
	if res, err := config.ResolveMCPConfig("", only); err == nil && res.Config != nil {
		ids := make([]string, 0, len(res.Config.MCPServers))
		for id := range res.Config.MCPServers {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		lines = append(lines, "  mcp:        "+strings.Join(ids, ", "))
	}
	if events := ownHookEvents(p); len(events) > 0 {
		lines = append(lines, "  hooks:      "+strings.Join(events, ", "))
	}
	if connector, err := appconnect.Load(p.Path); err == nil {
		names := make([]string, 0, len(connector.Operations))
		for name, operation := range connector.Operations {
			names = append(names, name+" ("+operation.Effect+")")
		}
		sort.Strings(names)
		lines = append(lines, "  connector:  "+strings.Join(names, ", "))
	}
	if len(lines) == 0 {
		fmt.Fprintln(w, "  contributes nothing this build recognises")
		return
	}
	for _, l := range lines {
		fmt.Fprintln(w, l)
	}
}

func ownSkillNames(p config.DiscoveredPlugin) []string {
	res := tools.ResolveSkills([]plugin.Source{{Dir: filepath.Join(p.Path, config.PluginSkillsSubdir)}})
	names := make([]string, 0, len(res.Entries))
	for _, e := range res.Entries {
		names = append(names, e.Name)
	}
	return names
}

func ownAgentNames(p config.DiscoveredPlugin) []string {
	res, err := tools.ResolveAgentDefs([]plugin.Source{{Dir: filepath.Join(p.Path, config.PluginAgentsSubdir)}})
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(res.Defs))
	for _, d := range res.Defs {
		names = append(names, d.Name)
	}
	return names
}

func ownHookEvents(p config.DiscoveredPlugin) []string {
	res := config.ResolvePluginHooks([]config.DiscoveredPlugin{p})
	var events []string
	for _, e := range corehook.EventNames() {
		if n := len(res.Config.Entries(e)); n > 0 {
			events = append(events, fmt.Sprintf("%s (%d)", e, n))
		}
	}
	return events
}

// writePluginShadowed names the definitions a higher layer replaced. Without
// this the plugin would read as fully active while part of it never loads.
func writePluginShadowed(w io.Writer, name string, shadowed []plugin.Shadowed) {
	var lines []string
	for _, s := range shadowed {
		if s.Loser.Layer == plugin.LayerPlugin && s.Loser.Plugin == name {
			lines = append(lines, fmt.Sprintf("    %s — overridden by %s", s.Name, s.Winner))
		}
	}
	if len(lines) == 0 {
		return
	}
	sort.Strings(lines)
	fmt.Fprintln(w, "  shadowed:")
	for _, l := range lines {
		fmt.Fprintln(w, l)
	}
}

// writePluginEnv reports the declared environment contract and which of it is
// missing here, which is the most common reason a plugin looks installed and
// does not work.
func writePluginEnv(w io.Writer, m plugin.Manifest) {
	if len(m.Env) == 0 {
		return
	}
	fmt.Fprintln(w, "  environment:")
	for _, e := range m.Env {
		state := "set"
		if _, ok := os.LookupEnv(e.Name); !ok {
			state = "not set"
			if e.Required {
				state = "NOT SET, required"
			}
		}
		line := fmt.Sprintf("    %s — %s", e.Name, state)
		if e.Description != "" {
			line += ": " + e.Description
		}
		fmt.Fprintln(w, line)
	}
}

// findingsFor picks the cross-plugin findings that concern this plugin, so a
// collision is reported against every side of it.
func findingsFor(findings []plugin.Finding, name string) []plugin.Finding {
	var out []plugin.Finding
	for _, f := range findings {
		if f.Concerns(name) {
			out = append(out, f)
		}
	}
	return out
}
