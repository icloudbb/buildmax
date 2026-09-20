# MCP CLI Skill prototype

Copy this directory to `<BUILDMAX_HOME>/plugins/mcp-cli` to make the Skill
available to a local Agent. Register a remote server with `buildmax connect mcp
<name> <url>` first. The Skill demonstrates on-demand tool discovery and a
workflow over the CLI; it does not install or authenticate an MCP server.

See [MCP servers](../../manual/mcp.md) for Bearer token setup and prototype
limits. The Agent runtime also has `LoadMcpTools` and `CallMcpTool` directly;
this sample tests how an Agent uses the CLI through a Skill.
