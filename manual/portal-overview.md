# Portal overview

The Portal is BuildMax's web app. It gives a team a shared place to start work,
run agents in the background, and collect the results — all backed by the same
agent runtime the command line uses. This page orients you to the interface; the
two pages after it walk through the day-to-day tasks.

## Signing in

Open the Portal URL your deployment gives you and sign in with your password, a
single-use login code from an operator, or your organization's single sign-on
when the deployment offers it. Your operator's setup decides which of these you
see and whether new sign-ups are allowed. If you can't get in, that is an
operator question, not something you can change from the browser.

Once you are in, everything you see belongs to a **space** (below), and the app
remembers where you were.

### Confirming a CLI or Desktop sign-in

A BuildMax CLI or Desktop can sign in through the Portal, so it never handles
your password or your organization's sign-in. It shows you a link and a code
such as `WDJB-MJHT`. Open the link, or go to `#/sign-in` on the Portal and type
the code. If you are not signed in to the Portal yet, sign in first; you come
back to the same request afterwards, single sign-on included.

The page shows what you are deciding:

- which client is asking (**BuildMax CLI** or **BuildMax Desktop**) and the
  name it gives itself, such as `alice-mbp · macOS`. That name is **reported by
  the client, not verified**: anyone can make a client claim any name;
- the code, large. Check that it matches the code your CLI or Desktop shows;
- when the request started and when it expires, ten minutes later; and
- the account it signs in as, which is the one you are signed in to the Portal
  with, and when that session ends. It never outlasts your Portal session.

Choose **Confirm sign-in** only if you started this sign-in yourself, just now.
If someone sent you the link or the code, choose **Deny**: a confirmed request
signs *their* client in as *you*. Opening the page decides nothing; only one of
the two buttons does. Afterwards return to your client, which finishes on its
own, and the code is gone from the page's address.

A code that is mistyped, expired, or already confirmed or denied shows one
message, "This code is not valid any more"; start the sign-in again from the
client. If less than an hour of your Portal session is left, the page offers
**Sign in again** first, so the client gets a full-length session.

