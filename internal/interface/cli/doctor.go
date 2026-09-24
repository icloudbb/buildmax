package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/infra/git"
	"github.com/icloudbb/buildmax/internal/infra/llm"
	"github.com/icloudbb/buildmax/internal/infra/sandbox"
	"github.com/icloudbb/buildmax/internal/interface/auth"

	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/spf13/cobra"
)

type doctorSeverity int

const (
	doctorOK doctorSeverity = iota
	doctorWarn
	doctorFail
)

type doctorCheck struct {
	Severity doctorSeverity
	Title    string
	Detail   string
	Next     string
}

func newDoctorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check local setup before the first run",
		Long: `Check the local BuildMax setup without contacting a hosted LLM provider.

Doctor verifies the app data directory, settings.yaml, model entries, workspace
state, the local project this directory belongs to and the state of its memory,
git availability, and sandbox dependencies. It reads: it never registers a
project or changes one. A model entry served by a
local runtime is checked against that runtime — which model is pulled and what
it can do — because those are the answers no configuration file holds. It exits
2 only when a required first-run prerequisite is missing.`,
		Args:         cobra.NoArgs,
		RunE:         runDoctor,
		SilenceUsage: true,
	}
	cmd.Flags().String("workspace", "", "workspace directory to check (default: current directory)")
	return cmd
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	workspace, _ := cmd.Flags().GetString("workspace")
	checks := collectDoctorChecks(cmd.Context(), workspace)
	writeDoctorReport(cmd.OutOrStdout(), checks)
	if doctorHasFailure(checks) {
		return &ExitError{Code: ExitUsage, Err: errors.New("doctor found setup problems")}
	}
	return nil
}

func collectDoctorChecks(ctx context.Context, workspace string) []doctorCheck {
	checks := []doctorCheck{
		checkDataDir(),
		checkGitAvailable(),
	}
	// The mode comes first because it decides what the rest means: signed in,
	// settings.yaml's models serve nothing and are not required.
	mode, managed := checkMode(ctx)
	checks = append(checks, mode)
	settings, settingsChecks := checkSettings(ctx, managed)
	checks = append(checks, settingsChecks...)
	checks = append(checks, checkWorkspace(workspace))
	checks = append(checks, checkProject(ctx, workspace)...)
	checks = append(checks, checkDetachedSessions(ctx))
	checks = append(checks, checkSandboxDeps(settings))
	return checks
}

func checkDataDir() doctorCheck {
	dir := config.DataDir()
	if dir == "" {
		return doctorCheck{Severity: doctorFail, Title: "BUILDMAX_HOME", Detail: "app data directory resolved to an empty path"}
	}
	if info, err := os.Stat(dir); err == nil {
		if !info.IsDir() {
			return doctorCheck{
				Severity: doctorFail,
				Title:    "BUILDMAX_HOME",
				Detail:   fmt.Sprintf("%s exists but is not a directory", dir),
				Next:     "Set BUILDMAX_HOME to a directory, or remove the file at that path.",
			}
		}
		return doctorCheck{Severity: doctorOK, Title: "BUILDMAX_HOME", Detail: dir}
	} else if errors.Is(err, fs.ErrNotExist) {
		return doctorCheck{
			Severity: doctorWarn,
			Title:    "BUILDMAX_HOME",
			Detail:   fmt.Sprintf("%s does not exist yet", dir),
			Next:     "Run `buildmax init` to create it with a starter settings.yaml.",
		}
	} else {
		return doctorCheck{Severity: doctorFail, Title: "BUILDMAX_HOME", Detail: fmt.Sprintf("cannot inspect %s: %v", dir, err)}
	}
}

func checkSettings(ctx context.Context, managed bool) (config.Settings, []doctorCheck) {
	path := config.SettingsPath()
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) && managed {
			return config.Settings{}, []doctorCheck{{
				Severity: doctorOK,
				Title:    "settings.yaml",
				Detail:   "not present, and not needed while signed in: the deployment's models serve every prompt",
			}}
		}
		if errors.Is(err, fs.ErrNotExist) {
			return config.Settings{}, []doctorCheck{{
				Severity: doctorFail,
				Title:    "settings.yaml",
				Detail:   fmt.Sprintf("%s was not found", path),
				Next:     "Run `buildmax init --api-key <key>` or `buildmax init` to create it.",
			}}
		}
		return config.Settings{}, []doctorCheck{{
			Severity: doctorFail,
			Title:    "settings.yaml",
			Detail:   fmt.Sprintf("cannot inspect %s: %v", path, err),
		}}
	}

	settings, err := config.LoadSettings()
	if err != nil {
		return config.Settings{}, []doctorCheck{{
			Severity: doctorFail,
			Title:    "settings.yaml",
			Detail:   err.Error(),
			Next:     "Fix the YAML syntax, or re-run `buildmax init --force` to regenerate a starter file.",
		}}
	}

	if managed {
		// Its models serve nothing while signed in, so probing them would report
		// a default and endpoints that no prompt in this mode reaches.
		return settings, []doctorCheck{{
			Severity: doctorOK,
			Title:    "settings.yaml",
			Detail:   fmt.Sprintf("%s — its %d model(s) are unused while signed in; `buildmax logout` switches to them", path, len(settings.Models)),
		}}
	}
	checks := []doctorCheck{{
		Severity: doctorOK,
		Title:    "settings.yaml",
		Detail:   path,
	}}
	checks = append(checks, checkModels(ctx, settings)...)
	if check, ok := checkDefaultModel(settings); ok {
		checks = append(checks, check)
	}
	return settings, checks
}

