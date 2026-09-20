package agentapp

import (
	"log/slog"

	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	tools "github.com/icloudbb/buildmax/internal/tool"
	"github.com/icloudbb/buildmax/internal/util"
)

// agentTypeToolsAt rebuilds one agent type's tool set against a different
// workspace root, for a delegate working in its own worktree.
//
// Everything except the root is what the parent's set would have been: the
// same definitions, the same filtering, the same MCP tools. Rebuilding rather
// than reusing is the point — the parent's instances hold the parent's
// workspace, and a delegate given those would write into the tree it was
// supposed to stay out of.
//
// Returns nil when the type is unknown or nothing resolved, which the caller
// reports rather than running the delegate somewhere unintended.
func (a *AgentApp) agentTypeToolsAt(client cllm.LLMClient, agentType string, ws util.Workspace) []cllm.Tool {
	if a == nil {
		return nil
	}
	toolNames, all, ok := a.agentDefToolsFor(agentType)
	if !ok {
		slog.Warn("delegate worktree: unknown agent type", "type", agentType)
		return nil
	}

	registry := cllm.NewToolRegistry()
	registry.AppendTools(buildBaseTools(client, ws, a.skillsRegistry.NewTool(), a.Sandbox(), a.webSearchAPIKey, a.artifactPublisher, nil)...)
	if a.mcpManager != nil {
		if reg := a.mcpManager.Registry(); reg != nil {
			registry.AppendTools(tools.GatewayTools(reg)...)
		}
	}

	// A built-in naming no tools takes the whole set, exactly as
	// BuildAgentTypes reads it.
	if all {
		return registry.Tools()
	}
	return ResolveAgentTypeTools(agentType, toolNames, registry)
}

// agentDefToolsFor reports the tool names one agent type declares, and whether
// it declares none and therefore takes everything. Built-ins first, then user
// definitions, matching the precedence BuildAgentTypes applies.
func (a *AgentApp) agentDefToolsFor(name string) (toolNames []string, all bool, found bool) {
	for _, def := range tools.BuiltinSubAgentDefs() {
		if def.Name == name {
			return def.ToolNames, def.ToolNames == nil, true
		}
	}
	for _, def := range a.subagentsRegistry.Definitions() {
		if def.Name == name {
			return def.ToolNames, false, true
		}
	}
	return nil, false, false
}
