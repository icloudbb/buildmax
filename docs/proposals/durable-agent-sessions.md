# Durable Agent Sessions

> **简体中文：** [阅读中文镜像](../zh-CN/proposals/durable-agent-sessions.md)
>
> **Audience:** contributors, operators, security reviewers, and early adopters · **Status:** proposal — under discussion
>
> **Opened:** 2026-08-22

Related: [roadmap](../ROADMAP.md) R5 (general durable Session sync is outside
the first Beta gate);
[surface positioning](../design/surface-positioning.md),
[session architecture](../contribute/architecture/session.md),
[sessions and traces guide](../../manual/sessions-and-traces.md),
[durable run trace](../design/durable-run-trace.md),
[context durability](../design/context-durability.md),
[space governance](../design/space-governance.md), and
[data model](../contribute/architecture/data-model.md).

## Contents

- [1. Decision Question](#1-decision-question)
- [2. Problem And Current Context](#2-problem-and-current-context)
- [3. Product Value Hypotheses](#3-product-value-hypotheses)
- [4. Goals](#4-goals)
- [5. Non-Goals](#5-non-goals)
- [6. Terms And Mental Model](#6-terms-and-mental-model)
- [7. Options](#7-options)
- [8. Candidate Product Decisions](#8-candidate-product-decisions)
- [9. Candidate Resource Model](#9-candidate-resource-model)
- [10. Storage And Projection](#10-storage-and-projection)
- [11. Synchronization Protocol](#11-synchronization-protocol)
- [12. Workspace Identity And Cross-Device Continuation](#12-workspace-identity-and-cross-device-continuation)
- [13. Server Viewer And Stable Links](#13-server-viewer-and-stable-links)
- [14. Authorization And Governance](#14-authorization-and-governance)
- [15. Privacy, Security, And Integrity](#15-privacy-security-and-integrity)
- [16. Deployment Policy](#16-deployment-policy)
- [17. Failure Semantics](#17-failure-semantics)
- [18. Migration And Compatibility](#18-migration-and-compatibility)
- [19. Surface Behavior](#19-surface-behavior)
- [20. Architecture Placement](#20-architecture-placement)
- [21. Phased Path](#21-phased-path)
- [22. Prototype Acceptance Criteria](#22-prototype-acceptance-criteria)
- [23. Evidence Needed Before Acceptance](#23-evidence-needed-before-acceptance)
- [24. Open Questions](#24-open-questions)
- [25. Likely Destination If Accepted](#25-likely-destination-if-accepted)
- [26. Candidate Conclusion](#26-candidate-conclusion)

## 1. Decision Question

Should BuildMax make an authenticated local Agent session a first-class,
revisioned Server resource so that a private deployment can preserve it, render
it at a stable authorized URL, relate a frozen point in it to a pull request or
other work object, and let another device continue from it?

The likely answer is yes, but not by treating a session file as an ordinary
cloud-synchronized document and not by merging it with a Portal Conversation.
The candidate direction is:

- local execution and local persistence remain authoritative while a turn is
  running;
- a connected client uploads immutable checkpoints after stable turn
  boundaries;
- the Server owns remote metadata, authorization, retention, relations, and a
  durable copy of each accepted checkpoint;
- a URL embedded in another system identifies a specific immutable revision;
- another device may view or fork any compatible checkpoint, while exact
  continuation requires workspace checks and single-writer coordination;
- a client-reported transcript is useful provenance, but is not represented as
  tamper-proof audit evidence; and
- deployments choose whether synchronization is disabled, optional, or
  required for their managed local mode. Direct local-only use still exists.

This proposal asks for evidence and decisions before that direction becomes a
roadmap commitment. It does not document a shipped Server session service.

## 2. Problem And Current Context

BuildMax deliberately has one Agent runtime and distinct product surfaces:
CLI/TUI and Desktop execute against a local workspace, Portal organizes space
work, and Workers execute durable background TaskRuns. That split remains the
right product boundary, but session durability stops at the machine boundary
for local execution.

### 2.1 What exists locally

A local session is resumable Agent state, not just a chat view. Its journal
reduces to an `internal/core/session.State` containing:

- user, assistant, and tool messages;
- tool calls and tool results;
- provider-owned reasoning state required by some protocols;
- text and image parts;
- compaction boundary and accumulated summary;
- durable notes and todos; and
- the additional system prompt the session ran under.

Its metadata separately carries the title, workspace, selected model, and token
and cost totals.

`internal/agentapp.SessionManager` persists each session as a bundle under
`<BUILDMAX_HOME>/sessions/<session_id>/`: `meta.json` holds current selections
and running totals, and the append-only `history.jsonl` journal holds the
conversation. A rebuildable `index.json` under `<BUILDMAX_HOME>/sessions/` is
the picker projection used by CLI/TUI and Desktop. Saving is incremental: each
message, tool boundary, compaction, and state change is appended to the journal
before the call that made it returns, rather than the session being rewritten
after a completed turn. See [session architecture](../contribute/architecture/session.md).
The same run also writes a bounded, redacted trace under
`<BUILDMAX_HOME>/sessions/<session_id>/traces/`.

Those two records answer different questions:

| Record | Primary question | Resume input? |
|---|---|---|
| Session | What conversation state should the Agent continue with? | Yes |
| Trace | What did one run actually call, execute, spend, and observe? | No |

A useful remote session page eventually needs both, but synchronizing one does
not silently synchronize the other.

### 2.2 What exists on the Server

Portal Conversations are durable Space resources with normalized message rows.
They are Tier 1 orchestration objects and the single user-facing voice for
Portal turns. They can start durable Tasks and read their TaskRun results. Their ownership, concurrency, and lifecycle are not the same as a
local Agent session.

Worker execution already proves a narrower version of the required storage
path. A Task owns a UUID session ID. Each run uploads its session bundle —
`meta.json` and `history.jsonl`, without traces — into the run-scoped object
namespace; a later run restores those two files before continuing.
The Server can project the latest stored task session as a task conversation.
That implementation is scoped to one Task's run history. It is not a general
session registry: it cannot list a person's local sessions, assign visibility,
pin a checkpoint, relate it to a commit, or let another device claim it.

The database explicitly treats session IDs as an exception: Task and TaskRun
rows point at UUID-named files rather than a session table. CLI and Desktop do
not use the Server database for session persistence at all.

### 2.3 What authenticated local mode provides

CLI and Desktop can sign in to a BuildMax deployment. A connected local Agent
still runs locally; sign-in supplies identity, managed models, and narrow
bridges to space work. This is a useful foundation for session synchronization:
the client has an authenticated user, a deployment URL, and a reason to send
prompts through that deployment.

Sign-in is currently a connector, not a universal gate. That is an intentional
product decision: BuildMax must remain useful as one local binary with direct
models and no Server. An enterprise-managed installation may choose a stricter
policy, but the product as a whole should not make local-only execution an
accidental unsupported mode.

### 2.4 The missing product loop

Today a local session can produce an important result such as a commit, pull
request, incident diagnosis, migration plan, or release decision, but the
organization has no durable Server object that connects the result to the work
that produced it.

The consequences are practical:

- a reviewer sees the diff but not the request, corrections, tools, or
  validation that led to it;
- a user changing devices must copy files and identify the session manually;
- a lost machine loses local Agent history even when the model calls went
  through the company's Server;
- a spacemate cannot receive a view-only handoff without copying transcript
  text into another system;
- support and security investigations cannot begin from one stable session
  URL; and
- other systems cannot hold a durable reference to local Agent work.

Central storage alone does not close every gap. The Server must also define
identity, revisioning, authorization, conflict behavior, content policy,
retention, and what it means to resume against a different workspace.

## 3. Product Value Hypotheses

The feature has several possible values. They should not be assumed to have
equal demand or equal implementation cost.

| Use case | Expected value | Main dependency |
|---|---:|---|
| Recover a session after device loss or replacement | High | Reliable checkpoint upload and download |
| Open an authenticated record from a pull request or Issue | High | Stable immutable URL and relation model |
| Explain what changed and what was validated during review | High | Session, trace, change, and result projection |
| Hand unfinished work to another person | High | Explicit sharing and workspace compatibility |
| Continue one's own work on another device | Medium to high | Workspace identity, single writer, and conflict UX |
| Search one's prior work | Medium to high | Safe indexing and user-scoped retrieval |
| Generate standups, cost summaries, or recurring-work insights | Medium | Search/index quality and privacy controls |
| Let administrators inventory Agent use | High for some operators | Metadata visibility without default content access |
| Treat local activity as compliance-grade evidence | Potentially high | Stronger capture and integrity than file upload provides |

The strongest first hypothesis is not seamless multi-device execution. It is
that a private space gets a stable, reviewable provenance record for important
local Agent work. Cross-device continuation is a valuable follow-on whose
success depends on workspace state that the session does not currently own.

There is external evidence that this category is real. GitHub Copilot now
documents [synchronized local session data](https://docs.github.com/en/copilot/concepts/agents/copilot-cli/chronicle)
and [session management](https://docs.github.com/en/copilot/how-tos/copilot-on-github/use-copilot-agents/manage-and-track-agents)
covering view-only sharing, logs reachable from Agent-produced changes,
cross-surface history queries, and continuation. That validates demand for the
category, not the exact BuildMax design. BuildMax's distinct opportunity is to
provide the same continuity inside a privately deployed system while retaining
direct local execution.

## 4. Goals

- Preserve completed local Agent turns in a private deployment without making
  the Server the execution host.
- Give every synchronized session a stable identity and every accepted state a
  monotonically ordered immutable revision.
- Render an authorized, useful Server-side session page that combines the
  conversation with linked execution evidence without exposing resume-only
  opaque state.
- Let a pull request, Issue, Task, artifact, commit, incident, or external
  system refer to the exact session checkpoint relevant to it.
- Make backup, download, and same-user cross-device continuation reliable.
- Define explicit fork behavior instead of corrupting or automatically merging
  divergent histories.
- Keep Space as the Server ownership and authorization boundary.
- Give deployments clear synchronization, visibility, retention, deletion,
  and content-inspection policies.
- Distinguish client-reported history from Server-observed or Worker-produced
  execution evidence.
- Reuse the shared Agent session format and runtime rather than introducing a
  second resume representation in CLI or Desktop.

## 5. Non-Goals

- Requiring a Server, a login, or remote persistence for direct local-only use.
- Moving CLI/Desktop Agent execution to the Server.
- Replacing Portal Conversation, Issue, Task, TaskRun, Workflow, or Artifact.
- Making Desktop a Portal administration client.
- Synchronizing the local source tree, uncommitted changes, credentials,
  settings, installed plugins, or MCP processes as an implicit side effect of
  session synchronization.
- Guaranteeing bit-for-bit execution reproducibility on another device.
- Concurrent multi-writer editing of one session history.
- Automatically merging divergent Agent conversations.
- A general Session Tree, arbitrary parent/child messaging, Agent mailbox,
  fan-out/fan-in, or automatic join protocol.
- Public anonymous session links in the first release.
- Exposing provider-owned reasoning payloads or claiming to display private
  chain-of-thought.
- Treating an uploaded local file as a tamper-proof audit log.
- Natural-language organization-wide search in the first storage slice.
- Automatically adding assistant, model, or tool attribution or session links
  to commits and pull request descriptions. The repository excludes that
  convention while still allowing human co-author credit; changing it would be
  a separate explicit product and contribution decision.

## 6. Terms And Mental Model

### 6.1 Local Session

The existing resumable session bundle managed by AgentApp and persisted under
`BUILDMAX_HOME`. It is the active runtime history while CLI or Desktop executes
locally.

### 6.2 Durable Agent Session

The Server resource that owns remote identity, Space scope, access policy,
metadata, revisions, lifecycle, and relations for a synchronized Agent
session. The product may call it a “Session”; code and schema should use an
unambiguous name such as `agent_session` so it is not confused with an
authentication session.

A Durable Agent Session is not itself a running process. It may be active,
idle, or archived while no Agent is executing.

### 6.3 Revision And Checkpoint

A revision is one immutable Server-accepted state of a session. A checkpoint
is the resumable payload and display projection attached to that revision.
Revision numbers are monotonic within one session and begin at one; they do
not imply global ordering.

A stable external reference always names both a session and a revision. A
session-level URL may redirect to latest for ordinary browsing, but it is not
the provenance URL attached to another work object.

### 6.4 Run And Trace

A run is one Agent loop execution within a session, normally initiated by one
user turn. A trace is the bounded, redacted record of that run. Several runs
may advance one session; one session revision may acknowledge the state after
one or more recovered run fragments, but the normal case is one completed run
followed by one revision.

### 6.5 Relation

A typed association between a frozen session revision and another durable
object, for example an Issue, Task, TaskRun, Artifact, repository, commit, pull
request, deployment, or external incident. A relation is not embedded free
text and does not make the target an authorization credential.

### 6.6 Replica And Device

A replica is one local copy of a session. A device identity is diagnostic and
coordination metadata, not an authority by itself. Authentication determines
who may act; a registered client instance helps explain where a revision came
from and whether another replica is active.

### 6.7 Continue, Resume, And Fork

This proposal uses three distinct operations:

| Operation | Meaning |
|---|---|
| Continue locally | Add a turn to the local replica already open on this device |
| Exact resume | Download a checkpoint and continue the same logical session after compatibility and writer checks |
| Fork | Create a new session whose initial context comes from a named checkpoint |

Fork is always safe from a history-integrity perspective. Exact resume is more
convenient but requires stronger preconditions.

## 7. Options

### 7.1 Option A: Keep local sessions local

Continue the current split. Worker sessions remain recoverable within one Task;
Portal Conversations remain the only centrally visible foreground history.

| Strength | Concern |
|---|---|
| No new data collection, authorization, storage, or conflict semantics | Leaves local execution outside the enterprise continuity and provenance loop |

This is defensible if early adopters mostly use local sessions for disposable
work and attach only final artifacts to Portal. It should be validated rather
than assumed.

### 7.2 Option B: Opaque remote backup

Upload the current session JSON to object storage and let the same user
download it. Do not create a rich Server entity or viewer.

| Strength | Concern |
|---|---|
| Smallest recovery feature and closest to the existing Worker path | Cannot support stable relations, Space sharing, search, lifecycle policy, or useful review |

This may be a good implementation stepping stone. It is too narrow as the
long-term product model because every later feature would have to reconstruct
metadata and authorization around anonymous blobs.

### 7.3 Option C: Revisioned Server resource with local execution

Create a Durable Agent Session with relational metadata and immutable
checkpoint blobs. Local AgentApp remains the active writer, and clients sync
at stable boundaries.

| Strength | Concern |
|---|---|
| Supports recovery, URLs, relations, sharing, governance, and later indexing while preserving local-first execution | Requires a new resource model, sync protocol, content policy, and conflict UX |

This is the likely direction.

### 7.4 Option D: Reuse Portal Conversation

Import each local session as a Portal Conversation and write its messages into
`conversation_message`.

| Strength | Concern |
|---|---|
| Reuses listing, message persistence, Space authorization, and some UI | Conflates Tier 1 orchestration with local runtime state; has no revisioned resume payload; creates ambiguous writers and lifecycle |

The message shapes are similar because both feed the same Agent core. That is
not enough to make their product semantics identical. Portal Conversations
accept live Space turns, serialize them through a Server queue, and speak to the
user. A local session may exist privately, run offline, carry resume-only
state, and later publish a frozen checkpoint. Reusing one table would hide
rather than remove these differences.

### 7.5 Option E: Server-canonical live event stream

Send every message and tool event to the Server as it occurs and make the
Server event log the source of truth. Local files become a cache.

| Strength | Concern |
|---|---|
| Best central durability and strongest foundation for live monitoring | Makes network availability part of local correctness, greatly expands ingestion volume, and still does not synchronize workspace state |

This could be an enterprise capture mode later. It is too large for the first
slice and weakens the direct local product unless it is explicitly limited to
a deployment policy.

[Remote Control](../design/remote-control.md) now ships a live relay that is
not Option E. An opted-in CLI/TUI session relays its redacted events through the
Server so another device can watch and steer it, but the relay is fail-open and
non-canonical: the local session stays authoritative, and the Server keeps a
live-session registry and a buffered stream rather than a durable transcript.
It therefore neither provides nor replaces the durable checkpoints this
proposal is about.

## 8. Candidate Product Decisions

The rest of this proposal develops Option C and makes its unsettled choices
visible.

### 8.1 Local execution remains local

Sync must attach at AgentApp's session lifecycle seam. CLI and Desktop should
not each implement their own remote session format or call a separate Agent
runtime. The local session remains usable when synchronization is disabled and,
subject to deployment policy, when the Server is temporarily unavailable.

### 8.2 Space remains the remote ownership boundary

Every Durable Agent Session belongs to exactly one Space. A session created
without an explicit shared Space belongs to the user's personal Space. It also
records an owner user. Visibility controls who in that Space can read content:

| Visibility | Candidate readers |
|---|---|
| `private` | Owner; narrowly authorized break-glass paths if a deployment enables them |
| `space` | Current Space members |

The first release should not support a single mutable session spanning several
Spaces. Publishing work to another Space either creates a frozen shared copy or
requires an explicit ownership transfer whose history is audited. The simpler
first choice is a shared copy.

This distinction matters because “the administrator can inventory sessions”
does not automatically mean “the administrator can read every prompt and tool
result.” Metadata access, content access, retention authority, and break-glass
access are separate capabilities.

### 8.3 Preserve the offline-created UUID

Local sessions already use UUIDs, and offline clients need to allocate identity
without asking a Server. The likely compatibility choice is to preserve that
UUID as the durable session's external identity. The database may have an
internal primary key according to the eventual entity-identity decision, but
the sync protocol should not force existing local sessions to be renamed.

An alternative is a Server wrapper ID plus a local UUID. That adds a permanent
mapping and makes an offline-created link unusable until registration. It is
not justified unless the open entity-identity proposal adopts a universal
format that must also include Agent sessions.

### 8.4 Revisions are immutable

The Server never overwrites revision content. Metadata such as title, pin,
visibility, and archive state changes through separate commands. A new Agent
turn creates a new revision whose `base_revision` must match the Server's
current revision.

Immutability supplies:

- stable external references;
- understandable conflicts;
- retention and legal-hold semantics;
- content digest verification;
- the ability to explain what a pull request linked at creation time; and
- a clean path from client-reported history to stronger append-time evidence.

It does not by itself prove the contents were true when work occurred.

### 8.5 Completed turns are the first synchronization boundary

The first version uploads after AgentApp finalizes a turn locally. It does not
stream token deltas or every tool event. A completed assistant response is the
existing stable save point and gives the user a coherent checkpoint.

A later client may also upload:

- a `running` marker for presence;
- periodic crash-recovery snapshots;
- trace chunks; or
- a terminal failed/canceled checkpoint.

Those are follow-ons. They must not make partial, internally inconsistent
message sequences resumable as though they were complete turns.

### 8.6 Single writer; fork on divergence

One logical session has one accepted head. A client upload names its
`base_revision`:

- if it equals the current head, the Server may accept the next revision;
- if the same idempotency key or content digest was already accepted, the
  Server returns the existing acknowledgement;
- if the head advanced, the Server returns a conflict and never overwrites it;
- the client may discard its local divergent turn, export it, or create a fork
  from the common revision; and
- the Server never auto-merges histories.

A short writer lease can improve UX once exact cross-device resume exists, but
revision preconditions remain the correctness mechanism. A lease can expire;
an immutable accepted revision cannot.

### 8.7 Frozen relations, not mutable “latest” links

A relation to a pull request or other work object names a session revision.
The session overview may show later continuation, but the target's provenance
link remains frozen. This avoids rewriting historical meaning when the user
continues the session for unrelated follow-up work.

### 8.8 Server policy, permissions, and credentials are re-resolved

Downloading a session does not restore credentials, process environment,
approval grants, or authoritative policy. Exact resume reconstructs
conversation state, then resolves the current device's effective settings,
Server policy, Space membership, model access, hooks, sandbox, tools, plugins,
and credentials.

The checkpoint records what the prior run used for explanation. It does not
grant the next run the same authority. In particular, an `allow session`
approval belongs to one in-memory local session execution and must not become
a portable remote capability.

## 9. Candidate Resource Model

This is a logical model, not a committed database schema. The data-model source
of truth remains the eventual row structs after implementation.

### 9.1 `agent_session`

| Field | Purpose |
|---|---|
| `session_id` | Existing offline-created UUID, unique |
| `space_id` | Required ownership and authorization boundary |
| `owner_user_id` | User who owns the private session and normal write authority |
| `title` | User-visible mutable metadata |
| `visibility` | `private` or `space` in the first slice |
| `source_surface` | Initial source such as CLI, TUI, Desktop, Worker, or import |
| `current_revision` | Monotonic accepted head |
| `created_at`, `updated_at` | Lifecycle and ordering |
| `archived_at`, `archived_by` | Reversible removal from active lists |
| `deleted_at`, `deleted_by` | Tombstone preventing stale replicas from silently recreating it |
| `retention_class` | Optional operator-selected policy reference, not arbitrary client text |

Session execution state such as “currently running” should not be a permanent
enumeration on this row until there is a durable supervisor. Presence and
leases expire; archive and deletion are durable lifecycle state.

### 9.2 `agent_session_revision`

| Field | Purpose |
|---|---|
| `session_id`, `revision` | Immutable composite identity |
| `base_revision` | Optimistic concurrency precondition and lineage within one session |
| `snapshot_key` | Server-owned object-storage key |
| `snapshot_digest` | Digest of exact stored bytes |
| `snapshot_size` | Admission, diagnostics, and quota |
| `schema_version` | Decoder compatibility independent of product version |
| `message_count` | Bounded listing/debug metadata |
| `created_by`, `device_id`, `created_at` | Attribution and origin |
| `source_trust` | Candidate class such as `client_reported`, `server_observed`, or `worker_produced` |
| `workspace_descriptor_key` | Optional structured resume/provenance metadata |
| `display_projection_key` | Optional safe rendering projection |
| `resumable` | Whether a supported client can decode the raw checkpoint |

The Server computes size and digest from received bytes. It does not trust
client-supplied values. A revision admission transaction must not publish a
database head until the object is durably stored, and an uploaded unreferenced
object must be collectible if the transaction fails.

### 9.3 `agent_session_run`

One session may contain many local runs and may later connect to a TaskRun.
The candidate association records:

| Field | Purpose |
|---|---|
| `session_id`, `revision` | Checkpoint reached by the run |
| `run_id` | Agent runtime trace ID |
| `task_run_id` | Optional durable Worker TaskRun |
| `trace_key` | Optional synchronized trace object |
| `surface`, `model_ref` | Explanation metadata, not a credential |
| `started_at`, `ended_at`, `outcome` | Timeline projection |
| `prompt_tokens`, `completion_tokens` | User and operator accounting projection |

Local direct-model usage cannot be treated as Server-metered billing evidence.
Managed calls already recorded by the gateway remain the authoritative ledger
for what the deployment served.

### 9.4 `agent_session_relation`

| Field | Purpose |
|---|---|
| `session_id`, `revision` | Frozen provenance source |
| `relation_type` | Closed set such as Issue, Task, TaskRun, Artifact, repository, commit, pull request, or external URL |
| `target_id` | BuildMax public ID when the target is a BuildMax entity |
| `provider`, `repository_ref`, `external_ref` | Structured external identity where applicable |
| `url` | Display/navigation value, validated by relation type |
| `created_by`, `created_at` | Attribution |

A polymorphic relation trades database-enforced strictness for extensibility.
The service must validate Space ownership for BuildMax targets and repository
scope for provider targets. An alternative is one typed join per internal
entity plus a separate external-link table. The first implementation should
choose based on the queries Portal actually needs, not on a desire for a
universal graph.

### 9.5 Replica cursor and writer lease

Replica acknowledgement and writer presence do not need to live on the session
row. Candidate short-lived records are:

- last revision downloaded and uploaded by a device;
- last-seen time and client version;
- optional writer lease token, owner, expiry, and base revision; and
- pending deletion acknowledgement.

These records are operational. Retention may be short, and their absence must
not make the immutable revision history invalid.

## 10. Storage And Projection

### 10.1 Hybrid persistence

Use the relational database for metadata, authorization, lifecycle, and
relations. Use object storage for raw resumable checkpoints, traces, and large
display payloads.

Do not place the entire current session JSON in one mutable text column. The
format contains provider-owned raw JSON and base64 image data, evolves with the
shared LLM contract, and can be large. Conversely, do not make object keys the
only registry: list, authorize, retain, relate, and tombstone are relational
operations.

### 10.2 Raw checkpoint versus display projection

The raw checkpoint is for a trusted BuildMax client to resume. The Portal page
should use a projection that deliberately includes or excludes each field:

| Content | Raw checkpoint | Default viewer |
|---|---:|---:|
| User and assistant text | Yes | Yes, subject to access policy |
| Tool name and status | Yes | Yes |
| Tool arguments and output | Yes | Summarized or access-gated |
| Notes and todos | Yes | Optional section |
| Compaction summary | Yes | Explanation metadata, not duplicated as a turn |
| Image bytes | Yes initially | Served as authorized attachments, not inline JSON |
| Provider-owned state | Yes when required to resume | Never displayed or indexed |
| Additional system prompt | Yes today | Restricted explanation view |
| Credentials and approval grants | Never | Never |

The first implementation can generate the projection at upload time and store
it beside the raw blob. If it is regenerated later, the projection version and
source digest must be recorded so a viewer never implies it is the exact raw
record.

### 10.3 Attachments and size

The current message format can carry base64 images. Repeated immutable session
snapshots would duplicate every prior image and message prefix. A simple first
slice may accept that cost under strict per-session and per-revision limits,
but the durable format should eventually externalize binary parts into
content-addressed authorized objects.

Storage admission needs explicit limits for:

- compressed and uncompressed revision bytes;
- number and size of message parts;
- trace bytes and record count;
- total retained bytes per user and Space; and
- import batch size.

The Server must verify media types and reject malformed encodings. It should
not unpack arbitrary archives as part of session ingestion.

### 10.4 Encryption and object identity

Checkpoints contain source and prompt data and should use the deployment's
object-store encryption controls. Server-owned object keys must derive from
authorized internal identity, not accept a client-provided relative path.

Content-addressing may deduplicate identical blobs, but authorization remains
attached to the revision record. A digest is not a credential and must not be a
download route parameter that bypasses Space membership.

## 11. Synchronization Protocol

### 11.1 Registration

On the first eligible save, the client registers the local UUID with a Space,
owner, title, source surface, and initial checkpoint. Registration is
idempotent for the same user, deployment, and UUID. A UUID already owned by a
different user or Space is a conflict, not an adoption path.

Importing an existing local session creates revision 1 from its current state.
The Server does not invent earlier revisions from individual messages or file
timestamps.

### 11.2 Upload

An upload contains at least:

- session ID;
- base revision;
- client idempotency key;
- schema version;
- raw checkpoint stream;
- display projection or enough data for the Server to derive it;
- workspace descriptor;
- associated run metadata and optional trace; and
- requested relations created at this checkpoint.

The candidate acceptance sequence is:

1. Authenticate the user and authorize write access to the Space session.
2. Reject tombstoned, archived-for-write, over-limit, or incompatible input.
3. Stream into a Server-owned temporary object while measuring and hashing.
4. Validate the checkpoint envelope and session identity.
5. Compare `base_revision` with the current head inside a transaction.
6. Publish the immutable object and revision metadata.
7. Advance the session head and commit relations atomically from the API's
   perspective.
8. Return revision, digest, and Server time.

Exact object-store/database atomicity is impossible. The service needs a named
reconciliation rule: a database revision never points to an object that was
not successfully finalized, and temporary or finalized-but-unreferenced
objects are swept after a grace period.

### 11.3 Local outbox

Optional synchronization must not slow or fail a completed local turn because
the Server is temporarily unavailable. AgentApp should enqueue a small durable
sync job after local save, then retry with bounded exponential backoff. UI and
CLI status expose `synced`, `pending`, `conflict`, `rejected`, or `disabled`.

The outbox stores references to immutable local checkpoint bytes or a copied
upload payload. It must not point only at the live session journal, because
the journal may advance before a retry and make an idempotency key refer to
different bytes.

In required mode, failure semantics are a deployment decision:

- fail before starting a new turn when no acceptable remote checkpoint exists;
- allow a bounded offline grace period or byte count, then refuse new turns;
  or
- allow local work indefinitely but mark the device non-compliant.

Silently claiming required capture while indefinitely queueing unsent work is
not acceptable. The chosen policy must be visible before a user authorizes
local tool execution.

### 11.4 Download

A client downloads by session and revision after authorization. The response
includes digest, schema version, workspace descriptor, source trust, and
relations before streaming the checkpoint. The client writes to a temporary
file, verifies the digest and embedded session ID, then atomically installs it
under `BUILDMAX_HOME`.

An unsupported future schema is view-only until the client upgrades. It must
not be partially decoded and resaved as an older format.

### 11.5 Conflict

A revision conflict response includes safe metadata about the current head and
the caller's common base. It does not return another user's content. The local
surface offers:

1. open the remote head read-only;
2. discard the unsynced local branch;
3. save the local branch as a new fork; or
4. export both for manual comparison.

“Force push session” should not exist in the first release. If operators ever
need corrective replacement, it should create a new revision and a governance
record rather than erase accepted history.

### 11.6 Rename, pin, archive, and delete

These are metadata commands, not new transcript revisions.

- Concurrent rename may use metadata versioning or last-writer-wins with actor
  and time visible; it does not affect resume correctness.
- Pin is normally user-specific presentation state and may remain local or in
  a user-session preference table rather than on the shared resource.
- Archive hides a session from active lists but keeps revisions and links.
- Delete creates a remote tombstone before asynchronous content erasure. A
  stale device receives `gone` and cannot recreate the UUID accidentally.
- Legal hold can override physical erasure but must not pretend the user's
  delete request was fulfilled; the UI states the retention reason.

Local deletion and remote deletion are separate choices. Deleting a local
cache must not erase organizational evidence without an explicit remote
operation, and a required-sync policy may prohibit remote deletion while
allowing local cleanup.

## 12. Workspace Identity And Cross-Device Continuation

### 12.1 Why transcript synchronization is insufficient

An Agent session can say “edit the function we just inspected” because its
messages refer to a workspace state. Another device may have:

- no checkout;
- a checkout at a different path;
- the same repository at a different commit;
- different uncommitted changes;
- different `AGENTS.md`, hooks, skills, plugins, or MCP configuration;
- a model client that cannot consume stored provider state; or
- different operating-system tools and permissions.

Downloading messages without surfacing those differences creates a false
promise of continuity.

### 12.2 Workspace descriptor

Each checkpoint should carry a structured descriptor whose fields are facts,
not portable authority:

| Field | Purpose |
|---|---|
| Workspace kind | Git checkout, plain directory, or Worker materialization |
| Repository provider and normalized remote identity | Match the same project without relying on a local path |
| Base and head commit | Explain code state and enable compatibility checks |
| Branch name | Hint only; not identity |
| Dirty state | Clean, dirty, unknown, plus digest or change reference |
| Workspace-relative file/change summary | Review and mismatch explanation |
| Local path | Diagnostic to the owner only; never a cross-device identity |
| Instruction-layer digests | Detect changed AGENTS.md or workspace policy |
| Effective model protocol | Decide whether provider state is reusable |
| Tool/plugin/skill manifest | Explain missing capabilities without carrying credentials |
| Platform and client version | Compatibility diagnostics |

The descriptor should avoid uploading the entire settings file or environment.
Names, versions, digests, and enabled capability identifiers are generally
enough. Secret values are never included.

### 12.3 Resume levels

The client should make its guarantee explicit:

| Level | Requirement | Behavior |
|---|---|---|
| View | Authorized checkpoint | Render only |
| Context fork | Decodable checkpoint | New session, current workspace and policy, visible mismatch warnings |
| Compatible resume | Same repository and acceptable revision relationship | Continue same session after writer check |
| Reconstructed resume | Compatible repository plus available change/workspace snapshot | Materialize state, then continue |

The first cross-device release should support View and Context fork. Compatible
resume follows once repository matching and single-writer UX are proven.
Reconstructed resume depends on a versioned workspace capability BuildMax has
neither built nor planned, and should not be smuggled into session
synchronization.

### 12.4 Dirty workspaces

If device A has uncommitted changes, a session checkpoint may explain them but
cannot recreate them unless BuildMax also stores a change bundle or workspace
snapshot. The client must offer an honest choice:

- continue against the current B-device workspace with a mismatch warning;
- acquire the referenced change bundle through an authorized Artifact
  operation;
- clone or check out the recorded commit; or
- stop and ask the user to prepare the workspace.

It must not silently apply a patch merely because the session references it.
Applying workspace state is a separate mutation with its own review,
authorization, and conflict semantics.

### 12.5 Provider state and model changes

Provider-owned reasoning state may only be replayed by the protocol that
created it. A resumed session using a different model or protocol retains the
portable text/tool history and drops incompatible opaque state according to
the existing LLM adapter contract. The viewer never decodes that state.

## 13. Server Viewer And Stable Links

### 13.1 Page composition

A useful session page should not be a JSON dump. Candidate sections are:

- overview: title, owner, Space, time, surface, sync/trust status;
- conversation: user and assistant text with compact tool activity;
- activity: one card per run with model, duration, token usage, outcome, and
  trace link;
- changes and validation: files, commands/tests, commits, and result summaries
  where evidence exists;
- outputs: linked Artifacts and TaskRun results;
- provenance: repository, base/result commits, related PR/Issue/Task;
- environment: capability and policy fingerprints with mismatch warnings; and
- lineage: imported-from or forked-from checkpoint, without implementing a
  general Session Tree.

Raw tool output, system/additional prompt content, and full traces may require a
stronger permission or explicit reveal action. Provider state is never shown.

### 13.2 URL semantics

Candidate browser routes are illustrative, not registered API contracts:

```text
/spaces/{space_id}/sessions/{session_id}
/spaces/{space_id}/sessions/{session_id}/revisions/{revision}
```

The first may follow current head. The second is immutable and is the only
candidate for a provenance relation. Both require authentication and current
Space authorization. A non-member receives the same not-found behavior used by
other ID-addressed Space resources. No bearer access is implied by knowing the
URL.

### 13.3 Pull request integration

The minimum feature is “Copy immutable session link” plus a structured
relation created by the user or Agent outcome flow. Provider integrations can
later surface the link through:

- a check-run details URL;
- a BuildMax status or review panel;
- a bot comment under an operator policy; or
- a pull request field/body convention chosen by that repository.

The relation records repository, pull request identity, result commit, session
revision, and creator. A pull request link should open a review projection, not
automatically grant the recipient access.

BuildMax's current contribution convention excludes assistant, model, and tool
attribution and assistant session links from its own pull request descriptions
and commit history; it does not exclude credit for substantive human
collaboration. This proposal does not quietly reverse that decision. A product
integration should prefer a structured provider surface such as a check details
link, and any change to repository contribution policy requires its own explicit
acceptance.

### 13.4 External API

If accepted, the likely API capability groups are:

- register/list/read/archive/delete Agent sessions;
- create and fetch immutable revisions;
- download a resumable checkpoint;
- fork from a revision;
- share or change visibility;
- create/list/delete typed relations; and
- fetch a sanitized viewer projection and linked run evidence.

The live route tree remains the source of truth when implementation begins.
This proposal intentionally does not assign final HTTP methods or copy the
whole API surface.

## 14. Authorization And Governance

### 14.1 Candidate access matrix

The exact matrix needs product validation, but its dimensions should be
explicit:

| Action | Owner | Space member on space-visible session | Space admin/owner | System Administrator |
|---|---:|---:|---:|---:|
| List own metadata | Yes | — | Policy-dependent inventory | Deployment metadata only by default |
| Read private content | Yes | No | No by default | No by default |
| Read space-visible content | Yes | Yes | Yes | No by default unless also Space member |
| Append revision | Yes or delegated writer | No | No without explicit takeover | No |
| Change visibility | Yes | No | Possible policy override | No |
| Archive own session | Yes | No | Policy-dependent | No |
| Delete content | Policy-dependent | No | Retention-policy authority | Legal-hold/retention administration only |
| Break-glass read | — | — | Optional explicit flow | Optional explicit flow |

BuildMax should not invent deployment-wide raw-content access merely because a
System Administrator role exists. If a customer requires such access, it is a
separate, audited policy with a clear UI warning and reason capture.

### 14.2 Governance events

Session execution and each uploaded revision are operational records, not
automatically duplicate audit events. Governance candidates are actions that
change authority or evidence lifecycle:

- synchronization policy changed;
- session visibility changed;
- Space ownership transferred or a revision published across Spaces;
- break-glass content access requested and used;
- retention class or legal hold changed;
- remote content deleted or export performed; and
- writer ownership forcibly taken over.

Ordinary session creation, revision ingestion, runs, and relations remain in
their own durable operational records unless an investigation requirement
proves an audit duplicate adds evidence.

### 14.3 Membership changes

Authorization is evaluated on every Server read and download. Removing a user
from a Space removes access to space-visible sessions immediately; a previously
downloaded local copy cannot be remotely erased and the product must not claim
otherwise. Required enterprise deployments may use managed-device controls
outside BuildMax for that stronger guarantee.

If an owner leaves, the Space needs a retention and reassignment policy. The
Server must not silently transfer private content to the owner's manager. A
candidate policy is to retain encrypted content for the configured window,
make it unavailable to ordinary members, and require an audited retention or
break-glass action to reassign it.

## 15. Privacy, Security, And Integrity

### 15.1 Raw session sensitivity

Unlike traces, current session messages are not a redacted evidence format.
They may contain source excerpts, command output, tool arguments, local paths,
user-provided secrets, personal data, screenshots, or opaque provider state.
Private deployment solves data locality, not internal least privilege.

Before upload, the product must tell the user and operator what is captured.
The Server should support a bounded content inspection pipeline, but common
secret-shape redaction cannot be presented as comprehensive DLP. Redacting the
raw checkpoint can also make it impossible to resume faithfully, so the system
may need separate encrypted raw content and redacted display/search
projections.

### 15.2 Trust classes

At least three evidence origins differ:

| Source | What the Server can claim |
|---|---|
| Client-reported local checkpoint | An authenticated user uploaded these bytes at this time |
| Server-observed managed model call | The deployment served this model call and recorded its ledger metadata |
| Worker-produced session/trace | A Server-dispatched TaskRun uploaded these bytes under its run credential |

None proves that arbitrary local tool activity occurred exactly as represented.
A local session file can be edited before synchronization. If compliance-grade
local capture becomes a requirement, a later mode may append events while the
run occurs, bind them with a hash chain and Server receipts, and correlate
managed calls. Even that does not attest to the whole host without a stronger
managed-device boundary.

The viewer should expose source trust without alarming copy such as “verified”
unless the verification claim is precisely defined.

### 15.3 Share defaults

Local sessions are private by default. A deployment may require capture, but
capture does not imply Space-wide content sharing. Publishing a revision to a
Space or relating it to a shared Issue/PR is an explicit action with a preview
of what recipients can read.

The first release should not support unauthenticated public share tokens.
Revocable signed links still escape ordinary Space membership semantics and are
easy to paste into external systems. Their demand should be proven separately.

### 15.4 Prompt injection and hostile content

A remote session is untrusted content even when it was uploaded by a member.
Viewing must not execute HTML, scripts, tool calls, MCP actions, or embedded
instructions. Download-and-resume makes the content input to an Agent, so the
client must mark its origin and reapply current tool permissions. A shared
session cannot carry the original owner's approval decisions into the
recipient's environment.

### 15.5 Export and portability

Users and authorized Spaces need a documented export that includes the raw
checkpoint, a human-readable projection, metadata, digests, relations, and an
optional trace bundle. Export is a read of sensitive content and may be audited.
The format should be versioned and should not include credentials or remote
object-store keys.

## 16. Deployment Policy

A private deployment needs policy rather than one hard-coded behavior. The
candidate modes are:

| Mode | Local behavior |
|---|---|
| `disabled` | No remote registration or upload; current local-only behavior |
| `optional` | User chooses account/session defaults; failures queue visibly and do not fail local turns |
| `required` | Authenticated managed local mode must capture checkpoints under a defined offline rule |

This likely belongs to Server-delivered managed policy, not only the mutable
local settings file. A user must not be able to disable an enterprise-required
capture policy by editing YAML. Conversely, direct local mode against a
personal provider remains outside that deployment unless an external device
management policy prohibits it.

Required mode still needs an explicit offline decision. The proposal does not
choose among:

- no new Agent turn while the Server is unreachable;
- bounded offline turns/bytes/time; or
- run locally but mark the device and work non-compliant.

The evidence needed is how often real developers need disconnected execution
and whether the deployment's governance promise is capture, prevention, or
eventual retention.

## 17. Failure Semantics

| Failure | Optional mode | Required mode candidate |
|---|---|---|
| Server unavailable after local turn | Persist outbox; show pending | Persist within grace or mark/refuse according to policy |
| Authentication expired | Refresh; otherwise show action required | Stop before policy threshold is exceeded |
| Revision conflict | Preserve both; require fork/discard choice | Same; never overwrite |
| Upload rejected for size/policy | Keep local session; explain unsynced state | Refuse further turns once policy says capture is mandatory |
| Object stored, database transaction failed | Reconcile orphan later; do not acknowledge revision | Same |
| Database row committed, object unavailable | Treat as integrity incident; revision not resumable until repaired | Same |
| Download digest mismatch | Do not install; retain prior local copy | Same |
| Unsupported schema | View metadata; require client upgrade | Same |
| Space membership removed | Deny remote read/write | Deny; local copy cannot be clawed back |
| Remote tombstone encountered | Stop re-upload; offer local export | Stop re-upload; obey retention policy |
| Client crashes mid-turn | Last complete checkpoint remains valid; trace may show incomplete run | Same |

Every failure needs a user-visible sync state. A background logger is not
enough when the deployment claims sessions are durable.

## 18. Migration And Compatibility

### 18.1 Existing local sessions

An authenticated user may opt into importing selected sessions or all sessions
for one workspace. Import creates one current checkpoint as revision 1 with
source `legacy_import`. The import records original `created_at`, current
import time, and local workspace metadata. It does not infer earlier revision
times from message order.

The uploader must tolerate an old readable session schema and preserve fields
it understands. A future format envelope should let old clients decline to
rewrite unknown versions rather than discard fields.

### 18.2 Existing Worker sessions

The current TaskRun object-storage session is already durable within one Task
and should continue to work during early implementation. A later adapter can
register a corresponding Durable Agent Session and revisions after successful
run upload. It should not copy historical run blobs into a new namespace until
there is a retention and deduplication reason.

Task and TaskRun keep their current session correlation during migration. A
new session table must not make a Worker unable to resume because Server
metadata registration was temporarily degraded.

### 18.3 Portal Conversations

No automatic migration. A Portal Conversation may relate to or start a local
session, and a local session may publish a result back, but their identities
remain distinct. A future unified history view can project both without
forcing one storage model.

### 18.4 Local index

The local `index.json` picker projection needs remote state such as Server URL,
Space, remote revision, and sync state. Fork origin already exists locally: a
fork records `forked_from` in `meta.json`, and the index projects it. Storing
the remote state in the existing shared index risks making ordinary session
listing fragile. A
candidate is a separate sync-state store keyed by session ID, leaving the
runtime session bundle and picker index backwards compatible.

## 19. Surface Behavior

### 19.1 CLI/TUI

Candidate commands and displays, not committed syntax:

- session list shows local-only, synced, pending, conflict, and remote-only;
- a status command prints deployment, Space, local revision, remote revision,
  and last error;
- sync/import/download accept explicit session IDs and support dry-run where
  an import is broad;
- `resume` can resolve a remote-only session after authentication;
- `fork` is offered when workspace compatibility or revision ownership makes
  exact resume unsafe; and
- a copy-link action emits an immutable revision URL, not a latest URL.

Print-mode automation must retain deterministic output and stable exit codes.
Background synchronization must not write progress into the answer stream.

### 19.2 Desktop

Desktop is the natural first rich client:

- local and remote session filters;
- sync-state badges and actionable conflicts;
- private/space visibility control;
- remote download and workspace mapping;
- session detail with linked outcomes;
- copy immutable link and publish checkpoint; and
- compatibility report before resume.

This remains a local workbench. Space membership, retention, break-glass, and
deployment policy administration stay in Portal.

### 19.3 Portal

Portal owns:

- authorized session inventory and detail;
- Space visibility and relations;
- review projection and linked trace/results;
- retention and governance surfaces;
- operator metadata views; and
- links back to Issue, Task, TaskRun, Artifact, repository, commit, or PR.

Portal does not execute a downloaded local session against the user's machine.
“Continue on device” may issue a short-lived handoff request that an
authenticated Desktop accepts, but arbitrary browser-to-local control is a
separate security design.

### 19.4 External systems

The stable URL is the minimum integration contract. Provider-specific apps,
webhooks, or checks can follow. A generic relation API must validate URLs and
must not fetch arbitrary external content on creation.

## 20. Architecture Placement

| Concern | Candidate owner |
|---|---|
| Pure session/revision/workspace-descriptor value types | `internal/core/session` where they remain domain-pure |
| Local save, outbox, upload scheduling, download, compatibility | `internal/agentapp` |
| CLI/TUI commands and status | `internal/interface/cli` |
| Desktop bindings and local UX | `internal/interface/desktop` and Desktop frontend |
| Authenticated HTTP client | `internal/interface/client` |
| Durable session orchestration and policy | New focused service under `internal/service` |
| Store contracts | `internal/core/model` or a focused pure domain package |
| Metadata rows and queries | `internal/infra/db` |
| Checkpoint, trace, and projection blobs | Existing object-storage abstraction, extended deliberately |
| Space authorization and HTTP endpoints | `internal/server/handlers` |
| Session viewer and governance UI | Portal |
| Shared presentational transcript components | `gui/` where both Desktop and Portal need them |

The Agent loop should not learn about HTTP synchronization. It already writes
through session history and event seams; AgentApp is the correct assembly
layer for persistence side effects. `internal/core/session` remains free of
config, Server clients, database, and object storage.

## 21. Phased Path

### Phase 0: Validate capture and policy

- Interview or observe design-partner spaces using local CLI/Desktop sessions
  for work that reaches review.
- Determine whether their first need is recovery, review provenance, handoff,
  or compliance capture.
- Decide disabled/optional/required policy and offline behavior.
- Threat-model raw session ingestion, content access, deletion, membership
  removal, and break-glass.
- Establish retention and storage-size assumptions from real session files.

Exit evidence: at least one real workflow repeatedly needs a durable session
record, and operators/users agree on who may read its content.

### Phase 1: Private backup and immutable viewer

- Add Durable Agent Session metadata and immutable revision storage.
- Upload after completed local turns through a durable outbox.
- Default to the user's personal Space and private visibility.
- List and view one's own synchronized sessions in Portal.
- Download/export a raw checkpoint and human-readable projection.
- Show source trust, digest, revision, sync status, and retention.
- Import existing sessions as one `legacy_import` revision.

No cross-device write, Space sharing, search, or PR automation is required.

### Phase 2: Provenance and Space sharing

- Publish a frozen revision to a Space with an explicit content preview.
- Add typed Issue, Task, TaskRun, Artifact, repository, commit, and PR
  relations.
- Synchronize or link per-run traces and managed-call evidence.
- Render changes, validation, outputs, and immutable provenance URL.
- Add governance records for visibility, export, retention, deletion, and
  break-glass actions.
- Validate a structured provider integration such as a check details link.

### Phase 3: Cross-device fork and compatible resume

- Register device replicas and expose remote-only sessions in CLI/Desktop.
- Add workspace descriptors and compatibility diagnostics.
- Download and context-fork from a checkpoint.
- Add optimistic revision writes and conflict UX.
- Introduce a short writer lease only if exact resume needs it.
- Support compatible exact resume for the same owner and repository.

Workspace snapshot materialization remains outside this phase. Nothing in
BuildMax provides the service it would need, and no plan does either.

### Phase 4: Search, insights, and stronger capture

- Index the deliberately safe display projection under user/Space scope.
- Add keyword and semantic search with retention-aware deletion.
- Build standup, cost, repeated-failure, and instruction-improvement views only
  after users validate them.
- Evaluate live append-time capture, hash chaining, and Server receipts for
  deployments that need stronger integrity.
- Add operator aggregate inventory without granting raw-content access.

## 22. Prototype Acceptance Criteria

The first implementation slice is credible when:

1. A CLI or Desktop turn completes locally while the Server is unavailable,
   appears as visibly pending, and uploads exactly once after recovery.
2. Repeating an upload with the same idempotency key cannot create a second
   revision.
3. Two devices advancing the same base produce a conflict; neither accepted
   revision is overwritten, and the losing branch can be saved as a fork.
4. A URL naming revision N renders the same checkpoint after revisions N+1
   and N+2 exist.
5. A user outside the owning Space cannot distinguish a missing session from an
   inaccessible one.
6. Private captured content is not readable merely because someone is a Space
   admin or System Administrator, unless an accepted policy says otherwise.
7. A downloaded checkpoint is installed only after digest and schema checks.
8. A session with image parts, provider state, compaction, notes, todos, and
   additional prompt round-trips without losing resumable data.
9. The default viewer never renders provider-owned state and does not execute
   session content.
10. A pull-request relation remains pinned to its original revision after the
    session continues.
11. Deleting a remote session creates a tombstone that prevents a stale local
    outbox from recreating it.
12. Optional sync failure does not break the Agent turn; required mode follows
    its documented offline threshold and never silently claims compliance.
13. An imported legacy session is labeled client-reported and does not acquire
    invented historical revisions.
14. `git diff --check`, documentation link checks, focused service/store tests,
    and both CLI/Desktop sync-state tests pass without touching the user's real
    `BUILDMAX_HOME`.

## 23. Evidence Needed Before Acceptance

### 23.1 User evidence

- How often do users need a prior session after the original task is complete?
- Do reviewers open the record and answer a question faster, or is a generated
  PR summary enough?
- Are handoffs usually to the same user on another device or to another person?
- How often does the target device already have a compatible checkout?
- Do users understand the difference between view, fork, and exact resume?
- Does required capture reduce willingness to use local Agent execution?

Useful pilot measures include session-link open rate, time from review question
to answer, percentage of cross-device attempts that pass compatibility checks,
pending-sync duration, conflict rate, storage per active user, and deletion or
privacy support requests.

### 23.2 Operator evidence

- Is the desired governance outcome central retention, content inspection,
  usage inventory, incident investigation, or prevention of unrecorded work?
- Must administrators read raw content, or is Space-controlled sharing enough?
- What retention and legal-hold rules apply to prompts, tool output, images,
  and traces?
- Is bounded offline execution acceptable under required capture?
- Which repositories and Spaces need automatic relation creation?

### 23.3 Technical evidence

- Size distribution of real session files and duplicate-prefix cost across
  revisions.
- Failure rate and latency of object-store uploads after completed turns.
- Compatibility of session schemas across released BuildMax versions.
- Accuracy of repository and workspace matching on users' actual layouts.
- Ability to render a safe useful projection without losing raw resumability.
- Reconciliation behavior under database/object-store partial failure.
- Whether the existing Worker session path can share storage and retention
  infrastructure without coupling local sessions to TaskRun ownership.

## 24. Open Questions

### Product and ownership

- Is a synchronized local session private in a personal Space by default, or
  should a managed repository map it directly to a shared Space?
- Does publishing to a Space copy a frozen revision or transfer the whole
  session?
- Can a Space member fork a space-visible session into a private personal
  session, and what provenance remains visible?
- Is session continuity primarily a personal feature or a space work-object
  feature?
- Should the product use “Session” everywhere while code uses
  `agent_session`, or choose a different user-facing term such as “Agent run
  history” or “work record”?

### Capture policy

- Which deployments need required synchronization?
- In required mode, what offline grace is both honest and usable?
- Is direct-model local execution capturable under the same policy, or does the
  policy apply only to managed-model mode?
- May a user exclude a single session containing especially sensitive data, or
  would that violate the deployment's purpose?
- Does the Server receive raw content, a client-encrypted blob, or both raw and
  redacted projections? Client-held encryption keys would prevent Portal
  rendering and organizational recovery.

### Identity and revisions

- Does the current UUID remain the external identity once an Agent session is a
  Server table, especially if the entity-identity proposal is accepted?
- Is one completed turn always one revision, including failed or canceled
  runs?
- Should metadata changes have their own revision stream for governance, or
  only ordinary updated timestamps plus audit actions?
- How long are tombstones kept after content erasure?

### Workspace and resume

- What exact repository identity normalization works for SSH aliases, mirrors,
  forks, and monorepos?
- Which dirty-workspace summary can be uploaded safely without storing the
  whole patch?
- Is a writer lease needed, or do optimistic revisions plus explicit fork give
  acceptable UX?
- What session fields must be portable when the model protocol changes?
- When current Server policy conflicts with the checkpoint's recorded setup,
  which mismatches block resume and which only warn?
- Does “continue on another device” require a Desktop handoff protocol, or is a
  normal authenticated list/download flow sufficient?

### Viewer and relations

- Which tool arguments and results are visible by default to a Space reviewer?
- Should a PR relation use provider checks, comments, a PR body field, or only
  a manually copied link?
- Can one revision relate to several commits or PRs, and how are superseded
  relations represented?
- Is a generic external relation worth its validation and authorization cost,
  or should the first release support only BuildMax entities plus GitHub?

### Retention and evidence

- Are session and trace retention one policy or separate policies?
- What does deletion mean when a frozen session revision is referenced by a
  retained TaskRun, Artifact, audit investigation, or pull request?
- Does a customer need legal hold before ordinary remote deletion ships?
- What integrity statement is useful and supportable for client-reported local
  work?
- Which content reads, exports, shares, and forced ownership changes require
  governance audit events?

## 25. Likely Destination If Accepted

Acceptance should not leave this proposal as a permanent shadow roadmap. It
would require:

1. Add the accepted priority and phase to `docs/ROADMAP.md`, likely after the
   current Beta gates and within the bridge between Desktop session polish and
   Space governance.
2. Move stable rationale into a Durable Agent Session design record and split
   security/retention decisions into existing governance designs where they
   belong.
3. Update the session architecture to distinguish local persistence from
   remote replication without moving I/O into core.
4. Update surface positioning only if synchronization changes the promised
   Desktop/Portal bridge.
5. Update the data model and store architecture when row structs and object
   keys exist.
6. Add user documentation for sync modes, visibility, links, download/resume,
   deletion, and failure states when each behavior ships.
7. Create focused implementation issues for Server metadata/blob storage,
   AgentApp outbox, Portal viewer, provenance relations, and cross-device
   continuation rather than one cross-repository feature branch.
8. Delete this proposal after the accepted decisions have a durable home.

If evidence supports only backup and recovery, accept Phase 1 and reject the
broader Space provenance/search scope. If reviewers only need outcome summaries,
strengthen Artifact and TaskRun views rather than retaining every local
session. If required capture is the dominant requirement, design the stronger
append-time evidence mode before claiming local session history is an audit
record. If cross-device resume repeatedly fails on workspace mismatch, keep
context fork and stop promising exact continuation until something can
reconstruct workspace state — nothing in BuildMax does, and nothing is planned
to.

## 26. Candidate Conclusion

> BuildMax should treat a synchronized local Agent session as a revisioned,
> Space-owned Server resource whose execution remains local. Completed-turn
> checkpoints provide private recovery first; immutable authorized URLs and
> typed relations then make Agent work reviewable from PRs, Issues, Tasks, and
> other systems. Cross-device continuation is explicit: view and context fork
> come first, exact resume requires workspace compatibility and a single
> writer, and divergence always creates a fork rather than an automatic merge.
> Raw local checkpoints remain client-reported sensitive content, not audit
> truth. Deployment policy decides whether capture is disabled, optional, or
> required without removing BuildMax's direct local-only mode.

This candidate is coherent with the current product boundaries and reuses
working local and Worker session mechanisms. It is still contingent on real
evidence that spaces value the provenance record, accept its content-access
model, and can operate its retention cost.
