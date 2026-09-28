---
id: admin-runtime-operations-view
title: Show stalled work, failure classes, and Spaces needing attention in Administration
roadmap: R3
source: docs/design/system-administration.md#m7-runtime-operations-metadata--accepted-not-built
depends_on: [70-task-run-failure-class.md]
verification: ["./make test", "./make test mysql", "kind", "./make e2e kind"]
claim:
pr:
---

## Outcome

A System Administrator with no Space membership can answer three questions from
Administration: is work stalled and since when, is a failure the platform's or
the Space's, and which Spaces and owners are affected.

In the 2026-09-28 drill, Overview showed Ready while a pod quota blocked every
worker. `SCHEDULED 3` and `PENDING 30` looked the same whether stuck or
draining. Finding the affected Spaces needed SQL, and a personal Space that
held the platform failures was invisible.

## Scope

- Add a runtime summary to `GET /api/admin/system`:
  - the oldest PENDING `created_at`;
  - the oldest SCHEDULED-without-`started_at` `created_at`;
  - the count of RUNNING runs whose `last_seen_at` is older than the reaper's
    liveness grace;
  - failures by `failure_class` over the last 24 hours.

  Each read stays best-effort, as M3 requires.
- Add `GET /api/admin/runtime/spaces`, a paginated list of Spaces, team and
  personal, that have an active run or a failure in the window. Order it by
  oldest active `created_at`. Each row has:
  - Space id, name, and personal flag;
  - owners (id and email);
  - active counts by status;
  - the oldest active `created_at`;
  - failure counts by class.
  
  Guard it with the system-admin grant and bound it with the paging helpers.
  Keep `internal/server/static/openapi.json` in sync.
- In Portal Administration Overview:
  - show stall ages (computed from `server_time`), the stale RUNNING count, and
    failure classes next to the status counts;
  - list the Spaces needing attention, each linking to the existing Admin Space
    detail.

## Out Of Scope

- Dispatch pause, force-cancel, and cross-Space retry, which design §13 M7
  excludes.
- `buildmax admin` parity, which is §17 question 13.
- Changing the Admin Spaces list, which still omits personal Spaces; the new
  route covers them.
- Any Agent, Workflow, Schedule, or Issue field, run input, output, or error
  text.

## Acceptance Criteria

- Authorization:
  - a System Administrator without membership reads both summaries;
  - a Space owner without a grant, and an anonymous caller, are refused;
  - admin status still does not open any Space content route.
- Leak test: fixtures carrying distinctive Agent instructions, run inputs, and
  error text never appear in either response body.
- Both replicas of a two-Server kind deployment return the same summary.
- With a namespace pod quota injected, Overview shows a growing oldest-SCHEDULED
  age. The affected Spaces, including a personal one, appear with their owners.
- Overview renders a partial dependency failure as unavailable data, not as
  zero.

## Verification

- `./make test`: handler tests, including `system_authz_matrix_test.go` and the
  leak fixtures, plus the Portal component tests.
- `./make test mysql`: the new aggregate queries in `internal/infra/db`. Run the
  package unfiltered once for the paged-query source test.
- `kind`: after `kind reload server`, replay the drill's pod-quota and
  frozen-worker injections against both replicas.
- `./make e2e kind`: extend `portal/e2e/admin.spec.ts` for the Overview section.

## Notes

Update the documentation that describes Administration and the admin API when
the change ships: design §10 and §7.1, `docs/current-state.md` (removing
"runtime metadata" from the remaining gaps), and the Portal overview manual
entry. Record the evidence against R3's diagnosis journey.
