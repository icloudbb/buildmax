---
id: portal-members-and-invitations
title: Name pending invitees, confirm role elevation, and keep every Space settings tab reachable
roadmap: R6
source: docs/design/ui-experience-program.md#phases
depends_on: []
verification: ["./make test", "./make check portal", "./make e2e visual", "./make e2e kind"]
claim:
pr:
---

## Outcome

A Space owner can tell which pending invitation belongs to whom, and is told
in plain words when an email has no account. Raising a member's role takes a
deliberate step and says what changed. Every Space settings tab is reachable
on a phone. This task carries Portal audit findings P4 (Major), P13, P14, and
P20.

## Findings

**P4 (Major, state legibility). Pending invitations are identified only by an
opaque ID.**

- Screen: Space settings → Members.
- Reproduction: **Invite** `nora@buildmax.local` as Member.
- Actual: the new row reads `3n4sj7muxl32i3ejxscq — Invited as member, expires
  10/10/2026, 06:09:40`, beside an older `q6p6c6vl2454y2xv547q`. Neither row
  names the invitee, so the owner cannot tell which one to **Revoke**.
- Expected: the invitee's email or name.

**P13 (Minor). The unknown-email invite error is written for operators.**

- Reproduction: **Invite** an email that has no account.
- Actual: the error reads "…ask a system administrator to create one (POST
  /api/admin/users or buildmax-server user create), then invite it".

**P14 (Minor). A role change applies from a select with no confirmation or
saved feedback.**

- Reproduction: change a member (Carol, in the fixture Space) from Member to
  Admin with the role select.
- Actual: the change applies at once and was still set after a reload. A
  mistaken selection silently grants Admin.

**P20 (Minor). Space settings tabs are hidden at 390 px.**

- Actual: a horizontally scrolling pill shows 3 of the 8 tabs, with no scroll
  affordance.

## Verified Facts

These were checked on `main` at `e36fa722`. `invitationResponse` in
`internal/server/handlers/space/spaces.go` carries `id`, `space_id`,
`user_id`, `role`, `invited_by`, `expires_at`, and `created_at`, but no
invitee identity. Only Space admins may list invitations
(`space_authz_matrix_test.go`). P4 therefore needs a server change.

## Scope

- Add the invitee's email and display name to the invitation response for the
  roles that may already list invitations. The inviter supplied that email, so
  this reveals nothing they could not already learn by inviting. Update
  `openapi.json` and the handler and authorization tests. Show the name and
  email on the row, with the ID as secondary metadata.
- Recognize the unknown-account condition, through a stable error code if the
  server lacks one, and show person-facing copy in both locales: no account
  uses this email, so ask a system administrator to create one, then invite
  it. Keep API routes and CLI commands out of the primary text.
- Make a role change that raises authority ask for confirmation, naming the
  member and the new role. After any role change, give saved feedback that
  names the member and the role. Follow the mutation feedback model in the
  [state and permission feedback record](../design/portal-state-and-permission-feedback.md#mutation-feedback).
  Use task 16's `Toast` if it has landed.
- At 390 px, make every settings tab reachable without hidden horizontal
  scrolling, for example by wrapping or with a select.

## Out Of Scope

- Who may invite or change roles.
- Creating accounts from an invitation, which is deliberately left unbuilt.

## Acceptance Criteria

- After inviting `nora@buildmax.local`, the pending row shows her email or
  name, and **Revoke** names her.
- The unknown-email error contains no API path or command.
- Raising a member to Admin needs a confirmation. Every role change shows a
  saved message that names the member and the role.
- At 390 px all 8 settings tabs are visible or plainly reachable.

## Verification

1. `./make test` for the invitation response and its authorization.
2. `./make check portal`, then `./make e2e visual`. The settings template
   baseline may change on purpose.
3. Use the kind loop, then `./make e2e kind`.

## Notes

Audit context: the phase 0 Portal audit ran on 2026-10-06 against `main` at
`e98efc7a`, on an ephemeral kind cluster with the mock model and
`./make kind fixtures --runs`. The role change was reverted afterwards. The
operator was an Agent with repository knowledge. The full report is kept in
history at
[`718a6ab3`](https://github.com/icloudbb/buildmax/blob/718a6ab3969d35bdba7f66013d8ccfefe7549af8/docs/contribute/exploratory-runs/2026-10-06-portal-ui-journey-audit.md).
