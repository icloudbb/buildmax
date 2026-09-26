package main

import (
	"bytes"
	"errors"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/infra/secret"
)

func TestOceanConfigUsesPersistentResourceDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, name := range []string{
		"BUILDMAX_OCEAN_STATE_DIR",
		"BUILDMAX_OCEAN_PROJECT",
		"BUILDMAX_OCEAN_VPC",
		"BUILDMAX_OCEAN_BUCKET",
		"BUILDMAX_OCEAN_REGION",
		"BUILDMAX_OCEAN_DATABASE_VERSION",
	} {
		t.Setenv(name, "")
	}

	cfg, err := loadOceanConfig()
	if err != nil {
		t.Fatalf("loadOceanConfig: %v", err)
	}
	if cfg.project != "buildmax-beta" || cfg.vpc != "buildmax-beta" || cfg.bucket != "buildmax-beta" || cfg.region != "sgp1" || cfg.databaseVersion != "8.4" {
		t.Fatalf("defaults = %#v", cfg)
	}
	wantStateSuffix := filepath.Join(".buildmax", "qualification", "ocean")
	if filepath.Base(cfg.stateDir) != "ocean" || !pathHasSuffix(cfg.stateDir, wantStateSuffix) {
		t.Errorf("state dir = %q, want suffix %q", cfg.stateDir, wantStateSuffix)
	}
}

func TestOceanConfigAcceptsExplicitPersistentResources(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("BUILDMAX_OCEAN_STATE_DIR", stateDir)
	t.Setenv("BUILDMAX_OCEAN_PROJECT", "project-x")
	t.Setenv("BUILDMAX_OCEAN_VPC", "vpc-x")
	t.Setenv("BUILDMAX_OCEAN_BUCKET", "bucket-x")
	t.Setenv("BUILDMAX_OCEAN_REGION", "nyc3")
	t.Setenv("BUILDMAX_OCEAN_DATABASE_VERSION", "9.0")

	cfg, err := loadOceanConfig()
	if err != nil {
		t.Fatalf("loadOceanConfig: %v", err)
	}
	if cfg.project != "project-x" || cfg.vpc != "vpc-x" || cfg.bucket != "bucket-x" || cfg.region != "nyc3" || cfg.databaseVersion != "9.0" {
		t.Fatalf("overrides = %#v", cfg)
	}
	if cfg.stateDir != stateDir {
		t.Errorf("state dir = %q, want %q", cfg.stateDir, stateDir)
	}
}

func TestOceanStateCannotLiveInRepository(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("BUILDMAX_OCEAN_STATE_DIR", filepath.Join(root, ".artifacts", "ocean-state"))
	if _, err := loadOceanConfig(); err == nil {
		t.Fatal("loadOceanConfig accepted state inside the repository")
	}
}

func TestOceanFilesAreOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix permission bits")
	}
	cfg := oceanConfig{stateDir: t.TempDir()}
	for _, path := range []string{oceanStatePath(cfg), oceanKubeconfigPath(cfg), oceanPlanPath(cfg, false)} {
		if err := os.WriteFile(path, []byte("secret"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := protectOceanFiles(cfg); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{oceanStatePath(cfg), oceanKubeconfigPath(cfg), oceanPlanPath(cfg, false)} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("%s mode = %o, want 600", path, got)
		}
	}
}

func TestOceanDoctorDoesNotCreateStateDirectory(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "not-created")
	cfg := oceanConfig{
		project:         "buildmax-beta",
		vpc:             "buildmax-beta",
		bucket:          "buildmax-beta",
		region:          "sgp1",
		databaseVersion: "8.4",
		stateDir:        stateDir,
	}
	_ = oceanDoctor(cfg)
	if exists(stateDir) {
		t.Fatalf("oceanDoctor created %s", stateDir)
	}
}

func TestOceanApplicationConfigRequiresPrivateAccessBoundary(t *testing.T) {
	t.Setenv("BUILDMAX_OCEAN_HOSTNAME", "buildmax.beta.cloudbb.io")
	t.Setenv("BUILDMAX_OCEAN_ALLOWED_CIDRS", "")
	if _, err := loadOceanApplicationConfig(); err == nil {
		t.Fatal("loadOceanApplicationConfig accepted a public deployment with no allow-list")
	}
}

