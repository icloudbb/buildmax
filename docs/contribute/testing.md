# Testing

> **简体中文：** [阅读中文镜像](../zh-CN/contribute/testing.md)
> **Audience:** contributors and code-changing agents · **Status:** current

What to run after a change, what it needs, and where to look when it fails.
The reasoning behind this split — why end-to-end suites are a local feedback
loop rather than a pull-request gate — is in
[../design/end-to-end-testing.md](../design/end-to-end-testing.md).

For autonomous, observation-led review of a user journey, use
[Agent exploratory testing](exploratory-testing.md). It explains how to choose
branches while operating the product, judge findings, preserve evidence, and
turn discoveries into regressions. Use it alongside the required suites below;
it has no blanket PASS result and does not replace scripted verification.
Exploratory journeys may use configured real models, including paid inference,
within the task's scope and budget; record model and usage evidence as that
guide describes.

## The Short Version

```bash
./make test           # everything below the deployment: unit, integration, CLI, Desktop bridge
./make test ./internal/tool -run TestX   # one package or one test, same isolated home
./make test mysql     # the store scope, against a real MySQL you point it at
./make e2e cli        # just the CLI and TUI suite
./make e2e desktop    # just the Desktop bridge suite
./make e2e desktop-ui # desktop/frontend through `wails dev`'s browser bridge
./make e2e desktop-ui core # just the @smoke subset CI gates desktop changes on
./make e2e desktop-launch # launch the packaged app (after `build desktop`) and require it to stay up
./make e2e local      # Portal in a browser, against a Compose stack this command owns
./make e2e all        # cli, desktop, then local — the release-time matrix
./make kind up        # build the local cluster and verify a Kubernetes worker run
./make kind smoke     # rerun the deployment flow without rebuilding
./make e2e kind       # run the Portal browser suite against that cluster
```

`./make test` is the loop. The CLI and Desktop suites live inside it because
they run in seconds and need nothing but a temporary directory; a suite you
have to remember to run is one that stops being run.

Narrow it with `./make test`, not with a bare `go test`. Only the task runner
sets `BUILDMAX_HOME`, and a test that reaches the real `~/.buildmax` reads the
contributor's own sessions, settings, and credentials — so its result depends on
whose machine it runs on. `config.DataDir` panics rather than fall back, and a
package whose code reads those paths gives itself a `TestMain` calling
`testsupport.RunWithIsolatedHome`.

## Which Suite For What You Changed

| You changed | Run |
|---|---|
| Anything in `internal/`, `cmd/`, `tools/` | `./make test` |
| A row struct, a store method, or a query in `internal/infra/db` | `./make test mysql` — `./make test` alone skips every one of those tests |
| The agent loop, tools, permissions, sessions, the TUI | `./make test`, then `./make e2e cli` |
| Plugins, packaging, or the Marketplace routes | `./make test`, then `./make e2e cli` |
| The Desktop bridge, its events, approvals, or session history | `./make e2e desktop` |
| desktop/frontend's React app, or how it calls a bound Go method | `./make e2e desktop-ui` |
| The Wails config, the desktop asset embedding, or the app's packaging | `./make build desktop` — nothing else builds the packaged app, and `go build ./...` compiles the `!desktop` stub instead |
| A shared component in `gui/` | `./make check gui` |
| Portal presentation or a shared component | `./make check portal` or `./make check gui`, then `./make e2e local` for the fast browser loop |
| Portal behavior or a server route Portal calls | Use the local kind loop below; finish with `./make e2e kind` |
| Server handlers, services, authentication, conversations, or task dispatch | Use the local kind loop below; run `./make kind smoke`, plus `./make e2e kind` when the behavior is browser-visible |
| Worker, scheduler, storage, or the model gateway | `./make kind up`, then `./make kind smoke`; use `./make kind smoke managed` for the gateway path |
| Deployment manifests, Dockerfiles, ingress, or the Kubernetes worker path | `./make kind up`, then `./make e2e kind` |
| Documentation | `./make check docs` |

