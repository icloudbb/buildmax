# MCP 服务器

[Model Context Protocol](https://modelcontextprotocol.io/) 服务器为 Agent 提供
BuildMax 本身不附带的工具——你的问题跟踪系统、你的数据库、你的内部服务。

## 从 CLI 连接远端服务器

```bash
buildmax connect mcp context7 https://mcp.context7.com/mcp
buildmax mcp list
buildmax mcp tools context7
buildmax mcp schema context7 resolve-library-id
buildmax mcp call context7 resolve-library-id --json '{"libraryName":"react","query":"React hooks"}'
```

`connect mcp` 检查端点及工具列表，然后把服务器写入
`<BUILDMAX_HOME>/mcp.json`，供本机所有工作区使用；不会覆盖已有同名条目。
SSE 端点使用 `--transport sse`。除本机回环测试服务器外，端点必须使用 HTTPS。

对于接受静态 Bearer token 的服务器，将令牌放在进程环境变量中，并只传变量名：

```bash
export MY_MCP_TOKEN=your-token
buildmax connect mcp work https://mcp.example.com/mcp --bearer-env MY_MCP_TOKEN
```

配置仅保存 `bearer_token_env`，不保存令牌值。后续 CLI、Desktop 或 Agent
运行仍须提供该环境变量；缺失时连接失败。CLI 的每条命令会建立新的 MCP 会话。
本原型尚未实现 MCP OAuth 发现、浏览器登录、令牌刷新或多账户选择。
Skill 可组合 `buildmax mcp tools`、`schema` 和 `call` 表达工作流程。
示例见 [`sample-plugins/mcp-cli`](../../sample-plugins/mcp-cli)。

`buildmax mcp call` 接受最大 1 MiB 的 JSON 对象。服务器标注为只读的工具可
直接运行；其他工具需要交互式终端中的逐项输入确认。此 CLI 确认与 Agent 运行时
的 `CallMcpTool` 权限规则相互独立。只读标注是服务器的自我声明；拥有未隔离
shell 权限的本地 Agent 也能绕过 CLI。两者都不构成单次运行的应用授权。

## 配置

MCP 服务器在 `mcp.json` 中声明，可以位于以下两处之一或两者：

| 文件 | 作用范围 |
|---|---|
| `<BUILDMAX_HOME>/mcp.json` | 本机上的每个工作区 |
| `<workspace>/.buildmax/mcp.json` | 仅该工作区 |

两个文件会被**合并**，当同一个服务器 id 同时出现在两处时以工作区中的条目为准。这正是有用的形态：共享的服务器放全局，项目专属的服务器随项目一起提交。

```json
{
  "mcpServers": {
    "github": {
      "type": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": { "GITHUB_TOKEN": "$GITHUB_TOKEN" }
    },
    "internal-api": {
      "type": "http",
      "url": "https://mcp.internal/api"
    }
  }
}
```

| 字段 | 含义 |
|---|---|
| `type` | 传输方式——`stdio` 表示本地子进程，或使用某种 HTTP/SSE 传输 |
| `command`、`args` | 要启动的进程，用于 `stdio` |
| `env` | 该进程的环境变量 |
| `url` | 端点，用于 HTTP 传输 |
| `bearer_token_env` | 远端服务器 Bearer token 的可选环境变量名 |

## 变量展开

`$VAR` 和 `${VAR}` 会在 `command`、`args`、`env` 的值以及 `url` 中展开，展开依据是进程环境加上一个内置变量：

| 变量 | 解析为 |
|---|---|
| `${WORKSPACE_ROOT}` | 本次运行的工作区目录 |

这就是把密钥排除在已提交的 `mcp.json` 之外的方法——引用 `$GITHUB_TOKEN`，让环境来提供它。

注意该名称不带 `BUILDMAX_` 前缀。无法识别的变量会展开为空字符串而不是失败，因此拼错的名称会表现为一条以 `/` 开头的路径，而不是一个错误。

## Agent 如何使用它们

MCP 工具不会被逐个注入到提示词中。有两个网关工具来处理它们：

- `LoadMcpTools` —— 发现某个已连接服务器所提供的内容
- `CallMcpTool` —— 调用其中一个

这让工具列表始终保持精简，无论连接了多少台服务器，代价是 Agent 首次访问某台服务器时会多一次往返。

## 审批

服务器会描述它的每个工具，并可将其中一个标记为只读。BuildMax 用这一点来决定是否在调用前询问你：被标注为只读的工具会无需提示直接运行，其他任何工具都会在 CLI TUI 和 Desktop 上提示，而在没有人可询问的场合会被拒绝——print 模式、worker 和 Portal 会话。

省略该标注的服务器会被当作写操作处理，因为协议无法区分"非只读"和"未说明"。要停止被追问某台你信任的服务器，在提示时回答 `a` 以覆盖本次会话，或写一条规则：

```yaml
tools:
  permissions:
    "CallMcpTool:github/*": allow
```

完整细节参见：[工具权限](工具权限.md)。

## 检查它是否正常工作

在 TUI 中运行 `/mcp` 查看已连接的服务器及其状态。启动失败的服务器会在那里显示出来，而不是在运行中途悄然失败。

仓库中在 `tools/mcp` 附带了一个小型测试服务器，支持 stdio、SSE 和 streamable HTTP——在没有真实后端的情况下用于验证集成很有用。

## 说明

- 一台 MCP 服务器是你用自己的凭据启动的一个进程。添加一台的态度应当和添加一个依赖一样。
- 钩子匹配器将 MCP 调用看作 `CallMcpTool`，而不是底层的工具名称。
- 如果有可用的 web fetch MCP 工具，Agent 会被指示优先使用它而非内置的 `WebFetch`。

## 相关

- [工具](工具.md) —— 内置工具集
- [工具权限](工具权限.md) —— 哪些调用会停下来询问
- [钩子](钩子.md) —— `mcp_tool` 也可作为一种钩子传输方式
