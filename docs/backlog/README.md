# BuildMax Backlog

> **Audience:** maintainer and agents · **Status:** current

The backlog is the execution queue inside BuildMax's planning and delivery loop.
It holds the decomposed, ready-to-execute units of the project's own main-line
work, one file per task, so any session — a person or an agent — can pick one up
with no prior context and carry it to a merged change.

It is deliberately not GitHub issues. GitHub issues are the surface for external
contributors; this backlog is the maintainer-and-agent execution queue. See
"One item, one place" below.

## Planning And Execution Loop

```text
Roadmap priority and outcome
            |
            v
proposal, only when a decision is open
            |
            v
approved design → backlog task → claim → implementation and verification → pull request
                                                                        |
                                                                        v
                                                   current-state and documentation
                                                   + changelog when user-visible
                                                                        |
                                                                        v
                                                            Roadmap refresh
```

[`docs/ROADMAP.md`](../ROADMAP.md) is both the entry and return point. It owns
theme-level priority, sequencing, release outcomes, and completion gates; it
does not hold executable units. When an outcome still has a material product,
security, data, or architecture choice, a proposal frames that decision. Once
the choice is approved, [`docs/design/`](../design/README.md) owns its durable
rationale and this backlog owns its independently executable slices.

An Agent carries a ready task through implementation, proportional verification,
documentation, and a pull request. After merge,
[`docs/current-state.md`](../current-state.md) and affected documentation record
what now ships. A user-visible change also receives a changelog entry; a test-only
or internal documentation task does not invent one. The resulting evidence and
newly exposed gaps feed the next Roadmap review, which closes the loop.

A proposal is not mandatory ceremony. Small direct fixes may use the entry-gate
exception below, and an approved design can be decomposed immediately. A task
that discovers an unresolved decision leaves execution and returns to proposal
or design instead of letting the implementing Agent choose product direction
silently.

## Ownership

| Role | Owns |
|---|---|
| Maintainer | Roadmap priority, proposal and design approval, admission and ordering of backlog tasks, and merge or release decisions |
| Planning Agent | Verify current code and documentation, detect duplicate tracking, and draft or refresh tasks without promoting an unresolved decision |
| Implementing Agent | Claim the highest-priority unblocked task, stay inside its scope, run its required verification, update affected documentation, and deliver a reviewable pull request |
| Verifier or reviewer | Test the claimed outcome and forbidden side effects against the task and evidence; return a product or architecture ambiguity to the maintainer |

The purpose of this split is to concentrate maintainer attention on priority,
real decisions, and acceptance while Agents perform routine investigation and
delivery. It does not delegate merge, release, or silent scope-expansion
authority.

## The Queue Is The Directory

Each task is one file named `NN-slug.md`. The two-digit `NN` prefix is the
priority: the directory sorted by name is the queue, highest priority first.
Leave gaps (`10`, `20`, `30`) so a task can be inserted without renaming its
neighbours. Reordering is a rename; there is no separate index to keep in sync.

Only `NN-slug.md` files are live tasks. [`TEMPLATE.md`](TEMPLATE.md) is the
starting point for a new one and is not itself a task.

The frontmatter is also the machine-readable status signal. `./make board`
derives the project status view from it — a task is in progress when `claim` is
set, in review when `pr` is set, ready when it is unclaimed and every
`depends_on` file has already merged and been deleted, and blocked while a
`depends_on` file still exists. `NN` order groups it by priority and `roadmap`
groups it by Roadmap theme. An architecture test rejects a live task whose
frontmatter is missing a field or uses a malformed `roadmap`, `claim`, or `pr`
value, so the view cannot silently go stale.

Keep a short ready horizon rather than decomposing the whole Roadmap. There
should be enough unblocked work for the next few Agent sessions, while later
themes stay at Roadmap or design granularity until their dependencies and
evidence make the task boundaries stable.

## From Roadmap To Ready Tasks

Replenish the queue from the highest active Roadmap priority:

1. Read the Roadmap outcome and completion gate, its approved design sections,
   current-state evidence, and the code that owns the behavior.
2. Search the backlog, open pull requests, and GitHub Issues before drafting. A
   unit already tracked elsewhere is not copied here.
3. Return unresolved choices to proposal or design. Do not encode one
   implementation option as a ready task before its rationale is approved. A
   decision request to the maintainer states the user outcome, current
   evidence, constraints, a recommended option, one meaningful alternative, and
   the exact decision requested — short enough to decide before reviewing a
   design that already assumes a direction.
4. Split the accepted work at independently verifiable outcomes. Each task must
   be small enough for one implementation session and name dependencies on
   other task files when it cannot start alone.
5. The maintainer decides which drafts enter the queue and assigns their `NN`
   order. Agents take the first unclaimed task whose dependencies are complete.

## Lifecycle

- **Create** a task when its source design is approved. A planning session may
  draft it, but the maintainer's priority decides its `NN` and whether it enters
  the queue at all.
- **Claim** a task by setting `claim` in its frontmatter before starting work,
  so two sessions do not pick the same one. Write it as `<handle> <YYYY-MM-DD>`
  (e.g. `gougoujiang 2026-09-13`). Clear it if the work is abandoned. At most one
  live claim per task.
- **Limit concurrency** so parallel work does not turn the maintainer into the
  merge queue: at most two or three active writing tasks, and at most one in the
  same capability at a time. Stateful deployment suites run one at a time unless
  their commands own isolated environments (see the ephemeral kind cluster in
  [`docs/contribute/testing.md`](../contribute/testing.md)). A red shared
  boundary pauses new changes to that boundary until the failure is classified.
- **Open a pull request** and record its number in the `pr` frontmatter field, so
  a claimed task that is being written is distinguishable from one already in
  review. Clear `pr` only if the pull request closes without merging.
- **Delete** the file when the work merges. The backlog holds only pending work;
  the permanent record is its pull request, applicable current-state and
  documentation updates, and a changelog entry when the change is user-visible.
  There is no archive here — git history keeps deleted tasks.
- **Refresh:** readiness is not permanent. A task whose scope, acceptance, or
  cited design has gone stale leaves the queue (delete it, or drop it back to a
  draft) until it is made ready again.

## Entry Gate

- Main-line work goes through a design record first, and the task's `source`
  links the approved design section it derives from.
- Small, low-risk, self-evident work (a doc fix, a narrow test gap) may enter
  the backlog directly with `source: direct` and no design record.

## One Item, One Place

A unit of work lives in exactly one tracking surface. Main-line and
agent-executed work is a backlog task. Externally contributable work is a GitHub
issue (`agent-ready`, `good first issue`, `help wanted`). Do not mirror the same
work into both.

## What A Ready Task Must Answer

A task is ready only when a session with no prior context can act on it. See
[`TEMPLATE.md`](TEMPLATE.md) for the exact shape. It must state the outcome and
why it matters, the scope and what is explicitly out of scope, concrete
acceptance criteria, and the verification scopes to run — selected from
[`docs/contribute/testing.md`](../contribute/testing.md).
