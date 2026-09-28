# Operator incident drill without Space membership — 2026-09-28

> **简体中文：** [阅读中文镜像](../../zh-CN/contribute/exploratory-runs/2026-09-28-portal-admin-operator-incident-drill.md)

**Charter.** The first slice of
[cross-Space work visibility](../../proposals/admin-cross-space-work-visibility.md)
§8: in a multi-Space, two-Server deployment, can a System Administrator who
belongs to no Space answer *whether BuildMax is processing work, where progress
stopped, and who can act* from today's Administration views? For each injected
incident, record the question the operator could not answer, the missing field,
the action taken, and whether answering needed Space content. Role:
`ops@drill.local`, `system_admin` grant, member of no Space. Surfaces: Portal
Administration (driven with `drive-portal`), the Admin API, and `buildmax admin`;
then the operator's realistic fallback, `kubectl` and server logs. About 25
minutes of active drilling after setup. Not exercised: the proposal's second
question (observing a real multi-Space member), which needs a person, not an
Agent walkthrough.

**Environment.** Worktree at `cc1a41d1` (main), clean. macOS arm64, Go 1.26.6,
kind 0.31.0, kubectl 1.35.1. Task-owned ephemeral cluster
`buildmax-eph-bc6fca9f` from `BUILDMAX_KIND_EPHEMERAL=1 ./make kind up`: two
`buildmax-server` replicas, MySQL, MinIO, Kubernetes worker Jobs,
`worker_llm_transport: direct`. Starting data: `./make kind fixtures --runs`
(BuildMax QA and QA Pagination), plus two team Spaces created for the drill —
Drill Payments (owner `pat@drill.local`) and Drill Research (owner
`rin@drill.local`) — whose task inputs carry distinctive markers
(`LEAKMARK-*`, `ACME-CONFIDENTIAL`, `NIGHTJAR`) for the leak check. Alice granted
`ops@drill.local` through `POST /api/admin/grants`; that is preparation.

**Model.** In-cluster mock only (`deployment smoke ok`), no cost. The mock's
stall control was used to hold one run in RUNNING.

## Explored

| Incident (injection) | Ground truth | What Administration showed | Operator fallback |
|---|---|---|---|
| I1 Capacity: namespace `ResourceQuota pods=<current>`, then two Payments and one Research task | 3 runs SCHEDULED, Jobs created, pods refused `exceeded quota` | Health **Ready** (green); Task runs `SCHEDULED 3` with no age; Spaces list shows no status; Space detail shows only "runs this period 3/1000" | `kubectl get jobs` (0/1) and `FailedCreate` events named the cause; deleting the quota recovered all three about five minutes after submission with no BuildMax action |
| I2 Lost worker: mock stall, run reaches RUNNING, `kill -STOP` on the worker process | Reaper failed it 2m21s later: "this run lost its worker: nothing was heard from it for 2m0s" | `RUNNING 1` became `FAILED` +1; nothing names the Space, the reason, or that the reaper acted | One WARN log line `failed a task run whose worker stopped reporting`, carrying `task_run_id` only |
| I3 Existing failures (fixtures and `kind up` drills) | 4 FAILED: 3 in the smoke account's **personal** space (worker shutdown; two object-storage `PutObject` failures) — platform faults; 1 in BuildMax QA (disabled Secret) — a Space configuration fault | `FAILED 4`, indistinguishable | Cause and Space only by SQL: `task_run` joined to `task` and `space` |
| I4 Queue: 30 tasks submitted at once across three Spaces | Drained PENDING 30 → 0 in about 80s, about 0.4 runs/s | One `PENDING` number; a single view cannot tell draining from stuck; comparing repeated refreshes can | None needed |

Leak check: `ops@drill.local` read `/api/admin/{system,config,spaces,spaces/{id},audit-events,llm/calls,users}`
and none of the markers appeared. Space content routes (`/agents`, `/schedules`,
task detail) answered `space not found`. The content boundary held.

## Findings

**F1 — An operator cannot detect a work stall from Administration.**
Usability obstacle / missing operational metadata; high impact on the incident
outcome, reproduced once per incident. In I1 the Overview reported Ready while no
worker could start; `SCHEDULED 3` carries no age, and the same number is normal
at any instant of a healthy deployment. In I4 a static `PENDING 30` looked the
same draining or stuck. The operator question that failed was "is work moving,
and since when has it not?". Missing fields: the age of the oldest PENDING and
oldest SCHEDULED run, and RUNNING runs whose `last_seen_at` is stale. All are
derivable from durable `task_run` columns (`created_at`, `k8s_job_created_at`,
`started_at`, `last_seen_at`), so they are truthful across two replicas. **No
Space content is needed.** This is the §17 question 16 aggregate of the system
administration design, now backed by an observed miss.

