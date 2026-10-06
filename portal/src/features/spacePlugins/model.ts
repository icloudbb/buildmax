import type { Translate } from "@buildmax/gui"
import type { MessageKey } from "../../i18n"
import type {
  ApiAgent,
  ApiPluginActivation,
  ApiPluginCuration,
  ApiPluginRelease,
} from "../../lib/api/types"
import { newestInstallable } from "../plugins/releaseSelection"

/**
 * The reading of a space's plugin list that the section renders.
 *
 * It is computed here rather than in the component so the rules — what is
 * stale, what nothing names, what cannot be activated at all — are testable
 * without a DOM, and so the component stays a rendering of one value.
 */
export interface PluginRow {
  name: string
  displayName: string
  description: string
  /** Absent when this space has not activated the plugin. */
  activation: ApiPluginActivation | null
  /** The newest release the space could be pinned to, or null when there is none. */
  newest: ApiPluginRelease | null
  /**
   * Set when no release can be activated and the reason is what it contributes.
   * Phase D2 replaces this with the operator's unattended-eligibility flag.
   */
  executableOnly: boolean
  /** A newer release exists than the one pinned. A pin never moves on its own. */
  staleVersion: string | null
  /** Space agents whose definition names this plugin. */
  usedBy: string[]
}

/** contributesExecutable reports whether a release brings hooks or MCP servers. */
export function contributesExecutable(release: ApiPluginRelease): boolean {
  return Boolean(release.inspection.hooks?.length || release.inspection.mcp?.length)
}

/**
 * activatableReleases is what a space could pin to today: published, not
 * withdrawn, and contributing nothing executable.
 */
export function activatableReleases(releases: ApiPluginRelease[]): ApiPluginRelease[] {
  return releases.filter((r) => !contributesExecutable(r))
}

/**
 * buildPluginRow joins one catalog entry with what this space did about it.
 *
 * staleVersion is deliberately computed against the newest *activatable*
 * release rather than the newest release: offering an update the server would
 * refuse is worse than offering none.
 */
export function buildPluginRow(args: {
  name: string
  displayName: string
  description: string
  releases: ApiPluginRelease[]
  activation: ApiPluginActivation | null
  agents: ApiAgent[]
}): PluginRow {
  const { name, displayName, description, releases, activation, agents } = args
  const candidates = activatableReleases(releases)
  const newest = newestInstallable(candidates)
  const anyInstallable = newestInstallable(releases)

  let staleVersion: string | null = null
  if (activation && newest && newest.version !== activation.version) {
    // Only forward. A space pinned deliberately to an older release is not
    // behind, and calling it stale would push somebody to undo that choice.
    if (isNewer(newest.version, activation.version)) staleVersion = newest.version
  }

  return {
    name,
    displayName,
    description,
    activation,
    newest,
    executableOnly: newest === null && anyInstallable !== null,
    staleVersion,
    usedBy: agents.filter((a) => a.plugins?.includes(name)).map((a) => a.name),
  }
}

/** isNewer compares two stable versions; anything that will not parse is not newer. */
export function isNewer(candidate: string, current: string): boolean {
  const a = parseVersion(candidate)
  const b = parseVersion(current)
  if (!a || !b) return false
  for (let i = 0; i < 3; i += 1) {
    if (a[i] !== b[i]) return a[i] > b[i]
  }
  return false
}

function parseVersion(version: string): number[] | null {
  const match = /^(\d+)\.(\d+)\.(\d+)$/.exec(version)
  if (!match) return null
  return [Number(match[1]), Number(match[2]), Number(match[3])]
}

/**
 * curationCopy spells out what a mode means.
 *
 * The bare word does not tell a reader whether an empty list means nothing is
 * available or everything is, which is exactly the confusion two modes create.
 */
export function curationCopy(curation: ApiPluginCuration, t: Translate<MessageKey>): string {
  return curation === "curated"
    ? t("settings.plugins.curationCurated")
    : t("settings.plugins.curationOpen")
}

/** originCopy says how an activation came to exist, and who caused it. */
export function originCopy(activation: ApiPluginActivation, t: Translate<MessageKey>): string {
  return activation.origin === "automatic"
    ? t("settings.plugins.originAutomatic", { user: activation.activated_by })
    : t("settings.plugins.originManual", { user: activation.activated_by })
}
