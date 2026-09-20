import { useCallback, useEffect, useMemo, useState } from "react"
import { Button, getInitials } from "@buildmax/gui"
import type {
  ApiAgent,
  ApiPlugin,
  ApiPluginActivation,
  ApiPluginCuration,
  ApiPluginRelease,
} from "../../lib/api/types"
import { getErrorMessage } from "../../lib/errorMessage"
import { getAgents } from "../agents/api"
import { getPlugin, listPlugins } from "../plugins/api"
import {
  activatePlugin,
  listActivations,
  movePin,
  setActivationEnabled,
  setCuration,
} from "./api"
import { buildPluginRow, curationCopy, originCopy, type PluginRow } from "./model"
import { Alert } from "../../components/state/Alert"
import { classifyError, deriveResourceState, type RequestError } from "../../state/resourceState"

/**
 * SpacePlugins is what this space's background runs may use.
 *
 * It is not the catalog page, which says what the deployment publishes and
 * hands over an install command for somebody's own machine. This is the space's
 * decision about its workers, and the record that answers "why did this run
 * have this capability".
 */
export function SpacePlugins({
  token,
  spaceId,
  canManage,
}: {
  token: string | null
  spaceId: string | null
  canManage: boolean
}) {
  // null means "not yet successfully fetched", distinct from [] meaning the
  // deployment genuinely publishes nothing. See deriveResourceState.
  const [rowsData, setRowsData] = useState<PluginRow[] | null>(null)
  const [curation, setCurationState] = useState<ApiPluginCuration>("open")
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<RequestError | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const [opened, setOpened] = useState<string | null>(null)
  // A mutation's error, tagged with which control caused it ("curation" or a
  // plugin name) so it renders next to that control rather than as a
  // page-level banner nothing points back to.
  const [actionError, setActionError] = useState<{ key: string; message: string } | null>(null)

  const load = useCallback(async () => {
    if (!token || !spaceId) return
    setLoading(true)
    setLoadError(null)
    try {
      const [catalog, activations, agents] = await Promise.all([
        listPlugins(token),
        listActivations(token, spaceId),
        // A reader who cannot list agents still gets the activations; the
        // "which agents name it" line is the part that goes missing.
        getAgents(spaceId, token).catch((): ApiAgent[] => []),
      ])
      const byName = new Map<string, ApiPluginActivation>(
        activations.activations.map((a: ApiPluginActivation) => [a.plugin_name, a]),
      )
      const releasesByName = await loadReleases(
        token,
        catalog.plugins.map((p: ApiPlugin) => p.name),
      )
      setCurationState(activations.curation)
      setRowsData(
        catalog.plugins
          .filter((entry: ApiPlugin) => !entry.archived_at)
          .map((entry: ApiPlugin) =>
            buildPluginRow({
              name: entry.name,
              displayName: entry.display_name || entry.name,
              description: entry.description ?? "",
              releases: releasesByName.get(entry.name) ?? [],
              activation: byName.get(entry.name) ?? null,
              agents,
            }),
          ),
      )
    } catch (err) {
      // rowsData from a prior successful fetch (if any) is left in place, so
      // a failed refresh reads as Stale rather than wiping the list.
      setLoadError(classifyError(err, "Failed to load this space's plugins"))
    } finally {
      setLoading(false)
    }
  }, [token, spaceId])

  useEffect(() => {
    void load()
  }, [load])

  const rowsState = useMemo(
    () => deriveResourceState({ loading, data: rowsData, error: loadError, isEmpty: (data) => data.length === 0 }),
    [loading, rowsData, loadError]
  )
  const rows = rowsData ?? []

  async function run(key: string, action: () => Promise<unknown>) {
    if (!token || !spaceId) return
    setBusy(key)
    setActionError(null)
    try {
      await action()
      await load()
    } catch (err) {
      setActionError({ key, message: getErrorMessage(err, "That did not work") })
    } finally {
      setBusy(null)
    }
  }

  const activeCount = rows.filter((r) => r.activation?.enabled).length

  return (
    <section className="tp">
      <div className="tp__head">
        <div>
          <h2 className="tp__title">Plugins</h2>
          <p className="tp__copy">
            What this space&apos;s background runs may use. An agent loads only the
            plugins it names — activating one here makes it available to name, and
            changes no existing agent.
          </p>
        </div>
        {!loading && rows.length > 0 ? (
          <span className="tp__count">
            {activeCount} of {rows.length} active
          </span>
        ) : null}
      </div>

      {(rowsState.kind === "error" ||
        rowsState.kind === "forbidden" ||
        rowsState.kind === "notFound" ||
        rowsState.kind === "stale") && (
        <Alert
          tone={rowsState.kind === "stale" ? "stale" : rowsState.kind}
          message={rowsState.error.message}
          retry={{ label: "Retry", onClick: () => void load() }}
        />
      )}

      <CurationControl
        curation={curation}
        canManage={canManage}
        busy={busy === "curation"}
        error={actionError?.key === "curation" ? actionError.message : null}
        onChange={(next) =>
          run("curation", () => setCuration(token as string, spaceId as string, next))
        }
      />

      {rowsState.kind === "loading" ? (
        <div className="tp-list" aria-hidden>
          {[0, 1, 2].map((i) => (
            <div key={i} className="tp-card tp-card--skeleton" />
          ))}
        </div>
      ) : rowsState.kind === "error" || rowsState.kind === "forbidden" || rowsState.kind === "notFound" ? null : rows.length === 0 ? (
        <p className="tp-empty">This deployment has published nothing yet.</p>
      ) : (
        <ul className="tp-list">
          {rows.map((row) => (
            <PluginRowView
              key={row.name}
              row={row}
              canManage={canManage}
              busy={busy === row.name}
              error={actionError?.key === row.name ? actionError.message : null}
              expanded={opened === row.name}
              onToggle={() => setOpened(opened === row.name ? null : row.name)}
              onActivate={() =>
                run(row.name, () => activatePlugin(token as string, spaceId as string, row.name))
              }
              onUpdate={(version) =>
                run(row.name, () =>
                  movePin(token as string, spaceId as string, row.name, version),
                )
              }
              onSetEnabled={(enabled) =>
                run(row.name, () =>
                  setActivationEnabled(token as string, spaceId as string, row.name, enabled),
                )
              }
            />
          ))}
        </ul>
      )}

      <p className="tp-foot">
        A pin never moves on its own: a release published after an activation cannot
        change what a run loads until somebody updates it here. What is installed on
        your own machine is a different thing — <code>buildmax plugin list</code> there.
      </p>
    </section>
  )
}

