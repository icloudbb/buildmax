package cli

import (
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/interface/auth"
)

func TestRootCommand_InvalidSessionIDReturnsError(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
	}{
		{"non-uuid string", "not-a-uuid"},
		{"short", "x"},
		{"too short", "123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := NewRootCommand()
			root.SetArgs([]string{"--session-id", tt.sessionID})
			err := root.Execute()
			if err == nil {
				t.Fatal("Execute(): want error for invalid --session-id")
			}
			if !strings.Contains(err.Error(), "invalid session-id") {
				t.Errorf("error message should contain 'invalid session-id': %q", err.Error())
			}
		})
	}
}

// A session id is a UUID, so a malformed -r value is a usage error the caller
// can fix, told apart from the "session not found" a well-formed but unknown id
// gets when it is opened.
func TestRootCommand_InvalidResumeIDReturnsError(t *testing.T) {
	for _, id := range []string{"not-a-uuid", "x", "123"} {
		t.Run(id, func(t *testing.T) {
			root := NewRootCommand()
			root.SetArgs([]string{"-r", id, "-p", "hi"})
			err := root.Execute()
			if err == nil {
				t.Fatal("Execute(): want error for invalid -r id")
			}
			if !strings.Contains(err.Error(), "invalid resume id") {
				t.Errorf("error message should contain 'invalid resume id': %q", err.Error())
			}
		})
	}
}

// TestRootCommand_FlagErrorPrecedesModelCheck pins the order of the two usage checks. A bad flag
// combination is fixable without a model configured, so reporting the missing configuration
// first would send the user to solve the wrong problem.
func TestRootCommand_FlagErrorPrecedesModelCheck(t *testing.T) {
	t.Setenv("BUILDMAX_HOME", t.TempDir()) // no settings.yaml, so the model check would also fail

	root := NewRootCommand()
	root.SetArgs([]string{
		"--append-system-prompt", "a",
		"--append-system-prompt-file", "b",
		"-p", "hi",
	})
	err := root.Execute()

	if err == nil {
		t.Fatal("Execute(): want an error for two mutually exclusive flags")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error = %q, want the flag conflict rather than the model configuration", err.Error())
	}
}

