---
id: fold-conventions-into-server-arch
title: Fold API surface conventions into server architecture and retire the design record
roadmap: none
source: docs/design/api-surface-conventions.md#9-status-and-rollout
depends_on: [110-openapi-split-listener-boundary.md, 120-openapi-version-from-build.md, 130-admin-state-transition-routes.md, 140-auth-route-grouping.md, 150-webhook-keys-handler-relocation.md]
verification: ["./make check docs", "./make test ./internal/architecture"]
claim:
pr: 611
---

## Outcome

The naming and placement conventions live where a contributor adding a route
already looks — the server architecture documentation — and the API surface
conventions design record is retired now that its reconciliation has merged, so
there is one authoritative home for the rules rather than two.

## Scope

- Fold §3–§5 of `docs/design/api-surface-conventions.md` into
  `docs/contribute/architecture/server.md` as a concise conventions section
  (path syntax, parameters, ownership, addressing, state transitions,
  authorization by record, auth grouping, no URL versioning, listener-split
  OpenAPI). Keep it factual and short; rationale stays in git history.
- Delete `docs/design/api-surface-conventions.md` and its zh-CN mirror
  `docs/zh-CN/design/API表面约定.md`, and remove both index rows
  (`docs/design/README.md`, `docs/zh-CN/design/设计文档索引.md`).
- Update `docs/current-state.md` to describe the reconciled surface if it
  characterizes the API surface.

## Out Of Scope

Any further route change; all route work is done in tasks 110–150. This is a
documentation consolidation.

## Acceptance Criteria

- `docs/contribute/architecture/server.md` states the conventions a route author
  needs, with no dependence on the deleted design record.
- The design record and its mirror are deleted and their index rows removed.
- `./make check docs` passes with no broken links to the removed record.

## Verification

`./make check docs` confirms links resolve after the deletion; `./make test
./internal/architecture` confirms the documentation and route/spec checks pass.

## Notes

This task closes the reconciliation. It cannot start until tasks 110–150 have
merged (their files are deleted), which its `depends_on` encodes.