function CurationControl({
  curation,
  canManage,
  busy,
  error,
  onChange,
}: {
  curation: ApiPluginCuration
  canManage: boolean
  busy: boolean
  error: string | null
  onChange: (next: ApiPluginCuration) => void
}) {
  const other: ApiPluginCuration = curation === "curated" ? "open" : "curated"
  return (
    <div className="tp-mode">
      <div className="tp-mode__text">
        <span className="tp-mode__label">
          <span className={`tp-mode__badge tp-mode__badge--${curation}`}>
            {curation === "curated" ? "Curated" : "Open"}
          </span>
          catalog mode
        </span>
        <p className="tp-mode__copy">{curationCopy(curation)}</p>
        {error ? (
          <p className="tp__error" role="alert">
            {error}
          </p>
        ) : null}
      </div>
      {canManage ? (
        <Button
          variant="secondary" busy={busy}
          onClick={() => onChange(other)}
        >
          {other === "curated" ? "Curate this list" : "Open the catalog"}
        </Button>
      ) : null}
    </div>
  )
}

function PluginRowView({
  row,
  canManage,
  busy,
  error,
  expanded,
  onToggle,
  onActivate,
  onUpdate,
  onSetEnabled,
}: {
  row: PluginRow
  canManage: boolean
  busy: boolean
  error: string | null
  expanded: boolean
  onToggle: () => void
  onActivate: () => void
  onUpdate: (version: string) => void
  onSetEnabled: (enabled: boolean) => void
}) {
  const { activation } = row
  const status = pluginStatus(row)
  const chips = contributionChips(row.newest)
  return (
    <li className={`tp-card ${expanded ? "tp-card--open" : ""}`}>
      <div className="tp-card__head">
        <span className="tp-card__logo" aria-hidden>
          {getInitials(row.displayName)}
        </span>
        <div className="tp-card__ident">
          <span className="tp-card__title">{row.displayName}</span>
          <span className="tp-card__name">{row.name}</span>
        </div>
        <span className={`tp-status tp-status--${status.tone}`}>
          <span className="tp-status__dot" aria-hidden />
          {status.label}
        </span>
      </div>

      <p className="tp-card__meta">{metaLine(row)}</p>

      {chips.length > 0 || row.staleVersion ? (
        <div className="tp-chips">
          {chips.map((chip) => (
            <span key={chip} className="tp-chip">
              {chip}
            </span>
          ))}
          {row.staleVersion ? (
            <span className="tp-chip tp-chip--warn">Update to {row.staleVersion} available</span>
          ) : null}
        </div>
      ) : null}

      <div className="tp-card__actions">
        <Button variant="tertiary" size="compact" onClick={onToggle}>
          {expanded ? "Hide details" : "Details"}
        </Button>
        {canManage && !activation && row.newest ? (
          <Button variant="secondary" size="compact" busy={busy} onClick={onActivate}>
            Activate
          </Button>
        ) : null}
        {canManage && activation && row.staleVersion ? (
          <Button
            variant="secondary" size="compact" busy={busy}
            onClick={() => onUpdate(row.staleVersion as string)}
          >
            Update to {row.staleVersion}
          </Button>
        ) : null}
        {canManage && activation ? (
          <Button
            variant="tertiary" size="compact"
            disabled={busy}
            onClick={() => onSetEnabled(!activation.enabled)}
          >
            {activation.enabled ? "Suspend" : "Resume"}
          </Button>
        ) : null}
      </div>

      {error ? (
        <p className="tp__error" role="alert">
          {error}
        </p>
      ) : null}

      {expanded ? <PluginRowDetail row={row} /> : null}
    </li>
  )
}

