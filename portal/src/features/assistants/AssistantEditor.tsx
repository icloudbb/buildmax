import { useEffect, useId, useMemo, useState } from "react"
import { Button } from "@buildmax/gui"
import type {
  ApiAgent,
  ApiArtifact,
  ApiAssistantDefinition,
  ApiServiceAccount,
  ApiWorkflow,
} from "../../lib/api/types"
import { getAgents, listAgentModels } from "../agents/api"
import { getWorkflows } from "../workflows/api"
import { listArtifacts } from "../artifacts/api"
import { getServiceAccounts } from "../spaces/api"
import { useT, type MessageKey } from "../../i18n"
import {
  type AssistantDraft,
  type RosterDraft,
  draftToDefinition,
  newRosterEntry,
  parseFieldList,
  parseOutputSchema,
  workflowResultProperties,
} from "./model"

/** The server lists at most this many artifacts per page; enough to pick from. */
const ARTIFACT_PAGE = 200

interface Options {
  agents: ApiAgent[]
  workflows: ApiWorkflow[]
  artifacts: ApiArtifact[]
  artifactsTotal: number
  models: string[]
  serviceAccounts: ApiServiceAccount[]
  /** What could not be loaded, so a missing choice is explained rather than silent. */
  failed: MessageKey[]
}

function useEditorOptions(spaceId: string, token: string | null): Options | null {
  const [options, setOptions] = useState<Options | null>(null)
  useEffect(() => {
    if (!token) return
    let cancelled = false
    void Promise.allSettled([
      getAgents(spaceId, token),
      getWorkflows(spaceId, token),
      listArtifacts(spaceId, token, { limit: ARTIFACT_PAGE }),
      listAgentModels(token),
      getServiceAccounts(spaceId, token),
    ]).then(([agents, workflows, artifacts, models, accounts]) => {
      if (cancelled) return
      const failed: MessageKey[] = []
      const value = <T,>(r: PromiseSettledResult<T>, what: MessageKey, fallback: T): T => {
        if (r.status === "fulfilled") return r.value
        failed.push(what)
        return fallback
      }
      const artifactList = value(artifacts, "assistants.editor.option.files", { items: [], total: 0 })
      setOptions({
        agents: value(agents, "assistants.editor.option.agents", []),
        // Only a published Workflow can be on a roster.
        workflows: value(workflows, "assistants.editor.option.workflows", { workflows: [] }).workflows.filter((w) => w.status === "published"),
        artifacts: artifactList.items,
        artifactsTotal: artifactList.total,
        models: value(models, "assistants.editor.option.models", []),
        serviceAccounts: value(accounts, "assistants.editor.option.serviceAccounts", []),
        failed,
      })
    })
    return () => {
      cancelled = true
    }
  }, [spaceId, token])
  return options
}

/**
 * The whole definition of an Assistant: who it is, what it may run and read,
 * who may ask, and the service account its work runs as. Used for create and
 * edit; read-only for members, who may see but not change it.
 */
