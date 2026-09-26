# Space Membership Lifecycle

> **简体中文：** [阅读中文镜像](../zh-CN/design/Space成员生命周期.md)

## Contents

- [Status](#status)
- [1. Decision](#1-decision)
- [2. Product Goal](#2-product-goal)
- [3. Current Baseline](#3-current-baseline)
- [4. Main Gaps](#4-main-gaps)
- [5. In Scope](#5-in-scope)
- [6. Out Of Scope](#6-out-of-scope)
- [7. Permission Matrix Additions](#7-permission-matrix-additions)
- [8. Backend Plan](#8-backend-plan)
- [9. Frontend Plan](#9-frontend-plan)
- [10. Validation](#10-validation)
- [11. Risks](#11-risks)
- [12. Open Questions](#12-open-questions)
- [13. Recommended First PR](#13-recommended-first-pr)

## Status

- roadmap_priority: implemented foundation for roadmap R3 candidate qualification
- status: `implemented` — §5.1 (invitation), §5.2 (role change), §5.3
  (ownership transfer), and §5.4 (space-scoped access recovery) are shipped
  end to end: `internal/core/space`, `internal/service/space`,
  `internal/server/handlers/space`, `internal/infra/db`, and §9's Portal
  surfaces (Space → Members: invite, pending list, role selector, transfer
  confirmation, login code; Account → Invitations: list and accept). All
  four §12 open questions are decided. §5.5 records removal effects and
  disabled-owner recovery, shipped with the account deactivation lifecycle
- follows: [space-governance.md](./space-governance.md),
  [system-administration.md](./system-administration.md)
- roadmap: [../ROADMAP.md](../ROADMAP.md)
- created_at: `2026-08-30`

## 1. Decision

**BuildMax does not support self-service signup.** `allow_signup` stays an
opt-in escape hatch for a small or trusted deployment, and the code already
states why it must default closed: nothing verifies that whoever typed an
address controls it, so open registration on a reachable server is how
somebody claims a colleague's address
(`internal/service/identity/account.go`, `ErrSignupClosed`). This document does
not revisit that decision, and it adds nothing that requires an outbound mail
channel — BuildMax has none, by design (`internal/core/identity/login_code.go`).

**Account existence and space membership are two different authorities, and
this document keeps them that way rather than blurring them into one
invitation flow.**

- **Creating an account is a deployment identity concern.** An operator can use
  `POST /api/admin/users` / `buildmax-server user create`, per
  [system-administration.md](./system-administration.md), and an OIDC deployment
  can use the bounded JIT path in
  [enterprise identity and access](./enterprise-identity-and-access.md). This
  document adds neither path. A space-scoped call that could also mint an
  account would create another place that decides who gets to exist in the
  deployment and would eventually drift from the identity service's rules
  (domain policy, personal Space, disablement, audit) — exactly what
  [AGENTS.md](../../AGENTS.md)'s ownership-boundary rule exists to prevent.
- **Bringing an existing account into a space is the space owner's (or admin's)
  job.** That is what "invitation" means in the rest of this document: an
  owner or admin who wants someone in their space asks for them by email, and
  the person is added — pending their acceptance, unlike today — if and only
  if that email already has an account.

The consequence stated plainly: **a space owner who wants to bring in someone
who has never used BuildMax cannot do it alone.** An operator creates the
account first, or an enabled OIDC JIT policy creates it on that person's first
successful corporate login; only then can the owner invite the resulting
address. This document treats that separation as the correct shape, not as
unfinished work — see §4.1 and §6 for why, and for what closing it inside the
Space boundary would have cost.

What the split does buy back is real: because a space-scoped invitation can
never create an account, it also never has to decide what credential to hand
whoever holds one — the mechanism this document's first draft needed to
prevent a space owner from minting a login for a stranger's account is not
needed at all once account creation is somebody else's job. Simpler and
harder to get wrong beat one fewer manual step.

The shipped OIDC JIT path now validates that boundary. It is a second,
IdP-driven way to create an account on first assertion, standing beside the
operator's manual path rather than replacing it. Because §5.1 only asks "does
this email already have an account" and never asks "who created it" or "how,"
OIDC shipped without changing this lifecycle: an SSO-provisioned account is
invitable the moment it exists, the same as an operator-created account. A
design that let Space invitation create accounts would instead need a third
opinion about identity provisioning; keeping account and membership authority
apart avoids that entirely.

Given that split, three things are still broken:

- **"Adding" an existing user is instant and unconfirmed.** `AddMember`
  refuses only when the email does not resolve
  (`internal/service/space/service.go:111`, `ErrUserDoesNotExist`); when it
  does, the person is added immediately, with no pending state, no
  acceptance, and no chance to decline. An owner who mistypes an address
  silently grants a stranger's account space access.
- A member's role cannot change without deleting and re-adding them, which
  loses `created_at` and produces a `space.member_removed` /
  `space.member_added` pair that reads as a departure and a return, not a
  promotion.
- Ownership cannot move to another member, so a space is permanently bound to
  whoever created it, and a locked-out member depends on a `system_admin`
  grant existing somewhere in the deployment even when their own space's owner
  would be the obvious person to help.

This document designs those three journeys — invitation (bounded to existing
accounts), role change, ownership transfer — plus member-scoped access
recovery, as small, boring, single-implementation extensions of
`internal/core/space`, not as a new subsystem. Space-level *approval workflows*
(someone else must sign off before a sensitive action executes) stay out of
scope, per [space-governance.md](./space-governance.md) §6 — that decision is
not reopened here.

## 2. Product Goal

A space owner should be able to run their space's membership day to day without
depending on a `system_admin` for anything except bringing in someone who has
truly never touched BuildMax before:

- invite an existing account into the space, with the person able to see it
  coming and decline it
- correct a role without erasing history
- hand the space to someone else when they leave
- unblock a locked-out member of their own space

And every one of those four actions should leave the same kind of trace the
audit trail already keeps for adding and removing a member — no new visible
inconsistency where some membership changes are recorded and others are not.

## 3. Current Baseline

Backend anchors:

- roles and the membership store contract in `internal/core/space/space.go`
- the role/action decision in `internal/core/space/policy.go`
- membership commands in `internal/service/space/service.go`:
  `AddMember` (member role only, target must already have an account, adds
  instantly), `RemoveMember` (owner cannot remove themselves)
- space HTTP routes in `internal/server/handlers/space/spaces.go`
- account creation and credential issuance, entirely `system_admin`-scoped:
  `POST /api/admin/users`, `POST /api/admin/users/{user_id}/login-code`
  (`internal/server/handlers/admin/admin_users.go`), and the single-use code
  primitive behind the second one,
  `internal/core/identity/login_code.go`
- the account-disablement and lockout-recovery design in
  [system-administration.md](./system-administration.md) §6 and §8, which
  this document does not duplicate — it is the answer for a `system_admin`
  acting deployment-wide, not for a space owner acting inside their own space
- the audit trail and its action vocabulary in `internal/core/audit/audit.go`
  (`SpaceMemberAdded`, `SpaceMemberRemoved`, and the naming pattern they set)

Current action model (`internal/core/space/policy.go`):

- `ActionManageSpaceMembers` — owner only. Covers add and remove today; there is
  no `Action` yet for changing a role or transferring ownership.
- No action exists for issuing a login code, because the capability itself
  does not exist below the deployment scope.

**A user may belong to more than one space, concurrently, and this is the
common case, not an edge case.** `space_member` has a unique index on
`(space_id, user_id)` only — none on `user_id` alone
(`internal/infra/db/space.go:53-54`). `CreateUser` gives every account exactly
one personal space (`personal_for_user_id` is the column carrying a unique
index, `internal/infra/db/user.go:194-216`), and a user can additionally own or
be added to any number of ordinary spaces; `ListSpacesByUser` returns all of
them, and Portal's `SpaceContext`
(`portal/src/contexts/SpaceContext.tsx`) is a working space switcher, not a
placeholder. Nothing in §5 needs to special-case an invitee who already
belongs elsewhere: accepting an invitation is one more `space_member` row,
never a conflict, and the only membership that stays exactly one per user is
the personal space, which no path in this document creates, removes, or
transfers. It also means a single account can hold several pending
invitations from unrelated spaces at once, each accepted or declined on its
own — the accept flow in §5.1 is written against a caller's whole list for
exactly this reason.

## 4. Main Gaps

### 4.1 No Invitation Path For Someone Without An Account

`AddMember` requires `s.Users.UserByEmail` to resolve
(`internal/service/space/service.go:107-113`). A space owner cannot onboard
anyone who has not already been created by a `system_admin`. This is real
friction in a deployment with few `system_admin` grants, and §1 explains why
this document accepts it rather than closing it — see §6 for what closing it
would have cost.

### 4.2 Adding An Existing User Is Instant, Not An Invitation

Calling it `AddMember` is accurate: it adds. There is no pending state, no
acceptance, and no way for the person being added to see it coming or decline
it. An audit entry exists after the fact (`SpaceMemberAdded`), but nothing before
it. This is the gap §5.1 closes.

### 4.3 No Role Change

`Allows` (`internal/core/space/policy.go`) distinguishes `owner`, `admin`, and
`member`, but nothing in `internal/service/space` can move a member between
them. The only route to a role change is remove-then-re-add, which:

- requires the target to still have an account and still be willing to be
  invited back
- is two audit events that read as a departure and a fresh join, not a
  promotion
- briefly leaves the space with no record of the person at all

### 4.4 No Ownership Transfer

`RemoveMember` refuses an owner removing themselves
(`ErrCannotRemoveSelf`), which is correct — but there is no route to the
situation that check exists to prevent: an owner who is leaving with nobody
else able to take over. Today the only way to change who owns a space is
direct database access, which is exactly the class of operation
[system-administration.md](./system-administration.md) §6 exists to make
unnecessary for account grants, and does not yet cover for space ownership.

### 4.5 Access Recovery Is Deployment-Scoped Only

`LoginCodeStore.CreateLoginCode` is reachable only through
`POST /api/admin/users/{user_id}/login-code`, which requires `system_admin`
(`internal/server/handlers/admin/admin_users.go:200`). A space owner watching a
locked-out spacemate has no path of their own — they must find a
`system_admin`, who may not exist in a small deployment beyond whoever ran the
bootstrap command once.

## 5. In Scope

### 5.1 Space-Scoped Invitation

Add `POST /api/spaces/{space_id}/invitations`, taking an email and an optional
role (member or admin — never owner; see §5.2 for why ownership moves through
a separate, explicit action). It **replaces**
`POST /api/spaces/{space_id}/members`, the current instant-add route, rather
than living beside it — per [AGENTS.md](../../AGENTS.md), Alpha means fixing
a wrong shape everywhere at once, and two routes that both add a member would
be exactly the duplicated authority §1 argues against, just one layer down.

Authorization is a new `ActionInviteSpaceMember`, not a reuse of
`ActionManageSpaceMembers`, because the two callers differ: **owner may invite
at `member` or `admin`; admin may invite only at `member`.** An admin who
could invite another admin could staff the space with peers who outrank the
one thing membership management still reserves to owner — role change and
ownership transfer stay `ActionManageSpaceMembers` / `ActionChangeMemberRole`,
both still owner-only, so this does not let an admin build a path around
those.

Behavior:

- The email must resolve to an existing account
  (`coreidentity.UserStore.UserByEmail`), exactly as `AddMember` requires
  today. When it does not, the call is refused with a new
  `ErrInviteeAccountRequired`, whose message says what to do next — ask a
  `system_admin` to create the account (`POST /api/admin/users` /
  `buildmax-server user create`), then invite the resulting address. This is
  §4.1, accepted rather than closed; see §1.
- When it resolves, and the account is not already a member, the call
  creates a **pending** `space_invitation` row and nothing else — no
  credential, no session, no account-level side effect of any kind. This is
  deliberate: an earlier draft of this section had every invitation able to
  create an account and mint a login credential for it, which would have let
  a space owner obtain a working login for *any* address in the deployment
  simply by "inviting" it, existing account or not. Bounding invitation to
  accounts that already exist — decided in §1 — removes that risk by
  construction rather than by a rule this section would otherwise have to
  keep enforcing.
- The pending row expires after `InvitationTTLDefault = 72 * time.Hour` if
  nobody accepts it. This is a property of the offer, not of any credential —
  nothing in this flow issues one — three days rather than a shorter window
  because an invitation is issued to be acted on whenever the recipient next
  opens Portal, not inside the same exchange it was sent in.
- Discovery is entirely in-app: `GET /api/invitations` (authenticated, no
  space parameter — it answers "what is pending for *me*") lists what the
  caller may accept, using whatever session they reached on their own —
  their password, or a login code someone with standing already gave them.
  BuildMax has no mail channel and this flow needs none: there is nothing to
  deliver out of band, because the invitee already has a way in. An owner who
  wants the person to notice sooner can still say so however the space
  already talks; the product does not depend on it.
- `POST /api/invitations/{id}/accept` activates one. It takes no code —
  identity was already established getting the session, so accepting is
  authorized by "this is my own pending row," not by proving anything a
  second time.
- Revoking a pending invitation (`DELETE /api/spaces/{space_id}/invitations/{id}`)
  is `ActionInviteSpaceMember` too — whoever could send it may withdraw it.

This closes §4.2 without reopening the account-creation question §1 already
settled, and without ever letting one space's invitation become a way to reach
an account another space, or no space, already had a claim on.

### 5.2 Role Promotion And Demotion

Add `ActionChangeMemberRole` to `internal/core/space/policy.go`, owner-only,
and `PATCH /api/spaces/{space_id}/members/{user_id}` taking a target role.

Rules:

- Owner may set a member to `admin` or `member`, and `admin` to `owner` or
  `member`.
- Setting a target to `owner` demotes the caller to `admin` in the same
  transaction — see §5.3, this *is* ownership transfer, exposed as one
  endpoint rather than two, because "promote someone to owner while I stay
  owner too" is not a state this document defines a meaning for.
- The last owner cannot demote themselves without transferring first. This is
  the space-scoped version of the rule
  [system-administration.md](./system-administration.md) §6 applies to the
  last `system_admin` grant: the **API** refuses to leave a space with none,
  the same way it refuses to leave a deployment with none.

### 5.3 Ownership Transfer

Not a separate endpoint — §5.2's `PATCH` with `role: owner` targeting a
current admin or member is the whole mechanism. This section exists to record
the decision the endpoint makes: transfer is **unilateral and immediate**, not
subject to the receiving member's acceptance.

That is a deliberate choice, decided rather than deferred: it matches how
`AddMember` already works today (an owner's action, not a two-party
handshake), and it avoids building a second pending-state mechanism alongside
§5.1's invitation in the same document. It is reversible: the new owner can
transfer back, or demote the former owner, exactly as any owner could do to
any other admin. See open question 1 for the record of why this was decided
rather than left open.

### 5.4 Space-Scoped Access Recovery

Add `POST /api/spaces/{space_id}/members/{user_id}/login-code`, owner-only,
authorized by `ActionManageSpaceMembers` — no new `Action` needed, since
helping a member back in is a membership-management act like adding or
removing one. It calls the same `LoginCodeStore.CreateLoginCode` the
deployment-scoped admin route already uses, after checking the target is a
member of the caller's space.

This does not replace
[system-administration.md](./system-administration.md)'s `system_admin`
route — that one still exists, still works deployment-wide, and is what
recovers an owner who has no co-owner and no admin left in their own space.
This route only removes the dependency on a `system_admin` existing at all
for the common case of one member locked out of an otherwise healthy space.

It is also the one place in this document a login code is issued at all — a
narrower, deliberately re-drawn boundary from §5.1's first draft: here the
target is already a known member of the caller's own space, so there is no
question of an owner minting a credential for a stranger's account.

### 5.5 Removal Effects And Owner Recovery

Removing a member hard-deletes their one `space_member` row. A departure's
provenance lives in the `space.member_removed` audit event, not in a retained
or soft-deleted row, which would also collide with the unique index on
`(space_id, user_id)`. A later invitation is a fresh join with a new
`created_at`; it restores nothing and resurrects none of the member's paused
Schedules or canceled runs. This is the destructive axis of the two described
in [system administration](system-administration.md) §8; account disablement is
the reversible one and keeps every membership row.

Removal withdraws the person's authority to drive work in that Space, including
work started by a durable Schedule or Workflow rather than a request:

- their enabled Schedules in the Space pause with
  `pause_reason = creator_not_member` at the next due time;
- their pending and running TaskRuns there end `CANCELED` with
  `cancel_reason = creator_not_member`, through the dispatch and worker-fetch
  gates or the eligibility reconciler's next sweep;
- their work and access in other Spaces are untouched; and
- Tasks, results, Artifacts, traces, and audit records stay in the Space, and
  remaining members keep reading them.

Removal triggers no cleanup of its own; the gates and the one-minute
reconciler converge on it. The execution-eligibility rule, its checkpoints, and
its gaps are in [system administration](system-administration.md) §8.2.

Ownership normally moves by §5.3 before an owner leaves. When every recorded
owner of a shared Space has been disabled instead, no owner can make that move,
so a System Administrator may recover the Space under narrow conditions: every
recorded owner is disabled, the successor is already an enabled member, the
Space is not personal, and no membership is created. The action reuses §5.3's
transfer, grants the administrator no content access, and records
`space.ownership_recovered`. It is reachable through
`PUT /api/admin/spaces/{space_id}/owner`, Portal's "Make owner" in
Administration → Spaces, and the break-glass
`buildmax-server space recover-owner <space_id> <successor_email>` for when the
public Server or the IdP is unavailable. It never transfers a Space an owner can
still sign in to. See [system administration](system-administration.md) §8.4.

## 6. Out Of Scope

- **Space-initiated account creation.** §1's central decision: an invitation
  never creates an account, regardless of how much friction that leaves for
  onboarding someone who has never touched BuildMax. The alternative —
  letting a space-scoped call create accounts — was drafted and rejected here
  specifically because it could not avoid also deciding what credential to
  hand back, and every answer to that question either handed a space owner a
  working login for an arbitrary address or reinvented account-claim
  semantics `system_admin` already owns. A `system_admin` who also owns the
  space they are onboarding into already has both grants and can do both
  steps back to back; that is the accepted path for a single self-hosted
  deployment operator, not a gap.
- **Space approval workflows.** Decided out of scope by
  [space-governance.md](./space-governance.md) §6; not reopened here. A
  sensitive-action approval loop (someone else must confirm before an owner's
  action takes effect) is a different, larger design than a lifecycle for
  actions an owner is already trusted to take alone.
- **Ownership transfer requiring the target's acceptance.** Decided against —
  see open question 1 for the reasoning kept for the record.
- **Any out-of-band delivery mechanism for a space invitation.** §5.1 needs
  none — an invitation targets an account that can already authenticate on
  its own, so there is nothing to hand over. This is different from, and
  should not be confused with, `system_admin`'s login-code delivery in
  [system-administration.md](./system-administration.md), which still
  applies whenever an account is created and still requires an operator to
  deliver a code out of band.
- **Bulk invitation, CSV import, or invitation-driven account creation.** No
  evidence of demand yet; build from an observed deployment's need, not
  speculatively — the same restraint
  [space-governance.md](./space-governance.md) §11 states for custom roles
  applies here. Deployment-scoped OIDC JIT provisioning now ships separately;
  §1 records why it required no change to this lifecycle: §5.1 never looks
  past "does this account exist."
- **Custom roles, or any role beyond owner/admin/member.** Unchanged from
  [space-governance.md](./space-governance.md) §6.
- **Cross-space invitation acceptance UI beyond the minimum.** Portal work here
  is scoped to what §9 lists; a richer invitation inbox is a follow-up if
  spaces end up with several invitations outstanding at once.

## 7. Permission Matrix Additions

Extends the matrix in [space-governance.md](./space-governance.md) §7:

| Action | Owner | Admin | Member |
|---|---:|---:|---:|
| Invite an existing account at `member` role | yes | yes | no |
| Invite an existing account at `admin` role | yes | no | no |
| Revoke a pending invitation | yes | yes (`ActionInviteSpaceMember`) | no |
| Accept own invitation | — (any authenticated invitee) | — | — |
| Change a member's role | yes | no | no |
| Transfer ownership | yes | no | no |
| Issue a login code for a space member | yes | no | no |

Invitation is the one membership action admin holds, and only at `member` —
resolving open question 2. Role change, ownership transfer, and issuing a
login code stay owner-only, consistent with `ActionManageSpaceMembers` already
being owner-only for add and remove: none of those three lets an admin change
who has more power than they do, which inviting a fellow admin would.

## 8. Backend Plan

### M1. Invitation Store

- `Invitation` type and store methods in `internal/core/space`: create pending
  (against a resolved user id), list pending for a space, list pending for a
  user (backs `GET /api/invitations`), accept by id, revoke by id.
- New table, following `docs/contribute/architecture/data-model.md`'s rules
  for a new `xxxRow`: singular name (`space_invitation`), holding space id,
  invited user id, role, inviter, status, `created_at`, and the expiry
  `InvitationTTLDefault` computes from it. No code or code hash — §5.1 never
  issues one.
- `ErrInviteeAccountRequired` in `internal/service/space`, returned when
  `UserByEmail` finds nothing, naming the `system_admin` path forward rather
  than reusing the bare `ErrUserDoesNotExist` message `AddMember` uses today.
- `InvitationTTLDefault = 72 * time.Hour` in `internal/core/space`, beside
  `Invitation` — a space-membership decision, not a property of any identity
  primitive, since nothing here touches `LoginCodeStore`.
- `POST /api/spaces/{space_id}/members` and `AddMember` are removed, not
  deprecated, per §5.1 — replaced outright by the invitation routes below.

### M2. Invitation Routes

```text
POST   /api/spaces/{space_id}/invitations      owner or admin, member role only for admin — §5.1
GET    /api/spaces/{space_id}/invitations      owner or admin, list this space's pending invitations
DELETE /api/spaces/{space_id}/invitations/{id} owner or admin, revoke before acceptance
GET    /api/invitations                      authenticated, lists the caller's own pending invitations
POST   /api/invitations/{id}/accept          authenticated, no code — §5.1 explains why
```

### M3. Role Change And Ownership Transfer

- `ActionChangeMemberRole` in `internal/core/space/policy.go`.
- `SetMemberRole` in `internal/service/space/service.go`, with the last-owner
  guard from §5.2.
- `PATCH /api/spaces/{space_id}/members/{user_id}` in
  `internal/server/handlers/space/spaces.go`.
- New audit actions: `space.member_invited`, `space.invitation_accepted`,
  `space.invitation_revoked`, `space.invitation_expired`,
  `space.member_role_changed`, `space.ownership_transferred` — following the
  `SpaceMemberAdded` / `SpaceMemberRemoved` naming already in
  `internal/core/audit/audit.go`. Transfer gets its own action distinct from a
  role change even though §5.3 implements it as one call, because an
  investigation asking "did ownership ever move" should not have to infer it
  from two `member_role_changed` rows.
- `space.invitation_expired` departs from the pattern
  [space-governance.md](./space-governance.md) §5.4 sets for a failed login — a
  failed login is silent because it says nothing about who the actor was, but
  an invitation names a specific, already-resolved account before anyone acts
  on it, so its outcome is worth recording either way. It is written lazily,
  the moment an accept is attempted against a row already past
  `InvitationTTLDefault`, rather than by a sweep scanning for rows that timed
  out unattended — an invitation nobody ever tried to accept produces no
  event, the same way an unopened door makes no sound.

### M4. Space-Scoped Login Code

- `IssueMemberLoginCode` in `internal/service/space/service.go`, checking
  target membership and calling `LoginCodeStore.CreateLoginCode`.
- `POST /api/spaces/{space_id}/members/{user_id}/login-code`.
- Audit action `space.member_login_code_issued`, distinct from the existing
  `user.login_code_issued` so a reader of the space's own trail (owner-only,
  per [space-governance.md](./space-governance.md) §5.5) sees it without
  needing `system_admin` visibility into the deployment-wide trail.

## 9. Frontend Plan

### M1. Invite Flow

In space settings member list, replace the current instant "Add member" form
with "Invite": same email-and-role input, but a failed lookup now shows the
`ErrInviteeAccountRequired` message in place rather than a bare validation
error — telling the owner or admin exactly who to ask
(a `system_admin`) rather than leaving them to guess why nothing happened.
There is nothing to copy or deliver on success; the row simply moves to a
pending-invitations section with revoke.

A separate "Invitations" surface (from `GET /api/invitations`) shows what the
signed-in user has been invited to and lets them accept or ignore each one.

### M2. Role Change UI

Replace the implicit "remove and re-add to change role" workaround with a
role selector on each member row, owner-only, disabled with explanatory text
for anyone else — following the existing disabled-state pattern from
[space-governance.md](./space-governance.md) §5.3/§9.

### M3. Ownership Transfer Confirmation

Even though §5.3 makes transfer unilateral on the backend, the UI puts a
distinct, harder-to-misclick confirmation in front of "make them owner"
specifically, separate from the ordinary role dropdown — the backend
irreversibility-by-immediate-effect is a reason for more UI friction here, not
less.

### M4. Member Login Code

A per-member "issue login code" action next to remove, visible to the owner
only, using the same one-time reveal pattern as the admin surface.

## 10. Validation

Backend:

```sh
./make test ./internal/core/space ./internal/service/space ./internal/server/handlers/space ./internal/infra/db
```

Frontend:

```sh
cd portal && npm run build
```

Full:

```sh
./make test
```

Manual scenarios:

1. Owner invites an email with no existing account; refused with
   `ErrInviteeAccountRequired`, naming the `system_admin` path, and nothing
   is created.
2. A `system_admin` creates that account and issues a login code separately;
   the owner then invites the same email successfully, producing a pending
   invitation and nothing else.
3. Invitee logs in with their own credential (password, or the code from
   scenario 2) and sees the pending invitation at `GET /api/invitations`;
   accepting activates membership.
4. The same not-yet-accepted email is invited by a second, unrelated space;
   the invitee's next login shows both pending invitations and can accept
   either independently.
5. Owner revokes a pending invitation before it is accepted; it no longer
   appears in `GET /api/invitations` for that user.
6. A pending invitation left untouched past `InvitationTTLDefault`; the next
   accept attempt against it is refused and `space.invitation_expired` is
   recorded.
7. Owner changes a member to admin, then back to member; one audit row per
   change, no `member_removed`/`member_added` pair.
8. Owner transfers ownership to an admin; caller becomes admin, target becomes
   owner, `space.ownership_transferred` recorded, and the new owner can
   immediately reverse it.
9. Sole owner cannot demote themselves without transferring first.
10. Owner issues a login code for a locked-out member of their own space; a
    member of a different space, or an admin of this one, cannot.

## 11. Risks

- **Reintroducing account creation into the space-invitation path.** §1 and
  §5.1 record why this was drafted and rejected: it cannot avoid also
  deciding what credential to hand back, and every answer either lets a space
  owner mint a working login for an arbitrary address or reinvents
  `system_admin`'s account-claim semantics. Any future change to `POST
  /api/spaces/{space_id}/invitations` that starts creating accounts reopens
  this and needs the same scrutiny this document gave it, not less.
- **Existence of an email as an account becoming visible to whoever can
  invite.** `ErrInviteeAccountRequired` versus a created pending invitation
  tells a space owner or admin whether an arbitrary address has a BuildMax
  account, which is not a new disclosure — `AddMember`'s `ErrUserDoesNotExist`
  already draws the same distinction today — but it is worth naming rather
  than leaving implicit, since this document is the one giving that
  boundary a permanent home. Closing it would mean an invitation to a
  nonexistent address either silently doing nothing or lying about having
  succeeded, both of which are worse for the common, non-adversarial case
  this document is designed for.
- **Ownership transfer without acceptance surprising the new owner.** Decided
  against a confirmation step (open question 1); mitigated by the audit record
  and the reversibility noted in §5.3.
- **Scope creep toward a general policy platform.** Every action in §5 maps to
  one owner-triggered, immediately-effective change with one audit row — the
  same shape [space-governance.md](./space-governance.md) already established.
  Resist adding conditions, delays, or multi-party sign-off; that is space
  approval workflows, explicitly out of scope.

## 12. Open Questions

1. ~~Should ownership transfer require the target's acceptance rather than
   taking effect immediately?~~ **Decided: no, immediate and unilateral.** An
   in-app pending-transfer state was the alternative considered — feasible
   without a mail channel, mirroring §5.1's invitation — but the first slice
   stays to one new pending-state mechanism, not two. Revisit if an accidental
   or unwanted transfer occurs in practice; §5.3 already makes it reversible
   in the meantime.
2. ~~Should `admin` ever be allowed to invite a `member` (not `admin`)?~~
   **Decided: yes.** §5.1 and §7 give admin `ActionInviteSpaceMember` at
   `member` role only; role change and ownership transfer stay owner-only, so
   this does not let an admin reach anything `ActionManageSpaceMembers` still
   reserves.
3. ~~What is the invitation's TTL?~~ **Decided: `InvitationTTLDefault =
   72 * time.Hour`.** Originally framed as a credential's lifetime; after §1
   moved account creation out of this flow entirely, it is simply how long a
   pending `space_invitation` row stays acceptable — three days rather than a
   shorter window because an invitation is meant to be acted on whenever the
   recipient next opens Portal, not inside the exchange that sent it.
4. ~~Does a revoked or expired invitation need its own audit action?~~
   **Decided: yes, both.** `space.invitation_revoked` for an explicit
   withdrawal and `space.invitation_expired` for an accept attempted past its
   TTL — see §8 M3 for why this departs from the silent-failed-login
   precedent in [space-governance.md](./space-governance.md) §5.4.

## 13. Recommended First PR

1. `Invitation` core type, store methods, and the `space_invitation` table.
2. Invitation routes (M2) and the accept flow, replacing
   `POST /api/spaces/{space_id}/members` outright.
3. Audit actions for invite/accept/revoke/expire.
4. Portal invite action, pending-invitations list, and the "my invitations"
   surface.

Role change, ownership transfer, and the space-scoped login code (§5.2–§5.4)
are independent of the invitation mechanism and can land as a second PR in
either order.
