package architecture_test

// Documentation constraints. These keep docs/ and the manual/ manual honest about
// things the code is the source of truth for: every relative link must resolve,
// every document must appear in the complete index, every environment variable
// must be documented, every LLM-facing tool name must appear in the user-facing
// tool guide, every cited file and `./make` command must exist, and every CLI
// command must reach the reference page.
//
// Conventions these enforce: docs/contribute/documentation.md.

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/tool"
)

// markdownFiles returns every documentation file whose links are checked.
// manual/ is the end-user manual and docs/ the contributor and design set; both
// are held to the same link and path integrity.
func markdownFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	for _, dir := range []string{"docs", "manual"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, ".md") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	for _, name := range []string{
		"README.md",
		"CHANGELOG.md",
		"CONTRIBUTING.md",
		"SECURITY.md",
		"AGENTS.md",
		"CLAUDE.md",
		".buildmax/README.md",
		// Community health files live in .github/, where GitHub still surfaces
		// them; their links are checked from there.
		".github/CODE_OF_CONDUCT.md",
		".github/GOVERNANCE.md",
		".github/MAINTAINERS.md",
		".github/SUPPORT.md",
		".github/TRADEMARKS.md",
	} {
		p := filepath.Join(root, name)
		if _, err := os.Stat(p); err == nil {
			files = append(files, p)
		}
	}
	return files
}

var markdownLinkRe = regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)

// TestDocsLinksResolve fails when a relative markdown link points at a file
// that does not exist. Moving a document without updating its inbound links is
// the most common way documentation rots.
func TestDocsLinksResolve(t *testing.T) {
	root := repoRoot(t)
	for _, file := range markdownFiles(t, root) {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, m := range markdownLinkRe.FindAllStringSubmatch(string(body), -1) {
			target := strings.TrimSpace(m[1])
			if i := strings.Index(target, "#"); i >= 0 {
				target = target[:i]
			}
			if target == "" ||
				strings.HasPrefix(target, "http://") ||
				strings.HasPrefix(target, "https://") ||
				strings.HasPrefix(target, "mailto:") {
				continue
			}
			resolved := filepath.Join(filepath.Dir(file), target)
			if _, err := os.Stat(resolved); err != nil {
				rel, _ := filepath.Rel(root, file)
				t.Errorf("%s: broken link %q", rel, m[1])
			}
		}
	}
}

// TestDocsIndexCoversEveryDocument keeps the mobile-friendly directory
// complete. Unlike the task-oriented docs/README.md, docs/index.md promises a
// direct link to every English and translated Markdown file under docs/.
func TestDocsIndexCoversEveryDocument(t *testing.T) {
	root := repoRoot(t)
	indexPath := filepath.Join(root, "docs", "index.md")
	body, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read docs/index.md: %v", err)
	}

	linked := map[string]bool{}
	for _, m := range markdownLinkRe.FindAllStringSubmatch(string(body), -1) {
		target := strings.TrimSpace(m[1])
		if i := strings.Index(target, "#"); i >= 0 {
			target = target[:i]
		}
		if target == "" ||
			strings.HasPrefix(target, "http://") ||
			strings.HasPrefix(target, "https://") ||
			strings.HasPrefix(target, "mailto:") {
			continue
		}
		linked[filepath.Clean(filepath.Join(filepath.Dir(indexPath), filepath.FromSlash(target)))] = true
	}

	err = filepath.WalkDir(filepath.Join(root, "docs"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		if !linked[path] {
			rel, _ := filepath.Rel(root, path)
			t.Errorf("%s is missing from docs/index.md", filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk docs: %v", err)
	}
}

// TestEnvVarsDocumented fails when config.EnvVars gains a variable that
// docs/reference/configuration.md does not mention. env_spec.go is the source
// of truth; this keeps the reference table from silently falling behind it.
func TestEnvVarsDocumented(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "docs", "reference", "configuration.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read configuration reference: %v", err)
	}
	doc := string(body)
	for _, ev := range config.EnvVars() {
		if !strings.Contains(doc, ev.Name) {
			t.Errorf("environment variable %s is in config.EnvVars but not in docs/reference/configuration.md", ev.Name)
		}
	}
}

