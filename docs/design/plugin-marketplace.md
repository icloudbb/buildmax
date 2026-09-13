# Plugin Distribution And Private Marketplace

> **简体中文：** [阅读中文镜像](../zh-CN/design/插件市场.md)

> **Audience:** contributors and operators · **Status:** partly implemented —
> Phases A, B, and C ship; space and worker distribution D1 also ships and its
> remaining work is designed in
> [plugin-space-distribution.md](./plugin-space-distribution.md)
>
> User documentation for what ships:
> [manual/plugins.md](../../manual/plugins.md)

## Contents

- [Status](#status)
- [1. Decision](#1-decision)
- [2. Why Two Distribution Modes](#2-why-two-distribution-modes)
- [3. Plugin Directory Contract](#3-plugin-directory-contract)
- [4. Discovery And Local State](#4-discovery-and-local-state)
- [5. Runtime Resolution](#5-runtime-resolution)
- [6. Repository Distribution](#6-repository-distribution)
- [7. Private Marketplace Distribution](#7-private-marketplace-distribution)
- [8. Trust And Capability Inspection](#8-trust-and-capability-inspection)
- [9. Product Surfaces](#9-product-surfaces)
- [10. Provenance And Worker Use](#10-provenance-and-worker-use)
- [11. Implementation Ownership](#11-implementation-ownership)
- [12. Delivery Phases](#12-delivery-phases)
- [13. Alternatives Rejected](#13-alternatives-rejected)
- [14. Validation](#14-validation)
- [15. Open Questions](#15-open-questions)
- [Related Documents](#related-documents)

## Status

- roadmap_priority: `post-Beta, P4 follow-on`
- status: `partially_implemented` — Phases A, B, and C are shipped: the
  directory format and manifest, discovery, resolution and merging for skills,
  subagents, MCP, and hooks, collision and shadowing reports,
  `${BUILDMAX_PLUGIN_ROOT}`, per-run provenance in traces, the local CLI
  commands, and the Marketplace itself — packaging, the catalog and its
  releases, package storage, publication, browse, download, and install, plus
  the Portal and Desktop surfaces. Phase D, space and worker distribution, is
  under way: D1 ships space activation, agent selection (including the Agent
  modal's plugin field), Portal management, server-side pinning, and worker
  materialization for skills and subagents. Executable hooks/MCP remain open, as
  does credential-file secret delivery; environment-variable secret delivery has
  shipped. Its record is
  [plugin-space-distribution.md](./plugin-space-distribution.md)
- follows: [enterprise-deployment.md](./enterprise-deployment.md),
  [space-governance.md](./space-governance.md), and
  [system-administration.md](./system-administration.md)
- relates_to: [hook-system.md](./hook-system.md),
  [tool-permissions.md](./tool-permissions.md), and
  [trust-harness.md](./trust-harness.md)
- roadmap: [../ROADMAP.md](../ROADMAP.md)
- created_at: `2026-08-22`

## 1. Decision

BuildMax will treat a plugin as a reusable directory containing capabilities
that already work under `.buildmax/`. A plugin adds distribution and lifecycle;
it does not add another runtime or extension API.

The plugin directory contains the supported content directly. It does **not**
contain another `.buildmax/` directory:

```text
code-review/
├── plugin.yaml
├── README.md
├── skills/
├── agents/
├── mcp.json
├── hooks.yaml
└── hooks/
```

Installed plugins live directly under:

```text
<BUILDMAX_HOME>/plugins/<plugin-name>/
```

The default is `~/.buildmax/plugins/<plugin-name>/`. Respecting
`BUILDMAX_HOME` keeps tests, workers, and isolated installations out of a
contributor's real home.

BuildMax will support two distribution modes over this same directory format:

1. a Git repository cloned directly into the plugins directory;
2. an immutable release published to the deployment's private Plugin
   Marketplace and explicitly downloaded by a user.

These modes share validation, discovery, runtime resolution, and diagnostics.
They deliberately keep different update and trust semantics: a repository is a
developer-controlled working tree; a Marketplace release is an
administrator-published artifact identified by version and digest.

## 2. Why Two Distribution Modes

The two modes serve different parts of one lifecycle rather than competing to
be the only installer.

| Concern | Repository clone | Private Marketplace |
|---|---|---|
| Fits when | You are the only one depending on this copy | Others depend on this copy |
| Setup | `git clone` into the plugins directory | Browse and click/run install |
| Iteration | Edit and test in place | Publish a new immutable release |
| Update | Explicit Git operation | Explicit Marketplace update |
| Identity | Plugin name plus repository URL | Catalog ID plus plugin name |
| Reproducibility | Commit plus dirty state | Release version plus SHA-256 digest |
| Governance | Repository access and review | A stable identity to name later, plus a yank switch |
| Credentials | User's Git/SSH credentials | Existing BuildMax login |
| Requires a BuildMax server | No | Yes, to publish and to install |
| Server dependency at run time | None after clone | None after download |
| Future worker use | Unsuitable as a mutable source | Suitable as a pinned artifact |

### 2.1 Why Repository Clone Is Worth Supporting

Requiring an author to pack, upload, publish, and download after every edit
would make plugin development unnecessarily slow. A developer should be able to
run:

```bash
git clone git@code.example.com:agents/code-review.git \
  ~/.buildmax/plugins/code-review
```

and have BuildMax discover it on the next runtime start. The repository may be
on a branch, at a detached commit, or locally dirty. That mutability is useful
in development and must be visible rather than prohibited.

BuildMax does not automatically pull a repository. Git already owns branches,
credentials, merge conflicts, and dirty working trees; reproducing those rules
inside an updater would be fragile. A later `buildmax plugin clone` command may
provide a convenience wrapper, but direct `git clone` remains a supported path
and must not require a BuildMax-generated lock record.

### 2.2 Why The Marketplace Still Packages Bytes

The Marketplace should not install by cloning the publisher's repository onto
every member's machine. Doing that would:

- require every consumer to hold Git host credentials;
- make a branch or tag mutable from the consumer's perspective;
- couple installation availability to the Git server;
- make the administrator's reviewed bytes difficult to identify later;
- make yank, audit, and deterministic rollback ambiguous.

The Marketplace therefore stores an archive made from the same plugin root. A
published release is immutable and identified by `(plugin, version, digest)`.
The user performs an explicit download/install action; publication alone never
changes local capability.

### 2.3 Which Mode A Copy Belongs In

It is tempting to describe this as development versus production. That is the
wrong cut, and stating it that way would make the design claim things that are
not true.

The question that actually decides the mode is **who else depends on this
copy**. One person's plugin, maintained on their own machine and used only by
them, is legitimately a clone forever — it is production for them, and pushing
it through an administrator's release machinery would buy nobody anything. A
plugin three spaces run in their daily work needs a stable identity and a kill
switch long before it stops changing every week.

Two consequences follow, and neither is a defect to fix later:

- **A deployment is not a prerequisite for using plugins.** The Marketplace
  belongs to a BuildMax deployment (§7.1) and installing from it requires a
  server login. The CLI runs as a single binary with no server at all, and that
  is a product priority rather than a degraded mode. For that installation
  shape a cloned or hand-placed directory is the only mode there is, and it is
  the final one, not a staging area.
- **A deployment that does have a Marketplace still cannot assume everything
  came from it.** Today only a System Administrator may publish (§7.1), so an
  engineer whose plugin is genuinely useful to their space has no self-serve way
  to give it a stable identity. Some of them will keep a clone instead. A
  convention that is quietly violated at scale is worse than no convention,
  because it makes provenance look answered when it is not. §4.5 gives an
  operator a way to make the rule real where they mean it, and open question 4
  records the authority gap that causes the pressure in the first place.

### 2.4 One Promotion Path

The intended flow is:

```text
Git repository
    ↓ clone and edit in ~/.buildmax/plugins
local validation and testing
    ↓ pack and publish a named version
private Marketplace release
    ↓ explicit user install
managed local copy in ~/.buildmax/plugins
```

There is no automatic synchronization between a repository installation and a
Marketplace installation. Converting one source type to the other requires an
explicit replace operation so BuildMax never overwrites a working tree.

## 3. Plugin Directory Contract

### 3.1 Supported Content

The plugin root may contribute only content already supported in a workspace
`.buildmax/` directory:

| Path | Meaning |
|---|---|
| `skills/<name>/SKILL.md` | Skill instructions and their adjacent resources |
| `agents/<name>.md` | Subagent definitions |
| `mcp.json` | MCP server definitions |
| `hooks.yaml` | Hook definitions |
| `hooks/` | Scripts or resources referenced by `hooks.yaml` |

`README.md` and `LICENSE` may accompany the runtime content. Unknown runtime
files are reported by validation rather than silently treated as a new plugin
feature.

A plugin may contain only a subset. A skill-only plugin is valid; so is a
plugin that provides MCP configuration and no skill.

### 3.2 `plugin.yaml`

The field reference for authors is
[manual/plugins.md](../../manual/plugins.md); what follows is why the format
stops where it does.

The manifest carries the identity a directory cannot derive for itself, the one
compatibility bound worth stating before a release exists, and the environment
a plugin expects an operator to supply. Everything a plugin *does* is still
derived from the payload rather than declared here.

```yaml
name: code-review
version: 1.2.0
description: Company code review skills and agents.

display_name: Code Review
homepage: https://code.example.com/agents/code-review
maintainer: Platform Space <platform@example.com>
license: Apache-2.0

min_buildmax_version: 0.9.0

env:
  GITHUB_TOKEN:
    description: Token the github MCP server authenticates with.
  REVIEW_WEBHOOK_URL:
    description: Where the post_tool_use hook posts review results.
    required: false
```

| Field | Required | Meaning |
|---|---|---|
| `name` | yes | Stable local identity and default catalog slug. Lowercase, hyphen-separated. |
| `version` | to publish | Semantic version of the next release. Absent is fine in a tree that is never published. |
| `description` | no | One line describing what the plugin is for. The catalog's search and listing text. |
| `display_name` | no | Human-readable title for Desktop and Portal. Defaults to `name`. |
| `homepage` | no | Where a user reads more. Displayed, never fetched. |
| `maintainer` | no | Free-form owner string — who to ask. Not an identity BuildMax authenticates. |
| `license` | no | SPDX identifier, for display. |
| `min_buildmax_version` | no | Oldest BuildMax this plugin is known to work on. |
| `env` | no | Environment variables the payload expects, keyed by name. |

Only `name` is required to load a directory. The four display fields are inert:
nothing in the runtime reads them, and a wrong value misinforms a reader without
changing behavior.

#### Version Belongs In The Manifest, Not In The Publish Command

BuildMax itself takes its version from a Git tag rather than a source file,
because a pipeline builds its release from that tag. A plugin is published by a
person running a command against a directory, which may not be a Git repository
at all, so the same rule would fail: deriving a release version from a tag would
require Git, break the `local` source type, and reintroduce the Git coupling
§2.2 rejects.

Putting the version in the manifest makes the bump a reviewable line in a diff
rather than an argument in one person's shell history, and it gives the
`409` on a duplicate `(plugin, version)` an obvious remedy: edit and commit
the manifest.

The version is a claim about the *next* release, never about what is installed:

| Source | What identifies the local copy |
|---|---|
| `repository` | Commit and dirty state. The manifest version is shown as *declared*, not as installed identity. |
| `local` | Directory path. Same treatment. |
| `marketplace` | The release row: version and digest, which are what the manifest said at publication. |

So a dirty working tree that still says `1.2.0` is not lying about anything —
status never claims it *is* 1.2.0.

#### One Lower Bound, No Constraint Grammar

`min_buildmax_version` is a single version, not a range expression. The case
worth expressing is "this plugin uses a hook event added in 0.9.0". Upper bounds
and range operators would demand a constraint parser for a case that is rare and
usually a guess about a future that has not happened.

| Situation | Behavior |
|---|---|
| Client is older | Install and update refuse and name the bound. An already-installed copy still loads, with a warning. |
| Client reports `dev` or an untagged build | Treated as satisfying every bound; `validate` notes that the check was skipped. |
| Field absent | No constraint. |

Refusing at install and warning at load is deliberate: install is the moment a
user can still pick another release, while blocking a developer's existing clone
after a downgrade would be a worse failure than running it.

It also gives release selection something to work with. `buildmax plugin install
<name>` without `--version` picks the newest release that is not a prerelease,
not yanked, and whose `min_buildmax_version` the client satisfies. Prerelease
versions such as `1.3.0-rc.1` publish normally and are reachable only by exact
version.

#### The Environment Contract Is Documentation, Cross-Checked

Validation already derives which environment variable *names* a payload
references. What it cannot derive is what a name is for or whether the plugin
degrades without it, and that is the most common thing a user is missing at
install time. `env` supplies exactly that, and nothing else: names, prose, and
an optional `required` flag that defaults to `true`.

Because a declaration that nobody checks becomes fiction, the validator compares
it against derived usage:

| Situation | Result |
|---|---|
| Declared and referenced | Reported as the plugin's environment contract |
| Declared, never referenced | Warning: stale declaration |
| Referenced, not declared | Warning: undocumented requirement |
| `required: true` but unset when a runtime assembles | Reported in status and diagnostics; the run proceeds |
| A value-shaped key under an entry | **Error.** A manifest must never carry a secret |

Unset-but-required does not block a run, matching how MCP and hooks already
degrade: a server that cannot start reports that it could not start. The
manifest's job is to make the cause legible before the failure, not to add a new
gate.

#### Unknown Fields

The parser accepts unknown fields so the format can grow additively, and
`buildmax plugin validate` lists them. Silence in both directions would turn
`descripton:` into an invisible mistake; an error would make every older client
reject a newer plugin. `manifest_version` is reserved and unset means 1. It
should be required only when an incompatible change actually exists.

#### What The Manifest Still Does Not Declare

| Not included | Why |
|---|---|
| Content lists (`skills: [...]`) | Duplicates the directory that is already the source of truth, and rots on the next added file |
| Declared permissions or tool allow-lists | A plugin cannot grant itself permission (§8), and a declaration can disagree with the payload it sits next to |
| Dependencies on other plugins | Needs a resolver, a lockfile, and a story for diamond conflicts. No concrete plugin needs it yet |
| A namespace or prefix for contributed names | §5 makes a collision a hard error today. Namespacing changes tool-facing contracts and should follow evidence |
| Configurable payload paths (`hooks_file:`) | Convention already locates every supported file |
| Keywords and categories | A single deployment's catalog is small enough that `description` is the search surface |

### 3.3 Plugin-Relative Files

MCP and hook configuration may need to refer to scripts bundled with the
plugin. BuildMax supplies one source-scoped expansion value:

```text
${BUILDMAX_PLUGIN_ROOT}
```

It resolves to the directory containing `plugin.yaml`. Each plugin's MCP and
hook configuration is expanded with its own root before configurations are
merged. It is a BuildMax-provided value, not a process environment variable a
plugin can override.

Skills already receive their skill directory when invoked and need no new path
rule. `WORKSPACE_ROOT` keeps its current meaning: the workspace the agent is
operating on, not the plugin directory.

## 4. Discovery And Local State

### 4.1 Directory Discovery Is The Source Of Truth

At startup, BuildMax scans each immediate subdirectory of:

```text
<BUILDMAX_HOME>/plugins/
```

A directory with a valid `plugin.yaml` is a plugin. This is intentionally enough
for a manually cloned repository to work. Discovery does not depend on a
registry generated by BuildMax.

Directories beginning with `.` are reserved for BuildMax's staging, cache, and
state and are not plugins.

The manifest's `name` is the plugin's identity, not the directory it sits in.
A Marketplace install always creates `<BUILDMAX_HOME>/plugins/<name>/`, but
`git clone` takes a destination argument and a developer sometimes points it
somewhere else. A directory whose name differs from the manifest loads, with
the mismatch reported — refusing it would fail a plugin over the one detail
that carries no consequence. Two directories claiming one name is the §4.3
conflict instead: neither loads, and both directories are named, because
directory order must not silently pick a winner.

### 4.2 Manager State Is Supplemental

BuildMax keeps supplemental state at:

```text
<BUILDMAX_HOME>/plugins/.state.json
```

Entries are keyed by directory name rather than by plugin name. The directory
is the only identity that exists before a manifest parses, and a manifest that
does not parse is exactly when the disabled flag still has to be readable.

It records facts known to the installer, such as:

- source type: `repository`, `marketplace`, or `local`;
- repository URL and last observed commit, when available;
- Marketplace server, catalog ID, release version, and digest;
- whether the plugin is disabled;
- install and update timestamps.

The state file does not list plugin content and is not required for discovery.
A repository cloned manually appears as an unmanaged repository plugin until
BuildMax inspects its `.git` metadata. An ordinary directory appears as a local
plugin. BuildMax never writes source metadata into the plugin directory because
doing so would dirty a Git checkout.

State changes use a process lock and atomic replacement. If `.state.json` is
missing or damaged, valid plugin directories still load, but BuildMax reports
that Marketplace provenance or disabled state could not be recovered.

### 4.3 Source Replacement

The same plugin name cannot be active from two directories or sources. In
particular, Marketplace installation refuses to overwrite an existing Git
working tree. The user must explicitly remove, rename, or replace it.

Marketplace update is staged under a reserved dot directory, validated, and
atomically exchanged with the active directory. The previous managed copy may
be retained in a cache for rollback. Repository updates remain ordinary Git
operations performed by the user.

### 4.4 Runtime Snapshot

`agentapp` resolves plugins when it starts and keeps that snapshot for the life
of the assembled runtime. A clone, pull, install, update, disable, or removal
does not change an in-flight run. CLI sees changes on its next invocation;
Desktop rebuilds its runtime after a managed plugin action.

A repository edited while a run is in progress can still change a file the run
has not read yet. The trace therefore records repository dirty state at run
start, and the UI describes repository plugins as development sources rather
than immutable inputs. Authors who need reproducibility use a clean commit or a
Marketplace release.

### 4.5 Operator Source Policy

A deployment that means "only administrator-published plugins run here" needs
something to check, not only something to say. `<BUILDMAX_HOME>/policy.yaml` is
the existing operator-controlled file, so this is one more block on a mechanism
that already exists:

```yaml
# <BUILDMAX_HOME>/policy.yaml
plugins:
  allowed_sources: [marketplace]
```

Unset means every source loads, which is the current behavior. When set,
discovery skips a plugin whose recorded source type is not listed, and reports
each skipped directory by name and source rather than hiding it: a plugin that
silently does not load is a worse failure than one that refuses to.

This is the one place where §4.2's supplemental state stops being optional.
Source type comes from `.state.json`, so a directory with no record — or a
damaged one — has unknown provenance, and unknown is not `marketplace`. With
the policy set, such a directory does not load, and the reason it gives is that
its provenance could not be established, not that it was the wrong source.
Discovery stays fail-open where nothing was asserted and turns fail-closed only
where an operator asserted something, which is the behavior each of those two
positions deserves.

Two honest limits, both of which follow from §8:

- **This is fleet management, not a security boundary.** `policy.yaml` lives in
  the user's own `BUILDMAX_HOME`, so a local user can edit it. It is worth
  having where an operator controls the machine — a managed device, a built
  image, a container — and worth nothing against the user of an unmanaged
  laptop, who could equally write the configuration by hand.
- **It constrains provenance, not behavior.** Restricting sources says where
  bytes came from. Tool permissions, hook gates, and sandbox decisions are what
  constrain what those bytes may do.

`internal/config/permissions.go` records the matching precedent in the other
direction: a `policy.yaml` block for tool permissions was specified and then
dropped, because a worker's `BUILDMAX_HOME` is created fresh per run and an
operator policy placed on a long-lived host would not automatically reach it.
Plugin source policy therefore governs discovery in the home that actually
contains it; space activations and immutable run pins, rather than a persistent
worker plugin directory, govern which releases a background run receives.

Source classification lives with discovery rather than with each surface that
displays it. A recorded source is the answer when there is one; otherwise the
directory is classified by looking for `.git`, which is a stat rather than a
call to Git — asking Git would answer for the nearest enclosing repository, so
a plugins directory inside somebody's home checkout would make every plugin in
it look like a clone.

A policy that will not parse is reported and applies nothing. Refusing every
plugin because `policy.yaml` has a typo would turn one mistake into an outage,
and the scan says what it could not apply.

## 5. Runtime Resolution

Plugins add a third configuration source without changing existing tool
contracts. Precedence is:

```text
workspace .buildmax  >  user-global configuration  >  plugins
```

The concrete behavior follows each subsystem:

| Content | Resolution |
|---|---|
| Skills | Workspace first, then global, then plugins sorted by name; first name wins |
| Subagents | Workspace first, then global, then plugins sorted by name; first name wins |
| MCP servers | Plugins merge first, then global, then workspace; later server IDs replace earlier ones |
| Hooks | Additive order: global settings, plugins sorted by name, workspace |

Two plugins contributing the same skill, subagent, or MCP server ID are an
error, with both sources named. Alphabetic order exists for deterministic
loading and hook dispatch; it must not silently resolve an identity collision.

A global or workspace definition may intentionally override a plugin
definition. `buildmax plugin status` and Desktop diagnostics show that shadowing
instead of making the plugin appear fully active.

## 6. Repository Distribution

Repository distribution is supported by convention rather than by a new server
protocol:

1. The author keeps `plugin.yaml` and plugin content at the repository root.
2. A developer clones or checks out that repository under the plugins
   directory.
3. `buildmax plugin validate <path>` parses every contributed capability and
   reports conflicts and derived effects.
4. BuildMax loads the repository directly on the next runtime start.

Status reports, when Git metadata is available:

- remote URL;
- branch or detached state;
- full commit hash;
- whether tracked or untracked changes exist.

BuildMax does not require a remote, does not fetch during discovery, and does
not send Git credentials anywhere. Private repository authentication remains
the user's existing Git/SSH setup.

Discovery staying offline leaves one real risk: a clone never updates, and
nothing tells its owner it has fallen behind. `buildmax plugin status --fetch`
therefore compares against the remote **only when a user asks for it**, and
reports how far behind the checkout is. Explicit and on demand keeps the
no-network-during-discovery invariant intact while making the one failure mode
that actually bites long-lived clones visible.

Repository distribution has repository provenance rather than Marketplace
publication provenance. That is a difference in what can be named afterwards,
not a difference in review quality: a clone of a branch-protected internal
repository has stronger review provenance than a directory an administrator
uploaded from a laptop. What the Marketplace adds is an identity that survives
the event — a version and a digest someone can point at months later — and a
yank switch. §7.2 keeps the two kinds of provenance attached to each other
rather than letting publication discard the first.

## 7. Private Marketplace Distribution

### 7.1 Scope And Roles

The Marketplace belongs to the deployment, not a space:

| Action | Authority |
|---|---|
| Browse, inspect, and download | Any active authenticated user |
| Create or edit a catalog entry | System Administrator |
| Publish or yank a release | System Administrator |
| Archive a plugin | System Administrator |
| Install or update locally | User controlling that `BUILDMAX_HOME` |

This lets a System Administrator manage company capabilities without granting
access to space prompts, files, artifacts, or traces. Per-space catalog visibility
is deferred until there is evidence that a deployment-wide catalog is too broad.

Publication authority is a separate question from catalog visibility, and the
more pressing one. With administrator-only publication there is no self-serve
path between "a clone on one machine" and "a company-wide release", so a plugin
useful to one space has nowhere to acquire a stable identity. Open question 4
carries this; §2.3 explains why leaving it open has a cost.

### 7.2 Pack And Publish

Publishing takes a plugin directory. The version comes from the directory's own
`plugin.yaml` (§3.2), so there is no version argument:

```bash
buildmax plugin publish ./code-review
```

The client validates and packs the directory. When that directory is a Git
checkout, the client also sends its remote, commit, and dirty state alongside
the archive. The server independently:

1. authenticates the System Administrator;
2. streams the bounded archive while calculating SHA-256;
3. validates paths, `plugin.yaml`, and every supported payload parser;
4. rejects a manifest with no `version`, or one that is not a semantic version;
5. derives a sanitized capability report;
6. stores immutable bytes;
7. creates the release row and audit event, recording the release's
   `min_buildmax_version` for selection and the source provenance the client
   reported.

The server reads the version from the packed manifest rather than trusting a
client-supplied field, so the release row and the bytes cannot disagree.

Source provenance is what keeps §2.4's promotion path from breaking at its last
step. Without it a release is a directory that appeared on somebody's laptop,
and nothing connects the reviewed commit to the bytes everyone downloads.
Publishing a dirty checkout, or a directory that is not a repository at all,
stays permitted and is recorded and displayed as such — a plugin whose payload
was assembled rather than committed is a legitimate case, and the catalog
should say so rather than refuse it. Unlike version and digest, this provenance
is client-reported and the server cannot verify it; it is shown as a claim
about where the bytes came from, never as proof.

A first publish creates the catalog entry, taking its display name and
description from the manifest. Requiring a separate create would make the one
command above fail on its first use, and the authority is the same either way:
only a System Administrator reaches either route. The creation is recorded as
its own audit event, so a name that appeared by accident is visible rather than
inferred. `POST /api/admin/plugins` remains the way to reserve a name or edit
metadata without publishing.

Publishing the same `(plugin, version)` twice returns `409`, even for identical
bytes. A correction requires a new version, which means editing and committing
the manifest — the bump is reviewable rather than typed once into a shell.

### 7.3 Install And Update

A member explicitly installs a release:

```bash
buildmax plugin install code-review
buildmax plugin install code-review --version 1.2.0
```

Installation:

1. uses the existing BuildMax login for the selected server;
2. selects a release — an exact `--version`, or the newest one that is not a
   prerelease, not yanked, and whose `min_buildmax_version` this client
   satisfies;
3. shows version, publisher, digest, derived capabilities, and any declared
   environment variable that is not currently set;
4. downloads to a reserved staging directory;
5. verifies the SHA-256 digest before extraction;
6. validates the extracted directory again;
7. atomically places it at
   `<BUILDMAX_HOME>/plugins/code-review/`;
8. records Marketplace provenance in `.state.json`.

An exact `--version` that the client is too old for is refused with the bound
named, not silently installed.

Publishing does not push to clients. Updates are explicit and show the old and
new capability reports before replacement. There is no automatic update in the
first version.

### 7.4 Yank And Archive

- Yanking a release removes it from default install and update selection.
  Existing downloaded copies continue to work. Exact-version recovery requires
  an explicit acknowledgement of the yanked state.
- Archiving a plugin hides it from the default catalog and prevents new
  releases. It does not remotely delete local copies.
- Published bytes are never replaced in place. Product APIs expose no hard
  delete for a release that may explain a past installation or run.

### 7.5 Storage And Model

The server needs only two deployment-scoped concepts:

- `Plugin`: stable catalog identity, name, description, and archive state;
- `PluginRelease`: plugin, version, `min_buildmax_version`, digest, object key,
  size, publisher, publication time, sanitized inspection, client-reported
  source provenance, and optional yank state.

Package bytes sit behind a narrow plugin package storage interface with local
filesystem and S3-compatible implementations. They are not task artifacts and
do not inherit space artifact authorization or retention.

Implementation uses singular database tables and new prefixed public IDs, and
updates the data-model and ID references in the same change. The exact row and
API request shapes belong in the implementation slice rather than this first
design.

### 7.6 API Shape

The intended surface is small:

```text
GET  /api/plugins                                            shipped
GET  /api/plugins/{plugin_name}                              shipped
GET  /api/plugins/{plugin_name}/releases/{version}/download  shipped

GET  /api/admin/plugins                                      shipped
POST /api/admin/plugins                                      shipped
GET  /api/admin/plugins/{plugin_name}/releases               shipped
POST /api/admin/plugins/{plugin_name}/releases               shipped
PUT  /api/admin/plugins/{plugin_name}/releases/{version}/state shipped
PUT  /api/admin/plugins/{plugin_name}/state                  shipped
```

Both halves ship. `internal/server/handlers/routes.go` and
`internal/server/handlers/admin` are the source of truth for them now. Two
routes were added while implementing: an administrator listing, because hiding
a retired entry from the person who retired it leaves no way to restore it, and
an unarchive to match, following the enable/disable pairs the model routes
already use.

Publishing sends the archive as the request body rather than as a field inside
a document, so the bytes stream from the connection to disk with no decoder
holding them. The publisher's claim about the checkout travels as
`source_remote`, `source_commit`, `source_branch`, and `source_dirty` query
parameters, which keeps the body one thing. Download will stream the same way.

Refusals distinguish the request from its timing: a package this deployment
could not load is `400`, an upload past the size bound is `413`, and a version
that is already published or an entry that is retired is `409` — the same
request would have succeeded a moment earlier, so nothing about it is malformed.

Browsing and downloading need an active account and nothing more. A release
changes nothing until somebody installs it deliberately, so reading the catalog
is not a privileged action; publishing to it is, and that half is the
administrator's.

Which release to install is decided by the client, because only the client
knows its own version. The detail route returns every release including
withdrawn ones, marked, and download names one exactly. Downloading a withdrawn
release answers `409` until the request says `allow_yanked=true`: yanking is a
default-selection control rather than a deletion, so refusing outright would
strand the recovery §7.4 allows. The response carries the digest in
`X-Buildmax-Digest`, so a client verifies what it received without a second
request.

The audit trail records catalog creation, metadata change, release publication,
yank, and archive. It records actor, target, version, and digest prefix, never
package contents or configuration values. Downloads are ordinary authenticated
reads rather than governance events in the first slice.

## 8. Trust And Capability Inspection

Both source modes can provide active behavior:

- skills and subagents are instructions that can cause tool use;
- stdio MCP starts a process, potentially with credentials;
- HTTP MCP and hooks can reach network destinations;
- command hooks execute local programs.

The same validator inspects both modes and reports:

- skill, subagent, and MCP names;
- MCP transport, executable name, and HTTP host;
- hook event, type, executable name, and HTTP host;
- referenced environment variable names, and how they compare with the
  manifest's `env` declarations;
- local file paths referenced through `BUILDMAX_PLUGIN_ROOT`;
- collisions and shadowed definitions;
- unknown manifest fields.

It does not store command arguments, header values, environment values, prompts,
or file contents in the catalog inspection record.

Marketplace publication means “published by this administrator,” not “safe.”
Repository plugins are labeled with their repository and working-tree state.
Existing tool permissions, hook gates, sensitive-path checks, sandbox decisions,
and operator policy continue to apply; a plugin cannot grant itself permission.

The package contract forbids embedded credentials. The manifest's `env` block
carries names and prose only, and a value-shaped entry is a validation error.
Validation also rejects obvious private-key material and warns about suspicious
literal MCP environment values, but cannot prove that arbitrary instructions or
text contain no secret.

Direct clone does not weaken an existing enforcement boundary: a local user who
can clone into `~/.buildmax/plugins` can already write their own `.buildmax`
configuration. Deployments that need execution enforcement use operator policy,
not Marketplace provenance as a substitute.

## 9. Product Surfaces

### 9.1 CLI

The first useful command set is intentionally small:

```text
buildmax plugin validate [path]
buildmax plugin list
buildmax plugin status [name] [--fetch]
buildmax plugin publish <path>
buildmax plugin install <name> [--version <version>]
buildmax plugin update <name>
buildmax plugin disable <name>
buildmax plugin enable <name>
buildmax plugin uninstall <name>
```

Direct `git clone` is part of the repository workflow; a redundant BuildMax
clone command is optional. `uninstall` must not silently delete a dirty Git
working tree. For a repository source it prints the path and requires the user
to manage or explicitly confirm removal.

### 9.2 Desktop

Desktop reports what a project's runtime resolved rather than what the plugins
directory holds: a plugin whose skill a workspace overrides is not contributing
that skill however installed it is. Installing, updating, and removing go
through the same mechanism the CLI runs, and every action rebuilds the assembled
runtimes — a runtime keeps the inventory it started with, and Desktop is the
surface where the same person does both.

It shows:

- repository, local, or Marketplace source, and whether operator policy
  restricts which of those load;
- commit and dirty state, or release version and digest plus the source
  provenance recorded at publication;
- contributed capabilities and required environment names;
- collision, shadowing, validation, and update status;
- explicit install, update, disable, and uninstall actions.

### 9.3 Portal

Portal's System Administration area manages catalog entries and releases.
Normal users may browse the catalog, but Portal must not claim a plugin is
installed on a local machine it cannot inspect. The primary install surfaces
are CLI and Desktop; raw archive download from Portal is optional.

## 10. Provenance And Worker Use

Every run records the active plugin inventory at start.

For a repository plugin:

```text
name, source type, remote URL when present, commit, dirty flag
```

For a Marketplace plugin:

```text
name, catalog ID, version, digest, source server
```

This bounded metadata contains no package content or secrets. Skill invocation,
subagent start, MCP calls, and hook events should carry plugin origin when the
resolved definition came from one.

Portal and worker distribution consume only immutable Marketplace releases.
The server resolves an Agent's named plugins against the Space activation and
records exact release pins on the TaskRun; the worker downloads only those pins
with its run token and materializes them before runtime assembly. It never
clones a mutable repository, receives a developer's Git credential, or resolves
“latest” while starting a run. D1 admits skills and subagents only. Executable
hooks/MCP and secret delivery remain in the follow-on
[space distribution design](plugin-space-distribution.md).

## 11. Implementation Ownership

The dependency direction remains unchanged:

- `internal/config` discovers plugin roots, resolves source-relative paths, and
  merges plugin configuration layers;
- `internal/agentapp` assembles the resolved skills, subagents, MCP servers, and
  hooks for CLI and Desktop;
- `internal/core/plugin` owns Marketplace domain records and store interfaces;
- `internal/service/plugin` owns publication and catalog lifecycle;
- `internal/infra/db` persists catalog metadata;
- `internal/infra/objectstore` stores immutable package bytes;
- `internal/server/handlers` owns authenticated catalog and administration
  routes;
- `internal/interface/cli` and Desktop expose local workflows.

`internal/core/agent` remains unaware of plugins. It receives the same resolved
tools, instructions, and hooks it receives today.

## 12. Delivery Phases

### Phase A — Directory Format And Repository Workflow — shipped

- root-level plugin format and the `plugin.yaml` manifest;
- discovery under `<BUILDMAX_HOME>/plugins`;
- validation, merge rules, plugin-relative paths, and diagnostics;
- direct cloned-repository support, including the on-demand `status --fetch`
  drift check;
- plugin provenance in traces.

Acceptance: cloning a valid repository into a clean isolated plugins directory
makes its capability available to CLI and Desktop without copying files or
writing a generated registry.

### Phase B — Private Marketplace — shipped

- catalog records and package object storage;
- administrator publish, yank, and archive flows;
- authenticated browse and download;
- explicit local install/update with digest verification;
- client-reported source provenance recorded on the release;
- the `policy.yaml` `allowed_sources` operator control, which has nothing to
  restrict until a second source type exists;
- audit events and source-aware local state.

Acceptance: an administrator publishes one version from a tested repository;
another company account installs it by name; both sides report the same digest.
Held by `TestMarketplacePublishThenInstall` in the `cli` end-to-end suite, which
drives the released binary against a server it does not share memory with. The
account that installs holds no grant, so the authority split of §7.1 is tested
rather than assumed.

### Phase C — Product UI — shipped

- Portal catalog administration and browsing;
- Desktop Marketplace install and update;
- clear capability, source, dirty-state, and provenance presentation.

Acceptance: a member can discover and use a Marketplace plugin without editing
configuration or handling an archive manually. Portal's Account area lists what
the deployment publishes and hands over the command; Desktop resolves a release,
shows what it contributes, and installs it.

Portal never says a plugin is installed. Installing happens where the agent
runs, a server cannot see that machine, and a button there would be lying about
where it ran — so Portal offers a command and Desktop offers a button, each on
the side of the boundary it can honour.

### Phase D — Space And Worker Distribution, Under Way

The follow-on design this asked for is
[plugin-space-distribution.md](./plugin-space-distribution.md). It decides space
ownership, who may enable active hooks or stdio MCP, package materialization,
version pinning, and what bounds executable content once it reaches a worker;
it puts secret scope in a further record of its own.

## 13. Alternatives Rejected

### Marketplace Only

This gives the cleanest governance story but makes every development edit pass
through release machinery. It would push authors back to copying directories by
hand, outside the product. Supporting a directly cloned repository keeps the
authoring loop obvious and promotes the same bytes into the Marketplace later.

### Git Only

This is pleasant for developers and poor for broad company consumption. It
requires Git credentials and knowledge, exposes mutable refs, and provides no
central publication, audit, or stable digest. It remains a source mode, not the
managed company catalog.

### Marketplace Backed By Git Clone At Install Time

Storing only repository URLs and refs in the catalog looks cheaper than storing
archives, but moves mutability, Git authentication, and availability into every
installation. The reviewed release must be the bytes users download, so the
Marketplace stores packages.

### Version Supplied Only At Publish Time

An earlier draft kept the manifest to `name` and `description` and took the
version from `publish --version`, reasoning that an author should not have to
bump a field while iterating in Git. They do not: a version is bumped when
publishing, not when editing.

What the flag actually cost was a source of truth. The version existed only in
one person's shell history, so a `409` had no obvious remedy, the release row
and the packed bytes could disagree, and nothing in review showed that a release
was about to happen. Moving it into the manifest makes the bump a diff, and
§3.2 keeps it from ever being mistaken for installed identity.

### Copy Into Global Configuration

Copying skills into `<BUILDMAX_HOME>/skills` and merging JSON/YAML destroys
ownership information. Update and uninstall could not distinguish user-authored
content from plugin content. Keeping one directory per plugin makes its source
and lifecycle inspectable.

### General Plugin Runtime

Dynamic binaries or a new SDK create another extension contract beside skills,
subagents, MCP, and hooks and expand the execution boundary. Packaging current
behavior delivers reuse without that cost.

## 14. Validation

Implementation is not complete until tests prove:

- a plugin root needs no nested `.buildmax` directory;
- a manifest carrying only `name` loads, and every other field is optional;
- an unknown manifest field loads and is reported by `validate`;
- publish refuses a manifest with a missing or non-semantic `version`, and the
  release row's version comes from the packed manifest;
- a repository or local plugin never reports its declared version as installed
  identity;
- a client older than `min_buildmax_version` is refused at install, warned at
  load, and a `dev` build is refused at neither;
- default release selection skips prereleases, yanked releases, and releases
  the client is too old for;
- declared-but-unreferenced and referenced-but-undeclared environment names
  each produce a warning, and a value under an `env` entry is an error;
- a required environment variable that is unset degrades visibly without
  blocking the run;
- a manually cloned valid repository is discovered without `.state.json`;
- a directory whose name differs from the manifest name loads with the mismatch
  reported, and two directories claiming one name load neither;
- discovery never performs a network request or Git fetch, and `status` reaches
  the network only under an explicit `--fetch`;
- an unset `allowed_sources` loads every source, and a set one skips a
  non-listed source while naming each skipped directory;
- with `allowed_sources` set, a plugin whose provenance cannot be established
  from `.state.json` does not load and says which of the two it is;
- publishing a dirty checkout or a directory that is not a repository succeeds
  and is labeled as such, and the recorded provenance is presented as a
  client-reported claim;
- repository commit and dirty state appear in status and trace metadata;
- Marketplace installation refuses to overwrite a Git working tree;
- archive traversal, symlink, duplicate-path, file-count, and expanded-size
  attacks are rejected on publish and install;
- a modified or truncated download fails digest verification and is never
  installed;
- a published release cannot be overwritten;
- publication alone never changes an installed plugin;
- workspace and global definitions resolve ahead of plugin definitions;
- two plugins with a colliding runtime name fail with both sources named;
- global, plugin, and workspace hooks run in documented order;
- `${BUILDMAX_PLUGIN_ROOT}` is scoped to the originating plugin;
- an in-flight runtime keeps its resolved snapshot across managed updates;
- a non-admin cannot mutate the catalog and an admin gains no space-content
  access through catalog routes;
- audit and trace records contain provenance but no configuration values or
  secrets;
- CLI and Desktop load the same plugin inventory through `agentapp`;
- `git diff --check`, documentation link checks, focused Go tests, and relevant
  CLI/Desktop end-to-end suites pass.

Two of these are true by construction rather than by a test, and are worth
naming so nobody reads the list as fully held. Nothing in the audit or trace
records has a field that could carry a configuration value — the shapes were
built that way — but no test asserts it; and the CLI and Desktop load one
inventory because they share `agentapp`, which no test drives from both sides.

## 15. Open Questions

1. Should a future convenience clone command invoke the system Git binary or
   only validate a repository the user cloned themselves?
2. Should the Marketplace retain one previous local version automatically, or
   should rollback redownload an exact release?
3. Should Portal expose raw archive downloads, or direct users to CLI/Desktop
   so installation state stays truthful?
4. Should publication stay administrator-only, or should there be a self-serve
   scope — a user- or space-owned catalog entry that an administrator can
   promote — so that a plugin one space depends on can get a stable identity
   without an administrator in the loop? This is the authority axis, separate
   from the per-space *visibility* question §7.1 defers, and §2.3 argues it is
   the one creating pressure against the convention.
5. What evidence would justify dependencies between plugins, or a declared
   namespace for contributed names, given that §5 makes a collision a hard
   error today?
6. Should `min_buildmax_version` gate anything beyond install refusal and a
   load-time warning — for example, refusing to publish a release whose
   payload uses a hook event the declared bound predates?
7. Do regulated deployments need offline signatures in addition to TLS,
   administrator publication, and server-calculated digests?

## Related Documents

- [Skills and subagents](../../manual/skills-and-subagents.md)
- [MCP servers](../../manual/mcp.md)
- [Hooks](../../manual/hooks.md)
- [Configuration reference](../reference/configuration.md)
- [System administration](./system-administration.md)
- [Space governance](./space-governance.md)
- [Tool permissions](./tool-permissions.md)
- [Trust harness](./trust-harness.md)
