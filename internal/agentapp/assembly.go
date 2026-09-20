package agentapp

import (
	"log/slog"
	"strings"
	"time"

	"github.com/icloudbb/buildmax/internal/agentapp/job"
	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/core/agent"
	"github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/icloudbb/buildmax/internal/core/subagent"
	tools "github.com/icloudbb/buildmax/internal/tool"
	"github.com/icloudbb/buildmax/internal/util"
)

// MaxAdditionalSystemPromptChars bounds the additional system prompt. It sits in the system
// prompt, which is re-sent in full on every call and has no trimming path, so it is bounded
// when it is resolved rather than degraded later.
const MaxAdditionalSystemPromptChars = agent.MaxUserAuthoredSystemPromptChars

// ValidateAdditionalSystemPrompt rejects text that does not fit the budget. The error names the
// size and the limit so whoever supplied it — a flag, a file, or an agent record — can see what
// to cut.
func ValidateAdditionalSystemPrompt(text string) error {
	return ValidateInstructionLayers("", text)
}

// ValidateInstructionLayers bounds the complete user-authored instruction
// prefix. Space and Agent instructions are both permanent on every model call,
// so they share one budget rather than each quietly doubling it.
func ValidateInstructionLayers(spaceInstructions, additionalSystemPrompt string) error {
	return agent.ValidateInstructionLayers(spaceInstructions, additionalSystemPrompt)
}

// BuildEffectiveSystemPrompt builds the agent system prompt for a workspace, an optional model
// name, and an optional additional system prompt.
//
// The local layers run from least to most specific, and every one is additive:
//
//  1. the runtime prompt, which carries the tool-usage conventions
//  2. ~/.buildmax/AGENTS.md — personal rules
//  3. <ws>/AGENTS.md — project rules
//  4. the additional system prompt — this run's user-authored identity and constraints
//
// Portal workers additionally insert their Space instructions before layer 4.
// Together the layers form the cacheable prefix for one run. The compaction
// summary changes, and RunLoop appends it after them; it is never added here.
//
// Pass an empty modelName when it is not yet known, and empty additional text when the run has
// none.
func BuildEffectiveSystemPrompt(workspaceDir, modelName, additionalSystemPrompt string, caps PromptCapabilities) string {
	prompt, _ := buildSystemPromptWithLayers(workspaceDir, modelName, "", additionalSystemPrompt, "", caps)
	return prompt
}

// BuildSystemPromptWithLayers builds the prompt and reports which layers contributed to it.
// The layer list goes into the run trace, so a finished run can say what it was told before
// the conversation began rather than leaving it to be inferred from behaviour.
func BuildSystemPromptWithLayers(workspaceDir, modelName, additionalSystemPrompt string, caps PromptCapabilities) (string, []agent.PromptLayer) {
	return buildSystemPromptWithLayers(workspaceDir, modelName, "", additionalSystemPrompt, "", caps)
}

func buildSystemPromptWithLayers(workspaceDir, modelName, spaceInstructions, additionalSystemPrompt, additionalLayerName string, caps PromptCapabilities) (string, []agent.PromptLayer) {
	effectivePrompt := DefaultSystemPrompt
	layers := []agent.PromptLayer{{Name: "runtime", Chars: len(DefaultSystemPrompt)}}
	appendLayer := func(name, text string) {
		effectivePrompt += "\n\n" + text
		layers = append(layers, agent.PromptLayer{Name: name, Chars: len(text)})
	}

	if modelName != "" {
		effectivePrompt += "\n\n# Runtime context\nCurrent model: " + modelName
	}
	if caps.Artifacts {
		appendLayer("artifacts", artifactPromptLayer)
	}
	if caps.Issue != nil {
		appendLayer("issue", issuePromptLayer(caps.Issue))
	}
	if global, err := ReadAgentsMd(config.DataDir()); err == nil && global != "" {
		appendLayer("user_agents_md", global)
	}
	if ws, err := ReadAgentsMd(workspaceDir); err == nil && ws != "" {
		appendLayer("workspace_agents_md", ws)
	}
	if shared := strings.TrimSpace(spaceInstructions); shared != "" {
		appendLayer("space_instructions", "# Space instructions\n"+shared)
	}
	if extra := strings.TrimSpace(additionalSystemPrompt); extra != "" {
		if additionalLayerName == "" {
			additionalLayerName = "additional_system_prompt"
		}
		heading := "# Additional instructions\n"
		if additionalLayerName == "agent_instructions" {
			heading = "# Agent instructions\n"
		}
		appendLayer(additionalLayerName, heading+extra)
	}
	return effectivePrompt, layers
}

