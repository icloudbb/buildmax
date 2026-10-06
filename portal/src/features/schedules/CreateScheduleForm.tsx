import { useEffect, useMemo, useState } from "react"
import { Button } from "@buildmax/gui"
import type { ApiAssistant, ApiAssistantRequester } from "../../lib/api/types"
import { getErrorMessage } from "../../lib/errorMessage"
import { listAssistantRequesters, listAssistants } from "../assistants"
import { WorkflowRunInputForm } from "../workflows/RunInputForm"
import { buildInputValue, parseInputSchema, type InputFormValues } from "../workflows/runInput"
import { createSchedule } from "./api"
import { deliveringAssistants } from "./delivery"
import { useStableT, useT } from "../../i18n"

// ScheduleExecutorOption is one thing a schedule can fire: an agent (its input is
// a prompt) or a workflow (its input is the run input its input_schema declares,
// carried here as the workflow's definition JSON so the form can render it).
export interface ScheduleExecutorOption {
  kind: "agent" | "workflow"
  id: string
  name: string
  definition?: string
}

interface CreateScheduleFormProps {
  token: string
  spaceId: string
  // Fixed executor: a detail page pins its own agent or workflow, so no picker shows.
  pinned?: ScheduleExecutorOption
  // Selectable executors: the space overview lets a member choose what runs. An
  // empty array means the space has nothing to schedule yet.
  executors?: ScheduleExecutorOption[]
  // Owners and admins may send each result to a person through a Space
  // Assistant, which speaks for the Space.
  canDeliver?: boolean
  onCreated: () => Promise<void>
  onCancel: () => void
}