// TestToolNamesDocumented fails when a tool name constant is missing from the
// user-facing tool guide. These exact strings are what users type into hook
// matchers and subagent "tools:" fields, so a rename that skips the docs
// silently breaks working configuration.
func TestToolNamesDocumented(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "manual", "tools.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read tool guide: %v", err)
	}
	doc := string(body)
	names := []string{
		tool.ToolNameRead, tool.ToolNameWrite, tool.ToolNameEdit,
		tool.ToolNameGlob, tool.ToolNameGrep, tool.ToolNameBash,
		tool.ToolNameWebFetch, tool.ToolNameTodoWrite,
		tool.ToolNameSkill, tool.ToolNameTask, tool.ToolNameWorktree,
		tool.ToolNameLoadMCPTools, tool.ToolNameCallMCPTool,
	}
	for _, name := range names {
		if !strings.Contains(doc, "`"+name+"`") {
			t.Errorf("tool %q is registered but not documented in manual/tools.md", name)
		}
	}
}

// TestArchitectureToolInventoryCoversEveryToolNameConstant fails when the
// contributor architecture inventory falls behind names.go. Unlike the
// user-facing guide, this inventory accounts for surface-scoped tools that are
// not registered in every runtime.
func TestArchitectureToolInventoryCoversEveryToolNameConstant(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "docs", "contribute", "architecture", "tools.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read tool architecture: %v", err)
	}
	doc := string(body)

	namesPath := filepath.Join(root, "internal", "tool", "names.go")
	file, err := parser.ParseFile(token.NewFileSet(), namesPath, nil, 0)
	if err != nil {
		t.Fatalf("parse tool names: %v", err)
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			values, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, ident := range values.Names {
				name, ok := strings.CutPrefix(ident.Name, "ToolName")
				if ok && !strings.Contains(doc, "`"+name+"`") {
					t.Errorf("tool %q is declared in internal/tool/names.go but not inventoried in docs/contribute/architecture/tools.md", name)
				}
			}
		}
	}
}

// agentsMDPathRe matches repository paths written in backticks, e.g. `internal/config`.
var agentsMDPathRe = regexp.MustCompile("`((?:internal|cmd|docs|manual|portal|gui|desktop|config-examples|deployment|eval|scripts|\\.github|\\.buildmax)/[A-Za-z0-9_./-]*)`")

// generatedSegments name directories absent from a fresh checkout. Documentation
// legitimately refers to them -- gui/dist, portal/dist, gui/node_modules after a
// build, .local after `./make setup local` -- but whether they happen to be on
// the machine running the tests says nothing about whether the document is
// accurate, and .local would make these tests depend on one contributor's own
// configuration.
var generatedSegments = map[string]bool{
	"dist": true, "node_modules": true, "bin": true, "build": true, ".local": true,
}

// generatedFiles are single paths in the same category: written by a command
// rather than committed, so a fresh clone does not have them. Documentation
// names deployment/compose/.env because that is where the Compose stack's
// secrets live; deployment/compose/generate-env.sh writes it.
var generatedFiles = map[string]bool{
	"deployment/compose/.env": true,
}

// isGenerated reports whether p is a path a checkout has to be built or set up
// to have, either by its own name or by sitting under such a directory.
func isGenerated(p string) bool {
	if generatedFiles[p] {
		return true
	}
	for _, seg := range strings.Split(p, "/") {
		if generatedSegments[seg] {
			return true
		}
	}
	return false
}

