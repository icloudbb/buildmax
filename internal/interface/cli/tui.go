package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/icloudbb/buildmax/internal/agentapp"
	"github.com/icloudbb/buildmax/internal/core/agent"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
	"github.com/icloudbb/buildmax/internal/interface/auth"
	"github.com/muesli/termenv"
)

// detectGlamourStyle returns "dark" or "light" by querying the terminal background
// color. This must be called BEFORE tea.NewProgram so the terminal response arrives
// on stdin before Bubble Tea takes ownership of it.
func detectGlamourStyle() string {
	if termenv.HasDarkBackground() {
		return "dark"
	}
	return "light"
}

// tuiAppConfig is the runtime assembly for an interactive session. It is separate
// from runTUI so the wiring can be asserted without starting Bubble Tea — the
// workspace reaching it is exactly what was missing before.
func tuiAppConfig(workspace, additionalSystemPrompt string, source auth.ModelSource, overrides runOverrides) agentapp.AppConfig {
	return agentapp.AppConfig{
		WorkspaceDir:           workspace,
		EnableMCP:              true,
		Policy:                 agent.AllowAllPolicy(),
		ModelEntries:           source.Entries,
		DefaultModel:           source.Default,
		ManagedServerURL:       source.ServerURL,
		ManagedToken:           auth.TokenForServer,
		RemoteControl:          overrides.RemoteControl,
		RemoteControlName:      overrides.RemoteControlName,
		ArtifactPublisher:      auth.ArtifactPublisherForSession(),
		Issue:                  issueContextOf(overrides.Issue),
		Surface:                coregw.CallSurfaceCLI,
		AdditionalSystemPrompt: additionalSystemPrompt,
		SandboxRunOverride:     overrides.Sandbox,
		MaxIterations:          overrides.MaxIterations,
		// Interactive TUI only: print mode has no host process to own a job
		// and deliberately does not set this.
		EnableBackgroundJobs: true,
		// The TUI is where a user can see which tree the session is in and
		// answer a removal prompt, which is what makes moving the root safe
		// to do autonomously. See docs/design/workspace-root-and-worktrees.md D8.
		EnableWorktrees:      true,
		EnableLocalProject:   true,
		DisableProjectMemory: overrides.NoProjectMemory,
	}
}

// runTUIFunc is the seam the root command calls through, so a test can assert what
// the flags resolved to without launching an interactive program.
var runTUIFunc = runTUI

func runTUI(sessionID, modelName, additionalSystemPrompt, workspace string, overrides runOverrides, createIfMissing bool) error {
	source, err := resolveModelSource(context.Background())
	if err != nil {
		return err
	}
	// The relay (built inside NewAgentApp) delivers a remote prompt through this
	// sink; its program is wired in after tea.NewProgram below, like the approval
	// handler. Created here so the config can carry its handler.
	promptSink := newRemotePromptSink()
	cfg := tuiAppConfig(workspace, additionalSystemPrompt, source, overrides)
	cfg.RemotePromptHandler = promptSink.Deliver
	app, err := agentapp.NewAgentApp(cfg)
	if err != nil {
		return err
	}
	defer app.Close()
	for _, notice := range app.StartupNotices(relinkCommandHint) {
		fmt.Fprintln(os.Stderr, notice)
	}
	openSession := app.OpenSession
	if createIfMissing {
		openSession = app.OpenOrCreateSession
	}
	sess, err := openSession(sessionID)
	if err != nil {
		return err
	}
	// Held for the life of the TUI, which is the session's whole visible life.
	// Deferred here so no path that returns before the program starts leaves the
	// lock behind, and routed through the model once there is one: /fork
	// replaces the session and closes the parent itself, so a release fixed on
	// `sess` would end the parent a second time and never end the fork at all.
	var model *Model
	defer func() {
		if model != nil {
			app.CloseSession(model.CurrentSession())
			return
		}
		app.CloseSession(sess)
	}()
	if modelName != "" {
		sess.SetModel(modelName)
	}
	runStatus, err := app.EstimateRunUsage(sess)
	if err != nil {
		runStatus = agentapp.RunUsage{}
	}

	// Detect terminal color scheme before the program starts so glamour never
	// needs to query the terminal (which would inject escape-sequence responses
	// into stdin and corrupt the input box).
	glamourStyle := detectGlamourStyle()

	// Print banner and any existing session history to the terminal scrollback
	// before the TUI program takes over the bottom strip.
	if notice := issueSessionNotice(overrides.Issue, source); notice != "" {
		fmt.Print(notice + "\n")
	}
	fmt.Print(buildHistoryForScrollback(sess.Messages(), 80, glamourStyle))

	approval := NewTUIApprovalHandler()
	opts := TUIOpts{
		App:          app,
		Session:      sess,
		ModelName:    sess.ModelName(app.DefaultModelName()),
		Workspace:    app.Workspace(),
		SessionsDir:  app.SessionsDir(),
		Approval:     approval,
		GlamourStyle: glamourStyle,
		RunStatus:    runStatus,
	}
	model = NewModel(opts)
	defer model.Close()
	p := tea.NewProgram(model)
	approval.SetProgram(p)
	promptSink.SetProgram(p)
	if _, err := p.Run(); err != nil {
		slog.Error("TUI failed", "err", err)
		return fmt.Errorf("TUI: %w", err)
	}
	return nil
}
