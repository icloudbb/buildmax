import type { FormModalSelectOption, Translate } from "@buildmax/gui"
import type { MessageKey } from "../i18n"

// Mirrors config.SandboxNetworkTier / config.SandboxFilesystemTier in the Go
// backend. See docs/design/agent-sandbox-policy.md.

type T = Translate<MessageKey>

function networkTiers(t: T): FormModalSelectOption[] {
  return [
    { value: "none", label: t("agents.sandbox.network.none"), description: t("agents.sandbox.network.noneHint") },
    {
      value: "registries",
      label: t("agents.sandbox.network.registries"),
      description: t("agents.sandbox.network.registriesHint"),
    },
    { value: "open", label: t("agents.sandbox.network.open"), description: t("agents.sandbox.network.openHint") },
  ]
}

function filesystemTiers(t: T): FormModalSelectOption[] {
  return [
    { value: "workspace", label: t("agents.sandbox.fs.workspace"), description: t("agents.sandbox.fs.workspaceHint") },
    {
      value: "workspace_plus_shared_read",
      label: t("agents.sandbox.fs.sharedRead"),
      description: t("agents.sandbox.fs.sharedReadHint"),
    },
    {
      value: "workspace_plus_external_write",
      label: t("agents.sandbox.fs.externalWrite"),
      description: t("agents.sandbox.fs.externalWriteHint"),
    },
  ]
}

/** An agent that declares nothing inherits the space's default, and only then
 * falls through to the strictest baseline -- so the first option here is
 * "inherit," not a hardcoded tier. */
export function agentSandboxNetworkTierOptions(t: T): FormModalSelectOption[] {
  return [
    { value: "", label: t("agents.sandbox.spaceDefault"), description: t("agents.sandbox.spaceDefaultNetworkHint") },
    ...networkTiers(t),
  ]
}

export function agentSandboxFilesystemTierOptions(t: T): FormModalSelectOption[] {
  return [
    { value: "", label: t("agents.sandbox.spaceDefault"), description: t("agents.sandbox.spaceDefaultFilesystemHint") },
    ...filesystemTiers(t),
  ]
}

/** A space's own default has no further tier to inherit from -- leaving it
 * unset means the strictest baseline applies to every agent that declares
 * nothing. */
export function spaceSandboxNetworkTierOptions(t: T): FormModalSelectOption[] {
  return [
    { value: "", label: t("agents.sandbox.noDefault"), description: t("agents.sandbox.noDefaultNetworkHint") },
    ...networkTiers(t),
  ]
}

export function spaceSandboxFilesystemTierOptions(t: T): FormModalSelectOption[] {
  return [
    { value: "", label: t("agents.sandbox.noDefault"), description: t("agents.sandbox.noDefaultFilesystemHint") },
    ...filesystemTiers(t),
  ]
}
