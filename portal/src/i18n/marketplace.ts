import type { Messages } from "@buildmax/gui"

// The deployment-wide plugin catalog and one plugin's detail.
export const marketplaceMessages = {
  en: {
    "marketplace.title": "Marketplace",
    "marketplace.subtitle":
      "Plugins this deployment publishes — skills, subagents, MCP servers, and hooks you can install on your own machine.",
    "marketplace.count": { one: "{count} plugin", other: "{count} plugins" },
    "marketplace.searchPlaceholder": "Search plugins…",
    "marketplace.searchLabel": "Search plugins",
    "marketplace.error.load": "Failed to load the plugin catalog",
    "marketplace.empty.title": "Nothing published yet",
    "marketplace.empty.copy": "When an administrator publishes a plugin, it shows up here for anyone to install.",
    "marketplace.noMatch": "No plugin matches “{query}”.",
    // The foot wraps a `buildmax plugin list` code element between these two.
    "marketplace.foot.lead":
      "Installing happens where the agent runs. This page cannot see your machine, so what is installed there is ",
    "marketplace.foot.tail": " there.",
    "marketplace.noDescription": "No description.",

    "marketplace.detail.newest": "Newest release",
    "marketplace.detail.needs": " · needs BuildMax {version}+",
    "marketplace.detail.digest": " · digest {digest}…",
    "marketplace.detail.environment": "Environment",
    "marketplace.detail.environmentHint":
      "Reads these variables — a plugin that looks installed and does nothing is usually one that is unset:",
    "marketplace.detail.activation": "Space activation",
    "marketplace.detail.activationHint":
      "Publishing here does not activate it anywhere. To let {space}'s background runs use it, activate it in Space Plugins.",
    "marketplace.detail.openSpacePlugins": "Open Space Plugins",
    "marketplace.detail.withdrawn":
      "Nothing here is installable: every release was withdrawn. An exact version can still be recovered from a terminal.",
    "marketplace.detail.contributes": "Contributes",
    "marketplace.detail.nothingRecognised": "Nothing this build recognises.",
    "marketplace.detail.install": "Install",
    "marketplace.detail.copy": "Copy",
    "marketplace.detail.copied": "Copied",

    "marketplace.group.skills": "Skills",
    "marketplace.group.subagents": "Subagents",
    "marketplace.group.mcp": "MCP servers",
    "marketplace.group.hooks": "Hooks",
    "marketplace.chip.skills": { one: "{count} skill", other: "{count} skills" },
    "marketplace.chip.subagents": { one: "{count} subagent", other: "{count} subagents" },
    "marketplace.chip.mcp": { one: "{count} MCP server", other: "{count} MCP servers" },
    "marketplace.chip.hooks": { one: "{count} hook", other: "{count} hooks" },
  },
  "zh-CN": {
    "marketplace.title": "插件市场",
    "marketplace.subtitle": "此部署发布的插件：可安装到你自己机器上的技能、子 Agent、MCP 服务器和 hook。",
    "marketplace.count": "{count} 个插件",
    "marketplace.searchPlaceholder": "搜索插件…",
    "marketplace.searchLabel": "搜索插件",
    "marketplace.error.load": "加载插件目录失败",
    "marketplace.empty.title": "尚未发布任何插件",
    "marketplace.empty.copy": "管理员发布插件后，它会显示在这里，供任何人安装。",
    "marketplace.noMatch": "没有与“{query}”匹配的插件。",
    "marketplace.foot.lead": "插件安装在 Agent 运行的地方。此页面看不到你的机器，要查看那里已安装的插件，请在那里运行 ",
    "marketplace.foot.tail": "。",
    "marketplace.noDescription": "暂无描述。",

    "marketplace.detail.newest": "最新版本",
    "marketplace.detail.needs": " · 需要 BuildMax {version}+",
    "marketplace.detail.digest": " · 摘要 {digest}…",
    "marketplace.detail.environment": "环境变量",
    "marketplace.detail.environmentHint": "读取以下变量。看似已安装却不起作用的插件，通常是因为这些变量未设置：",
    "marketplace.detail.activation": "Space 启用",
    "marketplace.detail.activationHint":
      "在这里发布不会在任何地方启用它。要让 {space} 的后台运行使用它，请在 Space 插件中启用。",
    "marketplace.detail.openSpacePlugins": "打开 Space 插件",
    "marketplace.detail.withdrawn": "这里没有可安装的版本：所有版本都已撤回。仍可在终端中恢复某个确切版本。",
    "marketplace.detail.contributes": "提供内容",
    "marketplace.detail.nothingRecognised": "没有当前版本能识别的内容。",
    "marketplace.detail.install": "安装",
    "marketplace.detail.copy": "复制",
    "marketplace.detail.copied": "已复制",

    "marketplace.group.skills": "技能",
    "marketplace.group.subagents": "子 Agent",
    "marketplace.group.mcp": "MCP 服务器",
    "marketplace.group.hooks": "hook",
    "marketplace.chip.skills": "{count} 个技能",
    "marketplace.chip.subagents": "{count} 个子 Agent",
    "marketplace.chip.mcp": "{count} 个 MCP 服务器",
    "marketplace.chip.hooks": "{count} 个 hook",
  },
} satisfies Messages<string>
