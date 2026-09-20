package agentapp

import (
	"context"
	"github.com/icloudbb/buildmax/internal/util"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/agent"
	"github.com/icloudbb/buildmax/internal/core/llm"
	tools "github.com/icloudbb/buildmax/internal/tool"
)

type stubTool struct{}

func (stubTool) Name() string                                            { return "StubSkill" }
func (stubTool) Description() string                                     { return "stub" }
func (stubTool) Parameters() any                                         { return map[string]any{} }
func (stubTool) Execute(context.Context, map[string]any) (string, error) { return "", nil }

type stubPublisher struct{}

func (stubPublisher) PublishArtifact(context.Context, tools.ArtifactUpload) (tools.PublishedArtifact, error) {
	return tools.PublishedArtifact{}, nil
}

func toolNames(list []llm.Tool) map[string]bool {
	out := make(map[string]bool, len(list))
	for _, t := range list {
		out[t.Name()] = true
	}
	return out
}

// A surface with no artifact service must not offer the tool at all. A tool
// registered only to answer "unavailable" costs a round trip and teaches the
// model nothing — see docs/design/unified-artifacts.md section 7.1.
func TestArtifactToolIsAbsentWithoutAPublisher(t *testing.T) {
	names := toolNames(buildBaseTools(nil, util.FixedRoot(t.TempDir()), stubTool{}, agent.NoopSandbox{}, "", nil, nil))
	if names[tools.ToolNameUploadArtifact] {
		t.Error("a session with no artifact service must not be offered the tool")
	}
	if !names[tools.ToolNameRead] {
		t.Error("the ordinary tools should still be there")
	}
	if !names[tools.ToolNameWebSearch] {
		t.Error("web search should be available without extra configuration")
	}
}

func TestArtifactToolIsPresentWithAPublisher(t *testing.T) {
	names := toolNames(buildBaseTools(nil, util.FixedRoot(t.TempDir()), stubTool{}, agent.NoopSandbox{}, "", stubPublisher{}, nil))
	if !names[tools.ToolNameUploadArtifact] {
		t.Error("a session with an artifact service must be offered the tool")
	}
}

// The prompt describes the tools the agent was actually given, so a session
// without the capability is never told to publish anything.
func TestArtifactPromptLayerFollowsTheCapability(t *testing.T) {
	dir := t.TempDir()
	without := BuildEffectiveSystemPrompt(dir, "m", "", PromptCapabilities{})
	with, layers := BuildSystemPromptWithLayers(dir, "m", "", PromptCapabilities{Artifacts: true})

	if strings.Contains(without, tools.ToolNameUploadArtifact) {
		t.Error("a session with no artifact capability must not be told about the tool")
	}
	if !strings.Contains(with, tools.ToolNameUploadArtifact) {
		t.Error("a session with the capability should be told when to use it")
	}
	var found bool
	for _, l := range layers {
		if l.Name == "artifacts" {
			found = true
		}
	}
	if !found {
		t.Error("the layer should be traced, so a finished run can say what it was told")
	}
}
