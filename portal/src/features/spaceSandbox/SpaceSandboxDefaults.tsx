import { useCallback, useEffect, useState } from "react"
import { getErrorMessage } from "../../lib/errorMessage"
import { useStableT, useT } from "../../i18n"
import { spaceSandboxFilesystemTierOptions, spaceSandboxNetworkTierOptions } from "../../lib/sandboxTiers"
import { getSandboxDefaults, setSandboxDefaults } from "./api"

/**
 * SpaceSandboxDefaults is the tiers an agent that declares neither inherits --
 * the same "what this space's background runs may use" question SpacePlugins
 * answers for installed tools, answered here for network and filesystem
 * access. See docs/design/agent-sandbox-policy.md §9 M3.
 */
export function SpaceSandboxDefaults({
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
  const networkTierOptions = spaceSandboxNetworkTierOptions(t)
  const filesystemTierOptions = spaceSandboxFilesystemTierOptions(t)
  const [networkTier, setNetworkTier] = useState("")
  const [filesystemTier, setFilesystemTier] = useState("")
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    if (!token || !spaceId) return
    setLoading(true)
    setError(null)
    try {
      const got = await getSandboxDefaults(token, spaceId)
      setNetworkTier(got.sandbox_network_tier ?? "")
      setFilesystemTier(got.sandbox_filesystem_tier ?? "")
    } catch (err) {
      setError(getErrorMessage(err, stableT("settings.sandbox.loadError")))
    } finally {
      setLoading(false)
    }
  }, [token, spaceId, stableT])

  useEffect(() => {
    void load()
  }, [load])

  async function save(next: { networkTier: string; filesystemTier: string }) {
    if (!token || !spaceId) return
    setSaving(true)
    setError(null)
    const previous = { networkTier, filesystemTier }
    setNetworkTier(next.networkTier)
    setFilesystemTier(next.filesystemTier)
    try {
      await setSandboxDefaults(token, spaceId, {
        sandbox_network_tier: next.networkTier,
        sandbox_filesystem_tier: next.filesystemTier,
      })
    } catch (err) {
      setNetworkTier(previous.networkTier)
      setFilesystemTier(previous.filesystemTier)
      setError(getErrorMessage(err, stableT("settings.sandbox.saveError")))
    } finally {
      setSaving(false)
    }
  }

  return (
    <section className="settings-page__section">
      <div className="settings-page__section-head">
        <div>
          <h2 className="settings-page__section-title">{t("settings.sandbox.title")}</h2>
          <p className="settings-page__section-copy">{t("settings.sandbox.copy")}</p>
        </div>
      </div>

      {error ? (
        <p className="settings-section__error" role="alert">
          {error}
        </p>
      ) : null}

      {loading ? (
        <p className="admin-empty">{t("shell.loading")}</p>
      ) : (
        <div className="admin-sections">
          <div>
            <label className="modal__label" htmlFor="space-sandbox-network-tier">
              {t("settings.sandbox.network")}
            </label>
            <select
              id="space-sandbox-network-tier"
              className="modal__input"
              value={networkTier}
              disabled={!canManage || saving}
              onChange={(e) => save({ networkTier: e.target.value, filesystemTier })}
            >
              {networkTierOptions.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
            <p className="modal__hint">
              {networkTierOptions.find((o) => o.value === networkTier)?.description}
            </p>
          </div>
          <div>
            <label className="modal__label" htmlFor="space-sandbox-filesystem-tier">
              {t("settings.sandbox.filesystem")}
            </label>
            <select
              id="space-sandbox-filesystem-tier"
              className="modal__input"
              value={filesystemTier}
              disabled={!canManage || saving}
              onChange={(e) => save({ networkTier, filesystemTier: e.target.value })}
            >
              {filesystemTierOptions.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
            <p className="modal__hint">
              {filesystemTierOptions.find((o) => o.value === filesystemTier)?.description}
            </p>
          </div>
        </div>
      )}
    </section>
  )
}
