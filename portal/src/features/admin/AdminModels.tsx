import { Button, Field, Input, Spinner, useToast } from "@buildmax/gui"
import { useCallback, useEffect, useMemo, useState } from "react"
import { useStableT, useT } from "../../i18n"
import type { ApiAdminModel } from "../../lib/api/types"
import { getErrorMessage } from "../../lib/errorMessage"
import { createAdminModel, listAdminModels, setAdminModelEnabled, type AdminCreateModelInput } from "./api"
import { Alert } from "../../components/state/Alert"
import { EmptyState } from "../../components/state/EmptyState"
import { classifyError, deriveResourceState, type RequestError } from "../../state/resourceState"

/**
 * AdminModels shows which models this deployment will call, and adds new ones.
 *
 * Every enabled model is callable by every user of the deployment, so enabled
 * state is the whole answer to whether a row can be used. One of them is the
 * default, which is what a caller that names no model gets.
 *
 * A model can be added here now that the provider credential is encrypted at
 * rest: the key is a write-only password field, sent in the request body alone,
 * stored sealed, and never read back. A deployment with no encryption key
 * configured refuses a model that carries one, and the form reports that.
 */
interface ModelForm {
  name: string
  providerType: string
  apiURL: string
  model: string
  apiKey: string
  contextWindow: string
  callTimeout: string
  maxTokens: string
  reasoning: string
  cacheMode: string
  cacheTTL: string
  currency: string
  inputPrice: string
  cacheReadPrice: string
  cacheWritePrice: string
  outputPrice: string
  vision: boolean
  capabilities: string
}

const emptyForm: ModelForm = {
  name: "",
  providerType: "openai_compatible",
  apiURL: "",
  model: "",
  apiKey: "",
  contextWindow: "",
  callTimeout: "",
  maxTokens: "",
  reasoning: "",
  cacheMode: "",
  cacheTTL: "",
  currency: "",
  inputPrice: "",
  cacheReadPrice: "",
  cacheWritePrice: "",
  outputPrice: "",
  vision: false,
  capabilities: "",
}

// Empty optional fields are left out of the request so the server applies the
// same defaults the shell command does, rather than being sent as blanks.
function buildInput(f: ModelForm): AdminCreateModelInput {
  const str = (s: string) => (s.trim() === "" ? undefined : s.trim())
  const num = (s: string) => (s.trim() === "" ? undefined : Number(s))
  const caps = f.capabilities
    .split(",")
    .map((c) => c.trim())
    .filter(Boolean)
  return {
    name: f.name.trim(),
    api_url: f.apiURL.trim(),
    model: f.model.trim(),
    provider_type: str(f.providerType),
    api_key: str(f.apiKey),
    context_window: num(f.contextWindow),
    call_timeout: num(f.callTimeout),
    max_tokens: num(f.maxTokens),
    reasoning: str(f.reasoning),
    cache_mode: str(f.cacheMode),
    cache_ttl: str(f.cacheTTL),
    currency: str(f.currency),
    input_price: str(f.inputPrice),
    cache_read_price: str(f.cacheReadPrice),
    cache_write_price: str(f.cacheWritePrice),
    output_price: str(f.outputPrice),
    vision: f.vision || undefined,
    capabilities: caps.length > 0 ? caps : undefined,
  }
}

