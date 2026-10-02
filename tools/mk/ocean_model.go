package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type oceanModelConfig struct {
	name            string
	provider        string
	apiURL          string
	model           string
	apiKey          string
	contextWindow   int
	currency        string
	inputPrice      string
	cacheReadPrice  string
	cacheWritePrice string
	outputPrice     string
}

func oceanModel(cfg oceanConfig, args []string) error {
	if len(args) == 0 {
		return usageErrorf("ocean", "ocean model needs an action")
	}
	if len(args) > 1 {
		return usageErrorf("ocean", "ocean model takes exactly one action")
	}
	if err := requireCommands("kubectl"); err != nil {
		return err
	}
	if !exists(oceanKubeconfigPath(cfg)) {
		return fmt.Errorf("ocean kubeconfig does not exist; run `%s ocean up` first", mk())
	}

	switch args[0] {
	case "init":
		return oceanModelInit(cfg)
	case "list":
		return oceanModelList(cfg)
	default:
		return usageErrorf("ocean", "unknown ocean model action: %s", args[0])
	}
}

func loadOceanModelConfig() (oceanModelConfig, error) {
	contextWindow, err := oceanModelContextWindow()
	if err != nil {
		return oceanModelConfig{}, err
	}

	cfg := oceanModelConfig{
		name:            strings.TrimSpace(envOr("BUILDMAX_OCEAN_MODEL_NAME", "GPT-5.6 Luna")),
		provider:        strings.TrimSpace(envOr("BUILDMAX_OCEAN_MODEL_PROVIDER", "openai")),
		apiURL:          strings.TrimSpace(envOr("BUILDMAX_OCEAN_MODEL_API_URL", "https://openrouter.ai/api/v1")),
		model:           strings.TrimSpace(envOr("BUILDMAX_OCEAN_MODEL_ID", "openai/gpt-5.6-luna")),
		apiKey:          strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")),
		contextWindow:   contextWindow,
		currency:        strings.TrimSpace(envOr("BUILDMAX_OCEAN_MODEL_CURRENCY", "USD")),
		inputPrice:      strings.TrimSpace(envOr("BUILDMAX_OCEAN_MODEL_INPUT_PRICE", "0.2")),
		cacheReadPrice:  strings.TrimSpace(envOr("BUILDMAX_OCEAN_MODEL_CACHE_READ_PRICE", "0.02")),
		cacheWritePrice: strings.TrimSpace(envOr("BUILDMAX_OCEAN_MODEL_CACHE_WRITE_PRICE", "0.25")),
		outputPrice:     strings.TrimSpace(envOr("BUILDMAX_OCEAN_MODEL_OUTPUT_PRICE", "1.2")),
	}
	for name, value := range map[string]string{
		"BUILDMAX_OCEAN_MODEL_NAME":     cfg.name,
		"BUILDMAX_OCEAN_MODEL_PROVIDER": cfg.provider,
		"BUILDMAX_OCEAN_MODEL_API_URL":  cfg.apiURL,
		"BUILDMAX_OCEAN_MODEL_ID":       cfg.model,
		"OPENROUTER_API_KEY":            cfg.apiKey,
	} {
		if value == "" {
			return oceanModelConfig{}, fmt.Errorf("%s is required for `%s ocean model init`", name, mk())
		}
	}
	return cfg, nil
}

// oceanModelContextWindow is the catalog row's context window, also told to
// managed worker runs, which cannot learn it from the gateway per call.
func oceanModelContextWindow() (int, error) {
	contextWindow, err := strconv.Atoi(envOr("BUILDMAX_OCEAN_MODEL_CONTEXT_WINDOW", "1050000"))
	if err != nil || contextWindow <= 0 {
		return 0, errors.New("BUILDMAX_OCEAN_MODEL_CONTEXT_WINDOW must be a positive integer")
	}
	return contextWindow, nil
}

