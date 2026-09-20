// Package inspect derives a bounded, sanitized description of what a plugin
// package contributes.
//
// It reads a package the way a run would — the same manifest, MCP, hook, and
// subagent parsers — but produces a report rather than a runtime. Publication
// and `plugin validate` both use it, which is why it depends only on core: a
// second implementation would let a package pass one check and fail the other.
//
// It is a subpackage because internal/core/subagent already depends on
// internal/core/plugin for the layer vocabulary, so the parent cannot import it
// back.
//
// What it deliberately does not carry: command arguments, header values,
// environment values, prompt text, and file contents. A catalog record is not a
// place to publish the inside of somebody's configuration.
package plugininspect

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	coreconnect "github.com/icloudbb/buildmax/internal/core/appconnect"
	corehook "github.com/icloudbb/buildmax/internal/core/hook"
	coremcp "github.com/icloudbb/buildmax/internal/core/mcp"
	"github.com/icloudbb/buildmax/internal/core/plugin"
	"github.com/icloudbb/buildmax/internal/core/subagent"
)

// Package layout, relative to the plugin root.
const (
	skillsDir    = "skills"
	agentsDir    = "agents"
	skillFile    = "SKILL.md"
	mcpFile      = "mcp.json"
	hooksFile    = "hooks.yaml"
	hookScripts  = "hooks"
	readmeFile   = "README.md"
	licenseFile  = "LICENSE"
	maxInspected = 512
)

// Package is what one plugin directory contributes.
type Package struct {
	Manifest  plugin.Manifest
	Skills    []string
	Subagents []plugin.Subagent
	MCP       []plugin.MCPServer
	Hooks     []plugin.Hook

	// EnvRefs are the environment variable names the payload reads, sorted.
	// Names only: a value here would be a secret in a catalog record.
	EnvRefs []string

	// PluginPaths are files inside the package that its own configuration
	// reaches for, sorted and relative to the plugin root.
	PluginPaths []string

	Findings []plugin.Finding
}

// HasErrors reports whether anything found would stop the package loading.
func (p Package) HasErrors() bool { return plugin.HasErrors(p.Findings) }

// Dir inspects a plugin package rooted at fsys.
//
// The returned error means there is no manifest to describe — every other
// problem is a finding, so one broken file still yields a report of everything
// else the package holds.
func Dir(fsys fs.FS) (Package, error) {
	data, err := fs.ReadFile(fsys, plugin.ManifestFile)
	if err != nil {
		return Package{}, fmt.Errorf("read %s: %w", plugin.ManifestFile, err)
	}
	m, findings, err := plugin.Parse(data)
	if err != nil {
		return Package{}, err
	}

	p := Package{Manifest: m, Findings: findings}
	refs := newRefSet()

	p.Skills = inspectSkills(fsys, &p)
	p.Subagents = inspectSubagents(fsys, &p)
	p.MCP = inspectMCP(fsys, &p, refs)
	p.Hooks = inspectHooks(fsys, &p, refs)
	inspectConnector(fsys, &p, refs)

	p.EnvRefs, p.PluginPaths = refs.sorted()
	p.Findings = append(p.Findings, unsupportedEntries(fsys)...)
	p.Findings = append(p.Findings, missingPluginPaths(fsys, p.PluginPaths)...)
	p.Findings = append(p.Findings, crossCheckEnv(m, p.EnvRefs)...)
	return p, nil
}

func inspectConnector(fsys fs.FS, p *Package, refs *refSet) {
	data, err := fs.ReadFile(fsys, coreconnect.ManifestFile)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		p.Findings = append(p.Findings, readFinding(coreconnect.ManifestFile, err))
		return
	}
	m, err := coreconnect.Parse(data)
	if err != nil {
		p.Findings = append(p.Findings, plugin.Finding{Severity: plugin.SeverityError, Field: coreconnect.ManifestFile, Message: err.Error()})
		return
	}
	refs.env[m.Auth.ClientIDEnv] = true
}

