# Local Issue Work Bridge

> **简体中文：** [阅读中文镜像](../zh-CN/proposals/local-issue-work-bridge.md)
>
> **Audience:** contributors, product reviewers, operators, and early adopters · **Status:** proposal — under discussion
>
> **Opened:** 2026-08-22

Related: [roadmap](../ROADMAP.md),
[surface positioning](../design/surface-positioning.md),
[data model](../contribute/architecture/data-model.md),
[unified artifacts](../design/unified-artifacts.md),
[Desktop architecture](../contribute/architecture/desktop.md),
[CLI architecture](../contribute/architecture/cli.md),
[Issue agent access](../design/issue-agent-access.md),
[Agent bridge CLI](../design/agent-bridge-cli.md), and the
[durable Agent sessions proposal](durable-agent-sessions.md).

## Contents

- [Decision Question](#decision-question)
- [Problem And Current Context](#problem-and-current-context)
- [Current Foundations](#current-foundations)
- [User Outcomes](#user-outcomes)
- [Goals](#goals)
- [Non-Goals](#non-goals)
- [Ownership And Authority](#ownership-and-authority)
- [Surface Boundary](#surface-boundary)
- [Options And Trade-Offs](#options-and-trade-offs)
- [Candidate Product Decisions](#candidate-product-decisions)
- [Candidate First Slice](#candidate-first-slice)
- [Offline And Conflict Semantics](#offline-and-conflict-semantics)
- [Model, Data, And Trust Boundary](#model-data-and-trust-boundary)
- [Relationship To Durable Agent Sessions](#relationship-to-durable-agent-sessions)
- [Delivery Phases](#delivery-phases)
- [Open Questions And Evidence Needed](#open-questions-and-evidence-needed)
- [Likely Destination If Accepted](#likely-destination-if-accepted)

## Decision Question

How should an authenticated CLI/TUI or Desktop client receive, execute,
decompose, delegate, and report work from a Space Issue without becoming a local
copy of Portal or making a BuildMax Server a requirement for local use?

The likely direction is:

> Issue is the Server-owned work object shared across surfaces. CLI and Desktop
> are local execution clients for that work; Portal remains its complete
> management surface. A local Agent Session and a remote TaskRun may both work
> on an Issue, but they retain distinct identity, lifecycle, authority, and
> execution boundaries.

Roadmap R5 item 1 schedules this decision and, if accepted, its remaining first
slice. That is not acceptance of the capability: this proposal makes the
product boundary and minimum useful bridge concrete enough to accept, change,
or reject before implementation continues.

## Problem And Current Context

BuildMax already has two valuable operating profiles:

- CLI/TUI and Desktop run the shared Agent Core against a local workspace and
  remain useful with no BuildMax Server;
- Server, Portal, and workers add Space work, Issues, Workflows, durable
  background execution, shared results, and governance.

Keeping the local surfaces independent is the right execution boundary. Keeping
them unaware of Space work is not. In a private deployment, that leaves a person
copying an Issue description into a local prompt, recreating its decomposition
in personal notes, and pasting the result back into Portal. The Space cannot see
which work is local, remote, delegated, blocked, or finished without asking the
person to maintain a second manual trail.

The opposite direction is also wrong. Rebuilding the Portal board, Workflow
editor, Space administration, quota, audit, and cloud file management inside
Desktop would create two management products and weaken the local workbench.
Putting those features into the CLI would make a direct terminal executor feel
like a remote administration client.

The missing product loop is narrower:

```text
                   Server-owned Issue
                  /                  \
       local Agent Session       Task / TaskRun
       CLI or Desktop            Worker execution
                  \                  /
               comments, artifacts, status,
                 and execution references
```

The Issue is the shared work protocol. The execution objects on either side do
not become the same object merely because they contribute to the same outcome.

## Current Foundations

Several required pieces already exist:

- An Issue belongs to a Space, may have an owner (a person) and an executor
  (an Agent or Workflow) independently, and may have one level of child Issues.
- Issue comments form a durable human- and Agent-readable work thread.
- Tasks carry an optional Issue ID, so remote execution is already attributable
  to an Issue.
- CLI and Desktop can authenticate to a Server without moving their Agent loop
  off the local machine.
- Managed models let a connected local client use the deployment's model
  catalog without holding a provider credential.
- An authenticated local Agent can publish a unified Artifact to the Server.
- The current surface-positioning decision already permits an owned-work inbox,
  starting a local Session from an Issue, and returning results.
- `buildmax issue list` shows what the signed-in person owns, across every
  space they are in. The server-side listing filters it needs — by owner and by
  status — now exist; `openapi.json` had described them for a while before
  anything implemented them.
- `buildmax issue start <id>` scopes one local session to one Issue: the Agent can
  read it and report back. The report is stored as `local_agent`, a claim the
  relaying person is accountable for, never as the `agent` a worker run writes.
  It scopes one run and remembers nothing — the durable `IssueLink` below is
  still this proposal's to design.
- Starting such a session prints the server, space, Issue, and where prompts go
  before the first model call, which is the visibility rule under
  [Model, Data, And Trust Boundary](#model-data-and-trust-boundary) in its
  cheapest form. The Session header that shows sync state as well is still
  unbuilt.
- `buildmax issue show` reads one Issue, `buildmax issue comment` posts a
  report on it, and `buildmax issue status` moves it.
  Status stays a person's action, per
  [Status Is A Space Statement, Not Presence](#status-is-a-space-statement-not-presence),
  and the change carries the version it was read at.
- An Agent reads and reports on the Issue it is working by running
  `buildmax issue show` and `buildmax issue comment` through Bash, per
  [Agent bridge CLI](../design/agent-bridge-cli.md); the product boundary —
  status, assignment, and hierarchy never Agent-writable — stays in
  [Issue agent access](../design/issue-agent-access.md). Locally those
  commands run with the person's own credential and take an Issue id; this
  proposal does not redesign that boundary.

The authenticated Issue client the local interfaces need exists
(`internal/interface/client`). The missing pieces are a durable or explicitly
local relation between an Issue and a local Session — `buildmax issue start`
scopes one session and is not remembered — the mapping from an Issue to a
local workspace, and product semantics for status, offline work, local result
publication, model policy, and conflicts.

## User Outcomes

A connected local user should be able to:

1. See work assigned to them without browsing the full Space board.
2. Open an Issue and choose the local workspace in which to handle it.
3. Start a local Agent Session with an explicit, inspectable snapshot of the
   selected Issue context.
4. Keep local files, tools, approvals, and execution on the local machine.
5. Break the current Issue into child Issues and, with confirmation, assign or
   delegate those children through the Server.
6. Observe related remote execution and consume its results from the local
   workbench.
7. Return a bounded summary, comments, Artifacts, and an explicit status update
   to the Issue.
8. Continue ordinary local work when no Server exists or when no Issue is
   linked.

A Space should be able to open the Issue in Portal and understand what was
assigned, what ran remotely, what a person handled locally, and what result was
returned, without Portal claiming to control the person's machine.

## Goals

- Make Issue the canonical cross-surface work object.
- Let local and remote execution contribute to one Issue without conflating
  their lifecycles.
- Support contextual decomposition, assignment, delegation, tracking, and
  result return from CLI/TUI and Desktop.
- Keep Portal the complete Space management and governance surface.
- Preserve direct local use with no Server, account, Space, or network.
- Make Server destination, Space, model transport, sync state, and data movement
  visible before work crosses a boundary.
- Establish a small first slice that does not depend on full Session sync.

## Non-Goals

- Rebuilding the Portal Issue board or administration navigation in Desktop.
- Adding Workflow authoring, Space membership, role, quota, audit, or deployment
  administration to CLI or Desktop.
- Treating a local Agent Session as a Worker TaskRun.
- Letting the Server claim it can stop, resume, or inspect a local process when
  the client has not implemented that contract.
- Uploading a local workspace automatically or treating Space files and a local
  directory as synchronized copies.
- Synchronizing complete local Session content in the first bridge slice.
- Making connected mode mandatory for the open-source local product.
- Claiming that a locally reported result is tamper-proof audit evidence.

## Ownership And Authority

The bridge depends on one clear owner for each kind of fact:

| Object or fact | Authority | Local surface role |
|---|---|---|
| Issue title, description, status, owner, executor, hierarchy, comments | Server | Read and perform authorized contextual mutations |
| Local workspace and path mapping | Local client | Choose, persist locally, and never imply Server possession |
| Local Session messages, tool state, approvals, and live process | Local client | Execute and persist under the existing local contract |
| Task and TaskRun lifecycle | Server and Worker | Trigger or observe; never impersonate a Worker |
| Space Artifact metadata and content | Server | Publish explicitly selected output and retain the returned reference |
| Issue-to-local-Session relation | Open decision | Keep locally first or register bounded metadata on Server |
| Issue work status | Server | Change only through an explicit authorized action |
| Local execution presence | Local client | Do not derive Issue status from a process heartbeat |

Opening an Issue must not silently claim it, change its status, send its
contents to a model, or upload local data. Those are separate user-visible
actions.

## Surface Boundary

Sharing the Issue object does not require interface parity:

| Capability | CLI/TUI | Desktop | Portal |
|---|---:|---:|---:|
| Assigned-work inbox | Command or panel | First-class view | Full filters and board |
| Current Issue detail, children, comments | Compact | Rich contextual view | Full detail and history |
| Start local Session from Issue | Yes | Yes | Handoff or launcher |
| Update current Issue status | Explicit command/action | Explicit action | Full editing |
| Create child Issue under current Issue | Explicit, confirmed | Contextual flow | Full editing |
| Assign or delegate a child | Explicit, confirmed | Contextual flow | Full editing |
| Observe related TaskRuns and results | Compact | Selected subset | Full drill-down |
| Publish summary or Artifact | Yes | Yes | View and manage |
| Browse and reorganize the whole Space board | No | No | Yes |
| Author Workflows or administer Space policy | No | No | Yes |

The local product promise becomes:

> Receive Space work, execute it locally, coordinate related work, and return a
> result.

It does not become:

> Administer the Space operating system from every client.

## Options And Trade-Offs

| Option | Strength | Main concern |
|---|---|---|
| Keep local sessions isolated; copy results manually | Smallest product and protocol | Breaks enterprise continuity, provenance, decomposition, and tracking |
| Rebuild Portal Issue management in Desktop | Feature parity and one native UI | Duplicates product ownership and dilutes the local workbench |
| Treat every local Session as a TaskRun | Reuses server execution records | Makes false claims about scheduling, process control, approvals, and availability |
| Add a contextual Issue bridge around the local runtime | Preserves local execution while closing the Space work loop | Requires explicit relation, sync, policy, and conflict semantics |
| Make the Server canonical for a live local event stream | Strong central visibility | Makes network and Server ingestion part of local correctness |

The likely direction is the contextual Issue bridge. A live event stream may
exist later as an explicit enterprise capture policy; it is not the default
meaning of connected local work.

## Candidate Product Decisions

### Issue Is The Shared Work Object

An authenticated local surface consumes the same Issue IDs and Space
authorization as Portal. It does not create a parallel local Issue database.
Local caching is a view and an offline aid, never a second authority.

### Local Session And TaskRun Stay Distinct

A Local Session is interactive, machine-owned, approval-capable, and possibly
offline. A TaskRun is Server-created, Worker-executed, and durably scheduled.
Both may relate to one Issue, and one Issue may have several of either.

The first version should link a local Session to at most one Issue at a time.
This keeps context and result attribution legible. Starting work on a different
Issue creates or forks a Session instead of silently reassigning its history.

### Local Issue Actions Are Contextual

CLI and Desktop may modify the current Issue and its immediate children when
the authenticated user has permission. Broad board management remains in
Portal. Remote mutations require an explicit user action or an ordinary Agent
tool approval; merely opening or discussing an Issue changes nothing.

Which of those the Agent may do at all is settled:
[Issue agent access](../design/issue-agent-access.md) gives it a bounded comment
and nothing else. Status, assignment, hierarchy, and child creation are user
actions in this proposal's surfaces, not tool calls.

### Decomposition Crosses Execution Planes

From a linked Session, a person may create child Issues and assign them to a
person, Agent, or Workflow. Starting the relevant remote flow remains a Server
operation. Remote results return through durable Issue, TaskRun, comment, and
Artifact state; they do not depend on the originating local process staying
open.

### Issue Context Is A Visible Snapshot

Starting a Session should build a bounded Issue context snapshot: title,
description, relevant hierarchy, selected comments, and selected Artifact
references. The UI shows what will be included and where the selected model
sends it. It does not append an unbounded comment thread or automatically fetch
every Artifact into the model context.

The snapshot records the Issue update time or future revision token so the
Session can later say its starting context is stale. It remains input, not a
live synchronized prompt.

### Workspace Mapping Is Local

The first launch from an Issue asks for a local directory. A mapping may be
remembered by deployment, Space, and a future repository or workspace identity,
but the Server does not infer a local path and the client does not upload the
directory as a side effect.

### Status Is A Space Statement, Not Presence

`in_progress` means the Space says work is in progress. It does not mean a local
process is alive. Starting a local Session may offer to set the status and
owner, but the user confirms the mutation. Losing the client connection does
not move the Issue back or mark it failed.

If the product later needs live local execution presence, that belongs in a
separate execution record with honest `last_seen` semantics.

### Result Return Is Explicit And Bounded

A local Session may post a user-authored summary, publish selected Artifacts,
and propose a status change. Complete transcripts, traces, diffs, and workspace
contents are not uploaded implicitly. A future durable Session relation may
provide deeper provenance without overloading Issue comments.

## Candidate First Slice

The bridge can close a useful loop before a new Server session service exists.

### Local metadata

A sidecar record keyed by local Session ID can hold a candidate `IssueLink`:

```text
server_url
space_id
issue_id
linked_at
issue_updated_at_at_link
workspace_path or workspace mapping reference
last_sync_state
```

This shape is illustrative, not a committed file format. Keeping it separate
from the resumable Session payload avoids making ordinary local Session loading
depend on Server metadata.

### Server interaction

The authenticated local client for the existing Issue routes exists
(`internal/interface/client`, shipped with the
[Agent bridge CLI](../design/agent-bridge-cli.md)). It already lists the
Issues the current user owns, fetches one Issue with its children and
comments, patches status, posts a comment, starts an existing Agent or
Workflow when explicitly requested, and publishes an Artifact.

The first slice's remaining work is the Issue-to-Session link in
[Local metadata](#local-metadata) and the
[workspace mapping](#workspace-mapping-is-local). The client still lacks
patching assignment, creating an immediate child Issue, and relating a
published Artifact to the Issue; that last relation must use the unified
Artifact identity rather than copying an object-store path into a comment.

### Local behavior

- Desktop presents an assigned-work inbox and a contextual Issue panel.
- CLI/TUI offers discoverable commands or panels without adding Issue chatter
  to print-mode answer output.
- Starting local work creates a normal local Session and writes its Issue link.
- The Session header shows Server, Space, Issue, model transport, and sync state.
- Finishing work offers summary, Artifact, and status actions separately.
- “Open in Portal” remains the escape hatch for full management.

No Server schema is required merely to remember the first local link. The
first slice should validate whether users actually move work through this loop
before committing to a general synchronized Session resource.

## Offline And Conflict Semantics

Local execution must not fail merely because the Server becomes unavailable.
Remote mutations, however, must never be reported as complete when they are
not.

Two defensible first-slice choices are:

| Choice | Behavior | Cost |
|---|---|---|
| Fail remote actions explicitly | Local Session continues; publish and status actions say Server unavailable | Small and honest, but no offline completion queue |
| Persist a bounded outbox | Actions show pending and retry after authentication/network recovery | Better continuity, but requires idempotency, ordering, and conflict UX |

Silent best-effort writes are not an option. If an outbox is added, each entry
needs an idempotency key and visible `pending`, `failed`, or `conflict` state.

Issue mutation needs an optimistic concurrency contract, such as an update
version or `updated_at` precondition. A stale local snapshot must not overwrite
a newer owner, executor, status, description, or hierarchy without a conflict
the user can resolve.

## Model, Data, And Trust Boundary

Connected local work creates a data-boundary decision that ordinary local work
does not: Space Issue content may be sent to a personal direct model.

Before the first model call, the client must make visible:

- the source Server and Space;
- which Issue context will be included;
- whether the model is `direct` or `buildmax` managed; and
- the destination the model entry names.

A deployment may eventually require managed models for Space-linked work. The
current local policy mechanisms are not a strong enforcement boundary, so the
product must not claim this restriction until client policy distribution and
enforcement are designed and verified.

Issue descriptions, comments, and remote results are also untrusted model
input. Their provenance should remain visible, and inserting them into context
must not relabel them as system instructions.

Other trust requirements:

- Space authorization is checked on every Server operation, not only when the
  Issue is first linked.
- Removing Space membership stops further remote reads and writes but cannot
  erase a local copy already downloaded.
- Local result provenance is a client report unless a stronger append-time
  evidence mode is implemented.
- A Portal viewer must not imply that Server governance covered direct model
  calls, local shell execution, or unsynchronized work it never observed.

## Relationship To Durable Agent Sessions

The Issue bridge and durable Session sync solve different problems:

- the bridge connects work intake, decomposition, delegation, status, and
  results;
- durable Sessions provide recovery, revisioned checkpoints, sharing,
  provenance, and cross-device continuation.

The first bridge should not wait for full Session sync. It should preserve a
clean upgrade path: a later Server-side Session resource can become the target
of the Issue relation without turning the Session into a Portal Conversation or
a TaskRun.

If durable Sessions are accepted first, this proposal should reuse their
identity, authorization, visibility, and revision contracts rather than invent
a second local-execution record.

## Delivery Phases

### Phase 1: Receive, Work, Return

- assigned Issue listing — **done**, `buildmax issue list`;
- Issue detail and bounded context snapshot — **done**, `buildmax issue show`
  for both a person and the Agent;
- local Session link and workspace mapping — **not done**. `buildmax issue start`
  scopes one run and remembers nothing, and the workspace is wherever the command ran.
  Both wait on open questions 1 and 4;
- explicit summary, Artifact, and status return — **done** for a summary
  (`buildmax issue comment`, stored with `local_agent` provenance) and status
  (`buildmax issue status`); an Artifact published
  from a runless Session still has nowhere to appear in the Issue's Results
  panel, which is open question 5; and
- clear Server, Space, model destination, and sync state — **partly**: the first
  three print before the first model call. There is no sync state to show yet,
  because nothing is synchronized.

### Phase 2: Decompose And Coordinate

- child Issue creation from the current Issue;
- contextual assignment to person, Agent, or Workflow;
- explicit remote execution trigger;
- related TaskRun/result notifications in the local surface; and
- conflict-safe updates and a durable outbox if evidence justifies it.

### Phase 3: Continue And Govern

- relation to a durable Server-side Agent Session;
- checkpoint publication and cross-device view or fork;
- optional or required enterprise capture policy;
- managed-model and local-tool policy for Space-linked work; and
- Portal projection of bounded local execution metadata and provenance.

Each phase must leave unconnected local execution complete and must avoid
claiming Server authority over behavior the Server cannot observe or control.

## Open Questions And Evidence Needed

1. Is one linked Issue per local Session the right first constraint, or do real
   workflows need a Session to contribute to several Issues?
2. Should starting work offer to assign the Issue to the current user, require
   it already be assigned, or permit unassigned collaborative work?
3. Does Phase 1 need a durable outbox, or is explicit retry sufficient for the
   first early adopters?
4. What stable repository or workspace identity can safely remember local path
   mappings across Issues and devices?
5. Should a local result create a specialized execution-summary record, a
   normal user comment with relations, or wait for Durable Agent Sessions?
   [Issue agent access](../design/issue-agent-access.md) §11 asks the same
   question from the tool side: an Artifact a runless Session publishes has no
   task run to hang on, and the Issue's outputs aggregation reads runs.
6. Which Portal view distinguishes “worked locally” from “ran in a Worker”
   without presenting unverifiable client claims as audit evidence? The comment
   thread already does, through the `local_agent` author kind; the Results
   panel and the run list do not.
7. When may a deployment refuse direct models for Space-linked work, and what
   device-management or signed-policy mechanism makes that enforceable?
8. Do spaces actually decompose and delegate work from the local context, or is
   receive-and-return the dominant workflow?

Evidence should come from a small number of real local-to-Space workflows:
software change, data analysis, incident investigation, and document work. The
decision should measure manual copying removed, result traceability, conflict
frequency, and whether users still need the full Portal during execution.

## Likely Destination If Accepted

If the direction is accepted:

1. update [surface positioning](../design/surface-positioning.md) so contextual
   Issue work is a committed CLI/Desktop bridge, not only an optional inbox;
2. replace R5's decision placeholder in [ROADMAP.md](../ROADMAP.md) with the
   accepted delivery scope;
3. align with the durable Agent sessions decision on identity and relations;
4. create focused work items in [`docs/backlog/`](../backlog/README.md) —
   where main-line work lives, not GitHub Issues — for the local link and
   workspace mapping, Desktop and CLI surfaces, result relations, and policy
   work;
5. update user documentation only when a slice ships; and
6. delete this proposal after its durable rationale has moved to the accepted
   design records.
