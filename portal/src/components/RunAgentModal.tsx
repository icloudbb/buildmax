import { useState, useEffect } from "react"
import type { Agent } from "../lib/types"
import { BaseModal, Button } from "@buildmax/gui"
import { useT } from "../i18n"

interface RunAgentModalProps {
  open: boolean
  agent: Agent | null
  loading: boolean
  error: string | null
  onClose: () => void
  onStart: (input: string) => void
}

export function RunAgentModal({
  open,
  agent,
  loading,
  error,
  onClose,
  onStart,
}: RunAgentModalProps) {
  const t = useT()
  const [input, setInput] = useState("")

  useEffect(() => {
    if (open && agent) {
      setInput("")
    }
  }, [open, agent])

  function handleSubmit() {
    onStart(input.trim())
  }

  if (!agent) return null

  return (
    <BaseModal
      open={open}
      title={t("agents.runNamed", { name: agent.name })}
      titleId="run-agent-modal-title"
      onClose={onClose}
      className="modal--large"
    >
      <div className="modal__body">
        <p className="modal__hint" id="run-agent-modal-hint">
          {t("agents.runModal.hint")}
        </p>
        <label className="modal__label" htmlFor="run-agent-modal-input">
          {t("agents.runModal.task")}
        </label>
        <textarea
          id="run-agent-modal-input"
          className="modal__textarea run-agent-modal__textarea"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          rows={10}
          disabled={loading}
          placeholder={t("agents.runModal.placeholder")}
          aria-describedby="run-agent-modal-hint"
        />
        {error ? (
          <p className="modal__error" id="run-agent-modal-error" role="alert">
            {error}
          </p>
        ) : null}
      </div>
      <div className="modal__actions">
        <Button
          variant="secondary"
          onClick={onClose}
          disabled={loading}
        >
          {t("agents.runModal.cancel")}
        </Button>
        <Button
          variant="primary"
          busy={loading}
          onClick={handleSubmit}
          disabled={loading || input.trim() === ""}
        >
          {t("agents.runModal.start")}
        </Button>
      </div>
    </BaseModal>
  )
}