// BuildAgentTypes merges built-in sub-agent definitions with caller-provided user defs into
// an AgentTypeConfig map ready for tools.NewTask.
func BuildAgentTypes(registry llm.ToolRegistry, userDefs []subagent.Def) map[string]tools.AgentTypeConfig {
	builtinDefs := tools.BuiltinSubAgentDefs()
	agentTypes := make(map[string]tools.AgentTypeConfig, len(builtinDefs))
	for _, def := range builtinDefs {
		var resolved []llm.Tool
		if def.ToolNames == nil {
			resolved = registry.Tools()
		} else {
			resolved = ResolveAgentTypeTools(def.Name, def.ToolNames, registry)
		}
		agentTypes[def.Name] = tools.AgentTypeConfig{
			Tools:        resolved,
			SystemPrompt: def.SystemPrompt,
			Description:  def.Description,
			// Built-in types use runner defaults for Model and MaxIterations.
		}
	}
	for _, def := range userDefs {
		if _, exists := agentTypes[def.Name]; exists {
			slog.Warn("skip user-defined agent: name conflicts with built-in", "name", def.Name)
			continue
		}
		resolved := ResolveAgentTypeTools(def.Name, def.ToolNames, registry)
		if len(resolved) == 0 {
			slog.Warn("skip user-defined agent: no valid tools resolved", "name", def.Name)
			continue
		}
		agentTypes[def.Name] = tools.AgentTypeConfig{
			Tools:         resolved,
			SystemPrompt:  def.SystemPrompt,
			Description:   def.Description,
			Model:         def.Model,
			MaxIterations: def.MaxIterations,
		}
	}
	return agentTypes
}

// ResolveAgentTypeTools resolves tool names from a registry; skips unknowns with a warning.
func ResolveAgentTypeTools(agentName string, toolNames []string, registry llm.ToolRegistry) []llm.Tool {
	if toolNames == nil {
		return nil
	}
	resolved := make([]llm.Tool, 0, len(toolNames))
	for _, name := range toolNames {
		t := registry.Lookup(name)
		if t == nil {
			slog.Warn("skip unknown tool in agent def", "agent", agentName, "tool", name)
			continue
		}
		resolved = append(resolved, t)
	}
	return resolved
}

// buildBaseTools returns the standard set of workspace tools for an agent.
// sandboxView is the SandboxView the Bash tool wraps spawned commands
// through; pass agent.NoopSandbox{} (or nil) to leave bash unsandboxed.
// buildBaseTools assembles the default tools.
//
// publisher is nil on a surface with no artifact service, and the artifact tool
// is then absent from the list rather than present and failing. A tool that
// exists only to answer "unavailable" costs a round trip and teaches the model
// nothing; one that is not there is a fact it can act on immediately. See
// docs/design/unified-artifacts.md section 7.1.
//
// jobs follows the same rule for Bash's run_in_background: nil keeps the
// parameter out of the schema entirely.
func buildBaseTools(client llm.LLMClient, ws util.Workspace, skillTool llm.Tool, sandboxView agent.SandboxView, searchAPIKey string, publisher tools.ArtifactPublisher, jobs *job.Manager) []llm.Tool {
	if sandboxView == nil {
		sandboxView = agent.NoopSandbox{}
	}
	base := []llm.Tool{
		tools.NewReadFile(ws),
		tools.NewWriteFile(ws),
		tools.NewBash(ws).WithSandbox(sandboxView).WithJobs(jobs),
		tools.NewGlob(ws),
		tools.NewEditFile(ws),
		tools.NewGrep(ws),
		tools.NewWebFetch(client, 15*time.Minute).WithSandbox(sandboxView),
		tools.NewWebSearch(searchAPIKey).WithSandbox(sandboxView),
		tools.NewTodoWrite(),
		tools.NewNoteWrite(),
		skillTool,
	}
	if publisher != nil {
		base = append(base, tools.NewUploadArtifact(ws, publisher))
	}
	return base
}
