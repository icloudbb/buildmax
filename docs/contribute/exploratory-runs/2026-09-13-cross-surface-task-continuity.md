# Desktop, Portal, server, and worker continuity — 2026-09-13

> **简体中文：** [阅读中文镜像](../../zh-CN/contribute/exploratory-runs/2026-09-13-cross-surface-task-continuity.md)

**Charter.** Journeys: (1) a configured Desktop user creates a short local
task, approves its write, and recovers the session after leaving and relaunching;
(2) a Portal user sends a conversation turn, leaves and reopens it, then creates
an Agent-backed Task and continues it as a second TaskRun; (3) the deployed
server and worker boundary carry those actions through real MySQL, object
storage, Kubernetes Jobs, sandboxing, and the internal worker API. Rationale:
these are the shortest connected journeys that exercise all requested shipped
surfaces without manufacturing coverage. Roles: configured local Desktop user
and ordinary Portal user. Allowed mutations: one disposable local project, one
test-only Portal Agent/Task, one task-owned ephemeral kind cluster, and
gitignored evidence. Time spent: about 75 minutes of active exploration, with a
long idle interval before the final browser-suite attempt.

**Environment.** Clean starting commit `f894e0fc` on dedicated worktree
`buildmax-exploratory-surfaces-20260913`, branch
`codex/exploratory-surfaces-20260913`. macOS 26.6.2 arm64; Go 1.26.6; Node
24.19.0; Docker 29.7.2; kind 0.31.0; kubectl 1.35.1. Desktop was built and run
through the Wails developer bridge. Portal/server/worker ran in task-owned kind
cluster `buildmax-eph-2fd7a1ed` at `http://localhost:51415`, with two server
replicas, real MySQL and MinIO, and per-run worker Jobs. Starting Portal data was
the deployment-smoke account's personal Space; the new Agent and Task were
uniquely named for this session.

**Models.** Desktop used the configured real OpenRouter-backed
`openai/gpt-5.6-luna` for one small trial: 8,666 input / 84 output tokens,
4,165 cache-read / 4,254 cache-write tokens, displayed cost 0.001297 USD and
cache saving 0.000537 USD. Portal Tasks and conversations used the committed
free in-cluster `BuildMax smoke` model; the inspected run reported 3 input / 3
output tokens and no managed-gateway accounting, as expected for the cluster's
direct mock mode. Prompts and files contained only test phrases.

**Explored** (action → observation → next question):

1. Built Desktop, opened disposable project `Desktop Explore Worktree`, and
   submitted “Create desktop-worktree.txt containing exactly
   DESKTOP-WORKTREE-OK…” → the Write approval showed the path and content;
   `Allow once` produced the exact file and `DESKTOP-DONE`. Session
   `da4bc3e4-23d7-4c19-9a10-fe7a02723d02`, run
   `3swnft26yvqzoaidrc7a`.
2. Left for New Chat, reopened the session, stopped and restarted the Desktop
   browser driver, and reopened it again from Recent chats → the user turn,
   Write result, assistant reply, model, token/cache totals, cost, and run
   summary remained available.
3. Signed into the ephemeral Portal as the ordinary deployment-smoke account,
   sent “How is this deployment doing?” through the home composer → the real
   WebSocket-backed conversation returned `deployment smoke ok`. Left for
   another Space page, returned to Chat, reopened the recent conversation, and
   reloaded its direct URL → both turns survived.
4. Created Agent `Exploratory continuity 20260913`, ran it with initial input
   `Verify direct Task continuity.`, and reopened Task
   `3zozxlh4dydi4ayomcmq` from the Agent's Runs table and by direct URL → the
   Task completed through worker Job `ocef3i24db3bxh722tya` and survived reload.
5. Continued the same Task with `Second direct turn for continuity.` → new
   TaskRun `gazixnq742fyden2h2da` succeeded, the conversation showed two user
   turns and two results, Task details showed two runs, and the runs API linked
   the second to the first with `previous_task_run_id`.
6. Inspected Task and Run details → sandbox decision (`bwrap`, `auto_allow`),
   worker input, model, tokens, timing, workspace restore/checkpoint, MCP state,
   and managed-call mode were visible. Comparing this with the runs API exposed
   Finding 1.
7. Ran the deployment smoke twice → both attempts passed Portal, auth, Space
   authorization, storage, scheduler, worker execution, artifact, retry, and
   cancellation. The second boundary probe progressed past its labelled-worker
   allow and unlabelled-pod deny assertions, then exposed Finding 2.
8. Queried the public worker-shaped route from the host and an unlabelled
   in-cluster pod → both actually received HTTP 404. Server/worker logs also
   showed the two explored TaskRuns reaching only the internal worker listener,
   restoring/checkpointing their workspace, streaming results, and terminating
   successfully.

**Findings.**

