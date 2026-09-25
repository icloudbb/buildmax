// Package cli: root and subcommands for the BuildMax CLI.
package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/interface/auth"

	"github.com/google/uuid"
	"github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/spf13/cobra"
)

var rootLong = fmt.Sprintf(`BuildMax – AI Agent CLI

  buildmax                    Start the TUI (new session)
  buildmax -r ID              Start the TUI with session ID
  buildmax -c                 Resume the most recent session (TUI or print mode)
  buildmax -p QUERY           Send QUERY to the LLM and print the response (no TUI)
  buildmax -r ID -p QUERY     Resume session ID, send QUERY, then print and save

Sessions:
  Each run with -p saves the session under the app data directory (see BUILDMAX_HOME or ~/.buildmax).
  Use -r/--resume <session-id> to continue a previous session (TUI or print mode); value must be a valid UUID.
  Use -c/--continue to resume the most recent session (by creation time); -r takes precedence if both are set.
  Use --session-id <uuid> to use a specific session ID (load if exists, else create); value must be a valid UUID.

Configuration:
  Run "buildmax init" to create a starter settings.yaml.
  Run "buildmax doctor" to check local setup before the first real run.
  Models are configured in BUILDMAX_HOME/settings.yaml (default ~/.buildmax).
  The first entry under models: is the default; select another with --model.
  Default model when none is configured: %s
`, config.DefaultModel)

// NewRootCommand creates and returns the root cobra command for BuildMax.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "buildmax",
		Short:         "BuildMax – AI Agent CLI",
		Long:          rootLong,
		RunE:          runRoot,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	// Cobra auto-adds `completion` to any root with subcommands. The generated
	// script is free to keep working, but the entry reads as noise next to ten
	// commands people can remember, so it stays out of the help listing.
	root.CompletionOptions.HiddenDefaultCmd = true
	root.Flags().BoolP("help", "h", false, "help for buildmax")
	addRunFlags(root)
	root.Flags().BoolP("version", "v", false, "print version and exit")
	root.AddCommand(newInitCommand())
	root.AddCommand(newDoctorCommand())
	root.AddCommand(newVersionCommand())
	root.AddCommand(newLoginCommand())
	root.AddCommand(newLogoutCommand())
	root.AddCommand(newMeCommand())
	root.AddCommand(newSandboxCommand())
	root.AddCommand(newToolsCommand())
	root.AddCommand(newIssueCommand())
	root.AddCommand(newAgentCommand())
	root.AddCommand(newTaskCommand())
	root.AddCommand(newArtifactCommand())
	root.AddCommand(newWorkflowCommand())
	root.AddCommand(newAdminCommand())
	root.AddCommand(newPluginCommand())
	root.AddCommand(newConnectCommand())
	root.AddCommand(newAppCommand())
	root.AddCommand(newMCPCommand())
	root.AddCommand(newModelsCommand())
	root.AddCommand(newInfoCommand())
	root.AddCommand(newUsageCommand())
	root.AddCommand(newProjectCommand())
	groupTopLevelCommands(root)
	return root
}

// Command groups shown in `buildmax --help`. Server commands reach the signed-in
// server; local commands do not. See docs/design/agent-bridge-cli.md section 7.
const (
	groupServer = "server"
	groupLocal  = "local"
)

// serverCommandNames is the set of top-level commands that reach the BuildMax
// server, so a reader can see at a glance which need a login. Everything else
// registered on the root is local.
var serverCommandNames = map[string]bool{
	"login": true, "logout": true, "me": true,
	"issue": true, "agent": true, "task": true, "artifact": true, "workflow": true,
	"admin": true, "plugin": true, "usage": true,
}

// groupTopLevelCommands sorts the registered commands into the two help groups.
// It assigns groups after registration, rather than at each AddCommand call, so
// the registration keeps the flat shape the architecture test recognizes and no
// command gains a wrapper path. The grouping changes only how --help reads.
func groupTopLevelCommands(root *cobra.Command) {
	root.AddGroup(
		&cobra.Group{ID: groupServer, Title: "Server commands (act on the BuildMax server you signed in to):"},
		&cobra.Group{ID: groupLocal, Title: "Local commands (act on this machine):"},
	)
	for _, c := range root.Commands() {
		if serverCommandNames[c.Name()] {
			c.GroupID = groupServer
			continue
		}
		c.GroupID = groupLocal
	}
}

