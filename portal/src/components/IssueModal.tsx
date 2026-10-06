import { useState } from "react"
import { BaseModal, Button } from "@buildmax/gui"
import type { ApiSpaceMember } from "../lib/api/types"
import { peopleOnly } from "../features/spaces/api"
import type { Agent, Issue, Workflow } from "../lib/types"
import { useT } from "../i18n"
import { useStatusLabel } from "../lib/statusLabels"

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
  const t = useT()
  const statusLabel = useStatusLabel()
  const [title, setTitle] = useState("")
  const [description, setDescription] = useState("")
  const [status, setStatus] = useState<Issue["status"]>("todo")
  const [ownerValue, setOwnerValue] = useState("")
  const [executorValue, setExecutorValue] = useState("")
  const selectedWorkflowId = executorValue.startsWith("workflow:") ? executorValue.slice("workflow:".length) : ""
  const selectableWorkflows = workflows.filter(
    (workflow) => workflow.status === "published" || workflow.id === selectedWorkflowId,
  )

  // Dismissing the dialog (Escape, the close button, the backdrop) keeps the
  // draft, so a stray key does not lose what was typed; Cancel is the explicit
  // discard. A created issue navigates away, which unmounts the draft.
  function discard() {
    setTitle("")
    setDescription("")
    setStatus("todo")
    setOwnerValue("")
    setExecutorValue("")
    onClose()
  }

  function memberLabel(member: ApiSpaceMember): string {
    if (member.user_id === userId) return t("issues.me")
    if (member.user_name && member.user_name.trim() !== "") return member.user_name
    if (member.user_email && member.user_email.trim() !== "") return member.user_email
    return t("issues.memberId", { id: member.user_id.slice(0, 8) })
  }

  return (
    <BaseModal
      open={open}
      title={t("issues.new")}
      titleId="issue-modal-title"
      onClose={onClose}
      className="modal--large"
    >
      <div className="modal__body">
        <div className="issues-page__form">
          <label className="issues-page__field">
            <span className="issues-page__field-label">{t("issues.field.title")}</span>
            <input
              className="issues-page__input"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder={t("issues.create.titlePlaceholder")}
            />
          </label>
          <label className="issues-page__field">
            <span className="issues-page__field-label">{t("issues.field.description")}</span>
            <textarea
              className="issues-page__textarea"
              rows={6}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t("issues.create.descriptionPlaceholder")}
            />
          </label>
          <label className="issues-page__field">
            <span className="issues-page__field-label">{t("issues.field.status")}</span>
            <select className="issues-page__select" value={status} onChange={(e) => setStatus(e.target.value as Issue["status"])}>
              <option value="todo">{statusLabel("todo")}</option>
              <option value="in_progress">{statusLabel("in_progress")}</option>
              <option value="done">{statusLabel("done")}</option>
            </select>
          </label>
          <div className="issue-detail-page__split">
            <label className="issues-page__field">
              <span className="issues-page__field-label">{t("issues.field.owner")}</span>
              <select className="issues-page__select" value={ownerValue} onChange={(e) => setOwnerValue(e.target.value)}>
                <option value="">{t("issues.unassigned")}</option>
                {peopleOnly(members).map((member) => (
                  <option key={member.user_id} value={member.user_id}>
                    {memberLabel(member)}
                  </option>
                ))}
              </select>
              <span className="issues-page__field-label">{t("issues.field.ownerHint")}</span>
            </label>
            <label className="issues-page__field">
              <span className="issues-page__field-label">{t("issues.field.executor")}</span>
              <select className="issues-page__select" value={executorValue} onChange={(e) => setExecutorValue(e.target.value)}>
                <option value="">{t("issues.none")}</option>
                {agents.map((agent) => (
                  <option key={agent.id} value={`agent:${agent.id}`}>{agent.name}</option>
                ))}
                {allowWorkflowAssignment
                  ? selectableWorkflows.map((workflow) => (
                      <option key={workflow.id} value={`workflow:${workflow.id}`}>
                        {workflow.status !== "published"
                          ? t("issues.workflowWithStatus", { name: workflow.name, status: statusLabel(workflow.status).toLowerCase() })
                          : workflow.name}
                      </option>
                    ))
                  : null}
              </select>
              <span className="issues-page__field-label">
                {allowWorkflowAssignment
                  ? t("issues.field.executorHint")
                  : t("issues.field.executorHintRestricted")}
              </span>
            </label>
          </div>
          {error ? (
            <p className="modal__error" role="alert">
              {error}
            </p>
          ) : null}
          <div className="modal__actions">
            <Button variant="secondary" onClick={discard} disabled={loading}>
              {t("issues.cancel")}
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
              {t("issues.create.submit")}
            </Button>
          </div>
        </div>
      </div>
    </BaseModal>
  )
}
