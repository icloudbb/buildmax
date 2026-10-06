import type { FormModalFieldConfig, FormModalSelectOption, Translate } from "@buildmax/gui"
import type { ApiSecretConsumption } from "../../lib/api/types"
import type { MessageKey } from "../../i18n"
import { agentSandboxFilesystemTierOptions, agentSandboxNetworkTierOptions } from "../../lib/sandboxTiers"

// The empty-value option for the model select: no name means the run uses the
// deployment's default model.
export function deploymentDefaultModelOption(t: Translate<MessageKey>): FormModalSelectOption {
  return { value: "", label: t("agents.field.deploymentDefault") }
}

// The scalar agent fields (everything but the plugin and secret sub-editors),
// the single source both the create dialog and the inline detail-page editor
// render from. `group` keys split the fields into the sections both surfaces
// show in their sidebars (see agentGroupMeta). The model select offers the
// deployment default first, then every model the catalog lists; both editors
// pass the models they fetched, so a deployment with no catalog (or one still
// loading) shows only the default.
export function agentFields(models: string[], t: Translate<MessageKey>): FormModalFieldConfig[] {
  return [
    {
      key: "name",
      label: t("agents.field.name"),
      type: "text",
      placeholder: t("agents.field.namePlaceholder"),
      maxLength: 200,
      group: "basics",
    },
    {
      key: "description",
      label: t("agents.field.description"),
      type: "text",
      placeholder: t("agents.field.descriptionPlaceholder"),
      optional: true,
      maxLength: 500,
      group: "basics",
    },
    {
      key: "instructions",
      label: t("agents.field.instructions"),
      type: "textarea",
      placeholder: t("agents.field.instructionsPlaceholder"),
      optional: true,
      rows: 4,
      group: "basics",
    },
    {
      key: "model",
      label: t("agents.field.model"),
      type: "select",
      optional: true,
      options: [deploymentDefaultModelOption(t), ...models.map((name) => ({ value: name, label: name }))],
      group: "basics",
    },
    {
      key: "sandbox_network_tier",
      label: t("agents.field.network"),
      type: "select",
      optional: true,
      options: agentSandboxNetworkTierOptions(t),
      group: "sandbox",
    },
    {
      key: "sandbox_filesystem_tier",
      label: t("agents.field.filesystem"),
      type: "select",
      optional: true,
      options: agentSandboxFilesystemTierOptions(t),
      group: "sandbox",
    },
  ]
}

export interface AgentDefinitionInput {
  name: string
  description?: string
  instructions?: string
  model?: string
  plugins?: string[]
  sandbox_network_tier?: string
  sandbox_filesystem_tier?: string
  secret_consumption?: ApiSecretConsumption
}

/**
 * normalizeConsumption drops incomplete grants -- a row with no secret chosen,
 * or a selected item with no variable name -- so a half-filled row does not
 * reach the API. An empty result is sent as an empty object, which clears the
 * agent's consumption (create and PATCH both replace the whole definition).
 */
export function normalizeConsumption(c: ApiSecretConsumption): ApiSecretConsumption {
  const env = (c.env ?? []).filter((g) => {
    if (!g.secret) return false
    if (g.item) return Boolean(g.env_name)
    return true
  })
  return { env }
}

/**
 * buildAgentDefinition assembles the create/update body from the scalar field
 * values plus the two sub-editors' state, so the trimming and normalization
 * rules live in one place. Returns null when the required name is empty, which
 * a caller treats as "do not submit". `plugins` is always sent as the array
 * (empty clears it) because both create and PATCH replace the whole definition.
 */
export function buildAgentDefinition(
  values: Record<string, string>,
  plugins: string[],
  consumption: ApiSecretConsumption,
): AgentDefinitionInput | null {
  const name = values.name?.trim()
  if (!name) return null
  return {
    name,
    description: values.description?.trim() || undefined,
    instructions: values.instructions?.trim() || undefined,
    model: values.model || undefined,
    plugins,
    sandbox_network_tier: values.sandbox_network_tier || undefined,
    sandbox_filesystem_tier: values.sandbox_filesystem_tier || undefined,
    secret_consumption: normalizeConsumption(consumption),
  }
}
