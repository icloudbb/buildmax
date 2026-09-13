---
id: space-secret-audit-events
title: Record space Secret lifecycle actions in the audit trail
roadmap: none
source: docs/design/space-secrets.md#11-audit-and-provenance
depends_on: []
verification: ["./make test ./internal/service/secret", "./make test mysql", "./make e2e kind"]
claim:
---

## Outcome

A space owner can see in the space audit trail that a Secret was created,
disabled, or destroyed — the security-relevant credential events the audit
surface exists for. Today those actions leave no trace, so the one role that can
both manage secrets and read the trail cannot tell from it that a credential
entered or left the space, while structurally identical actions (webhook keys,
plugin activation, model catalog changes, artifact share links) are all
recorded.

## Scope

Add the space-scoped Secret audit actions the design already specifies and emit
them where the secret lifecycle is owned.

- `docs/design/space-secrets.md` §11 enumerates `secret.created`,
  `secret.rotated`, `secret.disabled`, `secret.destroyed`,
  `secret.consumption_changed`, `secret.materialized`, `secret.revoked`, and
  `secret.access_denied`. Only the run-level `task_run_secret` snapshot (the
  `secret.materialized` equivalent, `RecordEnvGrant` on the worker route) ships
  today.
- `internal/core/audit/audit.go` has no `secret.*` action constant; add the
  space-lifecycle ones (at minimum `secret.created`, `secret.disabled`,
  `secret.destroyed` — the owner-visible lifecycle confirmed missing), each with
  the same rationale comment style as the neighbouring credential/capability
  actions.
- Emit them from where the secret lifecycle is owned — `internal/service/secret`
  (`Create`, item edit, state change, destroy) or the handlers in
  `internal/server/handlers/space/secrets.go`, matching where sibling actions
  (plugin activation, model catalog) record. The event carries actor, space,
  secret public ID, and bounded non-sensitive detail only — never item names'
  values, ciphertext, or a hash, per §11.
- Reconcile the design record: if create/disable/destroy ship now, §18 Phase 1's
  "done" list should say so; if any §11 action is deliberately deferred, §11 or
  §18 must state that rather than listing it as part of the model.

## Out Of Scope

The consumption/materialization/revocation/access-denied variants beyond the
create/disable/destroy lifecycle, unless they fall out naturally; the run-level
`task_run_secret` snapshot, which already ships. No change to secret storage,
encryption, or delivery.

## Acceptance Criteria

- Creating a Secret writes a `secret.created` event visible in the space audit
  trail (Portal Space settings → Audit) with the secret as target and no
  sensitive detail.
- Disabling and destroying a Secret each write their event.
- The design record (§11 and §18 Phase 1) and the code agree on which secret
  audit actions ship.
- No secret value, ciphertext, or hash appears in any audit event.

## Verification

`./make test ./internal/service/secret` (and the audit package) for the emit
wiring and the no-sensitive-detail assertion; `./make test mysql` if the audit
row write touches `internal/infra/db`; `./make e2e kind` to confirm the event
appears in the deployed Space settings → Audit view after a create.

## Notes

Confirmed 2026-09-13 on `main` cceba61c (still current at origin/main 560690b3;
none of the cited files changed between them) against an ephemeral kind cluster
as alice: created Secret `qa-probe-secret` in a space; Space settings → Audit
showed only the fixture `workflow.created`/`agent.created` entries and no secret
event, before and after reload. `internal/service/secret/service.go` has zero
audit references and `createSecretHandler` invokes no recorder, so the gap is in
code, not just the view. Comparable actions that are recorded:
`webhook_key.created` ("a webhook key admits work under its owner's identity"),
`plugin.activated` ("why did this run have this capability"), `llm_model.created`,
`artifact.share_created` — a Secret grant is at least as security-relevant, as
the Portal Secrets page's own warning ("an agent you grant a secret to can read
its value") states.
