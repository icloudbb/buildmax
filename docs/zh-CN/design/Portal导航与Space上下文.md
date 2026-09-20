# Portal 导航与 Space 上下文

> **翻译说明：** 本文是[英文原文](../../design/portal-navigation-and-space-context.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
> **受众：** Portal 贡献者与产品设计者 · **状态：** 已实现 —— 第 1 阶段（导航
> 用语：Home 更名为 Chat，侧边栏按 Work/Reuse/Data/Manage 分组，暴露 Files，
> 将部署管理从 Space 范围中分离）、第 2 阶段（规范化的 `#/spaces/{space_id}/...`
> 路由；每个 Space 拥有的页面都从路由本身读取 Space，而不是此前选中的 Space）、
> 第 3 阶段（Space 切换会把每个 Space 拥有的路由都重定向到目标 Space 中有效的
> 目的地，由同一张穷举、经类型检查的表驱动——补上了此前 Agent 与 Task 在切换时
> 仍悄悄显示上一个 Space 数据的缺口）、第 4 阶段（解析状态：无法识别的路由会
> 渲染未找到页面，而不是悄悄回退到 Chat；Agent、Workflow、Workflow Run、Task
> 与 Issue 详情页能区分真正的"未找到"与"无权访问的 Space 或资源"）与第 5 阶段
> （定位完善：浏览器标签页标题会标出当前页面，Space 范围的路由还会带上 Space
> 名称；窄屏紧凑头部与面包屑携带与桌面端侧栏相同的 Space/类型/位置线索；面向
> 旧扁平 hash 的、有时限的迁移重定向已移除——旧 hash 现在只是普通的未找到，而
> 不再是静默别名）均已全部上线。

本文定义 Portal 导航如何表达作用域，以及 URL 如何确保 Space 所拥有的资源始终位于
正确的 Space 中。它是 R3 候选版本运维流程的已实现基础，不改变路线图优先级。

## 目录

- [目标结果](#目标结果)
- [证据与约束](#证据与约束)
- [决策](#决策)
- [路由与导航模型](#路由与导航模型)
- [实施切片](#实施切片)
- [验收标准](#验收标准)
- [被否决的替代方案](#被否决的替代方案)
- [相关记录](#相关记录)

## 目标结果

用户无需拼凑应用状态就能回答三个问题：我在哪个 Space、正在查看哪类对象、某个导航
动作会把我带到哪里。复制的链接必须重新打开同一 Space 作用域的资源，或明确报告该
资源不可访问。

## 证据与约束

- Space 是 Portal 资源的所有权与授权边界，而 Account 和部署管理属于其他作用域。
- 当前实体 hash 不包含 Space。在选择了另一个 Space 时打开复制链接，会产生误导性的
  未找到状态。
- 切换 Space 时，一部分详情页会重定向，但并非所有 Space 资源页都会。未知 hash 会
  回退到 Home，从而掩盖导航错误。
- 当前 Home 实际是 Conversation 编辑器，而[产品愿景](产品愿景.md)把 Issue 定义为
  面向用户的主要工作对象。称其为“Home”会让它看起来像并不存在的 Space 总览。
- BuildMax 仍处于 Alpha，不需要兼容偶然形成的 hash 结构，因此不应使用永久旧路由层
  掩盖稳定模型。

## 决策

Portal 明确区分两个导航作用域：

1. **Space 作用域**包含所选 Space 拥有的工作和资源。
2. **全局作用域**包含 Account、Help、Marketplace 和部署管理。

对于带 Space 前缀的页面，URL 是 Space 上下文的权威来源。规范的 ID 解析例外可以改为
从已授权的资源响应设置 Space 上下文。Shell 绝不依赖之前选择的 Space 来解释资源
标识符。

Space 主导航按用户意图分组：

| 分组 | 目的地 |
|---|---|
| 工作 | Issues、Chat |
| 复用 | Agents、Workflows |
| 数据 | Files、Artifacts |
| 管理 | Space 设置以及有权访问的管理界面 |

“Home”更名为“Chat”。在 Space 聚合查询和运维者路径证据明确应该显示哪些结果之前，
不新增真正的 Overview。Portal 不得通过向所有集合 API 发起 fan-out 请求来拼出总览。

部署管理在视觉和结构上都位于 Space 作用域之外。其页面不会继续展示可用的 Space
切换器，以免暗示所选 Space 会改变部署级权限：切换器变为静态的 “Deployment”
标签，侧边栏在该作用域下直接列出各管理区块，其后是 “Back to space” 返回项，而
不再显示按 Space 分组的导航。

## 路由与导航模型

规范 Space 路由以 `#/spaces/{space_id}` 开头，集合和详情在其后扩展，例如：

```text
#/spaces/{space_id}/issues
#/spaces/{space_id}/issues/{issue_id}
#/spaces/{space_id}/chat/{conversation_id}
#/spaces/{space_id}/agents/{agent_id}
#/spaces/{space_id}/workflows/{workflow_id}
#/spaces/{space_id}/workflow-runs/{run_id}
#/spaces/{space_id}/tasks/{task_id}
#/spaces/{space_id}/files
```

Artifact 保留[统一 Artifact](统一工件.md)和
[Artifact 公开分享与预览](工件公开分享与预览.md)已经接受的公开能力与稳定详情链接
规则。`#/artifact/{artifact_id}` 是唯一的 ID 解析详情例外：完成授权查询后，Portal
选择该 Artifact 的 Space 并显示其上下文。经过认证的集合从 `#/artifacts` 移至带 Space
前缀的数据路由；公开分享 URL 仍位于认证 shell 之外。

全局路由不增加 Space 前缀，包括 Account、Marketplace、Help 和部署管理。

路由处理遵循以下规则：

- 解析 Space 路由时，先确认该 Space 存在且账号可见，再在该 Space 中加载资源。
- 从详情页切换 Space 时，进入目标 Space 对应的集合页，绝不保留原 Space 的标识符。
- 缺失 Space、缺失资源和禁止访问采用
  [Portal 状态与权限反馈](Portal状态与权限反馈.md)定义的不同页面状态。
- 未知路由显示带安全导航动作的未找到页面，不再静默显示 Chat。
- 数据加载后，breadcrumb 使用可读名称；加载期间使用稳定的类型名称。原始公共 ID
  只是次要元数据，而不是主要定位线索。

## 实施切片

每个切片可按顺序独立合并：

1. **导航语言。** 将 Home 改为 Chat，整理侧栏分组，暴露 Files，并分开
   全局动作；此步不改变路由。
2. **规范路由解析器。** 引入带类型的 Space 前缀路由生成和匹配，并逐个迁移资源族。
3. **Space 切换。** 为所有 Space 路由集中实现切换目标规则，移除页面级重定向列表。
4. **解析状态。** 增加未找到和禁止访问路由，再移除回退到 Home 的行为。
5. **定位完善。** 统一 breadcrumb、页面标题和响应式折叠行为。

迁移期间，内部链接只能生成规范路由。旧 hash 的临时重定向仅限于迁移变更，并在本记录
标记为已实现前删除；它不是兼容性契约。

## 验收标准

- 每个经过认证的 Space 集合与详情 URL 都包含 Space 公共 ID，或是明确记录的 ID 解析
  例外。本设计中 Artifact detail 是唯一例外。
- 重新加载或复制任何受支持 URL，都能保留同一 Space 和资源。
- 从任何 Space 资源路由切换 Space，都会进入有效的目标 Space 集合，且不显示原
  Space 的陈旧数据。
- 未知、禁止访问和缺失路由可区分且可采取行动。
- 在桌面和窄屏下，侧栏分组与 breadcrumb 都能表达 Space、资源类型和当前位置。
- Portal 路由测试覆盖直接进入、浏览器前进/后退、Space 切换和无效 hash；浏览器套件
  至少覆盖 Issue、Task、Agent 和 Workflow 深链接。

## 被否决的替代方案

- **只在本地存储所选 Space。** URL 仍然有歧义，共享链接会依赖无关的浏览器历史。
- **按 ID 全局解析所有资源。** 这会削弱可见的所有权边界，使授权行为更难理解。
- **立即创建仪表盘。** 没有权威聚合查询和经过验证的运维问题时，只会增加标签和网络
  fan-out，而不会增加有用的产品概念。
- **永久保留所有旧 hash。** Alpha 没有要求该状态的外部契约，永久别名会增加路由行为。

## 相关记录

- [产品愿景](产品愿景.md)
- [Space 治理](Space治理.md)
- [实体身份与关系键](实体身份.md)
- [Artifact 公开分享与预览](工件公开分享与预览.md)
- [Portal 响应式与无障碍交互](Portal响应式与无障碍交互.md)
