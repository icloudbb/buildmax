package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/icloudbb/buildmax/internal/agentapp"
	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/core/agent"

	"github.com/spf13/cobra"
)

// newToolsCommand builds `buildmax tools`. See docs/design/tool-permissions.md §5.6.
func newToolsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tools",
		Short: "Inspect the tools available to the agent",
	}
	cmd.AddCommand(newToolsStatusCommand())
	return cmd
}

func newToolsStatusCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "status",
		Short: "Print each tool's access classification and resolved permission",
		RunE: func(cmd *cobra.Command, _ []string) error {
			workspace, _ := cmd.Flags().GetString("workspace")
			app, err := agentapp.NewAgentApp(agentapp.AppConfig{
				WorkspaceDir: workspace,
				EnableMCP:    false,
				// Match an interactive CLI run so the Browser tools appear when a
				// system browser is installed and are honestly absent when it is
				// not. Discovery only looks up the executable; it launches nothing.
				EnableBrowser:  true,
				SandboxSurface: config.SandboxSurfaceCLI,
				Policy:         agent.AllowAllPolicy(),
			})
			if err != nil {
				return err
			}
			defer app.Close()
			return writeToolsStatus(os.Stdout, app.ToolEntries(), app.PermissionRules(), app.PermissionIssues())
		},
	}
	c.Flags().String("workspace", "", "workspace directory (default: current directory)")
	return c
}

func writeToolsStatus(w io.Writer, entries []agentapp.ToolEntry, rules []config.PermissionEntry, issues []string) error {
	if len(entries) == 0 {
		fmt.Fprintln(w, "No tools are registered.")
		return nil
	}
	// The table is what an interactive run would do. Autonomous surfaces never
	// raise the category prompt, so stating one number for both would be a lie
	// in whichever direction it was rounded.
	fmt.Fprintln(w, "Resolved for an interactive run (CLI TUI, Desktop).")
	fmt.Fprintln(w)

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TOOL\tACCESS\tACTION\tSOURCE")
	for _, e := range entries {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", e.Name, e.Access, e.Action, e.Source)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	// Rules that name a dispatch target have no row above, so print them
	// separately rather than letting a configured rule look like it was dropped.
	var scoped []config.PermissionEntry
	for _, r := range rules {
		if isScopedRule(r.Key, entries) {
			scoped = append(scoped, r)
		}
	}
	if len(scoped) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Rules for dispatch targets:")
		tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "  RULE\tACTION\tSOURCE")
		for _, r := range scoped {
			fmt.Fprintf(tw, "  %s\t%s\t%s\n", r.Display, r.Action, r.Source)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}

	if len(issues) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Ignored — unknown action (use allow, ask, or deny):")
		for _, bad := range issues {
			fmt.Fprintf(w, "  %s\n", bad)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Argument-dependent tools resolve per call: Bash asks only for a risky")
	fmt.Fprintln(w, "command, and a read of a sensitive path asks whatever this table says.")
	fmt.Fprintln(w, "Configure with tools.permissions in settings.yaml.")
	return nil
}

// isScopedRule reports whether a configured key names something other than a
// registered tool. Case-insensitive for the reason in config.PermissionEntry.
func isScopedRule(key string, entries []agentapp.ToolEntry) bool {
	for _, e := range entries {
		if strings.EqualFold(e.Name, key) {
			return false
		}
	}
	return true
}
