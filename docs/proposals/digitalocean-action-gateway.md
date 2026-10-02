# DigitalOcean Action Gateway Architecture Assessment

> **Simplified Chinese:** [Read the derived translation](../zh-CN/proposals/digitalocean-action-gateway.md)
>
> **Audience:** contributors, product designers, operators, and security reviewers · **Status:** proposal — under discussion
>
> **Opened:** 2026-10-02

Related: [DigitalOcean managed Agent infrastructure assessment](digitalocean-managed-agent-infrastructure.md),
[Agent execution identity and delegation](agent-execution-identity-and-delegation.md),
[Agent delegation to user applications](agent-app-delegation.md),
[tool permissions](../design/tool-permissions.md),
[Space Secrets](../design/space-secrets.md),
[tools architecture](../contribute/architecture/tools.md), and
[MCP servers](../../manual/mcp.md).

## Contents

- [1. Decision Question](#1-decision-question)
- [2. Essential Outcome And Current Constraints](#2-essential-outcome-and-current-constraints)
- [3. Research Scope And Confidence](#3-research-scope-and-confidence)
- [4. What Action Gateway Is](#4-what-action-gateway-is)
- [5. Reconstructed Architecture](#5-reconstructed-architecture)
- [6. Resource And Authority Model](#6-resource-and-authority-model)
- [7. Tool Discovery And Model Context](#7-tool-discovery-and-model-context)
- [8. Policy And Approval](#8-policy-and-approval)
- [9. Credential Brokerage](#9-credential-brokerage)
- [10. Execution And Failure Semantics](#10-execution-and-failure-semantics)
- [11. Operations, Privacy, Limits, And Cost](#11-operations-privacy-limits-and-cost)
- [12. Comparison With BuildMax](#12-comparison-with-buildmax)
- [13. Lessons For BuildMax](#13-lessons-for-buildmax)
- [14. Candidate BuildMax Direction](#14-candidate-buildmax-direction)
- [15. Evidence-Producing Experiments](#15-evidence-producing-experiments)
- [16. Risks And Open Questions](#16-risks-and-open-questions)
- [17. Likely Destination If Accepted](#17-likely-destination-if-accepted)

## 1. Decision Question

What is architecturally valuable in DigitalOcean Action Gateway, should
BuildMax consume it as an optional external tool broker, and which concepts
should BuildMax adopt without turning a public-preview service into a required
part of the product?

The candidate answer is:

1. The most important design is not the managed MCP endpoint. It is the
   separation of tool eligibility, execution permission, external-account
   authorization, model-context exposure, result projection, and execution
   reliability.
2. BuildMax should preserve TaskRun as the authoritative authorization and
   provenance envelope. A gateway session is a resolved capability attachment,
   not a replacement for TaskRun or Session.
3. BuildMax should evaluate Action Gateway first as an optional remote MCP and
   credential-broker backend. It should not build a provider catalog or a new
   Toolbelt product until actual tool scale and reuse create that need.
4. The highest-value native lesson is to distinguish a provider Connection
   from a Secret. A Secret gives bytes to a run; a Connection lets a broker
   exercise authority without revealing those bytes to the run.

This paper records research and a candidate direction. It does not add an
integration to the roadmap.

## 2. Essential Outcome And Current Constraints

### 2.1 Essential user outcome

A BuildMax user should be able to let an Agent act on an external account with
an understandable scope, an approval at the consequential boundary, and an
audit trail tied to the TaskRun, without handing the Agent a reusable provider
token when a brokered call would suffice.

An operator should be able to choose a managed broker, a privately deployed MCP
server, or direct run-level credentials without changing the meanings of Task,
TaskRun, approval, and result provenance.

### 2.2 Evidence that this matters

BuildMax already has three partial answers:

- remote and stdio MCP configuration, with `LoadMcpTools` and `CallMcpTool`
  keeping individual schemas out of the initial tool list;
- `allow`, `ask`, and `deny` tool policy, per-call risk checks, session grants,
  and interactive approvals; and
- Space Secrets selected by an immutable Agent revision and materialized for a
  TaskRun.

Those foundations still leave a demonstrated gap. A Space Secret is delivered
to the run, where the Agent can read it. The experimental local app connector
keeps an OAuth credential in OS storage and exposes fixed operations, but is not
a Space-scoped, per-run, auditable delegation path. Remote MCP authentication
uses a bearer-token environment variable and has no OAuth, refresh,
multi-account selection, or Server-owned connection lifecycle.

Action Gateway is useful evidence because it treats those missing concerns as
one coherent execution boundary rather than as more environment variables.

### 2.3 Current constraints

- BuildMax must remain useful for local and private deployment. No DigitalOcean
  resource may become a required domain entity.
- Space is the Server ownership and authorization boundary. A free-form
  external actor identifier cannot replace BuildMax account, membership, or
  execution identity.
- TaskRun owns one turn or attempt, its authorization snapshot, and its
  authoritative result. External call logs may supplement but not replace it.
- The current priority remains private-deployment Beta qualification. Any
  optional managed gateway integration follows that gate unless it directly
  closes a qualifying security risk.
- BuildMax is Alpha. If a connection or capability model is accepted, it should
  be implemented coherently rather than hidden behind compatibility shapes.

## 3. Research Scope And Confidence

This assessment was verified on 2026-10-02 using only DigitalOcean's official
documentation and API reference. The main sources are:

- the Action Gateway [overview](https://docs.digitalocean.com/products/managed-agents/action-gateway/concepts/overview/),
  [sessions](https://docs.digitalocean.com/products/managed-agents/action-gateway/concepts/sessions/),
  [actors](https://docs.digitalocean.com/products/managed-agents/action-gateway/concepts/actors/), and
  [connections](https://docs.digitalocean.com/products/managed-agents/action-gateway/concepts/connections/);
- [tool policies](https://docs.digitalocean.com/products/managed-agents/action-gateway/concepts/tool-policies/),
  [server-side approval](https://docs.digitalocean.com/products/managed-agents/action-gateway/how-to/configure-server-side-approval/), and
  [reliable execution](https://docs.digitalocean.com/products/managed-agents/action-gateway/concepts/reliable-execution/);
- [tool search](https://docs.digitalocean.com/products/managed-agents/action-gateway/concepts/tool-search/),
  [invoke](https://docs.digitalocean.com/products/managed-agents/action-gateway/concepts/invoke-tool/),
  [code execution](https://docs.digitalocean.com/products/managed-agents/action-gateway/concepts/code-execution-tool/),
  [Toolbelts](https://docs.digitalocean.com/products/managed-agents/action-gateway/concepts/toolbelts/), and
  [output views](https://docs.digitalocean.com/products/managed-agents/action-gateway/how-to/add-tool-output-view/);
- [custom providers](https://docs.digitalocean.com/products/managed-agents/action-gateway/how-to/add-custom-tool-provider/),
  [production guidance](https://docs.digitalocean.com/products/managed-agents/action-gateway/how-to/use-in-production/), and the
  [public API](https://docs.digitalocean.com/reference/api/reference/action-gateway/);
- [limits](https://docs.digitalocean.com/products/managed-agents/action-gateway/details/limits/),
  [pricing](https://docs.digitalocean.com/products/managed-agents/action-gateway/details/pricing/),
  [usage insights](https://docs.digitalocean.com/products/managed-agents/action-gateway/how-to/track-usage/), and
  [data privacy](https://docs.digitalocean.com/products/managed-agents/action-gateway/details/data-privacy/).

DigitalOcean documents the external behavior, not the private implementation.
Section 5 reconstructs the minimum architecture implied by those contracts and
labels it as an inference. Product claims are current for public preview and
must be re-qualified before a production decision.

No paid or credentialed Action Gateway call was made for this paper. Operational
behavior that depends on a real provider, approval client, failure injection,
or rate limit remains a hypothesis for the experiments in section 15.

## 4. What Action Gateway Is

Action Gateway is a managed tool control and execution plane. It is usable
without Harness Runtime: an Agent can run on a laptop, in another cloud, or in
an application framework and connect to one session-specific Streamable HTTP
MCP endpoint. Python and TypeScript SDKs can also create sessions and invoke
known tools directly.

It combines six capabilities:

1. a catalog of versioned provider operations;
2. late discovery and optional preloading of tool schemas;
3. session-scoped selection and `allow` / `ask` / `deny` policy;
4. OAuth or API-key connections to external accounts;
5. brokered execution, retries, output projection, and trace aggregation; and
6. an ephemeral Python sandbox for computation and programmatic tool calling.

It is not:

- an Agent loop or conversation store;
- a deterministic workflow engine;
- a transaction coordinator across tools;
- an identity provider for the application using it;
- a mechanism that narrows an overprivileged provider token's native scopes;
- a general VPC proxy for every provider call; or
- an enforcement boundary over tools the same Agent can reach outside that
  gateway session.

That last point is critical. A restrictive Action Gateway session does not
constrain a separate MCP server, shell command, browser, or provider SDK already
available to the Agent.

## 5. Reconstructed Architecture

### 5.1 Control plane and data plane

The documented behavior implies two planes:

```text
Control plane

  Public providers -----> versioned tools -----> Toolbelts
          |                       |                    |
  Custom MCP servers             +---- output views  |
          |                                            |
  Team / DO user ---> Actor ---> Connection            |
          |                   provider account          |
          +-------------------------+-------------------+
                                    |
                              Gateway Session
                     selection + policy + context config
                                    |
                              managed MCP URL

Data plane

  Agent / application
          |
          v
  authenticate session owner and team
          |
  resolve immutable session configuration
          |
  check tool selection
          |
  evaluate policy and, if needed, bind approval
          |
  resolve Actor Connection and provider credential
          |
  validate arguments -> execute -> bounded retry
          |
  apply output view -> trace -> return result
```

This is an inference from the public contract, not a claim about DigitalOcean's
internal services. It is nevertheless a useful architectural reading because
each stage has different ownership and failure semantics.

### 5.2 Why the separation matters

A traditional MCP client often collapses all of this into one configuration
entry: server URL, token, available schemas, and execution happen together.
Action Gateway deliberately makes the following axes independent:

| Axis | Question | Action Gateway control |
|---|---|---|
| Catalog eligibility | Can this session reach the operation at all? | Selected tools or Toolbelts |
| Model visibility | Does the model see the schema before asking for it? | Preload versus search |
| Invocation authority | May this exact call run? | Policy and optional argument match |
| External identity | Which provider account performs it? | Actor-selected Connection |
| Result exposure | Which returned fields reach the client/model? | Output view |
| Replay behavior | Can a transient failure be retried safely? | Catalog execution policy and provider idempotency |

These controls cannot substitute for one another. Preloading does not grant
authority; an `allow` rule cannot widen tool selection; approval does not create
an OAuth connection; an output view does not reduce provider scope; and a retry
policy does not make a multi-tool workflow transactional.

### 5.3 Immutable gateway sessions

A gateway session binds an actor, tool selection, policy, instructions,
preloads, output views, and optional VPC attachment to a returned MCP URL.
Configuration is fixed at creation. A change creates a replacement session and
requires clients to use its new URL.

This is a strong property for audit and cacheability: the URL names a stable
capability environment. It is not durable execution state. Reusing the URL does
not preserve `action_code` files, variables, a conversation, or an Agent run.

## 6. Resource And Authority Model

### 6.1 The resource graph

The minimum useful model is:

```text
DigitalOcean Team
  |
  +-- DigitalOcean User (owns Sessions and Connections)
  |      |
  |      +-- Actor ID ----------------------+
  |      |                                  |
  |      +-- Connection -> Provider Account |
  |                                         |
  +-- Provider -> Tool versions             |
  |       `-- optional Custom MCP server    |
  +-- Toolbelt versions                     |
  +-- Output views                          |
  `-- Gateway Session <---------------------+
          `-- MCP URL
```

The public API exposes providers and tools, custom MCP servers, tool health,
Toolbelts, output views, connections, sessions, actors/users, and actor-specific
limits. Their lifecycles are intentionally not one entity.

### 6.2 Actor is a selector, not authentication

An actor ID is an application-chosen stable identifier. A session uses it to
select the provider connections for the person or workload on whose behalf it
acts. It is not a DigitalOcean login, provider username, session ID, or bearer
credential.

DigitalOcean adds an important ownership condition: session and connection must
have the same DigitalOcean owner, team, and actor ID. Another team member cannot
reuse a connection merely by supplying the same actor string.

The application still authenticates its user and chooses the actor. Allowing an
untrusted request to submit an arbitrary actor ID is an account-confusion
vulnerability. The gateway can enforce ownership of the selected record, but it
cannot know whether the application chose the right user.

### 6.3 Provider, credential, and Connection are different

- A provider defines operations and their schemas.
- A credential supplies OAuth client material, an OAuth authorization, or an
  API key.
- A Connection associates authorization to one provider account with one
  owner and actor.

A team-reusable credential lets another team member create their own
Connection; it does not turn the original user's Connection into shared
authority. One owner/actor/provider combination has one Connection. Different
accounts at the same provider require different actor IDs.

This is more precise than treating `GITHUB_TOKEN` as both the integration, the
account identity, and the authority grant.

### 6.4 Toolbelt is a versioned set, not a workflow

A Toolbelt groups up to a documented catalog limit of tools for selection,
group policy, and optional preloading. A reference such as
`toolbelt:research@1` pins membership. Publishing another version does not
change existing sessions.

The Agent still calls each member tool. Toolbelts provide no ordering,
dependency, transaction, provider Connection, or shared approval. They are a
configuration reuse primitive, not an orchestration primitive.

## 7. Tool Discovery And Model Context

### 7.1 Three meta-tools

By default the MCP endpoint exposes three meta-tools instead of thousands of
schemas:

- `action_search` accepts one to five natural-language use cases plus optional
  provider/tag filters and returns a bounded set of names, versions,
  descriptions, and input schemas;
- `action_invoke` validates and runs one or more named tools, with independent
  calls in the same request running in parallel; and
- `action_code` runs Python in a fresh sandbox and may call catalog tools
  through a provided helper.

Search is not invocation. It filters to the session's reachable and permitted
catalog, but the returned candidate is not authorization for particular
arguments. Policy and Connection resolution run again when the tool executes.

### 7.2 Preloading and direct calls

Known, frequently used tools may be preloaded into the MCP tool list. This
removes the discovery turn at the cost of permanently placing their names,
descriptions, and schemas in model context. Preloading neither grants permission
nor prevents discovery of other eligible tools.

An application that already knows the operation and arguments can call it
through the SDK without involving a model, search, or `action_code`. DigitalOcean
explicitly recommends keeping fixed ordering in application code when the
workflow must be deterministic.

The useful design is therefore a spectrum rather than one universal tool path:

```text
fixed application step -> direct SDK call
known Agent operation   -> preloaded tool
open-ended Agent task   -> search then invoke
data-heavy composition  -> ephemeral code calling governed tools
```

### 7.3 Output views

An output view is an immutable, tool-version-specific projection of an object
result. A team-created view retains selected nested fields; DigitalOcean may
also supply public transformed views. The session binds one view to one tool,
and every direct, meta-tool, and `action_code` invocation receives that shape.
The client cannot request full output for one call.

Views reduce data and model tokens after execution. They do not alter the
provider operation, its input, provider scope, or tool charge. Deleting a view
used by an existing session makes calls fail; there is no fallback to full
output.

The architectural lesson is broader than token savings. Result projection is a
data-minimization boundary, but only when the omitted fields are not needed for
correctness and the projection itself is versioned and testable.

## 8. Policy And Approval

### 8.1 Evaluation order

The documented evaluation order is:

1. Refuse a tool outside the session's selection.
2. Evaluate the default action and matching tool, Toolbelt, and argument rules.
3. Use the most specific rule. For equally specific matches, `deny` wins over
   `ask`, which wins over `allow`.
4. If the answer is `ask`, obtain a valid inline or out-of-band approval.
5. Continue through provider authorization and execution.

An omitted default resolves to `ask`, but DigitalOcean recommends making it
explicit. A restrictive session must allow the meta-tools it expects to use;
allowing a discovered provider tool alone does not necessarily allow the
discovery or dispatch mechanism.

The same selection and policy apply to direct calls, `action_invoke`, and calls
from `action_code`. Python is not an escape hatch around governance.

### 8.2 Approval semantics

Inline approval depends on MCP elicitation support in the client. A remote MCP
connection by itself is not evidence that prompts work. When inline elicitation
is unavailable or times out, the gateway returns an approval request for a
separate human decision path.

An out-of-band approval is bound to:

- the team;
- gateway session;
- tool version;
- exact arguments; and
- an expiry.

It is consumed once. The client invokes the same tool with the same arguments
after approval. A denial cannot be overridden, an expired approval cannot be
reused, and approval does not grant provider scopes or create a Connection.

The Agent must not approve its own request. The decision endpoint requires the
session owner's DigitalOcean authority, but the integrating application still
has to keep that credential outside the Agent's reach.

### 8.3 What policy does not prove

Gateway policy controls only calls through the gateway. It does not prove that:

- the provider token is least-privileged;
- a selected tool is correctly classified by its provider;
- the Agent lacks another path to the same API;
- the person approving understands the provider-side effect; or
- a later, similar call is covered by an earlier approval.

Least privilege therefore needs both provider scopes and gateway policy. One
cannot repair the other.

## 9. Credential Brokerage

### 9.1 Execution-time resolution

Action Gateway stores connected-account credentials in DigitalOcean Secrets
Manager and resolves them when the tool executes. The model receives the tool
inputs and result, not the stored OAuth token or API key.

This is materially different from run-level secret delivery:

```text
Run-level Secret
  Secret -> worker environment/file -> Agent process -> provider

Brokered Connection
  Agent -> typed tool request -> gateway -> stored credential -> provider
```

The brokered path reduces credential exfiltration from the Agent environment.
It does not remove delegated authority: a compromised Agent can still use every
operation and argument its session permits.

### 9.2 Authentication methods and lifecycle

A provider may use DigitalOcean's OAuth application, a customer OAuth
application, or an API key. Connections can be pre-authorized or created when
an interactive call reports that authorization is required. Unattended work
must pre-authorize because it cannot stop for provider sign-in.

Authorization can expire, be revoked, lack a required scope, or require
provider-specific settings. Those are Connection failures, distinct from tool
denial and human approval. Approving a call never refreshes OAuth or widens
scope.

### 9.3 Custom providers

A team can register a reachable HTTPS Streamable HTTP MCP endpoint with no
authentication, a stored API key, or per-actor OAuth. Tool discovery imports
its schemas into the catalog, after which the team enables only the tools it
needs. The gateway prefixes slugs with the provider name.

OAuth authorization and token URLs must use HTTPS and the same domain as the
MCP endpoint. A session's VPC attachment does not make a private custom MCP
server reachable through the provider execution path; the public-preview
restriction requires a reachable HTTPS endpoint.

Custom MCP support turns Action Gateway into a governance wrapper around
private integration code, but it also creates a supply-chain boundary. The
gateway can govern a call; it cannot attest that the remote server implements
the advertised semantics safely.

## 10. Execution And Failure Semantics

### 10.1 Validation and parallelism

`action_invoke` validates names, versions, and arguments against catalog
schemas. Multiple calls in one request run in parallel, so array order expresses
no dependency. Dependent calls require another request or `action_code`.

Each underlying call remains independently subject to selection, policy,
approval, Connection, provider rate limits, and billing. Batching is not a way
to bypass limits and is not an atomic unit.

### 10.2 Ephemeral code execution

`action_code` receives Python source and returns stdout, stderr, and exit code.
Every call starts a fresh sandbox: files, variables, and installed packages do
not persist, even within one gateway session. Its timeout includes time spent
calling tools.

Programmatic tool calling can reduce model round trips and keep intermediate
provider results out of the conversation. It is useful for filtering, joining,
and calculation. It is not rollback: completed provider actions survive a
later exception.

A session VPC attachment applies to this code sandbox. It does not route all
catalog-provider traffic through the VPC and does not replace private-service
authentication or network policy.

### 10.3 Retries and idempotency

Retry behavior belongs to each catalog tool, not to the model or session
policy. Eligible transient failures may use bounded attempts, deadline-aware
backoff and jitter, and provider `Retry-After` guidance. Validation errors,
missing Connections, and policy denials are not retry candidates.

Automatic replay is allowed only where catalog configuration establishes a
safety contract, such as read-only semantics or a provider idempotency-key
header. One logical invocation reuses its generated key across automatic
retries. A new `action_invoke` request or a re-run of `action_code` is a new
logical invocation and gets no continuity guarantee.

A timeout is ambiguous: the provider may have completed the write before the
response was lost. Recovery must inspect provider state and retry only missing
effects. Successful retries do not produce exactly-once semantics or a
transaction across tools.

## 11. Operations, Privacy, Limits, And Cost

### 11.1 Observability

Every gateway tool call is traced. Insights aggregates request volume, success
rate, provider and tool usage, status, and provider P50/P95 latency over one-,
seven-, and thirty-day windows.

It is an operational view, not an authoritative execution ledger:

- data may lag live activity by up to fifteen minutes;
- detailed charts use a raw sample capped at 5,000 rows during busy periods;
- aggregate totals remain full-window values; and
- the documented product view does not establish the same ownership and
  lineage as a BuildMax TaskRun trace.

### 11.2 Limits and budgets

Team limits and lower actor-specific limits apply separately to Exa search,
Exa fetch, DigitalOcean actions, and standard third-party actions. Provider
rate and concurrency limits still apply. Each call inside a batch or Python
workflow consumes its own limit.

Action Gateway currently has no per-session or per-product spend cap. Standard
SaaS and MCP invocation is documented at $0.10 per 1,000 calls; search is
included, while code execution, paid provider tools, model inference, and
Harness Runtime have separate charges. A looping or compromised Agent can
consume a prepaid balance, and reaching zero blocks only tools that require
prepayment.

Rate limiting is therefore not a budget boundary. Production use needs an
application-side call budget, loop guard, and cancellation path even when the
gateway enforces RPM.

### 11.3 Privacy and residency

The gateway processes tool inputs, results, connected-account credentials, and
usage data. DigitalOcean says it does not use service content, tool inputs,
tool results, or Agent output to train generalized models. It does collect
limited search signals, with identifiers and status flags rather than prompts,
arguments, or results, for tool-search improvement.

During public preview, Insights is enabled by default and can be disabled only
through support. Service content and Insights telemetry are documented as
processed in United States regions, with no Insights regional localization.
Third-party provider terms and retention still apply to data sent to them.

Public-preview data, records, and execution state may be lost. A private or
regulated BuildMax deployment cannot infer compliance from credential
isolation alone; residency, subprocessors, prohibited-data rules, deletion,
and backup posture require a separate decision.

## 12. Comparison With BuildMax

| Concern | Action Gateway | BuildMax today | Consequence |
|---|---|---|---|
| Agent runtime | External client; no Agent loop | Shared Go loop across local, Desktop, eval, and workers | Complementary, not a runtime replacement |
| Durable authority | Immutable gateway Session config | TaskRun authorization snapshot and authoritative result | Gateway config should attach to, not replace, TaskRun |
| Tool exposure | Selected tools/Toolbelts, search, preload | Runtime registry plus Agent/subagent tool selection and two MCP meta-tools | Similar late binding; BuildMax lacks a large semantic catalog |
| MCP schema context | Search returns bounded relevant schemas | MCP catalog names/descriptions are embedded in `LoadMcpTools`; one full schema is loaded by exact server/tool | Current shape is sufficient at small scale |
| Permission | Selection, default/rules, arguments, allow/ask/deny | Configured policy, argument risk, declared access, tool default, hooks | BuildMax policy covers builtins too; gateway policy is richer for catalog calls |
| Approval | One action, exact arguments/version/session, inline or out-of-band | Per-run prompt ID; allow once or in-memory scope grant | BuildMax has good UI routing but a broader session grant and no persisted action-bound approval record |
| External identity | Application Actor selects user-owned Connection | Account/Space/TaskRun identities; experimental local app OAuth | Do not import free-form Actor as authority; derive a binding from BuildMax identity |
| Credential | Resolved by broker at call time, absent from model context | Space Secret materialized into run environment; remote MCP bearer from environment | Brokered invocation closes a real exposure gap |
| Multi-account | Different actor IDs select different provider accounts | No Server-side MCP OAuth or multi-account Connection | Concrete gap for application delegation |
| Result minimization | Immutable per-tool output views | Bounded/truncated outputs and Secret redaction, no typed projection | Projection is valuable when stable schemas and large results justify it |
| Reliability | Catalog-specific retries and idempotency | Tool-owned behavior; Agent loop guard; no generic MCP retry contract | Central policy is useful, but only with verified tool semantics |
| Audit | Delayed aggregate gateway Insights | Bounded redacted per-run JSONL trace and TaskRun provenance | BuildMax must ingest external facts into its own trace |
| Cost control | Actor RPM, prepaid balance, no per-session spend cap | Task/run quotas, model usage, loop guard; no gateway budget | Preserve BuildMax admission and per-run bounds |
| Private deployment | Managed US service | Local/private product principle | Adapter must be optional |

### 12.1 Existing BuildMax strengths

BuildMax already avoids one common context failure. It exposes two MCP gateway
tools rather than registering every remote tool schema with the model. The
catalog of names and short descriptions is placed in `LoadMcpTools`, and the
full schema is fetched only for the chosen server/tool. This is the same design
family as Action Gateway search and invoke, with deterministic exact lookup
instead of semantic search.

BuildMax's tool policy also applies across local files, Bash, browser, builtins,
and MCP. Action Gateway governs only its own execution plane. `GrantScope`
already ensures that approving one `CallMcpTool` target does not approve every
tool on every server.

TaskRun provenance, portable checkpoints, bounded traces, Secret redaction,
and a product-owned work model are also stronger foundations than gateway-only
operational telemetry.

### 12.2 Structural gaps

The important gaps are:

1. no Server-side Connection resource representing a provider authorization
   without revealing its credential to the run;
2. no brokered tool-execution path tied to TaskRun authority;
3. no OAuth/refresh/multi-account lifecycle for remote MCP or Portal Agents;
4. no immutable resolved capability manifest that records tool versions,
   Connection bindings, policy digest, and result-shape contracts together;
5. no action-bound durable approval suitable for a worker that resumes after a
   separate human decision; and
6. no provider-aware retry and idempotency contract for remote tool calls.

These gaps do not imply six new top-level products. Several belong in one
resolved TaskRun capability attachment and one broker adapter.

## 13. Lessons For BuildMax

### 13.1 Keep six axes separate

BuildMax should explicitly distinguish:

1. **availability** — what tools this run can ever reach;
2. **visibility** — which schemas are initially shown to the model;
3. **permission** — whether this call and arguments may execute;
4. **credential binding** — whose external account will perform it;
5. **result projection** — what returned data reaches the Agent; and
6. **replay policy** — what the executor may retry automatically.

Combining any two creates predictable errors: preloaded tools that accidentally
become grants, approval that silently chooses an account, output truncation
mistaken for data minimization, or generic retries that duplicate writes.

### 13.2 Model Connection separately from Secret

A Secret is appropriate when the task inherently needs credential bytes: a
compiler fetching a private module, `git`, a user-authored script, or an
arbitrary CLI. A Connection is appropriate when a bounded broker can perform a
typed operation and the run needs authority rather than the bytes.

The difference is not storage UI. It is the trust boundary:

| Secret grant | Connection grant |
|---|---|
| Materializes bytes into a TaskRun | Keeps bytes in broker custody |
| Supports arbitrary commands | Supports declared provider operations |
| Agent can exfiltrate value | Agent can abuse only allowed operations |
| Rotation changes delivered material | Rotation stays behind stable Connection identity |

BuildMax should not mutate Space Secret into both concepts. If a concrete
provider journey proves the need, introduce Connection as a distinct resource
with explicit ownership, revocation, health, and audit.

### 13.3 Resolve capability at TaskRun admission

The stable concept BuildMax needs is not another generic Session. At TaskRun
admission, resolve an immutable capability manifest containing only the fields
proven necessary, potentially:

```text
subject and authority mode
selected tool references and versions
connection bindings
effective policy digest
output-shape references
broker backend reference
per-run call and cost bounds
```

The TaskRun owns this snapshot. A gateway-specific session ID or MCP URL is an
infra detail inside the resolved attachment. Rotation or revocation may make a
future call fail, but cannot silently rewrite what the TaskRun was authorized
to attempt.

### 13.4 Derive external actor bindings

If an adapter needs a DigitalOcean actor ID, BuildMax should derive an opaque,
stable value from the authenticated subject and connection purpose. It must not
accept a caller-supplied arbitrary actor as an authorization decision, put an
email address in the identifier, or treat equality of the external string as
proof of BuildMax ownership.

BuildMax remains responsible for deciding whether a personal Connection or a
Space-owned automation Connection may be used by a TaskRun. DigitalOcean's
user/team ownership check is defense in depth, not that decision.

### 13.5 Bind approval to the proposed action

BuildMax's prompt IDs correctly prevent a stale answer from resolving a later
prompt. For durable worker approval, the stronger target is an approval record
bound to TaskRun, tool identity/version, canonical arguments hash, Connection,
policy revision, expiry, and one consumption.

That is narrower than today's optional in-memory “allow this scope for the
session” grant. The existing convenience can remain local; it should not become
the contract for unattended delegated actions.

### 13.6 Adopt search only when catalog scale proves it

`LoadMcpTools` already keeps full schemas late-bound. Semantic search adds
ranking uncertainty, evaluation burden, and search-signal privacy questions.
BuildMax should first make its deterministic catalog bounded and inspectable.
Add semantic discovery only when users routinely face enough enabled tools that
exact provider/tool selection fails.

The evidence threshold should be measured task success, context savings, and
wrong-tool rate across realistic catalogs, not the availability of an embedding
index.

### 13.7 Treat output views as versioned contracts

Generic truncation protects context size but can remove the identifier needed
for a later write. A typed projection is safer only if it is tied to a tool
version, validated against its output schema, visible in the run's resolved
configuration, and tested with empty and optional fields.

BuildMax should not add a global OutputView entity for hypothetical savings.
A first provider adapter may own a fixed projection in code. Generalize only
after two independently configured integrations need reuse.

### 13.8 Centralize replay only with a safety contract

Remote execution benefits from consistent timeouts, backoff, `Retry-After`, and
idempotency keys. The gateway must not infer safety from HTTP method, MCP
read-only hints alone, or an LLM decision. A tool adapter needs an explicit
retry class and, for writes, a provider-supported idempotency mechanism or a
reconciliation operation.

The trace must distinguish logical invocation, provider attempt, ambiguous
completion, reconciliation, and manual retry.

### 13.9 Preserve BuildMax's authoritative trace

An external gateway call should emit BuildMax events for requested, denied,
waiting approval, dispatched, provider-attempted where observable, succeeded,
failed, and completion-unknown. Store the external invocation and approval IDs
as correlation data, never as the only record.

Provider results still pass through BuildMax bounds and redaction before they
enter history, streams, TaskRun result, or artifacts. “The gateway traces it”
is not a reason to weaken those controls.

### 13.10 Do not confuse tool governance with sandboxing

A broker keeps provider credentials away from the Agent but does not contain
filesystem, shell, browser, or network behavior. Conversely, a microVM contains
process effects but does not decide whether a Stripe refund was authorized.

BuildMax must keep sandbox, egress, tool policy, Connection scope, human
approval, and provider authorization as separate defenses.

## 14. Candidate BuildMax Direction

### 14.1 Option A — learn only, no integration

Keep the current MCP path and use this research to improve native capability
resolution and approval. This has no provider dependency and may be sufficient
for private deployments whose integrations are already MCP servers.

It does not close the connected-account credential gap unless BuildMax builds
or adopts another broker.

### 14.2 Option B — configure Action Gateway as ordinary remote MCP

An operator creates a gateway session outside BuildMax and registers its URL as
a remote MCP server. The Agent sees Action Gateway's three meta-tools through
BuildMax's existing `LoadMcpTools` / `CallMcpTool` pair.

This is the smallest technical experiment and tests interoperability. It does
not deliver a product-quality connection journey, because DigitalOcean login,
session ownership, actor choice, policy, Connection lifecycle, and approval
remain outside BuildMax. It also creates a nested discovery path:

```text
LoadMcpTools -> CallMcpTool(action_search)
             -> CallMcpTool(action_invoke)
```

That cost may be acceptable for a spike and undesirable as the final model
interface.

### 14.3 Option C — first-class optional broker adapter

BuildMax creates and resolves Action Gateway sessions through its API, exposes
the selected provider tools through a native broker interface, and maps gateway
events into TaskRun trace. The adapter owns DigitalOcean-specific identity,
session, authentication, and error translation.

This is the candidate production direction only if experiments prove a real
provider journey. The portable interface should describe operations BuildMax
needs — create resolved capability, invoke, await approval, revoke, inspect
health — rather than mirror every DigitalOcean resource.

### 14.4 Recommended sequence

1. **Document and harden the native boundary.** Define the resolved per-run
   capability facts and action-bound approval semantics without adding a
   provider.
2. **Run the remote-MCP interoperability spike.** Measure nested discovery,
   client authentication, approvals, trace correlation, and result handling.
3. **Choose one concrete connected-account journey.** GitHub or Jira is more
   useful than a generic catalog demo because ownership, OAuth refresh,
   approval, and revocation are observable.
4. **Build a provider-specific adapter if evidence is positive.** Avoid a
   generic Connection marketplace in the first slice.
5. **Generalize only after a second backend or provider.** That is the point at
   which shared Connection, projection, or broker interfaces have evidence.

No step requires Toolbelt, Actor, OutputView, or GatewaySession to become
BuildMax top-level entities.

## 15. Evidence-Producing Experiments

### 15.1 Interoperability and context experiment

Connect one Action Gateway session through BuildMax remote MCP and run the same
tasks with exact preloaded tools and search-based discovery.

Record:

- initial tool-schema tokens;
- number of model/tool round trips;
- correct-tool and wrong-tool rate;
- end-to-end latency and tool charges; and
- how nested `LoadMcpTools` / `action_search` affects model reliability.

Test small, medium, and large selected catalogs. Success means the adapter shape
is understandable and the measured benefit survives the extra indirection.

### 15.2 Identity and Connection isolation experiment

Create two application users, two actor bindings, and different accounts at one
provider. Attempt cross-user actor substitution, another team member's session,
revoked OAuth, missing scope, and connection refresh.

Success means every refusal is attributable, no credential enters model/tool
output or BuildMax trace, and BuildMax can explain which authenticated subject
selected which external account without relying on the actor string alone.

### 15.3 Approval experiment

Exercise allow, deny, inline ask, out-of-band ask, expiry, argument change,
tool-version change, duplicate decision, cancellation, and resumed TaskRun.

Success means one approval authorizes exactly one intended call, stale or
changed calls cannot consume it, a deny cannot be softened, and both systems'
records correlate to the same TaskRun and tool call.

### 15.4 Failure and idempotency experiment

Use a test provider that can fail before a write, after committing a write,
during response transmission, under rate limit, and after returning
`Retry-After`.

Success means read retries are bounded, provider-idempotent writes do not
duplicate, ambiguous writes enter a reconciliation state, and a new Agent call
is never mistaken for an automatic retry of the old logical invocation.

### 15.5 Data minimization experiment

Use a provider result containing required identifiers, optional fields,
sensitive metadata, and a large body. Compare full output, gateway output view,
and BuildMax-side projection/redaction.

Success means the Agent completes follow-up actions with fewer tokens, omitted
fields do not leak through traces or alternate return paths, and a tool/view
version change fails visibly instead of silently changing semantics.

### 15.6 Budget and abuse experiment

Trigger repeated search, invocation, parallel batches, and `action_code` loops
under low actor limits and a BuildMax per-run budget.

Success means BuildMax stops the TaskRun before an external prepaid balance is
the only brake, reports partial effects, and distinguishes gateway, provider,
and BuildMax limit failures.

### 15.7 Private-deployment decision experiment

For one candidate customer profile, document region, data class, telemetry,
subprocessors, provider terms, outage behavior, export, deletion, and fallback
to a private MCP server.

Success means an operator can make an explicit deployment choice. Passing a
functional demo is not sufficient evidence for regulated or private use.

## 16. Risks And Open Questions

### 16.1 Product and vendor risks

- Action Gateway is a public preview whose API, limits, pricing, retention, and
  availability can change.
- A managed catalog introduces provider-version and semantic supply-chain risk.
- Session configuration is immutable, but deleting an output view can break an
  existing bound session; immutability is therefore not full dependency
  closure.
- User-owned DigitalOcean Connections may not fit Space-owned unattended
  automation without an explicit service-authority design.
- No per-session spend cap means external cost safety depends on BuildMax and
  operator controls.
- US-only processing and Insights residency may exclude deployments that would
  otherwise value the credential broker.

### 16.2 Architecture questions

1. Is the first required Connection personal, Space-owned, or both? Which real
   journey proves it?
2. Can BuildMax create a least-privilege Action Gateway session without holding
   a broad user DigitalOcean token in the worker or Agent environment?
3. How are gateway sessions revoked or replaced when a TaskRun is cancelled,
   a member loses access, or a policy changes?
4. Which policy is authoritative when BuildMax says `ask` and the gateway says
   `allow`, or vice versa? The safe composition is the stricter answer, but the
   approval UX and error mapping need proof.
5. Can Action Gateway approval be resolved by Portal while preserving BuildMax
   as the user-visible audit authority?
6. Does tool search return enough version and effect metadata for BuildMax to
   explain a proposed action before approval?
7. Can external invocation identifiers be obtained consistently enough to
   reconcile gateway Insights with a TaskRun trace?
8. What is the availability and fallback contract when the broker is down but
   a private MCP alternative exists?
9. Which result data does Insights retain, for how long, and what changes when
   Insights is disabled?
10. Does the latency and token saving of `action_code` justify sending
    intermediate provider data into another managed execution boundary?

## 17. Likely Destination If Accepted

This proposal should not become a permanent parallel architecture document.
If evidence supports native brokered invocation, accepted rationale should move
to:

- [Agent execution identity and delegation](agent-execution-identity-and-delegation.md)
  for subject, authority, delegation, and action-bound approval;
- [Space Secrets](../design/space-secrets.md) or a focused Connection design
  record for the boundary between delivered secrets and brokered credentials;
- [tool permissions](../design/tool-permissions.md) for effective-policy
  composition; and
- [tools architecture](../contribute/architecture/tools.md) plus user-facing
  MCP/connection documentation for shipped behavior.

A DigitalOcean adapter would then become a backlog item only after its concrete
journey, acceptance evidence, and private-deployment trade-off are accepted.
If the evidence does not justify integration, retain the general lessons in the
relevant design records and delete this proposal; git history preserves the
vendor assessment.
