import { useCallback, useEffect, useMemo, useState } from "react"
import { BaseModal, Button, ButtonLink, getInitials } from "@buildmax/gui"
import type { ApiPlugin, ApiPluginRelease } from "../../lib/api/types"
import { getPlugin, listPlugins } from "../../features/plugins/api"
import { newestInstallable } from "../../features/plugins/releaseSelection"
import { useSpace } from "../../contexts/SpaceContext"
import { buildHash } from "../../router"
import { Alert } from "../../components/state/Alert"
import { classifyError, deriveResourceState, type RequestError } from "../../state/resourceState"

/**
 * Marketplace is the deployment-wide plugin catalog, reached from the header
 * for any signed-in user.
 *
 * It is a browse surface: installing happens where the agent runs, which this
 * page cannot see, so it hands over the command rather than a button. The list
 * route carries only name and description, so each card's newest release is
 * fetched alongside to show what a plugin actually contributes — the catalog is
 * deployment-scoped and small, so fetching all of them up front is cheap.
 */
// Stable identity so `plugins` doesn't churn every render while pluginsData is
// still null (before the first successful fetch).
const EMPTY_PLUGINS: ApiPlugin[] = []

export function Marketplace({ token }: { token: string | null }) {
  const { currentSpace, currentUserRole } = useSpace()
  // null means "not yet successfully fetched", distinct from [] meaning the
  // deployment genuinely publishes nothing. See deriveResourceState.
  const [pluginsData, setPluginsData] = useState<ApiPlugin[] | null>(null)
  const [releases, setReleases] = useState<Record<string, ApiPluginRelease | null>>({})
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<RequestError | null>(null)
  const [query, setQuery] = useState("")
  const [selected, setSelected] = useState<string | null>(null)

  const load = useCallback(() => {
    if (!token) return
    setLoading(true)
    setLoadError(null)
    listPlugins(token)
      .then(async (res) => {
        const active = res.plugins.filter((p) => !p.archived_at)
        setPluginsData(active)
        // Enrich each card with its newest installable release, in parallel.
        // A failure on one plugin leaves that card without contributions
        // rather than failing the whole page.
        const entries = await Promise.all(
          active.map(async (p): Promise<[string, ApiPluginRelease | null]> => {
            try {
              const detail = await getPlugin(token, p.name)
              return [p.name, newestInstallable(detail.releases)]
            } catch {
              return [p.name, null]
            }
          }),
        )
        setReleases(Object.fromEntries(entries))
      })
      // pluginsData from a prior successful fetch (if any) is left in place,
      // so a failed refresh reads as Stale rather than wiping the catalog.
      .catch((err) => setLoadError(classifyError(err, "Failed to load the plugin catalog")))
      .finally(() => setLoading(false))
  }, [token])

  useEffect(load, [load])

  const pluginsState = useMemo(
    () => deriveResourceState({ loading, data: pluginsData, error: loadError, isEmpty: (data) => data.length === 0 }),
    [loading, pluginsData, loadError]
  )
  const plugins = pluginsData ?? EMPTY_PLUGINS

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return plugins
    return plugins.filter((p) => {
      const haystack = [p.name, p.display_name ?? "", p.description ?? ""].join(" ").toLowerCase()
      return haystack.includes(q)
    })
  }, [plugins, query])

  const selectedPlugin = selected ? plugins.find((p) => p.name === selected) ?? null : null

  return (
    <div className="marketplace">
      <div className="marketplace__head">
        <div>
          <h1 className="marketplace__title">Marketplace</h1>
          <p className="marketplace__subtitle">
            Plugins this deployment publishes — skills, subagents, MCP servers, and
            hooks you can install on your own machine.
          </p>
        </div>
        {!loading && plugins.length > 0 ? (
          <span className="marketplace__count">
            {plugins.length} {plugins.length === 1 ? "plugin" : "plugins"}
          </span>
        ) : null}
      </div>

      {plugins.length > 0 ? (
        <div className="marketplace__search">
          <SearchIcon />
          <input
            type="search"
            className="marketplace__search-input"
            placeholder="Search plugins…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            aria-label="Search plugins"
          />
        </div>
      ) : null}

      {(pluginsState.kind === "error" ||
        pluginsState.kind === "forbidden" ||
        pluginsState.kind === "notFound" ||
        pluginsState.kind === "stale") && (
        <Alert
          tone={pluginsState.kind === "stale" ? "stale" : pluginsState.kind}
          message={pluginsState.error.message}
          retry={{ label: "Retry", onClick: load }}
        />
      )}

      {pluginsState.kind === "loading" ? (
        <div className="marketplace__grid" aria-hidden>
          {[0, 1, 2].map((i) => (
            <div key={i} className="mkt-card mkt-card--skeleton" />
          ))}
        </div>
      ) : pluginsState.kind === "error" || pluginsState.kind === "forbidden" || pluginsState.kind === "notFound" ? null : plugins.length === 0 ? (
        <div className="marketplace__empty">
          <StorefrontGlyph />
          <p className="marketplace__empty-title">Nothing published yet</p>
          <p className="marketplace__empty-copy">
            When an administrator publishes a plugin, it shows up here for anyone to
            install.
          </p>
        </div>
      ) : filtered.length === 0 ? (
        <p className="marketplace__empty-copy marketplace__empty-copy--inline">
          No plugin matches “{query}”.
        </p>
      ) : (
        <div className="marketplace__grid">
          {filtered.map((plugin) => (
            <PluginCard
              key={plugin.name}
              plugin={plugin}
              release={releases[plugin.name] ?? null}
              onOpen={() => setSelected(plugin.name)}
            />
          ))}
        </div>
      )}

      <p className="marketplace__foot">
        Installing happens where the agent runs. This page cannot see your machine,
        so what is installed there is <code>buildmax plugin list</code> there.
      </p>

      {selectedPlugin ? (
        <PluginDetailModal
          plugin={selectedPlugin}
          release={releases[selectedPlugin.name] ?? null}
          spaceId={currentSpace?.id ?? null}
          spaceName={currentSpace?.name ?? null}
          canManageSpace={currentUserRole === "owner" || currentUserRole === "admin"}
          onClose={() => setSelected(null)}
        />
      ) : null}
    </div>
  )
}

