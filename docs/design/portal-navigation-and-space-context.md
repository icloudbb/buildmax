# Portal Navigation and Space Context

> **简体中文：** [阅读中文镜像](../zh-CN/design/Portal导航与Space上下文.md)
> **Audience:** Portal contributors and product designers · **Status:**
> implemented — slice 1 (navigation language: Home renamed to Chat, sidebar
> grouped into Work/Resources/Manage, Files exposed, deployment
> Administration separated from Space scope), slice 2 (canonical
> `#/spaces/{space_id}/...` routes; every Space-owned page reads its Space from
> the route, not a previously selected one), slice 3 (Space switching
> redirects every Space-owned route to a valid destination in the target
> Space, from one exhaustive, type-checked table — closing a gap where Agent
> and Task silently kept showing the previous Space's data on switch), slice 4
> (resolution states: an unrecognized route renders a not-found page instead
> of silently falling through to Chat; Agent, Workflow, Workflow Run, Task,
> and Issue detail distinguish a genuine not-found from a forbidden Space or
> resource), and slice 5 (orientation polish: the browser tab title names the
> page and, for Space-scoped routes, the Space; the narrow compact header and
> breadcrumbs carry the same Space/type/location cues as the desktop sidebar;
> the migration-bounded redirects for the pre-Space-prefix hashes are removed
> — an old flat hash is now an ordinary not-found, not a silent alias) have
> all shipped.

This record defines how Portal navigation communicates scope and how URLs keep
Space-owned resources in the correct Space. It is an independently deliverable
foundation for the R3 candidate operator journey; it does not change roadmap
priority.

## Contents

- [Outcome](#outcome)
- [Evidence and constraints](#evidence-and-constraints)
- [Decision](#decision)
- [Route and navigation model](#route-and-navigation-model)
- [Implementation slices](#implementation-slices)
- [Acceptance criteria](#acceptance-criteria)
- [Alternatives rejected](#alternatives-rejected)
- [Related records](#related-records)

## Outcome

A user can always answer three questions without reconstructing application
state: which Space am I in, what kind of object am I viewing, and where will a
navigation action take me? A copied link must reopen the same Space-scoped
resource or report that it cannot be accessed.

## Evidence and constraints

- Space is the ownership and authorization boundary for Portal resources, while
  Account and deployment administration are different scopes.
- Current entity hashes omit the Space. Opening a copied link under another
  selected Space therefore produces a misleading not-found state.
- Space switching redirects some detail pages but not every Space-owned page.
  Unknown hashes fall back to Home, hiding navigation errors.
- The current Home page is a Conversation composer, while Issue is the primary
  user-facing work object in [product vision](product-vision.md). Calling that
  surface “Home” overstates it as a Space overview.
- BuildMax is Alpha. There is no compatibility requirement for accidental hash
  shapes, so the stable model should not be obscured by a permanent legacy
  routing layer.

## Decision

Portal has two explicit navigation scopes:

1. **Space scope** contains work and resources owned by the selected Space.
2. **Global scope** contains Account, Help, Marketplace, and deployment
   administration.

The URL is authoritative for Space context on Space-prefixed pages. A canonical
ID-resolved exception may instead set Space context from the authorized resource
response. The shell never relies on a previously selected Space to interpret a
resource identifier.

The primary Space navigation is grouped by user intent:

| Group | Destinations |
|---|---|
| Work | Chat, Issues, Agents, Workflows, Schedules |
| Resources | Files, Artifacts |
| Manage | Space settings and permitted administrative surfaces |

“Home” becomes “Chat.” A true Overview is not added until a Space-wide query
and operator-journey evidence establish which outcomes belong there. Portal
must not implement an overview by fanning out across every collection API.

Deployment administration is visually and structurally outside Space scope.
Its pages do not show an active Space switcher as if the selected Space changed
their authority: the switcher becomes a static "Deployment" label, and the
sidebar lists the administration sections themselves under that scope, followed
by a "Back to space" return, rather than the Space-grouped nav.

## Route and navigation model

Canonical Space routes begin with `#/spaces/{space_id}`. Collections and
details extend that prefix, for example:

```text
#/spaces/{space_id}/issues
#/spaces/{space_id}/issues/{issue_id}
#/spaces/{space_id}/chat/{conversation_id}
#/spaces/{space_id}/agents/{agent_id}
#/spaces/{space_id}/workflows/{workflow_id}
#/spaces/{space_id}/workflow-runs/{run_id}
#/spaces/{space_id}/tasks/{task_id}
#/spaces/{space_id}/files
```

Artifacts keep their accepted public-capability and stable-detail-link rules
from [unified artifacts](unified-artifacts.md) and
[artifact public sharing and preview](artifact-public-sharing-and-preview.md).
`#/artifact/{artifact_id}` is the single ID-resolved detail exception: after an
authorized lookup, Portal selects the artifact's Space and renders that context.
The authenticated collection moves from `#/artifacts` to the Space-prefixed
Resources route; public share URLs remain outside the authenticated shell.

Global routes do not acquire a Space prefix. They include Account, Marketplace,
Help, and deployment administration.

Route handling follows these rules:

- Resolving a Space route first verifies that the Space exists and is visible
  to the account, then loads the resource within that Space.
- A Space switch from a detail page goes to the corresponding collection in the
  target Space. It never retains an identifier from the previous Space.
- A missing Space, missing resource, and forbidden resource have distinct page
  states as defined by
  [Portal state and permission feedback](portal-state-and-permission-feedback.md).
- Unknown routes render a not-found page with safe navigation actions. They do
  not silently render Chat.
- Breadcrumbs use human-readable labels when loaded and stable type labels while
  loading; raw public IDs are secondary metadata, not the main orientation cue.

## Implementation slices

Each slice can merge independently in order:

1. **Navigation language.** Rename Home to Chat, group the sidebar, expose
   Files, and separate global actions without changing routes.
2. **Canonical route parser.** Introduce typed Space-prefixed route generation
   and matching, then migrate one resource family at a time.
3. **Space switching.** Centralize the switch destination rule for all Space
   routes and remove page-specific redirect lists.
4. **Resolution states.** Add not-found and forbidden routes, then replace the
   fallback-to-Home behavior.
5. **Orientation polish.** Standardize breadcrumbs, page titles, and responsive
   collapse behavior.

During migration, internal links must only emit canonical routes. Any temporary
redirect for an old hash is bounded to the migration change and removed before
the record is marked implemented; it is not a compatibility contract.

## Acceptance criteria

- Every authenticated Space collection and detail URL contains a Space public
  ID or is an explicitly documented ID-resolved exception. Artifact detail is
  the only such exception in this design.
- Reloading or copying any supported URL preserves the same Space and resource.
- Switching Space from every Space-owned route lands in a valid target-Space
  collection and never shows stale data from the previous Space.
- Unknown, forbidden, and missing routes are distinguishable and actionable.
- Sidebar grouping and breadcrumbs expose Space, resource type, and location at
  desktop and narrow widths.
- Portal route tests cover direct entry, browser back/forward, Space switching,
  and an invalid hash. The browser suite covers at least Issue, Task, Agent, and
  Workflow deep links.

## Alternatives rejected

- **Keep the selected Space only in local storage.** A URL would remain
  ambiguous and shared links would depend on unrelated browser history.
- **Resolve every resource globally by ID.** That weakens the visible ownership
  boundary and makes authorization behavior harder to reason about.
- **Create a dashboard immediately.** Without an authoritative aggregate query
  and validated operator questions, it adds a label and network fan-out rather
  than a useful product concept.
- **Preserve every legacy hash indefinitely.** Alpha has no external contract
  requiring that state, and permanent aliases multiply routing behavior.

## Related records

- [Product vision](product-vision.md)
- [Space governance](space-governance.md)
- [Entity identity and relational keys](entity-identity.md)
- [Artifact public sharing and preview](artifact-public-sharing-and-preview.md)
- [Portal responsive and accessible interaction](portal-responsive-and-accessible-interaction.md)
