import { useMemo, useState } from "react"
import {
  Background,
  Controls,
  Handle,
  MarkerType,
  Position,
  ReactFlow,
  ReactFlowProvider,
  type Connection,
  type Edge,
  type EdgeChange,
  type Node,
  type NodeChange,
  type NodeProps,
} from "@xyflow/react"
import "@xyflow/react/dist/style.css"
import { Button } from "@buildmax/gui"
import type { Agent } from "../../lib/types"
import { nodeOutputSource, WORKFLOW_INPUT_SOURCE, type WorkflowStepDraft } from "./steps"
import type { WorkflowStepsState } from "./useWorkflowSteps"

const ISSUE_ACCESS_OPTIONS = ["none", "if_bound", "required"]

interface StepNodeData extends Record<string, unknown> {
  label: string
  agentName: string
  prompt: string
  issueAccess?: string
  errorCount: number
}

type StepNode = Node<StepNodeData, "wfStep">

/** The rendered step box. Handles are the connection points: an incoming edge
 *  lands on the left (a `needs` this step depends on), an outgoing edge leaves
 *  the right (a step that depends on this one). */
function StepNodeComponent({ data, selected }: NodeProps<StepNode>) {
  return (
    <div
      className={`wf-node${selected ? " wf-node--selected" : ""}${data.errorCount > 0 ? " wf-node--error" : ""}`}
    >
      <Handle type="target" position={Position.Left} />
      <div className="wf-node__id">{data.label}</div>
      <div className="wf-node__agent">{data.agentName || "No agent selected"}</div>
      <div className="wf-node__prompt">{data.prompt || "No prompt"}</div>
      <div className="wf-node__tags">
        {data.issueAccess && data.issueAccess !== "none" ? (
          <span className="wf-node__badge">issue: {data.issueAccess}</span>
        ) : null}
        {data.errorCount > 0 ? (
          <span className="wf-node__badge wf-node__badge--error">
            {data.errorCount} {data.errorCount === 1 ? "issue" : "issues"}
          </span>
        ) : null}
      </div>
      <Handle type="source" position={Position.Right} />
    </div>
  )
}

const nodeTypes = { wfStep: StepNodeComponent }

/** Column-by-longest-dependency-chain layout, so a node sits to the right of
 *  every node it needs. Positions are UI-only and never leave this component. */
function computeLayout(steps: WorkflowStepDraft[]): Record<string, { x: number; y: number }> {
  const byId = new Map(steps.map((s) => [s.id, s]))
  const depthCache = new Map<string, number>()
  const depthOf = (id: string, stack: Set<string>): number => {
    const cached = depthCache.get(id)
    if (cached !== undefined) return cached
    if (stack.has(id)) return 0
    stack.add(id)
    let depth = 0
    for (const need of byId.get(id)?.needs ?? []) {
      if (byId.has(need)) depth = Math.max(depth, depthOf(need, stack) + 1)
    }
    stack.delete(id)
    depthCache.set(id, depth)
    return depth
  }
  const rowByDepth: Record<number, number> = {}
  const positions: Record<string, { x: number; y: number }> = {}
  for (const step of steps) {
    const depth = depthOf(step.id, new Set())
    const row = rowByDepth[depth] ?? 0
    rowByDepth[depth] = row + 1
    positions[step.id] = { x: depth * 260, y: row * 150 }
  }
  return positions
}

/** The set of steps `id` transitively depends on, following `needs` edges. */
function predecessorsOf(steps: WorkflowStepDraft[], id: string): Set<string> {
  const byId = new Map(steps.map((s) => [s.id, s]))
  const preds = new Set<string>()
  const walk = (current: string) => {
    for (const need of byId.get(current)?.needs ?? []) {
      if (!byId.has(need) || preds.has(need)) continue
      preds.add(need)
      walk(need)
    }
  }
  walk(id)
  return preds
}

interface WorkflowVisualEditorProps {
  state: WorkflowStepsState
  agents: Agent[]
  disabled: boolean
}

/**
 * The visual workflow editor: a React Flow canvas of the step graph beside an
 * inspector for the selected step. `state.steps` is the source of truth for
 * content and `needs` edges; node positions are local UI state. Every mutation
 * goes through the shared step state, so the raw JSON view and Save see the same
 * definition.
 */