export function AssistantEditor({
  spaceId,
  token,
  initial,
  mode,
  readOnly,
  busy,
  error,
  onSubmit,
  onCancel,
}: {
  spaceId: string
  token: string | null
  initial: AssistantDraft
  mode: "create" | "edit"
  readOnly: boolean
  busy: boolean
  error: string | null
  onSubmit: (definition: ApiAssistantDefinition) => void
  onCancel?: () => void
}) {
  const t = useT()
  const [draft, setDraft] = useState<AssistantDraft>(initial)
  const [localError, setLocalError] = useState<string | null>(null)
  const options = useEditorOptions(spaceId, token)
  const ids = useId()

  const set = <K extends keyof AssistantDraft>(key: K, value: AssistantDraft[K]) =>
    setDraft((prev) => ({ ...prev, [key]: value }))

  const setEntry = (index: number, next: Partial<RosterDraft>) =>
    setDraft((prev) => ({ ...prev, roster: prev.roster.map((e, i) => (i === index ? { ...e, ...next } : e)) }))

  function submit() {
    const built = draftToDefinition(draft, t)
    if ("error" in built) {
      setLocalError(built.error)
      return
    }
    setLocalError(null)
    onSubmit(built.definition)
  }

  const modelOptions = useMemo(() => {
    const names = options?.models ?? []
    return draft.model && !names.includes(draft.model) ? [draft.model, ...names] : names
  }, [options, draft.model])

  const field = (name: string) => `${ids}-${name}`

  return (
    <form
      className="sec-form asst-editor"
      aria-label={mode === "create" ? t("assistants.editor.formNew") : t("assistants.editor.formEdit")}
      onSubmit={(e) => {
        e.preventDefault()
        submit()
      }}
    >
      <div className="sec-form__head">
        <h3 className="sec-form__title">{mode === "create" ? t("assistants.editor.formNew") : t("assistants.editor.definition")}</h3>
      </div>
      {options && options.failed.length > 0 ? (
        <p className="sec__error" role="alert">
          {t("assistants.editor.loadFailed", {
            what: options.failed.map((key) => t(key)).join(t("assistants.listSeparator")),
          })}
        </p>
      ) : null}

      <fieldset className="asst-editor__fields" disabled={readOnly || busy}>
        <div className="sec-field">
          <label className="modal__label" htmlFor={field("name")}>
            {t("assistants.editor.name")}
          </label>
          <input
            id={field("name")}
            className="modal__input"
            value={draft.name}
            maxLength={255}
            placeholder={t("assistants.editor.namePlaceholder")}
            onChange={(e) => set("name", e.target.value)}
          />
        </div>
        <div className="sec-field">
          <label className="modal__label" htmlFor={field("description")}>
            {t("assistants.editor.description")}{" "}
            <span className="sec-field__optional">{t("assistants.editor.optional")}</span>
          </label>
          <textarea
            id={field("description")}
            className="modal__textarea"
            rows={2}
            maxLength={2000}
            value={draft.description}
            placeholder={t("assistants.editor.descriptionPlaceholder")}
            onChange={(e) => set("description", e.target.value)}
          />
        </div>
        <div className="sec-field">
          <label className="modal__label" htmlFor={field("instructions")}>
            {t("assistants.editor.instructions")}{" "}
            <span className="sec-field__optional">{t("assistants.editor.optional")}</span>
          </label>
          <textarea
            id={field("instructions")}
            className="modal__textarea"
            rows={5}
            maxLength={32000}
            value={draft.instructions}
            onChange={(e) => set("instructions", e.target.value)}
          />
        </div>
        <div className="asst-editor__row">
          <div className="sec-field">
            <label className="modal__label" htmlFor={field("model")}>
              {t("assistants.editor.model")}
            </label>
            <select
              id={field("model")}
              className="modal__input"
              value={draft.model}
              onChange={(e) => set("model", e.target.value)}
            >
              <option value="">{t("assistants.editor.modelDefault")}</option>
              {modelOptions.map((m) => (
                <option key={m} value={m}>
                  {m}
                </option>
              ))}
            </select>
          </div>
          <div className="sec-field">
            <label className="modal__label" htmlFor={field("audience")}>
              {t("assistants.editor.whoCanAsk")}
            </label>
            <select
              id={field("audience")}
              className="modal__input"
              value={draft.audience}
              onChange={(e) => set("audience", e.target.value as AssistantDraft["audience"])}
            >
              <option value="space_members">{t("assistants.audience.spaceMembers")}</option>
              <option value="all_users">{t("assistants.audience.allUsers")}</option>
            </select>
          </div>
        </div>

        <div className="sec-field">
          <label className="modal__label" htmlFor={field("service-account")}>
            {t("assistants.editor.serviceAccount")}
          </label>
          <select
            id={field("service-account")}
            className="modal__input"
            value={draft.serviceAccountId}
            onChange={(e) => set("serviceAccountId", e.target.value)}
          >
            {mode === "create" ? <option value="">{t("assistants.editor.serviceAccountNew")}</option> : null}
            {draft.serviceAccountId && !options?.serviceAccounts.some((sa) => sa.id === draft.serviceAccountId) ? (
              <option value={draft.serviceAccountId}>{draft.serviceAccountId}</option>
            ) : null}
            {(options?.serviceAccounts ?? []).map((sa) => (
              <option key={sa.id} value={sa.id}>
                {sa.name}
                {sa.disabled_at ? t("assistants.editor.serviceAccountDisabled") : ""}
              </option>
            ))}
          </select>
          <p className="sec-edit__hint">{t("assistants.editor.serviceAccountHint")}</p>
        </div>

        <fieldset className="asst-editor__group">
          <legend className="modal__label">{t("assistants.editor.roster")}</legend>
          <p className="sec-edit__hint">{t("assistants.editor.rosterHint")}</p>
          {draft.roster.length === 0 ? <p className="sec-card__noitems">{t("assistants.editor.rosterEmpty")}</p> : null}
          <ul className="asst-roster">
            {draft.roster.map((entry, index) => (
              <li key={entry.key ?? index}>
                <RosterEntryEditor
                  entry={entry}
                  fieldId={field(`roster-${index}`)}
                  agents={options?.agents ?? []}
                  workflows={options?.workflows ?? []}
                  readOnly={readOnly}
                  onChange={(next) => setEntry(index, next)}
                  onRemove={() => set("roster", draft.roster.filter((_, i) => i !== index))}
                />
              </li>
            ))}
          </ul>
          {!readOnly ? (
            <div className="sec-card__actions">
              <Button
                size="compact"
                onClick={() => set("roster", [...draft.roster, newRosterEntry("agent")])}
              >
                {t("assistants.editor.addAgent")}
              </Button>
              <Button
                size="compact"
                onClick={() => set("roster", [...draft.roster, newRosterEntry("workflow")])}
              >
                {t("assistants.editor.addWorkflow")}
              </Button>
            </div>
          ) : null}
        </fieldset>

        <fieldset className="asst-editor__group">
          <legend className="modal__label">{t("assistants.editor.readableFiles")}</legend>
          <p className="sec-edit__hint">{t("assistants.editor.readableFilesHint")}</p>
          <ReadableFilesPicker
            artifacts={options?.artifacts ?? []}
            total={options?.artifactsTotal ?? 0}
            selected={draft.readableFiles}
            onChange={(next) => set("readableFiles", next)}
          />
        </fieldset>
      </fieldset>

      {localError || error ? (
        <p className="sec__error" role="alert">
          {localError ?? error}
        </p>
      ) : null}
      {!readOnly ? (
        <div className="sec-form__actions">
          {onCancel ? (
            <Button variant="secondary" onClick={onCancel} disabled={busy}>
              {t("assistants.editor.cancel")}
            </Button>
          ) : null}
          <Button type="submit" variant="primary" busy={busy}>
            {mode === "create" ? t("assistants.editor.create") : t("assistants.editor.save")}
          </Button>
        </div>
      ) : null}
    </form>
  )
}

