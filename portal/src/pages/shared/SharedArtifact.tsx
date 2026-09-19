import { useEffect, useState } from "react"
import { ButtonLink } from "@buildmax/gui"
import type { ApiSharedMeta } from "../../lib/api/types"
import { getErrorMessage } from "../../lib/errorMessage"
import {
  ArtifactContentView,
  fetchSharedContent,
  fetchSharedMeta,
  formatSize,
  sharedRawUrl,
} from "../../features/artifacts"

/**
 * SharedArtifact is the public page a share link opens. It needs no login: the
 * token in the URL is the whole authorization. It fetches the artifact's public
 * metadata and content and renders them through the same view the authenticated
 * detail page uses, so a shared Markdown doc reads as formatted text and a
 * shared HTML prototype runs in its sandbox.
 */
export function SharedArtifact({ token }: { token: string }) {
  const [meta, setMeta] = useState<ApiSharedMeta | null>(null)
  const [text, setText] = useState<string | null>(null)
  const [objectUrl, setObjectUrl] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    let created: string | null = null
    setLoading(true)
    setError(null)
    ;(async () => {
      try {
        const m = await fetchSharedMeta(token)
        if (cancelled) return
        setMeta(m)
        if (m.preview !== "none") {
          const res = await fetchSharedContent(token)
          if (cancelled) {
            if (res.objectUrl) URL.revokeObjectURL(res.objectUrl)
            return
          }
          if (res.text != null) setText(res.text)
          if (res.objectUrl) {
            created = res.objectUrl
            setObjectUrl(res.objectUrl)
          }
        }
      } catch (err) {
        if (!cancelled) setError(getErrorMessage(err, "This link is not available"))
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => {
      cancelled = true
      if (created) URL.revokeObjectURL(created)
    }
  }, [token])

  if (loading) {
    return (
      <div className="shared-page">
        <p className="page-activity__empty">Loading…</p>
      </div>
    )
  }

  if (error || !meta) {
    // The server answers 404 for an unknown, revoked, or expired link and for a
    // deleted artifact alike, so this page cannot say which — only that there is
    // nothing here.
    return (
      <div className="shared-page">
        <div className="shared-page__card">
          <h1 className="shared-page__title">Link unavailable</h1>
          <p className="shared-page__subtitle">
            This link may have expired, been revoked, or never existed.
          </p>
        </div>
      </div>
    )
  }

  const label = meta.title?.trim() || meta.filename

  return (
    <div className="shared-page">
      <div className="shared-page__head">
        <div>
          <h1 className="shared-page__title">{label}</h1>
          <p className="shared-page__subtitle">
            {meta.filename} · {formatSize(meta.size_bytes)}
          </p>
        </div>
        {/* A normal link, not a fetch: the raw URL is public, so the browser can
            download it directly without this page holding any credential. */}
        <ButtonLink variant={meta.preview === "none" ? "primary" : "secondary"} href={sharedRawUrl(token, true)}>
          Download
        </ButtonLink>
      </div>

      {meta.preview === "none" ? (
        <p className="page-activity__empty">
          This file type is offered as a download rather than a preview.
        </p>
      ) : (
        <div className="shared-page__body">
          <ArtifactContentView
            filename={meta.filename}
            mediaType={meta.media_type}
            text={text}
            objectUrl={objectUrl}
          />
        </div>
      )}
    </div>
  )
}
