# Agent Sandbox Research Memo

> **Audience:** runtime, platform, and security contributors · **Status:** research snapshot; recommendations are not implementation decisions
> **Reviewed:** 2026-10-02 · **Scope:** coding agents, code interpreters, plugins, and multi-tenant workers
> **简体中文：** [详细中文镜像](../zh-CN/reference/agent-sandbox-research.md)

Related: [current state](../current-state.md), [sandbox boundaries](../design/sandbox-boundaries.md), [Agent sandbox policy](../design/agent-sandbox-policy.md), and [roadmap](../ROADMAP.md). This memo does not change accepted decisions. English is authoritative; the Chinese memo is a derived, more expansive presentation of the same findings.

## Contents

- [1. Outcome and method](#1-outcome-and-method)
- [2. Findings](#2-findings)
- [3. Threat model](#3-threat-model)
- [4. OS mechanisms](#4-os-mechanisms)
- [5. Containers and Kubernetes](#5-containers-and-kubernetes)
- [6. gVisor](#6-gvisor)
- [7. MicroVMs and VMs](#7-microvms-and-vms)
- [8. WebAssembly](#8-webassembly)
- [9. Managed products](#9-managed-products)
- [10. Agent product evidence](#10-agent-product-evidence)
- [11. Network and credentials](#11-network-and-credentials)
- [12. Files, resources, and lifecycle](#12-files-resources-and-lifecycle)
- [13. Selection matrix](#13-selection-matrix)
- [14. Measurement and qualification](#14-measurement-and-qualification)
- [15. BuildMax assessment](#15-buildmax-assessment)
- [16. Research register and open questions](#16-research-register-and-open-questions)

## 1. Outcome and method

The user outcome is autonomous checkout, dependency installation, compilation, testing, and service execution with a bounded impact when code or instructions are malicious. Host credentials, other tenants, platform control services, and production authority should not become ambient privileges of an execution environment.

Agents read untrusted repositories, websites, and tool outputs before running programs. Dependency installers and tests are executable code too. Approving a shell string cannot establish the safety of everything it launches. Anthropic describes practical containment challenges involving prompt injection and agents crossing intended boundaries. [Engineering experience](https://www.anthropic.com/engineering/how-we-contain-claude).

This research uses official documentation, project repositories, and local implementation inspection. **Provider claims**, **local static observations**, and **engineering recommendations** have different evidential strength. No independent security audit, escape testing, benchmark, paid-provider trial, or deployment qualification was performed. Access date is the review date; an access date is not a publication date.

Coverage is representative, not exhaustive. Prices, quotas, regions, versions, and maintenance status must be rechecked before procurement or implementation. This is a reference memo rather than an approved design or new roadmap item.

## 2. Findings

1. A sandbox is a system combining execution isolation, file authorization, enforced egress, credential handling, quotas, trusted API authorization, and cleanup.
2. Ordinary containers share a host kernel. Rootless execution, seccomp, and LSMs help but do not establish a separate kernel boundary.
3. gVisor and VM runtimes are practical candidates for stronger general Linux agent isolation. Compatibility and operational evidence decide between them.
4. Wasm fits controlled plugins better than arbitrary native development environments.
5. Destination allowlists do not authorize business operations: reaching GitHub does not grant permission to push to every repository.
6. BuildMax should qualify its current boundaries before adopting another runtime, including hosts where network isolation is unavailable. A VM alone does not repair tool authorization or credential-proxy flaws.

These are research judgments, not accepted implementation commitments.

## 3. Threat model

| Threat | Protected asset | Controls | Residual concern |
|---|---|---|---|
| Prompt injection | Repositories and external accounts | Trusted operation authorization, scoped grants, sensitive-action review | Harm through already-authorized actions |
| Malicious installation/test code | Host and other tenants | Process isolation, gVisor/VM, patching | Runtime, kernel, or VMM vulnerabilities |
| Exfiltration | Code and secrets | Enforced egress, minimal readable mounts, credential broker | Allowed services can receive stolen data |
| Resource exhaustion | Neighbors and budgets | Aggregate resource quotas and deadlines | Shared hardware and I/O contention |
| Snapshot/cache contamination | Tenant state | Identity binding, private writable layers, cleanup | Retained backups and snapshots |
| MCP/RPC/preview bypass | Control plane and applications | Separate API and port authorization | Execution isolation does not constrain remote authority |

Command isolation covers wrapped subprocesses; environment isolation covers the entire worker; business authorization governs external effects. None substitutes for another. Hardware side channels, privileged host administrators, and provider control-plane compromise may require dedicated infrastructure where the demonstrated risk justifies it.

## 4. OS mechanisms

Linux namespaces partition mount, process, network, IPC, user, and other views; cgroups govern resources. UID mapping, capabilities, devices, inherited descriptors, Unix sockets, and mount propagation still matter. Changing cwd is not confinement. [Docker security](https://docs.docker.com/engine/security/).

Seccomp-BPF filters syscall numbers and direct arguments. It cannot dereference pathname pointers into a complete file policy. The kernel explicitly treats filtering as a component of sandbox construction. Broad profiles weaken protection; narrow profiles can break runtimes and development tools. Verify successful profile loading and refusal when required enforcement is absent. User-notification mediation introduces additional race and delegation concerns. [Kernel seccomp documentation](https://docs.kernel.org/userspace-api/seccomp_filter.html).

Landlock lets unprivileged software restrict its rights through a stackable LSM. Supported filesystem and port-based network controls depend on the running ABI; port restrictions are not domain or HTTP-operation authorization. Probe actual capabilities and define a minimum required set. Thread scope and enforcement timing matter when integrating with Go; best-effort sample behavior is not automatically appropriate for unattended workers. [Landlock](https://docs.kernel.org/userspace-api/landlock.html).

AppArmor commonly organizes policy around paths; SELinux around labels and types. Both add host mandatory access controls, require deployed policy and audit support, and retain the shared-kernel boundary. Neither supplies application-level domain or Git-branch authorization. [Docker LSM discussion](https://docs.docker.com/engine/security/).

Bubblewrap constructs isolation from caller-selected parameters. Read-only host mounts can still expose secrets; networking requires explicit enforcement. [Project documentation](https://github.com/containers/bubblewrap).

Anthropic publicly describes Linux bubblewrap and macOS Seatbelt combined with a proxy for local command confinement. This is not a tenant VM. [Claude Code sandboxing](https://www.anthropic.com/engineering/claude-code-sandboxing).

Windows requires its own native permissions or VM/WSL2 assessment. Multiple processes inside one WSL2 instance are not automatically separate VMs. BuildMax currently lacks a native Windows OS sandbox backend. This research does not qualify Windows-native compatibility.

## 5. Containers and Kubernetes

OCI containers offer broad existing Linux tool compatibility. Rootless reduces daemon and container host privileges but leaves shared-kernel, socket, mount, and network risks. [Rootless Docker](https://docs.docker.com/engine/security/rootless/).

A candidate baseline includes non-root execution, minimal capabilities, no privilege escalation, read-only rootfs, private temporary files, seccomp/LSM, aggregate quotas, no host-sensitive mounts or daemon sockets, and no unnecessary service-account token. Workloads must demonstrate compatibility and actual enforcement.

Kubernetes schedules execution and applies policy; Jobs, namespaces, and RBAC do not introduce a new kernel boundary. RuntimeClass can select a different runtime. [Kubernetes security](https://kubernetes.io/docs/concepts/security/).

NetworkPolicy enforcement depends on the network plugin. Standard policies primarily express L3/L4 rules, not generic domain or HTTP-path authorization. Verify ingress/egress, DNS, IPv6, metadata, node, and neighboring-Pod access. [NetworkPolicy](https://kubernetes.io/docs/concepts/services-networking/network-policies/).

Hardened containers can fit internal workloads with explicit risk acceptance. Public hostile multi-tenancy requires a defensible shared-kernel risk decision or evaluation of gVisor/VM isolation.

## 6. gVisor

gVisor's Sentry implements application-facing Linux interfaces in userspace; runsc integrates with containers and Gofer participates in file access. It reduces direct application exposure to the host kernel, rather than eliminating every host syscall or trusted component. [Architecture](https://gvisor.dev/docs/architecture_guide/intro/), [security model](https://github.com/google/gvisor/blob/master/g3doc/architecture_guide/security.md).

Container interfaces ease adoption, while syscall compatibility and file/syscall-heavy performance require workload testing. Browsers, debuggers, nested containers, and special devices require separate qualification. Mounts, secrets, and egress still need policy. [Production guidance](https://gvisor.dev/docs/user_guide/production/).

A BuildMax PoC should test Go, Node, Python, real repositories, file watching, background services, and existing bubblewrap nesting. If environment-level isolation replaces a command mechanism, change the accepted design explicitly rather than silently disabling checks.

## 7. MicroVMs and VMs

Firecracker uses KVM and a compact virtual-device model. Applications use a guest kernel; VMM, KVM, host, and hardware remain trusted and patchable components. Production guidance includes jailer, host hardening, seccomp, and resource configuration. [Design](https://github.com/firecracker-microvm/firecracker/blob/main/docs/design.md), [host requirements](https://github.com/firecracker-microvm/firecracker/blob/main/docs/prod-host-setup.md).

Firecracker is a VMM, not a complete agent service. Images, guest agents, networking, scheduling, file transfer, snapshots, logs, and cleanup remain platform responsibilities. It suits teams with KVM infrastructure and a separate-kernel requirement. GPUs, elaborate devices, and desktops need further assessment.

firecracker-containerd integrates containerd with Firecracker execution. The GitHub API reported `archived: false` on the review date; a new deployment must still establish release support, compatibility, and security response ownership. It is neither ordinary Docker nor the VMM itself. [Repository](https://github.com/firecracker-microvm/firecracker-containerd).

Cloud Hypervisor is another VMM with broader modern cloud features; it is not a scheduler. [Project](https://github.com/cloud-hypervisor/cloud-hypervisor).

Kata supplies container-runtime integration using a guest kernel and selectable hypervisors. Multiple containers in a Pod can share a VM, so the isolation unit must be understood at Pod level. [Kata architecture](https://github.com/kata-containers/kata-containers/blob/main/docs/design/architecture/README.md).

Kata preserves Kubernetes workflows but adds virtualization-node, guest, storage, networking, startup, and memory requirements. Qualify CSI, image sharing, nested sandboxing, and the chosen hypervisor/version combination. A published Cloud Hypervisor QCOW backing-file advisory demonstrates that image parsing and host file handling remain security concerns even with guest kernels. [Official advisory](https://github.com/cloud-hypervisor/cloud-hypervisor/security/advisories/GHSA-jmr4-g2hv-mjj6).

## 8. WebAssembly

Wasm constrains module memory and explicitly imported host functionality; WASI uses capability-oriented file access. Host APIs, compiler/runtime correctness, and output handling remain trusted concerns. [Wasmtime security](https://docs.wasmtime.dev/security.html).

Wasmer is another Wasm ecosystem; its introductory documentation alone does not establish every permission boundary of a concrete deployment. Qualify engine, WASI version, host imports, and advisories. [Wasmer documentation](https://docs.wasmer.io/).

Good candidates include recompilable utilities, format conversion, rules, and controlled plugins. Arbitrary Bash, native extensions, compilers, subprocesses, and services are not transparently compatible. One working Python example does not establish arbitrary Python project support. GPU and complete Linux development environments are outside the recommended Wasm use case here.

Overpowered host functions can still authorize harmful effects. Apply execution interruption/fuel, memory and output limits, and independent state.

## 9. Managed products

| Product | Publicly documented | Unresolved qualification | Implication |
|---|---|---|---|
| E2B | Firecracker microVM per sandbox; pause/resume; enterprise BYOC | Selected region, network controls, quotas, SDK, contracts and reports | Agent APIs accelerate integration; application authorization remains necessary |
| Modal | Both gVisor and VM runtimes; public egress by default with restrictions available | Runtime availability, domain feature status, devices and cost | Do not describe every sandbox as gVisor |
| Daytona | Default Linux containers and other sandbox types; isolation page also claims microVMs | Actual default host boundary, deployment, plan, version and provider explanation | Conflicting descriptions must be resolved before security selection |

Sources: [E2B security](https://e2b.dev/security), [Modal security](https://modal.com/docs/guide/sandbox-networking), [Daytona sandboxes](https://www.daytona.io/docs/en/sandboxes/), [Daytona isolation](https://www.daytona.io/docs/en/isolation/).

E2B distinguishes managed BYOC in a customer's account from self-hosting. Open-source availability does not establish commercial delivery terms. Modal sandbox resource authority is distinct from ordinary workspace access; injected credentials expand it. Preserve Daytona's discrepancy as an open issue rather than guessing its topology.

Procurement must establish execution unit, tenancy, residency, snapshot/log retention, deletion, actual egress granularity, private connectivity, credential scopes, incident response, and exit/export options. Compliance reports do not prove absence of execution escapes.

## 10. Agent product evidence

| Product | Public evidence | Do not infer |
|---|---|---|
| Local Claude Code | OS file/network confinement and proxy, bubblewrap/Seatbelt | VM isolation or prevention of all prompt injection |
| Cloud Claude coding | Scoped Git credential proxy; containment differs across products | One runtime across all Claude products |
| OpenAI sandbox/Agents environments | Hosted/self-hosted execution; isolation, egress and external credentials | Undisclosed VMM or every product's internal implementation |
| GitHub Codespaces | Separate VM and network per codespace, development container inside | Safety of arbitrary mounts or networking settings |
| Replit | 2026 article describes container-to-microVM rollout | Completed migration across every product and region |
| Code Interpreter category | A code-execution product category | Firecracker/gVisor solely from its name |

Sources: [Anthropic architecture](https://www.anthropic.com/engineering/claude-code-sandboxing), [OpenAI security](https://developers.openai.com/api/docs/guides/agents-api/environments/security), [Codespaces](https://docs.github.com/en/codespaces/reference/security-in-github-codespaces), [Replit engineering](https://replit.com/blog/defense-in-depth-how-replit-secures-every-layer-of-the-vibe-coding-stack).

These demonstrate differing combinations, not one universal industry standard. Historical launch defaults should not be generalized to current products.

## 11. Network and credentials

A candidate enforced-egress topology gives the execution environment no direct outbound route except a trusted proxy/gateway. Proxy environment variables are client conventions and can be ignored by malicious code. Enforce the restriction through namespaces, routing, firewalls, or provider networking.

Test raw TCP/UDP, DNS, IPv6, QUIC, literal IPs, redirects, CONNECT, allowed domains resolving to private addresses, rebinding, metadata, and Unix sockets. Package registries, CDNs, Git dependencies, and installation scripts are separate concerns.

An allowed domain may host an attacker's account. CONNECT destination checks cannot authorize encrypted HTTP methods, paths, or tenant-specific operations. A narrower business API may be needed. TLS interception introduces certificate and privacy consequences and should not be assumed.

Keep long-lived LLM, Git, and cloud credentials in trusted services. Use short-lived run-scoped grants; brokers validate repository, branch, operation, destination, and budget before attaching real credentials. OpenAI's documentation separates application and environment keys and describes external credential mediation. Actual secrets injected into environment variables remain readable by generated code. [OpenAI security](https://developers.openai.com/api/docs/guides/agents-api/environments/security).

Brokers must prevent arbitrary forwarding, cross-tenant use, replay, redirect leakage, and logging leakage. Protecting token bytes does not prevent misuse of already-authorized operations.

## 12. Files, resources, and lifecycle

| Stage | Evidence required |
|---|---|
| Create | Tenant binding, trusted image digest, no inherited host HOME/secrets |
| Prewarm | No previous tenant state; private writable cache layer; credential-free images |
| Execute | Separate read/write policy; descendants confined; aggregate quotas and deadline |
| Export | Path normalization, no escaping symlinks, count/size/permission limits |
| Pause | Memory, files, sockets, and credentials treated as sensitive |
| Resume | Revocations survive restore; expired grants replaced; version checks |
| Finish | Whole process group, routes, volumes, and temporary authority reclaimed |
| Cleanup | Idempotent after crashes/restarts; snapshot, backup and log retention applied |

Read-only HOME mounts still disclose secrets. Shared writable dependency caches enable poisoning and tenant contamination; favor trusted caches and per-run writes.

Process rlimits are not necessarily aggregate task limits. Include disk, inodes, PIDs, network, output, and time in budgets. Terminal escapes, HTML previews, and exported files are untrusted input to their viewers. Bind preview ports to authenticated runs and expire them.

Remote MCP business authority and local stdio process execution need separate governance.

## 13. Selection matrix

This is qualitative engineering analysis assuming correct configuration and patching; no measured figures or absolute security ranking are implied.

| Option | Boundary | Compatibility | Operational burden | Suitable condition | Main concern |
|---|---|---|---|---|---|
| bubblewrap/Seatbelt | Local OS policy/views | Usually good on native platform | Low–medium | Personal coding agent | Shared kernel and policy/socket exposure |
| Rootless OCI | Namespaces and host privilege | Usually good | Low–medium | Internal development/builds | Shared kernel, egress not automatic |
| OCI + gVisor | Userspace Linux interface | Workload-specific | Medium | Existing container platform, multi-tenancy | Syscalls/devices/nesting and I/O |
| Kata + VMM | Pod guest kernel | Usually good; integration-specific | Medium–high | Kubernetes with virtualization | KVM and guest operations |
| Direct Firecracker | MicroVM guest kernel | Good inside suitable guest | High | Custom separate-kernel execution | Full environment lifecycle ownership |
| Cloud Hypervisor | VM guest kernel | Good inside suitable guest | High | More VM functionality | Device/input surface maintenance |
| Wasmtime/Wasmer | Module and host imports | Not transparent Linux support | Low for controlled plugins, high migration | Narrow tools/plugins | Cannot replace full coding environment |
| Managed sandbox | Selected provider runtime | Product-specific | Lower integration, recurring costs | Rapid hosted delivery | Residency, provider and exit dependence |

Prefer OS confinement for local work; hardened containers for accepted internal risk; compare gVisor and Kata/VM for public hostile execution; consider Wasm for narrow plugins and managed services where outsourcing is acceptable. GPUs, browsers, nested Docker, and private networks are separate qualification requirements.

Do not build a universal backend abstraction before a demonstrated deployment requires it. Qualify one target and candidate lifecycle first.

## 14. Measurement and qualification

Split startup into API acceptance, scheduling, image preparation, isolation creation, guest/process boot, mounts/authorization, first command, and dependency readiness. Report cold starts, prewarming, and snapshot restoration separately.

Under matched hardware, images, limits, repositories, and cache conditions, record P50/P95/P99, failure rates, peak memory, CPU/I/O/storage, concurrency, and cleanup latency. An initial sample of at least 100 creations/restores can reveal issues but cannot establish a large-scale SLA.

Cost per successful task includes active CPU/memory, idle prewarm capacity, image/cache/snapshot storage, networking/logging, control plane, retries, and engineering operations. Recheck current billing duration, minimum increments, quotas, and paused-state costs. No potentially stale unit-price comparison is supplied.

| Qualification group | Scenarios | Required evidence |
|---|---|---|
| Compatibility | Real Go/Node/Python projects, browsers, builds, services | Versions, commands, exit status, limitations |
| Files | Outside reads/writes, symlinks, caches, sockets, inherited descriptors | Denials plus authorized positive controls |
| Network | Proxy-free direct access, IPv6/UDP, private/metadata addresses, DNS/redirects | Application result and trusted network observation |
| Credentials | Env/proc/log/export, unauthorized repository/branch, revoked replay | Real keys absent; trusted rejection |
| Resources | Fork, memory, disk, inode, output flooding | Limits enforced; neighbors remain healthy |
| Lifecycle | Cancel, crash, node/control-plane restart, stale restore | No orphan state, authority or tenant leakage |
| Enforcement failure | Missing backend/profile, proxy failure, unavailable runtime | Refusal in required-security mode; no silent fallback |

Destructive probes belong in isolated test infrastructure. Preserve configuration, versions, controls, observations, untested cases, and accepted risks. This is a test plan, not a passing report.

## 15. BuildMax assessment

Static inspection was refreshed against `origin/main` commit `7489748a` before opening the PR on the review date:

| Concern | Observed behavior | Consequence |
|---|---|---|
| Surface defaults | Official image marker selects strict worker baseline; unmarked host can select CLI baseline | Not every conceivable worker launch defaults fail closed |
| Linux Bash | bubblewrap mounts/private tmp/PID/IPC/UTS; parent proc rebound read-only; root commands drop capabilities and worker is non-dumpable | Command boundary; proc is not a wholly private view, with worker environment protection added |
| Linux egress | Restricted policy uses a probed network namespace and Unix-socket proxy bridge; allow-all, missing socat, failed probe, or proxy startup failure retain shared networking | Enforcement is conditional; shared-network runs can bypass proxy environment variables |
| Worker Pod | Root with SYS_ADMIN and NET_ADMIN, read-only rootfs, Localhost seccomp; Bash drops capabilities | Not non-root whole-worker strong isolation |
| MCP | Unattended profile refuses stdio; local stdio bypasses Bash wrapper | Bash confinement does not cover all MCP |
| local_process | Server host trust domain | Not an independent tenant environment |
| Native platforms | Linux bubblewrap, macOS Seatbelt; unsupported backend elsewhere | Native Windows needs another approach |

Sources: [current state](../current-state.md), [config](../../internal/config/sandbox.go), [Linux backend](../../internal/infra/sandbox/bwrap_linux.go), [worker runtime](../../internal/agentapp/taskrun/runtime.go), [Job](../../internal/infra/k8s/job.go), [unsupported backend](../../internal/infra/sandbox/unsupported_other.go). Network-isolation selection and fallback were also checked in [Manager](../../internal/infra/sandbox/manager.go). The network probe may fail without making the filesystem backend unavailable, so backend fail-closed policy is not proof of mandatory egress isolation. This is not deployed enforcement qualification.

Recommended sequence:

1. Prove existing file, direct-network, MCP, credential, and cleanup boundaries in an isolated worker environment. Verify `network_isolated` and failure paths, rather than treating domain selectors or backend fail-closed policy as proof of mandatory Linux egress.
2. Separate trusted authorization and credential custody from untrusted tools. Preserve Space ownership and Task/TaskRun execution ownership; inspect bridge, MCP, and HTTP paths as well as Bash.
3. For one real multi-tenant deployment, compare gVisor and Kata/VM. Consider direct Firecracker when its operational burden is justified. Qualify existing bubblewrap, capabilities, images, and network policy.
4. Change architecture only with explicit design updates and end-to-end evidence. This memo does not authorize bypassing fail-closed policy or adding unsandboxed fallback.

Single-binary Go CLI/TUI, private deployment, and agentapp ownership remain constraints. Borrow isolation mechanisms without adding Node to the shipped CLI.

## 16. Research register and open questions

All linked sources were accessed on 2026-10-02. Provider descriptions establish public claims, not independently verified deployments.

| Source group | Finding | Limit |
|---|---|---|
| Linux/Docker/bubblewrap/Kubernetes | Mechanism, policy, and scheduling are distinct | Actual host/configuration must be tested |
| gVisor | Userspace interface and production trade-offs | BuildMax compatibility not tested |
| Firecracker/Kata/Cloud Hypervisor | Guest kernel, VMM, and integration responsibilities | No KVM qualification performed |
| Wasmtime/Wasmer | Capability boundary and migration constraints | Detailed Wasmer model remains to verify |
| E2B/Modal/Daytona | Managed environment/runtime choices | Daytona discrepancy unresolved; no independent audit |
| Anthropic/OpenAI/GitHub/Replit | Different product boundaries and rollout states | Unpublished internals remain unknown |
| BuildMax implementation | Specific command/worker gaps | Static evidence only |

Before implementation, establish the deployment's adversaries and risk acceptance; whether a shared kernel is acceptable; KVM/nested-virtualization and staff availability; actual GPU/browser/Docker/ptrace/file-watching/private-network needs; Daytona's conflicting descriptions; provider price, region, contract, deletion and private-delivery terms; per-run versus per-Task environment lifetime; and whether a candidate can reduce worker privilege while retaining subprocess enforcement.

Require PoC evidence rather than brand names, advertised startup latency, or the presence of a VM. Revisit findings after new vulnerabilities, runtime changes, or provider rollouts.
