# Issue Agent Access: What The Agent May Say About Its Work Order

> **简体中文：** [阅读中文镜像](../zh-CN/design/Issue Agent访问.md)

## Contents

- [Status](#status)
- [1. Decision](#1-decision)
- [2. What The Agent May Never State](#2-what-the-agent-may-never-state)
- [3. A Local Agent's Report Is A Claim, And Says So](#3-a-local-agents-report-is-a-claim-and-says-so)
- [4. Untrusted Input Stays Data](#4-untrusted-input-stays-data)
- [5. Out Of Scope](#5-out-of-scope)
- [6. Open Questions](#6-open-questions)

## Status

- roadmap_priority: `unscheduled` — this decides the Agent-mutation question
  that the implemented Issue model deliberately left separate; it is not
  placed in [../ROADMAP.md](../ROADMAP.md)
- status: `product boundary in force; mechanism superseded` — the rules below
  hold on both planes (a worker run started from an Issue, and a local session
  started with `buildmax issue start`). The *mechanism* is no longer two
  in-process tools: an Agent reads and reports on its Issue by running
  `buildmax issue` through `Bash`, and the per-Issue guardrails live in the
  Server route. This record now owns only the product boundary — what an Agent
  may assert about Space-owned work — which is transport-independent.
- superseded_by (mechanism): [agent-bridge-cli.md](./agent-bridge-cli.md) — it
  replaced the `GetIssue` / `ReportToIssue` tools with the `buildmax` command
  surface and moved scope to the run token and the comment budget/body limit to
  `issue.Service.CreateComment`. Its §8 restates the boundary below in the
  command-surface world.
- relates: [tool-permissions.md](./tool-permissions.md),
  [unified-artifacts.md](./unified-artifacts.md),
  [surface-positioning.md](./surface-positioning.md),
  [portal-execution-model.md](./portal-execution-model.md)
- precedes: [surface-positioning.md §5.5](./surface-positioning.md#55-local-issue-work),
  which decided the local Issue work this record left open (no durable
  Issue↔Session link, workspace mapping, or local-result record type)
- touches: `internal/service/issue`, `internal/server/handlers/work`,
  `internal/server/handlers/worker`
- created_at: `2026-08-29`

## 1. Decision

An Agent working an Issue may do exactly two things to it: **read** the bounded
Issue it was started for — its description, sub-issues, and recent discussion —
and **post one bounded report** on that Issue's thread. Nothing else about the
Issue is Agent-writable.

How it reaches the Issue is [agent-bridge-cli.md](./agent-bridge-cli.md)'s
concern: `buildmax issue show` / `buildmax issue comment`, scoped to the run's
one Issue by the run token in a worker and by the user's login locally. This
record decides only the *boundary* — what the Agent may assert — which holds
whatever the transport.

Three rules make that boundary:

1. **Status, owner, executor, and hierarchy are never Agent-writable.** §2.
2. **A local Agent's report is stored as a claim**, `local_agent`, not `agent`.
   §3.
3. **Issue text is data, never a prompt layer.** §4.

## 2. What The Agent May Never State

`status`, `owner_id`, `executor_kind`, `executor_id`, and `parent_issue_id` are
not writable by any Agent path. Creating a child Issue is not one either. A
worker run has no route that writes them. Locally, `buildmax issue status` exists
for the person and runs with their full authority, so an Agent in a local
session is held back by the issue-linked run's `issue` prompt layer, not by the
Server (see [Agent Bridge CLI](agent-bridge-cli.md) §8).

This preserves an invariant the product already holds — nothing in the codebase
moves an Issue's status on its own — rather than inventing one. The reasoning is
asymmetric cost: `done` is what a space reads to plan around, and its meaning is
*a person accepted this*. If a model can write it, the word stops carrying that,
and the loss is a space coordination failure. What is saved by letting the model
write it is one click by someone who was going to read the result anyway.

An Agent that believes work is finished says so in its report. A person moves
the Issue.

## 3. A Local Agent's Report Is A Claim, And Says So

A worker run's report is stored as `agent`: the run token that wrote it is the
Agent's own credential, and the task and run it names are records the deployment
holds. A local session has none of that. It holds a *person's* session, it ran
on a machine the deployment did not schedule, admitted no quota for, and
recorded no trace of.

Storing both under `agent` would make a Portal reader believe the deployment
vouched for something it never saw. So a local report is stored as
`local_agent`, authored by the person who relayed it — the one identity the
server verified, and the accountable one. It names no task and no run, because
there is none. Portal shows it as reported rather than said.

The space comment route accepts `author_kind` only as absent or `local_agent`. A
person's session may not write `agent` or `system`: those are the deployment's
own voices, written by a run token and by the server.

This does not make a local report evidence. It makes the claim legible as a
claim, which is the most a client report can honestly be.

## 4. Untrusted Input Stays Data

An Issue's description and comments are third-party text. Anyone on the space
can write them, and a future inbound connector could carry them in from an
external tracker. They are the same trust class as `WebFetch` output.

Two consequences, both binding:

- **They arrive as data the Agent reads, never as a system-prompt layer.** This
  is a security rule and a caching rule at once: `AGENTS.md` fixes the system
  prompt from bounded instruction layers that are stable for a run, so they can
  be the cacheable prefix. A mutable Issue snapshot in a layer would break that
  prefix on every edit as surely as it would launder a comment into an
  instruction. Command output lands on stdout and the Agent reads it as a `Bash`
  result — already data.
- **The rendered thread labels every comment with its author kind.** A model
  that cannot tell a spacemate's comment from its own principal's instruction
  has no basis for treating them differently.

Starting a worker run still flattens the Issue into the run's initial message.
That is input, and it stays input; nothing here moves it into a layer.

## 5. Out Of Scope

- Any Issue mutation beyond a comment — status, assignment, reparenting, child
  creation, delete, or archive.
- Addressing an Issue other than the scoped one, including a sibling or the
  parent of a scoped child. The transport enforces this (the run token names one
  Issue; the local commands are the user's own authority); this record forbids
  it as a boundary regardless.
- The command surface, transport, and per-Issue guardrail placement, all owned
  by [agent-bridge-cli.md](./agent-bridge-cli.md).
- A durable Issue↔Session link and offline outbox, decided against in
  [surface-positioning.md §5.5](./surface-positioning.md#55-local-issue-work).

## 6. Open Questions

1. **Where does a runless session's result appear?** Answered for now in
   [surface-positioning.md §5.5](./surface-positioning.md#55-local-issue-work):
   in the discussion. A local report is a `local_agent` or person comment that
   names any Artifact it published; outputs aggregation keeps reading runs, and
   no record for local work is added until a need for one is observed.
2. **Does a Portal Tier 1 conversation get Issue access, and at what scope?** A
   conversation is the single voice to the user but is not scoped to one Issue,
   so the boundary above would need a different scoping story.
3. **Does a scoped child Issue need to see its parent?** Reading upward is a
   wider scope than "the work order in front of me", and the parent's
   description is often where the actual requirement lives.
4. **Does `local_agent` need a session of its own to become evidence?** §3
   decides how a local report is recorded, not how far it can be trusted; making
   it evidence is the durable-Agent-sessions question, not this one.
