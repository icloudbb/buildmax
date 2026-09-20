# MCP servers

[Model Context Protocol](https://modelcontextprotocol.io/) servers give the
agent tools BuildMax does not ship — your issue tracker, your database, your
internal services.

## Connect a remote server from the CLI

```bash
buildmax connect mcp context7 https://mcp.context7.com/mcp
buildmax mcp list
buildmax mcp tools context7
buildmax mcp schema context7 resolve-library-id
buildmax mcp call context7 resolve-library-id --json '{"libraryName":"react","query":"React hooks"}'
```

`connect mcp` checks the endpoint and its tool list, then adds the server to
`<BUILDMAX_HOME>/mcp.json` for all local workspaces. It refuses to replace an
existing server. Use `--transport sse` for an SSE endpoint. Plain HTTP is
accepted only for a loopback test server; other endpoints require HTTPS.

For a server that accepts a static Bearer token, set the token in the process
environment and pass its variable name:

```bash
export MY_MCP_TOKEN=your-token
buildmax connect mcp work https://mcp.example.com/mcp --bearer-env MY_MCP_TOKEN
```

The config saves `bearer_token_env`, never the token value. The environment
variable must also be present for later CLI, Desktop, or Agent runs; a missing
value fails the connection. The CLI creates a new MCP session for each command.
This prototype does not perform MCP OAuth discovery, browser login, refresh, or
multi-account selection. A Skill can call `buildmax mcp tools`, `schema`, and
`call` to express a workflow over these operations. A small example is in
[`sample-plugins/mcp-cli`](../sample-plugins/mcp-cli).

`buildmax mcp call` accepts a JSON object up to 1 MiB. A tool marked read-only
by its MCP server runs directly; every other tool requires typed confirmation
in an interactive terminal. This CLI confirmation is separate from the Agent
runtime's `CallMcpTool` permission rules. A server's read-only annotation is
its own claim, and a local Agent with unrestricted shell access can bypass the
CLI. Treat neither as a per-run application grant.

## Configuration

MCP servers are declared in `mcp.json`, in either or both of:

| File | Scope |
|---|---|
| `<BUILDMAX_HOME>/mcp.json` | Every workspace on this machine |
| `<workspace>/.buildmax/mcp.json` | That workspace only |

Both files are **merged**, with the workspace entry winning when the same server
id appears in both. That is the useful shape: shared servers global, project
specific ones checked into the project.

```json
{
  "mcpServers": {
    "github": {
      "type": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": { "GITHUB_TOKEN": "$GITHUB_TOKEN" }
    },
    "internal-api": {
      "type": "http",
      "url": "https://mcp.internal/api"
    }
  }
}
```

| Field | Meaning |
|---|---|
| `type` | Transport — `stdio` for a local subprocess, or an HTTP/SSE transport |
| `command`, `args` | The process to start, for `stdio` |
| `env` | Environment for that process |
| `url` | Endpoint, for HTTP transports |
| `bearer_token_env` | Optional environment variable name for a remote server's Bearer token |

## Variable expansion

`$VAR` and `${VAR}` are expanded in `command`, `args`, `env` values, and `url`,
against the process environment plus one built-in:

| Variable | Resolves to |
|---|---|
| `${WORKSPACE_ROOT}` | The workspace directory for this run |

This is how you keep secrets out of a checked-in `mcp.json` — reference
`$GITHUB_TOKEN` and let the environment supply it.

Note that the name carries no `BUILDMAX_` prefix. An unrecognized variable
expands to an empty string rather than failing, so a misspelled name shows up as
a path that begins at `/` instead of an error.

## How the agent uses them

MCP tools are not injected into the prompt one by one. Two gateway tools handle
them:

- `LoadMcpTools` — discover what a connected server offers
- `CallMcpTool` — invoke one

This keeps the tool list small no matter how many servers are connected, at the
cost of one extra round trip when the agent first reaches for a server.

## Approval

A server describes each of its tools, and can mark one read-only. BuildMax uses
that to decide whether to ask you before the call: a tool advertised as
read-only runs unprompted, anything else prompts on the CLI TUI and Desktop, and
is refused on surfaces with nobody to ask — print mode, workers, and Portal
conversations.

A server that omits the annotation is treated as writing, because the protocol
cannot tell "not read-only" apart from "did not say". To stop being asked about
a server you trust, answer a prompt with `a` for the session, or write a rule:

```yaml
tools:
  permissions:
    "CallMcpTool:github/*": allow
```

Full detail: [Tool permissions](tool-permissions.md).

## Checking it works

Run `/mcp` in the TUI to see connected servers and their status. A server that
fails to start shows up there rather than failing silently mid-run.

A small test server ships with the repository at `tools/mcp`,
supporting stdio, SSE, and streamable HTTP — useful for verifying an integration
without a real backend.

## Notes

- An MCP server is a process you are starting with your credentials. Treat
  adding one like adding a dependency.
- Hook matchers see MCP calls as `CallMcpTool`, not as the underlying tool name.
- If a web fetch MCP tool is available, the agent is instructed to prefer it
  over the built-in `WebFetch`.

## Related

- [Tools](tools.md) — the built-in tool set
- [Tool permissions](tool-permissions.md) — which calls stop and ask
- [Hooks](hooks.md) — `mcp_tool` is also available as a hook transport
