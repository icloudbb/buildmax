# 代码与提交约定

> **翻译说明：** 本文是[英文原文](../../contribute/conventions.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
> **受众：** 贡献者 · **状态：** 当前有效

那些从代码本身看不出来、但评审会据以拦下拉取请求的项目级规则。分层规则见
[repo-layout.md](repo-layout.md#dependency-direction)，由 `internal/architecture`
中的测试强制执行；文档规则见 [documentation.md](documentation.md)。

## 包拥有能力

> 包按业务能力组织。
> 文件按可读性组织。
> 函数围绕单一职责组织。

包是一条语义上的所有权边界，而不是用来让文件保持简短的文件夹。每个包
拥有一项可以描述为业务能力或某个精确基础设施关切的职责——
`task`、`workflow`、`identity`、`pluginarchive`——绝不是像 `models`、
`services`、`repositories` 或 `interfaces` 这样的仓库级技术分组。

发现所有权问题并不等于获得了在此处修复它的许可。除非某项功能离开这次
重构就无法正确实现，否则不要把包结构调整当成功能变更的附带产物。记录
下这个发现，并单独提出迁移方案。

包的大小从来都不是理由，无论支持哪个方向。唯一的判断标准是：它是否有
一个邻居没有的、会促使它变化的理由。`core/apierr` 不到 200 行却值得拥有
自己的边界，因为一个错误对整个应用意味着什么,会因为其他任何东西都不
共享的原因而变化。当拆分能提升导航体验时拆分文件；只有当变化的理由出现
分歧时才拆分包。

不要以包里装的东西命名包,而应以它拥有什么命名。`common`、`shared`、
`util`、`helpers`、`base`、`misc` 以及 `model`/`models` 命名的是一个容器,
于是没人能说清里面该放什么,结果一切都会漂移进去。跨切面的基础设施是
被允许的,但它需要一个精确的名字和一个狭窄的职责。

`internal/core` 是一个依赖层前缀,而不是包名。其下的每个包都携带一项
能力:`core/agent`、`core/llm`、`core/session`。`internal/util` 正是这条
规则所禁止的样子——一个容纳若干互不相关能力的容器名。它早于这条规则
存在,是已知的技术债,不是可以效仿或继续添加的模式。`internal/core/model`
是另一个例子,现已按领域拆分为一个个独立的包:`core/task`、`core/space`、
`core/issue`、`core/conversation`,以及其余部分。

绝不要通过把不相关的代码搬进一个通用包来解决导入环。导入环是所有权
或依赖方向出错的证据。把最小的共享概念移交给它真正的所有者,或者改为
传递一个 ID 或一个输入契约。

## 每条业务规则只有一个所有者

一次状态迁移、一条校验规则、一个授权决策、一条生命周期约束、重试资格、
一条默认值规则,以及一个错误的含义——每一种都只应有一个权威实现。
HTTP 处理器、CLI 命令、Worker、调度器和 Store 都委托给它,绝不重述它。
Task run 状态是应当效仿的形态:`core/task` 拥有合法的状态迁移,
`infra/db.TransitionTaskRun` 只负责原子地应用其中一次迁移。

在添加上述任何一类规则之前,先在整个仓库中搜索是否已有语义职责相同
的规则,找出如今拥有该概念的包,并在所有权清晰时扩展那个所有者。只有
在确实是一个不同概念时,才编写第二份实现。

这种搜索必须覆盖整个仓库,因为在某个处理器里重新实现的规则,从该处理器
所在包内部是看不见的。这条规则适用于业务规则;不带业务含义的辅助函数
不需要遵循它。

## 重复要先分类,再决定是否清除

在做任何决定之前,先说清你发现的是什么。重复有三种:

- **文本重复**——语法相似,含义无关。通常保留不动。
- **结构重复**——并行的类型、接口、校验器、映射器或错误。有时是一次
  边界转换,有时是意外。
- **知识重复**——同一个业务事实在多处实现。这才是要紧的一种,通常
  应当归于一个所有者。

然后说清这两处之间是什么关系:共享知识、合理的边界转换、有意为之的
局部重复,还是纯属巧合的相似。只有当两处是同一个概念、拥有同一个
所有者、并因同一个原因而变化时,才共享代码。

宁可选择小而明确的局部重复,也不要选择一个具有误导性的共享抽象。两个
只是看起来相似的东西,以后会被强行拆开,而到那时接缝已经长在了错误
的位置。

下面这些配对已经看起来相似,并且是刻意分开的。每一对都有记录说明原因;
它们都不是等待清理的对象。

| 看起来像 | 实际不是 | 原因见 |
|---|---|---|
| Local Job | 持久化的 Task 和 TaskRun | [design/local-background-jobs.md](../design/本地后台任务.md) |
| Local Session | Portal 的 Conversation | [design/surface-positioning.md](../design/界面定位.md) |
| 已配置的模型 | 目录记录、已解析的目标 | [design/llm-gateway.md](../design/LLM网关.md) |
| Artifact | 运行输出、Space home 文件 | [design/unified-artifacts.md](../design/统一工件.md) |
| 领域实体 | `db` 行、wire DTO | [design/entity-identity.md](../design/实体身份.md) |
| Task run 状态 | workflow run 与 Issue 状态 | 三个包,三套词汇 |
| 用户 Session | Worker 运行令牌 | [design/worker-run-token.md](../design/Worker运行令牌.md) |
| Plugin 包存储 | Space 的 Artifact 存储 | [design/plugin-marketplace.md](../design/插件市场.md) |
| 网关协议错误 | `core/apierr` 的拒绝 | [design/llm-gateway.md](../design/LLM网关.md) |

一个小包出现在这里,不是因为它小。`llmwire`、`pluginwire`、`authtoken`、
`plugininspect` 以及处理器的信任边界之所以小,是因为它们变化的理由很
狭窄——这正是本节其余部分所应用的判断标准。

接口应存在于确实需要可替换性的地方,并且应紧邻它的消费者。不要为每个
具体实现都镜像出一个接口,也不要仅仅因为两个方法签名相同,就合并两个
分属不同消费者的接口:`service/task` 和 `service/llmgateway` 各自声明
一个配额检查器是正确的做法,因为准入一个 Task 和准入一次模型调用可能
会分道扬镳。

边界模型在保护真实的传输、领域、持久化或外部 API 边界时,值得付出映射
成本。一个领域实体、一个 `db` 行和一个 wire DTO 是三种不同的东西,并且
应当一直保持不同。一条由 request、input、params、command、payload 和
领域类型组成、彼此复制相同字段却不强制任何约束的链条,不是一个边界。

不要为了减少行数而添加基础 Service、通用 Repository、万能映射器或
辅助框架。更少的行数不是目标。

## 所有权变更要一次性搬动每一个调用方

当一次变更确立或纠正了一个规范所有者时:

1. 找出每一个实现和调用点。
2. 决定规范所有者。
3. 如上所述对重复进行分类。
4. 为不应改变的行为建立测试保护。
5. **在同一次变更中**迁移该概念**及其每一个调用方**。
6. 在同一次变更中删除被取代的实现。
7. 运行 `./make fmt`、`./make lint`、`./make test`,以及相关的
   `./make check` 范围。

第 5 步没有商量余地。本项目处于 Alpha 阶段,不欠任何兼容性窗口,因此没
有理由让同一个事实的两份定义并存,而且 `TestNoInternalTypeAliases` 已经
拒绝了常见的兼容垫片写法。渐进式推进应当发生在变更*之间*——一次拉取
请求对应一项能力——而绝不应发生在一次变更*内部*。

在汇报架构性工作时,要说明现在哪个包拥有每个受影响的概念、清除了哪些
重复的知识、哪些相似的代码被有意保持分离及其原因,以及行为是如何被
验证的。

目标不是包的数量最多,也不是重复的行数最少。目标是一个仓库,让人能轻易
找到某个概念的所有者,并且每条业务规则只有一个权威实现。除了上面提到
的架构测试,其余由评审来把关这些规则;每一条都是一次判断,所以应在拉取
请求中论证它,而不是机械地套用它。

## 持久化数据使用 snake_case

本项目写入磁盘的一切——会话文件、配置、任意 JSON——都使用同一种命名
风格。

- JSON 对象的键使用 **snake_case**:`created_at`、`tool_call_id`、
  `tool_calls`。
- 为每个会被序列化到磁盘的结构体显式添加 `json:"snake_case"` 标签。
  不要依赖 Go 的默认行为,那会使用 Go 的字段名。

## 时刻使用 `time.Time`、`DATETIME(6)`、RFC 3339

一个被持久化的时间点,在 Go 中是 `time.Time`,在 MySQL 中是
`DATETIME(6)` 列,在传输协议上是 RFC 3339 字符串。可选表示为
`*time.Time` 和一个可为 `NULL` 的列;缺失绝不用哨兵零值表示。写入时使用
`time.Now().UTC()`。

时长、配额、重试次数和 token 计数不是时刻:它们保持为
`int64` / `bigint` / JSON 数字,字段名携带单位——`TimeoutSeconds`、
`duration_ms`。没有有意义的一天中具体时刻的值,则是 `DATE` 和
`"2026-08-23"`。

其推理过程,以及这条规则所取代的旧做法,见
[../design/timestamp-representation.md](../design/时间戳表示.md)。

## 数据库表名使用单数

每种实体类型对应一张表,以单数命名:`user`、`agent`、
`conversation`、`task`、`task_run`。绝不使用 `users` 或 `tasks`。这适用于
本项目创建或迁移的每一张表。

## 实体 ID 是不透明的公开句柄

一个 Server 实体拥有两个标识符,各自只承担一个角色。`id` 是一个
`bigint unsigned` 主键,是 MySQL 内部的关系键,绝不离开
`internal/infra/db`。`public_id` 是每个边界都能看到的句柄:96 位
密码学随机数据,写成 20 个小写 base32 字符,并以同样的文本形式存储
(`char(20) ascii_bin`),因此直接查询数据库读到的就是每个其他边界所
展示的那个句柄。

```text
ivyoh5qcfu6ypfkhyedq
```

用 `util.NewPublicID` 生成一个句柄,它返回错误而不是 panic;用
`util.CanonicalPublicID` 校验一个句柄,它接受任意大小写,并拒绝任何非
规范写法。类型前缀已经不存在了:路由、JSON 字段和列本身已经说明了类型,
没有任何逻辑再根据前缀分派。

不是每一行都配得上一个句柄。一个 join 行、一个修订版本和一个目录发布,
都改为用其父级加上一个自然键来寻址。哪些表拥有句柄、哪些引用变成数字、
哪些保持为不透明字符串,这些决定记录在
[../design/entity-identity.md](../design/实体身份.md)中;新增表之前请先
阅读它。

在 JSON 中,一个资源把自己的句柄命名为 `id`,并为各种关系保留语义化的
名字——`{"id": ..., "space_id": ..., "conversation_id": ...}`。对行排序时
以 `created_at` 为主、以行键为平局判定,绝不使用公开句柄排序:微秒级
时间戳缩小了冲突概率,但没有消除它,而只比较时间戳的分页边界仍可能
跳过或重复某一行。

标识符格式只有一种。登录链、trace 文件和 Desktop Project 都像任何实体
一样不带前缀地使用它。唯一的例外是 Local Job,它写作 `jb_<public id>`:
它的 ID 会作为工具输出中的裸字符串抵达模型,而自由文本正是类型前缀能
说出周围上下文说不出的信息的唯一场合。Agent Session ID 是 UUID,因为
它们命名的是一个文件,而不是这套编码方案所标识的任何东西。

## 工具输出是写给 LLM 看的

一个工具的返回值会作为一条 tool 角色消息回传给模型。它不是日志行,
也不是面向用户的字符串。

- **成功要说清楚。** 说明做了什么或发现了什么,简洁到配得上它花费的
  token。
- **失败要说得有用。** `path outside allowed root` 和 `file not found`
  能让模型决定是重试、调整,还是告知用户。单独一个 `error` 做不到这点。
  Agent 在把工具错误传给模型的路上,会为其加上 `error:` 前缀。

## 日志是写给排障用的

一条规则决定日志级别,这样一个阈值就能筛出有意义的内容:

| 级别 | 含义 |
|---|---|
| `Error` | 一个用户任务单元失败或丢失了 |
| `Warn` | 已降级,但任务完成了——包括忽略了用户配置项的 fail-open 路径 |
| `Info` | 生命周期和状态迁移 |
| `Debug` | 逐条细节 |

在本项目中,`Warn` 数量多于 `Error` 是预期行为,而不是异味:hook、trace、
沙箱和 Skill 加载都是按设计 fail-open 的,每一种都是真正的"降级但已
完成"。

身份信息放在属性里,绝不放进消息文本。一个子系统只设置一次
`component`——`slog.With("component", "scheduler")`——消息本身只描述
事件本身。在使用它的位置构建这个 logger,而不是放进包级变量,因为包级
变量会在 `infra/log` 于启动时安装真正的 handler 之前,就捕获了
`slog.Default()`。

属性的键使用 `snake_case`,错误统一叫 `"err"`。

关联信息沿 context 传递。`infra/log.With(ctx, ...)` 添加的属性,会被
handler 附加到每一条用该 context 记录的日志上,因此调用点只需使用标准库
的 `slog.*Context` 函数,不需要额外导入任何东西。正是这层间接,才让
`internal/core` 能在不导入 infra 的情况下携带一次运行的标识符。Server
为每个请求打上 `request_id` 并通过 `X-Request-Id` 返回它;一个 Worker 会
在默认 logger 上打上 `component` 和 `task_run_id`,因为它只执行恰好一次
Task run。

绝不记录凭据,也绝不记录 provider 的原始错误正文——它可能携带账户
标识符和请求片段。

## 颜色来自主题 token

在 `gui`、`portal` 和 `desktop/frontend` 中,`gui/src/theme.css` 是唯一允许
出现颜色字面量的样式表。其他规则的颜色、阴影和遮罩层都取自其中的语义 token,
例如 `--color-danger`、`--color-warning`、`--shadow-popover` 或
`--color-overlay`,这样深色主题无需逐页覆盖即可生效。浅色调用 `color-mix()`
基于 token 生成,而不是写一个新的字面量。当没有现成 token 表达规则所需的含义时,
在 `theme.css` 中为两个主题同时新增一个;不要为已有 token 添加同义词。

`var()` 必须引用某个样式表定义过的自定义属性。未定义且没有回退值时,整条声明
会静默失效;带回退值时,它会藏起一个字面量。

Stylelint 在每个包的 `npm run lint` 中强制执行这两条规则,`./make check
gui|portal|desktop` 会运行它。共享配置和未定义属性规则位于
`gui/stylelint/tokens.mjs`。`desktop/frontend/src/lib/terminalTheme.js` 中的
xterm 配色是交给 xterm 的 JavaScript,是唯一的具名例外。

## 提交信息与拉取请求

仓库的公开记录承载项目内容：变更、理由、验证，以及对实质性协作负责的
人。重复 assistant、模型或工具署名只会产生噪声，而且 trailer 无法提供
复现或审计 Agent 运行所需的结构化来源信息。

- 提交主题是**单行祈使句**——`Move the Dockerfiles into
  deployment/docker`,而不是 `moved dockerfiles` 或 `fix stuff`。当原因
  从 diff 中看不出来时,补充一段正文。
- **拉取请求以 merge commit 合并,因此分支上的每一次提交都会落到
  `main` 上。** 每个提交主题都遵循上面那条规则,而不只是拉取请求标题:
  `Fix the worker artifact path`——不是 `Bug fix`,不是 `WIP`,也不是单独
  一个 Issue 编号。把分支组织成你想留在历史中的那些提交,并在合并之前
  重写它,而不是追加一个 `fix review comments` 提交——那个提交会永久
  留在历史中。
- **拉取请求标题会成为 merge commit 的主题。** 它是历史对这次变更整体
  的呈现,所以要把它写成一行祈使句,脱离下面的分支也能读懂。
- **标题带有 [Conventional Commits](https://www.conventionalcommits.org/)
  类型前缀**,这样 `main` 上的 merge commit 能一眼分类:
  `feat: Add the black-box worker trial adapter`、`docs: Document AI-first
  contribution practices`、`fix: Derive the Compose stack's cors_origin
  from its Portal port`。类型有 `feat`、`fix`、`docs`、`refactor`、
  `perf`、`test`、`build`、`ci`、`chore` 和 `revert`。当能有效收窄标题
  时加上作用域——`feat(portal): …`——当变更破坏现有公开行为时在冒号前
  加 `!`——`refactor(server)!: …`。
- **前缀只出现在标题上。** 分支上的提交主题保持为朴素的祈使句,没有
  任何东西会解析这个前缀:changelog 条目是 `docs/changelog/` 下手写的
  文件,发布版本号是选定的,不是推导出来的。这个前缀只是给读
  `git log --oneline` 的人看的标签。
- 让分支保持为**一次连贯的变更**。merge commit 是回退操作的目标,所以
  一个做了三件不相关事情的拉取请求,以后无法单独回退其中一件。两件
  变更,就开两个拉取请求。
- `Co-authored-by` 可以用于标明对变更有实质贡献的人。**不要**用它标记
  assistant、模型、提供商或工具。
- **不要**在提交中添加 `Claude-Session` 或其他工具 trailer。
- **不要**在拉取请求描述中添加“Generated with …”footer 或 assistant
  session 链接。如果评审确实需要了解本次任务特有的 AI 背景，请在 pull
  request 的普通正文中说明。
- 拉取请求描述的形式请遵循
  [`.github/pull_request_template.md`](../../../.github/pull_request_template.md)。
- 为任何用户或运维人员会注意到的变化添加 changelog 条目:新增或变更的
  行为、新增配置、移除项,以及对已发布行为的修复。内部重构、仅测试的
  改动和文档编辑不需要。
- **一条条目就是一个新文件**,`docs/changelog/<category>/<slug>.md`,
  里面存放它最终会成为的那一条 Markdown 列表项。各自新增文件的分支
  永远不会冲突;而各自往 `CHANGELOG.md` 的 `## [Unreleased]` 里追加一行
  的分支,过去每次都会冲突。`./make changelog` 会预览它们最终会并入
  的章节;完整规则见 [`docs/changelog/README.md`](../changelog/README.md)。

## 相关文档

- [CONTRIBUTING.md](../../../CONTRIBUTING.md)——前置条件、构建与测试、
  拉取请求
- [repo-layout.md](repo-layout.md)——目录树与依赖方向
- [documentation.md](documentation.md)——行为变化时应更新什么
