# 测试

> **翻译说明：** 本文是[英文原文](../../contribute/testing.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
> **受众：** 贡献者与修改代码的 Agent · **状态：** 当前有效

一次变更之后应该运行什么、它需要什么，以及失败时该去哪里查看。这种
拆分背后的理由——为什么端到端套件是本地反馈循环，而不是拉取请求的
门禁——见 [../design/end-to-end-testing.md](../design/端到端测试.md)。

如需自主操作用户旅程、根据观察推进评审，请使用 [Agent 探索性测试](exploratory-testing.md)。其中说明如何在操作产品时选择分支、判断发现、保留证据，并将发现沉淀为回归测试。它与下方必需的套件配合使用，不提供全面通过的结论，也不替代脚本化验证。
探索旅程可以在任务范围和预算内使用已配置的真实模型，包括付费推理；按该指导记录模型及用量证据。

## 简要版本

```bash
./make test           # everything below the deployment: unit, integration, CLI, Desktop bridge
./make test ./internal/tool -run TestX   # one package or one test, same isolated home
./make test mysql     # the store scope, against a real MySQL you point it at
./make e2e cli        # just the CLI and TUI suite
./make e2e desktop    # just the Desktop bridge suite
./make e2e desktop-ui # desktop/frontend through `wails dev`'s browser bridge
./make e2e local      # Portal in a browser, against a Compose stack this command owns
./make e2e all        # cli, desktop, then local — the release-time matrix
./make kind up        # build the local cluster and verify a Kubernetes worker run
./make kind smoke     # rerun the deployment flow without rebuilding
./make e2e kind       # run the Portal browser suite against that cluster
```

`./make test` 是日常循环。CLI 和 Desktop 套件之所以内嵌其中，是因为它们
只需几秒钟、除了一个临时目录之外什么都不需要；一个需要你记得去运行的
套件，就是一个终将不再被运行的套件。

用 `./make test` 来收窄范围，而不是裸的 `go test`。只有任务运行器会设置
`BUILDMAX_HOME`，而一个触及真实 `~/.buildmax` 的测试会读到贡献者本人的
Session、设置和凭据——于是它的结果取决于运行它的机器是谁的。
`config.DataDir` 遇到这种情况会 panic 而不是回退，任何代码会读取这些
路径的包，都会给自己一个调用 `testsupport.RunWithIsolatedHome` 的
`TestMain`。

## 你改了什么，就运行对应的套件

| 你改动了 | 运行 |
|---|---|
| `internal/`、`cmd/`、`tools/` 中的任何东西 | `./make test` |
| `internal/infra/db` 中的一个行结构体、一个 Store 方法，或一条查询 | `./make test mysql`——单独的 `./make test` 会跳过其中每一个测试 |
| Agent 循环、工具、权限、Session、TUI | `./make test`，然后 `./make e2e cli` |
| Plugin、打包，或 Marketplace 路由 | `./make test`，然后 `./make e2e cli` |
| Desktop bridge 及其事件、审批,或 Session 历史 | `./make e2e desktop` |
| desktop/frontend 的 React 应用，或它调用某个绑定 Go 方法的方式 | `./make e2e desktop-ui` |
| Wails 配置、desktop 资源内嵌,或该应用的打包方式 | `./make build desktop`——没有别的命令能构建打包好的应用，而 `go build ./...` 只会编译 `!desktop` 桩代码 |
| `gui/` 中的一个共享组件 | `./make check gui` |
| Portal 的展示层或一个共享组件 | `./make check portal` 或 `./make check gui`，再用 `./make e2e local` 作快速的浏览器循环 |
| Portal 的行为,或 Portal 调用的某个 Server 路由 | 使用下面的本地 kind 循环；以 `./make e2e kind` 收尾 |
| Server 处理器、Service、认证、Conversation,或 Task 派发 | 使用下面的本地 kind 循环；运行 `./make kind smoke`，若行为在浏览器中可见则再加上 `./make e2e kind` |
| Worker、调度器、存储,或模型网关 | `./make kind up`，然后 `./make kind smoke`；网关路径用 `./make kind smoke managed` |
| 部署清单、Dockerfile、ingress,或 Kubernetes 的 Worker 路径 | `./make kind up`，然后 `./make e2e kind` |
| 文档 | `./make check docs` |

从最窄的套件开始以获得快速反馈,然后运行被改动的边界所要求的更广泛
证据。当结论依赖同源 ingress、已部署的配置、真实的后端服务,或
Kubernetes Worker 执行时,单元测试或 Compose 检查都不能替代 kind。

## 面向 Portal 和 Server 的 kind 循环

对于实质性的 Portal 和 Server 工作,本地 kind 集群是首选的端到端环境。
它把构建好的 Portal 和 Server 镜像放在同一个 ingress 之后,使用真实的
MySQL 和 MinIO,并在 Kubernetes 的 Worker Job 中执行 TaskRun。这些是
组件测试和更快的 Compose 循环无法一并证明的产品边界。

```bash
./make kind up              # create or update the cluster, then verify a real worker run
./make kind reload portal   # rebuild and restart only Portal during the edit loop
./make kind reload server   # rebuild and restart only the server during the edit loop
./make kind smoke           # rerun the deterministic API-to-worker deployment flow
./make kind smoke managed   # also prove gateway inference without a worker credential
./make e2e kind             # run the Portal browser suite through the shared ingress
```

当集群不存在时先运行 `kind up`。做完一次仅涉及 Portal 的编辑后,重新
加载 Portal 并运行 `e2e kind`。做完一次 Server 编辑后,重新加载 Server 并
运行 `kind smoke`;如果 Portal 消费了被改动的行为,再加上 `e2e kind`。
当一次临时的浏览器检查需要预先填充的业务视图时,配合使用 `kind fixtures`
和 `kind login`。对清单、Portal/Server 之外的镜像,或 Worker Job 路径的
改动需要 `kind up`,因为 `kind reload` 只针对那两个长期运行的 Deployment。

kind 命令会创建或修改本地基础设施,并被有意排除在自动化的拉取请求检查
之外。这让它们成为一种按比例投入的选择,而不是可有可无的选择:当一次
Portal 或 Server 的变更提出了端到端的主张时,作者有责任产出本地集群的
证据。

## 每个套件需要什么,以及要花多久

| 套件 | 需要 | 通常耗时 | 拥有 |
|---|---|---|---|
| `./make e2e cli` | Go | 60 秒以内 | 一个临时 `BUILDMAX_HOME`、一个 workspace,以及一个它在进程内启动的 Marketplace server |
| `./make e2e desktop` | Go | 60 秒以内 | 同上 |
| `./make e2e desktop-ui` | Go、Node、Chromium | 60 秒以内 | 一个 `wails dev` 进程,以及一个全新、用完即弃的 `BUILDMAX_HOME` |
| `./make e2e local` | Docker、Node、Chromium | 10 分钟以内 | 一个它自己启动并停止的 Compose 技术栈 |
| `./make e2e compose` | 一个已经在运行的 Compose 技术栈 | 2 分钟以内 | 什么都不拥有——它只是访客 |
| `./make e2e kind` | 一个已经在运行的 kind 集群 | 2 分钟以内 | 什么都不拥有——它只是访客 |
| `./make compose smoke` | Docker | 5 分钟以内 | 一个它留下来继续运行的 Compose 技术栈 |
| `./make compose upgrade-drill` | Docker、来源发布版本的镜像 | 10 分钟以内 | 一个它启动并删除的 Compose 项目，以及它构建的候选镜像 |
| `./make kind up` | Docker、kubectl | 20 分钟以内 | 一个它留下来继续运行的集群 |

没有一个套件需要 provider 的 API key。它们全部都由
`internal/testsupport/mockllm` 回答模型请求,该模块会回放一个已提交的
场景。

`./make e2e desktop-ui` 是一个固定的、脚本化的检查——如果想临时摆弄
desktop/frontend 的界面(点一遍某个流程、给某个视图截图、看看某个已
绑定的 Go 方法返回了什么,而在此之前你还不知道要断言什么),请启动
`./make run desktop-dev`,并用
[`.buildmax/skills/drive-desktop/`](../../../.buildmax/skills/drive-desktop/SKILL.md)
来驱动它。

## Store 范围

`internal/infra/db` 中的每个测试,在 `BUILDMAX_TEST_DSN` 未设置时都会
自我跳过,因此一次绿色的 `./make test` 对 schema、查询、事务或
MySQL 专属行为什么都说明不了。`./make test mysql` 才是能说明这些的
范围:

```bash
docker run --rm -d --name buildmax-test-mysql \
  -e MYSQL_ROOT_PASSWORD=buildmax -e MYSQL_DATABASE=buildmax \
  -p 3306:3306 mysql:8.0
export BUILDMAX_TEST_DSN='root:buildmax@tcp(127.0.0.1:3306)/buildmax'
./make test mysql
./make test mysql -run TestCreateSpace    # `go test` flags pass through
```

这个账号需要 `CREATE DATABASE` 权限:该范围会在一个唯一命名的数据库上
运行,由它创建并丢弃,因此它绝不会写入你的 DSN 所指向的那个数据库。
它在没有 DSN 时会拒绝运行,而不是跳过;如果该范围内的某个测试仍然因为
DSN 缺失而跳过,它也会失败——它存在正是为了解决"一个门禁可以在什么
都没测的情况下变绿"这个问题。

它绝不会替你启动 Docker。把它指向一个你已经在运行的 server;一个会
把启动容器当作副作用的测试命令,就是一个会改动这台机器的命令。

CI 会在每个拉取请求上,对照一个锁定版本的 `mysql:8.0` 服务容器运行
这条相同的命令。`BUILDMAX_TEST_DSN` 已设置时,`./make check ci` 也会
运行它;该变量不存在时,它会说明自己没有运行。设计记录见
[../design/verification-program.md](../design/验证计划.md) §4。

`./make agent-smoke` 是例外,而且它不是一个测试:它用一个真实模型驱动
Agent 的工具,需要一个 key,并报告一张由模型自己撰写、关于自己的
PASS/FAIL 表格。请阅读它的输出;它的退出码只能说明进程结束了。

`./make cache-qualify` 是第二个例外,原因相同,但更加尖锐。树上的每一个
缓存测试都针对一个伪造的上游运行,这只能证明 BuildMax 发送了什么,
而对 provider 拿这些内容做了什么毫无证明——一个请求可以形态完美无缺,
而 provider 依然拒绝缓存它,原因可能是最小前缀长度、不受支持的模型,
或已过期的保留窗口。该套件会针对一个由 `BUILDMAX_CACHE_QUALIFY_*`
指定的真实 provider,运行
[prompt-cache-control.md](../design/提示缓存控制.md) 所把关的那些场景,
在通过之前,没有任何 provider 或网关会被描述为具备缓存能力。未设置时,
它会像普通 `./make test` 下的 Store 测试一样跳过——但它没有自己专属的、
拒绝这样做的范围,因为与 MySQL 不同,一个付费 provider 没有免费的本地
替代品。

`./make eval` 是第三个例外,它度量的是那些套件刻意不度量的东西。它默认
构建 CLI,并把 `evaluation/suite/` 中的 CLI 任务当作黑盒来评估;
`--surface worker` 会构建 Worker 并选取它的任务,`--surface all` 会同时
运行两个界面。每一种都使用真实模型、重复的 trial、读取最终 workspace
与本次运行 trace 的 grader,以及一份带不确定性的通过率报告。
这度量的是 Agent 的质量,而不是边界的验证——上面的套件证明某个行为已经
接好了线,而评估问的是一个模型驱动它的可靠程度如何。它需要一个 key 并
消耗 token。凡是不需要 key 就能检查的部分——任务有效性、oracle、
grader,以及适配器——都改在 `./make test` 中运行,这样一个什么都测不出的
任务在花钱之前就会被抓到。这样设计的原因见
[design/evaluation-system.md](../design/评估系统.md),如何运行它、以及
一个 task 和一个 bundle 各自保存什么,见
[evaluation/README.md](../../../evaluation/README.md)。

`./make eval harbor` 报告的是一个外部坐标,而不是产生一个新坐标。Harbor
运行 Terminal-Bench 4.0,由它的验证器决定每一个结果;导入过程读取那次
已完成的作业,并按同一份契约归档,因此一个外部结果和一次本地运行携带
相同的 subject 元组、相同的失败分类,以及相同的带不确定性的通过率。
它是在度量而不是把关——一个 subject 没有解出的任务,就是一个分数。

`./make eval harbor run` 会启动那次运行:它根据锁定版本组装出 Harbor
命令、启动它,并导入这次作业。它需要 Docker 和一个模型 API key,并且
会花钱,因此它和本地套件一样需要慎重对待。`./make eval harbor --job <dir>`
是单独执行导入,用于别人已经跑完的作业;那一半不构建任何东西,也不调用
任何模型。`./make doctor harbor` 会报告一次运行需要什么,
`./make setup harbor` 负责安装它;见
[evaluation/harbor/README.md](../../../evaluation/harbor/README.md)。

oracle 冒烟测试和一个单任务金丝雀已经端到端跑通过这条路径。这只对
一个任务、并且仅到此为止做了验证;目前没有 Terminal-Bench 分数。拓宽
它是尚待完成的工作,而第一个金丝雀就在评估系统之外发现了一个产品缺陷,
因此可以预期下一个会发现更多。

如果缺少某个前置条件,该套件会在开始之前说明缺的是哪一个。最容易让人
措手不及的两个:

```bash
npm --prefix portal ci                                  # Portal test dependencies
npm --prefix portal exec -- playwright install chromium # the browser itself
```

## 依附 还是 拥有

一个 Portal 套件要么依附于某个部署,要么拥有一个部署,并且会在开始之前
说明是哪一种。

- **拥有**(`./make e2e local`)会用这次运行为自己选定的项目名和端口,
  启动一个 Compose 技术栈、测试它,再把它连同卷一起拆掉。因为名字和
  端口都是每次运行现选的,它永远不必去猜测某个已经在通常端口上应答的
  东西,究竟是某位贡献者的持久化技术栈,还是自己上次运行遗留下来的
  残余——多次运行(不同的工作树、不同的 Agent、一个人手动执行的
  `./make compose up`)可以同时各自拥有一套属于自己的技术栈。
- **依附**(`./make e2e compose`、`./make e2e kind`)是访客。它使用固定的
  诊断账号 `deployment-smoke@buildmax.local`,只创建带有唯一标记的资源,
  并打印出它留下了什么——其中大多数都没有对应的删除路由,因此那行输出
  就是清理说明。

### 在第一个部署旁边再运行一个部署

`./make kind up` 和 `./make compose up` 会保留每份文档都假定的固定名字
和端口,因为某位贡献者是手动启动它的,并期望一个可预期的地址。第二个
部署如果是有意在第一个旁边运行的,就需要被告知不要与它冲突:

```bash
# A second Compose stack
BUILDMAX_COMPOSE_PROJECT=buildmax-2 BUILDMAX_SERVER_PORT=15678 BUILDMAX_PORTAL_PORT=18080 \
  ./make compose up

# A second kind cluster
BUILDMAX_KIND_CLUSTER=buildmaxdev2 BUILDMAX_KIND_PORTAL_PORT=18080 BUILDMAX_KIND_TLS_PORT=18443 \
  ./make kind up
```

对针对那个技术栈的每一个后续命令都传入相同的变量(`compose
status`/`logs`/`down`、`kind status`/`logs`/`down`、
`e2e compose`/`e2e kind`)——没有任何东西会持久保存名字和端口之间的
映射,记住它的正是这次调用本身。

`./make e2e local` 不需要这些:它每次运行都已经为自己挑选了全新的项目名
和端口,这正是它能在不检查还有什么在运行的情况下,与上述两者中任何一个
并行运行的原因。

## 当某个套件失败时

浏览器套件产生的一切都会写到同一个地方,并在每次运行开始时清空:

```text
.artifacts/e2e/portal/
├── run.txt      the deployment, the run id, and the command that reproduces it
└── results/     Playwright traces, screenshots, and error context per failed test
```

用 `npx playwright show-trace <path>` 打开一份 trace。对于 Go 编写的
套件,失败信息就在测试输出里:每一个都会打印它期望什么、看到了什么——
对终端和 Desktop 套件来说,还会打印它当时拥有的整个屏幕或事件流。

在改动代码之前先阅读产物。一个失败的端到端测试指出的是一个边界;从
测试名称去猜测那个边界,正是一个真实缺陷变成一条被削弱的断言的方式。

## CI 运行什么

| 触发条件 | 运行什么 |
|---|---|
| 每个拉取请求 | 必需的 `ci.yml` 作业:Go、前端、开源合规,以及部署冒烟健康检查 |
| 相关的拉取请求 | 针对 Go/任务运行器改动的 Windows 检查、发布配置校验,或一次 Portal 镜像构建 |
| 合并到 `main` | 必需 CI、Windows、CodeQL、发布快照,以及按路径划分的部署冒烟测试 |
| 定时 | 每日部署冒烟测试与每周 CodeQL 分析 |
| 手动触发 | 所选定的工作流,用于发布准备或怀疑出现的环境回归 |

端到端验证被刻意排除在拉取请求门禁之外。一次合并后的失败,由破坏它的
那次合并的作者去分诊,如果不能在一个工作日内修复,那次合并就会被回退。
一个间歇性失败的测试,会在当天就被隔离,而不是被重试。

真正让所有人对此负责的是 **Deployment smoke health** 这个作业:它不
验证你的拉取请求,而是拒绝在上一次部署冒烟测试失败的情况下,让任何
东西继续叠加到 `main` 上,并指出是哪次运行、哪个提交让它变成了这样。
一个以修复该套件为目的的拉取请求,会带上 `deployment-smoke-fix` 标签,
从而被放行。

每一次合并后的运行,都会把每个套件报告为通过、失败、已取消,或因某项
策略而跳过并说明原因。一个被跳过的套件,绝不能作为某条路径已经通过的
证据。

## 发布之前

```bash
./make check ci   # required PR suite plus conditional release/Windows checks
./make e2e all    # cli, desktop, then a browser run against a stack it owns
```

`./make e2e all` 刻意省略了 kind:那个套件需要一个集群,而一个悄悄构建
集群的发布检查会让人措手不及。对于实质性的 Portal 或 Server 工作,以及
每一个与部署形态相关的改动,请用 `./make kind up` 接上
`./make e2e kind` 显式补上这个空缺。

## 前端组件测试

`gui/` 和 `desktop/frontend` 在带有 jsdom document 的 vitest 下运行它们
的组件测试,因此一个测试断言的是一个观察者能看到什么、哪个键做什么,
而不是某个函数返回了什么。`gui` 是一个共享组件自身行为被证明一次、
供两个界面使用的地方;一个界面的测试只覆盖它自己对那个组件的接线。
Portal 把浏览器级别的断言留给 Playwright,在那里,一个真实引擎才是
重点。

```bash
./make check gui               # build the package, type-check, run its tests
npm --prefix gui test          # the tests alone, while iterating
```

`gui` 中的 `npm test` 会先做类型检查再运行:`tsconfig.json` 排除了测试
文件,这样任何测试文件的 `.d.ts` 都不会进入 `dist/`;`tsconfig.test.json`
则为了这次检查把它们放回来。

## 新增一个测试

把它放在能证明这个论断的最低层级。一个展示层的决定属于前端单元测试,
一个 Service 契约属于 Go 测试,一条处理器规则属于 HTTP 测试。只有当
结论依赖若干这类测试协同工作——一个真实的二进制文件、一次真实的部署、
一个浏览器——时,才诉诸端到端套件,并在测试中说明那个边界是什么。设计
记录的 §6 列出了合格的路径,以及对于部署类路径,列出了任何处理器测试
都触及不到的那个确切事实。
