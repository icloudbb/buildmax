package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"github.com/icloudbb/buildmax/internal/infra/secret"
)

const (
	oceanManifestTemplate = "deployment/ocean/buildmax.yaml.tmpl"
	oceanNamespace        = "buildmax"

	// The deployment KEK: the Secret the manifest mounts, the path server.yaml
	// names inside the server pod, and the key id of the one generated key.
	oceanKEKSecret = "buildmax-kek"
	oceanKEKPath   = "/etc/buildmax/kek/kek.json"
	oceanKEKKeyID  = "file:root:1"

	// These are the immutable multi-platform release manifests, not mutable
	// tags. A later candidate is selected explicitly through the matching env
	// variables and recorded in the qualification evidence.
	defaultOceanBuildMaxImage = "ghcr.io/icloudbb/buildmax@sha256:64e6775796b4bf0cb1145e3aaa79084e170f1ec340bd5af1cddc1a28cc0336dd"
	defaultOceanPortalImage   = "ghcr.io/icloudbb/buildmax-portal@sha256:82165de877e4cae3c5a1c598b6f39b37a94db114ab6ce315b237d5913f7e2e2b"
	defaultOceanEdgeImage     = "caddy:2.10.2-alpine@sha256:4c6e91c6ed0e2fa03efd5b44747b625fec79bc9cd06ac5235a779726618e530d"
)

var (
	oceanHostnamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)
	oceanDigestPattern   = regexp.MustCompile(`@sha256:[0-9a-f]{64}$`)
)

type oceanApplicationConfig struct {
	hostname      string
	allowedCIDRs  []string
	buildmaxImage string
	portalImage   string
	edgeImage     string
}

type oceanManifestData struct {
	BuildMaxImage string
	PortalImage   string
	EdgeImage     string
}

func loadOceanApplicationConfig() (oceanApplicationConfig, error) {
	hostname := strings.ToLower(strings.TrimSpace(os.Getenv("BUILDMAX_OCEAN_HOSTNAME")))
	if !oceanHostnamePattern.MatchString(hostname) || len(hostname) > 253 {
		return oceanApplicationConfig{}, errors.New("BUILDMAX_OCEAN_HOSTNAME must be a full hostname such as beta.example.com")
	}
	allowed, err := parseOceanCIDRs(os.Getenv("BUILDMAX_OCEAN_ALLOWED_CIDRS"))
	if err != nil {
		return oceanApplicationConfig{}, err
	}
	if len(allowed) == 0 {
		return oceanApplicationConfig{}, errors.New("BUILDMAX_OCEAN_ALLOWED_CIDRS is required so the beta is not exposed to the untrusted public network")
	}

	cfg := oceanApplicationConfig{
		hostname:      hostname,
		allowedCIDRs:  allowed,
		buildmaxImage: envOr("BUILDMAX_OCEAN_IMAGE", defaultOceanBuildMaxImage),
		portalImage:   envOr("BUILDMAX_OCEAN_PORTAL_IMAGE", defaultOceanPortalImage),
		edgeImage:     envOr("BUILDMAX_OCEAN_EDGE_IMAGE", defaultOceanEdgeImage),
	}
	for name, image := range map[string]string{
		"BUILDMAX_OCEAN_IMAGE":        cfg.buildmaxImage,
		"BUILDMAX_OCEAN_PORTAL_IMAGE": cfg.portalImage,
		"BUILDMAX_OCEAN_EDGE_IMAGE":   cfg.edgeImage,
	} {
		if !oceanDigestPattern.MatchString(image) {
			return oceanApplicationConfig{}, fmt.Errorf("%s must pin an image digest, got %q", name, image)
		}
	}
	return cfg, nil
}

func parseOceanCIDRs(value string) ([]string, error) {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n'
	})
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		_, network, err := net.ParseCIDR(field)
		if err != nil {
			return nil, fmt.Errorf("BUILDMAX_OCEAN_ALLOWED_CIDRS contains invalid CIDR %q", field)
		}
		result = append(result, network.String())
	}
	return result, nil
}

