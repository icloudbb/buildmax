package workflow

import (
	"reflect"
	"testing"
)

func nodes(specs ...[2]any) []DefinitionNode {
	out := make([]DefinitionNode, len(specs))
	for i, s := range specs {
		out[i] = DefinitionNode{ID: s[0].(string), Needs: s[1].([]string)}
	}
	return out
}

func TestBuildGraph_OrderAndPredecessors(t *testing.T) {
	// A diamond: research feeds analyze and summarize, both feed report.
	g, err := BuildGraph(nodes(
		[2]any{"research", []string(nil)},
		[2]any{"analyze", []string{"research"}},
		[2]any{"summarize", []string{"research"}},
		[2]any{"report", []string{"analyze", "summarize"}},
	))
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}
	// Deterministic topological order breaks ties by definition position.
	want := []string{"research", "analyze", "summarize", "report"}
	if got := g.Order(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Order = %v, want %v", got, want)
	}
	// report transitively depends on research through both middle nodes.
	if !g.IsPredecessor("report", "research") {
		t.Error("report should have research as a transitive predecessor")
	}
	if !g.IsPredecessor("report", "analyze") || !g.IsPredecessor("analyze", "research") {
		t.Error("direct edges should be predecessors")
	}
	// Siblings are not predecessors of each other, and roots have none.
	if g.IsPredecessor("analyze", "summarize") {
		t.Error("sibling analyze must not depend on summarize")
	}
	if g.IsPredecessor("research", "report") {
		t.Error("a root cannot depend on a later node")
	}
}

func TestBuildGraph_RejectsBadEdges(t *testing.T) {
	cases := map[string][]DefinitionNode{
		"cycle": nodes(
			[2]any{"a", []string{"b"}},
			[2]any{"b", []string{"a"}},
		),
		"self loop": nodes([2]any{"a", []string{"a"}}),
		"unknown need": nodes(
			[2]any{"a", []string(nil)},
			[2]any{"b", []string{"missing"}},
		),
		"duplicate need": nodes(
			[2]any{"a", []string(nil)},
			[2]any{"b", []string{"a", "a"}},
		),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := BuildGraph(in); err == nil {
				t.Fatalf("BuildGraph(%s) = nil error, want rejection", name)
			}
		})
	}
}

func TestBuildGraph_IndependentRootsKeepDefinitionOrder(t *testing.T) {
	g, err := BuildGraph(nodes(
		[2]any{"c", []string(nil)},
		[2]any{"a", []string(nil)},
		[2]any{"b", []string(nil)},
	))
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}
	if got, want := g.Order(), []string{"c", "a", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Order = %v, want definition order %v", got, want)
	}
}
