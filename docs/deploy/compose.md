# Compose Quickstart

> **简体中文：** [阅读中文镜像](../zh-CN/deploy/compose.md)
> **Audience:** operators · **Status:** current
>
> A space deployment on one machine, in about five minutes. For what the pieces
> are and how to run them anywhere else, read [overview.md](overview.md) first
> or after — this page is the short path.

## What You Get

Three containers: MySQL, the server (which spawns workers itself), and the
Portal. No MinIO — the server and its workers share one container and one
volume, so the local filesystem backend is enough here. A deployment where
workers run elsewhere needs blob storage both can reach.

Sharing that container is also the security deal this stack makes: a task run is
a child process of the server it was scheduled by, under the same uid, so the
two are one trust domain. That is fine for a space that already trusts everyone
who can submit work. If you need the server separated from the code a model
chooses, run workers as Kubernetes Jobs instead — [overview.md](overview.md).

## Run It

For contribution work, one command starts the stack with a deterministic mock
model and proves the full flow through a real worker process:

```bash
./make compose smoke
```

That check covers Portal reachability, account bootstrap, space storage,
conversation and TaskRun creation, scheduler execution, model response,
artifact retrieval, and cancelling a run that is executing. It requires no provider key. Inspect failures with
`./make compose logs`, then stop the stack with `./make compose down`. On
success it prints a fresh single-use Portal login code for the smoke account.

The same flow runs with task-run inference going through the managed gateway
instead of a provider:

```bash
./make compose smoke managed
```

That variant proves the thing the default one cannot: the worker completes a
real task while holding no provider credential. It checks the run's trace
records the catalog model name as its model, which a worker that had quietly used a
provider key could not produce. The two are separate stacks because transport
is startup configuration — one server cannot serve both — and both modes need
end-to-end coverage. Switching between them recreates the server container.

`./make compose status` changes nothing. It lists every service in the stack,
including exited ones, and probes the server and Portal ports on the host, so a
stack that was never started is distinguishable from one whose server is
crash-looping before you read any logs.

For interactive evaluation with a real model, start the regular stack:

```bash
cd deployment/compose
./generate-env.sh          # writes .env with generated secrets
docker compose up -d
```

The first `up` builds the images from this checkout, which takes a few minutes.
Once a release publishes them, `docker compose pull` fetches them instead.

Wait for the server to report healthy:

```bash
docker compose ps
```

## Create Your Account

Nobody can register themselves — [signup is closed by
default](authentication.md). Create the first account from inside the server
container:

```bash
docker compose exec server buildmax-server user create you@example.com
docker compose exec server buildmax-server user login-code you@example.com
```

The second command prints a single-use code. Open <http://localhost:8080>,
choose "Forgot your password, or have a login code?", and enter that email and
code. Then set a password from account settings — after that you sign in with it
normally.

The Desktop app and `buildmax login` sign in to the same account, but they call
the API directly: their **Server URL** is <http://localhost:5678>, not the
Portal's port.

## Add a Model

The Portal runs without one, but conversations cannot reach a model until you
set a key. Put it in `.env`:

```bash
BUILDMAX_CONVERSATION_MODEL_API_KEY=sk-your-key-here
```

then `docker compose up -d` to apply it. The endpoint and model id are in
`server.yaml` next to the compose file — any OpenAI-compatible provider works.

## Put Something In The Workspace

A new space starts empty, and an agent with nothing to read cannot show you much.
Each space has a persistent file space — the workspace — that the worker
materializes into every task run, so whatever you put there is what the agent
sees when it works.

Open the **Files** page at <http://localhost:8080/#/explore>. **Upload Files**
takes individual files; **Upload Folder** preserves the directory structure,
which is the one to use here.

This repository ships a set of datasets for exactly this moment:

```text
sample-data/sales/       revenue by region and quarter, across nested year folders
sample-data/access_log/  a web access log
sample-data/orders/      e-commerce orders, with a README describing the columns
```

Upload `sample-data/sales/` as a folder, then start a conversation and ask for
something that requires reading it — "which region grew fastest between 2024 and
2025, and show the numbers you used". Work heavy enough to become a background
task runs in a worker against a copy of this same workspace, and reports its
result back into the conversation that started it.

[sample-data/README.md](../../sample-data/README.md) lists all fifteen datasets.
They are ordinary files with nothing special about them: any folder of your own
works the same way.

## What To Change Before Trusting It

This stack is shaped for a laptop. Before it faces anyone else:

- **Ports are published to the host.** The gateway on `8080` (the Portal and the
  API on one origin) and the server on `5678` (direct, for the CLI) are reachable
  from wherever the machine is reachable.
- **No TLS.** Everything speaks plain HTTP. Put a reverse proxy in front for TLS;
  the browser already reaches a single origin through the gateway.
- **The agent runs shell commands.** The worker executes what the model asks
  for, inside the server container. The official image selects the worker
  sandbox baseline; this Compose local-process path does not create a separate
  host trust boundary. See [sandbox boundaries](../../manual/sandbox.md).
- **Storage is a Docker volume.** `docker compose down -v` deletes every
  workspace, artifact, and account with it.

## Changing The Host Ports

`BUILDMAX_PORTAL_PORT` (the gateway the browser opens) and `BUILDMAX_SERVER_PORT`
(the server's direct port) in `.env` are the whole change. The browser reaches
the Portal and the API through the one gateway origin, so there is no
cross-origin pair to keep in step; the server's `cors_origin` is derived from
`BUILDMAX_PORTAL_PORT` only because the WebSocket upgrade still checks the
request origin against it.

Moving `BUILDMAX_PORTAL_PORT` is also how this stack runs beside a
[kind cluster](local-kind.md), which publishes `8080` and cannot move.

## Common Problems

| Symptom | Cause |
|---|---|
| `run ./generate-env.sh first` | `.env` is missing; compose refuses rather than starting with empty secrets |
| Portal loads but a page briefly shows 502 | The gateway is still warming up or following a just-restarted upstream; reload |
| `signup is disabled on this server` | Expected. Create the account with `user create` |
| `invalid otp` | The code is single-use and expires in an hour; issue another |
| Server restarts in a loop | Usually MySQL: `docker compose logs mysql` |

## Teardown

```bash
docker compose down       # keep the data
docker compose down -v    # delete it
```

## Related

- [overview.md](overview.md) — the topology, and configuration field by field
- [authentication.md](authentication.md) — accounts, login codes, what is missing
- [local-kind.md](local-kind.md) — the Kubernetes path instead of this one