/** pluginStatus is the single at-a-glance state a reader scans down the list. */
function pluginStatus(row: PluginRow): { label: string; tone: string } {
  if (row.executableOnly) return { label: "Needs operator approval", tone: "blocked" }
  if (!row.activation) {
    if (!row.newest) return { label: "No release", tone: "idle" }
    return { label: "Available", tone: "idle" }
  }
  if (!row.activation.enabled) return { label: "Suspended", tone: "suspended" }
  return { label: "Active", tone: "active" }
}

/** metaLine is the plain-language line under the identity. */
function metaLine(row: PluginRow): string {
  if (!row.activation) {
    if (row.executableOnly) {
      return "Every release contributes hooks or MCP servers, which a space cannot activate yet."
    }
    if (!row.newest) return "Nothing here can be activated."
    return "Not activated — no agent can name it until it is."
  }
  const used =
    row.usedBy.length > 0
      ? `Named by ${row.usedBy.join(", ")}`
      : "No agent names it, so no run loads it"
  return `Pinned to v${row.activation.version} · ${used}`
}

/** activationSummary is the one line that says where this plugin stands. */
export function activationSummary(row: PluginRow): string {
  if (!row.activation) {
    if (row.executableOnly) {
      return "Cannot be activated yet: every release contributes hooks or MCP servers"
    }
    if (!row.newest) return "Nothing here can be activated"
    return "Not activated"
  }
  const state = row.activation.enabled ? "Activated" : "Suspended"
  const stale = row.staleVersion ? `, ${row.staleVersion} available` : ""
  const used =
    row.usedBy.length > 0
      ? `named by ${row.usedBy.join(", ")}`
      : "no agent names it, so no run loads it"
  return `${state} at ${row.activation.version}${stale} · ${used}`
}

