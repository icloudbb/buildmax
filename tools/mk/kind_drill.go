package main

import "fmt"

// Drills are destructive operator rehearsals run against a kind cluster: they
// rotate credentials or wipe and restore data. They are not part of `kind
// smoke`, run only on demand, and only on the worktree's own ephemeral cluster.

// cmdKindDrill runs one named drill. Each is destructive, so each checks for
// the ephemeral cluster itself.
func cmdKindDrill(args []string) error {
	if len(args) != 1 {
		return usageErrorf("kind", "drill needs one drill name: rotation or restore")
	}
	switch args[0] {
	case "rotation":
		return kindRotationDrill()
	case "restore":
		return kindRestoreDrill()
	default:
		return usageErrorf("kind", "unknown drill %q; want rotation or restore", args[0])
	}
}

// requireEphemeralKindCluster refuses to drill a cluster this worktree does not
// own: the resident buildmaxdev, one named by BUILDMAX_KIND_CLUSTER, or none.
func requireEphemeralKindCluster() error {
	e, ok := readEphemeralKind()
	if !ok {
		return fmt.Errorf("a kind drill replaces or wipes what the cluster it runs against holds, so it runs only on this worktree's ephemeral cluster, and %s records none; create one with BUILDMAX_KIND_EPHEMERAL=1 %s kind up", ephemeralKindMarker, mk())
	}
	if cluster := kindClusterName(); cluster != e.cluster {
		return fmt.Errorf("BUILDMAX_KIND_CLUSTER selects %q, not this worktree's ephemeral cluster %q; a kind drill runs only on the ephemeral one", cluster, e.cluster)
	}
	return nil
}
