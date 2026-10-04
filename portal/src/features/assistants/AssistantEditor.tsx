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
  failed: string[]
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
      const failed: string[] = []
      const value = <T,>(r: PromiseSettledResult<T>, what: string, fallback: T): T => {
        if (r.status === "fulfilled") return r.value
        failed.push(what)
        return fallback
      }
      const artifactList = value(artifacts, "files", { items: [], total: 0 })
      setOptions({
        agents: value(agents, "agents", []),
        // Only a published Workflow can be on a roster.
        workflows: value(workflows, "workflows", { workflows: [] }).workflows.filter((w) => w.status === "published"),
        artifacts: artifactList.items,
        artifactsTotal: artifactList.total,
        models: value(models, "models", []),
        serviceAccounts: value(accounts, "service accounts", []),
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
  const [draft, setDraft] = useState<AssistantDraft>(initial)
  const [localError, setLocalError] = useState<string | null>(null)
  const options = useEditorOptions(spaceId, token)
  const ids = useId()

  const set = <K extends keyof AssistantDraft>(key: K, value: AssistantDraft[K]) =>
    setDraft((prev) => ({ ...prev, [key]: value }))

  const setEntry = (index: number, next: Partial<RosterDraft>) =>
    setDraft((prev) => ({ ...prev, roster: prev.roster.map((e, i) => (i === index ? { ...e, ...next } : e)) }))

  function submit() {
    const built = draftToDefinition(draft)
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
      aria-label={mode === "create" ? "New assistant" : "Assistant definition"}
      onSubmit={(e) => {
        e.preventDefault()
        submit()
      }}
    >
      <div className="sec-form__head">
        <h3 className="sec-form__title">{mode === "create" ? "New assistant" : "Definition"}</h3>
      </div>
      {options && options.failed.length > 0 ? (
        <p className="sec__error" role="alert">
          Could not load {options.failed.join(", ")}; those choices are missing below.
        </p>
      ) : null}

      <fieldset className="asst-editor__fields" disabled={readOnly || busy}>
        <div className="sec-field">
          <label className="modal__label" htmlFor={field("name")}>
            Name
          </label>
          <input
            id={field("name")}
            className="modal__input"
            value={draft.name}
            maxLength={255}
            placeholder="HR help desk"
            onChange={(e) => set("name", e.target.value)}
          />
        </div>
        <div className="sec-field">
          <label className="modal__label" htmlFor={field("description")}>
            Description <span className="sec-field__optional">(optional)</span>
          </label>
          <textarea
            id={field("description")}
            className="modal__textarea"
            rows={2}
            maxLength={2000}
            value={draft.description}
            placeholder="What requesters can ask it about"
            onChange={(e) => set("description", e.target.value)}
          />
        </div>
        <div className="sec-field">
          <label className="modal__label" htmlFor={field("instructions")}>
            Instructions <span className="sec-field__optional">(optional)</span>
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
              Model
            </label>
            <select
              id={field("model")}
              className="modal__input"
              value={draft.model}
              onChange={(e) => set("model", e.target.value)}
            >
              <option value="">Deployment default</option>
              {modelOptions.map((m) => (
                <option key={m} value={m}>
                  {m}
                </option>
              ))}
            </select>
          </div>
          <div className="sec-field">
            <label className="modal__label" htmlFor={field("audience")}>
              Who can ask
            </label>
            <select
              id={field("audience")}
              className="modal__input"
              value={draft.audience}
              onChange={(e) => set("audience", e.target.value as AssistantDraft["audience"])}
            >
              <option value="space_members">Members of this space</option>
              <option value="all_users">Every active user of this deployment</option>
            </select>
          </div>
        </div>

        <div className="sec-field">
          <label className="modal__label" htmlFor={field("service-account")}>
            Service account
          </label>
          <select
            id={field("service-account")}
            className="modal__input"
            value={draft.serviceAccountId}
            onChange={(e) => set("serviceAccountId", e.target.value)}
          >
            {mode === "create" ? <option value="">Create one with this assistant&apos;s name</option> : null}
            {draft.serviceAccountId && !options?.serviceAccounts.some((sa) => sa.id === draft.serviceAccountId) ? (
              <option value={draft.serviceAccountId}>{draft.serviceAccountId}</option>
            ) : null}
            {(options?.serviceAccounts ?? []).map((sa) => (
              <option key={sa.id} value={sa.id}>
                {sa.name}
                {sa.disabled_at ? " (disabled)" : ""}
              </option>
            ))}
          </select>
          <p className="sec-edit__hint">The authority its work runs as, instead of any one person&apos;s.</p>
        </div>

        <fieldset className="asst-editor__group">
          <legend className="modal__label">Roster</legend>
          <p className="sec-edit__hint">
            The Agents and published Workflows it may run. Each declares a result shape and which of its top-level
            fields may reach a requester; nothing else from a run does.
          </p>
          {draft.roster.length === 0 ? <p className="sec-card__noitems">It runs nothing; it can only answer from its files.</p> : null}
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
                Add agent
              </Button>
              <Button
                size="compact"
                onClick={() => set("roster", [...draft.roster, newRosterEntry("workflow")])}
              >
                Add workflow
              </Button>
            </div>
          ) : null}
        </fieldset>

        <fieldset className="asst-editor__group">
          <legend className="modal__label">Readable files</legend>
          <p className="sec-edit__hint">Space files it may read to answer. Anything it can read, anyone who can ask may learn.</p>
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
              Cancel
            </Button>
          ) : null}
          <Button type="submit" variant="primary" busy={busy}>
            {mode === "create" ? "Create assistant" : "Save changes"}
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
  const isAgent = entry.kind === "agent"
  const choices = isAgent
    ? agents.map((a) => ({ id: a.id, name: a.name }))
    : workflows.map((w) => ({ id: w.id, name: w.name }))
  const kindLabel = isAgent ? "Agent" : "Workflow"

  // Releasable candidates come from the schema: the Agent entry's own, or the
  // Workflow's result node. When neither can be read, the names are typed.
  const candidates = useMemo(() => {
    if (isAgent) {
      const parsed = parseOutputSchema(entry.schemaText)
      return "error" in parsed ? { fields: null, error: entry.schemaText.trim() ? parsed.error : null } : { fields: parsed.properties, error: null }
    }
    const wf = workflows.find((w) => w.id === entry.id)
    return { fields: wf ? workflowResultProperties(wf.definition) : null, error: null }
  }, [isAgent, entry.schemaText, entry.id, workflows])

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
            <option value="">Choose {isAgent ? "an agent" : "a published workflow"}</option>
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
            Remove {kindLabel.toLowerCase()}
          </Button>
        ) : null}
      </div>

      {isAgent ? (
        <div className="sec-field">
          <label className="modal__label" htmlFor={`${fieldId}-schema`}>
            Output schema (JSON)
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
          <legend className="modal__label">Releasable fields</legend>
          {candidates.fields.length === 0 ? (
            <p className="sec-card__noitems">The result schema has no top-level properties.</p>
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
            Releasable fields (comma-separated)
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
          <p className="sec-edit__hint">Top-level properties of the result node&apos;s output schema.</p>
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
  // A selected file beyond the loaded page still shows, so saving never drops it silently.
  const unknown = selected.filter((id) => !artifacts.some((a) => a.id === id))
  if (artifacts.length === 0 && unknown.length === 0) {
    return <p className="sec-card__noitems">This space has no files to choose from.</p>
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
        <p className="sec-edit__hint">Showing the newest {artifacts.length} of {total} files.</p>
      ) : null}
    </div>
  )
}
