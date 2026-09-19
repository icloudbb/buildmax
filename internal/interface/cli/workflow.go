package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/icloudbb/buildmax/internal/interface/client"
)

func newWorkflowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflow",
		Short: "Run workflows on the server",
		Long: "Start workflow runs on the BuildMax server you are signed in to, and\n" +
			"follow them. Authoring workflows stays in Portal; only a published\n" +
			"workflow can be run. Sign in with `buildmax login` first.",
	}
	cmd.AddCommand(newWorkflowListCommand())
	cmd.AddCommand(newWorkflowRunCommand())
	cmd.AddCommand(newWorkflowStatusCommand())
	return cmd
}

func newWorkflowListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List workflows and whether each can be run",
		Args:  cobra.NoArgs,
		RunE:  runWorkflowList,
	}
	cmd.Flags().String("space", "", "only this space; searched across your spaces when omitted")
	return cmd
}

func runWorkflowList(cmd *cobra.Command, _ []string) error {
	serverURL, token, err := signedInServer(cmd)
	if err != nil {
		return err
	}
	space, _ := cmd.Flags().GetString("space")
	c := client.NewClient(serverURL)

	var workflows []client.Workflow
	if space != "" {
		workflows, err = c.ListWorkflows(cmd.Context(), token, space)
		if err != nil {
			return err
		}
	} else {
		var problems []error
		workflows, problems = c.ListOwnedWorkflows(cmd.Context(), token)
		for _, p := range problems {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n", p)
		}
	}
	if len(workflows) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No workflows.")
		return nil
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "WORKFLOW\tSTATUS\tSPACE\tNAME")
	for _, wf := range workflows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", wf.ID, wf.Status, wf.SpaceID, oneLine(wf.Name))
	}
	_ = w.Flush()
	return nil
}

func newWorkflowRunCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <workflow>",
		Short: "Start a run of a published workflow",
		Long: "Starts a run of a workflow by name or id, and prints the run it created.\n" +
			"Only a published workflow can run. Follow the run with\n" +
			"`buildmax workflow status <id>`.\n\n" +
			"--input passes JSON that satisfies the workflow's input schema, and is\n" +
			"only accepted when the workflow declares one. --issue links the run to\n" +
			"an issue, required when a step declares issue access. With --space the\n" +
			"workflow is looked up there; without it, your spaces are searched and an\n" +
			"ambiguous name is refused.",
		Args: cobra.ExactArgs(1),
		RunE: runWorkflowRun,
	}
	cmd.Flags().String("space", "", "the space that holds the workflow; searched across your spaces when omitted")
	cmd.Flags().String("input", "", "JSON input for the run, when the workflow declares an input schema")
	cmd.Flags().String("issue", "", "link the run to this issue id")
	return cmd
}

func runWorkflowRun(cmd *cobra.Command, args []string) error {
	serverURL, token, err := signedInServer(cmd)
	if err != nil {
		return err
	}
	space, _ := cmd.Flags().GetString("space")
	input, _ := cmd.Flags().GetString("input")
	issue, _ := cmd.Flags().GetString("issue")
	c := client.NewClient(serverURL)

	wf, err := c.FindWorkflow(cmd.Context(), token, space, args[0])
	if err != nil {
		return err
	}
	if wf.Status != "published" {
		return fmt.Errorf("workflow %s is %s, not published; only a published workflow can run", wf.Name, wf.Status)
	}
	run, err := c.RunWorkflow(cmd.Context(), token, wf.SpaceID, wf.ID, input, issue)
	if err != nil {
		return fmt.Errorf("run workflow: %w", err)
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Started %s in space %s.\n", wf.Name, wf.SpaceID)
	fmt.Fprintf(out, "run %s (%s)\n", run.ID, run.Status)
	fmt.Fprintf(out, "Check it: buildmax workflow status %s\n", run.ID)
	return nil
}

func newWorkflowStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status <run-id>",
		Short: "Show a workflow run's status",
		Args:  cobra.ExactArgs(1),
		RunE:  runWorkflowStatus,
	}
	cmd.Flags().String("space", "", "the space that holds the run; searched across your spaces when omitted")
	return cmd
}

func runWorkflowStatus(cmd *cobra.Command, args []string) error {
	serverURL, token, err := signedInServer(cmd)
	if err != nil {
		return err
	}
	space, _ := cmd.Flags().GetString("space")
	run, err := client.NewClient(serverURL).FindWorkflowRun(cmd.Context(), token, space, args[0])
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%s  %s\n", run.ID, run.Status)
	if run.ErrorMessage != nil && *run.ErrorMessage != "" {
		fmt.Fprintf(out, "error: %s\n", *run.ErrorMessage)
	}
	return nil
}