Start with the narrowest suite for fast feedback, then run the wider evidence
the changed boundary requires. A unit or Compose check is not a substitute for
kind when the claim depends on same-origin ingress, deployed configuration,
real backing services, or Kubernetes worker execution.

## The Kind Loop For Portal And Server

The local kind cluster is the preferred end-to-end environment for substantive
Portal and server work. It puts the built Portal and server images behind the
same ingress, uses real MySQL and MinIO, and executes TaskRuns in Kubernetes
worker Jobs. Those are product boundaries that component tests and the faster
Compose loop cannot all prove together.

```bash
./make kind up              # create or update the cluster, then verify a real worker run
./make kind reload portal   # rebuild and restart only Portal during the edit loop
./make kind reload server   # rebuild and restart only the server during the edit loop
./make kind smoke           # rerun the deterministic API-to-worker deployment flow
./make kind smoke managed   # also prove gateway inference without a worker credential
./make e2e kind             # run the Portal browser suite through the shared ingress
```

Run `kind up` first when no cluster exists. After a Portal-only edit, reload
Portal and run `e2e kind`. After a server edit, reload the server and run
`kind smoke`; add `e2e kind` when Portal consumes the changed behavior. Use
`kind fixtures` plus `kind login` when an ad hoc browser check needs populated
business views. Changes to manifests, images outside Portal/server, or the
worker Job path require `kind up`, because `kind reload` only targets the two
long-running Deployments.

The kind commands create or mutate local infrastructure and deliberately stay
outside automatic pull-request checks. That makes them a proportional choice,
not an optional one: when a Portal or server change makes an end-to-end claim,
the author is responsible for producing the local-cluster evidence.

## What Each Suite Needs, And How Long It Takes

| Suite | Needs | Normal | Owns |
|---|---|---|---|
| `./make e2e cli` | Go | under 60 s | a temporary `BUILDMAX_HOME`, a workspace, and a Marketplace server it starts in process |
| `./make e2e desktop` | Go | under 60 s | the same |
| `./make e2e desktop-ui` | Go, Node, Chromium | under 60 s | a `wails dev` process and a fresh, discarded `BUILDMAX_HOME` |
| `./make e2e local` | Docker, Node, Chromium | under 10 min | a Compose stack it starts and stops |
| `./make e2e compose` | a Compose stack already running | under 2 min | nothing — it is a guest |
| `./make e2e kind` | a kind cluster already running | under 2 min | nothing — it is a guest |
| `./make compose smoke` | Docker | under 5 min | a Compose stack it leaves running |
| `./make compose upgrade-drill` | Docker, the source release's image | under 10 min | a Compose project it starts and removes, and the candidate image it builds |
| `./make kind up` | Docker, kubectl | under 20 min | a cluster it leaves running |

No suite needs a provider API key. Every one of them answers the model from
`internal/testsupport/mockllm`, which replays a committed scenario.

`./make e2e desktop-ui` is a fixed, scripted check. It runs in two tiers: the
full command runs every case; `./make e2e desktop-ui core` runs only the cases
tagged `@smoke`, which is what the Desktop UI workflow gates every desktop change
on (a non-required check, on macOS, since `wails dev` shows a native window). Tag
a new case `@smoke` only when it is worth running on every desktop PR; leave the
rest for the full run and the release matrix. For poking at desktop/frontend's UI
ad hoc (click through a flow, screenshot a view, read what a bound Go method
returns, before you know what to assert), start `./make run desktop-dev` and
drive it with
[`.buildmax/skills/drive-desktop/`](../../.buildmax/skills/drive-desktop/SKILL.md)
instead.

## The Store Scope

Every test in `internal/infra/db` skips itself when `BUILDMAX_TEST_DSN` is
unset, so a green `./make test` says nothing about schema, query, transaction,
or MySQL-specific behavior. `./make test mysql` is the scope that does:

