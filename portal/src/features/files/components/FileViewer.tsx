import { useT } from "../../../i18n"

interface FileViewerProps {
  selectedFileId: string | null
  selectedFileName: string | null
  fileLoading: boolean
  fileError: string | null
  fileContent: string | null
}

export function FileViewer({
  selectedFileId,
  selectedFileName,
  fileLoading,
  fileError,
  fileContent,
}: FileViewerProps) {
  const t = useT()
  if (!selectedFileId) return null

  return (
    <section className="page-explore__viewer" aria-label={t("files.fileContent")}>
      <h3 className="page-explore__viewer-title">{selectedFileName ?? selectedFileId}</h3>
      {fileLoading && <p className="page-explore__viewer-loading">{t("shell.loading")}</p>}
      {fileError && (
        <p className="page-explore__viewer-error" role="alert">
          {t("files.viewerError", { message: fileError })}
        </p>
      )}
      {!fileLoading && !fileError && (
        <pre className="page-explore__viewer-content">{fileContent ?? ""}</pre>
      )}
    </section>
  )
}
