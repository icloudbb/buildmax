package agentapp

import (
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/agent"
	tools "github.com/icloudbb/buildmax/internal/tool"
	"github.com/icloudbb/buildmax/internal/util"
)

// AskUser holds a run until a person answers, so only a surface with someone
// at it registers the tool; everything else never offers it to the model.
func TestAskUserIsRegisteredOnlyWhereSomeoneAnswers(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		app, err := NewAgentApp(AppConfig{WorkspaceDir: t.TempDir(), EnableAskUser: enabled})
		if err != nil {
			t.Fatalf("NewAgentApp: %v", err)
		}
		t.Cleanup(func() { _ = app.Close() })
		found := false
		for _, entry := range app.ToolEntries() {
			found = found || entry.Name == tools.ToolNameAskUser
		}
		if found != enabled {
			t.Errorf("EnableAskUser=%v: AskUser registered = %v", enabled, found)
		}
	}
}

// A subagent reports to its parent, which decides whether the user needs
// asking, so no agent definition can name the tool.
func TestBaseToolsExcludeAskUser(t *testing.T) {
	base := buildBaseTools(nil, util.FixedRoot(t.TempDir()), stubTool{}, agent.NoopSandbox{}, "", nil, nil)
	for _, tl := range base {
		if tl.Name() == tools.ToolNameAskUser {
			t.Fatal("AskUser is in the base set, so a subagent definition can name it")
		}
	}
}

// The tool alone is not enough: a real model asked in prose instead. A run
// that has the tool is told to prefer it, and a run without it hears nothing.
func TestAskUserPromptLayerFollowsTheCapability(t *testing.T) {
	dir := t.TempDir()
	without := BuildEffectiveSystemPrompt(dir, "m", "", PromptCapabilities{})
	with, layers := BuildSystemPromptWithLayers(dir, "m", "", PromptCapabilities{AskUser: true})
	if strings.Contains(without, tools.ToolNameAskUser) {
		t.Error("a run without the tool must not be told to use it")
	}
	if !strings.Contains(with, tools.ToolNameAskUser) {
		t.Error("a run with the tool should be told to prefer it over asking in prose")
	}
	found := false
	for _, l := range layers {
		found = found || l.Name == "ask_user"
	}
	if !found {
		t.Error("the layer should be traced")
	}
}