```bash
docker run --rm -d --name buildmax-test-mysql \
  -e MYSQL_ROOT_PASSWORD=buildmax -e MYSQL_DATABASE=buildmax \
  -p 3306:3306 mysql:8.0
export BUILDMAX_TEST_DSN='root:buildmax@tcp(127.0.0.1:3306)/buildmax'
./make test mysql
./make test mysql -run TestCreateSpace    # `go test` flags pass through
```

The account needs `CREATE DATABASE`: the scope runs on a uniquely named
database it creates and drops, so it never writes to the one your DSN names.
It refuses to run without a DSN rather than skipping, and fails if a test in
the scope skips for the DSN's absence anyway — a gate that can go green by
testing nothing is the problem it exists to solve.

It never starts Docker for you. Point it at a server you already run; a test
command that starts containers as a side effect is one that mutates the machine.

CI runs this same command against a pinned `mysql:8.0` service container on
every pull request. `./make check ci` runs it too when `BUILDMAX_TEST_DSN` is
set, and says it did not when the variable is absent. The design record is
[../design/verification-program.md](../design/verification-program.md) §4.

`./make agent-smoke` is the exception, and it is not a test: it drives the
agent's tools with a real model, needs a key, and reports a PASS/FAIL table the
model wrote about itself. Read its output; its exit code says only that the
process finished.

`./make cache-qualify` is the second exception, for the same reason and a
sharper one. Every cache test in the tree runs against a fake upstream, which
proves what BuildMax sends and nothing about what a provider does with it — and
a request can be perfectly shaped while the provider declines to cache it, for a
minimum prefix length, an unsupported model, or an expired retention window.
The suite runs the scenarios
[prompt-cache-control.md](../design/prompt-cache-control.md) gates on against a
real provider named by `BUILDMAX_CACHE_QUALIFY_*`, and no provider or gateway is
described as cache-capable until it passes. Unset, it skips the way the store
tests do under a plain `./make test` — with no scope of its own that refuses to,
because unlike MySQL there is no free local stand-in for a paid provider.

`./make eval` is the third, and it measures something the suites deliberately do
not. It builds the CLI and evaluates the CLI tasks in `evaluation/suite/` as a
black box by default; `--surface worker` builds the worker and selects its tasks,
and `--surface all` runs both surfaces. Each uses a real model, repeated trials,
graders that read the final workspace and the run's trace, and a report of pass
rate with its uncertainty.
That is agent quality, not boundary verification — a suite above proves a
behavior is wired, and evaluation asks how reliably a model drives it. It needs
a key and spends tokens. Everything it can check without one — task validity,
oracles, graders, and the adapter — runs in `./make test` instead, so a task
that measures nothing is caught before it costs anything. See
[design/evaluation-system.md](../design/evaluation-system.md) for why it is
shaped this way, and [evaluation/README.md](../../evaluation/README.md) for how
to run it and what a task and a bundle hold.

`./make eval harbor` reports an external coordinate rather than producing one.
Harbor runs Terminal-Bench 4.0 and its verifier decides every outcome; the
import reads the finished job and files it in the same contract, so an external
result and a local one carry the same subject tuple, the same failure taxonomy,
and the same pass rate with its uncertainty. It measures rather than gates — a
task the subject did not solve is a score.

`./make eval harbor run` starts that run: it assembles the Harbor command from
the pins, launches it, and imports the job. It needs Docker and a model API key
and it spends money, so it is as deliberate as the local suite. `./make eval
harbor --job <dir>` is the import on its own, for a job someone else ran; that
half builds nothing and calls no model. `./make doctor harbor` reports what a
run needs and `./make setup harbor` installs it; see
[evaluation/harbor/README.md](../../evaluation/harbor/README.md).

The oracle smoke and a one-task canary have run through that path end to end.
That verifies it for one task and no further; there is no Terminal-Bench score.
Widening it is the open work, and the first canary found a product bug outside
evaluation entirely, so expect the next one to find more.

