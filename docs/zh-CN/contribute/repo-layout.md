# 仓库目录树

> **翻译说明：** 本文是[英文原文](../../contribute/repo-layout.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
> **受众：** 贡献者 · **状态：** 当前有效
>
> **本文件是仓库目录树的唯一权威来源。** README、`AGENTS.md` 和架构参考
> 文档都链接到这里，而不是重复它，这样包移动时只有一个地方需要更新。

## 顶层目录

```text
buildmax/
├── cmd/                  Entry points for the shipped binaries (main.go only)
├── tools/                Build and test tooling; never shipped
├── internal/             All Go implementation
├── portal/               Portal web app (React 19 + Vite + TypeScript)
├── desktop/frontend/     Desktop frontend (React 19 + Vite); src/lib holds the
│                      pure helpers, src/components the panels and modals
├── gui/                  Shared React package @buildmax/gui, used by both
├── docs/                 Documentation
├── config-examples/      settings.yaml / server.yaml / hooks.yaml examples
├── deployment/           Deployment manifests, Compose, Dockerfiles, local kind
├── evaluation/           Evaluation and qualification system
├── sample-data/          Seed datasets to upload into a workspace or point the CLI at
├── .github/              CI workflows, issue and PR templates, community health files
├── .buildmax/            This repository's own workspace agent config — see .buildmax/README.md
├── make, make.bat        One-line shims around the task runner in tools/mk
└── *.md, LICENSE         README, CONTRIBUTING, SECURITY, CHANGELOG, AGENTS
```

以下内容是生成产物，从不提交：`.local/`（你自己的配置，由
`./make setup local` 写入）、`bin/`（`./make build` 的输出）、`dist/`
（GoReleaser）、`NOTICE-THIRD-PARTY`、`testing-sandbox/`（`./make test`
的数据目录），以及每一个 `node_modules/` 和前端的 `dist/`。

根目录下的 Markdown 文件，以及各自面向的读者：

| 文件 | 受众 |
|---|---|
| `README.md` | 任何登陆本仓库的人 |
| `CONTRIBUTING.md` | 贡献者——前置条件、构建、测试、拉取请求 |
| `SECURITY.md` | 漏洞报告者与运维人员 |
| `CHANGELOG.md` | 用户与运维人员，按发布版本组织 |
| `AGENTS.md` | Agent，在此工作区的每次运行中都会读取（遵循 [agents.md](https://agents.md/) 约定）。`CLAUDE.md` 指向它。 |
| `.github/CODE_OF_CONDUCT.md`、`SUPPORT.md`、`GOVERNANCE.md`、`MAINTAINERS.md`、`TRADEMARKS.md` | 社区健康文件。GitHub 会像展示根目录文件一样，从 `.github/` 中展示它们。 |

`deployment/` 保存的是运行一次部署所需的一切，而不是构建一个二进制文件
所需的东西：

| 路径 | 内容 |
|---|---|
| `deployment/docker/` | `Dockerfile.buildmax`（从源码构建 Go 二进制文件）、`Dockerfile.portal`（通过 nginx 提供 Portal）、`Dockerfile.release`（打包 GoReleaser 交叉编译出的二进制文件）。三者都以**仓库根目录**作为构建上下文。 |
| `deployment/compose/` | 单机 Compose 技术栈——一条**真实的部署路径**，运行已发布的 GHCR 镜像；见 [deploy/compose.md](../deploy/compose.md) |
| `deployment/kind/` | 搭建**本地开发用** kind 集群的清单——kind 配置、ingress-nginx、MySQL、MinIO。从不属于真实部署，由 `tools/mk/kind.go` 在 `./make kind up` 背后应用。 |
| `deployment/ocean/` | 用于一次性 DigitalOcean beta 资质验证基础设施的 OpenTofu 配置。它读取持久化的 Project、VPC 和 Spaces bucket，只拥有 `./make ocean` 背后临时的 DOKS 和 MySQL 资源。 |
| `deployment/production/` | 私有部署参考：一份写来供人阅读和改造的纯 YAML 清单，外加它所假定的依赖契约。刻意不做成 chart 或 kustomize base，这样它能转换成集群已经在用的任何管理方式。没有任何东西会应用它；`internal/architecture` 会解析它，以防它腐坏 |
| `deployment/smoke/` | 让 Compose 和 kind 冒烟测试保持确定性的覆盖配置与 mock 模型 |
| `deployment/buildmax-deploy.yaml` | `./make kind up` 所使用的可工作 Kubernetes 清单 |

`kind/` 仍然是本地测试基础设施，不是受支持的部署路径：这个简称对应
`./make kind` 命令，而这张表和 [deploy/local-kind.md](../deploy/local-kind.md)
定义了它的范围。`compose/` 不同，因为它是设计给运维人员运行的——它的受众
是运维人员，`README.md` 把它归在"为一个 Space 运行它"之下，
`compose.yaml` 拉取的是 `ghcr.io/icloudbb/buildmax`。`smoke/` 是两个
冒烟测试共用的测试脚手架。

本仓库没有 `scripts/` 目录。仓库工具——发布归档校验、第三方声明生成、
npm 许可证检查——都放在 `tools/mk` 中，以 `./make` 命令的形式存在，这样
每项任务在 macOS、Linux 和 Windows 上都以相同方式运行，并由相同的测试
覆盖。CI 和 GoReleaser 以 `go run ./tools/mk <task>` 的方式调用这些命令，
不需要任何 shell。

## 嵌套的 Go 模块

有一类目录位于根模块之外，拥有自己的 `go.mod`：

- **`gui/`、`portal/`、`desktop/frontend/`** 不包含任何 Go 代码。它们的
  `go.mod` 是一道边界。Go 工具不会像对待 `testdata` 那样对 `node_modules`
  做特殊处理，因此如果没有这道边界，根目录下的每一次 `go build ./...`、
  `go vet ./...`、`go test ./...` 和 `go mod tidy`，都会编译 npm 包碰巧
  附带的任何 Go 源码——由 ESLint 间接引入的 `flatted` 包就带了一份。任何
  安装 npm 依赖的目录都需要一个 `go.mod`；`internal/architecture` 中有
  一个测试强制执行这一点。
`evaluation/suite/*/state/` 目前不需要这样的边界：提交的 Task fixture
只保存数据和文本，而不是 Go 源码。如果将来某个 fixture 打包了 Go 模块，
它就需要出于和前端相同的原因拥有自己的 `go.mod`——否则 `go build ./...`
会编译一个本该是坏的 fixture。

## 二进制程序

| 路径 | 二进制程序 | 角色 |
|---|---|---|
| `cmd/buildmax` | `buildmax` | CLI/TUI |
| `cmd/buildmax-server` | `buildmax-server` | HTTP API + 进程内调度器 |
| `cmd/buildmax-worker` | `buildmax-worker` | 执行一次 Task run，然后退出 |
| `cmd/buildmax-desktop` | — | Wails 桌面应用；内嵌 `desktop/frontend` |

每个 `cmd/*` 包都是一个薄薄的 `main.go`，委托给 `internal/`。

## `tools/`

`tools/` 保存用于构建和测试本仓库的 Go 程序。它们使用与项目其余部分相同
的语言，这样每项任务无需 shell 即可在 macOS、Linux 和 Windows 上运行，
但它们不是产品的一部分：没有任何发布会构建它们，`cmd/` 或 `internal/`
下的任何代码也都不能导入它们。依赖方向是单向的——一个工具可以伸手进入
`internal/`，`tools/eval` 就是这么做的，但它是把 CLI 当作一个黑盒，通过
运行构建好的二进制文件来度量它。

| 路径 | 二进制程序 | 角色 |
|---|---|---|
| `tools/mk` | — | `./make` 和 `make.bat` 背后的任务运行器，也是 CI 和 GoReleaser 调用的仓库工具 |
| `tools/eval` | `eval` | 评估运行器：把已构建的二进制文件当作黑盒，对照 `evaluation/suite/` 进行度量 |
| `tools/mcp` | — | 一个小型 MCP 服务器，让 MCP 接线在测试和 `/mcp` 中有真实的对象可以连接 |

与 `cmd/` 的这种拆分，正是"不随产品发布"这一事实被写下来的方式，
`go-licenses` 也是这样理解的：它只检查 `./cmd/...`，因为一个没有任何东西
会再分发的依赖不承担任何署名义务。目前无论怎么划定范围都不会遗漏什么——
`tools/` 下的每个第三方模块都已经能从某个已发布的二进制文件触达，而
`tools/mk` 只依赖标准库。

## `internal/`

```text
internal/
├── bootstrap/          进程启动与依赖装配（server、worker、objectstore）
├── config/             YAML + 环境变量的配置加载与路径解析
│
├── core/               纯领域层——不导入 infra
│   ├── appconnect/     connector.yaml 的格式与固定操作校验
│   ├── apierr/         一个 service 为什么拒绝：一个 Kind，传输层据此映射出
│   │                   一个状态码，加上 ErrNotFound——调用者指名的行或
│   │                   对象不存在时，一个 store 会怎么说
│   ├── llm/            LLM 契约（Message、ToolDef、ToolCall、Usage、LLMClient）、
│   │                   Tool 契约、ToolRegistry，以及工具策略
│   ├── jsonschema/     结构化输出与 Workflow 输入 Schema 共用的 JSON Schema
│   │                   子集：子集编译期检查与取值校验
│   ├── hook/           hook 配置的形状、其事件与传输方式
│   ├── mcp/            mcp.json 文档的形状及其校验规则
│   ├── agent/          工具调用循环、事件、hook、沙箱契约
│   ├── plugin/         Plugin manifest、版本算术，以及发现、解析与发布
│   │                   共享的分层词汇，加上目录条目、其发布版本，以及一个
│   │                   space 的激活状态
│   ├── subagent/       subagent 定义文件的形状及其 frontmatter
│   ├── space/           Space、其成员、其 store，以及 HTTP 守卫与 space
│   │                   service 共同强制执行的那唯一一个角色/操作决策
│   ├── artifact/       Artifact：某人选择保留的一个文件、其记录、其
│   │                   store，以及其内容对象如何被寻址
│   ├── llmgateway/     一次部署代理并记录的内容：模型目录条目、调用
│   │                   台账条目，以及它们各自的 store
│   ├── quota/          档位限额，以及一次拒绝据以衡量的用量窗口
│   ├── conversation/   持久化的 Conversation 及其消息：Tier 1 编排并
│   │                   存储的内容，有别于本地 session
│   ├── workflow/       一个 space 可复用的图计划、其修订版本，以及其
│   │                   执行流经的 run 与 node-run 状态
│   ├── agentdef/       一个 space 定义的 Agent 及其修订版本——即一个
│   │                   Agent 被配置成什么样子，而非运行它的那个循环
│   ├── issue/          Issue、其层级结构与 owner/executor 词汇，以及人与
│   │                   agent 留在它上面的评论
│   ├── audit/          仅追加的轨迹：什么算一个事件、哪些操作值得记录，
│   │                   以及它如何被读取与清理
│   ├── secret/         Space Secret：一组具名条目、其生命周期，以及
│   │                   已加密字节与 store 的契约——不含加密实现，不含
│   │                   持久化
│   ├── task/           Tier 2 持久化工作：Task、其 run，以及它们唯一
│   │                   合法的一套状态转移、run 输出与投递
│   ├── identity/       调用者是谁：账户、其凭证、其轮换的 session，以及
│   │                   它持有的部署角色
│   ├── eligibility/    此账户现在能否在此 Space 运行工作：账户未停用且仍是
│   │                   成员这一闸门，供各持久派发路径与 HTTP guard 共用
│   ├── schema/         数据库自陈已发生过什么：infra/db 报告的已应用
│   │                   迁移，以及 admin 路由读取的内容
│   ├── session/        本地 session 模型；持久化实现放在 agentapp 里
│   ├── remotesession/  Remote Control 暴露的、账号归属的活设备会话注册表：
│   │                   presence 及其 store
│   └── localproject/   本地 Project：CLI、TUI 与 Desktop 的 session 为
│                       同一个仓库或目录共享的身份，以及其跨 session
│                       记忆将归属的范围
│
├── agentapp/           Agent 运行时装配：LLM client 缓存、工具注册表、
│   │                   MCP、hook、沙箱、trace、技能、session、工作区
│   ├── job/            本地后台任务：身份、状态、输出、关停
│   ├── worktree/       一个 session 的 Git worktree 生命周期：谁可以
│   │                   进入、谁占用着它，以及它迁移的根目录
│   └── taskrun/        一次 task run，运行在它自己 run 范围的目录内（worker）
│
├── service/            应用服务：协调 store、执行规则
│   ├── conversation/   Portal 前台聊天，以及可选的 Task 编排
│   │   └── channel/    规范化的 turn 类型与 channel 适配器（webhook）
│   ├── agent/          Agent 定义、其修订版本，以及删除防护
│   ├── artifact/       一个 space 保留的持久化文件；不知道生产者是谁
│   ├── llmcatalog/     模型目录接受什么、更改它会记录什么；shell 与
│   │                   admin 路由都调用它
│   ├── systemadmin/    谁持有部署范围的角色；最后一位持有者规则依赖
│   │                   调用者的权限本身来触发，而非一个标志位
│   ├── accountlifecycle/ 编排账户停用/启用及其收尾——session、webhook key、
│   │                   schedule、在途 run——并计算停用影响投影
│   ├── spacerecovery/  仅限"owner 全部停用"时的所有权恢复:把一名已启用成员提升
│   │                   为 owner
│   ├── identity/       什么能证明调用者是谁：验证一个凭证并开启它
│   │                   换来的 session
│   ├── issue/          Issue service
│   ├── task/           Task 与 task_run service
│   ├── workspace/      Task 工作区检查点的跨存储提交：校验一个载荷
│   │                   描述符、确认其字节已持久化，然后记录权威指针
│   ├── workflow/       Workflow 与 workflow-run 编排
│   ├── audit/          记录一次敏感操作已经发生（治理用途，而非
│   │                   诊断用途——见该包自己的文档）
│   ├── plugin/         Marketplace 发布与目录生命周期
│   ├── plugininspect/  对一个 plugin 归档贡献了什么内容的脱敏检查
│   ├── secret/         Space Secret 生命周期：校验条目，通过一个
│   │                   Sealer 封存它们，存储元数据与已封存字节；没有
│   │                   明文读取路径
│   ├── space/           成员关系：谁在一个 space 里、谁可以改变这一点
│   ├── quota/          Space 配额执行
│   └── llmgateway/     模型目录、名称解析、路由与受管调用
│
├── tool/               运行时 agent 工具：Read、Write、Edit、Bash、Glob、
│                       Grep、WebFetch、TodoWrite、NoteWrite、Skill、Task，
│                       以及 MCP 网关，加上 UploadArtifact、Job 相关工具，
│                       以及界面提供时的 Monitor。names.go 是工具名称的
│                       唯一权威来源。
│
├── infra/              外部系统的实现
│   ├── browser/        Go 自持的 headless Chrome/Edge over CDP：可执行文件
│   │                   发现、按会话隔离的 profile 与页面、tool.BrowserController 实现
│   ├── db/             核心仓储接口的 MySQL/GORM 实现
│   ├── objectstore/    本地文件系统与 S3/MinIO 存储：space home、run
│   │                   输出，以及 artifact 内容——三个键空间，一个后端
│   ├── llm/            LLMClient 之于 BuildMax 所讲的线上协议：
│   │                   OpenAI Chat Completions、OpenAI Responses、Anthropic Messages
│   ├── llmwire/        受管推理的带版本号线上契约
│   ├── llmremote/      调用 BuildMax 受管网关的 LLM client
│   ├── mcp/            MCP 协议、client 传输、注册表
│   ├── oidc/           OpenID Connect provider：基于 go-oidc 的带缓存的
│   │                   Discovery 与 JWKS，以及仅限非对称算法的 ID token 校验
│   ├── hook/           Hook 传输方式：command、http、mcp_tool、prompt
│   ├── pluginwire/     私有 plugin Marketplace 的线上契约
│   ├── pluginarchive/  plugin 归档的打包，以及经过加固的解包
│   ├── wsarchive/      Task 工作区检查点载荷：tar.zst.v1 打包，以及
│   │                   经过加固、对抗性校验的解包
│   ├── proc/           本地后台任务的进程监督：进程组派生、有界输出
│   │                   环形缓冲、进程树终止
│   ├── secret/         Space Secret 密码学：条目映射的信封加密，以及
│   │                   包装 DEK 的 KEK 提供方
│   ├── sandbox/        Seatbelt/bwrap 后端、出站代理、违规事件
│   ├── sessionstore/   Session 日志文件后端：JSONL 编解码、单写者锁、
│   │                   尾部修复、抢救式恢复
│   ├── localprojectstore/ 本地 Project 文件后端：bundle、可重建的
│   │                   目录投影，以及写者锁
│   ├── locallaunchpadstore/ 桌面端 Launchpad（快速启动应用）的 JSON 文件后端
│   ├── localschedulehistorystore/ 桌面端定时任务的执行历史（每次触发及其创建的会话）
│   ├── localschedulestore/ 桌面端本地定时任务的 JSON 文件后端
│   ├── localterminalsnapshotstore/ 桌面端终端缓冲快照（重启后恢复）的存储后端
│   ├── trace/          持久化的 run-trace 记录器（有边界、经脱敏的 JSONL）
│   ├── k8s/            Kubernetes worker job 启动器
│   ├── workerclient/   面向 server worker API 的 worker 端 HTTP client
│   ├── runbridge/      每次运行的 Unix socket 反向代理，转发到 worker API，
│   │                   使子进程无需持有 run token 即可访问
│   │                   （docs/design/agent-bridge-cli.md）
│   ├── runrelay/       Remote Control 的出站 agent WebSocket：本地会话拨向 server
│   │                   注册并中继其输出（docs/design/remote-control.md）
│   ├── httpclient/     为其 Go client 解码 server 的错误信封
│   ├── flock/          持有者退出时由操作系统释放的建议性文件锁
│   ├── git/            分支、diff 与 worktree 相关辅助函数
│   └── log/            slog + lumberjack 日志
│
├── interface/          本地面向用户的入口
│   ├── appconnect/     本地 OAuth、令牌存储与固定 HTTP 调用
│   ├── cli/            Cobra CLI、Bubble Tea TUI、打印模式
│   ├── desktop/        Wails 应用桥接
│   ├── slashcmd/       聊天斜杠命令集合的共享唯一事实来源，供 TUI 与
│   │                   Desktop 共同读取
│   ├── pluginmgr/      本地安装、发布与移除 plugin——CLI 与 Desktop
│   │                   都运行的同一套机制
│   ├── auth/           登录 client 与凭证持久化
│   └── client/         面向 BuildMax server API 的 HTTP client
│
├── server/             面向 Portal 与 worker 回调的 HTTP API
│   ├── handlers/       路由处理器
│   │   ├── account/    行为主体账号跨 space 拥有的资源：webhook key
│   │   ├── admin/      部署范围的路由；其 Config 无法触达任何 space
│   │   ├── artifact/   以不透明 ID 寻址的 Artifact；space 来自该记录本身
│   │   ├── auth/       建立一个 session：登录、刷新、登出、密码
│   │   ├── auditexport/  space 与 admin 审计路由共用的 CSV 导出
│   │   ├── llmcallview/  为一行调用账本计价，供 space 与 admin 路由共用
│   │   ├── llmhttp/    通过 HTTP 暴露的受管网关，供 space 与 worker 路由共用
│   │   ├── runterminal/  向任何在关注它的人宣布一次 run 已完成
│   │   ├── space/       一个 space 拥有什么：成员、agent、用量、审计
│   │   ├── work/       Issue、workflow、task、conversation 及其 run
│   │   └── worker/     Worker API；以一个 run token 而非 session 鉴权
│   ├── access/         谁在调用、哪个 space，以及是否被允许
│   ├── authtoken/      为一个 worker 出示的 run token 签名与验证
│   ├── httputil/       共享的请求/响应辅助函数
│   ├── scheduler/      领取待处理的 task run 并派生 worker
│   ├── websocket/      实时连接、stream hub，以及协议本身
│   ├── turnqueue/      跨两条路径序列化一个 conversation 的 turn
│   └── static/         内嵌的 OpenAPI 与 Swagger 资产
│
├── architecture/       架构约束测试（导入边界）
├── e2e/                驱动一个已构建二进制文件的端到端套件
│   └── cli/            CLI 黄金路径：真实二进制文件、临时 home、脚本化模型
├── mock/               仅供测试的内存态 store
├── testsupport/        仅供测试、绝不随产品发布的辅助工具（JWT 签发）
│   └── mockllm/        在三种 LLM 线上协议上给出脚本化的模型回复
└── util/               公共 ID 编解码、带前缀的 ID、工作区路径解析、
    │                   小型字符串与时间辅助函数
    └── secretscan/     识别常见的密钥形态；run trace 据此脱敏发现的
                        内容，project memory 拒绝持久化它
```

## `evaluation/`

评估与资质验证系统。它位于 `internal/` 之外，因为它是贡献者与运维人员
使用的工具，而不是产品代码，也不会被任何产品二进制文件触达。

```text
evaluation/
├── contract/           Task、subject、trial-bundle、grader-result 与
│                       experiment 类型、失败分类，以及 bundle 目录布局
├── adapter/            黑盒执行：让一个已构建的二进制文件跑一次 trial
│                       并收集其证据。CLI adapter 驱动 `buildmax -p`；
│                       worker adapter 对着它自己提供的一个控制平面
│                       派发 `buildmax-worker`
├── grader/             确定性结果、task 自带命令，以及 trace/policy grader
├── runner/             套件加载、预检、重复执行、统计、配对比较，以及
│                       报告。Summarize 把任意一组 bundle 归约成一个
│                       subject 的结果向量，使一次导入的 benchmark 与
│                       一次本地 run 共享同一套算术
├── trace/              在同一套边界之下，为 adapter、trace grader 与
│                       Harbor importer 读取一次 run 的持久化 JSONL trace
├── harbor/             外部 Terminal-Bench 4.0 目标：已钉定版本的 harness、
│                       数据集与 adapter 版本，Harbor 用来在一个 task
│                       容器里运行已构建 CLI 的 Python agent，以及把
│                       一个已完成的 job 归档成 trial bundle 的 importer
└── suite/<task>/       task.json，加上物化进 trial 工作区的 state/，
                        以及从不会被物化的 graders/ 与 oracle/
```

评估系统的 Go 包只使用标准库，因此评估不会给产品的 `go.mod` 增加任何
东西。由于它们位于根模块之内，`./make test`、`vet`、`lint` 和
`govulncheck` 无需第二条流水线就能覆盖它们。

`evaluation/harbor/src/` 是本仓库唯一的 Python 代码，这个例外很窄：
Harbor 的自定义 Agent 边界是一个 Python 类，因此不可能用 Go 为它编写
适配器。它不会被构建，不会随产品发布，不会被任何 Go 包导入，也不属于
任何 `./make check` 范围；Go 核心和 CLI 依然是一个不含 Python 或 Node 的
单一二进制文件。它旁边的 Go 文件锁定了一次结果所依赖的版本，并把 Python
约束在 `evaluation/adapter` 所写入的 trial-home 形状之内。见
[evaluation/harbor/README.md](../../../evaluation/harbor/README.md)。

一个 trial bundle 是一个目录而不是一个文件：它的大部分证据——JSONL
trace、workspace 状态、生成的 artifact——本来就是文件，而把一次失败的
全部证据留在一起，正是一次失败的 trial 欠贡献者的复现路径。Bundle 写在
`.artifacts/evaluation/` 之下，不会被提交。两条路径分别如何运行，以及一个
task 和一个 bundle 各自保存什么，见
[evaluation/README.md](../../../evaluation/README.md)；其余的所有权范围见
[design/evaluation-system.md](../design/评估系统.md)。

## 依赖方向

```text
bootstrap ──▶ interface / server / service / agentapp / infra ──▶ core
```

- `core` 不从 `config`、`infra`、`service`、`server`、`agentapp` 或
  `interface` 导入任何东西。它是纯领域代码。
- `config` 只做环境变量和文件加载；它不导入 infra 的实现。
- `infra` 不从 `bootstrap`、`interface` 或 `server` 导入任何东西。
- `server` 不从 `bootstrap`、`config` 或 `interface` 导入任何东西。
- `service` 不从 `agentapp`、`bootstrap`、`interface` 或 `server` 导入任何
  东西。一个 Service 由某种传输方式抵达，而绝不会反过来伸手去要一种
  传输方式。
- `agentapp` 不从 `bootstrap`、`interface` 或 `server` 导入任何东西。每个
  组装它的界面都位于它之上。
- `gorm.io` 只被 `infra/db` 导入。在这条边界之上，"没有这一行"就是
  `apierr.ErrNotFound`，由 Store 负责转译成它。
- `mock` 和 `testsupport` 只从 `_test.go` 文件中被导入。两者都不能被
  随产品发布的代码触达。
- `internal/tool` 不是纯粹的——它按需导入 infra（MCP、git）。

这些规则由 `internal/architecture` 中的测试强制执行。如果某次变更触发了
其中一条，问题出在那次导入上，而不是那个测试上。

## 前端

| 目录 | 包 | 说明 |
|---|---|---|
| `gui/` | `@buildmax/gui` | 共享的展示型 React 组件与主题。构建产物在 `gui/dist/`。 |
| `portal/` | — | 通过 `"@buildmax/gui": "file:../gui"` 依赖 gui |
| `desktop/frontend/` | — | 通过 `file:../../gui` 依赖 gui |

Portal 和 Desktop 共享的是**部件**，而不是逻辑——数据、鉴权和路由各自
属于各自的应用。两者都运行 React 19。

## 相关文档

- [architecture/](architecture/README.md)——每个子系统做什么
- [CONTRIBUTING.md](../../../CONTRIBUTING.md)——构建、测试与运行