func TestOceanApplicationConfigPinsImagesAndNormalizesCIDRs(t *testing.T) {
	t.Setenv("BUILDMAX_OCEAN_HOSTNAME", "BuildMax.Beta.CloudBB.io")
	t.Setenv("BUILDMAX_OCEAN_ALLOWED_CIDRS", "203.0.113.7/32, 2001:db8::1/128")
	t.Setenv("BUILDMAX_OCEAN_IMAGE", "example/buildmax@sha256:"+strings.Repeat("a", 64))
	t.Setenv("BUILDMAX_OCEAN_PORTAL_IMAGE", "example/portal@sha256:"+strings.Repeat("b", 64))
	t.Setenv("BUILDMAX_OCEAN_EDGE_IMAGE", "example/edge@sha256:"+strings.Repeat("c", 64))

	cfg, err := loadOceanApplicationConfig()
	if err != nil {
		t.Fatalf("loadOceanApplicationConfig: %v", err)
	}
	if cfg.hostname != "buildmax.beta.cloudbb.io" {
		t.Errorf("hostname = %q", cfg.hostname)
	}
	if got := strings.Join(cfg.allowedCIDRs, ","); got != "203.0.113.7/32,2001:db8::1/128" {
		t.Errorf("allowed CIDRs = %q", got)
	}
	if cfg.buildmaxImage != "example/buildmax@sha256:"+strings.Repeat("a", 64) {
		t.Errorf("image = %q", cfg.buildmaxImage)
	}
}

func TestOceanApplicationConfigRejectsMutableImageTags(t *testing.T) {
	t.Setenv("BUILDMAX_OCEAN_HOSTNAME", "buildmax.beta.cloudbb.io")
	t.Setenv("BUILDMAX_OCEAN_ALLOWED_CIDRS", "203.0.113.7/32")
	t.Setenv("BUILDMAX_OCEAN_IMAGE", "ghcr.io/example/buildmax:latest")
	if _, err := loadOceanApplicationConfig(); err == nil {
		t.Fatal("loadOceanApplicationConfig accepted a mutable image tag")
	}
}

func TestOceanCaddyfileRestrictsApplicationAndRoutesOneOrigin(t *testing.T) {
	got := oceanCaddyfile(oceanApplicationConfig{
		hostname:     "buildmax.beta.cloudbb.io",
		allowedCIDRs: []string{"203.0.113.7/32"},
	})
	for _, want := range []string{
		"buildmax.beta.cloudbb.io",
		"not remote_ip 203.0.113.7/32",
		"reverse_proxy buildmax:5678",
		"reverse_proxy buildmax-portal:80",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Caddyfile missing %q:\n%s", want, got)
		}
	}
}

func TestOceanManifestUsesOnlyPinnedImages(t *testing.T) {
	app := oceanApplicationConfig{
		buildmaxImage: "example/buildmax@sha256:" + strings.Repeat("a", 64),
		portalImage:   "example/portal@sha256:" + strings.Repeat("b", 64),
		edgeImage:     "example/edge@sha256:" + strings.Repeat("c", 64),
	}
	manifest, err := renderOceanManifest(app)
	if err != nil {
		t.Fatal(err)
	}
	text := string(manifest)
	for _, want := range []string{app.buildmaxImage, app.portalImage, app.edgeImage, "externalTrafficPolicy: Local"} {
		if !strings.Contains(text, want) {
			t.Errorf("manifest missing %q", want)
		}
	}
	if strings.Contains(text, "{{") {
		t.Error("manifest contains an unresolved template expression")
	}
}

func TestOceanManifestMountsTheKEKWhereServerConfigPointsIt(t *testing.T) {
	manifest, err := renderOceanManifest(oceanApplicationConfig{})
	if err != nil {
		t.Fatal(err)
	}
	text := string(manifest)
	for _, want := range []string{"secretName: " + oceanKEKSecret, "mountPath: " + path.Dir(oceanKEKPath)} {
		if !strings.Contains(text, want) {
			t.Errorf("manifest missing %q", want)
		}
	}
}

func TestOceanServerConfigEnablesTheKEK(t *testing.T) {
	body := oceanServerConfig(oceanConfig{region: "sgp1"}, oceanApplicationConfig{
		hostname:      "buildmax.beta.cloudbb.io",
		buildmaxImage: "example/buildmax@sha256:" + strings.Repeat("a", 64),
	}, map[string]string{
		"database_private_host": "private-db.example",
		"database_port":         "25060",
		"database_user":         "doadmin",
		"database_name":         "buildmax",
		"spaces_endpoint":       "https://sgp1.digitaloceanspaces.com",
		"spaces_bucket_name":    "buildmax-beta",
	})
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "server.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvKeyBuildmaxHome, home)
	cfg, err := config.LoadServerConfig()
	if err != nil {
		t.Fatalf("ocean server.yaml does not load: %v", err)
	}
	if cfg.Secret.KEKFile != oceanKEKPath {
		t.Errorf("secret.kek_file = %q, want %q; without it `model add --api-key` is refused", cfg.Secret.KEKFile, oceanKEKPath)
	}
}

