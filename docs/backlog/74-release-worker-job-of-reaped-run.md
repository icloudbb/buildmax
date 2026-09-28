---
id: release-worker-job-of-reaped-run
title: Delete the worker Job when the reaper ends a run
roadmap: R2
source: direct
depends_on: []
verification: ["./make test", "kind"]
claim:
pr:
---

## Outcome

A run the stale-run reaper has ended no longer holds a worker pod.

In the 2026-09-28 operator drill, a worker frozen with `SIGSTOP` was reaped as
FAILED after about 2m20s, but its pod stayed `Running`. The Job has
`ttlSecondsAfterFinished` but no `activeDeadlineSeconds`, and nothing deletes
it. A partitioned or hung worker therefore keeps its pod, and any capacity it
holds, until it recovers by itself.

A run stuck in SCHEDULED behind a pod quota is abandoned after
`worker.run_timeout`, and its Job keeps retrying pod creation after that.

State correctness was unaffected: on thaw the worker logged
`run already claimed; this worker has nothing to do`, and the result stayed
FAILED.

## Scope

When the reaper moves a run with a recorded `k8s_job_name` to a terminal status,
delete that Job with background propagation, so its pods go with it. This
covers all three reaper sweeps: lost worker, abandoned, and unconfirmed cancel.

Put the deletion behind a narrow capability that `K8sJobRunner` implements and
the reaper uses when it is present. The local-process runner has no Job. A
deletion failure is logged with `task_run_id` and never reverses the terminal
status.

## Out Of Scope

- Adding `activeDeadlineSeconds`. The run timeout is the server's policy, not
  the Job's.
- Terminating workers for runs a person cancels normally. The worker already
  honors the cancel poll.

## Acceptance Criteria

- A unit test shows each reaper sweep requesting deletion of the run's Job, and
  shows that a deletion error leaves the run terminal.
- On kind, a frozen worker's pod is gone shortly after its run is reaped.

## Verification

- `./make test` for `internal/server/scheduler` and `internal/infra/k8s`.
- `kind`: freeze a worker process on the node (`kill -STOP`, as in the drill),
  wait for the reap, and confirm the Job and pod are removed.
  `tools/mk/worker_loss_probe.go` is the natural place to assert it.

## Notes

Found during the operator drill that grounds
[system administration](../design/system-administration.md) §13 M7. That drill's
report was retired once converted; `git log --diff-filter=D --
docs/contribute/exploratory-runs/` recovers it (finding F6).
