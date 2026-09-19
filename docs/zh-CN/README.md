# BuildMax 文档

> **翻译说明：** 本文是[英文原文](../README.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。

按你想完成的任务组织。

## 使用 BuildMax

最终用户手册位于 [`manual/`](../../manual)，涵盖安装、快速入门、核心概念、所有 CLI 命令、内置工具、技能与子 Agent、MCP、插件、Hook、沙箱、工具权限和 Portal 使用流程。它随 Portal 镜像交付，可在应用内通过 **Help** 阅读；[`manual/manifest.json`](../../manual/manifest.json) 是其目录。

| | |
|---|---|
| [简介](../../manual/introduction.md) | BuildMax 是什么，以及它的三个使用界面 |
| [安装](../../manual/install.md) | 获取二进制文件 |
| [快速入门](../../manual/quickstart.md) | 五分钟内完成首次 Agent 运行 |
| [支持矩阵](../../manual/support.md) | 支持的平台、界面、部署方式及非目标 |
| [../sample-data/](../../sample-data/README.md) | 十五组可随时丢弃的示例数据集：上传到 Space 工作区，或让 CLI 指向其中一组 |

## 为 Space 部署和运行

| | |
|---|---|
| [deploy/compose.md](deploy/compose.md) | 约五分钟内在单机上完成 Space 部署 |
| [deploy/overview.md](deploy/overview.md) | 拓扑、要求、配置与容器 |
| [deploy/authentication.md](deploy/authentication.md) | **对外开放 Server 前必读**：账户、登录码及尚未具备的能力 |
| [deploy/local-kind.md](deploy/local-kind.md) | 一条命令创建本地集群并执行 Kubernetes Job 冒烟验证 |
| [deploy/digitalocean.md](deploy/digitalocean.md) | 用于 Beta 资格验证的临时外部 DOKS 和 MySQL 环境 |
| [deploy/beta-readiness.md](deploy/beta-readiness.md) | 对固定版本的私有部署候选版本进行资格验证并记录证据 |

## 查阅参考资料

| | |
|---|---|
| [reference/configuration.md](reference/configuration.md) | 所有配置文件字段和环境变量 |
| [CLI 参考](../../manual/cli.md) | 命令、标志与斜杠命令（位于用户手册） |
| [reference/webhook.md](reference/webhook.md) | 从外部系统触发运行 |

HTTP API 提供自描述文档：`GET /openapi.json`，可在 `/swagger/` 浏览。

## 参与开发

| | |
|---|---|
| [../CONTRIBUTING.md](../../CONTRIBUTING.md) | 前置要求、构建、测试、代码边界与拉取请求 |
| [contribute/areas.md](contribute/areas.md) | 选择贡献方向，寻找适合自身经验的工作 |
| [contribute/first-pr.md](contribute/first-pr.md) | 从克隆到提交拉取请求的完整流程，无需 API 密钥 |
| [contribute/conventions.md](contribute/conventions.md) | 命名、ID、工具输出、提交信息与变更日志条目 |
| [contribute/repo-layout.md](contribute/repo-layout.md) | 仓库目录结构与依赖方向 |
| [contribute/testing.md](contribute/testing.md) | 各类变更应运行的测试套件、所需条件，以及 CI 的运行时机 |
| [contribute/exploratory-testing.md](contribute/exploratory-testing.md) | 自主探索用户旅程，留下固定测试用例之外的可复现发现 |
| [evaluation/README.md](../../evaluation/README.md) | 如何评估一个构建：本地套件、外部基准及结果包内容 |
| [changelog/README.md](changelog/README.md) | 如何添加变更日志条目，以及发布时如何汇总条目 |
| [contribute/architecture/](contribute/architecture/README.md) | 各子系统当前的工作方式 |
| [contribute/documentation.md](contribute/documentation.md) | 文档规范 |
| [contribute/dependency-licenses.md](contribute/dependency-licenses.md) | 许可证审计及重新执行方式 |
| [contribute/releasing.md](contribute/releasing.md) | 版本管理、发布、验证与发布恢复 |

## 理解设计理由

| | |
|---|---|
| [current-state.md](current-state.md) | 基于代码的实现情况与就绪程度评估 |
| [ROADMAP.md](ROADMAP.md) | 当前优先级与实施顺序 |
| [backlog/](../backlog/README.md) | 维护者与 Agent 的规划闭环，以及按优先级排列、可直接执行的任务 |
| [design/](design/设计文档索引.md) | 按 domain 浏览、以生命周期标记的设计理由 |
| [../SECURITY.md](../../SECURITY.md) | 漏洞披露与运维人员责任 |

## 探索未来方向

| | |
|---|---|
| [proposals/](proposals/README.md) | 尚在讨论中的早期跨领域方向，不代表路线图承诺 |

## 规范

每篇文档开头都标明受众与状态，让读者能在一行之内判断其可信程度：

```markdown
> **Audience:** operators · **Status:** current
```

不设归档目录：退役文档直接删除，由 git 历史保存。文档撰写和退役规则见 [contribute/documentation.md](contribute/documentation.md)。
