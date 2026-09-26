# Private Deployment Reference

> **Audience:** operators · **Status:** current — reference, not an installer

[`buildmax.yaml`](buildmax.yaml) is a complete BuildMax deployment written to be
read and adapted. It is not applied as-is: every `REPLACE_ME` must be replaced,
and a `kubectl apply` of the unedited file fails to start rather than coming up
against the wrong dependencies.

There is no Helm chart and no kustomize base. The manifest is plain YAML so it
can be converted into whatever your cluster is already managed with, rather than
arriving with a structure you have to undo.

## What BuildMax Brings, And What You Bring

BuildMax deploys the server, the Portal, and the worker Jobs the server creates
per task run. It does not install a database, an object store, an ingress
controller, or certificates.

`deployment/buildmax-deploy.yaml` is the other path: a self-contained stack with
its own MySQL and MinIO, used by `./make kind up`. That one is a development
environment. Do not adapt it for production — it exists to be started and thrown
away, and its manifest hardcodes in-cluster dependency addresses that only
resolve there.

## The Dependency Contract

### MySQL

| Requirement | Value |
|---|---|
| Version | 8.0 or later |
| Character set | `utf8mb4` — the DSN asks for it, and the schema assumes it |
| Privileges | Full DDL on its own schema: BuildMax creates and alters its tables at startup |
| TLS | Set `database.tls: "true"` so the connection is encrypted and the certificate verified |

The DDL privilege is not optional. The schema is applied by the server on
start — `AutoMigrate` for additive changes plus an ordered migration list for
everything else — so a user restricted to DML cannot bring the server up. Grant
it on the BuildMax schema alone, not server-wide.

Sizing is driven by task runs and messages rather than by user count. A space
running a few hundred task runs a day is a small database.

### Object storage

| Requirement | Value |
|---|---|
| API | S3, or an S3-compatible implementation |
| Bucket | One, dedicated. BuildMax prefixes its keys but does not expect to share |
| Access | Read, write, and list on the whole prefix |
| Credentials | IRSA, workload identity, or an instance profile preferred; static keys supported |

Leave `endpoint` empty for AWS S3, so the SDK resolves the regional endpoint and
uses virtual-host addressing. Set it to the base URL of a store you run, which
also switches addressing to bucket-in-path. `path_style` overrides that
derivation for a compatible store that uses virtual-host addressing.

Leaving the access key and secret key unset is the better configuration where
your platform supports it. Workers are handed the storage credentials to read
and write run state, and a worker executes model-chosen shell commands — so a
static key is a long-lived credential inside that blast radius, while a
projected identity is not.

Lifecycle rules are yours to set. BuildMax tombstones deleted or expired
artifacts and, when `storage.artifact_purge_after_days` is non-zero, reclaims
their object content after that recovery window. Run retention is otherwise an
operator concern.

Before accepting production traffic, validate this contract from the adapted
deployment with the same workload identity the server and workers will use. The
validation must prove read, write, and list access to the configured prefix.
This is deployment initialization rather than a `/readyz` check: readiness
intentionally performs only read-only dependency availability checks. The
repository's kind smoke covers this for its development MinIO instance; an
explicit validation command for an operator-managed bucket is not implemented
yet.

### Ingress and TLS

The manifest assumes one origin serves both the Portal and the API, which is
what `BUILDMAX_API_BASE: "/"` depends on. Serving them from two hosts means
setting an absolute API base and configuring `cors_origin` accordingly.

No ingress class is assumed and no controller-specific annotations are included.
Set `ingressClassName`, and add whatever annotations your controller needs.

`/healthz` and `/readyz` are deliberately **not** routed through the Ingress. The
kubelet reaches them directly on the pod; publishing them only exposes
dependency status to anyone who asks.

### Worker API boundary

The server exposes two listeners. The public API is the `buildmax-api` Service,
and it is the only backend the Ingress names — the broad `/api` rule is safe
because the public listener carries no worker route. The worker control API
(`/api/worker/*`) is served on a second listener, fronted by the internal
`buildmax-worker-api` `ClusterIP` on port 5679, and the `buildmax-server`
`NetworkPolicy` admits that port only from pods carrying the worker labels the
runner stamps on every worker Job. A `ClusterIP` alone is discoverability, not
authorization; the policy is what makes it a boundary, and it protects direct
pod-IP access as well as Service access.

