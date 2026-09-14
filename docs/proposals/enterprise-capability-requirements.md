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
- [Candidate Requirement Inventory](#candidate-requirement-inventory)
- [Agent Lifecycle Requirements](#agent-lifecycle-requirements)
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

BuildMax remains Alpha. Its roadmap targets a dependable private-deployment
Beta for one trusted Space on a private network; SSO is outside that gate. The
existing Space boundary and shared Agent runtime remain the starting
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
| Roles, quota, and audit | [Space governance](../design/space-governance.md), [administration operations](system-administration-operations.md) | Collects questions about centralized visibility and control |
| Secrets and execution trust | [Space secrets](../design/space-secrets.md), [trust harness](../design/trust-harness.md), [sandbox boundaries](../design/sandbox-boundaries.md), [plugin distribution](../design/plugin-space-distribution.md) | Identifies policy and integration requirements without claiming they exist |
| Deployment and recovery | [Enterprise deployment](../design/enterprise-deployment.md), [Beta readiness](../deploy/beta-readiness.md) | Connects deployment controls to measurable operating outcomes |

## Goals And Non-Goals

Goals are to give discovery a concrete vocabulary, identify lifecycle and
failure cases that feature lists often miss, and define the evidence needed to
promote a candidate requirement into an accepted product requirement.

Non-goals are to define a community-versus-enterprise feature boundary, approve
an enterprise edition, make a business-model recommendation, change pricing or
licensing, reorder the roadmap, create implementation backlog items, or promise
production readiness. This inventory does not authorize a new organization
hierarchy, custom policy language, license server, or separate runtime. Each new
concept would need a concrete requirement that existing concepts cannot satisfy.

## Candidate Requirement Inventory

The following are candidate outcomes to evaluate for a target enterprise
deployment. They are not a universal minimum set. The required depth depends on
the deployment's users, operating model, and threat model, and every accepted
requirement needs a measurable acceptance test.

| Area | Candidate capabilities | Acceptance question |
|---|---|---|
| Identity and personnel | Corporate SSO; account disablement; session revocation; a documented joiner/leaver process, automated when needed | Can an employee join through the corporate identity system and lose access promptly on departure? |
| Authorization and isolation | Space or team roles; resource isolation; service accounts and scoped, revocable API credentials where automation needs them | Can a user or Agent access only the resources and credentials its work permits? |
| Audit and accountability | Login, authority changes, configuration changes, credential-use metadata, and execution records; authorized search, export, or external collection when required | Can an operator reconstruct who authorized and performed an action, and identify missing evidence? |
| Data and secrets | Retention and deletion rules; redaction; encrypted transport and credential storage; rotation; declared storage locations and model-provider destinations | Can the operator explain where data goes, remove it according to policy, and replace compromised credentials? |
| Agent execution governance | Model, tool, and plugin admission; enforced execution boundaries; destination controls where required; approval of high-risk actions where required; cancellation and emergency suspension | Can the operator constrain execution and stop further work with clearly documented limits? |
| Usage and cost | Usage attribution by the dimensions operators need; budgets or quotas; alerts; concurrency and rate limits | Can the operator attribute consumption, bound unattended use, and explain refusal or overrun behavior? |
| Operation and recovery | Monitoring and alerts; backup and restore; upgrade and rollback; diagnostics; documented deployment procedures and operating ownership | Can an operator detect failure, recover data and execution state, and maintain the deployment? |

An execution trace is not by itself an authority audit. A quota counter is not
by itself a hard monetary budget. A manifest with several replicas is not
high-availability qualification. Each claim needs evidence for the complete
behavior it promises. Audit records must not expose secret values.

## Agent Lifecycle Requirements

Discovery and eventual acceptance tests should cover transitions, not just
administration screens:

- When an employee is disabled, specify treatment of existing sessions, API
  credentials, queued or running TaskRuns, and schedules. Distinguish preventing
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
- When usage reaches a limit or an operator suspends execution, specify new
  admission, in-flight calls, cancellation, partial results, and any bounded
  overshoot. Cancellation cannot undo an external action already completed.

These are questions to resolve through the owning designs. They do not claim
that existing lifecycle behavior is missing or that all work must be stopped
under every transition.

## Conditional Requirements

These candidates become requirements only when a target deployment supplies the
corresponding evidence.

| Candidate capability | Evidence that makes it necessary |
|---|---|
| SCIM or directory synchronization | Manual joiner/leaver handling cannot meet the deployment's scale or revocation requirement |
| Both SAML and OIDC | Target identity systems require both; one usable integration may be enough for an initial deployment |
| Custom roles and cross-Space administration | Fixed roles and existing Space administration cannot express demonstrated responsibilities |
| External secret providers and workload identity | A named credential system or short-lived credential requirement; follow the roadmap's existing secrets sequence |
| SIEM delivery, tamper-evident audit, and special retention controls | An explicit investigation, retention, or security requirement beyond the existing audit mechanisms |
| High availability and multi-region recovery | Documented availability, recovery-time, and recovery-point objectives; basic restore capability remains foundational |
| Fully offline deployment | A target environment forbids external dependencies, including model calls or update services |
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

Open questions include which deployment profiles to study first, what prevents
deployment today, who owns unattended work after personnel changes, which
controls are universal versus environment-specific, and how operators prove
recovery, revocation, and cost-containment outcomes.

A possible first research slice connects corporate login and access revocation
to the existing authorization and audit lifecycle. Execution and usage controls
should follow demonstrated gaps. This is a discovery hypothesis, not a competing
priority queue; the current Beta work remains governed by the roadmap.

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
