# Unified Artifacts

> **简体中文：** [阅读中文镜像](../zh-CN/design/统一工件.md)

## Contents

- [Status](#status)
- [1. Decision](#1-decision)
- [2. Product Goal](#2-product-goal)
- [3. Current Baseline](#3-current-baseline)
- [4. Scope](#4-scope)
- [5. Domain Model](#5-domain-model)
- [6. Access And URLs](#6-access-and-urls)
- [7. Agent Tool Contract](#7-agent-tool-contract)
- [8. Authorization, Governance, And Limits](#8-authorization-governance-and-limits)
- [9. Storage And Failure Semantics](#9-storage-and-failure-semantics)
- [10. Delivery Phases](#10-delivery-phases)
- [11. Alternatives Rejected](#11-alternatives-rejected)
- [12. Open Questions And Evidence Needed](#12-open-questions-and-evidence-needed)
- [13. Acceptance Criteria](#13-acceptance-criteria)

## Status

- roadmap_priority: `P2 follow-on`
- status: `implemented` (§10 phases 1 and 2: the artifact object, storage, API,
  and Portal's top-level artifact list and detail page; `UploadArtifact` on
  every surface with a server, and artifacts on issue result cards. Registering
  a run's output directory is decided against — §12 question 6. Phase 3 external
  sharing, once decided against, is reopened and specified in
  [artifact-public-sharing-and-preview.md](./artifact-public-sharing-and-preview.md)
  — §12 question 4. Retention and the space storage quota are
  implemented too: `ArtifactRetainer` applies `ExpiresAt` and reclaims
  tombstoned objects, and `max_storage_bytes` on the quota tier is a hard
  admission check — §8 and §12 question 2. Phase 4 follow-ons stay open)
- The legacy per-run "output files" feature (the `task_run_artifact` table, its
  `RunOutputStorage`/`result.md` object namespace, the `.../artifacts` listing
  routes, and Portal's run-files modal) has since been removed. An artifact is
  now the only durable file a run produces; a run's reply is recorded on the
  TaskRun, and a Task's working files are recovered through its workspace
  checkpoint (see [task-workspace-checkpoints.md](./task-workspace-checkpoints.md)).
  §3 and §5 below describe that superseded baseline as historical context.
- follows: [surface-positioning.md](./surface-positioning.md) and
  [space-governance.md](./space-governance.md)
- roadmap: [../ROADMAP.md](../ROADMAP.md)
- created_at: `2026-08-22`

## 1. Decision

An artifact is a first-class BuildMax object, not a by-product of a task run.
The system provides one artifact capability, and anything that produces a file
a user should keep — an agent, a background run, a direct upload — uses that
capability. A task run that wants to preserve its output calls the artifact
service like any other producer. A run therefore keeps its reproducible output
directory, while the files it produced can be found, referenced, and shared
without knowing its task or run ID.

This is what separates the model from the `artifact`/`artifact_item` tables
that migration `0001_artifact_tables_to_task_run_artifact` removed. Those were
a child structure of a task run: no run, no artifact, and the only way to name
one was the run it hung off. The object described here owns its identity and
lifetime, and records its producer as provenance rather than as a parent.

The artifact's identity is its opaque public ID. That ID is what the service
returns, what a tool reports, and what a user cites. A URL is one surface's
rendering of that ID, not the identity itself.

Artifacts are a server capability. They live in a Space namespace, which is the
existing authorization boundary; a user working alone is represented by their
personal Space and need not see the Space concept in the UI. CLI and Desktop
reach artifacts by being logged in to a BuildMax server — the private
deployment case this product is built for. A local session running straight
against a model provider, with no server, has no artifact capability at all,
and no artifact tool appears in its tool list.

The shared Agent runtime exposes an `UploadArtifact` tool only where that
capability is present. It accepts an explicitly named local file, streams it
through the artifact service, records an Artifact, and returns the artifact's
ID and canonical URL.

The object store remains an implementation detail. A BuildMax artifact URL is
not a bucket key and is not an object-store presigned URL.

## 2. Product Goal

An agent, a user, or a background run should be able to make a useful file
available to the space and give the recipient one durable reference. The
recipient should be able to:

- preview or download it when they have space access;
- attach it to an issue, conversation, or result without copying storage keys;
- keep that reference in a document or message and have it still resolve; and
- intentionally create a revocable external sharing link when access outside
  the space is required.

The product language is **artifact**, not bucket, object, result directory, or
worker output path.

## 3. Current Baseline

BuildMax already has most storage primitives, but not the product object:

| Capability | Current behavior | Limit |
|---|---|---|
| Object storage | `internal/infra/objectstore` supports local filesystem and S3-compatible storage, including MinIO. | Keys are infrastructure details. |
| Space files | Portal accepts uploads into a mutable space file tree. | A file has no durable identity, provenance, immutable version, or share model. |
| Task-run artifacts | A worker archives `artifacts/` and records `task_run_artifact` paths. | A file is addressed through its task run and relative path; it cannot be independently referenced. |
| Artifact viewing | Space members can retrieve run output through space-authenticated task-run routes. | It is text/Markdown-oriented and not a general file-preview or download contract. |
| IDs | `ar_` and `f_` prefixes were reserved in `internal/util/id.go`. | They named a resource that was removed; see below. Type prefixes are gone entirely now — see [entity-identity.md](entity-identity.md). |

`task_run_artifact` is intentionally a set of paths, not a durable resource:
it has no public handle or timestamps. The new model must not reinterpret
that table as if it already provided the required contract.

An `artifact`/`artifact_item` table pair existed before the first public
release. Migration `0001_artifact_tables_to_task_run_artifact` in
`internal/infra/db/migration.go` removed it: it copies `artifact_item` into
`task_run_artifact`, drops both tables, and drops the `last_artifact_id` and
`artifact_seq` columns from `task` and `chat`. That model was a run's child
structure, which is why collapsing it into a path index lost nothing worth
keeping. Reusing the `artifact` table name is safe on any deployment that has
applied migration 0001, and the table this design creates is a different
object for the reason section 1 gives.

## 4. Scope

### 4.1 In Scope

- An Artifact in a Space namespace — a personal workspace being the user's
  personal Space — with a stable opaque ID as its canonical reference.
- One immutable file per Artifact in the first slice.
- Server-mediated upload, metadata lookup, preview, and download.
- `UploadArtifact` as a normal shared Agent tool, registered only on a surface
  that has an authenticated artifact service.
- Provenance for agent uploads, task-run outputs, and direct user uploads.
- Stable authenticated URLs and separate revocable external share links.
- Portal artifact cards, list/detail view, and safe lightweight previews.
- Migration of newly produced worker output files into the unified model while
  retaining the existing task-run artifact listing for compatibility.
- Space authorization, deletion/tombstoning, retention hooks, quotas, and audit
  events at the artifact boundary.

### 4.2 Out Of Scope

- A local artifact store for a CLI or Desktop session with no server login.
  Such a session keeps its normal local output behavior and has no artifact
  tool; see section 7.1.
- A general editable drive or synchronized local folder.
- Replacing the mutable space `home/` file space.
- A public-by-default file host.
- Multi-file archives, folders, version history, deduplication, or client-side
  encryption in the first slice.
- Direct browser-to-S3 uploads before the server upload contract and controls
  have proven adequate.
- A full document editor, arbitrary file conversion service, or malware
  scanner implementation. The API must leave room for these controls.
- Cross-space artifact ownership or copying artifacts between deployments.

## 5. Domain Model

### 5.1 Artifact

The Artifact model has one immutable content object per artifact. `SpaceID` is
required because Space is the authorization namespace, including for a user
working alone in their personal Space:

```go
type Artifact struct {
	ID              uint   `json:"-"`
	ArtifactID      string `json:"artifact_id"`
	SpaceID          string `json:"space_id"`
	Filename        string `json:"filename"`
	MediaType       string `json:"media_type"`
	SizeBytes       int64  `json:"size_bytes"`
	SHA256          string `json:"sha256"`
	StorageKey      string `json:"-"`
	CreatedByType   string `json:"created_by_type"`
	CreatedByID     string `json:"created_by_id"`
	SourceType      string `json:"source_type"`
	SourceID        string `json:"source_id,omitempty"`
	Title           string `json:"title,omitempty"`
	DeletedAt       *int64 `json:"deleted_at,omitempty"`
	ExpiresAt       *int64 `json:"expires_at,omitempty"`
	CreatedAt       int64  `json:"created_at"`
}
```

There is one artifact store. A logged-in CLI or Desktop session creates the
same Artifact, in the same Space namespace, through the same service Portal
uses; the surfaces differ only in how they render the result. Should a
local-only store ever be added, it must issue canonical public IDs from the start,
so that the reference a tool returns means the same thing on every surface.

`StorageKey` is private and generated by the storage adapter. No API, tool
output, trace, or UI exposes it. `SHA256` is calculated while streaming the
upload and proves what was stored; it is not a cross-space deduplication key.

Initial source types are:

| Source type | Source ID | Meaning |
|---|---|---|
| `agent` | agent run or session ID when available | An agent invoked `UploadArtifact`. |
| `task_run` | task run ID | The worker materialized the file as run output. |
| `user_upload` | optional request/audit correlation ID | A member uploaded a file directly. |
| `system` | optional operation ID | BuildMax generated the file without an agent call. |

`created_by_type` distinguishes a user, agent, worker, or system actor; it
does not invent a user ID for automated work. This follows the existing audit
model's actor rule.

An artifact does not contain a free-form metadata JSON column. The fields
above are deliberately queryable and bounded. Add a concrete field only when a
product behavior needs it; arbitrary JSON is an easy place for content,
prompts, and credentials to leak into durable metadata.

### 5.2 Links To Work

An Artifact may be linked to a task run, issue, conversation, or result card.
In the first slice, `source_type` and `source_id` capture the producing
operation, and presentation derives the relevant task/issue/conversation from
that source. A separate many-to-many attachment table is deferred until one
artifact must be attached to independent work objects.

`source_id` holds the task **run**, not the task. The run is the operation that
produced the file, and it is what distinguishes one attempt from another; the
task, issue, and conversation are all reachable from it. A reader going the
other way — an issue asking what its work produced — has to gather every run of
every task, not each task's last one, or a retried task hides what its earlier
attempts published.

This avoids making an uploaded file depend on a task run merely because the
first producer was a worker.

### 5.3 Task-Run Output Is Not Artifacts

`task_run_artifact(task_run_id, relative_path)` stays the run's index of what it
left in its own output directory, and nothing there becomes an Artifact
automatically.

This reverses an earlier draft of this section, which had each uploaded file
create an Artifact on terminal run processing. Two things were wrong with it.
The copy is real: the compatibility route reads the run-output key space, so
registering the same files in the artifact key space stores every output twice.
And the harvesting is the one section 11 rejects — an agent's output directory
is exactly the place `.env` files, caches, and intermediate work end up, and
scanning it is no safer for being done by the server.

The two are different objects with different jobs. A run's output directory is
the reproducible record of what that run produced; an artifact is a file
someone is meant to keep. An agent turns the first into the second by choosing,
once, with `UploadArtifact`.

Nothing changes an old task-run path into an artifact ID, and old output is not
backfilled. The task-run route keeps its current response for the runs that
have one.

## 6. Access And URLs

### 6.1 Identity And Routes

The canonical reference is the artifact's public handle. It is unique on its
own — 96 bits of crypto-random data — so locating an artifact needs nothing
else. Space is an authorization fact the
record carries, not part of the address.

Two route shapes follow from that split:

```text
GET /api/artifacts/{artifact_id}          # detail
GET /api/artifacts/{artifact_id}/content  # content
GET /api/spaces/{space_id}/artifacts        # the space's listing
```

The ID-addressed routes resolve the artifact, read its `SpaceID`, and require
the caller to be a member of that space. This is a new authorization path. It
cannot reuse `access.Guard.UserAndPathSpace`, which takes the space from the
request path, so it needs its own guard method and its own entry in the space
authorization matrix test.

A caller who is not a member gets `404`, never `403`. The opaque
`artifact_id` is an identifier and not a credential, and no response may turn
it into an existence oracle — that is what section 13's non-enumeration
criterion means in practice.

The space-scoped route is the listing and space-view surface. It is not a second
address for one artifact.

The Portal provides a human-facing detail route in addition to these API
routes: `#/artifact/{artifact_id}`, resolving the same Artifact through the
same ID-addressed API and therefore under the same authorization. It carries no
space in its address for the reason this section gives, and reports a refusal in
the words the API's 404 permits — not found, without saying whether it exists.
The listing is `#/artifacts`, a top-level area rather than a space-settings
tab, because an artifact is what work produced rather than a knob that
configures the space.

### 6.2 External Share Links

External sharing is a separate capability, not a flag on the canonical URL.
Creating a link generates a high-entropy, non-guessable share token and a URL
such as:

```text
/shared/artifacts/{share_token}
```

A share link identifies one artifact and has at least:

- creator and creation time;
- optional expiry, with a bounded deployment default;
- revoked time; and
- optional download count for audit and future limits.

MVP policy was **authenticated space access only**. Public sharing is now
specified and reopened in
[artifact-public-sharing-and-preview.md](./artifact-public-sharing-and-preview.md):
a revocable stored share token, an anonymous `/api/shared/...` route, and a
server-rendered public link. That record answers the authorization matrix, the
revocation model, and the audit events this paragraph left open. Do not
implement permanent S3 presigned URLs as a shortcut: they bypass BuildMax
authorization, cannot be centrally revoked, and couple saved links to
object-store configuration.

### 6.3 Content Delivery

All content requests authorize or validate the share token before retrieving
the object. The server may stream content itself or, after authorization,
redirect to a short-lived object-store URL. The latter is an optimization, not
the public contract.

Content headers use stored, server-validated metadata; never trust an uploaded
filename or caller-supplied MIME type alone. Download uses a safe content
disposition and filename. Inline previews are an allowlist, not a browser
guess: plain text, Markdown rendered through the existing sanitizer, and safe
image/PDF types may be added deliberately. HTML, SVG, executable content, and
unknown types download as attachments in the first slice.

## 7. Agent Tool Contract

### 7.1 Availability

`UploadArtifact` is a shared runtime tool, assembled in `internal/agentapp`
alongside other default tools. It is registered only where the surface has an
authenticated artifact service: a server deployment's own runtime, or a CLI or
Desktop session logged in to a BuildMax server. `internal/interface/auth`
already answers that question for the local surfaces — `IsLoggedIn` and
`CanAuthenticate(serverURL)` — so it is a precondition of tool assembly, not
something discovered at call time.

Where there is no such service, the tool is **absent from the tool list**. It
is not registered in a state where every call fails. A tool that exists only to
answer "unavailable" costs a round trip and teaches the model nothing, while a
tool that is not there is a fact the model can act on immediately.

The tool is never emulated by returning a source-file path. A path on one
machine is not a shareable object, and a session with no server keeps its
normal local output behavior instead.

### 7.2 Invocation

The agent supplies:

| Argument | Required | Rule |
|---|---|---|
| `path` | yes | A regular readable file within the effective workspace or allowed runtime output directory. |
| `title` | no | A short user-facing label; filename remains the content name. |
| `purpose` | no | A bounded human-readable note for the result card, not arbitrary file metadata. |

The tool must reject directories, device files, symlinks that escape the
allowed root, files over the active quota, and paths it cannot safely open. It
streams the file, creates the Artifact only after storage succeeds, and returns
the Artifact ID, filename, size, and canonical URL. A storage failure returns a
meaningful tool error and leaves no successful artifact record.

The tool does not auto-upload every file an agent writes. The model must choose
the final file it intends to present. This makes the output understandable,
avoids accidental publication of `.env` files or caches, and keeps storage
costs bounded.

### 7.3 Prompting And Results

The runtime prompt tells agents to use `UploadArtifact` for a file that should
be delivered, retained, or shared, and to cite the returned artifact reference
in their final answer. An Artifact result card should include title, filename,
producing run or agent, creation time, preview/download action, and the
canonical link.

The LLM-facing name follows `internal/tool/names.go`; when implementation
adds the constant, `manual/tools.md` becomes the user-facing source of
truth for its arguments and availability.

## 8. Authorization, Governance, And Limits

Artifacts are space resources. Space membership is the baseline read boundary,
and the user's personal Space is the private single-user case. An artifact never
derives authorization from a run URL alone. The detailed matrix
is part of implementation, but the proposed first-slice policy is:

| Action | Member | Admin | Owner | Outside space |
|---|---:|---:|---:|---:|
| Read/download space artifact | yes | yes | yes | no |
| Upload through an authorized agent/user flow | yes | yes | yes | no |
| Delete own direct artifact | yes | yes | yes | no |
| Delete any space artifact | no | yes | yes | no |
| Create/revoke external share link | no initially | future | future | no |

Deletion is a tombstone first: it immediately hides metadata and blocks
content access, records an audit event, then schedules physical object removal
under retention policy. It does not rewrite task-run history. A run page can
say its former output has been removed without leaking a storage key.

`ArtifactRetainer` in `internal/server/scheduler` is that retention policy and
the only thing that removes artifact content outside the upload-rollback path.
It sweeps hourly in two phases: expiry tombstones artifacts whose `ExpiresAt`
has passed, then the purge reclaims the objects of artifacts tombstoned before
`storage.artifact_purge_after_days` ago. That grace defaults to **0** —
deletion has already taken effect at the authorization boundary, so holding the
bytes afterwards is cost and exposure rather than safety. Setting days is for a
deployment whose bucket tooling offers an undelete; BuildMax offers none.

An artifact's `StorageKey` is the record of whether its bytes are still held.
Every artifact gets one at creation, so an empty key on a tombstoned row means
the object is gone. The sweep clears it rather than adding a `purged_at`
column: "no key" and "nothing stored" are the same fact, and two columns that
must eventually agree will one day not.

The object is removed before the row is updated. The reverse would let a crash
in between leave a row claiming the bytes are gone while the bucket still bills
for them; this way a crash leaves a row saying the object is there, and the
next sweep removes what is already absent — which §9's content-store contract
makes a success.

The service enforces per-file size and space storage quota before accepting a
file, and counts the final stored bytes. The per-file cap is
`storage.max_artifact_mb`; the space allowance is the quota tier's
`max_storage_bytes`, checked through `artifact.StorageAdmitter` — see §12
question 2 for why it is a hard admission check. Permitted MIME categories and
virus-scanning integration remain operator policy decisions; the Artifact
service exposes the required decision points but does not invent configuration
fields before they exist.

At minimum, audit these metadata-only actions: artifact created, artifact
deleted, artifact expired, artifacts purged, share created, and share revoked.
Expiry is recorded per artifact, because it is the one tombstone no member
asked for and a reader looking for why a file went has to find it named. A
purge is recorded per sweep with a count and a byte total, because the
tombstone that authorized each one is already in the trail and this says only
that the bytes are now gone. Audit records must not contain file contents,
signed URLs, share tokens, or a user-provided description that has not been
bounded and reviewed.

## 9. Storage And Failure Semantics

The storage key is private, generated by the adapter, and unrelated to any
URL. A representative shape for a server deployment is
`spaces/{space_id}/artifacts/{artifact_id}/content`, which makes object ownership
clear while letting bucket layout evolve behind the adapter. Its resemblance to
an API route is a naming coincidence and not a contract: nothing outside the
adapter may parse, construct, or depend on a key.

Existing task-run outputs are keyed by the creating user rather than the space.
`objectstore.RunOutputFileKey` produces
`<prefix>/<created_by>/artifacts/<conversation>/<task>/<run>/<path>`. New runs
write through the artifact service into its key space instead of that one.
Objects written before this design keep their old keys, stay reachable only
through the compatibility route, and are not copied.

The interface `internal/infra/objectstore` called `ArtifactStorage` was
run-output storage — its methods take `RunRef` and `RunObjectRef`. It is now
`RunOutputStorage`, matching the `cfg.RunOutputs` and `db/run_output.go` naming
already in use. Two interfaces sharing one name would have been the easiest way
for a later change to quietly re-invert the dependency direction this design
exists to fix.

The native capability's contract has since moved to its consumer as
`internal/service/artifact.ContentStore`, with the reference type in
`internal/core/artifact`. The direction is the same one this section argues for,
stated the other way round: the service says what it needs and an adapter
happens to satisfy it, rather than the storage package publishing an interface
the service must take.

Upload is write-once:

1. authorize and validate the source stream;
2. reserve an Artifact ID and generated storage key;
3. stream to a temporary or final object while measuring size and SHA-256;
4. commit the immutable Artifact metadata only after the object is durable;
5. remove any uncommitted object on failure, with a reconciler for crashes
   between storage and database work.

No content update endpoint exists. A changed file creates a new artifact and
may later be linked to its predecessor if product requirements demand version
lineage.

The storage interface remains portable across the server's local-filesystem
backend and S3/MinIO. That is a choice about where a deployment's object store
lives; it says nothing about whether a CLI or Desktop session is local, which
section 7.1 settles on its own terms. Server-side behavior, not
storage-provider URL behavior, defines the product contract.

## 10. Delivery Phases

### Phase 1 — Foundations

- Add core Artifact model, store, object-storage contract, and stable remote
  Space-scoped read/download APIs; expose a personal Space as a personal
  workspace in product UI.
- Add migrations, identifier generation, authorization tests, quota checks,
  and redacted audit events.
- Provide Portal listing/detail with download and narrow safe previews, as a
  top-level area — see §6.1.
- Keep public sharing disabled.

### Phase 2 — Agent And Worker Producers

- Add `UploadArtifact` to the shared Agent runtime and document it in the tool
  guide.
- Enable it for Portal task runs and for logged-in CLI and Desktop sessions,
  and leave it unregistered on a session with no server.
- Show deliberately published artifacts in conversation and issue result cards.
  A run's output directory is not registered — see §12 question 6.
- Retain task-run artifact route compatibility and migrate Portal consumers.

### Phase 3 — Intentional External Sharing — reopened

Originally not planned. It is now reopened and specified in
[artifact-public-sharing-and-preview.md](./artifact-public-sharing-and-preview.md),
because the product now needs an agent to hand a person one link that opens and
renders — most often a Markdown document, sometimes an HTML prototype. That
record settles the questions this phase deferred (approved roles, expiry
defaults, revocable tokens, anonymous access, audit) for the anonymous-link
slice; malware scanning stays out of scope there as here. The reopening is
exactly the "a deployment that needs it reopens this" this paragraph
anticipated, not a design that pre-empted the need.

### Phase 4 — Follow-ons

- Artifact attachments to multiple work objects.
- Batch/folder outputs, manifests, and optional lineage/version relationships.
- Direct-to-object-store upload optimization with server-issued short-lived
  upload credentials, only if the server-streaming path becomes a bottleneck.
- Content scanning and richer previews where deployment requirements demand
  them.

## 11. Alternatives Rejected

### Keep Artifacts Worker-Only

This preserves the simplest current implementation but makes logged-in local
sessions, direct user uploads, and future non-worker execution second-class. It
also forces users to understand task-run hierarchy to preserve a useful output.

### Give Server-Less Local Sessions Their Own Artifact Store

A local store would let `UploadArtifact` exist everywhere, which sounds like
the local-first answer. It is not: the tool would return two different kinds of
thing depending on the surface, and the model would have to explain which one
the user got. It also doubles the metadata model and the storage adapter, and
buys a second sync problem the moment anyone wants to publish upward.

Artifacts exist to be found, referenced, and shared by other people. A session
with no server has no other people in it. Such a session keeps writing files
where it already writes them, which is the behavior that fits it. Section 4.2
records this as out of scope rather than deferred, and section 5.1 records the
one condition a later local store would have to meet.

### Reuse Mutable Space Files As Artifacts

Space files are workspace state. They can be overwritten or deleted and do not
identify a producing operation. Calling them artifacts would make a saved link
change meaning as the workspace changes.

### Return Object-Store URLs Directly

This exposes deployment details, varies across providers, weakens auditing and
authorization, and makes revocation depend on object-store behavior. Short
lived signed URLs may still be used behind an authorized BuildMax endpoint.

### Give Every Agent Full Automatic Upload

Automatic directory harvesting is convenient only until it uploads secrets,
intermediate outputs, or large caches. An explicit tool invocation gives the
model and the user one legible publishing event.

## 12. Open Questions And Evidence Needed

1. **Space selection for local surfaces:** ~~settled for the first slice~~.
   `POST /api/artifacts` resolves the caller's personal Space when the request
   names none and honours an explicit `?space_id=` that the caller is a member
   of. A local client therefore publishes without ever being told about spaces.
   What remains open is only whether a client should be able to *choose* and
   remember a space, which is a settings question rather than an API one.
2. **Space storage quota:** ~~decided: the quota tier, as a hard admission
   check~~. `max_storage_bytes` joins `quota_tier` beside the run and token
   limits, and `QuotaService.CheckStorage` refuses an upload that would cross
   it — a 429, like the other two, because the file is fine and the space is
   full. It is separate from `Check` because storage is a stock and the others
   are rates: there is no window, `period_days` does not apply, and the total
   falls when an artifact is deleted rather than when time passes.

   Hard rather than alert-only, which is the opposite of what a rate limit
   would argue for. Tokens are already spent by the time a total is read, so
   refusing later work only stops more spending; bytes accepted stay accepted,
   and an operator who finds out afterwards has no remedy inside BuildMax. The
   seeded tiers deliberately set no storage limit, so a deployment acquires one
   only by choosing it — the same principle as `audit.retention_days`
   defaulting to keep everything.
3. **Sensitive-content controls:** which private deployments require malware
   scanning, DLP, or MIME restrictions before agent upload is enabled? Gather
   operator requirements rather than hardcoding a cloud-oriented policy.
4. **External sharing:** ~~decided: not planned~~ → **reopened**. Public
   anonymous links are now specified in
   [artifact-public-sharing-and-preview.md](./artifact-public-sharing-and-preview.md);
   authenticated cross-organization sharing stays out of scope there. See phase 3.
5. **Preview support:** which types can be rendered safely and usefully on all
   Portal clients? Start with an allowlist and measure demand.
6. **Run output as artifacts:** ~~decided: no~~. A run's output directory is
   not registered as artifacts, and no backfill of old output is planned. It
   would have meant a second copy of every output file — the compatibility
   route still reads the run-output key space — for the same directory
   harvesting section 11 rejects for agents, applied to a directory an agent
   wrote. An agent that wants a file kept publishes it deliberately with
   `UploadArtifact`; a run's output directory stays the reproducible record of
   what the run left behind. See section 5.3.

7. **A run's state can be silently short:** ~~open~~ → **decided: fail the
   run**. Not an artifact question — run output is deliberately not artifacts
   (question 6) — but it is the other half of "what a run left behind". The
   run-global upload in `internal/agentapp/taskrun/runtime.go` used to log a
   refused upload and report `SUCCEEDED` anyway, still pointing at a trace
   storage did not hold and leaving the session bundle the next turn resumes
   from missing. A run whose state cannot be stored now ends `FAILED` with a
   message naming object storage, keeping its reply and usage, and it records a
   trace pointer only once the trace is stored. The kind write-denial drill
   ([end-to-end-testing.md](end-to-end-testing.md) §6.6) is the deployed
   evidence.

## 13. Acceptance Criteria

The first usable increment is complete when:

- a user or authorized agent can upload one explicit file to a personal or
  shared Artifact workspace;
- the response carries a stable opaque ID that another authorized space member
  can resolve, preview when allowed, or download;
- a CLI or Desktop session with no server login has no artifact tool in its
  tool list at all;
- a non-member can neither use that reference nor learn whether it exists;
- object keys, provider credentials, and share tokens do not appear in API
  responses, tool output, traces, or audit events;
- a worker-created result appears as a unified Artifact without losing its
  task-run provenance;
- upload failure never reports a successful Artifact and leaves recoverable
  storage/database state; and
- deletion takes effect immediately at the BuildMax authorization boundary.