// TestAgentsMDPathsExist fails when AGENTS.md cites a repository path that does
// not exist. AGENTS.md is loaded into every agent session, so a stale path there
// misleads more often than a stale path in any single document — and it has
// repeatedly been the source that other documents copied from.
//
// Generated paths are skipped: they are absent from a fresh checkout, and
// requiring them made this test pass on a developer machine and fail in CI.
func TestAgentsMDPathsExist(t *testing.T) {
	root := repoRoot(t)
	body, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	for _, m := range agentsMDPathRe.FindAllStringSubmatch(string(body), -1) {
		p := strings.TrimSuffix(m[1], "/")
		if isGenerated(p) {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			t.Errorf("AGENTS.md cites %q, which does not exist", m[1])
		}
	}
}

// TestAgentsMDRoutesExist fails when AGENTS.md lists an HTTP route the server
// does not register. The route list is a summary, so extra registered routes are
// fine — a documented route that does not exist is not.
//
// It scans every package under internal/server. Reading only handlers/routes.go
// would see 7 of the registrations: that file composes the subpackages, and
// each one registers its own routes. A citation of any of the others would look
// like a route the server does not have.
func TestAgentsMDRoutesExist(t *testing.T) {
	root := repoRoot(t)
	paths := map[string]bool{}
	for route := range registeredRoutes(t, root) {
		_, pattern, ok := strings.Cut(route, " ")
		if !ok {
			t.Fatalf("route %q is not %q", route, "METHOD /pattern")
		}
		paths[pattern] = true
	}

	body, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	cited := regexp.MustCompile("`(?:[A-Z/]+ )?(/api/[^`,;]+)`").FindAllStringSubmatch(string(body), -1)
	for _, m := range cited {
		route := strings.TrimSpace(m[1])
		// Prose forms like /api/worker/* and /api/spaces/{space_id}/... name a
		// group of routes rather than one, and have nothing to match against.
		if strings.HasSuffix(route, "*") || strings.HasSuffix(route, "...") {
			continue
		}
		if !paths[route] {
			t.Errorf("AGENTS.md documents route %q, which the server does not register", route)
		}
	}
}

// TestWorkspaceAgentConfigMatchesRepository guards the checked-in dogfooding
// configuration against the same drift that affects human-facing docs. These
// files are executable instructions: stale commands can make an agent dirty a
// worktree or publish changes unexpectedly.
func TestWorkspaceAgentConfigMatchesRepository(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range []string{
		".buildmax/skills/smoke/SKILL.md",
	} {
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		for _, stale := range []string{"`go test ./...`", "module buildmax", "rm -rf"} {
			if strings.Contains(string(body), stale) {
				t.Errorf("%s contains stale or unsafe instruction %q", rel, stale)
			}
		}
	}

	module, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	moduleLine := strings.SplitN(string(module), "\n", 2)[0]
	smoke, err := os.ReadFile(filepath.Join(root, ".buildmax", "skills", "smoke", "SKILL.md"))
	if err != nil {
		t.Fatalf("read smoke skill: %v", err)
	}
	if !strings.Contains(string(smoke), moduleLine) {
		t.Errorf("smoke skill does not use current %q", moduleLine)
	}
}

func TestWorkspaceSubagentToolsExist(t *testing.T) {
	root := repoRoot(t)
	known := map[string]bool{
		tool.ToolNameRead: true, tool.ToolNameWrite: true, tool.ToolNameEdit: true,
		tool.ToolNameGlob: true, tool.ToolNameGrep: true, tool.ToolNameBash: true,
		tool.ToolNameWebFetch: true, tool.ToolNameTodoWrite: true,
		tool.ToolNameSkill: true, tool.ToolNameTask: true,
		tool.ToolNameLoadMCPTools: true, tool.ToolNameCallMCPTool: true,
	}
	files, err := filepath.Glob(filepath.Join(root, ".buildmax", "agents", "*.md"))
	if err != nil {
		t.Fatalf("list workspace agents: %v", err)
	}
	toolsLine := regexp.MustCompile(`(?m)^tools:\s*(.+)$`)
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		match := toolsLine.FindStringSubmatch(string(body))
		if match == nil {
			t.Errorf("%s has no tools frontmatter", file)
			continue
		}
		for _, name := range strings.Split(match[1], ",") {
			name = strings.TrimSpace(name)
			if !known[name] {
				t.Errorf("%s cites unknown tool %q", file, name)
			}
		}
	}
}

