---
name: gmail
description: "Read the connected Gmail profile or message IDs through the bounded local connector."
---

# Gmail connector prototype

Use `buildmax app gmail-connector profile` to inspect the connected mailbox and
`buildmax app gmail-connector list_messages` to list message IDs. These are the
only operations this skill should run autonomously. Ask the user to perform
`buildmax connect gmail-connector` first if no connection exists.

Creating a draft is an interactive user action. Give the user a proposed subject
and body, then let them run `buildmax app gmail-connector create_draft --json
'{"message":{"raw":"<base64url MIME message>"}}'` in their terminal. The CLI
requires a terminal confirmation. Never try to synthesize its confirmation.
