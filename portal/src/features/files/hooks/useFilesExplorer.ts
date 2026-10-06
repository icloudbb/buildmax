import { useCallback, useRef, useState } from "react"
import type { ExploreNode } from "../../../lib/types"
import { findNodeById, findParentId, getChildren } from "../../../lib/explore"
import { getErrorMessage } from "../../../lib/errorMessage"
import { useFetch } from "../../../hooks/useFetch"
import { getFileContent, getFileTree, uploadFiles } from "../api"
import { useStableT, useT } from "../../../i18n"

interface UseFilesExplorerOptions {
  spaceId: string | null
  token: string | null
}

export function useFilesExplorer({ spaceId, token }: UseFilesExplorerOptions) {
  const t = useT()
  const stableT = useStableT()
  const {
    data: tree,
    loading: treeLoading,
    error: treeError,
    errorKind: treeErrorKind,
    refetch: refetchTree,
  } = useFetch(
    () => getFileTree(spaceId!, token!),
    [spaceId, token],
    {
      enabled: !!(spaceId && token),
      errorMessage: (e) => getErrorMessage(e, stableT("files.error.loadTree")),
    }
  )

  const [expandedIds, setExpandedIds] = useState<Set<string>>(new Set(["."]))
  const [selectedFolderId, setSelectedFolderId] = useState(".")
  const [selectedFileId, setSelectedFileId] = useState<string | null>(null)
  const [uploading, setUploading] = useState(false)
  const [uploadMsg, setUploadMsg] = useState<{ text: string; isError: boolean } | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const folderInputRef = useRef<HTMLInputElement>(null)

  const {
    data: fileContent,
    loading: fileLoading,
    error: fileError,
  } = useFetch(
    () => getFileContent(spaceId!, selectedFileId!, token!),
    [spaceId, selectedFileId, token],
    {
      enabled: !!(spaceId && token && selectedFileId),
      errorMessage: (e) => getErrorMessage(e, stableT("files.error.loadFile")),
    }
  )

  const toggleFolder = useCallback((id: string) => {
    setExpandedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  const selectFolder = useCallback((folderId: string) => {
    setSelectedFolderId(folderId)
  }, [])

  const selectListFolder = useCallback((folderId: string) => {
    setSelectedFolderId(folderId)
    setExpandedIds((prev) => new Set(prev).add(folderId))
    setSelectedFileId(null)
  }, [])

  const selectFile = useCallback((node: ExploreNode) => {
    if (node.type !== "file") return
    setSelectedFileId(node.id)
  }, [])

  // Narrow layouts show one of "a folder's contents" or "a selected file" at
  // a time (no side tree), so they need a single Back action: clear the file
  // selection if one is open, otherwise step up to the parent folder.
  const goBack = useCallback(() => {
    if (selectedFileId) {
      setSelectedFileId(null)
      return
    }
    if (!tree || selectedFolderId === ".") return
    const parentId = findParentId(tree, selectedFolderId)
    if (parentId !== null) setSelectedFolderId(parentId)
  }, [tree, selectedFolderId, selectedFileId])

  const canGoBack = selectedFileId !== null || selectedFolderId !== "."

  const doUpload = useCallback(
    async (files: File[], paths?: string[], options?: { maxFiles?: number }) => {
      if (!spaceId || !token) {
        setUploadMsg({ text: stableT("files.upload.notAuthenticated"), isError: true })
        return
      }
      const { maxFiles } = options ?? {}
      if (maxFiles != null && files.length > maxFiles) {
        setUploadMsg({ text: stableT("files.upload.tooMany", { max: maxFiles }), isError: true })
        return
      }
      setUploading(true)
      setUploadMsg(null)
      try {
        const res = await uploadFiles(files, spaceId, token, paths)
        setUploadMsg({ text: stableT("files.upload.done", { count: res.uploaded.length }), isError: false })
        await refetchTree()
      } catch (err) {
        setUploadMsg({ text: getErrorMessage(err, stableT("files.upload.failed")), isError: true })
      } finally {
        setUploading(false)
      }
    },
    [spaceId, token, refetchTree, stableT]
  )

  const handleUpload = useCallback(
    async (e: React.ChangeEvent<HTMLInputElement>) => {
      const selected = e.target.files
      if (!selected || selected.length === 0) return
      const files = Array.from(selected)
      try {
        await doUpload(files, undefined, { maxFiles: 10 })
      } finally {
        if (fileInputRef.current) fileInputRef.current.value = ""
      }
    },
    [doUpload]
  )

  const handleFolderUpload = useCallback(
    async (e: React.ChangeEvent<HTMLInputElement>) => {
      const selected = e.target.files
      if (!selected || selected.length === 0) return
      const files = Array.from(selected)
      const paths = files.map((f) => f.webkitRelativePath)
      try {
        await doUpload(files, paths)
      } finally {
        if (folderInputRef.current) folderInputRef.current.value = ""
      }
    },
    [doUpload]
  )

  const children = tree ? getChildren(tree, selectedFolderId) : []
  const selectedFolderNode = tree ? findNodeById(tree, selectedFolderId) : undefined
  const folderName = selectedFolderId === "." ? t("files.root") : selectedFolderNode?.name ?? "—"
  const selectedFileName =
    tree && selectedFileId ? findNodeById(tree, selectedFileId)?.name ?? null : null

  return {
    tree,
    treeLoading,
    treeError,
    treeErrorKind,
    refetchTree,
    expandedIds,
    selectedFolderId,
    selectedFileId,
    fileContent,
    fileLoading,
    fileError,
    fileInputRef,
    folderInputRef,
    uploading,
    uploadMsg,
    children,
    folderName,
    selectedFileName,
    canGoBack,
    toggleFolder,
    selectFolder,
    selectListFolder,
    selectFile,
    goBack,
    handleUpload,
    handleFolderUpload,
  }
}
