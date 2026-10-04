---
id: channel-gateway-many-bots
title: Key the chat Gateway by bot and let any bot start a pairing
roadmap: R5
source: docs/design/space-assistants.md#9-a-gateway-with-many-bots
depends_on: []
verification: ["./make test", "./make test mysql", "./make check docs"]
claim:
pr:
---

## Outcome

The chat Gateway can serve more than one bot per platform, which Space
Assistants need, and a person can link their chat account from whichever bot
they message first. Today the Gateway assumes one bot per platform and only the
operator's system bot can issue a link code.

## Scope

- Give each connector a key: `system` for the bot configured in `server.yaml`,
  and an opaque key for bots registered later. Key the receive lease
  (`channel-connector:<platform>:<key>`), event deduplication, and per-chat
  queues by it in `internal/service/channel`.
- Add a nullable/defaulted connector key column to `channel_pairing` and to
  `conversation`; `offerPairing` records the issuing connector, `ConfirmPairing`
  replies through it, and `ReportRunTerminal` sends through the connector the
  conversation arrived on (falling back to `system`).
- Let the Gateway register and remove connectors at runtime (start/stop the
  receive loop for one key) and exist even when no system bot is configured, so
  the next task can add Assistant bots without a restart. No caller registers
  one yet besides tests.
- Refuse registering a connector whose platform bot id equals one already
  registered, since a Telegram token can have only one poller. The Telegram
  connector exposes the bot id it learns from `getMe`.

## Out Of Scope

- The Assistant entity, binding storage, and reconciling bindings from the
  database (64-space-assistant-entity.md).
- Any change to the personal assistant's behavior or messages.

## Acceptance Criteria

- All existing channel Gateway, handler, and Telegram tests pass unchanged in
  meaning.
- A test with two connectors on one platform shows: a code offered by bot B is
  confirmed through bot B; each bot's messages queue and deduplicate separately;
  each holds its own lease; a run started from bot B's conversation reports
  through bot B.
- A connector registered at runtime starts receiving, and one removed stops,
  without affecting the other.
- A second registration of the same bot id is refused.
- `docs/design/instant-messaging-channels.md` §4, §6, §8, §9 (and zh mirror)
  describe connector keys.

## Verification

`./make test ./internal/service/channel/... ./internal/infra/imchannel/...`
first, then `./make test`; `./make test mysql` for the new columns;
`./make check docs` for the design edits.

## Notes

Telegram user ids are the same across bots, so `channel_identity` needs no bot
dimension. Keep the Gateway's single-replica behavior (nil Locker) working.
