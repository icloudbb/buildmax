import { useCallback, useEffect, useMemo, useState } from "react"
import { Button, IconButton, getInitials } from "@buildmax/gui"
import type { ApiSecret } from "../../lib/api/types"
import { getErrorMessage } from "../../lib/errorMessage"
import { useStableT, useT, type MessageKey } from "../../i18n"
import { createSecret, editSecret, listSecrets, setSecretState } from "./api"
import { Alert } from "../../components/state/Alert"
import { classifyError, deriveResourceState, type RequestError } from "../../state/resourceState"
import { isAllowed, type PermissionState } from "../../state/permissionState"

/**
 * SpaceSecrets manages a space's stored credentials: create one, edit its items,
 * disable or destroy it. Values are write-only -- nothing here reads one back.
 *
 * The two consequences in §3 of the design are stated plainly at the top,
 * because a Space that misreads them will grant a credential it should not: an
 * agent can read every Secret granted to its run, and a member who can trigger
 * a shared agent can obtain its values. See docs/design/space-secrets.md.
 */

type ItemRow = { key: string; value: string }

function emptyRows(): ItemRow[] {
  return [{ key: "", value: "" }]
}

function rowsToItems(rows: ItemRow[]): Record<string, string> {
  const items: Record<string, string> = {}
  for (const row of rows) {
    const key = row.key.trim()
    if (key) items[key] = row.value
  }
  return items
}