export function AdminModels({ token }: { token: string | null }) {
  const t = useT()
  const stableT = useStableT()
  // null means "not yet successfully fetched", distinct from [] meaning the
  // catalog genuinely has no models. See deriveResourceState.
  const [modelsData, setModelsData] = useState<ApiAdminModel[] | null>(null)
  const [defaultModel, setDefaultModel] = useState<string | undefined>()
  const [loading, setLoading] = useState(true)
  const [listError, setListError] = useState<RequestError | null>(null)
  const [busyId, setBusyId] = useState<string | null>(null)
  // Toggle's own error, tagged with which model it was.
  const [toggleError, setToggleError] = useState<{ id: string; message: string } | null>(null)
  const toast = useToast()
  const [form, setForm] = useState<ModelForm>(emptyForm)
  const [creating, setCreating] = useState(false)
  // The create-model form's own error: a page-level message, since the form
  // is a singleton with no row to associate it with.
  const [formError, setFormError] = useState<string | null>(null)

  const load = useCallback(() => {
    if (!token) return
    setLoading(true)
    setListError(null)
    listAdminModels(token)
      .then((res) => {
        setModelsData(res.models)
        setDefaultModel(res.default_model)
      })
      // modelsData from a prior successful fetch (if any) is left in place,
      // so a failed refresh reads as Stale rather than wiping the catalog.
      .catch((err) => setListError(classifyError(err, stableT("admin.models.loadError"))))
      .finally(() => setLoading(false))
  }, [token, stableT])

  useEffect(load, [load])

  const modelsState = useMemo(
    () => deriveResourceState({ loading, data: modelsData, error: listError, isEmpty: (data) => data.length === 0 }),
    [loading, modelsData, listError]
  )
  const models = modelsData ?? []

  function set<K extends keyof ModelForm>(key: K, value: ModelForm[K]) {
    setForm((f) => ({ ...f, [key]: value }))
  }

  function toggle(entry: ApiAdminModel) {
    if (!token) return
    if (
      entry.enabled &&
      !window.confirm(t("admin.models.retireConfirm", { name: entry.name }))
    )
      return
    setBusyId(entry.id)
    setToggleError(null)
    setAdminModelEnabled(token, entry.id, !entry.enabled)
      .then(load)
      .catch((err) =>
        setToggleError({ id: entry.id, message: getErrorMessage(err, stableT("admin.changeFailed")) }),
      )
      .finally(() => setBusyId(null))
  }

  function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!token || creating) return
    setCreating(true)
    setFormError(null)
    createAdminModel(token, buildInput(form))
      .then((created) => {
        // Clear the whole form, which is what drops the key from the page — it
        // was never persisted anywhere else.
        setForm(emptyForm)
        toast(stableT("admin.models.added", { name: created.name }))
        load()
      })
      .catch((err) => setFormError(getErrorMessage(err, stableT("admin.models.notAdded"))))
      .finally(() => setCreating(false))
  }

  return (
    <div className="admin-sections">
      <section className="settings-page__section">
        <div className="settings-page__section-head">
          <div>
            <h2 className="settings-page__section-title">{t("admin.models.title")}</h2>
            <p className="settings-page__section-copy">
              {t("admin.models.copy")}
              {defaultModel ? t("admin.models.defaultCopy", { model: defaultModel }) : ""}
            </p>
          </div>
        </div>

        {(modelsState.kind === "error" ||
          modelsState.kind === "forbidden" ||
          modelsState.kind === "notFound" ||
          modelsState.kind === "stale") && (
          <Alert
            tone={modelsState.kind === "stale" ? "stale" : modelsState.kind}
            message={modelsState.error.message}
            retry={{ label: t("shell.retry"), onClick: load }}
          />
        )}

        {modelsState.kind === "loading" ? (
          <Spinner />
        ) : modelsState.kind === "readyEmpty" ? (
          <EmptyState message={t("admin.models.empty")} />
        ) : modelsState.kind === "error" || modelsState.kind === "forbidden" || modelsState.kind === "notFound" ? null : (
          <ul className="admin-list">
            {models.map((entry) => (
              <li key={entry.id} className="admin-list__row">
                <span className="admin-list__main">
                  {entry.name}
                  <span className="admin-list__meta"> · {entry.model}</span>
                </span>
                <span className={entry.enabled ? "admin-pill admin-pill--ok" : "admin-pill"}>
                  {entry.enabled ? t("admin.models.enabled") : t("admin.models.retired")}
                </span>
                {entry.name === defaultModel ? (
                  <span className="admin-list__meta">{t("admin.models.default")}</span>
                ) : null}
                <Button
                  variant={entry.enabled ? "danger" : "secondary"}
                  size="compact"
                  busy={busyId === entry.id}
                  onClick={() => toggle(entry)}
                >
                  {entry.enabled ? t("admin.retire") : t("admin.enable")}
                </Button>
                {toggleError?.id === entry.id ? (
                  <p className="settings-section__error" role="alert">
                    {toggleError.message}
                  </p>
                ) : null}
              </li>
            ))}
          </ul>
        )}

        <p className="admin-scope-note">
          {t("admin.models.scopeNoteBefore")}
          <code>llm.default_model</code>
          {t("admin.models.scopeNoteAfter")}
        </p>
      </section>

      <section className="settings-page__section">
        <div className="settings-page__section-head">
          <div>
            <h2 className="settings-page__section-title">{t("admin.models.addTitle")}</h2>
            <p className="settings-page__section-copy">{t("admin.models.addCopy")}</p>
          </div>
        </div>

        {formError ? (
          <p className="settings-section__error" role="alert">
            {formError}
          </p>
        ) : null}

        <form className="admin-form" onSubmit={submit}>
          <div className="admin-form__grid">
            <Field label={t("admin.models.name")} className="admin-item">
              <Input
                value={form.name}
                onChange={(e) => set("name", e.target.value)}
                placeholder="Claude Sonnet 5"
              />
            </Field>
            <Field label={t("admin.models.provider")} className="admin-item">
              <Input
                value={form.providerType}
                onChange={(e) => set("providerType", e.target.value)}
                placeholder="openai_compatible"
              />
            </Field>
            <Field label={t("admin.models.apiURL")} className="admin-item">
              <Input
                value={form.apiURL}
                onChange={(e) => set("apiURL", e.target.value)}
                placeholder="https://api.example.com/v1"
              />
            </Field>
            <Field label={t("admin.models.providerModel")} className="admin-item">
              <Input
                value={form.model}
                onChange={(e) => set("model", e.target.value)}
                placeholder="vendor/model-name"
              />
            </Field>
            <Field label={t("admin.models.apiKey")} className="admin-item">
              <Input
                type="password"
                autoComplete="off"
                value={form.apiKey}
                onChange={(e) => set("apiKey", e.target.value)}
                placeholder="sk-…"
              />
            </Field>
            <Field label={t("admin.models.contextWindow")} className="admin-item">
              <Input
                type="number"
                value={form.contextWindow}
                onChange={(e) => set("contextWindow", e.target.value)}
                placeholder="128000"
              />
            </Field>
          </div>

          <details className="admin-advanced">
            <summary>{t("admin.models.advanced")}</summary>
            <div className="admin-form__grid">
              <Field label={t("admin.models.callTimeout")} className="admin-item">
                <Input type="number" value={form.callTimeout} onChange={(e) => set("callTimeout", e.target.value)} />
              </Field>
              <Field label={t("admin.models.maxTokens")} className="admin-item">
                <Input type="number" value={form.maxTokens} onChange={(e) => set("maxTokens", e.target.value)} />
              </Field>
              <Field label={t("admin.models.reasoning")} className="admin-item">
                <Input value={form.reasoning} onChange={(e) => set("reasoning", e.target.value)} placeholder="off, low, medium, high" />
              </Field>
              <Field label={t("admin.models.cacheMode")} className="admin-item">
                <Input value={form.cacheMode} onChange={(e) => set("cacheMode", e.target.value)} placeholder="auto, off, force" />
              </Field>
              <Field label={t("admin.models.cacheTTL")} className="admin-item">
                <Input value={form.cacheTTL} onChange={(e) => set("cacheTTL", e.target.value)} placeholder="provider_default, 5m, 1h" />
              </Field>
              <Field label={t("admin.models.currency")} className="admin-item">
                <Input value={form.currency} onChange={(e) => set("currency", e.target.value)} placeholder="USD" />
              </Field>
              <Field label={t("admin.models.inputPrice")} className="admin-item">
                <Input value={form.inputPrice} onChange={(e) => set("inputPrice", e.target.value)} />
              </Field>
              <Field label={t("admin.models.outputPrice")} className="admin-item">
                <Input value={form.outputPrice} onChange={(e) => set("outputPrice", e.target.value)} />
              </Field>
              <Field label={t("admin.models.cacheReadPrice")} className="admin-item">
                <Input value={form.cacheReadPrice} onChange={(e) => set("cacheReadPrice", e.target.value)} />
              </Field>
              <Field label={t("admin.models.cacheWritePrice")} className="admin-item">
                <Input value={form.cacheWritePrice} onChange={(e) => set("cacheWritePrice", e.target.value)} />
              </Field>
              <Field label={t("admin.models.capabilities")} className="admin-item">
                <Input value={form.capabilities} onChange={(e) => set("capabilities", e.target.value)} placeholder="text_chat, tool_calls" />
              </Field>
              <label className="admin-check">
                <input type="checkbox" checked={form.vision} onChange={(e) => set("vision", e.target.checked)} />
                {t("admin.models.vision")}
              </label>
            </div>
          </details>

          <div className="admin-toolbar">
            <Button
              type="submit"
              variant="primary"
              busy={creating}
              disabled={!form.name.trim() || !form.apiURL.trim() || !form.model.trim()}
            >
              {t("admin.models.add")}
            </Button>
          </div>
        </form>
      </section>
    </div>
  )
}