export function WorkflowVisualEditor({ state, agents, disabled }: WorkflowVisualEditorProps) {
  const { steps, errors } = state
  const [positions, setPositions] = useState<Record<string, { x: number; y: number }>>({})
  const [selectedId, setSelectedId] = useState<string | null>(null)

  const layout = useMemo(() => computeLayout(steps), [steps])
  const agentName = useMemo(() => new Map(agents.map((a) => [a.id, a.name])), [agents])
  // Fall back to the first step so the inspector is always populated when there
  // is something to edit; an explicit click overrides it.
  const effectiveSelectedId = (selectedId && steps.some((s) => s.id === selectedId) ? selectedId : steps[0]?.id) ?? null

  // Per-step error counts, keyed by step id, so a node can mark itself.
  const errorCountById = useMemo(() => {
    const counts: Record<string, number> = {}
    for (const err of errors) {
      if (err.index < 0 || err.index >= steps.length) continue
      const id = steps[err.index].id
      counts[id] = (counts[id] ?? 0) + 1
    }
    return counts
  }, [errors, steps])

  const nodes = useMemo<StepNode[]>(
    () =>
      steps.map((step) => ({
        id: step.id,
        type: "wfStep",
        position: positions[step.id] ?? layout[step.id] ?? { x: 0, y: 0 },
        selected: step.id === effectiveSelectedId,
        draggable: !disabled,
        data: {
          label: step.id,
          agentName: agentName.get(step.targetAgentId) ?? "",
          prompt: step.prompt,
          issueAccess: step.issueAccess,
          errorCount: errorCountById[step.id] ?? 0,
        },
      })),
    [steps, positions, layout, effectiveSelectedId, disabled, agentName, errorCountById],
  )

  const edges = useMemo<Edge[]>(
    () =>
      steps.flatMap((step) =>
        (step.needs ?? []).map((need) => ({
          id: `${need}=>${step.id}`,
          source: need,
          target: step.id,
          markerEnd: { type: MarkerType.ArrowClosed },
        })),
      ),
    [steps],
  )

  function onNodesChange(changes: NodeChange[]) {
    setPositions((prev) => {
      let next = prev
      for (const change of changes) {
        if (change.type === "position" && change.position) {
          next = { ...next, [change.id]: change.position }
        }
      }
      return next
    })
    if (disabled) return
    for (const change of changes) {
      if (change.type === "remove") {
        state.removeStep(change.id)
        setSelectedId((id) => (id === change.id ? null : id))
      } else if (change.type === "select" && change.selected) {
        setSelectedId(change.id)
      }
    }
  }

  function onEdgesChange(changes: EdgeChange[]) {
    if (disabled) return
    for (const change of changes) {
      if (change.type === "remove") {
        const edge = edges.find((e) => e.id === change.id)
        if (edge) state.disconnectNeed(edge.target, edge.source)
      }
    }
  }

  function onConnect(connection: Connection) {
    if (disabled || !connection.source || !connection.target) return
    if (connection.source === connection.target) return
    // The new edge means target depends on source. That is a cycle only if
    // source already depends on target, so reject it rather than let the graph
    // become one the runtime would reject.
    if (predecessorsOf(steps, connection.source).has(connection.target)) return
    state.connectNeed(connection.target, connection.source)
  }

  const selectedStep = steps.find((s) => s.id === effectiveSelectedId) ?? null

  return (
    <div className="wf-visual">
      <div className="wf-visual__toolbar">
        {!disabled ? (
          <Button
            variant="secondary"
            size="compact"
            onClick={() => {
              const id = state.addStep()
              setSelectedId(id)
            }}
          >
            Add step
          </Button>
        ) : null}
        <Button variant="tertiary" size="compact" onClick={() => setPositions({})}>
          Re-layout
        </Button>
        <label className="wf-visual__parallel">
          <span className="issues-page__field-label">Max parallel</span>
          <input
            className="issues-page__input"
            type="number"
            min={1}
            placeholder="auto"
            value={state.maxParallelNodes ?? ""}
            disabled={disabled}
            onChange={(e) => {
              const value = e.target.value.trim()
              state.setMaxParallelNodes(value === "" ? null : Number(value))
            }}
          />
        </label>
      </div>

      <div className="wf-visual__body">
        <div className="wf-visual__canvas">
          {steps.length === 0 ? (
            <p className="page-activity__empty">No steps yet. Add one to begin.</p>
          ) : (
            <ReactFlowProvider>
              <ReactFlow
                nodes={nodes}
                edges={edges}
                nodeTypes={nodeTypes}
                onNodesChange={onNodesChange}
                onEdgesChange={onEdgesChange}
                onConnect={onConnect}
                onNodeClick={(_, node) => setSelectedId(node.id)}
                onPaneClick={() => setSelectedId(null)}
                nodesConnectable={!disabled}
                elementsSelectable
                fitView
                minZoom={0.2}
              >
                <Background />
                <Controls showInteractive={false} />
              </ReactFlow>
            </ReactFlowProvider>
          )}
        </div>

        <aside className="wf-visual__inspector">
          {selectedStep ? (
            <StepInspector
              key={selectedStep.id}
              state={state}
              agents={agents}
              disabled={disabled}
              step={selectedStep}
              stepErrors={errors
                .filter((e) => e.index >= 0 && steps[e.index]?.id === selectedStep.id)
                .map((e) => e.message)}
              predecessors={predecessorsOf(steps, selectedStep.id)}
              allSteps={steps}
            />
          ) : (
            <p className="page-activity__meta">Select a step to edit it, or drag from a step's right edge to another step's left edge to add a dependency.</p>
          )}
        </aside>
      </div>
    </div>
  )
}

