package plugininspect

import (
	"strings"
	"testing"
	"testing/fstest"
)

func file(body string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(body)} }

func findingText(p Package) string {
	var b strings.Builder
	for _, f := range p.Findings {
		b.WriteString(f.String())
		b.WriteString("\n")
	}
	return b.String()
}

func TestDirValidatesConnectorAndClientIDEnvironment(t *testing.T) {
	manifest := `auth:
  authorization_url: https://accounts.example.com/auth
  token_url: https://accounts.example.com/token
  client_id_env: APP_CLIENT_ID
  scopes: [read]
api_origin: https://api.example.com
operations:
  profile: {method: GET, path: /profile, effect: read}
`
	fsys := fstest.MapFS{
		"plugin.yaml":    file("name: app\nenv:\n  APP_CLIENT_ID:\n    description: Public OAuth client ID.\n"),
		"connector.yaml": file(manifest),
	}
	got, err := Dir(fsys)
	if err != nil || got.HasErrors() || len(got.Findings) != 0 {
		t.Fatalf("valid connector: %v, %s", err, findingText(got))
	}
	fsys["connector.yaml"] = file(strings.Replace(manifest, "path: /profile", "path: /../secret", 1))
	got, err = Dir(fsys)
	if err != nil || !got.HasErrors() {
		t.Fatalf("invalid connector accepted: %v, %s", err, findingText(got))
	}
}