// --workspace reached the agent definition lookup but not the agent itself, so
// tools, AGENTS.md, and the footer's git branch all ran against the current
// directory while --agent resolved somewhere else. Both halves are pinned here.
func TestRootCommand_PassesWorkspaceToTUI(t *testing.T) {
	t.Setenv("BUILDMAX_HOME", t.TempDir())
	writeTestSettings(t, "models:\n  - model: stub\n    name: stub\n    api_key: x\n")
	workspace := t.TempDir()

	var gotWorkspace string
	var gotOverrides runOverrides
	orig := runTUIFunc
	runTUIFunc = func(sessionID, modelName, additionalSystemPrompt, ws string, overrides runOverrides, createIfMissing bool) error {
		gotWorkspace = ws
		gotOverrides = overrides
		return nil
	}
	t.Cleanup(func() { runTUIFunc = orig })

	root := NewRootCommand()
	root.SetArgs([]string{"--workspace", workspace, "--sandbox", "--sandbox-mode", "regular",
		"--max-iterations", "1500"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotWorkspace != workspace {
		t.Errorf("workspace passed to the TUI = %q, want %q", gotWorkspace, workspace)
	}
	if !gotOverrides.Sandbox.Enable {
		t.Error("--sandbox did not reach the TUI runtime")
	}
	if gotOverrides.Sandbox.AutoAllowBashIfSandboxed == nil || *gotOverrides.Sandbox.AutoAllowBashIfSandboxed {
		t.Errorf("sandbox mode = %v, want regular", gotOverrides.Sandbox.AutoAllowBashIfSandboxed)
	}
	if gotOverrides.MaxIterations != 1500 {
		t.Errorf("MaxIterations reaching the TUI = %d, want 1500", gotOverrides.MaxIterations)
	}
}

// --session-id carries a caller-chosen id through to the TUI with the
// create-on-miss contract set, while -r/--resume stays open-only. The two share
// the same effective-id argument, so the boolean is what keeps them distinct.
func TestRootCommand_SessionIDCreatesOnMissWhileResumeStaysOpenOnly(t *testing.T) {
	const id = "11111111-2222-3333-4444-555555555555"
	tests := []struct {
		name             string
		args             []string
		wantSessionID    string
		wantCreateOnMiss bool
	}{
		{"session-id creates on miss", []string{"--session-id", id}, id, true},
		{"resume stays open-only", []string{"-r", id}, id, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BUILDMAX_HOME", t.TempDir())
			writeTestSettings(t, "models:\n  - model: stub\n    name: stub\n    api_key: x\n")

			var gotSessionID string
			var gotCreateOnMiss bool
			orig := runTUIFunc
			runTUIFunc = func(sessionID, modelName, additionalSystemPrompt, ws string, overrides runOverrides, createIfMissing bool) error {
				gotSessionID = sessionID
				gotCreateOnMiss = createIfMissing
				return nil
			}
			t.Cleanup(func() { runTUIFunc = orig })

			root := NewRootCommand()
			root.SetArgs(tt.args)
			if err := root.Execute(); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if gotSessionID != tt.wantSessionID {
				t.Errorf("session id reaching the TUI = %q, want %q", gotSessionID, tt.wantSessionID)
			}
			if gotCreateOnMiss != tt.wantCreateOnMiss {
				t.Errorf("createIfMissing = %v, want %v", gotCreateOnMiss, tt.wantCreateOnMiss)
			}
		})
	}
}

func TestTUIAppConfigCarriesWorkspace(t *testing.T) {
	regular := false
	sandboxRun := config.SandboxRunOverride{Enable: true, AutoAllowBashIfSandboxed: &regular}
	cfg := tuiAppConfig("/tmp/some-workspace", "extra prompt", auth.ModelSource{},
		runOverrides{Sandbox: sandboxRun, MaxIterations: 1500})
	if cfg.WorkspaceDir != "/tmp/some-workspace" {
		t.Errorf("WorkspaceDir = %q, want %q", cfg.WorkspaceDir, "/tmp/some-workspace")
	}
	if cfg.AdditionalSystemPrompt != "extra prompt" {
		t.Errorf("AdditionalSystemPrompt = %q, want %q", cfg.AdditionalSystemPrompt, "extra prompt")
	}
	if !cfg.EnableMCP {
		t.Error("EnableMCP should stay on for an interactive session")
	}
	if cfg.Policy == nil {
		t.Error("an interactive session needs a policy that can ask")
	}
	if !cfg.SandboxRunOverride.Enable || cfg.SandboxRunOverride.AutoAllowBashIfSandboxed == nil || *cfg.SandboxRunOverride.AutoAllowBashIfSandboxed {
		t.Errorf("SandboxRunOverride = %+v, want enabled regular mode", cfg.SandboxRunOverride)
	}
	if cfg.MaxIterations != 1500 {
		t.Errorf("MaxIterations = %d, want 1500", cfg.MaxIterations)
	}
}

// An empty --workspace still means "current directory"; the fix must not turn the
// default into an empty path handed to the runtime.
func TestTUIAppConfigWithoutWorkspaceLeavesTheDefault(t *testing.T) {
	if cfg := tuiAppConfig("", "", auth.ModelSource{}, runOverrides{}); cfg.WorkspaceDir != "" {
		t.Errorf("WorkspaceDir = %q, want empty so the runtime falls back to the working directory", cfg.WorkspaceDir)
	}
}

func TestRootCommand_SandboxModeErrorsPrecedeModelCheck(t *testing.T) {
	t.Setenv("BUILDMAX_HOME", t.TempDir())

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "mode requires sandbox", args: []string{"--sandbox-mode", "regular"}, want: "requires --sandbox"},
		{name: "invalid mode", args: []string{"--sandbox", "--sandbox-mode", "permissive"}, want: "want auto_allow or regular"},
		// A negative cap is rejected rather than clamped, unlike the same value
		// in settings.yaml: a file states a preference the runtime may correct,
		// while a flag is what this invocation asked for, and silently running
		// something else is how a benchmark reports the wrong bound.
		{name: "negative iteration cap", args: []string{"--max-iterations", "-1"}, want: "invalid --max-iterations"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := NewRootCommand()
			root.SetArgs(tt.args)
			err := root.Execute()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Execute() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestPrintAppConfigCarriesRunOverrides(t *testing.T) {
	autoAllow := true
	opts := printOptions{Overrides: runOverrides{
		Sandbox: config.SandboxRunOverride{
			Enable:                   true,
			AutoAllowBashIfSandboxed: &autoAllow,
		},
		MaxIterations: 1500,
	}}
	cfg := printAppConfig(opts, auth.ModelSource{})
	if !cfg.SandboxRunOverride.Enable || cfg.SandboxRunOverride.AutoAllowBashIfSandboxed == nil || !*cfg.SandboxRunOverride.AutoAllowBashIfSandboxed {
		t.Errorf("SandboxRunOverride = %+v, want enabled auto_allow mode", cfg.SandboxRunOverride)
	}
	if cfg.MaxIterations != 1500 {
		t.Errorf("MaxIterations = %d, want 1500", cfg.MaxIterations)
	}
}

// Cobra re-adds the default completion command on every Execute, so hiding it has to survive
// that, and the generated script has to keep working for anyone who already installed one.
func TestRootCommand_CompletionStaysOutOfHelp(t *testing.T) {
	var out strings.Builder
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(--help): %v", err)
	}
	if strings.Contains(out.String(), "completion") {
		t.Errorf("help should not list the completion command:\n%s", out.String())
	}

	out.Reset()
	root = NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"completion", "zsh"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute(completion zsh): %v", err)
	}
	if !strings.Contains(out.String(), "compdef") {
		t.Error("completion zsh should still print a usable script")
	}
}
