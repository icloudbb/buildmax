# Enterprise Capability Requirements Inventory

> **简体中文：** [阅读中文镜像](../zh-CN/proposals/enterprise-capability-requirements.md)
>
> **Audience:** maintainers, prospective enterprise users, and operators · **Status:** proposal — under discussion
>
> **Opened:** 2026-09-13
>
> **Primary domain:** Operations and Deployment

Related: [roadmap](../ROADMAP.md), [current state](../current-state.md),
[enterprise identity](../design/enterprise-identity-and-access.md),
[Space governance](../design/space-governance.md), and
[private-deployment qualification](../deploy/beta-readiness.md).

## Contents

- [Outcome And Current Constraints](#outcome-and-current-constraints)
- [Existing Documentation](#existing-documentation)
- [Goals And Non-Goals](#goals-and-non-goals)
- [Coverage Model](#coverage-model)
- [Detailed Functional Coverage](#detailed-functional-coverage)
- [Cross-Capability Lifecycle Contracts](#cross-capability-lifecycle-contracts)
- [Conditional Requirements](#conditional-requirements)
- [Requirement Assessment](#requirement-assessment)
- [Questions And Evidence](#questions-and-evidence)
- [Likely Destination If Accepted](#likely-destination-if-accepted)

## Outcome And Current Constraints

An enterprise should be able to let its teams use Agents continuously while
controlling identity, access, data, execution, and cost, and recovering from
failures through an accountable operating process.

This paper inventories candidate requirements that commonly arise in enterprise
deployments so discovery can test them against real environments. It does not
define an enterprise edition, reserve any capability for paid distribution, or
recommend commercialization, packaging, pricing, licensing, or services. The
inventory is not proof of demand, a roadmap commitment, a release checklist, or
a description of shipped behavior.

BuildMax remains Alpha. Enterprise deployment, administration, governance, and
execution controls are already active product work. Corporate SSO has an
accepted design: its durable-session and OIDC login/association slices are
implemented with Okta as the named provider, while qualification remains open.
The current code and current-state document remain authoritative about which
slices have actually shipped. This proposal therefore does not reopen whether
to build SSO or duplicate its protocol design. It defines the wider functional
envelope that makes those foundations usable as one enterprise operating
system.

The existing Space boundary and shared Agent runtime remain the starting
constraints. Any validated requirement must preserve independent local use and
must not introduce another execution or authorization owner.

## Existing Documentation

Several parts already have an authoritative home. This inventory connects
cross-cutting requirements without replacing implementation status or repeating
technical designs. Consult current code and the roadmap when an older record
uses historical phases or describes an earlier baseline.

| Concern | Existing home | What this inventory adds |
|---|---|---|
| Corporate identity | [Enterprise identity and access](../design/enterprise-identity-and-access.md) | Relates SSO and provisioning to personnel lifecycle outcomes |
| Sessions and automation credentials | [Client sessions and API credentials](client-sessions-and-api-credentials.md) | Connects revocation to unattended work and account lifecycle |
| Roles, quota, and audit | [Space governance](../design/space-governance.md), [system administration](../design/system-administration.md) | Collects questions about centralized visibility and control |
| Secrets and execution trust | [Space secrets](../design/space-secrets.md), [trust harness](../design/trust-harness.md), [sandbox boundaries](../design/sandbox-boundaries.md), [plugin distribution](../design/plugin-space-distribution.md) | Identifies policy and integration requirements without claiming they exist |
| Deployment and recovery | [Enterprise deployment](../design/enterprise-deployment.md), [Beta readiness](../deploy/beta-readiness.md) | Connects deployment controls to measurable operating outcomes |

## Goals And Non-Goals

Goals are to define the user and operator journeys an enterprise-capable
BuildMax must cover, make the minimum behavior of each capability explicit,
identify lifecycle and failure cases that feature lists often miss, and define
the evidence needed to promote a candidate into an accepted product
requirement. The inventory also identifies where already-planned capabilities
must connect so that a collection of screens does not masquerade as a complete
control loop.

Non-goals are to define a community-versus-enterprise feature boundary, approve
an enterprise edition, make a business-model recommendation, change pricing or
licensing, re-specify OIDC, reorder the roadmap, create implementation backlog
items, or promise production readiness. This inventory does not authorize a new
organization hierarchy, custom policy language, license server, or separate
runtime. Each new concept would need a concrete requirement that existing
concepts cannot satisfy.

## Coverage Model

The proposal uses three coverage levels so “enterprise capability” does not
become an unbounded feature label:

| Level | Meaning | Examples |
|---|---|---|
| Foundation already owned | Existing or active work that this proposal relies on and connects, without taking over its detailed design | Private deployment, System Administration, Space governance, Space Secrets, sandbox and trace, corporate OIDC SSO |
| Core functional coverage | Outcomes that a target enterprise deployment should evaluate as one coherent operating loop | Personnel lifecycle, RBAC and access governance, execution control, audit, data governance, cost control, and recovery |
| Conditional extension | An integration or stronger guarantee added only for a named deployment requirement | SCIM, SAML, custom roles, SIEM delivery, external secret managers, hard egress policy, HA, multi-region, or offline operation |

A foundation can already be partly shipped while the end-to-end enterprise
outcome remains incomplete. Conversely, inclusion in core functional coverage
does not mean every deployment needs the strongest possible implementation.
Each accepted slice still needs a named user, threat or operating constraint,
and measurable acceptance evidence.

## Detailed Functional Coverage

### E1. Corporate identity and personnel lifecycle

**User outcome:** an employee uses the corporate identity system, receives only
BuildMax-owned authority, and loses that authority within the organization's
declared offboarding bound.

The functional coverage is login-method discovery, OIDC sign-in, verified
external-identity association, JIT or existing-only provisioning, local
administrator break glass, account enable/disable, live-session visibility,
single-session and all-session revocation, and a guided joiner/leaver procedure.
The leaver procedure must state the effect on human sessions, machine
credentials, invitations, memberships, schedules, queued TaskRuns, running
TaskRuns, and retained results. The enterprise identity design owns OIDC
protocol and browser details; SCIM and SAML remain conditional extensions.

**Minimum evidence:** one named IdP journey proves first login, repeat login,
association conflict, disabled-account refusal, session revocation, bounded
IdP-only offboarding, IdP outage, and administrator recovery without exposing
provider tokens.

### E2. Deployment and Space administration

**User outcome:** an authorized operator can understand and operate the
deployment without database inspection, while a Space owner can govern one
Space without receiving deployment-wide authority.

The deployment surface covers a redacted health and version overview;
administrators; searchable accounts and sessions; Space metadata, membership,
quota tier, and aggregate usage; model and plugin catalog state; and durable
queue, TaskRun, and worker diagnostics. Routine mutations use one Admin API with
Portal and `buildmax admin` as peer clients. Database-direct
`buildmax-server` commands remain limited to bootstrap and break-glass recovery.
Space settings continue to own membership, fixed roles, ownership transfer,
quota visibility, shared Agent and Workflow governance, and Space audit.

This coverage does not introduce an Organization entity, let deployment
administrators browse Space-authored content by default, or make process-start
configuration look dynamically editable when it is not.

**Minimum evidence:** joiner, access-recovery, leaver, quota-assignment, catalog,
and stuck-run diagnosis journeys produce the same authoritative state and audit
record through every supported routine surface; recovery remains possible when
Portal or the IdP is unavailable.

### E3. Role-based access control and machine access

**User outcome:** every human or unattended caller has an explicit owner,
scope, lifetime, and revocation path, and no credential silently grants more
authority than the current Space policy allows.

BuildMax needs role-based access control (RBAC), but the minimum is not a
user-defined policy platform. The core model has four deliberately separate
layers:

| Layer | Role or principal | Permission boundary |
|---|---|---|
| Deployment | An active `system_admin` grant | Operates accounts, deployment metadata, catalogs, quotas, and recovery; does not imply membership or content access in every Space |
| Space | Membership with `owner`, `admin`, or `member` | Maps through the one authoritative Space action matrix and never grants access outside that Space |
| Protected action | A named domain action such as managing members, Secrets, Agents, Workflows, schedules, or audit | Both route and owning service enforce the same decision; unknown roles and actions deny by default |
| Execution or integration | Worker, webhook, personal token, or future Space-owned service account | Uses a purpose-specific audience and scope; it is not disguised as a human role or session |

The core functional coverage is centralized authorization; cross-Space
isolation; fixed-role descriptions and effective-access visibility; assignment,
change, and revocation through one authoritative service; last effective System
Administrator and Space owner protection; audit of authority changes and
denials; and current-membership checks for starting, continuing, retrying, and
scheduling work. The backend makes every decision; Portal may explain or hide an
unavailable action but is never the enforcement boundary. A role removal takes
effect on the next protected request, while the lifecycle contract below
defines what happens to already-running work.

Human sessions, personal access tokens, Space-owned service accounts, webhook
keys, and TaskRun worker credentials stay separate because they have different
owners and revocation semantics. Machine credentials need least scope, expiry
or rotation, one-time secret display, last-used metadata, disable/revoke
operations, and audit that identifies the machine actor rather than its
creator.

IdP group claims do not bypass BuildMax authorization. Add another built-in
system role only when a named operator needs separation of duties, a custom
Space role only when the fixed roles cannot express a demonstrated
responsibility, and a per-resource ACL only when Space membership is proven too
coarse. Service accounts are likewise built only when a demonstrated unattended
workflow cannot safely use a human-owned credential. None of these conditions
requires a generic policy language by default.

**Minimum evidence:** a generated inventory shows each role's effective actions
and assignments; an authorization matrix covers every protected route, service
mutation, and credential type; and tests include unknown roles/actions,
disabled owners, removed membership, expired scope, cross-Space access,
last-holder concurrency, rotation, and immediate revocation. An implementation
that enforces a rule only in Portal does not pass.

### E4. Agent execution governance

**User outcome:** a Space can constrain what an Agent may execute, understand
the exact environment admitted for a run, and stop future work during an
incident.

The functional coverage is admission of models, tools, plugins, hooks, and
Secrets; worker sandbox posture; any required destination policy; immutable
run-scoped resolution and plugin pins; explicit refusal reasons; schedule pause;
TaskRun cancellation; and a deployment or Space emergency suspension that
blocks new admission. If a target workflow requires approval for a high-risk
action, the approval binds to the action and security-relevant parameters and
has explicit expiry, mutation, retry, and failure behavior.

No generic policy language is required merely to expose existing allow/deny and
lifecycle controls. Revocation changes admission for later work; it does not
silently hot-load a new environment into an executing Agent or pretend to undo
an external side effect that already happened.

**Minimum evidence:** tests and a deployed journey show allowed and denied
admission, run-environment provenance, fail-closed official workers, schedule
pause, cancellation, emergency suspension, and the declared bounded behavior
of work already in flight.

### E5. Audit, investigation, and accountability

**User outcome:** an authorized reviewer can reconstruct who or what changed
authority, configuration, credentials, and execution state, without turning the
audit system into another store of prompts or secrets.

The functional coverage is typed human, service, worker, and system actors;
stable action names; target and Space identity; related TaskRun when relevant;
success or denial outcome; safe timestamps and bounded detail; authorized
search by actor, action, target, Space, and time; CSV or JSONL export; and an
explicit retention/pruning policy whose own operation is visible. Identity
association and authority changes that must never exist without evidence need
transactional audit semantics. Runtime trace, LLM-call accounting, and authority
audit remain separate data products with explicit links and retention rules.

**Minimum evidence:** a reviewer can follow a login or machine credential to a
governed action and related TaskRun, see later revocation, export the same
records, and verify that known credentials, provider tokens, prompts, and tool
output appear nowhere in the audit path.

### E6. Data and secret governance

**User outcome:** an operator can explain where enterprise data and credentials
are stored or sent, apply a declared lifecycle, and recover or rotate them
without crossing Space boundaries.

The functional coverage is a data-flow inventory for database, object storage,
logs, traces, backups, model providers, tools, and plugins; TLS at deployment
boundaries; encrypted managed credentials; write-only secret input; Space- and
run-scoped secret delivery; redaction; key and credential rotation procedures;
and retention/deletion behavior by data class. Backup and restore must cover the
relational/object pairing and document whether audit, traces, credentials, and
in-flight execution can be recovered consistently.

Data residency, customer-managed keys, external secret providers, workload
identity, legal hold, and fully offline operation remain conditional until a
named environment requires them.

**Minimum evidence:** a data-flow review plus rotation, deletion, and restore
drills demonstrate the declared locations and residual copies; seeded secret
values do not appear in responses, logs, traces, audit, artifacts, or another
Space.

### E7. Usage, capacity, and cost control

**User outcome:** an owner can attribute consumption, predict capacity pressure,
and place a tested bound on unattended work.

The functional coverage is usage by Space, Task/TaskRun, model/provider, and
time window; quota-tier assignment; run, token, storage, concurrency, and rate
limits where the deployment needs them; threshold warnings; and clear admission
errors. Every limit defines when it is checked, treatment of queued and in-flight
work, retry behavior, and maximum possible overshoot. Cost views must identify
their price source and freshness; a token quota is not presented as a hard
monetary budget.

**Minimum evidence:** concurrent admission tests and an unattended-work journey
show deterministic attribution, warning, refusal, recovery after the window or
operator change, and a measured overshoot bound.

### E8. Operation, recovery, and supportability

**User outcome:** an operator can install, observe, diagnose, upgrade, recover,
and support BuildMax through documented procedures with known recovery limits.

The functional coverage is a supported deployment shape; configuration
validation; liveness, readiness, dependency, queue, and stale-run signals;
redacted logs and diagnostics; build/configuration identity; backup and restore;
schema upgrade and rollback procedures; lost-run handling; and named operating
ownership. Claims about availability require observed behavior across Server,
coordination, MySQL, object storage, ingress, and worker Jobs rather than replica
counts alone.

**Minimum evidence:** a fresh install, dependency outage, backup/restore,
upgrade/rollback, and stuck- or lost-run drill record detection time, operator
actions, data/execution outcome, and the tested RPO/RTO boundary. High
availability and multi-region recovery remain conditional stronger guarantees.

An execution trace is not by itself an authority audit. A quota counter is not
by itself a hard monetary budget. A manifest with several replicas is not
high-availability qualification. Each claim needs evidence for the complete
behavior it promises. Audit records must not expose secret values.

## Cross-Capability Lifecycle Contracts

Acceptance tests must cover transitions, not just administration screens:

| Event | Contract that must be explicit |
|---|---|
| Account disabled or external identity no longer accepted | New login and user requests; existing sessions; machine credentials; invitations and memberships; schedules; queued and running TaskRuns; result ownership |
| Space membership or role removed | New Space requests; continuation/retry; queued work; running work; later result access; last-owner protection |
| Session or machine credential revoked | Already-issued access; refresh/rotation; concurrent requests; scheduled work; audit and last-used state |
| Model, plugin, tool, hook, or Secret disabled | New admission; queued materialization; pinned running environment; retries and successor TaskRuns; provenance shown to the operator |
| Quota reached or execution suspended | New work; queued work; in-flight model/tool calls; bounded overshoot; partial result; resume behavior |
| Approval expires or approved input changes | Action binding; invalidation; retry; duplicate delivery; failure and audit behavior |
| Dependency or control plane unavailable | Existing sessions and runs; new admission; readiness; retries; break glass; reconciliation after recovery |

These contracts belong in the owning designs and services. The inventory
requires them to agree at the seams; it does not prescribe that every event
must cancel every running action.

## Conditional Requirements

These candidates become requirements only when a target deployment supplies the
corresponding evidence.

| Candidate capability | Evidence that makes it necessary |
|---|---|
| SCIM or directory synchronization | Manual joiner/leaver handling cannot meet the deployment's scale or revocation requirement |
| SAML or multiple simultaneous identity providers | A target identity system cannot use the accepted OIDC path, or issuer migration and multiple workforce populations cannot be handled operationally |
| Native connected-client SSO or device authorization | A named deployment requires managed CLI/Desktop access and cannot use Portal or permitted local login |
| MFA or step-up enforced by BuildMax | The IdP assurance context and ordinary BuildMax authorization cannot satisfy a named sensitive-action requirement |
| Custom roles, access reviews, and cross-Space administration | Fixed roles and existing Space administration cannot express demonstrated responsibilities or review obligations |
| External secret providers and workload identity | A named credential system or short-lived credential requirement; follow the roadmap's existing secrets sequence |
| SIEM delivery, tamper-evident audit, and special retention controls | An explicit investigation, retention, or security requirement beyond the existing audit mechanisms |
| Approval gates, content controls, or hard network destination policy | A named high-risk action, data-loss scenario, or worker threat model that existing admission and sandbox boundaries cannot control |
| High availability and multi-region recovery | Documented availability, recovery-time, and recovery-point objectives; basic restore capability remains foundational |
| Data residency, customer-managed keys, or fully offline deployment | A target environment constrains storage, key custody, external dependencies, model calls, or update services |
| Certifications, formal assurance reports, or response objectives | A documented governance or operating requirement and an organization able to sustain the required process and evidence |

Security controls necessary to meet a deployment's claimed boundary cannot be
deferred merely because only some environments require the associated
integration.

## Requirement Assessment

Discovery should classify each candidate independently. Classification is about
evidence and scope, not product packaging.

| Classification | Evidence threshold | Consequence |
|---|---|---|
| Validated requirement | A named user or operator journey, a current constraint, and a measurable acceptance test | Consider it through the roadmap and owning design |
| Conditional requirement | A requirement is real only for a documented deployment, identity system, threat model, or operating objective | Keep the condition explicit; do not generalize it to every installation |
| Not currently justified | No observed workflow or constraint fails without it | Leave it out and retain the evidence gap as an open question when useful |

Prefer the smallest set of controls that satisfies the demonstrated outcome.
Before adding an entity, hierarchy, policy language, or management surface,
identify the exact acceptance test that existing concepts cannot pass.

## Questions And Evidence

Interview prospective enterprise teams and record the use case, users and
operators, deployment environment, blocking requirement, threat model,
acceptance test, and consequences when the requirement is unmet. Prefer a
bounded pilot with explicit success and failure criteria over inferred demand
from another project's feature list.

Open questions include which deployment profiles to study first, which
non-identity capability prevents adoption after SSO, who owns unattended work
after personnel changes, what emergency-stop boundary operators expect, which
controls are universal versus environment-specific, and how operators prove
recovery, revocation, and cost-containment outcomes.

SSO implementation should supply identity-specific evidence through its owning
design rather than becoming the first research question again. The next useful
discovery slices are the seams it exposes: a complete leaver journey across
sessions and unattended work; a quota/suspension journey across admission and
in-flight execution; and an incident journey from detection through recovery
and audit. These are discovery hypotheses, not a competing priority queue; the
current roadmap still governs delivery order. The first seam has shipped: account
deactivation, execution eligibility, and Space owner recovery are recorded in
[system administration](../design/system-administration.md) §8 and
[Space membership lifecycle](../design/space-membership-lifecycle.md) §5.5, with
their known gaps. Its real-provider leaver qualification remains part of
[enterprise identity](../design/enterprise-identity-and-access.md) Phase 3.

Before accepting a requirement, require a named journey and accountable owner,
a bounded implementation scope with identified ownership and failure behavior,
and verification of the promised deployment and lifecycle outcomes. Keep
unresolved assumptions explicit.

## Likely Destination If Accepted

Put validated priorities in the roadmap and durable decisions in the relevant
design records. Approved, decomposed main-line work belongs in the backlog;
update operator documentation only as behavior ships. Once the useful
requirements have either been accepted with evidence or rejected, move durable
rationale to the owning records and retire this inventory according to the
documentation conventions.
