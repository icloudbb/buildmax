# 发布 BuildMax

> **翻译说明：** 本文是[英文原文](../../contribute/releasing.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
> **读者：** 维护者 · **状态：** 当前有效

维护者从 `main` 创建 BuildMax 发布。推送版本标签会启动 `.github/workflows/release.yml`，构建归档和容器镜像、生成校验和与 SBOM，并发布来源证明。

容器镜像在发布前扫描；存在已有修复的 HIGH 或 CRITICAL 漏洞时，发布失败，不推送任何内容。`.github/workflows/portal-image.yml` 中的 Portal 镜像也遵循此规则，且在拉取请求时也扫描。此前两者在推送后才扫描，发现问题只能让作业失败：`v0.2.0-alpha.3` 发布了两个包含已有修复的 openssl CVE 的镜像，随后因此失败。

alpha 阶段，`.github/workflows/release-prepare.yml` 每天检查是否应发布。当最新标签已存在至少 72 小时且有未发布 changelog 条目时，它创建 `release/next` 拉取请求。它不会按定时器自动发布：维护者必须评审并合并该请求。合并会创建附注标签，并触发现有的二进制、server 镜像、Portal 镜像和桌面工作流。

## 版本规则

BuildMax 遵循语义化版本。alpha 阶段使用 `v0.2.0-alpha.1` 这样的标签；GoReleaser 将预发布标签标记为 GitHub pre-release。稳定版本省略预发布后缀。

绝不移动或复用已发布标签。发布出错时，使用新的补丁版本或预发布版本修复。

## 准备

定时创建的拉取请求完成步骤 1、2 中的机械操作。仓库必须允许 GitHub Actions 创建拉取请求，分支保护必须允许 `github-actions[bot]` 推送分支。由于 `GITHUB_TOKEN` 推送不会启动另一个工作流，准备作业会显式在 `release/next` 上触发 CI、完整发布快照、Portal 镜像构建和 Compose 升级演练。

要在 72 小时窗口之前准备发布，可手动运行 **Prepare release**；它仍会拒绝创建空发布。关闭其拉取请求可推迟发布。后续符合条件的运行会基于当时的 `main` 和全部 changelog 条目重新创建请求。请求保持打开期间，定时运行不会修改候选版本及维护者的编辑。

1. 确认 `main` 已更新，所有必需 CI 检查通过。
2. 将未发布条目合并进 [CHANGELOG.md](../../../CHANGELOG.md)：

   ```bash
   ./make changelog                 # preview the section
   ./make changelog release 0.1.0   # write it and clear docs/changelog/
   ```

   提交前阅读结果；合并是机械操作，按文件名而非读者关心程度排序。它也是发布页面的内容，因此应预览工作流将发布的正文：

   ```bash
   ./make release notes v0.2.0-alpha.1
   ```

   正文使用 `.github/release-notes.tmpl` 并填入本版本章节：亮点和升级说明位于安装步骤上方，分类列表在下方。标签在 `CHANGELOG.md` 中没有对应章节时，发布失败。
3. 检查 [SECURITY.md](../../../SECURITY.md)、安装说明、已知限制和配置示例是否需要本版本相关更新。
4. 指明候选版本的升级来源：最新已发布的标签，即部署实际从其升级的版本。`internal/infra/db/testdata/schema/` 只保存一个以该来源命名的转储；若它仍是更早的标签，刷新它：

   ```bash
   ./make release upgrade-fixture 0.2.0-alpha.15
   ```

   该命令需要 Docker。它以该标签的 server 镜像连接一个专用 MySQL 容器，通过该标签自身的 API 写入固定数据集，并用该版本的 schema、数据行和迁移台账替换原有转储。将转储提交到 `release/next`，或先合入 `main`。CI 的 MySQL 作业随后用候选版本的 `db.New` 升级它，并断言每个写入的实体都得以保留。若 API 变化导致写入失败，按来源版本的 API 更新 `tools/mk/upgrade_fixture.go`。
5. 在 Compose 中演练从该来源的升级。准备作业会在 `release/next` 上触发 **Upgrade drill** 工作流；在其他 ref 上可手动运行它，或在本地运行：

   ```bash
   ./make compose upgrade-drill                           # newest tag behind HEAD -> this checkout
   ./make compose upgrade-drill --from 0.2.0-alpha.15 --to 0.2.0-alpha.16
   ```

   演练使用专属的 Compose 项目，结束后将其删除。它启动来源镜像，通过其 API 写入升级夹具的数据集，然后停止 server，备份数据库（`mysqldump`）、`server-data` 卷和 `server.yaml`。它换上候选版本（除非 `--to` 指定已发布标签，否则从当前检出构建），并通过 API 读回每个写入的实体。随后必须有一个新任务成功。接着它让来源镜像再次连接已升级的数据库启动。最后恢复备份、启动来源版本，并检查来源版本提供全部写入数据，而不包含候选版本创建的任务。工作流把写入 `.artifacts/upgrade-drill/result.md` 的结果表作为运行摘要发布。

   只有同时满足两个条件时，来源版本才必须拒绝已升级的数据库：候选版本记录了来源台账中没有的迁移，且来源版本为 0.2.0-alpha.16 或更新。更早的镜像早于该拒绝机制，因此演练只记录其行为而不做判定。台账一致时，来源版本必须能启动。合并发布拉取请求前，先查明任何失败的原因。通过只是在一次性单机栈上的演练，不是 Beta 就绪记录所需的候选证据。
6. 运行本地验证命令：

   ```bash
   ./make test
   ./make build
   ./make release licenses
   npm exec --yes --package=markdownlint-cli2@0.23.2 -- markdownlint-cli2
   goreleaser check
   goreleaser release --snapshot --clean --skip=publish,docker
   ./make release verify --all
   ./make release verify
   ```

快照需要 GoReleaser `v2.17.1`、Syft `v1.51.0` 和 `go-licenses v1.6.0`。CI 安装这些精确版本；执行发布时，PATH 中需要 GoReleaser 才能运行上述快照。仅编辑 `.goreleaser.yaml` 的贡献者不需要安装：拉取请求和 `./make check ci` 分别通过 action 和 `go run` 使用相同固定版本验证配置。

## 签名与来源证明

alpha 发布为归档和 SBOM 使用 GitHub Artifact Attestations，为 GHCR 镜像使用 Docker Buildx 生成的 SBOM 和来源证明，不附加独立 Cosign 签名。这样，GitHub 托管的源码与产物只有一条有文档说明的无密钥验证路径，不必要求用户理解两套等效身份系统。

如果 BuildMax 开始在 GHCR 之外发布镜像、在 GitHub Releases 之外分发产物，或需要脱离 GitHub 证明服务也能验证的签名，再重新考虑 Cosign。使用方应通过 digest 识别容器镜像，而非依赖可变标签。

## 发布

合并自动生成的 `release/next` 拉取请求，是 alpha 发布的常规批准方式。提升工作流验证标题和 changelog 是否指向下一个编号 alpha，创建标签，然后显式触发各发布工作流——**Release**、**Portal image** 和 **Desktop release**。使用 `GITHUB_TOKEN` 推送标签时，GitHub 会抑制标签触发工作流，因此显式触发是必要操作，并非重复执行。

如果标签已存在后，触发或发布任一环节失败，请针对该标签手动重跑 **Release** 和 **Portal image**，不要重新创建或移动标签。

要发布当前编号 alpha 系列以外的版本，或自动化不可用时，请手动创建标签：

在已评审的 `main` 提交上创建附注标签，仅推送该标签：

```bash
git tag -a v0.2.0-alpha.1 -m "v0.2.0-alpha.1"
git push origin v0.2.0-alpha.1
```

不要从尚未进入 `main` 的本地提交创建发布标签。发布工作流负责创建 GitHub Release 和发布到 GHCR。

## 验证

工作流完成后：

1. 确认 GitHub Release 附有所有预期平台归档、`checksums.txt`，以及每个归档对应的一份 SPDX SBOM；正文包含该版本的 changelog 章节。
2. 下载一个归档，验证校验和，并运行 `buildmax version`。
3. 确认归档包含 `LICENSE`、`NOTICE-THIRD-PARTY`、`README.md`、`SECURITY.md`、`CHANGELOG.md` 和 `config-examples/`。
4. 按[安装指南](../../../manual/install.md)验证 GitHub 证明。
5. 通过 digest 拉取 `ghcr.io/icloudbb/buildmax:<version>`，确认容器可启动。alpha 版本不得移动 `latest` 标签。镜像扫描在发布前已通过，因此此时发布工作流失败，表示推送后的某个步骤失败，而不是镜像存在漏洞。
6. 确认 `ghcr.io/icloudbb/buildmax-portal:<version>` 存在，且版本**相同**。它由同一标签触发的独立工作流（`.github/workflows/portal-image.yml`）发布，因此该工作流失败会造成二进制已发布但 Portal 镜像缺失。这是有意的取舍，但必须检查，不能假定成功。设置 `BUILDMAX_API_BASE` 后运行它，确认 `/config.js` 包含该值，且 `/third-party-notices.txt` 提供 npm 许可证归属声明。

7. 确认桌面产物已附加：`buildmax-desktop_<version>_darwin_<arch>.dmg` 和
   `buildmax-desktop_<version>_windows_amd64.exe`，各带其 `.sha256`。它们由同一标签
   触发的独立按 OS 作业 `.github/workflows/desktop-release.yml` 发布——因为
   GoReleaser 的单个 Linux runner 无法构建原生 macOS bundle。该作业失败会保持发布其余
   部分完好而缺失桌面下载，因此要检查而非假定。下载 `.dmg`，验证校验和，并按
   [安装指南](../../../manual/install.md)在越过 Gatekeeper 后打开应用一次。

alpha 阶段桌面 bundle **未签名、未公证**：下载后的 macOS 应用会被 Gatekeeper 拦住，
Windows 二进制会触发 SmartScreen 警告，需用户清除一次。签名与公证是下一步；在此之前，
安装指南记录了这道一次性步骤。

## 处理有问题的发布

不要悄悄替换发布产物或复用标签。给受影响的 GitHub Release 添加醒目警告，在可安全披露时创建跟踪 Issue，并发布修正版本。安全问题在公开讨论前，应遵循 [SECURITY.md](../../../SECURITY.md) 中的私下处理流程。
