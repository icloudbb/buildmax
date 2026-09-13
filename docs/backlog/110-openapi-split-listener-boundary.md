---
id: openapi-split-listener-boundary
title: Split openapi.json into public and worker documents along the listener boundary
roadmap: none
source: docs/design/api-surface-conventions.md#5-decision-split-openapi-along-the-listener-boundary
depends_on: []
verification: ["./make test ./internal/architecture", "./make check docs"]
claim:
pr:
---

## Outcome

A reader of the public API surface no longer sees the worker control plane, and
the "spec matches routes exactly" check runs per listener instead of reconciling
one file against two disjoint route sets. The two OpenAPI documents follow the
network boundary the code already enforces between `RegisterPublic` and
`RegisterWorker`.

## Scope

Split the single `internal/server/static/openapi.json` into a public document and
a worker document, each corresponding to one `Register*` method (design §5).

- Resolve the design's one open question (§8): decide whether the two documents
  are two committed files or one source generated into two views, and record the
  decision in the design record before implementing. Prefer whichever keeps the
  match-the-routes check simplest.
- Move every `/api/worker/*` path into the worker document; everything else
  (auth, space, work, artifact, admin, plugins, llm, inbound webhook, WebSocket)
  stays in the public document.
- Update the `GET /openapi.json` / Swagger serving so each listener serves its
  own document, consistent with docs/design/worker-api-network-boundary.md.
- Update `internal/architecture/openapi_test.go` and
  `worker_listener_test.go` so the exact-match check runs against each document's
  own route set.
- Group the public document's sub-audiences (admin, shared-artifact, auth) with
  OpenAPI `tags`; do not split them into further documents.

## Out Of Scope

Route renames (tasks 130 and 140) and `info.version` stamping (task 120). This
task only re-partitions the existing spec and its checks.

## Acceptance Criteria

- Two OpenAPI documents exist, split exactly along `RegisterPublic` /
  `RegisterWorker`; no `/api/worker/*` path appears in the public document and no
  public path appears in the worker document.
- The architecture check verifies each document against its listener's registered
  routes and fails if either drifts.
- Swagger/openapi serving exposes the correct document per listener.

## Verification

`./make test ./internal/architecture` proves each document matches its listener's
routes exactly. `./make check docs` confirms no documentation link or reference
broke.

## Notes

Design §8 leaves the two-files-versus-one-source choice to this task; make the
call, update the design record's §8, then implement. The listener boundary is
authoritative in `internal/server/handlers/routes.go`.
