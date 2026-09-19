# Agent Bridge CLI: One Command Surface From Agent To Server

> **简体中文：** [阅读中文镜像](../zh-CN/design/Agent 桥接 CLI.md)

## Contents

- [Status](#status)
- [1. Decision](#1-decision)
- [2. The Gap This Closes](#2-the-gap-this-closes)
- [3. Why This Reverses Issue Agent Access](#3-why-this-reverses-issue-agent-access)
- [4. One Surface, Two Contexts](#4-one-surface-two-contexts)
- [5. Where Scope And Guardrails Live Now](#5-where-scope-and-guardrails-live-now)
- [6. Credential Handling](#6-credential-handling)
- [7. Command Surface](#7-command-surface)
- [8. What The Agent May Never Do](#8-what-the-agent-may-never-do)
- [9. What This Supersedes](#9-what-this-supersedes)
- [10. Out Of Scope](#10-out-of-scope)
- [11. Implementation Phases](#11-implementation-phases)
- [12. Open Questions](#12-open-questions)

## Status

- roadmap_priority: `unscheduled` — this decides how an Agent reaches Server
  resources at all; it does not yet sit in [../ROADMAP.md](../ROADMAP.md) and
  will be placed against R5 when scheduled.
- status: `implemented` — the maintainer accepted, on `2026-09-19`, that a single
  `buildmax` command surface becomes the Agent's way to reach the Server, **fully
  replacing** the in-process Issue tools rather than standing beside them. All of
  §11 has shipped: the Server-side guardrails, the local command surface, the
  worker bridge, the retirement of `GetIssue` / `ReportToIssue`, and the kind
  end-to-end proof — which also caught and fixed the sandbox tmpfs masking the
  bridge socket (§4).
- reverses: [issue-agent-access.md](./issue-agent-access.md) — its mechanism
  (the in-process `GetIssue` / `ReportToIssue` tools), now removed. Its product
  boundary (§8 here) survives unchanged, and that record is reduced to it.
- follows: [worker-run-token.md](./worker-run-token.md),
  [client-modes.md](./client-modes.md),
  [worker-api-network-boundary.md](./worker-api-network-boundary.md)
- relates: [tool-permissions.md](./tool-permissions.md),
  [sandbox-boundaries.md](./sandbox-boundaries.md),
  [unified-artifacts.md](./unified-artifacts.md),
  [../proposals/client-sessions-and-api-credentials.md](../proposals/client-sessions-and-api-credentials.md)
- folds: the client-command half of
  [../proposals/local-issue-work-bridge.md](../proposals/local-issue-work-bridge.md)
- touches: `internal/interface/cli`, `internal/interface/client`,
  `internal/interface/auth`, `internal/agentapp/taskrun`, `internal/bootstrap`,
  `internal/config`, `internal/server/handlers/work`,
  `internal/server/handlers/worker`, `internal/tool`
- created_at: `2026-09-19`

## 1. Decision

An Agent reaches the BuildMax Server through **one command surface** — the
`buildmax` binary it already runs from — invoked as ordinary subprocesses
through the `Bash` tool. There is no per-capability in-process tool for Server
resources. Reading an Issue, commenting on it, publishing an Artifact,
triggering another Agent, listing work — all of it is a `buildmax` subcommand.

The same subcommands serve a person at a terminal and an Agent inside a run.
The command names and output do not change between them. What changes underneath
is **which credential authenticates the call** and therefore **which routes and
which resources the caller is allowed** — resolved automatically from the run
context, never chosen by the model.

This reverses the earlier decision in
[issue-agent-access.md](./issue-agent-access.md) to give the Agent two bespoke
in-process tools. §3 recovers that record's rationale and states why a command
surface satisfies it better under current conditions.

## 2. The Gap This Closes

Today an Agent can reach the Server only through Go tools compiled into the
runtime: `GetIssue`, `ReportToIssue`, `UploadArtifact`, and managed inference.
Every new Server capability an Agent should be able to use — trigger a run,
list its assigned Issues, start a Workflow — is a new tool with its own
registration, permission entry, and provider round-trip. The surface grows one
concept per capability.

Two facts make that costly:

1. **Non-native Agents have no tools at all.** A Harbor custom Agent, a shell
   script, or any executor that is not BuildMax's own loop cannot call a Go
   tool. It can run a command. If Server access is a tool, those executors are
   locked out; if it is a command, they are first-class.
2. **A subprocess inside a run has no way back to the Server today.** The run
   token is read once and immediately scrubbed from the environment
   (`takeEnv` in `internal/bootstrap/worker.go`), and `FilterWorkerEnv`
   (`internal/config/env_spec.go`) strips unowned `BUILDMAX_*` vars from
   children. The Go-level capabilities are held in process; nothing a
   `Bash`-spawned command can use exists. A command surface has to close this
   gap, and closing it is what makes the surface uniform.

One command surface removes the "one tool per capability" growth and admits
every executor that can run a program.

## 3. Why This Reverses Issue Agent Access

[issue-agent-access.md](./issue-agent-access.md) chose two in-process tools for
three deliberate reasons. Each still matters; none requires a tool.

**Scope by construction.** `GetIssue` / `ReportToIssue` take no Issue
identifier — the scope is a constructor argument, so the model cannot address a
second Issue. The command surface preserves this, but moves the guarantee to
the credential rather than the tool constructor. In a worker run the run token
names exactly one TaskRun and its one Issue; the worker routes reject any other
Issue regardless of arguments (see
[worker-run-token.md](./worker-run-token.md)). `buildmax issue view` with no
argument resolves to *that* Issue because there is no other it is allowed to
name. Scope is enforced where authority actually lives — the token and the
route — not in a client-side constructor that a second client could bypass.

**Guardrails.** The comment budget (`issueReportBudget = 3`) and body limit
(2000 characters) live in the tool layer today, which means they hold only for
callers that go through the tool. Moving to a command surface forces them to the
Server route, where they hold for the tool, the CLI, Portal, and any future
client alike. This is the single-authoritative-implementation rule from
[AGENTS.md](../../AGENTS.md): a validation rule belongs at the Server, not
replicated per client. The reversal *improves* guardrail placement.

**Untrusted input stays data.** Issue text must arrive as a tool result, never
as a prompt layer, so a comment cannot inject instructions. Command output is
already data — it lands on stdout and the Agent reads it as a `Bash` result,
exactly the property the tool gave. No regression.

So the tool's three guarantees are kept, and two of them move to a stronger
place. What the tool uniquely offered — being available without a binary on
`PATH` and appearing as a named entry in the tool-permission policy — is
addressed in §7 and §11.

## 4. One Surface, Two Contexts

The `buildmax` binary detects its context and picks a transport and credential
without the model's involvement.

**Local context** — a person's `buildmax` session, or an Agent running inside
one on the person's machine. Calls use the logged-in user's credential from the
existing auth broker (`internal/interface/auth`, `TokenForServer`), reach the
public listener, and are authorized as that user. This is already how a human's
`buildmax issue` works; an Agent invoking the same command inherits the same
path. Available scope is the user's full authority (see §6 for the accepted
risk).

**Worker context** — an Agent inside a worker run, where the run token is
scrubbed from the environment. The worker process runs a small **bridge**: a
Unix-domain socket inside the run whose path is exported to subprocesses as
`BUILDMAX_BRIDGE_SOCK`. The `buildmax` binary, seeing that variable, sends its
Server calls to the socket; the bridge attaches the in-process run token and
proxies to the internal worker listener
([worker-api-network-boundary.md](./worker-api-network-boundary.md)). The token
never enters the subprocess environment, arguments, or output. Available scope
is exactly the worker routes: this run, its one Issue, its Artifacts, its
Secrets, its managed inference — no more.

The socket lives under `/tmp`, which the worker's Bash sandbox masks with a
private tmpfs, so the sandbox must re-expose exactly that socket for the
subprocess to reach it: the bwrap backend binds `BUILDMAX_BRIDGE_SOCK` back in
after the tmpfs (`internal/infra/sandbox`). Without that bind the `buildmax`
command a sandboxed Agent runs cannot dial the bridge at all — found only by the
kind end-to-end run (§11 phase 5), not by any unit test.

Context selection is by presence: `BUILDMAX_BRIDGE_SOCK` set → worker context;
otherwise a stored login → local context; neither → the command explains it is
not connected. A command that a context does not permit (for example
`agent trigger` under a run token, which the worker routes deliberately do not
expose) fails with a clear, LLM-meaningful message — never a silent degrade and
never a new worker route opened to force symmetry.

## 5. Where Scope And Guardrails Live Now

Authorization has one authoritative home: the Server route, keyed by the
credential.

- **Worker context** is bounded by the run token and the worker route set. The
  boundary is the *absence* of routes: there is no Issue-update route, no
  membership route, no task-create route, so an Agent under a run token cannot
  reach them whatever it types. This is unchanged from
  [worker-run-token.md](./worker-run-token.md); the bridge only carries the
  token, it grants nothing extra.
- **Local context** is bounded by the user's Space authorization, checked on
  every call, exactly as Portal is. Membership removal stops further access.
- **Per-Issue guardrails move Server-side.** Both the public and worker comment
  routes converge on one service function, `issue.Service.CreateComment`, so the
  Agent body limit (`AgentCommentBodyLimit`, applied to `agent`/`local_agent`
  authors before Artifact references are appended) and the per-run budget
  (`RunCommentBudget`, counted by `source_task_run_id`) are enforced there — one
  authoritative implementation that binds the runtime tool, the CLI, and any
  future client. The stricter Agent limit does not touch a person's comment,
  which keeps the universal `CommentBodyLimit`. The tool-layer constants were
  removed with the tools (§11 phase 4).

## 6. Credential Handling

The controlling rule from
[client-sessions-and-api-credentials.md](../proposals/client-sessions-and-api-credentials.md)
is that a credential must not be reachable by anything the model can see.

- **Worker context honours it fully.** The run token stays in the worker
  process; the subprocess receives only a socket path. Nothing the model reads
  or writes carries the token.
- **Local context accepts a documented risk.** The maintainer chose, on
  `2026-09-19`, to let a local Agent use the **user's own credential directly**
  rather than a scope-narrowed session token. Because the local `Bash` sandbox
  is off by default, an Agent that can run `buildmax` can already act with the
  user's full authority — reading any Issue the user can, triggering any Agent
  the user can, and, if the user is an administrator, reaching admin routes.
  This is accepted as within the user's own trust domain on their own machine.
  It is recorded here as a conscious trade, not an oversight; a narrowed local
  credential (route-level `scope`/`aud`/`client_id`, still unbuilt per the
  credentials proposal) is the upgrade path if that authority ever needs
  bounding, and §12 tracks it.

The `buildmax` binary never receives a token through an argument, an environment
variable it must be handed, prompt text, or tool input. Locally it reads the
broker; in a worker it uses the socket. Both keep the secret out of the model's
reach except for the local full-authority trade above.

## 7. Command Surface

The surface is the existing `buildmax` command tree, extended so that Server
capabilities an Agent needs are all reachable. Names are illustrative; the
authoritative list is the code and [manual/cli.md](../../manual/cli.md).
The point is the shape, not the exact spellings.

**Organization.** Server-resource commands stay at the top level as singular
resource nouns — `buildmax issue`, `buildmax agent`, `buildmax task`,
`buildmax artifact`, `buildmax workflow`, `buildmax run` — extending the
existing `buildmax issue` group rather than moving under a wrapper such as
`buildmax connect …`. The resource noun *is* the grouping: `buildmax issue
--help` answers "what can I do to an Issue." Three reasons decide this:

1. **One surface, human and Agent.** §1 requires identical command names across
   a person's terminal and an Agent's `Bash`. A `connect` namespace would either
   rename the shipped `buildmax issue` commands or create two ways to do one
   thing, and would signal a special "mode" where there is only a resolved
   context (§4).
2. **A group axis is a resource, not a transport.** Whether a command reaches
   the Server is a property of its resource, decided per route, not a mode the
   caller opts into. `connect` reads as an action (it is what `buildmax login`
   already does), so nesting nouns under it is a category error.
3. **Agent ergonomics.** The Agent *types* these through `Bash`; every required
   segment is another token and another chance to err. `buildmax issue view`
   beats `buildmax connect issues view`.

To keep a long top level legible, commands are sorted in `--help` with cobra
command groups (`cmd.AddGroup` / `GroupID`, available in the vendored cobra
`v1.10.2`): a "Server" group (`issue`, `agent`, `task`, `run`, `artifact`,
`workflow`, `admin`, `plugin`, `usage`) and a "Local" group (`init`, `doctor`,
`version`, `sandbox`, `tools`, `project`). Grouping changes only how `--help`
presents commands; invocation paths are unchanged. This is how a user sees that
a set of commands is the same kind without a naming-tree layer that the model
must reproduce.

| Command | Local (user authority) | Worker (run-scoped) |
|---|---|---|
| `issue view` | any Issue the user may read | the run's one Issue |
| `issue list` | the user's assigned Issues | not available |
| `issue comment` | Server-enforced budget/limit | same, on the run's Issue |
| `artifact publish <path>` | into a named Space | into the run's Space |
| `run status` | a run the user may read | this run (status, cancel flag) |
| `agent trigger` / `task create` | yes | not available (single-run edge) |
| `workflow run` | yes | not available |

Two rules keep the surface honest:

1. **No command takes a credential.** Context resolves it (§4).
2. **Unavailable is explicit.** A command a context does not permit says so and
   exits non-zero with a message written for the model, per the tool-output
   rule in [AGENTS.md](../../AGENTS.md). It does not degrade silently, and the
   worker route set is never widened to make a local command work under a run
   token.

Output is written for an LLM reader first: meaningful on success and failure,
stable, and free of the credential. Machine-readable output (`--json`) is
available for scripted Agents.

## 8. What The Agent May Never Do

These invariants are inherited from
[issue-agent-access.md](./issue-agent-access.md) and hold regardless of
transport:

- **Status, owner, executor, and hierarchy are not Agent-writable.** The Agent
  says what happened through a comment; only a person states what state the work
  is in. No command exposes these as an Agent-usable write in either context.
- **A local Agent's report is stored as a claim** (`local_agent` authorship),
  not as a Worker-verified result.
- **Issue and comment text is data, never a prompt layer** (§3).

The command surface changes the *mechanism* of Agent Server access, not the
*boundary* of what an Agent may assert about Space-owned work.

## 9. What This Supersedes

- **[issue-agent-access.md](./issue-agent-access.md)'s mechanism.** The
  `GetIssue` / `ReportToIssue` tools and their `internal/tool` registration are
  removed (§11 phase 4). That record is reduced to the surviving product
  boundary, which §8 here owns and expresses in the command-surface world; an
  issue-linked run learns of the command through the `issue` prompt layer, since
  there is no longer a tool to discover.
- **The client-command half of
  [../proposals/local-issue-work-bridge.md](../proposals/local-issue-work-bridge.md).**
  That proposal's question of how a local client reads, reports, and returns
  work is answered here. Its still-open questions (durable Issue↔Session link,
  workspace mapping, local-result record type) are not decided by this record
  and keep it open until they are.

## 10. Out Of Scope

- Route-level `scope` / `aud` / `client_id` and a narrowed local credential.
  Tracked by the credentials proposal; §6 accepts full user authority locally
  for now.
- Personal access tokens and service accounts for headless non-interactive use
  outside a run. A named use case would revive credentials-proposal Stage 3.
- The durable Issue↔Session link and offline outbox from the bridge proposal.
- Any new worker route. This record adds none; it only carries the existing run
  token to a subprocess.

## 11. Implementation Phases

1. **Server-side guardrails first.** Move the Issue comment budget and body
   limit into `issue.Service.CreateComment` — the one function both the public
   and worker comment routes reach — with tests, so they hold before any client
   stops enforcing them. **Shipped:** `AgentCommentBodyLimit`, `RunCommentBudget`
   (counted by a new `CountIssueCommentsBySourceTaskRun` store method), and
   Artifact-reference composition all moved into the service.
2. **Local command surface.** Extend `internal/interface/cli` and
   `internal/interface/client` with the breadth commands (§7) over the existing
   auth broker; confirm an Agent's `Bash` subprocess reaches them with the
   user's credential. Highest value, lowest new plumbing. **Shipped so far:**
   `buildmax issue comment` (posts a `local_agent` report via `CommentOnIssue`),
   `buildmax agent trigger` and `buildmax task status` (the trigger-and-observe
   loop, via `TriggerAgent`/`GetTask` with `FindAgent`/`FindTask` fanning out
   across spaces since there is no ambient space), `buildmax artifact publish`
   (uploads a file via `PublishArtifact` over `httpclient.UploadFile`, printing
   the id usable in an `Artifacts:` reference), `buildmax workflow run`/`list`/
   `status` (start a published workflow and follow it, via `RunWorkflow`/
   `ListWorkflows`/`GetWorkflowRun` with `FindWorkflow` fanning out), and the
   `--help` command groups (Server vs Local, `groupTopLevelCommands` in
   root.go). Remaining: `task create`, `run status`.
3. **Worker bridge.** Run a Unix-socket reverse proxy inside the worker run that
   injects the run token and forwards to the worker listener, so a subprocess
   reaches the worker API without ever holding the token. **Shipped so far:** the
   transport — `internal/infra/runbridge`, started in `bootstrap.RunWorker` bound
   to the run, which exports `BUILDMAX_BRIDGE_SOCK` and `BUILDMAX_TASK_RUN_ID`
   (the Bash subprocess inherits them; neither is secret, and `_SOCK`/`_ID` names
   survive sandbox env scrubbing, so no `FilterWorkerEnv` change is needed). It
   forwards only `/api/worker/` paths and fails open (a run whose bridge cannot
   start still executes). The CLI detects the socket (`inWorkerRun`) and routes
   accordingly: `buildmax issue comment` inside a run posts to the run's one
   issue through the bridge (worker route, no id, the run budget) and refuses an
   issue id, and `buildmax artifact publish` uploads through the bridge to the
   run's artifact route, with the space taken from the run token. `issue show`
   and `task status` inside a run take no id and read the run's own issue and
   status through the bridge. Commands with no worker route — `agent trigger`,
   `task create`, `workflow run` — stay local-only and fail under a run token,
   which is the single-run boundary, not a gap. Remaining: kind proof that one
   run's bridge cannot reach another run's routes (already true by the run
   token, which the bridge only carries).
4. **Retire the tools.** Remove `GetIssue` / `ReportToIssue` from
   `internal/tool`, update `internal/tool/names.go`, and reduce or retire
   [issue-agent-access.md](./issue-agent-access.md) per §9. Ensure the
   `buildmax` binary is on `PATH` inside worker images so the surface exists
   where the Agent runs. **Shipped:** the tool structs, their registration, and
   the tool-layer budget/body-limit constants are gone; the dead `IssueClient`
   plumbing through `agentapp` and `internal/interface/auth`/`client` is removed
   (the `tool.IssueClient` port stays — the `buildmax` command uses it over the
   bridge). An issue-linked run now gets an `issue` system-prompt layer
   (`agentapp.PromptCapabilities.Issue`) pointing it at `buildmax issue show` /
   `comment`, the only signal it has that an Issue exists. `buildmax` is already
   at `/usr/local/bin/buildmax` in `deployment/docker/Dockerfile.buildmax`, so
   the worker image needs no change. issue-agent-access.md is reduced to the
   product boundary.
5. **Documentation and evidence.** Update [manual/cli.md](../../manual/cli.md),
   [../current-state.md](../current-state.md), the tool inventory, and add a
   changelog fragment; run the kind end-to-end path to show an Agent inside a
   run using the CLI to reach only its own run. **Shipped:** the kind run proved
   both halves against a real worker pod. Positive path — the mock was armed to
   run `buildmax issue comment` in a worker run; the CLI posted through the
   bridge (`"Commented on this run's issue."`) and the comment landed on that
   run's own Issue as `agent` with its `source_task_run_id`. Isolation — inside
   a run, `buildmax issue show` read the run's own Issue, while the same command
   with `BUILDMAX_TASK_RUN_ID` overridden to another run was refused `403 … this
   run token does not authorize that task run` (`worker/run_token.go`). The run
   first surfaced the tmpfs/socket defect fixed in §4; a bwrap golden test locks
   the socket bind in.

## 12. Open Questions

1. **Tool-permission granularity.** Named tools appear individually in
   [tool-permissions.md](./tool-permissions.md); collapsing Server access into
   `Bash` coarsens that policy. Does the bridge or a `buildmax`-aware permission
   matcher need to re-expose per-command approval, or is `Bash` policy plus
   Server authorization sufficient?
2. **Binary availability everywhere.** Worker images must carry `buildmax`; do
   all execution surfaces (evaluation adapters, third-party executors) get it on
   `PATH`, and what is the failure mode when they do not?
3. **When to narrow local authority.** §6 accepts full user authority locally.
   What concrete use case first requires the route-level `scope`/`aud` and a
   narrowed local credential, moving that work out of §10?
4. **`--json` contract stability.** If scripted Agents depend on machine output,
   which commands commit to a stable JSON shape, and where is that contract
   recorded?
