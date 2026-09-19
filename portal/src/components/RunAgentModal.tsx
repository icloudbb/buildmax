import { useState, useEffect } from "react"
import type { Agent } from "../lib/types"
import { BaseModal, Button } from "@buildmax/gui"

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
      title={`Run ${agent.name}`}
      titleId="run-agent-modal-title"
      onClose={onClose}
      className="modal--large"
    >
      <div className="modal__body">
        <p className="modal__hint" id="run-agent-modal-hint">
          Describe what you want this agent to do. Its saved instructions are applied by the worker.
        </p>
        <label className="modal__label" htmlFor="run-agent-modal-input">
          Task
        </label>
        <textarea
          id="run-agent-modal-input"
          className="modal__textarea run-agent-modal__textarea"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          rows={10}
          disabled={loading}
          placeholder="What should the agent do?"
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
          Cancel
        </Button>
        <Button
          variant="primary"
          busy={loading}
          onClick={handleSubmit}
          disabled={loading || input.trim() === ""}
        >
          Start
        </Button>
      </div>
    </BaseModal>
  )
}