/** One plugin in the grid: identity, description, and what it contributes. */
function PluginCard({
  plugin,
  release,
  onOpen,
}: {
  plugin: ApiPlugin
  release: ApiPluginRelease | null
  onOpen: () => void
}) {
  const title = plugin.display_name || plugin.name
  const chips = contributionChips(release)
  return (
    <button type="button" className="mkt-card" onClick={onOpen}>
      <div className="mkt-card__head">
        <span className="mkt-card__logo" aria-hidden>
          {getInitials(title)}
        </span>
        <div className="mkt-card__ident">
          <span className="mkt-card__title">{title}</span>
          <span className="mkt-card__name">{plugin.name}</span>
        </div>
        {release ? <span className="mkt-card__version">v{release.version}</span> : null}
      </div>

      {plugin.description ? (
        <p className="mkt-card__desc">{plugin.description}</p>
      ) : (
        <p className="mkt-card__desc mkt-card__desc--empty">No description.</p>
      )}

      {chips.length > 0 ? (
        <div className="mkt-chips">
          {chips.map((chip) => (
            <span key={chip} className="mkt-chip">
              {chip}
            </span>
          ))}
        </div>
      ) : null}
    </button>
  )
}

/** PluginDetailModal shows one plugin's newest release and how to install it. */
function PluginDetailModal({
  plugin,
  release,
  spaceId,
  spaceName,
  canManageSpace,
  onClose,
}: {
  plugin: ApiPlugin
  release: ApiPluginRelease | null
  spaceId: string | null
  spaceName: string | null
  canManageSpace: boolean
  onClose: () => void
}) {
  const title = plugin.display_name || plugin.name
  return (
    <BaseModal
      open
      title={title}
      titleId={`plugin-detail-${plugin.name}`}
      onClose={onClose}
      className="modal--large"
    >
      <div className="mkt-detail">
        <div className="mkt-detail__ident">
          <span className="mkt-card__logo mkt-card__logo--lg" aria-hidden>
            {getInitials(title)}
          </span>
          <div>
            <p className="mkt-detail__name">{plugin.name}</p>
            {plugin.description ? (
              <p className="mkt-detail__desc">{plugin.description}</p>
            ) : null}
          </div>
        </div>

        {release ? (
          <>
            <p className="mkt-detail__meta">
              Newest release <strong>v{release.version}</strong>
              {release.min_buildmax_version
                ? ` · needs BuildMax ${release.min_buildmax_version}+`
                : ""}
              {` · digest ${release.digest.replace(/^sha256:/, "").slice(0, 12)}…`}
            </p>

            <Contributions release={release} />

            {release.inspection.env_refs?.length ? (
              <section className="mkt-detail__section">
                <h3 className="mkt-detail__section-title">Environment</h3>
                <p className="mkt-detail__hint">
                  Reads these variables — a plugin that looks installed and does
                  nothing is usually one that is unset:
                </p>
                <div className="mkt-chips">
                  {release.inspection.env_refs.map((name) => (
                    <span key={name} className="mkt-chip mkt-chip--mono">
                      {name}
                    </span>
                  ))}
                </div>
              </section>
            ) : null}

            <InstallCommand name={plugin.name} />

            {spaceName && canManageSpace ? (
              <section className="mkt-detail__section">
                <h3 className="mkt-detail__section-title">Space activation</h3>
                <p className="mkt-detail__hint">
                  Publishing here does not activate it anywhere. To let {spaceName}&apos;s
                  background runs use it, activate it in Space Plugins.
                </p>
                {spaceId ? (
                  <ButtonLink variant="secondary" href={buildHash({ name: "space", spaceId, section: "plugins" })}>
                    Open Space Plugins
                  </ButtonLink>
                ) : null}
              </section>
            ) : null}
          </>
        ) : (
          <p className="mkt-detail__meta">
            Nothing here is installable: every release was withdrawn. An exact
            version can still be recovered from a terminal.
          </p>
        )}
      </div>
    </BaseModal>
  )
}

