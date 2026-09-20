# CLI

> **翻译说明：** 本文是[英文原文](../../../contribute/architecture/cli.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
> **受众：** 贡献者 · **状态：** 当前有效
>
> 面向用户的命令与标志参考：[manual/cli.md](../../../../manual/cli.md)

## 用途

`internal/interface/cli` 拥有 Cobra 命令树、打印模式和 Bubble Tea TUI。`cmd/buildmax/main.go` 是包裹它的轻薄入口。

## 入口

```go
func main() {
    s, _ := config.LoadSettings()
    log.Init(log.LogConfig{
        LogsDir: config.LogsDir(), Level: config.LogLevel(s.LogLevel), AlsoStdout: false,
    })
    root := cli.NewRootCommand()
    if err := root.Execute(); err != nil {
        os.Exit(cli.ExitCodeFor(err))   // ExitError is printed by the command, not here
    }
}
```

`main` 从 settings 解析日志级别并传入；`internal/infra/log` 本身不读取配置。`AlsoStdout: false` 为 TUI 和管道中的打印模式输出保持终端整洁。

## 命令树

| 命令 | 文件 |
|---|---|
| `buildmax`（根命令：TUI 或打印模式） | `root.go` |
| `init` | `init.go`（模板：`templates/settings.yaml.tmpl`） |
| `version` | `version.go` |
| `login`、`logout`、`me` | `login.go` |
| `sandbox status\|deps\|mode\|enable\|disable` | `sandbox.go` |
| `connect`、`app` | `app_connect.go` 和 `internal/interface/appconnect`：本地 OAuth 与固定应用调用 |
| `connect mcp`、`mcp` | `mcp_cli.go`：注册远端 MCP 服务器，并通过 `internal/infra/mcp` 发现和调用工具 |

`NewRootCommand()` 在根命令上注册十一个标志；面向用户的表格见 [manual/cli.md](../../../../manual/cli.md)。

## 分派（`runRoot`）

1. `--version` 提前返回并打印版本。
2. 将 `--output` 解析为一种格式；错误值是**用法错误**，不是崩溃。
3. 若给出 `--session-id`，验证其为 UUID。
4. 解析实际会话 ID：优先使用显式的 `--session-id`，否则对 `--resume` / `--continue` 使用 `resolveSessionTarget`。它同时返回会话*和*工作区，因为本地 Project 决定两者：`--continue` 在当前目录解析出的 Project 内选择，`--resume` 返回会话运行时的目录，并拒绝属于其他 Project 的会话。
5. `checkModelConfig()`：尽早以用法错误失败，而不是等到 LLM 调用时才失败。它区分三种状态，因为后续操作各不相同：没有 settings 文件（提示使用 `buildmax init`）、文件中没有 `models:` 条目，以及第一个模型仍持有 `init` 写入的 `APIKeyPlaceholder`。
6. `--print` 非空 → `runPrintMode(printOptions{...})`。否则 → `runTUI`。

## 退出码

这是封装 `buildmax -p` 的脚本所依赖的稳定契约。`ExitError` 通过 Cobra 的 `RunE` 携带退出码，让 `main` 能将其返回：

| 退出码 | 含义 |
|---|---|
| 0 | 成功 |
| 1 | 一般失败 |
| 2 | 用法错误：错误标志，或缺少模型等配置 |
| 3 | 工具被策略阻止 |
| 4 | LLM 或 Agent 运行时错误 |
| 5 | 为工具错误保留 |
| 6 | 已取消：SIGINT 或上下文取消 |

## 打印模式

实现位于 `print.go` 和 `print_format.go`。有三种输出格式：`text`（面向人阅读，除非使用 `--quiet`，否则附带统计页脚）、`json`（结束时输出一个对象）和 `jsonl`（每行一个事件，流式输出）。`--include-deltas` 为 jsonl 添加 `llm_delta` 事件，输出较多，但能显示 token 级进度。

事件流来自 Agent 循环的 `EventSink`，TUI 和运行轨迹也使用这个接入点。

## TUI

实现位于 `tui.go`、`tui_model.go` 和 `chat_*.go` 文件。`NewModel(TUIOpts)` 构造根 Bubble Tea 模型：

```go
type TUIOpts struct {
    App          *agentapp.AgentApp
    Session      *agentapp.SessionContext
    ModelName    string
    Workspace    string
    SessionsDir  string
    Approval     agent.ApprovalHandler
    GlamourStyle string          // "dark" or "light", detected once at startup
    RunStatus    agentapp.RunUsage
}
```

`Approval` 使工具审批提示具备交互能力；这是同一个 `agent.ApprovalHandler` 插槽，Worker 将其留为 nil。

模型、焦点处理和斜杠命令见 [tui.md](tui.md)。

## 依赖

- **使用**：`internal/agentapp`（运行时组装）、`internal/config`、`internal/core/agent`（`ApprovalHandler` 契约）、`internal/core/session`
- **外部依赖**：`github.com/spf13/cobra`、`github.com/charmbracelet/bubbletea`

## 说明

- CLI 从不自行构造工具或 LLM 客户端，这由 `agentapp` 完成。因此 Desktop 应用和 Worker 才具有相同的行为。
- 另见：[概览](overview.md)、[Agent 循环](agent-loop.md)、[TUI](tui.md)、[配置](config.md)。
