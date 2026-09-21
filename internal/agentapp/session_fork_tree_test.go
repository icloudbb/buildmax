package agentapp

import (
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/core/session"
)

func TestBuildForkTreeFindsTheWholeConnectedTree(t *testing.T) {
	at := func(hour int) time.Time { return time.Date(2026, 9, 21, hour, 0, 0, 0, time.UTC) }
	items := []session.ItemSummary{
		{ID: "other", ProjectID: "p2", Kind: session.KindUser},
		{ID: "grandchild", ProjectID: "p1", Kind: session.KindUser, CreatedAt: at(4), ForkedFrom: &session.ForkedFrom{SessionID: "child-a", HeadID: "i3"}},
		{ID: "child-b", ProjectID: "p1", Kind: session.KindUser, CreatedAt: at(3), ForkedFrom: &session.ForkedFrom{SessionID: "root", HeadID: "i2"}},
		{ID: "root", ProjectID: "p1", Kind: session.KindUser, CreatedAt: at(1), Title: "Root"},
		{ID: "child-a", ProjectID: "p1", Kind: session.KindUser, CreatedAt: at(2), Title: "First branch", ForkedFrom: &session.ForkedFrom{SessionID: "root", HeadID: "i1"}},
	}

	tree := BuildForkTree(items, "grandchild")
	if tree == nil {
		t.Fatal("BuildForkTree returned nil")
	}
	if tree.ID != "root" || len(tree.Children) != 2 {
		t.Fatalf("tree = %+v, want root with two children", tree)
	}
	if tree.Children[0].ID != "child-a" || tree.Children[1].ID != "child-b" {
		t.Fatalf("children = %+v, want creation order", tree.Children)
	}
	grandchild := tree.Children[0].Children[0]
	if grandchild.ID != "grandchild" || !grandchild.Current || grandchild.ForkPointID != "i3" {
		t.Errorf("grandchild = %+v, want current node and its fork point", grandchild)
	}
}

func TestBuildForkTreeKeepsADeletedParentVisible(t *testing.T) {
	items := []session.ItemSummary{
		{ID: "child", ProjectID: "p1", Kind: session.KindUser, ForkedFrom: &session.ForkedFrom{SessionID: "deleted", HeadID: "i1"}},
		{ID: "sibling", ProjectID: "p1", Kind: session.KindUser, ForkedFrom: &session.ForkedFrom{SessionID: "deleted", HeadID: "i2"}},
	}

	tree := BuildForkTree(items, "child")
	if tree == nil || tree.ID != "deleted" || !tree.Missing {
		t.Fatalf("tree = %+v, want the deleted parent as a placeholder root", tree)
	}
	if len(tree.Children) != 2 || !tree.Children[0].Current {
		t.Fatalf("children = %+v, want both surviving branches and child current", tree.Children)
	}
}

func TestBuildForkTreeRejectsUnknownCurrentAndSurvivesCycles(t *testing.T) {
	if got := BuildForkTree(nil, "missing"); got != nil {
		t.Fatalf("unknown current produced %+v", got)
	}

	items := []session.ItemSummary{
		{ID: "a", ProjectID: "p1", Kind: session.KindUser, ForkedFrom: &session.ForkedFrom{SessionID: "b"}},
		{ID: "b", ProjectID: "p1", Kind: session.KindUser, ForkedFrom: &session.ForkedFrom{SessionID: "a"}},
	}
	if got := BuildForkTree(items, "b"); got == nil {
		t.Fatal("cycle made the tree disappear")
	}
}