That listener serves TLS. Supply its certificate — valid for
`buildmax-worker-api.buildmax.svc.cluster.local` — as the `buildmax-worker-api-tls`
Secret (keys `tls.crt`, `tls.key`), mounted only into server pods. Publish the
signing CA as the `buildmax-worker-api-ca` ConfigMap (key `worker-api-ca.crt`),
named in `worker.k8s.ca_config_map`; the runner mounts it read-only into each
worker pod at `worker.server_ca_file`, and the worker verifies the server
against it with no insecure fallback. cert-manager can issue both from one
internal Issuer. The server refuses to start if the keypair will not load.

### Worker sandbox

A worker pod executes model-chosen shell commands inside `bubblewrap`, not
unconfined and not under Kubernetes' `RuntimeDefault` seccomp profile —
`RuntimeDefault` blocks `bubblewrap` from creating its own sandbox namespaces
once capabilities are dropped, which every worker pod's `securityContext`
does. Before applying this file, create the ConfigMap the manifest's
`buildmax-worker-seccomp` `DaemonSet` installs onto every node:

```sh
kubectl create configmap buildmax-worker-seccomp -n buildmax \
  --from-file=worker-bwrap.json=deployment/seccomp/worker-bwrap.json
```

Re-run it whenever `deployment/seccomp/worker-bwrap.json` changes. See
[`deployment/seccomp/README.md`](../seccomp/README.md) for what the profile
allows and why, and
[`docs/design/agent-sandbox-policy.md`](../../docs/design/agent-sandbox-policy.md)
for how this fits the rest of the sandbox effort. A worker pod refuses to
start on any node the `DaemonSet` has not yet reached.

### Key-encryption key

The server seals managed-model provider credentials and Space Secrets in the
database under a deployment key-encryption key (KEK). The manifest sets
`secret.kek_file: /etc/buildmax/kek/kek.json` and mounts it from the
`buildmax-kek` Secret into server pods only: read-only, outside
`BUILDMAX_HOME`, with `defaultMode: 0400`. The server runs as non-root, and the
pod's `fsGroup: 65532` is what lets it read the file — the kubelet adds group
read, so it is `0440` owned by `root:65532`. Keep `fsGroup` equal to the
server's group if you change the pod's `securityContext`, or the server cannot
read its key. Without a KEK, `buildmax-server model add
--api-key` is refused rather than storing the key in the clear, and Space
Secrets are off.

Generate the key file once and create the Secret before applying the manifest:

```sh
printf '{"current":"file:root:1","keys":{"file:root:1":"%s"}}\n' \
  "$(openssl rand -base64 32)" > kek.json
kubectl create secret generic buildmax-kek -n buildmax \
  --from-file=kek.json=kek.json
```

