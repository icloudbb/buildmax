# Local Kubernetes Deployment

> **简体中文：** [阅读中文镜像](../zh-CN/deploy/local-kind.md)
> **Audience:** contributors and operators · **Status:** beta
>
> Use this path for Kubernetes worker Jobs, RBAC, Ingress, MinIO, manifests,
> and substantive Portal or server changes whose behavior crosses the browser,
> API, ingress, backing services, or worker execution. The faster
> [Compose smoke](compose.md) remains useful for the inner loop, but does not
> prove those Kubernetes boundaries.

## Requirements

- Docker with at least 6 GB available
- kubectl

kind itself is not a prerequisite: `tools/mk` pins it and runs it through
`go run`, so every cluster is created, inspected, and deleted by the same
version. The command does not install system packages or start background
port-forwards. It always addresses the selected cluster through an explicit
kubectl context.

## Start And Verify

```bash
./make kind up
```

This creates the `buildmaxdev` cluster, then:

1. installs ingress-nginx, MySQL, and MinIO — the server and `mc` images come
   from [SILO](https://silo.pgsty.com), the community MinIO fork, since MinIO
   stopped publishing images
2. creates the `bmstore` bucket and widens the MySQL dev grant, each from an
   in-cluster Job
3. builds and loads the server, Portal, and deterministic mock-model images
4. generates an ephemeral local Secret and applies the BuildMax manifests
5. waits for every Deployment to become ready
6. creates a real TaskRun, executes it in a Kubernetes worker Job, and verifies
   its artifact through the API

The cluster config and the dependency manifests it applies live in
`deployment/kind/`; the orchestration is `tools/mk/kind.go`. They are
development-only and are not part of a real deployment.

Open <http://localhost:8080>. Portal and API share that origin, so no
`/etc/hosts` entries or CORS pairing are needed. The command prints a fresh
single-use code for `deployment-smoke@buildmax.local` after verification.

That origin is also the **Server URL** for the Desktop app and `buildmax login`.
The default both offer, `http://localhost:5678`, is the port a server started on
this machine listens on; nothing publishes it here, because the ingress is the
only way in.

That code is spent the first time it is used, and is printed once. When it is
gone, `./make kind info` issues another one — it does not, and cannot, show the
old one.

## Daily Commands

```bash
./make kind smoke   # rerun the end-to-end assertions without rebuilding
./make kind smoke managed  # the same, with task runs reaching models through the gateway
./make kind seed    # put the models in .local/settings.yaml into the cluster's catalog
./make kind fixtures # seed idempotent business data for automated testing
./make kind use-model "Claude Sonnet 5"  # run the cluster's own inference on a seeded model
./make kind mock    # switch the cluster's own inference back to the free mock
./make kind reload  # rebuild and load local images, then restart the deployments
./make kind reload server  # the same, for just the server (or portal)
./make kind info    # endpoints, plus a fresh login code for the smoke account
./make kind login   # the same code as JSON on stdout, for a script instead of a human
./make kind forward # forward the in-cluster MySQL and MinIO to 127.0.0.1
./make kind status  # read-only summary of the cluster, ingress, and workloads
./make kind logs    # pods, jobs, events, server, Portal, and worker logs
./make kind logs server  # just the server's logs (or portal, worker, mysql, minio, ingress)
./make kind down    # delete the selected cluster
```

`smoke managed` swaps the `buildmax-config` ConfigMap for
`deployment/smoke/server.kind.managed.yaml`, restarts the server, and reruns the
same assertions with task-run inference going through the gateway. It proves
what the default run cannot: a worker Job completes a real task holding no
provider credential, and its run token reaches the pod through the Job spec.
The cluster stays in managed mode afterwards — rerun `./make kind up` to return
it to direct.

`info` prints the cluster, the Portal URL and its health, the MinIO credentials,
and issues a single-use login code — for `deployment-smoke@buildmax.local`
by default, or for the account named as `./make kind info alice@example.com`.
`login` skips the human-readable banner and prints `{"email","code","portal_url"}`
JSON instead, creating the account first if it does not exist yet; the
`drive-portal` skill (`.buildmax/skills/drive-portal/`) uses it to sign in a
headless browser without anyone copying a code by hand.

`fixtures` fills the running deployment with **business data**; `seed` fills the
**model catalog**. Start with `./make kind fixtures`, then sign in using
`./make kind login alice@buildmax.local`. Select **BuildMax QA** for the main
scenarios and **BuildMax QA Pagination** for long lists.

| Area | Fixture coverage |
|---|---|
| Accounts and isolation | Alice and Bob retain their populated personal Spaces; Carol and Dave have empty personal Spaces; Alice holds System Administrator authority so the admin surfaces are reachable |
| Collaboration | Alice owns BuildMax QA, Bob is admin, Carol is member, Dave has a pending invitation; all emails end in `@buildmax.local` |
| Issues | All three statuses; unassigned, person, Agent, and Workflow assignment; parent with two children and mixed progress; Markdown, Unicode, empty descriptions, comment threads |
| Agents and Workflows | Personal Docs Writer/Release Notes; shared QA Writer/QA Reviewer; two-step draft, published, and archived Workflows, with lifecycle revision history |
| Files | Five files under `fixtures/`: nested Markdown, CSV, JSON, Unicode filename, and empty text |
| Artifacts | Synthetic text, HTML sandbox preview, and binary download fixtures |
| Space settings | Nonempty Agent instructions and active/disabled Secrets containing explicitly fake values; account webhook keys; API changes also populate audit events |
| Plugins and Marketplace | The three `sample-plugins/` published to the deployment catalog, one activated in BuildMax QA and the rest left available to activate |
| Schedules | Recurring agent schedules with varied cron expressions and timezones, some paused (in BuildMax QA Pagination) |
| Pagination and volume | Separate Space with 105 Issues (35 per status) including a 25-comment thread, plus long lists to page and scroll: 12 agents, 9 workflows across all three statuses, 60 artifacts (past the "Load more" threshold), 8 extra secrets, and 8 schedules |
| Admin scale | 60 synthetic accounts (roughly one in eight disabled) so the Accounts page spans more than one page and its status filter has a cohort; each also gets a personal Space |
| Execution (`--runs`) | Conversation transcript, a Task with Continue and Retry, Issue Agent result, two-step Workflow result, worker traces and workspace checkpoints |

```bash
./make kind fixtures --runs
```

`--runs` executes Kubernetes workers and requires the reference free mock
configuration. It refuses model-selection overrides and customized server
configuration; it does not switch a deployment's model. If you previously used
`kind use-model`, run `./make kind mock` first. It does not call a paid provider.
A failed or canceled execution is reported rather than replaced with a fake
success. Fix the underlying failure and retry that Task before seeding again.

Reruns match resource names/titles, artifact filenames, file paths, and the
fixture conversation's first message. Lists are paginated fully and missing
comments are matched individually by body, so an interrupted comment seed can
resume. Existing Issue statuses, descriptions, file contents, Agent definitions,
and member roles are preserved. Fixture Issue assignments, Workflow lifecycle
states, Secret states, schedule paused/enabled state, and account disabled state
are reconciled; empty Space instructions are filled. Webhook keys and bulk
accounts are matched by name and email so a rerun adds only what is missing.
An already-published plugin version and an existing activation are left as they
are rather than republished.
Do not rename fixture resources if you want them reused. These are named test
data in a development cluster, not a concurrent seed transaction: run one
fixture command at a time. A lost response to a create without a server
idempotency key is recovered by looking up its stable fixture identity on rerun.

This is populated test data, not proof of every product feature. It does not
seed managed-model grants (those need an explicit catalog), send webhooks, or
manufacture running/failed/canceled records. Use `kind smoke` for
worker failure-boundary and cancellation checks, `kind smoke managed` for the
managed gateway, and `e2e kind` for browser journeys. Artifact public sharing
remains an action to test from the seeded artifact rather than creating public
links during initialization.

`status` changes nothing. It prints the selected cluster and context, probes
<http://localhost:8080/healthz> through the ingress, and lists nodes plus the
Deployments, Jobs, and Pods in `ingress-nginx`, `db`, `storage`, and
`buildmax`. Use it to tell a missing cluster from an unhealthy one before
reading the much longer `kind logs` output.

Use an isolated cluster name when another contributor or task owns the default:

```bash
BUILDMAX_KIND_CLUSTER=buildmax-my-change \
BUILDMAX_KIND_PORTAL_PORT=18080 \
BUILDMAX_KIND_TLS_PORT=18443 \
  ./make kind up
```

The default cluster uses host ports `8080` and `8443`. Set
`BUILDMAX_KIND_PORTAL_PORT` and `BUILDMAX_KIND_TLS_PORT` with the cluster name to
move them; pass the same three values to every later `kind` or `e2e kind`
command for that cluster. The Compose stack publishes Portal on `8080` by
default too, so either move the kind ports as above or move Compose:

```bash
BUILDMAX_PORTAL_PORT=8081 ./make compose up
```

Nothing else has to follow: `cors_origin` and the Portal's API base are both
derived from the ports in `deployment/compose/.env`.

## Read The Data A Run Wrote

MySQL and MinIO have ClusterIP Services and the cluster publishes only the
ingress ports, so neither is reachable from this machine on its own.

```bash
./make kind forward     # publishes both to 127.0.0.1 until you stop it
```

It forwards MySQL to `3306` and MinIO to `9000` (API) and `9001` (console), and
prints how to connect to each. Every line the forwards write is tagged with the
target it came from. A target whose host port is already taken on this machine
is skipped with a warning — a local MySQL on `3306` costs you that forward, not
MinIO's — and the warning names the kubectl command that forwards it to a port
of your choosing.

While it runs, connect to MySQL with any client — `mysql -h 127.0.0.1 -P 3306
-ubuildmax -pbuildmax buildmax`, or the DSN
`buildmax:buildmax@tcp(127.0.0.1:3306)/buildmax`. Those are the development
credentials in `deployment/kind/mysql.yaml`; the database is `emptyDir` and
goes away with the cluster. The account may use any schema, not just `buildmax`,
so `database.name` in a local `server.yaml` can say whatever you like — the
server creates the schema it is pointed at on first start. The same DSN in
`BUILDMAX_TEST_DSN` is what runs the store integration tests under
`internal/infra/db` against a real MySQL.

MinIO's console is <http://127.0.0.1:9001> with `minio` / `minio123`, and the
run artifacts are in the `bmstore` bucket.

For a single query, skip the forward and use kubectl directly:

```bash
kubectl --context kind-buildmaxdev -n db exec deployment/mysql -- \
  mysql -ubuildmax -pbuildmax buildmax -e "select * from task_run\G"
```

## Smoke Versus Real Providers

The local command deliberately overlays `deployment/smoke/server.kind.yaml`
and an in-cluster OpenAI-compatible mock. This keeps contribution checks
deterministic and ensures CI never needs a provider credential.

For a private deployment, use `deployment/buildmax-deploy.yaml` as the readable
baseline and configure a real model endpoint. `./make setup local` writes
`.local/buildmax-secret.yaml` from `deployment/buildmax-secret.example.yaml`;
fill it in and `kubectl apply -f` it yourself. `./make kind up` never reads that
file — it generates its own throwaway Secret — so do not use the generated smoke
Secret or the mock model outside local verification.

### Drive The Cluster With Your Own Models

`./make kind seed` puts every provider model in `.local/settings.yaml` into the
cluster's catalog. It exists so the CLI and Desktop can exercise the managed
transport — `transport: buildmax` — against real inference without a hosted
deployment to point at.

```bash
./make kind up      # the stack, still answering from the mock
./make kind seed    # your models in its catalog
```

The command adds each model with `buildmax-server model add` and stops there: a
catalog row is callable as soon as it exists, so nothing needs restarting and no
configuration changes. Sign in with `buildmax login` against
<http://localhost:8080>; a stored login puts the client in managed mode, where
`buildmax models` reads the deployment catalog and local `settings.yaml` model
entries are not used.

A model is named by the `name` it was added under, which is its display name in
`.local/settings.yaml`, or its model id when it has none. After login,
`buildmax models` shows the name to use.

`seed` deliberately leaves the cluster's own inference alone.
`conversation.model` and the worker keep answering from the in-cluster mock, so
Portal conversations and `./make kind smoke` stay deterministic and cost
nothing. A rerun is safe: a model whose name is already in the catalog keeps
its row and its ID. Changing a seeded model's endpoint or credential means
renaming it in `.local/settings.yaml`, or rebuilding the cluster — `add` does not
update a row.

To make the cluster's own Portal conversations and task runs answer from a
seeded model, switch them over explicitly:

```bash
./make kind use-model "Claude Sonnet 5"   # conversations + task runs via the gateway
./make kind mock                          # back to the free in-cluster mock
```

`use-model` sets `BUILDMAX_WORKER_LLM_TRANSPORT`, `BUILDMAX_LLM_DEFAULT_MODEL`,
and `BUILDMAX_CONVERSATION_MODEL_TARGET` on the server Deployment and restarts
it; the committed ConfigMap is untouched, so `mock` — which clears them — puts
the cluster back exactly. It calls the real provider and spends its quota, which
is why it is a separate step from `seed` rather than part of it.

A real credential in `.local/settings.yaml` reaches the cluster's MySQL in plain
text. That database is thrown away with the cluster, and this path is for local
verification only.

### A real model without a provider key

Between the mock and a hosted provider there is a third option: point the
deployment at an Ollama daemon **on this machine**. Real inference, real tool
calls, no credential, no bill — and the gateway, the `llm_call` ledger, and
quota all run for real.

Do not put the daemon in the cluster. A pod cannot reach the host's GPU, so
inference falls back to the CPU of the VM the cluster runs in. Leave it on the
host and give the deployment an address that reaches it — under Docker Desktop
that is `host.docker.internal`, which resolves inside pods and forwards even to
a daemon bound to the host's loopback. `kind seed` rewrites a loopback address
to that name for you; by hand it is:

```bash
# a catalog target; it is callable by name as soon as the row exists
kubectl --context kind-buildmaxdev -n buildmax exec deployment/buildmax-server -- \
  buildmax-server model add --name "Host Ollama" --provider ollama \
      --api-url http://host.docker.internal:11434 \
      --model qwen3:8b --context-window 32000
```

`deployment/buildmax-deploy.yaml` carries the same thing for `conversation.model`
as a commented block. On a Linux host the address is the Docker bridge gateway
(`docker network inspect kind`) and the daemon needs `OLLAMA_HOST=0.0.0.0`.

## Why Compose Still Exists

Compose and kind verify the same user-visible flow but different execution
contracts:

| Path | Worker | Storage | Best for |
|---|---|---|---|
| Compose | local process in the server container | shared local filesystem | Fast Portal and API iteration when deployment boundaries are unchanged |
| kind | one Kubernetes Job per TaskRun | MinIO shared by server and workers | Substantive Portal/server integration, Jobs, RBAC, Ingress, object storage, and manifests |

Keeping both makes the inner loop quick without mistaking it for complete
deployment evidence. Finish in kind whenever the claim crosses the browser,
API, ingress, backing services, or worker execution.
