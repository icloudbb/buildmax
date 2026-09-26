---
id: backup-restore-drill
title: Rehearse a paired database and bucket restore on kind
roadmap: R2
source: docs/design/verification-program.md
depends_on: [14-kek-in-deployments.md, 18-storage-reference-check.md]
verification: [kind]
claim:
pr:
---

## Outcome

The restore procedure is written, rehearsed, and measured, so the R3 operator can
run it against real dependencies.

## Scope

- `./make kind drill restore` (separate from `kind smoke`, refuses without an
  ephemeral-cluster marker): seed through the API; fingerprint IDs, statuses,
  checksums; quiesce; `mysqldump --single-transaction` then `mc mirror` (database
  first); wipe the db, storage, and buildmax namespaces; restore before the server
  starts (original KEK, new JWT secret); run the task-18 check; re-fingerprint,
  diff, and report RTO and accepted loss.
- A new backup-restore runbook under docs/deploy (+ zh-CN if mirrored):
  what to back up (schema, bucket prefix, KEK, server.yaml), database-first
  ordering and the no-delete window, a separate recovery bucket (the checkpoint
  orphan sweep would delete a live bucket's newer blobs), no Telegram token in
  recovery, and verification. Link it from the production README and
  beta-readiness.

## Acceptance Criteria

- The drill passes end to end on an ephemeral cluster with a measured RTO.
- ROADMAP R2 and current-state record the rehearsal (kind is a rehearsal, not the
  pinned-candidate evidence).

## Verification

The drill on `BUILDMAX_KIND_EPHEMERAL=1`.
