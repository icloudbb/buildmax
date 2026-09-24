# Instant-Messaging Channels

> **简体中文：** [阅读中文镜像](../zh-CN/design/即时通讯渠道.md)

> **Audience:** contributors · **Status:** accepted — Phase 1 (Telegram direct
> messages) implemented
>
> This record decides how chat platforms such as Telegram, Feishu/Lark,
> DingTalk, WeCom, and Slack reach BuildMax. It builds on
> [Agent execution and Task threads](agent-execution-and-task-threads.md) (the
> Conversation and Task planes), [server coordination](server-coordination.md)
> (replicas and leases), and [Remote Control](remote-control.md). The user guide
> is [manual/chat-apps.md](../../manual/chat-apps.md).

## Contents

- [1. Decision](#1-decision)
- [2. Why The Conversation Plane](#2-why-the-conversation-plane)
- [3. What Platforms Impose](#3-what-platforms-impose)
- [4. Concepts](#4-concepts)
- [5. Message Lifecycle](#5-message-lifecycle)
- [6. Pairing](#6-pairing)
- [7. Authorization And Disclosure](#7-authorization-and-disclosure)
- [8. Receiving Across Replicas](#8-receiving-across-replicas)
- [9. Outcome Reports](#9-outcome-reports)
- [10. Adding A Platform](#10-adding-a-platform)
- [11. Phasing](#11-phasing)
- [12. Open Questions](#12-open-questions)

## 1. Decision

A chat platform is a **transport into the Tier 1 Conversation** that Portal chat
already uses. A server-side **Connector** holds one bot credential and
normalizes platform messages. A platform-neutral **Gateway** decides, before any
model runs, who the sender is and which Space they may use. It then runs the
turn through the same turn queue and `conversation.Service` as a Portal message,
and sends the reply back. When a Task started from that conversation finishes,
the Gateway reports the outcome to the chat.

A message acts as a BuildMax user only after that user has **linked** the chat
account. The link starts in the chat and is confirmed in Portal. Each message is
checked against the one eligibility authority, so an account that is disabled
or leaves the Space loses chat access immediately.

Telegram direct messages ship first. Groups, other platforms, streaming
replies, and approvals from chat are later phases (§11).

## 2. Why The Conversation Plane

An instant-messaging chat is foreground conversation: a person asks, the
assistant answers, and heavy work is handed to durable Tasks. That is exactly
what a Conversation is ([Task threads](agent-execution-and-task-threads.md)
§4). A cron firing is different. It is not a conversation, which is why
`ChannelCron` [was moved off this plane](scheduled-agent-execution.md). A chat
message is the one kind of origin that belongs here, and it does not become an
execution or authorization parent: Tasks it starts stay Space-owned.

Four alternatives were rejected.

- **Bridging the generic webhook** through n8n or a script. A webhook key names
  one account, so every chat participant would act as the key's owner. There
  would be no per-sender authorization, no threads, and no streaming.
- **A local gateway in the CLI or Desktop**, the OpenClaw and Claude Code
  Channels shape. The local Bash sandbox defaults off, so a chat message would
  steer an unsandboxed process with the user's full authority. There is no local
  daemon either. This shape's public record, with exposed gateways, token theft,
  and marketplace malware, argues for waiting until a sandboxed local runtime
  exists.
- **Chat as a Remote Control viewer.** It is valuable for approving a live local
  session from a phone, but it only reaches sessions already running on an open
  machine. It is a later projection over the identity this record builds (§11).
- **Channels as external plugins.** That would freeze an interface before two
  adapters have proved it. §10 is the seam instead.

## 3. What Platforms Impose

These constraints come from the platforms' documentation as of 2026-09. They
are what the Connector contract is shaped around.

| Platform | Inbound without a public URL | Consumers per bot | Native streaming |
|---|---|---|---|
| Telegram | `getUpdates` long polling | One. Webhook and polling are exclusive | `sendMessageDraft` (private chats) |
| Slack | Socket Mode | Up to 10 sockets. Each event goes to one | `chat.startStream` / `appendStream` / `stopStream` |
| Feishu/Lark | Long-connection WebSocket | Up to 50. Each event goes to one | CardKit streaming card |
| DingTalk | Stream mode | Several. Each event goes to one | AI Card streaming |
| WeCom AI bot | Long connection | One. The newest connection evicts the old | `stream` message |
| Discord | Gateway WebSocket | Sharded | Message edits only |

- **No platform broadcasts.** Each message reaches one consumer, and every
  platform delivers at least once. Receiving therefore needs one holder per bot
  (§8) and deduplication.
- **Outbound-connection modes need only egress.** That is the common shape of a
  private BuildMax deployment, so connectors prefer them to webhooks.
- **Final-message delivery works everywhere.** Streaming is an enhancement, not
  a prerequisite.

Prior art (OpenClaw, Hermes Agent, Claude Code Channels, Claude in Slack, Codex
and Cursor in Slack) converges on the patterns this record adopts:

- the platform user id is bound out of band, and access is denied by default;
- a conversation is keyed by chat and thread;
- groups need an @mention;
- the clicker is re-authorized on every approval.

The same prior art shows the failures to avoid:

- anyone who can post can steer the agent;
- anyone who can reply can approve;
- control planes and tokens get exposed;
- link previews exfiltrate data.

## 4. Concepts

Each concept is listed with the requirement that fails without it.

- **Connector configuration** is not an entity. It is `channels.telegram.bot_token`
  in `server.yaml`, injected through `BUILDMAX_TELEGRAM_BOT_TOKEN`.
  - [Space Secrets §4](space-secrets.md) already places BuildMax's own
    credentials in operator configuration.
  - A deployment normally has one bot per platform.
  - Space-owned bots wait for a team that needs its own bot identity.
- **`channel_identity`** is platform, tenant, and external user id mapped to a
  user.
  - Without it, a message cannot carry per-sender authority.
  - It is deliberately not `external_identity`. That row is a sign-in credential
    with JIT account creation, and a chat link must never sign anyone in.
  - Its key is the platform's immutable id, never a display name.
  - `tenant` is empty on Telegram. It exists because Feishu, DingTalk, and WeCom
    ids are scoped per organization or app, and adding it to the unique key
    later would be a migration.
- **`channel_pairing`** holds the hash of a pending link code. Without it, a
  link cannot be confirmed on a trusted surface (§6).
- **`conversation.channel_ref`** is the chat a platform-carried conversation
  answers to.
  - It lets the next message continue a conversation, and lets an outcome be
    reported to the right chat.
  - Several rows share one ref. The newest is the chat's current conversation.
    `/new` and `/space` start another in place, and no pointer table is needed.
  - Only the Gateway sets it. `telegram` is therefore not in `ValidChannels()`,
    and neither the HTTP nor the WebSocket create path accepts it.

The old `channel.Adapter` interface (`Receive(raw any)`, a `Send` nobody
called) was not extended. It stays the webhook's own type, and chat platforms
use `core/channel.Connector`.

## 5. Message Lifecycle

1. **Receive.** On the lease holder (§8), the Connector normalizes a platform
   message to `Inbound`, and the Gateway queues it behind any earlier message
   from the same chat.
   - A chat is answered in order, and different chats proceed concurrently.
   - A chat can have at most 10 messages waiting.
   - A redelivered event id is dropped.
2. **Gate, before any model call.**
   - Anything but a private chat is ignored.
   - An unlinked sender gets a pairing reply and nothing else.
   - A linked sender's conversation is the newest for that chat, or a new one
     in their personal Space.
   - `eligibility.Check(user, space)` runs, and a refusal is a fixed reply with
     no Space data.
3. **Turn.** `Handler.RunChannelTurn` submits the turn to `turnqueue` and runs
   `conversation.Service.HandleTurn`, as a Portal message does. A chat turn and
   a Portal turn on one conversation are therefore serialized against each other.
   The acting user is the sender, with their quota.
4. **Reply.** The reply is sent as plain text with link previews disabled. It is
   split to the platform's limit, which is 4096 UTF-16 units on Telegram.
   - A typing indicator refreshes while the turn runs.
   - Model text is never sent with a Markdown parse mode, because an unescaped
     character fails the whole message.
   - Errors become fixed sentences, or an `apierr` message written for callers.
     Other errors are logged, and the chat gets a generic sentence with a Portal
     link.
5. **Commands.**
   - `/new` starts a new conversation in the current Space.
   - `/space` lists the user's Spaces; `/space <n>` switches and starts a
     conversation there.
   - `/help` shows the commands.
   - Unlinking is Portal-only, so there is one audited path for it.

## 6. Pairing

1. A message from an unlinked chat account gets an 8-character code.
   - The alphabet has no ambiguous characters, which gives about 40 bits.
   - The code expires in 10 minutes and is single-use, and only its SHA-256 is
     stored.
   - A new code replaces the account's previous one.
   - The reply carries a `public_base_url/#/account/chat/<code>` link. A hash
     fragment never reaches a server log.
2. The person signs in to Portal. **Account → Chat accounts** looks the code up
   with `GET /api/channel-link-pairings?code=`, where the request log redacts
   `code`. It shows the platform and handle being linked.
3. On confirmation, `POST /api/channel-links` consumes the code and creates the
   link in one locked transaction. The chat is then told it is linked.

The code is confirmed on the trusted surface, as in OpenClaw, Claude Code
Channels, and Claude Tag, rather than pasting a Portal-issued secret into a
chat. Showing the handle before confirming is the defense against consent
phishing: someone sending a victim their own link code.

- One unlinked chat account is offered at most one code per 30 seconds, so
  strangers messaging the bot cannot turn it into a database writer.
- A chat account already linked to someone else refuses a new link
  (`ErrAlreadyLinked`, 409) until that link is removed.
- Linking and unlinking are audited as `channel_link.created` and
  `channel_link.removed`. The platform's account ids are never in the trail.

## 7. Authorization And Disclosure

- **Deny by default.** An unlinked sender, a group chat, a disabled account, or
  a non-member produces no model call and no Space data.
- **The sender is the actor.** Every turn and Task records the linked user.
  There is no bot principal and no fallback account, unlike the webhook path's
  configured `user_id`.
- **No groups yet.** Every reply in a group reaches people who may not belong to
  the Space. Serving a group needs an explicit, audited binding by a Space
  owner or admin (§11).
- **Injection surface.** Chat text is untrusted input to the Tier 1 model, like
  Portal text.
  - Tier 1 tools only start, continue, and read Space work, and never read
    Secrets.
  - Worker runs keep their fail-closed sandbox.
  - Only the triggering message becomes a turn.
- **Exfiltration by rendering.** Link previews are off on every bot message.
- **Credentials.** The bot token lives only in operator configuration.
  - It is reported only as set or not in the redacted configuration.
  - Connector errors are redacted, because the token is part of every Bot API
    URL.
  - It is never passed to a TaskRun.
- **Retention.** Messages persist on the platform's servers. The user guide says
  so.

## 8. Receiving Across Replicas

A Connector's `Receive` runs only on the replica holding the connector's lease.

- **Single replica.** In `coordination.mode: local` there is one replica and no
  lease.
- **Redis mode.**
  - Each replica calls `TryAcquireLock("channel-connector:<platform>")` every 10
    seconds until it wins.
  - The winner holds the lock for a 30-second TTL and renews it in the
    background.
  - The renewer closes `Lease.Lost()` when the key is gone, or cannot be
    confirmed for a whole TTL. The Gateway then cancels `Receive` at once.
  - Before this change the lock did not report loss. A holder receiving
    indefinitely has to know, or it keeps consuming beside the next holder.
- **Offsets.** Telegram's offset lives in memory. Telegram keeps every update
  not yet confirmed by asking past it, so a new holder resumes from what the old
  one left. On stop, the connector confirms what it took, so a handoff does not
  replay answered messages.
- **Draining.** The server stops receivers first, then waits for turns, then for
  their replies. A message queued behind a drain gets a "restarting, send it
  again" reply.
- **Sending needs no lease.** Replies, link confirmations, and outcome reports
  go out from whichever replica has them.

## 9. Outcome Reports

`OnTaskRunTerminal` also calls `Gateway.ReportRunTerminal`.

- **Which runs.** Only a run whose Task carries a Conversation with a
  `channel_ref` is reported.
- **Who can still see it.** The report is sent only while the conversation's
  owner still passes eligibility for its Space, and still has a link on that
  platform.
- **What it says.** The report gives the Task title and status, up to 1500
  characters of output (500 of an error), and a Portal link.

Known gaps:

- Runs finished by the stale-run reaper do not fire `OnTaskRunTerminal`, so they
  are not reported. The same gap affects Issue run comments.
- Workflow runs started from chat are not reported, because their node Tasks
  carry no Conversation.

## 10. Adding A Platform

A platform is one package under `internal/infra/imchannel/<platform>`
implementing `core/channel.Connector`:

- `Platform()`
- `Receive(ctx, deliver)`, which normalizes to `Inbound`, handles transient
  errors itself, and returns early only on unrecoverable ones;
- `Send(ctx, Outbound)`, which splits to the platform limit and disables
  previews;
- `Typing`
- `Info`

Then add a `channels.<platform>` block to `ServerConfig`, with its secret
override in `env_spec.go` and its redaction. Build the connector in
`bootstrap/channels.go`.

Nothing in the Gateway, stores, routes, or Portal changes: pairing, gating,
turns, replies, and reports are platform-neutral. `Inbound.Tenant` and the
`tenant` column exist for platforms whose ids are organization-scoped.

## 11. Phasing

| Phase | Scope | State |
|---|---|---|
| 1 | Telegram private chats, final-message replies, pairing and Portal link management, `/new` and `/space`, outcome reports, receive lease | Shipped |
| 2 | Feishu/Lark long connection. Group chats with an audited Space binding by a Space owner or admin, @mention gating, thread → Conversation via `channel_ref`, card rendering | Not started |
| 3 | Streaming through each platform's native primitive, with a throttled-edit fallback | Not started |
| 4 | Remote Control projection: push a user's live local-session approvals and completion to their linked chat, with buttons that re-authorize the clicker | Not started |
| 5 | DingTalk, WeCom, Slack, Discord, each an adapter, by demand | Not started |

Two ordinary fixes surfaced while designing this. Neither blocks Phase 1:

- the stale-run reaper should fire the terminal hook;
- the inbound webhook's default `webhook.user_id: "webhook"` is looked up as a
  user and cannot succeed.

## 12. Open Questions

1. **Enterprise identity shortcut.** When the deployment's OIDC issuer is the
   same Feishu, DingTalk, or WeCom tenant, can a chat account be linked
   automatically from a stable tenant claim instead of pairing?
2. **Group membership.** Is an explicit binding decision enough, or should
   replies be refused in groups containing non-members? Most platforms cannot
   enumerate members cheaply.
3. **Notifications beyond the originating chat.** Should a linked user subscribe
   their chat to events that did not start there, such as Issue assignment,
   schedule paused, or Workflow failed? Each needs a server event that does not
   exist yet.
4. **Approvals in worker runs.** A chat approval for a server-side TaskRun needs
   a parked, resumable "waiting for approval" run state. That is a change to the
   execution plane with its own design.
5. **Cost visibility.** Chat turns spend the user's quota like Portal turns. Does
   a chat reply need to show its cost?
