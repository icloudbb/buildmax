# Space Secrets And Run Delivery

> **简体中文：** [阅读中文镜像](../zh-CN/design/Space密钥.md)

## Contents

- [Status](#status)
- [1. Problem](#1-problem)
- [2. Decision](#2-decision)
- [3. What This Does Not Protect](#3-what-this-does-not-protect)
- [4. Scope And Ownership](#4-scope-and-ownership)
- [5. Resource Model](#5-resource-model)
- [6. Consumption Configuration](#6-consumption-configuration)
- [7. Run Lifecycle](#7-run-lifecycle)
- [8. Delivery Modes](#8-delivery-modes)
- [9. Storage Backends](#9-storage-backends)
- [10. Authorization](#10-authorization)
- [11. Audit And Provenance](#11-audit-and-provenance)
- [12. Redaction](#12-redaction)
- [13. Existing Credential Debt](#13-existing-credential-debt)
- [14. Failure Semantics](#14-failure-semantics)
- [15. API Shape](#15-api-shape)
- [16. Package Boundaries](#16-package-boundaries)
- [17. Alternatives Rejected](#17-alternatives-rejected)
- [18. Phases](#18-phases)
- [19. Acceptance Evidence](#19-acceptance-evidence)
- [20. Open Questions](#20-open-questions)

## Status

- roadmap_priority: post-Beta hardening for the credential debt in §13; the
  space-facing surface is implemented foundation for [`R3`](../ROADMAP.md)
  candidate qualification. Credential-file delivery is R5 item 3; short-lived
  exchange, external providers, and workload identity are R5 item 6. It answers
  Phase D3 of
  [plugin-space-distribution.md](plugin-space-distribution.md), which deferred
  secret delivery to a follow-on record.
- status: `Phase 1 complete` — a Space owner stores a Secret (encrypted,
  no reveal), an Agent revision declares it, a run receives it in its
  environment through the worker route and the `env_scrub` allow-list, the
  materialization is recorded in `task_run_secret`, and the run's values are
  redacted from the trace, from tool results before the model, and from streamed
  output, manages Secrets through an owner-only Portal page, and configures an
  Agent's consumption in the agent editor, which flags a grant whose Secret or
  item no longer resolves. Phase 1 is complete; Phases 2–5 (file delivery,
  short-lived exchange, external providers, workload identity) follow.
- phase_1: complete — storage, agent consumption and its validation, the
  owner-only HTTP surface and worker delivery, the `task_run_secret` audit,
  exact-value redaction across trace, tool results, and stream, the Portal
  management page, the agent consumption editor, and its consumption-health.
- supersedes: the `run-scoped-secret-broker` proposal, whose settled decisions
  are here and whose remaining uncertainty is §20.
- model: a Secret is one Space-owned group of named items, stored as a single
  encrypted row; items are not versioned. Consumption is configured on the Agent
  revision, which pins it. §5 and §6 carry the reasoning; §17 records what a
  version table and a per-item flag would have added and why they are out.

## 1. Problem

A worker executes model-chosen commands on behalf of a Space, and useful work
needs credentials: an Agent driving `git` and `gh` needs GitHub authority, one
calling an internal service needs its credential, one deploying needs cloud
authority.

Before Phase 1, there was no Space-scoped Secret resource: credentials arrived
through the deployment environment without per-Agent selection or consumption
audit. Phase 1 closes that product gap. The remaining ambient worker credential
debt is tracked separately in §13 and Phase 0; adding Space Secrets does not
remove deployment object-storage credentials or change run-token delivery.

Two properties motivated the change; Phase 1 fixes the first, while §13 still
tracks the second:

- The old `env_scrub` denylist removed names such as `GITHUB_TOKEN`, so
  credentials usable on the CLI baseline disappeared in workers. Phase 1
  replaces this with a deny-by-default baseline that admits explicitly granted
  environment names while keeping BuildMax's own credentials denied.

- The worker still receives credentials that are not its Space's business at
  all — the deployment's object-store key, and in direct mode a provider API
  key. §13 records what has to leave.

## 2. Decision

BuildMax gains a Server-side Secret Broker that:

1. represents a Secret as a Space-owned, versioned resource, or as a reference
   to an external secret manager;
2. binds its use to an immutable Agent revision that declares which Secrets it
   needs and how each is delivered;
3. snapshots that authorization onto one TaskRun when the worker claims it;
4. materializes only the values computed for that run; and
5. delivers each value into the run as an environment variable, a rendered
   credential file, or both — whichever the Agent declared.

Values are write-only in every user-facing API. There is no reveal operation.

### 2.1 Delivery Is Run-Level, Not Per-Consumer

The delivery question has one answer and it is worth stating as a decision
rather than a detail: a credential is delivered to the **run**, not to a named
process inside it.

An earlier design delivered values only into named consumers — a stdio MCP
server's explicit environment, a typed HTTP authorization slot, a file handed
to one process — and refused delivery to model-chosen commands. It is rejected
because of what a BuildMax Agent is. The Agent composes shell commands and
invokes tools it selects at run time, including tools it writes during the run.
Any mechanism whose unit of work is "one adapter per tool" is sized against an
unbounded set, so it authenticates `gh` and not the Python script the model
wrote a minute later. That is not a smaller version of the feature; it is a
different feature that does not solve the problem.

The two supported modes are therefore both run-level. §17 records what this
costs and what was given up with it.

## 3. What This Does Not Protect

Both delivery modes put the value inside the run, where the Agent's own
commands can read it. `printenv` reveals an environment grant; `cat` reveals a
file grant. Nothing here prevents that, and no surface may describe it as if it
did.

What remains, and what each part is worth:

| Control | What it buys |
|---|---|
| Space ownership | One Space's values are unreachable from another Space's runs |
| Agent-revision consumption config | A run receives only the items its Agent configured, not the Space's whole set |
| TaskRun snapshot | The authorization is fixed when the run is claimed and cannot be widened from inside the run |
| Short-lived credentials | A disclosed value expires; exchange at run start is the main exfiltration control |
| Narrow provider scope | A repository-scoped token cannot act outside that repository, whoever holds it |
| Encrypted storage | A database or backup disclosure does not yield values |
| No reveal API | A value cannot be recovered through the management surface, only used by an authorized run |
| Audit and revocation | Every grant is attributable and can be withdrawn |
| Exact-value redaction | Reduces accidental appearance in logs, traces, and tool results; not a confidentiality boundary |

The boundary this design draws is around **which run may exercise which
authority, for how long, on whose behalf**. It is not a boundary around the
bytes of the value once a run is authorized to use it.

Two consequences that Portal copy and user documentation must carry, because a
Space that misreads them will grant a credential it should not have granted:

- an Agent can read every Secret granted to its run; and
- a member who can trigger a shared Agent can obtain the values that Agent
  holds, without ever gaining a binding or read permission of their own.

A Space that cannot accept this should narrow the credential's provider-side
scope, shorten its lifetime, or use a different Agent. Delivery mode is not the
lever.

## 4. Scope And Ownership

**Space is the only ownership scope.** A Secret belongs to exactly one Space,
which is the same boundary that owns Agents, plugin activations, and audit. An
Agent definition may consume only Secrets in its own Space; a consumption config
naming another Space's Secret is refused when the revision is saved, not at run
time.

There is deliberately no deployment-global, Space-independent Secret that Agents
across Spaces could name. The pressure to add one is real — an operator with one
shared internal credential would rather write it once — and it is refused for
now because a global value has no owner to attribute it to, no Space to revoke
it from, and no answer to "which Spaces' runs can read this". A Space that needs
the same credential as another Space creates its own Secret with the same value;
that duplication is the visible cost of an ownership model that stays
answerable.

This does not describe BuildMax's own credentials. The database password, JWT
signing key, KEK, object-store administration credential, and managed provider
keys are operator-owned deployment configuration, never Space Secrets, and never
delivered to a run as a grant. §5.3 keeps the classes apart.

There is also no Server-side Project or deployment-environment entity
introduced to scope Secrets. Space is the Server ownership boundary; the local
`Project` of `internal/core/localproject` is a client concept and irrelevant
here.

## 5. Resource Model

The `xxxRow` structs in `internal/infra/db` remain the schema source of truth
once written; the shapes below are what they must express. Public identifiers
use `NewPublicID` per [entity-identity.md](entity-identity.md); timestamps
follow [timestamp-representation.md](timestamp-representation.md).

The model is deliberately two tables: a Secret, and a per-run audit snapshot.
How a run consumes a Secret is not a table at all — it is configuration on the
Agent revision (§6), which is already append-only, so the revision pins what a
run consumed while the Secret's values stay live and rotatable.

### 5.1 `secret`

One Space-owned Secret. A **Secret is a group**: one name holding several named
**items** — `access_key_id` and `secret_access_key`, or `username` and
`password`. A single-value credential is just a group with one item.

| Field | Meaning |
|---|---|
| `id`, `public_id` | Numeric relational key and opaque public handle |
| `space_id` | Ownership and authorization boundary; see §4 |
| `name` | Space-unique, non-secret display name |
| `description` | Optional bounded explanation; must not carry an item value |
| `provider` | `embedded`, or an operator-configured external provider name |
| `state` | `active`, `disabled`, or `destroyed` |
| `ciphertext`, `nonce` | The AEAD-encrypted item map, or an encrypted provider descriptor |
| `item_names` | The keys present, stored in the clear |
| `wrapped_dek`, `key_id` | The DEK sealed by a KEK, and which KEK sealed it (§9.1) |
| `created_by`, timestamps | Administrative attribution |

The items are **one encrypted JSON object in one row**. The value is a
`map[string]string` — item name to item value — sealed as a whole. This is the
shape a small deployment reasons about, a backup carries, and a KMS wraps, and
it is what makes rotation correct without a version table: replacing the map is
one atomic write, so a run reading it reads a self-consistent set. There is no
window in which a run sees a new `access_key_id` beside an old
`secret_access_key`, which is the whole reason the items live together.

`item_names` is plaintext because a name is not a value, and because Portal
listing and §6 validation must work without decrypting anything. An item name is
an identifier — `[A-Za-z_][A-Za-z0-9_]*`, validated on write — so a whole group
can be injected as environment variables (§6.2) with no item that silently
cannot be.

Every item is write-only through the API; §15 has no reveal operation. The
non-secret parameters a rendered file also needs — a cluster's server URL and CA
certificate, an AWS region — are not items: §6.3 supplies them as literals on
the Agent, where they stay readable. Placement expresses the classification, so
no item carries a secret-or-config flag and creating a Secret is nothing but
typing item names and values.

`(space_id, name)` is unique. Renaming changes display metadata, not identity.
Disabling refuses new run grants and new materializations. Destruction erases
recoverable material once no active reference remains; it does not rewrite audit
history.

This design does not version a Secret's items. Grouping already gives atomic
rotation, which was the only correctness the version table bought; the rest —
an in-flight run keeping an old value — is moot because a run materializes once
at claim time and does not re-read. Two runs claimed on either side of a
rotation each get an internally consistent set, which is the honest guarantee.
§17 records what versioning would have added and why it is not worth its weight
here.

There is deliberately no `kind` field. Tagging a Secret with a credential family
would make storage responsible for interpreting the value and would assert that
one Secret is one whole rendered file — sealing a kubeconfig's non-secret server
URL and CA certificate inside the encrypted blob. Item names carry whatever
structure a credential has, and the rendering is chosen on the Agent.

### 5.2 `task_run_secret`

One non-secret audit snapshot of what a run was granted.

| Field | Meaning |
|---|---|
| `task_run_id` | The run |
| `secret_id`, `item_name` | Exactly which item of which Secret resolved |
| `agent_revision_id` | The consumption configuration that authorized it |
| `provider_version` | Exact external version resolved, if available |
| `delivery` | `env` or `file` |
| `env_name`, `file_target` | Resolved delivery target |
| `status` | `pending`, `materialized`, `revoked`, `expired`, or `failed` |
| `expires_at` | When an exchanged or leased credential stops working |
| `materialized_at`, `revoked_at` | Runtime lifecycle evidence |

It holds no ciphertext, plaintext, lease token, provider error body, or hash of
a value. A hash would enable offline guessing for a low-entropy item and is not
needed to explain the run.

### 5.3 Credential Classes Kept Apart

One storage and delivery mechanism must not absorb credentials whose lifecycle
is already different.

| Class | Examples | Owner | Handling |
|---|---|---|---|
| Deployment bootstrap | database password, JWT signing key, Secret KEK | operator | deployment injection; never a Space Secret |
| Server-managed upstream | model API key, object-store administration credential | operator | encrypted store or external reference; never delivered to a Space's run |
| Space execution | GitHub token, Slack token, internal service credential | Space | this design |
| Ephemeral run authority | run token, presigned URL, STS credential, Vault lease | Server or external issuer | minted or exchanged at run time; short TTL; not a reusable Space Secret |
| User authentication | password verifier, refresh token, webhook key | account subsystem | existing hash, rotation, and revocation models |

## 6. Consumption Configuration

How a run consumes Secrets is configured on the Agent, per the requirement that
a Space sets this up where the Agent is defined. It is not a separate binding
resource: it is a structured field on the Agent definition, carried into each
append-only Agent revision, and validated by the Secret service on save and
again when the worker claims the run.

Putting it on the revision is what replaces the version table. The revision pins
exactly which items a run consumes and how, so "what did this run use" is
answerable from immutable state, while the item values behind those names stay
live and rotatable.

### 6.1 What An Agent Declares

An Agent's consumption config is a list of entries, each either an environment
grant or a file grant. A grant may draw items from any Secret the Agent's Space
owns; one Agent commonly mixes several groups.

Every grant carries `required` (default true). A required grant that cannot be
produced fails the run before the Agent starts; an optional one is skipped and
the skip is recorded.

### 6.2 Environment Variables

Two forms, and the choice is the Agent's:

- **Selected items.** Name a `secret` and an `item`, and a variable name. The
  item arrives under that name. Items from different groups sit side by side —
  a `GITHUB_TOKEN` from one Secret and a `SLACK_TOKEN` from another — which is
  the ordinary case and the recommended default, because the config reads as a
  list of exactly what the Agent uses.
- **The whole group.** Name a `secret` with no item, and every item arrives
  under its own name, optionally with a `prefix`. This is Kubernetes' `envFrom`
  shape. It is supported as a convenience so a Space that has already grouped a
  credential need not restate every member, and §5.1's identifier constraint on
  item names is what makes it well-defined.

The whole-group form hands the run items the Agent did not name individually,
widening what §3 already concedes. It is a Space's call, not a default to reach
for, and Portal shows it as a whole-group grant rather than expanding it into a
list that implies each item was chosen.

The resolved variable names of a revision must not collide — across two
whole-group grants, or a whole-group and a selected one — and the collision is
refused when the config is saved, with `prefix` available to resolve it.

### 6.3 Credential Files

A file grant names a **renderer** and maps its parameters. The renderer is
BuildMax code that knows one credential family's file layout; the Agent supplies
each parameter from a Secret item or a literal.

| Renderer | Target | Parameters |
|---|---|---|
| `aws_credentials` | `~/.aws/credentials` | `access_key_id`, `secret_access_key`, `session_token?`, `region?` |
| `kubeconfig` | `~/.kube/config` | `server`, `certificate_authority_data`, `token` |
| `git_credentials` | `~/.git-credentials` (+ `~/.gitconfig`) | `host`, `username`, `password` |
| `netrc` | `~/.netrc` | `machine`, `login`, `password` |
| `npmrc` | `~/.npmrc` | `registry`, `auth_token` |
| `docker_config` | `~/.docker/config.json` | `registry`, `username`, `password` |

A parameter is satisfied by a `{secret, item}` reference or by a literal. The
literal is where a cluster's server URL and CA certificate belong — on the
Agent, readable, not sealed in a Secret. A `region` is a literal; only the
genuinely secret parameter is an item.

The renderer, not free text, owns `target_path` and file `mode`, which is what
keeps §8.3's write constraints enforceable: an Agent chooses a renderer and
fills its parameters but never names an output path, so it cannot render into
`.bashrc`. Built-in renderers cover the common families; a Space-defined template
for a family BuildMax does not ship is a Phase 2 open question (§20), not part
of the first shape.

A file validates when every required parameter is satisfied, every referenced
Secret belongs to the Agent's Space, and every referenced item appears in that
Secret's `item_names`.

### 6.4 Using An Item Twice

Nothing stops one item feeding both an environment grant and a file parameter —
a token useful as `GH_TOKEN` and as the `password` of a `git_credentials`
render. Both resolve from the Secret's current items in a given run, and the
snapshot records each target separately.

## 7. Run Lifecycle

```text
Space Owner                 Server                         Worker
    |                         |                              |
    |-- write/rotate Secret -->| re-encrypt the item map    |
    |-- configure consumption->| validate same-Space ownership|
    |                         |                              |
    |                    dispatch TaskRun                    |
    |                         |-- resolve Agent revision     |
    |                         |-- read its consumption config|
    |                         |-- snapshot task_run_secret   |
    |                         |                              |
    |                         |<---- claim with run token ---|
    |                         |-- require live matching run  |
    |                         |-- decrypt current items      |
    |                         |-- exchange short-lived creds |
    |                         |-- return computed bundle --->|
    |                         |                              |-- set declared env grants
    |                         |                              |-- render declared files
    |                         |                              |
    |                         |<---- report terminal state --|
    |                         |-- revoke leases, close grant |
```

The snapshot happens where the Agent revision and plugin pins are already
resolved: while the worker claims the run. The items are decrypted once, at
claim, so the run reads a self-consistent map even if a rotation lands a moment
later. Resolution never uses values the worker supplied, and a worker cannot
browse Space state.

The worker-facing operation returns the bundle computed for one TaskRun. It
accepts no Secret ID, name, or provider path. Its route is
registered in `internal/server/handlers/routes.go` and described in
`internal/server/static/openapi.json` when built.

Materialization requires all of: a valid run token whose run matches the path;
a non-terminal run in the expected execution state; the pinned revision's
consumption config; an enabled Secret whose current items still satisfy that
config; and a configured, healthy backend.

The response uses TLS, sets `Cache-Control: no-store`, and bypasses body
logging. Retrying a failed fetch is allowed — "read exactly once" is not useful
if a lost response permanently fails an otherwise valid run — and every
successful materialization is recorded.

## 8. Delivery Modes

Two modes. An Agent revision configures each independently; §6 says how.

### 8.1 Prerequisite: A Run-Scoped `HOME` — implemented

Neither mode is well-defined until the run owns its operating-system `HOME`.
A worker already gets a run-scoped `BUILDMAX_HOME` (the run's global directory,
`RuntimeTaskRunGlobalDir`) and a run-scoped directory of the space's persistent
files (`RuntimeTaskRunHomeDir`), but its OS `HOME` was whatever the container
image set, shared across every run in that container.

This is now fixed. `taskrun` gives each run a dedicated, empty OS `HOME` —
`<run-dir>/oshome`, distinct from both directories above — created `0700`,
scoped over the agent run with `HOME` and `USERPROFILE`, and gone with the
run's ephemeral tree. It is deliberately not `RuntimeTaskRunHomeDir`: that
directory holds the space's materialized files and is the wrong place for a
run's private tool state or a rendered credential, and it is deliberately not
`BUILDMAX_HOME`, whose global directory is uploaded after the run — a rendered
credential must not be. The two reasons it was needed: a rendered credential
file needs a private location tools look in by default, and anything a tool
writes to `~/.config` must not survive into an unrelated run.

### 8.2 Environment Variables

The runtime places each `env` grant into the environment of the commands the run
executes: a selected item under the variable name the Agent chose, or every item
of a group under its own name with an optional prefix (§6.2).

This is the universal mode. It needs no cooperation from the tool, covers
non-HTTP protocols, and works for a program written during the run. It is the
fallback whenever a credential family has no file convention, and the first mode
to implement.

Two rules bound it:

- a grant is placed under the name the config resolved to and no other,
  never additionally exported under a guessed alias; and
- the run's environment is otherwise the deny-by-default baseline of §13.1, not
  the worker's inherited environment. Delivering declared grants is not a reason
  to stop withholding the deployment's own credentials.

### 8.3 Run-Scoped Credential Files

The runtime runs a renderer (§6.3) to write a file under the run's `HOME`, in
the layout the credential family already defines, and tools find it themselves.
Each parameter is resolved from a Secret item or a literal before the Agent
starts.

The unit is the credential family, not the tool — this is what keeps the work
bounded. One rendered `~/.aws/credentials` serves the AWS CLI, every language
SDK, and Terraform at once. One rendered `~/.git-credentials` with its
`~/.gitconfig` serves `git` and everything that shells out to `git`. The mode
exists for two distinct reasons:

- some families have no usable environment form at all — `~/.kube/config` and
  `~/.docker/config.json` carry structure a single variable cannot; and
- where an environment form does exist, the file form additionally reaches tools
  that only read configuration.

Write constraints, all mandatory:

| Constraint | Reason |
|---|---|
| Target resolves inside the run's `HOME` | A file under the workspace is one `git add -A` from being committed, and is a candidate for artifact upload |
| No `..` traversal, no symlink following | The same, by another route |
| Mode `0600`, never executable | A credential file is data |
| Refuse shell-startup and command-bearing targets — `.bashrc`, `.profile`, `.zshrc`, `git` config keys naming a command | Rendering into one of these injects code into the run rather than delivering a credential |
| Removed when the run ends | The run directory is removed anyway; this must not depend on that |

### 8.4 Using Both

An Agent may place an item in a variable and wire the same item into a renderer
parameter (§6.4). Both resolve from the Secret's current items in a given run,
and the snapshot records each target separately. Where a family's file form
references a variable rather than embedding the value, the renderer uses that
form.

## 9. Storage Backends

### 9.1 Embedded Encrypted Store

The default for an out-of-the-box private deployment. Only ciphertext and
wrapped data-encryption keys are stored in MySQL.

Each Secret's item map is encrypted with AES-256-GCM or an equivalently
reviewed AEAD under a fresh random DEK and nonce, rewritten whole on every edit.
Associated data binds the ciphertext to the Space public ID, so a ciphertext
moved to another Space's row fails authentication -- the cross-Space isolation
the threat model defends. It binds nothing else. Per-deployment isolation is
already cryptographic: another deployment has a different KEK, so unwrapping the
DEK fails before GCM is reached, and binding a deployment id in the associated
data would instead break a disaster-recovery replica that deliberately shares
the KEK to read the same rows. It binds no Secret public ID either: that ID is
minted when the row is inserted, after the value is sealed, and an intra-Space
ciphertext swap needs database write access, which is the deployment operator
the model already trusts.

The DEK is wrapped by a KEK, which never belongs in the database or
`server.yaml`. A KEK provider interface (§16) has three implementations, and a
deployment selects one: a mounted key file for the portable baseline, a cloud
KMS using workload identity, or a Vault transit key where a deployment already
runs Vault. `key_id` on the row names which KEK wrapped that DEK, so unwrap
selects the right key without a value ever being stored.

**The default is the mounted key file, and these properties are decided rather
than left to implementation:**

- The file is mounted read-only into the Server process, mode `0400`, and lives
  nowhere under a workspace, `BUILDMAX_HOME`, artifact path, or trace path. A
  Kubernetes Secret or Compose volume delivers it.
- **`key_id` is `<backend>:<name>:<version>`** — `file:root:1`,
  `vault:transit/buildmax:2`, `kms:<alias>:<ver>`. The backend prefix is not
  decoration: a deployment that migrates from the file backend to KMS keeps
  rows wrapped by each, and unwrap has to route to the right one. `name` allows
  more than one logical root key, and `version` is the rotation counter. For the
  file backend the map is keyed by this exact string.
- **The file holds a set of KEKs, not one.** It is a map of `key_id` to key
  material plus a pointer to the one KEK new writes use. A single key would make
  the row's `key_id` versioning inert and would make KEK rotation a stop-the-
  world re-encryption. With a set, rotation adds a new KEK, moves the pointer,
  and rewraps each row's `wrapped_dek` in the background — the ciphertext never
  changes because the DEK did not.
- **The KEK is never passed through an environment variable**, not even for
  Compose convenience. A process environment is visible to the process and its
  children, which is the exposure this whole design withholds a value from; the
  root key that unwraps every value is the last thing to place there. Compose
  mounts the file like every other deployment.

DEK rotation needs no operation: every edit rewrites a Secret's row under a
fresh DEK, so a compromised or aged DEK is replaced by the next write. KEK
rotation is an operator action, `buildmax-server secret rewrap`, and its
mechanics are decided:

- The operator generates a new KEK, adds it to the key file under a new
  `key_id`, and moves the current pointer to it. New writes use it immediately.
  Existing rows keep their old `key_id` and stay decryptable because the file
  still holds the old KEK.
- `rewrap` walks every row carrying a `wrapped_dek` — Space Secret values and
  managed-model credentials, which share the deployment KEK, and later external
  descriptors — and for each unwraps the DEK with the row's named KEK, rewraps
  it under the current KEK, and updates `wrapped_dek` and `key_id`. The
  `ciphertext` never changes, because the DEK did not. Nor does any associated
  data: the DEK wrap binds none, so a rewrap cannot disturb the Space or
  credential AAD its payload was sealed under. It is batched, idempotent,
  resumable after interruption, and skips rows already on the target `key_id`,
  so re-running it is always safe. It reports how many rows it moved off each
  key, then how many rows each key in the file still protects.
- It runs with no downtime and takes no lock on the feature: rows are readable
  and writable throughout, since both KEKs are present. A concurrent edit that
  rewrites a row under the current KEK simply leaves nothing for `rewrap` to do
  on that row; `rewrap` uses an optimistic check — each row's write is
  conditional on the wrapped DEK it read — so it never clobbers a newer write.
  It does not move a row's `updated_at`: no value changed, and the model
  gateway rebuilds a cached provider client when that timestamp moves.
- Removing the old KEK from the file is a separate, explicit step, refused while
  any row still names it. The key file is operator-owned and read only at load
  time, so the refusal is where the file is loaded: the Server, and `rewrap`
  itself, count sealed rows by `key_id` and refuse to start while a counted key
  is missing from the file, naming the key and its row count. A rolling restart
  onto a file that dropped a referenced key therefore stalls with the old
  replicas still serving, instead of making rows permanently undecryptable.
  A Space Secret row's `key_id` is its plaintext column, so its count is one
  grouped `SELECT`. A model credential keeps its `key_id` inside its sealed
  blob, the one place it is written; the catalog is small and operator-curated,
  so the count reads those blobs rather than keep a second copy that could
  disagree. The same count runs with no key file configured, so sealed data
  without its KEK fails startup rather than reading as empty. The
  `buildmax-server model` commands load the file without the count, so
  `model set-key` can still reseal a credential whose key is gone.

KEK rotation is a deployment maintenance action, not a Space action: it emits a
server operational log, not a Space Secret audit event (§11), because no Space
value changed and the audit trail is Space-scoped.

The Server fails startup when encrypted data exists and its KEK is missing or
unusable. It must not generate a replacement key or treat values as empty.
Losing the KEK means losing every value it wrapped, and backup documentation
states that the key file is backed up and custodied separately from the
database — a backup holding both is a backup with no protection at all.

### 9.2 External Secret Reference

An external backend stores an encrypted descriptor naming a provider, a
provider-managed Secret, and a version selector. BuildMax does not copy the
value into its database. At resolution the Server authenticates with its
workload identity, retrieves the value or a dynamic credential, records the
exact provider version when available, and applies the same grant and delivery
rules as embedded mode.

Provider configuration is deployment-scoped and operator-managed. A Space cannot
submit an arbitrary Vault address or cloud endpoint that would send the Server's
provider identity elsewhere: the operator defines named providers, TLS roots,
regions, allowed path prefixes, and authentication methods, and a Space record
selects only among them.

Vault is the first external integration, because private deployment is a product
promise and Vault serves on-premises static and dynamic Secrets. AWS and Google
follow the same interface. Provider parity is not a first-slice requirement.

### 9.3 Short-Lived Credentials

Where a target supports it, the Server exchanges a stored long-lived credential
for a short-lived one at run start — a GitHub App installation token, an STS
`AssumeRole` result, a Vault lease — and delivers that instead.

This is not a convenience. Under §2.1's run-level delivery, the value is
reachable by the Agent, so lifetime and provider-side scope carry most of what
is left of the exfiltration control. A one-hour repository-scoped token that
leaks is a bounded incident; a stored organization-wide token that leaks is not.
An exchanged credential is preferred over a stored one whenever both are
possible, and `task_run_secret.expires_at` (§5.2) exists so a run and its
operator can see when it stops working.

Workload identity — a dedicated OIDC issuer letting a live TaskRun federate
directly with Vault, AWS, or Google — is the same idea without the stored
credential, and is the last phase rather than the first.

## 10. Authorization

The roles are `owner`, `admin`, and `member`, per
[space-governance.md](space-governance.md).

| Action | Owner | Admin | Member |
|---|---:|---:|---:|
| List Secret metadata and consumption health | yes | yes | no |
| Create a Secret, edit its items, disable, or destroy it | yes | no | no |
| Configure an Agent revision's Secret consumption | yes | yes | no |
| Save an Agent revision consuming a Secret that does not exist | no | no | no |
| Trigger an already authorized Agent run | yes | yes | yes |
| Read Secret audit events | yes | no | no |
| Reveal a value | no | no | no |

Members may indirectly use a credential by running an Agent whose owner
authorized it. That is necessary for shared automation, and under §3 it also
means the member can read the value. Both facts belong in the Portal surface;
neither may be implied away.

Value authority stays with the owner until BuildMax has finer Space grants;
consumption sits with `admin` because it edits an Agent, which `admin` already
owns, and it grants no ability to read a value the owner did not place. If operator evidence shows owners cannot be the operational Secret
managers, add an explicit `secret_manager` grant rather than quietly widening
`admin`.

This design does not depend on the unbuilt approval system. A future high-risk
or environment approval can gate materialization without changing the stored
model.

## 11. Audit And Provenance

Audit actions: `secret.created`, `secret.rotated`, `secret.disabled`,
`secret.destroyed`, `secret.consumption_changed`, `secret.materialized`,
`secret.revoked`, and `secret.access_denied`.

Shipped today: the space lifecycle events `secret.created`, `secret.disabled`,
and `secret.destroyed`, emitted by the space Secret handlers with the Secret
public ID as target and an empty detail; and `secret.materialized`, recorded per
run as the `task_run_secret` snapshot on the worker route. The remaining
actions — `secret.rotated`, `secret.consumption_changed`, `secret.revoked`, and
`secret.access_denied` — are not yet emitted; they arrive with the rotation,
consumption-change, and denial paths they name.

An event names the actor, Space, Secret public ID, Agent revision or TaskRun,
action, and a bounded non-sensitive detail such as the delivery mode and target
name. It
never carries plaintext or ciphertext, a hash of plaintext, a provider token,
lease ID, response body, or full path, an HTTP header or environment value, or
prompt, tool output, command arguments, or file contents.

The existing audit table can represent a materialization with the Secret as
target and the TaskRun as bounded detail. If volume or query needs make that
awkward, a dedicated append-only access ledger is the alternative. Two
partially overlapping trails would be worse than either, so exactly one is
authoritative for "which runs materialized this Secret?"

Trace provenance records handles, Agent revision IDs, delivery modes, target
names, and materialization outcomes. It records no provider locator and no
value.

## 12. Redaction

A run's materialized static values are registered with a per-run exact-value
redactor before the Agent starts. `secretscan` gains a `Redactor` over those
values, and the sinks the design names are covered:

- **the durable trace** — the trace `Recorder` carries a `Redactor` and every
  free-text field it writes passes through it, exact values then shape-based;
- **tool results before they enter model context** — `RunLoop` redacts a tool
  result once, in `executeCall`, before it reaches the `EventToolEnd` event, the
  model context, the hooks, and the application log (`logToolResult`); and
- **streamed output** — the worker's stream adapter redacts each delta before
  it reaches the watcher.

The tool-result and stream paths use `RedactExact` — exact values only, not the
shape scan — because there the output is one the consumer must still read: a
model continuing its work on a tool result, a person watching a stream, would be
mangled by blanking every token-shaped substring. The durable trace, a
diagnostic artifact, applies both.

Exact-value redaction ignores empty and very short values (below six bytes), so
ordinary output is not replaced everywhere, and skips oversized values so a
large certificate does not make every redaction pass unbounded. It is defense in
depth, not a boundary: a run that holds a value can encode or transform it past
the redactor, which is why the primary control stays withholding the value from
the general environment.

Under run-level delivery this is mitigation against **accident**, not against
the Agent. A run holding a value can encode, split, transform, or transmit it,
and a model told to print a credential will succeed. The purpose is to keep a
value from drifting into a durable artifact nobody intended it to reach — a
trace shipped to an operator, a tool result cached in a conversation. Every
description of it says so.

Artifact upload stays out. Rewriting arbitrary artifacts can corrupt outputs and
declaring every binary safe would be false, so this design claims no artifact
DLP. Exact-match scanning of bounded text previews, with a warning or
quarantine, is available later as its own measured decision.

## 13. Existing Credential Debt

Adding Space Secrets without removing broader deployment credentials from workers
would produce a narrow new door beside an open old one. Under run-level delivery
this matters more, not less: the run is now expected to hold its own grants, so
everything else it holds should be there deliberately.

### 13.1 The Sandbox Environment Denylist

`internal/infra/sandbox/env_scrub.go` strips secret-shaped variables from any
sandboxed child — an exact list including `GITHUB_TOKEN` and `GH_TOKEN`, plus a
suffix rule matching `_TOKEN`, `_KEY`, `_SECRET`, `_PASSWORD`, `_PASSWD`, and
`_PWD`. `Bash.childEnv` applies it whenever the sandbox is active, which on the
worker baseline is always.

It was the right instinct at the wrong altitude, and blocked this design
outright: a Space's declared `GH_TOKEN` grant never reached the shell, and
because the sandbox defaults off on the CLI baseline and on for workers, the
same Agent configuration worked locally and failed silently in a pod.

**Done — it is now an allow-list over the denylist:**

- BuildMax's own credentials — `BUILDMAX_RUN_TOKEN`, `BUILDMAX_JWT_SECRET`,
  `BUILDMAX_API_KEY` — are an `alwaysDenyExact` set that no allow-list can
  re-admit: they are the deployment's own authority, never a value a run is
  granted;
- a name this run declared as a grant passes even though it is secret-shaped —
  `Manager.AllowEnvNames` records the run's grant names, and `ScrubEnvList`
  admits them; and
- every other secret-shaped name is still stripped, as before.

`agentapp` sets the run's grant values in the process environment and passes
the grant names to the sandbox, so a grant like `GH_TOKEN` reaches the agent's
commands while the run token does not. The full deny-by-default flip of the
inherited environment — with an enumerated operational baseline (`PATH`,
`HOME`, `LANG`…) — is deliberately not part of this: it needs that baseline
defined and touches the escape-hatch open question (§20), so it stays separate.
Deleting the file was never the fix; the run token is a live credential for the
managed inference gateway and the worker routes.

### 13.2 Object Storage

Workers receive long-lived object-store credentials. The replacement is
Server-mediated object transfer, run-prefix presigned requests, or cloud
workload identity — respectively costing Server bandwidth, request
orchestration, or portability on MinIO deployments. Artifact upload already goes
through a run-scoped Server route; workspace and run-state transfer decide
whether the remaining credential can be removed entirely. This is prerequisite
work, not something the Space Secret feature absorbs.

### 13.3 Direct Model Credentials

Managed inference is the recommended cloud-worker mode because the provider key
and upstream details stay on the Server. Direct mode may remain for trusted
local execution, but a cloud worker should not receive a deployment-wide
provider key by default. The plaintext `llm_model.api_key` migrates to the
encrypted backend of §9.1 or an external operator reference. It stays
deployment-scoped and Server-only; it does not become a Space Secret.

### 13.4 Run Token Delivery

The worker clears `BUILDMAX_RUN_TOKEN` from its environment after reading it,
but Kubernetes keeps the value in the Job specification
(`internal/infra/k8s/job.go`). A per-run immutable Kubernetes Secret mounted as
a file, with an owner reference and restrictive RBAC, removes it from the Job
environment and lets garbage collection follow the Job. This is defense in
depth: the run token stays run-scoped, expires, and is refused by terminal-state
checks either way.

## 14. Failure Semantics

| Failure | Run behavior |
|---|---|
| Required grant unsatisfiable | Fail before the Agent starts, naming the Secret and the item or parameter |
| Secret disabled, destroyed, or missing a consumed item | Fail before materialization, naming the Secret and item |
| Renderer parameter unsatisfied | Fail before the Agent starts, naming the parameter; do not write a partial file |
| Embedded KEK unavailable | Server health degraded; refuse affected materialization without treating the value as empty |
| External provider unavailable | Retry within a bounded startup budget, then fail with a sanitized provider-class error |
| Run token invalid or run terminal | Refuse without revealing whether a Secret exists outside the run grant |
| Exchanged credential or lease expires mid-run | Renew where the provider allows it; otherwise fail rather than continue as unauthenticated work |
| Revocation fails after terminal state | Preserve the terminal outcome, record a sanitized revocation failure, retry out of band within a bound |

## 15. API Shape

User-facing operations are conventional resource APIs that are write-only for
item values: list and get Secret metadata, including `item_names`; create a
Secret with its items; edit items; disable, re-enable, and destroy. Editing
items supports two request shapes over the same operation — a per-item patch
(set or remove named keys) for a row-by-row editor, and a whole-map replace for
a raw-JSON editor — because Portal offers both and they must not be two
divergent code paths. An Agent revision's consumption config is written through
the Agent API and validated against Space Secrets there; a read-only
consumption-health view reports an item a revision consumes that a Secret no
longer has, and a renderer parameter nothing satisfies.

No response includes an item value. A write returns metadata and `item_names`
only. Request fields carrying values are excluded from request logging,
validation errors, and audit details.

The worker-facing surface returns only the grant set already computed for its
run — no list, get-by-name, or provider lookup. It is
`GET /api/worker/task-runs/{task_run_id}/secrets`, authenticated by the run
token, and its response carries the decrypted values, so it sets
`Cache-Control: no-store` and rides its own route rather than the run bundle
that carries plugins and instructions.

Route strings live in `internal/server/handlers/routes.go`, and
`internal/server/static/openapi.json` must describe them, including the absence
of a reveal operation.

## 16. Package Boundaries

| Area | Responsibility |
|---|---|
| `internal/core/secret` | Secret metadata, item map and sealed-bytes types, consumption config, run grants, errors, and narrow store interfaces |
| `internal/service/secret` | Secret lifecycle: item-name validation, sealing through a `Sealer`, item edits (patch and replace), state changes, space scoping; later renderer parameter resolution, materialization, exchange, and revocation |
| `internal/service/agent` | Validates an Agent revision's consumption against the space's live Secrets when it is saved, through a narrow `SecretLookup`, the same way it validates a plugin selection |
| `internal/infra/secret` | AEAD/envelope implementation, external provider adapters, and credential-exchange clients |
| `internal/bootstrap` | The `buildmax-server secret rewrap` KEK-rotation command, alongside the existing `run-token` admin command |
| `internal/infra/db` | Row structs and metadata/ciphertext persistence; no provider calls |
| `internal/server/handlers` | User and worker authentication, Space authorization, request/response shaping. Reading Secret metadata is owner-or-admin (`space.ActionReadSecrets`), managing values is owner-only (`space.ActionManageSecrets`); all routes are value-write-only and report 503 when no KEK file is configured |
| `internal/agentapp/taskrun` | Consume an authorized in-memory grant set, place environment grants, run renderers into the run's `HOME` |
| `internal/infra/sandbox` | Apply the §13.1 deny-by-default environment policy and admit exactly this run's declared names |

`internal/core` imports no configuration, cryptography provider,
infrastructure, GORM, Server code, or Agent application assembly. Configuration
selects provider implementations during bootstrap; it does not resolve Space
resources. The Secret service takes a small KEK/provider interface so embedded,
Vault, and cloud implementations do not leak into handlers or the runtime.

## 17. Alternatives Rejected

### Per-Consumer Injection Only

Deliver values only into named processes — a stdio MCP server's environment, a
typed HTTP authorization slot, a file handed to one consumer — and refuse
delivery to model-chosen commands.

It is the only rejected option that would have kept a value out of the general
run environment, and it would have made a release digest an enforceable
delivery condition. It is rejected because it reaches only the consumers
BuildMax has adapted, which is the wrong side of an unbounded set: the Agent's
central capability is invoking tools it selects at run time, and under this
model those receive nothing. It remains the right shape for a future consumer
that genuinely is one named process, and the plugin-digest binding it needs returns with it.

### Credential Helpers And `PATH` Wrappers

Register a credential helper, an AWS `credential_process`, or a `kubectl` exec
plugin pointing at a BuildMax broker; or place same-name wrapper binaries on the
run's `PATH` so a token reaches only the child.

Both are per-tool adaptation against an unbounded set of tools, for the same
reason as above. A version-control credential helper configured through a
run-scoped configuration file is a special case that costs nothing, because it
is one line of generated configuration rather than an adapter — where that is
true it belongs under §8.3's file rendering, not as a mechanism of its own.

### Credential Injection At The Sandbox Egress Proxy

Terminate TLS on the existing loopback proxy (`internal/infra/sandbox/proxy.go`),
attach an `Authorization` header per target host, and re-establish TLS upstream.
The value would stay in worker memory and never enter the run.

This is the only evaluated option that would change §3's honest statement: the
Agent could exercise the authority without ever holding the value, and because
the proxy sees method and path, request-level policy would become possible. It
also scales per target host rather than per tool.

It is rejected on cost, not merit. It requires a run-scoped CA and a deliberate
man-in-the-middle inside the run; certificate-pinning tools break; the CA must
be installed per language runtime, which is its own list; and it does nothing
for non-HTTP protocols. It is recorded here because the pieces it needs — the
loopback proxy, `HostMatcher`'s per-host patterns, and `Manager.ChildEnv`'s
proxy routing — already exist, so revisiting it later is a smaller step than it
looks.

### A `kind` Field On The Secret

Tag each Secret with a credential family — `github_token`, `kubeconfig` — and
use it to pick a default file renderer.

Rejected because it makes storage interpret the value and asserts that one
Secret is one whole rendered file, which would seal a kubeconfig's non-secret
server URL and CA certificate inside the encrypted blob. Item names carry a
credential's structure, and §6.3's renderer is chosen on the Agent.

### A Secret-Or-Config Flag On Each Item

Mark every item write-only or readable, so non-secret configuration could live
beside the credential and still be visible — the split Kubernetes draws between
Secret and ConfigMap, and GitHub Actions between secrets and variables.

Rejected because it charges a per-item classification to every Space on every
write to serve a case that placement already answers. A Secret holds what should
be write-only; §6.3's renderer literals hold what a cluster shares and should be
readable. Configuration is visible where it lives, and creating a Secret stays
nothing but typing item names and values.

### Versioning A Secret's Items

Keep an immutable version per write, so a run pins an exact version and rotation
never disturbs it.

Rejected as weight without a matching gain. The correctness a version table
would buy — no run seeing a half-rotated credential — is already delivered by
storing a group's items as one atomic row (§5.1): replacing the map is one
write, and a run decrypts it once at claim. The remaining property, an in-flight
run keeping a superseded value, is moot because a run does not re-read, and the
consumption config that says *what* a run used is already pinned by the Agent
revision. A version table would add a second history to reconcile, a retention
question for abandoned runs, and a decrypt-old-version path, for a guarantee the
single-row rewrite already makes. It can be added later if evidence demands
point-in-time value recovery; nothing here forecloses it.

### Deployment Environment Variables Only

Keep the current model and ask operators to put every credential in the worker
environment. No new database, crypto, UI, or provider code — and no Space scope,
no per-Agent account choice, no rotation, and no usable audit. Every run of
every Agent gets every value.

### A Deployment-Global Secret Scope

See §4. Refused because a global value has no owner to attribute it to, no Space
to revoke it from, and no answer to which Spaces' runs may read it.

### External Secret Managers Only

Requiring Vault or a cloud provider, with the worker retrieving values through a
workload identity, gives strong provider lifecycle and no BuildMax value
storage. It violates the out-of-the-box private-deployment promise, differs in
authorization per provider, and hands workers provider lookup ability unless
carefully brokered. External backends belong under §9.2, not instead of it.

## 18. Phases

### Phase 0 — Unblock And Isolate The Run

- **done** — give each run its own empty operating-system `HOME` (§8.1),
  distinct from `BUILDMAX_HOME` and the space-files directory;
- replace the `env_scrub` denylist with §13.1's deny-by-default,
  allow-declared policy, keeping BuildMax's own credentials denied. This lands
  with Phase 1's grant delivery, not alone: the allow-list is empty until a
  consumed name exists, the deny-by-default baseline needs the operational-env
  set defined, and it touches the escape-hatch open question (§20);
- replace deployment-wide object-store credentials with Server-mediated or
  run-scoped access (§13.2);
- make managed inference the cloud-worker default (§13.3); and
- move Kubernetes run-token delivery out of the Job environment (§13.4).

The OS `HOME` and the `env_scrub` policy are prerequisites for any delivery at
all; the `HOME` half is done. The rest must be explicit before BuildMax claims
a worker holds only what its run needs.

### Phase 1 — Embedded Space Secrets With Environment Delivery

- **done** — the `secret` and `task_run_secret` rows, items as one encrypted
  map, with the store scope passing against MySQL;
- **done** — envelope encryption with a portable mounted KEK;
- **done** — environment consumption config on the Agent revision, validated
  against the space's live Secrets when the revision is saved and versioned with
  it;
- **done** — owner-only create, item edit (per-item patch and whole-map
  replace), disable, and destroy over HTTP, gated on a configured KEK file, with
  the agent request carrying the consumption config;
- **done** — a run-token worker route resolves the run's agent consumption,
  decrypts each grant against the run's space, and returns the env bundle over a
  `no-store` response; the worker fetches it, sets the grants in the run's
  environment, and the `env_scrub` allow-list admits the declared names while
  still denying BuildMax's own credentials. A required grant that is disabled,
  destroyed, or gone fails the run; an optional one is skipped;
- **done** — a required grant that cannot be produced fails the run before the
  Agent does its work, an optional one is skipped;
- **done** — the space lifecycle audit events `secret.created`,
  `secret.disabled`, and `secret.destroyed`, emitted by the create and set-state
  handlers with the Secret public ID as target and no item name, value, or
  ciphertext in the event;
- **done** — the `task_run_secret` audit snapshot: the worker route records one
  row per materialized grant, idempotent on (run, secret, item) so a retried
  fetch records once, carrying no value; the write is fail-open beside a run
  that already got its grant; and
- **done** — per-run exact-value redaction removes a run's grant values from the
  durable trace, from tool results before they enter model context (and the
  hooks and log with them), and from streamed output; and
- **done** — the Portal Secrets page (owner-only management of metadata and
  items, create with a row editor or raw JSON, per-item edit, disable, destroy,
  §3's consequences in a notice) and the agent-side consumption editor (an
  owner or admin configures an Agent's env grants against the space's secrets in
  the create and edit modals). Consumption-health surfaces a grant whose Secret
  or item no longer resolves, in the editor and as a count on the agent card.
  Phase 1 is complete.

Environment delivery is first because it is universal and needs no renderer.
Phase 1 is complete, including the Portal surface and exact-value redaction.
Credential-file delivery and later backends remain in Phases 2–5.

### Phase 2 — Credential File Delivery

- the built-in renderers of §6.3, beginning with the families that have no
  usable environment form — `kubeconfig`, `docker_config` — then those where a
  file additionally helps: `git_credentials`, `aws_credentials`, `npmrc`,
  `netrc`;
- file consumption config on the Agent revision, mapping parameters to items or
  literals;
- every §8.3 write constraint, enforced by the renderer owning `target_path`
  and `mode`; and
- one item usable as a variable and a renderer parameter at once.

### Phase 3 — Short-Lived Credential Exchange

- exchange at run start where the target supports it: installation tokens, STS
  `AssumeRole`, Vault leases;
- `expires_at` on the run grant, surfaced;
- renewal behavior for runs that outlive the credential; and
- prefer an exchanged credential over a stored one whenever both are possible.

Placed before external providers deliberately: under run-level delivery,
credential lifetime carries more of this design's safety than provider breadth
does.

### Phase 4 — External Provider References

Operator-configured provider records and path ceilings; Vault first, then
evidence-driven AWS and Google; exact provider versions and access outcomes
recorded; dynamic leases where exposed; backup, outage, and rotation operations
documented.

### Phase 5 — Workload Identity

A dedicated OIDC issuer with rotating signing keys and JWKS; immutable TaskRun
subject and audience claims; exchange for Vault, AWS, or Google short-lived
authority; stored static cloud credentials removed from supported paths.

## 19. Acceptance Evidence

Phase 1 does not ship until these hold:

1. a database backup alone cannot recover a value;
2. no user-facing operation reveals a value;
3. a worker cannot obtain a Secret outside its stored TaskRun grants, and a run
   receives nothing its Agent revision did not configure;
4. an Agent revision consuming another Space's Secret is refused when saved, as
   is one naming an item the Secret does not have;
5. rotating a multi-item Secret is atomic: a run resolves every item from one
   decrypt, never one item from before a rotation and another from after;
6. the worker's inherited environment does not reach model-chosen commands, and
   BuildMax's own credentials are never delivered as grants;
7. rotation affects a run claimed after it, never one already materialized;
8. a terminal run cannot materialize again;
9. audit and trace metadata explain the grant without containing credential
   material; and
10. every surface describing the feature states that the run can read what it
    was granted.

Supporting work: a schema and crypto review covering nonce generation, AEAD
associated data, DEK wrapping, KEK loss, rotation, and destruction; an
operational drill restoring an encrypted backup with the correct KEK and failing
safely without it; a KEK-rotation drill proving `rewrap` re-wraps every row with
no downtime, is resumable after interruption, and that removing the old KEK is
refused while any row still names it; a rendered-file test proving every write constraint,
including refusal of a template targeting a shell-startup file or a path outside
the run's `HOME`; cross-Space and cross-run authorization matrix tests;
failure-injection for KMS and Vault timeouts, provider denial, lease expiry, and
redaction paths; and latency measurement for Secret and object-storage
brokerage so the boundary does not create an unexamined bottleneck.

Because these rows live in `internal/infra/db`, the scope needs
`./make test mysql` with a real DSN; a green `./make test` says nothing about
them.

## 20. Open Questions

1. **Decided** — reading Secret metadata (list and get, never a value) is
   owner-or-admin via `space.ActionReadSecrets`; managing values stays owner-only
   via `space.ActionManageSecrets`. An admin needs the list to configure an
   Agent's consumption, and the metadata carries no value.
2. Is an external-provider locator encrypted with the value, or is
   database-visible provider metadata necessary for operation and audit?
3. Which object-storage replacement preserves large-file performance across
   local-process, Compose, and Kubernetes deployments?
4. Does a materialization append to the existing audit trail, a dedicated access
   ledger, or both with one explicitly derived from the other?
5. How is a lease renewed when the Agent loop is blocked in a long tool call,
   and what happens when renewal cannot complete?
6. Which OIDC claims are stable and useful to Vault, AWS, and GCP without
   exposing mutable names?
7. What artifact behavior is honest when a credential-bearing process writes the
   value, or a transformed form of it, into an output file?
8. Do the built-in renderers suffice, or does a deployment need Space-defined file
   templates — and if so, how are `target_path` and `mode` constrained so a
   template cannot render into a shell-startup file?
9. Does the run's environment baseline need an operator escape hatch for
   deployments that legitimately pass ambient configuration into runs, or does a
   grant cover every real case?
10. Does any real credential need point-in-time value recovery strongly enough
    to reintroduce item versioning, given §17 leaves the door open?