func oceanDeploy(cfg oceanConfig) error {
	app, err := loadOceanApplicationConfig()
	if err != nil {
		return err
	}
	if err := requireCommands("tofu", "kubectl"); err != nil {
		return err
	}
	if !exists(oceanStatePath(cfg)) || !exists(oceanKubeconfigPath(cfg)) {
		return fmt.Errorf("ocean infrastructure is not ready; run `%s ocean up` first", mk())
	}
	if err := oceanPrepare(cfg); err != nil {
		return err
	}

	// The CA data source was added after the first infrastructure run. A
	// refresh-only apply records it and new outputs without changing cloud
	// resources or accepting an ordinary create/update plan.
	if err := oceanTofu(cfg, nil, "apply", "-refresh-only", "-auto-approve", "-input=false"); err != nil {
		return fmt.Errorf("refresh DigitalOcean outputs: %w", err)
	}

	serverConfig, databaseCA, secretData, err := oceanDeploymentInputs(cfg, app)
	if err != nil {
		return err
	}
	kek, err := oceanKEK(cfg, func() (bool, error) {
		name, err := oceanKubectlOutput(cfg, "get", "secret", oceanKEKSecret, "--namespace", oceanNamespace, "--ignore-not-found", "--output", "name")
		return name != "", err
	})
	if err != nil {
		return err
	}
	caddyConfig := oceanCaddyfile(app)

	if err := oceanApplyObject(cfg, map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]any{
			"name": "buildmax",
		},
	}); err != nil {
		return err
	}
	for _, object := range []map[string]any{
		{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": "buildmax-config", "namespace": "buildmax"},
			"data":       map[string]string{"server.yaml": serverConfig},
		},
		{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": "buildmax-database-ca", "namespace": "buildmax"},
			"data":       map[string]string{"database-ca.pem": databaseCA},
		},
		{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": "buildmax-edge", "namespace": "buildmax"},
			"data":       map[string]string{"Caddyfile": caddyConfig},
		},
		{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata":   map[string]any{"name": "buildmax-secret", "namespace": "buildmax"},
			"type":       "Opaque",
			"stringData": secretData,
		},
		{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata":   map[string]any{"name": oceanKEKSecret, "namespace": "buildmax"},
			"type":       "Opaque",
			"stringData": map[string]string{"kek.json": string(kek)},
		},
	} {
		if err := oceanApplyObject(cfg, object); err != nil {
			return err
		}
	}

	manifest, err := renderOceanManifest(app)
	if err != nil {
		return err
	}
	if err := oceanKubectlInput(cfg, manifest, "apply", "-f", "-"); err != nil {
		return err
	}
	for _, deployment := range []string{"buildmax-server", "buildmax-portal", "buildmax-edge"} {
		if err := oceanKubectl(cfg, "rollout", "restart", "deployment/"+deployment, "-n", "buildmax"); err != nil {
			return err
		}
	}
	for _, deployment := range []string{"buildmax-server", "buildmax-portal", "buildmax-edge"} {
		if err := oceanKubectl(cfg, "rollout", "status", "deployment/"+deployment, "-n", "buildmax", "--timeout=300s"); err != nil {
			return fmt.Errorf("%s rollout: %w; inspect it with `%s ocean app-status`", deployment, err, mk())
		}
	}

	fmt.Printf("\nBuildMax is deployed for https://%s.\n", app.hostname)
	fmt.Printf("The edge is limited to: %s\n", strings.Join(app.allowedCIDRs, ", "))
	return oceanPrintLoadBalancer(cfg)
}

func oceanDeploymentInputs(cfg oceanConfig, app oceanApplicationConfig) (string, string, map[string]string, error) {
	outputs := make(map[string]string)
	for _, name := range []string{
		"database_private_host",
		"database_port",
		"database_user",
		"database_password",
		"database_ca",
		"database_name",
		"spaces_bucket_name",
		"spaces_endpoint",
	} {
		value, err := oceanOutput(cfg, name)
		if err != nil {
			return "", "", nil, err
		}
		outputs[name] = value
	}
	if !strings.Contains(outputs["database_ca"], "BEGIN CERTIFICATE") {
		return "", "", nil, errors.New("DigitalOcean database CA output is not a PEM certificate")
	}
	jwtSecret, err := oceanJWTSecret(cfg)
	if err != nil {
		return "", "", nil, err
	}

	serverConfig := oceanServerConfig(cfg, app, outputs)
	if target, err := os.ReadFile(oceanModelTargetPath(cfg)); err == nil {
		serverConfig += fmt.Sprintf("\nconversation:\n  model_target: %s\n", yamlString(strings.TrimSpace(string(target))))
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", nil, fmt.Errorf("read Ocean conversation model target: %w", err)
	}

	return serverConfig, outputs["database_ca"], map[string]string{
		"BUILDMAX_JWT_SECRET":               jwtSecret,
		"BUILDMAX_DATABASE_PASSWORD":        outputs["database_password"],
		"BUILDMAX_STORAGE_MINIO_ACCESS_KEY": os.Getenv("SPACES_ACCESS_KEY_ID"),
		"BUILDMAX_STORAGE_MINIO_SECRET_KEY": os.Getenv("SPACES_SECRET_ACCESS_KEY"),
	}, nil
}

