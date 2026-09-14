# 客户端 Session 与 API 凭据

> **翻译说明：** 本文是[英文原文](../../proposals/client-sessions-and-api-credentials.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。

> **受众：** 贡献者、产品评审人、运维人员与安全评审人 · **状态：** 提案 —— 已由实现收窄：持久人类 Session、Portal cookie 认证与原生 Secret 存储已交付；机器凭据、scope/audience、签名密钥轮换与自助管理仍在讨论
>
> **提出时间：** 2026-08-24

相关文档：[路线图](../ROADMAP.md) R5、
[部署认证](../deploy/authentication.md)、
[托管 LLM 网关设计](../design/LLM网关.md)、
[客户端模式设计](../design/客户端模式.md)、
[Worker 运行令牌设计](../design/Worker运行令牌.md)、
[数据模型](../contribute/architecture/data-model.md)，以及
[企业身份与访问设计](../design/企业身份与访问.md)。

## 目录

- [决策问题](#决策问题)
- [问题与当前背景](#问题与当前背景)
- [当前代码状态](#当前代码状态)
- [凭据职责与威胁模型](#凭据职责与威胁模型)
- [目标](#目标)
- [非目标](#非目标)
- [设计原则](#设计原则)
- [推荐的人类客户端流程](#推荐的人类客户端流程)
- [机器凭据](#机器凭据)
- [数据模型影响](#数据模型影响)
- [HTTP API 影响](#http-api-影响)
- [CLI、Desktop 与 Portal 的用户体验](#cli-desktop-与-portal-的用户体验)
- [方案与权衡](#方案与权衡)
- [代码与文档发现](#代码与文档发现)
- [若被采纳后的分阶段方向](#若被采纳后的分阶段方向)
- [需要产品决策的事项](#需要产品决策的事项)
- [做出决策所需的证据](#做出决策所需的证据)
- [若被采纳后的可能归宿](#若被采纳后的可能归宿)

## 决策问题

已经实现的人类 Session 路径回答了原始问题：BuildMax 登录后不会返回第三个长期
有效的网关 token。提案现在剩下的问题是：受支持的无人值守调用方应获得哪一种
显式凭据（如果需要），以及怎样的 audience、scope、所有权、到期与轮换契约才安全。

可能的方向是:

> 不应该。一次人类登录应当只创建一个可撤销的客户端 session。短期有效的
> access token 用于授权 API 调用,而一个可轮换的 refresh token 让该 session
> 保持可用。长期有效的机器凭据应当作为个人访问令牌(personal access token)
> 或服务账号(service-account)凭据被显式创建,而绝不应作为登录的副作用产生。
> 如果托管网关需要一个范围更窄的凭据,客户端应当按需获取一个短期有效、
> 限定受众(audience-restricted)的 token,而不是在登录时获得一个长期有效的
> 网关 token。

人类 Session 部分已经通过获采纳的[企业身份与访问](../design/企业身份与访问.md)
记录实现。PAT、服务账号、网关 token 交换与签名密钥轮换仍是提案，不是路线图承诺。

## 问题与当前背景

BuildMax 有两种不同的需求,它们看起来相似,仅仅是因为二者都需要一个 Bearer
请求头:

1. 一个人在 Portal、CLI/TUI 或 Desktop 上登录,并期望该 session 在
   access token 过期后依然可用,而不必反复向运维人员索要登录码。
2. 一个脚本、集成、CI 任务或共享服务可能需要在无人在场的情况下调用 API。

前者是人类 session,后者是机器权限(machine authority)。如果每次成功登录都
返回一个静态的长期 token,就会把这两者混为一谈:普通的登录会悄悄创建出一个
自动化凭据,登出对该凭据没有明确的含义,而审计轨迹也无法区分交互式客户端与
无人值守的调用方。

托管 LLM 网关并不构成第三种需求。本地 CLI/TUI 或 Desktop 上的 Agent 运行时长
可能超过一个 access token 的生命周期,但它可以在每次请求前续期该 session。
一次流式补全(streaming completion)在请求被接受时即已获得授权;它不需要一个
生命周期长于所有可能的模型调用的 access token。重新连接时可以先获取一个新的
access token。

这个区分在 BuildMax 中比在常规 API 客户端中更为重要。本地 Agent 可以执行
模型选择的命令,而 Bash 沙箱默认是关闭的。一个以文件形式存储、仅同一操作系统
用户可读的凭据,能够防止被另一个操作系统账户读取,但未必能防止在 Agent 自身
信任域内执行的代码读取。再添加一个长期有效的 bearer 密钥,只会增加这种暴露面,
而不会改善 session 的连续性。

## 当前代码状态

### 交互式登录

`POST /api/auth/login` 接受密码或运维人员签发的一次性登录码。任一凭证都会创建
持久 `auth_session`，并返回:

- `access_token`,以及为兼容而保留的旧字段 `token`;
- 当配置了 refresh-token 存储时返回 `refresh_token`;
- `expires_in`;以及
- 用户的公开身份字段。

每次登录都会创建带绝对生命周期的独立 session。refresh token 是一个不透明的随机密钥,
仅以 SHA-256 哈希的形式存储在 `user_refresh_token` 中。轮换(rotation)会
消耗当前呈现的行并在同一 `session_id` 链中创建替代行。在 `refresh_rotation_grace`
之外重用旧 token 会撤销整条链,并记录一条 `auth.refresh_reuse` 审计事件。

默认值如下:

| 设置项 | 当前默认值 | 当前含义 |
|---|---:|---|
| `access_token_ttl` | 7 天 | 一个未被存储的 access JWT 保持可用的时长 |
| `refresh_token_ttl` | 30 天 | 当前 refresh-token 行可被兑换的时长 |
| `refresh_rotation_grace` | 30 秒 | 一个已被消耗的 token 允许被并发的客户端进程再次兑换的时长 |
| `session_absolute_ttl` | 90 天 | 原生/密码/登录码 Session 不受刷新活动影响的硬上限 |

轮换会为每个替代 token 赋予 `now + refresh_token_ttl`，但不能让持久 Session
越过 `session_absolute_ttl`（默认 90 天）。

登出会撤销持久 Session 及其 refresh-token 链。请求守卫会在每次认证调用时解析
`sid`，因此登出、管理员撤销、绝对过期与账户禁用都会在下一次请求时停止已经签发的
access token。

### Access Token 的形态

用户 JWT 目前包含:

| Claim | 含义 |
|---|---|
| `sub` | 用户的公开 ID |
| `typ` | `access` |
| `sid` | 持久 `auth_session` |
| `jti`、`iat`、`exp` | 注册的 token 身份与生命周期 claim |

它没有强制校验的 issuer、audience、客户端身份或 scope。`typ` 能够防止 run token
冒充用户 access token,但一个合法的用户 access token 在其他方面对整个用户 API
都是通用的。

### 托管客户端

`<BUILDMAX_HOME>/auth.json` 的存在与否决定了是否进入托管模式(managed mode)。
CLI/TUI 与 Desktop 会获取部署的模型列表、构建一个远程 LLM 客户端,并在每次
托管请求之前申请凭据。`TokenForServer`:

- 拒绝把凭据发送到登录时存储的 Server URL 之外的其他地址;
- 当当前 access token 仍然可用时直接返回它;
- 在快要过期之前刷新它;以及
- 当服务器拒绝 refresh token 时结束本地登录状态。

因此,远程客户端已经能够顺利度过 access token 过期。不会有任何固定的 token
被硬编码进一个长期运行的 TUI 或 Desktop 进程中。

当前的网关路由为:

```text
GET  /api/llm/models
POST /api/llm/completions
```

两者都调用 `ActiveUser`。已登录就是它们全部的授权条件:每个已启用的目录中的
模型都是部署全局的,前台调用被归属到具体的人,而不是某个 Space。

### 客户端存储

CLI/TUI 与 Desktop 默认把 access/refresh token 保存在 OS 凭据存储中；不可用时
回退到明确报告的 `0600` 文件。`auth.json` 只保留 Server URL 与非密钥用户元数据。

Portal 把可续期 refresh 凭据保存在 Secure、HttpOnly、SameSite=Strict cookie 中，
短期 access token 只放内存。浏览器 JavaScript 无法读取 refresh token；刷新与登出
通过 cookie 完成。

### Worker 认证

Task-run worker 已经在使用本提案希望为机器执行保留的凭据形态:调度器会签发
一个短期有效的 run token,其 claim 指定唯一的用户、Space、task 与 TaskRun。
每个 worker 路由都会检查路径中所指定的 run 是否与凭据中的一致,而托管推理还
会额外要求该 run 处于执行中状态。

run token 的设计明确拒绝复用某个人的 access token。一个 worker 执行的是模型
选择的命令,而一个用户 token 会打开该用户可及的每一个 Space 与资源。归属
(attribution)并不需要冒充(impersonation)。同样的最小权限原则也应当适用于
个人访问令牌(PAT)与服务账号的设计。

## 凭据职责与威胁模型

各类凭据应当保持彼此独立:

| 凭据或证明 | 职责 | 预期生命周期 | 主要威胁 |
|---|---|---:|---|
| 密码、登录码、OIDC 授权或设备授权 | 证明某人可以开始一个 session | 一次认证事务 | 钓鱼、暴力破解、账户关联或验证码截获 |
| Access token | 授权一组有限的 API 调用 | 数分钟,而非数天 | 在过期前被直接重放;当前的 session 撤销并不能阻止这一点 |
| Refresh token | 延续一个客户端 session 并铸造新的 access token | 数天不活跃期,并有一个绝对上限 | 被窃取会创造出可续期的权限;轮换只能事后检测到重用 |
| 个人访问令牌(PAT) | 让某个人无人值守的客户端执行明确选定的操作 | 显式的、有过期时间的授予 | 静态重放、被遗忘的凭据、过度的 scope、人员离职 |
| 服务账号凭据 | 认证一个由 Space 或部署拥有的非人类主体 | 由策略控制或与工作负载绑定 | 归属关系失效(orphaned ownership)、宽泛的共享密钥、归属追溯薄弱 |
| Run token | 让一次已分派的 TaskRun 只能使用它自己的 worker 路由 | 一次 run | 在 run 结束或 token 过期前从进程或 Job 状态中泄漏 |

Refresh token 本身已经是人类 session 的长期有效部分。相比一个通用的长期
API token,它更安全,因为它只在 token 端点被接受、以哈希形式存储、可单独
撤销、使用后即轮换,并且与一个 session 族绑定。但它仍然是一个高价值密钥,
不能仅因为它不能直接调用网关就被视为无害。

OAuth 2.0 安全最佳当前实践(Security Best Current Practice)要求公开客户端
的 refresh token 必须绑定发送方(sender-constrained)或加以轮换,并建议将
access token 限定为最小必要的受众与权限范围。BuildMax 目前实现了轮换,但
没有实现受众或 scope 限制。参见
[RFC 9700](https://www.rfc-editor.org/info/rfc9700/) 与
[RFC 8707](https://www.rfc-editor.org/info/rfc8707/)。

## 目标

- 让人类登录在短期 access token 生命周期下依然可用,而不必新增第三个长期
  有效的登录凭据。
- 限制某个 access token 从托管 LLM 请求、客户端进程、日志或本地运行时泄漏后
  造成的影响。
- 让登出、session 过期、凭据轮换与账户禁用都具有明确且可测试的含义。
- 让 refresh token 不出现在 Agent 可见的配置、prompt、工具、hook、MCP、
  子 Agent(subagent)以及日志和 trace 中。
- 为脚本与服务提供一条显式的机器凭据路径,具备 scope、过期时间、归属关系与
  单独可撤销性。
- 保留 run 范围限定的 worker 凭据,以及直连本地模式。
- 保留已交付的 OIDC 浏览器流程与独立配置的原生登录姿态，而不把 OIDC 变成私有
  部署的必选项。
- 让 Space 成员身份与系统管理员授权保持为由服务器推导的授权结果,而不是
  客户端可以任意声明的内容。

## 非目标

- 在本提案中实现 OAuth、OIDC、设备授权、PAT 或服务账号。
- 在登录限流、SSO 或第二因素以及支持矩阵中的其他限制得到解决之前,支持面向
  公共互联网暴露。
- 把托管网关变成一个公开的 OpenAI 兼容 API。
- 为前台托管调用增加按 Space 划分的模型策略或 Space 归属;客户端模式设计已
  明确撤回了这两者。
- 让 access token 携带完整的 Space 成员身份或系统角色状态。这些权限是可变的,
  应当继续由服务器推导。
- 把安全的本地存储当作对一个完全被攻陷的客户端进程的防护。一个能够使用某个
  密钥的进程,也可能能够滥用其代理(broker);目标是防止随意的文件泄露,并
  减少该进程之外的重放,而非防御进程本身被攻陷。
- 用 PAT 或服务账号凭据取代 run token。

## 设计原则

### 一次人类登录,一个 Session

每一次交互式登录都恰好创建一个可独立查看、可独立撤销的客户端 session。CLI、
Desktop 与 Portal 不应仅仅因为运行在同一台机器上,就悄悄共享同一个
refresh-token 族。该 session 会记录是哪个客户端与设备创建了它;`platform`
不再只是一个面向运维人员的标签。

### 机器凭据不作为登录的副作用产生

一次成功的登录响应只返回一对 session 凭据。创建 PAT 或服务账号凭据需要一次
单独的、显式的操作,该操作要指定凭据名称、选择或接受其 scope,并选择一个
过期时间。

这让登出拥有一个明确的含义:它结束的是被选定的那个人类 session。它既不会
留下一个在登录时隐藏创建出来的 token,也不会意外删除用户为其他目的创建的
自动化凭据。

### 短期有效的 Access,可续期的 Session

长期运行的客户端不需要长期有效的 access token。它们需要的是一种安全地获取
另一个短期 token 的方式。客户端会在一次预计耗时较长、可能接近过期的请求开始
之前先行刷新。一旦服务器已经接受了一次 SSE 补全,过期本身并不会终止这次
正在进行中的调用;但一次新的或重新连接的请求必须重新完成认证。

供讨论的策略默认值建议如下:

| 生命周期 | 建议默认值 | 理由 |
|---|---:|---|
| 用户 access token | 15-30 分钟 | 限制一个未被安全存储的 bearer token,同时把刷新流量控制在适度水平 |
| 若引入仅用于网关的 access token | 5-15 分钟 | 它是在模型流量发生前即时申请的,只需要两个 scope |
| Refresh 不活跃期 | 30 天 | 保持当前的可用性预期 |
| 人类 session 的绝对生命周期 | 90 天 | 防止活跃状态让一次授权被无限续期 |
| PAT | 30 天,并由运维人员设定上限 | 让无人值守的权限变得显式,并迫使形成轮换策略 |

这些数值是产品与运维策略层面的决策,而非承诺。一个受信任的私有部署可能会
在过渡期内选择更长的 access-token 生命周期,但当前 7 天的默认值不应成为
生产环境的目标值,只要一次 session 登出还无法使其失效。

### Audience 与 Scope 在路由层被强制执行

至少,一个用户 access token 应当携带、且服务器应当校验以下内容:

| Claim | 建议含义 |
|---|---|
| `iss` | 签发该 token 的 BuildMax 部署的规范身份标识 |
| `aud` | 预期的资源,初始为 `buildmax-api` 或一个规范的 API URI |
| `scope` | 授予该客户端 token 的操作范围 |
| `client_id` | `buildmax-cli`、`buildmax-desktop`、`buildmax-portal` 或其他已注册的客户端 |
| `sub` | 用户或服务账号的公开 ID |
| `sid` | 人类 session,机器凭据中省略 |
| `typ` | 凭据类别,保留替代性检查 |
| `jti`、`iat`、`nbf`、`exp` | Token 身份与生命周期 |

托管网关所需的最小 scope 为:

```text
llm.models.read
llm.completions.create
```

Space 成员身份、Space 角色、当前账户状态、模型启用状态、配额以及系统管理员
授权仍然是服务器端读取的结果。一个 token scope 表明这个客户端授权可以尝试
执行哪一类操作;它并不断言该主体拥有某个资源。

有两种受众(audience)划分方式仍然可行:

1. **单一的 BuildMax API audience。** `aud=buildmax-api`,配合路由级别的 scope。
   在 Portal、本地客户端与网关仍是同一个资源服务器的前提下,这是改动最小的
   方案。
2. **一个独立的托管推理 audience。** 一个凭据代理(credential broker)使用
   人类 session 去申请一个短期有效的 `aud=buildmax-llm` token。网关路由拒绝
   通用 API token,而其他每个路由都拒绝这个 LLM token。这能减少跨路由重放,
   但会引入一个 token 交换协议。

第二种方案是纵深防御(defense in depth),而不是签发一个长期有效网关 token
的理由。如果 scope、短生命周期与安全的 refresh-token 存储能够一并落地,第一
种方案是一个合理的初始切片(slice)。

### 撤销拥有两层机制

在 session 存储不可用时,较短的 access-token 过期时间仍然是外层边界。当
session 存储可用时,一条显式的 session 行可以让常规的已认证请求检查拒绝一个
`sid` 已被撤销的 access token。BuildMax 目前已经在每次已认证请求中读取用户行,
以使账户禁用立即生效,因此增加 session 状态并不会在该路径上引入第一个数据库
依赖。

服务器应当区分:

- 撤销一个 session;
- 撤销某个用户的所有 session;
- 禁用账户,这将拒绝 session 与机器凭据;
- 撤销一个 PAT 或服务账号凭据;以及
- 轮换部署的签名密钥,这是一个运维事件,而不是 session 撤销的替代品。

密码修改与身份提供方(identity-provider)的取消供应(deprovisioning)需要一个
明确的策略:撤销所有 session、只撤销以密码认证的 session,还是保留 session
不变。当前密码修改后 session 仍然保持存活。

### 凭据存储在 Agent 权限之外

首选的原生客户端安排是:

```text
Agent 运行时 ── 请求 access token ──> 凭据代理(credential broker)
                                                  │
                                                  ├─ access token 保存在内存中
                                                  └─ refresh token 保存在操作系统密钥存储中
```

Desktop 应当使用操作系统原生的凭据存储:macOS 上的 Keychain、Windows 上的
Credential Manager,以及受支持的 Linux 桌面环境上的 Secret Service。CLI/TUI
应当使用同一类存储,或一个轻量的凭据帮助程序(credential-helper)接口。
`auth.json` 可以保留非密钥类的元数据,例如 Server URL、subject、session ID
以及所选的存储后端。

如果某个平台没有可用的密钥存储,在 Alpha 阶段可以保留一个 `0600` 文件作为
显式的回退方案,但界面必须报告这种较弱的存储模式。不存在任何有用的
静态加密回退方案,因为解密密钥就存放在密文旁边。

Access token 与 refresh token 绝不能被复制进:

- `settings.yaml` 或工作区配置;
- 进程参数或 shell 历史;
- Agent 环境变量;
- prompt、工具参数、hook 输入、MCP 输入或子 Agent(subagent)上下文;
- 常规日志、错误信息、trace 或分析数据;或
- session 记录(transcript)。

凭据代理还负责协调刷新。分开的 CLI 与 Desktop session 消除了跨应用的竞态。
多个 CLI 进程可以使用操作系统级锁、凭据帮助程序事务,或一个本地代理,这样
服务器就不再需要仅仅因为每个进程都读取同一个文件而设置一个宽泛的宽限窗口。

## 推荐的人类客户端流程

### 当前的密码与登录码流程

1. CLI 或 Desktop 通过 TLS 把密码或运维人员签发的登录码发送到
   `POST /api/auth/login`。
2. 服务器创建一个显式的客户端 session,并返回一个 access token 与一个可
   轮换的 refresh token。
3. 客户端把 refresh token 移入其密钥存储,并尽可能把 access token 保留在
   内存中。
4. 托管模型发现请求使用带有 `llm.models.read` 的 access token。
5. 每次补全请求使用带有 `llm.completions.create` 的 access token;如有需要,
   会先进行刷新。
6. 登出会撤销该 session,并清除本地的密钥与元数据状态,即便服务器无法访问也
   是如此。界面会报告服务器端撤销是否未能得到确认。

一个无法存储 refresh session 的部署可以为了开发兼容性而保留一种仅有
access token 的登录方式,但一个托管客户端应当报告该登录无法续期。一个把
托管推理作为运维服务提供的部署应当要求配置 session 存储,而不是把一个 7 天
有效期的 access token 当作其可用性机制。

### 未来的原生客户端 OIDC 流程

Portal OIDC 已交付。原生 CLI/Desktop OIDC 与无浏览器设备授权仍是未来工作：

- Desktop 与拥有可用浏览器的终端会打开系统浏览器,使用带 PKCE 的
  Authorization Code 流程与一个精确注册的重定向地址;
- 原生应用绝不嵌入身份提供方的登录表单,也不处理用户的 IdP 密码;
- 一个远程或无浏览器的终端可以使用设备授权(Device Authorization),显示一个
  短期有效的用户码与验证 URI;以及
- 无论哪种授权方式,最终都会创建上文所述的同一个 BuildMax 客户端 session 与
  token 生命周期。

设备授权是一种登录引导方式(login bootstrap),不是 PAT,也不是长期有效的
凭据。它需要短期的验证码有效期、轮询上限与限流。原生浏览器与设备指引已由
[RFC 8252](https://www.rfc-editor.org/info/rfc8252/) 与
[RFC 8628](https://www.rfc-editor.org/info/rfc8628/) 标准化。

### Portal Session

Portal 已共享服务器端 Session 模型，并使用同源 Secure、HttpOnly、
SameSite=Strict refresh cookie；access token 只保留在内存。该实现保持浏览器
JavaScript 看不到可续期密钥，同时仍由服务器端 Session 提供逐请求撤销。

## 机器凭据

### 个人访问令牌

当一个人希望某个脚本或外部集成以其身份行事,并接受该授权会随其账户被禁用而
结束时,PAT 是合适的选择。它不是交互式 TUI 或 Desktop 登录所推荐使用的凭据。

一个 PAT 应当:

- 被显式创建并命名;
- 只以明文形式返回一次,此后仅以哈希形式存储;
- 被限定为若干枚举的 scope 与一个 audience;
- 具有一个过期时间,受运维人员配置的最大值约束;
- 可被单独列出并撤销;
- 带有创建、最后使用、过期与撤销的元数据戳记;
- 拥有一个独特的密钥前缀,便于日志与密钥扫描工具识别;以及
- 除非有单独评审过的 scope 允许,否则会被密码、session 管理、凭据管理与
  系统管理相关的路由拒绝。

创建、撤销 PAT 以及修改其策略,都应当属于治理审计轨迹的一部分。大量的 API
调用应当记录在运营记录中,而不是每次请求都产生一条审计事件。

现有的 webhook 密钥并不是一个可以被直接扩展的 PAT 实现。它有名称、所有者、
哈希与创建时间,但没有 scope、audience、过期时间、最后使用时间、撤销状态,
也没有通用的路由认证契约。在一个经过深思熟虑的整合设计证明单一张表能够
同时保留两种产品的语义之前,它应当继续作为一种入站 webhook 凭据存在。

### 服务账号

当权限归属于一个 Space 或部署,而不是归属于某个人的雇佣与 session 生命周期
时,服务账号是合适的选择。它应当是一个独立的主体,具备:

- 一个不透明的公开 ID 与显示名称;
- 一个 Space 或部署所有者;
- 显式的角色与 scope;
- 启用/禁用状态;
- 创建者与治理审计记录;以及
- 一个或多个可独立轮换的凭据。

在执行环境能够提供工作负载身份(workload identity)的情况下,OIDC 联邦或
其他非对称证明方式优于静态共享密钥。静态凭据是一种回退方案,应当只展示一次、
以哈希形式静态存储、具有过期时间,并可单独撤销。

服务账号不得以密码登录,不得获得人类的 refresh session,不得隐式拥有一个
个人 Space,也不得继承创建它的那个人所拥有的每一个 Space 成员身份。调用与
审计事件应当将该服务账号本身标识为行为主体,并单独保留 `created_by` 字段。

### Task-Run Worker

Worker 将继续使用 run token。服务账号或 PAT 设计不会扩展
`/api/worker/*` 的范围,因为这些路由已经拥有一个更好的撤销与 scope 边界:
一个 TaskRun 及其在服务器上的状态。

## 数据模型影响

Alpha 阶段的策略允许一次性修正所有存储形态,而不是保留一个错误的契约。下面的
人类 Session 行已经实现；机器凭据相关行仍是提案。

### `auth_session`

**已交付：** 每次人类登录对应一行:

| 字段 | 用途 |
|---|---|
| 公开 ID | 稳定的 `sid` 与 API 句柄 |
| 用户 ID | Session 所有者 |
| 平台与认证方式 | 打开 Session 的界面与凭据；当前为信息字段 |
| 创建时间、最后活动时间 | 生命周期与经过限流的活动诊断 |
| 绝对过期时间 | 无论如何轮换都适用的最大生命周期 |
| 撤销时间 | 立即终止该 Session |

`user_refresh_token` 行会引用该 session,并保留 token 哈希、轮换/替换关系、
过期时间、使用记录与撤销证据。该 session 行使得列出与撤销操作无需从每一次
轮换记录中重新构建出整条家族链。

强制 client ID、用户可识别的设备名、单独的 Session 空闲过期时间与持久撤销原因仍是
拟议新增字段，不是当前行已经拥有的字段。

### `personal_access_token`

每个用户创建的机器凭据对应一行:

| 字段 | 用途 |
|---|---|
| 公开 ID、密钥前缀、密钥哈希 | 管理句柄与单向凭据查找 |
| 用户 ID、名称 | 所有者与可识别的用途说明 |
| Audience、scope | 强制执行的最小权限 |
| 创建者、创建时间 | 溯源信息 |
| 过期时间、最后使用时间 | 轮换与事件响应 |
| 撤销时间与原因 | 保留生命周期证据,而不是硬删除 |

### `service_account` 及凭据相关行

只有当 Space 或部署所拥有的自动化被确认为一项被接受的产品需求时,才应添加
这些内容。主体元数据与凭据应当分开存储,这样一个服务账号就可以在不改变身份
或审计历史的前提下轮换凭据。

### 签名密钥状态

当前单一的 `jwt_secret` 既用于签署用户 access token,也用于签署 run token。
一个可用于生产环境的轮换设计需要:一个当前签名密钥、为已签发 token 保留的
验证密钥、新 token 中携带的密钥 ID,以及一套不把密钥轮换当作 session 撤销
手段的运维流程。把用户签名密钥与 run 签名密钥分开,能够进一步限制某个密钥
特定故障的影响范围,但会增加运维配置负担,必须结合部署模型加以评估。

## HTTP API 影响

准确的路由只以 `internal/server/handlers/routes.go` 为权威来源。已交付的人类
Session 路由与拟议的自助/机器路由如下:

```text
POST   /api/auth/login                 # 已交付
POST   /api/auth/refresh               # 已交付
POST   /api/auth/logout                # 已交付

GET    /api/sessions                   # 拟议自助
DELETE /api/sessions/{session_id}      # 拟议自助
DELETE /api/sessions                   # 拟议自助

POST   /api/personal-access-tokens
GET    /api/personal-access-tokens
DELETE /api/personal-access-tokens/{token_id}
```

OIDC、设备授权、服务账号管理或 token 交换相关的路由,只应随各自被接受的设计
一并添加。上面这份路由草图并不隐含它们已经存在。

登录与刷新的 DTO 应当暴露 token 类型与由服务器计算得出的过期时间。刷新操作
必须保留或收窄原 session 的 audience 与 scope;客户端不能请求升级权限。如果
Alpha 阶段的客户端能够统一切换,那么可以直接移除遗留的重复字段 `token`,
而不必无限期地保留它。

每个已认证的路由都应声明其允许的凭据类型、audience 与所需的 scope。把一个
PAT 呈递给 `/api/auth/refresh`、把一个仅用于网关的 token 呈递给某个 Issue
路由、把一个用户 access token 呈递给某个 worker 路由,或把一个 run token
呈递给某个用户路由,都应当在资源授权检查之前就失败。

## CLI、Desktop 与 Portal 的用户体验

### CLI/TUI

面向用户的最小命令集为:

- `buildmax login` 会标识 Server、认证方式与凭据存储后端;
- `buildmax whoami` 会报告账户、Server、session 过期时间、客户端 session,
  以及当前使用的是安全存储还是文件回退存储;
- `buildmax logout` 会撤销并移除一个 session,并保留当前的行为:当 Server
  不可达时清除本地状态;
- session 列表与撤销命令让一个人无需求助系统管理员即可注销另一台设备;以及
- 创建 PAT 是一条独立的命令,需要提供名称、过期时间与显式的 scope,并只会
  打印一次密钥明文。

`buildmax login` 仍然是切换到托管模式的开关,而登出仍然是切回本地模型的
显式开关。凭据过期绝不会导致隐式回退。

### Desktop

Desktop 使用相同的后端 session 与凭据代理,在账户设置中展示当前设备与其他
session,并对过期或已撤销的登录状态进行标注,而不是悄悄进入本地模式。未来的
OIDC 将打开系统浏览器,而不是嵌入身份提供方页面。

### Portal

账户设置会列出 session 与 PAT 元数据,但在创建之后绝不会显示明文密钥。系统
管理界面保留全部撤销与账户禁用相关的恢复路径。服务账号管理应当归属于其
所在的 Space 或系统所有者,而不是放在个人 session 设置中。

## 方案与权衡

| 方案 | 优势 | 代价或失败风险 | 方向 |
|---|---|---|---|
| 为每次登录增加第三个长期有效的 token | 对客户端而言表面上更简单 | 使刷新职责重复,制造隐藏的机器权限,登出与审计含义模糊,泄漏窗口很大 | 拒绝 |
| 为 TUI/Desktop 使用一个静态 PAT | 无需实现刷新机制 | 交互式客户端持有一个可直接使用的长期有效密钥;重用检测能力弱,session 体验差 | 拒绝 |
| 保留 access + 轮换的 refresh,单一 API audience | 改动最小;现有客户端已在做刷新 | 一个泄漏的 access token 可能跨越其 scope 所允许的多个 API 区域 | 基础已交付；audience/scope 加固仍待完成 |
| 增加按需的网关 token 交换 | audience 分离效果强,LLM 凭据生命周期短 | 增加更多协议、缓存、失败与发现相关的行为 | 在基础 session 模型之上优先加固的方向 |
| 让每个 access token 都具备状态(stateful) | 可立即撤销 | 每次经过守卫的请求都需要数据库读取，并带来可用性耦合 | 已通过持久 Session 检查交付 |
| 用 DPoP 为原生 token 绑定发送方 | 单独被窃取的 token 用处更小 | 密钥生命周期与跨平台实现复杂度高;同进程内被攻陷时仍可使用该密钥 | 若有部署证据支持,可作为后续加固手段 |
| 仅新增 PAT | 用一个较小的主体模型解决个人脚本化需求 | 会鼓励以个人身份拥有自动化;无法解决 Space 拥有的服务问题 | 当存在真实的脚本化用例时有用 |
| 优先新增服务账号 | 为共享自动化提供正确的所有者 | 更大的授权、供应与界面改动面 | 等待出现 Space 拥有的自动化需求时再做 |
| 为每个原生客户端使用浏览器 PKCE | 标准的、支持 SSO 的流程 | 在远程/无头终端上使用不便 | 在有浏览器可用时使用 |
| 为每个 TUI 使用设备授权 | 可远程使用 | 需要轮询、验证码钓鱼相关的用户体验设计,以及在浏览器本可更简单时引入更多端点 | 无浏览器终端的回退方案 |

## 代码与文档发现

本提案依赖当前代码,而在部分旧文档与之不一致的地方进行了修正。这些修正
没有等待产品决策落地,而是已经全部完成:

- `docs/contribute/architecture/desktop.md` 描述了已被移除的 Desktop
  模式状态机制,以及已删除的 `UseLocalMode` 与 `ConnectToServer` 绑定。
  已修正为:`auth.json` 的存在与否即为模式本身。
- `docs/contribute/architecture/tui.md` 曾说 CLI 没有应用级别的模式,传输方式
  属于每个模型条目,并打印了一个 TUI 实际并不渲染的 `direct` 页脚标签。
  已修正为:该标签是 `local` 或部署主机名,并且它是应用本身的一个属性。
- `internal/server/static/openapi.json` 记录了 `/api/spaces/{space_id}/llm/*`
  以及一个裸的 `/api/conversations`,二者均未被注册。已修正为部署全局的
  `/api/llm/*` 与 Space 范围限定的 conversation 路由;所有已记录的路径现在
  都与 `routes.go` 一致。
- `docs/contribute/architecture/server.md` 曾说旧的共享 worker token 作为
  升级回退方案仍然保留。已修正为:它已被移除,run token 是 worker 路由
  唯一接受的凭据。
- `docs/contribute/architecture/data-model.md` 曾说没有任何服务器路径写入
  用户的最后登录元数据。已修正为:登录处理程序会调用 `UpdateLoginMeta`。
- `docs/design/llm-gateway.md` 曾把"刷新 versus 一个受限的客户端 token"列为
  一个未决问题,并把 access token 称为一个 24 小时的 JWT。已修正为:刷新
  机制已经实现,配置默认值为 7 天。安全原生存储与 Session 绝对生命周期此后已经
  交付；audience/scope 与机器身份仍未解决。
- P3 路线图中的一句话可能被误读为 CLI、TUI、Desktop 与 task run 都使用一种
  按 run 划分的凭据。已修正为:只有 task run 使用 run token,交互式客户端
  使用的是人类 session。

## 若被采纳后的分阶段方向

### 第一阶段:加固现有的双 token session

已交付：显式 Session 状态与绝对过期、逐请求 Session 强制检查、独立创建的客户端
Session、原生凭据存储接口、Portal cookie 流程、管理员列表/撤销，以及当前文档与
OpenAPI。仍待完成：缩短配置层默认 access-token 生命周期、issuer/audience/client/
scope 强制检查、自助 Session 管理与签名密钥轮换。

这一阶段不会改变任何托管模式的产品语义:登录仍然选择部署的模型,网关调用
仍然按用户归属,直连模式仍然不需要 Server。

### 第二阶段:企业级交互式登录

Portal 带 PKCE 的外部浏览器 OIDC 已交付，并创建同一个持久 BuildMax Session。
原生 CLI/Desktop 浏览器流程与无浏览器终端的设备授权仍待完成。

### 第三阶段:显式的机器身份

当一个受支持的个人脚本化/API 使用场景被明确指出时,增加 PAT。只有当
Space 或部署拥有的无人值守工作有一个具体的所有者与授权需求时,才增加服务
账号。不扩大 worker 认证的范围。

### 第四阶段:面向特定 Audience 或绑定发送方的 Token

如果事件证据、部署拓扑或某个外部 API 产品能够证明其复杂度是合理的,则增加
网关 token 交换或绑定发送方的 token。

## 需要产品决策的事项

1. 托管 LLM 网关是否会永久作为一个仅供 BuildMax 自身客户端使用的 API,还是
   第三方应用会成为一个受支持的产品界面?
2. 是否每个已登录的账户都应继续获得托管推理能力,还是即便模型选择保持全局
   统一,也需要一个部署层面的授权(entitlement)?
3. Access、refresh 不活跃期、session 绝对期限与 PAT 的生命周期,分别应支持
   哪些默认值,以及运维人员可配置的限制范围是什么?
4. 哪些是第一批对应实际受支持自动化用例的 PAT scope?托管推理是否属于其中
   之一?
5. 无人值守的权限应当归属于个人、Space 还是部署,因此是 PAT 已经足够,还是
   需要服务账号?
6. 签名密钥是否应当按用户 token 与 run token 类型分开,私有部署在轮换期间
   应当把验证密钥环(verification key ring)保存在哪里?
7. 原生托管客户端是否需要浏览器 OIDC 与设备授权，还是已交付的 Portal SSO 加
   原生本地登录可以作为受支持的分工？

## 做出决策所需的证据

- 针对 OS 凭据存储或明确报告的回退文件、本地模型选择的命令、hook、MCP 服务器、
  浏览器 JavaScript、日志与 worker 环境中的 token 窃取场景,进行一次威胁模型走查。
- 继续验证已经交付的 Keychain、Credential Manager、Secret Service 与明确报告的
  `0600` 回退在各平台上的行为。
- 针对多个 CLI 进程刷新同一个 session 的并发测试,包括丢失的刷新响应,以及
  宽限窗口之外的重放。
- 覆盖凭据类型、audience、scope、账户禁用、session 撤销、Space 授权与系统
  管理员权限分离的路由矩阵测试。
- 一次在不中断刷新 session 或正在运行的 run 的情况下轮换签名密钥的部署演练。
- 在选择 PAT、服务账号或二者兼有之前,针对第一个非交互式调用方给出产品层面
  的证据。
- 如果原生托管客户端被选为受支持的 SSO 界面，进行一次端到端的原生 OIDC 与设备流程试验。

## 若被采纳后的可能归宿

人类 Session 决策已经归入获采纳的企业身份设计。剩余机器凭据范围一旦获采纳，将会：

- 把选定的 PAT 和/或服务账号契约写入持久设计记录；
- 只把选定的机器身份与密钥轮换阶段及证据关口加入路线图；
- 同步更新部署认证、配置、支持、CLI、Desktop、Portal、数据模型与 OpenAPI
  相关文档,并与实现同步进行;
- 为 access-token claim、路由 scope、签名密钥轮换与选定的机器凭据创建聚焦实现工作；以及
- 让未选择的 PAT、服务账号、网关 token 交换与原生 OIDC/设备方向保持未实现。