If a prerequisite is missing, the suite says which one before it starts. The two
that catch people out:

```bash
npm --prefix portal ci                                  # Portal test dependencies
npm --prefix portal exec -- playwright install chromium # the browser itself
```

## Attached Or Owned

A Portal suite either attaches to a deployment or owns one, and it says which
before it starts.

- **Owned** (`./make e2e local`) starts a Compose stack under a project name
  and ports this run picked for itself, tests it, and takes it down again,
  volumes included. Because the name and ports are chosen fresh every run, it
  never has to guess whether something already answering on the usual port is
  a contributor's persistent stack or a leftover of its own — several runs
  (different worktrees, different agents, a human's `./make compose up`
  alongside them) can each own a stack of their own at the same time.
- **Attached** (`./make e2e compose`, `./make e2e kind`) is a guest. It uses the
  fixed diagnostic account `deployment-smoke@buildmax.local`, creates only
  uniquely tagged resources, and prints what it left behind — most of them have
  no delete route, so the line naming them is the cleanup instruction.

`./make e2e kind` first checks that the cluster's MySQL is Ready, and refuses
with a `kind reload` instruction otherwise, so an unhealthy shared database
fails fast instead of surfacing as a product bug part-way through the suite.

It also checks that the deployment matches the working tree. `kind up` and
`kind reload` stamp the commit they built onto the `buildmax-server` and
`buildmax-portal` Deployments (`buildmax.dev/source-commit`); before creating
any test data, `e2e kind` reads it back and refuses — again with a `kind
reload` instruction — when it does not match the current checkout. The images
use a mutable `:local` tag, so without this a green result against a stale
image reads as passing on code it never ran. A dirty tree at the same commit is
only a warning: a short commit cannot prove the running image carries your
uncommitted changes, so reload if you have edited code since. The commit is
also recorded in the run's `run.txt` as `source:`.

One spec is a deliberate exception to both modes. `resource-states.spec.ts`
intercepts the deployment's own responses with Playwright's `page.route` to
drive Portal into failure states — a 5xx, a refresh that fails after a good
load, a role lookup that errors — that a healthy backend will not produce on
demand. It tests how Portal *presents* a failing dependency, not the
deployment, so it neither reads real data nor owns a stack. Fault injection
stays in that one spec; every other Portal suite reports the real deployment
truthfully.

### Running A Second Deployment Alongside The First

`./make kind up` and `./make compose up` keep the fixed name and ports every
doc assumes, because a contributor started one by hand and expects a
predictable address. A second one, run deliberately alongside the first,
needs to be told not to collide with it:

```bash
# A second Compose stack
BUILDMAX_COMPOSE_PROJECT=buildmax-2 BUILDMAX_SERVER_PORT=15678 BUILDMAX_PORTAL_PORT=18080 \
  ./make compose up

# A second kind cluster
BUILDMAX_KIND_CLUSTER=buildmaxdev2 BUILDMAX_KIND_PORTAL_PORT=18080 BUILDMAX_KIND_TLS_PORT=18443 \
  ./make kind up
```

Pass the same variables to every later command against that stack (`compose
status`/`logs`/`down`, `kind status`/`logs`/`down`, `e2e compose`/`e2e kind`)
— nothing persists the mapping between a name and its ports, so the
invocation is what remembers it.

### An Ephemeral Kind Cluster For One Task

Handing every later command the same variables is fine for a person, but a task
running commands as separate processes cannot keep an environment variable
between them. For that, let the task own a cluster instead:

```bash
BUILDMAX_KIND_EPHEMERAL=1 ./make kind up   # unique name, free ports, once
./make e2e kind                            # reuses it, no variables needed
./make kind down                           # deletes the cluster and the record
```

