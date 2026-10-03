# 你的第一个拉取请求

> **翻译说明：** 本文是[英文原文](../../contribute/first-pr.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
> **读者：** 新贡献者 · **状态：** 当前有效

从克隆仓库到创建拉取请求的最短完整路径。需要 Go 和 git，**不需要模型 API 密钥**。首次构建会下载约 700 MB 的 Go 依赖；此后，本页每个命令都能在一分钟内完成，包括完整测试套件和拉取请求前的检查。

## 1. 克隆与构建

```bash
git clone https://github.com/icloudbb/buildmax.git
cd buildmax
./make doctor
./make setup local
./make build cli
```

Windows 上使用 `make.bat build cli`。`./make` 并非 GNU make，而是 `tools/mk` 中 Go 任务运行器的一行包装，因此所有平台运行相同任务代码。`./make help` 列出从日常操作到部署、发布的全部命令，`./make help <command>` 完整解释某个命令。

`./make setup local` 创建 `.local/`，这个唯一被 git 忽略的目录保存属于你而非仓库的配置，并打印还需填写的内容。本页操作不需要填写任何配置，首次变更不需要凭据；现在运行一次，`./make doctor` 就不会再提示它。将来需要模型 API 密钥时，也将其放在这里。

二进制生成在 `bin/buildmax`。`./make build cli` 跳过 server、worker 和前端，首次变更只需要这些。

## 2. 运行测试

```bash
./make test
```

此命令以 `BUILDMAX_HOME=./testing-sandbox` 运行 `go test ./...`，将数据写入仓库内被 git 忽略的目录，而非真实的 `~/.buildmax`。顺利通过意味着工具链配置正确。

如果计划修改 Go 代码，请在创建拉取请求前运行 Go 检查：

```bash
./make check go
```

一个命令覆盖 CI 的全部 Go 步骤：格式、`go mod tidy` 清洁度、构建、vet、race 套件和 lint。它只报告未格式化文件，不自动修复；修复请用 `./make fmt`。只运行 `./make test` 会漏掉这些步骤，而格式问题最容易让原本正确的变更在 CI 中失败。

## 3. 选择小任务

按容易合并的程度排序：

- 标有 `good first issue`、`help wanted` 或 `documentation` 的 Issue。
- 文档修复。`docs/` 中每项声明都应与代码一致；发现不一致就是真正的缺陷，欢迎提交拉取请求。`./make check docs` 负责验证，其 Markdown lint 是唯一需要 Node 的贡献者检查；没有 Node 时，可以创建拉取请求，让 CI 运行那部分。
- 为已经正常工作的行为补充缺失测试。
- 尝试快速入门时遇到的 CLI 或 TUI 体验问题。

要认领一个 issue 让别人知道你在做，在该 issue 下评论 `/assign`——机器人会把它指派给你，无需仓库权限。评论 `/unassign` 可释放。每人同时最多认领两个未关闭的 issue；认领后若数周无动静，会被自动释放，避免工作被一直占着。

开始更大的工作前，请阅读 [manual/support.md](../../../manual/support.md)。其中说明项目目前支持哪些界面和部署路径，以及 alpha 阶段明确不支持哪些内容，避免工作超出维护者能够接受的范围。

代码位置见 [repo-layout.md](repo-layout.md)。子系统工作原理见 [architecture/](architecture/README.md)。

## 4. 实施变更

```bash
git switch -c short-topic-name
# edit, then:
./make test ./internal/tool   # one package while iterating
./make test                   # the whole suite before you push
```

包模式放在 `go test` 参数前：写作 `./make test ./internal/tool -run TestX`，不要反过来。参数后出现包时会拒绝执行，而不会悄悄将范围扩大为 `./...`。

遵循周围代码的风格。diff 中看不到的规则，例如持久化 JSON 使用 `snake_case`、表名单数、实体 ID 前缀和面向 LLM 的工具输出，见 [conventions.md](conventions.md)。

文档是变更的一部分，而非后续工作：修改行为或配置时，在同一个拉取请求中更新 `manual/`、`docs/reference/` 和 `config-examples/`。[documentation.md](documentation.md) 说明何时更新哪些内容。

## 5. 提交并创建拉取请求

```bash
./make check ci
git commit -m "Fix the workspace path in the sandbox guide"
git push -u origin short-topic-name
```

`./make check ci` 包括必需的拉取请求套件，以及按路径划分的发布检查：上述 Go 检查、两个前端测试套件、文档检查和全仓库扫描。它需要固定版本的 Node；没有 Node 时，运行 `./make check go`，其余交给 CI。

提交标题是一行祈使句。`Co-authored-by` 可以用于标明实质性的人类协作；不要加入 assistant、模型或工具 trailer，也不要添加“Generated with …”页脚。如果用户或运维人员能感知变更，请在 [`docs/changelog/`](../changelog/README.md) 下新增 changelog 文件。

向 `main` 创建拉取请求并填写模板：问题、方案、验证方法以及尚缺内容。小而可验证胜过庞大而面面俱到；维护者一次能读完的拉取请求会更快获得评审。

请花些时间打磨**标题**及其下的提交标题。本项目使用 merge commit 合并拉取请求，因此两者都会进入 `main`，也都是一年后别人阅读 `git log` 时看到的内容：每个标题都应是一行祈使句，且具体到可以独立理解。

## CI 会运行什么

每个拉取请求运行 [CONTRIBUTING.md § Pull Requests](../../../CONTRIBUTING.md#pull-requests) 所述的三个必需作业：格式、`go mod tidy` 清洁度、构建、vet、golangci-lint、govulncheck、带 `-race` 的测试套件、三个前端构建和两个前端测试套件、git 历史机密扫描、依赖许可证检查，以及 Markdown lint。所有检查均不需要凭据，因此在 fork 上运行方式相同。

相关变更会增加 GoReleaser 配置验证或 Portal 镜像构建；`./make check ci` 始终运行前者。Go 作业会为 Windows 交叉编译；原生 Windows 套件在合并后运行，因为它需要 Windows 机器。

## 遇到困难时

- 运行时问题参见 [manual/troubleshooting.md](../../../manual/troubleshooting.md)
- 一般问题可到 [GitHub Discussions](https://github.com/icloudbb/buildmax/discussions) 提问，也可以在未完成的拉取请求中提问
- 应使用哪个渠道参见 [../../.github/SUPPORT.md](../../../.github/SUPPORT.md)
