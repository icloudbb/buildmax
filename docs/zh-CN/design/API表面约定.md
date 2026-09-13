# API 表面约定

> **翻译说明：** 本文是[英文原文](../../design/api-surface-conventions.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。

> **受众：** 新增 HTTP 路由的贡献者 · **状态：** 活动计划 —— 这些约定即刻治理新
> 路由；§6 的调和是已规划、并已拆解为 backlog 任务的工作，尚未交付任何一项

BuildMax server 的 HTTP 表面如何命名、归属与文档化。两个部分都在本记录内：决定一
条新路由该放在哪里的前瞻性约定（§3–§5），以及把少数分歧的现存路由收敛到这些约定
上的调和计划（§6）。调和计划在此完整写出——每一条会变更的现存路由都映射到其目标形
态——以便评审者在它被拆成 backlog 任务之前，先在一处看清整个表面的增量。

本记录取代已经退休的《API 表面约定》提案。它保留那份提案敲定的两项决策（暂不做
URL 版本化、沿 listener 边界拆分 OpenAPI），把其命名规则提升为持久约定，并补上提案
点出却未解决的那一项决策：认证路由的共同前缀（§3.7）。Git 历史保留那份提案。

相关文档：[Worker API 网络边界](Worker API网络边界.md)、[统一 Artifact](统一工件.md)、
[Space 密钥](Space密钥.md)、[实体身份](实体身份.md)、
[server 架构](../contribute/architecture/server.md)、
[当前状态](../current-state.md)、[ROADMAP.md](../ROADMAP.md)，以及
[backlog](../../backlog/README.md)。

## 目录

- [1. 问题与范围](#1-问题与范围)
- [2. 目标与非目标](#2-目标与非目标)
- [3. 命名与归属约定](#3-命名与归属约定)
- [4. 决策：暂不做 URL 版本化](#4-决策暂不做-url-版本化)
- [5. 决策：沿 listener 边界拆分 OpenAPI](#5-决策沿-listener-边界拆分-openapi)
- [6. 调和计划](#6-调和计划)
- [7. 考虑过的方案](#7-考虑过的方案)
- [8. 已解决的问题](#8-已解决的问题)
- [9. 状态与推进](#9-状态与推进)

## 1. 问题与范围

Server 在两个 listener 上暴露约 150 条路由注册。它们的组合以各 handler 子包的
`Register` 方法为权威来源，在 `internal/server/handlers/routes.go` 中经
`RegisterPublic` 与 `RegisterWorker` 接线，并由
`internal/server/static/openapi.json` 逐条精确对应。

这个表面是逐个功能长出来的。其中大部分已经一致——kebab-case 的路径段、复数形式的
集合、`{xxx_id}` 路径参数——但少数形状是局部选定的，如今彼此不一致。由于 BuildMax
处于 Alpha、没有冻结的 API 契约，且每个客户端（Portal、Desktop、CLI、worker）都从
本仓库发布并与 server 一起部署，现在是以最低成本把表面收敛的时机，而不是把分歧继续
带下去。正确的更正是连贯的——server、OpenAPI、客户端与测试在每条路由的一次改动中一
起变更——而不是留下让两种形状同时存在的兼容别名。

本记录只治理可寻址的 HTTP 表面及其文档。它不触碰 handler 内部、授权逻辑、存储，也不
改动两 listener 的网络边界——后者已在
[Worker API 网络边界](Worker API网络边界.md)中定下。`internal/tool/names.go` 仍是
LLM 面向的工具名的权威来源；这些约定不涉及它们。

## 2. 目标与非目标

目标：

- 给出一小组规则，无需再争论即可决定一条新路由该放在哪里、如何命名。
- 定下 URL 版本化与 OpenAPI 文档形状。
- 把分歧路由的完整新旧映射列全、集中可评审，使表面收敛到一种风格，而不是再叠加第三
  种。

非目标：

- 一个对外、受支持的公共 API 契约。今天没有带外消费者；承诺一个是另一项由证据驱动的
  独立决策。
- 重写 handler 内部、授权或存储。
- 改动两 listener 的网络边界。

## 3. 命名与归属约定

这些规则对每一条新路由即刻生效。多数已在遵循；把它们写下来只是为了消除逐条路由的争
论。

### 3.1 路径语法

路径段用 kebab-case，集合用复数：`task-runs`、`webhook-keys`、`audit-events`。已经一
致，现在成为规则。

### 3.2 路径参数

路径参数对公共标识符用 `{resource_id}`；当路径段是自然键而非 `NewPublicID` 时，用描
述性名字（`{plugin_name}`、`{version}`、`{revision}`）。见
[实体身份](实体身份.md)。

### 3.3 顶层与 space-scoped 的归属

当资源在单个 Space 内被管理时，它是 space-scoped——`/api/spaces/{space_id}/...`。顶层
路由——`/api/...`——保留给行为主体，即已认证的账号，及其跨 Space 视角："我拥有或能看
到的全部"这类聚合，例如 `/api/usage` 和我收到的邀请。

`webhook-keys` 是账号所有的——`user_webhook_key` 表按 `user_id` 绑定每一行——因此它就
属于顶层、维持原位、不移动。它之所以读起来像无主，只是因为规则没写下来。（其 handler
目前位于 `space` 包，尽管它是账号级的；把它归位是 §6.5 的后续事项，不是路由变更。）

### 3.4 集合寻址与单实体寻址

集合操作（创建、列举）挂在父路径下。读取或修改一个拥有持久 id 的实体，使用平铺的
`.../{entity}-runs/{id}` 形态，它无需父路径即可定位。这正是为什么 `task-runs` 和
`workflow-runs` 在 task 或 workflow 下创建和列举
（`.../tasks/{task_id}/runs`），却按自身 id 读取（`.../task-runs/{task_run_id}`）。现
在一条规则覆盖两者。

### 3.5 状态迁移

仅设置一个存储型生命周期标志的迁移——资源本已携带的布尔或小枚举——用状态子资源
`PUT .../state` 表达，即 Space secret 已在用的形态。`POST .../{动词}` 动作保留给
`set attribute = X` 无法表达的操作：创建新实体，或作用于活跃执行。判据是幂等性与副作
用，不是英文动词本身。每个资源的两条 enable/disable POST 合并为一条幂等的 `PUT`，因
此这也缩小了表面。

### 3.6 对全局标识的资源按记录授权

当资源被全局标识时，按其 id 定位的路由从记录而非路径取 Space。Artifact 是范式：
`/api/artifacts/{artifact_id}` 路径中不含 `space_id`，因为记录自带。见
[统一 Artifact](统一工件.md)。不要给一条记录已能授权的路由再加一个多余的 Space 段。

### 3.7 认证路由分组

行为主体的 Session 与凭证路由归入共同前缀 `/api/auth/`。今天它们直接散落在 `/api`
之下（`/api/login`、`/api/logout`、`/api/password`、`/api/otp/request`），只有
`/api/token/refresh` 做了分组，于是 auth 表面没有一个统一的查找与文档化之处。把它们
分组，让 auth 这一 listener 切片拥有一个前缀和一个 OpenAPI `tag`。这是已退休提案点为
不一致却未解决的那一项决策；§6.2 给出其映射，它也是超出提案已批准规则的评审点。

## 4. 决策：暂不做 URL 版本化

URL 版本化（`/api/v1/...`）是一种兼容工具，面向的是无法与 server 一起重新部署的消费
者。这样的消费者并不存在：Portal、Desktop、CLI 和 worker 都从本仓库发布并与 server 一
起部署，且 N-1 回滚承诺已被撤销，因此没有需要弥合的版本偏斜。现在加 `/v1/` 会发出一
个永远不会有后继的版本，因为表面完全可以与其客户端同步变更——一个恰好只有一个取值的
概念，被 Occam 剃刀拒绝。

仅当出现会 pin 到某个版本的带外消费者时才引入版本化——一个公共 API、一个第三方集成，
或一份发布的 SDK。这是由证据驱动的决策，不是日历驱动的；其可能形态是一个版本化的公
共子集，而非在整个内部表面之上加一个全局 `/v1/` 前缀。

`openapi.json` 中的 `info.version` 字段是 spec 元数据，不是 URL 版本。如今它是一个手写
的字面量（`0.0.7`），不绑任何来源、也没人读，因而会漂移。已决策：由构建从唯一的应用
版本源打戳——即 `tools/mk` 已在链接时注入 `config.Version` 构建变量的那个 git tag——而
不再手工维护。OpenAPI 3.0 规定 `info.version` 必填，因此这是把它绑到真实来源而非删
除；具体是构建改写所提供的 spec，还是 `GET /openapi.json` handler 在提供时注入
`config.Version`，是任务阶段的实现选择。

## 5. 决策：沿 listener 边界拆分 OpenAPI

如今一份 `openapi.json` 同时记录两个 listener，包含 `/api/worker/*`。这在文档层面抹掉
了代码与网络刻意维护的一条边界：公共 listener 无法 dispatch worker 路由，二者使用不同
认证（用户 JWT 与 run token），且运行在各自独立的 socket 上。见
[Worker API 网络边界](Worker API网络边界.md)。

沿这条已有边界把规范拆成一份公共文档和一份 worker 文档，各自对应一个 `Register*` 方
法。这不是新概念——它遵循代码中已强制的不变量——并让"spec 与路由逐条精确对应"的校验能
够按 listener 独立进行，而不是靠人肉把一个大文件与两组路由对齐。

不要再往下拆。Admin、shared-artifact 和 auth 路由同在公共 listener、同一套认证方案
（或一套明确的无认证方案）；把它们分成更多文档只会在没有边界支撑的情况下增殖产物。用
OpenAPI 的 `tags` 在公共文档内对这些子受众分组。

## 6. 调和计划

下列每一条都是会变更的现存注册。未列出的路由已经符合约定、维持原样；§6.4 记录了那些
不得"修正"的范式。每一处变更都是连贯的——server 路由、`openapi.json`、每一个调用它的
客户端，以及路由/spec 测试一起变更——不留别名，遵循 §1。

### 6.1 状态迁移重命名

每个资源的两条 enable/disable POST 合并为一条幂等的 `PUT .../state`，遵循 §3.5。四项
都在 admin 这一 listener 切片上。

| 当前路由 | 目标 | 存储标志 |
|---|---|---|
| `POST /api/admin/users/{user_id}/disable`、`.../enable` | `PUT /api/admin/users/{user_id}/state` | `disabled` |
| `POST /api/admin/llm/models/{model_id}/enable`、`.../disable` | `PUT /api/admin/llm/models/{model_id}/state` | enabled/disabled |
| `POST /api/admin/plugins/{plugin_name}/archive`、`.../unarchive` | `PUT /api/admin/plugins/{plugin_name}/state` | `archived` |
| `POST /api/admin/plugins/{plugin_name}/releases/{version}/yank` | `PUT /api/admin/plugins/{plugin_name}/releases/{version}/state` | `yanked` |

### 6.2 认证路由分组

遵循 §3.7。请求体、方法与认证均不变；只有路径移到 `/api/auth/` 之下。

| 当前路由 | 目标 |
|---|---|
| `POST /api/otp/request` | `POST /api/auth/otp` |
| `POST /api/login` | `POST /api/auth/login` |
| `POST /api/logout` | `POST /api/auth/logout` |
| `POST /api/password` | `POST /api/auth/password` |
| `POST /api/token/refresh` | `POST /api/auth/token/refresh` |

### 6.3 保留为动作的路由

这些是 `POST .../{动词}`，在 §3.5 下维持不变：每一项都创建新实体或命令活跃执行，是
`PUT .../state` 无法表达的。列出它们，以免后来的读者把它们误当成 §6.1 的重命名对象。

| 路由 | 为何保留为动作 |
|---|---|
| `POST /api/spaces/{space_id}/tasks/{task_id}/cancel` | 命令一个活跃 run |
| `POST /api/spaces/{space_id}/tasks/{task_id}/retry` | 创建一个新 run |
| `POST /api/invitations/{invitation_id}/accept` | 创建一个 membership |
| `POST /api/spaces/{space_id}/agents/{agent_id}/revisions/{revision}/restore` | 创建一个新 revision |
| `POST /api/spaces/{space_id}/workflows/{workflow_id}/revisions/{revision}/restore` | 创建一个新 revision |

### 6.4 范式路由 —— 不变更

这些已经体现了约定，不得改动：

- **双重 run 寻址（§3.4）。** `task-runs` 与 `workflow-runs` 在父下创建和列举、按持久
  id 读取。两种形态都保留。
- **按记录授权（§3.6）。** `/api/artifacts/{artifact_id}` 及其子路由从记录取 Space。保
  留。
- **顶层账号聚合（§3.3）。** `/api/usage`、`/api/invitations` 与 `/api/webhook-keys`
  是行为主体的跨 Space 视角。保留在顶层。
- **状态子资源（§3.5）。** `PUT /api/spaces/{space_id}/secrets/{secret_id}/state` 与
  `PUT /api/spaces/{space_id}/plugin-curation` 已是目标形态。
- **部分更新。** 对资源的 `PATCH`
  （`.../agents/{agent_id}`、`.../plugin-activations/{plugin_name}`、`.../secrets/{secret_id}`）
  是编辑字段的正确动词，不受 §3.5 影响。

### 6.5 非路由后续事项

- `webhook-keys` 的 handler 位于 `space` 包，尽管其路由是账号级的（§3.3）。把它归位到
  更合适的账号级包。这是内部移动；路由不变，客户端不受影响。

### 6.6 OpenAPI 与版本元数据

- 沿 `RegisterPublic`/`RegisterWorker` 把 `openapi.json` 拆成公共文档与 worker 文档
  （§5），并让"spec 与路由对应"的校验按 listener 运行。
- 从 `config.Version` 给 `info.version` 打戳（§4）。

## 7. 考虑过的方案

- **版本化：现在采用 `/api/v1/`。** 拒绝：没有消费者需要它，并且它会在 Alpha 期把今
  天的形状冻结为 "v1"，与产品原则相悖。
- **版本化：基于 header 或内容协商。** 同样的反对——它解决一个尚不存在的偏斜问题——却
  比 URL 前缀多出更多机械结构。
- **OpenAPI：保留单一文档。** 拒绝：它向公共表面读者暴露 worker 控制面，并让"与路由对
  应"的校验去把一个文件与两组不相交的路由集合调和。
- **OpenAPI：每个 handler 子包一份文档。** 拒绝：子包是内部拆分，不是对外边界；
  listener 拆分才是对读者和网络策略都有意义的边界。
- **调和：用别名同时保留新旧两种形状。** 拒绝：没有外部消费者需要旧路径，而一个活跃
  别名恰恰是本记录要消除的分歧。Alpha 让这一变更得以连贯完成。
- **auth 分组：把路由平铺在 `/api` 下不动。** 拒绝：那样 auth 表面就没有统一的前缀或
  tag，而这正是提案已指出的"读起来临时拼凑"的原因。

## 8. 已解决的问题

- 两份 OpenAPI 文档是作为两个独立提交文件存在，还是作为一份源生成两个视图？**已定：两
  个提交文件**——`internal/server/static/openapi.json`（公共）与 `openapi-worker.json`
  （worker）。规范是与其 handler 并置手工维护的，没有生成器可加；两个文件让"与路由对
  应"的校验各自按其 listener 的路由直接核对。worker 文档是一份提交的、由测试校验的产
  物；它不在 worker listener 上提供服务——该 listener 维持最小面（listener 边界测试保
  持 `/openapi.json` 不在其上）。用逐操作 `tags` 对公共文档的子受众（admin、auth、
  shared-artifact）分组是一项文档层面的润色，留作后续；拆分本身才是校验强制的边界。

## 9. 状态与推进

§3–§5 的约定即刻治理新路由。§6 的调和是已规划的；尚未交付任何一项。它拆解为 backlog
任务，每一项自成一体、可验证：

1. 沿 listener 边界拆分 `openapi.json`，并按 listener 运行"与路由对应"的校验（§6.6，一
   并解决 §8）。
2. 从 `config.Version` 给 `info.version` 打戳（§6.6）。
3. Admin 状态迁移重命名为 `PUT .../state`（§6.1）。
4. 把认证路由归入 `/api/auth/`（§6.2）。
5. 把 `webhook-keys` handler 归位到账号级包（§6.5）。
6. 把 §3–§5 折入 [server 架构](../contribute/architecture/server.md)——新增路由的贡献
   者会去看的地方——并在调和合并后删除本记录。Git 历史保留其理由。