func oceanModelInit(cfg oceanConfig) error {
	model, err := loadOceanModelConfig()
	if err != nil {
		return err
	}
	existing, err := oceanCatalogIDs(cfg)
	if err != nil {
		return err
	}
	id, known := existing[model.name]
	if known {
		fmt.Printf("Model %q is already in the Ocean catalog as %s; nothing changed.\n", model.name, id)
	} else {
		output, err := oceanAddModel(cfg, model)
		if err != nil {
			return err
		}
		fmt.Println(output)
		existing, err = oceanCatalogIDs(cfg)
		if err != nil {
			return err
		}
		id, known = existing[model.name]
		if !known {
			return fmt.Errorf("model %q was added but its catalog ID could not be read back", model.name)
		}
	}
	if err := oceanConfigureConversationModel(cfg, id); err != nil {
		return err
	}
	fmt.Printf("\nModel %q is ready for Portal conversations and managed worker calls.\n", model.name)
	return nil
}

func oceanModelList(cfg oceanConfig) error {
	return oceanKubectl(cfg, "exec", "--namespace", oceanNamespace, "--container", "server", "deploy/buildmax-server", "--", "buildmax-server", "model", "list")
}

func oceanCatalogIDs(cfg oceanConfig) (map[string]string, error) {
	output, err := oceanKubectlOutput(cfg, "exec", "--namespace", oceanNamespace, "--container", "server", "deploy/buildmax-server", "--", "buildmax-server", "model", "list")
	if err != nil {
		return nil, fmt.Errorf("read Ocean model catalog: %w", err)
	}
	return parseCatalogIDs(output), nil
}

func oceanAddModel(cfg oceanConfig, model oceanModelConfig) (string, error) {
	const script = `IFS= read -r model_api_key
exec buildmax-server model add \
  --name "$1" \
  --provider "$2" \
  --api-url "$3" \
  --model "$4" \
  --context-window "$5" \
  --currency "$6" \
  --input-price "$7" \
  --cache-read-price "$8" \
  --cache-write-price "$9" \
  --output-price "${10}" \
  --api-key "$model_api_key"`
	commandArgs := oceanAddModelCommand(cfg, model, script)
	cmd := exec.Command("kubectl", commandArgs...)
	cmd.Stdin = strings.NewReader(model.apiKey + "\n")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("initialize Ocean model catalog: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

func oceanAddModelCommand(cfg oceanConfig, model oceanModelConfig, script string) []string {
	return []string{
		"--kubeconfig", oceanKubeconfigPath(cfg),
		"exec", "--stdin", "--namespace", oceanNamespace, "--container", "server", "deploy/buildmax-server", "--",
		"/bin/sh", "-ec", script, "ocean-model-init",
		model.name,
		model.provider,
		model.apiURL,
		model.model,
		strconv.Itoa(model.contextWindow),
		model.currency,
		model.inputPrice,
		model.cacheReadPrice,
		model.cacheWritePrice,
		model.outputPrice,
	}
}

func oceanConfigureConversationModel(cfg oceanConfig, id string) error {
	if err := os.WriteFile(oceanModelTargetPath(cfg), []byte(id+"\n"), 0o600); err != nil {
		return fmt.Errorf("write Ocean conversation model target: %w", err)
	}
	if err := os.Chmod(oceanModelTargetPath(cfg), 0o600); err != nil {
		return fmt.Errorf("protect Ocean conversation model target: %w", err)
	}
	if err := oceanInit(cfg); err != nil {
		return err
	}
	app, err := loadOceanApplicationConfig()
	if err != nil {
		return err
	}
	serverConfig, _, _, err := oceanDeploymentInputs(cfg, app)
	if err != nil {
		return err
	}
	if err := oceanApplyObject(cfg, map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      "buildmax-config",
			"namespace": oceanNamespace,
		},
		"data": map[string]string{"server.yaml": serverConfig},
	}); err != nil {
		return fmt.Errorf("configure Ocean conversation model: %w", err)
	}
	if err := oceanKubectl(cfg, "rollout", "restart", "deployment/buildmax-server", "--namespace", oceanNamespace); err != nil {
		return err
	}
	if err := oceanKubectl(cfg, "rollout", "status", "deployment/buildmax-server", "--namespace", oceanNamespace, "--timeout=300s"); err != nil {
		return fmt.Errorf("restart BuildMax server with conversation model: %w", err)
	}
	return nil
}

func oceanModelTargetPath(cfg oceanConfig) string {
	return filepath.Join(cfg.stateDir, "conversation-model-target")
}
