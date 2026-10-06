import { TreePanel } from "../../../components/TreePanel"
import { Alert } from "../../../components/state/Alert"
import type { RequestErrorKind } from "../../../state/resourceState"
import type { ExploreNode } from "../../../lib/types"
import { useT } from "../../../i18n"

interface FileTreeProps {
  tree: ExploreNode | null
  treeLoading: boolean
  treeError: string | null
  treeErrorKind: RequestErrorKind | null
  onRetry: () => void
  expandedIds: Set<string>
  selectedFolderId: string
  onToggle: (id: string) => void
  onSelectFolder: (id: string) => void
}

export function FileTree({
  tree,
  treeLoading,
  treeError,
  treeErrorKind,
  onRetry,
  expandedIds,
  selectedFolderId,
  onToggle,
  onSelectFolder,
}: FileTreeProps) {
  const t = useT()
  return (
    <aside className="page-explore__tree-panel" aria-label={t("files.directoryTree")}>
      <div className="explore-tree">
        {treeLoading && <p className="explore-tree__loading">{t("shell.loading")}</p>}
        {treeError && (
          <Alert
            tone={treeErrorKind === "forbidden" || treeErrorKind === "notFound" ? treeErrorKind : "error"}
            message={treeError}
            retry={{ label: t("shell.retry"), onClick: onRetry }}
          />
        )}
        {tree && tree.type === "folder" && (
          <ul className="explore-tree__list" role="tree" aria-label={t("files.folderTree")}>
            <TreePanel
              node={tree}
              depth={0}
              expandedIds={expandedIds}
              selectedFolderId={selectedFolderId}
              onToggle={onToggle}
              onSelectFolder={onSelectFolder}
            />
          </ul>
        )}
      </div>
    </aside>
  )
}
