package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const oceanConfigDir = "deployment/ocean"

type oceanConfig struct {
	project         string
	vpc             string
	bucket          string
	region          string
	databaseVersion string
	stateDir        string
}

func cmdOcean(args []string) error {
	if len(args) == 0 {
		return usageErrorf("ocean", "ocean needs an action")
	}
	if args[0] != "show" && args[0] != "model" && args[0] != "database" && args[0] != "info" && len(args) > 1 {
		return usageErrorf("ocean", "ocean takes exactly one action")
	}

	cfg, err := loadOceanConfig()
	if err != nil {
		return err
	}
	switch args[0] {
	case "doctor":
		return oceanDoctor(cfg)
	case "plan":
		return oceanPlan(cfg, false)
	case "up":
		return oceanUp(cfg)
	case "deploy":
		return oceanDeploy(cfg)
	case "info":
		return oceanInfo(cfg, args[1:])
	case "app-status":
		return oceanAppStatus(cfg)
	case "show":
		return oceanShow(cfg, args[1:])
	case "model":
		return oceanModel(cfg, args[1:])
	case "database":
		return oceanDatabase(cfg, args[1:])
	case "status":
		return oceanStatus(cfg)
	case "down":
		return oceanDown(cfg)
	default:
		return usageErrorf("ocean", "unknown ocean action: %s", args[0])
	}
}

func loadOceanConfig() (oceanConfig, error) {
	stateDir := os.Getenv("BUILDMAX_OCEAN_STATE_DIR")
	if stateDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return oceanConfig{}, fmt.Errorf("locate home directory for ocean state: %w", err)
		}
		stateDir = filepath.Join(home, ".buildmax", "qualification", "ocean")
	}
	absStateDir, err := filepath.Abs(stateDir)
	if err != nil {
		return oceanConfig{}, fmt.Errorf("resolve ocean state directory: %w", err)
	}
	root, err := moduleRoot()
	if err != nil {
		return oceanConfig{}, err
	}
	if pathWithin(root, absStateDir) {
		return oceanConfig{}, errors.New("BUILDMAX_OCEAN_STATE_DIR must be outside the repository because state contains credentials")
	}

	return oceanConfig{
		project:         envOr("BUILDMAX_OCEAN_PROJECT", "buildmax-beta"),
		vpc:             envOr("BUILDMAX_OCEAN_VPC", "buildmax-beta"),
		bucket:          envOr("BUILDMAX_OCEAN_BUCKET", "buildmax-beta"),
		region:          envOr("BUILDMAX_OCEAN_REGION", "sgp1"),
		databaseVersion: envOr("BUILDMAX_OCEAN_DATABASE_VERSION", "8.4"),
		stateDir:        absStateDir,
	}, nil
}

