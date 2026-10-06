import { Button } from "@buildmax/gui"
import { useCallback, useEffect, useState } from "react"
import { getErrorMessage } from "../lib/errorMessage"
import { useStableT, useT } from "../i18n"
import { useTimestamp } from "../lib/dateFormat"
import { CopyButton } from "./CopyButton"
import {
  listWebhookKeys,
  createWebhookKey,
  revokeWebhookKey,
  type WebhookKeyMeta,
  type CreateWebhookKeyResponse,
} from "../features/webhookKeys/api"

const WEBHOOK_CODES = new Map([
  ["{header}", "Authorization: Bearer <key>"],
  ["{altHeader}", "X-Webhook-Key"],
  ["{endpoint}", "POST /api/webhook"],
])

interface WebhookKeysSectionProps {
  token: string | null
}

export function WebhookKeysSection({ token }: WebhookKeysSectionProps) {
  const t = useT()
  const formatTimestamp = useTimestamp()
  const stableT = useStableT()
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
      .catch((err) => setError(getErrorMessage(err, stableT("account.webhook.loadError"))))
      .finally(() => setLoading(false))
  }, [token, stableT])

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
      .catch((err) => setError(getErrorMessage(err, stableT("account.webhook.createError"))))
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
      .catch((err) => setError(getErrorMessage(err, stableT("account.webhook.revokeError"))))
      .finally(() => setRevokingId(null))
  }

  // The headers and the endpoint are code inside the sentence, so the
  // translated sentence is split at their placeholders.
  const description = t("account.webhook.description")
    .split(/(\{header\}|\{altHeader\}|\{endpoint\})/)
    .map((part, i) => {
      const code = WEBHOOK_CODES.get(part)
      return code ? <code key={i}>{code}</code> : part
    })

  return (
    <section className="settings-section settings-webhook">
      <h2 className="settings-panel__heading">{t("account.webhook.heading")}</h2>
      <div className="settings-panel__heading-divider" role="separator" />
      <p className="settings-webhook__description">
        {description}
      </p>

      {error && (
        <div className="settings-webhook__error" role="alert">
          {error}
        </div>
      )}

      {newKey && (
        <div className="settings-webhook__new-key" role="dialog" aria-label={t("account.webhook.newKey")}>
          <p className="settings-webhook__new-key-warning">
            {t("account.webhook.copyNow")}
          </p>
          <div className="settings-webhook__new-key-row">
            <code className="settings-webhook__new-key-value">{newKey.key}</code>
            <CopyButton value={newKey.key} />
          </div>
          <Button
            variant="secondary"
            onClick={handleCloseNewKey}
          >
            {t("account.webhook.done")}
          </Button>
        </div>
      )}

      <div className="settings-webhook__create">
        <input
          type="text"
          className="settings-webhook__input"
          placeholder={t("account.webhook.namePlaceholder")}
          value={keyName}
          onChange={(e) => setKeyName(e.target.value)}
          disabled={creating}
        />
        <Button
          variant="primary" busy={creating}
          onClick={handleCreateKey}
        >
          {t("account.webhook.create")}
        </Button>
      </div>

      {loading ? (
        <p className="settings-section__muted">{t("account.webhook.loading")}</p>
      ) : keys.length === 0 ? (
        <p className="settings-section__muted">{t("account.webhook.empty")}</p>
      ) : (
        <ul className="settings-webhook__key-list">
          {keys.map((k) => (
            <li key={k.id} className="settings-webhook__key-item">
              <span className="settings-webhook__key-name">{k.name || k.id}</span>
              <span className="settings-webhook__key-meta">
                {formatTimestamp(k.created_at)}
              </span>
              <Button
                variant="danger"
                size="compact"
                busy={revokingId === k.id}
                disabled={revokingId !== null}
                onClick={() => handleRevoke(k.id)}
              >
                {t("account.webhook.revoke")}
              </Button>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