// addRunFlags declares the flags that configure one agent run. Both the root
// command (plain `buildmax`) and `buildmax issue start` launch a run and read
// the same set, so the flags and the launch have one definition each rather
// than a copy per surface.
func addRunFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("print", "p", "", "send QUERY to the LLM and print the response (no TUI)")
	cmd.Flags().StringP("resume", "r", "", "session id to resume (TUI or print mode); must be a valid UUID")
	cmd.Flags().BoolP("continue", "c", false, "resume this directory's most recent session (by creation time)")
	cmd.Flags().Bool("project", false, "with --continue, widen the search to every directory of this project")
	cmd.Flags().String("session-id", "", "use a specific session ID (load if exists, else create); must be a valid UUID")
	cmd.Flags().String("model", "", "use model from settings by model id or name")
	cmd.Flags().String("workspace", "", "workspace directory for the agent (default: current directory)")
	cmd.Flags().Bool("no-project-memory", false, "do not read or write this project's memory for this run")
	cmd.Flags().Bool("sandbox", false, "require the Bash sandbox for this run without changing settings")
	cmd.Flags().String("sandbox-mode", "", "sandbox approval mode for this run: auto_allow or regular (requires --sandbox)")
	cmd.Flags().Int("max-iterations", 0,
		fmt.Sprintf("cap this run's model calls (%d-%d; default %d, or agent.max_iterations)",
			config.MinMaxIterations, config.MaxMaxIterations, config.DefaultMaxIterations))
	cmd.Flags().Bool("remote-control", false, "make this session reachable from another device through the managed server (requires a login)")
	cmd.Flags().String("remote-control-name", "", "the name another device shows for this session (default: this machine's host name)")
	cmd.Flags().String("agent", "", "append the body of a named definition from .buildmax/agents or ~/.buildmax/agents")
	cmd.Flags().String("append-system-prompt", "", "text appended to this run's system prompt")
	cmd.Flags().String("append-system-prompt-file", "", "file whose contents are appended to this run's system prompt")
	cmd.Flags().String("output", "text", "output format for -p print mode: text, json, jsonl")
	cmd.Flags().Bool("no-stream", false, "disable streaming of assistant reply to stdout in print mode")
	cmd.Flags().BoolP("quiet", "q", false, "suppress the stats footer in print text mode")
	cmd.Flags().Bool("include-deltas", false, "include llm_delta events in --output jsonl (verbose)")
}

func runRoot(cmd *cobra.Command, _ []string) error {
	if v, _ := cmd.Flags().GetBool("version"); v {
		fmt.Fprintf(os.Stdout, "buildmax version %s\n", config.VersionString())
		return nil
	}
	return runAgentSession(cmd, nil)
}