// oceanServerConfig renders server.yaml from the OpenTofu outputs.
func oceanServerConfig(cfg oceanConfig, app oceanApplicationConfig, outputs map[string]string) string {
	return fmt.Sprintf(`port: 5678
log_level: info
workspaces_dir: /buildmax/workspaces
cors_origin: %s
allow_signup: false
shutdown_grace: 25s

database:
  host: %s
  port: %s
  user: %s
  name: %s
  tls: "true"

storage:
  persist_backend: minio
  artifact_backend: minio
  minio:
    endpoint: %s
    region: %s
    bucket: %s
    prefix: qualification
    path_style: false

worker:
  run_mode: k8s_job
  # STOPGAP: public HTTP port; M3 switches to the https buildmax-worker-api
  # Service and drops allow_insecure_http.
  server_url: http://buildmax.buildmax.svc.cluster.local:5678
  allow_insecure_http: true
  k8s:
    namespace: buildmax
    image: %s
    config_map: buildmax-config
    home_dir: /buildmax
    resources:
      cpu_request: 500m
      cpu_limit: "2"
      memory_request: 1Gi
      memory_limit: 2Gi

secret:
  kek_file: %s
`, yamlString("https://"+app.hostname), yamlString(outputs["database_private_host"]), outputs["database_port"], yamlString(outputs["database_user"]), yamlString(outputs["database_name"]), yamlString(outputs["spaces_endpoint"]), yamlString(cfg.region), yamlString(outputs["spaces_bucket_name"]), yamlString(app.buildmaxImage), yamlString(oceanKEKPath))
}

func yamlString(value string) string {
	return strconv.Quote(value)
}

func oceanCaddyfile(app oceanApplicationConfig) string {
	return fmt.Sprintf(`%s {
	@blocked not remote_ip %s
	respond @blocked "Forbidden" 403

	@api path /api /api/* /openapi.json /swagger*
	handle @api {
		reverse_proxy buildmax:5678
	}
	handle {
		reverse_proxy buildmax-portal:80
	}
}
`, app.hostname, strings.Join(app.allowedCIDRs, " "))
}

func oceanJWTSecret(cfg oceanConfig) (string, error) {
	path := filepath.Join(cfg.stateDir, "jwt-secret")
	data, err := os.ReadFile(path)
	if err == nil {
		value := strings.TrimSpace(string(data))
		if value == "" {
			return "", errors.New("ocean JWT secret file is empty")
		}
		return value, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read ocean JWT secret: %w", err)
	}
	value, err := randomHex(32)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write ocean JWT secret: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", fmt.Errorf("protect ocean JWT secret: %w", err)
	}
	return value, nil
}

// oceanKEK returns the deployment KEK file kept in the state directory,
// generating it on the first deploy. It is never regenerated: the database
// holds model credentials and Space Secrets sealed under it, and new bytes
// under the same key id would leave them unreadable. So a missing local file while the
// cluster already holds a KEK Secret is refused rather than replaced.
func oceanKEK(cfg oceanConfig, clusterHasKEK func() (bool, error)) ([]byte, error) {
	path := oceanKEKStatePath(cfg)
	data, err := os.ReadFile(path)
	if err == nil {
		// Refuse a file the server would refuse, before it crash-loops on it.
		if _, err := secret.LoadKEKFile(path); err != nil {
			return nil, fmt.Errorf("ocean KEK %s: %w", path, err)
		}
		return data, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read ocean KEK: %w", err)
	}
	present, err := clusterHasKEK()
	if err != nil {
		return nil, fmt.Errorf("check the cluster for an existing KEK: %w", err)
	}
	if present {
		return nil, fmt.Errorf("%s is missing but the cluster already has the %s Secret; restore the file from your backup rather than generating a new key, which would make the stored model credentials unreadable", path, oceanKEKSecret)
	}
	data, err = newKEKFile(oceanKEKKeyID)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, fmt.Errorf("write ocean KEK: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("protect ocean KEK: %w", err)
	}
	fmt.Printf("Generated the deployment KEK at %s. Back it up apart from the database: without it, sealed credentials cannot be read.\n", path)
	return data, nil
}

