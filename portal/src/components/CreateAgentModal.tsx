import { useEffect, useMemo, useState } from "react"
import { FormModal } from "@buildmax/gui"
import type { ApiSecret, ApiSecretConsumption } from "../lib/api/types"
import type { Agent } from "../lib/types"
import {
  agentFields,
  buildAgentDefinition,
  buildAgentGroups,
  createAgent,
  listAgentModels,
  type AgentDefinitionInput,
} from "../features/agents"
import { listSecrets } from "../features/spaceSecrets/api"
import { listActivations } from "../features/spacePlugins/api"
import { listPlugins } from "../features/plugins/api"
import { nameablePlugins } from "../features/plugins/nameablePlugins"
import { apiAgentToAgent } from "../lib/api/mappers"
import { getErrorMessage } from "../lib/errorMessage"
import { SecretConsumptionEditor } from "./SecretConsumptionEditor"
import { PluginSelectionEditor } from "./PluginSelectionEditor"
import { useT } from "../i18n"

interface CreateAgentModalProps {
  open: boolean
  token: string | null
  spaceId: string
  onClose: () => void
  /** The created Agent; the caller decides where the person goes next. */
  onCreated: (agent: Agent) => void
}

/**
 * Creates a Space Agent. It loads its own options and owns the request, so
 * Agents and the Issue pages open the same dialog: an Issue with nothing to
 * run it creates its Agent in place instead of sending the person away.
 */
export function CreateAgentModal({ open, token, spaceId, onClose, onCreated }: CreateAgentModalProps) {
  const t = useT()
  const [consumption, setConsumption] = useState<ApiSecretConsumption>({})
  const [plugins, setPlugins] = useState<string[]>([])
  const [secrets, setSecrets] = useState<ApiSecret[]>([])
  const [availablePlugins, setAvailablePlugins] = useState<string[]>([])
  // null until listed: the model field is built from it.
  const [availableModels, setAvailableModels] = useState<string[] | null>(null)
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Only owners and admins open this dialog, and they may list each of these.
  // A failed list leaves its picker empty rather than blocking creation.
  useEffect(() => {
    if (!open || !token || !spaceId) return
    let cancelled = false
    setError(null)
    const secretsLoad = listSecrets(token, spaceId).then((res) => res.secrets ?? [])
    const pluginsLoad = Promise.all([
      listActivations(token, spaceId).catch(() => null),
      listPlugins(token).catch(() => null),
    ]).then(([activations, catalog]) => nameablePlugins(activations, catalog?.plugins ?? null))
    void secretsLoad.catch(() => []).then((list) => {
      if (!cancelled) setSecrets(list)
    })
    void pluginsLoad.catch(() => []).then((list) => {
      if (!cancelled) setAvailablePlugins(list)
    })
    return () => {
      cancelled = true
    }
  }, [open, token, spaceId])

  // The deployment's model catalog does not change between openings, so it is
  // listed once; an empty list leaves the picker at the deployment default.
  const modelsListed = availableModels !== null
  useEffect(() => {
    if (!open || !token || modelsListed) return
    let cancelled = false
    void listAgentModels(token)
      .catch(() => [])
      .then((list) => {
        if (!cancelled) setAvailableModels(list)
      })
    return () => {
      cancelled = true
    }
  }, [open, token, modelsListed])

  // FormModal clears what was typed whenever it receives new fields, so they
  // keep their identity across re-renders, and the dialog waits for the model
  // list rather than rebuilding its fields under the person typing.
  const fields = useMemo(() => agentFields(availableModels ?? [], t), [availableModels, t])
  const groups = buildAgentGroups({
    t,
    pluginEditor: (
      <PluginSelectionEditor value={plugins} onChange={setPlugins} available={availablePlugins} />
    ),
    secretEditor: (
      <SecretConsumptionEditor value={consumption} onChange={setConsumption} secrets={secrets} />
    ),
  })

  function create(definition: AgentDefinitionInput) {
    if (!token || !spaceId) return
    setCreating(true)
    setError(null)
    createAgent(spaceId, definition, token)
      .then((created) => {
        setConsumption({})
        setPlugins([])
        onCreated(apiAgentToAgent(created))
      })
      .catch((err) => setError(getErrorMessage(err, t("agents.error.create"))))
      .finally(() => setCreating(false))
  }

  return (
    <FormModal
      open={open && modelsListed}
      title={t("agents.createModal.title")}
      titleId="create-agent-title"
      fields={fields}
      groups={groups}
      layout="tabs"
      hint={t("agents.createModal.hint")}
      loading={creating}
      error={error}
      submitLabel={t("agents.create")}
      onClose={() => {
        setError(null)
        onClose()
      }}
      onSubmit={(values) => {
        const definition = buildAgentDefinition(values, plugins, consumption)
        if (definition == null) return
        create(definition)
      }}
    />
  )
}