func inspectSkills(fsys fs.FS, p *Package) []string {
	entries, err := fs.ReadDir(fsys, skillsDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() || len(names) >= maxInspected {
			continue
		}
		// A skill is its directory plus SKILL.md; there is nothing in the file
		// that can fail to parse, so presence is the whole check.
		if _, err := fs.Stat(fsys, path.Join(skillsDir, e.Name(), skillFile)); err != nil {
			p.Findings = append(p.Findings, plugin.Finding{
				Severity: plugin.SeverityWarning, Field: path.Join(skillsDir, e.Name()),
				Message: "no " + skillFile + ", so this directory contributes nothing",
			})
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

func inspectSubagents(fsys fs.FS, p *Package) []plugin.Subagent {
	entries, err := fs.ReadDir(fsys, agentsDir)
	if err != nil {
		return nil
	}
	var out []plugin.Subagent
	for _, e := range entries {
		if e.IsDir() || len(out) >= maxInspected {
			continue
		}
		rel := path.Join(agentsDir, e.Name())
		data, err := fs.ReadFile(fsys, rel)
		if err != nil {
			p.Findings = append(p.Findings, readFinding(rel, err))
			continue
		}
		def, err := subagent.ParseDef(data)
		if err != nil {
			// A run skips an unparseable definition with a warning, so
			// publishing one is not fatal either — but it must not be silent.
			p.Findings = append(p.Findings, plugin.Finding{
				Severity: plugin.SeverityWarning, Field: rel,
				Message: "not a subagent definition, so it contributes nothing: " + err.Error(),
			})
			continue
		}
		out = append(out, plugin.Subagent{Name: def.Name, Tools: def.ToolNames, Model: def.Model})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func inspectMCP(fsys fs.FS, p *Package, refs *refSet) []plugin.MCPServer {
	data, err := fs.ReadFile(fsys, mcpFile)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			p.Findings = append(p.Findings, readFinding(mcpFile, err))
		}
		return nil
	}
	root, err := coremcp.ParseConfig(data)
	if err != nil {
		p.Findings = append(p.Findings, plugin.Finding{
			Severity: plugin.SeverityError, Field: mcpFile, Message: err.Error(),
		})
		return nil
	}
	if root == nil {
		return nil
	}

	var out []plugin.MCPServer
	for id, s := range root.MCPServers {
		if err := coremcp.ValidateServerConfig(id, s); err != nil {
			p.Findings = append(p.Findings, plugin.Finding{
				Severity: plugin.SeverityError, Field: mcpFile, Message: err.Error(),
			})
			continue
		}
		refs.scan(s.Command)
		refs.scanAll(s.Args)
		for _, v := range s.Env {
			refs.scan(v)
		}
		refs.scan(s.URL)
		out = append(out, plugin.MCPServer{
			ID:         id,
			Transport:  s.Type,
			Executable: executableName(s.Command),
			Host:       hostOf(s.URL),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func inspectHooks(fsys fs.FS, p *Package, refs *refSet) []plugin.Hook {
	data, err := fs.ReadFile(fsys, hooksFile)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			p.Findings = append(p.Findings, readFinding(hooksFile, err))
		}
		return nil
	}
	cfg, err := corehook.ParseConfig(data)
	if err != nil {
		p.Findings = append(p.Findings, plugin.Finding{
			Severity: plugin.SeverityError, Field: hooksFile, Message: err.Error(),
		})
		return nil
	}

	var out []plugin.Hook
	for _, event := range corehook.EventNames() {
		for _, e := range cfg.Entries(event) {
			refs.scan(e.Command)
			refs.scan(e.URL)
			for _, v := range e.Headers {
				refs.scan(v)
			}
			// allowed_env names what an HTTP hook interpolates, so it is a
			// declared read even though the value never appears here.
			for _, name := range e.AllowedEnv {
				refs.env[name] = true
			}
			// Input and Prompt use ${field} and $ARGUMENTS against the hook
			// payload, not the environment. Only plugin paths are taken.
			refs.scanPathsOnly(e.Prompt)
			refs.scanAnyPathsOnly(e.Input)

			out = append(out, plugin.Hook{
				Event:      event,
				Type:       e.ResolvedType(),
				Matcher:    e.Matcher,
				Executable: executableName(e.Command),
				Host:       hostOf(e.URL),
				MCPServer:  e.Server,
				MCPTool:    e.Tool,
			})
		}
	}
	return out
}

// unsupportedEntries reports package content this build does not read, so a
// misplaced directory cannot pass for a working feature.
func unsupportedEntries(fsys fs.FS) []plugin.Finding {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil
	}
	known := map[string]bool{
		plugin.ManifestFile: true, skillsDir: true, agentsDir: true,
		mcpFile: true, hooksFile: true, hookScripts: true,
		coreconnect.ManifestFile: true,
		readmeFile:               true, licenseFile: true,
	}
	var findings []plugin.Finding
	for _, e := range entries {
		name := e.Name()
		// Dot-prefixed entries are repository furniture — .git, .gitignore,
		// .github. A plugin is normally a checkout, so reporting them as
		// unsupported content would be noise around every real finding.
		if known[name] || strings.HasPrefix(name, ".") {
			continue
		}
		findings = append(findings, plugin.Finding{
			Severity: plugin.SeverityWarning, Field: name,
			Message: "not a supported plugin file; this build ignores it",
		})
	}
	return findings
}

// missingPluginPaths reports a package that points at a file it does not ship.
func missingPluginPaths(fsys fs.FS, paths []string) []plugin.Finding {
	var findings []plugin.Finding
	for _, rel := range paths {
		if _, err := fs.Stat(fsys, rel); err != nil {
			findings = append(findings, plugin.Finding{
				Severity: plugin.SeverityWarning, Field: rel,
				Message: "referenced through ${" + plugin.VarPluginRoot + "} but not shipped in the package",
			})
		}
	}
	return findings
}

// crossCheckEnv compares the declared environment contract against what the
// payload actually reads. A declaration nobody checks becomes fiction, and an
// undeclared requirement is the thing a user is missing at install time.
func crossCheckEnv(m plugin.Manifest, refs []string) []plugin.Finding {
	referenced := map[string]bool{}
	for _, name := range refs {
		referenced[name] = true
	}
	var findings []plugin.Finding
	for _, e := range m.Env {
		if !referenced[e.Name] {
			findings = append(findings, plugin.Finding{
				Severity: plugin.SeverityWarning, Field: "env." + e.Name,
				Message: "declared but never referenced by this package",
			})
		}
	}
	for _, name := range refs {
		if _, declared := m.EnvVarByName(name); !declared {
			findings = append(findings, plugin.Finding{
				Severity: plugin.SeverityWarning, Field: name,
				Message: "read by this package but not declared under env in " + plugin.ManifestFile,
			})
		}
	}
	return findings
}

func readFinding(field string, err error) plugin.Finding {
	return plugin.Finding{Severity: plugin.SeverityError, Field: field, Message: err.Error()}
}

// executableName reduces a command to the program it starts. Arguments are
// dropped rather than recorded: what runs is the useful fact, and what it is
// told to do is the part that may carry a secret.
func executableName(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	first := strings.Fields(command)[0]
	first = strings.ReplaceAll(first, "${"+plugin.VarPluginRoot+"}", "")
	first = strings.ReplaceAll(first, "$"+plugin.VarPluginRoot, "")
	first = strings.TrimPrefix(first, "/")
	if i := strings.LastIndexAny(first, `/\`); i >= 0 {
		first = first[i+1:]
	}
	return first
}

// hostOf reduces a URL to its host, dropping the path and query a reader has no
// need for and that may identify more than the destination.
func hostOf(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if i := strings.Index(rawURL, "://"); i >= 0 {
		rawURL = rawURL[i+3:]
	}
	if i := strings.IndexAny(rawURL, "/?#"); i >= 0 {
		rawURL = rawURL[:i]
	}
	if i := strings.LastIndex(rawURL, "@"); i >= 0 {
		rawURL = rawURL[i+1:]
	}
	return rawURL
}
