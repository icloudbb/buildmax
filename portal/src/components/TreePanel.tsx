import type { ExploreNode } from "../lib/types"
import { isFolder } from "../lib/explore"

export interface TreePanelProps {
  node: ExploreNode
  depth: number
  expandedIds: Set<string>
  selectedFolderId: string
  onToggle: (id: string) => void
  onSelectFolder: (id: string) => void
}

/** Single folder row in the tree; children rendered in a nested <ul> */
export function TreePanel({
  node,
  depth,
  expandedIds,
  selectedFolderId,
  onToggle,
  onSelectFolder,
}: TreePanelProps) {
  if (node.type !== "folder") return null

  const isExpanded = expandedIds.has(node.id)
  const isSelected = selectedFolderId === node.id
  const folderChildren = (node.children ?? []).filter(isFolder)

  return (
    // Nested lists of disclosure buttons rather than an ARIA tree: a tree
    // promises arrow-key navigation between items, which this does not have.
    <li className="explore-tree__item" style={{ paddingLeft: `${depth * 1.25}rem` }}>
      <button
        type="button"
        className={`explore-tree__row ${isSelected ? "explore-tree__row--selected" : ""}`}
        onClick={() => {
          onSelectFolder(node.id)
          if (folderChildren.length > 0) onToggle(node.id)
        }}
        aria-expanded={folderChildren.length > 0 ? isExpanded : undefined}
        aria-current={isSelected ? "true" : undefined}
      >
        {folderChildren.length > 0 ? (
          <span className="explore-tree__icon" aria-hidden>
            {isExpanded ? "▼" : "▶"}
          </span>
        ) : (
          <span className="explore-tree__icon explore-tree__icon--empty" aria-hidden />
        )}
        <span className="explore-tree__label">{node.name}</span>
      </button>
      {isExpanded && folderChildren.length > 0 && (
        <ul className="explore-tree__list">
          {folderChildren.map((child) => (
            <TreePanel
              key={child.id}
              node={child}
              depth={depth + 1}
              expandedIds={expandedIds}
              selectedFolderId={selectedFolderId}
              onToggle={onToggle}
              onSelectFolder={onSelectFolder}
            />
          ))}
        </ul>
      )}
    </li>
  )
}
