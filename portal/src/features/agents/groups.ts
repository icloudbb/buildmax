import type { ReactNode } from "react"
import type { FormModalGroup, Translate } from "@buildmax/gui"
import type { MessageKey } from "../../i18n"

// The configuration sections shared by the create dialog and the inline detail-
// page editor. The create dialog lays these out as FormModal tabs (a left
// sidebar); the detail page renders the same groups as its own sidebar. Keeping
// the id/title/description in one place stops the two surfaces from drifting.
// The plugin and secret groups' content (their editors) is injected per surface
// via buildAgentGroups, because it needs that surface's live state.
export function agentGroupMeta(t: Translate<MessageKey>): FormModalGroup[] {
  return [
    { id: "basics", title: t("agents.group.basics") },
    { id: "sandbox", title: t("agents.group.sandbox"), description: t("agents.group.sandboxHint") },
    { id: "plugins", title: t("agents.group.plugins"), description: t("agents.group.pluginsHint") },
    { id: "secrets", title: t("agents.group.secrets"), description: t("agents.group.secretsHint") },
  ]
}

/**
 * buildAgentGroups injects the live plugin and secret editors into their groups,
 * so a FormModal-driven surface (the create dialog) can render them as tab bodies.
 */
export function buildAgentGroups(opts: {
  pluginEditor: ReactNode
  secretEditor: ReactNode
  t: Translate<MessageKey>
}): FormModalGroup[] {
  return agentGroupMeta(opts.t).map((group) => {
    if (group.id === "plugins") return { ...group, content: opts.pluginEditor }
    if (group.id === "secrets") return { ...group, content: opts.secretEditor }
    return group
  })
}
