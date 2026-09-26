# DigitalOcean Qualification Infrastructure

> **简体中文：** [阅读中文镜像](../zh-CN/deploy/digitalocean.md)
> **Audience:** operators · **Status:** current
>
> This is the lowest-cost external infrastructure prepared for the BuildMax
> beta gate. It provisions the infrastructure and deploys the pinned trial;
> the operator journey still determines whether it constitutes beta-gate evidence.

`./make ocean` manages a disposable DOKS cluster and managed MySQL cluster with
OpenTofu. It deliberately reuses three resources that an operator creates and
keeps outside OpenTofu:

| Resource | Default | Ownership |
|---|---|---|
| DigitalOcean Project | `buildmax-beta` | Persistent, operator-managed |
| VPC | `buildmax-beta` in `sgp1` | Persistent, operator-managed |
| Spaces bucket | `buildmax-beta` in `sgp1` | Persistent, operator-managed |
| DOKS cluster | `buildmax-beta-doks`, one `s-2vcpu-4gb` node | Disposable, OpenTofu-managed |
| Managed MySQL | `buildmax-beta-mysql`, one `db-s-1vcpu-1gb` node | Disposable, OpenTofu-managed |
| MySQL firewall and `buildmax` database | Attached to managed MySQL | Disposable, OpenTofu-managed |

DOKS high availability, auto-upgrade, and surge upgrade are explicitly off.
The DOKS and MySQL resources accrue charges until `./make ocean down` succeeds;
the Project and VPC are free, and the persistent Spaces subscription continues
independently. Confirm current provider prices before creating the resources.

## Prerequisites

