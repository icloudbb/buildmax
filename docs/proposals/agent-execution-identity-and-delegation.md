# Agent Execution Identity, Connectors And Delegation Strategy

> **简体中文：** [阅读中文镜像](../zh-CN/proposals/agent-execution-identity-and-delegation.md)
>
> **Audience:** maintainers, product reviewers, enterprise operators, and security reviewers · **Status:** proposal — under discussion
>
> **Opened:** 2026-10-01 · **Primary domain:** Trust and Security

Related: [roadmap](../ROADMAP.md) R5,
[worker run token](../design/worker-run-token.md),
[Agent execution and Task threads](../design/agent-execution-and-task-threads.md),
[Space secrets](../design/space-secrets.md),
[scheduled Agent execution](../design/scheduled-agent-execution.md),
[client sessions and API credentials](client-sessions-and-api-credentials.md),
[Agent delegation to user applications](agent-app-delegation.md), and the
[enterprise capability inventory](enterprise-capability-requirements.md).

This memorandum surveys current industry and community directions as of
2026-10-03, tests them against BuildMax's present architecture, and frames
choices. It is not a roadmap commitment or a claim that experimental vendor
features are settled standards.

It also records the joint product discussion of enterprise SSO, Agent identity,
application connectors, and controlled execution. Connector connection UX,
transport declarations, and local prototypes remain in
[Agent delegation to user applications](agent-app-delegation.md); this memo owns
their strategic relationship to execution authority, not a second connector
implementation specification.

## Contents

