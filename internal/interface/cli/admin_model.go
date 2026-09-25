package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"golang.org/x/term"

	"github.com/spf13/cobra"

	"github.com/icloudbb/buildmax/internal/interface/client"
)

// newAdminModelCommand groups the managed-model catalog verbs, reached over the
// Admin API as the signed-in administrator — the automation peer of the Portal
// Models area. The provider credential in `add` travels in the request body,
// stored encrypted at rest by the server; it is the same catalog these verbs
// reach through the database on `buildmax-server`, but as an authenticated
// client rather than a direct one.
func newAdminModelCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "model",
		Short: "Manage the deployment's model catalog",
	}
	cmd.AddCommand(newAdminModelListCommand())
	cmd.AddCommand(newAdminModelAddCommand())
	cmd.AddCommand(newAdminModelEnableCommand())
	cmd.AddCommand(newAdminModelDisableCommand())
	cmd.AddCommand(newAdminModelSetKeyCommand())
	return cmd
}

func newAdminModelSetKeyCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set-key <model_id>",
		Short: "Replace a catalog model's upstream key",
		Long: "Replace a model's upstream key in place, to rotate a leaked or expired\n" +
			"one. The model keeps its ID and name, so no client has to change what it\n" +
			"selects. The key is read from standard input — typed without echo, or\n" +
			"piped — never from an argument, which shell history would keep.",
		Args: cobra.ExactArgs(1),
		RunE: runAdminModelSetKey,
	}
}

func newAdminModelListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List catalog models, enabled or not",
		Args:  cobra.NoArgs,
		RunE:  runAdminModelList,
	}
}

func newAdminModelAddCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a model to the catalog",
		Long: "Add a model to the catalog over the Admin API. The API key is sent in\n" +
			"the request body and stored encrypted; a deployment with no encryption\n" +
			"key configured refuses a model that carries one.",
		Args: cobra.NoArgs,
		RunE: runAdminModelAdd,
	}
	f := cmd.Flags()
	f.String("name", "", "operator-facing name")
	f.String("api-url", "", "upstream base URL")
	f.String("api-key", "", "upstream credential (write-only); - reads it from standard input")
	f.String("model", "", "the provider's own model identifier")
	f.String("provider", "", "wire protocol the upstream speaks (default openai_compatible)")
	f.Int("context-window", 0, "usable context size")
	f.Int("call-timeout", 0, "per-call timeout in seconds")
	f.Int("max-tokens", 0, "cap on one response")
	f.String("reasoning", "", "reasoning effort: off, low, medium, high")
	f.String("cache-mode", "", "prompt cache policy: auto, off, force")
	f.String("cache-ttl", "", "prompt cache retention: provider_default, 5m, 1h")
	f.String("currency", "", "ISO 4217 code the prices are quoted in")
	f.String("input-price", "", "price per million fresh prompt tokens")
	f.String("cache-read-price", "", "price per million cached prompt tokens read")
	f.String("cache-write-price", "", "price per million prompt tokens cached")
	f.String("output-price", "", "price per million generated tokens")
	f.Bool("vision", false, "the upstream accepts image input")
	f.StringSlice("capabilities", nil, "comma-separated capability list")
	return cmd
}

func newAdminModelEnableCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "enable <model_id>",
		Short: "Enable a catalog model",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return runAdminModelSetEnabled(cmd, args[0], true) },
	}
}

func newAdminModelDisableCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <model_id>",
		Short: "Retire a catalog model",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return runAdminModelSetEnabled(cmd, args[0], false) },
	}
}

func runAdminModelList(cmd *cobra.Command, _ []string) error {
	serverURL, token, err := adminSessionFor()
	if err != nil {
		return err
	}
	models, defaultModel, err := client.NewClient(serverURL).ListModels(cmd.Context(), token)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(models) == 0 {
		fmt.Fprintln(out, "the catalog is empty")
		return nil
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tPROVIDER\tMODEL\tSTATE\tDEFAULT")
	for _, m := range models {
		state := "enabled"
		if !m.Enabled {
			state = "retired"
		}
		def := ""
		if m.Name == defaultModel {
			def = "default"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", m.ID, m.Name, m.ProviderType, m.Model, state, def)
	}
	return w.Flush()
}

func runAdminModelAdd(cmd *cobra.Command, _ []string) error {
	serverURL, token, err := adminSessionFor()
	if err != nil {
		return err
	}
	f := cmd.Flags()
	get := func(name string) string { v, _ := f.GetString(name); return v }
	getInt := func(name string) int { v, _ := f.GetInt(name); return v }
	capabilities, _ := f.GetStringSlice("capabilities")
	vision, _ := f.GetBool("vision")
	apiKey := get("api-key")
	if apiKey == "-" {
		if apiKey, err = readKey(cmd); err != nil {
			return err
		}
	}
	in := client.CreateModelInput{
		Name:            get("name"),
		ProviderType:    get("provider"),
		APIURL:          get("api-url"),
		APIKey:          apiKey,
		Model:           get("model"),
		ContextWindow:   getInt("context-window"),
		CallTimeout:     getInt("call-timeout"),
		MaxTokens:       getInt("max-tokens"),
		Reasoning:       get("reasoning"),
		CacheMode:       get("cache-mode"),
		CacheTTL:        get("cache-ttl"),
		Currency:        get("currency"),
		InputPrice:      get("input-price"),
		CacheReadPrice:  get("cache-read-price"),
		CacheWritePrice: get("cache-write-price"),
		OutputPrice:     get("output-price"),
		Vision:          vision,
		Capabilities:    capabilities,
	}
	created, err := client.NewClient(serverURL).CreateModel(cmd.Context(), token, in)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "added model %s (%s)\n", created.ID, created.Name)
	return nil
}

func runAdminModelSetEnabled(cmd *cobra.Command, modelID string, enabled bool) error {
	serverURL, token, err := adminSessionFor()
	if err != nil {
		return err
	}
	updated, err := client.NewClient(serverURL).SetModelEnabled(cmd.Context(), token, modelID, enabled)
	if err != nil {
		return err
	}
	state := "retired"
	if updated.Enabled {
		state = "enabled"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s is now %s\n", updated.Name, state)
	return nil
}

func runAdminModelSetKey(cmd *cobra.Command, args []string) error {
	serverURL, token, err := adminSessionFor()
	if err != nil {
		return err
	}
	key, err := readKey(cmd)
	if err != nil {
		return err
	}
	updated, err := client.NewClient(serverURL).ReplaceModelCredential(cmd.Context(), token, args[0], key)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "replaced the key for %s; the deployment uses it from the next call\n", updated.Name)
	return nil
}

// readKey reads a provider key from standard input: without echo when a person
// is typing, one line when it is piped. A key never comes from an argument,
// where shell history and process listings would keep it.
func readKey(cmd *cobra.Command) (string, error) {
	in := cmd.InOrStdin()
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(cmd.ErrOrStderr(), "API key: ")
		raw, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(cmd.ErrOrStderr())
		if err != nil {
			return "", fmt.Errorf("read key: %w", err)
		}
		return strings.TrimSpace(string(raw)), nil
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		return "", errors.New("no API key on standard input")
	}
	return strings.TrimSpace(line), nil
}
