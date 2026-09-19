import { useCallback, useEffect, useState } from "react"
import { BaseModal, Button } from "@buildmax/gui"
import type { ApiArtifactShare } from "../../lib/api/types"
import { ApiRequestError } from "../../lib/api/client"
import { getErrorMessage } from "../../lib/errorMessage"
import { CopyButton } from "../../components/CopyButton"
import { createShare, listShares, revokeShare } from "./api"
import { formatTime } from "./display"

interface ArtifactShareDialogProps {
  artifactId: string
  token: string | null
  open: boolean
  onClose: () => void
}

/**
 * ArtifactShareDialog manages an artifact's public links in a modal opened from
 * the Share action — sharing is an occasional, deliberate act, so it does not
 * belong in a card that sits on the page for every viewer.
 *
 * A created link's URL is shown once (its token cannot be reproduced from the
 * stored hash), so the fresh link is surfaced for copying while the list below
 * shows only each link's metadata and a Revoke.
 */
export function ArtifactShareDialog({ artifactId, token, open, onClose }: ArtifactShareDialogProps) {
  const [shares, setShares] = useState<ApiArtifactShare[]>([])
  const [freshLink, setFreshLink] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [unavailable, setUnavailable] = useState(false)
  const [busyAction, setBusyAction] = useState<string | null>(null)

  const load = useCallback(() => {
    if (!token) return
    return listShares(artifactId, token)
      .then((res) => setShares(res.items ?? []))
      .catch((err) => setError(getErrorMessage(err, "Could not load share links")))
  }, [artifactId, token])

  // Reload each time the dialog opens; clear the one-time link from a prior open.
  useEffect(() => {
    if (!open) return
    setFreshLink(null)
    setError(null)
    setUnavailable(false)
    load()
  }, [open, load])

  async function onCreate() {
    if (!token) return
    setBusyAction("create")
    setError(null)
    try {
      const share = await createShare(artifactId, token)
      setFreshLink(share.url ?? null)
      await load()
    } catch (err) {
      if (err instanceof ApiRequestError && err.status === 503) {
        setUnavailable(true)
      } else {
        setError(getErrorMessage(err, "Could not create a public link"))
      }
    } finally {
      setBusyAction(null)
    }
  }

  async function onRevoke(shareId: string) {
    if (!token) return
    setBusyAction(shareId)
    setError(null)
    try {
      await revokeShare(artifactId, shareId, token)
      if (freshLink) setFreshLink(null)
      await load()
    } catch (err) {
      setError(getErrorMessage(err, "Could not revoke the link"))
    } finally {
      setBusyAction(null)
    }
  }

  const live = shares.filter((s) => !s.revoked_at)

  return (
    <BaseModal
      open={open}
      title="Share this artifact"
      titleId="artifact-share-title"
      onClose={onClose}
      className="modal--large"
    >
      <div className="modal__body artifact-share-dialog">
        {unavailable ? (
          <p className="page-activity__empty">
            Public sharing is not enabled on this deployment.
          </p>
        ) : (
          <>
            <p className="artifact-share-dialog__hint">
              A public link opens this file without a BuildMax login. It is
              revocable and expires.
            </p>

            {error ? (
              <p className="settings-section__error" role="alert">
                {error}
              </p>
            ) : null}

            {freshLink ? (
              <div className="artifact-share-dialog__fresh">
                <p className="artifact-share-dialog__note">
                  New link — shown once. Copy it now.
                </p>
                <div className="artifact-share-dialog__link-row">
                  <code className="artifact-share-dialog__link">{freshLink}</code>
                  <CopyButton value={freshLink} />
                </div>
              </div>
            ) : null}

            <Button
              variant="primary"
              onClick={() => void onCreate()}
              busy={busyAction === "create"}
              disabled={busyAction !== null}
            >
              Create public link
            </Button>

            {live.length > 0 ? (
              <ul className="artifact-share-dialog__list">
                {live.map((s) => (
                  <li key={s.share_id} className="artifact-share-dialog__item">
                    <span className="artifact-share-dialog__meta">
                      Created {formatTime(s.created_at)}
                      {s.expires_at ? ` · expires ${formatTime(s.expires_at)}` : ""}
                      {` · ${s.retrieval_count} open${s.retrieval_count === 1 ? "" : "s"}`}
                    </span>
                    <Button
                      variant="danger"
                      size="compact"
                      onClick={() => void onRevoke(s.share_id)}
                      busy={busyAction === s.share_id}
                      disabled={busyAction !== null}
                    >
                      Revoke
                    </Button>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="page-activity__empty">No active links.</p>
            )}
          </>
        )}
      </div>
    </BaseModal>
  )
}
