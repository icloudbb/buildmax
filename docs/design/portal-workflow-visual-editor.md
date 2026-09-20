# Portal Workflow Visual Editor

> **简体中文：** [阅读中文镜像](../zh-CN/design/portal-workflow-visual-editor.md)
>
> **Audience:** Portal contributors · **Status:** active plan — partial
>
> **Opened:** 2026-09-20

Related: [Portal architecture](../contribute/architecture/portal.md),
[workflow runtime](workflow-runtime.md),
[Portal frontend page system](portal-frontend-page-system.md).

## Contents

- [Decision](#decision)
- [User outcome, evidence, and constraints](#user-outcome-evidence-and-constraints)
- [Data model and round-trip](#data-model-and-round-trip)
- [The visual editor](#the-visual-editor)
- [Dependency](#dependency)
- [Options and non-goals](#options-and-non-goals)
- [Verification](#verification)

## Decision

Should the Portal workflow editor let a user author a real dependency graph
visually, instead of only a linear chain of step cards with a raw-JSON escape
hatch for anything branching? **Yes.** The editing surface becomes two modes: a
**visual** node-and-edge canvas and a **raw JSON** view, both driven by one draft
state and validated by the one `validateSteps`. The structured step-card form is
retired; its per-node fields move into the visual editor's node inspector.

A workflow definition is a DAG of `agent_task` nodes joined by `needs` edges
(see [workflow runtime](workflow-runtime.md)). The previous form could only
express a straight line — a node depended on the one above it — so any fan-out or
join had to be typed as JSON. The visual editor makes the graph the primary
authoring object.

## User outcome, evidence, and constraints

A Space member editing a workflow can see its steps as a graph, add a step,
connect one step's completion to another's start, and edit a step's agent,
instruction, Issue access, and input bindings, without hand-writing JSON. A
branching plan (two steps that both feed a third) is authored by drawing edges,
not by knowing the `needs` array syntax. Raw JSON remains for exact inspection
and for fields the visual surface does not yet edit.

Constraints: the definition contract is unchanged and server-owned; the editor
only produces the same JSON the runtime already validates. Layout is not part of
the contract — node positions are a UI concern and are never written into the
definition. Portal keeps its own composition; `@buildmax/gui` stays neutral.

## Data model and round-trip

`steps.ts` remains the single serialize/parse/validate authority, and
`useWorkflowSteps` remains the single draft state. Two changes make it lossless
and graph-first:

- **Explicit edges.** On load, every node's `needs` is normalized to its
  effective value, so the graph is unambiguous and every edge operation is a
  plain array edit. The linear "a node with no `needs` depends on the one above
  it" default remains only as the reader for an unedited legacy definition.
- **Lossless preservation.** The draft now carries a node's `output_schema` and
  the definition's `input_schema` and `result` verbatim through parse and
  serialize, so switching a definition that declares them into the visual editor
  and saving no longer strips them. They are authored through raw JSON; the
  visual node inspector edits agent, instruction, Issue access, and bindings.

## The visual editor

The canvas renders each step as a node (its id, agent, a prompt snippet, its
Issue-access badge, and an error marker) and each `needs` entry as an edge into
the dependent node. `steps` is the source of truth for content and edges;
React Flow node **positions** are UI-only local state, seeded from a
depth-column auto-layout and adjustable by dragging, never persisted to the
definition. Interactions map to draft mutations: connecting an edge adds a
`needs` entry, deleting an edge removes it, deleting a node removes the step and
any edge that referenced it, and adding a node appends a root step the user then
connects. Selecting a node opens an inspector for its fields. A connection that
would create a cycle, a missing agent, or an empty prompt surfaces through the
same validation that gates Save. `max_parallel_nodes` is a workflow-level
control beside the canvas.

## Dependency

The canvas uses **`@xyflow/react`** (React Flow, MIT) — the established
React library for node-based editors — rather than extending the hand-rolled
read-only `WorkflowGraph` SVG with drag, edge creation, and selection. The
read-only run and plan graphs keep using `WorkflowGraph`; only the editor takes
the dependency.

## Options and non-goals

| Option | Trade-off |
|---|---|
| Keep the linear card form plus raw JSON | No new dependency, but branching DAGs stay JSON-only — the demonstrated gap. |
| **Visual canvas plus raw JSON (recommended)** | One graph-first authoring object; adds a focused, standard dependency. |
| Hand-roll drag/connect on `WorkflowGraph` | No dependency, but reinvents a mature interaction model at high cost and risk. |

Non-goals for this slice: dedicated form controls for `input_schema`, `result`,
and `output_schema` (preserved losslessly, authored in raw JSON); persisted
node positions; new node types beyond `agent_task`; and any change to the
definition contract or run model.

## Verification

`./make check portal` (lint, types, unit), `steps.ts` round-trip unit tests for
the newly preserved fields and explicit-edge normalization, and the workflow
browser spec extended to author a branch visually. Manual review at the
reference widths in both themes.