function PluginRowDetail({ row }: { row: PluginRow }) {
  return (
    <div className="tp-detail">
      {row.description ? <p className="tp-detail__desc">{row.description}</p> : null}
      {row.activation ? (
        <p className="tp-detail__meta">
          {originCopy(row.activation)} · digest <code>{row.activation.digest}</code>
        </p>
      ) : null}
      {row.activation && !row.activation.enabled ? (
        <p className="tp-detail__note">
          While suspended, a run whose agent names this plugin fails rather than
          running without it.
        </p>
      ) : null}
      {row.newest ? <ReleaseReport release={row.newest} /> : null}
      {row.executableOnly ? (
        <p className="tp-detail__note">
          Hooks and MCP servers start processes on the infrastructure a worker runs
          on. Activating them needs an operator&apos;s decision, which this
          deployment cannot record yet.
        </p>
      ) : null}
    </div>
  )
}

/** ReleaseReport is the same sanitized report an install shows locally. */
function ReleaseReport({ release }: { release: ApiPluginRelease }) {
  const groups = contributionGroups(release)
  return (
    <div className="tp-detail__section">
      <p className="tp-detail__meta">
        Newest activatable release <strong>v{release.version}</strong>, published by{" "}
        {release.published_by}.
      </p>
      {groups.map((group) => (
        <div key={group.label} className="tp-detail__group">
          <span className="tp-detail__group-label">{group.label}</span>
          <div className="tp-chips">
            {group.items.map((item) => (
              <span key={item} className="tp-chip tp-chip--mono">
                {item}
              </span>
            ))}
          </div>
        </div>
      ))}
      {release.inspection.env_refs?.length ? (
        <p className="tp-detail__note">
          {/* No per-space secret exists yet, so an unset variable is the usual
              reason an activated plugin starts and does nothing. */}
          Reads {release.inspection.env_refs.join(", ")}. A worker holds no per-space
          secrets, so any it needs will be unset.
        </p>
      ) : null}
    </div>
  )
}

/** contributionChips is the collapsed card's one-line payload summary. */
function contributionChips(release: ApiPluginRelease | null): string[] {
  if (!release) return []
  const insp = release.inspection
  const chips: string[] = []
  if (insp.skills?.length) chips.push(countLabel(insp.skills.length, "skill"))
  if (insp.subagents?.length) chips.push(countLabel(insp.subagents.length, "subagent"))
  if (insp.mcp?.length) chips.push(countLabel(insp.mcp.length, "MCP server"))
  if (insp.hooks?.length) chips.push(countLabel(insp.hooks.length, "hook"))
  return chips
}

/** contributionGroups is the expanded, named list of what a release brings. */
function contributionGroups(release: ApiPluginRelease): { label: string; items: string[] }[] {
  const insp = release.inspection
  const groups: { label: string; items: string[] }[] = []
  if (insp.skills?.length) groups.push({ label: "Skills", items: insp.skills })
  if (insp.subagents?.length)
    groups.push({ label: "Subagents", items: insp.subagents.map((s) => s.name) })
  if (insp.mcp?.length)
    groups.push({ label: "MCP servers", items: insp.mcp.map((s) => `${s.id} (${s.transport})`) })
  if (insp.hooks?.length)
    groups.push({ label: "Hooks", items: insp.hooks.map((h) => `${h.event} · ${h.type}`) })
  return groups
}

function countLabel(n: number, noun: string): string {
  return `${n} ${noun}${n === 1 ? "" : "s"}`
}

async function loadReleases(
  token: string,
  names: string[],
): Promise<Map<string, ApiPluginRelease[]>> {
  const pairs = await Promise.all(
    names.map(async (name) => {
      try {
        const res = await getPlugin(token, name)
        return [name, res.releases] as const
      } catch {
        // One unreadable entry must not blank the page: it shows as having no
        // activatable release, which is what the reader can act on anyway.
        return [name, [] as ApiPluginRelease[]] as const
      }
    }),
  )
  return new Map(pairs)
}
