# CLI

> **简体中文：** [阅读中文镜像](../../zh-CN/contribute/architecture/cli.md)
> **Audience:** contributors · **Status:** current
>
> User-facing command and flag reference: [manual/cli.md](../../../manual/cli.md)

## Purpose

`internal/interface/cli` owns the Cobra command tree, print mode, and the Bubble
Tea TUI. `cmd/buildmax/main.go` is a thin shell around it.

## Entry Point

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

`main` resolves the log level from settings and passes it in — `internal/infra/log`
reads no configuration itself. `AlsoStdout: false` keeps the terminal clean for
the TUI and for piped print-mode output.

## Command Tree

| Command | File |
|---|---|
| `buildmax` (root: TUI or print mode) | `root.go` |
| `init` | `init.go` (template: `templates/settings.yaml.tmpl`) |
| `version` | `version.go` |
| `login`, `logout`, `me` | `login.go` |
| `sandbox status\|deps\|mode\|enable\|disable` | `sandbox.go` |
| `connect`, `app` | `app_connect.go` and `internal/interface/appconnect`: local OAuth and fixed app calls |
| `connect mcp`, `mcp` | `mcp_cli.go`: register a remote MCP server, then discover and call its tools through `internal/infra/mcp` |

`NewRootCommand()` registers eleven flags on the root command; the user-facing
table is in [manual/cli.md](../../../manual/cli.md).

## Dispatch (`runRoot`)

1. `--version` short-circuits and prints.
2. Parse `--output` into a format; a bad value is a **usage error**, not a crash.
3. Validate `--session-id` as a UUID when given.
4. Resolve the effective session id: explicit `--session-id`, else
   `resolveSessionTarget` for `--resume` / `--continue`. It answers with a
   session *and* a workspace, because the local Project decides both:
   `--continue` selects within the Project the current directory resolves to,
   and `--resume` returns to the directory its session ran in and refuses one
   belonging to a different Project.
5. `checkModelConfig()` — fails early with a usage error, rather than failing
   later at the LLM call. It distinguishes three states, because each has a
   different next step: no settings file (point at `buildmax init`), a file
   with no `models:` entry, and a first model still holding the
   `APIKeyPlaceholder` that `init` writes.
6. `--print` non-empty → `runPrintMode(printOptions{...})`. Otherwise → `runTUI`.

## Exit Codes

A stable contract for scripts wrapping `buildmax -p`. `ExitError` carries the
code through Cobra's `RunE` so `main` can surface it:

| Code | Meaning |
|---|---|
| 0 | OK |
| 1 | Generic failure |
| 2 | Usage — bad flag, or missing configuration such as no model |
| 3 | Tool blocked by policy |
| 4 | LLM or agent runtime error |
| 5 | Reserved for tool errors |
| 6 | Cancelled — SIGINT or context cancellation |

## Print Mode

`print.go` and `print_format.go`. Three output formats: `text` (human, with a
stats footer unless `--quiet`), `json` (one object at the end), and `jsonl` (one
event per line, streamed). `--include-deltas` adds `llm_delta` events to jsonl,
which is verbose but shows token-level progress.

The event stream comes from the agent loop's `EventSink`, the same seam the TUI
and the run trace use.

## TUI

`tui.go`, `tui_model.go`, and the `chat_*.go` files. `NewModel(TUIOpts)` builds
the root Bubble Tea model:

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

`Approval` is what makes tool approval prompts interactive — the same
`agent.ApprovalHandler` slot the worker leaves nil.

See [tui.md](tui.md) for the model, focus handling, and slash commands.

## Dependencies

- **Uses**: `internal/agentapp` (runtime assembly), `internal/config`,
  `internal/core/agent` (the `ApprovalHandler` contract), `internal/core/session`
- **External**: `github.com/spf13/cobra`, `github.com/charmbracelet/bubbletea`

## Notes

- The CLI never builds tools or LLM clients itself; `agentapp` does. That is why
  the same behavior appears in the desktop app and the worker.
- See also: [Overview](overview.md), [Agent Loop](agent-loop.md), [TUI](tui.md),
  [Configuration](config.md).
