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

This creates the `buildmaxdev` cluster with [Cilium](https://cilium.io) as its
network plugin instead of kind's default kindnet, then:

1. installs ingress-nginx, MySQL, and MinIO — the server and `mc` images come
   from [SILO](https://silo.pgsty.com), the community MinIO fork, since MinIO
   stopped publishing images
2. creates the `bmstore` bucket and widens the MySQL dev grant, each from an
   in-cluster Job
3. builds and loads the server, Portal, deterministic mock-model, and mock
   OIDC provider images
4. generates an ephemeral local Secret and applies the BuildMax manifests
5. waits for every Deployment to become ready
6. creates a real TaskRun, executes it in a Kubernetes worker Job, and verifies
   its artifact through the API
7. signs in through single sign-on next to the login code it used, and checks a
   domain outside the allow list is refused (see [Single Sign-On](#single-sign-on))

Cilium enforces NetworkPolicy, including the worker API boundary, in the
kernel. kindnet's userspace policy engine degraded on a long-lived cluster: new
pods went unprotected, and DNS and API calls from others timed out until it was
restarted. The manifest is vendored in `deployment/kind/cilium.yaml`, with the
command that renders it. A cluster created before this change still runs
kindnet; `kind up` says so, and `./make kind down` followed by `./make kind up`
recreates it with Cilium. `./make kind fixtures` restores the QA data.

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
./make kind drill rotation # rotate every credential; ephemeral clusters only
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
./make kind drill restore  # rehearse a paired backup, wipe, and restore (ephemeral clusters only)
./make kind down    # delete the selected cluster
```

`drill restore` is destructive and separate from `smoke`: it deletes the `db`,
`storage`, and `buildmax` namespaces after backing them up, so it refuses to run
anywhere but a cluster created with `BUILDMAX_KIND_EPHEMERAL=1`. What it proves
and how the procedure applies to a real deployment are in
[backup-restore.md](backup-restore.md).

`smoke managed` swaps the `buildmax-config` ConfigMap for
`deployment/smoke/server.kind.managed.yaml`, restarts the server, and reruns the
same assertions with task-run inference going through the gateway. It proves
what the default run cannot: a worker Job completes a real task holding no
provider credential, and its run token reaches the pod through the Job spec.
The cluster stays in managed mode afterwards — rerun `./make kind up` to return
it to direct.

`drill rotation` rehearses the
[credential rotation runbook](credential-rotation.md): it rotates the JWT
secret, the database password, the storage key, a managed model's key, and the
KEK through Secret patches and server rollouts, asserts that each old
credential is refused while sessions, stored artifacts, and new runs survive,
and ends with a table of roll times and measured disruption. It holds one run
across the JWT rotation on purpose, so that run ends `FAILED`. Because it leaves
every credential replaced, it refuses to run unless this worktree created its
cluster with `BUILDMAX_KIND_EPHEMERAL=1 ./make kind up`; remove the cluster with
`./make kind down` afterwards.

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
| Governance | Frank joined BuildMax QA, scheduled a Workflow, and was removed, so his schedule pauses as `creator_not_member` at the next tick; Erin is disabled and was the only owner of BuildMax QA Archive (Bob is a member), which awaits owner recovery, and her schedule there is paused as `creator_disabled`; Carol owns an enabled schedule, so her deactivation impact is not empty |
| Issues | All three statuses; unassigned, person, Agent, and Workflow assignment; parent with two children and mixed progress; Markdown, Unicode, empty descriptions, comment threads |
| Agents and Workflows | Personal Docs Writer/Release Notes; shared QA Writer/QA Reviewer; two-step draft, published, and archived Workflows, with lifecycle revision history; QA Release Readiness, a published fan-out/fan-in graph with a typed `input_schema`, `max_parallel_nodes`, input and node-output bindings, per-node `issue_access`, and a result selector; QA Triage Classifier, with an `output_schema` node and a required-Issue node; QA Release Engineer at revision 3 with a plugin, sandbox tiers, and Secret consumption; QA Blocked Agent, which requires the disabled Secret |
| Human requests and policy | Published in BuildMax QA: QA Release Sign-off (Agent draft, `human_input` approval with a response schema and a one-week expiry, Agent publish bound to the answer); QA Clarify Scope (an Agent step for `AskUser` questions); QA Expiring Approval (a request that expires after a minute); QA Run Deadline (a one-minute run `timeout_seconds`); QA Flaky Gate (a `max_attempts: 2` gate on the blocked Agent beside a sibling and a join); QA Slow Step (a one-minute attempt timeout) |
| Files | Five files under `fixtures/`: nested Markdown, CSV, JSON, Unicode filename, and empty text |
| Artifacts | Synthetic text, HTML sandbox preview, and binary download fixtures; a live public share on the HTML artifact (its URL is printed when created) and a revoked one on the report |
| Space settings | Nonempty Agent instructions and active/disabled Secrets containing explicitly fake values; `registries` sandbox network default and curated plugin activation in BuildMax QA; account webhook keys; API changes also populate audit events |
| Plugins and Marketplace | The three `sample-plugins/` published to the deployment catalog, one activated in BuildMax QA and the rest left available to activate |
| Schedules | Recurring agent schedules with varied cron expressions and timezones, some paused (in BuildMax QA Pagination); a weekly Workflow schedule with typed input and a paused one without (in BuildMax QA); a schedule of QA Retired Check, archived after it was scheduled, which fails to start every minute until it pauses as `consecutive_failures` a few minutes after seeding |
| Pagination and volume | Separate Space with 105 Issues (35 per status) including a 25-comment thread, plus long lists to page and scroll: 12 agents, 9 workflows across all three statuses, 60 artifacts (past the "Load more" threshold), 8 extra secrets, and 8 schedules |
| Admin scale | 60 synthetic accounts (roughly one in eight disabled) so the Accounts page spans more than one page and its status filter has a cohort; each also gets a personal Space |
| Execution (`--runs`) | Conversation transcript, a Task with Continue and Retry, Issue Agent result, two-step Workflow result, worker traces and workspace checkpoints; a succeeded and a canceled QA Release Readiness run; a FAILED QA Blocked Agent Task; a CANCELED conversation Task; a webhook-channel conversation in Alice's personal Space; QA Release Sign-off runs whose approval was answered, declined, is still pending, and was canceled with the whole run; QA Clarify Scope runs with an answered and a pending question; a direct QA Writer Task awaiting an answer and one answered through Continue; expired-request, run-deadline, retried-gate, and timed-out Workflow runs, all `failed`; one of Carol's Tasks left `RUNNING` for about ten minutes, for the admin runtime view and her deactivation impact |

```bash
./make kind fixtures --runs
```

`--runs` executes Kubernetes workers and requires the reference free mock
configuration. It refuses model-selection overrides and customized server
configuration; it does not switch a deployment's model. If you previously used
`kind use-model`, run `./make kind mock` first. It does not call a paid provider.
An execution that fails or is canceled when it should succeed is reported
rather than replaced with a fake success. Fix the underlying failure and retry that Task before seeding again.

Reruns match resource names/titles, artifact filenames, file paths, and the
fixture conversation's first message. Lists are paginated fully and missing
comments are matched individually by body, so an interrupted comment seed can
resume. Existing Issue statuses, descriptions, file contents, Agent definitions,
and member roles are preserved, except that QA Release Engineer is revised up
to its third revision when it has fewer. Fixture Issue assignments, Workflow
lifecycle states, Secret states, schedule paused/enabled state, and account
disabled state are reconciled; empty Space instructions and sandbox defaults
are filled, and an open plugin curation is set to curated. A public share is
recreated once the previous one expires. Execution outcomes are matched by
Workflow run status and by Task input; Carol's long-running Task is started
again only when none is still active. Governance fixtures are matched by
schedule name and by Erin's disabled state: once Frank's schedule exists he is
not re-invited, and once Erin is disabled her Space is left as it is. Webhook keys and bulk accounts are
matched by name and email so a rerun adds only what is missing. An
already-published plugin version and an existing activation are left as they
are rather than republished.
Do not rename fixture resources if you want them reused. These are named test
data in a development cluster, not a concurrent seed transaction: run one
fixture command at a time. A lost response to a create without a server
idempotency key is recovered by looking up its stable fixture identity on rerun.

The failed and canceled records come from real runs, not rewritten rows: the
blocked Agent's required grant is refused when its run starts, and the
cancellations hold the mock's replies the way `kind smoke` does, so a run is
mid-turn when it is canceled. The questions, the timed-out step, and Carol's
running Task come from arming the mock with a one-shot `AskUser` or `Bash`
call, as `kind smoke` does. Each armed call is reserved for a model request
carrying that step's own instruction or Task input, so a schedule firing or
another run calling the mock at the same moment cannot take it; `kind up`
builds the mock image that honours the reservation. Publication allows no
timeout below a minute, so `--runs` takes
several minutes longer than before. The webhook conversation is sent with a key that
is deleted again afterwards.

This is populated test data, not proof of every product feature. It does not
seed managed-model grants (those need an explicit catalog), chat-platform
conversations or channel links (only the channel gateway creates those, from a
real bot), Remote Control sessions (they need a connected CLI), successful
schedule fires, or a quota refusal. Use `kind smoke` for worker failure-boundary and cancellation checks,
`kind smoke managed` for the managed gateway, and `e2e kind` for browser
journeys.

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
run artifacts are in the `bmstore` bucket. That is MinIO's administrator; the
server and workers use their own user, `buildmax` / `buildmax-storage`, which
`deployment/kind/minio-init.yaml` creates with access to `bmstore` only — the
shape a real deployment's storage identity has, and the one the rotation drill
rotates.

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
transport against real inference without a hosted
deployment to point at.

```bash
./make kind up      # the stack, still answering from the mock
./make kind seed    # your models in its catalog
```

The command adds each model with `buildmax-server model add`, passing its key on
standard input, and stops there: a catalog row is callable as soon as it exists,
so nothing needs restarting and no configuration changes. An entry whose
`api_key` is still an example placeholder is skipped rather than seeded. Sign in with `buildmax login` against
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
its row and its ID, and its key is replaced from `.local/settings.yaml` with
`buildmax-server model set-key`, so fixing a wrong key is an edit and another
seed. The summary says how many models were added and how many had their key
refreshed. Changing a seeded model's endpoint still means renaming it in
`.local/settings.yaml`, or rebuilding the cluster.

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

### Single Sign-On

Every kind stack offers SSO next to password and login-code sign-in, against a
mock OIDC provider deployed beside the server
(`deployment/smoke/mock-oidc.kind.yaml`). `kind up` and `kind smoke` sign in
through it over HTTP, and `./make e2e kind` does it in a browser.

The issuer is the mock's Service name on the cluster's TLS port,
`https://buildmax-smoke-oidc.buildmax.svc.cluster.local:8443`, so the server
pods and a browser reach it by one URL — the pods directly, the browser through
the ingress. To sign in by hand, make that name resolve to this machine and
accept kind's self-signed certificate when the browser asks:

```bash
echo "127.0.0.1 buildmax-smoke-oidc.buildmax.svc.cluster.local" | sudo tee -a /etc/hosts
```

The mock's form asks who you are and checks nothing. An address in
`buildmax.local` that has no account is provisioned on first sign-in; one that
already has an account, such as `alice@buildmax.local` after `kind fixtures`, is
linked to it.

To qualify a real IdP instead, register `<portal origin>/api/auth/oidc/callback`
as its sign-in redirect URI (`http://localhost:8080/api/auth/oidc/callback` on
`buildmaxdev`), add the IdP to `.local/env`, and run `./make kind up`:

```bash
BUILDMAX_KIND_OIDC_ISSUER=https://example.okta.com
BUILDMAX_KIND_OIDC_CLIENT_ID=0oaExampleClientId
BUILDMAX_KIND_OIDC_DISPLAY_NAME=Okta
BUILDMAX_KIND_OIDC_ALLOWED_DOMAINS=example.com
BUILDMAX_OIDC_CLIENT_SECRET=…
```

The smoke then checks only that sign-in starts at that IdP, and the browser
suite skips its SSO tests; signing in is done by hand. Remove the variables and
run `kind up` again to return to the mock.

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
