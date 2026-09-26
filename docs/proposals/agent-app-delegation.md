# Agent Delegation to User Applications

> **简体中文：** [阅读中文镜像](../zh-CN/proposals/agent-app-delegation.md)
>
> **Status:** proposal — under discussion
> **Opened:** 2026-09-20
> **Related:** [Desktop architecture](../contribute/architecture/desktop.md), [tool permissions](../design/tool-permissions.md), [plugin marketplace](../design/plugin-marketplace.md), [client credentials](client-sessions-and-api-credentials.md), [current state](../current-state.md)

## Contents

- [User outcome and evidence](#user-outcome-and-evidence)
- [Current constraints](#current-constraints)
- [Goals and non-goals](#goals-and-non-goals)
- [Delegation lifecycle](#delegation-lifecycle)
- [Options and trade-offs](#options-and-trade-offs)
- [Application-first connector direction](#application-first-connector-direction)
- [Narrow prototype](#narrow-prototype)
- [Questions and decision evidence](#questions-and-decision-evidence)
- [Possible destination](#possible-destination)

## User outcome and evidence

A person should be able to enter one workspace, tell an Agent what they want by
chat or voice, inspect the work and evidence, and deliberately let it use
selected applications on their behalf. The Desktop could become an entry point
for this work: conversation as the main thread, workspace as the organizing
unit, and files, terminal, browser, and connected applications as inspectable
surfaces. This is a hypothesis from product discussion, not a measured usage
finding. Ordinary work often spans a small set of applications, but we have no
evidence yet that people prefer one Agent entry point or will delegate the same
actions across work, personal, and sensitive contexts.

The immediate test is smaller than a Desktop redesign: can someone connect one
application, allow two useful reads, understand and approve a draft write, and
see what happened? This tests the delegation boundary before deciding how it
belongs in Desktop.

## Current constraints

BuildMax already has local plugins, Skills, MCP tools, tool permissions, and
native credential storage for BuildMax sign-in. It has no general application
connection contract. Skills can describe CLI usage; a generic `curl` Skill can
reach APIs, but forces each Skill to manage HTTP, OAuth refresh, response shape,
and secrets. MCP servers can expose typed tools, though each application still
needs its own server and authorization setup. An application's API may be
incomplete, unavailable, rate limited, or subject to provider review. UI
automation remains a distinct fallback with different reliability and consent.

The local Agent's Bash sandbox defaults off. A model with unrestricted shell
and network access can bypass a connector CLI or invoke it as the user. A YAML
allowlist alone is therefore not an Agent security boundary. Worker isolation,
an in-process tool broker, and revocable per-run grants are separate design work.

## Goals and non-goals

Goals: one comprehensible connection flow; operations named and bounded by a
trusted plugin; credentials stored separately from the plugin and workspace;
human-visible write intent; a traceable outcome; and evidence about where
people expect control in the Desktop journey.

This proposal does not promise a universal API adapter, arbitrary OpenAPI
execution, unattended writes, compatibility with every OAuth provider, or a
claim that desktop shell permissions enforce application delegation. It also
does not make every application a new BuildMax entity. A connection is a user's
credentialed relationship to a provider; a plugin supplies the public
operation contract; a grant is a separate runtime decision.

## Delegation lifecycle

1. **Discover:** A user reviews a plugin's source, provider, requested scopes,
   API host, operations, and effects. Plugin installation does not grant access.
2. **Connect:** The user completes provider consent in an external browser with
   OAuth Authorization Code and PKCE. The connection stores tokens outside the
   workspace. The provider's consent scopes set the outer limit of access.
3. **Grant:** A workspace or run grants a subset of declared operations to its
   Agent. A plugin cannot declare its own grant. Reads and writes can have
   different approval requirements. This step is future work for the prototype.
4. **Execute:** The runtime fixes the destination, method, and operation name;
   it validates input, refreshes tokens, applies policy, and records an outcome.
   Write intent is shown before execution. If a provider rejects or times out,
   the user sees the error and can retry or reconnect.
5. **Review and revoke:** The user can inspect recent calls, disable a grant,
   disconnect locally, and revoke access at the provider. A revoked provider
   token must stop future calls; already completed effects may need a separate
   compensating action. These controls are not yet in the prototype.

The Desktop should display the Agent's proposed action beside its source
conversation and the resulting artifact. Voice can initiate the request, but
high-impact approval needs a visual or equivalently unambiguous confirmation.
Terminal, file, and application views should be available for inspection without
making the user manually perform every step.

## Options and trade-offs

| Option | Useful property | Cost or boundary |
|---|---|---|
| Application-specific MCP server | Typed tools can enter existing runtime permissions | Repeated server installation, credentials, and lifecycle work |
| MCP-backed CLI plus Skill | Reuses existing MCP servers; CLI supports on-demand discovery while Skills express workflows | MCP server quality and authentication vary; CLI invocation alone does not enforce Agent grants |
| Skill plus raw `curl` | Portable and expressive | HTTP and secret handling repeated in prompts; weak central audit |
| Declared connector plus CLI and later broker | Reuses Skills and gives fixed operations a common connection flow | Requires a deliberately small schema and runtime policy to become secure |
| UI automation | Reaches applications with no suitable API | Fragile UI state, broader permissions, harder verification |

The prototype chooses a declared connector inside an existing plugin. Its
`connector.yaml` describes OAuth endpoints, client ID environment variable,
scopes, one API origin, and named fixed GET/POST operations. It does not contain
tokens, workspace grants, scripts, or arbitrary URLs. A Skill may use the CLI
for reads. Writes require terminal confirmation. This is an experiment in
interface shape and ergonomics, not proof of a hardened Agent permission model.

## Application-first connector direction

The next hypothesis is to make the application the user's stable unit of
connection. `buildmax connect gmail` would find a reviewed connector plugin,
show its provider, requested access, operations, and source, then connect an
account. Installing plugin code and authorizing a provider are separate,
explicit state transitions even when one command guides the user through both.
`buildmax app gmail <operation> --json ...` would offer the same CLI surface to
people, Agents, and Skills regardless of whether that connector uses MCP, HTTP,
or gRPC underneath. Existing `buildmax connect mcp <name> <url>` is a
transport-first experiment, not the intended application-facing command.

One plugin would contain a small `connector.yaml` declaration, documentation,
and optional Skills. The declaration would name the application, its connection
method, and stable operations with input/output shapes and effects. A transport
section would bind an operation to an MCP tool, a fixed HTTP method and path, or
a gRPC method with a referenced protobuf descriptor. A Skill would describe
task-level sequences and judgment over those operations. The YAML would never
contain live credentials, user grants, shell commands, or arbitrary executable
expressions. A scaffold command such as `buildmax connector new gmail
--transport http` could generate the manifest, a sample Skill, fixture tests,
and plugin metadata for Marketplace publication; the exact syntax remains open.

This approach can make simple connectors mostly declarative, but YAML alone
cannot implement every provider's OAuth variation, pagination, streaming,
binary payloads, retries, or unusual API semantics. The first contract should
cover demonstrated workflows and expose a reviewed extension point only when
one fails. gRPC needs a schema source and may need streaming support rather
than a YAML-only method name. Provider scopes are an outer limit, while the
runtime must enforce the selected account, declared operation, per-run grant,
approval, and redacted audit at the actual call boundary. A plugin's `effect`
field and an MCP server's read-only hint are claims to review, not authority.

The strongest comparison is one task implemented through two transports with
the same `connect` and `app` commands. Measure setup time, schema drift,
credential recovery, Agent call accuracy, and how clearly a person understands
the granted operations. If the same user-facing contract cannot survive that
test, the common layer is too broad or in the wrong place.

## Narrow prototype

The included `gmail-connector` sample declares two reads (`profile`,
`list_messages`) and one draft write (`create_draft`). `buildmax connect
gmail-connector` performs loopback OAuth with PKCE, and `buildmax app
gmail-connector <operation>` executes a fixed operation. Tokens use the OS
credential store by default; explicit `BUILDMAX_CREDENTIAL_STORE=file` stores
them in a file with mode 0600 on Unix and filesystem ACLs on Windows. The
sample requires the user's own Google OAuth desktop client ID and Gmail API
setup. Google may require verification for the requested scopes; no live Google
account is part of automated tests.
Google's `gmail.compose` scope includes sending email, so the provider token
has more authority than this connector's draft-only write. That gap matters
especially while local Bash remains unsandboxed.

The automated fixture exercises callback state and PKCE, token refresh,
separate token storage, two reads, a confirmed write, refusal of an undeclared
operation and an unconfirmed write, and rejection of unsafe connector URLs.
The experiment does not yet provide per-run grants, a durable call audit,
disconnect, multiple accounts, provider-independent write previews, or desktop
approval UI. Those omissions are evidence boundaries, not silent promises.

A second local prototype tests the MCP-backed path: `buildmax connect mcp
<name> <url>` checks a remote HTTP/SSE endpoint and registers it in the user's
`mcp.json`; `buildmax mcp tools|schema|call` offers CLI discovery and invocation
over the existing MCP runtime. A named environment variable can supply a static
Bearer token without saving it in the config. The CLI creates a new session per
command and requires terminal confirmation unless the server marks the tool
read-only. It does not yet implement MCP OAuth, persistent sessions, per-run
grants, or durable call audit. Compare the two prototypes on the same user
workflow before choosing a primary connection path.

## Questions and decision evidence

Run five to ten observed sessions across a real repeated workflow, including
one failed authorization and one changed write request. Record: whether the
user finds the connection affordance, understands scopes and the exact write,
can identify which workspace and Agent have access, and can recover from token
expiry, provider denial, and partial success. Count calls delegated versus
manual app switches; collect the reasons for switches. Review the provider's
API coverage for the actual tasks rather than guessing that most apps have
useful APIs.

Open decisions: should grants attach to an Agent run, workspace, or durable
Task; how can a local shell be isolated from credentials and network except
through a broker; what audit guarantee is required; how do multi-account and
organization policies compose; how should API and UI automation share one
user-facing approval model; and which applications justify first-party
connectors? A hardened version should prove revocation, least privilege,
token non-exfiltration, and authorization under hostile tool output.

## Possible destination

If sessions show a useful workflow and comprehensible control, move the
accepted authorization and execution contract to `docs/design/`, add prioritized
work to `docs/ROADMAP.md`, and make the Desktop connection and approval journey
an explicit part of its entry-point design. If the observed tasks are better
served by existing MCP tools or ordinary app switching, retire this proposal
and keep only the validated narrow capability.
