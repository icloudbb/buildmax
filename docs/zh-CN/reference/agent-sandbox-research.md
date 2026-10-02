# AI Agent 与不可信代码执行 Sandbox 研究备忘录

> **读者：** runtime、平台与安全工程贡献者 · **状态：** 研究记录，建议尚未成为实施决策
> **研究日期：** 2026-10-02 · **范围：** coding agent、代码解释器、插件执行与多租户 worker
> **英文依据：** [English memo](../../reference/agent-sandbox-research.md)。中文为派生镜像，存在差异时以英文为准。

相关记录：[BuildMax 当前状态](../../current-state.md)、[sandbox 设计](../../design/sandbox-boundaries.md)、[Agent sandbox 策略](../../design/agent-sandbox-policy.md)、[路线图](../../ROADMAP.md)。本文不修改这些记录中的已接受决策。

## 目录

- [1. 用户结果与研究方法](#1-用户结果与研究方法)
- [2. 核心结论](#2-核心结论)
- [3. 威胁模型与隔离层次](#3-威胁模型与隔离层次)
- [4. 操作系统机制](#4-操作系统机制)
- [5. 容器与 Kubernetes](#5-容器与-kubernetes)
- [6. gVisor](#6-gvisor)
- [7. MicroVM 与虚拟机](#7-microvm-与虚拟机)
- [8. WebAssembly 与 WASI](#8-webassembly-与-wasi)
- [9. 托管 Sandbox 产品](#9-托管-sandbox-产品)
- [10. Agent 产品的公开架构](#10-agent-产品的公开架构)
- [11. 网络与凭证](#11-网络与凭证)
- [12. 文件、资源与生命周期](#12-文件资源与生命周期)
- [13. 比较矩阵与选型](#13-比较矩阵与选型)
- [14. 性能、成本与验证计划](#14-性能成本与验证计划)
- [15. BuildMax 现状与建议](#15-buildmax-现状与建议)
- [16. 研究记录与待核实问题](#16-研究记录与待核实问题)

## 1. 用户结果与研究方法

目标是让 Agent 可以自主拉取代码、安装依赖、编译、测试、运行服务，同时把一次被提示注入、恶意依赖或错误命令影响的范围限制在被授权的执行环境内。开发者电脑上的 SSH key、其他租户的数据、平台控制面和生产权限都不应因为一次代码执行而自动暴露。

这一需求有现实依据：coding agent 会读取不可信仓库、网页和工具输出，随后执行任意程序；包管理器的安装脚本与测试程序本身也是代码执行。审批一条 shell 字符串不能证明其递归执行的行为安全。Anthropic 公开讨论了提示注入、模型越界和 containment 的工程经验。[官方经验记录](https://www.anthropic.com/engineering/how-we-contain-claude)

本次采用官方文档、项目代码与供应商技术说明；正文区分：**公开声明**、**本仓库静态核对**、**工程分析/建议**。公开声明不是独立审计结果；未执行逃逸测试、性能基准、付费产品试用或部署实验。动态网页核对日期为上述研究日期，网页没有稳定发布日期时不推断发布日期。

不追求列出所有厂商，也不建立“永不逃逸”的等级。重点覆盖可以落地的隔离机制、常见产品、运维生命周期与 BuildMax 的相关边界。价格、配额、地区、版本及维护活跃度需在采购或实施时重新核对。

## 2. 核心结论

1. **Sandbox 是组合系统。** 执行隔离、文件授权、网络出口、凭证代理、资源限制、控制面鉴权与清理都需要独立证据。
2. **容器默认共享宿主内核。** rootless、seccomp 与 LSM 有价值，但不能据此声称已经建立独立内核边界。
3. **通用 coding agent 的两个主要加强候选是 gVisor 与 VM。** 前者保留容器接口并减少直接宿主 syscall 暴露；后者增加 guest kernel 与 VMM 边界。选择取决于实际工具兼容性和基础设施能力。
4. **Wasm 更适合受控插件。** 通用仓库中的 Bash、原生二进制、编译链和后台服务无法透明迁移为 WASI 工作负载。
5. **网络域名允许列表不能替代操作授权。** 能访问 GitHub 不代表应能写任意仓库；能访问云 API 不代表应获得组织级权限。
6. **BuildMax 应先把现有边界说清楚并证明有效。** Linux 已加入网络命名空间，但隔离仍取决于策略、依赖与探测结果，失败路径保留共享网络；换一个 VM backend 也不会自动修复工具授权和代理漏洞。

以上是研究综合判断，不是新增路线图承诺。

## 3. 威胁模型与隔离层次

| 威胁 | 需要保护的对象 | 主要控制 | 残余风险 |
|---|---|---|---|
| 提示注入导致越权动作 | 仓库、业务应用、外部账号 | 可信端操作鉴权、有限授权、敏感动作审批 | 已授权动作本身仍可能造成损失 |
| 恶意测试与安装脚本 | 宿主、同机租户 | 进程隔离、gVisor 或 VM、补丁 | runtime、内核或 VMM 漏洞 |
| 外传代码与秘密 | 网络、凭证、工作区 | 强制出口、只读最小挂载、凭证代理 | 被允许服务仍可能接收秘密 |
| fork bomb、磁盘与日志洪泛 | 节点和成本预算 | cgroup、磁盘/PID/输出配额、期限 | 共享 I/O 与硬件资源干扰 |
| 快照与缓存串租户 | 历史状态、依赖缓存 | 身份绑定、私有可写层、失效和清理 | 备份及快照仍保留敏感数据 |
| RPC/MCP/预览端口绕过 | 控制面、其他应用 | 单独的 API 与端口授权 | 执行隔离不约束远端工具权力 |

区分三个边界：**命令级**只包裹特定子进程；**环境级**覆盖整个 worker 的进程、文件和网络；**业务级**约束 Git push、发信、部署等外部效果。三者不能互相代替。

共享 CPU 上的侧信道、宿主管理员访问、硬件漏洞和供应商控制面入侵不由普通 sandbox 完整解决。高敏感场景可能需要专用节点或独占机器，但这应由具体风险与成本决定。

## 4. 操作系统机制

### 4.1 namespaces、cgroups 与权限

Linux namespaces 提供 mount、PID、network、IPC、UTS、user 等视图隔离；cgroups 控制资源分配与进程组。它们组成容器的基础，但仍依赖同一个内核。需要明确 user namespace、UID 映射、capabilities、设备访问、继承 FD、Unix socket 和挂载传播；只改变工作目录没有安全隔离意义。[Docker security](https://docs.docker.com/engine/security/)

### 4.2 seccomp

seccomp-BPF 根据 syscall 号与直接参数过滤调用，缩小内核攻击面。它无法直接解引用路径字符串等指针，因此不能靠一个普通 syscall 过滤器实现完整的文件路径授权。内核文档明确将其定位为 sandbox 的组成机制。[Linux seccomp](https://docs.kernel.org/userspace-api/seccomp_filter.html)

过滤过宽削弱保护，过窄影响语言 runtime、线程、调试和构建工具。应该记录加载成功的证据、实际 profile 与架构，并验证缺少 profile 时拒绝运行。seccomp user notification 的用户态代理需要额外处理竞争条件，不能直接假定检查参数后代执行一定安全。

### 4.3 Landlock

Landlock 是允许非特权程序收紧自身权限的可叠加 LSM。能力取决于内核 ABI；当前官方文档包括文件与按端口限制的网络规则。端口规则不等同于域名或业务 API 策略。必须探测运行系统实际能力，不能把最新文档能力当成每台 Linux 的能力。[Landlock 文档](https://docs.kernel.org/userspace-api/landlock.html)

对 Go runtime 集成，要特别核实线程、子进程和规则生效时机。安全模式需要明确最低能力与 fail-closed 条件；官方示例里的兼容性降级并不自动适合无人值守 worker。

### 4.4 AppArmor 与 SELinux

它们为宿主提供强制访问控制。AppArmor 通常以路径规则组织策略，SELinux 以标签和类型策略组织访问。可为容器和 worker 提供额外限制，但需要发行版支持、策略部署与审计能力。二者都不增加独立 guest kernel，也不直接提供应用级域名或 Git 分支授权。[Docker security 的 LSM 说明](https://docs.docker.com/engine/security/)

### 4.5 bubblewrap、macOS 与 Windows

bubblewrap 是构造 namespace/mount 沙箱的工具，最终安全性取决于调用者传入的参数。把宿主根目录只读挂载并不能保护其中的秘密；必须单独限制读取。网络隔离也需要实际配置。[bubblewrap 项目](https://github.com/containers/bubblewrap)

macOS 本地命令隔离可以使用 Seatbelt 相关机制；Anthropic 公开方案采用 macOS Seatbelt 与 Linux bubblewrap，并结合代理。它是本地权限约束，不等同于租户 VM。[Claude Code sandboxing](https://www.anthropic.com/engineering/claude-code-sandboxing)

Windows 应独立评估原生权限隔离或 VM/WSL2 路径，不能假定 Linux flags 可用。WSL2 内运行多个任务也不意味着每个任务各有一台独立 VM。BuildMax 当前不提供原生 Windows OS sandbox backend，见第 15 节。本研究没有完成 Windows 原生方案的兼容性验证。

## 5. 容器与 Kubernetes

Docker/OCI 容器最适合快速运行既有 Linux 工具链。rootless 降低 daemon 与容器的宿主权限，但共享内核、挂载泄漏、暴露 daemon socket 和错误网络授权仍需防范。[Docker rootless](https://docs.docker.com/engine/security/rootless/)

建议的待验证基线：非 root、尽量丢弃 capabilities、禁止提权、只读 rootfs、私有临时目录、seccomp/LSM、CPU/内存/PID/磁盘配额、无宿主敏感挂载、无容器管理 socket、禁止自动注入不需要的服务账号令牌。是否支持这些条件必须按 workload 实测，不能把模板配置当作已生效证据。

Kubernetes 是调度与策略平台。Job、namespace、RBAC 并不会自动增强 runtime 的内核隔离；RuntimeClass 可以选择 gVisor/Kata 等运行时。[Kubernetes security](https://kubernetes.io/docs/concepts/security/)

NetworkPolicy 依赖网络插件实现。标准策略主要表达 L3/L4 规则，不提供通用 HTTP 路径或域名授权；配置应覆盖 ingress/egress，并验证 DNS、IPv6、metadata、节点和其他 Pod 的可达性。[NetworkPolicy](https://kubernetes.io/docs/concepts/services-networking/network-policies/)

容器适用于风险可接受的内部开发与构建。若目标是公开多租户恶意代码服务，应明确共享内核的风险接受依据，或评估 gVisor/VM，不能把普通容器包装成独立内核 sandbox。

## 6. gVisor

gVisor 的 Sentry 在用户态实现应用所需的 Linux 系统接口；runsc 与容器生态集成，文件访问还有 Gofer 等组成部分。它减少应用直接访问宿主内核的范围，但仍有可信组件和宿主调用，不等于“完全不使用宿主内核”。[架构](https://gvisor.dev/docs/architecture_guide/intro/)、[安全模型](https://github.com/google/gvisor/blob/master/g3doc/architecture_guide/security.md)

工程取舍：保留镜像与调度接口，适合已有 Kubernetes/containerd 的 Linux 服务；syscall 兼容性、文件系统和 syscall 密集型性能必须实测。浏览器、调试器、嵌套容器、特殊设备等不能仅凭普通 Python 能运行而判定支持。gVisor 也不是凭证、挂载与网络授权的替代品。[生产指南](https://gvisor.dev/docs/user_guide/production/)

BuildMax 的候选 PoC 应覆盖 Go/Node/Python、真实测试集、文件监控、后台服务以及现有 bubblewrap。嵌套 sandbox 能否工作必须验证；若改为环境级隔离，应显式修改设计与策略，不能靠关闭检查默默降级。

## 7. MicroVM 与虚拟机

### 7.1 Firecracker

Firecracker 使用 KVM，以精简虚拟设备运行 microVM。应用调用 guest kernel，逃出 guest 后还需要突破虚拟化相关边界；VMM、KVM、宿主及硬件仍需要补丁。生产要求包含 jailer、宿主加固、seccomp 和资源配置。[设计](https://github.com/firecracker-microvm/firecracker/blob/main/docs/design.md)、[生产宿主要求](https://github.com/firecracker-microvm/firecracker/blob/main/docs/prod-host-setup.md)

它是 VMM，不是开箱即用的 Agent 平台。镜像构建、guest agent、网络、调度、文件交换、快照、日志与清理都需平台提供。适合有 KVM 节点和平台工程能力、要求独立 kernel 的执行服务。GPU、复杂设备或完整桌面需要另外选型。

### 7.2 firecracker-containerd

该项目把 containerd 容器管理接到 Firecracker microVM，属于集成层，不应与普通 Docker 或 Firecracker 本体混为一类。研究日期的 GitHub API 返回 `archived: false`，不能将其描述为已归档；新系统采用前仍必须确定版本支持、维护责任、兼容矩阵和漏洞响应路径，不能把它列为无需维护的默认候选。[官方仓库](https://github.com/firecracker-microvm/firecracker-containerd)

### 7.3 Cloud Hypervisor 与 Kata

Cloud Hypervisor 是面向现代云 workload 的 Rust VMM，支持较丰富的设备和生命周期功能。它与 Firecracker 是同层候选，不是上层调度器。[项目说明](https://github.com/cloud-hypervisor/cloud-hypervisor)

Kata 则提供容器 runtime 集成层，用 guest kernel 运行容器，可接入不同 hypervisor。同一个 Pod 可以有多个容器共享 VM，因此隔离单元通常需按 Pod 理解，不能把每个 sidecar 当成另一台独立 VM。[Kata 架构](https://github.com/kata-containers/kata-containers/blob/main/docs/design/architecture/README.md)

Kata 适合希望保留 Kubernetes 工作流而增加虚拟化边界的团队；需要确认节点虚拟化、存储/网络/CSI、镜像共享、guest 启动、内存开销和嵌套 sandbox。Firecracker/Cloud Hypervisor 的具体设备与版本支持要按选定组合核对。

虚拟化安全还包含镜像解析与宿主文件处理。Cloud Hypervisor 曾公开 QCOW backing-file 相关宿主文件泄漏漏洞；这说明“有 guest kernel”不能替代 VMM 与镜像输入安全。[官方 advisory](https://github.com/cloud-hypervisor/cloud-hypervisor/security/advisories/GHSA-jmr4-g2hv-mjj6)

## 8. WebAssembly 与 WASI

Wasm 以模块内存和显式导入的 host 功能形成约束；WASI 文件访问采用 capability 模型。Wasmtime 还强调 host API、编译器与输出处理的安全责任。[Wasmtime security](https://docs.wasmtime.dev/security.html)

Wasmer 也是 Wasm runtime/生态选择，但本次所读入门页不足以证明具体部署的全部权限边界。需要核对实际 engine、WASI 版本、host imports 与 advisories。[Wasmer 官方文档](https://docs.wasmer.io/)

建议用于可重编译的小工具、格式转换、受控插件与规则计算。限制是现有 POSIX、原生扩展、任意编译器、子进程和服务不一定支持；Python 能在某个 Wasm 环境运行并不证明任意 Python 项目兼容。

Wasm 的 host function 若直接暴露文件、网络或高权限业务 API，模块仍可在授权范围内造成损失。另需执行时限、fuel/中断、内存限制、输出上限与独立实例状态。GPU 和完整 Linux 开发环境不作为本研究的 Wasm 推荐场景。

## 9. 托管 Sandbox 产品

| 产品 | 官方资料能确认什么 | 需要另行确认什么 | 工程选型含义 |
|---|---|---|---|
| E2B | 声明每个 sandbox 使用独立 Firecracker microVM；支持暂停恢复；BYOC 为企业方案 | 所选地区、网络规则、配额、SDK、审计报告及采购条件 | 较快获得 Agent 环境 API，仍需应用授权 |
| Modal | 文档列出 gVisor 和 VM 两种 runtime；默认允许访问公网，提供出口限制 | 所选 runtime 可用性、域名功能状态、特殊设备、成本 | 不能再统一描述为“所有 sandbox 都是 gVisor” |
| Daytona | sandbox 页面列出默认 Linux 容器及 VM 等类型；isolation 页面又声明 microVM | 默认类型的真实宿主边界、套餐、部署方式、版本及厂商解释 | 存在描述差异，先验证再比较 |

来源：[E2B security](https://e2b.dev/security)、[Modal networking/security](https://modal.com/docs/guide/sandbox-networking)、[Daytona sandbox](https://www.daytona.io/docs/en/sandboxes/)、[Daytona isolation](https://www.daytona.io/docs/en/isolation/)。

E2B 的 BYOC 说明明确区分“供应商管理、部署到你的账号”与自行托管；不能从开源仓库存在推导商业 BYOC 的交付条件。Modal 的 sandbox 不默认获得其他 workspace 资源的权力，但显式注入的凭证会扩大权限。Daytona 的文本矛盾保留为待核实项，不替厂商猜测实际拓扑。

托管方案采购应要求：执行单元定义、租户边界、数据驻留、快照/日志保留、删除行为、网络规则实际粒度、私网连接、令牌 scope、事故响应与退出路径。SOC 2 等控制审计不能直接证明代码执行没有逃逸风险。

## 10. Agent 产品的公开架构

| 产品/形态 | 公开确认 | 本研究不推断 |
|---|---|---|
| Claude Code 本地 | OS 文件/网络限制与代理；公开使用 bubblewrap/Seatbelt | 等同 VM，或能阻止所有提示注入 |
| Claude 云端代码环境 | 官方描述 scoped Git 凭证代理；其他 Claude 产品有不同 containment | 全部产品共享同一 runtime |
| OpenAI sandbox/Agents 环境 | 官方支持托管及自托管，强调环境隔离、出口和环境外凭证 | 未披露的 VMM、宿主与每个产品的内部实现 |
| GitHub Codespaces | 每个 codespace 独立 VM 与网络，内部使用开发容器 | 任意挂载和网络设置仍然安全 |
| Replit | 2026 官方文章称正在从容器迁移到 microVM | 所有地区和产品已经迁移完毕 |
| Code Interpreter 类产品 | 属于代码执行产品形态 | 仅从产品名推断 Firecracker/gVisor |

来源：[Anthropic 本地及 Git 代理](https://www.anthropic.com/engineering/claude-code-sandboxing)、[OpenAI sandbox security](https://developers.openai.com/api/docs/guides/agents-api/environments/security)、[Codespaces security](https://docs.github.com/en/codespaces/reference/security-in-github-codespaces)、[Replit 工程文章](https://replit.com/blog/defense-in-depth-how-replit-secures-every-layer-of-the-vibe-coding-stack)。

这组产品证明的是多种边界组合正在被采用，而不是存在一个所有 Agent 产品都使用的行业标准。旧发布文章中的网络默认值也不能作为当前全部产品的默认值。

## 11. 网络与凭证

### 11.1 强制出口

推荐待验证拓扑：执行环境无法直接外连，只能到受控代理/网关；代理在可信边界中鉴权、解析目的地并连接。HTTP_PROXY/HTTPS_PROXY 只是协作客户端约定，恶意程序可以忽略。必须在 namespace、路由、防火墙或运行平台网络层封住直连。

测试需覆盖 raw TCP/UDP、DNS、IPv6、QUIC、IP literal、重定向、代理 CONNECT、解析到私网的允许域名、DNS rebinding、metadata 与 Unix socket。允许下载依赖时，registry 的 CDN、Git 依赖和安装脚本要分别处理。

允许域名并不能阻止往该域名上的攻击者账号上传数据。仅检查 CONNECT host 也无法判断加密流量内的 HTTP 方法、路径和租户；业务操作需要更窄的代理/API。 TLS inspection 涉及证书信任和隐私，不能默认加入所有场景。

### 11.2 凭证代理

长期 LLM key、GitHub token、云管理 key 应留在可信控制面。sandbox 用短期、单 run、有限 scope 的凭证调用代理；代理验证目的仓库、分支、路径、API 动作以及配额，再加真实凭证。

OpenAI 官方安全文档也区分应用 key 和执行环境 key，并建议对第三方访问使用环境外代理；环境变量中的真实 secret 仍可被生成代码读取。[OpenAI sandbox security](https://developers.openai.com/api/docs/guides/agents-api/environments/security)

代理必须防止任意目标转发、跨租户授权、token 重用、重定向泄漏和日志泄漏。即使 token 无法导出，Agent 仍可能滥用已授权动作；凭证保护与操作审批需要分别证明。

## 12. 文件、资源与生命周期

| 阶段 | 应验证的行为 |
|---|---|
| 创建 | 租户身份与环境绑定；可信镜像 digest；不继承宿主 HOME 和秘密 |
| 预热 | 空白环境池不带前一租户状态；缓存私有写层；镜像内无账号 key |
| 执行 | 工作区外读取/写入分别限制；子孙进程受约束；有效 quota 与 deadline |
| 导出 | 路径规范化；不跟随越界符号链接；大小、数量与权限限制 |
| 暂停 | 内存、文件、socket 和凭证状态按敏感数据处理 |
| 恢复 | 撤销的授权不因快照复活；过期 token 重新发放；版本差异检查 |
| 结束 | 杀死整组进程；回收网络、卷、临时凭证与路由 |
| 清理 | 崩溃/超时/控制面重启可重复清理；快照、备份和日志按保留策略处理 |

只读文件仍能泄露；整个 HOME 只读挂载仍会暴露 SSH 配置、浏览器资料与开发凭证。共享可写依赖缓存会形成投毒与串租户风险，应优先采用可信构建缓存和每 run 可写层。

rlimit 与 cgroup 控制粒度不同，不能把某个进程的限制当作整个任务的聚合配额。磁盘容量、inode、PID、网络、日志输出和运行时间也必须计入成本。TTY escape、HTML 预览和导出文件会攻击展示端，展示器同样需要处理不可信内容。

预览端口应绑定 run、鉴权、到期回收，避免把本地开发服务器直接暴露公网。远端 MCP 工具的数据库/发信权限属于业务边界；本地 stdio MCP 属于进程边界，两者需分别治理。

## 13. 比较矩阵与选型

以下是工程分析，假设各方案正确配置且持续更新；没有实测数字，也不把行顺序解释为绝对安全排名。

| 方案 | 主要边界 | Linux 工具兼容性 | 起步/运维成本 | 适用条件 | 主要不足 |
|---|---|---|---|---|---|
| bubblewrap/Seatbelt | 本地 OS 权限与视图 | 同平台通常较好 | 低至中 | 本地 coding agent | 共享内核；策略和 socket 泄漏 |
| rootless OCI | namespaces、宿主权限 | 通常较好 | 低至中 | 私有开发/内部构建 | 共享内核；不能替代出口治理 |
| OCI + gVisor | 用户态 Linux 接口 | 需 workload 测试 | 中 | Linux 多租户，已有容器平台 | syscall/设备/嵌套兼容与 I/O 性能 |
| Kata + VMM | Pod 级 guest kernel | 通常较好，集成需验证 | 中至高 | Kubernetes 强隔离 | KVM 节点与 guest 运维 |
| 自建 Firecracker | microVM guest kernel | guest 内较好 | 高 | 独立内核、定制执行平台 | 环境生命周期需自行实现 |
| 自建 Cloud Hypervisor | VM guest kernel | guest 内较好 | 高 | 较复杂 VM 功能 | 更丰富设备与输入面需维护 |
| Wasmtime/Wasmer | 模块内存与 host imports | 不透明兼容原生 Linux | 受控插件低，迁移高 | 小插件与能力受限工具 | 无法替代完整 coding 环境 |
| 托管 sandbox | 取决于实际 runtime | 取决于产品 | 集成较低，持续费用 | 尽快上线托管执行 | 供应商、数据驻留与退出成本 |

建议路径：本地开发优先 OS sandbox；内部构建在明确风险接受后使用加固容器；公开多租户执行并行比较 gVisor 和 Kata/VM 的证据；专用插件考虑 Wasm；缺少平台团队且允许托管时验证托管服务。GPU/浏览器/嵌套 Docker 是单独的兼容性条件。

不要为所有部署预先建立通用 backend 框架。先选择一个实际目标与候选运行时，证明生命周期与边界后再判断是否需要抽象。

## 14. 性能、成本与验证计划

### 14.1 统一测量口径

“启动时间”必须拆开：API 接受 → 调度 → 镜像准备 → 创建隔离单元 → guest/进程启动 → 挂载与授权 → 第一个命令 → 依赖可用。快照恢复、预热池与全冷启动分别报告；不拿供应商热启动宣传数比较另一方案的冷镜像拉取。

每个候选在相同硬件、资源限制、镜像、仓库和缓存条件下记录 P50/P95/P99、失败率、峰值 RSS、CPU、I/O、磁盘、最大并发与清理延迟。初始计划可用至少 100 次创建与恢复观察尾部，但仍需注明样本有限，不能据此推断大规模 SLA。

### 14.2 成本模型

每个成功任务总成本 = 活跃 CPU/内存 + 空闲预热 + 镜像/缓存/快照存储 + 网络与日志 + 控制面 + 失败重试 + 平台维护。托管的计费时长、最小粒度、并发上限和暂停费用需拿当期价格验证；本研究不提供可能过期的单价。

高密度只降低部分计算成本。真实瓶颈可能是镜像分发、依赖下载、MySQL、对象存储或节点 I/O；对 coding agent 而言安装依赖和测试常比创建 sandbox 更耗时。

### 14.3 PoC 验收记录

| 测试组 | 内容 | 通过证据 |
|---|---|---|
| 兼容性 | Go/Node/Python、编译、真实测试、浏览器、后台服务 | 命令、版本、退出码、必要限制 |
| 文件 | 工作区外读写、symlink、缓存、socket、继承 FD | 拒绝结果与合法动作正向对照 |
| 网络 | 不设 proxy 的直连、IPv6/UDP、metadata、内网、DNS/重定向 | 应用结果及可信网络侧观测 |
| 凭证 | env/proc/log/导出、越权仓库/分支、撤销后重放 | 未暴露真实 key；可信端拒绝 |
| 资源 | fork、内存、磁盘、inode、输出洪泛 | 配额生效且邻居正常 |
| 生命周期 | 取消、崩溃、节点/控制面重启、过期快照恢复 | 无残留进程/凭证/路由及串租户 |
| 失效行为 | 缺依赖、profile 未加载、代理宕机、runtime 不可用 | 安全模式拒绝执行，无静默降级 |

破坏性用例只在隔离测试环境进行。报告应保存测试配置与版本、正反向对照、观测结果、未测项目及已接受风险。本备忘录提供计划，未声称这些测试已经通过。

## 15. BuildMax 现状与建议

### 15.1 本仓库静态核对

提交 PR 前已同步并重新核对研究日期的 `origin/main` commit `7489748a`；这是实现阅读，不是本轮部署验收。

| 关注点 | 当前可确认行为 | 含义 |
|---|---|---|
| surface 默认值 | 官方 worker 镜像标记选择严格 baseline；未标记宿主可落到 CLI baseline | 不能声称任意 worker 启动都默认 fail closed |
| Linux Bash | bubblewrap 挂载隔离、私有 tmp、PID/IPC/UTS；只读重新挂载父级 proc；root 命令丢弃 capabilities，worker 设置 non-dumpable | 是命令级边界，proc 并非完全私有视图，已增加 worker 环境保护 |
| Linux 网络 | 受限策略采用探测通过的网络命名空间与 Unix socket 代理桥；allow-all、缺少 socat、探测失败或代理启动失败会保留共享网络 | 隔离是条件性的；共享网络运行仍可绕过代理 env |
| worker Pod | 当前 root 加 SYS_ADMIN 与 NET_ADMIN、只读 rootfs、Localhost seccomp；Bash 丢弃 capabilities | 不能描述成非 root 全 worker 强隔离 |
| MCP | 无人值守 worker profile 拒绝 stdio；本地 stdio 不经 Bash wrapper | Bash 的隔离不自动覆盖所有 MCP |
| local_process | 仍在 server 宿主信任域 | 不等同独立租户执行环境 |
| 平台支持 | Linux bubblewrap、macOS Seatbelt；其他原生平台不支持该 backend | 原生 Windows 需独立方案 |

依据：[current-state](../../current-state.md)、[sandbox config](../../../internal/config/sandbox.go)、[Linux backend](../../../internal/infra/sandbox/bwrap_linux.go)、[worker runtime](../../../internal/agentapp/taskrun/runtime.go)、[Kubernetes Job](../../../internal/infra/k8s/job.go)、[不支持平台 backend](../../../internal/infra/sandbox/unsupported_other.go)。

### 15.2 建议顺序与决策门槛

网络隔离选择与失败路径另经 [Manager](../../../internal/infra/sandbox/manager.go) 核对：网络探测失败不会自动把文件隔离 backend 标记为不可用，因此 backend 的 fail-closed 不能证明出口隔离也必然 fail closed。

**第一步：证明现有边界。** 在隔离 worker 环境运行文件、直连网络、MCP、凭证和清理探针，明确哪些只能约束合作客户端。应核对 `network_isolated` 与降级路径；Linux 强制出口是独立需求，不能用域名列表 UI 或 backend 的 fail-closed 设置代替。

**第二步：按业务授权拆开可信与不可信执行。** 保持 Space 为资源授权边界；Task/TaskRun 拥有执行状态。可信代理持有平台与外部凭证，不可信工具侧仅获得有限 run 授权。对 bridge/MCP/HTTP 工具逐一核对，不能只验证 Bash。

**第三步：对一个真实部署比较环境级 runtime。** 若需求是公开多租户 Linux 执行，比较 gVisor 与 Kata/VM；有成熟 microVM 运维团队时再比较直接 Firecracker。验证现有 bubblewrap、SYS_ADMIN、镜像与网络策略在候选上的必要性和兼容性。

**第四步：只在有结果后改架构。** 全 worker 放入 VM 会改变基础设施成本、凭证路径和运行边界；需要提出明确设计变更、更新 source of truth 并完成端到端证据。本备忘录不授权绕过现有 fail-closed 或加入自动无沙箱 fallback。

CLI/TUI 单 Go binary、私有部署、现有 agentapp ownership 都是当前约束。可借鉴开源方案的机制，但不应为了获得本地 sandbox 引入 Node runtime 到 CLI。

## 16. 研究记录与待核实问题

### 16.1 资料登记

所有下列资料于 2026-10-02 查阅；正文链接为具体主张的依据。官方描述只证明提供者公开声称的机制，部署保证仍需验证。

| 资料组 | 研究所得 | 证据限制 |
|---|---|---|
| Linux/Docker/bubblewrap/Kubernetes | 区分内核机制、策略与调度 | 配置与宿主支持决定真实效果 |
| gVisor | 用户态内核接口与生产取舍 | 未运行 BuildMax workload |
| Firecracker/Kata/Cloud Hypervisor | guest kernel、VMM、runtime 集成层分工 | 未运行 KVM 或兼容性测试 |
| Wasmtime/Wasmer | 模块能力边界与迁移限制 | Wasmer 深入安全模型待补充 |
| E2B/Modal/Daytona | 托管环境与实际 runtime 类型 | Daytona 页面差异未解决；未独立审计 |
| Anthropic/OpenAI/GitHub/Replit | 产品间隔离机制和 rollout 不同 | 内部未披露细节不推断 |
| BuildMax 当前代码 | 命令 sandbox 与 worker 边界的具体差距 | 静态核对，未完成运行验证 |

### 16.2 实施前仍需回答

- 目标是个人本地助手、企业内部执行，还是公众多租户平台？哪些攻击者能力必须覆盖？
- 是否允许共享宿主内核？是否已有 KVM 节点、可用 nested virtualization 与运维人员？
- 哪些真实仓库需要 GPU、浏览器、Docker build、ptrace、文件监控或私网服务？
- Daytona 默认容器与 microVM 文档差异对应什么实际部署与套餐？
- 托管产品的地区、价格、合同、删除语义、审计报告和私有交付是否满足需求？
- 每 run 或每 Task 的环境寿命怎样影响状态恢复、授权撤销与存储费用？
- BuildMax 在候选 runtime 上能否同时收紧 worker 权限并保持现有子进程隔离？

后续决策应附真实 PoC 结果，不能仅根据品牌、宣传启动时间或“使用 VM”做结论。新漏洞、runtime 更换或供应商 rollout 都应触发复核。
