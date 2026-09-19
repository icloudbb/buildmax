import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Button } from "@buildmax/gui"
import type { ApiArtifact } from "../../lib/api/types"
import { getErrorMessage } from "../../lib/errorMessage"
import { downloadAuthenticated } from "../../lib/download"
import { buildHash } from "../../router"
import { useAuth } from "../../contexts/AuthContext"
import { useSpace } from "../../contexts/SpaceContext"
import { Alert } from "../../components/state/Alert"
import { EmptyState } from "../../components/state/EmptyState"
import { classifyError, deriveResourceState, type RequestError } from "../../state/resourceState"
import {
  artifactContentUrl,
  artifactLabel,
  confirmArtifactDeletion,
  deleteArtifact,
  formatSize,
  formatTime,
  listArtifacts,
  mayDelete,
  sourceLabel,
  uploadArtifact,
} from "../../features/artifacts"

const PAGE_SIZE = 50

/**
 * Every durable file the current space holds.
 *
 * A top-level area rather than a settings tab: an artifact is what work
 * produced, so it is browsed alongside issues and runs, not alongside the
 * knobs that configure the space.
 */
interface ArtifactsProps {
  spaceId: string
}

export function Artifacts({ spaceId }: ArtifactsProps) {
  const { token, user } = useAuth()
  const { currentUserRole } = useSpace()
  // null means "not yet successfully fetched", distinct from [] meaning the
  // space genuinely has no artifacts. See deriveResourceState.
  const [itemsData, setItemsData] = useState<ApiArtifact[] | null>(null)
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [busyAction, setBusyAction] = useState<{ id: string; kind: "download" | "delete" } | null>(null)
  const [uploading, setUploading] = useState(false)
  const [listError, setListError] = useState<RequestError | null>(null)
  // Upload's own error: a page-level message, since the Upload button is a
  // singleton with no row to associate it with.
  const [uploadError, setUploadError] = useState<string | null>(null)
  // Download/Delete's own error, tagged with which artifact it was, so it
  // renders next to that row instead of a page-level banner.
  const [rowError, setRowError] = useState<{ id: string; message: string } | null>(null)
  const fileInput = useRef<HTMLInputElement>(null)

  const load = useCallback(
    (offset: number) => {
      if (!spaceId || !token) return
      setLoading(true)
      setListError(null)
      listArtifacts(spaceId, token, { limit: PAGE_SIZE, offset })
        .then((res) => {
          setItemsData((prev) => (offset === 0 ? res.items : [...(prev ?? []), ...res.items]))
          setTotal(res.total)
        })
        // itemsData from a prior successful fetch (if any) is left in place,
        // so a failed refresh reads as Stale rather than wiping the list.
        .catch((err) => setListError(classifyError(err, "Failed to load artifacts")))
        .finally(() => setLoading(false))
    },
    [spaceId, token]
  )

  useEffect(() => {
    load(0)
  }, [load])

  const itemsState = useMemo(
    () => deriveResourceState({ loading, data: itemsData, error: listError, isEmpty: (data) => data.length === 0 }),
    [loading, itemsData, listError]
  )
  const items = itemsData ?? []

  async function onFileChosen(event: React.ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0]
    // Cleared straight away so choosing the same file twice still fires.
    event.target.value = ""
    if (!file || !spaceId || !token) return
    setUploading(true)
    setUploadError(null)
    try {
      await uploadArtifact(spaceId, token, file)
      load(0)
    } catch (err) {
      setUploadError(getErrorMessage(err, "Upload failed"))
    } finally {
      setUploading(false)
    }
  }

  async function onDownload(artifact: ApiArtifact) {
    if (!token) return
    setBusyAction({ id: artifact.id, kind: "download" })
    setRowError(null)
    try {
      await downloadAuthenticated(artifactContentUrl(artifact.id), token, artifact.filename)
    } catch (err) {
      setRowError({ id: artifact.id, message: getErrorMessage(err, "Download failed") })
    } finally {
      setBusyAction(null)
    }
  }

  async function onDelete(artifact: ApiArtifact) {
    if (!token) return
    if (!confirmArtifactDeletion(artifact)) return
    setBusyAction({ id: artifact.id, kind: "delete" })
    setRowError(null)
    try {
      await deleteArtifact(artifact.id, token)
      setItemsData((prev) => (prev ?? []).filter((a) => a.id !== artifact.id))
      setTotal((prev) => Math.max(0, prev - 1))
    } catch (err) {
      setRowError({ id: artifact.id, message: getErrorMessage(err, "Delete failed") })
    } finally {
      setBusyAction(null)
    }
  }

  const countLabel = itemsData === null ? null : total === 1 ? "1 artifact" : `${total} artifacts`

  return (
    <div className="page-activity">
      <div className="page-activity__head">
        <div>
          <h1 className="page-activity__title">Artifacts</h1>
          <p className="page-activity__subtitle">
            Files this space keeps. Each one has a stable reference any member can open, and its
            content never changes — a new version is a new artifact.
          </p>
        </div>
        <div className="page-activity__actions">
          <Button
            variant="primary"
            onClick={() => fileInput.current?.click()}
            busy={uploading}
            disabled={!spaceId}
          >
            Upload a file
          </Button>
          <input
            ref={fileInput}
            type="file"
            className="artifact-list__file-input"
            onChange={onFileChosen}
            hidden
          />
        </div>
      </div>

      {(itemsState.kind === "error" ||
        itemsState.kind === "forbidden" ||
        itemsState.kind === "notFound" ||
        itemsState.kind === "stale") && (
        <Alert
          tone={itemsState.kind === "stale" ? "stale" : itemsState.kind}
          message={itemsState.error.message}
          retry={{ label: "Retry", onClick: () => load(0) }}
        />
      )}
      {uploadError ? (
        <p className="settings-section__error" role="alert">
          {uploadError}
        </p>
      ) : null}

      <section className="issues-page__panel" aria-label="Artifact list">
        {countLabel ? <p className="page-activity__meta">{countLabel}</p> : null}

        {itemsState.kind === "readyEmpty" ? (
          <EmptyState message="Nothing kept here yet. Upload a file, or have an agent publish one with UploadArtifact." />
        ) : null}

        {itemsState.kind !== "forbidden" && itemsState.kind !== "notFound" && items.length > 0 ? (
          <ul className="artifact-list">
            {items.map((artifact) => (
              <li key={artifact.id} className="artifact-row">
                <div className="artifact-row__main">
                  {/* The name opens the artifact rather than the whole row: the
                      row carries its own buttons, and one cannot nest another. */}
                  <a className="artifact-row__name" href={buildHash({ name: "artifact", artifactId: artifact.id })}>
                    {artifactLabel(artifact)}
                  </a>
                  <span className="artifact-row__meta">
                    {artifact.filename} · {formatSize(artifact.size_bytes)} ·{" "}
                    {sourceLabel(artifact)}
                  </span>
                </div>
                <div className="artifact-row__meta">
                  <time>{formatTime(artifact.created_at)}</time>
                </div>
                <div className="artifact-row__actions">
                  <Button
                    variant="secondary"
                    size="compact"
                    onClick={() => onDownload(artifact)}
                    busy={busyAction?.id === artifact.id && busyAction.kind === "download"}
                    disabled={busyAction?.id === artifact.id}
                  >
                    Download
                  </Button>
                  {mayDelete(artifact, user?.id, currentUserRole) ? (
                    <Button
                      variant="danger"
                      size="compact"
                      onClick={() => void onDelete(artifact)}
                      busy={busyAction?.id === artifact.id && busyAction.kind === "delete"}
                      disabled={busyAction?.id === artifact.id}
                    >
                      Delete
                    </Button>
                  ) : null}
                </div>
                {rowError?.id === artifact.id ? (
                  <p className="settings-section__error" role="alert">
                    {rowError.message}
                  </p>
                ) : null}
              </li>
            ))}
          </ul>
        ) : null}

        {loading ? <p className="page-activity__empty">Loading…</p> : null}

        {itemsState.kind !== "forbidden" && itemsState.kind !== "notFound" && items.length < total ? (
          <Button
            variant="tertiary"
            onClick={() => load(items.length)}
            disabled={loading}
          >
            Show older ({total - items.length} more)
          </Button>
        ) : null}
      </section>
    </div>
  )
}