The file maps each key id to base64-encoded 32-byte key material and names the
`current` one new writes use; the format is in the
[configuration reference](../../docs/reference/configuration.md#the-deployment-key-encryption-key).
The Secret is deliberately not a document in `buildmax.yaml`: re-applying a
placeholder over the real key would lose it. The volume is not optional, so a
server pod does not start until the Secret exists, and a server whose key file
does not load refuses to start.

Back the key file up, and keep that backup apart from the database dump. A
backup holding both the dump and the key protects nothing, and losing the key
makes every sealed credential and Space Secret permanently unreadable — no
BuildMax command can recover them. Never regenerate the file or change the bytes
under an existing key id: the stored values stop decrypting, which breaks every
model and Secret that uses one. To replace the key, follow the
[credential rotation runbook](../../docs/deploy/credential-rotation.md#key-encryption-key).

### Model access

The reference points `conversation.model` at an OpenAI-compatible endpoint. A
deployment that would rather not distribute provider keys can serve
operator-approved models through the managed gateway instead; see
[`docs/design/llm-gateway.md`](../../docs/design/llm-gateway.md) for what is
implemented today.

## Upgrades

Schema changes are applied by the server at startup and move **forward only**.
There are no down migrations.

Rolling the binary back is not supported either. An older image refuses to
start against a database a newer release has migrated, because its startup
would re-add what the newer migrations dropped; images up to 0.2.0-alpha.15
predate that refusal and damage the database instead. So take a backup of the
database and the bucket together before every upgrade — which is also why the
manifest says to pin a version rather than track `:latest`. An upgrade that
goes wrong is recovered by restoring both from that backup and redeploying the
image tags that match it. See
[Compatibility](../../manual/support.md#compatibility) for the destructive
migrations and the `database.allow_newer_schema` override.

The rollout sets `maxUnavailable: 0`, so the new pod must pass readiness before
an old one goes away. The `startupProbe` gives the first pod room to finish
migrations before liveness starts counting.

Going away is its own sequence. A pod that receives SIGTERM stops reporting
ready, ends the streams watching a run so the Portal reopens them against
another pod, refuses new conversation turns while waiting for the ones already
running, and drains the requests it accepted — all inside `shutdown_grace`,
which the ConfigMap sets to 25s. `terminationGracePeriodSeconds: 45` and the
five-second `preStop` pause exist to contain that: raising one without the
others is what turns an orderly stop back into a kill.

A worker pod also honours SIGTERM. It stops the agent loop, preserves the output
and artifacts produced so far, reports the run as `FAILED` with an interrupted
message, and exits. A worker that disappears before it can report is closed as
`FAILED` by the liveness reaper after the configured grace; BuildMax does not
silently re-dispatch it. Retrying is an explicit operator action and creates a
new TaskRun. The exact shutdown contract is in
[`docs/design/graceful-shutdown.md`](../../docs/design/graceful-shutdown.md).

## Rotating Credentials

Every credential above — the JWT secret, database password, storage key,
provider keys, KEK, and worker API certificate — is replaced by changing its
Secret and rolling the server, with an overlap or a drain chosen per
credential. The
[credential rotation runbook](../../docs/deploy/credential-rotation.md) gives
each procedure and what users and runs notice.

## What This Reference Does Not Cover

Stated rather than left to be discovered:

- **Worker egress NetworkPolicy.** The manifest ships the server-ingress policy
  that gates the worker port (see "Worker API boundary" above), but not a
  default-deny on worker *egress*. Worker pods reach the model endpoint, object
  storage, and the server. The first private Beta records and accepts this
  residual limit. Pod-wide destination control is conditional hardening rather
  than a committed BuildMax dependency; see
  [`docs/design/trust-harness.md`](../../docs/design/trust-harness.md) §3.9 for
  the evidence that would reopen it.
- **Backups.** Database and bucket backups are yours. BuildMax has no export or
  import command. A database restore is only readable with the KEK it was
  sealed under, which you back up separately (see "Key-encryption key" above).
  [docs/deploy/backup-restore.md](../../docs/deploy/backup-restore.md) is the
  procedure: what to copy, database before bucket, the no-delete window, a
  separate recovery bucket, and how to verify.
  What BuildMax does provide is the check that a restored pair
  agrees: run `buildmax-server storage verify` in a server container (it reads
  the same `server.yaml`) after restoring. It walks every live artifact,
  workspace checkpoint payload, run trace, and plugin release package the
  database names, prints each object that is missing, altered, or unreadable
  with its record id, and exits non-zero if it found any. Add `--checksums` to
  also compare each object's size and SHA-256; that reads every stored byte.
  The check never deletes, rewrites, or tombstones a record or an object. Like
  every `buildmax-server` subcommand it opens the database through the
  server's store, which refuses a database a newer release has migrated and
  otherwise first applies that release's schema migrations, so run it from the
  release you are restoring onto.
- **Horizontal scaling of workers.** Worker Jobs are created per task run and
  bounded by their own resource settings, not by a replica count. Cluster
  capacity is what limits concurrency.
- **Multi-region, HA MySQL, and SSO.** Out of scope for this reference.
- **Verification against a live cloud provider.** This manifest is checked in
  CI for whether its configuration parses into the config the server actually
  reads, and its container hardening is the same set `./make kind up` applies
  and exercises on every run. It has not been applied against a real RDS or S3
  account — the dependency contract above is what stands in for that. Do not
  treat the reference as Beta-qualified until the external exercises in the
  [Beta readiness record](../../docs/deploy/beta-readiness.md) pass.
