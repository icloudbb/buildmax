package main

import "fmt"

// kind clusters run Cilium instead of kind's default kindnet, so NetworkPolicy
// is enforced in the kernel. See deployment/kind/cilium.yaml for why and how the
// manifest is rendered.
const (
	kindCiliumManifest = "deployment/kind/cilium.yaml"
	// kindPodSubnet is kind's default pod range, stated so Cilium allocates from
	// the same one.
	kindPodSubnet = "10.244.0.0/16"
	// kindCNIConfig leaves the network to Cilium. Nodes stay NotReady until
	// installKindCilium runs.
	kindCNIConfig = "networking:\n  disableDefaultCNI: true\n  podSubnet: " + kindPodSubnet + "\n"
)

// installKindCilium installs Cilium into a just-created cluster and waits for
// the nodes it makes Ready. A first install pulls its images into every node,
// which outlasts the two minutes kind up otherwise allows.
func installKindCilium() error {
	if err := kindKubectl("apply", "-f", kindCiliumManifest); err != nil {
		return fmt.Errorf("install cilium: %w", err)
	}
	if err := kindKubectl("rollout", "status", "daemonset/cilium", "-n", "kube-system", "--timeout=600s"); err != nil {
		return err
	}
	return kindKubectl("wait", "--for=condition=Ready", "nodes", "--all", "--timeout=600s")
}

// warnIfKindnet says when an existing cluster predates the switch: it keeps
// kindnet until it is recreated, and kind up cannot swap the plugin in place.
func warnIfKindnet() {
	if succeeds("kubectl", "--context", kindContext(), "get", "daemonset", "kindnet", "-n", "kube-system") {
		fmt.Printf("Note: cluster %q still runs kindnet. `%s kind down` then `%s kind up` recreates it with Cilium.\n",
			kindClusterName(), mk(), mk())
	}
}
