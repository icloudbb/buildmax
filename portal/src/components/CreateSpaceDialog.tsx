import { useState } from "react"
import { Button, BaseModal } from "@buildmax/gui"
import { useAuth } from "../contexts/AuthContext"
import { useSpace } from "../contexts/SpaceContext"
import { createSpace } from "../features/spaces/api"
import { getErrorMessage } from "../lib/errorMessage"

interface CreateSpaceDialogProps {
  open: boolean
  onClose: () => void
}

export function CreateSpaceDialog({ open, onClose }: CreateSpaceDialogProps) {
  const { token } = useAuth()
  const { refetchSpaces } = useSpace()
  const [spaceName, setSpaceName] = useState("")
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function handleCreate() {
    if (!token || !spaceName.trim() || creating) return
    setCreating(true)
    setError(null)
    try {
      const created = await createSpace({ name: spaceName.trim() }, token)
      setSpaceName("")
      await refetchSpaces(created.id)
      onClose()
    } catch (err) {
      setError(getErrorMessage(err, "Failed to create space"))
    } finally {
      setCreating(false)
    }
  }

  return (
    <BaseModal
      open={open}
      title="Create Space"
      titleId="create-space-dialog-title"
      onClose={() => {
        if (creating) return
        setSpaceName("")
        setError(null)
        onClose()
      }}
    >
      <div className="modal__body">
        <div className="space-settings-page__dialog">
          <p className="space-settings-page__muted">
            Create a new shared space for agents, workflows, issues, and conversations.
          </p>
          <input
            className="issues-page__input"
            type="text"
            value={spaceName}
            onChange={(e) => setSpaceName(e.target.value)}
            placeholder="e.g. Design, Ops, Research"
            autoFocus
          />
          {error ? (
            <p className="modal__error" role="alert">
              {error}
            </p>
          ) : null}
          <div className="space-settings-page__dialog-actions">
            <Button
              variant="secondary"
              disabled={creating}
              onClick={() => {
                setSpaceName("")
                setError(null)
                onClose()
              }}
            >
              Cancel
            </Button>
            <Button
              variant="primary" busy={creating}
              disabled={!spaceName.trim()}
              onClick={() => void handleCreate()}
            >
              Create Space
            </Button>
          </div>
        </div>
      </div>
    </BaseModal>
  )
}