func TestWorkspaceMCPPathsExist(t *testing.T) {
	root := repoRoot(t)
	body, err := os.ReadFile(filepath.Join(root, ".buildmax", "mcp.json"))
	if err != nil {
		t.Fatalf("read .buildmax/mcp.json: %v", err)
	}
	var config struct {
		Servers map[string]struct {
			Args []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(body, &config); err != nil {
		t.Fatalf("parse .buildmax/mcp.json: %v", err)
	}
	for server, cfg := range config.Servers {
		for _, arg := range cfg.Args {
			const prefix = "${WORKSPACE_ROOT}/"
			if !strings.HasPrefix(arg, prefix) {
				continue
			}
			if _, err := os.Stat(filepath.Join(root, strings.TrimPrefix(arg, prefix))); err != nil {
				t.Errorf("MCP server %q cites missing workspace path %q", server, arg)
			}
		}
	}
}

// The checked-in workspace configuration is executable input, so exercise the
// same parsers used by an agent run instead of relying only on documentation
// shape checks above.
func TestWorkspaceAgentConfigLoadsWithRuntimeParsers(t *testing.T) {
	root := repoRoot(t)
	t.Setenv(config.EnvKeyBuildmaxHome, t.TempDir())

	skills := tool.DiscoverSkillEntries([]string{filepath.Join(root, ".buildmax", "skills")})
	var skillNames []string
	for _, skill := range skills {
		skillNames = append(skillNames, skill.Name)
	}
	wantSkills := []string{"drive-desktop", "drive-portal", "smoke"}
	if strings.Join(skillNames, ",") != strings.Join(wantSkills, ",") {
		t.Fatalf("workspace skills = %v, want %v", skillNames, wantSkills)
	}

	defs, err := tool.LoadAgentDefs(filepath.Join(root, ".buildmax", "agents"))
	if err != nil {
		t.Fatalf("load workspace agents: %v", err)
	}
	if len(defs) != 1 || defs[0].Name != "sample-researcher" {
		t.Fatalf("workspace agents = %v, want sample-researcher", defs)
	}
	wantTools := []string{"Glob", "Grep", "Read", "WebFetch"}
	if strings.Join(defs[0].ToolNames, ",") != strings.Join(wantTools, ",") {
		t.Fatalf("sample-researcher tools = %v, want %v", defs[0].ToolNames, wantTools)
	}

	mcpConfig, err := config.LoadMCPConfigForWorkspace(root)
	if err != nil {
		t.Fatalf("load workspace MCP config: %v", err)
	}
	if mcpConfig == nil || len(mcpConfig.MCPServers) != 1 {
		t.Fatalf("workspace MCP servers = %v, want only mcp", mcpConfig)
	}
	server, ok := mcpConfig.MCPServers["mcp"]
	if !ok {
		t.Fatal("workspace MCP config did not load mcp")
	}
	wantServerPath := filepath.Join(root, "tools", "mcp")
	if len(server.Args) != 2 || server.Args[0] != "run" || filepath.Clean(server.Args[1]) != wantServerPath {
		t.Fatalf("mcp args = %v, want [run %s]", server.Args, wantServerPath)
	}
}

// The three checks below extend the ones above from AGENTS.md to every
// document, and from paths to the two other things a reader can act on and get
// wrong: a task-runner command, and the command list a user looks up.
//
// Each carries the drift it finds today, keyed to the issue open against it.
// Fixing one means deleting its entry — and an entry nothing reports fails too,
// so a list cannot quietly become a permanent exemption for something already
// repaired.

// staleMakeCommands is documented `./make` usage that tools/mk does not dispatch.
var staleMakeCommands = map[string]string{}

var documentedMakeCommandRe = regexp.MustCompile(`(?:\./make|make\.bat) ([a-z][a-z0-9-]*)`)

// TestDocumentedMakeCommandsExist fails when a document tells a contributor to
// run a task-runner command that no longer exists. Renaming one is cheap and
// the call sites are prose, so nothing else notices.
func TestDocumentedMakeCommandsExist(t *testing.T) {
	root := repoRoot(t)
	dispatched := taskRunnerCommands(t, root)
	seen := map[string]bool{}

	for _, file := range markdownFiles(t, root) {
		rel := docPath(root, file)
		// Design records describe the plan of the day. AGENTS.md settles a
		// conflict in favour of current code, so a record naming a command that
		// has since been renamed is history rather than drift.
		if isDesignRecord(rel) {
			continue
		}
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, m := range documentedMakeCommandRe.FindAllStringSubmatch(string(body), -1) {
			if dispatched[m[1]] {
				continue
			}
			key := rel + ": " + m[1]
			if _, known := staleMakeCommands[key]; known {
				seen[key] = true
				continue
			}
			t.Errorf("%s documents `./make %s`, which tools/mk does not dispatch", rel, m[1])
		}
	}
	assertAllReported(t, "staleMakeCommands", staleMakeCommands, seen)
}

// taskRunnerCommands reads the names tools/mk dispatches. The switch is bounded
// first: the file holds other switches whose cases are not commands.
func taskRunnerCommands(t *testing.T, root string) map[string]bool {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, "tools", "mk", "main.go"))
	if err != nil {
		t.Fatalf("read tools/mk/main.go: %v", err)
	}
	const marker = "switch args[0] {"
	start := strings.Index(string(body), marker)
	if start < 0 {
		t.Fatalf("tools/mk/main.go has no %q; this test can no longer find the command list", marker)
	}
	end := strings.Index(string(body)[start:], "\n\t}\n")
	if end < 0 {
		t.Fatal("tools/mk/main.go: the dispatch switch does not close where expected")
	}
	names := map[string]bool{}
	for _, m := range regexp.MustCompile(`case "([a-z][a-z0-9-]*)"`).
		FindAllStringSubmatch(string(body)[start:start+end], -1) {
		names[m[1]] = true
	}
	if len(names) < 5 {
		t.Fatalf("found only %d dispatch cases; the switch shape changed", len(names))
	}
	return names
}

