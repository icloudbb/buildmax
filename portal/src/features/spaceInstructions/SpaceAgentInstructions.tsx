import { Button } from "@buildmax/gui"
import { useCallback, useEffect, useState } from "react"
import { getErrorMessage } from "../../lib/errorMessage"
import { useStableT, useT } from "../../i18n"
import { getSpaceAgentInstructions, setSpaceAgentInstructions } from "./api"

const MAX_INSTRUCTIONS_CHARS = 8192

export function SpaceAgentInstructions({
  token,
  spaceId,
  canManage,
}: {
  token: string | null
  spaceId: string | null
  canManage: boolean
}) {
  const t = useT()
  const stableT = useStableT()
  const [saved, setSaved] = useState("")
  const [draft, setDraft] = useState("")
  const [revision, setRevision] = useState(0)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    if (!token || !spaceId) return
    setLoading(true)
    setError(null)
    try {
      const got = await getSpaceAgentInstructions(token, spaceId)
      setSaved(got.instructions)
      setDraft(got.instructions)
      setRevision(got.revision)
    } catch (err) {
      setError(getErrorMessage(err, stableT("settings.instructions.loadError")))
    } finally {
      setLoading(false)
    }
  }, [token, spaceId, stableT])

  useEffect(() => {
    void load()
  }, [load])

  async function save() {
    if (!token || !spaceId || saving || draft === saved) return
    setSaving(true)
    setError(null)
    try {
      const got = await setSpaceAgentInstructions(token, spaceId, draft)
      setSaved(got.instructions)
      setDraft(got.instructions)
      setRevision(got.revision)
    } catch (err) {
      setError(getErrorMessage(err, stableT("settings.instructions.saveError")))
    } finally {
      setSaving(false)
    }
  }

  return (
    <section className="settings-page__section">
      <div className="settings-page__section-head">
        <div>
          <h2 className="settings-page__section-title">{t("settings.instructions.title")}</h2>
          <p className="settings-page__section-copy">{t("settings.instructions.copy")}</p>
        </div>
      </div>

      {error ? <p className="settings-section__error" role="alert">{error}</p> : null}

      {loading ? (
        <p className="admin-empty">{t("shell.loading")}</p>
      ) : (
        <div>
          <label className="modal__label" htmlFor="space-agent-instructions">
            {t("settings.instructions.label")}
          </label>
          <textarea
            id="space-agent-instructions"
            className="modal__textarea space-agent-instructions__textarea"
            rows={10}
            maxLength={MAX_INSTRUCTIONS_CHARS}
            value={draft}
            disabled={!canManage || saving}
            placeholder={t("settings.instructions.placeholder")}
            onChange={(event) => setDraft(event.target.value)}
          />
          <div className="space-agent-instructions__footer">
            <p className="modal__hint">
              {t("settings.instructions.hint")}
              {revision > 0 ? t("settings.instructions.revision", { revision }) : ""}
            </p>
            <span className="space-agent-instructions__count">
              {draft.length.toLocaleString()} / {MAX_INSTRUCTIONS_CHARS.toLocaleString()}
            </span>
          </div>
          {canManage ? (
            <Button
              variant="primary" busy={saving}
              disabled={draft === saved}
              onClick={() => void save()}
            >
              {t("settings.instructions.save")}
            </Button>
          ) : (
            <p className="modal__hint">{t("settings.instructions.readOnly")}</p>
          )}
        </div>
      )}
    </section>
  )
}