func oceanKEKStatePath(cfg oceanConfig) string {
	return filepath.Join(cfg.stateDir, "kek.json")
}

func renderOceanManifest(app oceanApplicationConfig) ([]byte, error) {
	root, err := moduleRoot()
	if err != nil {
		return nil, err
	}
	source, err := os.ReadFile(filepath.Join(root, oceanManifestTemplate))
	if err != nil {
		return nil, fmt.Errorf("read ocean application manifest: %w", err)
	}
	tmpl, err := template.New("ocean").Parse(string(source))
	if err != nil {
		return nil, fmt.Errorf("parse ocean application manifest: %w", err)
	}
	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, oceanManifestData{
		BuildMaxImage: app.buildmaxImage,
		PortalImage:   app.portalImage,
		EdgeImage:     app.edgeImage,
	}); err != nil {
		return nil, fmt.Errorf("render ocean application manifest: %w", err)
	}
	return rendered.Bytes(), nil
}

func oceanApplyObject(cfg oceanConfig, object map[string]any) error {
	data, err := json.Marshal(object)
	if err != nil {
		return err
	}
	return oceanKubectlInput(cfg, data, "apply", "-f", "-")
}

func oceanKubectl(cfg oceanConfig, args ...string) error {
	return runCmd("kubectl", append([]string{"--kubeconfig", oceanKubeconfigPath(cfg)}, args...)...)
}

func oceanKubectlInput(cfg oceanConfig, input []byte, args ...string) error {
	commandArgs := append([]string{"--kubeconfig", oceanKubeconfigPath(cfg)}, args...)
	cmd := exec.Command("kubectl", commandArgs...)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", commandLine("kubectl", commandArgs), err)
	}
	return nil
}

func oceanKubectlOutput(cfg oceanConfig, args ...string) (string, error) {
	commandArgs := append([]string{"--kubeconfig", oceanKubeconfigPath(cfg)}, args...)
	cmd := exec.Command("kubectl", commandArgs...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s: %w: %s", commandLine("kubectl", commandArgs), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(output)), nil
}

func oceanAppStatus(cfg oceanConfig) error {
	if err := requireCommands("kubectl"); err != nil {
		return err
	}
	if !exists(oceanKubeconfigPath(cfg)) {
		return fmt.Errorf("ocean kubeconfig does not exist; run `%s ocean up` first", mk())
	}
	if err := oceanKubectl(cfg, "get", "pods,services", "-n", oceanNamespace, "-o", "wide"); err != nil {
		return err
	}
	fmt.Println()
	return oceanPrintLoadBalancer(cfg)
}

func oceanShow(cfg oceanConfig, args []string) error {
	kubectlArgs, err := oceanShowKubectlArgs(args)
	if err != nil {
		return err
	}
	if !exists(oceanKubeconfigPath(cfg)) {
		return fmt.Errorf("ocean kubeconfig does not exist; run `%s ocean up` first", mk())
	}
	return oceanKubectl(cfg, kubectlArgs...)
}

func oceanShowKubectlArgs(args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, usageErrorf("ocean", "ocean show needs a resource")
	}
	if len(args) > 1 {
		return nil, usageErrorf("ocean", "ocean show takes exactly one resource")
	}
	if args[0] != "all" {
		return nil, usageErrorf("ocean", "unknown ocean show resource: %s", args[0])
	}
	return []string{"get", "all", "--namespace", oceanNamespace, "--output", "wide"}, nil
}

func oceanPrintLoadBalancer(cfg oceanConfig) error {
	address, err := oceanKubectlOutput(cfg, "get", "service", "buildmax-edge", "-n", oceanNamespace, "-o", `jsonpath={.status.loadBalancer.ingress[0].ip}`)
	if err != nil {
		return err
	}
	hostname := strings.TrimSpace(os.Getenv("BUILDMAX_OCEAN_HOSTNAME"))
	if address == "" {
		fmt.Printf("Load Balancer address is pending. Rerun `%s ocean app-status`.\n", mk())
		return nil
	}
	fmt.Printf("Load Balancer IP: %s\n", address)
	if hostname != "" {
		fmt.Printf("Add this Route 53 record manually:\n  %s  A  %s\n", hostname, address)
	}
	return nil
}
