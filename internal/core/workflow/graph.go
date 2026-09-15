package workflow

import "fmt"

// Graph is the validated dependency graph of a workflow definition's nodes. It
// is built once from the nodes' `needs` edges: construction fails when an edge
// names a missing node, a node needs itself or the same node twice, or the
// edges form a cycle, so a built Graph is always a DAG whose every edge
// resolves. It answers the two questions the contract rests on -- is one node a
// transitive predecessor of another, and in what deterministic order do the
// nodes execute -- which replaces array position as the execution authority.
type Graph struct {
	order []string                   // deterministic topological order
	preds map[string]map[string]bool // node id -> its transitive predecessors
}

// BuildGraph validates the `needs` edges and precomputes transitive
// predecessors and a deterministic topological order. It errors when a `needs`
// edge names a node that does not exist, a node needs itself, a node lists the
// same need twice, or the edges form a cycle. Node ids are assumed already
// non-empty and unique; the caller validates that before building the graph.
func BuildGraph(nodes []DefinitionNode) (*Graph, error) {
	ids := make(map[string]bool, len(nodes))
	for i := range nodes {
		ids[nodes[i].ID] = true
	}
	needs := make(map[string][]string, len(nodes))
	for i := range nodes {
		n := &nodes[i]
		seen := make(map[string]bool, len(n.Needs))
		for _, dep := range n.Needs {
			switch {
			case dep == n.ID:
				return nil, fmt.Errorf("node %q needs itself", n.ID)
			case !ids[dep]:
				return nil, fmt.Errorf("node %q needs unknown node %q", n.ID, dep)
			case seen[dep]:
				return nil, fmt.Errorf("node %q lists need %q twice", n.ID, dep)
			}
			seen[dep] = true
		}
		needs[n.ID] = n.Needs
	}
	order, err := topoSort(nodes, needs)
	if err != nil {
		return nil, err
	}
	return &Graph{order: order, preds: transitivePreds(order, needs)}, nil
}

// topoSort returns the node ids in a deterministic topological order using
// Kahn's algorithm, breaking ties by definition (array) position so the same
// definition always yields the same order. It errors when the edges form a
// cycle, detected as nodes that never reach indegree zero.
func topoSort(nodes []DefinitionNode, needs map[string][]string) ([]string, error) {
	indeg := make(map[string]int, len(nodes))
	dependents := make(map[string][]string, len(nodes))
	pos := make(map[string]int, len(nodes))
	for i := range nodes {
		id := nodes[i].ID
		pos[id] = i
		indeg[id] = len(needs[id])
		for _, dep := range needs[id] {
			dependents[dep] = append(dependents[dep], id)
		}
	}
	var ready []string
	for i := range nodes {
		if indeg[nodes[i].ID] == 0 {
			ready = append(ready, nodes[i].ID)
		}
	}
	order := make([]string, 0, len(nodes))
	for len(ready) > 0 {
		best := 0
		for k := 1; k < len(ready); k++ {
			if pos[ready[k]] < pos[ready[best]] {
				best = k
			}
		}
		id := ready[best]
		ready = append(ready[:best], ready[best+1:]...)
		order = append(order, id)
		for _, dep := range dependents[id] {
			indeg[dep]--
			if indeg[dep] == 0 {
				ready = append(ready, dep)
			}
		}
	}
	if len(order) != len(nodes) {
		return nil, fmt.Errorf("workflow nodes form a cycle")
	}
	return order, nil
}

// transitivePreds computes each node's transitive predecessor set by folding in
// topological order: a node's predecessors are its direct needs plus every one
// of their predecessors, all of which are resolved before the node is reached.
func transitivePreds(order []string, needs map[string][]string) map[string]map[string]bool {
	preds := make(map[string]map[string]bool, len(order))
	for _, id := range order {
		set := make(map[string]bool)
		for _, dep := range needs[id] {
			set[dep] = true
			for p := range preds[dep] {
				set[p] = true
			}
		}
		preds[id] = set
	}
	return preds
}

// IsPredecessor reports whether from is a transitive predecessor of node -- that
// is, node cannot become ready until from has run -- so a binding on node may
// read from's output. It is false when either id is unknown.
func (g *Graph) IsPredecessor(node, from string) bool {
	return g.preds[node][from]
}

// Order is the deterministic topological execution order of the node ids. A run
// records each node run's index from this order, so listing node runs by index
// shows them in an order consistent with their dependencies.
func (g *Graph) Order() []string {
	return g.order
}
