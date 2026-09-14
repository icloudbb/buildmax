---
id: continued-taskrun-input-trace
title: Show the continued run's own input in the Run trace panel
roadmap: none
source: direct
depends_on: []
verification: []
claim:
pr:
---

## Outcome

A reviewer opening a continued TaskRun's Run details / trace panel sees the
input that run was actually given, not the task's first input. A wrong input in
the panel can send audit or diagnosis toward the wrong turn even though
execution used the correct input.

## Scope

Reproduce and root-cause the discrepancy first, then fix it. An exploratory run
on commit `f894e0fc` observed: create a direct Task with input A, Continue it
with input B, then open `Details` → `View trace` for the latest run. The panel
showed `Sent to the worker <A>`, while
`GET /api/spaces/{space}/tasks/{task}/runs` returned input B for that same run
and correctly linked it to the first run via `previous_task_run_id`. Execution
itself used B.

The static read of the current code does not explain it: the trace panel opens
on `task.last_run_id` (the continued run), `RunTraceModal` renders
`provenance.input`, and `GET /api/spaces/{space}/task-runs/{task_run_id}`
returns per-run `run.Input` — the same column the runs-list API reads, so the
two should agree. The load-bearing files
(`portal/src/pages/tasks/TaskDetail.tsx`,
`portal/src/features/runs/RunTraceModal.tsx`,
`internal/server/handlers/work/run_provenance.go`) are byte-identical to
`f894e0fc`. So the root cause is either how a continued run's `Input` is
persisted (`internal/service/task` → `internal/infra/db` CreateTaskRun), a
trace/caching path the panel actually reads, or a stale-run-id selection — find
it before changing code.

## Out Of Scope

The other three findings from the same exploratory run: the kind worker-route
probe false negative and the Desktop tool-card wrapping are fixed separately;
the `/api/admin/me` 403 on ordinary sign-in was triaged as by-design.

## Acceptance Criteria

- The exact runtime reproduction is captured (which layer returns A for run B).
- The continued run's trace panel and the runs-list API both show input B for
  the continued run.
- A regression test pins the per-run input through the layer that was wrong.

## Verification

Start with the narrowest server test for the run-provenance / task-run input
path (`./make test ./internal/server/... ./internal/service/task/...`), then the
Portal e2e that exercises Task continue + trace panel if the fix touches the
frontend. Confirm the reproduction no longer occurs against a real run.

## Notes

Converted from the 2026-09-13 cross-surface continuity exploratory run (Finding
"A continued TaskRun's Run details show the Task's first input"). Repro run ids
from that session: task `3zozxlh4dydi4ayomcmq`, continued run
`gazixnq742fyden2h2da`.
