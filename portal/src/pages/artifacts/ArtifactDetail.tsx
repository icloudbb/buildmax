import { useCallback, useEffect, useState } from "react"
import { Button, ButtonLink } from "@buildmax/gui"
import type { ApiArtifact } from "../../lib/api/types"
import { getErrorMessage } from "../../lib/errorMessage"
import { ApiRequestError } from "../../lib/api/client"
import { downloadAuthenticated } from "../../lib/download"
import { buildHash, navigate } from "../../router"
import { useAuth } from "../../contexts/AuthContext"
import { useApp } from "../../contexts/AppContext"
import { useSpace } from "../../contexts/SpaceContext"
import { CopyButton } from "../../components/CopyButton"
import {
  ArtifactOrigin,
  ArtifactPreview,
  ArtifactShareDialog,
  artifactContentUrl,
  artifactLabel,
  confirmArtifactDeletion,
  deleteArtifact,
  formatSize,
  formatTime,
  getArtifact,
  mayDelete,
  sourceLabel,
} from "../../features/artifacts"
import { useStableT, useT } from "../../i18n"

interface ArtifactDetailProps {
  artifactId: string
}

/**
 * One artifact, at an address that can be pasted into a message or a document.
 *
 * The page leads with the content itself: the rendered preview is the body,
 * actions sit in the header, and the file's metadata lives in a Details
 * disclosure rather than a card that competes with the content for the top of
 * the page.
 */
