# Chat apps (Telegram)

You can talk to your BuildMax assistant from Telegram. It is the same assistant
as Portal chat: it answers questions, starts work in a Space, and tells you in
the chat when work it started finishes. The conversations appear in Portal
chat too.

Your BuildMax server has to have a Telegram bot connected. If
**Account → Chat accounts** in Portal says no chat app is connected, ask your
operator (see [For operators](#for-operators) below).

## Link your Telegram account

1. Open the bot in Telegram. Its name is shown under **Account → Chat accounts**
   in Portal. Send it any message.
2. The bot answers with a link and a code such as `ABCD-EFGH`. Open the link, or
   enter the code under **Account → Chat accounts** and choose **Look up code**.
   You have 10 minutes.
3. Portal shows which Telegram account the code belongs to. Check that it is
   yours, then choose **Link account**. The bot confirms in Telegram.

Only confirm a code you asked for yourself: once linked, messages from that
Telegram account act as you. One Telegram account can be linked to one
BuildMax account.

## Use it

Send a message the way you would type one in Portal chat. The first message
starts a conversation in your personal Space; later messages continue it.
When the assistant starts a task, the bot sends a short report when the task
finishes, fails, or is canceled, with a link to the full result in Portal.

The assistant knows which Space the conversation is in, and can list your
Spaces when you ask. It cannot switch Space for you: send `/space` to do that.
In Portal chat, these conversations are marked **Telegram**.

| Command | What it does |
|---|---|
| `/new` | Start a new conversation in the current Space |
| `/space` | List your Spaces and show the current one |
| `/space <number>` | Switch to that Space and start a new conversation there |
| `/help` | Show the commands and the current Space |

The bot reads text messages only, and only in a private chat with you. It does
not answer in groups, because everyone in a group would see replies about your
Space.

## Unlink

Choose **Unlink** next to the account under **Account → Chat accounts**. The bot
stops acting for you at once; sending it a message again starts a new link.

If your BuildMax account is disabled, or you leave the Space a conversation
uses, the bot stops answering for it right away. The link stays until you
remove it.

## For operators

1. Create a bot with [@BotFather](https://t.me/BotFather) and copy its token.
2. Give the server the token. Prefer the environment, so it stays off disk:

   ```bash
   export BUILDMAX_TELEGRAM_BOT_TOKEN=123456:ABC...
   ```

   or in `server.yaml`:

   ```yaml
   channels:
     telegram:
       bot_token: ""   # or set BUILDMAX_TELEGRAM_BOT_TOKEN
   public_base_url: https://buildmax.example.com
   ```

3. Set `public_base_url` so the bot can send confirmation links. Without it,
   people type the code into Portal themselves.
4. Restart the server. Under **Administration**, **Effective configuration**
   shows `telegram_bot_token` as set.

The bot receives by long polling, so the server needs outbound HTTPS to
`api.telegram.org` and no public URL or ingress. Do not also set a webhook for
the same bot or use its token in another program: Telegram delivers each
message to one receiver, and the others miss messages. With several server
replicas (`coordination.mode: redis`), one replica receives at a time and
another takes over if it stops.

Chat turns use the same conversation model and each user's quota exactly as
Portal chat does. Telegram stores the messages on its own servers; do not
connect a bot if that is not acceptable for your deployment.

## Not yet available

Group chats, other chat apps (Slack, Feishu/Lark, DingTalk, WeCom), streaming
replies, approving tool calls from chat, and reports for workflow runs are not
built.
