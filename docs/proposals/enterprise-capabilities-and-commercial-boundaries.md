# Enterprise Capabilities And Commercial Boundaries — Memo

> **简体中文：** [阅读中文镜像](../zh-CN/proposals/enterprise-capabilities-and-commercial-boundaries.md)
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
- [Enterprise Capability Baseline](#enterprise-capability-baseline)
- [Agent Lifecycle Requirements](#agent-lifecycle-requirements)
- [Conditional Capabilities](#conditional-capabilities)
- [Commercial Options And Suggested Boundary](#commercial-options-and-suggested-boundary)
- [Questions And Decision Evidence](#questions-and-decision-evidence)
- [Likely Destination If Accepted](#likely-destination-if-accepted)

## Outcome And Current Constraints

An enterprise should be able to let its teams use Agents continuously while
controlling identity, access, data, execution, and cost, and recovering from
failures through an accountable operating process.

This memo records the enterprise-capability and paid-feature discussion. The
concrete interest is whether BuildMax should offer enterprise capabilities as
a commercial product. No customer interviews, procurement requirements, or
willingness-to-pay evidence were supplied in that discussion. The capability
list below is a discovery baseline, not proof of demand or a release checklist.

BuildMax remains Alpha. Its roadmap targets a dependable private-deployment
Beta for one trusted Space on a private network; SSO is outside that gate.
The existing Space boundary and shared Agent runtime remain the starting
constraints. Enterprise packaging must preserve independent local use and
must not introduce another execution or authorization owner.

## Existing Documentation

Several parts already have a home. This memo adds the cross-cutting capability
and commercial discussion; it does not replace their implementation status or
repeat their technical designs. Consult current code and the roadmap when an
older record uses historical phases or describes an earlier baseline.

| Concern | Existing home | What this memo adds |
|---|---|---|
| Corporate identity | [Enterprise identity and access](../design/enterprise-identity-and-access.md) | Place SSO and provisioning within the broader enterprise offer |
| Sessions and automation credentials | [Client sessions and API credentials](client-sessions-and-api-credentials.md) | Connect revocation to unattended work and enterprise account lifecycle |
| Roles, quota, and audit | [Space governance](../design/space-governance.md), [administration operations](system-administration-operations.md) | Distinguish foundational controls from possible centralized-management additions |
| Secrets and execution trust | [Space secrets](../design/space-secrets.md), [trust harness](../design/trust-harness.md), [sandbox boundaries](../design/sandbox-boundaries.md), [plugin distribution](../design/plugin-space-distribution.md) | Identify enterprise policy and integration questions without claiming those extensions exist |
| Deployment and recovery | [Enterprise deployment](../design/enterprise-deployment.md), [Beta readiness](../deploy/beta-readiness.md) | Separate operating requirements from commercial support commitments |

## Goals And Non-Goals

Goals are to give customer discovery a concrete capability baseline, preserve a
useful community product, and identify a small enterprise offering that can be
delivered and verified against a real customer journey.

Non-goals are to approve an enterprise edition, change pricing or licensing,
reorder the roadmap, create implementation backlog items, or promise production
readiness. This memo does not authorize a new organization hierarchy, custom
policy language, license server, or separate runtime. Each would need a concrete
requirement that existing concepts cannot satisfy.

## Enterprise Capability Baseline

These are outcomes to evaluate for a target enterprise deployment. The depth
of each control depends on that deployment's users and threat model. Entries
describe desired capabilities, not a shipped-feature inventory.

| Area | Typical capabilities | Acceptance question |
|---|---|---|
| Identity and personnel | Corporate SSO; account disablement; session revocation; a documented joiner/leaver process, automated when needed | Can an employee join through the corporate identity system and lose access promptly on departure? |
| Authorization and isolation | Space/team roles; resource isolation; service accounts and scoped, revocable API credentials where automation needs them | Can a user or Agent access only the resources and credentials its work permits? |
| Audit and accountability | Login, authority changes, configuration changes, credential-use metadata, and execution records; authorized search/export and external collection when required | Can an operator reconstruct who authorized and performed an action, and identify missing evidence? |
| Data and secrets | Retention/deletion rules; redaction; encrypted transport and credential storage; rotation; declared storage locations and model-provider destinations | Can the operator explain where data goes, remove it according to policy, and replace compromised credentials? |
| Agent execution governance | Model/tool/plugin admission; enforced execution boundaries; destination controls as required; high-risk action approval when required; cancellation and emergency suspension | Can the operator constrain execution and stop further work with clearly documented limits? |
| Usage and cost | Usage attribution by the dimensions customers need; budgets/quotas; alerts; concurrency and rate limits | Can the operator assign spend, bound unattended consumption, and explain refusal or overrun behavior? |
| Operation and delivery | Monitoring and alerts; backup/restore; upgrade/rollback; diagnostics; supported deployment procedures and support ownership | Can an operator detect failure, recover data and execution state, and maintain the deployment? |

An execution trace is not by itself an authority audit. A quota counter is not
by itself a hard monetary budget. A manifest with several replicas is not
high-availability qualification. Each claim needs evidence for the complete
behavior it promises. Audit records must not expose secret values.

## Agent Lifecycle Requirements

Enterprise discovery and eventual acceptance tests should cover transitions,
not just administration screens:

- When an employee is disabled, specify treatment of existing sessions, API
  credentials, queued/running TaskRuns, and schedules. Distinguish preventing
  new work from cancelling work already executing.
- When a member loses Space access, specify how pending and running work is
  authorized and whether its results remain accessible. Continuing a Task must
  not silently inherit authority the caller has lost.
- Where approval is required, bind it to the intended action and relevant
  parameters; define invalidation after changes, expiry, retries, and failure.
  A chat message saying “approved” is not a complete authorization mechanism.
- When a model, plugin, tool, or secret is revoked, specify when that decision
  takes effect. Respect run-scoped resolution and plugin pins; do not silently
  hot-load a different environment into a running Agent.
- When spend reaches a limit or an operator suspends execution, specify new
  admission, in-flight calls, cancellation, partial results, and any bounded
  overshoot. Cancellation cannot undo an external action already completed.

These are questions to resolve through the owning designs. They do not claim
that existing lifecycle behavior is missing or that all work must be stopped
under every transition.

## Conditional Capabilities

| Capability | Evidence that makes it necessary |
|---|---|
| SCIM or directory synchronization | Manual joiner/leaver handling cannot meet the customer's scale or revocation requirement |
| Both SAML and OIDC | Target identity systems require both; one usable integration is enough for an initial customer |
| Custom roles and cross-Space administration | Fixed roles and existing Space administration cannot express demonstrated responsibilities |
| External secret providers and workload identity | A named enterprise credential system or short-lived credential requirement; follow the roadmap's existing secrets sequence |
| SIEM delivery, tamper-evident audit, and special retention controls | An explicit investigation, retention, or security requirement beyond the existing audit mechanisms |
| High availability and multi-region recovery | Documented availability, recovery-time, and recovery-point objectives; basic restore capability remains foundational |
| Fully offline deployment | A target environment forbids external dependencies, including model calls, updates, or possible license checks |
| Certifications, 24/7 support, and contractual SLA | Procurement requires them and the organization can sustain the processes, staffing, and evidence |

Security controls necessary to meet a deployment's claimed boundary cannot be
deferred merely because the associated integration is a later commercial idea.

## Commercial Options And Suggested Boundary

| Option | Value sold | Trade-off |
|---|---|---|
| Open software with paid implementation and support | Deployment, integration, maintenance, and accountable assistance | Small initial product investment, but revenue depends on delivery capacity |
| Community core with paid enterprise additions | Corporate integrations and centralized governance | Recurring product value, but needs explicit feature, packaging, licensing, and maintenance decisions |
| Managed or dedicated deployment | Reduced operating burden and a supported environment | Adds hosting, security, incident-response, and ongoing service obligations |

The discussion favors preserving a complete community product while validating
paid enterprise additions and implementation services. This is a recommendation,
not an adopted commercial or licensing policy.

- Community baseline: independent local execution, self-hosting, core Agent
  capabilities, basic collaboration, existing permissions/audit/quota controls,
  execution safety, and foundational recovery. Avoid withdrawing existing open
  capabilities to manufacture a paid tier.
- Possible enterprise additions: corporate identity lifecycle integrations,
  centralized policy and cost administration, enterprise audit/secret-system
  integrations, and controlled plugin distribution. Basic SSO's placement is
  still open; the baseline's importance does not automatically decide its price.
- Commercial services: scoped deployment and integration, upgrade/recovery
  assistance, and dedicated support. Offer service levels only when operating
  evidence and staffing support the promise.

Private deployment itself is part of BuildMax's product purpose. The proposed
paid value is additional organizational control and delivery responsibility.
The existing [license](../../LICENSE) is unchanged by this memo. Decide any
future packaging and license terms separately before implementing entitlements.

## Questions And Decision Evidence

Start with three to five prospective enterprise teams and record the use case,
users, buyer, deployment environment, blocking requirement, acceptance test,
and willingness to pay. Prefer a paid pilot with explicit scope over inferred
demand from another project's feature list.

Open questions include which customer segment to serve first, what prevents
deployment today, who owns unattended work after personnel changes, which
capabilities belong in the community baseline, and whether customers primarily
want software, implementation, or ongoing operations. Pricing units, procurement
terms, and offline entitlement behavior need evidence before implementation.

A possible first enterprise slice connects corporate login and revocation to
the existing authorization/audit lifecycle. Execution and cost controls should
follow the first customer's demonstrated gaps. This is a hypothesis for customer
discovery, not a competing priority queue; the current Beta work remains governed
by the roadmap and must support any reliability claims made to a pilot customer.

Before accepting a direction, require a named operator journey and budget owner,
agreed community/paid boundaries, a bounded implementation with identified
ownership and failure behavior, and verification of the promised deployment and
lifecycle outcomes. Keep unresolved assumptions explicit.

## Likely Destination If Accepted

Put accepted priority in the roadmap and durable decisions in the relevant
design records. Approved, decomposed main-line work belongs in the backlog;
update operator documentation only as behavior ships. Once the commercial
direction is settled, move its enduring rationale to a design record and retire
this proposal according to the documentation conventions.