/** Contributions lists what a release brings, grouped by kind. */
function Contributions({ release }: { release: ApiPluginRelease }) {
  const insp = release.inspection
  const groups: { label: string; items: string[] }[] = []
  if (insp.skills?.length) groups.push({ label: "Skills", items: insp.skills })
  if (insp.subagents?.length)
    groups.push({ label: "Subagents", items: insp.subagents.map((s) => s.name) })
  if (insp.mcp?.length)
    groups.push({ label: "MCP servers", items: insp.mcp.map((s) => `${s.id} (${s.transport})`) })
  if (insp.hooks?.length)
    groups.push({ label: "Hooks", items: insp.hooks.map((h) => `${h.event} · ${h.type}`) })

  if (groups.length === 0) {
    return (
      <section className="mkt-detail__section">
        <h3 className="mkt-detail__section-title">Contributes</h3>
        <p className="mkt-detail__hint">Nothing this build recognises.</p>
      </section>
    )
  }

  return (
    <section className="mkt-detail__section">
      <h3 className="mkt-detail__section-title">Contributes</h3>
      <div className="mkt-detail__groups">
        {groups.map((group) => (
          <div key={group.label} className="mkt-detail__group">
            <span className="mkt-detail__group-label">{group.label}</span>
            <div className="mkt-chips">
              {group.items.map((item) => (
                <span key={item} className="mkt-chip mkt-chip--mono">
                  {item}
                </span>
              ))}
            </div>
          </div>
        ))}
      </div>
    </section>
  )
}

/** InstallCommand shows the command and copies it to the clipboard. */
function InstallCommand({ name }: { name: string }) {
  const [copied, setCopied] = useState(false)
  const command = `buildmax plugin install ${name}`

  const copy = () => {
    navigator.clipboard?.writeText(command).then(
      () => {
        setCopied(true)
        setTimeout(() => setCopied(false), 1500)
      },
      () => setCopied(false),
    )
  }

  return (
    <section className="mkt-detail__section">
      <h3 className="mkt-detail__section-title">Install</h3>
      <div className="mkt-install">
        <code className="mkt-install__cmd">{command}</code>
        <Button variant="secondary" size="compact" onClick={copy}>
          {copied ? "Copied" : "Copy"}
        </Button>
      </div>
    </section>
  )
}

/** contributionChips is the card's one-line summary of a release's payload. */
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

function countLabel(n: number, noun: string): string {
  return `${n} ${noun}${n === 1 ? "" : "s"}`
}

function SearchIcon() {
  return (
    <svg
      className="marketplace__search-icon"
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      <circle cx="11" cy="11" r="7" />
      <path d="m21 21-4.3-4.3" />
    </svg>
  )
}

function StorefrontGlyph() {
  return (
    <svg
      className="marketplace__empty-icon"
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      <path d="M3 9.5 4.5 4h15L21 9.5" />
      <path d="M3 9.5a2.5 2.5 0 0 0 5 0 2.5 2.5 0 0 0 5 0 2.5 2.5 0 0 0 5 0 2.5 2.5 0 0 0 3 0" />
      <path d="M5 11v8a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-8" />
      <path d="M9 20v-5h6v5" />
    </svg>
  )
}