func TestDirDescribesAWholePackage(t *testing.T) {
	fsys := fstest.MapFS{
		"plugin.yaml": file("name: code-review\nversion: 1.2.0\n" +
			"env:\n  GITHUB_TOKEN:\n    description: A token.\n  POLICY_TOKEN:\n    description: Another.\n"),
		"skills/review/SKILL.md": file("# review\n\nReview a change.\n"),
		"skills/lint/SKILL.md":   file("# lint\n\nLint a change.\n"),
		"agents/reviewer.md": file("---\nname: reviewer\ndescription: Reviews.\n" +
			"tools: Read, Grep\nmodel: fast\n---\n\nYou review.\n"),
		"mcp.json": file(`{"mcpServers":{
			"github":{"type":"stdio","command":"npx","args":["-y","server-github"],
			          "env":{"GITHUB_TOKEN":"$GITHUB_TOKEN"}},
			"internal":{"type":"http","url":"https://mcp.internal.example.com/api?tenant=acme"}}}`),
		"hooks.yaml": file("post_tool_use:\n  - type: command\n    matcher: \"Write|Edit\"\n" +
			"    command: \"${BUILDMAX_PLUGIN_ROOT}/hooks/format.sh --write\"\n" +
			"  - type: http\n    url: https://policy.internal.example.com/check\n" +
			"    headers:\n      Authorization: \"Bearer $POLICY_TOKEN\"\n" +
			"    allowed_env: [POLICY_TOKEN]\n"),
		"hooks/format.sh": file("#!/bin/sh\n"),
		"README.md":       file("# Code Review\n"),
	}

	got, err := Dir(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if got.HasErrors() {
		t.Fatalf("unexpected errors:\n%s", findingText(got))
	}
	if strings.Join(got.Skills, ",") != "lint,review" {
		t.Errorf("Skills = %v", got.Skills)
	}
	if len(got.Subagents) != 1 || got.Subagents[0].Name != "reviewer" ||
		strings.Join(got.Subagents[0].Tools, ",") != "Read,Grep" || got.Subagents[0].Model != "fast" {
		t.Errorf("Subagents = %+v", got.Subagents)
	}

	if len(got.MCP) != 2 {
		t.Fatalf("MCP = %+v", got.MCP)
	}
	if got.MCP[0].ID != "github" || got.MCP[0].Executable != "npx" || got.MCP[0].Transport != "stdio" {
		t.Errorf("stdio server = %+v", got.MCP[0])
	}
	// A URL is reduced to its host: the path and query identify more than the
	// destination and belong to nobody but the operator.
	if got.MCP[1].Host != "mcp.internal.example.com" {
		t.Errorf("http server host = %q", got.MCP[1].Host)
	}

	if len(got.Hooks) != 2 {
		t.Fatalf("Hooks = %+v", got.Hooks)
	}
	if got.Hooks[0].Event != "PostToolUse" || got.Hooks[0].Type != "command" ||
		got.Hooks[0].Executable != "format.sh" || got.Hooks[0].Matcher != "Write|Edit" {
		t.Errorf("command hook = %+v", got.Hooks[0])
	}
	if got.Hooks[1].Host != "policy.internal.example.com" {
		t.Errorf("http hook host = %q", got.Hooks[1].Host)
	}

	if strings.Join(got.EnvRefs, ",") != "GITHUB_TOKEN,POLICY_TOKEN" {
		t.Errorf("EnvRefs = %v", got.EnvRefs)
	}
	if strings.Join(got.PluginPaths, ",") != "hooks/format.sh" {
		t.Errorf("PluginPaths = %v", got.PluginPaths)
	}
}

// The record must not carry the inside of somebody's configuration.
func TestDirKeepsNoArgumentsHeadersOrPrompts(t *testing.T) {
	fsys := fstest.MapFS{
		"plugin.yaml": file("name: secretive\nenv:\n  TOKEN:\n    description: A token.\n"),
		"mcp.json": file(`{"mcpServers":{"a":{"type":"stdio","command":"/usr/bin/server",
			"args":["--api-key","sk-do-not-publish"],"env":{"TOKEN":"$TOKEN"}}}}`),
		"hooks.yaml": file("stop:\n  - type: prompt\n    prompt: \"Summarise: $ARGUMENTS and mention hunter2\"\n" +
			"  - type: mcp_tool\n    server: audit\n    tool: record\n" +
			"    input:\n      note: \"do-not-publish\"\n"),
	}

	got, err := Dir(fsys)
	if err != nil {
		t.Fatal(err)
	}
	rendered := renderAll(t, got)
	for _, secret := range []string{"sk-do-not-publish", "hunter2", "do-not-publish"} {
		if strings.Contains(rendered, secret) {
			t.Errorf("inspection leaked %q:\n%s", secret, rendered)
		}
	}
	// It still says what starts and what it talks to.
	if got.MCP[0].Executable != "server" {
		t.Errorf("Executable = %q", got.MCP[0].Executable)
	}
	if got.Hooks[1].MCPServer != "audit" || got.Hooks[1].MCPTool != "record" {
		t.Errorf("mcp hook = %+v", got.Hooks[1])
	}
	// $ARGUMENTS is BuildMax's own substitution, not something to ask an
	// operator for.
	for _, name := range got.EnvRefs {
		if name == "ARGUMENTS" {
			t.Error("ARGUMENTS is not an environment requirement")
		}
	}
}

func TestDirCrossChecksTheEnvironmentContract(t *testing.T) {
	fsys := fstest.MapFS{
		"plugin.yaml": file("name: drifted\nenv:\n  DECLARED_UNUSED:\n    description: Nobody reads this.\n"),
		"mcp.json": file(`{"mcpServers":{"a":{"type":"stdio","command":"x",
			"env":{"TOKEN":"$UNDECLARED_TOKEN"}}}}`),
	}

	got, err := Dir(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if got.HasErrors() {
		t.Fatalf("a drifted contract is a warning, not a refusal:\n%s", findingText(got))
	}
	text := findingText(got)
	if !strings.Contains(text, "DECLARED_UNUSED") || !strings.Contains(text, "declared but never referenced") {
		t.Errorf("stale declaration not reported:\n%s", text)
	}
	if !strings.Contains(text, "UNDECLARED_TOKEN") || !strings.Contains(text, "not declared under env") {
		t.Errorf("undocumented requirement not reported:\n%s", text)
	}
}

func TestDirReportsAPathThePackageDoesNotShip(t *testing.T) {
	fsys := fstest.MapFS{
		"plugin.yaml": file("name: broken-path\n"),
		"hooks.yaml": file("post_tool_use:\n  - type: command\n" +
			"    command: \"${BUILDMAX_PLUGIN_ROOT}/hooks/missing.sh\"\n"),
	}
	got, err := Dir(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(findingText(got), "not shipped in the package") {
		t.Errorf("a dangling plugin path should be reported:\n%s", findingText(got))
	}
}

func TestDirReportsUnparseablePayload(t *testing.T) {
	fsys := fstest.MapFS{
		"plugin.yaml":     file("name: broken\n"),
		"mcp.json":        file("{not json"),
		"agents/bad.md":   file("no frontmatter here"),
		"commands/why.md": file("unsupported location"),
	}
	got, err := Dir(fsys)
	if err != nil {
		t.Fatal(err)
	}
	text := findingText(got)
	// A broken mcp.json stops those servers from loading at all.
	if !got.HasErrors() {
		t.Errorf("a malformed mcp.json should be an error:\n%s", text)
	}
	// A malformed agent is skipped at load, so it is reported and survived.
	if !strings.Contains(text, "agents/bad.md") {
		t.Errorf("unparseable subagent not reported:\n%s", text)
	}
	if !strings.Contains(text, "commands") {
		t.Errorf("unsupported content not reported:\n%s", text)
	}
}

func TestDirRequiresAManifest(t *testing.T) {
	if _, err := Dir(fstest.MapFS{"skills/x/SKILL.md": file("# x\n")}); err == nil {
		t.Error("a directory with no manifest is not a package")
	}
	if _, err := Dir(fstest.MapFS{"plugin.yaml": file("- not a mapping\n")}); err == nil {
		t.Error("a manifest that is not a document should fail")
	}
}

func TestDirReportsAManifestProblemWithoutLosingTheRest(t *testing.T) {
	fsys := fstest.MapFS{
		"plugin.yaml":       file("name: Bad Name\n"),
		"skills/x/SKILL.md": file("# x\n"),
	}
	got, err := Dir(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if !got.HasErrors() {
		t.Error("an invalid name should be an error")
	}
	if len(got.Skills) != 1 {
		t.Error("the rest of the package should still be described")
	}
}

// renderAll flattens everything the inspection would publish, so a leak test
// cannot pass by missing a field.
func renderAll(t *testing.T, p Package) string {
	t.Helper()
	var b strings.Builder
	b.WriteString(findingText(p))
	b.WriteString(strings.Join(p.Skills, " "))
	b.WriteString(strings.Join(p.EnvRefs, " "))
	b.WriteString(strings.Join(p.PluginPaths, " "))
	for _, s := range p.Subagents {
		b.WriteString(s.Name + s.Model + strings.Join(s.Tools, " "))
	}
	for _, m := range p.MCP {
		b.WriteString(m.ID + m.Transport + m.Executable + m.Host)
	}
	for _, h := range p.Hooks {
		b.WriteString(h.Event + h.Type + h.Matcher + h.Executable + h.Host + h.MCPServer + h.MCPTool)
	}
	return b.String()
}

// A plugin is normally a checkout, so its repository furniture is not content
// to complain about.
func TestDirIgnoresDotEntries(t *testing.T) {
	fsys := fstest.MapFS{
		"plugin.yaml":     file("name: cloned\n"),
		".gitignore":      file("*.log\n"),
		".github/ci.yaml": file("on: push\n"),
	}
	got, err := Dir(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Findings) != 0 {
		t.Errorf("dot entries should be quiet:\n%s", findingText(got))
	}
}
