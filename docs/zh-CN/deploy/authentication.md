# 身份认证

> **翻译说明：** 本文是[英文原文](../../deploy/authentication.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
> **受众：** 运维人员 · **状态：** 当前
用户使用电子邮件地址和密码登录。BuildMax 无法发送电子邮件，以下特殊安排都源于这一点：账户由运维人员创建，领取账户或重置遗忘密码所需的一次性验证码也由人工交付。

更广泛的 Alpha 支持边界见[支持矩阵](../../../manual/support.md)。

## 创建账户

在服务器上执行两条命令，然后将验证码交给用户：

```bash
buildmax-server user create alice@example.com
buildmax-server user login-code alice@example.com
```

新账户没有密码。第二条命令只显示一次验证码：

```text
Login code for alice@example.com:

  bmxlogin_5e9e03467d578f8c248175343d627e814bc3ed10a8a05655a1c500b27dbd17cd

Valid until 2026-08-15T19:22:58+08:00, and only once.
```

通过你们已经信任的渠道发送验证码。用户在登录表单中选择“Forgot your password, or have a login code?”（忘记密码或已有登录码？），使用验证码登录，然后在账户设置中设置密码。此后就能正常登录，无需你再次介入。`--ttl` 可修改验证码的有效期，默认是一小时。

登录码是设置首个密码的唯一方式：用户凭它登录并选择自己的密码，该密码此后只存在于用户自己放置它的地方。没有任何命令可以代替他人设置密码——那会把一个密钥留在 shell 历史中，还要通过一个你随后必须信任的渠道交给对方。

这两条命令读取与服务器相同的 `server.yaml`，因此在容器中无需额外配置：

```bash
kubectl exec -n buildmax deploy/buildmax-server -- \
  buildmax-server user login-code alice@example.com
```

### 验证码的含义

验证码不是较弱的密码，而是运维人员为用户担保、通向设置密码流程的一次性凭证：

- **单次使用。** 兑换即消耗，无论随后的登录是否成功。输入错误的邮件地址也会消耗验证码，需要重新签发；这比给捡到验证码的人留下重试窗口更合适。
- **有有效期。** 默认一小时。
- **绑定一个账户。** 验证码标识用户，请求中的邮件地址必须与之匹配。它不能用于登录其他账户。
- **以 SHA-256 哈希存储。** 数据库备份不包含可直接使用的验证码，丢失后也无法读回，只能重新签发。

## System Administrator

System Administrator 是账户拥有的、针对整个**部署**的权限，与所有 Space 角色独立。Space 的 `owner`、`admin` 和 `member` 管理一个 Space 的成员及共享自动化，并不赋予服务器权限。服务器权限由单独的授权决定。

```bash
buildmax-server admin grant alice@example.com
buildmax-server admin revoke alice@example.com
```

`buildmax-server admin` 是紧急恢复路径：签发第一份授权，以及在部署失去全部管理员时通过撤销恢复访问。查看谁持有授权，以及日常的授权与撤销，通过针对运行中服务器的 `buildmax admin` 或在 Portal 中完成。

授权不会创建账户，请先运行 `buildmax-server user create`。这些命令和账户命令一样，读取服务器使用的 `server.yaml`，在容器中无需额外配置。

目前该授权允许访问 `/api/admin`：列出和检查账户、创建账户、签发登录码、禁用和启用访问、撤销会话，以及授予或撤销该角色本身。Portal 的 Administration 包含 Administrators、Accounts、Spaces、Models、Plugins、Overview 和 Audit。Administrators 区块支持列出、授予及撤销角色；`buildmax admin` 通过同一 API 提供登录后的命令行操作。该授权永远不会包含对 Space 的 Issue、Conversation、Artifact、文件或运行轨迹的访问。它们始终受 Space 成员资格保护，不属于某个 Space 的管理员无法读取其内容。

禁用账户会拒绝该账户持有的全部凭证：密码、登录码、刷新令牌、已经持有的访问令牌及 webhook 密钥。同时撤销会话、暂停其 Schedule，并取消其排队中和运行中的工作；正在运行的 Agent 会在其 worker 下一次检查取消时停止。Portal 会先展示禁用将停止哪些内容，并允许为离职人员永久退役该账户的 webhook 密钥；否则密钥会保留，以便其回来。这不是删除，不会移除任何内容；重新启用仅恢复账户状态。当一个共享 Space 的所有所有者都已被禁用时，可通过管理区 Spaces 中的 “Make owner” 或 `buildmax-server space recover-owner <space_id> <successor_email>` 将一名已启用的成员提升为所有者。

这条命令也是恢复途径，因此权限保存在数据库中，而非配置项中。无论部署拥有十名管理员还是一名也没有，其行为都相同。失去所有管理员的部署，用创建首位管理员的同一条命令即可恢复，无需保存、轮换或冒泄露风险维护紧急凭证。正因如此，命令行允许撤销最后一份授权，而 API 拒绝这样做。

授予和撤销权限会记入审计轨迹，`buildmax-server user` 执行的账户创建、密码设置和登录码签发也会记录。命令行操作记为系统操作者 `buildmax-server`：命令持有的是数据库凭证而不是会话，无法归属到具体个人。

## 登录返回的凭证

登录返回两种凭证：

| | 有效期 | 服务器端存储 | 可撤销 |
|---|---|---|---|
| **访问令牌** | 15 分钟（`access_token_ttl`） | 该 token 是签名 JWT，但它所指名的 Session 是一行记录（`auth_session`） | 是——server 每个请求都会检查该 Session，因此撤销会在 `access_token_ttl` 之内生效 |
| **刷新令牌** | 30 天（`refresh_token_ttl`） | 是，以哈希存于 `user_refresh_token`，属于该 Session | 是，立即生效 |

每个请求都携带访问令牌，并指名一个 Session（`sid`）。server 在每个已认证请求上通过同一个通道解析对应的 `auth_session` 记录，因此 `POST /api/auth/logout`、管理员的撤销和账户禁用会让已签发的访问令牌在其下一次调用时停止，而不是等到过期。刷新令牌只发送给 `POST /api/auth/token/refresh`，别无他处，返回时会被替换：每次兑换消耗提交的那个并签发下一个。

每次登录都会开启自己的 Session，并带有一个绝对寿命（`session_absolute_ttl`，默认 90 天）：越过该上限后，无论其刷新令牌轮换多频繁，该 Session 都会失效，用户需要重新登录。在笔记本上登录不会影响手机上的 Session，退出其中一个也不影响另一个。

**刷新令牌存放在哪里取决于客户端。** 原生 CLI 与 Desktop 客户端把它保存在操作系统凭证库中，并使用上文的 JSON 路由。Portal 则从不以脚本可读的形式拿到它：它通过 `POST /api/auth/portal/login` 登录，该接口只返回访问令牌，并把刷新令牌设置为一个 Secure、HttpOnly、`SameSite=Strict`、作用域限定在 `/api/auth/portal` 的 cookie。Portal 在 `POST /api/auth/portal/session`（页面加载、刷新以及遇到 401 时）用该 cookie 换取新的访问令牌，并在 `POST /api/auth/portal/logout` 清除它。这些路由要求同源 `Origin` 且不设置任何宽松的 CORS，因此 Portal 与 API 必须同源——生产环境用反向代理，本地用开发服务器的 `/api` 代理。

### 重用会结束会话

已兑换的刷新令牌再次被提交，说明存在两个副本。服务器无法判断哪个持有者合法，因此会撤销整个会话，合法用户也会被登出，并记录 `auth.refresh_reuse` 审计事件。

`refresh_rotation_grace`（默认 30 秒）是唯一例外。CLI 和 Desktop 的多个进程共享一个凭证文件，同时刷新是正常情况；在此窗口内，两者都会拿到可用令牌。增大该值也会扩大被盗令牌不被发现的时间窗口。

### 泄露的访问令牌的限度

撤销或禁用之所以能让访问令牌停止，是因为 server 会在下一个请求上检查它的 Session；但一个未经过该通道就到达用户的路由不会做这项检查——架构测试的存在正是为了让每个已认证路由都走这条通道。令牌本身仍是一个 bearer 凭据，没有按令牌的撤销列表，因此若 Session 检查被绕过，`access_token_ttl`（默认 15 分钟）仍是一个泄露令牌可用时长的上限。把它保持较短，代价只是增加刷新流量。

## 自助注册默认关闭，且没有界面

除非 `server.yaml` 设置 `allow_signup: true`，否则 `POST /api/auth/otp` 对 `intent: signup` 返回 `403`。账户通过 `buildmax-server user create` 创建，Portal 不提供注册表单。

即使设置 `allow_signup: true`，自助注册也只创建账户：新账户没有密码，也没有渠道向其拥有者发送信息，因此仍需运维人员签发登录码。这也是没有注册表单的原因。

系统不验证输入邮件地址的人是否控制该地址，这才是默认关闭注册的根本原因。仅可信网络可访问的部署可能适合开放注册；其他环境中，这会让他人冒用同事的地址。开启此选项时，服务器启动会记录警告。

## 密码

密码以带有每账户独立盐值的 argon2id 哈希存储，因此数据库导出不会提供可直接使用的密码，也无法用预计算表查询。哈希参数保存在每个哈希中，未来提高参数时会应用于新密码，而不会让现有密码失效。

唯一的规则是长度：**至少 12 个字符**，最多 1024 个。没有“一个数字加一个符号”之类的要求，因为组合规则容易促使用户选择符合规则却简短、可预测的密码。

修改密码需要当前密码。设置*第一个*密码不需要，因为刚兑换登录码的人尚无密码，这是恢复流程的最后一步。仅有会话有意不足以修改已有密码：被盗的访问令牌在其 Session 被撤销或它过期之前仍然有效，因此若允许仅凭会话就修改密码，就会让这段时间窗口变成一次持久的账户接管。

修改密码**不会**登出已有会话。如需登出，请另行撤销会话。

> **登录没有速率限制。** 密码尝试不会被限流，因此任何人可访问的服务器可能遭受在线暴力破解。12 字符的最短长度和内存密集型哈希提高了每次猜测的成本，但不能替代限流。对不可信网络开放的部署，应在前方设置速率限制器。统一限流能力已规划但尚未实现。

## 单点登录（OIDC）

部署可以让用户通过其组织已有的身份提供方经 OpenID Connect 登录。**首个支持的提供方是 Okta。**
SSO 只证明"来者是谁"；账号、会话以及每一个授权决策仍由 BuildMax 拥有——IdP 的分组或角色声明
在这里从不授予访问权限。

在 `server.yaml` 中用一个 `oidc` 块开启它：

```yaml
public_base_url: https://buildmax.example.com   # 必填；回调 URI 由它构建
oidc:
  enabled: true
  display_name: Okta                # 用于 Portal 登录按钮的标签
  issuer: https://example.okta.com  # 唯一的 URL 信任根；必须为 https
  client_id: 0oaExampleClientId
  provisioning: jit                 # jit（默认）或 existing_only
  allowed_email_domains:            # jit 下必填且非空
    - example.com
  session_max_age: 12h              # SSO 会话上限；默认 12h
```

客户端密钥应在部署时注入，而非写入文件：

```bash
BUILDMAX_OIDC_CLIENT_SECRET=…   # 从不被返回、记录日志或交给 worker
```

**一次登录如何变成账号。** 首次已验证登录时，BuildMax 按精确的 `(issuer, subject)` 对把 IdP
身份关联到账号——即使邮箱变化该值也不变。若尚无链接，则关联一个邮箱与已验证地址匹配的运维创建账号；
否则在 `provisioning: jit` 下，当已验证邮箱的域名在 `allowed_email_domains` 中时创建账号（及其个人
Space）。空的域名列表意味着*不为任何人*预配，而非所有人。`provisioning: existing_only` 从不创建账号
——由运维预配，SSO 仅做认证。已关联到另一身份的邮箱会被拒绝以待运维核对，绝不静默迁移。

**与 SSO 并存的原生登录。** `local_login` 独立于 SSO 管控密码与登录码登录：

- `all`（默认）——所有账号仍可原生登录。
- `system_admins`——仅系统管理员可以，作为 IdP 不可达而其他人使用 SSO 时的应急通道。
- `off`——无原生登录。仅在已配置 SSO 时才合理；若没有任何人能登录，服务器会在启动时告警。

**配置 Okta 应用。** 创建一个 OIDC **Web** 应用（机密客户端，`client_secret_basic`）。将其登录
回调 URI 设为 `<public_base_url>/api/auth/oidc/callback`。授予 `openid`、`email`、`profile`
scope，并分配应当访问此部署的人员或分组。把 issuer、client ID 与 client secret 填入上面的配置。

系统管理员可在 `GET /api/admin/users/{user_id}/identities` 查看某人的已关联身份，并且**在账号被禁用时**
用对应的 `DELETE` 移除其一。解绑仅移除绑定关系——绝不移除账号、其成员身份或历史——因此运维可以更正不匹配
并重新启用账号以进行一次全新的首次关联。

对 IdP 自身的端到端资格验证（固定的 Okta 租户、密钥与密钥轮换演练、RP 发起的登出）仍在完成中；上述登录、
关联与管理界面均已就绪。

## 尚未具备的能力

没有第二认证因素，也没有自助恢复：忘记密码需要向运维人员索取登录码，或在已配置 SSO 时通过 SSO 登录。系统不验证原生邮件地址是否属于使用者；地址在这里是标识，不是证明——这也是面向组织外部人员的部署需要在前方使用上述 OIDC 提供方的原因之一。

登录尝试没有限流，见[密码](#密码)一节的说明。

System Administrator 可在 Portal 的账户详情页或通过
`GET /api/admin/users/{user_id}/sessions` 查看有效登录 Session，并通过
`DELETE /api/admin/users/{user_id}/sessions/{session_id}` 撤销单个 Session，
或通过集合的 DELETE 路由撤销全部 Session。列表包含 Session ID、平台、认证
方式、创建时间、最近可见时间与绝对到期时间；最近可见时间经过节流，并非持续
的设备在线信号。目前没有用户自助 Session 管理页，也没有专门的管理 CLI
Session 命令。撤销一个 Session 会停用它的 refresh token 并将 `auth_session`
记录标记为已撤销，因此该 Session 下已签发的 access token 会在其下一个请求时
停止，而不是等到过期。

## 其他凭证

| 凭证 | 配置 | 保护范围 |
|---|---|---|
| **JWT 密钥** | `jwt_secret` / `BUILDMAX_JWT_SECRET` | 用于签署全部用户访问令牌，必填。使用 `openssl rand -hex 32` 生成，在部署时注入，不要提交到仓库。 |
| **Run 令牌** | 每次运行签发，通过 `BUILDMAX_RUN_TOKEN` 交付 | 保护 `/api/worker/*` 路由。以 JWT 密钥签名，标识一次运行的用户、Space 和 Task，仅授权该次运行。这不是运维配置，而是调度器为每个调度的运行签发的凭证。有效期为 `worker.run_token_ttl`，不支持续期，因此必须长于最长运行时间。 |
| **Webhook 密钥** | 通过 API 为每个用户创建 | 保护入站 `POST /api/webhook`。以 SHA-256 哈希存储，明文仅在创建时显示一次。见 [Webhook 参考](../reference/webhook.md)。 |

轮换 JWT 密钥会立即使所有已签发的访问令牌和 Run 令牌失效。刷新令牌不受影响，因为它们是存储行而非签名，所以客户端无需任何人重新登录即可恢复：Portal、CLI、Desktop 和 Remote Control 会话都会把 server 返回的 401 视为信号，兑换一次刷新令牌后重试。正在执行的运行会失去其 Run 令牌，无法再上报，并在几分钟后被存活回收器结算为 `FAILED`；如果这些运行很重要，请在轮换前先排空 worker。同一时间只有一个签名密钥，因此轮换会立即生效，而不会与旧密钥并存一段时间。由于刷新令牌不受影响，该密钥不是让所有人登出的手段——需要时请撤销 Session。[凭证轮换手册](credential-rotation.md)给出了该密钥及其他所有部署凭证的轮换流程和实测影响。

同样，Run 令牌是签名而非数据库行，无法在过期前撤销。它由作用域（单次运行）和运行状态约束：推理路由拒绝已不在执行的运行。

Portal 的账户详情页和 Admin API 都支持撤销单个或全部 Session。单 Session
撤销会核对其所属账户，并保留该账户的其他登录链。因为 server 会在每个请求上
检查令牌的 Session，被撤销 Session 的 access token 会在其下一次调用时停止，
而不是等到过期。这些操作不需要直接访问数据库。

## 报告问题

按照 [SECURITY.md](../../../SECURITY.md) 私下报告身份认证或授权漏洞，不要创建公开 Issue。