`buildmax login` and the Desktop sign-in page bring you here; see
[Models & modes](models-and-modes.md#managed-mode).

## The layout

**Left sidebar** — your main navigation:

- **Space switcher** at the top. Your personal space is listed under *Personal*
  (it is called *My Space* until you rename it); shared spaces are listed under
  *Spaces*. The **+** button creates a new space.
- **Chat** — start a conversation by describing what you want done. See
  [Conversations & issues](portal-issues.md).
- **Issues** — where the Portal opens: what is
  [waiting on you](portal-issues.md#waiting-on-you) in the current space, then
  the space's work items.
- **Workflows** — reusable, step-by-step plans. See
  [Agents & workflows](portal-agents-workflows.md).
- **Agents** — saved, reusable agent definitions.
- **Artifacts** — files and outputs produced by runs.
- **Administration** — deployment-wide settings. This appears only if you hold a
  system-administrator grant.

**Top bar** — on the right you'll find a **Help** icon (this manual), a
**Marketplace** icon (plugins this deployment publishes), and a light/dark theme
toggle.

**User menu** — the button at the bottom of the sidebar opens **Account**,
**Remote Control**, **Help**, the interface **Language** (English or 简体中文),
and **Sign Out**. The language is remembered in this browser and starts from the
browser's own language; Help follows it. Desktop has the same choice in its user
menu. Text the server returns, such as an error's detail, and Agent output are
shown as they arrive.

## Your signed-in sessions

**Account → Sessions** lists every browser, CLI, and Desktop signed in to your
account: what it is, the name a CLI or Desktop gave itself (marked **reported by
the client, not verified**), how it signed in, when it signed in and was last
active, and when it ends. The browser you are using is marked **This browser**.

**Sign out** beside any other session ends it: that client is refused on its
next request and has to sign in again. Use it for a CLI or Desktop you no longer
use, or one you confirmed by mistake. **Sign out of this browser** ends the
session you are using and returns you to the sign-in page. Signing out of the
Portal never ends your CLI or Desktop sessions; this list is where you do that.

## Spaces and roles

A **space** is the ownership boundary: issues, conversations, agents, workflows,
uploaded files, and run results all belong to one space and are never visible from
another. Switching spaces in the sidebar changes everything you see.

Spaces have three roles:

- **Owner** — full control, including secrets.
- **Admin** — manage members and space settings.
- **Member** — do work in the space.

Your personal *My Space* is a single-member space that is always yours.

## Space settings

Open **Space** from the user menu (owners and admins can change these):

- **Overview** — the space's basic details and its shared **Agent instructions**.
  Text you put here is sent to *every* background agent run in the space, before
  the selected agent's own instructions. Keep it short, and never put passwords,
  API keys, or other secrets in it, because it is sent with every model call.
- **Members** — invite and manage people and their roles. You invite by email,
  and the address must already have a BuildMax account; when none does, ask a
  system administrator to create one, then invite it. Each pending invitation
  shows whom it is for, with **Revoke**. Only the owner changes roles: raising
  someone to Admin asks you to confirm first, and every change says what was
  saved. Invitations you receive are under your account's **Invitations**,
  each naming the space, who invited you, the role, and when it expires; when
  you accept one, the space you joined becomes your current space.
- **Sandbox defaults** — the default confinement for `Bash` in this space's runs.
  See [Sandbox](sandbox.md).
- **Secrets** — values runs can use, managed by the owner.
- **Service accounts** — identities owned by the space that its automation runs
  as, so work does not depend on one person's account. Owners and admins create,
  rename, disable, and re-enable them. Each has a **sponsor**, an owner or admin
  accountable for it; when the sponsor leaves that role or is disabled, the
  account shows **Needs a sponsor** until an owner or admin chooses
  **Take sponsorship**. A service account is a member of this space only, cannot
  sign in, and never appears when you pick a person, such as an issue's owner.
  Personal spaces cannot have them.
- **Assistants** — service front doors the space publishes on their own
  Telegram bot, such as an HR or audit assistant, for people outside the space's
  own work. Owners and admins define each one's instructions, the Agents and
  published Workflows it may run (with which result fields may be shown to the
  person asking), the uploaded files (Artifacts) it may read, and who may ask: the space's members or
  every active user. Its work runs as a service account, created with its name
  unless you choose one. An assistant starts paused. **Publish** shows exactly
  who can ask and what it can read and run, including the Secrets those Agents
  hold and the space's Files their work reads, and asks you to confirm, because everything it can reach is disclosed to
  everyone who can ask; changing that later on a published assistant asks again.
  **Bind bot** takes a token from [@BotFather](https://t.me/BotFather); it is
  stored encrypted, so the deployment needs `secret.kek_file`, and a bot already
  connected to BuildMax is refused. People reach the bot after linking their
  Telegram account (see [Chat apps](chat-apps.md)). A published assistant
  answers people in its audience in a private chat, can start only its roster's
  Agents and Workflows, and tells each person on its first reply which space
  runs it and that the space can review the conversation, which appears in the
  space's conversations but continues only in the chat. `/new` starts a new
  conversation and `/help` describes the assistant. It answers from its readable
  files directly, without starting work; it reads only text files (Markdown,
  plain text, CSV, JSON, YAML, and the like) up to 128 KiB each. Its Agents and
  Workflow steps do not see those uploads: they read the space's **Files**, so a
  policy both need is uploaded to both. Each Agent and Workflow step it starts
  is told the name and email of the person asking, from their BuildMax account
  and never from what they type in the chat, so an Agent that looks up personal
  records can be told in its instructions to answer only for that person. Of a
  result, the assistant
  and the person asking see only the fields its roster entry marks releasable,
  never raw output, error text, or a link; when an Agent's task or a Workflow
  run it started finishes, the person gets those fields in the chat, or a short
  note if it failed or was stopped. When it
  cannot answer, it escalates: it opens an issue in the space and tells the
  person someone will follow up, which a member does with **Reply to requester**
  on the issue (see [Conversations & issues](portal-issues.md#issue-detail)). A
  schedule can also send its results to someone through it (see
  [Agents & workflows](portal-agents-workflows.md#send-results-to-a-person-through-an-assistant)).
  A paused one says it is paused. Personal spaces cannot have them.
- **Audit** — a record of what happened in the space.

## How models are chosen

Runs in the Portal use models your deployment manages, so you don't paste API keys
into the browser. Which models are available, and how they're accounted, is set by
your operator. For the difference between a model call that goes straight to a
provider and one that goes through a BuildMax deployment, see
[Models & modes](models-and-modes.md).

## Next

- Start and track work: [Conversations & issues](portal-issues.md).
- Build reusable agents and plans: [Agents & workflows](portal-agents-workflows.md).
- Understand the objects behind the screens: [Core concepts](concepts.md).
