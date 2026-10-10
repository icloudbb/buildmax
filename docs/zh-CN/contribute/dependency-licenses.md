# 依赖许可证

> **翻译说明：** 本文是[英文原文](../../contribute/dependency-licenses.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
> **读者：** 贡献者 · **状态：** 当前有效

BuildMax 使用 [Apache-2.0](../../../LICENSE) 发布。本页记录依赖所用的许可证，以及如何重新检查。

最近审计日期：2026-08-15。

## Go

报告显示，`cmd/` 下三个二进制文件可达的模块共有 129 个：

| 许可证 | 数量 |
|---|---|
| MIT | 59 |
| Apache-2.0 | 42 |
| BSD-3-Clause | 24 |
| BSD-2-Clause | 2 |
| MPL-2.0 | 1 |
| ISC | 1 |

所有依赖都兼容以 Apache-2.0 分发 BuildMax。除一个 MPL-2.0 模块外，其余均使用宽松许可证。

唯一的 MPL-2.0 依赖是 `github.com/go-sql-driver/mysql`。MPL-2.0 是文件级 copyleft：它要求公开**该库自身文件的修改**，对项目其他部分不附加条件。BuildMax 未修改地使用它，因此不需要额外操作。如果将来在仓库内 fork 或修补它，该 fork 的源码必须继续以 MPL-2.0 提供。

## npm

前端包（`gui/`、`portal/`、`desktop/frontend/`）的生产依赖只涉及 MIT 和 ISC。

许可证工具将 `@buildmax/gui`、`buildmax-portal` 和 `buildmax-desktop-frontend` 报告为 `UNLICENSED`。这是 `"private": true` 字段造成的现象，它遮蔽了这些包声明的 `"license": "Apache-2.0"`。它们是本仓库自身的包，不是第三方代码。

Portal 镜像携带这些许可证要求的归属声明。Docker 构建运行 `portal/scripts/collect-notices.mjs`，将 `gui` 和 `portal` 锁文件中每个生产依赖的许可证文本合并为 `third-party-notices.txt`，在站点根目录提供，与其声明归属的 bundle 放在一起。该脚本是 `./make release notices` 的 npm 对应实现；它使用 node 而不放在 `tools/mk` 中，是因为执行它的镜像构建阶段只有 `node_modules`，没有 Go 工具链。本地重新生成：

```bash
node portal/scripts/collect-notices.mjs --out /tmp/notices.txt gui portal
```

## 重新运行审计

Go 检查包括一道门禁：遇到与再分发冲突的 copyleft 许可证时失败：

```bash
go install github.com/google/go-licenses@v1.6.0
go-licenses report ./cmd/...
go-licenses check ./cmd/... --disallowed_types=forbidden,restricted
```

每个拉取请求的 CI 都运行 `check` 命令，因此包含禁止或受限许可证的依赖会使构建失败，而非事后才被发现。

检查所有三个 npm 锁文件：

```bash
./make release licenses
```

npm 检查读取三个已提交的锁文件，忽略仅开发使用的依赖和本地链接包；生产依赖缺少许可证元数据或许可证未经批准时，检查失败。每个拉取请求的 CI 都运行 Go 和 npm 检查。

`go-licenses` 在发现更多传递依赖时，可能对包含无法跟踪的汇编代码的模块输出警告。这些检查警告不是许可证问题；模块声明的许可证文件仍会被检查和收集。

## 发布中的归属声明

Apache-2.0 §4(d) 要求在再分发 Apache-2.0 依赖时保留其归属声明，而编译后的 BuildMax 二进制包含这些依赖代码。

`./make release notices` 将链接进二进制的每个模块的完整许可证文本收集到一个 `NOTICE-THIRD-PARTY` 文件中，最近一次运行包含 132 个模块。GoReleaser 将其作为构建前 hook 执行，因此每个发布归档和容器镜像都会携带该文件。此文件由生成产生，不提交到仓库。

本地生成：

```bash
go install github.com/google/go-licenses@v1.6.0
./make release notices
```

文档必须可复现：同一组依赖必须产生相同字节，因此模块按字节顺序拼接，文件头固定。`tools/mk` 中的 `TestWriteNoticesBytes` 和 `TestLicenseFilesSortedByByteOrder` 维护这一契约。

当 `GOROOT` 指向模块缓存内部时，`go-licenses` v1.6.0 无法解析标准库，而 Go 自动下载的工具链正放在那里。如果 `go env GOROOT` 显示 `golang.org/toolchain@...` 路径，请正常安装对应 Go 版本，或在容器中生成文件：

```bash
docker run --rm -v "$PWD:/repo" -w /repo golang:1.26.9 bash -c \
  'go install github.com/google/go-licenses@v1.6.0 && PATH=$PATH:$(go env GOPATH)/bin go run ./tools/mk release notices'
```