function RosterEntryEditor({
  entry,
  fieldId,
  agents,
  workflows,
  readOnly,
  onChange,
  onRemove,
}: {
  entry: RosterDraft
  fieldId: string
  agents: ApiAgent[]
  workflows: ApiWorkflow[]
  readOnly: boolean
  onChange: (next: Partial<RosterDraft>) => void
  onRemove: () => void
}) {
  const t = useT()
  const isAgent = entry.kind === "agent"
  const choices = isAgent
    ? agents.map((a) => ({ id: a.id, name: a.name }))
    : workflows.map((w) => ({ id: w.id, name: w.name }))
  const kindLabel = isAgent ? t("assistants.roster.agent") : t("assistants.roster.workflow")

  // Releasable candidates come from the schema: the Agent entry's own, or the
  // Workflow's result node. When neither can be read, the names are typed.
  const candidates = useMemo(() => {
    if (isAgent) {
      const parsed = parseOutputSchema(entry.schemaText, t)
      return "error" in parsed ? { fields: null, error: entry.schemaText.trim() ? parsed.error : null } : { fields: parsed.properties, error: null }
    }
    const wf = workflows.find((w) => w.id === entry.id)
    return { fields: wf ? workflowResultProperties(wf.definition) : null, error: null }
  }, [isAgent, entry.schemaText, entry.id, workflows, t])

  const [fieldText, setFieldText] = useState(entry.releasable.join(", "))

  function toggle(name: string, on: boolean) {
    onChange({ releasable: on ? [...entry.releasable, name] : entry.releasable.filter((f) => f !== name) })
  }

  return (
    <div className="asst-roster__entry">
      <div className="asst-editor__row">
        <div className="sec-field">
          <label className="modal__label" htmlFor={`${fieldId}-id`}>
            {kindLabel}
          </label>
          <select
            id={`${fieldId}-id`}
            className="modal__input"
            value={entry.id}
            onChange={(e) => onChange({ id: e.target.value, releasable: [] })}
          >
            <option value="">{isAgent ? t("assistants.roster.chooseAgent") : t("assistants.roster.chooseWorkflow")}</option>
            {entry.id && !choices.some((c) => c.id === entry.id) ? <option value={entry.id}>{entry.id}</option> : null}
            {choices.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
          </select>
        </div>
        {!readOnly ? (
          <Button size="compact" variant="tertiary" className="asst-roster__remove" onClick={onRemove}>
            {isAgent ? t("assistants.roster.removeAgent") : t("assistants.roster.removeWorkflow")}
          </Button>
        ) : null}
      </div>

      {isAgent ? (
        <div className="sec-field">
          <label className="modal__label" htmlFor={`${fieldId}-schema`}>
            {t("assistants.roster.outputSchema")}
          </label>
          <textarea
            id={`${fieldId}-schema`}
            className="modal__textarea sec-json"
            rows={5}
            value={entry.schemaText}
            placeholder={'{"type": "object", "properties": {"answer": {"type": "string"}}}'}
            onChange={(e) => onChange({ schemaText: e.target.value })}
          />
          {candidates.error ? <p className="sec__error">{candidates.error}</p> : null}
        </div>
      ) : null}

      {candidates.fields ? (
        <fieldset className="asst-editor__checks">
          <legend className="modal__label">{t("assistants.roster.releasable")}</legend>
          {candidates.fields.length === 0 ? (
            <p className="sec-card__noitems">{t("assistants.roster.noProperties")}</p>
          ) : (
            candidates.fields.map((name) => (
              <label key={name} className="asst-check">
                <input
                  type="checkbox"
                  checked={entry.releasable.includes(name)}
                  onChange={(e) => toggle(name, e.target.checked)}
                />
                <code>{name}</code>
              </label>
            ))
          )}
        </fieldset>
      ) : !isAgent && entry.id ? (
        <div className="sec-field">
          <label className="modal__label" htmlFor={`${fieldId}-fields`}>
            {t("assistants.roster.releasableTyped")}
          </label>
          <input
            id={`${fieldId}-fields`}
            className="modal__input"
            value={fieldText}
            onChange={(e) => {
              setFieldText(e.target.value)
              onChange({ releasable: parseFieldList(e.target.value) })
            }}
          />
          <p className="sec-edit__hint">{t("assistants.roster.releasableTypedHint")}</p>
        </div>
      ) : null}
    </div>
  )
}

function ReadableFilesPicker({
  artifacts,
  total,
  selected,
  onChange,
}: {
  artifacts: ApiArtifact[]
  total: number
  selected: string[]
  onChange: (next: string[]) => void
}) {
  const t = useT()
  // A selected file beyond the loaded page still shows, so saving never drops it silently.
  const unknown = selected.filter((id) => !artifacts.some((a) => a.id === id))
  if (artifacts.length === 0 && unknown.length === 0) {
    return <p className="sec-card__noitems">{t("assistants.files.none")}</p>
  }
  const toggle = (id: string, on: boolean) => onChange(on ? [...selected, id] : selected.filter((x) => x !== id))
  return (
    <div className="asst-files">
      {unknown.map((id) => (
        <label key={id} className="asst-check">
          <input type="checkbox" checked onChange={(e) => toggle(id, e.target.checked)} />
          <code>{id}</code>
        </label>
      ))}
      {artifacts.map((a) => (
        <label key={a.id} className="asst-check">
          <input type="checkbox" checked={selected.includes(a.id)} onChange={(e) => toggle(a.id, e.target.checked)} />
          <span>{a.title || a.filename}</span>
        </label>
      ))}
      {total > artifacts.length ? (
        <p className="sec-edit__hint">{t("assistants.files.showing", { shown: artifacts.length, total })}</p>
      ) : null}
    </div>
  )
}
