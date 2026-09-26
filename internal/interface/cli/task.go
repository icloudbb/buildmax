package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/icloudbb/buildmax/internal/infra/workerclient"
	"github.com/icloudbb/buildmax/internal/interface/auth"
)

func newTaskCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Inspect tasks on the server",
		Long: "Read the tasks running on the BuildMax server you are signed in to,\n" +
			"such as one `buildmax agent trigger` started. Sign in with\n" +
			"`buildmax login` first.",
	}
	cmd.AddCommand(newTaskStatusCommand())
	return cmd
}

func newTaskStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status <task-id>",
		Short: "Show a task's status, and its output once it finishes",
		Long: "Prints a task's current status, and its output once the run is done.\n\n" +
			"In a local session it takes a task id; with --space the task is read\n" +
			"there, otherwise your spaces are searched for one that holds it. Inside\n" +
			"a worker run it takes no id — it reports this run's own status, including\n" +
			"whether a stop has been requested.",
		Args: cobra.MaximumNArgs(1),
		RunE: runTaskStatus,
	}
	cmd.Flags().String("space", "", "the space that holds the task; searched across your spaces when omitted")
	return cmd
}

func runTaskStatus(cmd *cobra.Command, args []string) error {
	// Inside a worker run the only run it may read is its own; the worker route
	// names it from the token, so an id here would ask after another run.
	if wb := inWorkerRun(); wb != nil {
		if len(args) > 0 {
			return fmt.Errorf("inside a run, `task status` reports this run; drop the task id")
		}
		run, err := workerclient.GetWorkerTaskRun(cmd.Context(), wb.cfg, wb.taskRunID)
		if err != nil {
			return fmt.Errorf("read run: %w", err)
		}
		if run == nil || run.Run == nil {
			return fmt.Errorf("this run was not found")
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "%s  %s\n", run.Run.ID, run.Run.Status)
		if run.CancelRequested {
			fmt.Fprintln(out, "a stop has been requested")
		}
		return nil
	}

	if len(args) == 0 {
		return fmt.Errorf("task id required: buildmax task status <task-id>")
	}
	serverURL, token, err := signedInServer(cmd)
	if err != nil {
		return err
	}
	space, _ := cmd.Flags().GetString("space")
	task, err := auth.ServerClient(serverURL).FindTask(cmd.Context(), token, space, args[0])
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%s  %s\n", task.ID, task.Status)
	if task.Title != "" {
		fmt.Fprintf(out, "%s\n", oneLine(task.Title))
	}
	if task.Output != nil && strings.TrimSpace(*task.Output) != "" {
		fmt.Fprintf(out, "\n%s\n", strings.TrimSpace(*task.Output))
	}
	return nil
}