// runAgentSession launches one agent run (TUI or -p print mode) from the run
// flags on cmd. issueSession scopes the run to a space Issue when `buildmax
// issue start` supplied one, and is nil for a plain `buildmax` run.
func runAgentSession(cmd *cobra.Command, issueSession *auth.IssueSession) error {
	prompt, _ := cmd.Flags().GetString("print")
	resumeID, _ := cmd.Flags().GetString("resume")
	cont, _ := cmd.Flags().GetBool("continue")
	acrossProject, _ := cmd.Flags().GetBool("project")
	model, _ := cmd.Flags().GetString("model")
	sessionID, _ := cmd.Flags().GetString("session-id")
	workspace, _ := cmd.Flags().GetString("workspace")
	noProjectMemory, _ := cmd.Flags().GetBool("no-project-memory")
	remoteControl, _ := cmd.Flags().GetBool("remote-control")
	remoteControlName, _ := cmd.Flags().GetString("remote-control-name")
	sandboxEnabled, _ := cmd.Flags().GetBool("sandbox")
	sandboxMode, _ := cmd.Flags().GetString("sandbox-mode")
	maxIterations, _ := cmd.Flags().GetInt("max-iterations")
	promptFlags := systemPromptFlags{}
	promptFlags.Agent, _ = cmd.Flags().GetString("agent")
	promptFlags.AppendText, _ = cmd.Flags().GetString("append-system-prompt")
	promptFlags.AppendFile, _ = cmd.Flags().GetString("append-system-prompt-file")
	outputStr, _ := cmd.Flags().GetString("output")
	noStream, _ := cmd.Flags().GetBool("no-stream")
	quiet, _ := cmd.Flags().GetBool("quiet")
	includeDeltas, _ := cmd.Flags().GetBool("include-deltas")

	format, err := parseOutputFormat(outputStr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return &ExitError{Code: ExitUsage, Err: err}
	}
	sandboxRun, err := parseSandboxRunOverride(sandboxEnabled, sandboxMode)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return &ExitError{Code: ExitUsage, Err: err}
	}
	if maxIterations < 0 {
		err := fmt.Errorf("invalid --max-iterations %d: want %d-%d",
			maxIterations, config.MinMaxIterations, config.MaxMaxIterations)
		fmt.Fprintln(os.Stderr, err.Error())
		return &ExitError{Code: ExitUsage, Err: err}
	}
	overrides := runOverrides{Sandbox: sandboxRun, MaxIterations: maxIterations, NoProjectMemory: noProjectMemory, Issue: issueSession, RemoteControl: remoteControl, RemoteControlName: remoteControlName}

	if sessionID != "" {
		if _, err := uuid.Parse(sessionID); err != nil {
			fmt.Fprintln(os.Stderr, "invalid session-id: not a valid UUID")
			return &ExitError{Code: ExitUsage, Err: fmt.Errorf("invalid session-id: %w", err)}
		}
	}
	// A session id is a UUID, so a malformed -r value is a usage error the caller
	// can fix, reported as such rather than as the "session not found" a
	// well-formed but unknown id gets when it is opened.
	if resumeID != "" {
		if _, err := uuid.Parse(resumeID); err != nil {
			fmt.Fprintln(os.Stderr, "invalid resume id: not a valid UUID")
			return &ExitError{Code: ExitUsage, Err: fmt.Errorf("invalid resume id: %w", err)}
		}
	}

	var target sessionTarget
	if sessionID != "" {
		// --session-id names a specific session and creates it on miss, as its
		// help promises, so a caller can start a run under a deterministic id.
		// -r/--resume goes through resolveSessionTarget and stays open-only.
		target = sessionTarget{SessionID: sessionID, Workspace: workspace, CreateIfMissing: true}
	} else {
		target, err = resolveSessionTarget(cmd.Context(), resumeID, cont, acrossProject,
			workspace, cmd.Flags().Changed("workspace"))
		if err != nil {
			return err
		}
	}
	// A resumed session continues in the directory it ran in, so the workspace
	// the rest of this function passes on is the target's, not the one the
	// terminal happened to be in.
	effectiveSessionID, workspace := target.SessionID, target.Workspace

	// Argument errors are reported before the environment is inspected: a bad flag combination
	// is fixable without a model configured, and reporting the missing configuration first
	// sends the user to solve the wrong problem.
	additionalSystemPrompt, err := resolveAdditionalSystemPrompt(promptFlags, workspace)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return &ExitError{Code: ExitUsage, Err: err}
	}

	if err := checkModelConfig(); err != nil {
		return &ExitError{Code: ExitUsage, Err: err}
	}

	if prompt != "" {
		slog.Info("running print mode")
		return runPrintMode(printOptions{
			Prompt:                 prompt,
			SessionID:              effectiveSessionID,
			CreateIfMissing:        target.CreateIfMissing,
			ModelName:              model,
			Workspace:              workspace,
			Format:                 format,
			NoStream:               noStream,
			Quiet:                  quiet,
			IncludeDeltas:          includeDeltas,
			AdditionalSystemPrompt: additionalSystemPrompt,
			Overrides:              overrides,
		})
	}
	slog.Info("starting TUI")
	return runTUIFunc(effectiveSessionID, model, additionalSystemPrompt, workspace, overrides, target.CreateIfMissing)
}