func checkModels(ctx context.Context, settings config.Settings) []doctorCheck {
	if len(settings.Models) == 0 {
		return []doctorCheck{{
			Severity: doctorFail,
			Title:    "models",
			Detail:   "no models are configured",
			Next:     "Add a models: entry, or run `buildmax init --force` to regenerate settings.yaml.",
		}}
	}

	checks := make([]doctorCheck, 0, len(settings.Models))
	for i, m := range settings.Models {
		title := fmt.Sprintf("model[%d]", i)
		sev := optionalModelSeverity(i)
		display := m.Name
		if display == "" {
			display = m.Model
		}
		if strings.TrimSpace(m.Model) == "" {
			checks = append(checks, doctorCheck{
				Severity: sev,
				Title:    title,
				Detail:   "model id is empty",
				Next:     "Set the model field, for example `openai/gpt-4o-mini`.",
			})
			continue
		}
		if m.LLMProvider() == cllm.ProviderOllama {
			checks = append(checks, checkOllamaModel(ctx, i, title, display, sev, m))
			continue
		}
		if strings.TrimSpace(m.APIKey) == "" || m.APIKey == APIKeyPlaceholder {
			checks = append(checks, doctorCheck{
				Severity: sev,
				Title:    title,
				Detail:   fmt.Sprintf("%s has no real api_key", display),
				Next:     fmt.Sprintf("Replace %s on the api_key line with a real key.", APIKeyPlaceholder),
			})
			continue
		}
		if strings.TrimSpace(m.APIURL) == "" {
			checks = append(checks, doctorCheck{
				Severity: sev,
				Title:    title,
				Detail:   fmt.Sprintf("%s has an empty api_url", display),
				Next:     fmt.Sprintf("Set api_url, for example %s.", config.DefaultOpenRouterBaseURL),
			})
			continue
		}
		suffix := ""
		if i == 0 {
			suffix = " (default)"
		}
		checks = append(checks, doctorCheck{
			Severity: doctorOK,
			Title:    title,
			Detail:   fmt.Sprintf("%s -> %s%s", display, m.APIURL, suffix),
		})
	}
	return checks
}

// checkMode reports which mode this machine is in and whether it works, and
// whether a login decides the mode even when it no longer works: an expired or
// unreachable login is still managed mode, because it is still where every
// prompt would go.
func checkMode(ctx context.Context) (doctorCheck, bool) {
	creds, err := auth.StoredLogin()
	if err != nil {
		return doctorCheck{
			Severity: doctorWarn,
			Title:    "mode",
			Detail:   fmt.Sprintf("cannot read the stored login: %v", err),
			Next:     "Run `buildmax login` to sign in again, or `buildmax logout` to use local models.",
		}, false
	}
	if creds == nil {
		return doctorCheck{
			Severity: doctorOK,
			Title:    "mode",
			Detail:   "local: models come from settings.yaml and prompts go straight to their providers",
		}, false
	}
	if _, err := auth.ResolveModelSource(ctx); err != nil {
		next := modeNextStep(err)
		if next == "" {
			next = fmt.Sprintf("Run `buildmax logout` to use the models in settings.yaml instead of %s.", creds.ServerURL)
		}
		return doctorCheck{
			Severity: doctorFail,
			Title:    "mode",
			Detail:   fmt.Sprintf("signed in to %s, but its models cannot be read: %v", creds.ServerURL, err),
			Next:     strings.ReplaceAll(next, "\n", " "),
		}, true
	}
	return doctorCheck{
		Severity: doctorOK,
		Title:    "mode",
		Detail: fmt.Sprintf("signed in to %s: its models serve every prompt. Credentials: %s",
			creds.ServerURL, auth.StorageDescription(creds.Storage)),
	}, true
}