The first `up` picks a name nothing else uses (`buildmax-eph-<hex>`) and two
free host ports, and records them in `.local/kind-ephemeral.env`. Every later
`kind` and `e2e kind` in this worktree reads that record, so none of them need
the flag or the ports. `kind down` deletes the cluster and removes the record,
after which commands here target the resident `buildmaxdev` again. An explicit
`BUILDMAX_KIND_CLUSTER` still overrides the record. This is how several tasks
can each hold their own cluster without colliding, and without disturbing the
`buildmaxdev` cluster kept for manual use — create one only when the change
needs the kind boundary, and take it down when done.

`./make e2e local` needs none of this: it already picks a fresh project and
ports for itself on every run, which is what makes it safe to run alongside
either of the above without checking what else is up first.

## When A Suite Fails

Everything a browser suite produces goes to one place, cleared at the start of
every run:

```text
.artifacts/e2e/portal/
├── run.txt      the deployment, the run id, and the command that reproduces it
└── results/     Playwright traces, screenshots, and error context per failed test
```

Open a trace with `npx playwright show-trace <path>`. For the Go suites the
failure is the test output: each one prints what it expected, what it saw, and —
for the terminal and Desktop suites — the whole screen or event stream it had
at that point.

Read the artifact before changing code. A failing end-to-end test names a
boundary; guessing at the boundary from the test name is how a real defect
becomes a weakened assertion.

## What CI Runs

| Trigger | What runs |
|---|---|
| Every pull request | The required `ci.yml` jobs: Go, frontend, open-source policy, and deployment smoke health |
| Relevant pull request | Windows for Go/task-runner changes, release configuration validation, or a Portal image build |
| Merge to `main` | Required CI, Windows, CodeQL, release snapshot, and path-scoped deployment smoke |
| Schedule | Daily deployment smoke and weekly CodeQL analysis |
| Manual dispatch | The selected workflow, for release preparation or a suspected environment regression |

End-to-end verification is deliberately not a pull-request gate. A post-merge
failure is triaged by the author of the merge that broke it, and that merge is
reverted if it is not fixed within one working day. A test that fails
intermittently is quarantined the same day rather than retried.

What holds anyone to that is the **Deployment smoke health** job: it does not
verify your pull request, it refuses to add to a `main` whose last deployment
smoke failed, and it names the run and the commit that left it that way. A pull
request whose purpose is to repair the suite carries the
`deployment-smoke-fix` label and is let through.

Every post-merge run reports each suite as passed, failed, cancelled, or skipped
by policy with the reason. A skipped suite is never evidence that a journey
passed.

## Before A Release

```bash
./make check ci   # required PR suite plus conditional release/Windows checks
./make e2e all    # cli, desktop, then a browser run against a stack it owns
```

`./make e2e all` deliberately leaves out kind: that suite needs a cluster, and a
release check that quietly builds one is a surprise. For substantive Portal or
server work, and for every deployment-shaped change, fill that omission
explicitly with `./make kind up` followed by `./make e2e kind`.

## Frontend Component Tests

`gui/` and `desktop/frontend` run their component tests under vitest with a
jsdom document, so a test asserts what a viewer can see and which key does what
rather than what a function returned. `gui` is where a shared component's own
behaviour is proved once, for both surfaces; a surface's test covers only its
own wiring to that component. Portal keeps browser-level assertions in
Playwright, where a real engine is the point.

```bash
./make check gui               # build the package, type-check, run its tests
npm --prefix gui test          # the tests alone, while iterating
```

`npm test` in `gui` type-checks before it runs: `tsconfig.json` excludes test
files so no `.d.ts` for one reaches `dist/`, and `tsconfig.test.json` puts them
back for the check.

## Adding A Test

Put it at the lowest level that can prove the claim. A display decision belongs
in a frontend unit test, a service contract in a Go test, a handler rule in an
HTTP test. Reach for an end-to-end suite only when the outcome depends on
several of those cooperating — a real binary, a real deployment, a browser — and
say in the test what that boundary is. The design record's §6 lists the paths
that qualify and, for the deployment ones, the exact fact no handler test can
reach.
