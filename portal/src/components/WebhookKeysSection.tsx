import { Button } from "@buildmax/gui"
import { useCallback, useEffect, useState } from "react"
import { getErrorMessage } from "../lib/errorMessage"
import { CopyButton } from "./CopyButton"
import {
  listWebhookKeys,
  createWebhookKey,
  revokeWebhookKey,
  type WebhookKeyMeta,
  type CreateWebhookKeyResponse,
} from "../features/webhookKeys/api"

interface WebhookKeysSectionProps {
  token: string | null
}

export function WebhookKeysSection({ token }: WebhookKeysSectionProps) {
  const [keys, setKeys] = useState<WebhookKeyMeta[]>([])
  const [loading, setLoading] = useState(true)
  const [creating, setCreating] = useState(false)
  const [newKey, setNewKey] = useState<CreateWebhookKeyResponse | null>(null)
  const [keyName, setKeyName] = useState("")
  const [revokingId, setRevokingId] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const fetchKeys = useCallback(() => {
    if (!token) return
    setLoading(true)
    listWebhookKeys(token)
      .then((res) => setKeys(res.keys))
      .catch((err) => setError(getErrorMessage(err, "Failed to load keys")))
      .finally(() => setLoading(false))
  }, [token])

  useEffect(() => {
    fetchKeys()
  }, [fetchKeys])

  function handleCreateKey() {
    if (!token) return
    setError(null)
    setNewKey(null)
    setCreating(true)
    createWebhookKey({ name: keyName || undefined }, token)
      .then((res) => {
        setNewKey(res)
        setKeyName("")
        fetchKeys()
      })
      .catch((err) => setError(getErrorMessage(err, "Failed to create key")))
      .finally(() => setCreating(false))
  }

  function handleCloseNewKey() {
    setNewKey(null)
  }

  function handleRevoke(keyId: string) {
    if (!token) return
    setError(null)
    setRevokingId(keyId)
    revokeWebhookKey(keyId, token)
      .then(() => fetchKeys())
      .catch((err) => setError(getErrorMessage(err, "Failed to revoke key")))
      .finally(() => setRevokingId(null))
  }

  return (
    <section className="settings-section settings-webhook">
      <h2 className="settings-panel__heading">Webhook API keys</h2>
      <div className="settings-panel__heading-divider" role="separator" />
      <p className="settings-webhook__description">
        Use these keys to trigger runs from external systems (e.g. CI, scripts) for your account. Send the key in{" "}
        <code>Authorization: Bearer &lt;key&gt;</code> or <code>X-Webhook-Key</code> when calling{" "}
        <code>POST /api/webhook</code>.
      </p>

      {error && (
        <div className="settings-webhook__error" role="alert">
          {error}
        </div>
      )}

      {newKey && (
        <div className="settings-webhook__new-key" role="dialog" aria-label="New webhook key">
          <p className="settings-webhook__new-key-warning">
            Copy the key now. It won&apos;t be shown again.
          </p>
          <div className="settings-webhook__new-key-row">
            <code className="settings-webhook__new-key-value">{newKey.key}</code>
            <CopyButton value={newKey.key} />
          </div>
          <Button
            variant="secondary"
            onClick={handleCloseNewKey}
          >
            Done
          </Button>
        </div>
      )}

      <div className="settings-webhook__create">
        <input
          type="text"
          className="settings-webhook__input"
          placeholder="Key name (optional)"
          value={keyName}
          onChange={(e) => setKeyName(e.target.value)}
          disabled={creating}
        />
        <Button
          variant="primary" busy={creating}
          onClick={handleCreateKey}
        >
          Create key
        </Button>
      </div>

      {loading ? (
        <p className="settings-section__muted">Loading keys…</p>
      ) : keys.length === 0 ? (
        <p className="settings-section__muted">No webhook keys yet. Create one above.</p>
      ) : (
        <ul className="settings-webhook__key-list">
          {keys.map((k) => (
            <li key={k.id} className="settings-webhook__key-item">
              <span className="settings-webhook__key-name">{k.name || k.id}</span>
              <span className="settings-webhook__key-meta">
                {new Date(k.created_at).toLocaleString()}
              </span>
              <Button
                variant="danger"
                size="compact"
                busy={revokingId === k.id}
                disabled={revokingId !== null}
                onClick={() => handleRevoke(k.id)}
              >
                Revoke
              </Button>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
