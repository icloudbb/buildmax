---
id: service-accounts
title: Add Space-owned service accounts as a non-human user kind
roadmap: R5
source: docs/design/space-assistants.md#6-service-accounts
depends_on: []
verification: ["./make test", "./make test mysql", "./make check portal", "./make e2e kind", "./make check docs"]
claim: gougoujiang 2026-10-04
pr:
---

## Outcome

A Space can own a non-human principal that work runs as, accountable through a
human sponsor and independent of any one person staying. Space Assistants run
as one; without it their work would carry a named employee's authority.

## Scope

- `user.kind` (`human` default, `service`) and a nullable `sponsor_user_id`.
  Service accounts have no email (make the column nullable for that kind; the
  unique index allows several NULLs), no password, and no external identity.
- Every sign-in path refuses `kind = service`: login codes, password, OIDC/JIT
  linking, refresh, and chat pairing confirmation. Admin account operations
  that assume a human (invite, password reset, login-code issue) refuse it.
- A service account is a `member` of exactly one team Space; membership APIs
  refuse adding it to another Space, promoting it, or adding one to a personal
  Space.
- Space API and Portal (Space settings): owners and admins create, rename,
  disable, re-enable, and re-sponsor service accounts; each action is audited.
  The sponsor must be an owner or admin of the Space.
- Sponsor departure: when the sponsor stops being owner/admin of the Space or is
  disabled, mark the service account as needing a sponsor (a derived state is
  fine). Assistants consult it in a later task; nothing else stops.
- Administration user list marks service accounts and lets an administrator
  disable one; people pickers and invitations exclude them.
- OpenAPI updated for every new or changed route.

## Out Of Scope

- Using a service account as a Task authority from anywhere but tests; the
  Assistant front door does that (66-assistant-front-door-turn.md).
- Schedules or webhooks running as a service account.
- Any credential for a service account (PAT, API key, workload identity).

## Acceptance Criteria

- A Task admitted with `created_by` = an active service account passes
  admission, dispatch, first fetch, and reconcile eligibility; a disabled one
  is refused at each point, like a disabled human.
- Each sign-in path returns its ordinary refusal for a service account, with a
  test per path.
- Membership refusals above are tested, including cross-Space isolation.
- Sponsor-needed state appears when the sponsor is demoted, removed, or
  disabled, and clears when an owner/admin takes sponsorship.
- Portal: Space settings lists, creates, disables, and re-sponsors service
  accounts; a browser test covers create and disable.
- `docs/design/enterprise-identity-and-access.md` non-goals and
  `docs/design/system-administration.md` (and zh mirrors) reflect that service
  accounts exist without credentials; manual Space settings page updated.

## Verification

`./make test` on identity, space, eligibility, and handler packages first, then
`./make test`; `./make test mysql` for the schema change; `./make check portal`
and `./make e2e kind` for the settings page; `./make check docs`.

## Notes

Being a `user` row is the point: `created_by`, eligibility, membership, quota
attribution, and audit actors all key on user id and stay unchanged. See the
design's §6.1 for why the sponsor, a chosen user, and the Assistant itself were
rejected as the authority.
