package desktop

import (
	"context"
	"fmt"
	"os"

	"github.com/icloudbb/buildmax/internal/agentapp"
	"github.com/icloudbb/buildmax/internal/config"
	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	"github.com/icloudbb/buildmax/internal/interface/pluginmgr"
)

// The plugin surface. It reports what a project's runtime actually resolved
// rather than what the directory holds, because a plugin whose skill a
// workspace overrides is not contributing that skill however installed it is.
//
// Installing, updating, and removing go through internal/interface/pluginmgr,
// the same mechanism the CLI runs. See docs/design/plugin-marketplace.md §9.2.

// PluginEnvVar is one declared environment variable and whether it is set here.
type PluginEnvVar struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
	Set         bool   `json:"set"`
}

// PluginEntry is one installed plugin as the app shows it.
type PluginEntry struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path"`
	Source      string `json:"source"`
	// State is active, disabled, refused, or error — a decision and a defect
	// are different things and a reader should not have to tell them apart
	// from a message.
	State string `json:"state"`

	Version string `json:"version,omitempty"`
	Digest  string `json:"digest,omitempty"`

	RemoteURL string `json:"remote_url,omitempty"`
	Branch    string `json:"branch,omitempty"`
	Commit    string `json:"commit,omitempty"`
	Dirty     bool   `json:"dirty,omitempty"`

	Skills    []string       `json:"skills,omitempty"`
	Subagents []string       `json:"subagents,omitempty"`
	MCP       []string       `json:"mcp,omitempty"`
	Hooks     []string       `json:"hooks,omitempty"`
	Env       []PluginEnvVar `json:"env,omitempty"`
	// Shadowed names what a higher layer overrode, so a plugin does not read
	// as fully active while part of it never loads.
	Shadowed []string `json:"shadowed,omitempty"`
	Problems []string `json:"problems,omitempty"`
}

// PluginsResult is the project's plugin inventory.
type PluginsResult struct {
	Dir     string        `json:"dir"`
	Plugins []PluginEntry `json:"plugins"`
	// AllowedSources is the operator's restriction, empty when there is none.
	AllowedSources []string `json:"allowed_sources,omitempty"`
	// Notes are directory-level findings: a stray directory, unreadable state.
	Notes []string `json:"notes,omitempty"`
}

// GetPlugins returns what this project's runtime loaded.
//
// It reads the runtime's snapshot rather than scanning again, so what is shown
// is what the running agent has — an install completed a moment ago appears
// once the runtime is rebuilt, which is the same rule every other surface has.
func (a *App) GetPlugins(projectID string) (PluginsResult, error) {
	ag, err := a.agentAppForProject(projectID)
	if err != nil {
		return PluginsResult{}, err
	}
	snapshot := ag.Plugins()
	out := PluginsResult{
		Dir:            snapshot.Discovery.Dir,
		AllowedSources: snapshot.Discovery.Policy.AllowedSources,
	}
	for _, f := range snapshot.Discovery.Findings {
		out.Notes = append(out.Notes, f.String())
	}
	if err := snapshot.Discovery.StateErr; err != nil {
		out.Notes = append(out.Notes,
			"plugin state could not be read, so provenance and disabled state are unknown: "+err.Error())
	}

	provenance := map[string]coreplugin.Provenance{}
	for _, p := range snapshot.Provenance(context.Background()) {
		provenance[p.Name] = p
	}
	for _, p := range snapshot.Discovery.Plugins {
		out.Plugins = append(out.Plugins, pluginEntry(p, provenance[p.Name()], snapshot))
	}
	return out, nil
}

func pluginEntry(p config.DiscoveredPlugin, prov coreplugin.Provenance, snapshot agentapp.PluginSnapshot) PluginEntry {
	entry := PluginEntry{
		Name:        p.Name(),
		DisplayName: p.Manifest.DisplayTitle(),
		Description: p.Manifest.Description,
		Path:        p.Path,
		Source:      string(p.Source()),
		State:       pluginState(p),
		Version:     p.State.ReleaseVersion,
		Digest:      p.State.Digest,
		RemoteURL:   prov.RemoteURL,
		Branch:      prov.Branch,
		Commit:      prov.Commit,
		Shadowed:    snapshot.ShadowedNames(p.Name()),
	}
	if prov.Dirty != nil {
		entry.Dirty = *prov.Dirty
	}
	for _, f := range p.Findings {
		entry.Problems = append(entry.Problems, f.String())
	}
	for _, e := range p.Manifest.Env {
		_, set := os.LookupEnv(e.Name)
		entry.Env = append(entry.Env, PluginEnvVar{
			Name: e.Name, Description: e.Description, Required: e.Required, Set: set,
		})
	}
	return entry
}