// checkDefaultModel reports a default_model that names no entry. It resolves to
// the first entry instead, which is a working session against a model the file
// says is not the default — worth naming rather than leaving to be noticed.
func checkDefaultModel(settings config.Settings) (doctorCheck, bool) {
	name := strings.TrimSpace(settings.DefaultModel)
	if name == "" {
		return doctorCheck{}, false
	}
	for _, m := range settings.Models {
		display := m.Name
		if display == "" {
			display = m.Model
		}
		if display == name || m.Model == name {
			return doctorCheck{
				Severity: doctorOK,
				Title:    "default_model",
				Detail:   fmt.Sprintf("%s starts every new session", name),
			}, true
		}
	}
	return doctorCheck{
		Severity: doctorWarn,
		Title:    "default_model",
		Detail:   fmt.Sprintf("%q matches no entry in models, so the first one is used instead", name),
		Next:     "Set default_model to one of the names `buildmax models` lists, or remove it.",
	}, true
}

// doctorOllamaTimeout bounds the two calls this check makes to a local daemon.
// A diagnostic that hangs is worse than one that reports the daemon as slow.
const doctorOllamaTimeout = 3 * time.Second

// checkOllamaModel reports whether a local entry can actually run.
//
// It asks the daemon rather than the file, because everything that makes this
// provider fail is state the file cannot hold: whether the daemon is up,
// whether the model is pulled, and whether it can call tools at all. Each
// answer comes with the command that fixes it.
func checkOllamaModel(ctx context.Context, index int, title, display string, sev doctorSeverity, m config.ModelEntry) doctorCheck {
	baseURL := strings.TrimSpace(m.APIURL)
	if baseURL == "" {
		baseURL = config.DefaultOllamaBaseURL
	}
	ctx, cancel := context.WithTimeout(ctx, doctorOllamaTimeout)
	defer cancel()

	installed, err := llm.OllamaInventory(ctx, baseURL)
	if err != nil {
		return doctorCheck{
			Severity: sev,
			Title:    title,
			Detail:   fmt.Sprintf("%s cannot reach a daemon at %s: %v", display, baseURL, err),
			Next:     "Start it with `ollama serve`, or set api_url to where it listens.",
		}
	}
	if !ollamaHolds(installed, m.Model) {
		return doctorCheck{
			Severity: sev,
			Title:    title,
			Detail:   fmt.Sprintf("%s is not pulled on %s", m.Model, baseURL),
			Next:     fmt.Sprintf("Run `ollama pull %s`, or `buildmax models --local` to see what is installed.", m.Model),
		}
	}
	shown, err := llm.OllamaShow(ctx, baseURL, m.Model)
	if err != nil {
		// The model is there; only its details are missing. That is not worth
		// failing a first run over.
		return doctorCheck{
			Severity: doctorWarn,
			Title:    title,
			Detail:   fmt.Sprintf("%s is installed, but the daemon did not describe it: %v", display, err),
			Next:     "Check the daemon logs; the model may still run.",
		}
	}
	if !shown.HasCapability(llm.OllamaCapabilityTools) {
		return doctorCheck{
			Severity: sev,
			Title:    title,
			Detail:   fmt.Sprintf("%s does not support tool calling, so it cannot run the agent loop", display),
			Next:     "Pick a model whose capabilities include tools: `buildmax models --local`.",
		}
	}
	if shown.ContextWindow > 0 && m.ContextWindow > shown.ContextWindow {
		return doctorCheck{
			Severity: doctorWarn,
			Title:    title,
			Detail: fmt.Sprintf("%s sets context_window %d, above the %d this model was trained for",
				display, m.ContextWindow, shown.ContextWindow),
			Next: "Lower context_window, or expect the runtime to truncate the oldest messages.",
		}
	}
	if strings.TrimSpace(m.APIKey) != "" {
		return doctorCheck{
			Severity: doctorWarn,
			Title:    title,
			Detail:   fmt.Sprintf("%s carries an api_key, which a local runtime ignores", display),
			Next:     "Remove the api_key line.",
		}
	}
	suffix := ""
	if index == 0 {
		suffix = " (default)"
	}
	return doctorCheck{
		Severity: doctorOK,
		Title:    title,
		Detail:   fmt.Sprintf("%s -> %s%s%s", display, baseURL, ollamaModelSummary(shown), suffix),
	}
}

func ollamaHolds(installed []llm.OllamaModel, model string) bool {
	for _, m := range installed {
		if m.Model == model {
			return true
		}
	}
	return false
}

