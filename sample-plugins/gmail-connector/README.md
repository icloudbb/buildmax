# Gmail connector prototype

Copy this directory to `<BUILDMAX_HOME>/plugins/gmail-connector`. Create a Google
OAuth desktop client, enable Gmail API, and set `BUILDMAX_GMAIL_CLIENT_ID` to its
client ID. Run `buildmax connect gmail-connector` in a local terminal. Google
may require OAuth app verification before broader use of the requested scopes.
The `gmail.compose` provider scope permits sending messages even though this
connector exposes only draft creation; the CLI allowlist cannot narrow the
provider token itself.

The three declared calls are `profile`, `list_messages`, and `create_draft`.
`create_draft` requires a JSON object containing `message.raw`, a base64url
encoded MIME message, and terminal confirmation. The skill only runs the reads
autonomously. This sample has not been validated against a live Google account;
the automated test uses a local OAuth and API fixture.
