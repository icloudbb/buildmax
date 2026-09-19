package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/icloudbb/buildmax/internal/interface/client"
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
			"With --space the task is read there; without it, your spaces are\n" +
			"searched for one that holds it.",
		Args: cobra.ExactArgs(1),
		RunE: runTaskStatus,
	}
	cmd.Flags().String("space", "", "the space that holds the task; searched across your spaces when omitted")
	return cmd
}

func runTaskStatus(cmd *cobra.Command, args []string) error {
	serverURL, token, err := signedInServer(cmd)
	if err != nil {
		return err
	}
	space, _ := cmd.Flags().GetString("space")
	task, err := client.NewClient(serverURL).FindTask(cmd.Context(), token, space, args[0])
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
