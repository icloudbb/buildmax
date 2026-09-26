# Deployment Overview

> **简体中文：** [阅读中文镜像](../zh-CN/deploy/overview.md)
> **Audience:** operators · **Status:** current
>
> **BuildMax is alpha.** Read [authentication.md](authentication.md) before
> putting a server anywhere it can be reached by people you do not trust. The
> current deployment support boundaries are in
> [manual/support.md](../../manual/support.md).

Deploying BuildMax for a space means running two Go binaries plus two backing
services. There is nothing to install into the agent runtime itself — the
worker is the same runtime the CLI uses, started by the server.

To see it working before reading any of this, use the
[Compose smoke](compose.md) for the fast local-process path or the
[kind smoke](local-kind.md) for the Kubernetes Job path.

Those development checks do not qualify a release for production use. To test
a pinned candidate against external dependencies, failure cases, restore, and
rollback, prepare the disposable
[DigitalOcean qualification infrastructure](digitalocean.md) and use the
[Beta readiness record](beta-readiness.md).

## Topology

```text
             ┌────────────┐
  browser ──▶│   Portal   │  static React bundle
             └─────┬──────┘
                   │ HTTP + WebSocket
             ┌─────▼────────────────────┐        ┌──────────┐
             │     buildmax-server      │───────▶│  MySQL   │
             │  HTTP API + scheduler    │        └──────────┘
             └─────┬────────────────────┘
                   │ spawns one process (or k8s Job) per run
             ┌─────▼────────────────────┐        ┌──────────────────┐
             │     buildmax-worker      │───────▶│ Blob storage     │
             │  one task run, then exit │        │ local FS or S3   │
             └──────────────────────────┘        └──────────────────┘
```

| Component | Role |
|---|---|
| `buildmax-server` | HTTP API, auth, space data, WebSocket fan-out, and the in-process scheduler that claims `PENDING` runs |
| `buildmax-worker` | Executes exactly one task run and exits. Reads and writes blob storage **directly**, not through the server. |
| MySQL | Spaces, conversations, issues, workflows, tasks, runs, usage |
| Blob storage | Space file uploads (`persist_backend`) and run artifacts (`artifact_backend`). Local filesystem or any S3-compatible service such as MinIO. |
| Portal | Static frontend built from `portal/`; talks to the server over HTTP |

## Requirements

- MySQL reachable from the server
- Blob storage reachable from **both** the server and every worker
- A writable `workspaces_dir` shared by server and workers when running workers
  as local processes
- An LLM endpoint reachable from the server (Tier 1 conversation model) and
  from workers (the model used for the actual run)

## Configure

Everything lives in `<BUILDMAX_HOME>/server.yaml`. Start from the example and
fill in the four blocks that matter — `database`, `storage`, `worker`, and
`conversation`:

```bash
mkdir -p /etc/buildmax
export BUILDMAX_HOME=/etc/buildmax
cp config-examples/server.example.yaml $BUILDMAX_HOME/server.yaml
```

Inject secrets at deploy time instead of writing them into the file:

```bash
export BUILDMAX_JWT_SECRET="$(openssl rand -hex 32)"
```

