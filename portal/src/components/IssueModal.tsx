import { useState } from "react"
import { BaseModal, Button } from "@buildmax/gui"
import type { ApiSpaceMember } from "../lib/api/types"
import { peopleOnly } from "../features/spaces/api"
import { ExecutorField } from "../features/issues"
import type { Agent, Issue, Workflow } from "../lib/types"
import type { PermissionState } from "../state/permissionState"
import { useT } from "../i18n"
import { useStatusLabel } from "../lib/statusLabels"
import { CreateAgentModal } from "./CreateAgentModal"

interface IssueModalProps {
  open: boolean
  token: string | null
  spaceId: string
  agents: Agent[]
  workflows: Workflow[]
  members: ApiSpaceMember[]
  userId?: string
  loading: boolean
  /** Whether the reader may create Agents and assign Workflows. */
  manage: PermissionState
  error: string | null
  onClose: () => void
  /** An Agent created from this dialog, for the caller's list. */
  onAgentCreated: (agent: Agent) => void
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
  token,
  spaceId,
  agents,
  workflows,
  members,
  userId,
  loading,
  manage,
  error,
  onClose,
  onAgentCreated,
  onSubmit,
}: IssueModalProps) {
  const t = useT()
  const statusLabel = useStatusLabel()
  const [title, setTitle] = useState("")
  const [description, setDescription] = useState("")
  const [status, setStatus] = useState<Issue["status"]>("todo")
  const [ownerValue, setOwnerValue] = useState("")
  const [executorValue, setExecutorValue] = useState("")
  // Creating an Agent swaps this dialog for the Agent one and back, so the
  // draft survives and the new Agent becomes its executor.
  const [creatingAgent, setCreatingAgent] = useState(false)

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
    <>
      <BaseModal
        open={open && !creatingAgent}
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
              <ExecutorField
                value={executorValue}
                onChange={setExecutorValue}
                agents={agents}
                workflows={workflows}
                manage={manage}
                onCreateAgent={() => setCreatingAgent(true)}
              />
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
      <CreateAgentModal
        open={open && creatingAgent}
        token={token}
        spaceId={spaceId}
        onClose={() => setCreatingAgent(false)}
        onCreated={(agent) => {
          onAgentCreated(agent)
          setExecutorValue(`agent:${agent.id}`)
          setCreatingAgent(false)
        }}
      />
    </>
  )
}