// staleFilePaths is a cited file that does not exist.
var staleFilePaths = map[string]string{}

// documentedFileRe matches a backticked repository path carrying a file
// extension. Only files are checked: a bare `internal/app` is often prose about
// a package that deliberately does not exist — packages.md says exactly that —
// and `desktop/message-blocked` is an event name that reads like a path. The
// extension separates a file reference from both without a list of exceptions
// to maintain.
var documentedFileRe = regexp.MustCompile("`([A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)+\\.(?:go|md|ya?ml|jsonc?|tsx?|js|sh|toml|mod|sum|bat))`")

// TestDocumentedFilePathsExist extends TestAgentsMDPathsExist to every
// document. A file moving into its own package is ordinary, and the paragraph
// describing it is then worse than nothing: it sends a reader to a path that
// does not exist, in the document that was supposed to save them the search.
func TestDocumentedFilePathsExist(t *testing.T) {
	root := repoRoot(t)
	seen := map[string]bool{}

	for _, file := range markdownFiles(t, root) {
		rel := docPath(root, file)
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		text := string(body)
		// A design record's prose may name a file that was deleted or one the
		// plan has not written yet — that is what a record is for. Its tables
		// are the exception: a "where this lives" row is a reader's index, and
		// an index that points nowhere is drift rather than history.
		if isDesignRecord(rel) {
			text = tableRowsOnly(text)
		}
		for _, m := range documentedFileRe.FindAllStringSubmatch(text, -1) {
			cited := m[1]
			if isGenerated(cited) {
				continue
			}
			// Repository-relative only: a first segment that is not in the tree
			// belongs to someone else's layout, or to an example.
			if _, err := os.Stat(filepath.Join(root, strings.SplitN(cited, "/", 2)[0])); err != nil {
				continue
			}
			if _, err := os.Stat(filepath.Join(root, cited)); err == nil {
				continue
			}
			key := rel + ": " + cited
			if _, known := staleFilePaths[key]; known {
				seen[key] = true
				continue
			}
			t.Errorf("%s cites %s, which does not exist", rel, cited)
		}
	}
	assertAllReported(t, "staleFilePaths", staleFilePaths, seen)
}

