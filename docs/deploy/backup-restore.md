# Back Up And Restore A Deployment

> **简体中文：** [阅读中文镜像](../zh-CN/deploy/backup-restore.md)
> **Audience:** operators · **Status:** current — rehearsed on kind, not yet on a
> Beta candidate
>
> How to take a backup of a BuildMax server deployment that can be restored, and
> how to restore it into a recovery environment and prove the restore is whole.

BuildMax has no export or import command. The database and the object-storage
bucket are yours to back up with your own tools; this page states what they must
contain, in what order to copy them, and how to check the result. The procedure
is rehearsed end to end by `./make kind drill restore` (see
[Rehearse it on kind](#rehearse-it-on-kind)); the first Beta candidate still has
to run it against its own dependencies
([beta-readiness.md](beta-readiness.md#recovery-and-maintenance)).

## What To Back Up

| Item | Where it is | Why |
|---|---|---|
| The database schema | `database.name` in `server.yaml`: every table the server migrates | All records: accounts, Spaces, Tasks, TaskRuns, Artifacts, audit, usage, sealed Secrets and model credentials |
| The bucket prefix | `storage.minio.bucket` under `storage.minio.prefix` (default `workspaces`) | Artifact content, run traces and session bundles, workspace checkpoint payloads, plugin packages, and Space files |
| The KEK file | `secret.kek_file`, mounted from the `buildmax-kek` Secret | Unseals Space Secrets and managed-model credentials |
| `server.yaml` | the `buildmax-config` ConfigMap | Names the schema, bucket, prefix, retention windows, and every other setting the restored server must match |

Two of these deserve care:

- **Space files live only in the bucket.** No database row names them, so their
  recovery point is the moment the bucket copy read them, not the database
  snapshot, and `storage verify` cannot check them.
- **Keep the KEK backup apart from the database dump.** Either alone is harmless;
  together they unseal every stored credential. A dump restored without the KEK
  it was sealed under starts, but its Secrets and model credentials never
  decrypt again. See
  [Key-encryption key](../../deployment/production/README.md#key-encryption-key).

Nothing else needs a backup. Redis holds only live coordination state (streams,
connection events, turn leases) and starts empty. The JWT secret and the worker
API TLS certificate are regenerated for the recovery environment: a new JWT
secret signs every earlier session out, which a restore should do anyway.

## Take The Backup

### Database first, then the bucket

1. Take **one transactionally consistent snapshot** of the whole schema. With
   MySQL that is `mysqldump --single-transaction` (add `--routines --triggers
   --events --set-gtid-purged=OFF`), or your provider's point-in-time snapshot.
   Record when the snapshot started: that is the **recovery point**.
2. **Then** copy the bucket prefix, for example
   `mc mirror <alias>/<bucket>/<prefix> <destination>`.

Never copy the bucket first. The database names objects; the bucket copy must
hold at least every object the snapshot names. An object written after the
snapshot is harmless extra data, but an object the snapshot names that is not in
the copy is lost.

### Keep a no-delete window open while the bucket copies

Between the snapshot and the end of the bucket copy, nothing may delete an
object the snapshot still names. BuildMax deletes objects in three places; hold
each off for longer than the copy takes:

- `storage.artifact_purge_after_days` of at least `1`, so a deleted Artifact's
  bytes outlive the copy;
- `storage.checkpoint_orphan_grace_days` of at least `1`, so a checkpoint
  payload whose row disappears after the snapshot is not reclaimed mid-copy;
- no trace pruning during the copy: leave `trace.retention_days` at `0`, or take
  the backup well clear of the hourly sweep that prunes expired traces.

All three are `server.yaml` settings and need a server restart to change; see the
[configuration reference](../reference/configuration.md). The window costs only
storage.

### Online by default, quiesced before an upgrade

A routine backup runs **online**. A run in flight at the snapshot is restored as
RUNNING with no worker behind it; the recovery server's liveness reaper closes it
`FAILED` after its grace (two minutes without a report), and its result is the
**accepted loss** of an online backup. Retry it after the restore.

Before an upgrade, and whenever you want no run lost, **quiesce** first: stop
creating work, wait until no TaskRun is `RUNNING` or `SCHEDULED` and no worker
Job is active, then scale the server Deployment to zero before the snapshot. A
restored database that still holds `PENDING` runs dispatches them, and enabled
Schedules fire, as soon as the recovery server starts.

## Restore Into A Recovery Environment

Restore into an **empty recovery environment**, never on top of the live one:

- **Use its own bucket.** The recovery server's checkpoint orphan sweep runs at
  startup and deletes payloads its database does not name. Pointed at the live
  bucket, it would delete every checkpoint written after the recovery point.
- **Withhold the Telegram bot token.** A recovery server with the live
  deployment's token long-polls the same bot and takes its users' messages.
  Leave `channels.telegram.bot_token` and `BUILDMAX_TELEGRAM_BOT_TOKEN` unset
  until the recovery environment becomes the live one.

Then, **before any server starts**:

1. Create an empty database and load the dump with the same MySQL user grants
   the server uses.
2. Create the recovery bucket and copy the backup into it under the same
   prefix, for example `mc mirror <backup> <recovery-alias>/<bucket>/<prefix>`.
3. Create the `buildmax-kek` Secret from the **original** KEK file, a new
   `buildmax-secret` (fresh `BUILDMAX_JWT_SECRET`, the recovery database and
   storage credentials), and new worker API TLS material.
4. Create the `buildmax-config` ConfigMap from the backed-up `server.yaml`,
   changed only where the recovery environment differs (endpoints, bucket).
5. Deploy the **same image tags** the backup was taken from. A newer binary
   migrates the schema forward; an older one refuses to start against it. See
   [Upgrades](../../deployment/production/README.md#upgrades).

Create the ConfigMap before the server Deployment: `deployment/buildmax-deploy.yaml`
carries a default `buildmax-config`, and a server that starts on it would open
the restored database with the wrong settings.

## Verify The Restore

1. Run the reference check in a server container, which reads the same
   `server.yaml`:

   ```sh
   kubectl exec -n buildmax deploy/buildmax-server -- \
     buildmax-server storage verify --checksums
   ```

   It walks every live Artifact, workspace checkpoint payload, run trace, and
   plugin package the database names, reports each missing, altered, or
   unreadable object by record id, and exits non-zero on any finding. It repairs
   nothing. Run it on the source too before relying on a backup, so a finding
   afterwards is known to come from the restore.
2. Issue a fresh login code (`buildmax-server user login-code <email>`), sign
   in, and compare what you recorded before the backup: Space, Task, and TaskRun
   identifiers and statuses, trace, audit event, and usage records, and each
   Artifact's listed and downloaded SHA-256.
3. Continue a Task from before the backup. Its run restores the backed-up
   workspace checkpoint and session history, which is the proof those objects
   are usable and not only present.

The recovery time you report runs from the empty environment to the first
Artifact downloaded with a matching checksum. The recovery point is the database
snapshot. State the accepted loss: runs in flight at the snapshot, and anything
written after it.

## Rehearse It On Kind

`./make kind drill restore` rehearses this whole procedure on an ephemeral kind
cluster. It refuses to run anywhere else:

```sh
BUILDMAX_KIND_EPHEMERAL=1 ./make kind up
./make kind drill restore
./make kind down
```

It seeds a Task with a continued run (trace, checkpoint, session bundle, a
published Artifact), an uploaded and shared Artifact, a Space file, a Space
Secret consumed by an Agent, and the audit rows those produce. It fingerprints
them through the API, runs `storage verify --checksums`, quiesces the server,
takes `mysqldump --single-transaction` and then `mc mirror` into
`.local/drill/restore-<time>/` with digests, and saves the KEK and `server.yaml`.
It then deletes the `db`, `storage`, and `buildmax` namespaces (MySQL and MinIO
use `emptyDir`, so their data goes with them), restores the database and bucket
before any server starts, and recreates the server with the original KEK and
`server.yaml`, a new JWT secret, and new worker TLS.

It passes only when `storage verify --checksums` is clean, every table's row
count matches, every backed-up object is present with the same digest, every
fingerprint entry is unchanged, a pre-backup session token is refused, a
Continue restores the backed-up checkpoint and session history, and the Agent's
run still receives the pre-backup Secret value under the restored KEK. It prints
the recovery point, the recovery time, each stage's duration, and anything new
since the backup. Kind is a rehearsal of the procedure, not qualification
evidence for a candidate.

## Related

- [deployment/production/README.md](../../deployment/production/README.md) — the
  dependency contract, the KEK, and upgrades
- [beta-readiness.md](beta-readiness.md) — the restore item a candidate must pass
- [local-kind.md](local-kind.md) — the kind cluster the rehearsal runs on
