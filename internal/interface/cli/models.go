package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/icloudbb/buildmax/internal/agentapp"
	"github.com/icloudbb/buildmax/internal/config"
	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/icloudbb/buildmax/internal/infra/llm"
	"github.com/icloudbb/buildmax/internal/interface/auth"
)

func newModelsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "models",
		Short: "List the models this machine can run, and where each one sends prompts",
		Long: "Lists the models available to you and where each one sends prompts.\n\n" +
			"Which models those are follows from whether you are signed in: with a\n" +
			"login they are the ones that deployment offers, without one they are the\n" +
			"ones in settings.yaml. `buildmax login` and `buildmax logout` switch.\n\n" +
			"With --local, also lists what a local Ollama daemon has pulled, including\n" +
			"whether each model can call tools, and prints an entry ready to paste.",
		RunE: runModels,
	}
	cmd.Flags().Bool("local", false, "list the models a local Ollama daemon holds")
	cmd.Flags().String("ollama-url", config.DefaultOllamaBaseURL, "where the local daemon listens, for --local")
	return cmd
}

func runModels(cmd *cobra.Command, _ []string) error {
	source, err := resolveModelSource(cmd.Context())
	if err != nil {
		return err
	}
	if source.Managed() {
		printManagedModels(source)
	} else {
		settings, err := config.LoadSettings()
		if err != nil {
			return fmt.Errorf("load settings: %w", err)
		}
		printLocalModels(settings)
	}

	if local, _ := cmd.Flags().GetBool("local"); local {
		baseURL, _ := cmd.Flags().GetString("ollama-url")
		if err := printOllamaModels(cmd.Context(), cmd.OutOrStdout(), baseURL); err != nil {
			return err
		}
	}
	return nil
}

// printManagedModels lists what the signed-in deployment offers. Prompts go
// there, so the destination is stated once for the whole list rather than
// repeated on every row: in managed mode there is only one.
func printManagedModels(source auth.ModelSource) {
	fmt.Fprintf(os.Stdout, "Signed in to %s. Prompts, tool schemas, and tool results go there.\n",
		source.ServerURL)
	fmt.Fprintln(os.Stdout, "\nModels this deployment offers:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  NAME\tCONTEXT\tDEFAULT")
	for _, entry := range source.Entries {
		def := ""
		if entry.Name == source.Default {
			def = "yes"
		}
		window := "server default"
		if entry.ContextWindow > 0 {
			window = strconv.Itoa(entry.ContextWindow)
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\n", entry.Name, window, def)
	}
	_ = w.Flush()
	fmt.Fprintln(os.Stdout, "\nRun `buildmax logout` to use the models in settings.yaml instead.")
}

// printLocalModels lists settings.yaml, which is what a session with no login
// runs against. Every one of them is called from this machine.
func printLocalModels(settings config.Settings) {
	if len(settings.Models) == 0 {
		fmt.Fprintln(os.Stdout, "No models configured. Run `buildmax init` to write a starter settings.yaml,\nor `buildmax login` to use the models a BuildMax deployment offers.")
		return
	}
	fmt.Fprintln(os.Stdout, "Not signed in. Prompts go straight from this machine to each provider.")
	fmt.Fprintln(os.Stdout, "\nModels in settings.yaml:")
	defaultName := agentapp.DefaultModelName(settings)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  NAME\tMODEL\tDESTINATION\tDEFAULT")
	for _, entry := range settings.Models {
		cfg := agentapp.ModelConfigFromEntry(entry)
		def := ""
		if cfg.Name == defaultName {
			def = "yes"
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n", cfg.Name, cfg.ProviderModel, modelDestination(cfg), def)
	}
	_ = w.Flush()
}

// modelDestination is where prompts actually go for a local entry.
func modelDestination(cfg agentapp.ModelConfig) string {
	if cfg.BaseURL == "" {
		if cfg.Provider == cllm.ProviderOllama {
			return config.DefaultOllamaBaseURL
		}
		return config.DefaultOpenRouterBaseURL
	}
	return cfg.BaseURL
}

// ollamaListTimeout bounds the daemon calls behind --local. The listing asks
// the daemon about each model it holds, and a slow one must not hang the
// command.
const ollamaListTimeout = 10 * time.Second

// printOllamaModels lists what a local daemon holds.
//
// Capabilities are the column that matters: a model without tool calling
// cannot run the agent loop, and that is invisible in the model name. The
// paste-ready entry at the end goes in settings.yaml, which is what local mode
// runs against.
func printOllamaModels(ctx context.Context, out io.Writer, baseURL string) error {
	ctx, cancel := context.WithTimeout(ctx, ollamaListTimeout)
	defer cancel()

	installed, err := llm.OllamaInventory(ctx, baseURL)
	if err != nil {
		return fmt.Errorf("list local models: %w", err)
	}
	fmt.Fprintf(out, "\nLocal models on %s:\n", baseURL)
	if len(installed) == 0 {
		fmt.Fprintln(out, "  none — pull one, for example `ollama pull qwen3:8b`")
		return nil
	}

	described := make([]llm.OllamaModel, 0, len(installed))
	for _, m := range installed {
		// /api/tags carries neither the context length nor, on older daemons,
		// the capabilities. Asking per model is what makes the columns true.
		if shown, err := llm.OllamaShow(ctx, baseURL, m.Model); err == nil {
			shown.SizeBytes = m.SizeBytes
			if shown.ParameterSize == "" {
				shown.ParameterSize = m.ParameterSize
			}
			described = append(described, shown)
			continue
		}
		described = append(described, m)
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  MODEL\tSIZE\tPARAMS\tCONTEXT\tCAPABILITIES")
	for _, m := range described {
		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\n",
			m.Model, humanSize(m.SizeBytes), orDash(m.ParameterSize),
			orDash(contextLabel(m.ContextWindow)), orDash(strings.Join(m.Capabilities, ",")))
	}
	_ = w.Flush()

	usable := firstToolCallingModel(described)
	if usable.Model == "" {
		fmt.Fprintln(out, "\nNone of these can call tools, which the agent loop needs.")
		fmt.Fprintln(out, "Pull one that can, for example `ollama pull qwen3:8b`.")
		return nil
	}
	fmt.Fprintf(out, "\nTo use one, add to settings.yaml:\n\n"+
		"  - model: %s\n    name: %s (local)\n    provider: %s\n    api_url: %s\n    context_window: %d\n",
		usable.Model, usable.Model, cllm.ProviderOllama, baseURL, suggestedContextWindow(usable))
	return nil
}

// firstToolCallingModel picks what to show in the paste-ready entry. A model
// that cannot call tools would be a suggestion that does not work.
func firstToolCallingModel(models []llm.OllamaModel) llm.OllamaModel {
	for _, m := range models {
		if m.HasCapability(llm.OllamaCapabilityTools) {
			return m
		}
	}
	return llm.OllamaModel{}
}

// suggestedContextWindow keeps the daemon's answer as a ceiling: a model's full
// trained length can be more than the machine can allocate.
func suggestedContextWindow(m llm.OllamaModel) int {
	if m.ContextWindow <= 0 {
		return config.DefaultContextWindow
	}
	return min(m.ContextWindow, config.DefaultContextWindow)
}

func contextLabel(window int) string {
	if window <= 0 {
		return ""
	}
	return strconv.Itoa(window)
}

func orDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func humanSize(bytes int64) string {
	if bytes <= 0 {
		return "-"
	}
	const unit = 1000
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "kMGT"[exp])
}