func isDesignRecord(path string) bool {
	return strings.HasPrefix(path, "docs/design/") ||
		strings.HasPrefix(path, "docs/zh-CN/design/")
}

// undocumentedCLICommands is a command the binary offers that the reference
// page does not list.
var undocumentedCLICommands = map[string]string{}

// TestCLIReferenceCoversEveryCommand fails when a command reaches the binary
// without reaching manual/cli.md. That page is where a user looks for the command
// list, so a command missing from it is one nobody finds.
func TestCLIReferenceCoversEveryCommand(t *testing.T) {
	root := repoRoot(t)
	const page = "manual/cli.md"
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(page)))
	if err != nil {
		t.Fatalf("read %s: %v", page, err)
	}
	seen := map[string]bool{}
	for _, name := range rootCLICommands(t, root) {
		if strings.Contains(string(body), "buildmax "+name) {
			continue
		}
		if _, known := undocumentedCLICommands[name]; known {
			seen[name] = true
			continue
		}
		t.Errorf("%s does not document `buildmax %s`", page, name)
	}
	assertAllReported(t, "undocumentedCLICommands", undocumentedCLICommands, seen)
}

// rootCLICommands resolves the constructors root.go registers to their Use:
// names, so the page is compared against the binary rather than against a
// second list someone has to remember to update.
func rootCLICommands(t *testing.T, root string) []string {
	t.Helper()
	dir := filepath.Join(root, "internal", "interface", "cli")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var sources strings.Builder
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		sources.Write(body)
	}
	all := sources.String()

	rootBody, err := os.ReadFile(filepath.Join(dir, "root.go"))
	if err != nil {
		t.Fatalf("read root.go: %v", err)
	}
	useRe := regexp.MustCompile(`Use:\s+"([^"]+)"`)
	var names []string
	for _, m := range regexp.MustCompile(`AddCommand\((new[A-Za-z0-9_]*Command)\(`).
		FindAllStringSubmatch(string(rootBody), -1) {
		at := strings.Index(all, "func "+m[1]+"(")
		if at < 0 {
			t.Errorf("root.go registers %s(), which is not defined in the package", m[1])
			continue
		}
		use := useRe.FindStringSubmatch(all[at:])
		if use == nil {
			t.Errorf("%s() has no Use: field", m[1])
			continue
		}
		names = append(names, strings.Fields(use[1])[0])
	}
	if len(names) < 5 {
		t.Fatalf("found only %d root commands; the registration shape changed", len(names))
	}
	return names
}

// docPath is the repository-relative path with forward slashes, so a key and a
// message read the same on every platform. The Windows job found the need for
// it: filepath.Rel hands back backslashes there, so every known-drift entry
// both fired as an error and was reported as never seen.
func docPath(root, file string) string {
	rel, err := filepath.Rel(root, file)
	if err != nil {
		return file
	}
	return filepath.ToSlash(rel)
}

// assertAllReported fails for a known-drift entry that nothing found, so the
// list shrinks as the drift is fixed instead of outliving it.
func assertAllReported(t *testing.T, name string, known map[string]string, seen map[string]bool) {
	t.Helper()
	for key, issue := range known {
		if !seen[key] {
			t.Errorf("%s lists %q (%s), but nothing reported it; delete the entry if it is fixed", name, key, issue)
		}
	}
}

// tableRowsOnly keeps a document's Markdown table rows and drops everything
// else, so a check can hold a reference table to a stricter standard than the
// prose around it.
func tableRowsOnly(text string) string {
	var rows strings.Builder
	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "|") {
			rows.WriteString(line)
			rows.WriteString("\n")
		}
	}
	return rows.String()
}