- [Decision Question](#decision-question)
- [Executive Recommendation](#executive-recommendation)
- [Essential Outcome And Current Constraints](#essential-outcome-and-current-constraints)
- [The Identity Problem Is A Tuple](#the-identity-problem-is-a-tuple)
- [Delegated And Independent Authority](#delegated-and-independent-authority)
- [Why The Question Matters Now](#why-the-question-matters-now)
- [Enterprise SSO, Connectors And Executable Work](#enterprise-sso-connectors-and-executable-work)
- [Industry And Community Direction](#industry-and-community-direction)
- [What Is Converging And What Is Not](#what-is-converging-and-what-is-not)
- [BuildMax's Current Position](#buildmaxs-current-position)
- [Representative Scenarios](#representative-scenarios)
- [Strategic Options](#strategic-options)
- [Recommended Target Architecture](#recommended-target-architecture)
- [Authorization Lifecycle](#authorization-lifecycle)
- [Staged Direction If Accepted](#staged-direction-if-accepted)
- [Threat Model And Failure Semantics](#threat-model-and-failure-semantics)
- [Decision Criteria And Evidence](#decision-criteria-and-evidence)
- [Open Product Questions](#open-product-questions)
- [Likely Destination If Accepted](#likely-destination-if-accepted)
- [Sources](#sources)

## Decision Question

When a BuildMax Agent reads enterprise data or changes an external system,
which principal supplies the authority, which workload performs the action,
what credential reaches that workload, and how can an operator revoke and
explain the result?

Which connector operations make the business outcome executable, and how do
enterprise SSO, those operations, and runtime enforcement work together?

The tempting answer is to make every Agent a user. That solves only naming.
It does not decide whether an interactive request should be limited by Alice's
permissions, whether a monthly finance process should stop when its creator
leaves, whether a child Agent can inherit authority, or whether arbitrary Bash
may read a provider refresh token. The decision is therefore about execution
authority and credential custody, not merely an `agent_id` field.

## Executive Recommendation

BuildMax should position itself as the private **Agent execution and governance
plane**, not as another personal Agent and not as a replacement enterprise IdP.
Its differentiator should be the ability to run model- and tool-neutral Agents
inside an organization's boundary while producing a defensible answer to:

> Who requested this, which Agent revision and TaskRun acted, under whose
> authority, against which resource, with which capabilities and approval, for
> how long, and what evidence proves it?

The recommended long-term shape is hybrid:

1. An authenticated human or an explicitly organization-authorized non-human
   principal is the policy subject. An Agent may itself be that principal;
   a separate Space-owned automation principal requires a distinct lifecycle need.
2. A stable Agent identity names the governed actor and revision lineage, but
   does not receive broad authority merely because it exists.
3. Every TaskRun receives an ephemeral workload identity and remains the
   authoritative execution unit.
4. A credential broker exchanges that evidence for a short-lived credential
   bound to one audience, resource, and capability set. Typed connectors and
   remote MCP calls should prefer brokered invocation so the model never sees
   reusable credentials.
5. High-risk effects require an approval bound to the concrete operation and
   parameters, not a conversationally vague approval.
6. Direct environment or file delivery remains a compatibility profile for
   tools that cannot use a broker, clearly labeled as a weaker boundary.

BuildMax should not build the whole target at once. The order should be:
separate provenance from authority, validate one short-lived provider exchange,
validate direct Agent grants for demonstrated shared automation and add a
separate automation principal only when the mandate needs a separate lifecycle,
then add TaskRun workload federation and a broker where they remove a measured
credential risk. Multi-hop delegation should wait for a real cross-trust-domain
Agent-to-Agent use case.

## Essential Outcome And Current Constraints

### Essential user outcome

An enterprise should be able to allow useful autonomous work without granting
an LLM process an unbounded human credential. An operator reviewing an incident
must reconstruct the initiating event, accountable owner, executing software,
effective authority, target, approval, and outcome from durable evidence.

### Evidence that it matters

- Interactive Agents increasingly reach email, source control, ticketing,
  finance, and production systems rather than only generate text.
- Schedules, webhooks, and Agent-to-Agent calls execute when the original human
  may be offline or no longer employed.
- Prompt injection changes the threat model: data consumed by the Agent may
  influence which credentialed action it attempts.
- Enterprise identity vendors and cloud platforms now distinguish end-user
  delegation, autonomous workload authority, Agent registration, credential
  brokering, sponsorship, and per-run workload identity. This is evidence of a
  real control-plane gap, although the product shapes remain immature.

### Constraints that exist today

- Space is BuildMax's ownership and Portal authorization boundary.
- Task owns a continuing thread; TaskRun owns one execution attempt and its
  authoritative result. Execution authority belongs to TaskRun.
- Workers already use a short-lived run token instead of a user access token.
- Space Secrets can deliver run-scoped environment values, but the running
  model-selected process can read delivered values. Short-lived provider
  exchange and workload identity are designed but not implemented.
- Official workers fail closed when their Bash sandbox is unavailable, but
  general outbound network policy and all credential exfiltration paths are
  not yet bounded.
- Local CLI execution intentionally supports a broad, portable tool surface;
  Bash sandboxing defaults off. A connector policy cannot claim to be a hard
  security boundary while arbitrary shell and network access can bypass it.
- BuildMax must remain private-deployment friendly, model neutral, and usable
  without requiring one cloud vendor's identity product.
- The product is Alpha. A coherent domain correction is preferable to a
  compatibility layer around an overloaded `created_by` field.

## The Identity Problem Is A Tuple

The following concepts answer different questions and must not be collapsed:

| Concept | Question answered | Example |
|---|---|---|
| Initiator | What immediately requested the run? | Alice, a schedule, a webhook, or another TaskRun |
| Accountable owner or sponsor | Which human owns the continuing business decision? | Finance automation sponsor |
| Authority principal | Whose grants bound access? | Alice, an explicitly authorized Agent, or a separate automation principal |
| Agent identity | Which governed Agent definition is acting? | `invoice-reconciler` revision 17 |
| Execution identity | Which concrete workload is running now? | TaskRun `tr_...` in one worker Job |
| Credential lease | What proof can be presented to one target? | Five-minute GitHub token for one repository |
| Approval grant | Which specific elevated effect was approved? | Merge PR 418 at commit `abc123` |
| Delegation chain | Through which actors did authority flow? | Alice → manager Agent → deployment Agent |

Authentication proves an identity. Authorization decides what it may do.
Delegation explains why one actor may exercise another principal's authority.
Credential delivery determines whether the actor can copy that authority
elsewhere. Audit records what the system believed and what happened. Treating
these as one "Agent identity" produces either excessive privilege or unusable
automation.

The minimum authorization decision is therefore conceptually:

```text
initiator + authority principal + Agent revision + TaskRun
+ target audience/resource + requested capabilities + expiry
+ approval evidence + delegation provenance
```

This is an evidence envelope, not a requirement for one JWT, database row, or
universal policy language. Each field should become durable state only when a
concrete authorization, revocation, or audit requirement fails without it.

## Delegated And Independent Authority

The essential outcome is to let an Agent act within a clear business mandate,
while preserving who granted authority, who executed, and who is accountable.
Personal mail and team-owned reconciliation demonstrate different mandates;
neither a user account nor an Agent directory entry alone answers both. These
are alternative authority modes, not mutually exclusive kinds of Agent.

| Question | User-delegated authority | Independent organization authority |
|---|---|---|
| Whose grants permit the action? | An authenticated user's grant, limited by consent, target entitlements, and execution policy | An explicit organization grant to a non-human principal, limited by target and execution policy |
| Who is the actor? | The Agent and exact TaskRun, recorded separately from the user | The Agent and exact TaskRun, whether or not the Agent is also the principal |
| Appropriate journey | Alice processes her mail or submits her expenses | Finance runs monthly reconciliation against shared business resources |
| What ends eligibility? | User disablement, lost entitlement, revoked delegation, or expiry | Principal or grant disablement, expiry, or failed sponsor/review policy |
| What does a human sponsor mean? | Accountability for the Agent; not automatic delegation of the sponsor's rights | Accountability for the organization grant; not the credential subject by default |

One Agent may use either mode on different calls. User-delegated execution must
not silently switch to organization authority when the user's access is lost.
Audit should preserve both subject and actor where the provider supports it;
where it does not, BuildMax must retain the distinction and provider correlation.

Treating Agents like employees is useful for inventory, ownership, access
review, audit, and retirement. It does not imply human authentication methods,
an employee's entire role, or a mailbox/user account for every Agent. Registering
an Agent grants no authority. Explicit independent grants are a separate decision.

### When does a separate automation principal earn its cost?

Start with direct, bounded organization grants to a Space-governed Agent if its
identity and authority can share a lifecycle. Creator departure alone proves
the need for organization ownership, not an additional principal entity: a
directly authorized Agent can also survive its creator with sponsor transfer.

A separate Space-owned automation principal is justified only when a named
business mandate must retain its grants while the executing Agent is replaced,
or must authorize several separately identified Agents. Before adding it,
demonstrate that direct Agent grants and deliberate reauthorization do not
satisfy that journey. The separate principal must define eligible actors;
replacing an Agent must never automatically confer its predecessor's authority.

The policy subject, Agent actor, and TaskRun remain distinct roles in the
evidence model even when one stable identity fills both principal and actor.
Independent concepts do not require independent database entities. The first
organization-authority slice must decide this lifecycle boundary explicitly;
the remainder of this proposal uses a separate automation principal as a
conditional example, not a settled prerequisite for autonomous execution.

## Why The Question Matters Now

Personal Agent products optimize for one user carrying their own context and
connections across applications. That makes on-behalf-of access the natural
default: the Agent is a new interface over the user's existing authority.

An enterprise private Agent platform has a different center of gravity:

- work is often Space-owned rather than personally owned;
- schedules and event triggers need durable authority;
- multiple Agents and people may continue the same Task;
- execution occurs inside controlled workers and networks;
- the organization needs inventory, lifecycle, separation of duties, evidence,
  and an exit path from a provider;
- the same Agent may need user authority for one call and organization authority
  for another.

This gives BuildMax a durable advantage if it focuses: it can join identity,
execution isolation, workspace materialization, tool policy, credential
exchange, approval, and trace evidence in one private plane. Its disadvantage
is that consumers already get polished identity and application ecosystems
from large vendors. BuildMax should integrate with those systems and provide a
portable internal contract, not attempt to recreate their directories,
mailboxes, consent screens, and conditional-access engines.

## Enterprise SSO, Connectors And Executable Work

### User outcome and evidence boundary

An employee should be able to delegate a bounded business outcome across
applications, inspect what happened, and retain organizational control over
access and effects. For example, resolving a support issue may require reading
the issue, querying relevant logs, inspecting a repository, and creating a pull
request. Identity alone does not expose those operations; a connector catalog
alone does not authorize their use or prove that the work completed.

Agents and connectors are complementary: the Agent interprets the goal and
selects a course of action; connectors expose executable business operations
and reliable feedback. Useful task coverage depends on both reasoning and
available operations. This is a product hypothesis grounded in the journeys
in this memo, not measured demand for a connector platform or a claim that every
Agent task requires an application connector. Local file and code work may use
ordinary runtime tools.

### Responsibilities in one task

| Concern | Responsibility | Boundary |
|---|---|---|
| Enterprise identity and SSO | Authenticate people and non-human principals, supply trusted identity context, and govern identity lifecycle | Login eligibility is not permission for every API operation |
| Agent authority | Identify the actor, user or organization grant, exact run, target, and approval | Agent registration and sponsor assignment do not confer business access |
| Application connector | Expose named operations with inputs, outputs, effects, target/account selection, and provider error semantics | A connection makes credentials available through an authorized path; it is not a grant to every Agent |
| Agent reasoning and orchestration | Interpret the goal, select operations, react to results, and propose elevated actions | Model output cannot mint authority or approve its own effects |
| Runtime and call enforcement | Check grants and approvals at invocation, apply credential custody policy, persist progress, and correlate outcomes | Hard enforcement requires confinement of bypass paths |
| Target application | Enforce its business rules and resource permissions and report authoritative operation state | The runtime must not recreate or bypass the application's rules |

These are responsibilities, not a requirement for six new services or entities.
SSO, provider token exchange, and a connector's business operation semantics
must compose without becoming one overloaded concept.

### What enterprise SSO changes

XAA/ID-JAG explores extending existing enterprise SSO trust relationships to
cross-application authorization. In participating systems, this can reduce
repeated user consent and connection setup. It still requires target API and
authorization-server support; an application that supports SSO is not thereby
Agent-callable. Stable resource identifiers, operation semantics, and target
business permissions remain application integration work.

The strategic hypotheses are:

- Enterprises can move from inspecting only application membership to reviewing
  which Agent may perform which operation under which authority and task.
- A single Agent can compose existing applications into a work journey while
  preserving distinct target grants rather than receiving a universal token.
- Non-human inventory and sponsor transfer become necessary for autonomous
  workflows whose business ownership outlives an employee.
- Individually permitted operations may compose into an unauthorized outcome:
  CRM read plus email send does not authorize external customer-data export.
  A validated journey may therefore need data-use and destination constraints
  across calls, beyond individual OAuth scopes.
- Credential brokers and gateways can enforce actual invocations where the
  runtime confines direct credential and network access. Authorization loss
  stops future work but does not undo completed effects or erase copied data.

### A connector is a business capability contract

A useful connector must cover the operations required to complete a journey,
not just authenticate or search. Invoice reconciliation may need invoice reads,
order and receipt lookup, exception registration, and result verification;
payment is a distinct effect with its own authority and approval.

For the chosen operations, review:

- business meaning, input/output shapes, preconditions, and read/write effects;
- account, tenant, resource, and credential selection under the effective grant;
- pagination, provider limits, schema drift, and actionable error reporting;
- timeout and partial-success semantics, safe retry or idempotency where
  supported, and a way to establish whether the effect already occurred;
- provider operation IDs, audit correlation, and recovery or compensating
  actions where the business supports them.

The appropriate transport may be HTTP, MCP, or another reviewed integration.
A common tool protocol does not remove provider semantics. Skills can describe
how to combine operations; plugins can package their implementation; neither
installation nor workflow instructions grant access. The same connector can
serve different Agents with different operations and grants. Increasing
connector coverage makes more tasks executable; real Agent use reveals missing
operations and weak recovery semantics. Completed journeys matter more than
connector count.

### Product opportunities and the first evidence gate

These opportunities are strategic inferences, not roadmap commitments:

| Participant | Opportunity to validate |
|---|---|
| Existing enterprise IdP | Reuse directory and application trust to govern Agent identities, delegation, and credential exchange |
| Application provider or connector maintainer | Offer Agent-usable business actions with bounded access, explicit effects, and verifiable outcomes |
| Agent runtime such as BuildMax | Join enterprise authority to private execution, concrete approvals, recovery, and end-to-end evidence |
| Security and governance provider | Detect unowned Agents, excessive combined access, abnormal data movement, and revocation gaps |

For BuildMax, validate one issue-to-logs-to-repository-to-PR journey using the
organization's existing identity provider and a small operation set. Preserve
subject, Agent revision, TaskRun, and provider correlation for every call;
prove denied access, exact-action approval, a timeout with uncertain outcome,
and revocation before the next call or resumed execution. Identify which
controls the IdP, connector, runtime, and target each enforce.

Measure setup and maintenance effort, completed versus manually recovered
tasks, authorization accuracy, duplicate effects after retry, revocation
latency, and audit coverage. This evidence should select the next connector
contract and authority slice. Do not start with a universal manifest, a large
catalog, or a general policy language. Standardized access could lower the value
of connection plumbing; reliable, governed completion of real work is the
strategic hypothesis to test.

## Industry And Community Direction

### Microsoft: first-class Agent directory objects and sponsorship

Microsoft Entra Agent ID separates Agent identities from human and ordinary
application identities. Its documented model supports both autonomous access
and delegated user access, records an accountable sponsor, emits Agent-specific
audit entries, and optionally pairs an Agent identity with a user account only
when a target system requires human-like resources such as a mailbox. Newer
governance material adds sponsor transfer, lifecycle controls, access packages,
and distinct policies for autonomous and on-behalf-of access.

The useful signal is not that BuildMax needs an Entra-shaped object. It is that
stable inventory, technical ownership, business accountability, execution
mode, and compatibility user accounts are separate concerns. A synthetic user
account is an exception for legacy resource models, not the default Agent
identity.

### Google Cloud: attested Agent workload plus credential broker and gateway

Google Cloud Agent Identity uses a SPIFFE-based cryptographic identity tied to
the hosted Agent resource. It explicitly supports user-delegated authority,
the Agent's own cloud authority, machine-to-machine access, and external
credentials managed by an auth manager. In the gateway path, end-user
credentials can be decrypted at the gateway so the Agent never receives the
raw credential. Agent Registry, Agent Identity, and Agent Gateway divide
inventory, authentication, and enforcement.

This is the clearest large-vendor example of the target separation BuildMax
should preserve: workload identity says *which code is running*; brokered
credentials say *what it may call now*; a gateway enforces the call even when
the model is compromised.

### AWS: bind user and workload, then reach outbound credentials

Amazon Bedrock AgentCore documents user-delegated, machine-to-machine, and
on-behalf-of patterns. Its workload access token can bind both end-user and
Agent workload identity and is usable only with AgentCore first-party services,
including outbound credential providers. Runtime-managed Agents cannot extract
that token directly. AWS also documents a weaker caller-supplied `userId`
path and recommends cryptographically verified JWT identity for production.

The useful lesson is that an unverified `user_id` attribute is provenance, not
proof. BuildMax must derive the human subject from an authenticated boundary,
not accept a model or webhook payload saying "act as Alice."

### OpenAI: separate workspace user credentials, service accounts, and run controls

OpenAI's published enterprise material exposes more than one pattern rather
than one universal Agent identity. Workspace Agent access tokens are scoped to
Workspace Agents API operations. Codex access tokens can represent their
workspace creator, while service accounts provide non-human workspace
identities with independent role, group, plugin, expiry, and audit lifecycle.
ChatGPT Work cloud documentation also distinguishes individual, shared, and
Agent-owned accounts for connected applications and notes that a connected
account can differ from the user who requested the task.

The relevant signal for BuildMax is the explicit choice of credential subject.
"The user requested the run" does not prove that every downstream action uses
that user's account, and non-human automation needs its own lifecycle.

### Okta and Auth0: identity providers are becoming delegation brokers

Okta's Agent token exchange supports user and machine authority, resource
connections, Agent-to-Agent calls, audience and scope restriction, and
preservation of the original service identity across hops. Auth0 Token Vault
positions a broker between Agents and downstream OAuth tokens so refresh and
raw user credentials need not be exposed to Agent code.

Okta's September 2026 Agent SSO announcement extends first-class Agent identity
to Cross-App Access (XAA)-enabled integrations. XAA uses an IdP trusted for SSO
to mediate cross-application authorization; its ID-JAG mechanism is discussed
below. A supported product integration does not make every downstream API
compatible, nor establish the draft as a finalized standard.

This suggests that BuildMax's integration boundary should be a provider-neutral
credential exchange interface. An enterprise may use Vault, an IdP STS, a
GitHub App, a cloud STS, or a BuildMax-local provider. The domain model should
describe requested authority without embedding any vendor flow.

### Community standards: useful primitives, no complete Agent authority model

SPIFFE provides attested workload identity and short-lived SVIDs. Its guidance
deliberately keeps volatile roles and access policy out of long-lived identity
claims. OAuth 2.0 Token Exchange separates subject and actor tokens and can
restrict issued tokens by resource, audience, and scope, but exchange does not
automatically link revocation of input and output tokens. The IETF WIMSE group
is working on interoperability between workload identity, OAuth, JWT, SPIFFE,
and multi-hop context; that work confirms the problem and also confirms that
the combined model is not settled.

RFC 8693's JWT `sub` and `act` claims can distinguish the authority subject from
the current actor, with nested `act` claims recording prior actors. That history
does not itself enforce permission attenuation: the issuer must apply exchange
policy, and consumers evaluate the current actor and top-level claims rather
than treating prior actors as additional authorization grants.

**Cross-App Access and ID-JAG (working draft).** The IETF OAuth working group's
Identity Assertion JWT Authorization Grant builds on token exchange and JWT
authorization grants. An IdP issues an assertion for a downstream authorization
server that already trusts it for SSO; the client exchanges the assertion for a
target access token under the participating systems' policies. This supports
cross-domain user delegation without repeating a direct user-approval step at
every target authorization server. It does not grant unrestricted access or
define ownership of autonomous business workflows. As of this review, ID-JAG
is an active Internet-Draft, not a published RFC.

**Verifiable capability delegation (research prototype).** The AIP paper
proposes Invocation-Bound Capability Tokens for MCP, A2A, and HTTP, with signed
JWTs for a single hop and Biscuit policy chains for multi-hop delegation. Its
reference implementations explore verifiable provenance and holder-side
permission attenuation. This is a research proposal, not a protocol requirement
or an established interoperability standard. BuildMax should evaluate the
properties against a real child-Task journey before adopting a token format;
the paper's claims are not qualification evidence for BuildMax.

MCP authorization standardizes OAuth discovery and audience binding for remote
MCP servers and explicitly forbids passing an MCP client's token through to an
upstream API. A2A advertises transport authentication in an Agent Card and
requires servers to authorize calls, but leaves resource and business-action
authorization to the implementation. Neither protocol decides who owns a
scheduled enterprise automation or whether an Agent may exercise a user's
permission for a particular transaction.

## What Is Converging And What Is Not

### Converging direction

1. **Non-human identities are first-class inventory.** They have owners or
   sponsors, lifecycle, status, grants, and audit, rather than being anonymous
   API keys.
2. **User delegation and autonomous authority are distinct modes.** One Agent
   may use both, but the system must know which one applies to each call.
3. **Stable identity and ephemeral execution identity coexist.** A directory or
   Agent definition is the governance anchor; a short-lived workload identity
   represents the running instance.
4. **Credentials narrow at runtime.** Audience, resource, scope/capability, and
   expiration are selected when a target call is made.
5. **Brokers and gateways increasingly hold reusable secrets.** The preferred
   Agent-facing object is a capability or invocation, not a refresh token.
6. **Delegation needs both subject and actor.** Original authority and current
   software actor remain visible across exchange or Agent-to-Agent hops.
7. **Human accountability remains.** Sponsor and owner lifecycle exists because
   an autonomous identity cannot accept organizational responsibility.
8. **Protocol interoperability stops short of business authorization.** MCP,
   A2A, OAuth, and SPIFFE provide components; the enterprise runtime still owns
   admission, policy, approvals, and evidence.

### Not converged

- whether every Agent should receive a directory object or only deployed,
  autonomous Agents;
- whether Agent identity maps to definition, deployment, tenant instance,
  revision, or runtime;
- a portable vocabulary for capabilities below broad OAuth scopes;
- how approval evidence binds to natural-language intent and final parameters;
- how far delegation chains should propagate in asynchronous multi-Agent work;
- how revocation crosses independently issued tokens and offline systems;
- whether credentials may enter Agent memory for arbitrary code tools;
- a standard cross-vendor Agent registry and lifecycle model.

BuildMax should therefore adopt stable primitives and avoid exposing today's
vendor object names as its permanent product model.

## BuildMax's Current Position

### Strong foundations already present

- TaskRun is already the authoritative attempt and is a natural ephemeral
  execution subject.
- The worker run token is short-lived, audience-separated by token type, bound
  to Space, Task, and TaskRun, and intentionally narrower than a user token.
- Space is a credible organizational ownership boundary.
- Agent revisions and TaskRun provenance can identify the code/configuration
  that acted.
- Space Secrets centralize encrypted credential material, audit delivery, and
  redact known values from traces.
- Official workers have a fail-closed sandbox requirement and a single writable
  workspace root.
- Plugins, MCP, hooks, tool permissions, and traces supply useful enforcement
  and evidence attachment points.

### Gaps and overloaded meanings

1. `task_run.created_by` currently contributes both provenance and execution
   eligibility. That works for personal work but cannot express an
   organization-owned schedule that should survive its creator's departure.
2. The run token identifies a user as `sub`; it does not independently name an
   authority mode, automation principal, Agent actor, target, or capability.
3. Space Secrets primarily deliver credentials into the run. Redaction reduces
   accidental disclosure but does not stop a compromised Agent from reading and
   exporting the value.
4. The target service usually sees a provider credential, not the BuildMax
   TaskRun and Agent revision that caused the call.
5. Current approvals and tool permissions are not a universal, parameter-bound
   external-action authorization contract.
6. Schedule `created_by` preserves provenance but makes long-term organizational
   authority dependent on one human unless a new ownership mode exists.
7. General shell and network access can bypass a typed connector or gateway.
8. There is no first-class non-human principal lifecycle, sponsor transfer,
   grant review, or disable action.
9. There is no bounded delegation chain for a parent TaskRun admitting a child
   TaskRun across a trust boundary.

These gaps do not mean BuildMax should immediately add nine entities. They
identify the questions a staged design must answer.

## Representative Scenarios

### 1. Interactive source-code assistant: run as the user

Alice asks an Agent to inspect repositories she can access and draft a pull
request. BuildMax authenticates Alice, records her as initiator and authority
principal, starts a TaskRun with its own workload identity, and requests a
short-lived GitHub credential restricted to one organization and the required
read/write capabilities. The Agent can draft locally without approval; creating
the remote pull request may require confirmation depending on Space policy.

Value: repository access changes when Alice's membership changes; the audit
trail distinguishes Alice from the Agent revision and exact run; no reusable
Alice refresh token is placed in the worker.

### 2. Monthly finance reconciliation: run as the organization

A finance administrator publishes a monthly workflow. It reads a shared inbox,
compares invoices with ERP records, writes an exception report, and proposes
payments. If it uses the creator's identity, the process either breaks when the
creator leaves or silently continues under stale personal authority.

Finance explicitly authorizes its Agent with a human sponsor, fixed resources,
and expiry/review policy. If the business mandate must persist independently of
that Agent, Finance instead uses a separate Space-owned automation principal
with an explicit eligible-actor policy. Each scheduled
TaskRun receives a short-lived identity and brokered credentials. Drafting the
report is autonomous; releasing a payment requires a different capability and
an approval bound to payee, amount, currency, and source record.

Value: personnel changes do not ambiguously own business continuity, and high
risk authority is not bundled with ordinary reconciliation access.

### 3. Incident-response Agent: temporary elevation

An on-call engineer asks an Agent to diagnose a production outage. The normal
grant permits reading metrics and logs. The Agent proposes restarting service
`payments-api` in cluster `prod-sg`, showing supporting evidence. An approver
authorizes that exact operation for ten minutes. The broker issues or exercises
one action-bound capability; changing the cluster, service, or operation voids
the approval.

Value: BuildMax can provide fast intervention without giving the entire run a
general production-admin credential. The evidence ties diagnosis, proposal,
approval, action, and observed result together.

### 4. Customer-support triage: mixed authority in one run

An Agent reads a Space-owned support queue under an organization grant, then
needs the requesting support lead's identity to view a restricted customer
case. It finally posts a sanitized internal summary using the Space-owned bot.

Value: authority is chosen per target call. The system does not pretend that a
single run has one universal identity or copy the most privileged credential
into the whole process.

### 5. Manager Agent delegates to a specialist Agent

A planning Agent asks a deployment specialist to validate a release. The child
receives only repository read, test-environment deploy, and result-report
capabilities; production deploy is not inherited. The chain records the human
or automation principal, parent TaskRun, child Agent revision, and attenuated
grant. The child cannot further delegate unless policy allows another bounded
hop.

Value: multi-Agent composition becomes explainable and least-privileged rather
than a chain of bearer-token forwarding. This should not ship until BuildMax has
a real durable child-Task use case and an acceptance test for attenuation.

### 6. Private deployment with an existing Vault and IdP

A regulated customer runs BuildMax on Kubernetes, uses its own OIDC provider
for people, SPIFFE/SPIRE for workloads, and Vault for database leases. BuildMax
maps its portable authority envelope to those systems instead of forcing a
BuildMax directory. Another offline deployment uses BuildMax-signed TaskRun
JWTs and a local secret provider with the same domain contract.

Value: private deployment becomes an architectural advantage. Enterprises keep
their root of trust while BuildMax supplies the Agent-specific execution and
evidence plane.

## Strategic Options

| Option | Best fit | Advantages | Structural weakness |
|---|---|---|---|
| A. Keep `created_by` authority and improve secret delivery | Alpha simplicity and personal automation | Few concepts; reuses current eligibility checks | Shared automation remains tied to a person; provenance and authority stay overloaded |
| B. Make every Agent a durable principal | Autonomous catalog of enterprise Agents | Clear inventory, disable, grants, and audit | Definition identity becomes confused with each running instance; easy to accumulate broad standing privilege |
| B1. Grant authority directly to selected Space-governed Agents | Autonomous work whose grants and Agent identity share a lifecycle | Reuses the Agent identity; no separate mandate entity | Agent replacement needs deliberate reauthorization; unsuitable when the mandate must outlive the actor |
| C. Always run on behalf of the initiating user | Interactive personal assistants | Natural consent and existing entitlement reuse | Schedules, webhooks, shared work, and creator departure remain unsolved; user credentials become high-value targets |
| D. Add Space-owned automation principals | Shared schedules and team-owned operations | Explicit non-human lifecycle, sponsor, and stable authority | New lifecycle and recovery UX; wrong default could turn every Agent into a service account |
| E. Use only ephemeral TaskRun workload identity | Federated infrastructure and service-to-service calls | Small blast radius; strong per-run audit and revocation-by-expiry | Downstream systems need federation; a stable policy subject is still needed |
| F. Put credentials behind a broker or tool gateway | Typed connectors and high-value APIs | Model need not see reusable credentials; central policy and audit | Arbitrary Bash can bypass it unless network/tool boundaries are enforced; provider integration cost |
| G. Hybrid stable principal + Agent actor + ephemeral run + broker | Mixed interactive and autonomous enterprise work | Handles user, organization, per-run, and per-target concerns without conflation | More moving parts; must be delivered by evidence-driven slices |

### Assessment

Option A is a defensible near-term state, not a durable enterprise answer.
Options B or C alone overfit one product category. B1 is the simpler independent
authority baseline; choose D only when its separate lifecycle is necessary.
Option D solves ownership but
not runtime attestation or secret exposure. Option E solves execution identity
but not business authority. Option F is the strongest credential boundary but
cannot honestly cover unrestricted local tools. Option G is the recommended
target because each concept corresponds to a demonstrated lifecycle failure;
its cost is controlled by delaying each slice until evidence triggers it.

## Recommended Target Architecture

### 1. Three identity layers

**Policy principal.** Either an authenticated human or an explicitly authorized
non-human identity: the Space-governed Agent itself, or a separate Space-owned
automation principal when the lifecycle gate above is met. It owns grants and
is evaluated for active status. Independent organization authority requires a
human sponsor, purpose, expiry or review date, and a disable path. Agent
definitions do not acquire authority merely by being created or published.

**Agent actor.** A stable Agent identity and immutable revision identify what
software configuration acted. It supports inventory, allow/deny policy,
incident search, and revision rollout. Actor identification is not proof of
authority; an explicit grant is required even when the Agent is the principal.

**TaskRun workload.** A short-lived identity names the exact execution. It is
bound to Space, Task, Agent revision, runtime profile, and expiry. It requests
capabilities but cannot expand them.

### 2. Explicit authority modes

Start with a closed set rather than a general policy language:

| Mode | Authority source | Intended use |
|---|---|---|
| `user_delegated` | Authenticated human plus provider consent/grant | Interactive work on personal or user-restricted resources |
| `organization_grant` | Explicit grant to a Space-governed Agent or a justified separate automation principal | Shared schedules, webhooks, and team-owned background work |
| `approved_elevation` | Existing principal plus operation-bound approval | One high-risk effect outside the normal grant |
| `system_internal` | Deployment operator policy | Narrow BuildMax maintenance only; never a shortcut to business data |

A schedule or webhook is an initiator, not an authority mode. It must name an
eligible policy principal. A child Agent is an actor, not a new authority
source; it receives an attenuated grant derived from the parent.

### 3. Portable authority envelope

The control plane should be able to produce and persist an envelope equivalent
to the following example, which uses a separate automation principal. A direct
Agent grant instead names the Agent as `authority.principal_id` while retaining
the actor and exact run as separate evidence:

```json
{
  "initiator": {"type": "schedule", "id": "sch_..."},
  "authority": {
    "mode": "organization_grant",
    "principal_id": "ap_...",
    "grant_id": "gr_..."
  },
  "workload": {
    "space_id": "spc_...",
    "agent_id": "agt_...",
    "agent_revision_id": "ar_...",
    "task_id": "tsk_...",
    "task_run_id": "tr_..."
  },
  "target": {
    "audience": "github",
    "resource": "repo:acme/payments",
    "capabilities": ["contents:read", "pull_requests:write"]
  },
  "approval": {
    "id": "apr_...",
    "operation_digest": "sha256:..."
  },
  "expires_at": "2026-10-01T10:05:00Z"
}
```

This may be split among database snapshots, run-token claims, broker requests,
provider tokens, and audit records. Do not put mutable sponsor roles or general
policy into a long-lived identity certificate. Authorization services should
evaluate current state before issuing each lease.

### 4. Credential exchange, not credential delivery

The preferred flow is:

```text
authenticated trigger
  -> TaskRun authority snapshot
  -> attested TaskRun requests target capability
  -> broker rechecks principal, Agent, grant, target, and approval
  -> broker exchanges or invokes using a short-lived target credential
  -> outcome and provider correlation ID join the TaskRun trace
```

Provider implementations may include GitHub App installation tokens, Vault
dynamic leases, cloud STS, OAuth token exchange, a remote MCP authorization
server, or an on-prem custom provider. The common BuildMax interface should
request audience, resource, capabilities, maximum lifetime, and user/automation
subject; it should not expose a universal OAuth assumption.

For typed tools, prefer brokered invocation where the provider credential never
enters Agent address space. For arbitrary CLI tools that require a credential,
issue the narrowest short-lived lease possible and label the run as credential-
exposed. Static environment injection is the last compatibility tier.

### 5. Approval is a capability, not a chat message

An approval should bind:

- authority principal and approver;
- Agent revision and TaskRun, or an explicitly resumable operation;
- operation name, target resource, and normalized parameter digest;
- maximum effect, expiration, and use count;
- policy version and displayed human-readable intent.

If relevant parameters change, approval is requested again. Approval does not
turn the rest of the run into an administrator.

### 6. Audit two views of every consequential call

The internal view records the complete BuildMax envelope and decision. The
external view records which provider principal or token performed the action
and its correlation ID. Operators need both because downstream systems may see
a GitHub App or service account while BuildMax knows the originating user,
Agent revision, and TaskRun.

## Authorization Lifecycle

1. **Register:** publish or activate an Agent revision. Optionally associate it
   with allowed Space(s), runtime profile, targets, and a sponsor. Registration
   grants no external authority.
2. **Grant:** a user delegates selected provider access or an administrator
   grants the eligible non-human principal bounded capabilities. Record who
   approved it, why, resources, review/expiry, and whether delegation is
   allowed.
3. **Admit:** a trigger creates a TaskRun. The service authenticates the
   initiator, resolves the authority principal and mode, checks current
   eligibility, and snapshots the decision inputs.
4. **Attest:** the worker proves it is the admitted TaskRun and correct runtime.
   In the first slice this may be the run token; later it may be OIDC or SPIFFE
   workload identity.
5. **Exchange or invoke:** for each target, the broker rechecks current state,
   attenuates capabilities, validates approval, and obtains a short-lived
   credential or performs the call.
6. **Renew:** long runs renew only after another eligibility and policy check.
   Renewal is not automatic proof that the original principal remains valid.
7. **Record:** decision, target, operation, parameters or digest, provider
   correlation, output classification, and result join the bounded trace and
   durable audit stream.
8. **Revoke:** disabling a user, automation principal, Agent, grant, connection,
   or Space stops new leases immediately. Existing leases expire quickly or
   are revoked where the provider supports it. Running work enters an explicit
   cancelled or authority-lost result rather than silently continuing.
9. **Review and retire:** sponsors periodically review standing grants. Retiring
   an Agent or automation principal revokes connections and leaves immutable
   historical attribution.

## Staged Direction If Accepted

### Stage 0: correct the vocabulary and evidence model

- Separate `initiator`, `authority_principal`, `authority_mode`, and Agent actor
  in design and traces; preserve `created_by` as provenance until code changes
  are accepted.
- Define a fixed capability request and authority snapshot shape without a new
  general-purpose policy DSL.
- Make documentation explicit that a schedule creator is not necessarily the
  durable authority owner.
- Define terminal behavior for authority loss and credential renewal failure.

Success evidence: the six scenarios above can be represented without claiming
that a webhook or Agent definition is a human principal.

### Stage 1: one short-lived provider exchange

Implement one valuable provider already anticipated by Space Secrets, such as
a GitHub App installation token or Vault dynamic lease. Bind issuance to the
TaskRun, target, maximum lifetime, and requested capabilities; add redacted
audit evidence and revocation tests.

Success evidence: no static provider credential enters the worker for the
selected journey, a stolen token fails at another audience/resource where the
provider permits, and disabling the grant stops renewal.

### Stage 2: organization authority and the principal lifecycle decision

Validate a named organization-owned workflow using direct Agent grants first.
Require a sponsor, purpose, active/disabled state, grant set, created/updated
provenance, and review or expiry. Add a separate automation principal only when
the mandate must survive Agent replacement or span multiple actors and deliberate
reauthorization cannot satisfy the journey. Define and check eligible actors;
never automatically transfer grants to a replacement Agent. Personal schedules
continue using `user_delegated`; team schedules must deliberately convert to
`organization_grant`.

Success evidence: creator offboarding stops personal automation but does not
orphan an explicitly organization-owned workflow; sponsor transfer and disable
are understandable and audited. Replacing an Agent either requires explicit new
grants or an audited change to the separate principal's eligible actors.

### Stage 3: TaskRun workload federation

Implement a rotating issuer/JWKS and audience-bound TaskRun token, or a SPIFFE
bridge for deployments that already operate SPIRE. Keep the portable BuildMax
claims small: immutable run and workload properties, not volatile roles.

Success evidence: at least one real target trusts TaskRun federation, key
rotation and expiry work, and one static worker credential is removed rather
than merely adding another token.

### Stage 4: brokered connector and approval path

Move one typed high-value connector or remote MCP action behind a service that
holds refresh/static credentials, evaluates policy, and performs or exchanges
the call. Bind high-risk writes to normalized operation parameters.

Success evidence: the Agent cannot retrieve the reusable credential, direct
network bypass is either blocked in the hardened runtime profile or explicitly
reported, and audit joins internal and provider-side identities.

### Stage 5: bounded Agent-to-Agent delegation

Only after durable child Agent Tasks cross an authorization boundary, record a
delegation chain with maximum depth, audience, and strictly non-increasing
capabilities. Never forward an upstream bearer token as the mechanism.

Success evidence: a child cannot gain a capability absent from the parent
grant, each hop is visible, loops/depth are bounded, and revocation stops new
downstream leases.

### Explicitly deferred

- a universal enterprise policy language;
- a global cross-organization Agent identity network;
- automatic user accounts, inboxes, or mailboxes for every Agent;
- provider-independent transaction semantics for every external application;
- an Agent choosing its own authority mode or sponsor;
- claiming that connector policy secures unrestricted local Bash.

## Threat Model And Failure Semantics

| Threat | Required response |
|---|---|
| Prompt injection asks for a secret | Prefer brokered invocation; never place reusable credentials in model context or trace |
| Confused deputy presents a valid token to the wrong service | Bind and validate audience/resource; forbid token passthrough |
| Webhook or model claims to be Alice | Derive user identity only from authenticated server context; treat payload identity as untrusted data |
| Creator leaves but schedule continues | Personal authority becomes ineligible; organizational authority continues only with an active authorized non-human principal and valid sponsor policy |
| Shared service account launders privilege | Preserve initiator, Agent, TaskRun, target, and provider correlation in addition to the shared external principal |
| Stolen run or provider token is replayed | Short expiry, single audience, narrow resource, proof-of-possession where available, and run/lease correlation |
| Approval is reused for a different action | Bind operation, normalized parameters, resource, expiry, and use count |
| Parameters change after approval | Recompute digest at execution; mismatch fails closed and requests new approval |
| Child Agent expands authority | Each hop exchanges an attenuated grant; set maximum depth and no implicit onward delegation |
| Grant is revoked during a long run | Broker denies new/renewed leases; runtime surfaces `authority_lost` and stops affected work |
| IdP, Vault, or STS is unavailable | Do not fall back to static or broader credentials; retry within bounded lease safety, then fail visibly |
| Signing key rotates | Publish overlapping verification keys, stop new issuance on compromised keys, and keep token lifetime short |
| Arbitrary Bash bypasses the broker | Hardened profiles restrict egress/tooling; otherwise mark the boundary as advisory and keep exposed leases narrow |
| Credential appears in logs or traces | Redact known material, structurally exclude secret fields, scan fixtures, and treat trace failure as separate from credential policy |

The system should fail closed on authorization and credential issuance, even
though ordinary trace recording remains fail-open. Loss of observability must
not silently grant authority; loss of authority must not be reported as a
successful Agent result.

## Decision Criteria And Evidence

### Product criteria

- A user can tell whether a run acts as them or as the organization before it
  executes.
- A sponsor can find, disable, review, and transfer every non-human authority
  they own.
- A TaskRun can use different authority subjects for different target calls
  without receiving their reusable credentials.
- Private deployments can integrate an existing IdP, Vault, cloud STS, or
  SPIFFE deployment without adopting a BuildMax-hosted identity root.
- Local broad-tool workflows remain possible, but their weaker credential
  isolation is explicit rather than marketed as equivalent security.

### Security acceptance journeys

1. **Interactive GitHub:** Alice can create a pull request in one repository;
   removal of her repository access stops the next exchange or call.
2. **Organizational schedule:** monthly reconciliation survives creator
   departure only after explicit conversion to organization ownership and an
   active sponsor.
   Sponsor transfer does not expand grants. Agent replacement requires new
   direct grants or explicit authorization of the new actor by the separate
   principal; the old actor loses eligibility when retired.
3. **Production elevation:** a restart approval cannot authorize a deploy,
   another cluster, another service, or changed parameters.
4. **Audience theft:** a TaskRun or target token copied to another service/run
   is rejected.
5. **Credential custody:** the Agent cannot print the provider refresh token or
   long-lived key because it never enters Agent address space in the brokered
   path.
6. **Delegation attenuation:** parent-to-child authority cannot increase and
   cannot exceed the configured chain depth.

### Operational evidence

- issuance and broker latency at normal and peak load;
- revocation latency, including providers that cannot revoke an issued token;
- behavior during issuer, Vault, IdP, network, and clock failures;
- signing-key rotation and backup/restore of encrypted connection state;
- audit join rate between BuildMax and provider correlation IDs;
- rate of approvals, denials, abandoned runs, and bypass requests;
- support burden for private and air-gapped deployment profiles.

### Decision gates

- Do not add a separate automation principal merely for creator independence.
  Prove a mandate lifecycle independent of the executing Agent, and why direct
  Agent grants and deliberate reauthorization cannot satisfy it.
- Do not add a workload issuer until one real relying party can consume it and
  the work removes a static credential or enables measurable enforcement.
- Do not build a generic broker first; qualify one provider and one journey.
- Do not add multi-hop delegation until BuildMax admits durable child Agent
  work across a trust or policy boundary.
- Do not invent a policy DSL while a fixed authority mode, resource, capability,
  expiry, and approval model can express validated needs.
- Do not call the boundary hardened until egress and arbitrary tools cannot
  bypass it in the claimed runtime profile.

## Open Product Questions

1. Can the first organization-authority journey use direct Agent grants? If it
   needs a separate Space-owned automation principal, what mandate must outlive
   the actor, and is that principal visible or scoped behind a Workflow/Schedule?
2. Does every published Agent need a stable identity, or only Agents granted
   autonomous access? What lifecycle event creates and retires it?
3. Should a TaskRun with mixed user and organization authority display one
   primary mode or a call-by-call authority timeline?
4. Which first provider proves the architecture with the lowest product and
   deployment burden: GitHub App, Vault, a cloud STS, or remote MCP OAuth?
5. Is BuildMax ever the source of truth for automation principals, or always a
   mapper to an enterprise IdP when one exists?
6. Which hardened runtime profiles can truthfully guarantee that raw credentials
   and direct network paths are unavailable to Agent code?
7. What is the minimum approval object that survives pause/resume without
   permitting time-of-check/time-of-use substitution?
8. When another Space member continues a Task, should new work always use the
   new member's authority, retain organization authority, or require an explicit
   choice per target?
9. How should sponsor departure behave in deployments without manager hierarchy
   or automated identity provisioning?
10. Which parts of the authority envelope belong in the bounded trace, the
    durable audit log, provider tokens, and operator-visible UI?
11. Which cross-application journey demonstrates useful connector coverage and
    enterprise-managed authorization together, and which missing operation
    actually prevents completion?
12. Which connector semantics must be shared across transports, and which
    provider-specific retry, business rule, and recovery behavior should remain
    in the integration rather than a generic runtime abstraction?

## Likely Destination If Accepted

Accepted rationale should become a focused design record for Agent execution
identity and authority. Concrete slices should update:

- [Agent execution and Task threads](../design/agent-execution-and-task-threads.md)
  for provenance, authority modes, and TaskRun admission;
- [worker run token](../design/worker-run-token.md) for workload claims and
  issuer/audience rules;
- [Space secrets](../design/space-secrets.md) for provider exchange, workload
  federation, and credential custody;
- [scheduled Agent execution](../design/scheduled-agent-execution.md) for
  personal versus organization ownership;
- [Agent delegation to user applications](agent-app-delegation.md) for brokered
  connectors and action-bound approval;
- the roadmap and backlog only after an evidence gate selects an implementable
  slice.

## Sources

The following are evidence of current directions, not endorsements or promises
that BuildMax will reproduce a vendor feature.

### Enterprise platforms and identity vendors

- Microsoft, [What are agent identities?](https://learn.microsoft.com/en-us/entra/agent-id/what-are-agent-identities),
  [Agent identity concepts](https://learn.microsoft.com/en-us/entra/agent-id/key-concepts),
  and [manage Agent identities](https://learn.microsoft.com/en-us/entra/agent-id/manage-agent-identities-admin).
- Google Cloud, [Agent Identity overview](https://docs.cloud.google.com/iam/docs/agent-identity-overview).
- AWS, [AgentCore workload access tokens](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/get-workload-access-token.html).
- OpenAI, [Workspace Agent access tokens](https://learn.chatgpt.com/workspace-agents/authentication),
  [service accounts](https://learn.chatgpt.com/docs/enterprise/service-accounts),
  [access tokens](https://learn.chatgpt.com/docs/enterprise/access-tokens), and
  [ChatGPT Work cloud security](https://learn.chatgpt.com/docs/enterprise/chatgpt-work-cloud-security).
- Okta, [AI Agent token exchange](https://developer.okta.com/docs/guides/ai-agent-token-exchange/secret/main/)
  and [AI Agent lifecycle](https://developer.okta.com/docs/api/secures-ai/ai-agents).
- Okta, [Agent SSO announcement](https://www.okta.com/newsroom/press-releases/okta-brings-first-class-identity-to-ai-agents-with-agent-sso/).
- Auth0, [Token Vault](https://auth0.com/features/token-vault).

### Community protocols and standards

- SPIFFE, [concepts](https://spiffe.io/docs/latest/spiffe/concepts/) and
  [SPIFFE ID and SVID](https://spiffe.io/docs/latest/spiffe-specs/spiffe-id/).
- IETF, [Workload Identity in Multi-System Environments](https://datatracker.ietf.org/group/wimse/about/).
- IETF, [OAuth 2.0 Token Exchange, RFC 8693](https://www.rfc-editor.org/rfc/rfc8693.html).
- IETF OAuth working group, [ID-JAG / Cross-App Access](https://datatracker.ietf.org/doc/draft-ietf-oauth-identity-assertion-authz-grant/)
  (active Internet-Draft as of 2026-10-03; not a published RFC).
- Model Context Protocol,
  [2026-07-28 specification release](https://blog.modelcontextprotocol.io/posts/2026-07-28/)
  and [authorization specification](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/2025-06-18/basic/authorization.mdx).
- A2A Project, [protocol specification](https://github.com/a2aproject/A2A/blob/main/docs/specification.md)
  and [enterprise-ready security guidance](https://github.com/a2aproject/A2A/blob/main/docs/topics/enterprise-ready.md).

### Research prototypes

- Sunil Prakash, [AIP: Agent Identity Protocol for Verifiable Delegation Across MCP and A2A](https://arxiv.org/abs/2603.24775)
  (2026-03-25; research paper with reference implementations, not an accepted standard).
