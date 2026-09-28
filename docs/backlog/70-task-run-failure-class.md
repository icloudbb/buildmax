---
id: task-run-failure-class
title: Record a safe failure class on every failed TaskRun and tag run logs with space_id
roadmap: R3
source: docs/design/system-administration.md#m7-runtime-operations-metadata--accepted-not-built
depends_on: []
verification: ["./make test", "./make test mysql", "kind"]
claim:
pr:
---

## Outcome

Operators need to tell a platform failure from a Space's own misconfiguration
without reading Space content. In the 2026-09-28 operator drill, three platform
failures (object storage, worker shutdown) and one disabled Secret all showed
as `FAILED 4`. Only the raw `error_message` told them apart, and it contains
internal URLs, so Administration cannot show it. Scheduler and reaper log lines
named the run but not its Space. This task records the durable data that the
M7 projection (task 72) reads.

## Scope

- Add `failure_class` to `task_run` (`taskRunRow` in `internal/infra/db`). Define
  its closed enum in `internal/core/task` with the classes from design §13 M7:
  `dispatch`, `worker_lost`, `abandoned`, `interrupted`, `infrastructure`,
  `space_configuration`, `model`, `run`, `unclassified`.
- Server-side setters:
  - the scheduler's `failRun` (spawn or token failure) sets `dispatch`;
  - the stale-run reaper sets `worker_lost` or `abandoned`.
- Add a `failure_class` field to the worker's FAILED PATCH, and update the
  worker OpenAPI to match. The worker classifies from typed or sentinel errors:
  - `coretask.ErrRunInterrupted` is `interrupted`;
  - the plugin refusal (`reportPluginRefusal`) and Secret or grant failures are
    `space_configuration`;
  - checkpoint, trace, and storage persistence failures, and an unreachable
    worker API, are `infrastructure`;
  - provider or model errors are `model`;
  - other Agent-run errors are `run`.
  
  Anything unrecognized is `unclassified`. The server validates the value and
  stores `unclassified` for an unknown one. It never parses `error_message`.
- Add `space_id` to the scheduler, k8s runner, and reaper log lines that already
  carry `task_run_id`, fetching it with the run where the query does not already
  return it.

## Out Of Scope

- Any Admin route or Portal view. That is task 72.
- Showing the class to Space members. Their run view already shows
  `error_message`.
- Audit events for failures. The design keeps them out.

## Acceptance Criteria

- Every path that moves a run to FAILED stores a non-empty class from the enum,
  and a test covers each setter.
- A worker cannot store a value outside the enum.
- On kind, a frozen worker's run is reaped with `worker_lost`, and the fixture's
  disabled-Secret run fails with `space_configuration`.
- Scheduler and reaper log lines for a run include `space_id`.

## Verification

- `./make test` for core, scheduler, reaper, worker classification, and the
  worker PATCH handler. Run the `internal/infra/db` package unfiltered once,
  per the paged-query source test.
- `./make test mysql`, because the change touches `internal/infra/db`.
- `kind`: `./make kind smoke`, plus the reap and disabled-Secret checks above.
  Freeze the worker with `kill -STOP` on the kind node, as the drill did.

## Notes

The drill's injections are described in design §13 M7. The report was retired
once converted; `git log --diff-filter=D -- docs/contribute/exploratory-runs/`
recovers it. The failure paths are:

- `internal/server/scheduler/scheduler.go` (`failRun`);
- `internal/server/scheduler/stale_runs.go` (`sweepLostWorkers`,
  `sweepAbandoned`);
- `internal/agentapp/taskrun/runtime.go` (`reportRunFailure`);
- `internal/bootstrap/worker.go` (`reportPluginRefusal`).
