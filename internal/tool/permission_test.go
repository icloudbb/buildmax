package tool

import (
	"context"
	"github.com/icloudbb/buildmax/internal/util"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/agent"
	"github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/icloudbb/buildmax/internal/infra/mcp"
)

// permissionCase is one row of the table in docs/design/tool-permissions.md §6.
//
// autonomous is what the loop resolves before the Ask collapse, which
// TestPolicyAsk_NoHandler covers separately. The point of pinning both columns
// is the design's central claim: adding the derived tier must leave every
// autonomous surface exactly as it was.
type permissionCase struct {
	name        string
	tool        llm.Tool
	args        map[string]any
	access      llm.Access
	interactive llm.ToolAction
	autonomous  llm.ToolAction
}

func permissionTable(t *testing.T) []permissionCase {
	t.Helper()
	ws := testWorkspace(t, t.TempDir())
	task, err := NewTask(nil2Runner{}, map[string]AgentTypeConfig{
		"general-purpose": {},
		"explore":         {Tools: []llm.Tool{NewReadFile(util.FixedRoot(ws)), NewGrep(util.FixedRoot(ws))}},
	})
	if err != nil {
		t.Fatalf("NewTask: %v", err)
	}
	gateway := GatewayTools(&mcp.Registry{})
	loadMCP, callMCP := gateway[0], gateway[1]

	return []permissionCase{
		{"Read", NewReadFile(util.FixedRoot(ws)), map[string]any{"file_path": "a.txt"}, llm.AccessReadOnly, llm.ToolActionAllow, llm.ToolActionAllow},
		{"Glob", NewGlob(util.FixedRoot(ws)), map[string]any{"pattern": "**/*.go"}, llm.AccessReadOnly, llm.ToolActionAllow, llm.ToolActionAllow},
		{"Grep", NewGrep(util.FixedRoot(ws)), map[string]any{"pattern": "x"}, llm.AccessReadOnly, llm.ToolActionAllow, llm.ToolActionAllow},
		{"Skill", NewSkillFromEntries(nil), map[string]any{"skill": "s"}, llm.AccessReadOnly, llm.ToolActionAllow, llm.ToolActionAllow},
		{"WebFetch", NewWebFetch(nil, 0), map[string]any{"url": "https://example.com"}, llm.AccessReadOnly, llm.ToolActionAllow, llm.ToolActionAllow},
		{"WebSearch", NewWebSearch(""), map[string]any{"query": "example"}, llm.AccessReadOnly, llm.ToolActionAllow, llm.ToolActionAllow},
		{"LoadMcpTools", loadMCP, map[string]any{"server": "s", "tool_name": "t"}, llm.AccessReadOnly, llm.ToolActionAllow, llm.ToolActionAllow},
		// A delegation whose agent type reaches only read-only tools — §6
		// footnote 2. It does not prompt, because nothing it can reach would
		// have prompted on its own.
		{"Task/read-only type", task, map[string]any{"subagent_type": "explore"}, llm.AccessReadOnly, llm.ToolActionAllow, llm.ToolActionAllow},

		// Writes: the rows that gain an interactive prompt. CallMcpTool is the
		// one row that also tightens on autonomous surfaces — see §6.
		{"Write", NewWriteFile(util.FixedRoot(ws)), map[string]any{"file_path": "a.txt"}, llm.AccessWrite, llm.ToolActionAsk, llm.ToolActionAllow},
		{"Edit", NewEditFile(util.FixedRoot(ws)), map[string]any{"file_path": "a.txt"}, llm.AccessWrite, llm.ToolActionAsk, llm.ToolActionAllow},
		{"Task", task, map[string]any{"subagent_type": "general-purpose"}, llm.AccessWrite, llm.ToolActionAsk, llm.ToolActionAllow},
		// An empty registry knows of no read-only tool, so this is the
		// non-read-only path: it asks interactively and denies where nobody can
		// answer. The read-only path turns entirely on Registry.ToolIsReadOnly,
		// which TestToolIsReadOnly covers against a live tools/list response.
		{"CallMcpTool", callMCP, map[string]any{"server": "s", "tool_name": "t"}, llm.AccessWrite, llm.ToolActionAsk, llm.ToolActionAsk},

		// Bash keeps its own risk classifier as the authority: an ordinary
		// command must not start prompting just because Bash writes.
		{"Bash/safe", NewBash(util.FixedRoot(ws)), map[string]any{"command": "ls"}, llm.AccessWrite, llm.ToolActionAllow, llm.ToolActionAllow},
		{"Bash/risky", NewBash(util.FixedRoot(ws)), map[string]any{"command": "sudo rm -f /etc/hosts"}, llm.AccessWrite, llm.ToolActionAsk, llm.ToolActionAsk},

		// Scratch-state writers opt out of the derivation via DefaultAction.
		{"TodoWrite", NewTodoWrite(), map[string]any{}, llm.AccessWrite, llm.ToolActionAllow, llm.ToolActionAllow},
		{"NoteWrite", NewNoteWrite(), map[string]any{}, llm.AccessWrite, llm.ToolActionAllow, llm.ToolActionAllow},
	}
}

// TestToolAccessDeclarations pins the Access column of §6. A tool that changes
// what it does must change this table in the same commit.
func TestToolAccessDeclarations(t *testing.T) {
	for _, tc := range permissionTable(t) {
		t.Run(tc.name, func(t *testing.T) {
			if got := agent.DeclaredAccess(tc.tool, tc.args); got != tc.access {
				t.Errorf("Access = %v, want %v", got, tc.access)
			}
		})
	}
}

// TestResolvedActionBySurface pins both action columns of §6.
func TestResolvedActionBySurface(t *testing.T) {
	for _, tc := range permissionTable(t) {
		t.Run(tc.name, func(t *testing.T) {
			if got := agent.ResolveToolAction(nil, tc.tool, tc.args, true); got != tc.interactive {
				t.Errorf("interactive = %v, want %v", got, tc.interactive)
			}
			if got := agent.ResolveToolAction(nil, tc.tool, tc.args, false); got != tc.autonomous {
				t.Errorf("autonomous = %v, want %v", got, tc.autonomous)
			}
		})
	}
}

// TestAutonomousSurfaceUnchangedByDerivation is the design's acceptance
// condition stated directly: layer 4 must be invisible without a human. Any
// autonomous action that differs from the pre-change resolution is a worker
// regression, which is what §3.4 says this design exists to avoid.
func TestAutonomousSurfaceUnchangedByDerivation(t *testing.T) {
	for _, tc := range permissionTable(t) {
		t.Run(tc.name, func(t *testing.T) {
			want := legacyResolve(tc.tool, tc.args)
			if got := agent.ResolveToolAction(nil, tc.tool, tc.args, false); got != want {
				t.Errorf("autonomous action = %v, pre-change resolution = %v", got, want)
			}
		})
	}
}

// legacyResolve reproduces the resolution as it stood before the derived tier:
// arg-level check, then explicit tool default, then Allow.
func legacyResolve(tool llm.Tool, args map[string]any) llm.ToolAction {
	if checker, ok := tool.(llm.ArgChecker); ok {
		if action := checker.CheckArgs(args); action != llm.ToolActionAllow {
			return action
		}
	}
	if provider, ok := tool.(llm.PolicyProvider); ok {
		return provider.DefaultAction()
	}
	return llm.ToolActionAllow
}

// nil2Runner satisfies SubAgentRunner for construction only; nothing calls it.
type nil2Runner struct{}

func (nil2Runner) RunSubAgent(_ context.Context, _ SubAgentRunOpts, _ string) (string, error) {
	return "", nil
}