func pluginState(p config.DiscoveredPlugin) string {
	switch {
	case p.PolicyRefused:
		return "refused"
	case coreplugin.HasErrors(p.Findings):
		return "error"
	case p.State.Disabled:
		return "disabled"
	default:
		return "active"
	}
}

// InstallPluginPlan is what an install would do, resolved before anything is
// downloaded so the app can show it while the decision is still open.
type InstallPluginPlan struct {
	Name             string   `json:"name"`
	Version          string   `json:"version"`
	Digest           string   `json:"digest"`
	SizeBytes        int64    `json:"size_bytes"`
	PublishedBy      string   `json:"published_by,omitempty"`
	AlreadyInstalled bool     `json:"already_installed"`
	Skills           []string `json:"skills,omitempty"`
	Subagents        []string `json:"subagents,omitempty"`
	MCP              []string `json:"mcp,omitempty"`
	Hooks            []string `json:"hooks,omitempty"`
	// MissingEnv names the variables this release reads that are not set here,
	// which is the usual reason a plugin looks installed and does nothing.
	MissingEnv []string `json:"missing_env,omitempty"`
	// DirtySource says the release was packed from a working tree that was not
	// the commit it names.
	DirtySource bool `json:"dirty_source,omitempty"`
}

// PlanPluginInstall resolves which release an install would take.
func (a *App) PlanPluginInstall(name, version string, update bool) (InstallPluginPlan, error) {
	session, err := pluginmgr.Open()
	if err != nil {
		return InstallPluginPlan{}, err
	}
	opts := pluginmgr.Options{Name: name, Version: version, RequireInstalled: update}
	plan, err := session.Resolve(context.Background(), opts)
	if err != nil {
		return InstallPluginPlan{}, err
	}
	return installPlan(name, plan), nil
}

// InstallPlugin downloads and installs a release.
//
// It resolves again rather than taking the plan the app was shown: a release
// published between the two is a different install, and acting on a stale plan
// would put bytes on disk that nobody was shown.
func (a *App) InstallPlugin(name, version string, update bool) (InstallPluginPlan, error) {
	session, err := pluginmgr.Open()
	if err != nil {
		return InstallPluginPlan{}, err
	}
	opts := pluginmgr.Options{Name: name, Version: version, RequireInstalled: update}
	plan, err := session.Resolve(context.Background(), opts)
	if err != nil {
		return InstallPluginPlan{}, err
	}
	if plan.AlreadyInstalled {
		return installPlan(name, plan), nil
	}
	if err := session.Install(context.Background(), opts, plan.Release); err != nil {
		return InstallPluginPlan{}, err
	}
	// The runtime keeps the snapshot it was assembled with, so the caller has
	// to rebuild before a run sees this.
	a.rebuildRuntimes()
	return installPlan(name, plan), nil
}

// UninstallPlugin removes an installed plugin.
func (a *App) UninstallPlugin(name string, force bool) error {
	if _, err := pluginmgr.Uninstall(name, force); err != nil {
		return err
	}
	a.rebuildRuntimes()
	return nil
}

// SetPluginDisabled stops a plugin loading, or lets it load again.
func (a *App) SetPluginDisabled(name string, disabled bool) error {
	if _, err := pluginmgr.SetDisabled(name, disabled); err != nil {
		return err
	}
	a.rebuildRuntimes()
	return nil
}

func installPlan(name string, plan pluginmgr.Plan) InstallPluginPlan {
	release := plan.Release
	out := InstallPluginPlan{
		Name:             name,
		Version:          release.Version,
		Digest:           release.Digest,
		SizeBytes:        release.SizeBytes,
		PublishedBy:      release.PublishedBy,
		AlreadyInstalled: plan.AlreadyInstalled,
		Skills:           release.Inspection.Skills,
		DirtySource:      release.Source.Dirty,
	}
	for _, s := range release.Inspection.Subagents {
		out.Subagents = append(out.Subagents, s.Name)
	}
	for _, s := range release.Inspection.MCP {
		out.MCP = append(out.MCP, fmt.Sprintf("%s (%s)", s.ID, s.Transport))
	}
	for _, h := range release.Inspection.Hooks {
		out.Hooks = append(out.Hooks, fmt.Sprintf("%s %s", h.Event, h.Type))
	}
	for _, envName := range release.Inspection.EnvRefs {
		if _, set := os.LookupEnv(envName); !set {
			out.MissingEnv = append(out.MissingEnv, envName)
		}
	}
	return out
}
