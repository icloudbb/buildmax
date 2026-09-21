package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/icloudbb/buildmax/internal/agentapp"
)

func writeForkTree(w io.Writer, tree *agentapp.ForkTreeNode, loadError string) {
	if tree == nil && loadError == "" {
		return
	}
	fmt.Fprintln(w, "\nSession tree")
	if loadError != "" {
		fmt.Fprintf(w, "  cannot be read: %s\n", loadError)
		return
	}
	for _, line := range forkTreeLines(tree) {
		fmt.Fprintln(w, "  "+line)
	}
}

func forkTreeLines(tree *agentapp.ForkTreeNode) []string {
	if tree == nil {
		return nil
	}
	var lines []string
	var walk func(*agentapp.ForkTreeNode, string, bool, bool)
	walk = func(node *agentapp.ForkTreeNode, prefix string, last, root bool) {
		connector := ""
		if !root {
			if last {
				connector = "└─ "
			} else {
				connector = "├─ "
			}
		}
		lines = append(lines, prefix+connector+forkTreeNodeLabel(node))

		childPrefix := prefix
		if !root {
			if last {
				childPrefix += "   "
			} else {
				childPrefix += "│  "
			}
		}
		for i := range node.Children {
			walk(&node.Children[i], childPrefix, i == len(node.Children)-1, false)
		}
	}
	walk(tree, "", true, true)
	return lines
}

func forkTreeNodeLabel(node *agentapp.ForkTreeNode) string {
	if node == nil {
		return ""
	}
	if node.Missing {
		return node.ID + "  (source session deleted)"
	}
	title := strings.TrimSpace(node.Title)
	if title == "" {
		title = "(untitled)"
	}
	label := title + "  " + node.ID
	if node.Current {
		label += "  ← current"
	}
	return label
}

func renderForkTreePanel(tree *agentapp.ForkTreeNode, loadError string, maxWidth, maxRows, offset int) string {
	if loadError != "" {
		return truncateRunes("Session tree cannot be read: "+loadError, maxWidth)
	}
	lines := forkTreeLines(tree)
	if len(lines) == 0 {
		return "This session is not part of a saved fork tree."
	}
	if maxRows <= 0 {
		maxRows = 1
	}
	start := min(max(0, offset), len(lines)-1)
	available := maxRows
	if start > 0 {
		available--
	}
	end := min(len(lines), start+max(1, available))
	if end < len(lines) {
		available--
		end = min(len(lines), start+max(1, available))
	}
	var out []string
	if start > 0 {
		out = append(out, fmt.Sprintf("… %d earlier", start))
	}
	for _, line := range lines[start:end] {
		out = append(out, truncateRunes(line, maxWidth))
	}
	if end < len(lines) {
		out = append(out, fmt.Sprintf("… %d more", len(lines)-end))
	}
	return strings.Join(out, "\n")
}

func forkTreeCurrentIndex(tree *agentapp.ForkTreeNode) int {
	lines := forkTreeLines(tree)
	for i, line := range lines {
		if strings.Contains(line, "← current") {
			return i
		}
	}
	return 0
}