To store managed-model provider credentials or Space Secrets, also mount a
key-encryption key file and point `secret.kek_file` at it. Without one,
adding a model with an API key is refused. Generate the file once and back it
up separately from the database: losing it makes every sealed credential
unreadable. The format and generation command are in
[the KEK reference](../reference/configuration.md#the-deployment-key-encryption-key).

Field-by-field reference:
[reference/configuration.md](../reference/configuration.md).

## Run

```bash
buildmax-server                 # honours port from server.yaml, or --port
```

The scheduler starts with the server. It launches `worker.binary`, so
`buildmax-worker` must be on `PATH` or next to the server binary, and workers
must be able to reach `worker.server_url` with the run token the server issues it.
In this default `local_process` mode a worker is a child of the server under the
same uid, which puts both in one trust domain — see [Operating
Boundaries](#operating-boundaries).

Set `worker.run_mode: k8s_job` to have the scheduler create a Kubernetes Job per
run instead of a local process, using `worker.k8s.namespace` and
`worker.k8s.image`. That mode also requires all four `worker.k8s.resources`
bounds; the server refuses to start rather than schedule an unbounded worker.

In that mode a worker pod needs the same `server.yaml` the server has.
`worker.k8s.config_map` names a ConfigMap with a `server.yaml` key, which the
scheduler mounts into every worker pod at `worker.k8s.home_dir`, and the pod's
`BUILDMAX_HOME` is set to that directory. Credentials reach worker pods through
the inherited `BUILDMAX_*` environment. Leave `config_map` empty and worker pods
fall back to built-in defaults, which is almost never what you want.

Deployment-related changes run an end-to-end kind check in CI. It creates a
real worker Job and verifies the returned artifact, while unit tests continue
to check the generated Job and manifest contracts.

Check it is alive:

```bash
curl localhost:5678/healthz   # the process is up
curl localhost:5678/readyz    # its dependencies answer too
```

The API describes itself at `/openapi.json`, with a browsable UI at `/swagger/`.

## Portal

The Portal is a static bundle. Run the published image:

```bash
docker run -p 8080:80 \
  -e BUILDMAX_API_BASE=https://api.example.com \
  ghcr.io/icloudbb/buildmax-portal:<version>
```

`BUILDMAX_API_BASE` is applied at container start, not at build time, so one
image serves every deployment. Two things follow from that:

- **The browser calls that URL directly.** It must be an address the user's
  machine can reach — not a cluster-internal Service name.
- **The server's `cors_origin` must name the Portal's own origin**, or the
  browser blocks every request. Set both together.

Put the Portal and the server behind one hostname with a reverse proxy and the
problem disappears: set `BUILDMAX_API_BASE=/` and the Portal calls its own
origin, with no cross-origin request to permit.

The image tag matches the release it was built from, so
`ghcr.io/icloudbb/buildmax-portal:0.1.0` pairs with
`ghcr.io/icloudbb/buildmax:0.1.0`.

To build the bundle yourself instead:

```bash
cd portal && npm install && npm run build     # → portal/dist
```

Serve `portal/dist` from any static host. A hand-built bundle takes its API URL
from `VITE_API_BASE` at build time, or defaults to `http://localhost:5678`.

## Containers

| Image | Contents |
|---|---|
| `ghcr.io/icloudbb/buildmax` | CLI, server, and worker binaries |
| `ghcr.io/icloudbb/buildmax-portal` | Static frontend served by nginx |

Both are published per release tag, from `.goreleaser.yaml` and
`.github/workflows/portal-image.yml` respectively. They are built by separate
workflows so a frontend failure cannot hold up the binaries.

`deployment/docker/Dockerfile.buildmax` builds the Go binaries from source for
local use;
`deployment/buildmax-deploy.yaml` is a working Kubernetes manifest — namespace,
Secret, Deployment, Service, and Ingress — used by `./make kind up` against a
local kind cluster. It brings its own MySQL and MinIO and hardcodes their
in-cluster addresses, so it is a development environment rather than a template.

For a private deployment against dependencies you already operate, start from
[`deployment/production/`](../../deployment/production/README.md): one plain
YAML manifest plus the contract each dependency has to meet. It is written to
be read and adapted rather than applied, and it is plain YAML precisely so it
converts into whatever your cluster is already managed with.

## Operating Boundaries

An agent runtime executes model-selected shell commands and file edits. Treat
every deployment as an execution boundary:

- give the server and workers dedicated, least-privilege credentials
- keep `workspaces_dir` and blob storage off any host path that matters
- decide the network policy for workers explicitly; the
  [sandbox](../../manual/sandbox.md) can restrict Bash egress. Official worker
  images select it, while local CLI/Desktop defaults remain off and the worker
  API/cluster policy is a separate boundary
- never commit credentials to `server.yaml` in version control
- know which boundary you have: `local_process` runs workers as children of the
  server on one host, one trust domain, and narrowing what they inherit does not
  change that; `k8s_job` is where the server is separated from model-chosen code

Vulnerability disclosure: [SECURITY.md](../../SECURITY.md).

## Related

- [compose.md](compose.md) — the whole stack on one machine, server and workers
  in one trust domain
- [authentication.md](authentication.md) — creating accounts and issuing login codes
- [local-kind.md](local-kind.md) — one-command local cluster for development
- [digitalocean.md](digitalocean.md) — disposable external DOKS and MySQL for beta qualification
- [reference/webhook.md](../reference/webhook.md) — triggering runs from
  external systems