- **A continued TaskRun's Run details show the Task's first input** — Portal
  diagnostics, moderate impact, high confidence. Repro: create a direct Task
  with input A, Continue it with input B, then open `Details` → `View trace` for
  the latest run. Run id `gazixnq742fyden2h2da` displayed `Sent to the worker
  Verify direct Task continuity.` (A), while
  `GET /api/spaces/{space}/tasks/3zozxlh4dydi4ayomcmq/runs` returned input B for
  that same run and correctly linked it to the first run. Expected (basis:
  TaskRun owns one turn/attempt and the panel identifies a particular run): the
  panel shows B. The misleading input can send diagnosis or audit review toward
  the wrong turn; execution itself used B correctly.
- **The kind worker-route probe rejects a correct 404** — verification tooling,
  moderate impact, high confidence. `./make kind smoke` printed all deployment
  assertions as passed, then failed with `BM_NOT404`. The public route returned
  404 both from the host and from an unlabelled pod. BusyBox `wget` emitted
  `wget: server returned error: HTTP/1.1 404 Not Found`, but
  `kindExpectPublicWorkerRoute404` searches only for the substring ` 404 `,
  which that output does not contain. Expected: a real 404 passes the assertion.
  Result: `kind up`/`kind smoke` can exit nonzero on a correct boundary, masking
  whether other deployment checks are healthy.
- **Ordinary Portal sign-in emits avoidable 403 console errors** — Portal
  diagnostics, low impact, high confidence. Ingress evidence associated the
  browser errors with `GET /api/admin/me` (403 for the non-admin user); first
  Space initialization also requested
  `/api/spaces/null/conversations?limit=100` and received 403. Expected: an
  ordinary successful sign-in does not issue a request with a null Space id or
  surface expected authorization as console errors. No visible workflow loss
  was observed.
- **Desktop tool cards over-compress their content at 1280×720** — Desktop
  presentation, low impact, high confidence. The successful Write card wrapped
  `Write` between `t` and `e` and clipped the argument to
  `DESKTOP-WORKTRE…`; `DESKTOP-DONE` also broke at the hyphen. Expected: a short
  one-call transcript remains scannable at this ordinary viewport. Full details
  remained available elsewhere, so this is presentation rather than data loss.

**Positive observations.** Desktop's approval, exact file result, session
reopening, relaunch continuity, and spend/cache detail all behaved coherently.
Portal conversation history and direct Task URLs survived navigation and reload.
The same Task accepted a second turn as a distinct linked TaskRun. Task details
made the sandbox, workspace checkpoint, model, tokens, and worker execution
legible without cluster access. The actual public/worker listener separation
held despite the automated probe's parsing bug.

**Scripted checks.** `./make e2e desktop` passed; `./make e2e desktop-ui` passed
7/7; after its documented rebuild precondition, `./make e2e desktop-launch`
passed. `./make test ./internal/server/... ./internal/infra/k8s/...` passed. The
first combined narrow test invocation misspelled the existing package as
`internal/infra/kubernetes`; server packages still passed, and the corrected
command above passed in full. The final `./make e2e kind` did not reach
Playwright; see the environment limit below.

**Not exercised / blocked.** macOS was locked, so the native Desktop window,
native file chooser, native focus/hit-testing, and exact packaged-window layout
could not be driven. Project selection used the real Wails `OpenProject`
binding, after which all product actions used rendered controls; the packaged
launch smoke only proved the app stayed up. Portal admin, Issues, Workflows,
Schedules, plugins, secrets, cancellation controls, and responsive/mobile
layouts were outside this bounded charter. After the successful manual kind
journeys, the host's Docker VM was saturated by three pre-existing clusters
plus this task's cluster (8 GiB Docker memory, host load 11.66, each kind node
roughly 80–157% CPU). The task cluster's CoreDNS health checks timed out,
controller-manager/scheduler restarted, both server replicas became NotReady,
and server DNS lookups for MySQL failed. Consequently two final
`./make e2e kind` attempts stopped in preflight/account setup. Those attempts
are environment-blocked, not passing evidence and not classified as a BuildMax
product defect.

**Cleanup.** Deleted only task-owned kind cluster
`buildmax-eph-2fd7a1ed`; `buildmaxdev`, `luminadev`, and another contributor's
ephemeral cluster were untouched. Stopped the Desktop/Portal drivers and the
worktree's leftover Vite/esbuild processes. Moved the disposable Desktop
project and copied real-model sandbox to Trash, so recovery remains possible;
no login code or token is retained in this report. Gitignored screenshots,
sanitized Desktop metadata, trace, and output remain under
`.artifacts/exploratory-20260913-{desktop,portal-server}/` for local triage.

**Follow-up.** Triage the four findings independently. The wrong TaskRun input
and false-negative kind probe are the actionable priorities; this exploration
does not itself authorize fixes or claim full surface readiness.