Install [OpenTofu](https://opentofu.org/docs/intro/install/) and `kubectl`, then
create the local configuration directory and add the three credentials to
`.local/env`:

```bash
./make setup local
```

The DigitalOcean API token needs only the capabilities used by these resources:

- account and actions: read
- regions and sizes: read
- Kubernetes: create, read, update, delete, access cluster
- Tags: create (the OpenTofu provider tags its default DOKS node pool)
- databases: create, read, update, delete, view credentials
- Project: read and assign resource
- VPC: read

Project and VPC create, update, and delete are unnecessary because OpenTofu
reads them as existing resources. Scope the Spaces key to the `buildmax-beta`
bucket with read, write, and delete; BuildMax workers need all three during the
qualification run.

Verify the local setup without calling DigitalOcean or writing a file:

```bash
./make ocean doctor
```

## Create And Inspect

Review the create plan, then apply it:

```bash
./make ocean plan
./make ocean up
```

`up` repeats the plan and requires typing the Project name before it applies
anything. Once it completes:

```bash
./make ocean info
./make ocean status
export KUBECONFIG="$HOME/.buildmax/qualification/ocean/kubeconfig.yaml"
kubectl get nodes
```

`info` prints resource names and endpoints, but never the database password or
kubeconfig contents.

The OpenTofu source is under [`deployment/ocean/`](../../deployment/ocean/).
Names and the region can be changed without editing it:

| Variable | Default |
|---|---|
| `BUILDMAX_OCEAN_PROJECT` | `buildmax-beta` |
| `BUILDMAX_OCEAN_VPC` | `buildmax-beta` |
| `BUILDMAX_OCEAN_BUCKET` | `buildmax-beta` |
| `BUILDMAX_OCEAN_REGION` | `sgp1` |
| `BUILDMAX_OCEAN_DATABASE_VERSION` | `8.4` |
| `BUILDMAX_OCEAN_STATE_DIR` | `~/.buildmax/qualification/ocean` |

All three persistent resources must use the configured region.

## Deploy The Application Trial

The application phase needs a full hostname and at least one trusted client
network. CIDRs are enforced by the Caddy edge while its automatic certificate
challenge remains reachable:

```bash
BUILDMAX_OCEAN_HOSTNAME=buildmax.beta.cloudbb.io
BUILDMAX_OCEAN_ALLOWED_CIDRS=203.0.113.7/32
```

Put those values in `.local/env`, using the public CIDR of the machine
or private network that will perform qualification. Multiple CIDRs may be
separated by commas. A missing allow-list is a hard error: the beta limits do
not permit exposing the application directly to an untrusted public network.

Deploy the pinned trial images:

```bash
./make ocean deploy
./make ocean model init
./make ocean app-status
./make ocean show all
```

`show all` runs `kubectl get all --namespace buildmax --output wide` with the
owner-only kubeconfig written by `ocean up`. It is read-only and does not depend
on the contributor's current Kubernetes context.

`model init` reads `OPENROUTER_API_KEY` from `.local/env`, adds the configured
model when its name is not already present, selects its generated catalog ID for
Tier 1 conversations, and restarts only the BuildMax Server deployment. The key is
sent to the operator command over stdin and is never printed or rendered into a
Kubernetes manifest. Repeating the command reuses the existing catalog row.

The default is the repository's low-cost OpenRouter baseline, `GPT-5.6 Luna`
(`openai/gpt-5.6-luna`). Override its metadata with the
`BUILDMAX_OCEAN_MODEL_*` variables documented in `.env.example`; if prices
change, update them before initializing the catalog so call-cost records remain
accurate. Inspect the redacted catalog at any time:

```bash
./make ocean model list
```

The defaults are the immutable multi-platform digests published for
`v0.2.0-alpha.4`, plus a pinned Caddy 2.10.2 image. Override them only with
another digest, never a mutable tag:

| Variable | Default artifact |
|---|---|
| `BUILDMAX_OCEAN_IMAGE` | `ghcr.io/icloudbb/buildmax@sha256:64e6775796b4bf0cb1145e3aaa79084e170f1ec340bd5af1cddc1a28cc0336dd` |
| `BUILDMAX_OCEAN_PORTAL_IMAGE` | `ghcr.io/icloudbb/buildmax-portal@sha256:82165de877e4cae3c5a1c598b6f39b37a94db114ab6ce315b237d5913f7e2e2b` |
| `BUILDMAX_OCEAN_EDGE_IMAGE` | `caddy:2.10.2-alpine@sha256:4c6e91c6ed0e2fa03efd5b44747b625fec79bc9cd06ac5235a779726618e530d` |

`deploy` refreshes OpenTofu's read-only database CA output, combines that CA
with the image's public trust bundle, and starts BuildMax with `database.tls:
"true"`. The server therefore verifies both DigitalOcean MySQL and its public
HTTPS dependencies; it never uses `skip-verify`. Kubernetes receives the
database, Spaces, and generated JWT credentials through a Secret assembled in
memory. The rendered Secret is not written to the checkout.

The first `deploy` also generates the deployment key-encryption key (KEK) as
`kek.json` in the state directory and every deploy ships that same file as the
`buildmax-kek` Secret, mounted into the server pod only and named by
`secret.kek_file`. The server seals the model credential that `model init` adds
under it, so `model init` needs a deployment that already has it: after
upgrading from a deploy that predates the KEK, run `deploy` again first. The key
is never regenerated. If `kek.json` is missing while the cluster still holds the
`buildmax-kek` Secret, `deploy` refuses rather than replacing the key; restore
the file from your backup. See
[the KEK reference](../reference/configuration.md#the-deployment-key-encryption-key)
for the file format.

The command ends by printing the DigitalOcean Load Balancer IP. Add the record
manually in Route 53:

```text
buildmax.beta.cloudbb.io  A  <Load Balancer IP>
```

Caddy obtains the certificate after public DNS resolves. Rerun `app-status`
while the Load Balancer is pending, then verify from an allowed network:

```bash
curl -I https://buildmax.beta.cloudbb.io/
```

The application phase also creates one DigitalOcean Load Balancer and a 1 GiB
block-volume claim for Caddy's certificate state. Both are billable and are
associated with the disposable DOKS cluster, so `./make ocean down` removes
them with the cluster.

The deployment step validates the external MySQL, Spaces, Kubernetes, Load
Balancer, and TLS path without a model credential. `model init` is the separate,
explicit step that selects the approved managed model before the beta operator
journey.

## Inspect MySQL Locally

The managed database accepts traffic only from the DOKS cluster, and `info`
prints its private VPC hostname. Keep that firewall boundary and tunnel through
the Kubernetes API instead of adding a public database rule:

```bash
./make ocean info --show-secrets
./make ocean database forward
```

The first command explicitly prints the database username and password; ordinary
`info` keeps them hidden. The second creates a credential-free proxy deployment,
writes the DigitalOcean CA to the owner-only Ocean state directory, and forwards
MySQL to `127.0.0.1:13306` until interrupted. It prints a ready-to-run `mysql`
command that verifies the database CA. Set
`BUILDMAX_OCEAN_DATABASE_LOCAL_PORT` to override that local port.

The proxy has no Service and does not make MySQL reachable outside the
Kubernetes API. Its deployment disappears with the disposable DOKS cluster.

## State And Secrets

OpenTofu state contains the generated MySQL password and DOKS kubeconfig. The
command therefore refuses a state directory inside the Git checkout. The
state, saved plans, provider working directory, and generated kubeconfig live
under `BUILDMAX_OCEAN_STATE_DIR`, whose directory and sensitive files are set
to owner-only permissions.

Treat that directory as a credential:

- never upload it to the application Spaces bucket
- back it up securely while the managed resources exist
- do not delete it before `./make ocean down`
- rotate database credentials and Kubernetes access if it is disclosed

The directory also holds `kek.json`, the key that seals the model credentials
and Space Secrets in the managed database. Back it up apart from any database
backup: a backup holding both protects nothing, and a database restored without
its KEK has unreadable credentials that no BuildMax command can recover. `ocean
down` keeps the file, so the next deploy reuses it.

`.local/env` also remains local and gitignored. OpenTofu reads its credentials
through the environment populated by `./make`; no credential is written into the
OpenTofu source.

## Destroy

When the qualification session is over:

```bash
./make ocean down
```

The command shows a destroy plan and requires the Project name again. It
destroys the disposable stack declared in `deployment/ocean`: DOKS, managed
MySQL, the MySQL firewall/database, the cluster's Project assignment, and
DigitalOcean resources associated with that DOKS cluster. It retains the
existing Project, VPC, Spaces bucket, and every Route 53 record.

Check the DigitalOcean control panel after teardown. If an application
deployment created a load balancer outside this OpenTofu state, remove it
separately after preserving the beta-gate evidence.

## Next Qualification Step

Infrastructure and application deployment establish the real external
dependency boundary, but the beta gate still requires the pinned candidate to
be exercised. Record an approved managed model, then perform the operator
journey, failure drills, backup restore, and rollback below.

Record the eventual deployment, smoke results, failure drills, restore, and
rollback in the [Beta readiness record](beta-readiness.md).