// A Rel error means the paths share no common root -- different Windows
// volumes -- so the child is outside the parent, which is what we want to know.
func pathWithin(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func oceanDoctor(cfg oceanConfig) error {
	failed := false
	fmt.Println("Tools")
	for _, name := range []string{"tofu", "kubectl"} {
		if have(name) {
			fmt.Printf("  ok       %s\n", name)
		} else {
			fmt.Printf("  missing  %s\n", name)
			failed = true
		}
	}

	fmt.Println("\nCredentials loaded from the environment or " + localEnvPath)
	for _, name := range []string{"DIGITALOCEAN_TOKEN", "SPACES_ACCESS_KEY_ID", "SPACES_SECRET_ACCESS_KEY"} {
		if os.Getenv(name) != "" {
			fmt.Printf("  ok       %s\n", name)
		} else {
			fmt.Printf("  missing  %s\n", name)
			failed = true
		}
	}

	fmt.Println("\nPersistent resources (read only)")
	fmt.Printf("  Project        %s\n  VPC            %s\n  Bucket         %s\n  Region         %s\n  MySQL version  %s\n", cfg.project, cfg.vpc, cfg.bucket, cfg.region, cfg.databaseVersion)
	fmt.Printf("\nLocal state (contains credentials)\n  %s\n", cfg.stateDir)
	if failed {
		return errors.New("ocean prerequisites are incomplete; no resources were changed")
	}
	fmt.Println("\nReady. This check did not call DigitalOcean or change local files.")
	return nil
}

func oceanPlan(cfg oceanConfig, destroy bool) error {
	if err := oceanPrepare(cfg); err != nil {
		return err
	}
	if err := oceanTofu(cfg, nil, "validate"); err != nil {
		return err
	}
	args := []string{"plan", "-input=false", "-out=" + oceanPlanPath(cfg, destroy)}
	if destroy {
		args = append(args, "-destroy")
	}
	if err := oceanTofu(cfg, nil, args...); err != nil {
		return err
	}
	if err := os.Chmod(oceanPlanPath(cfg, destroy), 0o600); err != nil {
		return fmt.Errorf("protect ocean plan: %w", err)
	}
	fmt.Printf("\nSaved plan: %s\n", oceanPlanPath(cfg, destroy))
	return nil
}

func oceanUp(cfg oceanConfig) error {
	if err := oceanPlan(cfg, false); err != nil {
		return err
	}
	fmt.Println("\nThis creates billable DOKS and managed MySQL resources. DOKS HA is disabled.")
	if err := confirmOcean(cfg.project); err != nil {
		return err
	}
	if err := oceanTofu(cfg, nil, "apply", "-input=false", oceanPlanPath(cfg, false)); err != nil {
		return fmt.Errorf("%w; an apply can fail after creating some resources, so run `%s ocean status` before retrying", err, mk())
	}
	if err := oceanWriteKubeconfig(cfg); err != nil {
		return err
	}
	if err := protectOceanFiles(cfg); err != nil {
		return fmt.Errorf("protect ocean files: %w", err)
	}
	fmt.Printf("\nInfrastructure is ready. Kubeconfig: %s\nRun `%s ocean info` for safe connection details.\n", oceanKubeconfigPath(cfg), mk())
	return nil
}

func oceanInfo(cfg oceanConfig, args []string) error {
	showSecrets, err := oceanInfoShowsSecrets(args)
	if err != nil {
		return err
	}
	if !exists(oceanStatePath(cfg)) {
		fmt.Printf("No ocean state exists at %s. Run `%s ocean up` first.\n", oceanStatePath(cfg), mk())
		return nil
	}
	if err := oceanInit(cfg); err != nil {
		return err
	}
	fmt.Println("DigitalOcean beta qualification")
	for _, output := range []struct {
		label string
		name  string
	}{
		{"Project", "project_name"},
		{"Region", "region"},
		{"DOKS cluster", "kubernetes_cluster_name"},
		{"DOKS endpoint", "kubernetes_endpoint"},
		{"MySQL private host", "database_private_host"},
		{"MySQL port", "database_port"},
		{"MySQL database", "database_name"},
		{"Spaces bucket", "spaces_bucket_name"},
		{"Spaces endpoint", "spaces_endpoint"},
	} {
		value, err := oceanOutput(cfg, output.name)
		if err != nil {
			return err
		}
		fmt.Printf("  %-20s %s\n", output.label, value)
	}
	fmt.Printf("  %-20s %s\n", "Kubeconfig", oceanKubeconfigPath(cfg))
	if !showSecrets {
		fmt.Printf("\nDatabase credentials are hidden. Run `%s ocean info --show-secrets` to print them.\n", mk())
		fmt.Println("Kubeconfig contents are intentionally not printed.")
		return nil
	}
	for _, output := range []struct {
		label string
		name  string
	}{
		{"MySQL user", "database_user"},
		{"MySQL password", "database_password"},
	} {
		value, err := oceanOutput(cfg, output.name)
		if err != nil {
			return err
		}
		fmt.Printf("  %-20s %s\n", output.label, value)
	}
	fmt.Println("\nWarning: the values above grant database access. Do not paste or commit this output.")
	fmt.Println("Kubeconfig contents are intentionally not printed.")
	return nil
}

func oceanInfoShowsSecrets(args []string) (bool, error) {
	switch {
	case len(args) == 0:
		return false, nil
	case len(args) == 1 && args[0] == "--show-secrets":
		return true, nil
	case len(args) > 1:
		return false, usageErrorf("ocean", "ocean info takes at most one flag")
	default:
		return false, usageErrorf("ocean", "unknown ocean info flag: %s", args[0])
	}
}

func oceanStatus(cfg oceanConfig) error {
	if !exists(oceanStatePath(cfg)) {
		fmt.Printf("No ocean state exists at %s. OpenTofu manages no resources here.\n", oceanStatePath(cfg))
		return nil
	}
	if err := oceanInit(cfg); err != nil {
		return err
	}
	fmt.Println("OpenTofu state entries (data.* resources are persistent and read only)")
	if err := oceanTofu(cfg, nil, "state", "list"); err != nil {
		return err
	}
	fmt.Printf("\nState: %s\n", oceanStatePath(cfg))
	return nil
}

func oceanDown(cfg oceanConfig) error {
	if !exists(oceanStatePath(cfg)) {
		fmt.Printf("No ocean state exists at %s; nothing will be destroyed.\n", oceanStatePath(cfg))
		return nil
	}
	if err := oceanPlan(cfg, true); err != nil {
		return err
	}
	fmt.Println("\nThis destroys only the OpenTofu-managed DOKS and MySQL resources.")
	if err := confirmOcean(cfg.project); err != nil {
		return err
	}
	if err := oceanTofu(cfg, nil, "apply", "-input=false", oceanPlanPath(cfg, true)); err != nil {
		return err
	}
	if err := os.Remove(oceanKubeconfigPath(cfg)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove expired ocean kubeconfig: %w", err)
	}
	if err := os.Remove(oceanModelTargetPath(cfg)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove expired ocean model target: %w", err)
	}
	if err := protectOceanFiles(cfg); err != nil {
		return fmt.Errorf("protect ocean files: %w", err)
	}
	fmt.Println("\nDisposable DigitalOcean resources were destroyed. Project, VPC, and Spaces bucket were retained.")
	return nil
}

func oceanPrepare(cfg oceanConfig) error {
	for _, name := range []string{"DIGITALOCEAN_TOKEN", "SPACES_ACCESS_KEY_ID", "SPACES_SECRET_ACCESS_KEY"} {
		if os.Getenv(name) == "" {
			return fmt.Errorf("%s is not set; add it to %s", name, localEnvPath)
		}
	}
	return oceanInit(cfg)
}

func oceanInit(cfg oceanConfig) error {
	if err := requireCommands("tofu"); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.stateDir, 0o700); err != nil {
		return fmt.Errorf("create ocean state directory: %w", err)
	}
	if err := os.Chmod(cfg.stateDir, 0o700); err != nil {
		return fmt.Errorf("protect ocean state directory: %w", err)
	}
	return oceanTofu(cfg, nil, "init", "-input=false", "-backend-config=path="+oceanStatePath(cfg))
}

