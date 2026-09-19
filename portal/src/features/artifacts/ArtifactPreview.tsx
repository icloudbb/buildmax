import { useEffect, useState } from "react"
import { Button } from "@buildmax/gui"
import type { ApiArtifact } from "../../lib/api/types"
import { getErrorMessage } from "../../lib/errorMessage"
import { artifactLabel } from "./display"
import { fetchArtifactPreview } from "./api"
import { ArtifactContentView } from "./ArtifactContentView"

interface ArtifactPreviewProps {
  artifact: ApiArtifact | null
  token: string | null
  /** Omitted where the preview is part of the page rather than laid over it. */
  onClose?: () => void
}

/**
 * ArtifactPreview fetches an artifact's content with the caller's session and
 * hands it to the shared renderer. A type the server did not mark previewable
 * never reaches here — ArtifactDetail gates on `preview`.
 */
export function ArtifactPreview({ artifact, token, onClose }: ArtifactPreviewProps) {
  const [text, setText] = useState<string | null>(null)
  const [objectUrl, setObjectUrl] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [reloadKey, setReloadKey] = useState(0)

  useEffect(() => {
    if (!artifact || !token) return
    let cancelled = false
    let created: string | null = null
    setLoading(true)
    setError(null)
    setText(null)
    setObjectUrl(null)
    fetchArtifactPreview(artifact.id, token)
      .then((res) => {
        if (cancelled) {
          if (res.objectUrl) URL.revokeObjectURL(res.objectUrl)
          return
        }
        if (res.text != null) setText(res.text)
        if (res.objectUrl) {
          created = res.objectUrl
          setObjectUrl(res.objectUrl)
        }
      })
      .catch((err) => {
        if (!cancelled) setError(getErrorMessage(err, "Failed to load the preview"))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
      if (created) URL.revokeObjectURL(created)
    }
  }, [artifact, token, reloadKey])

  if (!artifact) return null

  // A dialog role is a claim about focus and dismissal. Embedded in a page it
  // is neither, so only the overlaid form -- the one with a way out -- says so.
  const overlaid = onClose != null

  return (
    <div
      className="artifact-preview"
      role={overlaid ? "dialog" : undefined}
      aria-label={overlaid ? artifact.filename : undefined}
    >
      {overlaid ? (
        <div className="artifact-preview__head">
          <span className="artifact-preview__title">{artifactLabel(artifact)}</span>
          <Button variant="tertiary" onClick={onClose}>
            Close
          </Button>
        </div>
      ) : null}
      <div className="artifact-preview__body">
        {loading ? <p className="page-activity__empty">Loading…</p> : null}
        {error ? (
          <div className="artifact-preview__error" role="alert">
            <p className="settings-section__error">{error}</p>
            <Button variant="secondary" size="compact" onClick={() => setReloadKey((current) => current + 1)}>Retry preview</Button>
          </div>
        ) : null}
        {!loading && !error ? (
          <ArtifactContentView
            filename={artifact.filename}
            mediaType={artifact.media_type}
            text={text}
            objectUrl={objectUrl}
          />
        ) : null}
      </div>
    </div>
  )
}