**F2 — Failures are not separable into platform and Space faults.**
Usability obstacle; high impact, since it decides whether the operator acts at
all. I3 put three platform failures and one Space misconfiguration behind the
same `FAILED 4`. The run's `error_message` does distinguish them but is unsafe to
show as-is: it contains internal service URLs and object-storage session paths.
Missing field: a **safe failure class** set by the component that fails the run
(for example worker lost, abandoned, spawn failed, storage, Space configuration,
model/provider, agent error), counted per class and time window. It needs a new
column or an enum derived where the failure is recorded; it does **not** need
content.

**F3 — The operator cannot name the affected Spaces or owners without SQL.**
Usability obstacle; medium impact — recovery in I1 did not need it, but
notifying the right people did. Admin's per-Space data is membership, tier, and
a period run total; nothing lists Spaces by stuck or failed runs. Scheduler,
k8s runner, and reaper logs carry `task_run_id` but not `space_id`, and a
`task_run` row reaches its Space only through `task`. Once a Space id is known,
the Admin Space detail names the owner, which answers "who can act". Missing
field: per-Space counts of active, stuck, and failed runs (Space id, name, owner
— already Admin metadata). No content; the proposal's §5 caution that Space
*names* can reveal work applies, but Admin already shows them.

**F4 — Personal spaces are invisible in Administration, including their failures.**
Confirmed gap against the operator outcome; medium impact. The Spaces list
states "Personal spaces are omitted"; search for `My Space` returns 0. Yet three
of the four failures in I3 — the platform faults — were in a personal space, and
the Overview counted them. `GET /api/admin/spaces/{id}` does answer for a
personal space (`personal: true`, usage), but only when the id is already known.
Any F3 projection must include personal spaces, or deployments where individuals
work alone will hide their incidents.

**F5 — Run lifecycle incidents leave no audit or operator-visible trace.**
Product question; low-medium impact. The reaper's decisions (lost worker,
abandoned, cancel grace) and scheduler failures (spawn failed, token mint) exist
only as log lines on whichever replica acted. The audit trail records none of
them. Whether they belong in audit (a record of actions) or in the F1 metadata
projection is a design choice; the drill shows the operator needed at least one.

**F6 — A reaped run's frozen worker pod lingers (suspected resource leak).**
Suspected defect; low impact, seen once. After the reaper failed the run in I2,
its pod stayed `Running`. The Job has `ttlSecondsAfterFinished: 300` but no
`activeDeadlineSeconds`, and the reaper does not delete the Job. After a network
partition or freeze, the worker keeps its pod until it recovers on its own. On
thaw it logged `run already claimed; this worker has nothing to do` and the
terminal result stayed FAILED, so there is no state-correctness problem, only
capacity. Unverified: whether a partitioned but running worker keeps consuming
model calls.

**Observation — dispatch throughput is fixed.** I4 drained at about 0.4
runs/s, which matches `maxConcurrentDispatch = 1` per replica on a 5s poll. The
queue drained, but a deployment's dispatch rate scales only with Server
replicas. That makes F1's oldest-PENDING age the signal an operator needs to
notice when it saturates.

## Decision evidence for the proposal

- Everything the operator missed was **operational metadata derivable from
  durable run state**: stall ages (F1), safe failure classes (F2), and per-Space
  counts including personal spaces (F3, F4). **No incident required reading an
  Agent, Workflow, Schedule, or Issue, or any Space content.** Recovery in I1 and
  I2 needed platform access (kubectl) and no Space authority.
- This supports the proposal's option C for the operator half and gives it a
  concrete slice for the system administration design's §17 question 16. It
  gives no evidence for options A or B — global Agent, Workflow, Schedule, or
  Issue inventories — for an operator.
- The member half (a combined view over one's own Spaces) was **not tested**
  and still needs observation of a real multi-Space user.

## Cleanup

`./make kind down` removed the ephemeral cluster and `.local/kind-ephemeral.env`.
Raw screenshots and logs were in the gitignored
`.artifacts/explore-admin-drill/` and are not needed to reproduce anything here.
The drill touched no shared cluster (`buildmaxdev` untouched).

## Follow-up

Convert F1–F4 into an accepted runtime-operations slice in the
[system administration design](../../design/system-administration.md), resolving
its §17 question 16 with this evidence, and then into a backlog task. That task
needs authorization and response-leak tests and a two-replica kind check, as the
proposal's §8 lists. Triage F5 within that decision. Take F6 as a small separate
fix or backlog item: delete or deadline the worker Job when a run is failed by
the reaper. Retire this report once those land.
