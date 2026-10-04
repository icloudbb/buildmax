---
id: space-assistant-entity
title: Add the Space Assistant entity, its bot binding, and its management page
roadmap: R5
source: docs/design/space-assistants.md#4-concepts
depends_on: [60-channel-gateway-many-bots.md, 62-service-accounts.md]
verification: ["./make test", "./make test mysql", "./make check portal", "./make e2e kind", "./make check docs"]
claim:
pr:
---

## Outcome

A Space owner can define an Assistant, bind its own Telegram bot, review what
publishing it discloses, and activate or pause it. Nothing answers requesters
yet; this task makes the configuration real and the bot reachable.

## Scope

- Core, store, service, and API for `assistant` and its append-only revisions:
  Space (team Spaces only), name, description, instructions, optional model
  target, roster (Space Agents and published Workflows), readable files (Space
  Artifact ids), audience (`space_members` | `all_users`), service account,
  sponsor, state (`active` | `paused`). Owners and admins manage; every change
  is audited.
- Creating an Assistant creates a same-named service account in the Space by
  default; the request may name an existing one instead.
- Roster entries carry a release contract: a Workflow entry uses the Workflow's
  `output_schema`; an Agent entry declares one; both list releasable top-level
  properties. An entry without one is refused. (Enforcement at run time is
  68-assistant-release-contracts.md.)
- `assistant_binding`: one Telegram binding per Assistant, token sealed with the
  Secret key-encryption key (covered by KEK rewrap), never stored as a Space
  Secret and never returned by the API. Binding calls `getMe`, stores the bot id
  and handle, and refuses a bot already bound or used as the system bot.
- Each replica reconciles enabled bindings of active Assistants into the
  Gateway (60-channel-gateway-many-bots.md) on a short interval and immediately
  on the replica that changed one. Until the front-door task lands, an Assistant
  bot answers linked requesters with a fixed "not available yet" reply; pairing
  from it works.
- Publish statement: activating, and saving audience, roster, or readable-file
  changes while active, returns a generated statement (who can ask, files it
  can read, Agents and Workflows it can run, Secrets those Agents hold) that the
  Portal shows and the request must confirm.
- Portal Space → Assistants list and detail/edit page, including bind/unbind
  and pause/activate.
- An Assistant pauses automatically while its service account is disabled or
  needs a sponsor.

## Out Of Scope

- Front-door turns (66), release enforcement (68), the read tool (70),
  escalation (72), delivery (74).

## Acceptance Criteria

- API and Portal create, edit, pause, activate, bind, and unbind an Assistant;
  non-owner/admin members are refused; personal Spaces are refused.
- The token never appears in any API response, log, audit event, or effective
  configuration; a KEK rewrap test covers bindings.
- Binding a duplicate bot is refused.
- A newly bound bot on a running server offers pairing within one reconcile
  interval on whichever replica holds its lease; unbinding stops it.
- Activation without confirming the current statement is refused.
- OpenAPI matches routes; browser test covers create → bind (mocked Telegram
  API) → activate with statement.

## Verification

`./make test` on the new packages first, then `./make test`; `./make test mysql`;
`./make check portal`; `./make e2e kind` (the kind stack can point the Telegram
connector at a mock API base URL); `./make check docs`.

## Notes

`channels.telegram.api_base_url` already exists for tests; bindings need the same
override from server configuration, not per binding.