export function ArtifactDetail({ artifactId }: ArtifactDetailProps) {
  const t = useT()
  const stableT = useStableT()
  const { token, user } = useAuth()
  const { setEntityLabel } = useApp()
  const { currentSpaceId, currentUserRole, setCurrentSpaceId } = useSpace()
  const [artifact, setArtifact] = useState<ApiArtifact | null>(null)
  const [loading, setLoading] = useState(true)
  const [notFound, setNotFound] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [busyAction, setBusyAction] = useState<"download" | "delete" | null>(null)
  const [shareOpen, setShareOpen] = useState(false)

  const load = useCallback(() => {
    if (!token) return
    setLoading(true)
    setError(null)
    setNotFound(false)
    getArtifact(artifactId, token)
      .then(setArtifact)
      .catch((err) => {
        // Only the server's own 404 means there is nothing to show. Any other
        // failure -- the network, a 500 -- is this request going wrong, and
        // reporting it as "no such artifact" would tell the reader something
        // untrue about the deployment's contents.
        if (err instanceof ApiRequestError && err.status === 404) {
          setNotFound(true)
          return
        }
        setError(getErrorMessage(err, stableT("artifacts.error.loadOne")))
      })
      .finally(() => setLoading(false))
  }, [artifactId, token, stableT])

  useEffect(() => {
    load()
  }, [load])

  // Publish the human label so the breadcrumb reads "Artifacts / Newton laws"
  // rather than the opaque id, the same seam agent and issue pages use.
  useEffect(() => {
    if (artifact) setEntityLabel(artifact.id, artifactLabel(artifact))
  }, [artifact, setEntityLabel])

  // Artifact detail is the one route resolved by id alone -- once the lookup
  // authorizes it, reconcile the shell to the artifact's own Space so the
  // sidebar, switcher, and "back to Artifacts" link agree with what is on
  // screen, per docs/design/portal-navigation-and-space-context.md.
  useEffect(() => {
    if (artifact) setCurrentSpaceId(artifact.space_id)
  }, [artifact, setCurrentSpaceId])

  async function onDownload() {
    if (!artifact || !token) return
    setBusyAction("download")
    setError(null)
    try {
      await downloadAuthenticated(artifactContentUrl(artifact.id), token, artifact.filename)
    } catch (err) {
      setError(getErrorMessage(err, stableT("artifacts.error.download")))
    } finally {
      setBusyAction(null)
    }
  }

  async function onDelete() {
    if (!artifact || !token) return
    if (!confirmArtifactDeletion(artifact, t)) return
    setBusyAction("delete")
    setError(null)
    try {
      await deleteArtifact(artifact.id, token)
      navigate({ name: "artifacts", spaceId: artifact?.space_id ?? currentSpaceId! })
    } catch (err) {
      setError(getErrorMessage(err, stableT("artifacts.error.delete")))
      setBusyAction(null)
    }
  }

  if (loading) {
    return (
      <div className="page-activity">
        <p className="page-activity__empty">{t("shell.loading")}</p>
      </div>
    )
  }

  if (!artifact) {
    // The server answers 404 both for an artifact that never existed and for
    // one in a space the reader is not in -- deliberately, so an id cannot be
    // used to probe. This page must not narrate a difference the API refuses
    // to make. A request that simply failed says so instead, and offers a
    // retry, because that one is worth trying again.
    return (
      <div className="page-activity">
        <div className="page-activity__head">
          <div>
            <h1 className="page-activity__title">{t("artifacts.detail.heading")}</h1>
            <p className="page-activity__subtitle">
              {notFound ? t("artifacts.detail.notFound") : t("artifacts.detail.loadFailed")}
            </p>
          </div>
        </div>
        {error ? (
          <p className="settings-section__error" role="alert">
            {error}
          </p>
        ) : null}
        <div className="page-activity__actions">
          {!notFound ? (
            <Button variant="secondary" onClick={load}>
              {t("artifacts.detail.tryAgain")}
            </Button>
          ) : null}
          {currentSpaceId ? <ButtonLink variant="tertiary" href={buildHash({ name: "artifacts", spaceId: currentSpaceId })}>{t("artifacts.detail.back")}</ButtonLink> : null}
        </div>
      </div>
    )
  }

  const canDelete = mayDelete(artifact, user?.id, currentUserRole)

  return (
    <div className="page-activity">
      <div className="page-activity__head">
        <div>
          <h1 className="page-activity__title">{artifactLabel(artifact)}</h1>
          <p className="page-activity__subtitle">
            {artifact.filename} · {formatSize(artifact.size_bytes)} · {sourceLabel(artifact, t)}
          </p>
        </div>
        <div className="page-activity__actions">
          <Button
            variant={artifact.preview === "none" ? "primary" : "secondary"}
            onClick={() => void onDownload()}
            busy={busyAction === "download"}
            disabled={busyAction !== null}
          >
            {t("artifacts.download")}
          </Button>
          <Button variant="secondary" onClick={() => setShareOpen(true)}>
            {t("artifacts.share")}
          </Button>
          {canDelete ? (
            <Button
              variant="danger"
              onClick={() => void onDelete()}
              busy={busyAction === "delete"}
              disabled={busyAction !== null}
            >
              {t("artifacts.delete")}
            </Button>
          ) : null}
        </div>
      </div>

      {error ? (
        <p className="settings-section__error" role="alert">
          {error}
        </p>
      ) : null}

      {/* The content itself, front and centre. A type the server will not render
          says so in place of a preview. */}
      {artifact.preview !== "none" ? (
        <ArtifactPreview artifact={artifact} token={token} />
      ) : (
        <p className="page-activity__empty">{t("artifacts.detail.noPreview")}</p>
      )}

      <details className="artifact-details">
        <summary className="artifact-details__summary">{t("artifacts.detail.details")}</summary>
        <dl className="artifact-details__facts">
          <div>
            <dt>{t("artifacts.detail.type")}</dt>
            <dd>{artifact.media_type || t("artifacts.detail.unknownType")}</dd>
          </div>
          <div>
            <dt>{t("artifacts.detail.size")}</dt>
            <dd>{formatSize(artifact.size_bytes)}</dd>
          </div>
          <div>
            <dt>{t("artifacts.detail.origin")}</dt>
            <dd>
              <ArtifactOrigin artifact={artifact} spaceId={currentSpaceId} token={token} />
            </dd>
          </div>
          <div>
            <dt>{t("artifacts.detail.created")}</dt>
            <dd>{formatTime(artifact.created_at)}</dd>
          </div>
          {artifact.expires_at ? (
            <div>
              <dt>{t("artifacts.detail.expires")}</dt>
              <dd>{formatTime(artifact.expires_at)}</dd>
            </div>
          ) : null}
          <div>
            {/* Proves what was stored, computed while streaming the upload. */}
            <dt>SHA-256</dt>
            <dd className="artifact-details__copyable">
              <code className="artifact-details__digest">{artifact.sha256}</code>
              <CopyButton value={artifact.sha256} label={t("artifacts.copy")} />
            </dd>
          </div>
        </dl>
      </details>

      <ArtifactShareDialog
        artifactId={artifact.id}
        token={token}
        open={shareOpen}
        onClose={() => setShareOpen(false)}
      />
    </div>
  )
}
