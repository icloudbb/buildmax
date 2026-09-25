package main

import (
	"os"
	"regexp"
	"testing"

	"gopkg.in/yaml.v3"
)

// The rendered config must hand the network to Cilium: without
// disableDefaultCNI kind installs kindnet as well, and two plugins race for
// every pod.
func TestRenderKindConfigLeavesTheNetworkToCilium(t *testing.T) {
	t.Chdir("../..")
	path, cleanup, err := renderKindConfig()
	if err != nil {
		t.Fatalf("renderKindConfig: %v", err)
	}
	t.Cleanup(cleanup)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var cfg struct {
		Networking struct {
			DisableDefaultCNI bool   `yaml:"disableDefaultCNI"`
			PodSubnet         string `yaml:"podSubnet"`
		} `yaml:"networking"`
		Nodes []any `yaml:"nodes"`
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("the rendered config is not valid YAML: %v\n%s", err, raw)
	}
	if !cfg.Networking.DisableDefaultCNI || cfg.Networking.PodSubnet != kindPodSubnet {
		t.Errorf("networking = %+v, want kindnet disabled on %s", cfg.Networking, kindPodSubnet)
	}
	if len(cfg.Nodes) != 2 {
		t.Errorf("nodes = %d, want the control plane and one worker", len(cfg.Nodes))
	}
}

// The vendored Cilium render must never carry key material: the chart's
// default generates Hubble's certificates at render time, which would commit
// private keys. hubble.tls.auto.method=cronJob generates them in the cluster.
func TestCiliumManifestCarriesNoKeyMaterial(t *testing.T) {
	raw, err := os.ReadFile("../../" + kindCiliumManifest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if regexp.MustCompile(`PRIVATE KEY|(?m)^\s*(tls|ca)\.key:\s*\S{20,}`).Match(raw) {
		t.Error("deployment/kind/cilium.yaml contains key material; render it with hubble.tls.auto.method=cronJob")
	}
}
