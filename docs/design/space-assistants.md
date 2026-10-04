# Space Assistants

> **简体中文：** [阅读中文镜像](../zh-CN/design/空间助手.md)

> **Audience:** contributors, product designers, and operators · **Status:**
> accepted — in progress: the many-bot Gateway (§9) and service accounts (§6)
> are built
>
> This record decides how a Space publishes **Assistants**: conversational
> service front doors that answer people outside the Space's own work, dispatch
> a bounded roster of the Space's Agents and Workflows as Tasks, and appear in
> chat under their own bot identity. It also decides the **service account**,
> the non-human principal whose authority an Assistant's work runs under. It
> builds on [instant-messaging channels](instant-messaging-channels.md) (the
> Gateway, pairing, and receive leases),
> [Agent execution and Task threads](agent-execution-and-task-threads.md)
> (Conversation and Task planes), [structured output](structured-output.md),
> and [Space governance](space-governance.md). The authority model is the first
> slice of the
> [Agent execution identity proposal](../proposals/agent-execution-identity-and-delegation.md)'s
> organization authority.

## Contents

- [1. Decision](#1-decision)
- [2. Two Kinds Of Conversational Surface](#2-two-kinds-of-conversational-surface)
- [3. Evidence And Adoption Decision](#3-evidence-and-adoption-decision)
- [4. Concepts](#4-concepts)
- [5. Role Separation](#5-role-separation)
- [6. Service Accounts](#6-service-accounts)
- [7. Authorization](#7-authorization)
- [8. Disclosure Boundary](#8-disclosure-boundary)
- [9. A Gateway With Many Bots](#9-a-gateway-with-many-bots)
- [10. The Front-Door Turn](#10-the-front-door-turn)
- [11. Results, Escalation, And Delivery](#11-results-escalation-and-delivery)
- [12. Review And Retention](#12-review-and-retention)
- [13. Alternatives Considered](#13-alternatives-considered)
- [14. Risks](#14-risks)
- [15. Phasing](#15-phasing)
- [16. Deferred](#16-deferred)
- [17. Open Questions](#17-open-questions)

## 1. Decision

A Space can publish several **Assistants**. Each is a Space-owned entity with
its own instructions, model, roster of Agents and published Workflows, audience,
sponsor, and Telegram bot. A requester's message reaches the Assistant's bot,
the Gateway identifies the requester through their existing chat link, checks
the Assistant's audience, and runs a Tier 1 Conversation turn configured by the
Assistant. Work the turn starts is an ordinary Task in the Assistant's Space.

That work runs as the Assistant's **service account**: a non-human user that
belongs to exactly one Space, cannot sign in, and is accountable through a human
sponsor. The requester is recorded as provenance, never as the authority. The
service account's Space membership is the organization's grant; the Assistant's
roster narrows it further.

What a requester may learn is bounded by what the Assistant can read, not by
filtering what it says. Roster members return a structured result whose
releasable fields are the only run output a requester sees.

Nothing here adds an execution plane, run type, or model loop. The personal
assistant and the operator-configured system bot are unchanged.

## 2. Two Kinds Of Conversational Surface

| | Personal assistant (shipped) | Space Assistant (this record) |
|---|---|---|
| What it is | The user's Tier 1 Conversation, reached from Portal or a linked chat | A service a Space publishes to an audience |
| Who it serves | Its own user | Requesters, usually outside the Space |
| Who owns it | The user | A Space, accountable through a sponsor |
| Whose authority runs the work | The user's | The Assistant's service account, bounded by its roster |
| What may be returned | Anything the user may already see | Only what the Space released to this audience |
| Bot | The deployment's system bot from `server.yaml` | The Assistant's own bot, bound in Portal |

In the personal assistant the requester, conversation owner, and authority are
one person, which is why the channel design can say "the sender is the actor;
there is no bot principal"
([instant-messaging channels §7](instant-messaging-channels.md#7-authorization-and-disclosure)).
That remains true on the personal path. A Space Assistant separates the three,
and every control in this record follows from that separation.

## 3. Evidence And Adoption Decision

The motivating case is departmental: an HR support bot, a marketing support
bot, an audit bot. Organizations already run such bots, mostly for
notifications, and many small and medium organizations run them on Telegram
rather than an enterprise chat suite. Chat suites ship per-team bots as a
product (Feishu aily, WeCom smart bots, DingTalk's assistant marketplace), and
enterprise agent builders converge on the separations this record makes: agent
definition, channel binding, trigger, and run as distinct objects; an explicit
choice between acting as the invoker and acting as the agent; and required
sponsors for non-human identities. The documented failures are identity
failures: event triggers running with a maker's credentials and over-sharing,
and scheduled agents failing silently when a person's token expired.

The proposal that preceded this record held it behind the Beta gate until a
named organization asked. On 2026-10-04 the maintainer decided to build the
validation slice without one. The Beta gate itself has passed, so this is R5
work ordered by that decision. The §15 measurements remain the evidence for
widening it; an adopting team remains the evidence for anything beyond §15.

## 4. Concepts

Each concept is listed with the requirement that fails without it.

**Assistant** (new, Space-owned).

| Field | Requirement that fails without it |
|---|---|
| Space, name, description | An organization runs several; requesters must know which one they reach |
| Instructions, model | HR and audit need different personas; Tier 1 has one fixed prompt and model per deployment |
| Roster: Space Agents and published Workflows, each with a release contract (§8) | The front door must dispatch a chosen set, not every Agent in the Space; the roster bounds both authority and disclosure |
| Readable files: an allowlist of Space Artifacts | Answering from material needs something to read, and what is readable is the disclosure boundary |
| Audience: `space_members` or `all_users` | Who may ask is the Space's decision, not a side effect of membership |
| Service account | Work needs an authority that is neither the requester nor a person who may leave (§6) |
| Sponsor and state (`active`, `paused`) | Someone is accountable, and the Space must be able to stop it at once |
| Revision | An answer must be attributable to the configuration that produced it, like Agent revisions |

Assistants live only in team Spaces. A personal Space has one member by
construction and cannot admit a service account.

Knowledge scope is the readable-files allowlist until a knowledge source exists.
A later knowledge base sets its scope on the Assistant, because scope is a
disclosure decision.

**Service account** (new user kind, §6). A user row with kind `service`, member
of exactly one Space, with no way to sign in.

**Channel binding** (new, child of Assistant). One Telegram bot token, sealed
with the Secret key-encryption key, together with the bot's Telegram id and an
enabled state. The token is not a Space Secret: a Space Secret exists to be
granted to Agents and materialized into worker runs, and a bot token must never
reach a worker. One Assistant has at most one binding in this record; a later
adapter may allow one per platform.

**Chat link** (existing, user-level, unchanged). `channel_identity` stays the
single fact that a chat account belongs to a BuildMax user. Telegram user ids
are the same across bots, so one link identifies a person to the system bot and
every Assistant bot. Assistants keep no link table of their own.

**Conversation** (extended). An optional `assistant_id`. A direct-message
Conversation with an Assistant is keyed by Assistant, chat, and requester. Its
`user_id` is the requester, so the turn is metered and recorded against them,
while its Space is the Assistant's.

**Task** (unchanged shape, new provenance). A Task an Assistant dispatches
records `requested_by` (the requester), `assistant_id`, and the Assistant
revision, beside `created_by`, which is the service account.

## 5. Role Separation

| Role | Who | Recorded as |
|---|---|---|
| Requester | The person asking, identified through their chat link | Conversation `user_id`; Task `requested_by` |
| Assistant (responder) | The front door and its revision | Conversation `assistant_id`; revision per message and per Task |
| Operating authority | The Assistant's service account, bounded by the roster | Task and TaskRun `created_by` |
| Executing actor | The Agent revision and TaskRun dispatched | Existing Task and TaskRun fields |
| Sponsor | A Space owner or admin accountable for the Assistant and its service account | Assistant and service account `sponsor` |

The audit record for a dispatched Task preserves all five. Collapsing them back
into one `created_by` loses the distinction the identity proposal names as the
overloaded meaning of that field.

## 6. Service Accounts

### 6.1 Why a separate principal

An Assistant's work needs an authority. The alternatives fail:

- **The requester.** Requesters usually hold no rights in the Space, and making
  each one a member turns every audience into Space membership.
- **The sponsor or another named user.** The work stops when that person leaves
  or changes role. It carries the person's whole Space identity, so requester
  text drives a named employee's authority and the audit says the person did
  it. Designating someone else without their consent is impersonation; with
  consent it only moves the problem to a different person.
- **The Assistant itself as the principal.** This is the identity proposal's
  simpler baseline (B1). It fails here on two counts. A department's authority
  is shared: the HR Assistant and HR's team Schedules should run as the same
  "HR operations" principal, and the mandate should survive replacing the
  Assistant. And every authority check in the system — `created_by`,
  eligibility, membership, audit actors — is keyed by user id, so a non-user
  principal would generalize all of them.

A service account is the identity proposal's option D, a Space-owned automation
principal, chosen because its trigger condition holds: the mandate spans
multiple actors and must outlive any one of them.

### 6.2 Shape

A service account is a `user` row with `kind = service`. Being a user is what
lets every existing check apply unchanged: a dispatched Task is admitted,
dispatched, fetched, and reconciled against `created_by` exactly as today.

| Property | Rule | Why |
|---|---|---|
| Space | Member of exactly one team Space, with role `member` | Its reach is that Space; it can never manage the Space or join another |
| Sign-in | None: no email, password, login code, SSO link, refresh token, PAT, or API key; every login path refuses the kind | Only server components can act as it, so it has no credential to steal or lifecycle to run |
| Chat link | None; pairing refuses it | It is never a requester |
| Sponsor | A human owner or admin of the same Space | Someone is accountable |
| State | Active or disabled, using the existing account disablement | One disable stops everything acting as it |
| Visibility | Listed in the Space's settings and the administration user list, marked as a service account; absent from invitations, sign-in, and people pickers | Inventory without being mistaken for a person |
| Quota | Usage counts against its Space, as all usage does | No separate allowance to manage |

Creating an Assistant creates a service account with the same name by default.
The Space owner may instead choose an existing service account in the Space,
which is how an Assistant and the Space's other automation share one authority.
Deleting an Assistant leaves its service account; disabling the service account
stops every Assistant that uses it.

### 6.3 Lifecycle

- **Who manages it.** Space owners and admins create, rename, disable, re-enable,
  and re-sponsor service accounts in their Space, with an audit event for each.
  Deployment administrators can disable any of them.
- **Sponsor departure.** When the sponsor stops being an owner or admin of the
  Space, or their account is disabled, the service account's Assistants pause
  and the service account is marked as needing a sponsor. An owner or admin who
  takes sponsorship resumes them. Pausing is fail-closed: an Assistant serving a
  wide audience never runs unowned. The service account keeps its identity and
  grants, so no reconfiguration is needed, which is the difference from a
  creator-bound `created_by`.

### 6.4 Relationship to the identity proposal

The service account's Space membership is this record's form of
`organization_grant`: an explicit, sponsor-accountable grant to a non-human
principal, ended by disabling it. It does not deliver the proposal's per-run
workload identity, brokered credentials, review or expiry dates, or target
entitlements; those stay in that proposal. Schedules and inbound webhooks may
later run as a service account instead of their creator; that conversion is not
part of this record.

## 7. Authorization

### 7.1 Who the requester is

A requester is an active BuildMax user whose chat account is linked by pairing.
On Telegram nothing weaker is trustworthy: a Telegram id proves only that some
Telegram account sent the message. The trust root is the Portal confirmation
of the pairing code, by the signed-in user who sees the chat handle
([instant-messaging channels §6](instant-messaging-channels.md#6-pairing)).

Pairing is user-level infrastructure, not a feature of the system bot:

- **Any server-managed bot can start a pairing.** An unlinked account that
  messages an Assistant's bot gets a code exactly as from the system bot, and
  confirmation writes the same `channel_identity` row. The pairing records which
  bot issued the code, so that bot confirms the link. A deployment with
  Assistant bots and no system bot can still link people.
- **One link serves every bot.** A person who paired once is recognized by the
  system bot and every Assistant.
- **The sign-in window applies** as on the personal path: a link acts only while
  the user's last BuildMax sign-in is within `channels.sign_in_window`.
- **Unlinking stays in Portal.**

### 7.2 Who may ask

The audience is a policy over BuildMax users:

- `space_members`: members of the Assistant's Space;
- `all_users`: any active user of the deployment.

A named list of users or Spaces waits for a team that needs it. A requester
outside the audience gets a fixed refusal and no model call. The audience,
the Assistant's state, the service account's state, and the requester's account
are checked on every message, before any model runs.

### 7.3 Whose authority runs the work

The service account's. The turn's tools act as the service account and can only
reach the roster: StartTask accepts only roster Agents, and the Workflow tools
list and run only roster Workflows. The server checks the roster on every call;
the model's choice is never trusted. Inside a dispatched run, the worker's
run-scoped API acts for the run's Task as it does today, so the run's reach is
the service account's Space membership.

Operations a roster member performs with consequence outside BuildMax are
bounded by that Agent's own Secret grants, network tier, and approval policy.
A question or approval that a run raises is answered by Space members, never by
the requester.

## 8. Disclosure Boundary

**Principle.** Anything an Assistant can read, directly or through its roster,
is treated as disclosed to its whole audience.

Model output is free text, and requesters are untrusted. Filtering output
afterwards misses rephrased, partial, or inferred content, so the control is on
what the Assistant can read.

| Layer | In this record | Mechanism |
|---|---|---|
| 1. Readable scope | Yes | The readable-files allowlist and the roster define everything reachable. Adding an Agent to the roster adds its files, Secrets, and network reach to what the audience can effectively learn |
| 2. Requester-bound lookups | No (§16) | Calls run as the requester with no model-supplied person id |
| 3. Release contract | Yes | Each roster entry has an `output_schema` and names its releasable top-level fields. Only those reach the front-door model or the requester |
| 4. Separation of contexts | Partly, by layer 3 | The model talking to the requester never holds raw run output |
| 5. Human review | Through escalation (§11) | An unanswerable request becomes an Issue a person answers |
| 6. Audit | Yes | Conversation, revision, requester, and dispatched Tasks are recorded; Space owners and admins can review (§12) |

**Release contract.** A Workflow roster entry uses the Workflow's own
`output_schema`. An Agent roster entry declares one on the entry, and StartTask
passes it as the Task's `output_schema`, so the run must satisfy it. The entry
lists which top-level properties are releasable. GetTask and GetWorkflowRun, when
called from an Assistant turn, return status and the releasable fields only:
no raw output, error text, transcript, or Portal link. A roster entry without a
release contract cannot be added.

**Publishing is a disclosure decision.** Activating an Assistant, and saving a
change to its audience, roster, or readable files while it is active, shows the
owner a generated statement and requires confirmation: who can ask, which files
it can read, which Agents and Workflows it can run, and which Secrets those
Agents hold. A Space owner who understands that statement is a stronger control
than any output filter.

**Requesters are told who reads their messages.** The Assistant's first reply in
a conversation names the Space that operates it and says the Space can review
the conversation. The bot token's holder can read the bot's messages through
Telegram regardless; the statement names the Space as holding it.

**Residual limit.** Within layer 1's scope, aggregation and inference remain
possible: a requester may learn more from many answers than from one. This is a
documented limit, not a guarantee.

## 9. A Gateway With Many Bots

The Gateway today keys connectors, receive leases, deduplication, chat queues,
pairing confirmations, and outcome reports by platform, because one platform has
one bot. It becomes keyed by **bot**:

- **Connector key.** Each connector has a key: `system` for the bot from
  `server.yaml`, and the binding id for an Assistant's bot. Leases are
  `channel-connector:<platform>:<key>`; deduplication and chat queues include the
  key.
- **Pairing records its bot.** `channel_pairing` stores the connector key that
  issued the code, and confirmation is sent through that connector.
- **Conversations record their bot.** The connector a conversation arrived on is
  the one its outcome reports leave through: the system bot for a personal
  conversation, the binding for an Assistant conversation.
- **Bindings change at runtime.** Every replica keeps its set of Assistant
  connectors in step with enabled bindings in the database, reconciling on a
  short interval and immediately on the replica that made the change. The
  receive lease still lets exactly one replica receive per bot. A disabled or
  deleted binding stops receiving at the next reconcile.
- **One receiver per token.** Telegram delivers each update to one poller, so a
  token must not serve twice. A binding stores the bot's Telegram id from
  `getMe`, unique across bindings and distinct from the system bot's; a duplicate
  is refused when bound.
- **The Gateway always exists** when Assistants may be bound, whether or not a
  system bot is configured.

The personal path is unchanged apart from its key: the system bot keeps serving
linked users in their own Spaces.

## 10. The Front-Door Turn

A message on an Assistant's bot is handled in this order, all before any model
call: private chat only; chat link lookup, or a pairing offer; sign-in window;
Assistant and binding active; service account active and sponsored; requester's
account active; requester in the audience. Each refusal is a fixed reply that
carries no Space data.

The turn reuses the Tier 1 runtime with an Assistant profile:

- **Prompt.** The Assistant's instructions replace the personal assistant's
  opening line. The tool guidance and the disclosure rules (§8) stay fixed and
  are not editable by the Assistant's author.
- **Model.** The Assistant's model, or the deployment's conversation model when
  unset.
- **Identity split.** The turn's model calls are metered to the requester in the
  Assistant's Space. Its tools act as the service account. Today one
  `UserID` serves both purposes; the turn input gains a separate acting user.
- **Tools.** StartTask and the Workflow tools limited to the roster; ListTasks,
  GetTask, GetWorkflowRun limited to this conversation and returning releasable
  fields only; a read tool over the readable files (list and read, text media
  types, bounded size); Escalate (§11). No ListSpaces, and no `/space` command.
- **Commands.** `/new` starts a new conversation with the same Assistant;
  `/help` shows the Assistant's description and operating Space.
- **Revision.** Each stored message records the Assistant revision that produced
  it.

Model usage counts against the Space's token quota like any Conversation turn.
An Assistant whose Space has spent its quota refuses with a fixed reply.

## 11. Results, Escalation, And Delivery

- **Outcome reports.** When a Task an Assistant dispatched ends, the requester's
  chat receives a report through the Assistant's bot: the releasable fields on
  success, and a fixed message on failure or cancellation. Raw output, error
  text, and Portal links are not sent. The personal path's report is unchanged.
- **Escalation.** The Escalate tool opens an Issue in the Assistant's Space,
  created by the service account, with the requester and conversation recorded,
  and tells the requester a person will follow up. A Space member answers from
  the Issue with an explicit "reply to requester" action, which sends through the
  Assistant's bot and is recorded on the Issue.
- **Outbound delivery.** A Schedule in the Space may name an Assistant and one
  requester as its delivery target. When a fire's run ends, the releasable result
  is sent to that requester through the Assistant's bot. Telegram lets a bot
  message only people who have started a chat with it, so the target must be a
  requester with an existing conversation with that Assistant. Workflow runs a
  Schedule fires are covered the same way. Chat and group targets wait for group
  support.

## 12. Review And Retention

- Dispatched Tasks are ordinary Tasks in the Space, visible to its members.
  Their input carries what the requester asked; that is part of what the Space
  sees, and the first-reply notice says so.
- Assistant conversations are readable by the Space's owners and admins, read
  only, from the Assistant's page. Ordinary members do not see them.
- Requesters do not see Assistant conversations in Portal; their history is the
  chat. They cannot delete it in BuildMax.
- Conversations have no retention policy or deletion path today, and Assistant
  conversations add none in this record. Requesters outside the Space make the
  gap sharper, so it is an open question (§17).

## 13. Alternatives Considered

| Option | Why not |
|---|---|
| Assistant as fields on Space Agent | Conflates a sandboxed Task executor holding Secrets with a Server-side front door, mixing the disclosure boundary with execution reach |
| One bot per Agent, every message a Task | A department needs one front door over several Agents; every message pays worker latency; no place for audience or readable scope |
| One deployment-wide router bot | A department cannot own its persona, audience, or disclosure boundary in a shared bot |
| Sponsor as `created_by` | Ties the mandate to one person and makes requester text drive that person's authority (§6.1) |
| "Acts as" a chosen user | Impersonation without consent; no gain over a sponsor with it (§6.1) |
| Assistant as its own principal | Cannot be shared with the Space's other automation, and would generalize every user-keyed authority check (§6.1) |
| Bot token as a Space Secret | Space Secrets are granted to Agents and materialized into runs; a bot token must not be |

## 14. Risks

- **Disclosure through the roster.** A Space owner adds a powerful Agent without
  realizing the audience inherits its reach. Mitigation: the publish statement
  and required release contracts.
- **Prompt injection from requesters.** Requesters are many and untrusted.
  Mitigation: layers 1, 3, and 4; roster checks on the server; approvals routed
  to Space members.
- **Service account sprawl.** Mitigation: required sponsor, pause on sponsor
  departure, inventory in Space and administration views.
- **Cost from a wide audience.** Every user can spend the Space's model budget.
  Mitigation: the Space token quota now; per-requester limits when usage shows
  the need.
- **Bot token holder.** Whoever holds the token can read and send as the bot
  outside BuildMax. This does not weaken pairing, which is issued by the Server
  and confirmed in Portal. Mitigation: requesters are told who operates the
  Assistant, and the token is sealed and never leaves the Server.

## 15. Phasing

Each slice is a backlog task, in order:

1. Gateway with many bots and pairing from any bot (§7.1, §9) — built; see
   [instant-messaging channels §4](instant-messaging-channels.md#4-concepts).
2. Service accounts (§6) — built; see
   [current state](../current-state.md#account-space-and-extension-surfaces).
3. [Assistant entity, binding, and management](../backlog/64-space-assistant-entity.md) (§4, §8 publish statement).
4. [Assistant front-door turn](../backlog/66-assistant-front-door-turn.md) (§7.2, §7.3, §10).
5. [Release contracts and Assistant outcome reports](../backlog/68-assistant-release-contracts.md) (§8, §11).
6. [Readable files tool](../backlog/70-assistant-readable-files.md) (§8, §10).
7. [Escalation to an Issue](../backlog/72-assistant-escalation.md) (§11).
8. [Schedule delivery through an Assistant](../backlog/74-assistant-schedule-delivery.md) (§11).
9. [Validation run](../backlog/76-space-assistant-validation.md): measure answer
   accuracy on a fixed question set, leakage under a scripted red-team set,
   front-door and worker latency, escalation rate, and whether a Space owner can
   say from the publish statement what the audience can learn.

Slices 5 to 8 depend on slice 4 and not on each other.

## 16. Deferred

- Requester-bound lookups ("my leave balance"), which need connector-level
  `user_delegated` calls.
- Group chats, @mention gating, and group audiences.
- Named-user and named-Space audiences.
- Chat platforms other than Telegram. Each adapter owns its platform's identity
  model, such as tenant-scoped ids or linking from an enterprise identity
  provider.
- A knowledge base and retrieval; the readable-files allowlist stands in for its
  scope.
- Per-requester and per-Assistant rate limits.
- Schedules and webhooks running as a service account.
- Per-run workload identity and brokered credentials for service accounts.

## 17. Open Questions

1. Should the publish statement be stored with the Assistant revision it
   approved, so an audit can show what the owner was told?
2. Is a deterministic outcome report enough, or should the front door phrase the
   releasable result in a follow-up turn? Decide from the validation run.
3. Does the validation run show front-door answers from readable files alone are
   accurate enough to keep worker dispatch rare?
4. How long are Assistant conversations kept, and who may delete them, given
   that their requesters cannot see or delete them in Portal?
