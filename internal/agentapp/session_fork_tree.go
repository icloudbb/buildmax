package agentapp

import (
	"fmt"
	"sort"

	"github.com/icloudbb/buildmax/internal/core/session"
)

// ForkTreeNode is one user session in the fork tree containing the session a
// caller asked about. The tree is a read model derived from the picker index;
// it is not stored independently from session provenance.
type ForkTreeNode struct {
	ID          string         `json:"id"`
	Title       string         `json:"title,omitempty"`
	ForkPointID string         `json:"fork_point_id,omitempty"`
	Current     bool           `json:"current,omitempty"`
	Missing     bool           `json:"missing,omitempty"`
	Children    []ForkTreeNode `json:"children,omitempty"`
}

// ForkTree returns the connected fork tree containing currentID. It reads the
// picker projection once and never opens an individual session journal.
func (s *SessionManager) ForkTree(currentID string) (*ForkTreeNode, error) {
	items, err := s.List()
	if err != nil {
		return nil, fmt.Errorf("load session index: %w", err)
	}
	tree := BuildForkTree(items, currentID)
	if tree == nil {
		return nil, fmt.Errorf("session %s is missing from the session index", currentID)
	}
	return tree, nil
}

// BuildForkTree derives the connected fork tree containing currentID from the
// picker projection. Only sessions in the current session's Project belong in
// the result. Fork preserves ProjectID, and keeping that boundary here prevents
// corrupt provenance from joining otherwise unrelated projects.
func BuildForkTree(items []session.ItemSummary, currentID string) *ForkTreeNode {
	all := make(map[string]session.ItemSummary, len(items))
	for _, item := range items {
		if item.Kind == session.KindUser {
			all[item.ID] = item
		}
	}
	current, ok := all[currentID]
	if !ok {
		return nil
	}
	byID := make(map[string]session.ItemSummary, len(all))
	for id, item := range all {
		if item.ProjectID == current.ProjectID {
			byID[id] = item
		}
	}

	// First find the oldest parent the index can still name. A missing parent
	// becomes a placeholder root: deletion must not make the surviving branch
	// look as though it was never forked.
	rootID := currentID
	seen := map[string]bool{}
	for {
		if seen[rootID] {
			// Provenance written by BuildMax cannot cycle. Pick a stable root for
			// damaged hand-edited data so /info remains readable rather than
			// looping forever.
			rootID = smallestID(seen)
			break
		}
		seen[rootID] = true
		item, exists := byID[rootID]
		if !exists || item.ForkedFrom == nil || item.ForkedFrom.SessionID == "" {
			break
		}
		rootID = item.ForkedFrom.SessionID
	}

	children := make(map[string][]session.ItemSummary)
	for _, item := range byID {
		if item.ForkedFrom == nil || item.ForkedFrom.SessionID == "" {
			continue
		}
		children[item.ForkedFrom.SessionID] = append(children[item.ForkedFrom.SessionID], item)
	}
	for parentID := range children {
		sort.Slice(children[parentID], func(i, j int) bool {
			if !children[parentID][i].CreatedAt.Equal(children[parentID][j].CreatedAt) {
				return children[parentID][i].CreatedAt.Before(children[parentID][j].CreatedAt)
			}
			return children[parentID][i].ID < children[parentID][j].ID
		})
	}

	var build func(string, map[string]bool) ForkTreeNode
	build = func(id string, path map[string]bool) ForkTreeNode {
		item, exists := byID[id]
		node := ForkTreeNode{ID: id, Current: id == currentID, Missing: !exists}
		if exists {
			node.Title = item.Title
			if item.ForkedFrom != nil {
				node.ForkPointID = item.ForkedFrom.HeadID
			}
		}

		nextPath := make(map[string]bool, len(path)+1)
		for ancestor := range path {
			nextPath[ancestor] = true
		}
		nextPath[id] = true
		for _, child := range children[id] {
			if nextPath[child.ID] {
				continue
			}
			node.Children = append(node.Children, build(child.ID, nextPath))
		}
		return node
	}

	tree := build(rootID, nil)
	return &tree
}

func smallestID(ids map[string]bool) string {
	var smallest string
	for id := range ids {
		if smallest == "" || id < smallest {
			smallest = id
		}
	}
	return smallest
}
