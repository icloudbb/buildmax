# Scheduled Agent Execution

> **简体中文：** [阅读中文镜像](../zh-CN/design/定时Agent执行.md)

> **Audience:** contributors and operators · **Status:** implemented. The
> `Schedule` domain and store, the dispatcher, the Space-scoped REST API, the
> Portal surfaces, and the removal of the `ChannelCron` placeholder shipped on
> 2026-09-11 (pull requests #548–#554). A schedule fired only an Agent then;
> §15 records the later generalization to an executor — an Agent or a published
> Workflow. §13 records the questions left open on purpose.

Related records:
[agent execution and Task threads](agent-execution-and-task-threads.md)
(§4.5 names schedule as a typed trigger origin),
[workflow runtime](workflow-runtime.md) (a schedule is one of the triggers that
starts a workflow run; §15),
[system administration](system-administration.md) (disabled-creator and quota
handling), and
[server coordination](server-coordination.md) (multi-replica claim safety).

## Contents

- [1. Purpose](#1-purpose)
- [2. Background](#2-background)
- [3. Goals](#3-goals)
- [4. Non-Goals](#4-non-goals)
- [5. First-Principles Shape](#5-first-principles-shape)
- [6. The Schedule Entity](#6-the-schedule-entity)
- [7. Firing: Claim, Admit, Advance](#7-firing-claim-admit-advance)
- [8. Time Semantics](#8-time-semantics)
- [9. Authorization, Quota, And Runaway Control](#9-authorization-quota-and-runaway-control)
- [10. Surfaces](#10-surfaces)
- [11. Disposition Of `ChannelCron`](#11-disposition-of-channelcron)
- [12. Decisions](#12-decisions)
- [13. Open Questions](#13-open-questions)
- [14. Delivery](#14-delivery)
- [15. Extension: Workflow Executors](#15-extension-workflow-executors)

## 1. Purpose

A Space can run an Agent automatically on a recurring time schedule — "every
weekday at 09:00, summarize the new Issues" — with no human triggering each
run. This record explains the smallest set of concepts that delivers that on
the existing Task execution plane, and why the alternatives were rejected.

## 2. Background

Before this work nothing time-driven existed. No `xxxRow` struct or domain type
carried a `next_fire_at`, `cron_expr`, or `run_at` field.
`internal/service/conversation/channel` defined `ChannelCron = "cron"` with no
adapter, parser, or timer behind it: an unbacked placeholder on the wrong plane
(§11). `internal/server/scheduler` was a dispatch poller that moves
already-`PENDING` TaskRuns to workers and never creates a run.

The execution plane the feature plugs into already existed, which is why the
new surface stays small. [Agent execution and Task
threads](agent-execution-and-task-threads.md) §4.5 declares API, webhook, and
schedule as typed, non-conversational trigger origins that create Space-owned
Tasks through the same service and record a typed trigger source. This record
fills in that named slot.

## 3. Goals

- A Space member can create a recurring trigger that runs a chosen Agent with a
  fixed input on a cron schedule in a chosen timezone.
- Each firing produces one ordinary Task and TaskRun through the existing Task
  application service, with a typed `schedule` trigger source, so status,
  cancellation, quota, trace, artifacts, and audit have exactly one owner —
  the same one every other run has.
- Firing is safe under multiple Server replicas: a due schedule fires exactly
  once per due time, never zero times because two replicas each assumed the
  other would, never twice because both claimed it.
- Server restart across a due time does not silently skip work or replay a
  backlog of historical fires.
- A schedule's history is discoverable as the list of Tasks it created; the
  schedule row itself stays small.

## 4. Non-Goals

- **Continuing one long-lived thread.** Each firing is a fresh objective and a
  fresh Agent session (a new Task), not a Continue on a growing thread. Thread
  accumulation would grow context and cost without a demonstrated need (§13).
- **Local CLI scheduling.** The CLI is a single-run process with no resident
  loop or multi-replica coordination. A user who wants the local binary on a
  timer uses the operating system's own cron, launchd, or Task Scheduler. The
  Desktop app, a resident GUI, is a separate case: it carries its own small
  in-process scheduler that fires local tasks while it is open, sharing no state
  with this Server design (see [`docs/current-state.md`](../current-state.md)).
  Server-side scheduling, the subject of this record, lives where a resident,
  coordinated, multi-user process already lives: the Server.
- **Event and webhook triggers.** Inbound events are a separate typed origin
  (`webhook` already exists as a trigger source). This record is time only.
- **Sub-minute granularity.** The smallest interval is one minute; finer
  cadence is a streaming/event concern, not a schedule.
- **Workflow-level schedules.** A schedule runs one Agent. Scheduling a
  Workflow remains a later Workflow-runtime slice.

## 5. First-Principles Shape

The essential outcome is "a run happens at a time nobody is present for".
Stripped to what must be true:

1. Something durable remembers *what* to run (Agent + input, in a Space) and
   *when* (a recurrence rule + timezone). That is one new entity: `Schedule`.
2. Something resident notices a due time and admits the run. The Server already
   runs resident poll loops in `internal/server/scheduler` (the dispatcher, the
   stale-run reaper, the retainers). The schedule dispatcher is one more loop
   of the same shape, not a new subsystem.
3. Admission already exists. `task.Service.CreateTask` validates Space, Agent,
   quota, and input and commits a Task plus first TaskRun atomically, tagging a
   `trigger_source`. A firing is a call to it with `trigger_source = schedule`.

Nothing else is required. A firing needs no "schedule run" table: the run it
produces *is* a TaskRun, which already records input, trigger source, status,
usage, output, trace, and artifacts. A schedule's firings are the Tasks whose
`schedule_id` is that schedule. A parallel execution-record type would
duplicate the plane that the Agent execution record unified.

```text
Schedule (space-owned, time trigger)
        |
        v
  task.Service.CreateTask   <-- same service Issue, Workflow, API, Portal call
        |
        v
   Task + first TaskRun
        |
        v
  Scheduler -> Worker -> shared Agent runtime
```

## 6. The Schedule Entity

A `Schedule` is Space-owned, mirroring how Space is authoritative for every
execution resource. The domain lives in `internal/core/schedule` (pure domain:
no cron library, no infra); the store in `internal/infra/db` uses the singular
`schedule` table.

```text
Schedule
  id                      NewPublicID
  space_id                required, authoritative owner
  executor_kind           "agent" or "workflow" -- what the schedule fires (§15)
  executor_id             required executor; an opaque handle executor_kind fixes
  created_by              required actor; carried onto each firing
  name                    human label
  input                   the fixed input each firing runs (a prompt for an
                          agent, run input JSON for a workflow)
  cron_expr               recurrence rule (standard five fields)
  timezone                IANA name, e.g. "Asia/Shanghai"
  enabled                 bool; a paused schedule keeps its row and next time
  next_fire_at            UTC instant the dispatcher claims on; the due index
  last_fire_at            UTC instant of the most recent fire (nullable)
  last_fire_ref           what the most recent fire produced -- a Task (agent)
                          or a workflow run (workflow), per executor_kind (nullable)
  consecutive_failures    bounds runaway cost (§9)
  created_at / updated_at
```

`next_fire_at` is the single fact the dispatcher queries and the
compare-and-swap target that makes firing exactly-once (§7). The schedule
stores no list of past fires; that list is a Task query for an agent schedule
and a workflow-run query for a workflow schedule (§15). Core does not parse
cron: callers compute `next_fire_at` from `cron_expr` and `timezone` with
`github.com/robfig/cron/v3`, a parser-only dependency; the loop is BuildMax's.

Persisted JSON uses explicit `snake_case` tags, the table name is singular, and
the id uses `NewPublicID`, per [entity identity](entity-identity.md).

## 7. Firing: Claim, Admit, Advance

`ScheduleDispatcher` in `internal/server/scheduler` polls on a coarse interval
(a minute is enough given minute granularity). Each tick, for each due
schedule, it performs one compare-and-swap claim, exactly as the run poller
claims a run with `TransitionTaskRun`:

```text
tick:
  candidates = store.DueSchedules(now)          # enabled AND next_fire_at <= now
  for s in candidates:
    next = cronNext(s.cron_expr, s.timezone, now)
    claimed = store.ClaimSchedule(s.id,
                expectedNextFireAt = s.next_fire_at,
                newNextFireAt      = next)       # conditional UPDATE
    if not claimed: continue                     # another replica took it
    task.Service.CreateTask(CreateTaskCmd{
        SpaceID: s.space_id, AgentID: s.agent_id,
        CreatedBy: s.created_by, Input: s.input,
        TriggerSource: RunTriggerSourceSchedule,
        ScheduleID: s.id,
    })
    store.RecordFire(s.id, task.id, outcome)       # last_fire_at, last_task_id
```

The conditional update — advance `next_fire_at` only if it still equals what
was read — makes firing exactly-once across replicas, reusing the
optimistic-concurrency pattern the run scheduler relies on. The claim advances
the clock *before* admission, so a `CreateTask` failure does not wedge the
schedule on the same due time forever; it is recorded as a failed fire and the
schedule proceeds to its next time. §9 bounds a schedule that fails every time.

`RunTriggerSourceSchedule` joins the `RunTriggerSource*` constants in
`internal/core/task/task.go`. `Task` carries an optional `schedule_id` origin
relation, exactly as it carries optional `issue_id` and `workflow_step_run_id`
— an origin, never an authorization parent.

## 8. Time Semantics

- **Timezone.** Cron is evaluated in the schedule's IANA timezone so "09:00"
  survives DST. Storage and all comparisons are UTC. The Server binary embeds
  the timezone database so a minimal container image resolves IANA names.
- **Missed fires coalesce.** If the Server is down across one or more due
  times, the next `cronNext` is computed from `now`, not from the stale
  `next_fire_at`. A schedule that should have fired at 09:00 and 10:00 during
  an outage that ends at 10:30 fires once, immediately, then resumes at its
  next regular time. No backfill storm, no silent whole-day skip.
- **Exactly-once per due time** under healthy operation follows from the
  compare-and-swap claim (§7).

## 9. Authorization, Quota, And Runaway Control

- **Authorization.** Creating, editing, enabling, disabling, and deleting a
  schedule is authorized through `schedule.space_id` and ordinary Space
  membership — the same rule Tasks use. The firing Task's `created_by` is the
  schedule's creator, so quota, audit, and the run token attribute to a real
  actor, matching how Issue-originated runs attribute.
- **Disabled creator.** The run scheduler already fails a run whose creator was
  disabled. For a *repeating* trigger that would mint a failed Task every
  firing, so a firing whose creator is disabled pauses the schedule
  (`enabled = false`) instead; re-enabling is an explicit act. This reuses the
  disabled-account concept from system administration rather than inventing
  schedule-specific authorization.
- **Quota.** Each firing passes through the existing quota check in
  `task.Service`. A firing refused by quota is a failed fire, not an error that
  stops the schedule by itself.
- **Runaway control.** A schedule whose firings fail every time would burn
  quota indefinitely. After five consecutive failed fires
  (`maxConsecutiveScheduleFailures`) the dispatcher pauses the schedule and
  logs why. This is the one guard included rather than deferred, because "an
  unattended trigger that fails forever" is a concrete cost failure, not a
  hypothetical. The pause reason is logged, not yet stored or shown in Portal
  (§13).

## 10. Surfaces

- **API.** Space-scoped, peer to the Task routes and registered the same way
  (each handler subpackage's `Register`, composed in `routes.go`, matched by
  `openapi.json`):

  ```text
  POST   /api/spaces/{space_id}/schedules                 { executor_kind, executor_id, name, input, cron_expr, timezone }
  GET    /api/spaces/{space_id}/schedules
  GET    /api/spaces/{space_id}/schedules/{id}
  PATCH  /api/spaces/{space_id}/schedules/{id}            { enabled?, input?, cron_expr?, timezone?, name? }
  DELETE /api/spaces/{space_id}/schedules/{id}
  GET    /api/spaces/{space_id}/schedules/{id}/tasks      # an agent schedule's fired Tasks
  GET    /api/spaces/{space_id}/schedules/{id}/runs       # a workflow schedule's fired runs (§15)
  ```

  `cron_expr` and `timezone` are validated at write time; an invalid expression
  is a `KindInvalid` refusal, never a row that fails silently at fire time.

- **Portal.** The executor's own detail page has a Schedules section — the Agent
  detail page for an agent schedule, the Workflow detail page for a workflow one
  (§15) — that lists that executor's schedules with next and last fire, enabled
  state, Enable/Disable, Delete, the Tasks or runs each schedule produced, and a
  form to add one. The Space-wide **Schedules** page in the sidebar shows every
  schedule across agents and workflows so an owner can see what unattended
  automation is running and pause it, and also creates one there — the same
  form, plus a picker for what runs. Both entry points share one create form and
  the `manage_schedules` member capability; editing an existing schedule stays
  on its executor's page.

- **Deleting a schedule** removes only the trigger. Tasks it already created are
  independent execution history and are untouched — they are not the
  schedule's children.

## 11. Disposition Of `ChannelCron`

`ChannelCron` sat in the *Conversation* channel enum. That was the wrong plane:
[Agent execution and Task threads](agent-execution-and-task-threads.md) §13.4
removed the synthetic Conversations that Issue and Workflow runs once created,
and a scheduled run likewise creates a Task directly and never a Conversation.
The typed source of a scheduled run is a TaskRun `trigger_source`, not a
conversation transport.

`ChannelCron` is therefore removed from `internal/service/conversation/channel`
and `ValidChannels()`, and `RunTriggerSourceSchedule` exists on the Task plane
instead. This is the Alpha "fix the wrong shape coherently, no compatibility
layer" rule: the placeholder moved to the plane it belongs on rather than being
wired up where it sat.

## 12. Decisions

| Decision | Chosen | Rejected alternative and why |
|---|---|---|
| Execution record per fire | Reuse Task + TaskRun | A dedicated `schedule_run` table duplicates the unified execution plane; the run already records everything. |
| Thread model | New Task each fire | Continue-the-thread grows context/cost with no shown need; deferred as a mode. |
| Input | A fixed string | Templating adds a contract with no demonstrated need in the first slice. |
| Trigger loop home | New loop in `internal/server/scheduler` | A separate service/binary adds a process and coordination surface for one poll loop. |
| Exactly-once | Compare-and-swap on `next_fire_at` | A leader lock or external scheduler (Temporal, cron sidecar) adds a dependency the Workflow record explicitly declined as a default. |
| Runaway response | Pause after five consecutive failures | Throttling keeps spending; a silent skip hides the failure. |
| Permission | Ordinary Space membership | A separate operator capability would be a second authorization rule for the same Space-owned resource. |
| Placeholder | Remove `ChannelCron`, add `RunTriggerSourceSchedule` | Implementing a cron Conversation adapter would cement the wrong plane. |
| Local CLI timer | Out of scope; use OS cron | A resident CLI daemon duplicates the Server's resident, coordinated loop. |
| Missed fires | One coalesced catch-up | Backfilling every missed slot risks a storm; silently skipping loses a signal. |
| Cron parsing | `robfig/cron/v3`, parser only | Writing a parser in-package buys nothing for the standard five fields; the loop stays BuildMax's. |

## 13. Open Questions

- **Pause reason.** A pause caused by consecutive failures or a disabled
  creator is logged. Storing the reason and showing it beside the paused state
  in Portal is a later, Portal-facing slice.
- **Long-outage suppression.** Whether a long outage should suppress the single
  catch-up fire (a max-staleness bound) rather than always firing once. The
  default remains one catch-up (§8) until there is evidence a stale run causes
  harm.
- **Continue-the-thread mode.** Whether a later "continue the same Task each
  fire" mode is worth adding once the fixed-objective slice has usage, and what
  would bound its context growth. Recorded so the default is a deliberate
  choice, not an omission.

## 14. Delivery

The five slices landed in order, each independently tested:

1. **Core + store** (#548): `internal/core/schedule`, the `schedule` table and
   store with `DueSchedules`, `ClaimSchedule`, and `RecordFire`, MySQL-scope
   claim-contention tests, `RunTriggerSourceSchedule`, and `Task.schedule_id`.
2. **Dispatcher** (#549): the fake-clock-driven loop with cron parsing,
   coalesced catch-up, disabled-creator pause, and consecutive-failure pause.
3. **API + OpenAPI** (#550): the Space-scoped routes and their authorization
   matrix coverage.
4. **Placeholder removal** (#551): `ChannelCron` removed with its tests and
   documentation.
5. **Portal** (#552, #553): the Agent detail section and the Space-wide
   Schedules page, with browser coverage. The timezone database is embedded in
   the Server binary (#554).

## 15. Extension: Workflow Executors

The first slices bound a schedule to one Agent. The natural next question — a
Space wants "every weekday at 09:00, run the triage *workflow*" the same way it
runs an agent — is answered by generalizing what a schedule fires rather than
adding a parallel entity. A schedule's target is *an executor*, the same notion
[issues](portal-work-and-execution-experience.md) already model with
`executor_kind` (`agent` or `workflow`) and an opaque `executor_id`. The
`Schedule` reuses that vocabulary: `agent_id` became `executor_kind` +
`executor_id`, and `last_task_id` became `last_fire_ref` (the Task an agent
firing produced, or the workflow run a workflow firing produced), so the whole
cron, exactly-once claim, catch-up, eligibility, and consecutive-failure
machinery is shared, not duplicated. At Alpha there is no compatibility burden;
the migration `schedule_agent_to_executor` backfills existing rows and drops the
old columns.

Firing branches on `executor_kind` at the one place it must: the dispatcher's
`startExecutor`. An agent schedule admits a Task through the Task service as
before; a workflow schedule starts a run through
`workflow.Service.StartWorkflowRun`, tagged with the schedule so the run records
its trigger. Both are attributed to the schedule's creator and metered as that
person's work, and both fold their failures into the same
consecutive-failure pause. A firing that cannot start its executor — including a
workflow that was unpublished or archived after the schedule was created, which
`StartWorkflowRun` refuses — is recorded as a failed fire and pauses the schedule
after the bound, exactly as an agent admission failure does.

Two rules keep a workflow schedule honest. First, a workflow must be **published**
to be scheduled: the service checks this at creation (a draft or archived
workflow can never start a run), and the Portal offers only published workflows.
Second, a workflow's `input` is the run input its `input_schema` declares, frozen
on the schedule and validated by `StartWorkflowRun` on each firing, just as a
manual run's input is; the Portal generates the same input form the manual Run
dialog uses. A workflow run records the `schedule_id` that started it, mirroring
`Task.schedule_id`, so a schedule can list its firing history
(`GET /api/spaces/{space_id}/schedules/{schedule_id}/runs`) the way an agent
schedule lists its triggered tasks.

The surfaces mirror the agent case: a Schedules section on the workflow detail
page (pinned to that workflow), and the Space-wide Schedules page, which now
lists and creates schedules for both kinds and labels each by its executor.
Managing schedules stays member-tier (`manage_schedules`); it is not gated on
the owner/admin capability that authoring a workflow needs.