func TestOceanKEKIsGeneratedOnceAndNeverReplaced(t *testing.T) {
	cfg := oceanConfig{stateDir: t.TempDir()}
	first, err := oceanKEK(cfg, func() (bool, error) { return false, nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := secret.LoadKEKFile(oceanKEKStatePath(cfg)); err != nil {
		t.Fatalf("generated KEK does not load as the server loads it: %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(oceanKEKStatePath(cfg))
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("KEK mode = %o, want 600", got)
		}
	}
	// The next deploy finds the Secret in the cluster and must ship the same key.
	second, err := oceanKEK(cfg, func() (bool, error) { return true, nil })
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("a second deploy produced a different KEK")
	}
}

func TestOceanKEKRefusesToReplaceAClusterKey(t *testing.T) {
	cfg := oceanConfig{stateDir: t.TempDir()}
	if _, err := oceanKEK(cfg, func() (bool, error) { return true, nil }); err == nil {
		t.Fatal("generated a new KEK while the cluster already holds one")
	}
	if exists(oceanKEKStatePath(cfg)) {
		t.Error("wrote a replacement KEK file")
	}
	lookupFailed := errors.New("cluster unreachable")
	if _, err := oceanKEK(cfg, func() (bool, error) { return false, lookupFailed }); !errors.Is(err, lookupFailed) {
		t.Fatalf("err = %v, want the cluster lookup failure", err)
	}
	if exists(oceanKEKStatePath(cfg)) {
		t.Error("generated a KEK without knowing whether the cluster holds one")
	}
}

func TestOceanKEKRejectsAMalformedFile(t *testing.T) {
	cfg := oceanConfig{stateDir: t.TempDir()}
	if err := os.WriteFile(oceanKEKStatePath(cfg), []byte(`{"current":"file:root:1","keys":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := oceanKEK(cfg, func() (bool, error) { return false, nil }); err == nil {
		t.Fatal("accepted a KEK file the server would refuse")
	}
	data, err := os.ReadFile(oceanKEKStatePath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"keys":{}`) {
		t.Error("overwrote the operator's KEK file")
	}
}

func TestOceanShowAllUsesBuildMaxNamespace(t *testing.T) {
	got, err := oceanShowKubectlArgs([]string{"all"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"get", "all", "--namespace", "buildmax", "--output", "wide"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("kubectl args = %q, want %q", got, want)
	}
}

func TestOceanShowRejectsMissingExtraAndUnknownResources(t *testing.T) {
	for _, args := range [][]string{nil, {"all", "extra"}, {"secrets"}} {
		if _, err := oceanShowKubectlArgs(args); err == nil {
			t.Errorf("oceanShowKubectlArgs(%q) succeeded", args)
		}
	}
}

func TestOceanInfoRequiresExplicitSecretsFlag(t *testing.T) {
	if show, err := oceanInfoShowsSecrets(nil); err != nil || show {
		t.Fatalf("no flag = (%v, %v), want (false, nil)", show, err)
	}
	if show, err := oceanInfoShowsSecrets([]string{"--show-secrets"}); err != nil || !show {
		t.Fatalf("show flag = (%v, %v), want (true, nil)", show, err)
	}
	if _, err := oceanInfoShowsSecrets([]string{"--bad"}); err == nil {
		t.Fatal("unknown info flag succeeded")
	}
}

func TestOceanModelDefaultsToOpenRouterBaseline(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "secret-key")
	for _, name := range []string{
		"BUILDMAX_OCEAN_MODEL_NAME",
		"BUILDMAX_OCEAN_MODEL_PROVIDER",
		"BUILDMAX_OCEAN_MODEL_API_URL",
		"BUILDMAX_OCEAN_MODEL_ID",
		"BUILDMAX_OCEAN_MODEL_CONTEXT_WINDOW",
	} {
		t.Setenv(name, "")
	}
	model, err := loadOceanModelConfig()
	if err != nil {
		t.Fatal(err)
	}
	if model.name != "GPT-5.6 Luna" || model.provider != "openai" || model.model != "openai/gpt-5.6-luna" || model.contextWindow != 1050000 {
		t.Fatalf("model defaults = %#v", model)
	}
}

func TestOceanModelCommandDoesNotContainAPIKey(t *testing.T) {
	model := oceanModelConfig{apiKey: "must-not-appear"}
	args := oceanAddModelCommand(oceanConfig{stateDir: t.TempDir()}, model, "read-key-from-stdin")
	if strings.Contains(strings.Join(args, " "), model.apiKey) {
		t.Fatal("kubectl command line contains the model API key")
	}
}

func TestOceanDatabaseForwardUsesNonstandardLocalPort(t *testing.T) {
	t.Setenv("BUILDMAX_OCEAN_DATABASE_LOCAL_PORT", "")
	port, err := oceanDatabaseLocalPort()
	if err != nil {
		t.Fatal(err)
	}
	if port != 13306 {
		t.Fatalf("local database port = %d, want 13306", port)
	}
}

func TestPathWithinTreatsUnrelatablePathsAsOutside(t *testing.T) {
	// Windows CI checks out on D: while the default state directory is under
	// C:, and filepath.Rel cannot relate two volumes.
	if pathWithin(filepath.FromSlash("/repo"), "relative-path") {
		t.Fatal("unrelatable paths reported as inside the repository")
	}
}

func pathHasSuffix(path, suffix string) bool {
	return strings.HasSuffix(filepath.Clean(path), filepath.Clean(suffix))
}
