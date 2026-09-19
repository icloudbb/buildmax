import { Button } from "@buildmax/gui"
import { useCallback, useEffect, useState } from "react"
import type { ApiPlugin, ApiPluginRelease } from "../../lib/api/types"
import { getErrorMessage } from "../../lib/errorMessage"
import {
  listAdminPluginReleases,
  listAdminPlugins,
  setAdminPluginArchived,
  yankAdminPluginRelease,
} from "./api"

/**
 * AdminPlugins manages the deployment's plugin catalog.
 *
 * It never says a plugin is installed. Installation happens in somebody's
 * BUILDMAX_HOME, which this server cannot see, and a catalog that guessed
 * would be wrong the first time anyone uninstalled. What is here is what the
 * deployment published; what is on a machine is `buildmax plugin list` there.
 *
 * Publishing is a command rather than a form for a reason that is not the
 * models one: what gets published is a directory on the publisher's machine,
 * and a browser cannot pack it.
 */
export function AdminPlugins({ token }: { token: string | null }) {
  const [plugins, setPlugins] = useState<ApiPlugin[]>([])
  const [loading, setLoading] = useState(true)
  const [busyName, setBusyName] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [expanded, setExpanded] = useState<string | null>(null)

  const load = useCallback(() => {
    if (!token) return
    setLoading(true)
    setError(null)
    listAdminPlugins(token)
      .then((res) => setPlugins(res.plugins))
      .catch((err) => setError(getErrorMessage(err, "Failed to load the plugin catalog")))
      .finally(() => setLoading(false))
  }, [token])

  useEffect(load, [load])

  function toggleArchived(entry: ApiPlugin) {
    if (!token) return
    const archived = Boolean(entry.archived_at)
    if (
      !archived &&
      !window.confirm(
        `Retire ${entry.name}?\n\n` +
          "It leaves the default catalog and accepts no new releases. Nothing is " +
          "deleted: copies already installed keep working, and restoring it undoes this.",
      )
    )
      return
    setBusyName(entry.name)
    setError(null)
    setAdminPluginArchived(token, entry.name, !archived)
      .then(load)
      .catch((err) => setError(getErrorMessage(err, "The change did not complete")))
      .finally(() => setBusyName(null))
  }

  return (
    <div className="admin-sections">
      <section className="settings-page__section">
        <div className="settings-page__section-head">
          <div>
            <h2 className="settings-page__section-title">Plugins</h2>
            <p className="settings-page__section-copy">
              What this deployment publishes. A release is skills, subagents, MCP
              servers, and hooks that run on the machine of whoever installs it.
            </p>
          </div>
        </div>

        {error ? (
          <p className="settings-section__error" role="alert">
            {error}
          </p>
        ) : null}

        {loading ? (
          <p className="admin-empty">Loading…</p>
        ) : plugins.length === 0 ? (
          <p className="admin-empty">Nothing has been published yet.</p>
        ) : (
          <ul className="admin-list">
            {plugins.map((entry) => {
              const archived = Boolean(entry.archived_at)
              return (
                <li key={entry.name} className="admin-list__row">
                  <span className="admin-list__main">
                    {entry.display_name || entry.name}
                    <span className="admin-list__meta"> · {entry.name}</span>
                  </span>
                  <span className={archived ? "admin-pill" : "admin-pill admin-pill--ok"}>
                    {archived ? "retired" : "published"}
                  </span>
                  <Button
                    variant="tertiary" size="compact"
                    onClick={() => setExpanded(expanded === entry.name ? null : entry.name)}
                  >
                    {expanded === entry.name ? "Hide releases" : "Releases"}
                  </Button>
                  <Button
                    variant={archived ? "secondary" : "danger"}
                    size="compact"
                    busy={busyName === entry.name}
                    onClick={() => toggleArchived(entry)}
                  >
                    {archived ? "Restore" : "Retire"}
                  </Button>
                </li>
              )
            })}
          </ul>
        )}

        {expanded ? <PluginReleases token={token} name={expanded} onChange={load} /> : null}

        <p className="admin-scope-note">
          Publishing is a command, because what is published is a directory on the
          publisher’s machine:
        </p>
        <code className="admin-code__value">buildmax plugin publish ./my-plugin</code>
        <p className="admin-scope-note">
          Installing happens on a machine, not here. This deployment cannot see what
          anybody installed — <code>buildmax plugin list</code> there answers that.
        </p>
      </section>
    </div>
  )
}

