package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/icloudbb/buildmax/internal/interface/client"
)

func newAgentCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Trigger agents on the server",
		Long: "Start agent runs on the BuildMax server you are signed in to.\n\n" +
			"Managing agents — creating them, editing instructions, revisions — stays\n" +
			"in Portal. This triggers one. Sign in with `buildmax login` first.",
	}
	cmd.AddCommand(newAgentTriggerCommand())
	return cmd
}

func newAgentTriggerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trigger <agent>",
		Short: "Start an agent run and print the task it created",
		Long: "Triggers an agent by name or id with an input, and prints the task it\n" +
			"created. Creating the task starts the agent's first run; follow it with\n" +
			"`buildmax task status <id>`.\n\n" +
			"Give the input with -m, or leave it off to read it from stdin. An agent\n" +
			"lives in a space: with --space it is looked up there; without it, your\n" +
			"spaces are searched and a name found in more than one is refused so a\n" +
			"run never starts against the wrong agent.",
		Args: cobra.ExactArgs(1),
		RunE: runAgentTrigger,
	}
	cmd.Flags().StringP("message", "m", "", "the input for the run; read from stdin when omitted")
	cmd.Flags().String("space", "", "the space that holds the agent; searched across your spaces when omitted")
	return cmd
}

func runAgentTrigger(cmd *cobra.Command, args []string) error {
	input, _ := cmd.Flags().GetString("message")
	if strings.TrimSpace(input) == "" {
		read, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return fmt.Errorf("read input from stdin: %w", err)
		}
		input = string(read)
	}
	if strings.TrimSpace(input) == "" {
		return fmt.Errorf("empty input: give the run an input with -m or on stdin")
	}
	serverURL, token, err := signedInServer(cmd)
	if err != nil {
		return err
	}
	space, _ := cmd.Flags().GetString("space")
	c := client.NewClient(serverURL)
	agent, err := c.FindAgent(cmd.Context(), token, space, args[0])
	if err != nil {
		return err
	}
	task, err := c.TriggerAgent(cmd.Context(), token, agent.SpaceID, agent.ID, input)
	if err != nil {
		return fmt.Errorf("trigger agent: %w", err)
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Triggered %s in space %s.\n", agent.Name, agent.SpaceID)
	fmt.Fprintf(out, "task %s (%s)\n", task.ID, task.Status)
	fmt.Fprintf(out, "Check it: buildmax task status %s\n", task.ID)
	return nil
}
