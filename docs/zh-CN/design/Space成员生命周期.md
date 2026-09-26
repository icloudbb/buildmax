# Space 成员生命周期

> **翻译说明：** 本文是[英文原文](../../design/space-membership-lifecycle.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。

## 目录

- [状态](#状态)
- [1. 决策](#1-决策)
- [2. 产品目标](#2-产品目标)
- [3. 当前基线](#3-当前基线)
- [4. 主要缺口](#4-主要缺口)
- [5. 范围内事项](#5-范围内事项)
- [6. 范围外事项](#6-范围外事项)
- [7. 权限矩阵新增内容](#7-权限矩阵新增内容)
- [8. 后端计划](#8-后端计划)
- [9. 前端计划](#9-前端计划)
- [10. 验证](#10-验证)
- [11. 风险](#11-风险)
- [12. 开放问题](#12-开放问题)
- [13. 建议的第一个 PR](#13-建议的第一个-pr)

## 状态

- roadmap_priority：路线图 R3 候选版本资格验证的已实现基础
- status：`implemented` —— §5.1（邀请）、§5.2（角色变更）、§5.3（所有权转让）和 §5.4（Space 范围的访问恢复）均已端到端交付：`internal/core/space`、`internal/service/space`、`internal/server/handlers/space`、`internal/infra/db`，以及 §9 中的 Portal 界面（Space → Members：邀请、待处理列表、角色选择器、转让确认、登录码；Account → Invitations：列表与接受）。§12 的四个开放问题均已决定。§5.5 记录移除的效果与所有者均已禁用时的恢复，随账户停用生命周期一同交付
- follows：[space-governance.md](./Space治理.md)、[system-administration.md](./系统管理.md)
- roadmap：[../ROADMAP.md](../ROADMAP.md)
- created_at：`2026-08-30`

## 1. 决策

**BuildMax 不支持自助注册。**`allow_signup` 仍然是面向小型或受信部署的可选逃生口，代码中已经说明了它为何必须默认关闭：系统无法验证填写某个邮箱地址的人是否真正掌控该地址，因此在可公网访问的服务器上开放注册，只会让人冒领同事的邮箱地址（`internal/service/identity/account.go`，`ErrSignupClosed`）。本文档不重新讨论这一决策，也不会新增任何需要出站邮件通道的内容——BuildMax 按设计没有邮件通道（`internal/core/identity/login_code.go`）。

**账户是否存在和 Space 成员身份是两种不同的权威，本文档让二者保持分离，而不是把它们混进同一条邀请流程里。**

- **创建账户是部署身份领域的职责。**运维人员可以使用 `POST /api/admin/users` / `buildmax-server user create`（见 [system-administration.md](./系统管理.md)），OIDC 部署也可以使用[企业身份与访问](./企业身份与访问.md)中的有界 JIT 路径；本文档两者都不新增。如果一个 Space 范围的调用也能铸造账户，就会再出现一处决定谁能在部署中存在的地方，并最终偏离身份服务的规则（域名策略、个人 Space、禁用、审计）——这正是 [AGENTS.md](../../../AGENTS.md) 的权责边界规则要防止的。
- **把已有账户带入某个 Space 是该 Space 所有者（或管理员）的职责。**本文档后续所说的"邀请"正是这个意思：想让某人加入自己 Space 的所有者或管理员按邮箱地址发出请求，只有当该邮箱已经拥有账户时，对方才会被加入——且不同于今天的做法，加入需要等待对方接受。

直接说明其后果：**想邀请一个从未使用过 BuildMax 的人的 Space 所有者，无法独自完成这件事。**运维人员必须先创建账户，或者启用的 OIDC JIT 策略在该用户首次成功企业登录时创建账户；之后所有者才能邀请由此产生的邮箱地址。本文档认为这种分离才是正确的形状，而不是未完成的工作——原因见 §4.1 和 §6，那里也说明了若要在 Space 边界内消除这一限制需要付出什么代价。

这种拆分换来的好处是实实在在的：因为 Space 范围的邀请永远不能创建账户，它也就永远不需要决定该给持有账户的人发放什么凭证——一旦账户创建成为别人的职责，本文档初稿中为防止 Space 所有者为陌生账户铸造登录凭证而设计的那套机制就完全不再需要。更简单、更不容易出错，胜过少走一步人工操作。

已经交付的 OIDC JIT 路径如今验证了这一边界。它是在首次断言时由 IdP 驱动创建账户的第二条路径，与运维人员手动创建并存，而不是取而代之。由于 §5.1 只会问"这个邮箱是否已经有账户"，从不追问"是谁创建的"或"如何创建的"，OIDC 无需改变本文档即可交付——SSO 预配的账户一旦存在就可以被邀请，和运维人员创建的账户毫无二致。而如果 Space 邀请也能创建账户，就会生出第三种身份预配观点；从一开始就把账户权威与成员权威分开，能彻底避免这个问题。

在这种拆分之下，仍有三件事没有解决：

- **"添加"一个已有用户是即时且未经确认的。**`AddMember` 只有在邮箱地址无法解析时才会拒绝（`internal/service/space/service.go:111`，`ErrUserDoesNotExist`）；一旦能够解析，对方就会立即被加入，没有待处理状态、没有接受动作，也没有拒绝的机会。所有者一旦输错地址，就会在不知情的情况下把 Space 访问权授予了一个陌生账户。
- 成员的角色无法在不删除再重新添加的情况下变更，这会丢失 `created_at`，并产生一对 `space.member_removed` / `space.member_added` 事件，读起来像是一次离开又回归，而不是一次晋升。
- 所有权无法转移给其他成员，因此 Space 永久绑定在创建者身上；一名被锁定的成员只能依赖部署中某处存在的 `system_admin` 授权，即便本该由自己 Space 的所有者出手相助才是最自然的做法。

本文档把这三段旅程——邀请（限定于已有账户）、角色变更、所有权转让——加上成员范围的访问恢复，设计成 `internal/core/space` 上小型、朴素、各自只有一处实现的扩展，而不是一个新子系统。Space 级别的*审批工作流*（某项敏感操作执行前必须由他人签字确认）仍按 [space-governance.md](./Space治理.md) §6 的决定排除在范围之外——该决定本文档不再重新讨论。

## 2. 产品目标

Space 所有者应当能够日常管理自己 Space 的成员关系，除了引入一个真正从未接触过 BuildMax 的人之外，不必依赖 `system_admin` 做任何事：

- 邀请一个已有账户加入 Space，且对方能事先看到邀请并可以拒绝；
- 在不抹去历史记录的情况下修正角色；
- 在自己离开时把 Space 交给别人；
- 解锁自己 Space 中被锁定的成员。

而且这四项操作都应当留下与现有的成员添加、移除审计轨迹同样的记录——不应出现某些成员变更被记录、另一些却没有被记录这种新的、可见的不一致。

## 3. 当前基线

后端锚点：

- `internal/core/space/space.go` 中的角色定义与成员存储契约；
- `internal/core/space/policy.go` 中的角色/操作判定；
- `internal/service/space/service.go` 中的成员命令：`AddMember`（仅限 member 角色，目标必须已有账户，即时添加）与 `RemoveMember`（所有者不能移除自己）；
- `internal/server/handlers/space/spaces.go` 中的 Space HTTP 路由；
- 完全归属于 `system_admin` 范围的账户创建与凭证发放：`POST /api/admin/users`、`POST /api/admin/users/{user_id}/login-code`（`internal/server/handlers/admin/admin_users.go`），以及后者背后的一次性登录码原语 `internal/core/identity/login_code.go`；
- [system-administration.md](./系统管理.md) §6 和 §8 中的账户禁用与锁定恢复设计——本文档不会重复它，那是 `system_admin` 在部署范围内行动的答案，不是 Space 所有者在自己 Space 内行动的答案；
- `internal/core/audit/audit.go` 中的审计轨迹及其操作命名（`SpaceMemberAdded`、`SpaceMemberRemoved`，以及二者确立的命名模式）。

当前的操作模型（`internal/core/space/policy.go`）：

- `ActionManageSpaceMembers`——仅限所有者，目前涵盖添加和移除；尚不存在用于变更角色或转让所有权的 `Action`。
- 不存在用于发放登录码的操作，因为该能力本身在部署范围以下并不存在。

**用户可以同时属于多个 Space，这是常见情况，而不是边缘情况。**`space_member` 只在 `(space_id, user_id)` 上有唯一索引——单独的 `user_id` 上没有唯一索引（`internal/infra/db/space.go:53-54`）。`CreateUser` 会为每个账户创建恰好一个个人 Space（`personal_for_user_id` 是带唯一索引的列，`internal/infra/db/user.go:194-216`），此外用户还可以拥有或被加入任意数量的普通 Space；`ListSpacesByUser` 会返回其全部，Portal 的 `SpaceContext`（`portal/src/contexts/SpaceContext.tsx`）是一个真正可用的 Space 切换器，而不是占位实现。§5 中的任何流程都不需要为"已经属于别的 Space 的受邀人"做特殊处理：接受邀请只是多新增一行 `space_member` 记录，绝不会产生冲突；唯一始终保持每个用户恰好一条的成员关系是个人 Space，本文档中没有任何路径会创建、删除或转让它。这也意味着单个账户可以同时持有来自多个互不相关的 Space 的若干待处理邀请，每一条都可以独立接受或拒绝——正因如此，§5.1 中的接受流程是针对调用者的整份列表设计的。

## 4. 主要缺口

### 4.1 没有账户的人没有邀请路径

`AddMember` 要求 `s.Users.UserByEmail` 能够解析成功（`internal/service/space/service.go:107-113`）。因此 Space 所有者无法引入任何尚未被 `system_admin` 创建的人。在 `system_admin` 授权很少的部署中，这是真实存在的摩擦；§1 说明了本文档为何选择接受这一点而不是彻底消除它——若要消除它需要付出的代价见 §6。

### 4.2 添加已有用户是即时的，而不是一次邀请

称之为 `AddMember` 是准确的：它就是"添加"。没有待处理状态、没有接受动作，被添加的人也无从提前得知或加以拒绝。事后会留下一条审计记录（`SpaceMemberAdded`），但事前什么都没有。这正是 §5.1 要弥补的缺口。

### 4.3 没有角色变更

`Allows`（`internal/core/space/policy.go`）区分 `owner`、`admin` 和 `member`，但 `internal/service/space` 中没有任何东西能让成员在这些角色之间转换。唯一的角色变更途径是先移除再重新添加，而这样做会：

- 要求目标仍然拥有账户，并且仍然愿意被重新邀请回来；
- 产生两条审计事件，读起来像是一次离开加一次重新加入，而不是一次晋升；
- 在短暂的时间窗口内，Space 中完全没有这个人的任何记录。

### 4.4 没有所有权转让

`RemoveMember` 会拒绝所有者移除自己（`ErrCannotRemoveSelf`），这是正确的——但对于该检查本应防止的那种局面，却没有任何应对路径：所有者要离开，却没有别人能够接手。今天，改变一个 Space 所有权归属的唯一方式是直接操作数据库，而这正是 [system-administration.md](./系统管理.md) §6 想要为账户授权消除、却尚未覆盖 Space 所有权的那类操作。

### 4.5 访问恢复仅限部署范围

`LoginCodeStore.CreateLoginCode` 只能通过 `POST /api/admin/users/{user_id}/login-code` 访问，而该接口需要 `system_admin` 权限（`internal/server/handlers/admin/admin_users.go:200`）。眼看着自己 Space 的成员被锁定在外的所有者，没有属于自己的恢复路径——他们必须去找一个 `system_admin`，而在小型部署中，除了当初运行过一次引导命令的人之外，可能根本不存在别的 `system_admin`。

## 5. 范围内事项

### 5.1 Space 范围的邀请

新增 `POST /api/spaces/{space_id}/invitations`，接收一个邮箱地址和一个可选角色（member 或 admin——绝不能是 owner；所有权为何要通过一个独立、显式的操作转移，见 §5.2）。它会**取代**当前的即时添加路由 `POST /api/spaces/{space_id}/members`，而不是与其并存——按照 [AGENTS.md](../../../AGENTS.md) 的要求，Alpha 阶段意味着要一次性在所有地方修正错误的形状，而两条都能添加成员的路由，正是 §1 所反对的重复权威在下一层的翻版。

授权使用新的 `ActionInviteSpaceMember`，而不是复用 `ActionManageSpaceMembers`，因为两类调用者并不相同：**所有者可以邀请 `member` 或 `admin`；管理员只能邀请 `member`。**如果允许管理员邀请另一位管理员，就等于让他们能给 Space 配备与自己平级的同僚，而角色变更与所有权转让恰恰是成员管理仍然保留给所有者的最后两件事——它们仍归 `ActionManageSpaceMembers` / `ActionChangeMemberRole` 管辖，且二者都仍然仅限所有者，因此这不会让管理员绕开这层限制。

行为：

- 邮箱地址必须能解析到一个已有账户（`coreidentity.UserStore.UserByEmail`），这与今天 `AddMember` 的要求完全一致。解析失败时，调用会被新的 `ErrInviteeAccountRequired` 拒绝，其错误信息会说明下一步该怎么做——请 `system_admin` 创建账户（`POST /api/admin/users` / `buildmax-server user create`），然后再邀请该地址。这正是 §4.1 中的缺口，本文档选择接受而不是消除它；见 §1。
- 当解析成功、且该账户尚不是成员时，调用只会创建一条**待处理**的 `space_invitation` 记录，此外不做任何事——不发放凭证、不创建会话、不产生任何账户级别的副作用。这是刻意为之：本节早期的草案曾让每一次邀请都能创建账户并为其铸造登录凭证，这会让 Space 所有者只需"邀请"部署中的*任意*地址，就能获得一个可用的登录方式，无论该地址是否已有账户。把邀请限定在已有账户范围内——在 §1 中决定——从结构上消除了这一风险，而不是靠本节不断维护一条规则来防范它。
- 若无人接受，待处理记录会在 `InvitationTTLDefault = 72 * time.Hour` 后过期。这是邀请这个提议本身的属性，而不是某个凭证的属性——本流程中不会发放任何凭证；之所以是三天而不是更短的窗口，是因为邀请是发出后等待接收者下次打开 Portal 时再处理的，而不是要求在发出邀请的同一次交互中就完成。
- 邀请的发现完全在应用内完成：`GET /api/invitations`（需要认证、不带 Space 参数——它回答的是"有哪些邀请正等着*我*"）会列出调用者可以接受的邀请，调用者凭借自己已经获得的会话即可查看——无论是用密码登录，还是使用某个有权限的人已经发给他们的登录码。BuildMax 没有邮件通道，这个流程也不需要——没有什么需要带外投递，因为受邀人本来就有登录的办法。如果所有者希望对方更快注意到邀请，仍然可以用 Space 现有的任何沟通方式去提醒；产品本身并不依赖这一点。
- `POST /api/invitations/{id}/accept` 用于激活一条邀请。它不需要任何代码——身份在获得会话时就已经确立，因此接受动作的授权依据是"这是我自己的待处理记录"，而不需要再次证明任何事情。
- 撤回一条待处理邀请（`DELETE /api/spaces/{space_id}/invitations/{id}`）同样使用 `ActionInviteSpaceMember`——谁能发出邀请，谁就能撤回它。

这样就弥补了 §4.2 的缺口，同时不会重新讨论 §1 已经解决的账户创建问题，也绝不会让某个 Space 的邀请变成一条触及别的 Space（或尚无任何 Space）已经拥有的账户的途径。

### 5.2 角色晋升与降级

在 `internal/core/space/policy.go` 中新增仅限所有者的 `ActionChangeMemberRole`，以及接收目标角色的 `PATCH /api/spaces/{space_id}/members/{user_id}`。

规则：

- 所有者可以把一名 member 设为 `admin` 或 `member`，把一名 admin 设为 `owner` 或 `member`。
- 把目标设为 `owner` 会在同一事务中把调用者本人降为 `admin`——见 §5.3，这*正是*所有权转让，只不过以一个端点而不是两个端点的形式暴露出来，因为"把某人提升为 owner 的同时自己继续留任 owner"是本文档没有为其定义任何含义的状态。
- 最后一名所有者不能在不先转让所有权的情况下自我降级。这是 [system-administration.md](./系统管理.md) §6 中适用于最后一个 `system_admin` 授权的规则在 Space 范围内的版本：**API** 会拒绝让某个 Space 一个所有者都不剩，就像它会拒绝让某个部署一个 `system_admin` 都不剩一样。

### 5.3 所有权转让

不是一个独立的端点——§5.2 中那个把 `role: owner` 作为目标、指向当前某个 admin 或 member 的 `PATCH` 请求就是全部机制。本节的作用是记录这个端点所做出的决定：转让是**单方面且立即生效**的，不需要接收方成员的同意。

这是一个经过取舍、已经做出而非搁置的决定：它与今天 `AddMember` 的运作方式一致（是所有者单方面的行动，而不是双方的握手确认），也避免了在同一份文档中，除了 §5.1 的邀请之外再构建第二套待处理状态机制。它是可逆的：新所有者可以把所有权转回去，或者降级前任所有者，就像任何所有者都可以对其他任意 admin 做的那样。为何这一点被直接决定而不是留作开放问题，记录见开放问题 1。

### 5.4 Space 范围的访问恢复

新增仅限所有者的 `POST /api/spaces/{space_id}/members/{user_id}/login-code`，由 `ActionManageSpaceMembers` 授权——不需要新的 `Action`，因为帮助成员重新登录和添加、移除成员一样，都是成员管理行为。它会先检查目标是否是调用者所在 Space 的成员，然后调用与部署范围管理路由相同的 `LoginCodeStore.CreateLoginCode`。

这并不会取代 [system-administration.md](./系统管理.md) 中的 `system_admin` 路由——那条路由依然存在，依然在部署范围内生效，用来恢复那些在自己 Space 中既没有共同所有者、也没有任何 admin 的所有者。这条新路由只是在"某个成员在一个原本健康的 Space 中被锁定"这一常见情形下，消除了对 `system_admin` 是否存在的依赖。

这也是本文档中唯一会发放登录码的地方——相较于 §5.1 初稿，这是一个刻意收窄、重新划定的边界：这里的目标已经是调用者自己 Space 中已知的成员，因此不存在所有者为陌生账户铸造凭证的问题。

### 5.5 移除的效果与所有者恢复

移除成员会硬删除其唯一的 `space_member` 记录。离开的来源记录保存在 `space.member_removed` 审计事件中，而不是一条保留或软删除的记录里——那样的记录还会与 `(space_id, user_id)` 上的唯一索引冲突。之后的邀请是一次新的加入，带有新的 `created_at`；它不恢复任何东西，也不会复活该成员已暂停的 Schedule 或已取消的运行。这是[系统管理](系统管理.md) §8 所述两条轴中的破坏性一轴；账户禁用是可逆的一轴，会保留每一条成员记录。

移除会收回此人在该 Space 中驱动工作的权限，包括由持久 Schedule 或 Workflow 而非请求发起的工作：

- 其在该 Space 中已启用的 Schedule 会在下一个到期时间以 `pause_reason = creator_not_member` 暂停；
- 其在该 Space 中待处理和正在运行的 TaskRun 会经由派发和 worker 拉取闸门，或资格对账器的下一次扫描，以 `cancel_reason = creator_not_member` 结束为 `CANCELED`；
- 其在其他 Space 中的工作和访问不受影响；并且
- Task、结果、Artifact、轨迹和审计记录都留在该 Space 中，其余成员仍可继续读取。

移除本身不触发任何清理；由各道闸门和每分钟运行的对账器收敛。执行资格规则、检查点及其缺口见[系统管理](系统管理.md) §8.2。

所有权通常在所有者离开前按 §5.3 转移。如果一个共享 Space 的每一位已记录所有者都已被禁用，就没有所有者能完成这一步，因此系统管理员可以在狭窄条件下恢复该 Space：每一位已记录所有者都已禁用、继任者已经是已启用的成员、该 Space 不是个人 Space，并且不创建任何成员关系。该操作复用 §5.3 的转移，不给管理员任何内容访问权，并记录 `space.ownership_recovered`。它可通过 `PUT /api/admin/spaces/{space_id}/owner`、Portal 管理区 Spaces 中的 “Make owner”，以及在公共 Server 或 IdP 不可用时使用的紧急救援命令 `buildmax-server space recover-owner <space_id> <successor_email>` 访问。它绝不转移一个仍有所有者能登录的 Space。见[系统管理](系统管理.md) §8.4。

## 6. 范围外事项

- **由 Space 发起账户创建。**这是 §1 的核心决定：无论这会给引入从未接触过 BuildMax 的人带来多大摩擦，邀请都绝不会创建账户。让 Space 范围的调用创建账户这一替代方案，在本文档中被起草又被否决，原因正是它无法回避"该发放什么凭证"这个问题，而这个问题的任何答案，要么让 Space 所有者获得任意地址的可用登录方式，要么就是重新发明了本已归属 `system_admin` 的账户认领语义。一个同时拥有目标 Space 所有权的 `system_admin` 本就同时持有这两种授权，可以前后接续地完成这两步操作；这对于单体自托管部署的运维者而言是被接受的路径，而不是一个缺口。
- **Space 审批工作流。**已由 [space-governance.md](./Space治理.md) §6 决定排除在范围之外，本文档不再重新讨论。敏感操作的审批回路（所有者的操作必须经他人确认才能生效）是一个与"所有者本就被信任可独立执行的操作的生命周期"完全不同、也更庞大的设计。
- **要求目标方接受的所有权转让。**已被否决——理由记录见开放问题 1。
- **任何用于 Space 邀请的带外投递机制。**§5.1 不需要——邀请的目标是一个本就能够自行完成身份验证的账户，因此没有什么需要额外交付的东西。这不同于、也不应与 [system-administration.md](./系统管理.md) 中 `system_admin` 的登录码投递相混淆，后者在每次创建账户时依然适用，且依然需要运维者带外投递登录码。
- **批量邀请、CSV 导入，或由邀请驱动的账户创建。**目前尚无需求证据；应基于观察到的实际部署需求来构建，而不是凭空推测——[space-governance.md](./Space治理.md) §11 对自定义角色所持的克制态度同样适用于此。部署范围的 OIDC JIT 预配现已独立交付；§1 记录了它为何无需改变这一生命周期：§5.1 从始至终只关心"这个账户是否存在"。
- **自定义角色，或 owner/admin/member 之外的任何角色。**与 [space-governance.md](./Space治理.md) §6 保持一致，未作改动。
- **超出最低限度的跨 Space 邀请接受界面。**这里的 Portal 工作范围限定于 §9 所列内容；如果某些 Space 最终会同时存在多条未处理邀请，一个更丰富的邀请收件箱可以作为后续工作。

## 7. 权限矩阵新增内容

扩展 [space-governance.md](./Space治理.md) §7 中的矩阵：

| 操作 | 所有者 | 管理员 | 成员 |
|---|---:|---:|---:|
| 以 `member` 角色邀请已有账户 | 是 | 是 | 否 |
| 以 `admin` 角色邀请已有账户 | 是 | 否 | 否 |
| 撤回一条待处理邀请 | 是 | 是（`ActionInviteSpaceMember`） | 否 |
| 接受自己的邀请 | ——（任何已认证的受邀人） | — | — |
| 变更某个成员的角色 | 是 | 否 | 否 |
| 转让所有权 | 是 | 否 | 否 |
| 为某个 Space 成员发放登录码 | 是 | 否 | 否 |

邀请是管理员唯一持有的成员管理操作，且仅限于 `member` 角色——这解决了开放问题 2。角色变更、所有权转让、发放登录码仍然仅限所有者，这与 `ActionManageSpaceMembers` 对添加和移除本就仅限所有者保持一致：这三项操作都不会让管理员触及比自己权力更大的角色，而邀请另一位管理员则会。

## 8. 后端计划

### M1. 邀请存储

- 在 `internal/core/space` 中新增 `Invitation` 类型及其存储方法：针对已解析的用户 id 创建待处理记录、列出某个 Space 的待处理记录、列出某个用户的待处理记录（支撑 `GET /api/invitations`）、按 id 接受、按 id 撤回。
- 按照 `docs/contribute/architecture/data-model.md` 中新建 `xxxRow` 的规则新增一张表：单数命名（`space_invitation`），字段包括 Space id、被邀请用户 id、角色、邀请人、状态、`created_at`，以及由 `InvitationTTLDefault` 据此计算出的过期时间。不含任何代码或代码哈希——§5.1 中从不发放登录码。
- 在 `internal/service/space` 中新增 `ErrInviteeAccountRequired`，在 `UserByEmail` 找不到结果时返回，其中会指明后续该走的 `system_admin` 路径，而不是复用今天 `AddMember` 使用的那条简单的 `ErrUserDoesNotExist` 消息。
- 在 `internal/core/space` 中，与 `Invitation` 放在一起新增 `InvitationTTLDefault = 72 * time.Hour`——这是一个 Space 成员管理层面的决定，而不是任何身份原语的属性，因为这里的任何逻辑都不涉及 `LoginCodeStore`。
- 按照 §5.1 的决定，`POST /api/spaces/{space_id}/members` 和 `AddMember` 被直接移除，而不是标记为弃用——完全由下面的邀请路由取代。

### M2. 邀请路由

```text
POST   /api/spaces/{space_id}/invitations      owner or admin, member role only for admin — §5.1
GET    /api/spaces/{space_id}/invitations      owner or admin, list this space's pending invitations
DELETE /api/spaces/{space_id}/invitations/{id} owner or admin, revoke before acceptance
GET    /api/invitations                      authenticated, lists the caller's own pending invitations
POST   /api/invitations/{id}/accept          authenticated, no code — §5.1 explains why
```

### M3. 角色变更与所有权转让

- 在 `internal/core/space/policy.go` 中新增 `ActionChangeMemberRole`。
- 在 `internal/service/space/service.go` 中新增 `SetMemberRole`，附带 §5.2 中的"最后一名所有者"防护。
- 在 `internal/server/handlers/space/spaces.go` 中新增 `PATCH /api/spaces/{space_id}/members/{user_id}`。
- 新增审计操作：`space.member_invited`、`space.invitation_accepted`、`space.invitation_revoked`、`space.invitation_expired`、`space.member_role_changed`、`space.ownership_transferred`——沿用 `internal/core/audit/audit.go` 中已有的 `SpaceMemberAdded` / `SpaceMemberRemoved` 命名方式。尽管 §5.3 中所有权转让是通过同一次调用实现的，转让仍然拥有独立于角色变更的专属操作，因为一次"所有权是否发生过转移"的调查，不应该依赖从两条 `member_role_changed` 记录中去推断。
- `space.invitation_expired` 与 [space-governance.md](./Space治理.md) §5.4 为登录失败设定的模式有所不同——登录失败之所以静默，是因为它无法说明行为发起者是谁，而一条邀请在任何人对其采取行动之前，就已经指名了一个具体的、已解析成功的账户，因此无论结果如何都值得记录。这条记录是惰性写入的：只有当有人对一条已经超过 `InvitationTTLDefault` 的记录尝试接受时才会写入，而不是靠一次扫描去找出那些无人问津就超时的记录——从未有人试图接受的邀请不会产生任何事件，就像一扇没被打开过的门不会发出声音一样。

### M4. Space 范围的登录码

- 在 `internal/service/space/service.go` 中新增 `IssueMemberLoginCode`，检查目标的成员身份后调用 `LoginCodeStore.CreateLoginCode`。
- 新增 `POST /api/spaces/{space_id}/members/{user_id}/login-code`。
- 新增审计操作 `space.member_login_code_issued`，与现有的 `user.login_code_issued` 区分开来，这样一来，阅读该 Space 自身轨迹的人（按 [space-governance.md](./Space治理.md) §5.5 仅限所有者）无需拥有 `system_admin` 对部署级轨迹的可见权限，也能看到这条记录。

## 9. 前端计划

### M1. 邀请流程

在 Space 设置的成员列表中，把目前即时生效的"Add member"表单替换为"Invite"：仍然是邮箱加角色的输入，但查找失败时会原地显示 `ErrInviteeAccountRequired` 的提示，而不是一条普通的校验错误——明确告诉所有者或管理员该去找谁（`system_admin`），而不是让他们自己去猜为什么什么都没发生。成功后没有任何内容需要复制或投递；该行会直接移动到带有"撤回"操作的待处理邀请区域。

另有一个独立的"Invitations"页面（数据来自 `GET /api/invitations`），展示当前登录用户被邀请加入的所有 Space，并允许对每一条分别接受或忽略。

### M2. 角色变更界面

用每一行成员上的角色选择器，取代隐性的"移除再重新添加以变更角色"的变通做法；该选择器仅所有者可用，对其他人则禁用并附带说明文字——沿用 [space-governance.md](./Space治理.md) §5.3/§9 中已有的禁用态模式。

### M3. 所有权转让确认

尽管 §5.3 让转让在后端是单方面生效的，但界面上仍会在"将其设为 owner"这一操作前专门加上一个不易误触的独立确认步骤，与普通的角色下拉框区分开来——后端"立即生效即不可逆"的特性，恰恰是这里应当增加而不是减少界面摩擦的理由。

### M4. 成员登录码

在"移除"旁边为每个成员提供"发放登录码"操作，仅所有者可见，使用与管理端页面相同的一次性展示模式。

## 10. 验证

后端：

```sh
./make test ./internal/core/space ./internal/service/space ./internal/server/handlers/space ./internal/infra/db
```

前端：

```sh
cd portal && npm run build
```

完整：

```sh
./make test
```

手动验证场景：

1. 所有者邀请一个尚无账户的邮箱地址；调用被 `ErrInviteeAccountRequired` 拒绝，其中指明了 `system_admin` 路径，且没有创建任何记录。
2. 一名 `system_admin` 创建该账户并单独发放一个登录码；随后所有者成功邀请同一个邮箱，产生一条待处理邀请，此外没有其他任何事情发生。
3. 受邀人用自己的凭证登录（密码，或场景 2 中的登录码），并在 `GET /api/invitations` 中看到该待处理邀请；接受后成员身份即刻生效。
4. 同一个尚未被接受的邮箱又被第二个无关的 Space 邀请；受邀人下次登录时会看到两条待处理邀请，并可以分别独立接受。
5. 所有者在一条待处理邀请被接受之前将其撤回；该邀请不再出现在该用户的 `GET /api/invitations` 结果中。
6. 一条待处理邀请超过 `InvitationTTLDefault` 仍未被处理；下一次针对它的接受尝试会被拒绝，并记录一条 `space.invitation_expired`。
7. 所有者把一名成员改为 admin，再改回 member；每次变更各产生一条审计记录，不出现 `member_removed`/`member_added` 这样的成对事件。
8. 所有者把所有权转让给一名 admin；调用者变为 admin，目标变为 owner，记录一条 `space.ownership_transferred`，新所有者可以立即将其转回。
9. 唯一的所有者不能在未先转让所有权的情况下自我降级。
10. 所有者为自己 Space 中一名被锁定的成员发放登录码；而另一个 Space 的成员，或本 Space 的管理员，都无法执行该操作。

## 11. 风险

- **把账户创建重新引入 Space 邀请路径。**§1 和 §5.1 记录了这一方案为何被起草又被否决：它无法回避"该发放什么凭证"这个问题，而任何答案要么让 Space 所有者能为任意地址铸造可用登录方式，要么就是重新发明了 `system_admin` 的账户认领语义。未来任何让 `POST /api/spaces/{space_id}/invitations` 开始创建账户的改动，都会重新打开这个问题，需要接受本文档给予它的同等审视，而不能更少。
- **某个邮箱是否存在账户，会向任何有权发出邀请的人可见。**`ErrInviteeAccountRequired` 与一条被成功创建的待处理邀请之间的区别，会告诉 Space 所有者或管理员某个任意地址是否拥有 BuildMax 账户，这并不是新出现的信息泄露——今天 `AddMember` 的 `ErrUserDoesNotExist` 已经做出了同样的区分——但值得明确指出而不是保持隐含，因为本文档正是为这一边界安排永久归宿的地方。若要消除它，就意味着对一个不存在的地址发出邀请，要么悄无声息地什么都不做，要么谎称已经成功，这两种做法对本文档所面向的、常见的非对抗性场景来说都更糟糕。
- **未经接受的所有权转让让新所有者感到意外。**已决定不增加确认步骤（开放问题 1）；通过审计记录以及 §5.3 中提到的可逆性来缓解这一风险。
- **范围蔓延为一个通用策略平台。**§5 中的每一项操作都对应一次由所有者触发、立即生效、产生一条审计记录的变更——这与 [space-governance.md](./Space治理.md) 已经确立的形状一致。应抵制加入条件、延迟或多方签字确认；那属于 Space 审批工作流，已被明确排除在范围之外。

## 12. 开放问题

1. ~~所有权转让是否应当需要目标方接受，而不是立即生效？~~**已决定：不需要，立即且单方面生效。**曾考虑过的替代方案是应用内的待处理转让状态——在没有邮件通道的情况下依然可行，与 §5.1 的邀请机制类似——但第一个切片选择只保留一套新的待处理状态机制，而不是两套。如果实践中出现意外或不受欢迎的转让，可以重新考虑此决定；与此同时，§5.3 已经让转让具备可逆性。
2. ~~`admin` 是否应当被允许邀请一名 `member`（而非 `admin`）？~~**已决定：可以。**§5.1 和 §7 给予 admin 仅限 `member` 角色的 `ActionInviteSpaceMember`；角色变更和所有权转让仍然仅限所有者，因此这不会让管理员触及 `ActionManageSpaceMembers` 仍然保留的任何权限。
3. ~~邀请的 TTL 应当是多久？~~**已决定：`InvitationTTLDefault = 72 * time.Hour`。**这个问题最初是按凭证生命周期来构思的；在 §1 把账户创建完全移出这条流程之后，它就只是一条待处理的 `space_invitation` 记录还能被接受多久——之所以是三天而不是更短的窗口，是因为邀请本就应当在接收者下次打开 Portal 时才被处理，而不是要求在发出邀请的那次交互中就完成。
4. ~~被撤回或过期的邀请是否需要各自独立的审计操作？~~**已决定：需要，两者都需要。**`space.invitation_revoked` 对应显式撤回，`space.invitation_expired` 对应超过 TTL 后仍尝试接受——这为何有别于 [space-governance.md](./Space治理.md) §5.4 中登录失败静默不记录的先例，见 §8 M3。

## 13. 建议的第一个 PR

1. `Invitation` 核心类型、存储方法，以及 `space_invitation` 表。
2. 邀请路由（M2）与接受流程，彻底取代 `POST /api/spaces/{space_id}/members`。
3. 邀请/接受/撤回/过期对应的审计操作。
4. Portal 的邀请操作、待处理邀请列表，以及"我的邀请"页面。

角色变更、所有权转让，以及 Space 范围的登录码（§5.2–§5.4）与邀请机制相互独立，可以作为第二个 PR 落地，二者内部的先后顺序均可。
