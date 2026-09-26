---
id: worker-storage-write-denial-drill
title: Prove a run fails legibly when the worker's object-storage write is denied
roadmap: R2
source: docs/design/end-to-end-testing.md
depends_on: []
verification: ["./make test", kind]
claim: gougoujiang 2026-09-26
pr:
---

## Outcome

An operator can trust that a worker losing object-storage write access fails the
run with a readable cause and never leaves an Artifact that claims a missing
object is downloadable. The server's `/readyz` cannot see this: workers write to
storage with their own client.

## Scope

- Inventory every worker object write and its order against the database record;
  fix any path that can leave a record without its object.
- A kind drill, run from `kindSmoke` like the other probes, that denies only the
  worker's path to MinIO, drives a run to a write, and asserts FAILED with a
  legible cause, retained trace and diagnostics, no downloadable record for a
  missing object, and a healthy server; then restores access and runs again.

## Out Of Scope

Server-side storage denial (already covered by `storage_denial_probe.go`).
Ticking the beta-readiness row, which belongs to the pinned-candidate operator.

## Acceptance Criteria

- The drill passes on an ephemeral kind cluster and fails if any assertion breaks.
- verification-program §6, end-to-end-testing §6, ROADMAP R2, and current-state
  (with zh-CN mirrors) record the case as proven.

## Verification

Unit tests for touched packages, then the drill on `BUILDMAX_KIND_EPHEMERAL=1`.

## Notes

Worker storage clients are built in `internal/bootstrap/worker.go`.
