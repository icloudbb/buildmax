import { cn } from "../../../lib/cn"
import type { ExploreNode } from "../../../lib/types"
import { useT } from "../../../i18n"

interface FileListProps {
  folderName: string
  children: ExploreNode[]
  /** While the tree is loading or failed, the FileTree panel already carries
   * that status; an empty list here must not additionally claim the folder
   * has no content when it is simply not known yet. */
  treeLoading: boolean
  treeError: string | null
  selectedFileId: string | null
  isRoot: boolean
  onSelectFolder: (folderId: string) => void
  onSelectFile: (node: ExploreNode) => void
}

export function FileList({
  folderName,
  children,
  treeLoading,
  treeError,
  selectedFileId,
  isRoot,
  onSelectFolder,
  onSelectFile,
}: FileListProps) {
  const t = useT()
  return (
    <div className="page-explore__file-list-wrap">
      <h2 className="page-explore__content-heading">{folderName}</h2>
      <ul className="page-explore__list" role="list">
        {children.length === 0 && (treeLoading || treeError) ? null : children.length === 0 ? (
          <li className="page-explore__empty">
            {isRoot ? t("files.emptyRoot") : t("files.emptyFolder")}
          </li>
        ) : (
          children.map((node) => (
            <li key={node.id} className="page-explore__item">
              {node.type === "folder" ? (
                <button
                  type="button"
                  className="page-explore__link"
                  onClick={() => onSelectFolder(node.id)}
                >
                  <span className="page-explore__icon page-explore__icon--folder" aria-hidden>📁</span>
                  <span className="page-explore__artifact-title">{node.name}</span>
                </button>
              ) : (
                <button
                  type="button"
                  className={cn(
                    "page-explore__link",
                    selectedFileId === node.id && "page-explore__link--selected"
                  )}
                  onClick={() => onSelectFile(node)}
                >
                  <span className="page-explore__icon page-explore__icon--file" aria-hidden>📄</span>
                  <span className="page-explore__artifact-title">{node.name}</span>
                </button>
              )}
            </li>
          ))
        )}
      </ul>
    </div>
  )
}