export function SpaceSecrets({
  token,
  spaceId,
  ownerState,
}: {
  token: string | null
  spaceId: string | null
  /** Owner-only capability state — see docs/design/portal-state-and-permission-feedback.md#permission-model. */
  ownerState: PermissionState
}) {
  const t = useT()
  const stableT = useStableT()
  const canManage = isAllowed(ownerState)
  // null means "not yet successfully fetched", distinct from [] meaning the
  // space genuinely has no secrets. See deriveResourceState.
  const [secretsData, setSecretsData] = useState<ApiSecret[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<RequestError | null>(null)
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState<string | null>(null)

  const load = useCallback(async () => {
    if (!token || !spaceId) return
    setLoading(true)
    setLoadError(null)
    try {
      const got = await listSecrets(token, spaceId)
      setSecretsData(got.secrets ?? [])
    } catch (err) {
      // secretsData from a prior successful fetch (if any) is left in place,
      // so a failed refresh reads as Stale rather than wiping the list.
      setLoadError(classifyError(err, stableT("secrets.error.load")))
    } finally {
      setLoading(false)
    }
  }, [token, spaceId, stableT])

  useEffect(() => {
    void load()
  }, [load])

  const secretsState = useMemo(
    () => deriveResourceState({ loading, data: secretsData, error: loadError, isEmpty: (data) => data.length === 0 }),
    [loading, secretsData, loadError]
  )
  const secrets = secretsData ?? []

  if (!canManage) {
    return (
      <section className="sec">
        <div className="sec__head">
          <div>
            <h2 className="sec__title">{t("secrets.title")}</h2>
            <p className="sec__copy">
              {ownerState === "unknown"
                ? t("secrets.checking")
                : ownerState === "failed"
                  ? t("secrets.unverified")
                  : t("secrets.ownerOnly")}
            </p>
          </div>
        </div>
      </section>
    )
  }

  const live = secrets.filter((s) => s.state !== "destroyed")

  return (
    <section className="sec">
      <div className="sec__head">
        <div>
          <h2 className="sec__title">{t("secrets.title")}</h2>
          <p className="sec__copy">{t("secrets.intro")}</p>
        </div>
        {!creating ? (
          <Button variant="primary" onClick={() => setCreating(true)}>
            {t("secrets.new")}
          </Button>
        ) : null}
      </div>

      <div className="sec-callout" role="note">
        <KeyIcon />
        <div>
          <strong>{t("secrets.warning.lead")}</strong>
          {t("secrets.warning.body")}
        </div>
      </div>

      {(secretsState.kind === "error" ||
        secretsState.kind === "forbidden" ||
        secretsState.kind === "notFound" ||
        secretsState.kind === "stale") && (
        <Alert
          tone={secretsState.kind === "stale" ? "stale" : secretsState.kind}
          message={secretsState.error.message}
          retry={{ label: t("shell.retry"), onClick: () => void load() }}
        />
      )}

      {creating ? (
        <CreateSecretForm
          onCancel={() => setCreating(false)}
          onCreate={async (name, description, items) => {
            if (!token || !spaceId) return
            await createSecret(token, spaceId, { name, description, items })
            setCreating(false)
            await load()
          }}
        />
      ) : null}

      {secretsState.kind === "loading" ? (
        <div className="sec-list" aria-hidden>
          {[0, 1].map((i) => (
            <div key={i} className="sec-card sec-card--skeleton" />
          ))}
        </div>
      ) : secretsState.kind === "error" || secretsState.kind === "forbidden" || secretsState.kind === "notFound" ? null : live.length === 0 && !creating ? (
        <div className="sec-empty">
          <KeyIcon />
          <p className="sec-empty__title">{t("secrets.empty.title")}</p>
          <p className="sec-empty__copy">{t("secrets.empty.copy")}</p>
        </div>
      ) : (
        <ul className="sec-list">
          {secrets.map((secret) => (
            <li key={secret.id}>
              <SecretCard
                secret={secret}
                open={editing === secret.id}
                onToggle={() => setEditing(editing === secret.id ? null : secret.id)}
                onEditItems={async (req) => {
                  if (!token || !spaceId) return
                  await editSecret(token, spaceId, secret.id, req)
                  setEditing(null)
                  await load()
                }}
                onSetState={async (state) => {
                  if (!token || !spaceId) return
                  await setSecretState(token, spaceId, secret.id, state)
                  await load()
                }}
              />
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

function CreateSecretForm({
  onCancel,
  onCreate,
}: {
  onCancel: () => void
  onCreate: (name: string, description: string, items: Record<string, string>) => Promise<void>
}) {
  const [name, setName] = useState("")
  const [description, setDescription] = useState("")
  const [rows, setRows] = useState<ItemRow[]>(emptyRows())
  const [raw, setRaw] = useState(false)
  const t = useT()
  const [rawText, setRawText] = useState("{\n  \n}")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit() {
    setError(null)
    let items: Record<string, string>
    if (raw) {
      try {
        const parsed: unknown = JSON.parse(rawText)
        if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
          throw new Error(t("secrets.error.jsonShape"))
        }
        items = {}
        for (const [k, v] of Object.entries(parsed as Record<string, unknown>)) {
          items[k] = String(v)
        }
      } catch (err) {
        setError(getErrorMessage(err, t("secrets.error.invalidJson")))
        return
      }
    } else {
      items = rowsToItems(rows)
    }
    if (!name.trim() || Object.keys(items).length === 0) {
      setError(t("secrets.error.required"))
      return
    }
    setBusy(true)
    try {
      await onCreate(name.trim(), description.trim(), items)
    } catch (err) {
      setError(getErrorMessage(err, t("secrets.error.create")))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="sec-form">
      <div className="sec-form__head">
        <h3 className="sec-form__title">{t("secrets.new")}</h3>
      </div>

      <div className="sec-field">
        <label className="modal__label" htmlFor="secret-name">
          {t("secrets.field.name")}
        </label>
        <input
          id="secret-name"
          className="modal__input"
          value={name}
          placeholder="aws-prod"
          onChange={(e) => setName(e.target.value)}
        />
      </div>

      <div className="sec-field">
        <label className="modal__label" htmlFor="secret-description">
          {t("secrets.field.description")}{" "}
          <span className="sec-field__optional">{t("secrets.field.optional")}</span>
        </label>
        <input
          id="secret-description"
          className="modal__input"
          value={description}
          placeholder={t("secrets.field.descriptionPlaceholder")}
          onChange={(e) => setDescription(e.target.value)}
        />
      </div>

      <div className="sec-field">
        <div className="sec-field__row">
          <span className="modal__label">{t("secrets.field.items")}</span>
          <Button variant="tertiary" size="compact" onClick={() => setRaw(!raw)}>
            {raw ? t("secrets.rowEditor") : t("secrets.pasteJson")}
          </Button>
        </div>
        {raw ? (
          <textarea
            className="modal__input sec-json"
            rows={6}
            value={rawText}
            spellCheck={false}
            onChange={(e) => setRawText(e.target.value)}
          />
        ) : (
          <ItemRowsEditor rows={rows} setRows={setRows} />
        )}
      </div>

      {error ? (
        <p className="sec__error" role="alert">
          {error}
        </p>
      ) : null}

      <div className="sec-form__actions">
        <Button variant="secondary" onClick={onCancel} disabled={busy}>
          {t("secrets.cancel")}
        </Button>
        <Button variant="primary" busy={busy} onClick={() => void submit()}>
          {t("secrets.create")}
        </Button>
      </div>
    </div>
  )
}

function ItemRowsEditor({
  rows,
  setRows,
}: {
  rows: ItemRow[]
  setRows: (rows: ItemRow[]) => void
}) {
  const t = useT()
  return (
    <div className="sec-items">
      {rows.map((row, i) => (
        <div key={i} className="sec-item">
          <input
            className="modal__input sec-item__key"
            aria-label={t("secrets.item.name")}
            placeholder="ITEM_NAME"
            value={row.key}
            onChange={(e) => {
              const next = rows.slice()
              next[i] = { ...row, key: e.target.value }
              setRows(next)
            }}
          />
          <input
            className="modal__input sec-item__val"
            aria-label={t("secrets.item.value")}
            placeholder={t("secrets.item.valuePlaceholder")}
            type="password"
            value={row.value}
            onChange={(e) => {
              const next = rows.slice()
              next[i] = { ...row, value: e.target.value }
              setRows(next)
            }}
          />
          <IconButton
            variant="tertiary"
            onClick={() => setRows(rows.filter((_, j) => j !== i))}
            aria-label={t("secrets.item.remove")}
            title={t("secrets.item.remove")}
          >
            ✕
          </IconButton>
        </div>
      ))}
      <Button
        variant="tertiary" size="compact" className="sec-items__add"
        onClick={() => setRows([...rows, { key: "", value: "" }])}
      >
        {t("secrets.item.add")}
      </Button>
    </div>
  )
}

const STATE_LABEL: Record<ApiSecret["state"], MessageKey> = {
  active: "secrets.state.active",
  disabled: "secrets.state.disabled",
  destroyed: "secrets.state.destroyed",
}

const STATE_TONE: Record<ApiSecret["state"], string> = {
  active: "active",
  disabled: "suspended",
  destroyed: "blocked",
}

function SecretCard({
  secret,
  open,
  onToggle,
  onEditItems,
  onSetState,
}: {
  secret: ApiSecret
  open: boolean
  onToggle: () => void
  onEditItems: (req: {
    items?: Record<string, string>
    set?: Record<string, string>
    remove?: string[]
  }) => Promise<void>
  onSetState: (state: "active" | "disabled" | "destroyed") => Promise<void>
}) {
  const t = useT()
  const destroyed = secret.state === "destroyed"
  return (
    <div className={`sec-card ${open ? "sec-card--open" : ""} ${destroyed ? "sec-card--dead" : ""}`}>
      <div className="sec-card__head">
        <span className="sec-card__logo" aria-hidden>
          {getInitials(secret.name)}
        </span>
        <div className="sec-card__ident">
          <span className="sec-card__name">{secret.name}</span>
          {secret.description ? (
            <span className="sec-card__desc">{secret.description}</span>
          ) : null}
        </div>
        <span className={`sec-status sec-status--${STATE_TONE[secret.state]}`}>
          <span className="sec-status__dot" aria-hidden />
          {t(STATE_LABEL[secret.state])}
        </span>
      </div>

      <div className="sec-card__items">
        {secret.item_names.length > 0 ? (
          secret.item_names.map((n) => (
            <span key={n} className="sec-chip">
              {n}
            </span>
          ))
        ) : (
          <span className="sec-card__noitems">{t("secrets.noItems")}</span>
        )}
      </div>

      {!destroyed ? (
        <div className="sec-card__actions">
          <Button variant="secondary" size="compact" onClick={onToggle}>
            {open ? t("secrets.close") : t("secrets.editItems")}
          </Button>
          {secret.state === "active" ? (
            <Button variant="tertiary" size="compact" onClick={() => void onSetState("disabled")}>
              {t("secrets.disable")}
            </Button>
          ) : (
            <Button variant="tertiary" size="compact" onClick={() => void onSetState("active")}>
              {t("secrets.enable")}
            </Button>
          )}
          <Button
            variant="danger" size="compact" className="sec-card__destroy"
            onClick={() => {
              if (window.confirm(t("secrets.destroyConfirm", { name: secret.name }))) {
                void onSetState("destroyed")
              }
            }}
          >
            {t("secrets.destroy")}
          </Button>
        </div>
      ) : null}

      {open && !destroyed ? (
        <EditItemsForm secret={secret} onEditItems={onEditItems} />
      ) : null}
    </div>
  )
}

function EditItemsForm({
  secret,
  onEditItems,
}: {
  secret: ApiSecret
  onEditItems: (req: {
    items?: Record<string, string>
    set?: Record<string, string>
    remove?: string[]
  }) => Promise<void>
}) {
  const [rows, setRows] = useState<ItemRow[]>(emptyRows())
  const t = useT()
  const [remove, setRemove] = useState<Set<string>>(new Set())
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  function toggleRemove(name: string) {
    const next = new Set(remove)
    if (next.has(name)) next.delete(name)
    else next.add(name)
    setRemove(next)
  }

  async function submit() {
    setError(null)
    const set = rowsToItems(rows)
    const removeList = [...remove]
    if (Object.keys(set).length === 0 && removeList.length === 0) {
      setError(t("secrets.edit.nothing"))
      return
    }
    setBusy(true)
    try {
      await onEditItems({ set, remove: removeList })
    } catch (err) {
      setError(getErrorMessage(err, t("secrets.edit.error")))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="sec-edit">
      <p className="sec-edit__hint">{t("secrets.edit.hint")}</p>
      {secret.item_names.length > 0 ? (
        <div className="sec-remove">
          {secret.item_names.map((name) => (
            <label key={name} className={`sec-remove__row ${remove.has(name) ? "sec-remove__row--on" : ""}`}>
              <input
                type="checkbox"
                checked={remove.has(name)}
                onChange={() => toggleRemove(name)}
              />
              {t("secrets.edit.remove")} <code>{name}</code>
            </label>
          ))}
        </div>
      ) : null}

      <span className="modal__label">{t("secrets.edit.setOrAdd")}</span>
      <ItemRowsEditor rows={rows} setRows={setRows} />

      {error ? (
        <p className="sec__error" role="alert">
          {error}
        </p>
      ) : null}

      <div className="sec-form__actions">
        <Button variant="primary" size="compact" busy={busy} onClick={() => void submit()}>
          {t("secrets.edit.save")}
        </Button>
      </div>
    </div>
  )
}

function KeyIcon() {
  return (
    <svg
      className="sec-icon"
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      <circle cx="7.5" cy="15.5" r="4.5" />
      <path d="m10.7 12.3 8.3-8.3" />
      <path d="m16 5 3 3" />
      <path d="m13 8 2.5 2.5" />
    </svg>
  )
}
