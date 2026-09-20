import { useMemo, useState } from "react"
import { Button } from "@buildmax/gui"
import { getErrorMessage } from "../../lib/errorMessage"
import { WorkflowRunInputForm } from "../workflows/RunInputForm"
import { buildInputValue, parseInputSchema, type InputFormValues } from "../workflows/runInput"
import { createSchedule } from "./api"

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
  onCreated: () => Promise<void>
  onCancel: () => void
}

// The single create form for a schedule, shared by the agent-detail section, the
// workflow-detail section, and the space-wide overview. The executor is either
// pinned by the host page or chosen from executors; exactly one is provided.
export function CreateScheduleForm({ token, spaceId, pinned, executors, onCreated, onCancel }: CreateScheduleFormProps) {
  const [selectedId, setSelectedId] = useState(pinned?.id ?? executors?.[0]?.id ?? "")
  const [name, setName] = useState("")
  const [prompt, setPrompt] = useState("")
  const [inputValues, setInputValues] = useState<InputFormValues>({})
  const [cronExpr, setCronExpr] = useState("")
  const [timezone, setTimezone] = useState("UTC")
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  const nothingToSchedule = executors !== undefined && executors.length === 0
  const executor = useMemo<ScheduleExecutorOption | undefined>(
    () => pinned ?? executors?.find((e) => e.id === selectedId),
    [pinned, executors, selectedId],
  )
  // A workflow's input_schema drives a generated form; null means it takes no input.
  const inputSchema = useMemo(
    () => (executor?.kind === "workflow" && executor.definition ? parseInputSchema(executor.definition) : null),
    [executor],
  )

  async function submit() {
    if (!executor) {
      setErr("Choose something to run.")
      return
    }
    if (!cronExpr.trim() || !timezone.trim()) {
      setErr("A cron expression and timezone are required.")
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
        setErr("A prompt is required.")
        return
      }
      input = prompt
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
        },
        token,
      )
      await onCreated()
    } catch (e) {
      setErr(getErrorMessage(e, "Failed to create schedule"))
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
          Runs
          {nothingToSchedule ? (
            <span className="agent-schedules__hint">Create an agent or publish a workflow first, then schedule it.</span>
          ) : (
            <select className="agent-schedules__input" value={selectedId} onChange={(e) => setSelectedId(e.target.value)}>
              {executors.map((e) => (
                <option key={`${e.kind}:${e.id}`} value={e.id}>
                  {e.name} ({e.kind})
                </option>
              ))}
            </select>
          )}
        </label>
      ) : null}
      <label className="agent-schedules__label">
        Name (optional)
        <input className="agent-schedules__input" value={name} onChange={(e) => setName(e.target.value)} placeholder="Nightly summary" />
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
          <p className="agent-schedules__hint">This workflow takes no input; each firing starts a run with none.</p>
        )
      ) : (
        <label className="agent-schedules__label">
          Prompt
          <textarea className="agent-schedules__input" value={prompt} onChange={(e) => setPrompt(e.target.value)} rows={3} placeholder="Summarize the new issues" />
        </label>
      )}
      <label className="agent-schedules__label">
        Cron expression
        <input className="agent-schedules__input" value={cronExpr} onChange={(e) => setCronExpr(e.target.value)} placeholder="0 9 * * *" />
        <span className="agent-schedules__hint">Five fields: minute hour day-of-month month day-of-week. Example: 0 9 * * * is 09:00 daily.</span>
      </label>
      <label className="agent-schedules__label">
        Timezone
        <input className="agent-schedules__input" value={timezone} onChange={(e) => setTimezone(e.target.value)} placeholder="Asia/Shanghai" />
        <span className="agent-schedules__hint">An IANA timezone name; the cron time is read in it.</span>
      </label>
      {err ? <p className="agent-schedules__error" role="alert">{err}</p> : null}
      <div className="agent-schedules__form-actions">
        <Button type="submit" variant="primary" busy={busy} disabled={nothingToSchedule}>
          Create schedule
        </Button>
        <Button variant="secondary" onClick={onCancel} disabled={busy}>
          Cancel
        </Button>
      </div>
    </form>
  )
}
