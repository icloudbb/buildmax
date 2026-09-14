# 文档约定

> **翻译说明：** 本文是[英文原文](../../contribute/documentation.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
> **读者：** 贡献者 · **状态：** 当前有效

## 按读者组织，而非按产物组织

**最终用户手册是 [`manual/`](../../../manual)**：按任务组织，每项能力一页，随 Portal 镜像交付，并在应用内的 **Help** 下提供。从快速入门、核心概念到每个 CLI 命令，安装、运行和使用 BuildMax 所需的一切都在这里；[`manual/manifest.json`](../../../manual/manifest.json) 是其目录。

英文是源文档。[`manual/zh/`](../../../manual/zh) 是简体中文镜像，每个英文页面对应一页，并有自己的 `manifest.json`；Help 页面提供 EN / 中文切换。中文文件使用中文名称（`沙箱.md`），因此每个 manifest 条目保留英文 `slug` 作为稳定 URL 键，并增加 `file` 指定磁盘上的文件。两种语言必须同步：修改英文页面时，在同一个拉取请求中更新 `zh/` 对应页面。

`docs/` 保存其余内容，按读者想回答的问题划分：

| 目录 | 读者 | 内容 |
|---|---|---|
| `deploy/` | 为 Space 运行系统的人 | 拓扑、认证、本地集群 |
| `reference/` | 查阅资料的人 | 配置、webhook，以表格而非叙述为主 |
| `contribute/` | 修改代码的人 | 布局、架构、本约定 |
| `design/` | 追问“为什么这样设计”的人 | 按 domain 浏览、以生命周期标记的语义化设计记录 |
| `proposals/` | 评估潜在未来方向的人 | 尚未承诺实施、跨领域的探索性文章 |

[`docs/index.md`](../../index.md) 是方便紧凑浏览和手机查看的完整双语文件目录。
`docs/README.md` 继续作为按任务组织的入口，不在其中重复完整清单。

判断文档归属的标准是**没有它谁会受阻**，而不是文档类型。

## 提案

`proposals/` 保存那些影响重大、在成为路线图承诺前需要书面比较的想法。它不是第二份路线图、Issue 跟踪器或归档。

提案必须：

- 将状态标为 `proposal — under discussion`；
- 用 `Opened: YYYY-MM-DD` 记录讨论开始日期；
- 选择贡献者查找该问题时最可能进入的 primary domain；
- 链接可能受影响的当前路线图计划和设计记录；
- 区分目标、非目标、选项、开放问题和决策所需证据；
- 避免把行为描述成已经交付。

多 Agent 圆桌应是一个提案目录，而非一篇文章。其 `README.md` 负责问题、决策边界、贡献索引和证据标准。每位参与者添加一篇明确署名的 `<agent-name>-view.md`，不得重写其他参与者的立场。只有圆桌 README 出现在 `proposals/README.md` 中；综合意见获采纳后，仍应退役该目录，将长期有效的理由移入设计记录。

决策尚未确定时保留提案。采纳后，将承诺的优先级放入 `ROADMAP.md`，把长期理由移入 `design/`，并按需创建实现 Issue，然后删除提案。被拒绝或替代的提案也应删除；git 历史会保留讨论。
当前索引只列开放提案；被采纳的理由属于设计记录，已退役的提案仍可通过 Git 历史查阅。

## 设计文档

使用稳定、语义化的文件名，例如 `sandbox-boundaries.md`。文件名描述主题，不描述时间顺序、路线图优先级或生命周期，使代码注释和其他文档引用时不必承接规划元数据：

```go
// Mirrors the design in docs/design/sandbox-boundaries.md.
```

`design/README.md` 按贡献者最可能从哪里查找记录的 primary domain 分组：

- **产品与执行模型**；
- **Agent 运行时与模型**；
- **本地体验**；
- **Space 平台**；
- **信任与安全**；
- **运维与部署**；
- **验证**。

Primary domain 用于发现文档，不表示排他的所有权。跨越边界的记录仍只出现在一个
domain 表格中，并在自身正文中说明相关 domain。只有当贡献者确实需要一个现有
domain 无法表达、长期稳定的独立入口时才增加 domain；不要只为了分类一篇记录而
创建目录。

每篇记录还具有一种生命周期：

- **方向**：跨越路线图阶段的长期决策。
- **活动计划**：属于某项 `ROADMAP.md` 优先级的计划或部分实现工作；交付或改变方向后即失效。
- **规范**：记录已实现或部分实现子系统设计的长期文档，需保持最新。

路线图优先级和详细实现状态保留在 `ROADMAP.md` 与各篇记录中。索引只保留生命周期、
简明进度标签与范围说明，使读者不必逐篇打开文档就能区分已交付、部分完成、未开始
和仅决策的记录，同时避免索引变成第二份路线图。不要在索引中加入百分比或逐切片
细节；应链接到负责该细节的设计记录。

设计文档是**设计理由，不是用户文档**。设计交付用户可配置功能时，面向用户的部分应进入 `manual/` 手册（查阅表格则进入 `reference/`）；设计文档链接到它，并保留取舍与开放缺口。

## 语言与翻译

英文是仓库文档的权威语言，也是产品与架构决策的唯一事实来源。简体中文设计记录作为派生镜像维护，让中文读者能够审阅相同理由，而不形成第二条决策流。

镜像采用固定形式：

- `docs/design/` 下每个 Markdown 文件，包括索引和未来子目录，都在 `docs/zh-CN/design/` 相同相对路径下有且仅有一个对应文件；
- 每篇英文设计记录在标题后立即链接中文对应页面；
- 每篇中文记录回链到英文源文件；
- 中文通知明确说明译文为派生内容，并声明两者不一致时以英文为准。

保持镜像最新是评审责任，而不是一道自动化门禁。不得仅编辑中文记录来解决分歧。先修正英文源文件，再在同一变更中同步译文。

完整翻译正文、标题、表格、链接文本和目录。保留代码、命令、标识符、路径、URL、schema 和配置键、路由模式，以及通过大小写标识产品概念的 BuildMax 领域名称。尤其保留 `Agent`、`Task`、`TaskRun`、`Space`、`Issue`、`Workflow`、`Run`、`CLI`、`TUI`、`Portal`、`Desktop`、`Project` 和 `Artifact` 的英文形式。中文正文可以围绕这些名称解释概念，但不得用新领域术语替换它们。中文设计记录之间的链接留在中文镜像内；指向镜像树之外文档的链接继续指向权威英文页面。

英文设计变更的作者负责同步中文对应文件。评审者按决策风险检查覆盖率与语义忠实度。其他文档保持英文，除非其目录在本节获得明确的镜像政策。

`docs/contribute/exploratory-runs/` 即获得该政策：每份报告与索引都在相同相对路径下有一个 zh-CN 对应文件，英文权威，并带上面所述的链接与派生译文通知。其报告是短暂的暂存产物（见该目录的 README），因此报告与其镜像一同提交，并一同被转化或丢弃。

## 退役文档

不设归档目录。不再描述当前方向的文档应**删除**：git 历史会保留它，而树中陈旧文档带来的成本超过其历史价值。恢复方法：

```bash
git log --diff-filter=D --oneline -- docs/
git show <commit>^:docs/path/to/file.md
```

若退役文档中仍有真实且必要的内容，先对照代码验证，将其移入 `manual/` 手册或 `reference/`，再删除原文。

## 文档头部

每篇文档开头标明读者和状态：

```markdown
> **Audience:** operators · **Status:** current
```

`Status` 可以是 `current`、`planned`，或明确限定语，如 `current — this describes a known gap`。读者应在一行内判断文档是否可信。

## 目录列表

`proposals/` 和 `design/` 下每篇文档都以 `## Contents` 列表开篇，置于头部和相关文档链接之后、首个章节之前：

```markdown
## Contents

- [1. Decision](#1-decision)
- [2. Why These Concepts Must Stay Separate](#2-why-these-concepts-must-stay-separate)
```

按文档顺序，每个 `##` 章节一行。不列子章节：长到需要滚动的列表已经失去概览作用。

这些文档往往长达数百行，读者通常需要先判断某节是否与自己有关。没有目录就必须滚动整篇文件才能判断，因此这里要求目录，而不只是鼓励。

两个 `README.md` 索引文件豁免，它们本身就是链接列表。`manual/` 手册和 `reference/` 页面也豁免：它们面向任务，目录会干扰任务而非服务于任务。

随文档维护目录。重命名或添加章节却不更新条目，比没有目录更糟，因为读者会信任已有目录。

## 单一事实来源

在两篇文档中重复同一事实，终将使其中一处出错。

| 事实 | 所在位置 | 其他位置 |
|---|---|---|
| 仓库目录树 | [repo-layout.md](repo-layout.md) | 链接 |
| 环境变量 | `internal/config/env_spec.go` → [reference/configuration.md](../reference/configuration.md) | 链接 |
| 配置文件字段 | `config-examples/*.example.yaml` → [reference/configuration.md](../reference/configuration.md) | 链接 |
| HTTP 路由 | 各 handler 子包的 `Register` 方法 → `/openapi.json` | 链接 |
| 路线图优先级 | [ROADMAP.md](../ROADMAP.md) | 链接 |

## 自动强制检查的内容

`internal/architecture/docs_test.go` 随普通测试套件运行，以下文档悄然腐化的情况会使构建失败：

| 测试 | 失败条件 |
|---|---|
| `TestDocsLinksResolve` | 相对 Markdown 链接指向不存在的文件 |
| `TestDocsIndexCoversEveryDocument` | `docs/` 下的 Markdown 文件未出现在[单页总览](../../index.md)中 |
| `TestEnvVarsDocumented` | `config.EnvVars()` 新增的变量未出现在 [reference/configuration.md](../reference/configuration.md) |
| `TestToolNamesDocumented` | 工具名称常量未出现在 [manual/tools.md](../../../manual/tools.md) |
| `TestArchitectureToolInventoryCoversEveryToolNameConstant` | `internal/tool/names.go` 声明的工具未出现在贡献者[工具清单](architecture/tools.md) |
| `TestAgentsMDPathsExist` / `TestAgentsMDRoutesExist` | [AGENTS.md](../../../AGENTS.md) 引用了不存在的路径或路由 |
| `TestDocumentedFilePathsExist` | 任一文档引用了不存在的仓库文件 |
| `TestDocumentedMakeCommandsExist` | 任一文档提及任务运行器无法分发的 `./make` 命令 |
| `TestCLIReferenceCoversEveryCommand` | 二进制中存在某命令，但 [manual/cli.md](../../../manual/cli.md) 未记录 |

工具名称检查存在的原因是：这些字符串是用户可见契约，会出现在 hook 的 `matcher` 正则和 subagent 的 `tools:` 字段中。重命名工具却不更新文档会悄悄破坏可用配置。架构检查从 `names.go` 读取声明，避免手工维护的测试列表遗漏新增的界面限定工具。

最后三项检查维护一份当前发现的偏差短名单，每项关联一个开放 Issue。修复偏差就删除条目；条目不再报告问题时也会使测试失败，因此列表会随文档修复而缩短，而不会遗留。设计记录豁免：它们记录当时的计划，与当前代码冲突时以代码为准。

其余均为约定，由评审维护。

## 随代码更新文档

| 变更 | 更新内容 |
|---|---|
| 包边界或运行时契约 | 同一拉取请求中更新 [architecture/](architecture/README.md) 对应文档 |
| 用户可见行为或配置 | `manual/` 手册、`reference/` 和 `config-examples/` |
| 方向 | 在 [../design/](../design/设计文档索引.md) 添加或更新语义化记录 |
| 包移动 | 仅更新 [repo-layout.md](repo-layout.md) |

## 风格

- 权威文档用英文编写；简体中文设计记录遵循上述镜像政策。
- 以仓库相对路径引用文档，确保链接在移动后仍有效。
- 查阅型内容优先使用表格，而非项目符号列表。
- 明确说明缺口。文档悄悄略去尚未工作的部分，比没有文档更糟；应写明“默认关闭”“尚未接入”“仅用于开发”。