// The single create form for a schedule, shared by the agent-detail section, the
// workflow-detail section, and the space-wide overview. The executor is either
// pinned by the host page or chosen from executors; exactly one is provided.
export function CreateScheduleForm({ token, spaceId, pinned, executors, canDeliver, onCreated, onCancel }: CreateScheduleFormProps) {
  const t = useT()
  const stableT = useStableT()
  const [selectedId, setSelectedId] = useState(pinned?.id ?? executors?.[0]?.id ?? "")
  const [name, setName] = useState("")
  const [prompt, setPrompt] = useState("")
  const [inputValues, setInputValues] = useState<InputFormValues>({})
  const [cronExpr, setCronExpr] = useState("")
  const [timezone, setTimezone] = useState("UTC")
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const [assistants, setAssistants] = useState<ApiAssistant[]>([])
  const [deliveryAssistantId, setDeliveryAssistantId] = useState("")
  const [requesters, setRequesters] = useState<ApiAssistantRequester[] | null>(null)
  const [requesterId, setRequesterId] = useState("")

  // Delivery is optional, so a failed lookup only hides it.
  useEffect(() => {
    if (!canDeliver) return
    let cancelled = false
    listAssistants(spaceId, token)
      .then((list) => {
        if (!cancelled) setAssistants(list)
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [canDeliver, spaceId, token])

  useEffect(() => {
    setRequesters(null)
    setRequesterId("")
    if (!deliveryAssistantId) return
    let cancelled = false
    listAssistantRequesters(spaceId, deliveryAssistantId, token)
      .then((list) => {
        if (cancelled) return
        setRequesters(list)
        setRequesterId(list[0]?.user_id ?? "")
      })
      .catch((e) => {
        if (!cancelled) setErr(getErrorMessage(e, stableT("schedules.form.error.loadRequesters")))
      })
    return () => {
      cancelled = true
    }
  }, [deliveryAssistantId, spaceId, token, stableT])

  const nothingToSchedule = executors !== undefined && executors.length === 0
  const executor = useMemo<ScheduleExecutorOption | undefined>(
    () => pinned ?? executors?.find((e) => e.id === selectedId),
    [pinned, executors, selectedId],
  )
  const deliveryOptions = useMemo(
    () => (executor ? deliveringAssistants(assistants, executor.kind, executor.id) : []),
    [assistants, executor],
  )
  // A choice made for another executor in the picker no longer applies.
  const deliveryVia = deliveryOptions.some((a) => a.id === deliveryAssistantId) ? deliveryAssistantId : ""
  // A workflow's input_schema drives a generated form; null means it takes no input.
  const inputSchema = useMemo(
    () => (executor?.kind === "workflow" && executor.definition ? parseInputSchema(executor.definition) : null),
    [executor],
  )

  async function submit() {
    if (!executor) {
      setErr(t("schedules.form.error.chooseExecutor"))
      return
    }
    if (!cronExpr.trim() || !timezone.trim()) {
      setErr(t("schedules.form.error.cronRequired"))
      return
    }
    let input: string
    if (executor.kind === "workflow") {
      if (inputSchema && inputSchema.fields.length > 0) {
        const { value, errors } = buildInputValue(inputSchema.fields, inputValues)
        if (errors.length > 0) {
          setErr(errors.join(" "))
          return
        }
        input = JSON.stringify(value)
      } else {
        input = ""
      }
    } else {
      if (!prompt.trim()) {
        setErr(t("schedules.form.error.promptRequired"))
        return
      }
      input = prompt
    }
    if (deliveryVia && !requesterId) {
      setErr(t("schedules.form.error.chooseRecipient"))
      return
    }
    setBusy(true)
    setErr(null)
    try {
      await createSchedule(
        spaceId,
        {
          executor_kind: executor.kind,
          executor_id: executor.id,
          name: name.trim(),
          input,
          cron_expr: cronExpr.trim(),
          timezone: timezone.trim(),
          ...(deliveryVia ? { delivery: { assistant_id: deliveryVia, requester_id: requesterId } } : {}),
        },
        token,
      )
      await onCreated()
    } catch (e) {
      setErr(getErrorMessage(e, t("schedules.error.create")))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form
      className="agent-schedules__form"
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
    >
      {executors ? (
        <label className="agent-schedules__label">
          {t("schedules.form.runs")}
          {nothingToSchedule ? (
            <span className="agent-schedules__hint">{t("schedules.form.nothing")}</span>
          ) : (
            <select className="agent-schedules__input" value={selectedId} onChange={(e) => setSelectedId(e.target.value)}>
              {executors.map((e) => (
                <option key={`${e.kind}:${e.id}`} value={e.id}>
                  {t("schedules.form.executorOption", { name: e.name, kind: t(`schedules.kind.${e.kind}`) })}
                </option>
              ))}
            </select>
          )}
        </label>
      ) : null}
      <label className="agent-schedules__label">
        {t("schedules.form.name")}
        <input className="agent-schedules__input" value={name} onChange={(e) => setName(e.target.value)} placeholder={t("schedules.form.namePlaceholder")} />
      </label>
      {executor?.kind === "workflow" ? (
        inputSchema && inputSchema.fields.length > 0 ? (
          <WorkflowRunInputForm
            fields={inputSchema.fields}
            values={inputValues}
            disabled={busy}
            onChange={(fieldName, value) => setInputValues((prev) => ({ ...prev, [fieldName]: value }))}
          />
        ) : (
          <p className="agent-schedules__hint">{t("schedules.form.noInput")}</p>
        )
      ) : (
        <label className="agent-schedules__label">
          {t("schedules.form.prompt")}
          <textarea className="agent-schedules__input" value={prompt} onChange={(e) => setPrompt(e.target.value)} rows={3} placeholder={t("schedules.form.promptPlaceholder")} />
        </label>
      )}
      <label className="agent-schedules__label">
        {t("schedules.form.cron")}
        <input className="agent-schedules__input" value={cronExpr} onChange={(e) => setCronExpr(e.target.value)} placeholder="0 9 * * *" />
        <span className="agent-schedules__hint">{t("schedules.form.cronHint")}</span>
      </label>
      <label className="agent-schedules__label">
        {t("schedules.form.timezone")}
        <input className="agent-schedules__input" value={timezone} onChange={(e) => setTimezone(e.target.value)} placeholder="Asia/Shanghai" />
        <span className="agent-schedules__hint">{t("schedules.form.timezoneHint")}</span>
      </label>
      {canDeliver && deliveryOptions.length > 0 ? (
        <fieldset className="agent-schedules__label">
          <legend>{t("schedules.form.deliver")}</legend>
          <label className="agent-schedules__label">
            {t("schedules.form.through")}
            <select
              className="agent-schedules__input"
              value={deliveryVia}
              onChange={(e) => setDeliveryAssistantId(e.target.value)}
            >
              <option value="">{t("schedules.form.dontSend")}</option>
              {deliveryOptions.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.name}
                </option>
              ))}
            </select>
          </label>
          {deliveryVia ? (
            requesters === null ? (
              <span className="agent-schedules__hint">{t("shell.loading")}</span>
            ) : requesters.length === 0 ? (
              <span className="agent-schedules__hint">{t("schedules.form.noRequesters")}</span>
            ) : (
              <label className="agent-schedules__label">
                {t("schedules.form.to")}
                <select className="agent-schedules__input" value={requesterId} onChange={(e) => setRequesterId(e.target.value)}>
                  {requesters.map((r) => (
                    <option key={r.user_id} value={r.user_id}>
                      {r.name || t("schedules.form.user", { id: r.user_id.slice(0, 8) })}
                    </option>
                  ))}
                </select>
              </label>
            )
          ) : null}
          <span className="agent-schedules__hint">{t("schedules.form.deliverHint")}</span>
        </fieldset>
      ) : null}
      {err ? <p className="agent-schedules__error" role="alert">{err}</p> : null}
      <div className="agent-schedules__form-actions">
        <Button type="submit" variant="primary" busy={busy} disabled={nothingToSchedule}>
          {t("schedules.form.create")}
        </Button>
        <Button variant="secondary" onClick={onCancel} disabled={busy}>
          {t("schedules.form.cancel")}
        </Button>
      </div>
    </form>
  )
}
