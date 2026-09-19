import { useState } from "react"
import { Button } from "@buildmax/gui"
import { getErrorMessage } from "../../lib/errorMessage"
import { createSchedule } from "./api"

export interface ScheduleAgentOption {
  id: string
  name: string
}

interface CreateScheduleFormProps {
  token: string
  spaceId: string
  // Fixed executor: the agent-detail page pins its own agent, so no picker shows.
  agentId?: string
  // Selectable executors: the space overview lets a member choose which agent runs.
  // An empty array means the space has no agents yet, so scheduling is blocked.
  agents?: ScheduleAgentOption[]
  onCreated: () => Promise<void>
  onCancel: () => void
}

// The single create/edit form for a schedule, shared by the agent-detail section
// and the space-wide overview. The executor is either pinned by the host page
// (agentId) or chosen from agents; exactly one of the two is provided.
export function CreateScheduleForm({ token, spaceId, agentId, agents, onCreated, onCancel }: CreateScheduleFormProps) {
  const [selectedAgent, setSelectedAgent] = useState(agents?.[0]?.id ?? "")
  const [name, setName] = useState("")
  const [input, setInput] = useState("")
  const [cronExpr, setCronExpr] = useState("")
  const [timezone, setTimezone] = useState("UTC")
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  const noAgents = agents !== undefined && agents.length === 0
  const effectiveAgentId = agentId ?? selectedAgent

  async function submit() {
    if (!effectiveAgentId) {
      setErr("Choose an agent to run.")
      return
    }
    if (!input.trim() || !cronExpr.trim() || !timezone.trim()) {
      setErr("Input, cron expression, and timezone are required.")
      return
    }
    setBusy(true)
    setErr(null)
    try {
      await createSchedule(
        spaceId,
        { agent_id: effectiveAgentId, name: name.trim(), input, cron_expr: cronExpr.trim(), timezone: timezone.trim() },
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
      {agents ? (
        <label className="agent-schedules__label">
          Agent
          {noAgents ? (
            <span className="agent-schedules__hint">Create an agent first, then schedule it.</span>
          ) : (
            <select className="agent-schedules__input" value={selectedAgent} onChange={(e) => setSelectedAgent(e.target.value)}>
              {agents.map((a) => (
                <option key={a.id} value={a.id}>{a.name}</option>
              ))}
            </select>
          )}
        </label>
      ) : null}
      <label className="agent-schedules__label">
        Name (optional)
        <input className="agent-schedules__input" value={name} onChange={(e) => setName(e.target.value)} placeholder="Nightly summary" />
      </label>
      <label className="agent-schedules__label">
        Prompt
        <textarea className="agent-schedules__input" value={input} onChange={(e) => setInput(e.target.value)} rows={3} placeholder="Summarize the new issues" />
      </label>
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
        <Button type="submit" variant="primary" busy={busy} disabled={noAgents}>
          Create schedule
        </Button>
        <Button variant="secondary" onClick={onCancel} disabled={busy}>
          Cancel
        </Button>
      </div>
    </form>
  )
}
