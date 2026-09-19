import { useEffect, useState } from "react"
import { Button, type FormModalFieldConfig } from "@buildmax/gui"
import type { Agent } from "../lib/types"
import type { ApiSecret, ApiSecretConsumption } from "../lib/api/types"
import { AGENT_GROUP_META, agentFields, buildAgentDefinition, type AgentDefinitionInput } from "../features/agents"
import { SecretConsumptionEditor } from "./SecretConsumptionEditor"
import { PluginSelectionEditor } from "./PluginSelectionEditor"

interface AgentConfigFormProps {
  agent: Agent
  secrets: ApiSecret[]
  availablePlugins: string[]
  availableModels: string[]
  canManage: boolean
  saving: boolean
  deleting: boolean
  error: string | null
  onSave: (definition: AgentDefinitionInput) => void
  onDelete: () => void
}

function seedValues(agent: Agent): Record<string, string> {
  return {
    name: agent.name,
    description: agent.description ?? "",
    instructions: agent.instructions ?? "",
    model: agent.model ?? "",
    sandbox_network_tier: agent.sandboxNetworkTier ?? "",
    sandbox_filesystem_tier: agent.sandboxFilesystemTier ?? "",
  }
}

/**
 * The agent's configuration as an inline page form — the same field set and
 * assembly rules the create dialog uses (via AGENT_FIELDS / buildAgentDefinition),
 * rendered for the detail page's Configuration tab instead of a modal. Seeding is
 * keyed on the agent's revision, so a save or restore re-seeds to the saved
 * values while an in-progress edit is never clobbered by an unrelated re-render.
 */
export function AgentConfigForm({
  agent,
  secrets,
  availablePlugins,
  availableModels,
  canManage,
  saving,
  deleting,
  error,
  onSave,
  onDelete,
}: AgentConfigFormProps) {
  const [values, setValues] = useState<Record<string, string>>(() => seedValues(agent))
  const [plugins, setPlugins] = useState<string[]>(agent.plugins ?? [])
  const [consumption, setConsumption] = useState<ApiSecretConsumption>(agent.secretConsumption ?? {})
  const [activeGroup, setActiveGroup] = useState<string>(AGENT_GROUP_META[0]?.id ?? "basics")

  useEffect(() => {
    setValues(seedValues(agent))
    setPlugins(agent.plugins ?? [])
    setConsumption(agent.secretConsumption ?? {})
    // Re-seed only when the authoritative version changes (save / restore),
    // never on every keystroke-driven re-render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agent.id, agent.revision])

  function setField(key: string, value: string) {
    setValues((prev) => ({ ...prev, [key]: value }))
  }

  function handleSubmit() {
    const definition = buildAgentDefinition(values, plugins, consumption)
    if (definition == null) return
    onSave(definition)
  }

  function handleDelete() {
    if (
      window.confirm(
        "Delete this agent? It leaves the space and cannot be restored. Runs, tasks, and history that already reference it stay readable.",
      )
    ) {
      onDelete()
    }
  }

  const disabled = !canManage || saving

  function renderField(field: FormModalFieldConfig) {
    const value = values[field.key] ?? ""
    return (
      <div key={field.key} className="agent-config__field">
        <label className="modal__label" htmlFor={`agent-cfg-${field.key}`}>
          {field.label}
          {field.optional ? <span className="modal__optional"> (optional)</span> : null}
        </label>
        {field.type === "textarea" ? (
          <textarea
            id={`agent-cfg-${field.key}`}
            className="modal__textarea"
            placeholder={field.placeholder}
            value={value}
            rows={field.rows ?? 4}
            maxLength={field.maxLength}
            disabled={disabled}
            onChange={(e) => setField(field.key, e.target.value)}
          />
        ) : field.type === "select" ? (
          <>
            <select
              id={`agent-cfg-${field.key}`}
              className="modal__input"
              value={value}
              disabled={disabled}
              onChange={(e) => setField(field.key, e.target.value)}
            >
              {(field.options ?? []).map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
            {field.options?.find((o) => o.value === value)?.description ? (
              <p className="modal__hint">{field.options.find((o) => o.value === value)?.description}</p>
            ) : null}
          </>
        ) : (
          <input
            id={`agent-cfg-${field.key}`}
            type="text"
            className="modal__input"
            placeholder={field.placeholder}
            value={value}
            maxLength={field.maxLength}
            autoComplete="off"
            disabled={disabled}
            onChange={(e) => setField(field.key, e.target.value)}
          />
        )}
      </div>
    )
  }

  const fields = agentFields(availableModels)
  const nameEmpty = !values.name?.trim()
  const group = AGENT_GROUP_META.find((g) => g.id === activeGroup) ?? AGENT_GROUP_META[0]

  // The active section's body. Basics and Sandbox are plain fields split by
  // group; Plugins and Secrets are their own sub-editors.
  function renderPanel() {
    switch (group?.id) {
      case "plugins":
        return <PluginSelectionEditor value={plugins} onChange={setPlugins} available={availablePlugins} />
      case "secrets":
        return <SecretConsumptionEditor value={consumption} onChange={setConsumption} secrets={secrets} />
      default:
        return fields.filter((f) => f.group === group?.id).map(renderField)
    }
  }

  return (
    <div className="agent-config">
      <div className="agent-config__body">
        <nav className="agent-config__nav" aria-label="Configuration sections">
          {AGENT_GROUP_META.map((g) => (
            <button
              key={g.id}
              type="button"
              className={
                g.id === group?.id
                  ? "agent-config__nav-item agent-config__nav-item--active"
                  : "agent-config__nav-item"
              }
              aria-current={g.id === group?.id}
              onClick={() => setActiveGroup(g.id)}
            >
              {g.title ?? g.id}
            </button>
          ))}
        </nav>
        <div className="agent-config__panel">
          {group?.description ? <p className="modal__hint">{group.description}</p> : null}
          {renderPanel()}
        </div>
      </div>

      {error ? (
        <p className="modal__error" role="alert">
          {error}
        </p>
      ) : null}

      {canManage ? (
        <div className="agent-config__actions">
          <Button variant="primary" busy={saving} disabled={disabled || deleting || nameEmpty} onClick={handleSubmit}>Save changes</Button>
          <Button variant="danger" busy={deleting} disabled={saving} onClick={handleDelete}>Delete agent</Button>
        </div>
      ) : (
        <p className="page-activity__empty">This agent is read-only for your role.</p>
      )}
    </div>
  )
}
