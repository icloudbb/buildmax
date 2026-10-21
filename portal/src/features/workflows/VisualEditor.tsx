import { useId, useMemo, useState } from "react"
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
import { Button, Field, Input, Select, Textarea } from "@buildmax/gui"
import type { Agent } from "../../lib/types"
import { useT } from "../../i18n"
import { useMediaQuery } from "../../hooks/useMediaQuery"
import { agentOptionLabels } from "../agents/optionLabel"
import { ISSUE_ACCESS_OPTIONS, issueAccessOption, stepExecutorLabel, stepHeading } from "./labels"
import { STEP_LIST_QUERY, WorkflowStepList } from "./StepList"
import {
  HUMAN_INPUT_STEP_TYPE,
  MAX_NODE_ATTEMPTS,
  nodeOutputSource,
  WORKFLOW_INPUT_SOURCE,
  YES_NO_SCHEMA,
  type WorkflowStepDraft,
} from "./steps"
import type { WorkflowStepsState } from "./useWorkflowSteps"

interface StepNodeData extends Record<string, unknown> {
  label: string
  agentName: string
  prompt: string
  issueBadge: string | null
  maxAttempts?: number
  timeoutSeconds?: number
  errorCount: number
}

/** Timeouts are stored in seconds but authored in minutes: the floor is one
 *  minute and real Agent steps run for many. Empty input means no timeout. */
function secondsToMinutesInput(seconds: number | null | undefined): string {
  return seconds ? String(seconds / 60) : ""
}

function minutesInputToSeconds(value: string): number | undefined {
  const trimmed = value.trim()
  if (trimmed === "") return undefined
  return Math.round(Number(trimmed) * 60)
}

/** A compact duration for a node badge: "90s", "30m", "2h", "1d". */
export function formatTimeout(seconds: number): string {
  if (seconds % 86400 === 0) return `${seconds / 86400}d`
  if (seconds % 3600 === 0) return `${seconds / 3600}h`
  if (seconds % 60 === 0) return `${seconds / 60}m`
  return `${seconds}s`
}

type StepNode = Node<StepNodeData, "wfStep">

/** The rendered step box. Handles are the connection points: an incoming edge
 *  lands on the left (a `needs` this step depends on), an outgoing edge leaves
 *  the right (a step that depends on this one). */
