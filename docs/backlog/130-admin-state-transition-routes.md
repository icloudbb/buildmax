---
id: admin-state-transition-routes
title: Replace admin enable/disable/archive/yank actions with PUT .../state
roadmap: none
source: docs/design/api-surface-conventions.md#61-state-transition-renames
depends_on: []
verification: ["./make test ./internal/server/handlers/admin", "./make check portal", "./make e2e kind"]
claim: gougoujiang 2026-09-13
pr: 611
---

## Outcome

Stored-lifecycle-flag transitions on the admin surface use one idempotent state
sub-resource instead of paired RPC-style POST actions, following the convention
in design §3.5. Two enable/disable routes per resource collapse into one `PUT`,
shrinking the surface.

## Scope

Rename the four admin transitions in design §6.1 to `PUT .../state`, coherently
across server, OpenAPI, clients, and tests (no alias left behind, per design §1):

| Current route(s) | Target | Stored flag |
|---|---|---|
| `POST /api/admin/users/{user_id}/disable`, `.../enable` | `PUT /api/admin/users/{user_id}/state` | `disabled` |
| `POST /api/admin/llm/models/{model_id}/enable`, `.../disable` | `PUT /api/admin/llm/models/{model_id}/state` | enabled/disabled |
| `POST /api/admin/plugins/{plugin_name}/archive`, `.../unarchive` | `PUT /api/admin/plugins/{plugin_name}/state` | `archived` |
| `POST /api/admin/plugins/{plugin_name}/releases/{version}/yank` | `PUT /api/admin/plugins/{plugin_name}/releases/{version}/state` | `yanked` |

- Update `internal/server/handlers/admin` route registration and handlers to take
  the desired state in the request body.
- Update the matching OpenAPI paths (public document).
- Update every client that calls these: Portal admin views, the `buildmax admin`
  CLI, and any Desktop surface.
- Update handler and architecture route/spec tests.

## Out Of Scope

The actions that stay actions (design §6.3): task cancel/retry, invitation
accept, revision restore. Non-admin state routes, which already use the target
shape (secrets `/state`, `plugin-curation`).

## Acceptance Criteria

- The four transitions are served as `PUT .../state` and the old POST action
  routes no longer exist.
- Setting a user disabled/enabled, a model enabled/disabled, a plugin
  archived/unarchived, and a release yanked all work through the new routes from
  the CLI and Portal.
- OpenAPI and the route set match; the architecture check passes.

## Verification

`./make test ./internal/server/handlers/admin` for handler behavior and the
route/spec match; `./make check portal` for the Portal client; `./make e2e kind`
to exercise a disable/enable round trip against the deployed admin surface.

## Notes

The stored flags already exist on these entities; this is a surface change, not a
storage or authorization change. Keep the request body minimal — the target state
only.