// ollamaModelSummary renders what the daemon reported, or nothing when it
// reported neither size nor window.
func ollamaModelSummary(m llm.OllamaModel) string {
	var parts []string
	if m.ParameterSize != "" {
		parts = append(parts, m.ParameterSize)
	}
	if m.ContextWindow > 0 {
		parts = append(parts, fmt.Sprintf("ctx %d", m.ContextWindow))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

func optionalModelSeverity(index int) doctorSeverity {
	if index == 0 {
		return doctorFail
	}
	return doctorWarn
}

func checkWorkspace(workspace string) doctorCheck {
	dir := workspace
	if strings.TrimSpace(dir) == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return doctorCheck{Severity: doctorFail, Title: "workspace", Detail: fmt.Sprintf("cannot read current directory: %v", err)}
		}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return doctorCheck{Severity: doctorFail, Title: "workspace", Detail: fmt.Sprintf("cannot resolve %s: %v", dir, err)}
	}
	info, err := os.Stat(abs)
	if err != nil {
		return doctorCheck{Severity: doctorFail, Title: "workspace", Detail: fmt.Sprintf("cannot inspect %s: %v", abs, err)}
	}
	if !info.IsDir() {
		return doctorCheck{
			Severity: doctorFail,
			Title:    "workspace",
			Detail:   fmt.Sprintf("%s is not a directory", abs),
			Next:     "Pass a directory with `--workspace <dir>`.",
		}
	}
	branch := git.CurrentBranch(abs)
	if branch == "" {
		return doctorCheck{
			Severity: doctorWarn,
			Title:    "workspace",
			Detail:   fmt.Sprintf("%s is not on a named git branch", abs),
			Next:     "First runs are safest in a git working tree you can diff and revert.",
		}
	}
	return doctorCheck{Severity: doctorOK, Title: "workspace", Detail: fmt.Sprintf("%s (git branch %s)", abs, branch)}
}

func checkGitAvailable() doctorCheck {
	path, err := exec.LookPath("git")
	if err != nil {
		return doctorCheck{
			Severity: doctorWarn,
			Title:    "git",
			Detail:   "git was not found on PATH",
			Next:     "Install git so you can inspect and revert agent changes.",
		}
	}
	return doctorCheck{Severity: doctorOK, Title: "git", Detail: path}
}

func checkSandboxDeps(settings config.Settings) doctorCheck {
	if !settings.Sandbox.Enabled {
		return doctorCheck{
			Severity: doctorWarn,
			Title:    "sandbox",
			Detail:   "disabled for local runs",
			Next:     "Optional: run `buildmax sandbox enable` and then `buildmax sandbox deps`.",
		}
	}
	rep := sandbox.CheckDeps()
	if rep.AllRequiredOK() {
		return doctorCheck{Severity: doctorOK, Title: "sandbox", Detail: fmt.Sprintf("%s backend dependencies are present", rep.Backend)}
	}
	missing := rep.FirstMissingRequired()
	next := missing.Hint
	if next == "" {
		next = "Run `buildmax sandbox deps` for platform-specific details."
	}
	return doctorCheck{
		Severity: doctorWarn,
		Title:    "sandbox",
		Detail:   fmt.Sprintf("enabled, but required dependency %s is missing", missing.Name),
		Next:     next,
	}
}

func writeDoctorReport(w io.Writer, checks []doctorCheck) {
	fmt.Fprintln(w, "BuildMax doctor")
	fmt.Fprintln(w, "===============")
	for _, c := range checks {
		fmt.Fprintf(w, "%-6s %s: %s\n", doctorMarker(c.Severity), c.Title, c.Detail)
		if c.Next != "" {
			fmt.Fprintf(w, "       next: %s\n", c.Next)
		}
	}
	failures, warnings := doctorCounts(checks)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Summary: %d failure(s), %d warning(s)\n", failures, warnings)
	if failures == 0 {
		fmt.Fprintln(w, "Ready for a first run:")
		fmt.Fprintln(w, "  buildmax -p \"Summarize what this project does\"")
	}
}

func doctorMarker(s doctorSeverity) string {
	switch s {
	case doctorFail:
		return "[FAIL]"
	case doctorWarn:
		return "[WARN]"
	default:
		return "[OK]"
	}
}

func doctorCounts(checks []doctorCheck) (failures, warnings int) {
	for _, c := range checks {
		switch c.Severity {
		case doctorFail:
			failures++
		case doctorWarn:
			warnings++
		}
	}
	return failures, warnings
}

func doctorHasFailure(checks []doctorCheck) bool {
	failures, _ := doctorCounts(checks)
	return failures > 0
}
