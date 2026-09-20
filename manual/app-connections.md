# Experimental app connections

A local plugin can declare a small set of HTTP operations in `connector.yaml`.
The CLI connects a user account through OAuth and calls only those declared
operations. This is an experimental local surface; it does not yet provide
workspace or per-run Agent grants.

For a server that already speaks MCP, use [MCP server connections](mcp.md)
instead. That CLI discovers the server's tools rather than declaring fixed HTTP
operations in a plugin.

## Try the Gmail sample

1. Copy [`sample-plugins/gmail-connector`](../sample-plugins/gmail-connector)
   into `<BUILDMAX_HOME>/plugins/gmail-connector` (normally
   `~/.buildmax/plugins/gmail-connector`).
2. Create a Google OAuth **Desktop** client, enable the Gmail API, and set
   `BUILDMAX_GMAIL_CLIENT_ID` to that client's ID. Configure the OAuth consent
   screen and test users as Google requires.
3. Check the declaration with `buildmax plugin validate
   ~/.buildmax/plugins/gmail-connector`, then run `buildmax connect
   gmail-connector` in a local terminal. The command opens an external browser,
   prints the authorization URL as a fallback, waits up to two minutes for a
   loopback callback, and stores the resulting refreshable token.
4. Run `buildmax app gmail-connector profile` or `buildmax app
   gmail-connector list_messages` to perform the two declared reads.

The draft write takes a JSON body of the form
`{"message":{"raw":"<base64url MIME message>"}}`. Run `buildmax app
gmail-connector create_draft --json '<body>'` in an interactive terminal. The
CLI prints the JSON body and requires you to type
`gmail-connector/create_draft` before sending it. A noninteractive Agent call
is refused. Review the decoded MIME message separately; the JSON preview
contains an encoded `raw` value and does not prove what its decoded content is.

Google may require OAuth verification for the sample's scopes. The automated
tests use a local fake provider; no live Google account has been validated.
Google's `gmail.compose` scope also permits sending messages, although this
connector declares only draft creation. The declared operation limit applies
to this CLI, not to other processes that obtain the provider token. See
[Google's Gmail scope list](https://developers.google.com/workspace/gmail/api/auth/scopes)
and [draft API guide](https://developers.google.com/workspace/gmail/api/guides/drafts).

## Connector declaration

One `connector.yaml` sits at the root of an installed plugin. It declares
`auth.authorization_url`, `auth.token_url`, `auth.client_id_env`, `auth.scopes`,
`api_origin`, and named `operations`. Each operation has a fixed `method`,
`path`, and `effect`. The prototype permits GET/read without a body and
POST/write with a JSON body, HTTPS endpoints, and HTTP only for loopback test
providers. Operation paths contain no query, fragment, traversal, or variables.
The plugin never contains tokens or grants. `buildmax plugin validate` checks
the declaration; `buildmax plugin status` lists its operations.

Connection tokens are stored in the operating system credential store by
default, keyed by `BUILDMAX_HOME` and plugin name. If that store is unavailable,
the connection fails. Explicit `BUILDMAX_CREDENTIAL_STORE=file` stores a
mode-0600 JSON token in `<BUILDMAX_HOME>/connections/<plugin>.json`; anyone
with access to the local user account or unsandboxed shell can read this file.
The same variable also affects BuildMax server login storage.

The CLI limits operations to one fixed API host, refuses cross-host redirects,
refreshes expiring tokens, and bounds request and response bodies to 1 MiB.
The provider can still reject scopes, rate limit calls, or change its API.
There is no disconnect command, multi-account selection, durable call history,
or Desktop approval surface yet. Revoke access at the provider to stop future
calls. A local Agent with unrestricted Bash and network access can bypass this
CLI; do not treat the declaration as a general Agent permission boundary.
