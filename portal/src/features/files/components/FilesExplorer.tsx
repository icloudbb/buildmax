import { Button } from "@buildmax/gui"
import { cn } from "../../../lib/cn"
import { useAuth } from "../../../contexts/AuthContext"
import { useMediaQuery } from "../../../hooks/useMediaQuery"
import { FileList } from "./FileList"
import { FileTree } from "./FileTree"
import { FileViewer } from "./FileViewer"
import { useFilesExplorer } from "../hooks/useFilesExplorer"

interface FilesExplorerProps {
  spaceId: string
  className?: string
}

export function FilesExplorer({ spaceId, className }: FilesExplorerProps) {
  const { token } = useAuth()
  const explorer = useFilesExplorer({ spaceId, token })
  // Narrow layouts show one column at a time (folder contents, or the
  // selected file) instead of a side tree — see
  // docs/design/portal-responsive-and-accessible-interaction.md.
  const narrow = useMediaQuery("(max-width: 767px)")
  const showList = !narrow || !explorer.selectedFileId

  return (
    <div className={className ?? "files-panel"}>
      <div className="page-explore__upload-bar">
        <input
          ref={explorer.fileInputRef}
          type="file"
          multiple
          className="page-explore__file-input"
          onChange={explorer.handleUpload}
        />
        <input
          ref={explorer.folderInputRef}
          type="file"
          className="page-explore__file-input"
          onChange={explorer.handleFolderUpload}
          {...{ webkitdirectory: "", directory: "" }}
        />
        <Button
          variant="primary" busy={explorer.uploading}
          onClick={() => explorer.fileInputRef.current?.click()}
        >
          Upload Files
        </Button>
        <Button
          variant="secondary" busy={explorer.uploading}
          onClick={() => explorer.folderInputRef.current?.click()}
        >
          Upload Folder
        </Button>
        {explorer.uploadMsg && (
          <span
            className={cn(
              "page-explore__upload-msg",
              explorer.uploadMsg.isError && "page-explore__upload-msg--error"
            )}
          >
            {explorer.uploadMsg.text}
          </span>
        )}
      </div>

      <div className="page-explore__panels">
        {!narrow && (
          <FileTree
            tree={explorer.tree}
            treeLoading={explorer.treeLoading}
            treeError={explorer.treeError}
            treeErrorKind={explorer.treeErrorKind}
            onRetry={explorer.refetchTree}
            expandedIds={explorer.expandedIds}
            selectedFolderId={explorer.selectedFolderId}
            onToggle={explorer.toggleFolder}
            onSelectFolder={explorer.selectFolder}
          />
        )}

        <div className="page-explore__content-panel">
          {narrow && explorer.canGoBack && (
            <button type="button" className="page-explore__back" onClick={explorer.goBack}>
              <span aria-hidden="true">‹ </span>Back
            </button>
          )}
          {showList && (
            <FileList
              folderName={explorer.folderName}
              children={explorer.children}
              treeLoading={explorer.treeLoading}
              treeError={explorer.treeError}
              selectedFileId={explorer.selectedFileId}
              isRoot={explorer.selectedFolderId === "."}
              onSelectFolder={explorer.selectListFolder}
              onSelectFile={explorer.selectFile}
            />
          )}
          <FileViewer
            selectedFileId={explorer.selectedFileId}
            selectedFileName={explorer.selectedFileName}
            fileLoading={explorer.fileLoading}
            fileError={explorer.fileError}
            fileContent={explorer.fileContent}
          />
        </div>
      </div>
    </div>
  )
}