/** PluginReleases lists what one entry has published, withdrawn ones included. */
function PluginReleases({
  token,
  name,
  onChange,
}: {
  token: string | null
  name: string
  onChange: () => void
}) {
  const [releases, setReleases] = useState<ApiPluginRelease[]>([])
  const [loading, setLoading] = useState(true)
  const [busyVersion, setBusyVersion] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(() => {
    if (!token) return
    setLoading(true)
    setError(null)
    listAdminPluginReleases(token, name)
      .then((res) => setReleases(res.releases))
      .catch((err) => setError(getErrorMessage(err, "Failed to load releases")))
      .finally(() => setLoading(false))
  }, [token, name])

  useEffect(load, [load])

  function yank(release: ApiPluginRelease) {
    if (!token) return
    const reason = window.prompt(
      `Withdraw ${name} ${release.version}?\n\n` +
        "It leaves the default choice for new installs. Copies already installed keep " +
        "working, and it stays installable by exact version.\n\n" +
        "Why? This is shown to anyone who asks for it afterwards.",
      "",
    )
    if (reason === null) return
    setBusyVersion(release.version)
    setError(null)
    yankAdminPluginRelease(token, name, release.version, reason)
      .then(() => {
        load()
        onChange()
      })
      .catch((err) => setError(getErrorMessage(err, "The withdrawal did not complete")))
      .finally(() => setBusyVersion(null))
  }

  if (loading) return <p className="admin-empty">Loading releases…</p>

  return (
    <div className="admin-sections">
      {error ? (
        <p className="settings-section__error" role="alert">
          {error}
        </p>
      ) : null}
      {releases.length === 0 ? (
        <p className="admin-empty">No releases yet.</p>
      ) : (
        <ul className="admin-list">
          {releases.map((release) => {
            const yanked = Boolean(release.yanked_at)
            return (
              <li key={`${release.plugin_name}@${release.version}`} className="admin-list__row">
                <span className="admin-list__main">
                  {release.version}
                  <span className="admin-list__meta"> · {contributionSummary(release)}</span>
                </span>
                {/* A digest is what lets a publisher and a consumer compare
                    notes about the same bytes, so it is shown rather than kept. */}
                <span className="admin-list__meta" title={release.digest}>
                  {shortDigest(release.digest)}
                </span>
                {yanked ? (
                  <span
                    className="admin-pill admin-pill--bad"
                    title={release.yanked_reason || undefined}
                  >
                    withdrawn
                  </span>
                ) : (
                  <span className="admin-pill admin-pill--ok">available</span>
                )}
                {release.source.dirty ? (
                  // Packed from a working tree that was not the commit it names.
                  <span className="admin-pill admin-pill--bad">dirty tree</span>
                ) : null}
                <Button
                  variant="danger" size="compact"
                  disabled={yanked || busyVersion === release.version}
                  onClick={() => yank(release)}
                >
                  Withdraw
                </Button>
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}

/** contributionSummary says what a release brings, in the order it matters. */
export function contributionSummary(release: ApiPluginRelease): string {
  const parts: string[] = []
  const counts: [number | undefined, string][] = [
    [release.inspection.skills?.length, "skill"],
    [release.inspection.subagents?.length, "subagent"],
    [release.inspection.mcp?.length, "MCP server"],
    [release.inspection.hooks?.length, "hook"],
  ]
  for (const [count, noun] of counts) {
    if (count) parts.push(`${count} ${noun}${count === 1 ? "" : "s"}`)
  }
  if (parts.length === 0) return "nothing this build recognises"
  return parts.join(", ")
}

/** shortDigest keeps a digest readable while staying unambiguous in a listing. */
export function shortDigest(digest: string): string {
  const hex = digest.startsWith("sha256:") ? digest.slice("sha256:".length) : digest
  return hex.slice(0, 12)
}
