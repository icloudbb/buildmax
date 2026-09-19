# BuildMax Evaluation And Qualification System

> **简体中文：** [阅读中文镜像](../zh-CN/design/评估系统.md)

> **Audience:** contributors, operators, and product designers · **Status:**
> partly implemented — the [section 18](#18-vertical-slice-implementation-plan) vertical slice has
> shipped for the CLI and worker surfaces; conversation and deployment adapters
> and phase 2 onward are planned. The Terminal-Bench 4.0 adapter and importer
> are built and pinned, and run a five-task local regression subset end to end
> with a real model (gpt-5.6-luna solves none of these hard tasks, which is the
> expected pipeline-regression signal, not a score). No wider run exists, so
> there is no Terminal-Bench score yet
>
> **Accepted:** 2026-08-22 · **Roadmap:** R4 post-Beta product-quality measurement

Related: [roadmap](../ROADMAP.md), [product vision](product-vision.md),
[surface positioning](surface-positioning.md), [trust harness](trust-harness.md),
[durable run trace](durable-run-trace.md), [end-to-end testing](end-to-end-testing.md),
and [managed LLM gateway](llm-gateway.md).

## Contents

- [1. Summary](#1-summary)
- [2. Problem And Current Context](#2-problem-and-current-context)
- [3. Product Question](#3-product-question)
- [4. Evaluation Consumers](#4-evaluation-consumers)
- [5. Goals](#5-goals)
- [6. Non-Goals](#6-non-goals)
- [7. Design Principles](#7-design-principles)
- [8. Proposed System Model](#8-proposed-system-model)
- [9. Execution Adapters](#9-execution-adapters)
- [10. Graders](#10-graders)
- [11. Suite Strategy](#11-suite-strategy)
- [12. Metrics, Comparisons, And Gates](#12-metrics-comparisons-and-gates)
- [13. Repository And Dependency Boundary](#13-repository-and-dependency-boundary)
- [14. External Frameworks And Benchmarks](#14-external-frameworks-and-benchmarks)
- [15. Decisions](#15-decisions)
- [16. Options And Trade-Offs](#16-options-and-trade-offs)
- [17. Delivery Sequence](#17-delivery-sequence)
- [18. Vertical Slice Implementation Plan](#18-vertical-slice-implementation-plan)
- [19. Risks And Mitigations](#19-risks-and-mitigations)
- [20. Evidence Required During Implementation](#20-evidence-required-during-implementation)
- [21. Documentation Lifecycle](#21-documentation-lifecycle)

## 1. Summary

BuildMax needs an evaluation system that measures its product promise, not a larger version of
its current coding smoke test.

The product is one shared Agent runtime exposed through a local workbench and a private Space
Platform. Its useful output is not merely an assistant response: it is an intentional, bounded,
explainable transformation of workspace or space state, with results a user can inspect and an
operator can govern. Evaluation must therefore cover capability, reliability, trust, and the
end-to-end product outcome across local and worker execution.

This design decides that BuildMax will:

1. retire the current `eval/` catalog, `internal/agenteval`, and the current benchmark runner
   without preserving their task or result formats;
2. create a new top-level `evaluation/` workspace in the main repository;
3. own a provider- and framework-neutral contract for tasks, evaluation subjects, trial bundles,
   grader results, and experiment comparisons;
4. evaluate built binaries and deployment artifacts through black-box execution adapters rather
   than making internal Go functions the benchmark interface;
5. use external frameworks where they are strong — for example Inspect for experiment control and
   statistics, Harbor for Terminal-Bench 2.1 and other container benchmarks, and a self-hosted
   observability platform as an optional viewer — without giving any one framework ownership of
   BuildMax's evaluation semantics;
6. keep public benchmarks as external coordinates while product-owned suites determine BuildMax
   release and qualification decisions; and
7. design first for maintainers and private-deployment operators, while leaving a deliberate path
   to pre-publication evaluation of Space Agents and Workflows.

The accepted product and governance decisions, and the technical choices still
delegated to evidence, are recorded in [section 15](#15-decisions).

## 2. Problem And Current Context

### 2.1 The current benchmark answers too small a question

The current harness runs a short prompt against a temporary workspace and executes one shell
grader. Its catalog contains small, synthetic Go tasks. This is useful as an early smoke test, but
it does not represent the current product:

- it measures mostly short-horizon local coding;
- it runs one trial and reports one binary pass/fail result;
- task instructions and grader logic share a fixture directory visible to the Agent;
- user settings, hooks, plugins, permissions, and sandbox configuration can change the subject
  being measured;
- malformed tasks and missing fixtures do not reliably invalidate the experiment;
- it does not distinguish Agent failure from grader or infrastructure failure;
- it does not evaluate Portal orchestration, worker execution, artifacts, managed inference,
  governance, or cross-surface parity; and
- although every run already creates a durable trace, the result format does not use that trace for
  grading or diagnosis.

This design treats the implementation and its formats as disposable. Some fixture ideas may be
recreated as low-value smoke cases, but compatibility with the current catalog is not a goal.

### 2.2 The product has outgrown model-only evaluation

BuildMax's behavior is produced by more than a model. A run depends on:

- the shared Agent loop and BuildMax revision;
- provider, model, reasoning configuration, context window, and output limits;
- system instructions, workspace instructions, memory, skills, and Agent definitions;
- built-in tools, MCP servers, plugins, and subagents;
- hooks, permissions, approvals, and sandbox resolution;
- the local, worker, or conversation execution surface;
- operating system, container image, resource limits, network availability, and model transport;
  and
- the state materialized before the run.

The evaluated subject is therefore an **Agent system configuration**, not a model name. A result
that cannot identify that configuration cannot support a model qualification or a regression
decision.

### 2.3 Product correctness has several kinds of evidence

A unit test can prove that a tool rejects an invalid path. It cannot prove that a real Agent
reliably chooses the correct tool. A model-graded rubric can assess whether a report is useful. It
should not decide whether an unauthorized write occurred. A Portal browser test can prove that a
result is displayed. It cannot prove that a worker produced the right business outcome over five
independent trials.

BuildMax should keep deterministic tests, behavioral evaluations, safety scenarios, public
benchmarks, and production feedback distinct. The evaluation system may aggregate their evidence,
but it must not turn them into one misleading score.

## 3. Product Question

The evaluation system should answer:

> Can this BuildMax configuration reliably and safely transform a user's intent into a useful,
> inspectable outcome on the surfaces and within the execution boundaries the product promises?

That question produces four evaluation domains:

| Domain | Question | Typical evidence |
|---|---|---|
| Capability | Can the Agent complete useful work? | Final state, tests, artifacts, rubric dimensions |
| Reliability | Does it complete the work consistently and recover from ordinary failure? | Repeated trials, pass^k, failure taxonomy, variance |
| Trust and control | Does it remain inside instructions, authorization, and execution boundaries? | Policy and trace assertions, adversarial scenarios, boundary records |
| Product outcome | Does the whole local or Space Platform flow deliver the intended result? | Conversation, TaskRun, artifact, state, trace, ledger, and UI evidence |

These domains have separate scorecards and gates. Capability must not compensate for a trust
violation, and a polished final response must not compensate for an incorrect final state.

## 4. Evaluation Consumers

### 4.1 BuildMax maintainers

Maintainers need to compare a candidate revision with a baseline after changing the Agent loop,
instructions, tools, adapters, model defaults, policies, or execution environment. The system must
identify per-case regressions, statistical uncertainty, failure class, cost, and a reproduction
path.

### 4.2 Private-deployment operators

An operator needs to qualify a model and deployment profile against representative private work:

- whether an approved model is capable enough for local and worker use;
- what reliability, latency, token use, and infrastructure error rate to expect;
- whether the worker and model transport enforce the intended boundary; and
- whether deployment-specific dependencies cause product failures.

The operator's dataset and traces must remain inside the deployment unless the operator explicitly
exports them.

### 4.3 Space Agent and Workflow authors

A later product surface may let a Space author run private scenarios before publishing an Agent or
Workflow. This design does not commit to that UI or data model. It does require that the task,
subject, trial, and grader contracts do not assume evaluation is maintainer-only.

## 5. Goals

- Define a stable BuildMax-owned evaluation contract independent of model provider and evaluation
  framework.
- Evaluate the artifacts users and operators run: built binaries, worker images, and deployed
  surfaces.
- Make local and worker differences explicit and measurable.
- Grade final state and observable outcomes before natural-language style.
- Use the durable run trace as structured process evidence without making exact trajectory matching
  the default.
- Support repeated trials, paired baseline comparisons, uncertainty, and separate infrastructure
  reliability reporting.
- Keep hidden graders and private holdouts isolated from the Agent.
- Preserve enough bounded evidence to reproduce and diagnose failures.
- Permit external benchmark and analysis integrations without a required SaaS dependency.
- Keep evaluation tooling dependencies outside the product runtime and Go Core.

## 6. Non-Goals

- Preserving the current task Markdown, inline shell grader, result JSONL, or benchmark CLI
  behavior.
- Building a public leaderboard or general-purpose evaluation SaaS.
- Treating one public benchmark as BuildMax's product score.
- Replacing deterministic unit, integration, architecture, or end-to-end tests with real-model
  trials.
- Sending private prompts, traces, workspaces, or grader output to BuildMax or a third party by
  default.
- Requiring a Server, Python, Node, Docker, or an evaluation platform for normal CLI and Desktop
  use.
- Productizing Space-facing evaluation before maintainer and operator workflows are credible.
- Claiming that an LLM judge is ground truth without calibration against human or deterministic
  labels.

## 7. Design Principles

### 7.1 Outcome first

For a state-changing Agent, the authoritative answer is normally the final environment state. A
reply saying that a report was uploaded is not evidence that the artifact exists. A reply saying a
booking was made is not evidence that the database changed. Deterministic state and contract
checks take precedence where they are possible.

### 7.2 Process evidence, not one approved path

The trace should answer whether the Agent used forbidden tools, crossed a boundary, omitted a
required verification, looped, compacted, delegated, or encountered a tool error. Exact tool-call
order should be required only when order is itself a product or safety contract. A creative path
that reaches a valid outcome inside the boundary should be allowed to pass.

### 7.3 Evaluate the system that ships

An in-process test helper is useful for development but is not the primary capability benchmark.
The authoritative execution adapters invoke built deliverables with a frozen subject manifest.

### 7.4 Separate capability failure from harness failure

Agent failure, invalid task, infrastructure failure, grader failure, timeout, and cancellation are
different results. Infrastructure failures do not silently count as model failures or disappear
from the report.

### 7.5 No global score hides a hard failure

Reports may offer suite summaries, but no weighted average turns a trust violation into a passing
release because coding quality increased elsewhere. Critical assertions remain hard gates.

### 7.6 Local-first and private by default

Trial bundles remain on the machine or deployment that produced them. Export is explicit,
redacted, bounded, and separable from execution. Optional hosted tools must not become required
for evaluation or normal product operation.

## 8. Proposed System Model

The proposed flow is:

```text
dataset + experiment
        |
        v
subject resolver --------> immutable subject manifest
        |
        v
execution adapter --------> isolated trial environment
        |
        v
canonical trial bundle <-- trace + final state + artifacts + usage + errors
        |
        v
graders ------------------> per-dimension scores, labels, explanations
        |
        v
experiment comparison ---> qualification report and gates
```

### 8.1 Task

A task defines:

- stable ID, version, suite, tags, and target evaluation domain;
- instruction or multi-turn scenario;
- visible initial state;
- execution surface and required capabilities;
- wall-time, turn, tool-call, token, and resource limits;
- required and optional graders;
- trial count defaults;
- environment and dependency versions; and
- references to hidden verification and an oracle.

Every committed task must pass preflight:

1. its initial state does not already satisfy the required outcome;
2. its oracle completes the task;
3. all required graders accept the oracle;
4. repeated oracle runs are deterministic enough for the stated interpretation; and
5. the Agent cannot access hidden grader or oracle material through the trial boundary.

### 8.2 Subject

The subject manifest freezes the evaluated configuration, including:

- BuildMax commit, build identity, and dirty-state digest;
- binary or container image digest;
- surface and execution adapter version;
- provider transport, model target or alias, reasoning and generation limits;
- effective system-prompt and tool-schema digests;
- prompt layers, skills, plugins, MCP, and Agent-definition provenance;
- permissions, hooks, and sandbox resolution;
- operating system, architecture, CPU, memory, and network profile; and
- dataset and contract versions.

Secrets and full private instruction bodies do not belong in the manifest. Their digests and safe
provenance identify the subject without making the result a credential or content store. When a
provider cannot reveal the exact model revision, the manifest records that uncertainty rather
than inventing precision.

### 8.3 Trial

A trial is one independent attempt by one subject at one task. Its terminal status is one of:

| Status | Meaning |
|---|---|
| passed | All required graders passed |
| failed | Execution completed but one or more required graders failed |
| agent_error | The Agent runtime failed before producing a gradable outcome |
| infrastructure_error | The environment or dependency failed independently of Agent capability |
| grader_error | Required grading could not complete |
| timed_out | The shared task wall-time budget expired |
| canceled | The experiment controller stopped the trial |
| invalid_task | Preflight or task integrity failed |

### 8.4 Canonical trial bundle

The trial bundle is the stable interchange boundary. It contains or safely references:

- task, dataset, experiment, subject, and trial identity;
- input and initial-state digests;
- terminal status and failure classification;
- final reply and structured result when permitted by retention policy;
- durable trace and child-trace references;
- final workspace or database-state evidence;
- artifact identities, hashes, and verification output;
- grader versions and per-dimension results;
- LLM calls, tool calls, tokens, duration, and resource observations; and
- a bounded reproduction description.

The contract is versioned and serializable without a BuildMax process. Runners and viewers may add
extensions, but qualification gates read only the BuildMax-owned fields.

## 9. Execution Adapters

The contract supports several adapters because no one environment represents the product:

| Adapter | Purpose |
|---|---|
| Agent Core/local | Fast behavioral work against the shared local runtime |
| CLI binary | Verify the shipped local command and workspace behavior |
| Desktop runtime | Verify local workbench parity below UI presentation |
| Worker/TaskRun | Verify materialization, non-interactive execution, artifacts, transport, and boundary |
| Conversation | Verify foreground replies, optional Agent-task delegation, structured result projection, and multi-turn chat outcomes |
| Deployment | Verify Compose or Kubernetes dependencies and operator-visible failure behavior |
| Harbor | Run Terminal-Bench 2.1 and other standardized container benchmarks |

The deterministic end-to-end suites remain distinct. They may share environment and process-launch
helpers with evaluation adapters, but a scripted model protocol test and a probabilistic real-model
trial retain different names, results, and release interpretations.

## 10. Graders

### 10.1 Deterministic outcome graders

Preferred whenever possible:

- tests and static analysis;
- file, artifact, schema, and content assertions;
- database or API state verification;
- authorization and ownership checks;
- deployment and dependency health evidence; and
- fail-to-pass plus pass-to-pass regression checks.

### 10.2 Trace and policy graders

These inspect structured events rather than prose:

- forbidden or missing tool use;
- argument and scope validity;
- approval, hook, and sandbox decisions;
- retry, loop, compaction, and premature-stop behavior;
- required verification before completion;
- subagent lineage and inherited policy; and
- boundary or redaction violations.

The current durable trace does not yet express every desired event. Missing hook execution,
file-change, per-command boundary, and retry evidence should remain explicit gaps rather than be
inferred from free text.

### 10.3 Model graders

Used for dimensions that cannot be reduced to state checks, such as usefulness, groundedness,
coverage, clarity, or whether a clarification was appropriate. Each dimension has an independent,
structured rubric and an `unknown` path. A model grader records its own subject and cost.

Before a model grader can become a hard gate, it must be evaluated on a frozen human-labeled set
for agreement, self-consistency, position or style bias, and adversarial robustness. Deterministic
failure cannot be overruled by a model grader.

### 10.4 Human grading

Human review calibrates model graders, adjudicates task or grader defects, and samples important
production-derived cases. It is not required on every routine experiment, and a post-hoc human
decision is recorded separately from the original automated result.

## 11. Suite Strategy

The first product-owned suites should be:

| Suite | Primary purpose |
|---|---|
| Agent Core regression | Tool loop, recovery, compaction, parallel calls, queued input, stopping |
| Local workbench | Coding, files, structured data, research, sessions, local artifacts |
| Worker and TaskRun | Materialization, managed/direct transport, artifacts, cancel, retry, timeout |
| Conversation and Workflow | Intent handling, clarification, delegation, result delivery, multi-turn state |
| Trust and control | Permissions, hooks, sandbox, injection, secrets, paths, network, space scope |
| Extensibility | MCP, skills, plugins, subagents, missing dependencies, provenance |
| Cross-surface parity | The same abstract task across local and worker environments |

Suites contain positive and negative cases. For example, testing whether the Agent searches when
required must be paired with cases where searching is unnecessary or forbidden. Capability suites
include difficult tasks with room to improve; regression suites contain behavior expected to pass
nearly all the time.

Public development suites are transparent and reproducible. A private or rotating holdout uses the
same contract but is retrieved by immutable dataset version and digest. Private data is not a Git
submodule and does not make public-suite execution depend on private access.

## 12. Metrics, Comparisons, And Gates

Each suite reports a vector rather than one project-wide score:

- pass@1 estimated from independent trials;
- pass@k where multiple attempts are a valid product behavior;
- pass^k where consistency is the requirement;
- confidence intervals and per-case variance;
- required-grader and critical-case failures;
- infrastructure, grader, timeout, and cancellation rates;
- LLM and tool calls, prompt and completion tokens;
- wall time and relevant latency percentiles;
- cost only when pricing input is explicit, versioned, and applicable; and
- trust, policy, and boundary violations as counts and named cases.

Candidate and baseline subjects run over the same task set and, where practical, in the same time
window and infrastructure profile. Comparison is paired by task and trial index. The report shows
absolute results, paired deltas, uncertainty, improved cases, regressed cases, and failures that
could not be scored.

The proposed cadence is:

| Cadence | Evaluation |
|---|---|
| Every pull request | Deterministic schema, oracle, grader, adapter, and mock-model checks |
| Risky Agent change, on demand | Small paired real-model regression experiment |
| Nightly | Pinned regression subjects with multiple trials |
| Weekly or qualification event | Broader capability, deployment, and model matrix |
| Release | Critical trust and reliability cases plus accepted paired-regression thresholds |
| Periodic external | Public benchmark subsets and reproducibility audit |

Real-model trials should not gate every pull request by default. Provider drift, credentials,
latency, and spend make them a poor substitute for deterministic verification. They become hard
gates only after the suite's own variance, task validity, infrastructure reliability, and expected
budget are measured.

## 13. Repository And Dependency Boundary

The new `evaluation/` workspace should remain in the main repository initially. Logical isolation
is required; a separate Git repository is not.

The intended ownership areas are:

| Area | Responsibility |
|---|---|
| contract | Task, subject, trial-bundle, grader-result, and experiment schemas |
| suites | Public product-owned task and scenario catalogs |
| adapters | CLI, worker, conversation, deployment, and external harness bridges |
| graders | Reusable deterministic, trace, policy, and semantic graders |
| environments | Local, container, Compose, and Kubernetes trial definitions |
| runner | Experiment execution, repetition, comparison, statistics, and reporting |

This is an ownership model, not yet a committed directory tree. The implementation
updates the repository-layout source of truth when the first slice establishes the actual shape.

Evaluation tooling may use Python, containers, or other developer dependencies with their own
lockfiles. Those dependencies do not enter the Go module, product binaries, or normal local and
deployment prerequisites. The cross-platform task runner remains the contributor entry point.

Large outputs live under the ignored artifact area rather than in source control. Private holdouts
may live in an access-controlled Git repository, object store, or CI artifact registry, but they
implement the main repository's versioned contract. A separate evaluation repository should be
considered only when independent ownership, release cadence, access control, scale, or reuse makes
cross-repository compatibility cheaper than co-development.

## 14. External Frameworks And Benchmarks

### 14.1 Framework roles

No external framework is the system of record. [Section 15.3](#153-experiment-controller-and-implementation-language)
makes a thin BuildMax controller in Go the default; the slice spikes Inspect against it, and
Inspect has to earn the switch rather than merely tie. Harbor is a separate environment and
benchmark adapter, not the alternative controller. The other integrations remain optional and
follow only when a demonstrated workflow needs them.

| Framework class | Candidate role | Boundary |
|---|---|---|
| Inspect AI | Challenger to the default controller: epochs, limits, scoring, logs, and statistics | Must emit and consume the BuildMax trial contract; adopting it also adopts Python, so it must clear section 15.3's overturn condition |
| Thin BuildMax controller | Default controller | Own only the minimum orchestration missing from the contract; do not grow an LLMOps platform |
| Harbor | First-choice container and public-benchmark adapter, beginning with Terminal-Bench 2.1 | An execution adapter, not the controller or product-result model |
| Phoenix or Langfuse | Later, optional self-hosted trace, experiment, and annotation UI | Viewer/export target; never required or authoritative; select at most one after local reports prove insufficient |
| Promptfoo | Optional adversarial prompt and red-space case generation | Generated cases return to BuildMax-owned tasks and graders; it is not in the main evaluation path |
| Provider eval APIs | Optional semantic graders or comparison services | No provider becomes required for the core workflow |

The spike tests the default rather than opening the question again. It must attempt at least a
local task, a worker artifact task, a Portal delegation task, a trust-boundary task, and a
repeated comparison, and it records the amount of adapter code, schema impedance, diagnostic
quality, concurrency behavior, and operational dependencies on both sides. Inspect is selected
only if it removes enough controller work to outweigh the Python toolchain it brings, and only if
it weakens neither the canonical contract, private-by-default execution, diagnostics, nor
portability. Harbor is evaluated separately because its strongest role is a container backend and
public-benchmark bridge.

### 14.2 Public benchmark roles

> **Update (2026-09-19):** the pinned target has advanced to Terminal-Bench 4.0
> (`terminal-bench/terminal-bench`, 66 tasks), and `pins.json` is the source of
> truth for the harness, dataset digest, and canary. The canary is now used as a
> small local regression subset — five cheap, Linux-only tasks run with a real
> model to catch a product regression — rather than a step toward a leaderboard
> score. The 2.1 rationale below is retained as the decision record for why
> Terminal-Bench was selected; the version-specific counts in it describe 2.1.

Terminal-Bench 2.1 is the first external capability benchmark and the accepted
Harbor integration target. Version 2.1 replaces 2.0 as BuildMax's baseline: its
own README states that 26 of the 89 tasks were modified — bugs, timeouts,
resources, and reward-hacking robustness — so a 2.0 score must never be compared
with a 2.1 score as if only the Agent changed. This paragraph said 28 when the
record was accepted; the upstream figure is 26.

It is selected because the live leaderboard names the Agent and model
separately, accepts custom Agents, reports repeated trials with uncertainty,
and includes Codex, Claude Code, and Harbor's reference Agent. That makes it a
better harness coordinate than a leaderboard that fixes one scaffold and
primarily compares models. Its deterministic container outcomes also fit the
BuildMax black-box and final-state contracts without requiring a model grader.

The integration has these boundaries:

- Pin the Harbor version, the `terminal-bench/terminal-bench-2-1` dataset
  release and digest, the BuildMax adapter version, and every container or
  provider dependency that can change a result.
- Expose the built BuildMax artifact through Harbor's custom-Agent boundary.
  Harbor owns task materialization and official verification; BuildMax does not
  maintain a second runner or copy Terminal-Bench tasks into product suites.
- Validate the adapter, evidence conversion, and oracle boundary on a named
  canary subset before spending on the complete benchmark. A full comparable
  run uses all 89 tasks, five attempts per task, and the leaderboard resource
  and timeout policy without modification.
- Convert every attempt into the BuildMax trial contract and retain the Harbor
  task identity, outcome, logs, resource limits, usage, and harness failure
  separately from Agent failure. The external verifier remains authoritative
  for the benchmark outcome.
- Run canaries, full qualification, and baseline comparisons only as explicit,
  scheduled, or release-time jobs. They are never ordinary pull-request gates.

Two operational facts, both found while building the adapter and both narrower
than they were assumed to be. A Harbor Hub account is not needed to run the
benchmark: Harbor's client is anonymous when logged out and public reads keep
working, so the pinned public dataset downloads and trials run without one.
Sign-in gates publishing, `--upload`, org and key management, and private
datasets. And community submissions to the 2.1 leaderboard are closed at the
time of writing — only maintainer-run submissions are added — so results here
can be compared against published rows but cannot yet become one. Neither
changes what is measured; both change what a run can be used for.

The primary harness comparison holds the model, reasoning effort, provider,
task version, resources, and attempt count constant while changing only the
Agent adapter. BuildMax should first compare against Harbor's reference Agent
and, where the same model is accessible, Codex and Claude Code. Native
Codex/model and Claude Code/model pairings remain useful product coordinates,
but a difference between them is not evidence of a harness advantage because
the model changed too.

Each experiment snapshots the relevant live-leaderboard rows rather than
copying mutable scores into this design record. Reports identify the complete
subject tuple — Agent and version, model and version, reasoning effort,
provider, Harbor and dataset versions, environment and resource profile — and
show pass rate with uncertainty, per-task outcomes, cost, tokens, latency,
timeouts, and harness faults. A headline percentage without that tuple is not a
BuildMax qualification result.

- SWE-bench-style tasks and
  [SWE-rebench](https://swe-rebench.com/) provide later repository-editing
  coordinates but do not define the product or replace the first Harbor target.
- [BoundaryBench](https://github.com/boundary-bench/boundary-bench) informs a
  later sandbox-policy coordinate. It reuses Terminal-Bench tasks under
  hardened policies, so it complements rather than replaces the unmodified 2.1
  baseline.
- [METR's scaffold comparison](https://metr.substack.com/p/2026-02-13-measuring-time-horizon-using-claude-code-and-codex)
  found no general statistically significant advantage for Codex or Claude Code
  over its default scaffolds on the tested models and task distribution. That
  is evidence for same-model paired comparisons, not for assuming one harness
  has a universal multiplier.
- tau-bench-style scenarios inform multi-turn user, policy, tool, and final-state evaluation.
- BFCL provides a model/tool-call coordinate rather than an end-to-end BuildMax score.
- GUI-computer benchmarks remain deferred while computer use is not a core product surface.

Public benchmark versions, harness versions, patches, exclusions, resources, attempts, and all
other deviations are reported. A leaderboard number without that context is not a qualification
result. Terminal-Bench can establish relative terminal and container task
capability; it cannot establish worker governance, Portal delivery,
cross-surface parity, private-deployment operability, or the other BuildMax
product outcomes.

## 15. Decisions

### 15.1 Evaluation boundary

| Decision | Accepted direction |
|---|---|
| Legacy compatibility | Retire the current catalog and harness; do not preserve their formats |
| Location | Use a new top-level `evaluation/` workspace in the main repository |
| Repository | Do not create a separate repository initially |
| Evaluation interface | Black-box built artifacts behind a BuildMax-owned contract |
| Framework posture | External frameworks are replaceable adapters or viewers |
| Product metric | Product-owned suites drive release decisions; public benchmarks are external coordinates |
| Aggregation | Separate capability, reliability, trust, and product-outcome scorecards; no global score |

### 15.2 Product and governance defaults

| Decision | Accepted direction |
|---|---|
| Audience | Maintainer and operator qualification are both initial scope; Space authoring is later |
| Priority | The black-box vertical slice is near-term enabling work before substantial new Agent capability |
| Private holdout | Qualification may use access-controlled or rotating data with a public schema, immutable version and digest, and a fully runnable public development suite |
| Real-model gate | Scheduled and release qualification may consume provider credentials and a bounded budget; ordinary pull requests remain deterministic by default |
| Trial privacy | Raw prompts, replies, traces, workspace snapshots, and grader bodies remain local/private by default and require explicit bounded export |
| Future Space scope | Contracts support future pre-publication Agent and Workflow evaluation without committing a Space-facing UI or roadmap phase |

### 15.3 Experiment controller and implementation language

The controller and its language are one decision, not two. Choosing Inspect chooses Python with
it, and the remaining combination — Python plus a controller BuildMax writes itself — is the
worst of both: a second contributor toolchain bought without the epochs, limits, scoring, and log
viewer that would have paid for it. That combination is removed from consideration.

| Decision | Accepted direction |
|---|---|
| Controller | A thin BuildMax controller, in Go, using only the standard library, inside the main module |
| Language | Go by default; Python enters only together with Inspect, never on its own |
| Overturn condition | The Inspect spike showing that the controller work it removes outweighs a second toolchain and the operator's install cost |

Three things decide it, in order of weight:

- **Operators run it themselves.** Section 4.2 requires a private-deployment operator to
  qualify a model, configuration, and deployment inside their own environment. The product is a
  single binary that needs no Node, and private deployment is meant to be boring. Requiring a
  Python install, a virtualenv, and a lockfile before an operator can qualify anything
  contradicts that directly.
- **No new dependency, and existing gates apply for free.** A standard-library controller adds
  nothing to `go.mod`, which is what section 13 asks for. Because it lives in the main module,
  `./make test`, `vet`, `lint`, and `govulncheck` already reach it through `./...`; the
  evaluation system's own correctness needs no second CI pipeline, lockfile, or pinning scheme.
- **Trace drift fails at compile time.** A Go controller parses traces through the record types
  in `internal/infra/trace`, so a change to the trace format breaks the build or the tests. A
  Python controller transcribes that schema by hand, and drift surfaces only at runtime — the
  in-house version of the framework-evolution risk in section 19.

Statistics is the strongest argument for Python and it is weaker than it appears. Section 12
needs binomial confidence intervals, pass^k, paired comparison, and per-case variance: a few
hundred lines of pure functions with fixed inputs and outputs, which is the most testable code
there is. Heavier analysis — power analysis, stratified sampling, inter-rater models — belongs to
phase 2 and later, and arrives as an offline Python script reading committed JSON bundles. The
contract is language-neutral, so that route stays open without deciding anything now.

Harbor and Terminal-Bench 2.1 are not an argument for Python either. Section 14.2 runs Harbor as a
separate process calling a BuildMax Agent adapter — an external tool invoking a BuildMax binary,
not a BuildMax controller importing a Harbor library — so the controller's language does not
reach it.

The cost is real and is what the overturn condition measures: experiment control (repetition,
concurrency, limits, cancellation, resumption) and report rendering must be written, and Inspect
supplies them along with a log viewer.

### 15.4 Technical decisions still delegated to evidence

The vertical slice must resolve these choices with evidence rather than
preference:

- Harbor's exact custom-Agent adapter mechanism for Terminal-Bench 2.1;
- the report renderer, given that the statistics above are implemented rather than imported; and
- Phoenix, Langfuse, or no external viewer.

The physical trial-bundle encoding is no longer among them. Step 1 settled it on a directory
rather than a single document: most of a bundle is already files — the JSONL trace, workspace
state, produced artifacts — so inlining them would contradict the bounded-evidence rule they
exist to serve, while one directory per attempt is the reproduction path section 17 requires.
`evaluation/contract` implements that layout.

Each follows from the black-box vertical slice and must preserve the contract and privacy decisions
above.

## 16. Options And Trade-Offs

| Option | Strength | Main concern |
|---|---|---|
| Expand the current Go benchmark | Smallest immediate change | Preserves an early coding-task abstraction that cannot represent the current product cleanly |
| Adopt one external framework as the system of record | Mature generic runner, scoring, and UI sooner | BuildMax product semantics, portability, and privacy become subordinate to another framework's model |
| Build a complete custom evaluation platform now | Maximum control | Front-loads a large LLMOps product before representative tasks and operator needs are proven |
| Own the contract and product suites; use replaceable runners, backends, and viewers | Keeps product meaning and privacy under BuildMax control while reusing mature infrastructure | Requires disciplined contract design and adapter conformance |

The accepted direction is the final option.

## 17. Delivery Sequence

### Phase 0: Evaluation charter and contract

- Define versioned task, subject, trial-bundle, grader-result, and experiment contracts.
- Define retention, redaction, and export boundaries.
- Define failure taxonomy and qualification-report semantics.
- Audit the durable trace against required process evidence.

### Phase 1: Black-box vertical slice

- Build a small representative suite spanning local, worker, conversation, and trust behavior.
- Run built binaries or images in isolated environments.
- Preserve hidden graders and an executable oracle.
- Produce one canonical trial bundle per attempt.
- Compare baseline and candidate over repeated trials.
- Spike Inspect versus a thin controller and add a Harbor adapter targeting
  Terminal-Bench 2.1.

The slice is successful when a failure hands a contributor a classification, trace, final-state
evidence, subject manifest, and bounded reproduction path — not merely a lower aggregate score.

Phases 0 and 1 are broken down into ordered increments, with the trace audit and adapter sketch
they depend on, in [section 18](#18-vertical-slice-implementation-plan).

### Phase 2: Maintainer and operator qualification

- Expand regression, capability, worker, trust, and cross-surface suites.
- Establish measured cadence, repetition, provider budget, and gates.
- Add model/config and deployment qualification reports.
- Add a versioned private or rotating holdout.
- Add optional export to a self-hosted viewer after the local workflow is sufficient.

### Phase 3: Production feedback loop

- Sample eligible failures and user feedback inside the owning deployment.
- Require human redaction and review before promotion into a dataset.
- Track task and grader defects separately from Agent defects.
- Re-run promoted cases against candidate subjects before release.

### Phase 4: Space-facing evaluation, if separately accepted

- Let a Space define private scenarios for an Agent or Workflow.
- Run those scenarios with space authorization and quota.
- Compare definition versions before publication.
- Keep evaluation results, traces, and datasets space-scoped.

This phase requires a separate product and data-model decision. The earlier phases only preserve a
contract path to it.

## 18. Vertical Slice Implementation Plan

Section 17 gives the phase direction. This section is the executable breakdown of phase 0 and
phase 1, written before implementation starts, and it carries the evidence that
[section 20](#20-evidence-required-during-implementation) requires as items 2 and 3: the trace
field inventory and the black-box adapter sketch.

Nothing here changes the accepted decisions in section 15. It implements the controller decision
in section 15.3 and resolves choices section 15.4 delegated to the slice.

### 18.1 What the current harness leaves behind

| Asset | Disposition |
|---|---|
| `eval/001-read-summarize` … `eval/013-worker-pool` | Deleted. A few may be recreated later as low-value smoke cases under the new contract |
| `internal/agenteval` | Deleted, formats included |
| `tools/eval` | Rewritten as the entry point for the new contract and runner |
| `./make eval` | Rewritten to dispatch the new runner; defaults to CLI tasks, with worker and all-surface runs selected explicitly |

Four defects justify replacement rather than extension, and they are more specific than
section 2.1 states:

- `internal/agenteval/catalog.go` uses the task directory itself as the fixture, so the runner
  copies `task.md` and the grader script it contains into the workspace. The grader is not
  merely co-located with the task; it is readable by the Agent under evaluation.
- `internal/agenteval/runner.go` treats a missing fixture directory as a warning and runs the
  task against an empty workspace. An invalid task is then recorded as an Agent failure, which
  is exactly the conflation section 7.4 forbids.
- `internal/agenteval/runner.go` constructs the runtime in process through
  `agentapp.NewAgentApp`. Nothing it measures can distinguish CLI, worker, or deployment
  behavior, and no built artifact is exercised.
- `internal/agenteval/result.go` records a boolean plus token counts. There is no subject
  identity, no trace reference, no repetition, and no failure class beyond agent versus grader
  error.

### 18.2 Trace evidence audit

This is the inventory section 20 requires before grading design is fixed. It was taken against
`internal/infra/trace/record.go` and `internal/infra/trace/summary.go`.

| Evidence a grader needs | Present in the trace today | Gap |
|---|---|---|
| Run identity, model, workspace, subagent lineage | `run_start`, with `trace_version` and parent run/tool-call links | None for the slice |
| Instruction provenance | `context_sources`, written even when empty, alongside the memory and compaction sources of the same run | None |
| Extension provenance | `plugins`, with repository-plugin mutability | MCP servers and skills are not separately named |
| Sandbox boundary | `sandbox_boundary`, once per run, `sandboxed` explicitly false rather than absent | No per-command decision |
| Tool use and argument scope | `tool_start`, `tool_end` with duration | Args and results are bounded at 4096 bytes, so a large-payload assertion cannot rely on them |
| Blocked call | `tool_denied` with `deny_reason` | `summary.go` collapses hook denial and policy denial into one `Denied`; a trace grader that must distinguish them has to parse the reason string |
| Context pressure and looping | `iter_start`, `llm_start`, `context_compacted` | None |
| Token, cache, and cost accounting | `llm_end` per call and cumulative, `run_end` totals, `cost_incomplete` | None |
| Mid-run instruction changes | `user_input`, `user_input_blocked` | None |
| Hook execution | Absent | Required before the trust and control suite is credible |
| File changes | Absent; inferable only from bounded tool arguments | Required before outcome grading can attribute a change |
| LLM retry and provider error | Absent | Required before reliability separates provider noise from Agent behavior |

The slice proceeds on what exists. Capability, reliability, and the permission half of trust
are gradable today. Hook execution and file-change events are prerequisites for the trust and
control suite, not for the slice, and they belong to
[trust harness](trust-harness.md) and [durable run trace](durable-run-trace.md) follow-ups
rather than being added here in passing.

### 18.3 Black-box adapter sketch

The CLI adapter is the slice's authoritative execution path:

```text
BUILDMAX_HOME=<trial home>   buildmax -p <instruction>
                             --workspace <trial workspace>
                             --model <subject model>
                             --output json
```

The trial home is built by the subject resolver, not inherited from the contributor. This is
what removes the section 2.1 problem where local settings, hooks, plugins, and permissions
silently change the subject being measured.

`internal/interface/cli/print_format.go` already emits a stable envelope carrying the reply,
tool-call count, duration, usage, cache and cost breakdown, context occupancy, exit code, and
`policy_denied`. The exit codes in `internal/interface/cli/exit_code.go` are a documented
contract, and they map onto the section 8.3 taxonomy directly:

| CLI exit | Trial status |
|---|---|
| `ExitOK` | Graders decide `passed` or `failed` |
| `ExitUsage` | `invalid_task` when the task supplied the bad input, `infrastructure_error` when the trial home did |
| `ExitPolicyDenied` | Graders decide; a trust task may require denial, so this is not a failure by itself |
| `ExitModelError` | `agent_error` |
| `ExitUserCancelled` | `canceled` |
| `ExitIterationCap` | `timed_out`: a stated budget expired, which is a judgement about the subject rather than a broken harness. Added later; see [section 18.7](#187-what-building-the-harbor-adapter-changed) |
| Wall-time budget expired | `timed_out` |
| Required grader could not run | `grader_error` |

One product change is required for the adapter to work at all. `agentapp.RunResult` already
carries `TraceID` and `TracePath`, but the CLI envelope does not expose either, so an external
caller cannot identify which trace file a run wrote — a session with repeated runs holds
several. The slice adds `trace_id` and `trace_path` to the print-mode envelope. It is
user-visible beyond evaluation, since it is also how a person finds the trace for a run they
just made, so it carries a changelog entry and a `manual/cli.md` update.

The worker adapter followed the slice and is built. What was described as missing — a way to
submit a trial and collect its bundle without a Portal user — turned out not to need one. A
worker reaches its server over HTTP and nothing else: it fetches the run, reports status,
streams output, and polls for cancellation. So the adapter serves that API itself, on a
loopback port, for exactly one run. No database, no space, no scheduler, and no Portal user is
involved, which is the same move `mockllm` makes for the model side.

The dispatch is a scheduler's: a run id on the command line, a run token in the environment,
and a `server.yaml` naming the control plane. What it exercises that the CLI cannot is the part
of the product only a worker has — materializing the space's persistent workspace into a
run-scoped directory, executing with no interactive surface, and reporting an outcome over the
API rather than to a terminal. The outcome is read from what the worker reported, not from its
exit code: a worker that failed the run reports FAILED and exits non-zero, while one that was
killed reports nothing at all, and only the control plane separates those.

The conversation and deployment adapters remain unbuilt.

### 18.4 Hidden grader boundary

The task directory holds the instruction, the initial state, the grader, and the oracle. Only
the declared initial state is materialized into the trial workspace. Nothing else in the task
directory is reachable from inside it. This is the concrete fix for 18.1's first defect, and it
is a property the slice tests directly: a task whose grader material appears in the workspace
fails preflight rather than producing a passing trial.

### 18.5 Deterministic checks without a provider key

`internal/testsupport/mockllm` already answers a model from a committed scenario and backs the
CLI end-to-end suite. The slice reuses it so the pull-request row of the section 12 cadence
table is real: contract round-trips, task schema validation, grader units, adapter process
launch and envelope parsing, and the exit-code-to-status mapping all run with no provider
credential. This keeps the repository rule that a full check requires no model API key. Real
models are reached only by on-demand, nightly, and release experiments.

Reusing it needs one architecture change made deliberately rather than by omission.
`internal/architecture/architecture_test.go` keeps `internal/testsupport` out of shipped code by
scanning the `internal`, `cmd`, and `deployment` trees; a new top-level `evaluation` tree escapes
that rule by not being listed, which is the wrong reason to be allowed. The slice adds
`evaluation` to the scanned trees.

It adds no exemption beside `deployment/smoke`, which an earlier draft of this section expected.
The rule already skips `_test.go`, so evaluation's end-to-end tests import the mock model without
one, and exempting the tree would additionally permit what must stay forbidden: a runner or
adapter answering its own model would report on the mock rather than on the subject.

### 18.6 Delivery increments

| Step | Delivers | Resolves |
|---|---|---|
| 1. Contract — **done** | `evaluation/contract`: versioned task, subject manifest, trial bundle, grader result, and experiment types with the failure taxonomy, in Go against the standard library alone per section 15.3; the trace audit above recorded in the repository | The physical trial-bundle encoding, settled on a directory in section 15.4; section 20 items 2 and 3 |
| 2. CLI adapter — **done** | `trace_id`/`trace_path` in the print envelope; subject-built trial home; the hidden-grader boundary; deterministic state and trace graders; one canonical trial bundle per attempt | The contract holds for a real execution path: `evaluation/adapter` runs the built binary against a scripted model and returns a gradable bundle |
| 3. Experiment — **done** | Repetition, paired baseline comparison, uncertainty, failure classification, preflight, and a local report; the mockllm pull-request gate | Section 15.3's Go controller holds: repetition, limits, cancellation, and the statistics came to roughly 700 lines with no new dependency. The report renderer is written rather than imported |
| 4. Retirement — **done** | `eval/` and `internal/agenteval` deleted; `tools/eval` rewritten around the contract rather than removed, since the entry point is still where a run starts; `./make eval` defaults to the CLI, while worker tasks are opt-in | The last roadmap acceptance criterion |
| 5. Harbor adapter — **done** | `evaluation/harbor`: pinned harness, dataset ref, and adapter version; the Python custom-Agent that uploads the built CLI into a task container and runs one prompt; the importer that files a finished job as trial bundles; `./make doctor harbor` and `./make eval harbor` | The contract holds for an external harness whose verifier BuildMax does not own. The oracle smoke passed 5/5 and a one-task canary ran end to end and imported, so the adapter drives the real harness for one task. It says nothing about the other 88, about repeated attempts, or about a score |
| 6. Canary run and comparison — **open** | The canary subset is pinned in `pins.json` and `--canary` selects it; running it is open, then the full 89 tasks at five attempts under the leaderboard's unmodified resource and timeout policy, and a baseline comparison against Harbor's reference agent | How BuildMax compares. One passing task is not a measurement |

Five things the slice found are worth carrying forward. Killing a subject at its budget does not
end the call: the process dies but its output pipes stay open through any grandchild it started,
so an unbounded wait turns the one status designed to bound a run into the thing that never
returns. And the durable trace does not distinguish a hook denial from a policy denial, so a
trust grader asserting on one has to read the reason string; section 18.2 lists that gap, and the
first real trust suite is where it stops being tolerable.

A grader command and an oracle both resolve against the task directory and both run with the
workspace as their working directory, so a relative suite path resolved against the workspace —
the one place a grader must never be satisfied from. Any future adapter that runs task-supplied
material inherits this.

And section 8.1's rule that a task's initial state must not already satisfy its required outcome
does not hold for a negative task, whose outcome is that nothing happened. Tasks declare
`negative` rather than being exempted case by case, and a negative task must carry a required
trace or model grader: without one it asserts only that nothing happened, and a subject that
never ran would satisfy it. This is a contract addition the slice earned, not one it assumed.

And the two surfaces do not put a task's initial state in the same place. A CLI run is given
the workspace directly; a worker materializes the space's files into a `home` subdirectory of
the run directory it works in. A path assertion is therefore surface-specific, which section
11's "parity is two tasks stating the same goal, not one task run twice" already implies but
which is easy to violate by copying a task between surfaces. Preflight materializes into the
layout the task's own surface uses, so a worker task whose assertions were written for the CLI
fails before it costs anything.

The subject follows from this too: a build reached through two adapters is two subjects, so the
runner stamps the execution path onto the subject per trial. Sharing one identity would let a
CLI result and a worker result pair as the same configuration measured twice.

The Inspect and Harbor spikes follow step 3. Section 14.1 already places framework selection
downstream of the slice, and a spike run before a canonical bundle exists would compare
adapters against a contract that is still moving.

### 18.7 What building and first running the Harbor adapter changed

Six things came out of building it, four of which are contract or product
decisions rather than adapter details. Running it added three more, and those
are the ones worth reading: none was reachable by testing, and two of them
would have made a published number wrong rather than absent.

**The iteration cap needed its own exit code.** The agent loop's cap was fixed
at 200 and reaching it exited `ExitModelError`, the same code as an unreachable
provider. Those ask opposite things of a harness: a provider failure is a fault
worth retrying, and an exhausted budget is an answer — retrying it pays for the
same cap again and, worse, discards the verifier's verdict on work that really
happened. The cap is now configurable through `agent.max_iterations` and
`--max-iterations`, and reaching it exits `7` with error kind `iteration_cap`.
The Harbor adapter swallows exactly that one code so the task's own verifier
still judges the workspace, and records the cap as the trial's failure class.
This is what keeps a spent budget out of the capability reading that
section 7.4 exists to protect.

**`Limits.Iterations` was decoration.** The contract had carried the field since
step 1, and no adapter passed it to anything: a task stating an iteration limit
ran to the binary's own default and the trial reported a bound nobody applied.
There was no flag to pass it to until the change above.

**Attempt indices are assigned, not read.** Harbor does not number attempts — a
trial name is the task plus a random suffix — so the importer numbers them per
task from a stable ordering by start time. The index is therefore a property of
the import rather than an external identity, and two imports of one job must
agree or a paired comparison would pair different attempts each time it ran.

**The artifact digest belongs to the code holding the file.** Section 7.3 makes
the digest what turns a result into a black-box measurement, and Harbor records
only the kwarg naming a binary path. An importer taking the digest from its
caller would let the caller name a file that never ran, with nothing downstream
able to check it. The adapter hashes the binary it uploads and records that;
the version and commit come from what the binary reported inside the container.
A trial missing the digest is refused rather than imported without one.

**Facts that cross a language boundary need a guard, because the failure is
silent.** The trial home's settings file and the subject tuple are both produced
in Python and consumed in Go. A key spelled differently on one side is not an
error — the loader or the map lookup simply misses it — so the run proceeds with
the field unset and the result is attributed to a subject that was never
configured that way. Three tests hold the pairs to each other: the settings keys
against `evaluation/adapter`'s own struct, the metadata keys against the
importer's, and the Python adapter version against `pins.json`.

**A Bash command that leaves a background process behind hung the agent
forever.** This is a product bug and nothing to do with evaluation, and only
running the benchmark found it. The Bash tool reads output through a pipe, and
Go's `Wait` returns only when every write end closes — so a server the agent
started earlier in a task held the pipe open, and a command killed at its own
deadline never returned. A canary trial sat on one tool call for two hours under
a documented 120-second timeout. The tool now bounds that wait.

The repository already knew this failure: `evaluation/adapter/cli.go` documents
it exactly, for the process the adapter launches. What it did not do was apply
the same reasoning one level down, inside the tool that launches commands of its
own. A hazard recorded in one place is not a hazard fixed everywhere, and this
is worth remembering the next time one is written down.

**An attempt that produced no usable evidence must not take the job with it.**
A subject killed mid-run leaves an empty result envelope — the shell creates the
file before the binary writes a byte — and treating that as a broken output
contract lost five good imports to one killed container. An import degrades the
trial and records the gap; it does not refuse the job.

Four smaller findings came from comparing real output against what the source
suggested: the trial result file is `result.json` and not the `results.json`
Harbor's own docstring names twice; a task is `<org>/<name>`, which a bundle's
task id cannot hold and which the task filter requires; and `task_checksum` is
bare hex where the same tree's ref is labelled. Each was an assumption read from
code rather than seen, and each would have failed on the first real import.

**Cancelling a coroutine is not stopping a container.** Harbor bounds the
agent phase with `asyncio.wait_for`, which is the right shape and the right
per-task budget — and it did nothing. Cancelling a coroutine that is awaiting a
`docker compose exec` ends the wait; the environment terminates the client only
on its own per-exec timeout, which the adapter never set. So the container kept
running the CLI after Harbor had given up on it, and a task declaring a
thirty-minute budget was observed still working, and still spending, at
ninety-eight minutes.

A run that outlives its budget is not slightly less precise; it is not
comparable at all, because every agent on the leaderboard was held to that
budget. The adapter now reaps the in-container process in the cleanup that
cancellation already runs, which is what makes the harness's own timeout mean
something. The budget itself stays Harbor's: a custom agent cannot read it —
Harbor passes `agent_timeout_sec` to the oracle agent alone — so an adapter that
tried to enforce its own would be restating a per-task number it has no way to
know.

**Cost cannot round-trip through the harness.** Harbor re-encodes what an agent
reports and holds cost as a float in dollars, while the runtime holds integer
nano-units. The importer therefore prefers BuildMax's own print-mode envelope,
which the adapter leaves in the trial's agent directory, and falls back to
Harbor's numbers only when no envelope exists — marking the recovered cost
incomplete, because a total rounded through a float should not read as exact.

### 18.8 Deliberately outside the slice

The original shipped slice deliberately excluded conversation and deployment
adapters, model-grader calibration, the private or rotating holdout, any
external viewer, public benchmarks, and the hook-execution and file-change
trace events. That exclusion scoped the first slice; it was not a rejection of
follow-on work. Section 14.2 now accepts Terminal-Bench 2.1 as the first
external target while the other items retain their existing homes.

## 19. Risks And Mitigations

| Risk | Mitigation |
|---|---|
| The evaluation system becomes a second product too early | Deliver the black-box vertical slice before UI, dataset management, or generalized services |
| Tasks test implementation trivia rather than product capability | State the construct each suite measures; require oracle and task review |
| Agents read or exploit graders | Separate visible and hidden material with a real execution boundary and adversarially test graders |
| LLM judges reward style or can be manipulated | Calibrate against frozen labels, separate dimensions, allow unknown, and keep deterministic gates authoritative |
| Provider or infrastructure noise is reported as a regression | Paired runs, repeated trials, explicit subject/resource manifests, and separate infrastructure status |
| Public tasks become contaminated or overfit | Private/rotating holdout, production-derived cases, and periodic task refresh |
| Trial bundles become a sensitive-data lake | Local/private default, bounding, redaction, retention, content-free manifests, explicit export |
| External framework evolution breaks evaluation | BuildMax-owned versioned contract and adapter conformance tests |
| Main-repository tooling burdens normal contributors | Independent locks and opt-in heavy commands; deterministic repository checks remain lightweight |
| Scores encourage optimizing the benchmark instead of the product | Multiple product domains, qualitative failure review, public-benchmark separation, and no global score |

## 20. Evidence Required During Implementation

The product direction is accepted before a framework is selected. Implementation
must not proceed past the first slice without evidence from:

1. a representative set of real BuildMax failure or manual-check scenarios;
2. an inventory of trace fields available and missing for outcome and trust grading —
   supplied by [section 18.2](#182-trace-evidence-audit);
3. black-box adapters for local and worker execution, plus a sketch for
   conversation execution — the built adapters and the remaining gap are
   recorded by [section 18.3](#183-black-box-adapter-sketch);
4. a privacy review of trial contents, retention, and export;
5. an initial provider-cost and repetition estimate;
6. an Inspect/controller spike and a Harbor custom-Agent adapter spike against
   Terminal-Bench 2.1; and
7. oracle and grader review showing that the initial tasks measure the claimed capability.

External practice supports the direction but does not decide BuildMax's product contract:

- [Anthropic's Agent evaluation guidance](https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents)
  distinguishes tasks, trials, graders, transcripts, outcomes, and Agent harnesses; recommends
  repeated trials and capability/regression separation; and treats final environment state as
  distinct from what the Agent claims.
- [Google ADK evaluation](https://github.com/google/adk-docs/blob/main/docs/evaluate/index.md)
  evaluates both final response and tool trajectory, including multi-turn criteria and user
  simulation.
- [Inspect AI](https://inspect.aisi.org.uk/tasks.html) models tasks from datasets, solvers, scorers,
  sandboxes, limits, and epochs, while its [metrics](https://inspect.aisi.org.uk/metrics.html)
  include uncertainty and inter-rater agreement.
- [Harbor](https://github.com/harbor-framework/harbor) evaluates arbitrary Agents in container
  environments and is the official harness for
  [Terminal-Bench 2.1](https://www.tbench.ai/leaderboard/terminal-bench/2.1).
  The [2.1 release](https://www.tbench.ai/news/terminal-bench-2-1) documents the
  corrections that make 2.0 results a different dataset rather than a baseline.
- The [Agentic Benchmark Checklist](https://github.com/uiuc-kang-lab/agentic-benchmarks/blob/main/ABC.md)
  emphasizes task and outcome validity, hidden ground truth, oracle solvers, contamination controls,
  and uncertainty reporting.

## 21. Documentation Lifecycle

This record owns the rationale and phased direction until the evaluation system
is implemented. The first implementation must update the repository layout and
testing documentation with the actual ownership boundaries, commands, data
formats, and operator workflow. Once the plan is complete, move enduring
contracts into contributor or reference documentation and delete this active
plan; Git history retains the decision context.