interface StepInspectorProps {
  state: WorkflowStepsState
  agents: Agent[]
  disabled: boolean
  step: WorkflowStepDraft
  stepErrors: string[]
  predecessors: Set<string>
  allSteps: WorkflowStepDraft[]
}

/** The editing panel for one selected step: its agent, prompt, Issue access,
 *  and input bindings. A binding may read the workflow input or a step this one
 *  depends on, matching the server rule. */
function StepInspector({ state, agents, disabled, step, stepErrors, predecessors, allSteps }: StepInspectorProps) {
  const predecessorSteps = allSteps.filter((s) => predecessors.has(s.id))
  const bindings = step.bindings ?? []
  return (
    <div className="wf-inspector">
      <div className="wf-inspector__head">
        <strong>Step</strong>
        <span className="page-activity__meta workflow-page__step-id">id: {step.id}</span>
        {!disabled ? (
          <Button variant="danger" size="compact" disabled={allSteps.length === 1} onClick={() => state.removeStep(step.id)}>
            Remove
          </Button>
        ) : null}
      </div>
      <label className="issues-page__field">
        <span className="issues-page__field-label">Agent</span>
        <select
          className="issues-page__input"
          value={step.targetAgentId}
          disabled={disabled}
          onChange={(e) => state.changeStep(step.id, { targetAgentId: e.target.value })}
        >
          <option value="">Select an agent</option>
          {agents.map((agent) => (
            <option key={agent.id} value={agent.id}>
              {agent.name} ({agent.id})
            </option>
          ))}
        </select>
      </label>
      <label className="issues-page__field">
        <span className="issues-page__field-label">Prompt</span>
        <textarea
          className="issues-page__textarea"
          rows={4}
          value={step.prompt}
          disabled={disabled}
          onChange={(e) => state.changeStep(step.id, { prompt: e.target.value })}
        />
      </label>
      <label className="issues-page__field">
        <span className="issues-page__field-label">Issue access</span>
        <select
          className="issues-page__input"
          value={step.issueAccess ?? "none"}
          disabled={disabled}
          onChange={(e) => state.changeStep(step.id, { issueAccess: e.target.value })}
        >
          {ISSUE_ACCESS_OPTIONS.map((option) => (
            <option key={option} value={option}>
              {option}
            </option>
          ))}
        </select>
      </label>
      <div className="workflow-page__step-bindings">
        <span className="issues-page__field-label">Inputs from the workflow input and steps this one depends on</span>
        {bindings.map((binding, bindingIndex) => (
          <div key={bindingIndex} className="workflow-page__binding">
            <input
              className="issues-page__input"
              aria-label={`Input ${bindingIndex + 1} name`}
              placeholder="name"
              value={binding.name}
              disabled={disabled}
              onChange={(e) => state.changeBinding(step.id, bindingIndex, { name: e.target.value })}
            />
            <select
              className="issues-page__input"
              aria-label={`Input ${bindingIndex + 1} source`}
              value={binding.source}
              disabled={disabled}
              onChange={(e) => state.changeBinding(step.id, bindingIndex, { source: e.target.value })}
            >
              <option value="">Select a source</option>
              <option value={WORKFLOW_INPUT_SOURCE}>Workflow input</option>
              {predecessorSteps.map((predecessor) => (
                <option key={predecessor.id} value={nodeOutputSource(predecessor.id)}>
                  {predecessor.id} output
                </option>
              ))}
            </select>
            <input
              className="issues-page__input"
              aria-label={`Input ${bindingIndex + 1} pointer`}
              placeholder="pointer, e.g. /text (empty = whole value)"
              value={binding.pointer}
              disabled={disabled}
              onChange={(e) => state.changeBinding(step.id, bindingIndex, { pointer: e.target.value })}
            />
            {!disabled ? (
              <Button variant="danger" size="compact" onClick={() => state.removeBinding(step.id, bindingIndex)}>
                Remove input
              </Button>
            ) : null}
          </div>
        ))}
        {!disabled ? (
          <Button variant="secondary" size="compact" onClick={() => state.addBinding(step.id)}>
            Add input
          </Button>
        ) : null}
      </div>
      {stepErrors.map((message, i) => (
        <p key={i} className="modal__error">
          {message}
        </p>
      ))}
    </div>
  )
}
