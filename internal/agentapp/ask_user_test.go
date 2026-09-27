package agentapp

import (
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/agent"
	tools "github.com/icloudbb/buildmax/internal/tool"
	"github.com/icloudbb/buildmax/internal/util"
)

// AskUser is registered only where a surface can take the answers, and in
// the form that surface answers in: waiting for someone at the session, or
// ending the turn for a later reply.
func TestAskUserFollowsTheMode(t *testing.T) {
	for _, mode := range []AskUserMode{AskUserOff, AskUserInteractive, AskUserDeferred} {
		app, err := NewAgentApp(AppConfig{WorkspaceDir: t.TempDir(), AskUser: mode})
		if err != nil {
			t.Fatalf("NewAgentApp: %v", err)
		}
		t.Cleanup(func() { _ = app.Close() })
		var description string
		for _, entry := range app.ToolEntries() {
			if entry.Name == tools.ToolNameAskUser {
				description = entry.Description
			}
		}
		switch mode {
		case AskUserOff:
			if description != "" {
				t.Error("AskUserOff registered the tool")
			}
		case AskUserInteractive:
			if !strings.Contains(description, "wait for the answers") {
				t.Errorf("interactive description = %q", description)
			}
		case AskUserDeferred:
			if !strings.Contains(description, "ends your turn") {
				t.Errorf("deferred description = %q", description)
			}
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
// that has the tool is told how to use it in its own mode, and a run without
// it hears nothing.
func TestAskUserPromptLayerFollowsTheMode(t *testing.T) {
	dir := t.TempDir()
	without := BuildEffectiveSystemPrompt(dir, "m", "", PromptCapabilities{})
	if strings.Contains(without, tools.ToolNameAskUser) {
		t.Error("a run without the tool must not be told to use it")
	}
	interactive, layers := BuildSystemPromptWithLayers(dir, "m", "", PromptCapabilities{AskUser: AskUserInteractive})
	if !strings.Contains(interactive, "continue in the same turn") {
		t.Error("an interactive run should be told the answer comes back in the turn")
	}
	deferred, _ := BuildSystemPromptWithLayers(dir, "m", "", PromptCapabilities{AskUser: AskUserDeferred})
	if !strings.Contains(deferred, "Nobody is watching this run") || strings.Contains(deferred, "continue in the same turn") {
		t.Error("a deferred run should be told its turn ends and to ask only when blocked")
	}
	found := false
	for _, l := range layers {
		found = found || l.Name == "ask_user"
	}
	if !found {
		t.Error("the layer should be traced")
	}
}