// runOverrides are the per-run flags that outrank settings.yaml for this
// invocation and nothing else. They travel as one value because both surfaces
// resolve them identically and neither is a property of the prompt, the model,
// or the workspace.
type runOverrides struct {
	Sandbox       config.SandboxRunOverride
	MaxIterations int
	// Issue scopes this run to one space Issue, or is nil for a plain run not
	// started by `buildmax issue start`. It is resolved once rather than per
	// turn: the Issue a session
	// works must not change under it, and the tools are registered from it when
	// the runtime is assembled.
	Issue *auth.IssueSession
	// NoProjectMemory keeps this run out of the project's memory in both
	// directions. There is no read-only variant: a run that may not look at
	// the document must not be able to replace it either.
	NoProjectMemory bool
	// RemoteControl opts this session into Remote Control, so another device can
	// watch it through the managed server. RemoteControlName is the label a
	// connected device shows; empty falls back to the host name.
	RemoteControl     bool
	RemoteControlName string
}

func parseSandboxRunOverride(enabled bool, mode string) (config.SandboxRunOverride, error) {
	if mode != "" && !enabled {
		return config.SandboxRunOverride{}, errors.New("--sandbox-mode requires --sandbox")
	}
	override := config.SandboxRunOverride{Enable: enabled}
	switch mode {
	case "":
		return override, nil
	case "auto_allow":
		v := true
		override.AutoAllowBashIfSandboxed = &v
	case "regular":
		v := false
		override.AutoAllowBashIfSandboxed = &v
	default:
		return config.SandboxRunOverride{}, fmt.Errorf("invalid --sandbox-mode %q: want auto_allow or regular", mode)
	}
	return override, nil
}

func parseOutputFormat(s string) (OutputFormat, error) {
	switch s {
	case "", "text":
		return OutputText, nil
	case "json":
		return OutputJSON, nil
	case "jsonl":
		return OutputJSONL, nil
	default:
		return OutputText, fmt.Errorf("invalid --output %q: want text, json, or jsonl", s)
	}
}

// checkModelConfig returns an error when the CLI has no usable model. It
// separates the three ways that happens — no file, a file with no models, and a
// file still holding the placeholder key `buildmax init` writes — because each
// one has a different next step, and "No model configured" answered none of
// them for a first-time user.
func checkModelConfig() error {
	// A session with stored credentials runs on the deployment's models, so
	// settings.yaml having none is not a reason to refuse. Whether that
	// deployment answers — and whether the login still works — is checked where
	// its list is fetched, which reports what actually failed.
	if creds, err := auth.StoredLogin(); err == nil && creds != nil {
		return nil
	}
	path := config.SettingsPath()
	s, err := config.LoadSettings()
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	if len(s.Models) == 0 {
		if _, statErr := os.Stat(path); errors.Is(statErr, fs.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "No configuration found.\n\n"+
				"Run `buildmax init` to create %s, then add your API key,\n"+
				"or `buildmax login` to use the models a BuildMax deployment offers.\n"+
				"Quickstart: %s\n", path, quickstartURL)
			return errors.New("no configuration file")
		}
		fmt.Fprintf(os.Stderr, "No model configured in %s.\n\n"+
			"Add a models: entry, or run `buildmax init --force` to regenerate the file.\n"+
			"Quickstart: %s\n", path, quickstartURL)
		return errors.New("no model configured")
	}
	// A local provider carries no credential, so an empty key there is the
	// configured state rather than an unfinished one.
	first := s.Models[0]
	if llm.ProviderNeedsCredential(first.LLMProvider()) && strings.TrimSpace(first.APIKey) == "" {
		fmt.Fprintf(os.Stderr, "The first model in %s has no api_key.\n\n"+
			"Add one, or use a local model with `buildmax init --ollama`.\n", path)
		return errors.New("api key not set")
	}
	if first.APIKey == APIKeyPlaceholder {
		fmt.Fprintf(os.Stderr, "The first model in %s still has the placeholder API key.\n\n"+
			"Replace %s on the api_key line with a real key.\n", path, APIKeyPlaceholder)
		return errors.New("api key not set")
	}
	return nil
}
