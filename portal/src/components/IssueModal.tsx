import { useEffect, useState } from "react"
import { BaseModal, Button } from "@buildmax/gui"
import type { ApiSpaceMember } from "../lib/api/types"
import type { Agent, Issue, Workflow } from "../lib/types"

interface IssueModalProps {
  open: boolean
  agents: Agent[]
  workflows: Workflow[]
  members: ApiSpaceMember[]
  userId?: string
  loading: boolean
  allowWorkflowAssignment?: boolean
  error: string | null
  onClose: () => void
  onSubmit: (values: {
    title: string
    description: string
    status: Issue["status"]
    owner_id: string
    executor_kind: "agent" | "workflow" | ""
    executor_id: string
  }) => void
}

/** Creates a new Issue. Editing an existing one happens on its own detail
 *  page (`IssueDetail`), which needs Discussion, Results, and Runs alongside
 *  the same fields -- there is no reason to fit all of that into one modal. */
export function IssueModal({
  open,
  agents,
  workflows,
  members,
  userId,
  loading,
  allowWorkflowAssignment = true,
  error,
  onClose,
  onSubmit,
}: IssueModalProps) {
  const [title, setTitle] = useState("")
  const [description, setDescription] = useState("")
  const [status, setStatus] = useState<Issue["status"]>("todo")
  const [ownerValue, setOwnerValue] = useState("")
  const [executorValue, setExecutorValue] = useState("")
  const selectedWorkflowId = executorValue.startsWith("workflow:") ? executorValue.slice("workflow:".length) : ""
  const selectableWorkflows = workflows.filter(
    (workflow) => workflow.status === "published" || workflow.id === selectedWorkflowId,
  )

  useEffect(() => {
    if (!open) return
    setTitle("")
    setDescription("")
    setStatus("todo")
    setOwnerValue("")
    setExecutorValue("")
  }, [open])

  function memberLabel(member: ApiSpaceMember): string {
    if (member.user_id === userId) return "Me"
    if (member.user_name && member.user_name.trim() !== "") return member.user_name
    if (member.user_email && member.user_email.trim() !== "") return member.user_email
    return `Member ${member.user_id.slice(0, 8)}`
  }

  return (
    <BaseModal
      open={open}
      title="New Issue"
      titleId="issue-modal-title"
      onClose={onClose}
      className="modal--large"
    >
      <div className="modal__body">
        <div className="issues-page__form">
          <label className="issues-page__field">
            <span className="issues-page__field-label">Title</span>
            <input className="issues-page__input" value={title} onChange={(e) => setTitle(e.target.value)} placeholder="What needs to be done?" />
          </label>
          <label className="issues-page__field">
            <span className="issues-page__field-label">Description</span>
            <textarea className="issues-page__textarea" rows={6} value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Add more context" />
          </label>
          <label className="issues-page__field">
            <span className="issues-page__field-label">Status</span>
            <select className="issues-page__select" value={status} onChange={(e) => setStatus(e.target.value as Issue["status"])}>
              <option value="todo">To do</option>
              <option value="in_progress">In progress</option>
              <option value="done">Done</option>
            </select>
          </label>
          <div className="issue-detail-page__split">
            <label className="issues-page__field">
              <span className="issues-page__field-label">Owner</span>
              <select className="issues-page__select" value={ownerValue} onChange={(e) => setOwnerValue(e.target.value)}>
                <option value="">Unassigned</option>
                {members.map((member) => (
                  <option key={member.user_id} value={member.user_id}>
                    {memberLabel(member)}
                  </option>
                ))}
              </select>
              <span className="issues-page__field-label">Who is accountable for this issue.</span>
            </label>
            <label className="issues-page__field">
              <span className="issues-page__field-label">Executor</span>
              <select className="issues-page__select" value={executorValue} onChange={(e) => setExecutorValue(e.target.value)}>
                <option value="">None</option>
                {agents.map((agent) => (
                  <option key={agent.id} value={`agent:${agent.id}`}>{agent.name}</option>
                ))}
                {allowWorkflowAssignment
                  ? selectableWorkflows.map((workflow) => (
                      <option key={workflow.id} value={`workflow:${workflow.id}`}>
                        {workflow.name}{workflow.status !== "published" ? ` (${workflow.status})` : ""}
                      </option>
                    ))
                  : null}
              </select>
              <span className="issues-page__field-label">
                {allowWorkflowAssignment
                  ? "What runs the work. Only `published` workflows are available."
                  : "What runs the work. Workflow assignment is limited to space owners and admins."}
              </span>
            </label>
          </div>
          {error ? (
            <p className="modal__error" role="alert">
              {error}
            </p>
          ) : null}
          <div className="modal__actions">
            <Button variant="secondary" onClick={onClose} disabled={loading}>
              Cancel
            </Button>
            <Button
              variant="primary"
              busy={loading}
              disabled={!title.trim()}
              onClick={() => {
                const [executorKind, executorID] = executorValue ? executorValue.split(":") : ["", ""]
                onSubmit({
                  title: title.trim(),
                  description,
                  status,
                  owner_id: ownerValue,
                  executor_kind: (executorKind as "agent" | "workflow" | "") || "",
                  executor_id: executorID || "",
                })
              }}
            >
              Create issue
            </Button>
          </div>
        </div>
      </div>
    </BaseModal>
  )
}
