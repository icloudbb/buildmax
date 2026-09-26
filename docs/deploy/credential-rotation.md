# Rotating Deployment Credentials

> **简体中文：** [阅读中文镜像](../zh-CN/deploy/credential-rotation.md)
> **Audience:** operators · **Status:** current — rehearsed on kind, not yet on a Beta candidate
This runbook replaces each credential a Kubernetes deployment of BuildMax holds,
one at a time, without rebuilding anything. For every credential it says how the
old and new values overlap, what has to drain before the old one goes, what
users and runs notice, and how to confirm the old value is dead.

The commands assume the [production reference](../../deployment/production/README.md):
namespace `buildmax`, the `buildmax-server` Deployment, and the
`buildmax-secret` and `buildmax-kek` Secrets. If a secret manager (External
Secrets, Vault, sealed-secrets) owns those Secrets, change the value there and
let it sync instead of patching the Secret directly.

## Contents

- [Before You Start](#before-you-start)
- [JWT Secret](#jwt-secret)
- [Database Password](#database-password)
- [Object-Storage Key](#object-storage-key)
- [Model Provider Keys](#model-provider-keys)
- [Key-Encryption Key](#key-encryption-key)
- [Worker API Certificate And CA](#worker-api-certificate-and-ca)
- [OIDC Client Secret, Telegram Bot Token, And Redis Password](#oidc-client-secret-telegram-bot-token-and-redis-password)
- [Measured Effect](#measured-effect)

## Before You Start

The server reads every credential below once, at startup. A changed Secret
therefore takes effect when the server rolls:

```sh
kubectl -n buildmax rollout restart deployment/buildmax-server
kubectl -n buildmax rollout status deployment/buildmax-server
```

The rollout replaces pods one at a time with `maxUnavailable: 0`, so the API
stays up. It is complete when `rollout status` returns and no old server pod is
still terminating (`kubectl -n buildmax get pods -l app=buildmax-server`).

Two credentials — the object-storage key and a direct provider key — are also
handed to each worker Job **by value** when the Job is created. A Job keeps the
key it started with for its whole run, so before retiring one of those keys,
wait until every worker Job created before the roll has finished. This lists the
worker Jobs still running and when each started:

```sh
kubectl -n buildmax get jobs -l app.kubernetes.io/name=buildmax-worker \
  -o jsonpath='{range .items[?(@.status.active)]}{.metadata.name}{"  "}{.metadata.creationTimestamp}{"\n"}{end}'
```

Rotate one credential at a time and verify it before starting the next, so a
failure points at one change.

## JWT Secret

`BUILDMAX_JWT_SECRET` in `buildmax-secret` signs user access tokens, worker run
tokens, and the state of an OIDC sign-in in progress. There is one signing key
at a time, so there is **no overlap**: the new secret is live the moment a new
pod serves, and every token the old one signed stops verifying.

1. Optionally drain. If the runs in flight matter, wait until no worker Job is
   active. Nothing pauses dispatch, so pick a quiet window.
2. Replace the secret and roll:

   ```sh
   kubectl -n buildmax patch secret buildmax-secret --type merge \
     -p "{\"stringData\":{\"BUILDMAX_JWT_SECRET\":\"$(openssl rand -hex 32)\"}}"
   kubectl -n buildmax rollout restart deployment/buildmax-server
   kubectl -n buildmax rollout status deployment/buildmax-server
   ```

What users and runs notice:

- **Signed-in people stay signed in.** Refresh tokens are stored rows, not
  signatures, so they survive. Portal, the CLI, Desktop, and a Remote Control
  session treat the first `401` as a cue to refresh once and retry.
- **Runs in flight are lost.** A worker's run token was signed with the old
  secret, so it can no longer report. Its run is settled `FAILED` with "this run
  lost its worker" once the liveness grace (2 minutes) and the next sweep pass;
  retry it as a new run. The worker Job itself keeps working until the run ends,
  then exits without being able to report its result.
- An OIDC sign-in that was between the redirect and the callback fails and has
  to be started again.
- Rotating the secret signs **nobody** out. To end sessions, revoke them — see
  [authentication.md](authentication.md#the-other-credentials).

Verify: a request with an access token issued before the rotation answers
`401`; a refresh with a refresh token issued before it answers `200`; a new run
succeeds.

## Database Password

MySQL 8.0.14 and later keep two passwords per account, so the new password is
added beside the old one, the server moves to it, and only then is the old one
removed. The server's pooled connections are already authenticated and are not
affected by either change. Workers never connect to the database, so nothing
drains.

1. As an account allowed to alter the BuildMax user, add the new password and
   keep the current one:

   ```sql
   ALTER USER 'buildmax'@'%' IDENTIFIED BY '<new-password>' RETAIN CURRENT PASSWORD;
   ```

2. Put the new password in `BUILDMAX_DATABASE_PASSWORD` and roll:

   ```sh
   kubectl -n buildmax patch secret buildmax-secret --type merge \
     -p '{"stringData":{"BUILDMAX_DATABASE_PASSWORD":"<new-password>"}}'
   kubectl -n buildmax rollout restart deployment/buildmax-server
   kubectl -n buildmax rollout status deployment/buildmax-server
   ```

3. Once no old server pod remains, discard the old password:

   ```sql
   ALTER USER 'buildmax'@'%' DISCARD OLD PASSWORD;
   ```

Expected effect: none. Verify that signing in to MySQL with the old password is
refused (`Access denied`), that the server pods' restart counts did not change,
and that their `/readyz` stayed `200`.

A database service that does not offer dual passwords is rotated with two
accounts instead: create a second account with the same grants, point
`database.user` in the server ConfigMap and `BUILDMAX_DATABASE_PASSWORD` at it,
roll, then drop the first account.

## Object-Storage Key

Skip this section if the server and workers reach the bucket through IRSA,
workload identity, or an instance profile: the platform rotates that identity.

A static key is rotated by overlapping two identities that hold the same bucket
permissions — two access keys on one IAM user, or two MinIO users with the same
policy. Workers hold the key by value, so the old one stays enabled until the
Jobs that carry it are gone.

1. Create the new key with the same access. On AWS, `aws iam create-access-key`
   on the same user; on MinIO:

   ```sh
   mc admin user add <alias> <new-access-key> <new-secret-key>
   mc admin policy attach <alias> <bucket-policy> --user <new-access-key>
   ```

2. Put it in the Secret and roll:

   ```sh
   kubectl -n buildmax patch secret buildmax-secret --type merge -p \
     '{"stringData":{"BUILDMAX_STORAGE_MINIO_ACCESS_KEY":"<new-access-key>","BUILDMAX_STORAGE_MINIO_SECRET_KEY":"<new-secret-key>"}}'
   kubectl -n buildmax rollout restart deployment/buildmax-server
   kubectl -n buildmax rollout status deployment/buildmax-server
   ```

3. Wait until every worker Job created before the roll has finished (see
   [Before You Start](#before-you-start)).
4. Disable the old key — `aws iam update-access-key --status Inactive`, or
   `mc admin user disable <alias> <old-access-key>` — and delete it once you are
   sure nothing still uses it.

Expected effect: none. A run whose worker still held the old key when it was
disabled fails at its next storage write, with a cause naming the refused
write. Verify that a request signed with the old key answers `403`, that an
artifact stored before the rotation still downloads with the same checksum, and
that a new run succeeds.

## Model Provider Keys

The overlap for a provider key is the provider's own: create the new key at the
provider before switching, and revoke the old one only after BuildMax has
stopped using it.

**A managed catalog model** keeps its key sealed in the database. Replace it in
place — the key is read from standard input, not the command line:

```sh
buildmax admin model set-key <model-id>
# or, database-direct from a server pod:
kubectl -n buildmax exec -i deploy/buildmax-server -- buildmax-server model set-key --id <model-id>
```

The change bumps the catalog row's revision, and the gateway uses the new key
from its next call: no restart, and managed workers never held the key. Calls
that were already in progress finish with the old key, so revoke it at the
provider once the model's call timeout has passed. Verify that a call through
the model succeeds and that the provider shows the old key unused.

**A direct model key** (`conversation.model.api_key`, set through
`BUILDMAX_CONVERSATION_MODEL_API_KEY`) is read by the server at startup and
handed by value to workers that call the provider directly. Patch the Secret,
roll the server, wait for the worker Jobs created before the roll to finish,
then revoke the old key at the provider.

## Key-Encryption Key

The KEK in `buildmax-kek` seals managed-model keys and Space Secrets. The key
file holds several keys at once and names the `current` one new writes use, so
rotation adds a key, moves `current`, re-wraps the stored rows, and only then
removes the old key. Each step is a roll, because every server pod has to be
able to open what any other pod writes.

1. Export the file, add a new key, and roll. `current` stays the old key:

   ```sh
   kubectl -n buildmax get secret buildmax-kek -o jsonpath='{.data.kek\.json}' | base64 -d > kek.json
   # add "file:root:2": "<output of: openssl rand -base64 32>" under "keys"
   kubectl -n buildmax create secret generic buildmax-kek --from-file=kek.json=kek.json \
     --dry-run=client -o yaml | kubectl apply -f -
   kubectl -n buildmax rollout restart deployment/buildmax-server
   kubectl -n buildmax rollout status deployment/buildmax-server
   ```

2. Set `"current"` to the new key id, apply the file the same way, and roll.
   Back up this file now — it is the only copy of the new key.
3. Re-wrap every stored row under the new key, then read its report:

   ```sh
   kubectl -n buildmax exec deploy/buildmax-server -- buildmax-server secret rewrap
   ```

   Every key except the current one must show `0 (no row uses it; it can be
   removed from the key file)`. A key still in use means a row was written by a
   pod that had not yet loaded the new `current`; run the command again.
4. Remove the old key from the file, apply it, and roll.

The server refuses to start while a stored row names a key its file does not
hold, so a rollout that does not come up in the last step means a row was left
behind: put the old key back, roll, and rewrap again. Nothing is decrypted or
re-encrypted, and no user or run notices the rotation.

Keep the old key's material with the database backups taken before the rewrap:
those dumps still name it, and a restore of one is unreadable without it. See
[the KEK reference](../reference/configuration.md#the-deployment-key-encryption-key).

## Worker API Certificate And CA

The server serves the worker API with the leaf certificate in the
`buildmax-worker-api-tls` Secret, loaded at startup. Each worker verifies it
against the CA bundle in the `buildmax-worker-api-ca` ConfigMap, which the Job
mounts and reads when it starts.

**A new leaf from the same CA** needs only a roll: replace `tls.crt` and
`tls.key` (cert-manager renews them for you), then roll the server before the
old leaf expires. Running workers still trust the CA, so nothing is lost.

**A new CA** is not overlapped. Write the new CA into `worker-api-ca.crt`,
replace the leaf with one it signed, and roll the server. Workers started before
the change trust only the old CA, cannot reach the server any more, and their
runs are settled `FAILED` like a JWT rotation's; retry them. To avoid the loss,
publish a bundle of the old and new CAs first, wait for the Jobs started before
it to finish, switch the leaf, and drop the old CA later.

## OIDC Client Secret, Telegram Bot Token, And Redis Password

These are not rehearsed by the kind drill; each follows the same
patch-and-roll shape.

| Credential | Overlap | Procedure | Effect |
|---|---|---|---|
| `BUILDMAX_OIDC_CLIENT_SECRET` | Most identity providers, Okta included, allow two active client secrets | Create a second secret at the provider, patch the Secret, roll, then deactivate the old secret at the provider | None expected |
| `BUILDMAX_TELEGRAM_BOT_TOKEN` | None: BotFather's `/revoke` ends the old token at once | Revoke and take the new token, patch the Secret, roll | The bot is silent until the roll completes; Telegram holds unread messages for up to 24 hours and the server reads them afterwards |
| `BUILDMAX_COORDINATION_REDIS_PASSWORD` | Redis 6+ ACL users hold several passwords | `ACL SETUSER <user> >new-password`, patch the Secret, roll, then `ACL SETUSER <user> <old-password` | None expected; established connections stay authenticated |

## Measured Effect

`./make kind drill rotation` rehearses the JWT, database, object-storage,
managed-model, and KEK procedures above on a disposable kind cluster and asserts
each result. It is a rehearsal of the procedure, not qualification: the
[Beta readiness record](beta-readiness.md) needs the same exercise against the
pinned candidate's own MySQL, S3, and ingress. The rehearsal on 2026-09-26
measured:

| Credential | Server roll | Old credential | Disruption |
|---|---|---|---|
| JWT secret | 22s | Old access token and a token newly signed with the old secret: `401`; old refresh token: `200` with a working access token | The run held in flight was settled `FAILED` 3m5s after the Secret changed; a new run succeeded |
| Database password | 24s | Old password: `Access denied` | None: 0 of 86 API reads through the ingress failed, `/readyz` stayed `200`, no pod restarted |
| Object-storage key | 24s | Old key after disable: `403` | None: 0 of 46 API reads failed; a pre-rotation artifact downloaded with the same checksum; a new run succeeded |
| Managed model key | none | Row revision changed; the next call sent the new key | None |
| KEK | 3 rolls, 22–24s each | `rewrap` moved every row; the old key was removed | None: the server restarted cleanly without the old key and the sealed model key still opened |