func oceanTofu(cfg oceanConfig, stdout *bytes.Buffer, args ...string) error {
	commandArgs := append([]string{"-chdir=" + oceanConfigDir}, args...)
	cmd := exec.Command("tofu", commandArgs...)
	cmd.Stdin = os.Stdin
	if stdout == nil {
		cmd.Stdout = os.Stdout
	} else {
		cmd.Stdout = stdout
	}
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), oceanEnv(cfg)...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", commandLine("tofu", commandArgs), err)
	}
	if err := protectOceanFiles(cfg); err != nil {
		return fmt.Errorf("protect ocean files: %w", err)
	}
	return nil
}

func oceanEnv(cfg oceanConfig) []string {
	return []string{
		"TF_DATA_DIR=" + filepath.Join(cfg.stateDir, ".terraform"),
		"TF_VAR_project_name=" + cfg.project,
		"TF_VAR_vpc_name=" + cfg.vpc,
		"TF_VAR_spaces_bucket_name=" + cfg.bucket,
		"TF_VAR_region=" + cfg.region,
		"TF_VAR_database_version=" + cfg.databaseVersion,
	}
}

func oceanOutput(cfg oceanConfig, name string) (string, error) {
	var stdout bytes.Buffer
	if err := oceanTofu(cfg, &stdout, "output", "-raw", name); err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

func oceanWriteKubeconfig(cfg oceanConfig) error {
	value, err := oceanOutput(cfg, "kubeconfig")
	if err != nil {
		return err
	}
	if err := os.WriteFile(oceanKubeconfigPath(cfg), []byte(value+"\n"), 0o600); err != nil {
		return fmt.Errorf("write ocean kubeconfig: %w", err)
	}
	return os.Chmod(oceanKubeconfigPath(cfg), 0o600)
}

func confirmOcean(project string) error {
	fmt.Printf("Type %q to continue: ", project)
	value, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return fmt.Errorf("read confirmation: %w", err)
	}
	if strings.TrimSpace(value) != project {
		return errors.New("confirmation did not match; no plan was applied")
	}
	return nil
}

func protectOceanFiles(cfg oceanConfig) error {
	for _, path := range []string{
		oceanStatePath(cfg),
		oceanStatePath(cfg) + ".backup",
		oceanKubeconfigPath(cfg),
		oceanModelTargetPath(cfg),
		oceanKEKStatePath(cfg),
		oceanDatabaseCAPath(cfg),
		oceanPlanPath(cfg, false),
		oceanPlanPath(cfg, true),
	} {
		if exists(path) {
			if err := os.Chmod(path, 0o600); err != nil {
				return err
			}
		}
	}
	return nil
}

func oceanPlanPath(cfg oceanConfig, destroy bool) string {
	name := "up.tfplan"
	if destroy {
		name = "down.tfplan"
	}
	return filepath.Join(cfg.stateDir, name)
}

func oceanStatePath(cfg oceanConfig) string {
	return filepath.Join(cfg.stateDir, "terraform.tfstate")
}

func oceanKubeconfigPath(cfg oceanConfig) string {
	return filepath.Join(cfg.stateDir, "kubeconfig.yaml")
}