function StepNodeComponent({ data, selected }: NodeProps<StepNode>) {
  const t = useT()
  return (
    <div
      className={`wf-node${selected ? " wf-node--selected" : ""}${data.errorCount > 0 ? " wf-node--error" : ""}`}
    >
      <Handle type="target" position={Position.Left} />
      <div className="wf-node__heading">{data.label}</div>
      <div className="wf-node__agent">{data.agentName}</div>
      <div className="wf-node__prompt">{data.prompt || t("workflows.editor.noPrompt")}</div>
      <div className="wf-node__tags">
        {data.issueBadge ? <span className="wf-node__badge">{data.issueBadge}</span> : null}
        {data.maxAttempts && data.maxAttempts > 1 ? (
          <span className="wf-node__badge">{t("workflows.editor.attemptsBadge", { count: data.maxAttempts })}</span>
        ) : null}
        {data.timeoutSeconds ? <span className="wf-node__badge">{t("workflows.editor.timeoutBadge", { duration: formatTimeout(data.timeoutSeconds) })}</span> : null}
        {data.errorCount > 0 ? (
          <span className="wf-node__badge wf-node__badge--error">
            {t("workflows.editor.errorBadge", { count: data.errorCount })}
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
 * definition. On a narrow screen the canvas gives way to the ordered step list,
 * which selects the step the inspector edits.
 */
export function WorkflowVisualEditor({ state, agents, disabled }: WorkflowVisualEditorProps) {
  const t = useT()
  const narrow = useMediaQuery(STEP_LIST_QUERY)
  const parallelId = useId()
  const runTimeoutId = useId()
  const { steps, errors } = state
  const [positions, setPositions] = useState<Record<string, { x: number; y: number }>>({})
  const [selectedId, setSelectedId] = useState<string | null>(null)

  // Rename a step's id and carry its canvas position across, so the node stays
  // put rather than snapping back to the computed layout under its new id.
  function renameStepAndPosition(oldId: string, newId: string) {
    setPositions((prev) => {
      const pos = prev[oldId]
      if (pos === undefined) return prev
      const next = { ...prev, [newId]: pos }
      delete next[oldId]
      return next
    })
    setSelectedId((id) => (id === oldId ? newId : id))
    state.renameStep(oldId, newId)
  }

  const layout = useMemo(() => computeLayout(steps), [steps])
  const agentLabels = useMemo(() => agentOptionLabels(agents), [agents])
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
      steps.map((step, index) => {
        const heading = stepHeading(index, t)
        const executor = stepExecutorLabel(step, agentLabels, t)
        const badge = issueAccessOption(step.issueAccess)?.badge
        return {
          id: step.id,
          type: "wfStep",
          position: positions[step.id] ?? layout[step.id] ?? { x: 0, y: 0 },
          selected: step.id === effectiveSelectedId,
          draggable: !disabled,
          // The node's accessible name is the step's, not its generated id.
          ariaLabel: `${heading}, ${executor}`,
          data: {
            label: heading,
            agentName: executor,
            prompt: step.prompt,
            issueBadge: badge ? t(badge) : null,
            maxAttempts: step.maxAttempts,
            timeoutSeconds: step.timeoutSeconds,
            errorCount: errorCountById[step.id] ?? 0,
          },
        }
      }),
    [steps, positions, layout, effectiveSelectedId, disabled, agentLabels, errorCountById, t],
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

  const selectedIndex = steps.findIndex((s) => s.id === effectiveSelectedId)
  const selectedStep = selectedIndex >= 0 ? steps[selectedIndex] : null

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
            {t("workflows.editor.addStep")}
          </Button>
        ) : null}
        {!disabled ? (
          <Button
            variant="secondary"
            size="compact"
            onClick={() => {
              const id = state.addStep("human")
              setSelectedId(id)
            }}
          >
            {t("workflows.editor.addInputStep")}
          </Button>
        ) : null}
        {!narrow ? (
          <Button variant="tertiary" size="compact" onClick={() => setPositions({})}>
            {t("workflows.editor.relayout")}
          </Button>
        ) : null}
        <div className="wf-visual__parallel">
          <label className="wf-visual__parallel-label" htmlFor={parallelId}>
            {t("workflows.editor.maxParallel")}
          </label>
          <Input
            id={parallelId}
            className="wf-visual__parallel-input"
            type="number"
            min={1}
            placeholder={t("workflows.editor.auto")}
            value={state.maxParallelNodes ?? ""}
            disabled={disabled}
            onChange={(e) => {
              const value = e.target.value.trim()
              state.setMaxParallelNodes(value === "" ? null : Number(value))
            }}
          />
        </div>
        <div className="wf-visual__parallel">
          <label className="wf-visual__parallel-label" htmlFor={runTimeoutId}>
            {t("workflows.editor.runTimeout")}
          </label>
          <Input
            id={runTimeoutId}
            className="wf-visual__parallel-input"
            type="number"
            min={1}
            placeholder={t("workflows.editor.none")}
            value={secondsToMinutesInput(state.runTimeoutSeconds)}
            disabled={disabled}
            onChange={(e) => state.setRunTimeoutSeconds(minutesInputToSeconds(e.target.value) ?? null)}
          />
        </div>
      </div>

      <div className="wf-visual__body">
        {narrow ? (
          <div className="wf-visual__list">
            <p className="wf-visual__narrow-note">{t("workflows.editor.narrowNote")}</p>
            {steps.length === 0 ? (
              <p className="page-activity__empty">{t("workflows.editor.noSteps")}</p>
            ) : (
              <WorkflowStepList
                steps={steps}
                agentLabels={agentLabels}
                label={t("workflows.editor.steps")}
                selectedId={effectiveSelectedId}
                onSelect={setSelectedId}
              />
            )}
          </div>
        ) : (
          <div className="wf-visual__canvas">
            {steps.length === 0 ? (
              <p className="page-activity__empty">{t("workflows.editor.noSteps")}</p>
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
        )}

        <aside className="wf-visual__inspector">
          {selectedStep ? (
            <StepInspector
              key={selectedStep.id}
              state={state}
              agents={agents}
              disabled={disabled}
              step={selectedStep}
              index={selectedIndex}
              agentLabels={agentLabels}
              stepErrors={errors
                .filter((e) => e.index >= 0 && steps[e.index]?.id === selectedStep.id)
                .map((e) => e.message)}
              predecessors={predecessorsOf(steps, selectedStep.id)}
              allSteps={steps}
              onRename={renameStepAndPosition}
            />
          ) : (
            <p className="page-activity__meta">{t("workflows.editor.selectHint")}</p>
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
  /** The step's position in the definition, which names it ("Step 2"). */
  index: number
  agentLabels: Map<string, string>
  stepErrors: string[]
  predecessors: Set<string>
  allSteps: WorkflowStepDraft[]
  onRename: (oldId: string, newId: string) => void
}

/** The editing panel for one selected step: its agent, prompt, Issue access,
 *  and input bindings, then its identifier under a disclosure. A binding may
 *  read the workflow input or a step this one depends on, matching the server
 *  rule. */
function StepInspector({
  state,
  agents,
  disabled,
  step,
  index,
  agentLabels,
  stepErrors,
  predecessors,
  allSteps,
  onRename,
}: StepInspectorProps) {
  const predecessorSteps = allSteps
    .map((s, i) => ({ step: s, index: i }))
    .filter(({ step: s }) => predecessors.has(s.id))
  const human = step.type === HUMAN_INPUT_STEP_TYPE
  const issueAccess = issueAccessOption(step.issueAccess)
  const bindings = step.bindings ?? []
  // The id is edited as a local draft and committed on blur/Enter, so the step's
  // id (and thus this node's identity) changes once per rename rather than on
  // every keystroke.
  const [idDraft, setIdDraft] = useState(step.id)
  const t = useT()
  const [idError, setIdError] = useState<string | null>(null)

  function commitId() {
    const next = idDraft.trim()
    if (next === step.id) {
      setIdError(null)
      return
    }
    if (!next) {
      setIdError(t("workflows.editor.idRequired"))
      return
    }
    if (allSteps.some((s) => s.id !== step.id && s.id === next)) {
      setIdError(t("workflows.editor.idTaken", { id: next }))
      return
    }
    setIdError(null)
    onRename(step.id, next)
  }

  return (
    <div className="wf-inspector">
      <div className="wf-inspector__head">
        <h4 className="wf-inspector__title">
          {stepHeading(index, t)}
          {human ? <span className="wf-inspector__kind">{t("workflows.editor.inputStep")}</span> : null}
        </h4>
        {!disabled ? (
          <Button variant="danger" size="compact" disabled={allSteps.length === 1} onClick={() => state.removeStep(step.id)}>
            {t("workflows.editor.remove")}
          </Button>
        ) : null}
      </div>
      {human ? (
        <HumanStepFields state={state} step={step} disabled={disabled} />
      ) : (
        <>
      <Field label={t("workflows.editor.agent")}>
        <Select
          value={step.targetAgentId}
          disabled={disabled}
          onChange={(e) => state.changeStep(step.id, { targetAgentId: e.target.value })}
        >
          <option value="">{t("workflows.editor.selectAgent")}</option>
          {agents.map((agent) => (
            <option key={agent.id} value={agent.id}>
              {agentLabels.get(agent.id) ?? agent.name}
            </option>
          ))}
        </Select>
      </Field>
      <Field label={t("workflows.editor.prompt")}>
        <Textarea
          rows={4}
          value={step.prompt}
          disabled={disabled}
          onChange={(e) => state.changeStep(step.id, { prompt: e.target.value })}
        />
      </Field>
      <Field label={t("workflows.editor.issueAccess")} hint={issueAccess ? t(issueAccess.hint) : undefined}>
        <Select
          value={step.issueAccess || "none"}
          disabled={disabled}
          onChange={(e) => state.changeStep(step.id, { issueAccess: e.target.value })}
        >
          {ISSUE_ACCESS_OPTIONS.map((option) => (
            <option key={option.value} value={option.value}>
              {t(option.label)}
            </option>
          ))}
          {/* A value only raw JSON could write stays visible rather than
              silently reading as the first option; validation names it. */}
          {issueAccess ? null : <option value={step.issueAccess}>{step.issueAccess}</option>}
        </Select>
      </Field>
      <div className="wf-inspector__row">
        <Field label={t("workflows.editor.attempts")}>
          <Input
            type="number"
            min={1}
            max={MAX_NODE_ATTEMPTS}
            placeholder="1"
            value={step.maxAttempts ?? ""}
            disabled={disabled}
            onChange={(e) => {
              const value = e.target.value.trim()
              // One attempt is the default, so it is stored as absent.
              state.changeStep(step.id, { maxAttempts: value === "" || Number(value) === 1 ? undefined : Number(value) })
            }}
          />
        </Field>
        <Field label={t("workflows.editor.attemptTimeout")}>
          <Input
            type="number"
            min={1}
            placeholder={t("workflows.editor.none")}
            value={secondsToMinutesInput(step.timeoutSeconds)}
            disabled={disabled}
            onChange={(e) => state.changeStep(step.id, { timeoutSeconds: minutesInputToSeconds(e.target.value) })}
          />
        </Field>
      </div>
      <p className="page-activity__meta">{t("workflows.editor.retryNote")}</p>
        </>
      )}
      <Field group label={t("workflows.editor.bindings")} className="workflow-page__step-bindings">
        {bindings.map((binding, bindingIndex) => (
          <div key={bindingIndex} className="workflow-page__binding">
            <Input
              className="workflow-page__binding-name"
              aria-label={t("workflows.editor.bindingName", { n: bindingIndex + 1 })}
              placeholder={t("workflows.editor.namePlaceholder")}
              value={binding.name}
              disabled={disabled}
              onChange={(e) => state.changeBinding(step.id, bindingIndex, { name: e.target.value })}
            />
            <Select
              className="workflow-page__binding-source"
              aria-label={t("workflows.editor.bindingSource", { n: bindingIndex + 1 })}
              value={binding.source}
              disabled={disabled}
              onChange={(e) => state.changeBinding(step.id, bindingIndex, { source: e.target.value })}
            >
              <option value="">{t("workflows.editor.selectSource")}</option>
              <option value={WORKFLOW_INPUT_SOURCE}>{t("workflows.editor.workflowInput")}</option>
              {predecessorSteps.map((predecessor) => (
                <option key={predecessor.step.id} value={nodeOutputSource(predecessor.step.id)}>
                  {t("workflows.editor.stepOutput", { step: stepHeading(predecessor.index, t) })}
                </option>
              ))}
            </Select>
            <Input
              aria-label={t("workflows.editor.bindingPointer", { n: bindingIndex + 1 })}
              placeholder={t("workflows.editor.pointerPlaceholder")}
              value={binding.pointer}
              disabled={disabled}
              onChange={(e) => state.changeBinding(step.id, bindingIndex, { pointer: e.target.value })}
            />
            {!disabled ? (
              <Button variant="danger" size="compact" onClick={() => state.removeBinding(step.id, bindingIndex)}>
                {t("workflows.editor.removeInput")}
              </Button>
            ) : null}
          </div>
        ))}
        {!disabled ? (
          <Button variant="secondary" size="compact" onClick={() => state.addBinding(step.id)}>
            {t("workflows.editor.addInput")}
          </Button>
        ) : null}
      </Field>
      {stepErrors.length > 0 ? (
        <ul className="workflow-page__errors">
          {stepErrors.map((message, i) => (
            <li key={i}>{message}</li>
          ))}
        </ul>
      ) : null}
      {/* The identifier is generated, so it is not part of authoring a step.
          Raw JSON, input bindings, and run diagnostics refer to the step by it,
          so it stays one disclosure away, renamable to something meaningful. */}
      <details className="wf-inspector__advanced" open={idError ? true : undefined}>
        <summary>{t("workflows.editor.advanced")}</summary>
        <Field label={t("workflows.editor.stepId")} hint={t("workflows.editor.stepIdHint")} error={idError}>
          <Input
            value={idDraft}
            disabled={disabled}
            onChange={(e) => setIdDraft(e.target.value)}
            onBlur={commitId}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault()
                e.currentTarget.blur()
              }
            }}
          />
        </Field>
      </details>
    </div>
  )
}

const FREE_TEXT = "free"
const YES_NO = "yes_no"
const CUSTOM = "custom"

/** The fields of an input step: the question a person answers, what kind of
 *  answer it takes, and how long it stays open. A custom answer schema is
 *  authored in raw JSON and shown here as such, not rewritten. */
function HumanStepFields({ state, step, disabled }: { state: WorkflowStepsState; step: WorkflowStepDraft; disabled: boolean }) {
  const t = useT()
  const answerKind =
    step.outputSchema === undefined ? FREE_TEXT : JSON.stringify(JSON.parse(step.outputSchema)) === YES_NO_SCHEMA ? YES_NO : CUSTOM
  return (
    <>
      <Field label={t("workflows.editor.question")}>
        <Textarea
          rows={4}
          value={step.prompt}
          disabled={disabled}
          onChange={(e) => state.changeStep(step.id, { prompt: e.target.value })}
        />
      </Field>
      <Field label={t("workflows.editor.answer")}>
        <Select
          value={answerKind}
          disabled={disabled || answerKind === CUSTOM}
          onChange={(e) => state.changeStep(step.id, { outputSchema: e.target.value === YES_NO ? YES_NO_SCHEMA : undefined })}
        >
          <option value={FREE_TEXT}>{t("workflows.editor.freeText")}</option>
          <option value={YES_NO}>{t("workflows.editor.yesNo")}</option>
          {answerKind === CUSTOM ? <option value={CUSTOM}>{t("workflows.editor.customSchema")}</option> : null}
        </Select>
      </Field>
      <Field label={t("workflows.editor.expiresAfter")}>
        <Input
          type="number"
          min={1}
          placeholder={t("workflows.editor.never")}
          value={secondsToMinutesInput(step.timeoutSeconds)}
          disabled={disabled}
          onChange={(e) => state.changeStep(step.id, { timeoutSeconds: minutesInputToSeconds(e.target.value) })}
        />
      </Field>
      <p className="page-activity__meta">{t("workflows.editor.waitNote")}</p>
    </>
  )
}
